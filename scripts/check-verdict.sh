#!/usr/bin/env bash
# scripts/check-verdict.sh — pre-commit cross-check for diff-keyed APPROVE verdict.
#
# Workstream A4 (hk-q6axs.4). Called by make agent-review after the
# agent-reviewer stores its verdict. Permits commit only when an APPROVE
# verdict exists in the diff-keyed cache at .harmonik/verdicts/<hash>.json.
# Absent/stale/REQUEST_CHANGES/BLOCK all fail CLOSED.
#
# Uses the same diff computation and sha256 hash as agent-reviewer/run so the
# hash resolves to the same verdict file.
#
# Usage: check-verdict.sh [--diff <REF>]
#   --diff REF    Git ref to diff from (default: HEAD~1)
#
# Exit 0: APPROVE verdict found for the current diff.
# Exit 1: no verdict cached, verdict is REQUEST_CHANGES/BLOCK, or parse error.

set -uo pipefail

DIFF_REF="HEAD~1"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --diff) DIFF_REF="$2"; shift 2 ;;
        --) shift; break ;;
        -*) echo "check-verdict: unknown flag: $1" >&2; exit 1 ;;
        *) echo "check-verdict: unexpected argument: $1" >&2; exit 1 ;;
    esac
done

GIT_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
    echo "check-verdict: not inside a git repository" >&2
    exit 1
}

# ── Compute diff (same algorithm as agent-reviewer/run) ────────────────────
DIFF="$(git diff "${DIFF_REF}" 2>/dev/null)"
if [[ -z "$DIFF" ]]; then
    DIFF="$(git diff "${DIFF_REF}..HEAD" 2>/dev/null)"
fi
if [[ -z "$DIFF" ]]; then
    echo "check-verdict: empty diff; skipping verdict check" >&2
    exit 0
fi

# ── Diff hash (first 16 hex chars of sha256, same as agent-reviewer/run) ──
if command -v sha256sum &>/dev/null; then
    DIFF_HASH="$(printf '%s' "$DIFF" | sha256sum | cut -c1-16)"
else
    DIFF_HASH="$(printf '%s' "$DIFF" | shasum -a 256 | cut -c1-16)"
fi

VERDICT_FILE="${GIT_ROOT}/.harmonik/verdicts/${DIFF_HASH}.json"

# ── Require cached verdict ─────────────────────────────────────────────────
if [[ ! -f "$VERDICT_FILE" ]]; then
    echo "check-verdict: no verdict cached for this diff (hash ${DIFF_HASH})" >&2
    echo "check-verdict: run 'make agent-review' to generate an APPROVE verdict before committing" >&2
    exit 1
fi

# ── Read verdict field ─────────────────────────────────────────────────────
VERDICT_VALUE="$(python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('verdict',''))" \
    < "$VERDICT_FILE" 2>/dev/null)" || {
    echo "check-verdict: failed to parse ${VERDICT_FILE}" >&2
    exit 1
}

# ── Enforce APPROVE-only ───────────────────────────────────────────────────
case "$VERDICT_VALUE" in
    APPROVE)
        echo "check-verdict: APPROVE for diff hash ${DIFF_HASH}" >&2
        exit 0
        ;;
    REQUEST_CHANGES)
        echo "check-verdict: BLOCK — verdict is REQUEST_CHANGES for this diff (hash ${DIFF_HASH})" >&2
        echo "check-verdict: address the agent-reviewer flags and obtain an APPROVE verdict before committing" >&2
        exit 1
        ;;
    BLOCK)
        echo "check-verdict: BLOCK — verdict is BLOCK for this diff (hash ${DIFF_HASH})" >&2
        echo "check-verdict: fix the BLOCK-level issues and obtain an APPROVE verdict before committing" >&2
        exit 1
        ;;
    "")
        echo "check-verdict: 'verdict' field missing or empty in ${VERDICT_FILE}" >&2
        exit 1
        ;;
    *)
        echo "check-verdict: unknown verdict '${VERDICT_VALUE}' in ${VERDICT_FILE}" >&2
        exit 1
        ;;
esac
