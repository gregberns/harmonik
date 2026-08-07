package daemon_test

// dot_node_subsumption_evidence_test.go — what the graph path accepts as proof
// that a bead's work already landed.
//
// When an implementer exits without moving HEAD, the node may close the bead as
// subsumed instead of hard-failing it. The evidence for that close was a commit
// message on the literal branch `main` carrying a "Refs: <bead-id>" line, and
// nothing else. Anyone may write that line, for any reason.
//
// Measured on this repository: bead hk-2hfyt, a P1 fleet-down bug, closed as
// done on 2026-07-12. The commit that satisfied the probe was a whole-repo
// gofumpt run whose entire message was "fmt: auto-format via gofumpt+gci" plus
// "Refs: hk-2hfyt". The fix never landed. A clone of this repository carries
// hundreds of bead identifiers in its history, so re-dispatching any named bead
// to an agent that did nothing read as success.
//
// The probe also asked the wrong branch: this program merges to an integration
// branch, and `main` is 845 commits behind it.
//
// Bead: hk-1a7yb.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// dotFixtureLandsOnBody is a bead body that declares a lands_on branch (bead
// YAML key `target_branch`, BI-009b) without declaring a cross-repo target.
func dotFixtureLandsOnBody(branch string) string {
	return "## Summary\n\nWork that lands on an integration branch.\n\n" +
		"## Branching\n\n```yaml\ntarget_branch: " + branch + "\n```\n"
}

// dotFixtureMentionOnlyCommit reproduces the commit that closed hk-2hfyt: a
// mechanical whole-repo reformat that names the bead and does none of its work.
//
// It is deliberately NOT a docs-only commit. The diff touches a .go file, so a
// "did it change anything real" check passes on it. Naming a bead is the only
// thing this commit does that relates to the bead.
func dotFixtureMentionOnlyCommit(t *testing.T, dir string, bead core.BeadID) {
	t.Helper()
	path := filepath.Join(dir, "formatted.go")
	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(path, []byte("package fixture\n\nvar Reformatted = true\n"), 0o644); err != nil {
		t.Fatalf("dotFixtureMentionOnlyCommit: write: %v", err)
	}
	dotFixtureGit(t, dir, "add", "formatted.go")
	dotFixtureGit(t, dir, "commit", "-m", "fmt: auto-format via gofumpt+gci\n\nRefs: "+string(bead))
}

// TestDotNode_MentionOnlyCommitIsNotEvidenceOfCompletion is the claim.
//
// The implementer does nothing at all. History already carries a commit that
// names the bead and does not do its work. The node must NOT close the bead.
//
// This test fails against the former probe, which read the "Refs:" line on main
// as completion and closed the bead.
func TestDotNode_MentionOnlyCommitIsNotEvidenceOfCompletion(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-1a7yb-mention-only")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureNoCommitHandler(t),
		BeforeRun: func(t *testing.T, projectDir string) {
			dotFixtureMentionOnlyCommit(t, projectDir, beadID)
		},
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was closed as done although the implementer did nothing and the only commit naming it is a whole-repo reformat (closed=%v).\n"+
			"A commit that NAMES a bead is not a commit that DOES its work. This is how hk-2hfyt, a P1 fleet-down bug, closed on a gofumpt run.",
			beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened after an implementer that did nothing; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_SubsumptionAsksTheBranchTheRunLandsOn proves the branch comes
// from the run's own resolved lands_on and not from a literal.
//
// The bead's work is merged on `integration`, which is where this bead lands.
// `main` carries none of it. The node must find the work.
//
// This test fails against the former probe, which ran `git log main` whatever
// branch the run landed on.
func TestDotNode_SubsumptionAsksTheBranchTheRunLandsOn(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-1a7yb-lands-on-integration")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript:   dotFixtureNoCommitHandler(t),
		BeadDescription: dotFixtureLandsOnBody("integration"),
		BeforeRun: func(t *testing.T, projectDir string) {
			// The work merges on the integration branch. main never sees it,
			// which is the state of this repository.
			dotFixtureGit(t, projectDir, "checkout", "-b", "integration")
			dotFixtureLandSubsumedCommit(t, projectDir, beadID)
			dotFixtureGit(t, projectDir, "checkout", "main")
		},
	})

	if reopened := res.Ledger.reopenedIDs(); len(reopened) > 0 {
		t.Errorf("bead %s was reopened although its work is merged on the branch it lands on (reopened=%v).\n"+
			"The node asked a branch the run does not land on.", beadID, reopened)
	}
	if closed := res.Ledger.closedIDs(); len(closed) == 0 {
		t.Errorf("bead %s was not closed subsumed; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestDotNode_RevertedWorkIsNotEvidenceOfCompletion covers the third gap: the
// work landed and a later commit took it back out. The bead is not done.
func TestDotNode_RevertedWorkIsNotEvidenceOfCompletion(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-1a7yb-reverted")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HandlerScript: dotFixtureNoCommitHandler(t),
		BeforeRun: func(t *testing.T, projectDir string) {
			dotFixtureLandSubsumedCommit(t, projectDir, beadID)
			dotFixtureGit(t, projectDir, "revert", "--no-edit", "HEAD")
		},
	})

	if closed := res.Ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("bead %s was closed as done although its landed work was reverted (closed=%v)", beadID, closed)
	}
	if reopened := res.Ledger.reopenedIDs(); len(reopened) == 0 {
		t.Errorf("bead %s was not reopened; events=%v", beadID, res.Bus.eventTypes())
	}
}

// TestBeadWorkLandedOn_BranchIsRequiredAndIsNeverDefaulted pins the rule the
// call sites depend on: the probe answers about the branch it is given, and an
// unnamed branch is not an invitation to pick one.
//
// Without this, a later "convenience" default would restore the defect without
// changing a single call site.
func TestBeadWorkLandedOn_BranchIsRequiredAndIsNeverDefaulted(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-1a7yb-branch-arg")
	repo := t.TempDir()
	workloopFixtureGitRepo(t, repo)
	dotFixtureGit(t, repo, "checkout", "-b", "integration")
	dotFixtureLandSubsumedCommit(t, repo, beadID)
	dotFixtureGit(t, repo, "checkout", "main")

	cases := []struct {
		name   string
		branch string
		want   bool
	}{
		{name: "the branch the work merged on", branch: "integration", want: true},
		{name: "a branch that never saw the work", branch: "main", want: false},
		{name: "no branch named", branch: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedBeadWorkLandedOn(t.Context(), repo, tc.branch, beadID)
			if got != tc.want {
				t.Errorf("beadWorkLandedOn(branch=%q) = %v, want %v", tc.branch, got, tc.want)
			}
		})
	}
}

// ── The claim-failure path ──────────────────────────────────────────────────
//
// The second caller of the same evidence: when `br` refuses a claim because a
// blocker bead is still open, the daemon looks for blockers whose work already
// merged and closes those stale records. It asked the same two wrong questions
// — the literal branch `main`, and a mention in a commit message — and it
// failed the other way round as well: a blocker that landed on the integration
// branch stayed invisible, so a ready bead read as blocked.

// blockedLedger reports one bead as blocked by a fixed edge list. Every other
// bead, and every other call, is the ordinary stub.
type blockedLedger struct {
	*stubBeadLedger
	blocked core.BeadID
	edges   []core.DependencyEdge
}

func (l *blockedLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if id != l.blocked {
		return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusBlocked, Edges: l.edges}, nil
}

// recordingCloser records every stale-blocker close the daemon asks for.
type recordingCloser struct {
	mu     sync.Mutex
	closed []core.BeadID
}

func (c *recordingCloser) SweepCloseBead(_ context.Context, _ brcli.TimeoutConfig, beadID core.BeadID) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = append(c.closed, beadID)
	return nil
}

func (c *recordingCloser) closedIDs() []core.BeadID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]core.BeadID(nil), c.closed...)
}

// TestAutoCloseStaleBlockers_AsksTheTargetBranchForRealEvidence drives the
// claim-failure path over two blockers that differ in one way each.
//
// The merged blocker's work is merged on the daemon's target branch,
// `integration`. Its ledger record is stale and must be closed. The former
// probe read `main`, which never saw the work, so this blocker stayed open and
// the bead it blocks could not be claimed.
//
// The named blocker is named by a commit on `main` that does none of its work.
// It must NOT be closed. The former probe closed it.
func TestAutoCloseStaleBlockers_AsksTheTargetBranchForRealEvidence(t *testing.T) {
	t.Parallel()

	const (
		blockedID = core.BeadID("hk-1a7yb-blocked")
		mergedID  = core.BeadID("hk-1a7yb-blocker-merged")
		namedID   = core.BeadID("hk-1a7yb-blocker-named-only")
	)

	projectDir := t.TempDir()
	workloopFixtureGitRepo(t, projectDir)
	// main names one blocker and does none of its work.
	dotFixtureMentionOnlyCommit(t, projectDir, namedID)
	// The other blocker's work is merged on the target branch.
	dotFixtureGit(t, projectDir, "checkout", "-b", "integration")
	dotFixtureLandSubsumedCommit(t, projectDir, mergedID)
	dotFixtureGit(t, projectDir, "checkout", "main")

	closer := &recordingCloser{}
	ledger := &blockedLedger{
		stubBeadLedger: &stubBeadLedger{},
		blocked:        blockedID,
		edges: []core.DependencyEdge{
			{FromBeadID: mergedID, ToBeadID: blockedID, EdgeKind: core.EdgeKindBlocks},
			{FromBeadID: namedID, ToBeadID: blockedID, EdgeKind: core.EdgeKindBlocks},
		},
	}

	daemon.ExportedAutoCloseStaleBlockersOnClaimFailure(t.Context(), daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                &stubEventCollector{},
		ProjectDir:         projectDir,
		TargetBranch:       "integration",
		HandlerBinary:      "/bin/true",
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		StaleBlockerCloser: closer,
	}, blockedID)

	closed := closer.closedIDs()
	var sawMerged, sawNamed bool
	for _, id := range closed {
		switch id {
		case mergedID:
			sawMerged = true
		case namedID:
			sawNamed = true
		}
	}
	if !sawMerged {
		t.Errorf("stale blocker %s was not closed although its work is merged on the target branch (closed=%v).\n"+
			"The path asked a branch this daemon does not target, so a merged blocker stays open and the bead it blocks cannot be claimed.",
			mergedID, closed)
	}
	if sawNamed {
		t.Errorf("blocker %s was closed although the only commit naming it does none of its work (closed=%v)", namedID, closed)
	}
}
