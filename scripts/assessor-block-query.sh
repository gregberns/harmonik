#!/usr/bin/env bash
# assessor-block-query.sh — deterministic found-by:* block query for the assessor gate (hk-g6plo)
#
# Queries open P0/P1 beads filed by any known assessor source (found-by:* family)
# and emits machine-readable JSON. Used by the assessor and the WS-E/4 gate.
#
# WHY: 'br list --label "found-by:*"' matches nothing — * is literal, not a glob.
# '--label-any' (OR logic) across the known found-by: sources is the correct fix.
# Ref: plans/2026-07-06-quality-system/08-assessor-wireup-plan.md §Gap B.
#
# Usage: assessor-block-query.sh [--scope <epic_id>] [--limit <n>]
#
# Options:
#   --scope <epic_id>  Scope to one epic via --label <epic_id> (beads carry no branch field)
#   --limit <n>        Result cap; default 0 = unlimited
#
# Output (stdout): JSON  {"issues":[...],"total":N,"limit":...,"offset":...,"has_more":...}
# Exit codes:
#   0 — no open P0/P1 found-by beads (PASS)
#   1 — one or more blocking beads found (BLOCK)
#   2 — query error (br failed or output unparseable)
#
# To add a new found-by source: append to FOUND_BY_SOURCES below.
# The gate inherits the new source automatically on the next run.

set -uo pipefail

# ── known found-by: sources ──────────────────────────────────────────────────
# Extend when a new assessor source type is introduced.
FOUND_BY_SOURCES=(
    found-by:assessor
    found-by:admiral
    found-by:fast-follow
    found-by:matrix
    found-by:exploratory
    found-by:live
    found-by:review
    found-by:comms-test-harness
)

# ── argument parsing ─────────────────────────────────────────────────────────
scope=""
limit=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --scope)
            [[ $# -lt 2 ]] && { printf 'assessor-block-query: --scope requires a value\n' >&2; exit 2; }
            scope="$2"; shift 2 ;;
        --limit)
            [[ $# -lt 2 ]] && { printf 'assessor-block-query: --limit requires a value\n' >&2; exit 2; }
            limit="$2"; shift 2 ;;
        -h|--help)
            sed -n 's/^# \?//p' "$0"; exit 0 ;;
        *)
            printf 'assessor-block-query: unknown argument: %s\n' "$1" >&2; exit 2 ;;
    esac
done

# ── build the br list command ─────────────────────────────────────────────────
# --priority 0 --priority 1 = P0 (critical) + P1 (high)
# --label-any (OR logic) across every known found-by: source
# --limit 0 = unlimited (default 50 would silently miss beads beyond the cap)
cmd=(br list --status open --priority 0 --priority 1)
for src in "${FOUND_BY_SOURCES[@]}"; do
    cmd+=(--label-any "$src")
done
[[ -n "$scope" ]] && cmd+=(--label "$scope")
cmd+=(--limit "$limit" --json)

# ── run query ────────────────────────────────────────────────────────────────
out=$("${cmd[@]}" 2>&1)
rc=$?

if [[ $rc -ne 0 ]]; then
    printf 'assessor-block-query: br list failed (rc=%d): %s\n' "$rc" "$out" >&2
    exit 2
fi

printf '%s\n' "$out"

# ── parse total and set exit code ─────────────────────────────────────────────
# exit 1 (BLOCK) when total > 0; exit 0 (PASS) when empty
total=$(printf '%s' "$out" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("total",0))' 2>/dev/null) || {
    printf 'assessor-block-query: failed to parse total from br output\n' >&2
    exit 2
}

[[ "$total" -gt 0 ]] && exit 1
exit 0
