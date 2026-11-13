#!/usr/bin/env bash
# Runs a cross-compiled sandbox test or calibrate binary as the unprivileged
# judge user, the way the real worker runs. Used by `make sandbox-test` and
# `make calibrate`, either directly on Linux (as root) or inside WSL2.
#
#   tools/sandbox-test.sh BINARY [ARGS...]
set -euo pipefail

bin=$1; shift
[[ $(id -u) -eq 0 ]] || { echo "sandbox-test.sh: run as root (it drops to the judge user)" >&2; exit 1; }
id judge >/dev/null 2>&1 || { echo "sandbox-test.sh: user judge missing; run deploy/install.sh users" >&2; exit 1; }

# PODIUM_SANDBOX_CAP_MB=N: run under the sandbox cap a bigger machine gets.
# The cap of isolate.slice is the host's, so this root wrapper sets it for
# the run (systemctl set-property --runtime, what the judge's pre-start step
# does) and puts the old value back on exit.
cap_mb=${PODIUM_SANDBOX_CAP_MB:-}
if [[ -n $cap_mb ]]; then
  [[ $cap_mb =~ ^[0-9]{3,6}$ ]] || { echo "sandbox-test.sh: PODIUM_SANDBOX_CAP_MB must be a whole number of MB" >&2; exit 1; }
  slice_max=/sys/fs/cgroup/isolate.slice/memory.max
  was=$(cat "$slice_max")
  [[ $was == max ]] && was=infinity
  systemctl set-property --runtime isolate.slice "MemoryMax=${cap_mb}M"
  echo "sandbox-test.sh: isolate.slice cap is $(( $(cat "$slice_max") >> 20 )) MB for this run (was $was bytes)"
fi
work=$(mktemp -d /tmp/podium-sandbox.XXXXXX)
trap 'rm -rf "$work"; [[ -z $cap_mb ]] || systemctl set-property --runtime isolate.slice "MemoryMax=$was"' EXIT
install -m 755 -o judge "$bin" "$work/run"
chown judge:judge "$work"
# The problem packs may sit under a parent the judge user cannot enter (a
# private home folder): hand it a copy from the work folder instead.
args=()
while (( $# )); do
  case $1 in
    -examples|--examples) args+=("$1" "$work/examples"); cp -RH -- "${2:?-examples needs a folder}" "$work/examples"; shift ;;
    -examples=*|--examples=*) args+=("${1%%=*}=$work/examples"); cp -RH -- "${1#*=}" "$work/examples" ;;
    *) args+=("$1") ;;
  esac
  shift
done
set -- ${args[@]+"${args[@]}"}
[[ ! -e $work/examples ]] || chown -R judge:judge "$work/examples"
cd "$work"
# TMPDIR inside $work keeps scratch files readable by judge only.
su -s /bin/bash judge -c "TMPDIR='$work' ./run $(printf '%q ' "$@")"
