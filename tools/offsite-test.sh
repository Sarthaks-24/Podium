#!/usr/bin/env bash
# Rehearses the offsite nightly copy (V3-30) on one Linux machine, no root,
# no network: podium-backup uploads to tools/fake-s3.py on loopback, then
# podium-restore offsite brings it back, and the restored database must
# match. Also checks that a half-configured or unreachable bucket keeps the
# local backup, sends no heartbeat and exits 1.
#
#   bash tools/offsite-test.sh        (Linux or WSL2: needs rclone, sqlite3,
#                                      jq and python3)
#
# Not as root: as root podium-backup switches to the podium user, which
# cannot write the test's temporary folders. In WSL2, whose default user
# is often root: runuser -u nobody -- bash tools/offsite-test.sh
set -euo pipefail
(( EUID != 0 )) || { echo "offsite-test: run as a normal user, not root (runuser -u nobody -- bash $0)"; exit 1; }

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
backup="$here/../deploy/backup/podium-backup"
restore="$here/../deploy/backup/podium-restore"
for c in rclone sqlite3 jq python3; do command -v "$c" >/dev/null || { echo "offsite-test: $c is not installed"; exit 1; }; done

fails=0
fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

tmp=$(mktemp -d)
port=$(( 20000 + RANDOM % 20000 ))
python3 "$here/fake-s3.py" "$tmp/s3" "$port" &
s3=$!
trap 'kill $s3 2>/dev/null || true; rm -rf "$tmp"' EXIT
mkdir -p "$tmp/s3/podium-nightly-test" "$tmp/live/db" "$tmp/live/tests/ab" "$tmp/backups"
for _ in $(seq 50); do curl -s -o /dev/null "http://127.0.0.1:$port/" && break; sleep 0.1; done

sqlite3 "$tmp/live/db/podium.db" "CREATE TABLE t(x); INSERT INTO t VALUES ('student code'), ('verdict');"
echo "1 2 3" > "$tmp/live/tests/ab/abcdef.in"
printf '#!/bin/sh\nexit 0\n' > "$tmp/server-stub" && chmod +x "$tmp/server-stub"

# The settings podium-backup reads from its unit's environment, written
# as backup.env too for podium-restore.
cat > "$tmp/backup.env" <<EOF
PODIUM_DB_PATH=$tmp/live/db/podium.db
PODIUM_TESTS_DIR=$tmp/live/tests
PODIUM_BACKUP_DIR=$tmp/backups
PODIUM_BACKUP_KEEP_DAYS=14
PODIUM_SERVER_BIN=/nonexistent
PODIUM_OFFSITE_BUCKET=podium-nightly-test
PODIUM_OFFSITE_PATH=nightly
PODIUM_OFFSITE_ENDPOINT=http://127.0.0.1:$port
PODIUM_OFFSITE_REGION=test-1
PODIUM_OFFSITE_ACCESS_KEY_ID=test-key
PODIUM_OFFSITE_SECRET_ACCESS_KEY=test-secret
EOF
run_backup() { (set -a; . "$tmp/backup.env"; set +a; for kv in "$@"; do export "${kv?}"; done; bash "$backup"); }

# 1. A full run uploads one archive.
if out=$(run_backup 2>&1); then
  if [[ $out == *"offsite copy nightly/"*".tar.gz uploaded"* ]]; then pass "the nightly backup is uploaded"; else fail "no upload line: $out"; fi
else
  fail "podium-backup failed: $out"
fi
n=$(find "$tmp/s3/podium-nightly-test/nightly" -name '20*.tar.gz' 2>/dev/null | wc -l)
[[ $n -eq 1 ]] && pass "one archive in the bucket" || fail "$n archives in the bucket, want 1"
if [[ $out == *"PODIUM_OFFSITE_SECRET_ACCESS_KEY set"* && $out != *test-secret* && $out != *test-key* ]]; then
  pass "the start-up report names the key, never its value"
else
  fail "the start-up report: $out"
fi

# 2. Restore from the bucket into a scratch folder.
if out=$(PODIUM_BACKUP_ENV="$tmp/backup.env" PODIUM_SERVER_BIN="$tmp/server-stub" bash "$restore" offsite "$tmp/scratch" 2>&1); then
  pass "podium-restore offsite ran"
else
  fail "podium-restore offsite failed: $out"
fi
if [[ $(sqlite3 "$tmp/scratch/podium.db" "SELECT group_concat(x) FROM t" 2>/dev/null) == "student code,verdict" ]]; then
  pass "the restored database holds the live rows"
else
  fail "the restored database differs"
fi
if cmp -s "$tmp/live/tests/ab/abcdef.in" "$tmp/scratch/tests/ab/abcdef.in"; then pass "the restored test files match"; else fail "test files missing or different after restore"; fi
if [[ ! -e $tmp/scratch/.unpack ]]; then pass "no unpack folder left behind"; else fail "the unpack folder was left in scratch"; fi
if out=$(PODIUM_BACKUP_ENV="$tmp/backup.env" PODIUM_SERVER_BIN="$tmp/server-stub" bash "$restore" offsite "$tmp/scratch2" '../../etc/passwd' 2>&1); then
  fail "a path as the archive name was accepted"
else
  pass "an archive name that is not a nightly stamp is refused"
fi

# 3. Half configured: the local backup is kept, exit 1, no heartbeat.
sleep 1
before=$(find "$tmp/backups" -maxdepth 1 -name '20*' -type d | wc -l)
if out=$(run_backup PODIUM_OFFSITE_SECRET_ACCESS_KEY= PODIUM_HEARTBEAT_URL=https://127.0.0.1:1/never 2>&1); then
  fail "a half-configured bucket exited 0"
else
  after=$(find "$tmp/backups" -maxdepth 1 -name '20*' -type d | wc -l)
  if [[ $out == *"PODIUM_OFFSITE_SECRET_ACCESS_KEY"*"missing"* && $after -eq $((before + 1)) && $out != *"heartbeat sent"* && $out != *"heartbeat ping failed"* ]]; then
    pass "a missing key keeps the local backup, names the variable and skips the heartbeat"
  else
    fail "half configured: kept $before -> $after; $out"
  fi
fi

# 4. Unreachable bucket: same outcome.
sleep 1
if out=$(run_backup "PODIUM_OFFSITE_ENDPOINT=http://127.0.0.1:1" 2>&1); then
  fail "an unreachable bucket exited 0"
elif [[ $out == *"offsite upload failed"*"is kept"* ]]; then
  pass "an unreachable bucket keeps the local backup and exits 1"
else
  fail "unreachable: $out"
fi

# 5. Plain http to anything but loopback is refused.
sleep 1
if out=$(run_backup "PODIUM_OFFSITE_ENDPOINT=http://example.com" 2>&1); then
  fail "a plain http endpoint was accepted"
elif [[ $out == *"must start with https://"* ]]; then
  pass "a plain http endpoint off loopback is refused"
else
  fail "http endpoint: $out"
fi

# 6. No offsite settings at all: the backup works as before.
sleep 1
if out=$(run_backup PODIUM_OFFSITE_BUCKET= PODIUM_OFFSITE_ENDPOINT= PODIUM_OFFSITE_REGION= PODIUM_OFFSITE_ACCESS_KEY_ID= PODIUM_OFFSITE_SECRET_ACCESS_KEY= 2>&1) && [[ $out != *offsite\ copy* ]]; then
  pass "without offsite settings the backup stays local and succeeds"
else
  fail "local only: $out"
fi

if (( fails > 0 )); then echo "$fails offsite check(s) failed"; exit 1; fi
echo "offsite checks passed"
