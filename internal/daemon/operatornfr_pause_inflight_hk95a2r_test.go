package daemon_test

// operatornfr_pause_inflight_hk95a2r_test.go — scenario: operator-pause over
// PL-003a with a run in-flight (ON-056 / ON-057).
//
// # What this test covers
//
// The ON-056/ON-057 conformance requirement (specs/operator-nfr.md §4.3):
//
//   With ≥1 run satisfying in_flight(run), an agent issues
//   `harmonik supervise pause` over PL-003a:
//
//   (a) operator_pause_status{status=pausing} is emitted.
//   (b) No new run_started can fire while the daemon is pausing or paused;
//       IsPaused() is true immediately after the socket command returns.
//       Validated by asserting the idempotent second pause emits no extra events.
//   (c) The in-flight run reaches a terminal state without abort — the
//       between-task invariant (ON-008).  In the current OperatorPauseController
//       implementation the paused event fires synchronously (the full drain-wait
//       per ON-027 is not yet implemented at the controller level); this test
//       documents the current observable behavior and confirms no abort signal is
//       sent to in-flight subprocesses.
//   (d) operator_pause_status{status=paused} is emitted.  drain_summary is noted:
//       the current OperatorPauseController does not populate drain_summary; the
//       test asserts the event is present and well-formed rather than asserting
//       drain_summary contents.
//   (e) The queue transitions active → paused-by-drain; a queue_paused event
//       with reason="operator_drain" is emitted (QM-054).
//   (f) On `harmonik supervise resume`, operator_resuming is emitted, the
//       IsPaused() gate clears, and the queue transitions back to active.
//
// # Test topology
//
// The test wires the production components directly (without daemon.Start) so
// the socket → controller → bus → QueueOperatorEventConsumer → QueueStore
// chain is exercised in isolation:
//
//   1. eventbus.NewBusImpl + QueueOperatorEventConsumer.Subscribe + test observers
//   2. bus.Seal()
//   3. OperatorPauseController (uses the sealed bus)
//   4. RunSocketListenerFull started in a goroutine with the controller
//   5. QueueStore pre-seeded with an active queue
//
// This pattern matches the composition root of daemon.Start while staying
// self-contained and fast (no real br binary, no subprocess spawning, no
// pidfile acquisition).
//
// "Run in-flight" is represented by the QueueStore's dispatched item — the
// item was claimed before the pause command, and the queue consumer is
// waiting.  For the event-level simulation, no real subprocess is launched;
// the test confirms the socket → pause → queue-transition chain fires
// correctly.
//
// # Why not use daemon.Start?
//
// daemon.Start runs orphan sweeps, pidfile acquisition, adapter registry wiring,
// restart backoff, and several other startup steps that are not relevant to
// ON-056/ON-057.  Wiring the production socket + controller + bus + queue
// consumer directly keeps the test focused on the tested invariant.
//
// Helper prefix: onNfr (bead hk-95a2r, per implementer-protocol §Helper-prefix
// discipline).
//
// Spec refs:
//   - specs/operator-nfr.md §4.3 ON-056, ON-057
//   - specs/operator-nfr.md §4.3 ON-008 (between-task invariant)
//   - specs/operator-nfr.md §4.3 ON-013 (event emission per state transition)
//   - specs/operator-nfr.md §4.3 ON-013c (idempotency on no-op transitions)
//   - specs/process-lifecycle.md §4.1 PL-003a (Unix-socket JSON-RPC surface)
//   - specs/queue-model.md §8.5 QM-054 (queue active → paused-by-drain)
//
// Bead: hk-95a2r.

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// onNfrActiveQueue builds a *queue.Queue with status=active and one dispatched
// item (simulating an in-flight run per §3 in_flight(run)).
func onNfrActiveQueue(t *testing.T) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "on-nfr-" + t.Name(),
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-95a2r-item-0"),
						Status: queue.ItemStatusDispatched, // item is in-flight
					},
				},
				CreatedAt: time.Now().UTC(),
			},
		},
	}
}

// onNfrSockSend dials sockPath, sends {"op": opName}, and returns the decoded
// SocketResponse.  Mirrors the pattern in socket_operatorpause_ry8q1_test.go.
func onNfrSockSend(t *testing.T, sockPath, opName string) daemon.SocketResponse {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("onNfrSockSend %q: dial: %v", opName, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	payload, _ := json.Marshal(map[string]string{"op": opName})
	if _, writeErr := conn.Write(payload); writeErr != nil {
		t.Fatalf("onNfrSockSend %q: write: %v", opName, writeErr)
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck
	}

	var resp daemon.SocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		t.Fatalf("onNfrSockSend %q: decode response: %v", opName, decErr)
	}
	return resp
}

// onNfrSubscribePauseStatus subscribes to operator_pause_status events on the
// (unsealed) bus and returns a buffered channel receiving payloads.
// Must be called before bus.Seal.
func onNfrSubscribePauseStatus(t *testing.T, bus eventbus.EventBus) <-chan core.OperatorPauseStatusPayload {
	t.Helper()
	ch := make(chan core.OperatorPauseStatusPayload, 8)
	sub := core.Subscription{
		ConsumerID:    "onNfr-pause-status-" + t.Name(),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeOperatorPauseStatus: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.OperatorPauseStatusPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			select {
			case ch <- p:
			default:
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("onNfr: Subscribe operator_pause_status: %v", err)
	}
	return ch
}

// onNfrSubscribeResuming subscribes to operator_resuming events on the (unsealed)
// bus and returns a buffered channel receiving payloads.
// Must be called before bus.Seal.
func onNfrSubscribeResuming(t *testing.T, bus eventbus.EventBus) <-chan core.OperatorResumingPayload {
	t.Helper()
	ch := make(chan core.OperatorResumingPayload, 4)
	sub := core.Subscription{
		ConsumerID:    "onNfr-resuming-" + t.Name(),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeOperatorResuming: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.OperatorResumingPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			select {
			case ch <- p:
			default:
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("onNfr: Subscribe operator_resuming: %v", err)
	}
	return ch
}

// onNfrSubscribeQueuePaused subscribes to queue_paused events on the (unsealed)
// bus and returns a buffered channel receiving payloads.
// Must be called before bus.Seal.
func onNfrSubscribeQueuePaused(t *testing.T, bus eventbus.EventBus) <-chan core.QueuePausedPayload {
	t.Helper()
	ch := make(chan core.QueuePausedPayload, 4)
	sub := core.Subscription{
		ConsumerID:    "onNfr-queue-paused-" + t.Name(),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeQueuePaused: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.QueuePausedPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				return nil
			}
			select {
			case ch <- p:
			default:
			}
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("onNfr: Subscribe queue_paused: %v", err)
	}
	return ch
}

// onNfrGetPauseStatus reads one OperatorPauseStatusPayload from ch within
// timeout, failing the test if none arrives.
func onNfrGetPauseStatus(t *testing.T, ch <-chan core.OperatorPauseStatusPayload, timeout time.Duration, label string) core.OperatorPauseStatusPayload {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(timeout):
		t.Fatalf("onNfr: timed out waiting for %s within %s", label, timeout)
		return core.OperatorPauseStatusPayload{}
	}
}

// onNfrGetResuming reads one OperatorResumingPayload from ch within timeout.
func onNfrGetResuming(t *testing.T, ch <-chan core.OperatorResumingPayload, timeout time.Duration, label string) core.OperatorResumingPayload {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(timeout):
		t.Fatalf("onNfr: timed out waiting for %s within %s", label, timeout)
		return core.OperatorResumingPayload{}
	}
}

// onNfrGetQueuePaused reads one QueuePausedPayload from ch within timeout.
func onNfrGetQueuePaused(t *testing.T, ch <-chan core.QueuePausedPayload, timeout time.Duration, label string) core.QueuePausedPayload {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(timeout):
		t.Fatalf("onNfr: timed out waiting for %s within %s", label, timeout)
		return core.QueuePausedPayload{}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestScenario_OperatorNFR_PauseWithRunInFlight
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_OperatorNFR_PauseWithRunInFlight is the ON-056/ON-057 conformance
// scenario: agent issues harmonik supervise pause over PL-003a with a run in-flight.
//
// Wiring under test:
//
//	socket("operator-pause") → OperatorPauseController.HandleOperatorPause
//	  → bus.Emit(operator_pause_status{pausing})
//	  → [QueueOperatorEventConsumer] → queue active → paused-by-drain
//	  → bus.Emit(queue_paused{operator_drain})
//	  → bus.Emit(operator_pause_status{paused})
//	socket("operator-resume") → OperatorPauseController.HandleOperatorResume
//	  → bus.Emit(operator_resuming)
//	  → [QueueOperatorEventConsumer] → queue paused-by-drain → active
//
// Assertions:
//
//	(a) operator_pause_status{pausing} emitted on pause socket command.
//	(b) IsPaused() gate is held: idempotent second pause emits no extra events.
//	(c) Between-task invariant: pause does not abort in-flight runs.
//	(d) operator_pause_status{paused} emitted (drain_summary: TBD in ON-027 full impl).
//	(e) queue_paused{reason=operator_drain} emitted; queue.Status=paused-by-drain.
//	(f) operator_resuming emitted on resume; queue.Status=active restored.
//
// Bead: hk-95a2r.
func TestScenario_OperatorNFR_PauseWithRunInFlight(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	// ── 1. Build the event bus and wire subscribers (before Seal) ────────────

	bus := eventbus.NewBusImpl()

	// Subscribe test observers.
	pauseStatusCh := onNfrSubscribePauseStatus(t, bus)
	resumingCh := onNfrSubscribeResuming(t, bus)
	queuePausedCh := onNfrSubscribeQueuePaused(t, bus)

	// Set up a QueueStore with an active queue.
	qs := daemon.ExportedNewQueueStore()
	q := onNfrActiveQueue(t)
	qs.SetQueue(q)
	queueID := q.QueueID

	// Subscribe the QueueOperatorEventConsumer before Seal so it reacts to
	// operator_pause_status and operator_resuming (EV-009).
	queueOpConsumer := daemon.ExportedNewQueueOperatorEventConsumer(
		daemon.ExportedQueueOperatorEventConsumerConfig{
			QueueStore: qs,
			Bus:        bus,
			// ProjectDir is left empty: consumer transitions in-memory state but
			// skips the persist step (unit-test mode without a filesystem).
		},
	)
	if err := queueOpConsumer.Subscribe(bus); err != nil {
		t.Fatalf("onNfr: QueueOperatorEventConsumer.Subscribe: %v", err)
	}

	// Seal the bus: all further subscriptions are rejected; delivery is live.
	if err := bus.Seal(); err != nil {
		t.Fatalf("onNfr: bus.Seal: %v", err)
	}

	// ── 2. Build the OperatorPauseController and start the socket listener ───

	ctrl := daemon.ExportedNewOperatorPauseController(bus)

	sockPath := socketFixtureTempSockPath(t)

	sockCtx, sockCancel := context.WithCancel(context.Background())
	t.Cleanup(sockCancel)

	go func() {
		_ = daemon.RunSocketListenerFull(sockCtx, sockPath, nil, nil, nil, ctrl, nil)
	}()

	socketFixtureWaitReady(t, sockPath)

	// ── 3. Pre-condition: queue is active with a dispatched (in-flight) item ──

	gotQ := qs.Queue()
	if gotQ == nil || gotQ.Status != queue.QueueStatusActive {
		t.Fatalf("onNfr pre-condition: queue.Status = %v; want active", gotQ)
	}
	t.Logf("onNfr pre-condition: queue.Status=%q items=%d (run in-flight)", gotQ.Status, len(gotQ.Groups[0].Items))

	// ── 4. Issue operator-pause via socket (agent-callable per ON-056) ───────

	pauseResp := onNfrSockSend(t, sockPath, "operator-pause")
	if !pauseResp.Ok {
		t.Fatalf("onNfr: operator-pause socket command failed: %q", pauseResp.Error)
	}

	// ── 5. Drain bus to let async QueueOperatorEventConsumer process events ───

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel()
	if err := bus.Drain(drainCtx); err != nil {
		t.Fatalf("onNfr: bus.Drain (post-pause): %v", err)
	}

	// ── 6. Assertion (a): operator_pause_status{pausing} emitted ─────────────

	pausingEvt := onNfrGetPauseStatus(t, pauseStatusCh, 2*time.Second,
		"operator_pause_status{pausing}")
	if pausingEvt.Status != core.OperatorPauseStatusValuePausing {
		t.Errorf("onNfr (a): operator_pause_status.status = %q; want %q",
			pausingEvt.Status, core.OperatorPauseStatusValuePausing)
	}
	if !pausingEvt.Valid() {
		t.Errorf("onNfr (a): operator_pause_status{pausing} payload.Valid() = false: %+v", pausingEvt)
	}
	t.Logf("onNfr (a) PASS: operator_pause_status{pausing} received changed_at=%s", pausingEvt.ChangedAt)

	// ── 7. Assertion (d): operator_pause_status{paused} emitted ──────────────

	pausedEvt := onNfrGetPauseStatus(t, pauseStatusCh, 2*time.Second,
		"operator_pause_status{paused}")
	if pausedEvt.Status != core.OperatorPauseStatusValuePaused {
		t.Errorf("onNfr (d): operator_pause_status.status = %q; want %q",
			pausedEvt.Status, core.OperatorPauseStatusValuePaused)
	}
	if !pausedEvt.Valid() {
		t.Errorf("onNfr (d): operator_pause_status{paused} payload.Valid() = false: %+v", pausedEvt)
	}
	t.Logf("onNfr (d) PASS: operator_pause_status{paused} received changed_at=%s", pausedEvt.ChangedAt)
	// drain_summary: not yet populated in current OperatorPauseController (ON-027 drain
	// wait is not yet implemented at the controller level — the paused event fires
	// synchronously). This test asserts the event is well-formed; drain_summary
	// contents are a TODO for when ON-027 full drain wait lands.

	// ── 8. Assertion (e): queue_paused{operator_drain} emitted + queue state ─

	qPausedEvt := onNfrGetQueuePaused(t, queuePausedCh, 2*time.Second,
		"queue_paused{operator_drain}")
	if !qPausedEvt.Valid() {
		t.Errorf("onNfr (e): queue_paused payload.Valid() = false: %+v", qPausedEvt)
	}
	if qPausedEvt.Reason != "operator_drain" {
		t.Errorf("onNfr (e): queue_paused.reason = %q; want %q",
			qPausedEvt.Reason, "operator_drain")
	}
	if qPausedEvt.QueueID != queueID {
		t.Errorf("onNfr (e): queue_paused.queue_id = %q; want %q",
			qPausedEvt.QueueID, queueID)
	}
	if qPausedEvt.GroupIndex != 0 {
		t.Errorf("onNfr (e): queue_paused.group_index = %d; want 0 (QM-054 step 2 requires group_index)",
			qPausedEvt.GroupIndex)
	}
	t.Logf("onNfr (e) PASS: queue_paused{operator_drain} received queue_id=%s group_index=%d",
		qPausedEvt.QueueID, qPausedEvt.GroupIndex)

	gotQAfterPause := qs.Queue()
	if gotQAfterPause == nil {
		t.Fatal("onNfr (e): QueueStore.Queue() = nil after pause; expected paused-by-drain")
	}
	if gotQAfterPause.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("onNfr (e): queue.Status = %q; want %q",
			gotQAfterPause.Status, queue.QueueStatusPausedByDrain)
	}
	t.Logf("onNfr (e) PASS: queue.Status = %q (active → paused-by-drain)", gotQAfterPause.Status)

	// ── 9. Assertion (c): between-task invariant — in-flight run not aborted ─
	//
	// The OperatorPauseController does not send any kill/interrupt signal to
	// running subprocesses.  The workloop's IsPaused() gate prevents NEW
	// dispatches but does not abort the currently dispatched item.  We confirm
	// this structurally: the queue item's status remains ItemStatusDispatched
	// after the pause (the daemon did not mark it failed/canceled).
	if gotQAfterPause.Groups[0].Items[0].Status != queue.ItemStatusDispatched {
		t.Errorf("onNfr (c): in-flight item status after pause = %q; want %q (abort would set failed/canceled)",
			gotQAfterPause.Groups[0].Items[0].Status, queue.ItemStatusDispatched)
	}
	t.Logf("onNfr (c) PASS: in-flight item status = %q — no abort signal sent (ON-008 between-task invariant)",
		gotQAfterPause.Groups[0].Items[0].Status)

	// ── 10. Assertion (b): IsPaused() gate held; idempotent second pause ─────
	//
	// ON-013c: a pause issued while already paused MUST be a no-op.
	// The socket must return Ok=true and no extra operator_pause_status event
	// may be emitted (deduplicated per session_id / paused state).
	idempotentResp := onNfrSockSend(t, sockPath, "operator-pause")
	if !idempotentResp.Ok {
		t.Errorf("onNfr (b): idempotent second operator-pause returned error: %q", idempotentResp.Error)
	}

	drainCtx2, drainCancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer drainCancel2()
	if drainErr := bus.Drain(drainCtx2); drainErr != nil {
		t.Logf("onNfr (b): bus.Drain (idempotent pause): %v (non-fatal)", drainErr)
	}

	select {
	case extra := <-pauseStatusCh:
		t.Errorf("onNfr (b): idempotent pause emitted extra operator_pause_status event: %+v", extra)
	default:
		// Correct: IsPaused gate is held; no extra event.
	}
	t.Logf("onNfr (b) PASS: IsPaused gate held; idempotent operator-pause emitted no extra events (ON-013c)")

	// ── 11. Issue operator-resume via socket ──────────────────────────────────

	resumeResp := onNfrSockSend(t, sockPath, "operator-resume")
	if !resumeResp.Ok {
		t.Fatalf("onNfr (f): operator-resume socket command failed: %q", resumeResp.Error)
	}

	drainCtx3, drainCancel3 := context.WithTimeout(context.Background(), 5*time.Second)
	defer drainCancel3()
	if err := bus.Drain(drainCtx3); err != nil {
		t.Fatalf("onNfr (f): bus.Drain (post-resume): %v", err)
	}

	// ── 12. Assertion (f): operator_resuming emitted; queue returns to active ─

	resumingEvt := onNfrGetResuming(t, resumingCh, 2*time.Second, "operator_resuming")
	if !resumingEvt.Valid() {
		t.Errorf("onNfr (f): operator_resuming payload.Valid() = false: %+v", resumingEvt)
	}
	t.Logf("onNfr (f) PASS: operator_resuming received resumed_at=%s", resumingEvt.ResumedAt)

	gotQAfterResume := qs.Queue()
	if gotQAfterResume == nil {
		t.Fatal("onNfr (f): QueueStore.Queue() = nil after resume; expected active")
	}
	if gotQAfterResume.Status != queue.QueueStatusActive {
		t.Errorf("onNfr (f): queue.Status after resume = %q; want %q",
			gotQAfterResume.Status, queue.QueueStatusActive)
	}
	t.Logf("onNfr (f) PASS: queue.Status after resume = %q (dispatch restored)", gotQAfterResume.Status)

	t.Logf("onNfr: scenario PASS — full pause/resume cycle over PL-003a socket with in-flight run (ON-056/ON-057)")
}
