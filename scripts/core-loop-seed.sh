#!/usr/bin/env bash
# core-loop-seed.sh — create the core-loop-proof fixture beads in a SCRATCH beads DB and
# emit the cell->bead_id MATRIX_SEED_MAP the matrix runner consumes (T9, hk-jjt6w).
#
# Reads scenarios/core-loop-proof/seed-beads.json, creates one OPEN bead per seed entry in
# the SCRATCH clone's beads DB (br auto-discovers .beads from the scratch CWD, in an
# isolated subshell — never the fleet DB), then writes a `cell<TAB>bead_id` map covering
# every cell in cells.json (a cell's bead = the seed for its harness family).
#
# USAGE: core-loop-seed.sh <scratch-path> <map-out-path>
# The daemon owns terminal transitions; these are created OPEN and never pre-assigned.

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SEEDS="$ROOT/scenarios/core-loop-proof/seed-beads.json"
CELLS="$ROOT/scenarios/core-loop-proof/cells.json"
command -v jq >/dev/null 2>&1 || { echo "jq required" >&2; exit 2; }
command -v br >/dev/null 2>&1 || { echo "br required" >&2; exit 2; }

SCRATCH="${1:?usage: core-loop-seed.sh <scratch-path> <map-out-path>}"
MAP_OUT="${2:?map-out-path required}"
[ -d "$SCRATCH/.beads" ] || { echo "scratch has no .beads dir: $SCRATCH (run scratch-daemon.sh init first)" >&2; exit 2; }

# key<TAB>bead_id, one line per created fixture
KEY2ID="$(mktemp "${TMPDIR:-/tmp}/core-loop-key2id.XXXXXX")"
trap 'rm -f "$KEY2ID"' EXIT

n="$(jq '.seeds | length' "$SEEDS")"
for i in $(seq 0 $((n-1))); do
    key="$(jq -r ".seeds[$i].key" "$SEEDS")"
    title="$(jq -r ".seeds[$i].title" "$SEEDS")"
    body="$(jq -r ".seeds[$i].body" "$SEEDS")"
    labels="$(jq -r ".seeds[$i].labels | join(\",\")" "$SEEDS")"
    # create in the SCRATCH DB (subshell CWD = scratch; never the fleet DB)
    out="$( cd "$SCRATCH" && br create --title="$title" --description="$body" \
              --type=task --priority=2 --labels="$labels" --json 2>&1 )"
    id="$(printf '%s' "$out" | jq -r '.id // empty' 2>/dev/null)"
    [ -n "$id" ] || { echo "seed '$key' create failed: $out" >&2; exit 1; }
    # br create auto-assigns to the owner; the daemon only dispatches UNASSIGNED beads
    # (it claims them itself), so a pre-assigned seed fast-fails as a claim-skip. Clear it.
    ( cd "$SCRATCH" && br update "$id" --assignee "" >/dev/null 2>&1 ) || true
    printf '%s\t%s\n' "$key" "$id" >> "$KEY2ID"
    echo "[core-loop-seed] $key -> $id ($labels)"
done

# emit MATRIX_SEED_MAP: for each cell, resolve its seed_bead KEY to the created bead id.
: > "$MAP_OUT"
jq -r '.cells[] | "\(.cell)\t\(.seed_bead)"' "$CELLS" | while IFS=$'\t' read -r cell key; do
    id="$(awk -F'\t' -v k="$key" '$1==k{print $2; exit}' "$KEY2ID")"
    [ -n "$id" ] || { echo "no seed created for cell $cell (key $key)" >&2; continue; }
    printf '%s\t%s\n' "$cell" "$id" >> "$MAP_OUT"
done
echo "[core-loop-seed] wrote MATRIX_SEED_MAP -> $MAP_OUT ($(wc -l < "$MAP_OUT" | tr -d ' ') cells)"
