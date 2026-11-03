#!/usr/bin/env bash
# Exercise the box reset helper against a temporary box root.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_script="$here/../deploy/reset-boxes"
fails=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

if [[ ! -f $source_script ]]; then
  fail "deploy/reset-boxes does not exist"
  exit 1
fi

root="$tmp/isolate"
mkdir -p "$root"/{9,10,11,12,13}
for id in 9 10 11 12 13; do printf '%s\n' "$id" > "$root/$id/marker"; done
sed "s#/var/lib/isolate#$root#g" "$source_script" > "$tmp/reset-boxes"
chmod +x "$tmp/reset-boxes"

PODIUM_JUDGE_BOX_BASE=10 PODIUM_JUDGE_LANES=3 "$tmp/reset-boxes"
if [[ ! -e $root/10 && ! -e $root/11 && ! -e $root/12 && -f $root/9/marker && -f $root/13/marker ]]; then
  pass "only the configured lane boxes are reset"
else
  fail "reset removed a neighboring box or left a lane box behind"
fi

for value in ../../../tmp 985; do
  mkdir -p "$root/10"
  printf 'keep\n' > "$root/10/marker"
  if PODIUM_JUDGE_BOX_BASE="$value" PODIUM_JUDGE_LANES=2 "$tmp/reset-boxes" >/dev/null 2>&1; then
    fail "invalid box base $value was accepted"
  elif [[ -f $root/10/marker ]]; then
    pass "invalid box base $value is refused without deleting a box"
  else
    fail "invalid box base $value deleted a box"
  fi
done

mkdir -p "$root/80"
printf 'keep\n' > "$root/80/marker"
if PODIUM_JUDGE_BOX_BASE=79 PODIUM_JUDGE_LANES=2 "$tmp/reset-boxes" >/dev/null 2>&1; then
  fail "a lane range containing the selftest box was accepted"
elif [[ -f $root/80/marker ]]; then
  pass "a lane range containing the selftest box is refused"
else
  fail "validation removed the selftest box"
fi

if grep -q 'rm -rf --one-file-system --' "$source_script"; then
  pass "the reset uses one-filesystem recursive removal"
else
  fail "the reset does not use rm --one-file-system"
fi
if grep -Fq 'ExecStartPre=+/usr/local/lib/podium/reset-boxes' "$here/../deploy/systemd/podium-judge.service"; then
  pass "systemd runs the reset as root before the worker"
else
  fail "podium-judge.service does not run the privileged reset"
fi
if grep -Fq 'install -m 755 "$HERE/reset-boxes" "$LIB/reset-boxes"' "$here/../deploy/install.sh"; then
  pass "install.sh installs the reset helper"
else
  fail "install.sh does not install the reset helper"
fi

if (( fails > 0 )); then echo "$fails reset-boxes check(s) failed"; exit 1; fi
echo "reset-boxes checks passed"
