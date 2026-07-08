#!/usr/bin/env bash
# conformance-gate.sh — conformance floor as EXECUTION gate that BLOCKS (WS-E/4, hk-g6plo.4)
#
# WHAT: the first live gate on twin epic hk-pnjgh.  Reads the structured verdict
# JSON from scripts/core-loop-matrix.sh (WS-E/3) and the found-by:* block query
# from scripts/assessor-block-query.sh (WS-E/2), then emits PASS or BLOCK.
#
# Gate rules (non-bypassable; fail-closed on every ambiguity):
#   1. VERDICT FILE missing or unreadable              → BLOCK (never false-green)
#   2. VERDICT FILE schema_version != 1                → BLOCK
#   3. Any cell with verdict "red"                     → BLOCK
#   4. Any cell with verdict "pending" (residual PENDING per T9 full-green gate)
#                                                      → BLOCK
#   5. Open P0/P1 found-by:* bead (via assessor-block-query.sh)
#                                                      → BLOCK
#   6. summary.red > 0  (summary-vs-cell consistency)  → BLOCK
#   7. summary.pending > 0                             → BLOCK
#   8. summary.green == 0 and there are fixtured cells → BLOCK
#   All checks pass + zero open blockers               → PASS
#
# Fail-closed detail: every error condition (parse failure, missing jq, br
# failure in non-advisory mode) produces a BLOCK, never a false-green.
# Exception: --skip-block-query disables rule 5 (for unit-test use only).
#
# Usage:
#   scripts/conformance-gate.sh --verdict <path> [flags]
#
# Required:
#   --verdict <path>     Path to the matrix-verdict.json (schema_version=1)
#                        written by scripts/core-loop-matrix.sh --verdict-out.
#
# Optional:
#   --scope <epic_id>    Scope the found-by:* query to one epic via --label
#                        (beads carry no branch field; scoping is via label).
#   --limit <n>          br list result cap passed to assessor-block-query.sh
#                        (default 0 = unlimited).
#   --skip-block-query   Skip the found-by:* block query (rule 5). For testing
#                        the verdict-file gate in isolation; NOT for production.
#   --json               Emit the gate verdict as a JSON line on stdout in
#                        addition to the human-readable summary.
#
# Machine-readable stdout (always):
#   GATE_VERDICT=PASS|BLOCK
#   GATE_REASON=<one-line reason for the verdict>
#   MATRIX_VERDICT_FILE=<path>      (echoed from --verdict)
#
# Exit codes:
#   0  — PASS: full-green matrix + zero open found-by:* blockers
#   1  — BLOCK: at least one failing condition (see rules above)
#   2  — usage error (bad flags)
#
# Depends on: jq, scripts/assessor-block-query.sh, python3 (for json parse
# in assessor-block-query.sh).

set -euo pipefail

SELF="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BLOCK_QUERY="$REPO_ROOT/scripts/assessor-block-query.sh"

log()  { printf '[conformance-gate] %s\n' "$*" >&2; }
die()  { printf '[conformance-gate] ERROR: %s\n' "$*" >&2; exit 2; }
block(){ printf '[conformance-gate] BLOCK: %s\n' "$*" >&2; _VERDICT=BLOCK; _REASON="$*"; }
pass() { printf '[conformance-gate] PASS: %s\n' "$*" >&2; _VERDICT=PASS; _REASON="$*"; }

# ── argument parsing ─────────────────────────────────────────────────────────
VERDICT_FILE=""
SCOPE=""
LIMIT=0
SKIP_BLOCK_QUERY=0
EMIT_JSON=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --verdict)
            [[ $# -lt 2 ]] && die "--verdict requires a value"
            VERDICT_FILE="$2"; shift 2 ;;
        --verdict=*)
            VERDICT_FILE="${1#--verdict=}"; shift ;;
        --scope)
            [[ $# -lt 2 ]] && die "--scope requires a value"
            SCOPE="$2"; shift 2 ;;
        --scope=*)
            SCOPE="${1#--scope=}"; shift ;;
        --limit)
            [[ $# -lt 2 ]] && die "--limit requires a value"
            LIMIT="$2"; shift 2 ;;
        --limit=*)
            LIMIT="${1#--limit=}"; shift ;;
        --skip-block-query)
            SKIP_BLOCK_QUERY=1; shift ;;
        --json)
            EMIT_JSON=1; shift ;;
        -h|--help)
            sed -n 's/^# \?//p' "$0"; exit 0 ;;
        *)
            die "unknown flag '$1' (see header for usage)" ;;
    esac
done

[[ -n "$VERDICT_FILE" ]] || die "--verdict <path> is required"

command -v jq >/dev/null 2>&1 || { block "jq is not installed — gate cannot parse the verdict file"; emit_and_exit; }

# ── helpers ───────────────────────────────────────────────────────────────────
_VERDICT=""
_REASON=""

emit_and_exit() {
    local rc=1
    [[ "$_VERDICT" == "PASS" ]] && rc=0
    echo "GATE_VERDICT=$_VERDICT"
    echo "GATE_REASON=$_REASON"
    echo "MATRIX_VERDICT_FILE=$VERDICT_FILE"
    if [[ "$EMIT_JSON" -eq 1 ]]; then
        jq -n \
            --arg verdict "$_VERDICT" \
            --arg reason "$_REASON" \
            --arg verdict_file "$VERDICT_FILE" \
            '{gate_verdict:$verdict, gate_reason:$reason, matrix_verdict_file:$verdict_file}' \
            2>/dev/null || true
    fi
    exit "$rc"
}

# ── rule 1: verdict file must exist and be readable ──────────────────────────
if [[ ! -f "$VERDICT_FILE" ]]; then
    block "verdict file not found: $VERDICT_FILE (fail-closed)"
    emit_and_exit
fi
if [[ ! -r "$VERDICT_FILE" ]]; then
    block "verdict file not readable: $VERDICT_FILE (fail-closed)"
    emit_and_exit
fi

# ── parse the verdict JSON ────────────────────────────────────────────────────
verdict_json=""
if ! verdict_json="$(cat "$VERDICT_FILE")"; then
    block "failed to read verdict file: $VERDICT_FILE (fail-closed)"
    emit_and_exit
fi

# ── rule 2: schema_version must be 1 ─────────────────────────────────────────
schema_ver=""
schema_ver="$(printf '%s' "$verdict_json" | jq -r '.schema_version // empty' 2>/dev/null)" || {
    block "verdict file is not valid JSON: $VERDICT_FILE (fail-closed)"
    emit_and_exit
}
if [[ -z "$schema_ver" ]]; then
    block "verdict file missing schema_version: $VERDICT_FILE (fail-closed)"
    emit_and_exit
fi
if [[ "$schema_ver" != "1" ]]; then
    block "verdict file schema_version=$schema_ver, want 1 (fail-closed)"
    emit_and_exit
fi

# ── rule 6+7: summary-level red/pending counters ─────────────────────────────
n_red="$(printf '%s' "$verdict_json" | jq -r '.summary.red // 0' 2>/dev/null)" || n_red=0
n_pending="$(printf '%s' "$verdict_json" | jq -r '.summary.pending // 0' 2>/dev/null)" || n_pending=0
n_green="$(printf '%s' "$verdict_json" | jq -r '.summary.green // 0' 2>/dev/null)" || n_green=0
n_total="$(printf '%s' "$verdict_json" | jq -r '.summary.total // 0' 2>/dev/null)" || n_total=0

log "matrix verdict: total=$n_total green=$n_green red=$n_red pending=$n_pending"

if [[ "$n_red" -gt 0 ]]; then
    block "matrix has $n_red red cell(s) — full-green required for PASS"
    emit_and_exit
fi

if [[ "$n_pending" -gt 0 ]]; then
    block "matrix has $n_pending pending cell(s) — T9 full-green gate requires zero PENDING"
    emit_and_exit
fi

# ── rules 3+4: per-cell red/pending scan (defence in depth vs summary drift) ─
bad_cells=""
bad_cells="$(printf '%s' "$verdict_json" | \
    jq -r '.cells[] | select(.verdict=="red" or .verdict=="pending") | "\(.cell)=\(.verdict)"' \
    2>/dev/null)" || {
    block "failed to parse cells from verdict file (fail-closed)"
    emit_and_exit
}

if [[ -n "$bad_cells" ]]; then
    block "failing cell(s): $(printf '%s' "$bad_cells" | tr '\n' ' ')"
    emit_and_exit
fi

# ── rule 8: zero green and fixtured cells present = something is wrong ────────
n_fixtured="$(printf '%s' "$verdict_json" | \
    jq -r '[.cells[] | select(.verdict != "skip")] | length' 2>/dev/null)" || n_fixtured=0
if [[ "$n_fixtured" -gt 0 && "$n_green" -eq 0 ]]; then
    block "matrix has $n_fixtured fixtured cell(s) but zero green (fail-closed)"
    emit_and_exit
fi

log "matrix check passed: green=$n_green pending=$n_pending red=$n_red"

# ── rule 5: found-by:* block query ───────────────────────────────────────────
if [[ "$SKIP_BLOCK_QUERY" -eq 0 ]]; then
    [[ -x "$BLOCK_QUERY" ]] || {
        block "assessor-block-query.sh not found or not executable: $BLOCK_QUERY (fail-closed)"
        emit_and_exit
    }

    bq_args=()
    [[ -n "$SCOPE" ]] && bq_args+=(--scope "$SCOPE")
    [[ "$LIMIT" -gt 0 ]] && bq_args+=(--limit "$LIMIT")

    bq_out=""
    bq_rc=0
    bq_out="$("$BLOCK_QUERY" "${bq_args[@]}" 2>&1)" || bq_rc=$?

    case "$bq_rc" in
        0) log "block query: no open P0/P1 found-by:* beads — PASS" ;;
        1)
            # extract total from the JSON output for the reason message
            bq_total="$(printf '%s' "$bq_out" | \
                python3 -c 'import sys,json; print(json.load(sys.stdin).get("total",0))' 2>/dev/null)" \
                || bq_total="?"
            block "found-by:* block query: $bq_total open P0/P1 bead(s) (BLOCK)"
            emit_and_exit ;;
        2)
            block "found-by:* block query failed (rc=2) — fail-closed: $bq_out"
            emit_and_exit ;;
        *)
            block "found-by:* block query returned unexpected rc=$bq_rc — fail-closed"
            emit_and_exit ;;
    esac
else
    log "block query skipped (--skip-block-query)"
fi

# ── PASS ─────────────────────────────────────────────────────────────────────
pass "full-green matrix (green=$n_green) + zero open found-by:* blockers"
emit_and_exit
