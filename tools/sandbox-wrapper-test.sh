#!/usr/bin/env bash
# Checks tools/sandbox-test.sh with mocked id, install, chown and su, so it
# runs without root: the -examples folder must be copied into the wrapper's
# private work folder and handed to the judge user from there, because the
# original may sit under a parent the judge user cannot enter.
set -euo pipefail

repo=$(cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
fail() { echo "$1" >&2; exit 1; }
mkdir -p "$tmp/bin" "$tmp/private/problems/sum"
echo 'statement' > "$tmp/private/problems/sum/statement.md"

cat > "$tmp/bin/id" <<'MOCK'
#!/usr/bin/env bash
[[ ${1:-} == -u ]] && echo 0
exit 0
MOCK
cat > "$tmp/bin/install" <<'MOCK'
#!/usr/bin/env bash
cp "${@: -2:1}" "${@: -1}"
MOCK
cat > "$tmp/bin/chown" <<'MOCK'
#!/usr/bin/env bash
echo "$*" >> "$CHOWN_LOG"
MOCK
# su -s SHELL USER -c CMD: run CMD here, as the test user.
cat > "$tmp/bin/su" <<'MOCK'
#!/usr/bin/env bash
bash -c "$5"
MOCK
# The "binary": records its arguments and the examples it can read.
cat > "$tmp/run-bin" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$ARGS_LOG"
for a in "$@"; do
  a=${a#-examples=}
  [[ -d $a && $a == /* ]] && cp -R "$a" "$SEEN" || true
done
MOCK
chmod +x "$tmp"/bin/* "$tmp/run-bin"
export PATH="$tmp/bin:$PATH" CHOWN_LOG="$tmp/chown.log" ARGS_LOG="$tmp/args.log" SEEN="$tmp/seen"

for form in separate equals; do
  rm -rf "$SEEN" "$CHOWN_LOG" "$ARGS_LOG"
  if [[ $form == separate ]]; then
    set -- -examples "$tmp/private/problems"
  else
    set -- "-examples=$tmp/private/problems"
  fi
  bash "$repo/tools/sandbox-test.sh" "$tmp/run-bin" -test.v "$@" -test.timeout 1m
  grep -qx -- '-test.v' "$ARGS_LOG" && grep -qx -- '-test.timeout' "$ARGS_LOG" || fail "$form: other arguments were lost"
  ex=$(grep -E 'examples' "$ARGS_LOG" | tail -n1)
  ex=${ex#-examples=}
  if [[ $form == separate ]]; then ex=$(grep -A1 -x -- '-examples' "$ARGS_LOG" | tail -n1); fi
  [[ $ex != "$tmp/private/problems" && $ex == /tmp/podium-sandbox.*/* ]] || fail "$form: the binary got '$ex', not a copy in the work folder"
  [[ -f $SEEN/sum/statement.md ]] || fail "$form: the copy did not hold the problem files"
  grep -q -- "-R judge:judge ${ex}\$" "$CHOWN_LOG" || fail "$form: the copy was not chowned to judge: $(cat "$CHOWN_LOG")"
  [[ ! -e $ex ]] || fail "$form: the work folder was not removed"
done
[[ -f $tmp/private/problems/sum/statement.md ]] || fail 'the original was touched'

# Without -examples nothing is copied and arguments pass through unchanged.
rm -f "$CHOWN_LOG" "$ARGS_LOG"
bash "$repo/tools/sandbox-test.sh" "$tmp/run-bin" calibrate -reps 5
[[ $(tr '\n' ' ' < "$ARGS_LOG") == 'calibrate -reps 5 ' ]] || fail 'arguments changed without -examples'
! grep -q -- '-R' "$CHOWN_LOG" || fail 'a copy was made without -examples'
echo 'sandbox-test.sh checks passed'
