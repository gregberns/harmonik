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

# The match is a here-string, NOT a pipe, and the difference is the whole
# usefulness of this scan. `grep -q` leaves the moment it matches; the writer
# feeding it then takes SIGPIPE; and under `pipefail` — set at the top of this
# file — the pipeline reports the WRITER's death as its own status. So
# `echo "$STAGED_ADDED" | grep -qE …` returned non-zero on the commits that DID
# contain a secret, and this scan read that as "no match" and let them through.
# Measured: a 40-byte staged diff carrying a fake key was blocked, and the same
# key inside a 527 KB staged diff — an ordinary commit — passed, five times out
# of five. A here-string has no writer process to kill.
#
# Do not read a size out of this as a safe threshold. It is a race between the
# writer and the reader, and the same command on the same input was measured
# giving different answers at 24 KB and at 32 KB in the same session. Small
# does not mean safe; it means the race is usually won.
# `-e` is load-bearing, not tidiness. The private-key pattern starts with five
# dashes, so without `-e` grep reads it as a bundle of options, prints
# "unrecognized option", and exits 2 — and `if` reads 2 as no-match. That
# pattern therefore matched nothing for the whole life of this script, and the
# `2>/dev/null` that used to sit on this line hid the message that said so. A
# staged RSA private key went straight through at any size.
#
# The three exit statuses are told apart for the same reason. grep says 0 for a
# match, 1 for none, and 2 or more for "I could not do the job" — a bad regex,
# unreadable input, a missing binary. Folding 2 into "no secret found" is how
# the defect above stayed invisible, so an error blocks the commit and says
# what it was.
for pattern in "${SECRET_PATTERNS[@]}"; do
    grep_err=$(grep -qE -e "$pattern" <<<"$STAGED_ADDED" 2>&1)
    case "$?" in
        0)
            echo "secret-scan: BLOCKED — potential secret matches pattern: ${pattern}"
            echo "  Run 'git diff --cached' to inspect staged content."
            found=1
            ;;
        1) ;;
        *)
            echo "secret-scan: BLOCKED — the scan could not run for pattern: ${pattern}"
            echo "  grep said: ${grep_err}"
            echo "  This is not a clean result. Fix the scan before committing."
            found=1
            ;;
    esac
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
