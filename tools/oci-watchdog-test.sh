#!/usr/bin/env bash
# Checks tools/oci-watchdog.sh against tools/fake-oci.py on loopback: every
# Oracle API request carries a signature the fake verifies with the key's
# public half, a stopped VM is started, a gone VM and a dead site raise
# the heartbeat's fail, and a wrong key is reported as such. No account,
# no network.
#
#   bash tools/oci-watchdog-test.sh     (Linux, WSL2 or Git Bash with
#                                        python3, openssl, curl and jq)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for c in python3 openssl curl jq; do command -v "$c" >/dev/null || { echo "oci-watchdog-test: $c is not installed"; exit 1; }; done
fails=0
fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

tmp=$(mktemp -d)
openssl genrsa -out "$tmp/key.pem" 2048 2>/dev/null
openssl rsa -in "$tmp/key.pem" -pubout -out "$tmp/pub.pem" 2>/dev/null
openssl genrsa -out "$tmp/other.pem" 2048 2>/dev/null
port=$(( 20000 + RANDOM % 20000 ))
python3 "$here/fake-oci.py" "$tmp" "$port" "$tmp/pub.pem" &
srv=$!
trap 'kill $srv 2>/dev/null || true; rm -rf "$tmp"' EXIT
for _ in $(seq 50); do curl -s -o /dev/null "http://127.0.0.1:$port/healthz" && break; sleep 0.1; done

# watch [KEYFILE]: one watchdog run with fresh logs.
watch() {
  : > "$tmp/log"
  OCI_TENANCY_OCID=ocid1.tenancy.test OCI_USER_OCID=ocid1.user.test OCI_FINGERPRINT=aa:bb \
  OCI_REGION=test-1 OCI_INSTANCE_OCID=ocid1.instance.secret-id \
  OCI_PRIVATE_KEY="$(cat "${1:-$tmp/key.pem}")" OCI_API_BASE="http://127.0.0.1:$port" \
  PODIUM_URL="http://127.0.0.1:$port" WATCHDOG_PING_URL="http://127.0.0.1:$port/ping" \
  WATCHDOG_NTFY_URL="http://127.0.0.1:$port/ntfy" \
    bash "$here/oci-watchdog.sh" 2>&1
}
logged() { grep -q -- "$1" "$tmp/log"; }

echo RUNNING > "$tmp/state"; echo ok > "$tmp/healthz"
out=$(watch)
if logged '^api GET RUNNING' && logged '^/ping VM running, site answers' && ! logged '/ping/fail'; then pass "a running VM with a live site pings ok"; else fail "running: $(cat "$tmp/log")"; fi
if [[ $out != *secret-id* && $out != *127.0.0.1* && $out != *BEGIN* ]]; then pass "the log shows no OCID, URL or key"; else fail "the log leaks: $out"; fi

echo down > "$tmp/healthz"
watch >/dev/null
if logged '^/ping/fail VM running but'; then pass "a running VM with a dead site pings fail"; else fail "dead site: $(cat "$tmp/log")"; fi

echo STOPPED > "$tmp/state"; echo ok > "$tmp/healthz"
watch >/dev/null
if logged '^api POST START' && logged '^/ntfy Oracle had stopped' && logged '^/ping/fail VM was stopped; started it'; then
  pass "a stopped VM is started with a signed POST, ntfy is told, the heartbeat fails until the site answers"
else
  fail "stopped: $(cat "$tmp/log")"
fi
if [[ $(cat "$tmp/state") == STARTING ]]; then pass "the start reached the API"; else fail "state after start: $(cat "$tmp/state")"; fi

echo GONE > "$tmp/state"
watch >/dev/null
if logged '^/ping/fail VM is GONE' && logged '^/ntfy The Podium VM is gone'; then pass "a deleted VM raises the rebuild alert"; else fail "gone: $(cat "$tmp/log")"; fi

echo RUNNING > "$tmp/state"
watch "$tmp/other.pem" >/dev/null
if logged 'bad-signature' && logged '^/ping/fail Oracle refused the watchdog'; then pass "a wrong key is refused by the API and reported as a key problem"; else fail "wrong key: $(cat "$tmp/log")"; fi

if out=$(OCI_REGION=x bash "$here/oci-watchdog.sh" 2>&1); then fail "missing settings exited 0"; elif [[ $out == *"OCI_TENANCY_OCID missing"* ]]; then pass "a missing setting is named"; else fail "missing settings: $out"; fi

if (( fails > 0 )); then echo "$fails watchdog check(s) failed"; exit 1; fi
echo "watchdog checks passed"
