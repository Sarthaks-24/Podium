#!/usr/bin/env bash
set -euo pipefail

repo=$(cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
mkdir -p "$tmp/bin"
cat > "$tmp/bin/curl" <<'CURL'
#!/usr/bin/env bash
count=0
[[ ! -f $CALLS ]] || count=$(cat "$CALLS")
count=$((count + 1))
printf '%s' "$count" > "$CALLS"
cat > "$STDIN"
(( count != 1 ))
CURL
chmod +x "$tmp/bin/curl"
export CALLS="$tmp/calls" STDIN="$tmp/stdin"
export PATH="$tmp/bin:$PATH"
export STATE_DIRECTORY="$tmp/state"
export PODIUM_ALERT_WEBHOOK='https://example.test/topic'

if "$repo/deploy/backup/podium-alert" send test-key title message; then
  echo 'expected the first webhook post to fail' >&2
  exit 1
fi
[[ ! -f "$STATE_DIRECTORY/check-test-key" ]] || { echo 'failed post wrote its throttle file' >&2; exit 1; }
"$repo/deploy/backup/podium-alert" send test-key title message
[[ $(cat "$CALLS") == 2 ]] || { echo 'second call did not post again' >&2; exit 1; }
# PF-1: with no PODIUM_ALERT_WEBHOOK in the environment (a hand run), the
# URL comes from alert.env, read as data and never sourced.
unset PODIUM_ALERT_WEBHOOK
export PODIUM_ALERT_ENV="$tmp/alert.env"
alert="$repo/deploy/backup/podium-alert"
# fresh: the mock curl succeeds from its second call on; clear state and output.
fresh() { printf 1 > "$CALLS"; rm -rf "$STATE_DIRECTORY" "$STDIN" "$tmp/out"; }
fail() { echo "$1" >&2; exit 1; }

fresh
{
  echo 'PODIUM_ALERT_WEBHOOK=https://example.test/from-file'
  printf 'touch %s/pwned1\n' "$tmp"
  printf 'OTHER=$(touch %s/pwned2)
' "$tmp"
} > "$PODIUM_ALERT_ENV"
"$alert" send file-key title message > "$tmp/out" 2>&1 || fail "no post with the URL in alert.env: $(cat "$tmp/out")"
grep -qF 'https://example.test/from-file' "$STDIN" || fail 'curl did not get the URL from alert.env'
[[ ! -e $tmp/pwned1 && ! -e $tmp/pwned2 ]] || fail 'alert.env was executed, not read as data'
! grep -qF 'example.test' "$tmp/out" || fail 'the webhook URL was printed'
"$alert" send file-key title message > "$tmp/out" 2>&1
grep -q 'held back' "$tmp/out" && [[ $(cat "$CALLS") == 2 ]] || fail 'throttle did not hold the second alert with a file URL'

for q in '"' "'"; do
  fresh
  printf 'PODIUM_ALERT_WEBHOOK=%shttps://example.test/quoted%s
' "$q" "$q" > "$PODIUM_ALERT_ENV"
  "$alert" test > "$tmp/out" 2>&1 || fail "quoted value ($q) not accepted: $(cat "$tmp/out")"
  grep -qF '"https://example.test/quoted"' "$STDIN" || fail "quotes ($q) were not stripped"
done

fresh
printf 'PODIUM_ALERT_WEBHOOK=https://example.test/crlf\r\n' > "$PODIUM_ALERT_ENV"
"$alert" test > "$tmp/out" 2>&1 || fail "CRLF alert.env not accepted: $(cat "$tmp/out")"

fresh
echo 'PODIUM_ALERT_WEBHOOK=https://example.test/from-file' > "$PODIUM_ALERT_ENV"
PODIUM_ALERT_WEBHOOK=https://example.test/from-env "$alert" test > /dev/null 2>&1
grep -qF 'from-env' "$STDIN" || fail 'the environment did not win over alert.env'

fresh
rm -f "$PODIUM_ALERT_ENV"
if "$alert" test > "$tmp/out" 2>&1; then fail 'missing alert.env: expected failure'; fi
grep -q 'cannot read' "$tmp/out" && [[ $(cat "$CALLS") == 1 ]] || fail "missing alert.env: wrong message or a post was made: $(cat "$tmp/out")"

fresh
echo 'PODIUM_ALERT_WEBHOOK=' > "$PODIUM_ALERT_ENV"
if "$alert" test > "$tmp/out" 2>&1; then fail 'empty value: expected failure'; fi
grep -q 'missing or empty' "$tmp/out" && ! grep -q 'cannot read' "$tmp/out" && [[ $(cat "$CALLS") == 1 ]] || fail "empty value: wrong message or a post was made: $(cat "$tmp/out")"
echo 'podium-alert checks passed'
