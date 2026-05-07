#!/usr/bin/env bash
# scripts/validate-commit-msg.sh — commit-msg hook called by lefthook.
#
# Usage: validate-commit-msg.sh <commit-msg-file>
#
# Validates:
#   1. Subject line matches Conventional Commits format.
#   2. Subject length ≤72 characters.
#   3. Non-trivial commits carry Reviewed-By: and Review-Verdict: trailers.
#   4. Review-Verdict: value is well-formed JSON with a "verdict" field.
#
# Trivial-commit bypass: add the trailer `Trivial: true` to the commit
# message to skip the Reviewed-By / Review-Verdict requirement.
# Use ONLY for typos, whitespace fixes, and obvious one-liners — the
# agent-reviewer is still required for all other commits per build-practices.md.
#
# Exit 0 = OK; exit 1 = validation failure (blocks commit).

set -euo pipefail

MSG_FILE="${1:-}"
if [[ -z "$MSG_FILE" || ! -f "$MSG_FILE" ]]; then
  echo "validate-commit-msg: error: no commit-message file provided or file not found" >&2
  exit 1
fi

MSG="$(cat "$MSG_FILE")"

# ── 1. Strip comment lines (lines starting with #) ───────────────────────────
STRIPPED="$(grep -v '^#' "$MSG_FILE" || true)"

# ── 2. Extract subject line (first non-blank line) ───────────────────────────
SUBJECT="$(echo "$STRIPPED" | awk 'NF{print;exit}')"

# ── 3. Conventional Commits subject validation ────────────────────────────────
# Pattern: <type>[(<scope>)][!]: <description>
# Allowed types (closed set per build-practices.md §Commit conventions):
#   feat fix refactor test docs chore spec build perf
CC_PATTERN='^(feat|fix|refactor|test|docs|chore|spec|build|perf)(\([^)]+\))?(!)?: .+'
if ! echo "$SUBJECT" | grep -qE "$CC_PATTERN"; then
  echo "validate-commit-msg: subject does not match Conventional Commits format." >&2
  echo "  Expected: <type>[(<scope>)][!]: <description>" >&2
  echo "  Allowed types: feat fix refactor test docs chore spec build perf" >&2
  echo "  Got: $SUBJECT" >&2
  exit 1
fi

# ── 4. Subject length ─────────────────────────────────────────────────────────
SUBJECT_LEN="${#SUBJECT}"
if (( SUBJECT_LEN > 72 )); then
  echo "validate-commit-msg: subject line is ${SUBJECT_LEN} chars; max is 72." >&2
  echo "  Got: $SUBJECT" >&2
  exit 1
fi

# ── 5. Trailing-period check ──────────────────────────────────────────────────
if echo "$SUBJECT" | grep -qE '\.$'; then
  echo "validate-commit-msg: subject must not end with a period." >&2
  exit 1
fi

# ── 6. Trivial-bypass detection ───────────────────────────────────────────────
# If the message contains `Trivial: true` anywhere in the trailer block,
# skip the Reviewed-By / Review-Verdict requirement.
IS_TRIVIAL=false
if echo "$STRIPPED" | grep -qE '^Trivial: true$'; then
  IS_TRIVIAL=true
fi

# ── 7. Merge / fixup commit bypass ───────────────────────────────────────────
# Merge commits and fixup!/squash! commits skip trailer validation.
if echo "$SUBJECT" | grep -qE '^(Merge|fixup!|squash!) '; then
  IS_TRIVIAL=true
fi

# ── 8. Reviewed-By + Review-Verdict trailer validation ───────────────────────
if [[ "$IS_TRIVIAL" == "false" ]]; then
  REVIEWED_BY="$(echo "$STRIPPED" | grep -E '^Reviewed-By:' || true)"
  REVIEW_VERDICT_LINE="$(echo "$STRIPPED" | grep -E '^Review-Verdict:' || true)"

  if [[ -z "$REVIEWED_BY" ]]; then
    echo "validate-commit-msg: missing required trailer 'Reviewed-By:' on a non-trivial commit." >&2
    echo "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes." >&2
    exit 1
  fi

  if [[ -z "$REVIEW_VERDICT_LINE" ]]; then
    echo "validate-commit-msg: missing required trailer 'Review-Verdict:' on a non-trivial commit." >&2
    echo "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes." >&2
    exit 1
  fi

  # ── 9. JSON well-formedness + required "verdict" field ───────────────────
  VERDICT_JSON="${REVIEW_VERDICT_LINE#Review-Verdict: }"
  VERDICT_JSON="${VERDICT_JSON#Review-Verdict:}"  # handle no-space variant

  if ! command -v jq &>/dev/null; then
    # jq absent: fall back to Python (present in macOS + most Linux envs).
    VERDICT_FIELD="$(echo "$VERDICT_JSON" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    print(d.get('verdict', ''))
except Exception as e:
    print('PARSE_ERROR: ' + str(e), file=sys.stderr)
    sys.exit(1)
" 2>&1)"
    PY_EXIT=$?
    if (( PY_EXIT != 0 )); then
      echo "validate-commit-msg: Review-Verdict trailer is not valid JSON." >&2
      echo "  Parse error: $VERDICT_FIELD" >&2
      echo "  Got: $VERDICT_JSON" >&2
      exit 1
    fi
  else
    # jq path
    if ! VERDICT_FIELD="$(echo "$VERDICT_JSON" | jq -r '.verdict' 2>/dev/null)"; then
      echo "validate-commit-msg: Review-Verdict trailer is not valid JSON." >&2
      echo "  Got: $VERDICT_JSON" >&2
      exit 1
    fi
  fi

  if [[ -z "$VERDICT_FIELD" || "$VERDICT_FIELD" == "null" ]]; then
    echo "validate-commit-msg: Review-Verdict JSON is missing the 'verdict' field." >&2
    echo "  Got: $VERDICT_JSON" >&2
    exit 1
  fi

  # ── 10. verdict enum check ────────────────────────────────────────────────
  case "$VERDICT_FIELD" in
    APPROVE|REQUEST_CHANGES)
      # Both are allowed to land per build-practices.md §Agent review.
      ;;
    BLOCK)
      echo "validate-commit-msg: BLOCK verdict must not be committed (fix first)." >&2
      echo "  Review-Verdict: $VERDICT_JSON" >&2
      exit 1
      ;;
    *)
      echo "validate-commit-msg: unknown verdict value '$VERDICT_FIELD'." >&2
      echo "  Allowed: APPROVE, REQUEST_CHANGES (BLOCK = fix before committing)." >&2
      exit 1
      ;;
  esac
fi

exit 0
