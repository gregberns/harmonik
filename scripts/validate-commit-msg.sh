#!/usr/bin/env bash
# scripts/validate-commit-msg.sh — commit-msg hook called by lefthook.
#
# Usage: validate-commit-msg.sh <commit-msg-file>
#
# Validates:
#   1. Subject line matches Conventional Commits format (closed type set).
#   2. Subject length ≤72 characters and no trailing period.
#   3. Non-trivial commits carry Reviewed-By: and Review-Verdict: trailers.
#   4. Review-Verdict: value is well-formed JSON matching agent-reviewer
#      schema v1: schema_version=1, verdict ∈ {APPROVE, REQUEST_CHANGES, BLOCK}
#      or config-reviewer schema v1: verdict ∈ {CLEAN, DRIFT_MINOR, DRIFT_MAJOR}.
#
# Trivial-commit bypass: add the trailer `Trivial: true` to the commit
# message to skip the Reviewed-By / Review-Verdict requirement.
# Use ONLY for typos, whitespace fixes, and obvious one-liners — the
# agent-reviewer is still required for all other commits per build-practices.md.
#
# Exit 0 = OK; exit 1 = validation failure (prints line-numbered errors).

set -uo pipefail

ERRORS=()
ERROR_COUNT=0

err() {
  ERROR_COUNT=$(( ERROR_COUNT + 1 ))
  ERRORS+=( "  [${ERROR_COUNT}] $*" )
}

MSG_FILE="${1:-}"
if [[ -z "$MSG_FILE" || ! -f "$MSG_FILE" ]]; then
  echo "validate-commit-msg [1]: no commit-message file provided or file not found" >&2
  exit 1
fi

# ── 1. Strip comment lines (lines starting with #) ───────────────────────────
STRIPPED="$(grep -v '^#' "$MSG_FILE" || true)"

# ── 2. Extract subject line (first non-blank line) ───────────────────────────
SUBJECT="$(printf '%s\n' "$STRIPPED" | awk 'NF{print;exit}')"

# ── 3. Conventional Commits subject validation ────────────────────────────────
# Pattern per bead hk-kv7fe: type[(scope)]: description
# Scope is restricted to lower-case alphanumerics, commas, hyphens.
# Breaking-change suffix (!) allowed per CC spec even if not listed in bead.
# Types (closed set per build-practices.md §Commit conventions + bead spec):
#   feat fix chore docs test refactor build ci perf revert style spec
# Note: bead spec's CC_PATTERN uses [a-z0-9,-]+ for scope; build-practices.md
# also defines `spec` as a valid type (not in standard CC but normative here).
CC_PATTERN='^(feat|fix|chore|docs|test|refactor|build|ci|perf|revert|style|spec)(\([a-z0-9,:-]+\))?(!)?: .+'
if ! printf '%s\n' "$SUBJECT" | grep -qE "$CC_PATTERN"; then
  err "subject does not match Conventional Commits format."
  err "  Expected: <type>[(<scope>)][!]: <description>"
  err "  Allowed types: feat fix chore docs test refactor build ci perf revert style spec"
  err "  Scope (if present) must be lowercase alphanumeric + commas/hyphens/colons."
  err "  Got: $SUBJECT"
fi

# ── 4. Subject length ─────────────────────────────────────────────────────────
SUBJECT_LEN="${#SUBJECT}"
if (( SUBJECT_LEN > 72 )); then
  err "subject line is ${SUBJECT_LEN} chars; max is 72."
  err "  Got: $SUBJECT"
fi

# ── 5. Trailing-period check ──────────────────────────────────────────────────
if printf '%s\n' "$SUBJECT" | grep -qE '\.$'; then
  err "subject must not end with a period."
fi

# ── 6. Trivial-bypass detection ───────────────────────────────────────────────
# If the message contains `Trivial: true` anywhere in the trailer block,
# skip the Reviewed-By / Review-Verdict requirement.
IS_TRIVIAL=false
if printf '%s\n' "$STRIPPED" | grep -qE '^Trivial: true$'; then
  IS_TRIVIAL=true
fi

# ── 7. Merge / fixup commit bypass ───────────────────────────────────────────
# Merge commits and fixup!/squash! commits skip trailer validation.
if printf '%s\n' "$SUBJECT" | grep -qE '^(Merge|fixup!|squash!) '; then
  IS_TRIVIAL=true
fi

# ── 8. Reviewed-By + Review-Verdict trailer validation ───────────────────────
if [[ "$IS_TRIVIAL" == "false" ]]; then
  REVIEWED_BY="$(printf '%s\n' "$STRIPPED" | grep -E '^Reviewed-By:' || true)"
  REVIEW_VERDICT_LINE="$(printf '%s\n' "$STRIPPED" | grep -E '^Review-Verdict:' || true)"

  if [[ -z "$REVIEWED_BY" ]]; then
    err "missing required trailer 'Reviewed-By:' on a non-trivial commit."
    err "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes."
  fi

  if [[ -z "$REVIEW_VERDICT_LINE" ]]; then
    err "missing required trailer 'Review-Verdict:' on a non-trivial commit."
    err "  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes."
  fi

  # Only validate JSON structure if the trailer is present.
  if [[ -n "$REVIEW_VERDICT_LINE" ]]; then
    # ── 9. JSON well-formedness + required fields ─────────────────────────
    VERDICT_JSON="${REVIEW_VERDICT_LINE#Review-Verdict: }"
    VERDICT_JSON="${VERDICT_JSON#Review-Verdict:}"  # handle no-space variant

    # Parse JSON using jq (preferred) or Python fallback.
    VERDICT_FIELD=""
    SCHEMA_VERSION=""
    PARSE_ERROR=""

    if command -v jq &>/dev/null; then
      if PARSED="$(printf '%s\n' "$VERDICT_JSON" | jq -r '(.schema_version|tostring) + "|" + .verdict' 2>/dev/null)"; then
        SCHEMA_VERSION="${PARSED%%|*}"
        VERDICT_FIELD="${PARSED##*|}"
      else
        PARSE_ERROR="jq could not parse JSON"
      fi
    else
      # jq absent: fall back to Python (macOS + most Linux envs).
      PARSED="$(printf '%s\n' "$VERDICT_JSON" | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
    sv = d.get('schema_version', '')
    v  = d.get('verdict', '')
    print(str(sv) + '|' + str(v))
except Exception as e:
    print('PARSE_ERROR: ' + str(e), file=sys.stderr)
    sys.exit(1)
" 2>&1)"
      PY_EXIT=$?
      if (( PY_EXIT != 0 )); then
        PARSE_ERROR="$PARSED"
      else
        SCHEMA_VERSION="${PARSED%%|*}"
        VERDICT_FIELD="${PARSED##*|}"
      fi
    fi

    if [[ -n "$PARSE_ERROR" ]]; then
      err "Review-Verdict trailer is not valid JSON."
      err "  Parse error: $PARSE_ERROR"
      err "  Got: $VERDICT_JSON"
    else
      # ── 10. schema_version check ─────────────────────────────────────
      if [[ "$SCHEMA_VERSION" != "1" ]]; then
        err "Review-Verdict JSON missing or wrong 'schema_version' (expected 1, got '${SCHEMA_VERSION}')."
        err "  Got: $VERDICT_JSON"
      fi

      # ── 11. verdict field present ────────────────────────────────────
      if [[ -z "$VERDICT_FIELD" || "$VERDICT_FIELD" == "null" ]]; then
        err "Review-Verdict JSON is missing the 'verdict' field."
        err "  Got: $VERDICT_JSON"
      else
        # ── 12. verdict enum check ────────────────────────────────────
        # agent-reviewer schema v1: APPROVE, REQUEST_CHANGES, BLOCK
        # config-reviewer schema v1: CLEAN, DRIFT_MINOR, DRIFT_MAJOR
        case "$VERDICT_FIELD" in
          APPROVE|REQUEST_CHANGES|CLEAN|DRIFT_MINOR|DRIFT_MAJOR)
            # OK — these may land in commits.
            ;;
          BLOCK)
            err "BLOCK verdict must not be committed (fix first)."
            err "  Review-Verdict: $VERDICT_JSON"
            ;;
          *)
            err "unknown verdict value '$VERDICT_FIELD'."
            err "  Allowed (agent-reviewer): APPROVE, REQUEST_CHANGES"
            err "  Allowed (config-reviewer): CLEAN, DRIFT_MINOR, DRIFT_MAJOR"
            err "  BLOCK = fix before committing, never in a commit."
            err "  Got: $VERDICT_JSON"
            ;;
        esac
      fi
    fi
  fi
fi

# ── Final: emit all errors or exit clean ─────────────────────────────────────
if (( ${#ERRORS[@]} > 0 )); then
  echo "validate-commit-msg: validation failed:" >&2
  for line in "${ERRORS[@]}"; do
    echo "$line" >&2
  done
  exit 1
fi

exit 0
