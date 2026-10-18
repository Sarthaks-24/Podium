#!/usr/bin/env bash
# Runs podium-server and podium-judge together in testing mode, on Linux or
# inside WSL2: no Google client, no secrets, sign-in from a form. Used by
# `make dev-test`, which also starts the Vite dev server (on Windows, Vite
# reaches the server through WSL2's localhost forwarding).
#
#   tools/dev-stack.sh BINDIR
#
# BINDIR holds Linux builds of podium-server and podium-judge. State lives
# under $PODIUM_DEV_STATE (default /var/tmp/podium-dev), outside the
# repository: SQLite's WAL needs a local Linux filesystem, not /mnt/<drive>.
# Runs as root and drops privileges: the server as user podium, the judge as
# user judge (both created by deploy/install.sh users).
set -euo pipefail

bin=${1:?usage: dev-stack.sh BINDIR}
state=${PODIUM_DEV_STATE:-/var/tmp/podium-dev}
die() { printf 'dev-stack.sh: %s\n' "$*" >&2; exit 1; }
[[ $EUID -eq 0 ]] || die "run as root (it drops to the podium and judge users)"
id judge >/dev/null 2>&1 || die "user judge missing; run: sudo deploy/install.sh packages isolate users"
server_user=podium
id "$server_user" >/dev/null 2>&1 || server_user=root

install -d -m 755 "$state"
install -d -m 700 -o "$server_user" "$state/server"
install -d -m 700 -o judge "$state/judge"
install -m 755 "$bin/podium-server" "$state/podium-server"
install -m 755 "$bin/podium-judge" "$state/podium-judge"

trap 'kill 0 2>/dev/null' EXIT INT TERM
runuser -u "$server_user" -- env -i PATH=/usr/bin:/bin PODIUM_MODE=testing \
  PODIUM_DB_PATH="$state/server/podium.db" PODIUM_TESTS_DIR="$state/server/tests" \
  "$state/podium-server" &
sleep 1
runuser -u judge -- env -i PATH=/usr/bin:/bin PODIUM_MODE=testing \
  PODIUM_JUDGE_CACHE_DIR="$state/judge/cache" PODIUM_JUDGE_LANES=2 \
  "$state/podium-judge" work &
printf '\npodium testing stack: server on 127.0.0.1:8080, judge API on 127.0.0.1:8081, state in %s\n' "$state"
printf 'Open http://localhost:5173 and sign in with any email. Ctrl+C stops everything.\n\n'
wait
