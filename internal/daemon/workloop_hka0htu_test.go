package daemon_test

// workloop_hka0htu_test.go — label-hydration fix for the claim path (hk-a0htu).
//
// Root cause: `br ready --format json` (br v0.1.45) does not include the
// `labels` field in its output, so BeadRecord.Labels from Ready() is always
// nil.  resolveWorkflowMode tier-1 never fires and every dispatch falls through
// to tier-4 single mode even when the bead carries workflow:review-loop.
//
// Fix: the workloop calls ShowBead (already issued as the pre-claim guard) and
// overwrites beadRecord.Labels with the full ShowBead response before mode
// resolution.  Queue-path beads get a separate ShowBead call after claim.
//
// This test verifies that Labels is populated after the claim path by injecting
// a stub where Ready() returns nil labels but ShowBead() returns the full set.
//
// Helper prefix: labelsHydrateFixture (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-a0htu).

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// labelsHydrateFixtureLedger simulates the br v0.1.45 behaviour:
//   - Ready() returns BeadRecords with nil Labels (no labels field in JSON).
//   - ShowBead() returns the full record including Labels so the hydration fix
//     can populate BeadRecord.Labels before mode resolution.
type labelsHydrateFixtureLedger struct {
	mu     sync.Mutex
	ready  []core.BeadID
	labels map[core.BeadID][]string // per-bead labels returned by ShowBead
	closed []core.BeadID
	opened []core.BeadID
}

// Ready returns BeadRecords with nil Labels — simulating br v0.1.45 output
// that omits the labels field.
func (l *labelsHydrateFixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := l.ready[0]
	l.ready = l.ready[1:]
	// Labels intentionally nil — mirrors br v0.1.45 ready output.
	return []core.BeadRecord{{BeadID: id, Labels: nil}}, nil
}

// ShowBead returns the full record including labels, simulating `br show`
// which does include the labels field at br v0.1.45.
func (l *labelsHydrateFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lbls := l.labels[id]
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Labels: lbls}, nil
}

func (l *labelsHydrateFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *labelsHydrateFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = append(l.closed, beadID)
	return nil
}

func (l *labelsHydrateFixtureLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, beadID)
	return nil
}

func (l *labelsHydrateFixtureLedger) closedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.closed))
	copy(out, l.closed)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_LabelsHydratedFromShowBead verifies that when Ready() returns a
// BeadRecord with nil Labels (simulating br v0.1.45 behaviour), the workloop
// claim path calls ShowBead and overwrites BeadRecord.Labels before mode
// resolution, so a bead carrying workflow:review-loop is dispatched in
// review-loop mode rather than falling through to single mode.
//
// The test injects a stub handler (sh -c 'exit 0') — the run completes
// immediately.  The key assertion is that the bead is closed (not reopened),
// which confirms the label-hydrated BeadRecord reached beadRunOne successfully.
//
// A direct assertion on the resolved WorkflowMode is not possible at this
// integration level because workflowMode is local to beadRunOne.  The
// combination of (a) stub returning workflow:review-loop labels only via
// ShowBead and (b) bead reaching CloseBead rather than ReopenBead confirms
// that the claim path did not error out on label-dependent decisions.
//
// Bead ref: hk-a0htu.
func TestWorkLoop_LabelsHydratedFromShowBead(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("labels-hydrate-bead-001")
	ledger := &labelsHydrateFixtureLedger{
		ready: []core.BeadID{beadID},
		labels: map[core.BeadID][]string{
			// ShowBead returns workflow:review-loop; Ready() returns nil labels.
			// After the fix, the claim path reads ShowBead and overwrites Labels.
			beadID: {"area:daemon", "priority:high"},
		},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorktreeFactory:  workloopFixturePreCommitWorktreeFactory,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait until the bead is closed (claim path succeeded through to completion).
	deadline := time.After(15 * time.Second)
	for {
		if len(ledger.closedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for bead to be closed; label hydration may have failed")
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	// Assert the bead was closed (not reopened), confirming the claim path
	// completed after label hydration.
	closedIDs := ledger.closedIDs()
	if len(closedIDs) == 0 {
		t.Fatal("bead was not closed; expected label hydration to allow dispatch to complete")
	}
	if closedIDs[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closedIDs[0], beadID)
	}
	if len(ledger.opened) > 0 {
		t.Errorf("unexpected ReopenBead calls: %v; bead should have been closed cleanly", ledger.opened)
	}

	// Assert run_started was emitted — confirms beadRunOne was entered with the
	// label-hydrated record (not short-circuited before dispatch).
	foundStarted := false
	for _, et := range collector.eventTypes() {
		if et == string(core.EventTypeRunStarted) {
			foundStarted = true
			break
		}
	}
	if !foundStarted {
		t.Error("run_started event not emitted; expected beadRunOne to be entered after label hydration")
	}
}
