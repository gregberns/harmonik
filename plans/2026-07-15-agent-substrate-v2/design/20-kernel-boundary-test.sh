#!/bin/bash
# The kernel may not NAME a domain concept.
# Checks DECLARED IDENTIFIERS only (so protobuf's own keywords can't false-hit),
# and matches SUBSTRINGS case-insensitively (so CamelCase "LogAppend" is caught --
# an earlier \blog\b version passed that file, which is why this note exists).
BANNED='agent|session|repo|log|tail|transcript|project|tmux|ssh|git|claude|codex|harmonik|prompt|model|token|human|note|search|task|worktree|commit|branch|file|path'
ALLOW='^(catalog|logic|dialog)$'
fail=0
for f in "$@"; do
  ids=$(sed 's://.*::' "$f" \
        | grep -oE '^[[:space:]]*(message|enum|service)[[:space:]]+[A-Za-z0-9_]+|^[[:space:]]*rpc[[:space:]]+[A-Za-z0-9_]+|^[[:space:]]*(repeated[[:space:]]+|optional[[:space:]]+)?[A-Za-z0-9_.<>, ]+[[:space:]]+[a-z_][a-z0-9_]*[[:space:]]*=[[:space:]]*[0-9]+' \
        | sed -E 's/^[[:space:]]*(message|enum|service|rpc)[[:space:]]+//; s/[[:space:]]*=[[:space:]]*[0-9]+//; s/.*[[:space:]]([a-z_][a-z0-9_]*)$/\1/' \
        | sed -E 's/^[[:space:]]+//')
  while read -r id; do
    [ -z "$id" ] && continue
    echo "$id" | grep -qiE "$ALLOW" && continue
    if echo "$id" | grep -qiE "$BANNED"; then echo "   BANNED IDENTIFIER in $(basename $f): $id"; fail=1; fi
  done <<< "$ids"
done
[ $fail -eq 0 ] && echo "VOCABULARY CLEAN -- the kernel names no domain concept." || echo ">>> BOUNDARY VIOLATION"
exit $fail
