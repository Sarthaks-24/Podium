#!/usr/bin/env bash
# Unit checks for deploy/install.sh that need no root, systemd or VM: the
# functions under test are cut out of the script and run against
# temporary files. Runs in Git Bash and on Linux; `make check` calls it.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/../deploy/install.sh"
fails=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

# fn NAME: the body of one top-level function of install.sh.
fn() { sed -n "/^$1() {/,/^}/p" "$script"; }

# check_unfinished_upgrade (C1-005), run in a subshell because die exits.
run_check() {
  (
    LIB=/usr/local/lib/podium
    PREVIOUS=$tmp/previous
    MANIFEST=$tmp/MANIFEST
    die() { printf '%s\n' "$*"; exit 1; }
    eval "$(grep '^manifest_get()' "$script")"
    eval "$(fn check_unfinished_upgrade)"
    check_unfinished_upgrade "$1" "$tmp/podium-server"
  )
}

reset() {
  rm -rf "$tmp/previous" "$tmp/MANIFEST"
  mkdir -p "$tmp/previous"
  printf 'old binary\n' > "$tmp/podium-server"
  printf 'release=podium-v1.0.0\npodium_server_sha256=%s\n' \
    "$(sha256sum "$tmp/podium-server" | cut -d' ' -f1)" > "$tmp/MANIFEST"
}

reset
if out=$(run_check podium-v1.1.0); then pass "a first upgrade goes on"; else fail "a first upgrade was refused: $out"; fi

reset
printf 'release=podium-v1.0.0\nupgraded_to=podium-v1.1.0\n' > "$tmp/previous/MANIFEST"
printf 'new binary\n' > "$tmp/podium-server"
if out=$(run_check podium-v1.1.0); then
  fail "a rerun after step 7 failed was not refused"
elif [[ $out == *"did not finish"*"rollback"* ]]; then
  pass "a rerun after step 7 failed is refused and names rollback"
else
  fail "unexpected refusal: $out"
fi

reset
printf 'release=podium-v1.0.0\nupgraded_to=podium-v1.1.0\n' > "$tmp/previous/MANIFEST"
if out=$(run_check podium-v1.1.0); then
  fail "a second run of a finished upgrade was not refused"
elif [[ $out == *"already installed"* ]]; then
  pass "a second run of a finished upgrade is refused"
else
  fail "unexpected refusal: $out"
fi

reset
printf 'release=podium-v1.0.0\nupgraded_to=podium-v1.1.0\n' > "$tmp/previous/MANIFEST"
if out=$(run_check podium-v1.2.0); then pass "the next upgrade after a finished one goes on"; else fail "the next upgrade was refused: $out"; fi

reset
printf 'release=podium-v0.9.0\n' > "$tmp/MANIFEST"
if out=$(run_check podium-v1.1.0); then pass "a MANIFEST without hashes is not refused"; else fail "a MANIFEST without hashes was refused: $out"; fi

# The upgrade calls the check before anything changes (step 1).
up=$(fn cmd_upgrade)
called=$(grep -n 'check_unfinished_upgrade' <<<"$up" | head -n1 | cut -d: -f1 || true)
stop=$(grep -n 'systemctl stop' <<<"$up" | head -n1 | cut -d: -f1 || true)
if [[ -n $called && -n $stop && $called -lt $stop ]]; then
  pass "upgrade checks for an unfinished upgrade before it stops anything"
else
  fail "cmd_upgrade must call check_unfinished_upgrade before it stops the judge"
fi

# The schema rollback gives the restored folder group write (C1-004).
rb=$(fn cmd_rollback)
restore=$(grep -n 'scratch/podium.db" "\$db"' <<<"$rb" | head -n1 | cut -d: -f1 || true)
group=$(grep -n '^ *db_group_rw$' <<<"$rb" | head -n1 | cut -d: -f1 || true)
start=$(grep -n 'systemctl start podium-server' <<<"$rb" | tail -n1 | cut -d: -f1 || true)
if [[ -n $restore && -n $group && $restore -lt $group && $group -lt $start ]]; then
  pass "rollback runs db_group_rw on the restored database before the services start"
else
  fail "cmd_rollback must call db_group_rw after restoring the database and before starting the services"
fi
if grep -q 'install -d -o podium -g podium -m 2770 "$(dirname "$db")"' <<<"$rb"; then
  pass "rollback recreates the database folder with mode 2770"
else
  fail "cmd_rollback must recreate the database folder with mode 2770"
fi

if (( fails > 0 )); then echo "$fails install.sh check(s) failed"; exit 1; fi
echo "install.sh checks passed"
