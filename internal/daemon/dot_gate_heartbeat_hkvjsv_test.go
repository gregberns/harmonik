package daemon

// dot_gate_heartbeat_hkvjsv_test.go — regression test: a long-running non-agentic
// SHELL gate node (the default commit_gate: go build/vet/test + scenario-gate,
// node timeout 900s) must emit periodic agent_heartbeat events carrying the run_id
// while the gate command executes, so the StaleWatcher keeps seeing events for the
// run and does NOT false-fire run_stale (hk-vjsv, codename:remote-substrate).
//
// # The bug
//
// dispatchDotToolNode executes the shell gate via exec.CommandContext (local) or
// the SSHRunner (remote). A shell command has no Claude handler session / NDJSON
// stream, so handler.RunHeartbeatLoop was never started for it — the agentic DOT
// path (dot_cascade.go:dispatchDotAgenticNode) and the cognition-gate path
// (dot_gate.go) DO start it, but the non-agentic shell branch did not. With zero
// events emitted for the run during the gate's multi-minute build+test, the
// StaleWatcher's lastEventAt stayed frozen at node_dispatch_requested; after the
// no-event window (~10 min) it fired run_stale and re-dispatched commit_gate
// WITHOUT killing the prior shell — a false-positive re-dispatch loop plus leaked
// gate shells (observed 3 concurrent full build+scenario runs on a worker).
//
// # The fix
//
// dispatchDotToolNode now takes (bus, runID) and starts handler.RunHeartbeatLoop
// for the lifetime of the gate command (both local and remote paths), stopping it
// via close(hbDone) the moment the command returns. RunHeartbeatLoop emits the
// first heartbeat immediately, then every dotGateHeartbeatInterval.
//
// # This test
//
// Drives the REAL dispatchDotToolNode with a gate command that sleeps longer than
// two heartbeat intervals (interval shrunk via the dotGateHeartbeatInterval test
// hook). It asserts the bus received MULTIPLE agent_heartbeat events stamped with
// the run's run_id — proving (a) the goroutine is wired on the shell path and
// (b) the PERIODIC tick fires, which is what keeps a gate outliving one interval
// non-stale. A nil-bus call (the isolation unit-test path) must NOT panic and
// must emit nothing.
//
// Bead: hk-vjsv.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// hkvjsvRecordedEvent captures a single bus emission with its run_id.
type hkvjsvRecordedEvent struct {
	RunID     core.RunID
	EventType core.EventType
}

// hkvjsvBus is a minimal handlercontract.EventEmitter that records emissions,
// concurrency-safe (the heartbeat fires from a goroutine while the gate runs).
type hkvjsvBus struct {
	mu     sync.Mutex
	events []hkvjsvRecordedEvent
}

func (b *hkvjsvBus) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, hkvjsvRecordedEvent{EventType: eventType})
	return nil
}

func (b *hkvjsvBus) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, hkvjsvRecordedEvent{RunID: runID, EventType: eventType})
	return nil
}

// hkvjsvHeartbeatsFor returns the agent_heartbeat events recorded for runID.
func (b *hkvjsvBus) hkvjsvHeartbeatsFor(runID core.RunID) []hkvjsvRecordedEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []hkvjsvRecordedEvent
	for _, e := range b.events {
		if e.EventType == core.EventTypeAgentHeartbeat && e.RunID == runID {
			out = append(out, e)
		}
	}
	return out
}

// hkvjsvNewRunID returns a fresh run id.
func hkvjsvNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hkvjsv: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// TestDispatchDotToolNode_LongGate_EmitsPeriodicHeartbeats is the hk-vjsv
// regression: a shell gate that outlives several heartbeat intervals must keep the
// run non-stale by emitting run-scoped agent_heartbeat events throughout, not just
// once at the start. Pre-fix the shell path emitted ZERO heartbeats.
func TestDispatchDotToolNode_LongGate_EmitsPeriodicHeartbeats(t *testing.T) {
	// Not t.Parallel(): this test and its two siblings in this file all
	// reassign the package-level dotGateHeartbeatInterval var; running them
	// concurrently raced under go test -race (hk-ri2in.4).
	//
	// Shrink the heartbeat cadence so the periodic tick is observable without a
	// 5-minute wait. Restore on cleanup. Production never reassigns this var.
	prev := dotGateHeartbeatInterval
	dotGateHeartbeatInterval = 80 * time.Millisecond
	t.Cleanup(func() { dotGateHeartbeatInterval = prev })

	bus := &hkvjsvBus{}
	runID := hkvjsvNewRunID(t)

	// Gate sleeps ~400ms — comfortably longer than ~4 heartbeat intervals — so we
	// observe the immediate heartbeat PLUS several ticker heartbeats.
	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, bus, runID, nil, t.TempDir(), toolNode("sleep 0.4", "10"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS for `sleep 0.4`, got %q (notes=%q)", outcome.Status, outcome.Notes)
	}

	hbs := bus.hkvjsvHeartbeatsFor(runID)
	if len(hbs) < 2 {
		t.Fatalf("hk-vjsv FAIL: long-running shell gate emitted %d agent_heartbeat events for run %s; "+
			"want >= 2 (immediate + >=1 periodic tick). With < 1 the StaleWatcher sees no event for the "+
			"run during the gate's build+test and false-fires run_stale, re-dispatching commit_gate without "+
			"killing the prior shell. Got: %d", len(hbs), runID.String(), len(hbs))
	}
}

// TestDispatchDotToolNode_NilBus_NoHeartbeatNoPanic verifies the isolation
// unit-test path (bus == nil): no heartbeat is started and the call does not panic
// — preserving the existing exit-state → Outcome behavior.
func TestDispatchDotToolNode_NilBus_NoHeartbeatNoPanic(t *testing.T) {
	prev := dotGateHeartbeatInterval
	dotGateHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { dotGateHeartbeatInterval = prev })

	ctx := context.Background()
	outcome, err := dispatchDotToolNode(ctx, nil, core.RunID{}, nil, t.TempDir(), toolNode("sleep 0.1", "10"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != core.OutcomeStatusSuccess {
		t.Fatalf("expected SUCCESS, got %q", outcome.Status)
	}
}

// TestDispatchDotToolNode_HeartbeatStopsWhenGateReturns guards against a leaked
// heartbeat goroutine: once the gate command returns and dispatchDotToolNode
// exits (closing hbDone), no further agent_heartbeat events appear for the run.
func TestDispatchDotToolNode_HeartbeatStopsWhenGateReturns(t *testing.T) {
	prev := dotGateHeartbeatInterval
	dotGateHeartbeatInterval = 40 * time.Millisecond
	t.Cleanup(func() { dotGateHeartbeatInterval = prev })

	bus := &hkvjsvBus{}
	runID := hkvjsvNewRunID(t)

	ctx := context.Background()
	_, err := dispatchDotToolNode(ctx, bus, runID, nil, t.TempDir(), toolNode("sleep 0.2", "10"), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	countAfterReturn := len(bus.hkvjsvHeartbeatsFor(runID))
	// Wait well past several intervals; the goroutine must be stopped.
	time.Sleep(250 * time.Millisecond)
	countLater := len(bus.hkvjsvHeartbeatsFor(runID))

	if countLater != countAfterReturn {
		t.Fatalf("hk-vjsv leak: agent_heartbeat count grew from %d to %d AFTER the gate returned — "+
			"the heartbeat goroutine leaked past close(hbDone)", countAfterReturn, countLater)
	}
}
