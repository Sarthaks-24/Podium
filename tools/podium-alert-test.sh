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
(( count != 1 ))
CURL
chmod +x "$tmp/bin/curl"
export CALLS="$tmp/calls"
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
echo 'podium-alert checks passed'
