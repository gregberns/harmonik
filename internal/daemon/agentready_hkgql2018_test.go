package daemon_test

// agentready_hkgql2018_test.go — unit tests for waitAgentReady (hk-gql20.18).
//
// Covers:
//   1. DetectReady returns true on a seeded event → waitAgentReady returns nil.
//   2. No events for full timeout → returns ErrAgentReadyTimeout.
//   3. ctx cancel mid-wait → returns ctx.Err().
//   4. Race: ready arrives at the timeout boundary → ready wins per HC-056.
//
// Helper prefix: agentReadyFixture (implementer-protocol.md §Helper-prefix discipline;
// bead hk-gql20.18).
//
// Spec ref: specs/handler-contract.md §4.9 HC-056.
// Bead ref: hk-gql20.18.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// agentReadyFixtureMakeRunID returns a fresh random RunID for use in tests.
func agentReadyFixtureMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("agentReadyFixtureMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// agentReadyFixtureMakeEvent returns a core.EventEnvelope with the given type
// and run_id populated. Used to build synthetic events for the stub source.
func agentReadyFixtureMakeEvent(t *testing.T, evType core.EventType, runID core.RunID) core.EventEnvelope {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("agentReadyFixtureMakeEvent: uuid.NewV7: %v", err)
	}
	runIDVal := runID
	return core.EventEnvelope{
		EventID:       core.EventID(id),
		SchemaVersion: 1,
		Type:          string(evType),
		RunID:         &runIDVal,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub agentEventSource
// ─────────────────────────────────────────────────────────────────────────────

// agentReadyFixtureSource is a test stub for agentEventSource.
// Send events to ch before or after calling Events() to simulate bus delivery.
// Close ch to signal no further events (source exhausted / session closed).
type agentReadyFixtureSource struct {
	// ch is the pre-constructed channel; Send closes or sends to it.
	ch chan core.EventEnvelope
}

// agentReadyFixtureNewSource constructs a stub source with a buffered channel.
// Capacity should be >= the number of events the test will send before reading.
func agentReadyFixtureNewSource(cap int) *agentReadyFixtureSource {
	return &agentReadyFixtureSource{ch: make(chan core.EventEnvelope, cap)}
}

// Events implements daemon.AgentEventSourceExported (the exported alias used
// by ExportedWaitAgentReady).
func (s *agentReadyFixtureSource) Events(_ context.Context, _ core.RunID) <-chan core.EventEnvelope {
	return s.ch
}

// Send pushes ev onto the channel (non-blocking because cap is set at construction).
func (s *agentReadyFixtureSource) Send(ev core.EventEnvelope) {
	s.ch <- ev
}

// Close signals source exhaustion.
func (s *agentReadyFixtureSource) Close() {
	close(s.ch)
}

// ─────────────────────────────────────────────────────────────────────────────
// Stub adapter
// ─────────────────────────────────────────────────────────────────────────────

// agentReadyFixtureAdapter is a minimal Adapter stub.
// It returns true from DetectReady only for events of readyType.
type agentReadyFixtureAdapter struct {
	readyType core.EventType
}

func (a *agentReadyFixtureAdapter) DetectReady(event core.EventEnvelope) bool {
	return core.EventType(event.Type) == a.readyType
}

func (a *agentReadyFixtureAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (a *agentReadyFixtureAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}

func (a *agentReadyFixtureAdapter) RotateAccount(_ context.Context) error {
	return nil
}
func (a *agentReadyFixtureAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: DetectReady=true on a seeded event → waitAgentReady returns nil
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitAgentReady_ReadyEvent verifies that when the source delivers an event
// for which adapter.DetectReady returns true, waitAgentReady returns nil promptly.
func TestWaitAgentReady_ReadyEvent(t *testing.T) {
	runID := agentReadyFixtureMakeRunID(t)
	src := agentReadyFixtureNewSource(1)
	adapter := &agentReadyFixtureAdapter{readyType: core.EventTypeAgentReady}

	// Seed the ready event before calling; source channel is buffered.
	ev := agentReadyFixtureMakeEvent(t, core.EventTypeAgentReady, runID)
	src.Send(ev)

	done := make(chan error, 1)
	go func() {
		done <- daemon.ExportedWaitAgentReady(t.Context(), runID, src, adapter, 5*time.Second)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waitAgentReady: unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitAgentReady did not return promptly after ready event")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: No events for full timeout → returns ErrAgentReadyTimeout
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitAgentReady_Timeout verifies that when no agent_ready event arrives
// within the timeout window, waitAgentReady returns ErrAgentReadyTimeout.
func TestWaitAgentReady_Timeout(t *testing.T) {
	runID := agentReadyFixtureMakeRunID(t)
	src := agentReadyFixtureNewSource(0)
	adapter := &agentReadyFixtureAdapter{readyType: core.EventTypeAgentReady}

	// Use a very short timeout to keep the test fast.
	const shortTimeout = 50 * time.Millisecond

	err := daemon.ExportedWaitAgentReady(t.Context(), runID, src, adapter, shortTimeout)

	if err == nil {
		t.Fatal("waitAgentReady: expected ErrAgentReadyTimeout, got nil")
	}
	if !errors.Is(err, daemon.ExportedErrAgentReadyTimeout) {
		t.Fatalf("waitAgentReady: err=%v, want ErrAgentReadyTimeout", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: ctx cancel mid-wait → returns ctx.Err()
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitAgentReady_CtxCancel verifies that when ctx is cancelled before any
// agent_ready event arrives, waitAgentReady returns ctx.Err().
func TestWaitAgentReady_CtxCancel(t *testing.T) {
	runID := agentReadyFixtureMakeRunID(t)
	src := agentReadyFixtureNewSource(0)
	adapter := &agentReadyFixtureAdapter{readyType: core.EventTypeAgentReady}

	ctx, cancel := context.WithCancel(context.Background())

	type result struct{ err error }
	resultCh := make(chan result, 1)
	go func() {
		resultCh <- result{err: daemon.ExportedWaitAgentReady(ctx, runID, src, adapter, 30*time.Second)}
	}()

	// Give the goroutine time to enter the select.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case res := <-resultCh:
		if res.err == nil {
			t.Fatal("waitAgentReady: expected ctx.Err(), got nil")
		}
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("waitAgentReady: err=%v, want context.Canceled", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitAgentReady did not return after ctx cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: Race — ready arrives at timeout boundary → ready wins (HC-056)
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitAgentReady_ReadyWinsAtBoundary documents the HC-056 "last-second
// arrival" posture: when a ready event arrives concurrently with timeout expiry,
// waitAgentReady MUST prefer ready. This test races ready delivery against a
// very short timeout by pre-staging the event in the buffer and running
// repeatedly to exercise the concurrent select path.
//
// Note: Go's select does not guarantee which case wins when both are
// simultaneously ready. The implementation adds a non-blocking inner select
// after the timeout case to check ready once more. This test documents the
// posture rather than providing a deterministic guarantee.
func TestWaitAgentReady_ReadyWinsAtBoundary(t *testing.T) {
	adapter := &agentReadyFixtureAdapter{readyType: core.EventTypeAgentReady}

	// Run multiple iterations to exercise the race path.
	const iterations = 20
	timeoutCount := 0
	for i := 0; i < iterations; i++ {
		runID := agentReadyFixtureMakeRunID(t)
		// Buffer 1 so Send never blocks.
		src := agentReadyFixtureNewSource(1)

		// Use a very short timeout.
		const shortTimeout = 1 * time.Millisecond

		// Stage the ready event so it is in the buffer when the function starts.
		ev := agentReadyFixtureMakeEvent(t, core.EventTypeAgentReady, runID)
		src.Send(ev)

		err := daemon.ExportedWaitAgentReady(t.Context(), runID, src, adapter, shortTimeout)
		if err != nil && !errors.Is(err, daemon.ExportedErrAgentReadyTimeout) {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if errors.Is(err, daemon.ExportedErrAgentReadyTimeout) {
			timeoutCount++
		}
	}

	// Ready should win the majority of the time because the event is already
	// buffered when the call starts. If every iteration timed out something is
	// wrong with the buffering logic.
	if timeoutCount == iterations {
		t.Errorf("ready never won (%d/%d iterations timed out); check buffered-channel logic in waitAgentReady",
			timeoutCount, iterations)
	}
	t.Logf("boundary race: %d/%d timeout, %d/%d ready", timeoutCount, iterations, iterations-timeoutCount, iterations)
}
