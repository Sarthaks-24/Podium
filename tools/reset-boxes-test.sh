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

# The sizing comes from "podium-judge size" and the caps are set with
# systemctl. Both are stand-ins that record what they were asked.
mkdir -p "$tmp/bin"
cat > "$tmp/judge-size" <<'STUB'
#!/usr/bin/env bash
[[ ${1:-} == size ]] || exit 9
if [[ -n ${STUB_SIZE_FAIL:-} ]]; then echo "PODIUM_JUDGE_LANES=9 is more than this machine's 2 vCPUs" >&2; exit 1; fi
if [[ -n ${STUB_SIZE_TEXT:-} ]]; then printf '%s\n' "$STUB_SIZE_TEXT"; exit 0; fi
printf 'lanes=%s\nsandbox_cap_mb=%s\nunit_cap_mb=%s\n' "${PODIUM_JUDGE_LANES:-2}" "${STUB_CAP:-1100}" "${STUB_UNIT:-256}"
STUB
cat > "$tmp/bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$STUB_SYSTEMCTL_LOG"
[[ -z ${STUB_SYSTEMCTL_FAIL:-} ]]
STUB
chmod +x "$tmp/judge-size" "$tmp/bin/systemctl"
export PODIUM_JUDGE_BIN="$tmp/judge-size" STUB_SYSTEMCTL_LOG="$tmp/systemctl.log"
export PATH="$tmp/bin:$PATH"

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

# The pre-start step sets the sandbox slice and the judge unit for this boot.
: > "$tmp/systemctl.log"
mkdir -p "$root"/{20,21,22,23}
STUB_CAP=3976 STUB_UNIT=384 PODIUM_JUDGE_BOX_BASE=20 PODIUM_JUDGE_LANES=3 "$tmp/reset-boxes"
want_calls=$'set-property --runtime isolate.slice MemoryMax=3976M\nset-property --runtime podium-judge.service MemoryMax=384M'
if [[ ! -e $root/20 && ! -e $root/22 && -d $root/23 && $(cat "$tmp/systemctl.log") == "$want_calls" ]]; then
  pass "boxes are reset for the sized lane count and both caps are set for this boot"
else
  fail "sized reset: boxes or caps wrong: $(cat "$tmp/systemctl.log")"
fi

: > "$tmp/systemctl.log"
mkdir -p "$root/30"; printf keep > "$root/30/marker"
for stub in "STUB_SIZE_FAIL=1" "STUB_SIZE_TEXT=lanes=x" "STUB_SIZE_TEXT=cap=5" "STUB_SIZE_TEXT=lanes=2;rm" \
  "STUB_CAP=0" "STUB_CAP=1099" "STUB_CAP=1048653" "STUB_UNIT=0" "STUB_UNIT=127" "STUB_UNIT=2049" "STUB_UNIT=999999999"; do
  if env "$stub" PODIUM_JUDGE_BOX_BASE=30 PODIUM_JUDGE_LANES=2 "$tmp/reset-boxes" >/dev/null 2>&1; then
    fail "a refused or unreadable size ($stub) was accepted"
  elif [[ -f $root/30/marker && ! -s $tmp/systemctl.log ]]; then
    pass "a refused or unreadable size ($stub) deletes no box and sets no cap"
  else
    fail "size $stub: a box was deleted or a cap set"
  fi
done

if STUB_SYSTEMCTL_FAIL=1 PODIUM_JUDGE_BOX_BASE=30 PODIUM_JUDGE_LANES=2 "$tmp/reset-boxes" >/dev/null 2>&1; then
  fail "a failed systemctl set-property was ignored"
else
  pass "a failed systemctl set-property fails the pre-start step"
fi

# lanes outside 1 to 16 are refused whatever the other numbers say.
mkdir -p "$root/30"; printf keep > "$root/30/marker"; : > "$tmp/systemctl.log"
for lanes in 0 17 99; do
  if PODIUM_JUDGE_LANES=$lanes PODIUM_JUDGE_BOX_BASE=30 "$tmp/reset-boxes" >/dev/null 2>&1 || [[ ! -f $root/30/marker || -s $tmp/systemctl.log ]]; then
    fail "a lane count of $lanes was accepted or touched a box"
  else
    pass "a lane count of $lanes is refused without touching a box or a cap"
  fi
done

$1 in shell: the default lane count is gone.
if grep -Eq 'PODIUM_JUDGE_LANES:-' "$source_script"; then
  fail "reset-boxes still carries its own default lane count"
else
  pass "reset-boxes has no sizing of its own"
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
