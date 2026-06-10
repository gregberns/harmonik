package daemon_test

// workloop_claim_stale_blocker_hkrnsjs_test.go — unit tests for
// autoCloseStaleBlockersOnClaimFailure (hk-rnsjs).
//
// Verifies that when ClaimBead fails and the target bead is "blocked", the
// daemon auto-closes any blocker beads whose implementations are already
// subsumed in main so the next workloop retry can succeed.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	daemon "github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// hkrnsjs_showLedger is a minimal beadLedger stub for hk-rnsjs tests.
// ShowBead returns the configured record for known IDs; all other methods no-op.
type hkrnsjs_showLedger struct {
	records map[core.BeadID]core.BeadRecord
}

func (l *hkrnsjs_showLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

func (l *hkrnsjs_showLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if r, ok := l.records[id]; ok {
		return r, nil
	}
	return core.BeadRecord{}, brcli.ErrBeadNotFound
}

func (l *hkrnsjs_showLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *hkrnsjs_showLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *hkrnsjs_showLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// sweepCloseRecorder implements lifecycle.BeadCat3cCloser and records which
// bead IDs were passed to SweepCloseBead.
type sweepCloseRecorder struct {
	mu     sync.Mutex
	closed []core.BeadID
}

func (r *sweepCloseRecorder) SweepCloseBead(_ context.Context, _ brcli.TimeoutConfig, beadID core.BeadID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = append(r.closed, beadID)
	return nil
}

func (r *sweepCloseRecorder) closedIDs() []core.BeadID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.BeadID, len(r.closed))
	copy(out, r.closed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestAutoCloseStaleBlockersOnClaimFailure_ClosesSubsumedBlocker verifies that
// when the target bead is "blocked" by a blocker already subsumed in main,
// SweepCloseBead is called for that blocker.
func TestAutoCloseStaleBlockersOnClaimFailure_ClosesSubsumedBlocker(t *testing.T) {
	dir := t.TempDir()
	hkrnsjs_gitSetup(t, dir, "hk-blocker1")

	const targetID core.BeadID = "hk-target"
	const blockerID core.BeadID = "hk-blocker1"

	targetRecord := core.BeadRecord{
		BeadID:        targetID,
		Title:         "target bead",
		BeadType:      "task",
		Status:        core.CoarseStatusBlocked,
		AuditTrailRef: string(targetID),
		Edges: []core.DependencyEdge{
			{
				FromBeadID: blockerID,
				ToBeadID:   targetID,
				EdgeKind:   core.EdgeKindBlocks,
			},
		},
	}

	bus := &stubEventCollector{}
	ledger := &hkrnsjs_showLedger{
		records: map[core.BeadID]core.BeadRecord{targetID: targetRecord},
	}
	recorder := &sweepCloseRecorder{}

	params := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         dir,
		HandlerBinary:      "echo",
		IntentLogDir:       t.TempDir(),
		StaleBlockerCloser: recorder,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
	}

	daemon.ExportedAutoCloseStaleBlockersOnClaimFailure(context.Background(), params, targetID)

	got := recorder.closedIDs()
	if len(got) != 1 || got[0] != blockerID {
		t.Errorf("SweepCloseBead calls: want [%s], got %v", blockerID, got)
	}
}

// TestAutoCloseStaleBlockersOnClaimFailure_SkipsIfNotBlocked verifies that when
// the bead is not in "blocked" status, no SweepCloseBead calls are made.
func TestAutoCloseStaleBlockersOnClaimFailure_SkipsIfNotBlocked(t *testing.T) {
	dir := t.TempDir()
	hkrnsjs_gitSetup(t, dir, "hk-blocker2")

	const targetID core.BeadID = "hk-target2"
	const blockerID core.BeadID = "hk-blocker2"

	// Status is "open" even though a blocker is in main — auto-close must not fire.
	targetRecord := core.BeadRecord{
		BeadID:        targetID,
		Title:         "target bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		AuditTrailRef: string(targetID),
		Edges: []core.DependencyEdge{
			{FromBeadID: blockerID, ToBeadID: targetID, EdgeKind: core.EdgeKindBlocks},
		},
	}

	bus := &stubEventCollector{}
	ledger := &hkrnsjs_showLedger{records: map[core.BeadID]core.BeadRecord{targetID: targetRecord}}
	recorder := &sweepCloseRecorder{}

	params := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         dir,
		HandlerBinary:      "echo",
		IntentLogDir:       t.TempDir(),
		StaleBlockerCloser: recorder,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
	}

	daemon.ExportedAutoCloseStaleBlockersOnClaimFailure(context.Background(), params, targetID)

	if got := recorder.closedIDs(); len(got) != 0 {
		t.Errorf("SweepCloseBead: expected no calls for non-blocked bead, got %v", got)
	}
}

// TestAutoCloseStaleBlockersOnClaimFailure_SkipsBlockerNotInMain verifies that
// when the bead is blocked but the blocker has NOT been merged to main, no
// SweepCloseBead call is made.
func TestAutoCloseStaleBlockersOnClaimFailure_SkipsBlockerNotInMain(t *testing.T) {
	dir := t.TempDir()
	hkrnsjs_gitSetupEmpty(t, dir) // commit without "Refs: hk-blocker3" trailer

	const targetID core.BeadID = "hk-target3"
	const blockerID core.BeadID = "hk-blocker3"

	targetRecord := core.BeadRecord{
		BeadID:        targetID,
		Title:         "target bead",
		BeadType:      "task",
		Status:        core.CoarseStatusBlocked,
		AuditTrailRef: string(targetID),
		Edges: []core.DependencyEdge{
			{FromBeadID: blockerID, ToBeadID: targetID, EdgeKind: core.EdgeKindBlocks},
		},
	}

	bus := &stubEventCollector{}
	ledger := &hkrnsjs_showLedger{records: map[core.BeadID]core.BeadRecord{targetID: targetRecord}}
	recorder := &sweepCloseRecorder{}

	params := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         dir,
		HandlerBinary:      "echo",
		IntentLogDir:       t.TempDir(),
		StaleBlockerCloser: recorder,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
	}

	daemon.ExportedAutoCloseStaleBlockersOnClaimFailure(context.Background(), params, targetID)

	if got := recorder.closedIDs(); len(got) != 0 {
		t.Errorf("SweepCloseBead: expected no calls when blocker not in main, got %v", got)
	}
}

// TestAutoCloseStaleBlockersOnClaimFailure_NoOpWhenCloserNil verifies that when
// staleBlockerCloser is nil (the default for test stubs), the function is a
// no-op and does not panic.
func TestAutoCloseStaleBlockersOnClaimFailure_NoOpWhenCloserNil(t *testing.T) {
	const targetID core.BeadID = "hk-noop"

	targetRecord := core.BeadRecord{
		BeadID:        targetID,
		Title:         "noop",
		BeadType:      "task",
		Status:        core.CoarseStatusBlocked,
		AuditTrailRef: string(targetID),
	}
	bus := &stubEventCollector{}
	ledger := &hkrnsjs_showLedger{records: map[core.BeadID]core.BeadRecord{targetID: targetRecord}}

	params := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         t.TempDir(),
		HandlerBinary:      "echo",
		IntentLogDir:       t.TempDir(),
		StaleBlockerCloser: nil, // nil = disabled; must not panic
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
	}

	daemon.ExportedAutoCloseStaleBlockersOnClaimFailure(context.Background(), params, targetID)
	// No panic = pass.
}

// ─────────────────────────────────────────────────────────────────────────────
// Git setup helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkrnsjs_gitSetup initialises a git repo in dir with a single commit on main
// that carries "Refs: <beadID>" so beadAlreadySubsumedInMain returns true for
// that bead.
func hkrnsjs_gitSetup(t *testing.T, dir string, beadID string) {
	t.Helper()
	hkrnsjs_git(t, dir, "init", "--initial-branch=main")
	hkrnsjs_git(t, dir, "config", "user.email", "test@test.com")
	hkrnsjs_git(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	hkrnsjs_git(t, dir, "add", ".")
	hkrnsjs_git(t, dir, "commit", "-m", "fix\n\nRefs: "+beadID)
}

// hkrnsjs_gitSetupEmpty initialises a git repo with a commit that does NOT
// reference any bead (so beadAlreadySubsumedInMain returns false for all IDs).
func hkrnsjs_gitSetupEmpty(t *testing.T, dir string) {
	t.Helper()
	hkrnsjs_git(t, dir, "init", "--initial-branch=main")
	hkrnsjs_git(t, dir, "config", "user.email", "test@test.com")
	hkrnsjs_git(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	hkrnsjs_git(t, dir, "add", ".")
	hkrnsjs_git(t, dir, "commit", "-m", "unrelated commit")
}

func hkrnsjs_git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
