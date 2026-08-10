#!/usr/bin/env bash
# scripts/validate-commit-msg.sh — commit-message validator (run via the
# agent-driven /check flow; git hooks are retired).
#
# Usage: validate-commit-msg.sh <commit-msg-file>
#
# Validates:
#   1. Subject line matches Conventional Commits format (closed type set).
#   2. Subject length ≤72 characters and no trailing period.
#   3. Non-trivial commits carry Reviewed-By: and Review-Verdict: trailers.
#   4. Review-Verdict: value is well-formed JSON matching agent-reviewer
#      schema v1: schema_version=1, verdict ∈ {APPROVE, REQUEST_CHANGES,
#      NOT_REVIEWED} or config-reviewer schema v1: verdict ∈ {CLEAN,
#      DRIFT_MINOR, DRIFT_MAJOR}. `notes` is required and must not be empty.
#
# The honest no-reviewer form (AGENTS.md, the `git commit -F` rule):
#
#   Reviewed-By: none — no reviewer was reached for this commit
#   Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED",
#                    "flags": ["no-reviewer-reached"], "notes": "…"}
#
# The rule says a commit whose reviewer could not be reached records that fact
# and lands anyway, and that such a trailer carries NO verdict of APPROVE.
# NOT_REVIEWED is that record. It passes. Before this was here, the honest
# sentence was the ONE shape the validator rejected, which pushed the author
# toward writing down an approval nobody gave.
#
# An APPROVE (and its config-reviewer twin CLEAN) is held to more than the
# other verdicts, so that the honest form is always the cheaper thing to write:
#   - Reviewed-By must name a reviewer skill this repo actually has
#     (`.claude/skills/*reviewer*`), optionally with a parenthetical qualifier.
#   - Reviewed-By must not name the author ("self").
#   - The JSON must carry the `flags` key that the reviewer skill always emits.
# None of this proves a reviewer ran — a shell script cannot see session state,
# and it never could. What it does is remove the vague middle ground: a false
# APPROVE now has to name agent-reviewer outright, which one grep can audit.
#
# Trivial-commit bypass: add the trailer `Trivial: true` to the commit
# message to skip the Reviewed-By / Review-Verdict requirement.
# Use ONLY for typos, whitespace fixes, and obvious one-liners — the
# agent-reviewer is still required for all other commits per build-practices.md.
#
# Exit 0 = OK; exit 1 = validation failure. Every problem found is reported, not
# just the first, and each is numbered on its own line.

set -uo pipefail

ERRORS=()
ERROR_COUNT=0

err() {
  ERROR_COUNT=$(( ERROR_COUNT + 1 ))
  ERRORS+=( "  [${ERROR_COUNT}] $*" )
}

# known_reviewers — the reviewer identities an APPROVE / CLEAN may name.
#
# Read off the filesystem rather than hard-coded, so adding a reviewer skill is
# enough and this list cannot rot behind it. Today that is agent-reviewer and
# agent-config-reviewer. If the skills directory cannot be found (the script is
# callable from anywhere, including a bare worktree), fall back to those two
# names rather than to "anything goes" — an unreadable directory must not turn
# into a free pass.
known_reviewers() {
  local root skills found=""
  root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null || true)"
  [[ -z "$root" ]] && root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd || true)"
  skills="${root}/.claude/skills"
  if [[ -n "$root" && -d "$skills" ]]; then
    local d
    for d in "$skills"/*reviewer*; do
      [[ -d "$d" ]] || continue
      found+="$(basename "$d")"$'\n'
    done
  fi
  if [[ -z "$found" ]]; then
    found=$'agent-reviewer\nagent-config-reviewer\n'
  fi
  printf '%s' "$found"
}

# check_approval_identity — the extra bar an APPROVE / CLEAN has to clear.
#
# Reads the global REVIEWED_BY (the whole `Reviewed-By:` line). An approval must
# name one of the reviewer skills this repo has, with an optional parenthetical
# qualifier after it — `agent-reviewer (codex harness)` passes, `Codex reviewer`
# does not. And it must not name the author: the rule says do not author your
# own approval, so a value that says "self" is refused here.
#
# This proves the name is a real reviewer. It cannot prove that reviewer ran.
check_approval_identity() {
  local verdict_name="$1" value base known matched=false
  value="${REVIEWED_BY#Reviewed-By:}"
  value="${value#"${value%%[![:space:]]*}"}"   # trim leading space
  value="${value%"${value##*[![:space:]]}"}"   # trim trailing space

  if [[ -z "$value" ]]; then
    err "the ${verdict_name} verdict needs a 'Reviewed-By:' value naming the reviewer that ran."
    return
  fi

  if printf '%s\n' "$value" | grep -qiE '(^|[^a-z])self([^a-z]|$)'; then
    err "the ${verdict_name} verdict must not be self-authored (Reviewed-By: ${value})."
    err "  The rule is: quote the reviewer's verdict, never author your own approval."
    err "  If no reviewer was reached, record that instead: \"verdict\": \"NOT_REVIEWED\"."
    return
  fi

  # Drop one trailing parenthetical qualifier, then compare the bare name.
  base="$(printf '%s\n' "$value" | sed -E 's/[[:space:]]*\([^()]*\)[[:space:]]*$//')"
  base="${base%"${base##*[![:space:]]}"}"

  # Lower-cased with tr, not with ${x,,}: this script has to run under the
  # bash 3.2 that ships with macOS as well as a modern one.
  local base_lc known_lc
  base_lc="$(printf '%s\n' "$base" | tr '[:upper:]' '[:lower:]')"
  while IFS= read -r known; do
    [[ -z "$known" ]] && continue
    known_lc="$(printf '%s\n' "$known" | tr '[:upper:]' '[:lower:]')"
    if [[ "$base_lc" == "$known_lc" ]]; then
      matched=true
      break
    fi
  done < <(known_reviewers)

  if [[ "$matched" != true ]]; then
    err "the ${verdict_name} verdict must name a reviewer skill this repo has; got '${value}'."
    err "  Known reviewers: $(known_reviewers | tr '\n' ' ')"
    err "  A qualifier in parentheses is fine: 'agent-reviewer (codex harness)'."
    err "  If no reviewer was reached, record that instead: \"verdict\": \"NOT_REVIEWED\"."
  fi
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
# Types (closed set per build-practices.md §Decisions, "Types (closed set)"):
#   feat fix refactor test docs chore spec build perf
# 9 types only; ci/revert/style are NOT canonical here. Update both this regex
# AND build-practices.md if the set ever expands.
CC_PATTERN='^(feat|fix|refactor|test|docs|chore|spec|build|perf)(\([a-z0-9,:-]+\))?(!)?: .+'
if ! printf '%s\n' "$SUBJECT" | grep -qE "$CC_PATTERN"; then
  err "subject does not match Conventional Commits format."
  err "  Expected: <type>[(<scope>)][!]: <description>"
  err "  Allowed types: feat fix refactor test docs chore spec build perf"
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
    #
    # Four fields come back, tab-separated, and none of them is free text: the
    # notes and flags values are reduced to a state word here so that a `notes`
    # string containing a tab or a newline cannot shift the other fields.
    #   1 schema_version  — as a string; "" when the key is absent
    #   2 verdict         — the value, or __NULL__ / __ABSENT__
    #   3 notes state     — ok | empty | badtype | absent
    #   4 flags state     — array | notarray | absent
    VERDICT_FIELD=""
    SCHEMA_VERSION=""
    NOTES_STATE=""
    FLAGS_STATE=""
    PARSE_ERROR=""

    JQ_PROG='
      if type != "object" then "NOTOBJ" else
      [ (if has("schema_version") then (.schema_version|tostring) else "" end),
        (if has("verdict") then (if .verdict == null then "__NULL__" else (.verdict|tostring) end) else "__ABSENT__" end),
        (if has("notes") then
           (if (.notes|type) != "string" then "badtype"
            elif (.notes|test("\\S")) then "ok"
            else "empty" end)
         else "absent" end),
        (if has("flags") then (if (.flags|type) == "array" or .flags == null then "array" else "notarray" end) else "absent" end)
      ] | @tsv end'

    if command -v jq &>/dev/null; then
      if PARSED="$(printf '%s\n' "$VERDICT_JSON" | jq -r "$JQ_PROG" 2>/dev/null)"; then
        :
      else
        PARSE_ERROR="jq could not parse JSON"
      fi
    else
      # jq absent: fall back to Python (macOS + most Linux envs).
      PARSED="$(printf '%s\n' "$VERDICT_JSON" | python3 -c "
import sys, json, re
try:
    d = json.load(sys.stdin)
except Exception as e:
    print('PARSE_ERROR: ' + str(e), file=sys.stderr)
    sys.exit(1)
if not isinstance(d, dict):
    print('NOTOBJ')
    sys.exit(0)
sv = str(d['schema_version']) if 'schema_version' in d else ''
if 'verdict' not in d:
    v = '__ABSENT__'
elif d['verdict'] is None:
    v = '__NULL__'
else:
    v = str(d['verdict'])
if 'notes' not in d:
    n = 'absent'
elif not isinstance(d['notes'], str):
    n = 'badtype'
elif re.search(r'\S', d['notes']):
    n = 'ok'
else:
    n = 'empty'
if 'flags' not in d:
    f = 'absent'
elif d['flags'] is None or isinstance(d['flags'], list):
    f = 'array'
else:
    f = 'notarray'
print('\t'.join([sv, v, n, f]))
" 2>&1)"
      PY_EXIT=$?
      if (( PY_EXIT != 0 )); then
        PARSE_ERROR="$PARSED"
      fi
    fi

    if [[ -z "$PARSE_ERROR" ]]; then
      if [[ "$PARSED" == "NOTOBJ" ]]; then
        PARSE_ERROR="Review-Verdict is valid JSON but not a JSON object"
      else
        IFS=$'\t' read -r SCHEMA_VERSION VERDICT_FIELD NOTES_STATE FLAGS_STATE <<<"$PARSED"
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

      # ── 11. notes field required and non-empty ───────────────────────
      # `notes` is a required field of schema v1 in every statement of it —
      # the agent-reviewer skill, the config-reviewer skill, and the Go reader
      # in internal/workspace/reviewverdict.go, which rejects a verdict file
      # with absent or empty notes. Only this validator used to let it through.
      case "$NOTES_STATE" in
        ok) ;;
        absent)
          err "Review-Verdict JSON is missing the required 'notes' field."
          err "  schema v1 requires: schema_version, verdict, notes."
          err "  Got: $VERDICT_JSON"
          ;;
        empty)
          err "Review-Verdict JSON has an empty 'notes' field."
          err "  notes carries the reason for the verdict; it must say something."
          err "  Got: $VERDICT_JSON"
          ;;
        badtype)
          err "Review-Verdict JSON field 'notes' must be a string."
          err "  Got: $VERDICT_JSON"
          ;;
        *)
          # Fail closed. The parser is meant to hand back one of the four words
          # above. Anything else means the record came apart — a notes string
          # holding a tab is the way that happens — and a validator that shrugs
          # at a record it could not read is a validator that approves it.
          err "Review-Verdict parser returned an unreadable 'notes' state ('${NOTES_STATE}')."
          err "  Got: $VERDICT_JSON"
          ;;
      esac

      # ── 12. verdict field present ────────────────────────────────────
      if [[ "$VERDICT_FIELD" == "__ABSENT__" || -z "$VERDICT_FIELD" ]]; then
        err "Review-Verdict JSON is missing the 'verdict' field."
        err "  Got: $VERDICT_JSON"
      elif [[ "$VERDICT_FIELD" == "__NULL__" ]]; then
        # A null verdict is the honest intent written in a shape that cannot be
        # told apart from a truncated trailer. This history holds both: trailers
        # cut off just after `"verdict": null`, and trailers that parse cleanly
        # with a null verdict. Say the absence in words instead.
        err "Review-Verdict 'verdict' is null."
        err "  A null verdict cannot be told apart from a truncated trailer."
        err "  If no reviewer was reached, say so: \"verdict\": \"NOT_REVIEWED\"."
        err "  Got: $VERDICT_JSON"
      else
        # ── 13. verdict enum check ────────────────────────────────────
        # agent-reviewer schema v1: APPROVE, REQUEST_CHANGES, BLOCK
        # config-reviewer schema v1: CLEAN, DRIFT_MINOR, DRIFT_MAJOR
        # AGENTS.md no-reviewer-reached rule: NOT_REVIEWED
        case "$VERDICT_FIELD" in
          APPROVE|CLEAN)
            # ── 14. an approval is held to more than the rest ─────────
            check_approval_identity "$VERDICT_FIELD"
            if [[ "$FLAGS_STATE" == "absent" ]]; then
              err "the ${VERDICT_FIELD} verdict must carry the 'flags' key that the reviewer skill emits."
              err "  Use \"flags\": [] when the reviewer raised nothing."
              err "  Got: $VERDICT_JSON"
            elif [[ "$FLAGS_STATE" == "notarray" ]]; then
              err "Review-Verdict JSON field 'flags' must be an array."
              err "  Got: $VERDICT_JSON"
            elif [[ "$FLAGS_STATE" != "array" ]]; then
              err "Review-Verdict parser returned an unreadable 'flags' state ('${FLAGS_STATE}')."
              err "  Got: $VERDICT_JSON"
            fi
            ;;
          REQUEST_CHANGES|DRIFT_MINOR|DRIFT_MAJOR|NOT_REVIEWED)
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
            err "  Allowed when no reviewer was reached: NOT_REVIEWED"
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
