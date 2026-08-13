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
[ -d "$SCRATCH" ] || { echo "scratch path is not a directory: $SCRATCH (run scratch-daemon.sh init first)" >&2; exit 2; }

# Resolve the scratch path to its absolute, symlink-free spelling BEFORE anything reads it.
#
# scripts/scratch-daemon.sh puts every path it records through guard_path, which canonicalizes
# with `pwd -P`. On macOS /tmp is a symlink to /private/tmp, so scratch-daemon.sh writes the
# origin URL as /private/tmp/h/core-loop-lt/.harmonik/scratch-origin.git. This script took $1
# raw, and the Makefile's shipped default is LT_SCRATCH ?= /tmp/h/core-loop-lt. The two spelled
# the same directory two ways, so the "origin is already isolated" test below could never match
# and the script silently re-pointed origin off the bare repository scratch-daemon.sh manages.
# One `pwd -P` here makes both sides spell the path identically. That is the same defect this
# change exists to fix: a reader and a writer with private copies of one fact.
SCRATCH="$(cd "$SCRATCH" && pwd -P)" \
    || { echo "cannot resolve scratch path to an absolute directory: $1" >&2; exit 2; }
[ -n "$SCRATCH" ] || { echo "scratch path resolved to an empty string: $1" >&2; exit 2; }

# scratch-versus-fleet guard. Everything below rewrites the repository it is pointed at:
# `git remote set-url origin`, `git branch -f`, and a force-push. What separates a scratch
# from a live checkout is AUTHORSHIP, not gitignore: `harmonik init` writes config.yaml for
# every live project, while only scripts/scratch-daemon.sh cmd_init writes audit-revision
# (scratch_revfile), and it writes it for every scratch it prepares. So the presence of
# audit-revision means scratch-daemon.sh built this tree. Refuse when it is absent, so a
# hand-run against a live project cannot silently re-point that clone's origin.
[ -f "$SCRATCH/.harmonik/audit-revision" ] || {
    echo "refusing: $SCRATCH has no .harmonik/audit-revision, so scratch-daemon.sh init did not prepare it." >&2
    echo "This script rewrites origin and force-pushes branches. Point it at a scratch clone, never a live harmonik checkout." >&2
    exit 2
}
[ -d "$SCRATCH/.beads" ] || { echo "scratch has no .beads dir: $SCRATCH (run scratch-daemon.sh init first)" >&2; exit 2; }

# The ref the daemon cuts every run worktree from. READ IT FROM THE DAEMON'S OWN CONFIG.
#
# This script used to write down its own copy of that ref — the literal `main` — and cut each
# landing branch from it. The two disagree in every scratch clone. scripts/scratch-daemon.sh
# isolate_push_target rewrites defaults.start_from to `scratch/main`, which it pins to the
# audited commit; the local `main` that provision_matrix_config creates comes from origin/main,
# which is the FLEET's main and is hundreds of commits away. So the run worktree started at the
# audited commit and the landing branch started at the fleet's main. The landing rebase
# (internal/runmerge/merge.go prepareRebase) then had to replay the whole divergence, it
# conflicted across internal/daemon, and every run ended `merge-failed: rebase_conflict`.
# Measured on a real clone of this repo: 932 commits to replay, 27 the other way, 7 conflicted
# files on the first collision.
#
# Reading the value keeps one owner for it. The daemon's precedence is bead > project defaults >
# spec default (internal/daemon/branching.go resolveBranchingFrom); a seed sets only
# target_branch, which is LandsOn, so StartFrom always comes from this file.
#
# Every failure here is fatal. A silent fallback to `main` is how the defect above survived.
BRANCHING="$SCRATCH/.harmonik/branching.yaml"
[ -f "$BRANCHING" ] || { echo "no branching config at $BRANCHING — this scratch was not prepared by scratch-daemon.sh init, so the ref the daemon starts runs from cannot be read" >&2; exit 2; }
BASE_REF="$(awk '/^[[:space:]]+start_from:/ { print $2; exit }' "$BRANCHING")"
[ -n "$BASE_REF" ] || { echo "$BRANCHING has no defaults.start_from — refusing to guess the ref the daemon starts runs from" >&2; exit 2; }
git -C "$SCRATCH" rev-parse --verify --quiet "${BASE_REF}^{commit}" >/dev/null \
    || { echo "defaults.start_from is '$BASE_REF' but that ref does not resolve in $SCRATCH — the daemon cannot start a run from it either" >&2; exit 2; }
echo "[core-loop-seed] daemon start_from = '$BASE_REF' ($(git -C "$SCRATCH" rev-parse --short "$BASE_REF")) — landing branches are cut from this ref"

# D2: isolate the scratch's `origin` from the fleet clone. The daemon's landing does
# `git push origin <target_branch>` (workloop.go mergeRunBranchToMain), and a scratch cloned
# from the fleet has origin = the fleet repo, which carries stale core-loop-proof-* branches
# from prior sessions. Pushing a fresh integration branch there non-FF-rejects, the daemon
# rebases the run's commit onto the STALE tip, hits a content conflict on the appended line,
# drops the commit, and leaves the branch at the stale SHA — a FALSE landing (the runner sees
# a branch "advance" that is not this run's change). Re-point origin at a throwaway bare repo
# seeded with only the daemon's start_from ref, so every integration-branch push is a clean
# fast-forward and the fleet's refs are never touched. Gated on any target_branch seed;
# idempotent.
#
# scripts/scratch-daemon.sh isolate_push_target now does this at init time, and it does it
# better: its bare repo carries the start_from ref the daemon was configured with. When that is
# already in place, LEAVE IT. Building a second bare on top of it dropped that ref and left the
# push target holding only a stale `main`, which is the same "keep a private copy of another
# component's fact" mistake this block is being fixed for.
if jq -e '[.seeds[] | select(.target_branch != null)] | length > 0' "$SEEDS" >/dev/null 2>&1; then
    CUR_ORIGIN="$(git -C "$SCRATCH" remote get-url origin 2>/dev/null || true)"
    case "$CUR_ORIGIN" in
        "$SCRATCH/.harmonik/"*)
            echo "[core-loop-seed] origin already isolated by scratch-daemon.sh -> $CUR_ORIGIN"
            ;;
        *)
            ORIGIN_BARE="$SCRATCH/.harmonik/matrix-origin.git"
            [ -d "$ORIGIN_BARE" ] || git init --quiet --bare "$ORIGIN_BARE"
            git -C "$SCRATCH" push --quiet --force "$ORIGIN_BARE" "$BASE_REF:refs/heads/$BASE_REF" \
                || { echo "failed to seed the throwaway origin '$ORIGIN_BARE' with '$BASE_REF' — refusing to point the daemon's landing pushes at an origin that lacks its own base ref" >&2; exit 1; }
            git -C "$SCRATCH" remote set-url origin "$ORIGIN_BARE"
            git -C "$SCRATCH" fetch --quiet origin 2>/dev/null || true
            echo "[core-loop-seed] isolated origin -> $ORIGIN_BARE (seeded with '$BASE_REF'; daemon landings push here, never the fleet)"
            ;;
    esac
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
        # UNTRACKED IS NOT ENOUGH — IT ALSO HAS TO BE IGNORED (hk-48zdw).
        # Go's build stamp is whole-tree `git status`, and that counts untracked
        # files. This copy therefore dirtied the very tree the gate pins: every
        # binary built after it carried vcs.modified=true, `harmonik version
        # --binary --contains <rev>` refused it with exit 3, and the assessor
        # contract voids any result from such a binary. The repo's own .gitignore
        # carries a root-anchored `/review-loop.dot` for that reason — ignored is
        # invisible to the stamp, and is still untouched by the `git reset --hard
        # HEAD` a landing runs on the project dir, which is why a TRACKED file
        # cannot serve here.
        #
        # This WARNS rather than fails, because a scratch pinned to a commit from
        # before that .gitignore entry legitimately has no such rule, and auditing
        # an old commit must stay possible. The refusal belongs to the gate, which
        # reads the stamp of the binary that actually got built:
        # `scratch-daemon.sh provenance`.
        if ! git -C "$SCRATCH" check-ignore -q -- review-loop.dot 2>/dev/null; then
            echo "[core-loop-seed] WARNING: review-loop.dot is NOT ignored in this scratch, so it will make the tree dirty and Go will stamp vcs.modified=true on the gate binary." >&2
            echo "[core-loop-seed]   No result from that binary is an audit of the pinned commit, and 'scratch-daemon.sh provenance' will refuse it." >&2
            echo "[core-loop-seed]   Expected: a root-anchored '/review-loop.dot' line in the .gitignore of the revision under audit (added for hk-48zdw)." >&2
        fi
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
    # D2: per-bead branch targeting. When the seed carries target_branch, (a) create/reset that
    # branch at the daemon's start_from ref in the SCRATCH clone BEFORE the bead exists (the
    # daemon does NOT create it — it does `git rev-parse <b>` and reopens the bead if absent),
    # and (b) append a ## Branching fenced-yaml block to the description so resolveBranching
    # lands the task there instead of on the project default (BI-009b). Idempotent (branch -f
    # resets to the current start_from tip).
    #
    # The branch MUST be cut from the same ref the run worktree is cut from. The landing rebases
    # the run branch onto this branch, so any gap between the two is replayed commit by commit.
    # $BASE_REF is read from the daemon's branching.yaml above for exactly that reason.
    tb="$(jq -r ".seeds[$i].target_branch // empty" "$SEEDS")"
    if [ -n "$tb" ]; then
        git -C "$SCRATCH" branch -f "$tb" "$BASE_REF" \
            || { echo "seed '$key': failed to create/reset branch '$tb' at '$BASE_REF' in $SCRATCH" >&2; exit 1; }
        # Publish it, so origin's copy cannot be a leftover from an earlier run. The landing
        # pushes this branch (internal/runmerge/merge.go gitPushOrigin), and a `scratch-daemon.sh
        # init --reuse` keeps the bare origin under .harmonik. A stale branch there rejects the
        # push as non-fast-forward, and the daemon then rebases the run onto the stale tip.
        git -C "$SCRATCH" push --quiet --force origin "$tb:refs/heads/$tb" \
            || { echo "seed '$key': failed to publish branch '$tb' to origin — the landing push would meet an origin this script could not place" >&2; exit 1; }
        body="$(printf '%s\n\n## Branching\n\n```yaml\ntarget_branch: %s\n```\n' "$body" "$tb")"
        echo "[core-loop-seed] $key -> lands on branch '$tb' (created/reset at '$BASE_REF', published to origin)"
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
