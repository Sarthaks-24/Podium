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
if grep -q 'as_podium_install -d -m 2770 "$(dirname "$db")"' <<<"$rb"; then
  pass "rollback recreates the database folder as podium with mode 2770"
else
  fail "cmd_rollback must recreate the database folder as podium with mode 2770"
fi

# An unreadable database stops the script instead of reading as "no
# migrations", which would send a rollback down the same-schema branch.
mig() { # RUNUSER_EXIT FILE
  (
    die() { printf '%s\n' "$*"; exit 1; }
    runuser() { [[ $RC == 0 ]] && echo "0001_identity,0002_preferences"; return "$RC"; }
    RC=$1
    eval "$(fn migrations)"
    migrations "$2"
  )
}
: > "$tmp/podium.db"
if out=$(mig 0 "$tmp/podium.db") && [[ $out == "0001_identity,0002_preferences" ]]; then pass "migrations lists the applied versions"; else fail "migrations: $out"; fi
if out=$(mig 0 "$tmp/missing.db") && [[ -z $out ]]; then pass "migrations of a missing database is empty"; else fail "migrations of a missing database: $out"; fi
if out=$(mig 1 "$tmp/podium.db"); then fail "an unreadable database read as '$out'"; elif [[ $out == *"cannot read the applied migrations"* ]]; then pass "an unreadable database stops the script"; else fail "unexpected: $out"; fi
if grep -q 'is missing.*before-rollback' <<<"$rb"; then pass "rollback refuses to run without a database"; else fail "cmd_rollback must refuse when the database is missing"; fi

# Root never runs install on a path podium could have swapped for a link.
for f in step_dirs cmd_rollback; do
  if fn "$f" | grep -E '^ *install .*(/var/lib/podium/|\$\(dirname "\$db"\)|"\$db")' >/dev/null; then
    fail "$f runs install as root inside a podium folder"
  else
    pass "$f creates podium's folders as podium"
  fi
done

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

backup="$here/../deploy/backup/podium-backup"
if grep -q '^stamp=$(date -u +%Y-%m-%dT%H%M%SZ)$' "$backup" && grep -q '\[\[ ! -e $final \]\] || die' "$backup"; then
  pass "podium-backup names each backup to the second and never reuses a folder"
else
  fail "podium-backup must name backups to the second and refuse an existing folder (C2-039)"
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
  marker=$tmp/ran
  out=$(judge_check "{\"status\":\"ok\",\"jobs_queued\":\"a[\$(touch $marker)]\",\"oldest_queued_s\":900,\"judge_last_claim_s\":\"b[\$(touch $marker)]\"}")
  if [[ -e $marker ]]; then
    fail "podium-check ran a command from /healthz"
  elif [[ $out == "PROBLEM judge state unreadable" ]]; then
    pass "podium-check refuses a queue state that is not whole numbers"
  else
    fail "unexpected answer to a non-numeric queue state: $out"
  fi
else
  echo "skip: jq not installed, podium-check's judge rule not run"
fi

# The backup folder comes from backup.env, as podium-backup reads it
# (C2-009): an upgrade under a moved PODIUM_BACKUP_DIR must find its
# pre-upgrade backup, and rollback must record that folder.
bdir_of() { # VALUE
  (
    envval() { [[ $1 == /etc/podium/backup.env && $2 == PODIUM_BACKUP_DIR ]] && echo "$BDIR"; true; }
    BDIR=$1
    eval "$(grep '^backup_dir()' "$script")"
    backup_dir
  )
}
if [[ $(bdir_of /srv/podium-backups) == /srv/podium-backups && $(bdir_of "") == /var/backups/podium ]]; then
  pass "backup_dir reads PODIUM_BACKUP_DIR and defaults to /var/backups/podium"
else
  fail "backup_dir: '$(bdir_of /srv/podium-backups)' and '$(bdir_of "")'"
fi
for f in cmd_upgrade cmd_verify step_backup; do
  if fn "$f" | grep -q '/var/backups/podium'; then
    fail "$f names /var/backups/podium instead of backup_dir"
  elif fn "$f" | grep -q 'backup_dir'; then
    pass "$f uses the configured backup folder"
  else
    fail "$f does not read backup_dir"
  fi
done

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

# release_arch and check_arch (V3-28): a release built for one processor
# is refused on another before anything is installed.
arch_check() { # MANIFEST_LINES MACHINE_ARCH (empty lines: no MANIFEST)
  (
    die() { printf '%s\n' "$*"; exit 1; }
    eval "$(fn release_arch)"
    eval "$(fn check_arch)"
    rm -f "$tmp/arch-MANIFEST"
    [[ $1 == none ]] || printf '%b' "$1" > "$tmp/arch-MANIFEST"
    check_arch "$tmp/arch-MANIFEST" "$2"
  )
}
if out=$(arch_check 'version=v1.4.90\narch=arm64\n' arm64); then pass "an arm64 release installs on arm64"; else fail "an arm64 release was refused on arm64: $out"; fi
if out=$(arch_check 'version=v1.4.90\narch=arm64\n' amd64); then
  fail "an arm64 release went on on amd64"
elif [[ $out == *"built for arm64 but this machine is amd64"*"ARCH=amd64"* ]]; then
  pass "an arm64 release is refused on amd64 and names the build"
else
  fail "unexpected refusal: $out"
fi
if out=$(arch_check 'version=v1.4.0\n' arm64); then fail "a release without arch= went on on arm64"; else pass "a release without arch= counts as amd64"; fi
if out=$(arch_check 'version=v1.4.0\n' amd64); then pass "a release without arch= installs on amd64"; else fail "a release without arch= was refused on amd64: $out"; fi
if out=$(arch_check none arm64); then pass "a repository checkout (no MANIFEST) is not refused"; else fail "a checkout was refused: $out"; fi

# image_rejects and input_order_ok: Oracle's image rejects everything
# after ssh in INPUT, ahead of ufw's chains.
eval "$(fn image_rejects)"
eval "$(fn input_order_ok)"
oracle='-P INPUT ACCEPT
-A INPUT -m state --state RELATED,ESTABLISHED -j ACCEPT
-A INPUT -p icmp -j ACCEPT
-A INPUT -i lo -j ACCEPT
-A INPUT -p udp -m udp --sport 123 -j ACCEPT
-A INPUT -p tcp -m state --state NEW -m tcp --dport 22 -j ACCEPT
-A INPUT -j REJECT --reject-with icmp-host-prohibited
-A INPUT -j ufw-before-logging-input
-A INPUT -j ufw-before-input
-A FORWARD -j REJECT --reject-with icmp-host-prohibited
-A FORWARD -j ufw-before-logging-forward
-A OUTPUT -d 169.254.0.0/16 -j InstanceServices'
want='-A INPUT -j REJECT --reject-with icmp-host-prohibited
-A FORWARD -j REJECT --reject-with icmp-host-prohibited'
if [[ $(image_rejects "$oracle") == "$want" ]]; then pass "the image's two bare REJECT rules are found"; else fail "image_rejects found: $(image_rejects "$oracle")"; fi
ufwonly='-P INPUT DROP
-A INPUT -j ufw-before-logging-input
-A INPUT -j ufw-before-input
-A INPUT -j ufw-reject-input'
if [[ -z $(image_rejects "$ufwonly") ]]; then pass "ufw's own rules are never taken for the image's"; else fail "image_rejects matched ufw: $(image_rejects "$ufwonly")"; fi
if input_order_ok "$(grep -- '-A INPUT\|-P INPUT' <<<"$oracle")"; then fail "Oracle's INPUT passed the order check"; else pass "a REJECT ahead of http and https fails the order check"; fi
if input_order_ok "$ufwonly"; then pass "ufw alone passes the order check"; else fail "ufw alone failed the order check"; fi
opened='-A INPUT -p tcp -m state --state NEW -m tcp --dport 80 -j ACCEPT
-A INPUT -p tcp -m state --state NEW -m tcp --dport 443 -j ACCEPT
-A INPUT -p udp -m udp --dport 443 -j ACCEPT
-A INPUT -j REJECT --reject-with icmp-host-prohibited'
if input_order_ok "$opened"; then pass "http and https accepted ahead of a REJECT pass"; else fail "accepts ahead of a REJECT failed the order check"; fi

# v4 NB: the installer makes one judge token for both new env files, keeps
# an existing one on a re-run, and never prints it.
eval "$(grep '^envval()' -A3 "$script")"
eval "$(fn shared_judge_token)"
eval "$(fn put_judge_token)"
envdir=$tmp/etc-podium
mkdir -p "$envdir"
t1=$(shared_judge_token "$envdir")
t2=$(shared_judge_token "$envdir")
if [[ $t1 =~ ^[0-9a-f]{64}$ && $t1 != "$t2" ]]; then pass "a new judge token is 32 random bytes as hex"; else fail "new judge token: '${#t1}' characters, repeat equal: $([[ $t1 == "$t2" ]] && echo yes || echo no)"; fi
printf 'PODIUM_MODE=production\nPODIUM_JUDGE_TOKEN=change-me\nPODIUM_LOG_LEVEL=info\n' > "$envdir/server.env"
cp "$envdir/server.env" "$envdir/judge.env"
JUDGE_TOKEN=$t1 put_judge_token "$envdir/server.env"
JUDGE_TOKEN=$t1 put_judge_token "$envdir/judge.env"
if [[ $(envval "$envdir/server.env" PODIUM_JUDGE_TOKEN) == "$t1" && $(envval "$envdir/judge.env" PODIUM_JUDGE_TOKEN) == "$t1" ]]; then
  pass "both new env files get the same judge token"
else
  fail "the two env files' judge tokens differ"
fi
if [[ $(envval "$envdir/server.env" PODIUM_MODE) == production && $(envval "$envdir/server.env" PODIUM_LOG_LEVEL) == info && ! -e $envdir/server.env.tmp ]]; then
  pass "writing the token keeps the other lines and leaves no temporary copy"
else
  fail "put_judge_token changed other lines or left server.env.tmp"
fi
if [[ $(shared_judge_token "$envdir") == "$t1" ]]; then pass "a re-run keeps the token the judge already holds"; else fail "a re-run made a new judge token"; fi
rm "$envdir/server.env"
if [[ $(shared_judge_token "$envdir") == "$t1" ]]; then pass "a re-run with only judge.env left keeps its token"; else fail "judge.env's token was not kept"; fi
if fn step_dirs | grep -E '(echo|printf|log)[^#]*JUDGE_TOKEN' >/dev/null || fn put_judge_token | grep -E 'awk .*\$JUDGE_TOKEN|sed .*JUDGE_TOKEN' >/dev/null; then
  fail "the judge token is printed or put on a command line"
else
  pass "the judge token is never printed and never on a command line"
fi
if fn step_dirs | grep -q 'install -o root -g root -m 600 "$(example_for "$svc")"' && fn put_judge_token | grep -q 'install -m 600 /dev/null'; then
  pass "both env files and the temporary copy are mode 600"
else
  fail "an env file or the temporary copy is not created mode 600"
fi
if fn checklist | grep -q 'the same$' || ! fn checklist | grep -q 'token is already set'; then fail "the first-run checklist still asks for the judge token"; else pass "the first-run checklist no longer asks for the judge token"; fi

if (( fails > 0 )); then echo "$fails install.sh check(s) failed"; exit 1; fi
echo "install.sh checks passed"
