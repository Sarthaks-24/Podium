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

work=$(mktemp -d /tmp/podium-sandbox.XXXXXX)
trap 'rm -rf "$work"' EXIT
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
