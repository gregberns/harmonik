#!/usr/bin/env bash
# core-loop-assert-cell.sh — fold ONE cell's captured event stream through the assertion
# library and print a per-gap breakdown + a single cell verdict (T9, hk-jjt6w).
#
# Separated from core-loop-matrix.sh so the fold is OFFLINE-TESTABLE against golden
# streams (no live daemon): the matrix runner's --assert path arms a subscribe capture
# and then calls this helper.
#
# USAGE:
#   core-loop-assert-cell.sh <capture.ndjson> <cell-spec-json> [<ref.ndjson|-> ]
#     <capture.ndjson>   the cell's captured event stream (NDJSON).
#     <cell-spec-json>   one expected-cell object (a JSON string).
#     <ref.ndjson>       OPTIONAL local reference stream for gap2 (remote cells); "-" or
#                        omitted → null (gap2 SKIP-LOUD).
#
# OUTPUT: one `GAP <gap> <verdict> <detail>` line per gap, then
#   `CELL_VERDICT <green|red|pending>` where:
#     red     — any gap verdict is `fail` (a real regression, or a wired known-RED)
#     pending — no fail, but ≥1 gap is `pending` (assertion not exercised / SKIP-LOUD)
#     green   — every listed gap passed
# EXIT: 0 green, 1 red, 2 pending (so the runner can distinguish a partial cell).

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIB="$ROOT/scripts/core-loop-assert.jq"
command -v jq >/dev/null 2>&1 || { echo "jq required" >&2; exit 3; }

CAP="${1:?usage: core-loop-assert-cell.sh <capture.ndjson> <spec-json> [<ref.ndjson>]}"
SPEC="${2:?spec-json required}"
REF="${3:-}"
[ -f "$CAP" ] || { echo "capture stream not found: $CAP" >&2; exit 3; }

ref_json='null'
if [ -n "$REF" ] && [ "$REF" != "-" ] && [ -f "$REF" ]; then
    ref_json="$(jq -s '.' "$REF")"
fi

results="$(jq -n --slurpfile events "$CAP" --argjson spec "$SPEC" --argjson ref_events "$ref_json" -f "$LIB")"
printf '%s\n' "$results" | jq -r '.[] | "GAP\t\(.gap)\t\(.verdict)\t\(.detail)"'

n_fail="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="fail")] | length')"
n_pending="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="pending")] | length')"
if [ "$n_fail" -gt 0 ]; then echo "CELL_VERDICT red"; exit 1;
elif [ "$n_pending" -gt 0 ]; then echo "CELL_VERDICT pending"; exit 2;
else echo "CELL_VERDICT green"; exit 0; fi
