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
    # Read by the functions eval'd below.
    # shellcheck disable=SC2034
    LIB=/usr/local/lib/podium
    # shellcheck disable=SC2034
    PREVIOUS=$tmp/previous
    # shellcheck disable=SC2034
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

# A schema rollback says which erasures it undoes and writes them down
# again (C1-047); the nightly backup prunes old pre-rollback folders.
if grep -q "action = 'user_forgotten' AND at >= \$upgraded_ms" <<<"$rb" && grep -q 'podium-rollback-forget-' <<<"$rb"; then
  pass "rollback lists the forget-user erasures it undoes"
else
  fail "cmd_rollback must list user_forgotten rows newer than the backup"
fi
if grep -q 'before-rollback-\*" -mtime +"\$keep"' "$here/../deploy/backup/podium-backup"; then
  pass "podium-backup prunes db.before-rollback-* after the keep days"
else
  fail "podium-backup must prune db.before-rollback-* folders"
fi

# The rsync copy runs as root with root's key, never as podium (C1-046).
backup="$here/../deploy/backup/podium-backup"
if grep -q 'as_podium rsync' "$backup"; then
  fail "podium-backup runs rsync as podium, whose key a taken-over server could use"
elif grep -q -- '-i $ssh_dir/id_ed25519' "$backup" && grep -q '^ssh_dir=/etc/podium/backup-ssh$' "$backup"; then
  pass "podium-backup copies with the root-only key in /etc/podium/backup-ssh"
else
  fail "podium-backup must copy with /etc/podium/backup-ssh/id_ed25519"
fi

# podium-check's stalled-judge rule (C1-048), with the server's answer and
# systemctl stubbed. Needs jq, as on the VM; skipped without it.
check="$here/../deploy/backup/podium-check"
judge_check() { # HEALTHZ_JSON
  (
    envget() { printf '%s' "$3"; }
    curl() { printf '%s' "$HEALTH"; }
    systemctl() { echo failed; }
    problem() { echo "PROBLEM $2"; }
    fine() { echo "ok $2"; }
    HEALTH=$1
    eval "$(sed -n '/^check_judge() {/,/^}/p' "$check")"
    check_judge
  )
}
if command -v jq >/dev/null; then
  out=$(judge_check '{"status":"ok","jobs_queued":3,"oldest_queued_s":900,"judge_last_claim_s":600}')
  if [[ $out == "PROBLEM judge stalled" ]]; then pass "podium-check alerts on jobs waiting with no judge claiming"; else fail "stalled judge not reported: $out"; fi
  out=$(judge_check '{"status":"ok","jobs_queued":3,"oldest_queued_s":900,"judge_last_claim_s":null}')
  if [[ $out == "PROBLEM judge stalled" ]]; then pass "podium-check alerts when no judge claimed since the start"; else fail "never-claimed judge not reported: $out"; fi
  out=$(judge_check '{"status":"ok","jobs_queued":3,"oldest_queued_s":900,"judge_last_claim_s":2}')
  if [[ $out == "ok judge" ]]; then pass "podium-check stays quiet while a judge claims (a paused contest holds jobs)"; else fail "false alarm: $out"; fi
  out=$(judge_check '{"status":"ok","jobs_queued":0,"oldest_queued_s":0,"judge_last_claim_s":null}')
  if [[ $out == "ok judge" ]]; then pass "podium-check stays quiet with an empty queue"; else fail "false alarm on an empty queue: $out"; fi
else
  echo "skip: jq not installed, podium-check's judge rule not run"
fi

# The upgrade path (B6.2): the new MANIFEST's upgrades_from refuses an
# older or unversioned install and a downgrade. Run in a subshell because
# die exits.
path_check() { # INSTALLED_MANIFEST_LINES NEW_VERSION UPGRADES_FROM
  (
    # Read by the functions eval'd below.
    # shellcheck disable=SC2034
    LIB=/usr/local/lib/podium
    # shellcheck disable=SC2034
    MANIFEST=$tmp/installed/MANIFEST
    # shellcheck disable=SC2034
    HERE=$tmp/new
    rm -rf "$tmp/installed" "$tmp/new"
    mkdir -p "$tmp/installed" "$tmp/new"
    [[ -z $1 ]] || printf '%b' "$1" > "$tmp/installed/MANIFEST"
    printf 'version=%s\nupgrades_from=%s\n' "$2" "$3" > "$tmp/new/MANIFEST"
    die() { printf '%s\n' "$*"; exit 1; }
    eval "$(grep '^manifest_get()' "$script")"
    eval "$(grep -E '^(version_ge|is_release_version)\(\)' "$script")"
    eval "$(fn installed_version)"
    eval "$(fn check_upgrade_path)"
    check_upgrade_path
  )
}
if out=$(path_check 'release=podium-v1.3.0\nversion=v1.3.0\n' v2.0.0 v1.4.0); then
  fail "an upgrade from v1.3.0 to a release that upgrades from v1.4.0 went on"
elif [[ $out == *"runs v1.3.0"*"only from v1.4.0"* ]]; then
  pass "an upgrade from v1.3.0 is refused when the release upgrades from v1.4.0"
else
  fail "unexpected refusal: $out"
fi
if out=$(path_check 'release=podium-v1.4.0\nversion=v1.4.0\n' v2.0.0 v1.4.0); then pass "an upgrade from the oldest allowed release goes on"; else fail "an allowed upgrade was refused: $out"; fi
if out=$(path_check 'release=podium-v1.0.0\n' v1.1.0 v1.0.0); then pass "a hand-built v1.0.0 (no version line) is read from its folder name"; else fail "v1.0.0 by folder name was refused: $out"; fi
if out=$(path_check '' v1.5.0 v1.5.0); then
  fail "an upgrade over an unversioned install went on"
elif [[ $out == *"cannot tell which release is installed"* ]]; then
  pass "an upgrade over an unversioned install is refused"
else
  fail "unexpected refusal: $out"
fi
if out=$(path_check 'version=v1.5.1\n' v1.5.0 v1.5.0); then
  fail "a downgrade went on"
elif [[ $out == *"older than the installed"*"rollback"* ]]; then
  pass "a downgrade is refused and names rollback"
else
  fail "unexpected refusal: $out"
fi

if (( fails > 0 )); then echo "$fails install.sh check(s) failed"; exit 1; fi
echo "install.sh checks passed"
