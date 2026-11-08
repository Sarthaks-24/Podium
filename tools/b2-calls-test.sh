#!/usr/bin/env bash
# Counts the list calls (Backblaze B2 class C) one day of podium's backup
# schedule makes, and fails above 1,000 (PF-11). B2's free tier allows 2,500
# a day; at the cap B2 refuses every listing until midnight UTC, which
# blocks podium-check, compaction and any restore.
#
# What is measured here, with the real scripts and real rclone against
# tools/fake-s3.py (which logs each request):
#   podium-check status   list calls per run (Litestream and offsite folders)
#   podium-backup         list calls per nightly run
# What is computed: runs a day, from deploy/systemd/podium-check.timer and
# the nightly timer, and Litestream's calls a day from the intervals in
# deploy/litestream.yml times the per-event counts measured with the real
# Litestream 0.5.16 under write load (cache/v2.0.2/b2-calls.md, summarised in
# docs/OPERATIONS.md "B2 transaction caps"). The per-event counts are
# constants below: change them only with a new measurement.
#
#   bash tools/b2-calls-test.sh     (Linux or WSL2: needs rclone, sqlite3, jq
#                                    and python3; skipped when one is missing)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
limit=1000

for c in rclone sqlite3 jq python3; do
  command -v "$c" >/dev/null || { echo "skip: $c is not installed, B2 call count not run"; exit 0; }
done
python3 -c 1 2>/dev/null || { echo "skip: python3 does not run, B2 call count not run"; exit 0; }
# podium-backup switches to the podium user as root, which cannot write the
# temporary folders: run as nobody instead, as offsite-test.sh asks.
if (( EUID == 0 )); then exec runuser -u nobody -- bash "$0" "$@"; fi

fails=0
fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

tmp=$(mktemp -d)
port=$(( 20000 + RANDOM % 20000 ))
python3 "$here/fake-s3.py" "$tmp/s3" "$port" "$tmp/calls.log" &
s3=$!
trap 'kill $s3 2>/dev/null || true; rm -rf "$tmp"' EXIT
mkdir -p "$tmp/s3/bkt/podium/0000" "$tmp/s3/off/nightly" "$tmp/etc" "$tmp/bin" "$tmp/state" \
  "$tmp/live/db" "$tmp/live/tests" "$tmp/backups"
for _ in $(seq 50); do curl -s -o /dev/null "http://127.0.0.1:$port/" && break; sleep 0.1; done
: > "$tmp/calls.log"
echo x > "$tmp/s3/bkt/podium/0000/0000000000000001-0000000000000001.ltx"
sqlite3 "$tmp/live/db/podium.db" "CREATE TABLE t(x); INSERT INTO t VALUES (1);"

cat > "$tmp/etc/litestream.env" <<EOF
LITESTREAM_ACCESS_KEY_ID=test-key
LITESTREAM_SECRET_ACCESS_KEY=test-secret
PODIUM_BACKUP_BUCKET=bkt
PODIUM_BACKUP_PATH=podium
PODIUM_BACKUP_ENDPOINT=http://127.0.0.1:$port
PODIUM_BACKUP_REGION=test-1
EOF
cat > "$tmp/etc/backup.env" <<EOF
PODIUM_DB_PATH=$tmp/live/db/podium.db
PODIUM_TESTS_DIR=$tmp/live/tests
PODIUM_BACKUP_DIR=$tmp/backups
PODIUM_BACKUP_KEEP_DAYS=14
PODIUM_SERVER_BIN=/nonexistent
PODIUM_OFFSITE_BUCKET=off
PODIUM_OFFSITE_PATH=nightly
PODIUM_OFFSITE_ENDPOINT=http://127.0.0.1:$port
PODIUM_OFFSITE_REGION=test-1
PODIUM_OFFSITE_ACCESS_KEY_ID=test-key
PODIUM_OFFSITE_SECRET_ACCESS_KEY=test-secret
EOF
# systemd and the server are not here: the check only needs them to answer.
printf '#!/bin/sh\necho active\n' > "$tmp/bin/systemctl"
printf '#!/bin/sh\nexit 22\n' > "$tmp/bin/curl"
chmod +x "$tmp/bin/systemctl" "$tmp/bin/curl"

list_calls() { grep -c '^C' "$tmp/calls.log" || true; }

# One podium-check run, as the timer runs it, with the env files in tmp.
sed "s#/etc/podium/#$tmp/etc/#g" "$here/../deploy/backup/podium-check" > "$tmp/podium-check"
: > "$tmp/calls.log"
PATH="$tmp/bin:$PATH" STATE_DIRECTORY="$tmp/state" PODIUM_DB_PATH="$tmp/live/db/podium.db" \
  bash "$tmp/podium-check" status > "$tmp/check.out" 2>&1 || true
check_run=$(list_calls)

# One nightly podium-backup run with the offsite upload.
sed "s#/etc/podium/backup.env#$tmp/etc/backup.env#" "$here/../deploy/backup/podium-backup" > "$tmp/podium-backup"
: > "$tmp/calls.log"
if ! (set -a; . "$tmp/etc/backup.env"; set +a; bash "$tmp/podium-backup" > "$tmp/backup.out" 2>&1); then
  fail "podium-backup did not finish against the fake bucket: $(tail -n 3 "$tmp/backup.out")"
fi
backup_run=$(list_calls)

# secs 30s|5m|1h: a Litestream interval in seconds.
secs() {
  case $1 in
    *ms) echo 0 ;;
    *s) echo "${1%s}" ;;
    *m) echo $(( ${1%m} * 60 )) ;;
    *h) echo $(( ${1%h} * 3600 )) ;;
    *) echo "bad interval $1" >&2; return 1 ;;
  esac
}

# Litestream 0.5.16 defaults when the file does not say (cmd/litestream
# DefaultConfig): levels 30 s, 5 min, 1 h; retention check every 15 s.
yml="$here/../deploy/litestream.yml"
levels=$(awk '/^levels:/ {f=1; next} f && /^  - interval:/ {print $3; next} f && !/^ / {f=0}' "$yml")
[[ -n $levels ]] || levels=$'30s\n5m\n1h'
retention=$(sed -n 's/^l0-retention-check-interval: *//p' "$yml" | head -n1)
retention=${retention:-15s}

# Measured with Litestream 0.5.16 under write load (cache/v2.0.2/b2-calls.md):
# a level 1 compaction lists the bucket about 3 times, a level 2 or 3 one 2
# times; each retention check lists level 1 and level 0 (2 calls); a start
# lists every level once or more (22 calls). The write-load case is the
# worst case: an idle database makes far fewer.
per_first=3
per_higher=2
per_retention=2
per_start=22
starts_a_day=3

ls_day=$(( starts_a_day * per_start ))
n=0
while read -r iv; do
  [[ -n $iv ]] || continue
  per=$per_higher; (( n > 0 )) || per=$per_first
  n=$(( n + 1 ))
  ls_day=$(( ls_day + 86400 / $(secs "$iv") * per ))
done <<< "$levels"
ls_day=$(( ls_day + 86400 / $(secs "$retention") * per_retention ))

timer_min=$(sed -n 's/^OnUnitActiveSec=\([0-9]*\)min$/\1/p' "$here/../deploy/systemd/podium-check.timer")
[[ -n $timer_min ]] || { fail "podium-check.timer has no OnUnitActiveSec=<n>min line"; timer_min=1; }
check_runs=$(( 1440 / timer_min ))

total=$(( ls_day + check_run * check_runs + backup_run ))
echo "info: podium-check $check_run list calls a run x $check_runs runs = $(( check_run * check_runs ))"
echo "info: podium-backup $backup_run list calls a night"
echo "info: Litestream $ls_day a day under write load (levels $(echo $levels), retention check $retention)"
echo "info: one day of the schedule: $total list calls, limit $limit"

if (( check_run >= 1 && check_run <= 2 )); then pass "podium-check lists once per bucket folder (measured $check_run)"; else fail "podium-check made $check_run list calls in one run, expected 1 or 2"; fi
if (( backup_run <= 1 )); then pass "podium-backup lists at most once a night (measured $backup_run)"; else fail "podium-backup made $backup_run list calls, expected at most 1"; fi
if (( total <= limit )); then pass "one day of the backup schedule makes $total list calls, at most $limit"; else fail "one day of the backup schedule makes $total list calls, over $limit (B2 free cap is 2,500)"; fi

if (( fails > 0 )); then echo "$fails check(s) failed"; exit 1; fi
echo "all checks passed"
