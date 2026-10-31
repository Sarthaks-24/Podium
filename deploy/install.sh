#!/usr/bin/env bash
# Install, upgrade, roll back and verify podium on an Ubuntu 24.04 VM
# (amd64, or arm64 such as Oracle's free Ampere A1). Every step is
# idempotent: safe to re-run.
#
# Usage:
#   sudo ./install.sh              run every step in order, then the checklist
#   sudo ./install.sh packages isolate
#                                  run only the named steps (used for local
#                                  WSL2 judge setup, where caddy and swap are
#                                  not wanted)
#   sudo ./install.sh upgrade      install this release over the running one
#                                  (drain, backup, keep the previous
#                                  binaries, install, verify). No deploy
#                                  within 48 hours of a contest: that rule
#                                  is in docs/RUNBOOK.md, not in code.
#   sudo ./install.sh rollback     put the previous release back
#   sudo ./install.sh verify       check the installed system, names only
#   sudo ./install.sh hold         keep apt from upgrading the compilers,
#                                  runtimes and isolate (before a contest
#                                  season); unhold to let them upgrade
#
# Steps: packages isolate testlib users dirs firewall app swap litestream
# backup alerts caddy. caddy comes last and skips itself while
# PODIUM_BASE_URL is a placeholder or its DNS name does not point at this
# VM, so a fresh install always reaches the end.
#
# The script never writes secrets. It creates /etc/podium/*.env from the
# example files only when they do not exist yet, and the owner fills in the
# values. It prints variable names, never values.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ALL_STEPS=(packages isolate testlib users dirs firewall app swap litestream backup alerts caddy)

NODE_MAJOR=22
JAVA_PKG=openjdk-21-jdk-headless
LIB=/usr/local/lib/podium
PREVIOUS=$LIB/previous
MANIFEST=$LIB/MANIFEST
# Compilers, runtimes and the sandbox whose version a validation depends
# on (B1.6): hold keeps apt from changing them, so a gcc update cannot
# change verdicts between a problem's validation and its contest.
HOLD_PACKAGES=(build-essential gcc g++ cpp gcc-13 g++-13 cpp-13 libstdc++-13-dev libgcc-13-dev
               "$JAVA_PKG" openjdk-21-jre-headless python3 python3.12 python3.12-minimal libpython3.12-stdlib
               nodejs isolate)
UNITS=(podium-server.service podium-judge.service podium-litestream.service podium-backup.service
       podium-backup.timer podium-alert@.service podium-check.service podium-check.timer)

log() { printf '\n== %s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "run as root (sudo ./install.sh)"
. /etc/os-release
[[ "${ID:-}" == "ubuntu" ]] || die "this script targets Ubuntu 24.04; found ${PRETTY_NAME:-unknown}"
# release_arch MANIFEST: the processor a release was built for. Releases
# from before arch= was written are amd64; a repository checkout (no
# MANIFEST) prints nothing and is built on the machine itself.
release_arch() {
  [[ -f $1 ]] || return 0
  local a
  a=$(sed -n 's/^arch=//p' "$1" | head -n1)
  echo "${a:-amd64}"
}

# check_arch MANIFEST MACHINE_ARCH: refuse binaries built for another
# processor before anything is installed (V3-28: amd64 and arm64 builds).
check_arch() {
  local want
  want=$(release_arch "$1")
  [[ -z $want || $want == "$2" ]] \
    || die "this release is built for $want but this machine is $2; build it with make release ARCH=$2"
}
check_arch "$HERE/MANIFEST" "$(dpkg --print-architecture)"
[[ $(dpkg --print-architecture) == amd64 || $(dpkg --print-architecture) == arm64 ]] \
  || die "podium releases are built for amd64 and arm64 only; this machine is $(dpkg --print-architecture)"

export DEBIAN_FRONTEND=noninteractive

# add_repo NAME KEY_URL "DEB_LINE"
add_repo() {
  local name=$1 key_url=$2 line=$3
  install -d -m 755 /etc/apt/keyrings
  if [[ ! -s /etc/apt/keyrings/$name.asc ]]; then
    curl -fsSL "$key_url" -o "/etc/apt/keyrings/$name.asc"
  fi
  echo "$line" > "/etc/apt/sources.list.d/$name.list"
}

# envval FILE NAME: a variable's value from an env file (last assignment,
# surrounding double quotes removed), empty when absent.
envval() {
  [[ -r $1 ]] || return 0
  sed -n "s/^$2=//p" "$1" | tail -n1 | sed -e 's/^"\(.*\)"$/\1/'
}

# placeholders FILE: the names whose values are still a placeholder
# (change-me, you@change-me..., your-google-client-id). Names only.
placeholders() {
  [[ -r $1 ]] || return 0
  sed -n -E 's/^([A-Z][A-Z0-9_]*)=("?)(.*change-me.*|your-[a-z-]+)\2$/\1/p' "$1"
}

# db_path and server URLs from server.env, with the server's defaults.
db_path() { local v; v=$(envval /etc/podium/server.env PODIUM_DB_PATH); echo "${v:-/var/lib/podium/db/podium.db}"; }
listen_addr() { local v; v=$(envval /etc/podium/server.env PODIUM_LISTEN); echo "${v:-127.0.0.1:8080}"; }
judge_addr() { local v; v=$(envval /etc/podium/server.env PODIUM_JUDGE_LISTEN); echo "${v:-127.0.0.1:8081}"; }
# The nightly backups' folder, as podium-backup reads it (C2-009).
backup_dir() { local v; v=$(envval /etc/podium/backup.env PODIUM_BACKUP_DIR); echo "${v:-/var/backups/podium}"; }

# migrations DB: the applied migration versions, comma-separated; nothing
# for a database that does not exist yet. A database that exists but
# cannot be read stops the script: an empty answer would make a rollback
# take the same-schema branch over a migrated database.
migrations() {
  [[ -f $1 ]] || return 0
  local out
  out=$(runuser -u podium -- sqlite3 -readonly -cmd '.timeout 5000' "$1" "SELECT group_concat(version, ',') FROM (SELECT version FROM schema_migrations ORDER BY version)") \
    && [[ -n $out ]] || die "cannot read the applied migrations of $1 (above); nothing changed"
  echo "$out"
}

release_name() { basename "$HERE"; }

step_packages() {
  log "packages: compilers, runtimes, tools"
  apt-get update -q
  apt-get install -y -q ca-certificates curl gnupg sqlite3 jq
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

# isolate for arm64, which the upstream apt repository does not publish
# (checked 2026-10-29: "Architectures: amd64"). Built from the upstream
# commit of the release the amd64 package carries, with that package's
# paths, so /usr/bin/isolate, /etc/isolate, /var/lib/isolate and
# isolate.service are where every later step and check expects them.
# Pinned by commit: git checks the fetched tree against the hash.
ISOLATE_VERSION=2.2.1
ISOLATE_COMMIT=9c84554464d2aa9161a424e70af67f4210a12c45
isolate_from_source() {
  if command -v isolate >/dev/null && isolate --version 2>/dev/null | head -n1 | grep -qx "The process isolator $ISOLATE_VERSION"; then
    echo "isolate $ISOLATE_VERSION already built"
    return
  fi
  command -v make >/dev/null && command -v gcc >/dev/null || die "make and gcc missing; run the packages step first"
  apt-get install -y -q git pkg-config libcap-dev libsystemd-dev
  local tmp
  tmp=$(mktemp -d)
  git -C "$tmp" init -q
  git -C "$tmp" fetch -q --depth 1 https://github.com/ioi/isolate.git "$ISOLATE_COMMIT"
  git -C "$tmp" checkout -q FETCH_HEAD
  [[ $(git -C "$tmp" rev-parse HEAD) == "$ISOLATE_COMMIT" ]] || die "isolate source is not commit $ISOLATE_COMMIT"
  # Programs and configuration only: the manual pages need asciidoc.
  local paths=(PREFIX=/usr VARPREFIX=/var CONFIGDIR=/etc UNITDIR=/usr/lib/systemd/system)
  make -C "$tmp" -s "${paths[@]}" isolate isolate-check-environment isolate-cg-keeper default.cf systemd/isolate.service
  make -C "$tmp" -s "${paths[@]}" install
  rm -rf "$tmp"
}

step_isolate() {
  if [[ $(dpkg --print-architecture) == amd64 ]]; then
    log "isolate: sandbox from the upstream apt repository"
    # Upstream packages are signed and receive security fixes through apt
    # (decision D21). The suite follows the Ubuntu codename.
    add_repo isolate https://www.ucw.cz/isolate/debian/signing-key.asc \
      "deb [arch=amd64 signed-by=/etc/apt/keyrings/isolate.asc] http://www.ucw.cz/isolate/debian/ ${VERSION_CODENAME}-isolate main"
    apt-get update -q
    apt-get install -y -q isolate
  else
    log "isolate: sandbox built from upstream $ISOLATE_VERSION (no arm64 package exists)"
    isolate_from_source
  fi
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
  isolate_tuning
  isolate --version | head -n1
  isolate-check-environment || echo "isolate-check-environment reported warnings; see docs/OPERATIONS.md"
}

# Settings isolate-check-environment asks for, made to survive a reboot:
# transparent huge pages off (timing noise) through a tmpfiles rule, a
# plain core pattern and hard-link protection through sysctl.d. Address
# space randomisation stays on (safety beats timing reproducibility).
# Ubuntu's apport rewrites core_pattern at boot, so it is turned off: its
# crash reports would also copy contestants' process memory to disk.
isolate_tuning() {
  cat > /etc/tmpfiles.d/podium-isolate.conf <<'EOF'
# Installed by podium's install.sh (isolate step): no transparent huge
# pages, for steadier sandbox timings.
w /sys/kernel/mm/transparent_hugepage/enabled - - - - never
w /sys/kernel/mm/transparent_hugepage/defrag - - - - never
w /sys/kernel/mm/transparent_hugepage/khugepaged/defrag - - - - 0
EOF
  cat > /etc/sysctl.d/91-podium-isolate.conf <<'EOF'
# Installed by podium's install.sh (isolate step), for isolate.
kernel.core_pattern = core
fs.protected_hardlinks = 1
EOF
  if systemctl list-unit-files apport.service >/dev/null 2>&1 && systemctl is-enabled --quiet apport.service 2>/dev/null; then
    systemctl disable --now apport.service
    echo "apport disabled: it rewrites kernel.core_pattern at boot"
  fi
  systemd-tmpfiles --create /etc/tmpfiles.d/podium-isolate.conf
  sysctl -q -p /etc/sysctl.d/91-podium-isolate.conf
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
  log "users: podium (server), judge (worker), podium-backup (Litestream and the heartbeat)"
  id podium >/dev/null 2>&1 || useradd --system --home-dir /var/lib/podium --shell /usr/sbin/nologin podium
  id judge  >/dev/null 2>&1 || useradd --system --home-dir /var/cache/podium-judge --shell /usr/sbin/nologin judge
  # The bucket keys live in processes of this user only, so a compromised
  # server (user podium) cannot read them from /proc (D116). It is in group
  # podium to write the database directory, which Litestream needs for
  # its checkpoints.
  id podium-backup >/dev/null 2>&1 || useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin podium-backup
  usermod -aG podium podium-backup
  # The packaged isolate binary is setuid root and executable only by the
  # isolate group (mode 4754), so the worker must be a member.
  getent group isolate >/dev/null || die "group isolate missing; run the isolate step first"
  usermod -aG isolate judge
}

# example_for NAME: the example env file of server or judge, from the
# release tarball's env/ or a repository checkout.
example_for() {
  local example="$HERE/env/$1.env.example"
  [[ -f "$example" ]] || example="$HERE/../$1/.env.example"
  [[ -f "$example" ]] || die "cannot find the $1 .env.example next to install.sh"
  echo "$example"
}

# as_podium_install ARGS: install(1) as the podium user, for folders and
# files inside /var/lib/podium. podium owns that folder, so a taken-over
# server could put a symlink where a folder is expected, and root's
# install -d follows it and chowns the target (say /usr/local/lib/podium).
# As podium the same call can only change what podium already owns.
as_podium_install() { setpriv --reuid=podium --regid=podium --init-groups -- install "$@"; }

# shared_judge_token DIR: the PODIUM_JUDGE_TOKEN new env files in DIR get
# (v4 NB). A real token already in server.env or judge.env is kept, so a
# re-run never rotates one the judge already holds; otherwise 32 random
# bytes as hex. The caller keeps it in a variable and never prints it.
shared_judge_token() {
  local f t
  for f in "$1/server.env" "$1/judge.env"; do
    t=$(envval "$f" PODIUM_JUDGE_TOKEN)
    if [[ -n $t && $t != change-me ]]; then
      printf '%s' "$t"
      return
    fi
  done
  head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n'
}

# put_judge_token FILE: sets PODIUM_JUDGE_TOKEN in FILE to $JUDGE_TOKEN.
# The value comes through the environment, never a command line other
# users could read in ps, and the temporary copy is mode 600 from the
# start; FILE keeps its own mode and owner.
put_judge_token() {
  local tmp="$1.tmp"
  install -m 600 /dev/null "$tmp"
  awk '/^PODIUM_JUDGE_TOKEN=/ { print "PODIUM_JUDGE_TOKEN=" ENVIRON["JUDGE_TOKEN"]; next } { print }' "$1" > "$tmp"
  cat "$tmp" > "$1"
  rm -f "$tmp"
}

step_dirs() {
  log "dirs: data, tests, judge cache, config"
  install -d -o podium -g podium -m 750 /var/lib/podium
  [[ -L /var/lib/podium/tests || -L /var/lib/podium/db ]] && die "/var/lib/podium/tests or /var/lib/podium/db is a symlink; move it aside and run the dirs step again"
  as_podium_install -d -m 750 /var/lib/podium/tests \
    || die "cannot make /var/lib/podium/tests as podium (it must be a folder podium owns)"
  # Group podium (podium-backup's Litestream) may write the database
  # directory; setgid keeps new files in the group.
  as_podium_install -d -m 2770 /var/lib/podium/db \
    || die "cannot make /var/lib/podium/db as podium (it must be a folder podium owns)"
  install -d -o judge  -g judge  -m 750 /var/cache/podium-judge
  install -d -o root   -g root   -m 755 /etc/podium
  local svc
  # v4 NB: one judge token for both new files, kept from an existing file
  # on a re-run; it is never printed, only passed through the environment.
  local JUDGE_TOKEN
  JUDGE_TOKEN=$(shared_judge_token /etc/podium)
  for svc in server judge; do
    [[ -e /etc/podium/$svc.env ]] && continue
    install -o root -g root -m 600 "$(example_for "$svc")" "/etc/podium/$svc.env"
    JUDGE_TOKEN=$JUDGE_TOKEN put_judge_token "/etc/podium/$svc.env"
    if [[ $svc == server ]]; then
      # The examples carry local relative paths; the VM uses absolute ones
      # (the server unit runs with a read-only filesystem outside them).
      sed -i -e 's|^PODIUM_DB_PATH=.*|PODIUM_DB_PATH=/var/lib/podium/db/podium.db|' \
             -e 's|^PODIUM_TESTS_DIR=.*|PODIUM_TESTS_DIR=/var/lib/podium/tests|' "/etc/podium/$svc.env"
    fi
    echo "created /etc/podium/$svc.env from the example; fill in the values before starting"
  done
}

# install_app_files: binaries, systemd units and helper scripts of this
# release, without starting anything. Used by app and upgrade.
install_app_files() {
  local bin
  for bin in podium-server podium-judge; do
    # -f, not -x: a tarball built on Windows loses the execute bit; install
    # sets the mode anyway.
    [[ -f "$HERE/bin/$bin" ]] || die "missing $HERE/bin/$bin; build a release with make release"
    install -m 755 "$HERE/bin/$bin" "/usr/local/bin/$bin"
  done
  install -m 644 "$HERE/systemd/podium-server.service" "$HERE/systemd/podium-judge.service" \
    "$HERE/systemd/podium-alert@.service" /etc/systemd/system/
  install -d -m 755 "$LIB"
  install -m 755 "$HERE/backup/podium-alert" "$LIB/podium-alert"
  # A copy of this script, so verify and rollback work without the release
  # folder: sudo /usr/local/lib/podium/install.sh verify
  install -m 755 "$HERE/install.sh" "$LIB/install.sh"
  systemctl daemon-reload
}

# write_manifest: what is installed now, for upgrade and rollback: the
# release's own MANIFEST from make release (version, commit, schema,
# migrations it carries, protocol, oldest release it upgrades from), plus
# when it was installed, the installed binaries' hashes and the
# migrations the database has.
write_manifest() {
  local applied
  applied=$(migrations "$(db_path)")
  {
    if [[ -f $HERE/MANIFEST ]]; then
      cat "$HERE/MANIFEST"
    else
      echo "release=$(release_name)"
      echo "note=installed from a folder without a MANIFEST (a repository checkout)"
    fi
    echo "installed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "podium_server_sha256=$(sha256sum /usr/local/bin/podium-server | cut -d' ' -f1)"
    echo "podium_judge_sha256=$(sha256sum /usr/local/bin/podium-judge | cut -d' ' -f1)"
    echo "migrations_applied=$applied"
  } > "$MANIFEST"
}

# manifest_get FILE KEY: one value of a MANIFEST.
manifest_get() { [[ -f $1 ]] && sed -n "s/^$2=//p" "$1" | head -n1; true; }

# installed_version: the installed release's version, from its MANIFEST:
# version= since v1.1.0, else the release folder's name (podium-v1.0.0).
installed_version() {
  local v
  v=$(manifest_get "$MANIFEST" version)
  [[ -n $v ]] || { v=$(manifest_get "$MANIFEST" release); v=${v#podium-}; }
  echo "$v"
}

# version_ge A B: A is B or later (v1.2.10 after v1.2.9).
version_ge() { [[ $(printf '%s\n%s\n' "${2#v}" "${1#v}" | sort -V | head -n1) == "${2#v}" ]]; }
is_release_version() { [[ $1 =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; }

# verify_release: every file of the unpacked release matches SHA256SUMS,
# so a damaged or half-copied tarball installs nothing. It proves
# integrity, not origin: SHA256SUMS travels in the same tarball. Check the
# tarball's .sha256 from the GitHub release before unpacking for that.
verify_release() {
  if [[ ! -f $HERE/SHA256SUMS ]]; then
    [[ -f $HERE/MANIFEST ]] && die "$HERE has a MANIFEST but no SHA256SUMS; unpack the release tarball again"
    echo "release: no SHA256SUMS (a repository checkout); files not checked"
    return
  fi
  (cd "$HERE" && sha256sum --quiet -c SHA256SUMS) \
    || die "files in $HERE do not match its SHA256SUMS (above); download and unpack the release again"
  echo "release: every file matches SHA256SUMS"
}

# check_upgrade_path: the new release's MANIFEST says the oldest release it
# upgrades from; an older installed release is refused before anything
# changes, and so is a downgrade (that is what rollback is for).
check_upgrade_path() {
  local from to have
  [[ -f $HERE/MANIFEST ]] || die "$HERE has no MANIFEST; upgrade only from a release built with make release"
  from=$(manifest_get "$HERE/MANIFEST" upgrades_from)
  to=$(manifest_get "$HERE/MANIFEST" version)
  have=$(installed_version)
  [[ -n $from && -n $to ]] || die "$HERE/MANIFEST has no version or upgrades_from line; build the release again with make release"
  if ! is_release_version "$have"; then
    die "cannot tell which release is installed (MANIFEST says '${have:-nothing}'); $to upgrades from $from or later. Install $from first, or ask before forcing anything"
  fi
  is_release_version "$from" && is_release_version "$to" \
    || die "$HERE/MANIFEST names version '$to' and upgrades_from '$from'; both must be vX.Y.Z (build the release again with make release VERSION=vX.Y.Z)"
  if ! version_ge "$have" "$from"; then
    die "this VM runs $have, and $to upgrades only from $from or later. Upgrade to $from first, then to $to"
  fi
  if ! version_ge "$to" "$have"; then
    die "$to is older than the installed $have; to go back use: sudo $LIB/install.sh rollback"
  fi
  echo "upgrade path: $have to $to (this release upgrades from $from or later)"
}

# check_unfinished_upgrade RELEASE SERVER_BIN: refuse an upgrade that
# would overwrite the rollback target. After an upgrade stopped at step 7
# the new binaries are installed but MANIFEST still names the old
# release, so a rerun would keep the new binaries as "previous" and record
# the migrated schema as the one before the upgrade (council C1-005). A
# second run of a finished upgrade to the same release would do the same.
check_unfinished_upgrade() {
  local release=$1 bin=$2 want have prev_to
  prev_to=$(manifest_get "$PREVIOUS/MANIFEST" upgraded_to)
  want=$(manifest_get "$MANIFEST" podium_server_sha256)
  if [[ -n $want && -f $bin ]]; then
    have=$(sha256sum "$bin" | cut -d' ' -f1)
    [[ $have == "$want" ]] || die "an upgrade${prev_to:+ to $prev_to} did not finish: $bin is not the binary $MANIFEST names. Nothing changed; run: sudo $LIB/install.sh rollback"
  fi
  if [[ -n $prev_to && $prev_to == "$release" ]]; then
    die "$release is already installed by an upgrade; running it again would replace the release kept for rollback in $PREVIOUS. Nothing changed; to go back run: sudo $LIB/install.sh rollback"
  fi
  return 0
}

# db_group_rw: the database files writable by group podium, so
# Litestream (user podium-backup) can checkpoint. SQLite gives the -wal
# and -shm files the database file's mode, so this holds once set.
# The folder and files belong to podium, which a taken-over server could
# swap for symlinks; so this runs as podium, never as root, and skips
# symlinks (P1 security review, finding 1).
db_group_rw() {
  local db dir
  db=$(db_path); dir=$(dirname "$db")
  [[ -d $dir && ! -L $dir ]] || return 0
  setpriv --reuid=podium --regid=podium --init-groups -- sh -c '
    chmod 2770 "$1"
    for f in "$2" "$2-wal" "$2-shm"; do
      if [ -f "$f" ] && [ ! -L "$f" ]; then chmod 660 "$f"; fi
    done' db_group_rw "$dir" "$db" || echo "warning: could not make $dir group-writable for Litestream"
}

step_app() {
  log "app: binaries and systemd units"
  verify_release
  install_app_files
  systemctl enable podium-server.service podium-judge.service
  local env bad
  for env in server judge; do
    bad=$(placeholders "/etc/podium/$env.env" | tr '\n' ' ')
    if [[ -n $bad ]]; then
      echo "not starting the services: /etc/podium/$env.env still has placeholder values for: $bad"
      return
    fi
  done
  systemctl restart podium-server.service podium-judge.service
  wait_healthz || die "podium-server did not answer /healthz; journalctl -u podium-server -n 50"
  db_group_rw
  write_manifest
}

# wait_healthz: up to 60 s for the server's /healthz to answer ok.
wait_healthz() {
  local _
  for _ in $(seq 60); do
    curl -fsS -m 2 "http://$(listen_addr)/healthz" 2>/dev/null | grep -q '"status":"ok"' && return 0
    sleep 1
  done
  return 1
}

# dns_points_here HOST: true when HOST resolves to an address of this VM
# (a local interface, or the public address behind the provider's NAT).
# Prints what it found. Let's Encrypt allows only a few failed attempts an
# hour, so a wrong record must stop the step before Caddy asks.
dns_points_here() {
  local host=$1 resolved local_ips public
  resolved=$(getent ahosts "$host" | awk '{print $1}' | sort -u | tr '\n' ' ')
  if [[ -z $resolved ]]; then
    echo "DNS: $host does not resolve yet. Create an A record for it pointing at this VM's public address, wait for it to resolve, then: sudo ./install.sh caddy"
    return 1
  fi
  local_ips=$(ip -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | tr '\n' ' ')
  # A provider with NAT gives the VM only a private address; ask once
  # what the outside sees (this sends nothing but the request itself).
  public=$(curl -fsS -4 -m 5 https://checkip.amazonaws.com 2>/dev/null | tr -d '[:space:]' || true)
  local ip
  for ip in $resolved; do
    if [[ " $local_ips $public " == *" $ip "* ]]; then
      echo "DNS: $host resolves to $ip, an address of this VM"
      return 0
    fi
  done
  if [[ -z $public ]]; then
    echo "DNS: $host resolves to $resolved; this VM's public address could not be determined, so the certificate request goes ahead"
    return 0
  fi
  echo "DNS: $host resolves to $resolved, but this VM is ${public} (interfaces: $local_ips). Fix the A record, wait for it to update, then: sudo ./install.sh caddy"
  return 1
}

step_caddy() {
  log "caddy: TLS reverse proxy"
  local base host
  base=$(envval /etc/podium/server.env PODIUM_BASE_URL)
  host=${base#https://}; host=${host#http://}; host=${host%%/*}
  if [[ -z "$host" || "$host" == *change-me* ]]; then
    echo "caddy skipped: PODIUM_BASE_URL in /etc/podium/server.env is still a placeholder. Set it, then: sudo ./install.sh caddy"
    return 0
  fi
  if ! dns_points_here "$host"; then
    echo "caddy skipped: no certificate is requested until DNS points here"
    return 0
  fi
  add_repo caddy https://dl.cloudsmith.io/public/caddy/stable/gpg.key \
    "deb [signed-by=/etc/apt/keyrings/caddy.asc] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main"
  apt-get update -q
  apt-get install -y -q caddy
  sed "s/PODIUM_HOST/$host/g" "$HERE/Caddyfile" > /etc/caddy/Caddyfile
  # Caddy retries certificates for every site it serves; add the www
  # redirect only when that name actually resolves.
  if getent hosts "www.$host" >/dev/null; then
    printf '\nwww.%s {\n\tredir https://%s{uri} permanent\n}\n' "$host" "$host" >> /etc/caddy/Caddyfile
  fi
  caddy validate --config /etc/caddy/Caddyfile
  # The admin API is off (see the Caddyfile), so a reload cannot work.
  systemctl restart caddy
}

# Inbound traffic: ssh, http (certificate checks and the https redirect)
# and https (TCP, and UDP for HTTP/3). Everything else is dropped,
# including the server's own ports, which listen on loopback anyway. ssh is
# allowed before the firewall is switched on, so an ssh session survives.
ssh_port() { local p; p=$(sshd -T 2>/dev/null | sed -n 's/^port //p' | head -n1); echo "${p:-22}"; }
# Oracle Cloud's Ubuntu images load their own rules at boot
# (netfilter-persistent, /etc/iptables/rules.v4): established traffic,
# ssh, then a bare REJECT in INPUT and FORWARD. ufw's chains are appended
# after that REJECT and never see a packet, so http and https stay closed
# however ufw is set. Installing ufw removes netfilter-persistent (ufw
# declares Breaks on it), so from the next boot only ufw's rules load, as
# on any VM. image_rejects prints the bare REJECT rules still loaded now
# (from iptables -S), which image_rejects_drop deletes; ufw's own rules
# all jump to ufw chains and are never matched.
image_rejects() {
  grep -E '^-A (INPUT|FORWARD) -j REJECT( |$)' <<<"$1" || true
}

image_rejects_drop() {
  local tool rule
  for tool in iptables ip6tables; do
    command -v "$tool" >/dev/null || continue
    while IFS= read -r rule; do
      [[ -n $rule ]] || continue
      # shellcheck disable=SC2086 # the rule is iptables words, split on purpose
      "$tool" -D ${rule#-A }
      echo "$tool: removed the image's '$rule' (ufw now filters inbound)"
    done < <(image_rejects "$("$tool" -S 2>/dev/null)")
  done
}

# input_order_ok RULES: true when the INPUT rules (iptables -S INPUT) have
# no REJECT, or accept 80/tcp, 443/tcp and 443/udp before the first one.
input_order_ok() {
  local reject at pat
  reject=$(grep -n -m1 -e '-j REJECT' <<<"$1" | cut -d: -f1)
  [[ -z $reject ]] && return 0
  for pat in '-p tcp .*--dport 80 .*-j ACCEPT' '-p tcp .*--dport 443 .*-j ACCEPT' '-p udp .*--dport 443 .*-j ACCEPT'; do
    at=$(grep -n -m1 -E -e "$pat" <<<"$1" | cut -d: -f1)
    [[ -n $at && $at -lt $reject ]] || return 1
  done
}

step_firewall() {
  log "firewall: only ssh, http and https inbound"
  if dpkg-query -W -f='${Status}' netfilter-persistent 2>/dev/null | grep -q 'ok installed'; then
    echo "netfilter-persistent is installed (a cloud image's own rules, such as Oracle's); ufw replaces it"
  fi
  apt-get install -y -q ufw
  ufw default deny incoming
  ufw default allow outgoing
  ufw allow "$(ssh_port)/tcp" comment 'ssh'
  ufw allow 80/tcp comment 'http'
  ufw allow 443/tcp comment 'https'
  ufw allow 443/udp comment 'https (HTTP/3)'
  ufw --force enable
  image_rejects_drop
  ufw status verbose
  if sshd -T 2>/dev/null | grep -qx 'passwordauthentication yes'; then
    echo "warning: ssh still accepts passwords. Once your key works, set PasswordAuthentication no in /etc/ssh/sshd_config.d/ and restart ssh (docs/OPERATIONS.md, Firewall and ssh)."
  fi
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

# Litestream streams the database to an S3-compatible bucket. Pinned to a
# release and refused unless the tarball's SHA-256 matches the one here
# (taken from the release's checksums.txt and checked against it).
LITESTREAM_VERSION=0.5.16
# Read through ${!sum} below, by the machine's architecture.
# shellcheck disable=SC2034
LITESTREAM_SHA256_amd64=9e29112380a942e4a62ee07773684396cb8b308dc4d67e130bef41f75e937f0a
# shellcheck disable=SC2034
LITESTREAM_SHA256_arm64=678022e4103145302598e35d37f8718392d42e153feeb1e2d4a64dd0cd3aaf10
step_litestream() {
  log "litestream: database replication to a bucket"
  if [[ -x /usr/local/bin/litestream ]] && [[ $(/usr/local/bin/litestream version 2>/dev/null) == "$LITESTREAM_VERSION" ]]; then
    echo "litestream $LITESTREAM_VERSION already installed"
  else
    local tmp arch sum tarball
    arch=$(dpkg --print-architecture)
    sum=LITESTREAM_SHA256_$arch
    tarball=litestream-$LITESTREAM_VERSION-linux-$([[ $arch == amd64 ]] && echo x86_64 || echo "$arch").tar.gz
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/$tarball" "https://github.com/benbjohnson/litestream/releases/download/v$LITESTREAM_VERSION/$tarball"
    echo "${!sum}  $tmp/$tarball" | sha256sum -c --quiet || die "litestream tarball checksum mismatch"
    tar -xzf "$tmp/$tarball" -C "$tmp" litestream
    install -m 755 "$tmp/litestream" /usr/local/bin/litestream
    rm -rf "$tmp"
  fi
  install -m 644 "$HERE/litestream.yml" /etc/litestream.yml
  install -d -m 755 "$LIB"
  install -m 755 "$HERE/backup/check-litestream-env" "$LIB/check-litestream-env"
  install -d -o root -g root -m 755 /etc/podium
  if [[ ! -e /etc/podium/litestream.env ]]; then
    install -o root -g root -m 600 "$HERE/litestream.env.example" /etc/podium/litestream.env
    echo "created /etc/podium/litestream.env from the example; fill in the bucket and key"
  fi
  install -m 644 "$HERE/systemd/podium-litestream.service" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable podium-litestream.service
  id podium-backup >/dev/null 2>&1 || die "user podium-backup missing; run the users step first"
  db_group_rw
  # Litestream ran as podium before v1.1.0: hand its state to podium-backup.
  local lsdir
  lsdir="$(dirname "$(db_path)")/.$(basename "$(db_path)")-litestream"
  [[ -d $lsdir ]] && chown -R podium-backup:podium "$lsdir"
  if [[ -n $(placeholders /etc/podium/litestream.env) ]]; then
    echo "not starting replication: /etc/podium/litestream.env still has placeholder values for: $(placeholders /etc/podium/litestream.env | tr '\n' ' ')"
    return
  fi
  systemctl restart podium-litestream.service
}

step_backup() {
  log "backup: nightly copy of the database and test files on the VM"
  command -v sqlite3 >/dev/null || die "sqlite3 missing; run the packages step first"
  id podium >/dev/null 2>&1 || die "user podium missing; run the users step first"
  # rclone lets podium-check list the Litestream bucket to see how far
  # behind replication is. From Ubuntu's signed archive (universe).
  command -v rclone >/dev/null || apt-get install -y -q rclone
  install -d -m 755 "$LIB"
  install -m 755 "$HERE/backup/podium-backup" "$LIB/podium-backup"
  install -m 755 "$HERE/backup/podium-restore" "$LIB/podium-restore"
  id podium-backup >/dev/null 2>&1 || die "user podium-backup missing; run the users step first"
  install -d -o root -g root -m 755 /etc/podium
  if [[ ! -e /etc/podium/backup.env ]]; then
    install -o root -g root -m 600 "$HERE/backup.env.example" /etc/podium/backup.env
    echo "created /etc/podium/backup.env from the example; set the heartbeat URL"
  fi
  local bdir
  bdir=$(backup_dir)
  install -d -o podium -g podium -m 700 "$bdir"
  chmod 700 "$bdir"
  install -m 644 "$HERE/systemd/podium-backup.service" "$HERE/systemd/podium-backup.timer" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now podium-backup.timer
  systemctl list-timers podium-backup.timer --no-pager | head -n 2
}

step_alerts() {
  log "alerts: a failed unit, disk, WAL, and replication post to the webhook"
  install -d -m 755 "$LIB"
  install -m 755 "$HERE/backup/podium-alert" "$LIB/podium-alert"
  install -m 755 "$HERE/backup/podium-check" "$LIB/podium-check"
  install -d -o root -g root -m 755 /etc/podium
  if [[ ! -e /etc/podium/alert.env ]]; then
    install -o root -g root -m 600 "$HERE/alert.env.example" /etc/podium/alert.env
    echo "created /etc/podium/alert.env from the example; set PODIUM_ALERT_WEBHOOK, then: sudo $LIB/podium-alert test"
  fi
  install -m 644 "$HERE/systemd/podium-alert@.service" "$HERE/systemd/podium-check.service" \
    "$HERE/systemd/podium-check.timer" /etc/systemd/system/
  systemctl daemon-reload
  systemctl enable --now podium-check.timer
  systemctl list-timers podium-check.timer --no-pager | head -n 2
}

# --- upgrade, rollback, verify ---------------------------------------------

# env_names_check: every variable the new release's examples list is named
# in the VM's env files. A required one missing stops the upgrade; an
# optional one is listed and its default applies. Names only, never values.
# A variable counts as optional when the comment above it has a line
# starting "Optional".
env_names_check() {
  local pair example file missing_req="" name kind
  for pair in "$(example_for server):/etc/podium/server.env" "$(example_for judge):/etc/podium/judge.env" \
              "$HERE/litestream.env.example:/etc/podium/litestream.env" "$HERE/backup.env.example:/etc/podium/backup.env" \
              "$HERE/alert.env.example:/etc/podium/alert.env"; do
    example=${pair%%:*} file=${pair#*:}
    [[ -f $example ]] || continue
    if [[ ! -f $file ]]; then
      case $file in
        */server.env|*/judge.env) die "$file is missing; run: sudo ./install.sh dirs, then fill it in" ;;
        *) echo "env: $file does not exist yet; its step creates it from the example"; continue ;;
      esac
    fi
    while read -r kind name; do
      grep -q "^$name=" "$file" && continue
      if [[ $kind == R ]]; then
        missing_req+=" $name ($file)"
      else
        echo "env: new optional variable $name is not in $file; its default applies (docs/OPERATIONS.md)"
      fi
    done < <(awk '
      /^#/ { if ($0 ~ /^# *Optional/) opt = 1; next }
      /^[A-Z][A-Z0-9_]*=/ { split($0, a, "="); print (opt ? "O" : "R"), a[1]; opt = 0; next }
      { opt = 0 }' "$example")
  done
  [[ -z $missing_req ]] || die "this release needs variables the VM does not have:$missing_req. Add each with sudoedit (docs/OPERATIONS.md, Configuration), then run the upgrade again"
}

cmd_upgrade() {
  [[ $# -eq 0 ]] || die "upgrade: unknown argument '$1' (usage: sudo ./install.sh upgrade)"
  [[ -x /usr/local/bin/podium-server ]] || die "no podium installed here; run sudo ./install.sh for a fresh install"
  [[ -f $HERE/bin/podium-server && -f $HERE/bin/podium-judge ]] || die "missing $HERE/bin; unpack a release built with make release"
  local db release
  db=$(db_path)
  release=$(release_name)
  [[ -f $db ]] || die "no database at $db; nothing to upgrade (start the server once, or run a fresh install)"

  log "upgrade to $release: 1/7 release files, upgrade path, configuration names"
  verify_release
  check_upgrade_path
  check_unfinished_upgrade "$release" /usr/local/bin/podium-server
  env_names_check

  log "2/7 judge: stop claiming, finish and report the running jobs (up to 20 minutes)"
  local t0=$SECONDS
  systemctl stop podium-judge.service
  echo "judge stopped after $(( SECONDS - t0 )) s"

  log "3/7 backup before the upgrade"
  local before after bdir
  local backup_ms=$(( $(date +%s) * 1000 ))
  bdir=$(backup_dir)
  before=$(readlink "$bdir/latest" 2>/dev/null || true)
  systemctl start podium-backup.service || echo "podium-backup reported a failure; checking whether the local backup was made"
  after=$(readlink "$bdir/latest" 2>/dev/null || true)
  if [[ -z $after || $after == "$before" ]]; then
    systemctl start podium-judge.service
    die "no new local backup was made in $bdir (journalctl -u podium-backup -n 30); the judge is running again and nothing else changed"
  fi
  local backup=$bdir/$after
  echo "backup: $backup"

  log "4/7 keep the running release in $PREVIOUS"
  rm -rf "$PREVIOUS.new"
  install -d -m 755 "$PREVIOUS.new/bin" "$PREVIOUS.new/systemd"
  cp -a /usr/local/bin/podium-server /usr/local/bin/podium-judge "$PREVIOUS.new/bin/"
  local u
  for u in "${UNITS[@]}"; do
    [[ -f /etc/systemd/system/$u ]] && cp -a "/etc/systemd/system/$u" "$PREVIOUS.new/systemd/"
  done
  if [[ -f $MANIFEST ]]; then cp "$MANIFEST" "$PREVIOUS.new/MANIFEST"; else
    echo "release=unknown (installed before MANIFEST existed)" > "$PREVIOUS.new/MANIFEST"
  fi
  local applied
  applied=$(migrations "$db") || { systemctl start podium-judge.service; die "the judge is running again and nothing else changed"; }
  {
    echo "migrations_before_upgrade=$applied"
    echo "backup_before_upgrade=$backup"
    echo "backup_started_ms=$backup_ms"
    echo "upgraded_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    echo "upgraded_at_ms=$(( $(date +%s) * 1000 ))"
    echo "upgraded_to=$release"
  } >> "$PREVIOUS.new/MANIFEST"
  rm -rf "$PREVIOUS"
  mv "$PREVIOUS.new" "$PREVIOUS"

  log "5/7 install $release"
  install_app_files
  step_users
  step_litestream
  step_backup
  step_alerts

  log "6/7 start: server (migrations run now), then the judge"
  systemctl restart podium-server.service
  if ! wait_healthz; then
    die "podium-server did not answer /healthz within 60 s (journalctl -u podium-server -n 50). Roll back with: sudo $LIB/install.sh rollback"
  fi
  systemctl start podium-judge.service
  db_group_rw
  write_manifest

  log "7/7 verify"
  local vcode=0
  cmd_verify || vcode=$?
  cat <<EOF

Upgraded to $release.
  Backup taken before the upgrade: $backup
  Previous release kept in:        $PREVIOUS
  To go back:                      sudo $LIB/install.sh rollback
EOF
  (( vcode == 0 )) || die "verify found problems (above); fix them or roll back"
}

cmd_rollback() {
  [[ -f $PREVIOUS/MANIFEST && -f $PREVIOUS/bin/podium-server && -f $PREVIOUS/bin/podium-judge ]] \
    || die "no previous release in $PREVIOUS (rollback works once, after an upgrade)"
  local db before now_list backup upgraded_ms prev_release
  db=$(db_path)
  # A rollback that stopped after moving the database away must not start
  # the previous server on an empty one, which would answer /healthz.
  if [[ ! -f $db ]]; then
    die "$db is missing$(ls -d "$(dirname "$db")".before-rollback-* 2>/dev/null | sed 's/^/; a moved copy: /' | tr -d '
'); put the right database back (docs/RUNBOOK.md, Restoring from a backup), then run rollback again"
  fi
  before=$(sed -n 's/^migrations_before_upgrade=//p' "$PREVIOUS/MANIFEST")
  backup=$(sed -n 's/^backup_before_upgrade=//p' "$PREVIOUS/MANIFEST")
  upgraded_ms=$(sed -n "s/^backup_started_ms=//p" "$PREVIOUS/MANIFEST")
  prev_release=$(sed -n 's/^release=//p' "$PREVIOUS/MANIFEST" | head -n1)
  now_list=$(migrations "$db")
  local newer="" v
  for v in ${now_list//,/ }; do
    [[ ",$before," == *",$v,"* ]] || newer+=" $v"
  done

  if [[ -z $newer ]]; then
    log "rollback to $prev_release: same schema, so only the binaries change"
    systemctl stop podium-judge.service
    systemctl stop podium-server.service
    restore_previous_files
    systemctl start podium-server.service
    wait_healthz || die "the previous podium-server did not answer /healthz (journalctl -u podium-server -n 50)"
    systemctl start podium-judge.service
  else
    log "rollback to $prev_release: the upgrade applied migrations$newer, which the previous server refuses"
    [[ -n $backup && -f $backup/podium.db ]] || die "the pre-upgrade backup $backup is gone; restore by hand (docs/RUNBOOK.md, Restoring from a backup)"
    local subs="unknown" forgotten=""
    if [[ -n $upgraded_ms ]]; then
      subs=$(runuser -u podium -- sqlite3 -readonly "$db" "SELECT count(*) FROM submissions WHERE created_at >= $upgraded_ms" 2>/dev/null || echo unknown)
      # Erasures since the backup come back with it (council C1-047).
      forgotten=$(runuser -u podium -- sqlite3 -readonly "$db" "SELECT coalesce(json_extract(details, '\$.user_id'), 0) FROM site_audit
        WHERE action = 'user_forgotten' AND at >= $upgraded_ms" 2>/dev/null || true)
    fi
    cat <<EOF

The database goes back to the backup taken just before the upgrade:
  $backup
Everything written since that backup started is lost, the upgrade included:
  $subs submissions and runs, and every sign-in, draft, clarification,
  announcement and setting change made since then.
The current database is kept as $(dirname "$db").before-rollback-<time>.
EOF
    if [[ -n $forgotten ]]; then
      cat <<EOF
$(wc -l <<<"$forgotten") account erasures (podium-server forget-user) were made since that backup.
The restore brings those people's data back. After the rollback, this
script writes the forget-user commands to run again to a file only root
can read, and prints its name.
EOF
    fi
    local answer
    read -r -p "Type ROLLBACK to restore that backup and the previous release: " answer
    [[ $answer == ROLLBACK ]] || die "nothing changed"
    local scratch work stamp
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    # A private folder of the podium user: the restore and its check read
    # podium's data, so they run as podium, not as root.
    work=$(mktemp -d /var/tmp/podium-rollback.XXXXXX)
    chown podium:podium "$work"
    scratch=$work/restore
    # Checked by the previous binary: it must accept the restored schema.
    runuser -u podium -- env PODIUM_SERVER_BIN="$PREVIOUS/bin/podium-server" "$LIB/podium-restore" nightly "$backup" "$scratch" \
      || { rm -rf "$work"; die "the backup in $backup failed its check (above); nothing changed"; }
    systemctl stop podium-judge.service
    systemctl stop podium-litestream.service podium-server.service
    mv "$(dirname "$db")" "$(dirname "$db").before-rollback-$stamp"
    # The modes the dirs step gives a fresh install: group podium may
    # write, so Litestream (podium-backup, D116) can make its state folder
    # and checkpoint in the restored folder.
    as_podium_install -d -m 2770 "$(dirname "$db")"
    as_podium_install -m 660 "$scratch/podium.db" "$db"
    db_group_rw
    # Adds files the backup has and the live folder lost; keeps the rest.
    local tests
    tests=$(envval /etc/podium/server.env PODIUM_TESTS_DIR); tests=${tests:-/var/lib/podium/tests}
    runuser -u podium -- cp -an "$scratch/tests/." "$tests/"
    restore_previous_files
    systemctl start podium-server.service
    wait_healthz || die "the previous podium-server did not answer /healthz (journalctl -u podium-server -n 50)"
    systemctl start podium-judge.service
    systemctl start podium-litestream.service || true
    rm -rf "$work"
    echo "restored $backup; Litestream starts a new history from it"
    if [[ -n $forgotten ]]; then
      # mktemp: a random name made with O_EXCL, root's and mode 600, so the
      # podium user cannot plant the file or a link to it in /var/tmp first.
      local redo ids owner
      redo=$(mktemp "/var/tmp/podium-rollback-forget-$stamp.XXXXXX.txt")
      ids=$(grep -E '^[1-9][0-9]*$' <<<"$forgotten" | paste -sd, - || true)
      owner=$(envval /etc/podium/server.env PODIUM_OWNER_EMAIL)
      (
        umask 077
        {
          echo "# Erasures undone by the rollback of $stamp. Run each line again (it is a dry run without -apply)."
          if [[ -n $ids ]]; then
            runuser -u podium -- sqlite3 -readonly "$db" "SELECT email FROM users WHERE id IN ($ids)" 2>/dev/null \
              | while read -r e; do printf 'sudo -u podium podium-server forget-user -db %q -owner %q -apply %q\n' "$db" "$owner" "$e"; done
          fi
          if grep -qx 0 <<<"$forgotten"; then
            echo "# Some erasures were of roster entries only (no account): find them in your records and erase them again."
          fi
        } > "$redo"
      )
      echo "erasures to apply again: $redo (root only; it holds email addresses)"
    fi
  fi
  # The previous release is now the current one; there is nothing further
  # back to roll to.
  cp "$PREVIOUS/MANIFEST" "$MANIFEST"
  sed -i '/^migrations_before_upgrade=\|^backup_before_upgrade=\|^upgraded_at\|^upgraded_to=/d' "$MANIFEST"
  echo "installed_at=$(date -u +%Y-%m-%dT%H:%M:%SZ) (rollback)" >> "$MANIFEST"
  mv "$PREVIOUS" "$PREVIOUS.rolled-back-$(date -u +%Y%m%dT%H%M%SZ)"
  cmd_verify || die "verify found problems after the rollback (above)"
  echo "rolled back to $prev_release"
}

restore_previous_files() {
  install -m 755 "$PREVIOUS/bin/podium-server" "$PREVIOUS/bin/podium-judge" /usr/local/bin/
  local u
  for u in "$PREVIOUS"/systemd/*; do
    [[ -f $u ]] && install -m 644 "$u" /etc/systemd/system/
  done
  systemctl daemon-reload
  # Litestream's state belongs to the user its restored unit runs as
  # (podium before v1.1.0, podium-backup since).
  local lsdir owner
  lsdir="$(dirname "$(db_path)")/.$(basename "$(db_path)")-litestream"
  owner=$(sed -n 's/^User=//p' /etc/systemd/system/podium-litestream.service 2>/dev/null | head -n1)
  [[ -d $lsdir && -n $owner ]] && id "$owner" >/dev/null 2>&1 && chown -R "$owner:podium" "$lsdir"
  if systemctl is-enabled --quiet podium-litestream.service 2>/dev/null; then systemctl restart podium-litestream.service || true; fi
}

# verify: the state the gate needs, as ok, warn or FAIL lines. Exit 1 on any
# FAIL. Names only, never values.
cmd_verify() {
  local fails=0
  ok() { printf '  ok    %s\n' "$*"; }
  warn() { printf '  warn  %s\n' "$*"; }
  fail() { printf '  FAIL  %s\n' "$*"; fails=$((fails + 1)); }

  log "verify: services"
  local u
  for u in podium-server podium-judge isolate var-lib-isolate.mount podium-backup.timer podium-check.timer; do
    if systemctl is-active --quiet "$u"; then ok "$u active"; else fail "$u is $(systemctl is-active "$u" 2>/dev/null || true)"; fi
  done
  if [[ -n $(placeholders /etc/podium/litestream.env) || ! -f /etc/podium/litestream.env ]]; then
    fail "podium-litestream not configured: /etc/podium/litestream.env has placeholders (the database has no copy off the VM)"
  elif systemctl is-active --quiet podium-litestream; then ok "podium-litestream active"
  else fail "podium-litestream is $(systemctl is-active podium-litestream 2>/dev/null || true)"
  fi
  local base
  base=$(envval /etc/podium/server.env PODIUM_BASE_URL)
  if [[ -z $base || $base == *change-me* ]]; then
    fail "PODIUM_BASE_URL is a placeholder, so caddy was skipped: no HTTPS yet"
  elif systemctl is-active --quiet caddy; then ok "caddy active"
  else fail "caddy is $(systemctl is-active caddy 2>/dev/null || echo missing): sudo ./install.sh caddy"
  fi

  log "verify: endpoints"
  if curl -fsS -m 3 "http://$(listen_addr)/healthz" 2>/dev/null | grep -q '"status":"ok"'; then ok "/healthz ok"; else fail "/healthz does not answer ok on $(listen_addr)"; fi
  local code
  code=$(curl -s -o /dev/null -m 3 -w '%{http_code}' -X POST "http://$(judge_addr)/api/judge/claim" 2>/dev/null || true)
  if [[ $code == 401 ]]; then ok "judge API answers 401 without a token"; else fail "judge API on $(judge_addr) answered '${code:-nothing}', want 401"; fi

  log "verify: network"
  if ufw status 2>/dev/null | grep -q '^Status: active'; then
    local extra
    extra=$(ufw status | awk '/ALLOW/ {print $1}' | sed 's/ (v6)//' | sort -u | grep -vxE "($(ssh_port)|80|443)(/tcp|/udp)?" || true)
    if [[ -z $extra ]]; then ok "ufw active: only ssh, 80 and 443 allowed"; else fail "ufw allows more than ssh, 80 and 443: $(echo "$extra" | tr '\n' ' ')"; fi
  else
    fail "ufw is not active: sudo ./install.sh firewall"
  fi
  # A REJECT ahead of ufw (Oracle's image) closes 80 and 443 whatever ufw says.
  if input_order_ok "$(iptables -S INPUT 2>/dev/null)"; then ok "no REJECT ahead of http and https in iptables INPUT"
  else fail "iptables INPUT rejects before accepting http and https (a cloud image's own rules): sudo ./install.sh firewall"; fi
  local addr port
  for addr in "$(listen_addr)" "$(judge_addr)"; do
    port=${addr##*:}
    if ss -Hltn "sport = :$port" | awk '{print $4}' | grep -qvE '^(127\.|\[::1\]|::1)'; then
      fail "something listens on port $port beyond loopback"
    elif ss -Hltn "sport = :$port" | grep -q .; then ok "port $port on loopback only"
    else fail "nothing listens on port $port"
    fi
  done

  log "verify: configuration (names only)"
  local f mode
  for f in server judge; do
    mode=$(envval "/etc/podium/$f.env" PODIUM_MODE)
    if [[ -z $mode || $mode == production ]]; then ok "$f.env runs in production mode"; else fail "$f.env sets PODIUM_MODE=$mode; the VM runs production"; fi
  done
  for f in server judge litestream backup alert; do
    local p=/etc/podium/$f.env bad
    if [[ ! -f $p ]]; then
      [[ $f == alert || $f == backup ]] && { fail "$p missing: sudo ./install.sh ${f/alert/alerts}"; continue; }
      fail "$p missing"; continue
    fi
    [[ $(stat -c '%U %a' "$p") == "root 600" ]] || fail "$p is $(stat -c '%U %a' "$p"), want root 600"
    bad=$(placeholders "$p" | tr '\n' ' ')
    if [[ -n $bad ]]; then fail "$p has placeholders: $bad"; else ok "$p has no placeholders"; fi
  done
  [[ -n $(envval /etc/podium/alert.env PODIUM_ALERT_WEBHOOK) ]] && ok "PODIUM_ALERT_WEBHOOK set" || fail "PODIUM_ALERT_WEBHOOK missing in /etc/podium/alert.env: no alert reaches anyone"
  [[ -n $(envval /etc/podium/backup.env PODIUM_HEARTBEAT_URL) ]] && ok "PODIUM_HEARTBEAT_URL set" || warn "PODIUM_HEARTBEAT_URL missing: a missed backup goes unnoticed"
  # Litestream copies the database only; the test files leave the VM only
  # with the offsite nightly archive (V3-30).
  [[ -n $(envval /etc/podium/backup.env PODIUM_OFFSITE_BUCKET) ]] && ok "PODIUM_OFFSITE_BUCKET set" || warn "PODIUM_OFFSITE_BUCKET missing: the test files have no copy off the VM"
  local bdir
  bdir=$(backup_dir)
  if [[ $(stat -c '%U %a' "$bdir" 2>/dev/null) == "podium 700" ]]; then ok "$bdir podium 700"; else fail "$bdir is $(stat -c '%U %a' "$bdir" 2>/dev/null || echo missing), want podium 700"; fi

  log "verify: clock"
  # Every freeze, end and extension is a clock comparison.
  local sync
  sync=$(timedatectl show -p NTPSynchronized --value 2>/dev/null || echo no)
  if systemctl is-active --quiet chrony || systemctl is-active --quiet chronyd || systemctl is-active --quiet systemd-timesyncd; then
    if [[ $sync == yes ]]; then ok "time sync active and synchronised"; else fail "time sync is running but not synchronised yet (timedatectl timesync-status)"; fi
  else
    fail "no time sync service is active: sudo systemctl enable --now systemd-timesyncd (or install chrony)"
  fi

  log "verify: sandbox host"
  if [[ $(cat /sys/kernel/mm/transparent_hugepage/enabled 2>/dev/null) == *"[never]"* ]]; then ok "transparent huge pages off"; else warn "transparent huge pages are on: sudo ./install.sh isolate"; fi
  [[ -f $MANIFEST ]] && ok "installed: $(sed -n 's/^release=//p' "$MANIFEST")" || warn "no $MANIFEST yet (written by app and upgrade)"
  local held
  held=$(apt-mark showhold 2>/dev/null | grep -cxE "$(IFS='|'; echo "${HOLD_PACKAGES[*]}")" || true)
  if (( held > 0 )); then ok "$held compiler, runtime and isolate packages held (unhold before upgrading them)"
  else warn "compilers, runtimes and isolate are not held; before a contest season: sudo ./install.sh hold"; fi

  log "verify: backup keys out of the server's reach"
  local pid user
  pid=$(systemctl show -p MainPID --value podium-litestream 2>/dev/null || echo 0)
  if [[ ${pid:-0} != 0 ]]; then
    user=$(ps -o user= -p "$pid" 2>/dev/null | tr -d ' ')
    if [[ $user == podium-backup ]]; then ok "podium-litestream runs as podium-backup"; else fail "podium-litestream runs as ${user:-unknown}, want podium-backup (sudo ./install.sh users litestream)"; fi
  fi
  if [[ $(stat -c '%G %a' "$(dirname "$(db_path)")" 2>/dev/null) == "podium 2770" ]]; then ok "database directory group podium 2770"
  else fail "$(dirname "$(db_path)") is $(stat -c '%G %a' "$(dirname "$(db_path)")" 2>/dev/null || echo missing), want group podium 2770 (sudo ./install.sh dirs litestream)"; fi

  if (( fails > 0 )); then
    printf '\nverify: %d problem(s)\n' "$fails"
    return 1
  fi
  printf '\nverify: everything checked is in order\n'
}

# held_candidates: the HOLD_PACKAGES installed on this machine.
held_candidates() {
  local p
  for p in "${HOLD_PACKAGES[@]}"; do
    dpkg-query -W -f='${Status}' "$p" 2>/dev/null | grep -qE '^(install|hold) ok installed$' && echo "$p"
  done
  true
}

cmd_hold() {
  local pkgs
  mapfile -t pkgs < <(held_candidates)
  (( ${#pkgs[@]} > 0 )) || die "none of the compiler, runtime or isolate packages is installed; run the packages and isolate steps first"
  log "hold: apt will not upgrade ${#pkgs[@]} packages until unhold"
  apt-mark hold "${pkgs[@]}"
  echo "Held. apt list --upgradable shows them as held back; security updates for them wait too."
  echo "To upgrade them outside a contest season: sudo ./install.sh unhold, apt upgrade, restart podium-judge (it reports the new toolchain), revalidate."
}

cmd_unhold() {
  local pkgs
  mapfile -t pkgs < <(held_candidates)
  (( ${#pkgs[@]} > 0 )) || die "none of the held packages is installed"
  log "unhold: apt may upgrade the compilers, runtimes and isolate again"
  apt-mark unhold "${pkgs[@]}"
  echo "After an upgrade of any of them: sudo systemctl restart podium-judge, then validate the next contest's problems again."
}

checklist() {
  cat <<'EOF'

First-run checklist (docs/OPERATIONS.md, "Install")
  1. Fill in /etc/podium/server.env and /etc/podium/judge.env (the judge
     token is already set, the same in both), then: sudo ./install.sh app
  2. Fill in /etc/podium/litestream.env, then: sudo systemctl restart podium-litestream
  3. Fill in /etc/podium/backup.env (heartbeat URL),
     then: sudo systemctl start podium-backup
  4. Fill in /etc/podium/alert.env, then: sudo /usr/local/lib/podium/podium-alert test
  5. Point DNS at this VM, then: sudo ./install.sh caddy
  6. sudo ./install.sh verify     must end with "everything checked is in order"
EOF
}

case ${1:-} in
  upgrade) shift; cmd_upgrade "$@"; exit ;;
  rollback) shift; [[ $# -eq 0 ]] || die "rollback takes no arguments"; cmd_rollback; exit ;;
  verify) shift; cmd_verify; exit ;;
  hold) shift; cmd_hold; exit ;;
  unhold) shift; cmd_unhold; exit ;;
esac

steps=("$@")
[[ ${#steps[@]} -gt 0 ]] || steps=("${ALL_STEPS[@]}")
for s in "${steps[@]}"; do
  declare -F "step_$s" >/dev/null || die "unknown step '$s' (known: ${ALL_STEPS[*]}; commands: upgrade, rollback, verify, hold, unhold)"
done
for s in "${steps[@]}"; do "step_$s"; done
[[ $# -gt 0 ]] || checklist
