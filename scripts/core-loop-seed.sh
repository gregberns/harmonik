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

# D2: isolate the scratch's `origin` from the fleet clone. The daemon's landing does
# `git push origin <target_branch>` (workloop.go mergeRunBranchToMain), and a scratch cloned
# from the fleet has origin = the fleet repo, which carries stale core-loop-proof-* branches
# from prior sessions. Pushing a fresh integration branch there non-FF-rejects, the daemon
# rebases the run's commit onto the STALE tip, hits a content conflict on the appended line,
# drops the commit, and leaves the branch at the stale SHA — a FALSE landing (the runner sees
# a branch "advance" that is not this run's change). Re-point origin at a throwaway bare repo
# seeded with only `main`, so every integration-branch push is a clean fast-forward create and
# the fleet's refs are never touched. Gated on any target_branch seed; idempotent.
if jq -e '[.seeds[] | select(.target_branch != null)] | length > 0' "$SEEDS" >/dev/null 2>&1; then
    ORIGIN_BARE="$SCRATCH/.harmonik/matrix-origin.git"
    [ -d "$ORIGIN_BARE" ] || git init --quiet --bare "$ORIGIN_BARE"
    git -C "$SCRATCH" push --quiet "$ORIGIN_BARE" "main:main" 2>/dev/null \
        || git -C "$SCRATCH" push --quiet "$ORIGIN_BARE" "HEAD:main" 2>/dev/null || true
    git -C "$SCRATCH" remote set-url origin "$ORIGIN_BARE"
    git -C "$SCRATCH" fetch --quiet origin 2>/dev/null || true
    echo "[core-loop-seed] isolated origin -> $ORIGIN_BARE (daemon landings push here, never the fleet)"
fi

# D4: provision review-loop.dot at the scratch root for the dot cell's `dot:review-loop`
# label (resolveWorkflowRef tier-1 -> <projectDir>/review-loop.dot). Its reviewer node
# carries NO harness= pin, so the reviewer inherits the implementer's resolved pi/ornith
# harness => a same-model review->implement round-trip.
#
# Why NOT override the project-default workflow.dot: the repo TRACKS workflow.dot (it is
# committed as standard-bead.dot, whose review node pins harness="claude-code" +
# model="claude-opus-4-8" => a claude leak). Overwriting the tracked file does not survive:
# every successful landing runs `git reset --hard HEAD` on the project dir (workloop.go
# Step 5b, EM-054), which restores the COMMITTED standard-bead.dot mid-run — so the first
# cell to land silently reverts the override and later dot cells leak to claude. A NEW,
# UNTRACKED file at the project root is immune to `reset --hard` (tracked-only), and the
# daemon's `git clean -fd` only targets run worktrees, never the project dir. Selecting it
# per-bead via the dot: label is also the daemon's designed tier-1 mechanism.
# Gated on a dot:review-loop seed; idempotent (plain overwrite).
if jq -e '[.seeds[] | select(.labels[]? == "dot:review-loop")] | length > 0' "$SEEDS" >/dev/null 2>&1; then
    RL="$ROOT/specs/examples/review-loop.dot"
    if [ -f "$RL" ]; then
        cp "$RL" "$SCRATCH/review-loop.dot"
        echo "[core-loop-seed] provisioned $SCRATCH/review-loop.dot <- specs/examples/review-loop.dot (untracked; dot:review-loop same-model reviewer)"
    else
        echo "[core-loop-seed] WARNING: $RL not found — dot cells fall back to standard-bead.dot (claude reviewer leak)" >&2
    fi
fi

# key<TAB>bead_id, one line per created fixture
KEY2ID="$(mktemp "${TMPDIR:-/tmp}/core-loop-key2id.XXXXXX")"
trap 'rm -f "$KEY2ID"' EXIT

n="$(jq '.seeds | length' "$SEEDS")"
for i in $(seq 0 $((n-1))); do
    key="$(jq -r ".seeds[$i].key" "$SEEDS")"
    title="$(jq -r ".seeds[$i].title" "$SEEDS")"
    body="$(jq -r ".seeds[$i].body" "$SEEDS")"
    labels="$(jq -r ".seeds[$i].labels | join(\",\")" "$SEEDS")"
    # D2: per-bead branch targeting. When the seed carries target_branch, (a) create/reset
    # that branch off main in the SCRATCH clone BEFORE the bead exists (the daemon does NOT
    # create it — it does `git rev-parse <b>` and reopens the bead if absent), and (b) append
    # a ## Branching fenced-yaml block to the description so resolveBranching lands the task
    # there instead of main (BI-009b). Idempotent (branch -f resets to the current main tip).
    tb="$(jq -r ".seeds[$i].target_branch // empty" "$SEEDS")"
    if [ -n "$tb" ]; then
        git -C "$SCRATCH" branch -f "$tb" main \
            || { echo "seed '$key': failed to create/reset branch '$tb' off main (does main exist in $SCRATCH?)" >&2; exit 1; }
        body="$(printf '%s\n\n## Branching\n\n```yaml\ntarget_branch: %s\n```\n' "$body" "$tb")"
        echo "[core-loop-seed] $key -> lands on branch '$tb' (created/reset off main)"
    fi
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
