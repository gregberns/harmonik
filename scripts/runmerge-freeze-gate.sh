#!/usr/bin/env bash
set -euo pipefail

# runmerge-freeze-gate.sh — P2 unit E5 RT13 freeze tripwire
# (plans/2026-07-21-p2-extraction/E5-dot-runloop.md §5b; _plan.md §3.2).
#
# The run-branch MERGE PATH left internal/daemon in slice RT13: the EM-052/EM-053
# merge-to-main sequence (carved out of workloop.go), the pre-rebase worktree
# hygiene, the gofumpt/gci format gate, the run-context strip, and the review
# trailer amend now live in internal/runmerge. depguard fences the IMPORT edge
# (internal/runmerge must not import internal/daemon) but it cannot forbid
# CREATING a file, so this grep ratchet closes the other door: it fails the build
# if a merge-path-shaped file reappears in internal/daemon, if one of the moved
# symbols is re-declared there, or if the daemon re-acquires a raw
# `git merge` / `git rebase` exec.
#
# NAMED CARVE-OUTS — each is a real, non-merge-path use of a matching name:
#
#   internal/daemon/beadsmergedriver.go        — registers the `beads-union` git
#       MERGE DRIVER in .git/config (BL-MRG). It configures git; it does not
#       merge a run branch.
#   internal/daemon/scenariotest/concurrent_merge.go — a scenario-test harness
#       package (test support, not production merge code).
#   internal/daemon/branching.go               — squashLanding / cherryPickLanding
#       run `git merge --squash` on the WM-019b TASK-BRANCH landing path, which
#       is a different operation from the EM-052 run-branch merge-to-main and was
#       never part of the RT13 cut.
#   *_test.go                                  — the merge-to-main INTEGRATION
#       tests (mergetomain_hkftyvo and its ~14 siblings) drive the whole work
#       loop through daemon.ExportedRunWorkLoop and correctly stay in daemon.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No new merge-path source file in the daemon package (recursive — a
#     sub-package is the obvious evasion of a -maxdepth 1 scan).
while IFS= read -r f; do
    case "$f" in
        internal/daemon/beadsmergedriver.go|internal/daemon/scenariotest/concurrent_merge.go)
            continue ;;
    esac
    echo "runmerge-freeze-gate: FORBIDDEN merge-path file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name '*merge*.go' -o -name '*fmtgate*.go' \
                -o -name '*stripruncontext*.go' -o -name '*reviewtrailer*.go' \) \
             ! -name '*_test.go')

# (2) No re-declaration of the funcs that moved to internal/runmerge. The
#     alternation also catches a re-declaration inside a grouped `var ( … )` /
#     `const ( … )` block, which a bare ^(func|var|const) anchor misses.
for sym in mergeRunBranchToMain RunBranchToTarget inlineMergeSubmit InlineSubmit \
           isRetryableMergeReason IsRetryableReason emitOutcomeEmitted EmitOutcomeEmitted \
           isHarmonikChurn IsHarmonikChurn commitResidualDelta CommitResidualDelta \
           discardDirtyChurn DiscardDirtyChurn cleanUntrackedFiles CleanUntrackedFiles \
           checkMainWorkingTreeDirty CheckMainWorkingTreeDirty \
           snapshotUntrackedFiles SnapshotUntrackedFiles \
           parsePorcelainPaths ParsePorcelainPaths filterIgnoredPaths \
           stripRunContextFromMerge StripRunContextFromMerge \
           appendReviewTrailersToHEAD AppendReviewTrailersToHEAD \
           removeWorktree RemoveWorktree gitRevParse \
           resolveMergeTips prepareInitialMerge prepareRebase gitRebaseAbort \
           runMergeBuildGate runMergeFmtGate runMergeFmtCheck runFmtPassesOnce \
           fmtGofumptPass fmtGciPass commitFmtChanges readGoModule \
           commitAdvanceRef gitPushOrigin commitHandlePushFailure \
           commitFinalizeWorkingTree mergedCommitPaths refreshMergedPaths \
           locallyEditedPaths writeRecoveryPatch gitUpdateRefBestEffort \
           isMergeBuildColdCacheError; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(func|var|const)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "runmerge-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (3) No re-declaration of the types that moved.
for sym in mergeOutcome Outcome mergeSubmit Submit mergePrepareKind \
           commitOutcome commitAdvanceResult mergeRunBranchToMainPayload \
           workingTreeRefreshFailedPayload mergeBuildFailedPayload; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E "^[[:space:]]*(type[[:space:]]+)?${sym}\b[[:space:]]*(=|struct|interface|func\()" internal/daemon 2>/dev/null | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "runmerge-freeze-gate: FORBIDDEN re-declaration of type ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (4) The run path must not re-acquire a raw merge/rebase exec. Any new
#     `git merge` / `git rebase` exec in internal/daemon production code outside
#     the branching.go task-branch landing path is a regression of the carve-out.
MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E '"git"[^)]*"(merge|rebase)"' internal/daemon 2>/dev/null | grep -v '^internal/daemon/branching.go:' || true)"
if [ -n "$MATCHES" ]; then
    echo "runmerge-freeze-gate: FORBIDDEN git merge/rebase exec in internal/daemon — it belongs in internal/runmerge:" >&2
    printf '%s\n' "$MATCHES" >&2
    HITS=$((HITS + 1))
fi

# (5) The carve-outs must still exist. A gate whose exceptions have been renamed
#     away silently stops testing what it claims to test.
for f in internal/daemon/beadsmergedriver.go internal/daemon/branching.go; do
    if [ ! -f "$f" ]; then
        echo "runmerge-freeze-gate: carve-out target $f is gone — re-derive this gate's exception list" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "runmerge-freeze-gate: FAIL — the run-branch merge path was extracted in P2 E5 RT13; build on internal/runmerge, do not reopen internal/daemon" >&2
    exit 1
fi
echo "runmerge-freeze-gate: OK — the run-branch merge path stays out of internal/daemon"
