#!/usr/bin/env bash
# Fails when the staged diff contains a real env file, a private key, or a
# string shaped like a known credential. Run by `make check-secrets` before
# every commit; install as a pre-commit hook with `make hooks`.
set -euo pipefail

status=0

# Real env files and key material must never be staged.
while IFS= read -r f; do
  case "$f" in
    *.env.example) ;;
    .env|*/.env|*.env|.env.*|*/.env.*|*.pem|*.key|*server.env|*judge.env)
      echo "check-secrets: staged file looks like a secret: $f" >&2
      status=1 ;;
  esac
done < <(git diff --cached --name-only --diff-filter=ACMR)

# label|extended regular expression
patterns=(
  'AWS access key id|AKIA[0-9A-Z]{16}'
  'Anthropic API key|sk-ant-[A-Za-z0-9_-]{20,}'
  'OpenAI-style API key|sk-(proj-)?[A-Za-z0-9_-]{32,}'
  'Google OAuth client secret|GOCSPX-[A-Za-z0-9_-]{20,}'
  'Google API key|AIza[0-9A-Za-z_-]{35}'
  'GitHub token|gh[pousr]_[A-Za-z0-9]{36}'
  'Slack token|xox[baprs]-[A-Za-z0-9-]{10,}'
  'private key|-----BEGIN ([A-Z]+ )?PRIVATE KEY-----'
  # Bounded so it does not match inside base64 hashes (package-lock.json).
  'Backblaze B2 application key|(^|[^A-Za-z0-9/+])K[0-9]{3}[A-Za-z0-9/+]{27}([^A-Za-z0-9/+=]|$)'
)
# Added lines only, including ones whose content starts with "+".
added=$(git diff --cached -U0 --no-color | grep '^+' | grep -v '^+++ ' || true)
for entry in "${patterns[@]}"; do
  label=${entry%%|*}
  if grep -Eq -- "${entry#*|}" <<<"$added"; then
    echo "check-secrets: staged change looks like a $label" >&2
    status=1
  fi
done

if [[ $status -eq 0 ]]; then
  echo "check-secrets: no secrets found in the staged diff"
fi
exit $status
