#!/usr/bin/env bash
# Pre-commit secret scan: blocks commits that add credential patterns or .env files.
# Scans staged diff lines (lines beginning with '+') only — ignores unchanged context.
set -uo pipefail

# Patterns that match known secret formats.
# Each entry is an ERE pattern applied to the added lines in the staged diff.
SECRET_PATTERNS=(
    'ANTHROPIC_API_KEY[[:space:]]*=[[:space:]]*[A-Za-z0-9_-]{10,}'
    'sk-ant-api[0-9]+-[A-Za-z0-9_-]{20,}'
    'sk-ant-[A-Za-z0-9_-]{30,}'
    'AWS_SECRET_ACCESS_KEY[[:space:]]*=[[:space:]]*[A-Za-z0-9/+]{20,}'
    'GITHUB_TOKEN[[:space:]]*=[[:space:]]*(gh[ps]_|github_pat_)[A-Za-z0-9_]'
    'OPENAI_API_KEY[[:space:]]*=[[:space:]]*sk-[A-Za-z0-9_-]{20,}'
    '-----BEGIN (RSA|EC|DSA|OPENSSH) PRIVATE KEY-----'
)

found=0

# Extract added lines from the staged diff (skip the diff header lines).
# Use process substitution to avoid pipefail on empty diffs.
STAGED_ADDED=$(git diff --cached --diff-filter=ACM 2>/dev/null \
    | grep '^+' 2>/dev/null \
    | grep -v '^+++' 2>/dev/null \
    || true)

for pattern in "${SECRET_PATTERNS[@]}"; do
    if echo "$STAGED_ADDED" | grep -qE "$pattern" 2>/dev/null; then
        echo "secret-scan: BLOCKED — potential secret matches pattern: ${pattern}"
        echo "  Run 'git diff --cached' to inspect staged content."
        found=1
    fi
done

# Block if any .env-family file is staged for addition or modification.
STAGED_ENV_FILES=$(git diff --cached --name-only --diff-filter=ACM 2>/dev/null \
    | grep -E '(^|\/)\.env($|\.|\-)' \
    || true)
if [ -n "$STAGED_ENV_FILES" ]; then
    echo "secret-scan: BLOCKED — .env file(s) staged for commit:"
    echo "$STAGED_ENV_FILES" | sed 's/^/  /'
    echo "  Env files may contain secrets. Add them to .gitignore."
    found=1
fi

if [ "$found" -eq 1 ]; then
    exit 1
fi
