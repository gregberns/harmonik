package daemon

// subsumptionevidence.go — what counts as proof that a bead's work already
// landed.
//
// # The question
//
// Two daemon paths must decide whether a bead's work is ALREADY on the branch
// the run lands on:
//
//   - the graph path, when an implementer exits without moving HEAD
//     (dot_cascade_core.go). A prior run may have merged the work, so the node
//     closes the bead as subsumed instead of hard-failing it.
//   - the claim-failure path, when `br` refuses a claim because a blocker bead
//     is still open although its work merged (scheduler.go).
//
// # What used to answer it, and why it was wrong (hk-1a7yb)
//
// Both paths called shared.MainHistoryHasRefsTrailer, which ran
//
//	git log main --format=%B --fixed-strings --grep "Refs: <bead-id>"
//
// and reported "landed" when any commit body carried that line. Four gaps, all
// measured on this repository:
//
//  1. The branch was the literal "main". The program's work sits on an
//     integration branch some 845 commits ahead of main, so no work this
//     program landed was visible to the probe. The claim-failure path failed the
//     other way round: a blocker that landed on the integration branch read as
//     still open.
//  2. A body mention was the whole of the evidence. The commit needed no diff
//     for the bead, no relation to the branch under test, and no survival: a
//     later revert still read as "landed".
//  3. Bead hk-2hfyt, a P1 fleet-down bug, closed on this evidence. The commit
//     that satisfied it was a whole-repo gofumpt run whose entire message was
//     "fmt: auto-format via gofumpt+gci" plus a "Refs: hk-2hfyt" line. The fix
//     never landed and is still absent.
//  4. The function's own doc comment already forbade the use ("never use this
//     as a standalone completion test"), and the trap fired anyway. The comment
//     was not a control.
//
// # What answers it now
//
// specs/beads-integration.md §4.7 BI-022 makes git authoritative for
// completion, and it names the evidence: a merge commit carrying
// `Harmonik-Bead-ID: <bead_id>`. The daemon writes that trailer itself when it
// lands a task branch (synthesizeMergeCommitMessage, WM-019), and a cherry-pick
// landing carries it over from the checkpoint commits. So the trailer marks
// work THE DAEMON MERGED, while a `Refs:` line marks a commit that NAMES a bead
// — which anyone may write, for any reason, at any time.
//
// lifecycle.GitMergeCommitScanner already reads that evidence for the orphan
// sweep's Cat 3c auto-close: exact trailer-value equality, a non-docs diff, the
// commit still an ancestor of the branch tip, and no later commit reverting it.
// This file routes both daemon paths onto that one definition rather than
// keeping a second, weaker one.
//
// # Direction of the remaining error
//
// Absent evidence is never read as "landed". A missed subsumption costs one
// re-dispatch of a bead that was already done. A false subsumption closes an
// open bug and tells everyone it is fixed. The first is cheap and self-
// correcting, the second is silent, so every failure here — no branch, no
// repository, a git error — answers "not landed".
//
// Bead: hk-1a7yb. Superseded: shared.MainHistoryHasRefsTrailer (deleted).

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// beadWorkLandedOn reports whether beadID's work is present on branch in
// repoDir, on the BI-022 evidence described above.
//
// branch is the branch under test — the run's resolved lands_on, or the
// daemon-wide target branch — and never a literal. An empty branch or repoDir
// yields false: with no branch to ask about there is no evidence, and probing a
// default would ask about the wrong branch, which is the defect this replaced.
func beadWorkLandedOn(ctx context.Context, repoDir, branch string, beadID core.BeadID) bool {
	if repoDir == "" || branch == "" {
		return false
	}
	scanner := lifecycle.GitMergeCommitScanner{ProjectDir: repoDir, TargetBranch: branch}
	landed, err := scanner.HasMergeCommitForBead(ctx, beadID)
	if err != nil {
		return false
	}
	return landed
}
