#!/usr/bin/env bash
# Fails when the web app's own source carries placeholder copy a student
# could see: "lorem ipsum", "TODO", "FIXME", or the word "placeholder" as
# text. Run by make release (web-quality item 32). It reads the app's
# source, not web/dist: the built chunks include vendor code (CodeMirror,
# KaTeX) whose own TODO comments are no concern of ours, and minified
# chunks cannot tell app copy from library code.
#
# Not flagged: the placeholder="..." attribute and ::placeholder in CSS
# (hint text in an input is wanted), comment lines, and tests.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../web"

hits=$(grep -rnIiE 'lorem ipsum|\bTODO\b|\bFIXME\b|\bplaceholder\b' src index.html public 2>/dev/null \
  | grep -vE '\.test\.ts:|\.spec\.ts:' \
  | grep -vE ':[0-9]+:\s*(//|/?\*|<!--)' \
  | grep -viE '\bplaceholder(=|\{)|::placeholder|placeholder:|\.placeholder\b' || true)
if [[ -n $hits ]]; then
  echo "check-placeholders: placeholder text in the web app:" >&2
  echo "$hits" >&2
  exit 1
fi
echo "check-placeholders: no placeholder text in web/src"
