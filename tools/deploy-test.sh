#!/usr/bin/env bash
# Checks for tools/deploy.sh that need no VM: the script runs from a copy in
# a temporary folder with git, make, ssh and scp replaced by stubs on PATH
# that record how they were called; plus release-manifest.sh's refusal of
# the placeholder UPGRADES_FROM. Runs in Git Bash and on Linux; `make
# check` calls it.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fails=0
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*"; fails=$((fails + 1)); }
pass() { echo "ok:   $*"; }

mkdir -p "$tmp/repo/tools" "$tmp/stub"
cp "$here/deploy.sh" "$tmp/repo/tools/deploy.sh"
log=$tmp/calls

# git: FAKE_TAG is the exact tag of HEAD (none when empty); FAKE_DIRTY=1
# makes git diff report changes.
cat > "$tmp/stub/git" <<'EOF'
#!/usr/bin/env bash
case $1 in
  describe) [[ -n ${FAKE_TAG:-} ]] && { echo "$FAKE_TAG"; exit 0; }; echo "fatal: no tag exactly matches" >&2; exit 128 ;;
  diff) exit "${FAKE_DIRTY:-0}" ;;
  *) exit 0 ;;
esac
EOF
# make: records its arguments and writes the tarball the VERSION names.
cat > "$tmp/stub/make" <<'EOF'
#!/usr/bin/env bash
echo "make $*" >> "$CALLS"
for a in "$@"; do
  if [[ $a == VERSION=* ]]; then mkdir -p release && echo tarball > "release/podium-${a#VERSION=}.tar.gz"; fi
done
EOF
# ssh: the guard answers FAKE_GUARD; mktemp answers a folder name.
cat > "$tmp/stub/ssh" <<'EOF'
#!/usr/bin/env bash
echo "ssh $*" >> "$CALLS"
case "$*" in
  *deploy-check*) exit "${FAKE_GUARD:-0}" ;;
  *mktemp*) echo /tmp/podium-deploy.abc123 ;;
esac
EOF
cat > "$tmp/stub/scp" <<'EOF'
#!/usr/bin/env bash
echo "scp $*" >> "$CALLS"
EOF
chmod +x "$tmp/stub/"*

# run ANSWER [ENV=VALUE...] -- [ARGS...]: deploy.sh with the stubs, the
# host answer on stdin; prints its output, returns its exit code.
run() {
  local answer=$1; shift
  local envs=()
  while [[ $1 != -- ]]; do envs+=("$1"); shift; done
  shift
  rm -f "$log" && : > "$log"
  rm -rf "$tmp/repo/release"
  (
    cd "$tmp"
    printf '%s\n' "$answer" | env -u VERSION PATH="$tmp/stub:$PATH" CALLS="$log" "${envs[@]}" \
      bash "$tmp/repo/tools/deploy.sh" "$@" 2>&1
  )
}

if out=$(run vm -- vm); then
  fail "an untagged commit without VERSION went on"
elif [[ $out == *"not a vX.Y.Z tag"*"VERSION=vX.Y.Z tools/deploy.sh"* && ! -s $log ]]; then
  pass "an untagged commit without VERSION stops before the guard and the build"
else
  fail "untagged commit: $out / $(cat "$log")"
fi

if out=$(run vm VERSION=v1.4.90 -- vm); then
  if grep -qx 'make release VERSION=v1.4.90' "$log" \
    && grep -q 'scp release/podium-v1.4.90.tar.gz vm:/tmp/podium-deploy.abc123/' "$log" \
    && grep -q 'tar xzf podium-v1.4.90.tar.gz; cd podium-v1.4.90; sudo ./install.sh upgrade$' "$log"; then
    pass "VERSION names the build, the tarball and the remote folder"
  else
    fail "VERSION=v1.4.90 calls: $(cat "$log")"
  fi
else
  fail "VERSION=v1.4.90 stopped: $out"
fi

if out=$(run vm FAKE_TAG=v1.5.0 -- vm) && grep -qx 'make release VERSION=v1.5.0' "$log"; then
  pass "a tagged commit builds the tag's version"
else
  fail "tagged commit: $out / $(cat "$log")"
fi

if out=$(run vm VERSION=1.4.90 -- vm); then
  fail "a VERSION without the v went on"
elif [[ $out == *"not a vX.Y.Z tag (1.4.90)"* && ! -s $log ]]; then
  pass "a malformed VERSION stops before anything runs"
else
  fail "malformed VERSION: $out"
fi

if out=$(run vm FAKE_TAG=v1.5.0 FAKE_DIRTY=1 -- vm); then
  fail "a tree with uncommitted changes went on"
elif [[ $out == *"uncommitted changes"* && ! -s $log ]]; then
  pass "uncommitted changes stop before anything runs"
else
  fail "dirty tree: $out"
fi

if out=$(run vm FAKE_TAG=v1.5.0 FAKE_GUARD=1 -- vm); then
  fail "a refusing guard was ignored"
elif [[ $out == *"guard refused"* ]] && ! grep -q '^make' "$log"; then
  pass "a refusing guard stops before the build"
else
  fail "guard refusal: $out / $(cat "$log")"
fi

if out=$(run vm FAKE_TAG=v1.5.0 FAKE_GUARD=1 -- vm --force "Rehearsal of the emergency override") \
  && grep -q "sudo ./install.sh upgrade --force Rehearsal\\\\ of\\\\ the" "$log"; then
  pass "--force goes on past a refusing guard and passes the reason quoted"
else
  fail "--force: $out / $(cat "$log")"
fi

if out=$(run other FAKE_TAG=v1.5.0 -- vm); then
  fail "a wrong host answer went on"
elif [[ $out == *"nothing left this machine"* ]] && ! grep -q '^scp' "$log"; then
  pass "a wrong host answer stops before the copy"
else
  fail "wrong host answer: $out / $(cat "$log")"
fi

if out=$(run vm -- vm --force short); then
  fail "a short --force reason went on"
elif [[ $out == *"at least 10 characters"* && ! -s $log ]]; then
  pass "a short --force reason is refused"
else
  fail "short reason: $out"
fi

# release-manifest.sh refuses a real release that still upgrades from the
# placeholder v1.0.0 (C2-040), before it looks at the folder; a drill
# build below v1.5.0 may keep it.
manifest() { bash "$here/release-manifest.sh" "$tmp/no-such-release" "$1" abc now "$2" 2>&1; }
if out=$(manifest v2.0.0 v1.0.0); then
  fail "a v2.0.0 release upgrading from the placeholder went on"
elif [[ $out == *"placeholder v1.0.0"* ]]; then
  pass "release-manifest refuses v2.0.0 with UPGRADES_FROM v1.0.0"
else
  fail "release-manifest placeholder: $out"
fi
for pair in "v1.4.90 v1.0.0" "v2.0.0 v2.0.0"; do
  # shellcheck disable=SC2086
  out=$(manifest $pair || true)
  if [[ $out == *"has no bin/"* ]]; then pass "release-manifest accepts version and upgrades_from $pair"; else fail "release-manifest $pair: $out"; fi
done

if (( fails > 0 )); then echo "$fails deploy.sh check(s) failed"; exit 1; fi
echo "deploy.sh checks passed"
