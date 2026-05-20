package daemon_test

// workloop_handlerpause_kac8g_test.go — dispatcher skip-on-paused tests (hk-kac8g).
//
// Acceptance criteria from bead spec:
//   - Dispatch loop pre-dispatch eligibility check calls IsPaused(agent_type):
//     with one handler paused, items of that type are skipped (not dispatched).
//   - queue_item_held_for_handler_pause emitted once per (item, paused_epoch)
//     — dedup across multiple dispatch ticks within one epoch.
//   - After resume, previously held items are dispatched normally.
//   - Gate works on both the queue-pull path and the br-ready fallback path.
//
// Bead ref: hk-kac8g.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// hpcNewController builds a HandlerPauseController backed by a sealed in-memory bus.
func hpcNewController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("hpcNewController: bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

// hpcPause pauses AgentTypeClaudeCode on ctrl with a minimal cause.
func hpcPause(t *testing.T, ctrl *daemon.HandlerPauseController) {
	t.Helper()
	cause := core.HandlerPauseCause{
		FailureClass: core.FailureClassTransient,
		SubReason:    "rate_limit",
		SourceRunID:  "run-pause-source",
		SourceBeadID: "hk-pause-src",
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := ctrl.Pause(context.Background(), core.AgentTypeClaudeCode, cause, nil); err != nil {
		t.Fatalf("hpcPause: %v", err)
	}
}

// hpcResume clears the pause on AgentTypeClaudeCode.
func hpcResume(t *testing.T, ctrl *daemon.HandlerPauseController) {
	t.Helper()
	if err := ctrl.Resume(context.Background(), core.AgentTypeClaudeCode, core.HandlerResumedByOperator); err != nil {
		t.Fatalf("hpcResume: %v", err)
	}
}

// hpcWaveQueue builds a minimal one-group active wave queue for one bead.
func hpcWaveQueue(t *testing.T, beadID core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hpc-test-queue-" + t.Name(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
				CreatedAt:  now,
			},
		},
	}
}

// hpcCountEventType counts events of a given type in the collector.
func hpcCountEventType(col *stubEventCollector, evtType string) int {
	n := 0
	for _, e := range col.allEvents() {
		if e.EventType == evtType {
			n++
		}
	}
	return n
}

// hpcPollEventType polls until at least minCount events of evtType appear or
// the deadline elapses.
func hpcPollEventType(t *testing.T, col *stubEventCollector, evtType string, minCount int, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		if hpcCountEventType(col, evtType) >= minCount {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("hpcPollEventType: timed out after %s waiting for %d %q events; got %d",
				deadline, minCount, evtType, hpcCountEventType(col, evtType))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestHandlerPause_QueuePath_SkipOnPaused
// ─────────────────────────────────────────────────────────────────────────────

// TestHandlerPause_QueuePath_SkipOnPaused verifies that when AgentTypeClaudeCode
// is paused, queue items are NOT dispatched and a queue_item_held_for_handler_pause
// event is emitted.  After resume, the item is dispatched normally.
//
// Bead ref: hk-kac8g.
func TestHandlerPause_QueuePath_SkipOnPaused(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-kac8g-queue-skip-001")

	bus := &stubEventCollector{}
	ctrl := hpcNewController(t)

	// Pause BEFORE the loop starts so the first dispatch tick is skipped.
	hpcPause(t, ctrl)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hpcWaveQueue(t, beadID))

	p := daemon.WorkLoopDepsParams{
		BrAdapter:              &stubBeadLedger{},
		Bus:                    bus,
		ProjectDir:             projectDir,
		HandlerBinary:          "/bin/sh",
		HandlerArgs:            []string{"-c", "exit 0"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:             qs,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		HandlerPauseController: ctrl,
	}
	deps := daemon.ExportedWorkLoopDeps(p)
	ledger := p.BrAdapter.(*stubBeadLedger)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the held event — proves the gate fired.
	hpcPollEventType(t, bus, string(core.EventTypeQueueItemHeldForHandlerPause), 1, 10*time.Second)

	// Bead must NOT have been dispatched while paused.
	if len(ledger.closedIDs()) > 0 {
		t.Fatal("bead was dispatched (closed) while handler was paused — expected hold")
	}

	// Resume and wait for dispatch.
	hpcResume(t, ctrl)
	queueDispatchFixturePollClosed(t, ledger, 1, 15*time.Second)

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}

	// Verify held event payload is well-formed.
	var foundHeld bool
	for _, e := range bus.allEvents() {
		if e.EventType != string(core.EventTypeQueueItemHeldForHandlerPause) {
			continue
		}
		var p core.QueueItemHeldForHandlerPausePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("unmarshal held payload: %v", err)
		}
		if !p.Valid() {
			t.Fatalf("held payload.Valid() = false: %+v", p)
		}
		if p.BeadID != string(beadID) {
			t.Errorf("held event bead_id=%q, want %q", p.BeadID, string(beadID))
		}
		if p.AgentType != core.AgentTypeClaudeCode {
			t.Errorf("held event agent_type=%q, want %q", p.AgentType, core.AgentTypeClaudeCode)
		}
		if p.PausedEpoch < 1 {
			t.Errorf("held event paused_epoch=%d, want >=1", p.PausedEpoch)
		}
		foundHeld = true
	}
	if !foundHeld {
		t.Fatal("no queue_item_held_for_handler_pause event found")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestHandlerPause_HeldEventDedup
// ─────────────────────────────────────────────────────────────────────────────

// TestHandlerPause_HeldEventDedup verifies the at-most-once dedup contract
// from event-model.md §8.11.3: within a single paused epoch, the held event
// is emitted exactly once per (bead_id, paused_epoch) pair regardless of how
// many dispatch ticks pass.
//
// Bead ref: hk-kac8g.
func TestHandlerPause_HeldEventDedup(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-kac8g-dedup-001")

	bus := &stubEventCollector{}
	ctrl := hpcNewController(t)

	// Pause so every tick holds the item.
	hpcPause(t, ctrl)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(hpcWaveQueue(t, beadID))

	p := daemon.WorkLoopDepsParams{
		BrAdapter:              &stubBeadLedger{},
		Bus:                    bus,
		ProjectDir:             projectDir,
		HandlerBinary:          "/bin/sh",
		HandlerArgs:            []string{"-c", "exit 0"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:             qs,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		HandlerPauseController: ctrl,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for at least one held event, then give the loop several more ticks
	// to exercise the dedup path.
	hpcPollEventType(t, bus, string(core.EventTypeQueueItemHeldForHandlerPause), 1, 10*time.Second)

	// Let the loop spin for ~3 more poll ticks.
	time.Sleep(3 * 2 * time.Second)

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}

	// Held event must have been emitted exactly once (dedup contract §8.11.3).
	n := hpcCountEventType(bus, string(core.EventTypeQueueItemHeldForHandlerPause))
	if n != 1 {
		t.Errorf("queue_item_held_for_handler_pause emitted %d times, want exactly 1 (dedup)", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestHandlerPause_BrReadyPath_SkipOnPaused
// ─────────────────────────────────────────────────────────────────────────────

// TestHandlerPause_BrReadyPath_SkipOnPaused verifies the skip-on-paused gate
// on the br-ready fallback path (no queue active).
//
// Bead ref: hk-kac8g.
func TestHandlerPause_BrReadyPath_SkipOnPaused(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-kac8g-brready-001")

	bus := &stubEventCollector{}
	ctrl := hpcNewController(t)

	// Pause BEFORE the loop starts.
	hpcPause(t, ctrl)

	// Prefill ready with enough copies to cover the pause + several ticks after
	// resume.  stubBeadLedger dequeues one per Ready call; we prepend many
	// copies so the bead is returned on every tick during the pause window and
	// at least once after resume.
	const copies = 30
	readyIDs := make([]core.BeadID, copies)
	for i := range readyIDs {
		readyIDs[i] = beadID
	}

	ledger := &stubBeadLedger{ready: readyIDs}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:              ledger,
		Bus:                    bus,
		ProjectDir:             projectDir,
		HandlerBinary:          "/bin/sh",
		HandlerArgs:            []string{"-c", "exit 0"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		HandlerPauseController: ctrl,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		// QueueStore: nil → br-ready path
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Held event should appear on the first skipped tick.
	hpcPollEventType(t, bus, string(core.EventTypeQueueItemHeldForHandlerPause), 1, 10*time.Second)

	// Bead must NOT have been claimed while paused.
	if len(ledger.closedIDs()) > 0 {
		t.Fatal("bead was dispatched while handler was paused (br-ready path)")
	}

	// Resume — next tick should dispatch.
	hpcResume(t, ctrl)
	queueDispatchFixturePollClosed(t, ledger, 1, 15*time.Second)

	cancel()
	select {
	case <-loopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("workloop did not exit after context cancellation")
	}
}
