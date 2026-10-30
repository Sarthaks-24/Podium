#!/usr/bin/env bash
# Runs podium-server and podium-judge together in testing mode, on Linux or
# inside WSL2: no Google client, no secrets, sign-in from a form. Used by
# `make dev-test`, which also starts the Vite dev server (on Windows, Vite
# reaches the server through WSL2's localhost forwarding), and by the load
# test (tools/loadtest/README.md).
#
#   tools/dev-stack.sh BINDIR
#
# BINDIR holds Linux builds of podium-server and podium-judge; build the
# server with -tags fakeai (as make dev-test does) for the test AI provider,
# which the release binary does not have. State lives
# under $PODIUM_DEV_STATE (default /var/tmp/podium-dev), outside the
# repository: SQLite's WAL needs a local Linux filesystem, not /mnt/<drive>.
# Runs as root and drops privileges: the server as user podium, the judge as
# user judge (both created by deploy/install.sh users).
#
# Optional, for running a second stack beside `make dev-test` or for load
# tests:
#   PODIUM_LISTEN        server address (default 127.0.0.1:8080)
#   PODIUM_JUDGE_LISTEN  judge API address (default 127.0.0.1:8081)
#   PODIUM_BASE_URL      the web app's address (default http://localhost:5173);
#                        set it when Vite runs on another port, since the
#                        server refuses requests from any other origin
#   PODIUM_LOG_LEVEL     both services (default info)
#   PODIUM_DEV_CPUS      pin both services to these CPUs (taskset list such
#                        as 0-1), to approximate a small VM
set -euo pipefail

bin=${1:?usage: dev-stack.sh BINDIR}
state=${PODIUM_DEV_STATE:-/var/tmp/podium-dev}
listen=${PODIUM_LISTEN:-127.0.0.1:8080}
judge_listen=${PODIUM_JUDGE_LISTEN:-127.0.0.1:8081}
level=${PODIUM_LOG_LEVEL:-info}
base_url=${PODIUM_BASE_URL:-http://localhost:5173}
die() { printf 'dev-stack.sh: %s\n' "$*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "run as root (it drops to the podium and judge users)"
id judge >/dev/null 2>&1 || die "user judge missing; run: sudo deploy/install.sh packages isolate users"
server_user=podium
id "$server_user" >/dev/null 2>&1 || server_user=root
pin=()
if [[ -n ${PODIUM_DEV_CPUS:-} ]]; then
  pin=(taskset -c "$PODIUM_DEV_CPUS")
fi

install -d -m 755 "$state"
install -d -m 700 -o "$server_user" "$state/server"
install -d -m 700 -o judge "$state/judge"
install -m 755 "$bin/podium-server" "$state/podium-server"
install -m 755 "$bin/podium-judge" "$state/podium-judge"

trap 'kill 0 2>/dev/null' EXIT INT TERM
"${pin[@]}" runuser -u "$server_user" -- env -i PATH=/usr/bin:/bin PODIUM_MODE=testing \
  PODIUM_BASE_URL="$base_url" PODIUM_LISTEN="$listen" PODIUM_JUDGE_LISTEN="$judge_listen" PODIUM_LOG_LEVEL="$level" \
  PODIUM_DB_PATH="$state/server/podium.db" PODIUM_TESTS_DIR="$state/server/tests" \
  "$state/podium-server" &
sleep 1
"${pin[@]}" runuser -u judge -- env -i PATH=/usr/bin:/bin PODIUM_MODE=testing \
  PODIUM_SERVER_URL="http://$judge_listen" PODIUM_LOG_LEVEL="$level" \
  PODIUM_JUDGE_CACHE_DIR="$state/judge/cache" PODIUM_JUDGE_LANES=2 \
  "$state/podium-judge" work &
printf '\npodium testing stack: server on %s, judge API on %s, state in %s\n' "$listen" "$judge_listen" "$state"
printf 'Open %s and sign in with any email. Ctrl+C stops everything.\n\n' "$base_url"
wait
