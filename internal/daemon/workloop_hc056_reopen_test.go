package daemon_test

// workloop_hc056_reopen_test.go — HC-056 timeout → ReopenBead → re-pickup (hk-kqdpf.8).
//
// Verifies the single-mode workloop HC-056 path end-to-end:
//  1. HC-056 timeout fires (adapter never fires DetectReady; handler sleeps).
//  2. ReopenBead is called, transitioning the bead back to open.
//  3. The outer poll loop re-picks the bead up on the next tick (second ClaimBead).
//
// The "re-pickup" assertion is the critical invariant: without it, a stuck
// in_progress bead would halt the daemon forever (Ready only returns open beads).
//
// Helper prefix: hc056Reopen (per implementer-protocol.md §Helper-prefix discipline;
// bead hk-kqdpf.8).
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead: hk-kqdpf.8.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hc056ReopenProjectDir creates the minimal project directory tree for this test.
func hc056ReopenProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("hc056ReopenProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("hc056ReopenProjectDir: mkdir beads-intents: %v", err)
	}
	return dir
}

// hc056ReopenAdapter is a minimal Adapter stub whose DetectReady always returns
// false. Used to force the HC-056 timeout path.
type hc056ReopenAdapter struct{}

func (a *hc056ReopenAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (a *hc056ReopenAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}
func (a *hc056ReopenAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (a *hc056ReopenAdapter) RotateAccount(_ context.Context) error { return nil }
func (a *hc056ReopenAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// hc056ReopenMakeRegistry constructs a sealed AdapterRegistry with the given
// adapter registered for claude-code.
func hc056ReopenMakeRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, &hc056ReopenAdapter{}); err != nil {
		t.Fatalf("hc056ReopenMakeRegistry: Register: %v", err)
	}
	return reg
}

// hc056ReopenLedger is a stub beadLedger that:
//   - Returns the seeded bead from Ready on the first call.
//   - After ReopenBead is called, returns the bead again on the next Ready call
//     (simulating the bead transitioning back to open).
//   - Records all ClaimBead and ReopenBead calls for assertions.
//
// This models the correct bead lifecycle: HC-056 → ReopenBead → re-pickup.
type hc056ReopenLedger struct {
	mu sync.Mutex

	beadID core.BeadID

	// readyQueue holds bead IDs to hand out from Ready.
	readyQueue []core.BeadID

	// claimCount records how many times ClaimBead was called.
	claimCount int

	// reopenCount records how many times ReopenBead was called.
	reopenCount int

	// claimedSecond is closed when the second ClaimBead call arrives.
	claimedSecond chan struct{}
	secondOnce    sync.Once
}

func newHC056ReopenLedger(beadID core.BeadID) *hc056ReopenLedger {
	return &hc056ReopenLedger{
		beadID:        beadID,
		readyQueue:    []core.BeadID{beadID}, // seed initial dispatch
		claimedSecond: make(chan struct{}),
	}
}

func (l *hc056ReopenLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.readyQueue) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := l.readyQueue[0]
	l.readyQueue = l.readyQueue[1:]
	return []core.BeadRecord{{BeadID: id, Status: core.CoarseStatusOpen}}, nil
}

func (l *hc056ReopenLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	// Stub always reports "open" — pre-claim guard passes unconditionally.
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *hc056ReopenLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.mu.Lock()
	l.claimCount++
	count := l.claimCount
	l.mu.Unlock()

	if count >= 2 {
		// Signal the test: second dispatch observed → re-pickup confirmed.
		l.secondOnce.Do(func() { close(l.claimedSecond) })
	}
	return nil
}

func (l *hc056ReopenLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *hc056ReopenLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	l.mu.Lock()
	l.reopenCount++
	// Re-enqueue the bead so the outer poll loop re-picks it up on the next
	// Ready call. This is the core invariant: ReopenBead must result in the
	// bead being dispatchable again.
	l.readyQueue = append(l.readyQueue, beadID)
	l.mu.Unlock()
	return nil
}

func (l *hc056ReopenLedger) getReopenCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenCount
}

func (l *hc056ReopenLedger) getClaimCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claimCount
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_HC056Timeout_ReopenAndRepickup verifies the end-to-end HC-056
// single-mode path:
//  1. The adapter never fires DetectReady, so the 50ms timeout elapses.
//  2. ReopenBead is called — the stub ledger records this and re-enqueues the bead.
//  3. The poll loop re-picks the bead up on the next Ready call — confirmed by
//     a second ClaimBead call arriving within the test deadline.
//
// The handler binary is `/bin/sh -c "sleep 10"` — it sleeps well beyond the
// 50ms timeout so ErrAgentReadyTimeout fires before the handler exits.
//
// Acceptance criteria (bead hk-kqdpf.8):
//   - ReopenBead called at least once after HC-056 timeout.
//   - Second ClaimBead observed (re-pickup confirmed).
//   - No panic; loop exits cleanly on context cancellation.
func TestWorkLoop_HC056Timeout_ReopenAndRepickup(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hc056-reopen-test-bead-001")

	projectDir := hc056ReopenProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	ledger := newHC056ReopenLedger(beadID)
	collector := &stubEventCollector{}
	reg := hc056ReopenMakeRegistry(t)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               collector,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "sleep 10"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:  reg,
		AgentReadyTimeout: 50 * time.Millisecond,
	})

	// The test deadline is generous: 50ms timeout + poll interval (2s) + margin.
	// In practice the second ClaimBead should arrive within ~2.1s.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the second ClaimBead (re-pickup confirmed) or test timeout.
	select {
	case <-ledger.claimedSecond:
		// Re-pickup observed — cancel the loop.
		cancel()
	case <-ctx.Done():
		t.Errorf("timed out waiting for re-pickup: claimCount=%d reopenCount=%d",
			ledger.getClaimCount(), ledger.getReopenCount())
		// Let loop drain.
	}

	// Wait for the loop goroutine to exit (should be fast after cancel).
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("work loop did not exit within 5s after context cancellation")
	}

	// Assertions: at least one ReopenBead and at least two ClaimBead calls.
	if reopens := ledger.getReopenCount(); reopens < 1 {
		t.Errorf("ReopenBead call count = %d; want ≥ 1", reopens)
	}
	if claims := ledger.getClaimCount(); claims < 2 {
		t.Errorf("ClaimBead call count = %d; want ≥ 2 (first dispatch + re-pickup)", claims)
	}

	t.Logf("HC-056 reopen+repickup OK: claimCount=%d reopenCount=%d",
		ledger.getClaimCount(), ledger.getReopenCount())
}
