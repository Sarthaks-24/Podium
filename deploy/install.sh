#!/usr/bin/env bash
# Install or update podium on an Ubuntu 24.04 VM. Idempotent: safe to re-run.
#
# Usage:
#   sudo ./install.sh              run every step in order
#   sudo ./install.sh packages isolate
#                                  run only the named steps (used for local
#                                  WSL2 judge setup, where caddy and swap are
#                                  not wanted)
#
# Steps: packages isolate testlib users dirs app caddy swap
#
# The script never writes secrets. It creates /etc/podium/server.env and
# /etc/podium/judge.env from the .env.example files only when they do not
# exist yet, and the owner fills in the values.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ALL_STEPS=(packages isolate testlib users dirs app caddy swap)

NODE_MAJOR=22
JAVA_PKG=openjdk-21-jdk-headless

log() { printf '\n== %s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "run as root (sudo ./install.sh)"
. /etc/os-release
[[ "${ID:-}" == "ubuntu" ]] || die "this script targets Ubuntu 24.04; found ${PRETTY_NAME:-unknown}"

export DEBIAN_FRONTEND=noninteractive
install -d -m 755 /etc/apt/keyrings

# add_repo NAME KEY_URL "DEB_LINE"
add_repo() {
  local name=$1 key_url=$2 line=$3
  if [[ ! -s /etc/apt/keyrings/$name.asc ]]; then
    curl -fsSL "$key_url" -o "/etc/apt/keyrings/$name.asc"
  fi
  echo "$line" > "/etc/apt/sources.list.d/$name.list"
}

step_packages() {
  log "packages: compilers, runtimes, tools"
  apt-get update -q
  apt-get install -y -q ca-certificates curl gnupg sqlite3
  add_repo nodesource https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
    "deb [signed-by=/etc/apt/keyrings/nodesource.asc] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main"
  apt-get update -q
  # g++ with static libstdc++ for -static builds, Java 21, Python 3.12, Node 22.
  apt-get install -y -q build-essential "$JAVA_PKG" python3 nodejs
  g++ --version | head -n1
  java -version 2>&1 | head -n1
  python3 --version
  node --version
}

step_isolate() {
  log "isolate: sandbox from the upstream apt repository"
  # Upstream packages are signed and receive security fixes through apt
  # (decision D21). The suite follows the Ubuntu codename.
  add_repo isolate https://www.ucw.cz/isolate/debian/signing-key.asc \
    "deb [arch=amd64 signed-by=/etc/apt/keyrings/isolate.asc] http://www.ucw.cz/isolate/debian/ ${VERSION_CODENAME}-isolate main"
  apt-get update -q
  apt-get install -y -q isolate
  grep -Eq '^box_root *= */var/lib/isolate$' /etc/isolate \
    || die "/etc/isolate box_root is not /var/lib/isolate; the tmpfs mount below would not cover it"
  # Cap every sandbox together. Boxes live in isolate.slice, not in the
  # podium-judge unit, so the judge unit's MemoryMax does not cover them.
  install -d -m 755 /etc/systemd/system/isolate.slice.d /etc/systemd/system/isolate.service.d
  install -m 644 "$HERE/systemd/isolate.slice.d/podium.conf" /etc/systemd/system/isolate.slice.d/podium.conf
  install -m 644 "$HERE/systemd/isolate.service.d/podium.conf" /etc/systemd/system/isolate.service.d/podium.conf
  # Box root on tmpfs: files written in a box count against its memory limit.
  install -m 644 "$HERE/systemd/var-lib-isolate.mount" /etc/systemd/system/var-lib-isolate.mount
  systemctl daemon-reload
  systemctl stop isolate.service 2>/dev/null || true
  systemctl enable --now var-lib-isolate.mount
  systemctl enable --now isolate.service
  findmnt -no FSTYPE /var/lib/isolate | grep -qx tmpfs || die "/var/lib/isolate is not on tmpfs"
  isolate --version | head -n1
  isolate-check-environment || echo "isolate-check-environment reported warnings; see docs/OPERATIONS.md"
}

# testlib.h for custom checkers, pinned to release 0.9.41 by checksum.
# Checkers are compiled inside isolate, which binds /usr, so
# /usr/local/include is on the compiler's search path there too.
TESTLIB_COMMIT=68f9f300b6abebec82d2a68d8ca04394f2664fb6
TESTLIB_SHA256=70f74c570f2b45d63086ae6e4c41bbb5c5ffd4428cba9914bbc0396d29be10d8
step_testlib() {
  log "testlib: header for custom checkers"
  if [[ -f /usr/local/include/testlib.h ]] && echo "$TESTLIB_SHA256  /usr/local/include/testlib.h" | sha256sum -c --quiet 2>/dev/null; then
    echo "testlib.h already installed"
    return
  fi
  local tmp
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/testlib.h" "https://raw.githubusercontent.com/MikeMirzayanov/testlib/$TESTLIB_COMMIT/testlib.h"
  echo "$TESTLIB_SHA256  $tmp/testlib.h" | sha256sum -c --quiet || die "testlib.h checksum mismatch"
  install -m 644 "$tmp/testlib.h" /usr/local/include/testlib.h
  rm -rf "$tmp"
}

step_users() {
  log "users: podium (server) and judge (worker)"
  id podium >/dev/null 2>&1 || useradd --system --home-dir /var/lib/podium --shell /usr/sbin/nologin podium
  id judge  >/dev/null 2>&1 || useradd --system --home-dir /var/cache/podium-judge --shell /usr/sbin/nologin judge
  # The packaged isolate binary is setuid root and executable only by the
  # isolate group (mode 4754), so the worker must be a member.
  getent group isolate >/dev/null || die "group isolate missing; run the isolate step first"
  usermod -aG isolate judge
}

step_dirs() {
  log "dirs: data, tests, judge cache, config"
  install -d -o podium -g podium -m 750 /var/lib/podium /var/lib/podium/db /var/lib/podium/tests
  install -d -o judge  -g judge  -m 750 /var/cache/podium-judge
  install -d -o root   -g root   -m 755 /etc/podium
  local svc example
  for svc in server judge; do
    [[ -e /etc/podium/$svc.env ]] && continue
    # Release tarball layout first, then a repository checkout.
    example="$HERE/env/$svc.env.example"
    [[ -f "$example" ]] || example="$HERE/../$svc/.env.example"
    [[ -f "$example" ]] || die "cannot find the $svc .env.example next to install.sh"
    install -o root -g root -m 600 "$example" "/etc/podium/$svc.env"
    if [[ $svc == server ]]; then
      # The examples carry local relative paths; the VM uses absolute ones
      # (the server unit runs with a read-only filesystem outside them).
      sed -i -e 's|^PODIUM_DB_PATH=.*|PODIUM_DB_PATH=/var/lib/podium/db/podium.db|' \
             -e 's|^PODIUM_TESTS_DIR=.*|PODIUM_TESTS_DIR=/var/lib/podium/tests|' "/etc/podium/$svc.env"
    fi
    echo "created /etc/podium/$svc.env from the example; fill in the values before starting"
  done
}

step_app() {
  log "app: binaries and systemd units"
  local bin
  for bin in podium-server podium-judge; do
    # -f, not -x: a tarball built on Windows loses the execute bit; install
    # sets the mode anyway.
    [[ -f "$HERE/bin/$bin" ]] || die "missing $HERE/bin/$bin; build a release with make release"
    install -m 755 "$HERE/bin/$bin" "/usr/local/bin/$bin"
  done
  install -m 644 "$HERE/systemd/podium-server.service" /etc/systemd/system/
  install -m 644 "$HERE/systemd/podium-judge.service" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable podium-server.service podium-judge.service
  for env in server judge; do
    if grep -q 'change-me' "/etc/podium/$env.env" 2>/dev/null; then
      echo "not starting the services: /etc/podium/$env.env still has placeholder values"
      return
    fi
  done
  systemctl restart podium-server.service podium-judge.service
}

step_caddy() {
  log "caddy: TLS reverse proxy"
  add_repo caddy https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
    "deb [signed-by=/etc/apt/keyrings/caddy.asc] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
  apt-get update -q
  apt-get install -y -q caddy
  local base host
  base=$(sed -n 's/^PODIUM_BASE_URL=//p' /etc/podium/server.env | tr -d '"' | tail -n1)
  host=${base#https://}; host=${host#http://}; host=${host%%/*}
  [[ -n "$host" && "$host" != *change-me* ]] || die "set PODIUM_BASE_URL in /etc/podium/server.env first"
  sed "s/PODIUM_HOST/$host/g" "$HERE/Caddyfile" > /etc/caddy/Caddyfile
  # Caddy retries certificates for every site it serves; add the www
  # redirect only when that name actually resolves.
  if getent hosts "www.$host" >/dev/null; then
    printf '\nwww.%s {\n\tredir https://%s{uri} permanent\n}\n' "$host" "$host" >> /etc/caddy/Caddyfile
  fi
  caddy validate --config /etc/caddy/Caddyfile
  systemctl reload-or-restart caddy
}

step_swap() {
  log "swap: 1 GB swap file as OOM insurance"
  # Sandboxes cannot use it: isolate sets memory.swap.max=0 on every box with
  # a memory limit. It only protects the server from a transient spike.
  if ! swapon --show=NAME | grep -qx /swapfile; then
    [[ -e /swapfile ]] || { fallocate -l 1G /swapfile; chmod 600 /swapfile; mkswap /swapfile; }
    swapon /swapfile
    grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab
  fi
  echo 'vm.swappiness=10' > /etc/sysctl.d/90-podium.conf
  sysctl -q --system
}

checklist() {
  cat <<'EOF'

First-run checklist
  1. Fill in /etc/podium/server.env and /etc/podium/judge.env (see docs/OPERATIONS.md).
  2. sudo ./install.sh caddy        once PODIUM_BASE_URL is set
  3. sudo systemctl restart podium-server podium-judge
  4. curl -fsS http://127.0.0.1:8080/healthz
EOF
}

steps=("$@")
[[ ${#steps[@]} -gt 0 ]] || steps=("${ALL_STEPS[@]}")
for s in "${steps[@]}"; do
  declare -F "step_$s" >/dev/null || die "unknown step '$s' (known: ${ALL_STEPS[*]})"
done
for s in "${steps[@]}"; do "step_$s"; done
[[ $# -gt 0 ]] || checklist
