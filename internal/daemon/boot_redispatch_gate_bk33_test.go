package daemon_test

// boot_redispatch_gate_bk33_test.go — unit test for the post-boot re-dispatch
// gate introduced by hk-bk33.
//
// Asserts that runWorkLoop does NOT dispatch a bead before spawnSubstrateReadyCh
// closes, and DOES dispatch promptly once the channel is closed.
//
// Helper prefix: bk33Fixture (hk-bk33 namespace convention).
// Bead ref: hk-bk33.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (namespaced _bk33 per hk-bk33 helper discipline)
// ─────────────────────────────────────────────────────────────────────────────

// bk33FixtureLedger is a minimal bead ledger that exposes ClaimBead calls via
// claimedCh. Ready returns one bead on the first call, then empty thereafter.
type bk33FixtureLedger struct {
	mu        sync.Mutex
	remaining []core.BeadID
	claimedCh chan struct{} // closed on first ClaimBead call
	closeOnce sync.Once
}

func newBK33FixtureLedger_bk33(beadID core.BeadID) *bk33FixtureLedger {
	return &bk33FixtureLedger{
		remaining: []core.BeadID{beadID},
		claimedCh: make(chan struct{}),
	}
}

func (l *bk33FixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.remaining) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := l.remaining[0]
	l.remaining = l.remaining[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (l *bk33FixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *bk33FixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.closeOnce.Do(func() { close(l.claimedCh) })
	return nil
}

func (l *bk33FixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *bk33FixtureLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// bk33FixtureEventSink is a no-op EventEmitter satisfying handlercontract.EventEmitter.
type bk33FixtureEventSink_bk33 struct {
	count atomic.Int64
}

func (s *bk33FixtureEventSink_bk33) Emit(_ context.Context, _ core.EventType, _ []byte) error {
	s.count.Add(1)
	return nil
}

func (s *bk33FixtureEventSink_bk33) EmitWithRunID(_ context.Context, _ core.RunID, _ core.EventType, _ []byte) error {
	s.count.Add(1)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_PostBootReDispatchGate_bk33 verifies that runWorkLoop does NOT
// dispatch a bead before spawnSubstrateReadyCh closes, and DOES dispatch once
// the channel closes.
//
// This is the regression test for hk-bk33: after a restart-backoff boot the
// spawn substrate is not immediately ready, and re-dispatching QM-002a-reverted
// beads before readiness causes spurious agent_ready_timeout.
func TestWorkLoop_PostBootReDispatchGate_bk33(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-bk33-test-bead"

	ledger := newBK33FixtureLedger_bk33(beadID)
	sink := &bk33FixtureEventSink_bk33{}

	// spawnReadyCh simulates the channel daemon.Start closes after ProbeSpawnReady.
	// We leave it open initially to verify the gate blocks dispatch.
	spawnReadyCh := make(chan struct{})

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:            ledger,
		Bus:                  sink,
		ProjectDir:           "", // no real git repo needed; handler exits immediately
		HandlerBinary:        "/bin/sh",
		HandlerArgs:          []string{"-c", "exit 0"},
		AdapterRegistry2:     NewSealedAdapterRegistryForTest(t),
		IntentLogDir:         t.TempDir(),
		SpawnSubstrateReadyCh: spawnReadyCh,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Phase 1: gate is open (spawnReadyCh not yet closed).
	// ClaimBead MUST NOT be called within this window — the gate should block
	// the workloop before the first dispatch tick.
	const gateHoldDuration = 200 * time.Millisecond
	select {
	case <-ledger.claimedCh:
		t.Fatal("bk33: ClaimBead fired before spawnSubstrateReadyCh was closed — gate did not block re-dispatch")
	case <-time.After(gateHoldDuration):
		// Expected: gate held for gateHoldDuration with no dispatch.
	}

	// Phase 2: close the readiness channel, simulating the substrate probe completing.
	close(spawnReadyCh)

	// ClaimBead MUST be called promptly after the gate opens.
	const postGateTimeout = 5 * time.Second
	select {
	case <-ledger.claimedCh:
		// Expected: dispatch occurred after readiness gate opened.
	case <-time.After(postGateTimeout):
		t.Fatalf("bk33: ClaimBead did not fire within %v after spawnSubstrateReadyCh was closed", postGateTimeout)
	}

	// Clean up: cancel context and wait for loop to exit.
	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("bk33: work loop did not exit after context cancellation")
	}
}

// TestWorkLoop_PostBootGateSkippedWhenNilCh_bk33 verifies that when
// spawnSubstrateReadyCh is nil (no restart-backoff, normal boot), the workloop
// dispatches immediately without waiting.
func TestWorkLoop_PostBootGateSkippedWhenNilCh_bk33(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-bk33-no-gate-bead"

	ledger := newBK33FixtureLedger_bk33(beadID)
	sink := &bk33FixtureEventSink_bk33{}

	// No SpawnSubstrateReadyCh — nil means gate is disabled.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              sink,
		ProjectDir:       "",
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     t.TempDir(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Without the gate, ClaimBead should fire promptly.
	const dispatchTimeout = 5 * time.Second
	select {
	case <-ledger.claimedCh:
		// Expected: dispatch fired without gate delay.
	case <-time.After(dispatchTimeout):
		t.Fatalf("bk33: ClaimBead did not fire within %v when gate is disabled (nil ch)", dispatchTimeout)
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("bk33: work loop did not exit after context cancellation")
	}
}
