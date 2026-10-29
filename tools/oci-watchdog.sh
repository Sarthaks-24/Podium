#!/usr/bin/env bash
# Keeps the Oracle Always Free VM up (V3-28, option B of
# cache/v3/oracle-deploy-plan.md). Run by .github/workflows/watchdog.yml
# every 30 minutes. Oracle may stop or delete an Always Free VM it deems
# idle; this asks Oracle's API for the VM's state and:
#
#   RUNNING      checks PODIUM_URL/healthz; pings the heartbeat ok, or
#                /fail when the site does not answer
#   STOPPED      starts the VM, posts to ntfy, pings /fail until the site
#                answers again on a later run
#   STARTING, STOPPING, PROVISIONING
#                pings ok and waits for the next run
#   TERMINATED or not found
#                pings /fail: the VM is gone, rebuild from backups
#                (docs/OPERATIONS.md, "Oracle Always Free")
#
# The heartbeat service (healthchecks.io) sends the alert, once per change
# of state, and also alerts when this stops running at all (GitHub pauses
# schedules in a repository with no activity for 60 days).
#
# Requests are signed with the API key as Oracle's API requires
# (rsa-sha256 over date, request target and host, plus the body headers
# for a POST), with curl and openssl only: no third-party code sees the key.
#
# Settings, all from the environment (GitHub secrets, except PODIUM_URL):
#   OCI_TENANCY_OCID OCI_USER_OCID OCI_FINGERPRINT OCI_REGION
#   OCI_INSTANCE_OCID   the VM and the watchdog user's API key
#   OCI_PRIVATE_KEY     the API key's private half, PEM text
#   PODIUM_URL          https://<host>, the site to check
#   WATCHDOG_PING_URL   healthchecks.io ping URL
#   WATCHDOG_NTFY_URL   optional: ntfy topic URL for "started the VM"
#   OCI_API_BASE        optional: API root, for tools/oci-watchdog-test.sh
#
# The repository is public and so are its logs: this prints states, never
# an OCID, a URL or a key.
set -euo pipefail

die() { echo "watchdog: $*" >&2; exit 1; }
for v in OCI_TENANCY_OCID OCI_USER_OCID OCI_FINGERPRINT OCI_REGION OCI_INSTANCE_OCID OCI_PRIVATE_KEY PODIUM_URL WATCHDOG_PING_URL; do
  if [[ -n ${!v:-} ]]; then echo "watchdog: $v set"; else die "$v missing (a repository secret; docs/OPERATIONS.md, Oracle Always Free)"; fi
done
echo "watchdog: WATCHDOG_NTFY_URL $([[ -n ${WATCHDOG_NTFY_URL:-} ]] && echo set || echo missing)"

host=iaas.$OCI_REGION.oraclecloud.com
base=${OCI_API_BASE:-https://$host}
key=$(mktemp)
trap 'rm -f "$key"' EXIT
chmod 600 "$key"
printf '%s\n' "$OCI_PRIVATE_KEY" > "$key"

# oci METHOD PATH: one signed request; prints the body, then the HTTP code
# on the last line.
oci() {
  local method=$1 path=$2 date target sig headers
  date=$(LC_ALL=C TZ=GMT date '+%a, %d %b %Y %H:%M:%S GMT')
  target="$(tr '[:upper:]' '[:lower:]' <<<"$method") $path"
  local signing="date: $date
(request-target): $target
host: $host"
  headers="date (request-target) host"
  local extra=()
  if [[ $method == POST ]]; then
    # An empty body: Oracle still wants its length, type and hash signed.
    local sha
    sha=$(printf '' | openssl dgst -sha256 -binary | openssl base64 -A)
    signing="$signing
content-length: 0
content-type: application/json
x-content-sha256: $sha"
    headers="$headers content-length content-type x-content-sha256"
    extra=(-H "content-length: 0" -H "content-type: application/json" -H "x-content-sha256: $sha" --data-binary '')
  fi
  sig=$(printf '%s' "$signing" | openssl dgst -sha256 -sign "$key" | openssl base64 -A)
  curl -sS -m 30 -X "$method" "$base$path" \
    -H "date: $date" -H "host: $host" "${extra[@]}" \
    -H "authorization: Signature version=\"1\",keyId=\"$OCI_TENANCY_OCID/$OCI_USER_OCID/$OCI_FINGERPRINT\",algorithm=\"rsa-sha256\",headers=\"$headers\",signature=\"$sig\"" \
    -w '\n%{http_code}'
}

# ping [fail] MESSAGE: the heartbeat, with the message as the body so it
# shows in the heartbeat service's log. A failed ping only warns: its
# silence is itself the alert.
ping() {
  local url=$WATCHDOG_PING_URL
  if [[ $1 == fail ]]; then url=$url/fail; shift; fi
  echo "watchdog: $1"
  curl -fsS -m 15 --retry 3 -o /dev/null --data-raw "$1" "$url" || echo "watchdog: warning: the heartbeat ping failed" >&2
}
notify() {
  [[ -n ${WATCHDOG_NTFY_URL:-} ]] || return 0
  curl -fsS -m 15 -o /dev/null -H "Title: Podium watchdog" -H "Priority: high" --data-raw "$1" "$WATCHDOG_NTFY_URL" \
    || echo "watchdog: warning: the ntfy post failed" >&2
}

path=/20160918/instances/$OCI_INSTANCE_OCID
out=$(oci GET "$path") || { ping fail "Oracle's API did not answer"; exit 0; }
code=${out##*$'\n'}
body=${out%$'\n'*}
case $code in
  200) state=$(jq -r '.lifecycleState // empty' <<<"$body") ;;
  404) state=GONE ;;
  401) ping fail "Oracle refused the watchdog's key (401): the API key, fingerprint or user changed"; exit 0 ;;
  *) ping fail "Oracle's API answered $code"; exit 0 ;;
esac
echo "watchdog: VM state $state"

case $state in
  RUNNING)
    if curl -fsS -m 15 --retry 2 "$PODIUM_URL/healthz" 2>/dev/null | grep -q '"status":"ok"'; then
      ping "VM running, site answers"
    else
      ping fail "VM running but the site's /healthz does not answer ok"
    fi
    ;;
  STOPPED)
    out=$(oci POST "$path?action=START") || out=$'\n000'
    code=${out##*$'\n'}
    if [[ $code == 200 ]]; then
      notify "Oracle had stopped the Podium VM. The watchdog started it again; the site should answer within a few minutes."
      ping fail "VM was stopped; started it (the next run checks the site)"
    else
      notify "Oracle stopped the Podium VM and the watchdog could not start it (API answered $code). Start it in the Oracle console."
      ping fail "VM is stopped and the start request failed ($code)"
    fi
    ;;
  STARTING|STOPPING|PROVISIONING|MOVING|CREATING_IMAGE)
    ping "VM is $state; checking again next run"
    ;;
  TERMINATED|TERMINATING|GONE)
    notify "The Podium VM is gone ($state). Rebuild it from the backups: docs/OPERATIONS.md, Oracle Always Free, Rebuild."
    ping fail "VM is $state: rebuild from backups"
    ;;
  *)
    ping fail "VM state '$state' is not one the watchdog knows"
    ;;
esac
