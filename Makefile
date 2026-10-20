# podium build and development tasks. Recipes run under bash: Git Bash on
# Windows (make can be started from PowerShell, cmd or Git Bash), any shell
# on Linux. On Windows install make with
#   winget install ezwinports.make
# Run `make help` for the list of targets.

# On Windows a bare "bash" is not found from PowerShell, or resolves to the
# WSL launcher in System32, and make silently falls back to cmd.exe. Name Git
# Bash by its full path instead; its bin/bash.exe puts grep, sed and find on
# PATH. Override with make GIT_BASH=... if Git is installed elsewhere.
ifeq ($(OS),Windows_NT)
  GIT_BASH ?= C:/Program Files/Git/bin/bash.exe
  SHELL := $(GIT_BASH)
else
  SHELL := bash
endif
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
# release runs check and build, and build reinstalls node_modules; never
# let them overlap under make -j.
.NOTPARALLEL:

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Fixed once per run: a recursive variable would ask git again at every
# use, and a file saved during the build would rename the release halfway.
VERSION    := $(VERSION)
WSL_DISTRO ?= Ubuntu-24.04
GOFLAGS_LINUX := GOOS=linux GOARCH=amd64 CGO_ENABLED=0
LDFLAGS    := -s -w -X main.version=$(VERSION)
RELEASE    := release/podium-$(VERSION)

# Run a root command inside WSL2 when on Windows, directly otherwise.
ifeq ($(OS),Windows_NT)
  LINUX = wsl.exe -d $(WSL_DISTRO) -u root --cd "$(CURDIR)" --
  EXE = .exe
  # The repository as WSL sees it, for paths handed to Linux programs.
  LINUX_CURDIR := $(shell wsl.exe -d $(WSL_DISTRO) -- wslpath -a "$(CURDIR)")
else
  LINUX = sudo
  EXE =
  LINUX_CURDIR := $(CURDIR)
endif

.PHONY: help
help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | expand -t 18

.PHONY: dev
dev: ## Run the server and the Vite dev server together (needs server/.env)
	@test -f server/.env || { echo "server/.env is missing. Copy server/.env.example to server/.env and set PODIUM_BASE_URL=http://localhost:5173"; exit 1; }
	@test -d web/node_modules || (cd web && npm ci)
	cd server && go build -o bin/podium-server-dev$(EXE) ./cmd/podium-server
	@trap 'kill 0' EXIT; \
	  (cd server && exec ./bin/podium-server-dev$(EXE) -env-file .env) & \
	  (cd web && exec npm run dev) & \
	  wait

.PHONY: dev-test
dev-test: ## Run server, judge and web app in testing mode: no keys needed (judge in WSL2 on Windows)
	cd server && $(GOFLAGS_LINUX) go build -o bin/linux/ ./cmd/...
	@test -d web/node_modules || (cd web && npm ci)
	@trap 'kill 0' EXIT; \
	  $(LINUX) bash tools/dev-stack.sh server/bin/linux & \
	  (cd web && exec npm run dev) & \
	  wait

.PHONY: web
web: ## Build the web app into web/dist
	cd web && npm ci && npm run build

.PHONY: embed
embed: web ## Copy web/dist into the server so the binary embeds it
	find server/internal/webui/dist -mindepth 1 ! -name .keep -delete
	cp -r web/dist/. server/internal/webui/dist/

.PHONY: build
build: embed ## Build Linux binaries (with the embedded web app) into server/bin
	cd server && $(GOFLAGS_LINUX) go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ ./cmd/...

.PHONY: test
test: ## Unit tests: Go and web
	cd server && go test ./...
	cd web && npm test

.PHONY: check
check: ## Everything CI runs: formatting, vet, svelte-check, ESLint, tests
	@out=$$(cd server && gofmt -l .); test -z "$$out" || { echo "gofmt needed:"; echo "$$out"; exit 1; }
	cd server && go mod tidy -diff
	cd server && go vet ./... && $(GOFLAGS_LINUX) go vet -tags sandbox ./...
	@test -d web/node_modules || (cd web && npm ci)
	cd web && npm run check && npm run lint
	$(MAKE) test

.PHONY: sandbox-test
sandbox-test: ## Hostile-program suite and M2 and M3 acceptance in isolate (Linux, or WSL2 on Windows)
	cd server && $(GOFLAGS_LINUX) go test -c -tags sandbox -o bin/calib.test ./internal/judge/calib
	cd server && $(GOFLAGS_LINUX) go test -c -tags sandbox -o bin/worker.test ./internal/worker
	$(LINUX) bash tools/sandbox-test.sh server/bin/calib.test -test.v -test.timeout 10m
	@# MSYS_NO_PATHCONV: Git Bash would turn the /mnt path into a Windows one.
	MSYS_NO_PATHCONV=1 $(LINUX) bash tools/sandbox-test.sh server/bin/worker.test -test.v -test.timeout 20m -examples "$(LINUX_CURDIR)/problems"

.PHONY: calibrate
calibrate: ## Measure this judge host and print the calibration tables
	cd server && $(GOFLAGS_LINUX) go build -trimpath -o bin/podium-judge ./cmd/podium-judge
	$(LINUX) bash tools/sandbox-test.sh server/bin/podium-judge calibrate -reps 5

.PHONY: release
release: check build ## Build the release tarball in release/
	bash tools/check-placeholders.sh
	rm -rf $(RELEASE) && mkdir -p $(RELEASE)/bin $(RELEASE)/env
	cp server/bin/podium-server server/bin/podium-judge $(RELEASE)/bin/
	cp -r deploy/. $(RELEASE)/
	cp server/.env.example $(RELEASE)/env/server.env.example
	cp judge/.env.example $(RELEASE)/env/judge.env.example
	tar -C release -czf $(RELEASE).tar.gz podium-$(VERSION)
	@echo "Built $(RELEASE).tar.gz"

.PHONY: deploy
deploy: ## Build a release and install it on HOST with install.sh upgrade (make deploy HOST=user@vm)
	@test -n "$(HOST)" || { echo "usage: make deploy HOST=user@vm"; exit 1; }
	bash tools/deploy.sh "$(HOST)"

.PHONY: check-secrets
check-secrets: ## Fail if the staged diff contains a secret
	bash tools/check-secrets.sh

.PHONY: hooks
hooks: ## Install check-secrets as a git pre-commit hook
	hooks=$$(git rev-parse --git-path hooks); mkdir -p "$$hooks"; \
	  printf '#!/bin/sh\nexec bash tools/check-secrets.sh\n' > "$$hooks/pre-commit"; \
	  chmod +x "$$hooks/pre-commit"

.PHONY: clean
clean: ## Remove build output
	rm -rf server/bin release web/dist
	find server/internal/webui/dist -mindepth 1 ! -name .keep -delete
