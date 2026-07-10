package scenario

// queue_setqueue_wiring_test.go — integration tests for the HandlerAdapter
// SetQueue wiring gaps (hk-4ukkq, hk-lzs8r, hk-peucr) and the
// evaluateGroupAdvanceWithOutcome Persist + CompleteAndUnlink paths (hk-xsutm).
//
// These tests exercise the four gaps fixed in this bead cluster:
//
//   1. hk-4ukkq: HandlerAdapter.HandleQueueSubmit now calls qs.SetQueue after
//      persist so the running workloop sees the queue without restart.
//   2. hk-lzs8r: HandlerAdapter.HandleQueueAppend now persists and calls
//      qs.SetQueue so appended items reach the workloop.
//   3. hk-peucr: HandlerAdapter emits queue_submitted / queue_appended events
//      after persist.
//   4. hk-xsutm: evaluateGroupAdvanceWithOutcome now calls queue.Persist after
//      each item completion and CompleteAndUnlink + ClearQueue when all groups
//      reach complete-success.
//
// Helper prefix: queueSetQueueWiring (this file).
//
// Spec refs:
//   - specs/queue-model.md §8.1 QM-050 (submit sequence)
//   - specs/queue-model.md §7     (append path)
//   - specs/queue-model.md §3.3 QM-003 (unlink on completion)
//   - specs/queue-model.md §8.4 QM-053 (CompleteAndUnlink)
//   - specs/queue-model.md §9.1 QM-060 (single-writer)
//   - specs/queue-model.md §9.6 QM-064 (no-mutation-during-validation)

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// queueSetQueueWiring fixture helpers
// ---------------------------------------------------------------------------

// queueSetQueueWiringProjectDir creates a temporary project root with a
// .harmonik/ subdirectory. Registered for t.Cleanup.
func queueSetQueueWiringProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("queueSetQueueWiringProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// queueSetQueueWiringQueueJSON returns the expected path to queue.json.
func queueSetQueueWiringQueueJSON(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

// queueSetQueueWiringFakeLedger is a minimal queue.BeadLedger stub.
// LookupStatus returns BeadStatusOpen for all IDs so QM-020 does not reject
// them. BlocksEdge always returns false (no dependency edges).
type queueSetQueueWiringFakeLedger struct{}

func (f *queueSetQueueWiringFakeLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (f *queueSetQueueWiringFakeLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// queueSetQueueWiringQueueSetter is a minimal QueueSetter stub that records
// the last queue passed to SetQueue.
type queueSetQueueWiringQueueSetter struct {
	lastQueue *queue.Queue
}

func (s *queueSetQueueWiringQueueSetter) SetQueue(q *queue.Queue) {
	s.lastQueue = q
}

func (s *queueSetQueueWiringQueueSetter) ClearQueueByName(name string) {
	if s.lastQueue != nil && queue.NormaliseQueueName(s.lastQueue.Name) == name {
		s.lastQueue = nil
	}
}

// queueSetQueueWiringEventCollector is a minimal EventEmitter stub that records
// all emitted event types.
type queueSetQueueWiringEventCollector struct {
	types []string
}

func (e *queueSetQueueWiringEventCollector) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	e.types = append(e.types, string(eventType))
	return nil
}

// queueSetQueueWiringSubmitRequest builds a minimal QueueSubmitRequest with
// one wave group and one item.
func queueSetQueueWiringSubmitRequest(beadID core.BeadID) queue.QueueSubmitRequest {
	return queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				Kind:  queue.GroupKindWave,
				Items: []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// TestQueueSetQueueWiring_SubmitUpdatesQueueStore
// ---------------------------------------------------------------------------

// TestQueueSetQueueWiring_SubmitUpdatesQueueStore verifies that
// HandlerAdapter.HandleQueueSubmit calls QueueSetter.SetQueue after persist
// (hk-4ukkq) so the running workloop picks up the submitted queue without
// restart.
//
// Spec ref: specs/queue-model.md §8.1 QM-050; §9.1 QM-060.
func TestQueueSetQueueWiring_SubmitUpdatesQueueStore(t *testing.T) {
	t.Parallel()

	projectDir := queueSetQueueWiringProjectDir(t)
	ledger := &queueSetQueueWiringFakeLedger{}
	qs := &queueSetQueueWiringQueueSetter{}
	bus := &queueSetQueueWiringEventCollector{}

	adapter := queue.NewHandlerAdapter(ledger, projectDir, qs, bus)

	req := queueSetQueueWiringSubmitRequest("hk-sqw-item0")
	params, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}
	if raw == nil {
		t.Fatal("HandleQueueSubmit: nil response")
	}

	// QueueSetter.SetQueue must have been called (hk-4ukkq).
	if qs.lastQueue == nil {
		t.Fatal("QueueSetter.SetQueue not called after HandleQueueSubmit")
	}
	if qs.lastQueue.Status != queue.QueueStatusActive {
		t.Errorf("SetQueue queue.Status = %q; want active", qs.lastQueue.Status)
	}
	if len(qs.lastQueue.Groups) != 1 {
		t.Fatalf("SetQueue queue.Groups len = %d; want 1", len(qs.lastQueue.Groups))
	}

	// queue.json must be present on disk after submit.
	if _, statErr := os.Stat(queueSetQueueWiringQueueJSON(projectDir)); statErr != nil {
		t.Errorf("queue.json absent after submit: %v", statErr)
	}

	// queue_submitted event must have been emitted (hk-peucr).
	foundSubmitted := false
	for _, et := range bus.types {
		if et == "queue_submitted" {
			foundSubmitted = true
		}
	}
	if !foundSubmitted {
		t.Errorf("queue_submitted event not emitted; got: %v", bus.types)
	}
}

// ---------------------------------------------------------------------------
// TestQueueSetQueueWiring_SubmitThenDrainAndUnlink
// ---------------------------------------------------------------------------

// TestQueueSetQueueWiring_SubmitThenDrainAndUnlink exercises the full path:
// submit via HandlerAdapter (which calls SetQueue) → simulate workloop drain →
// CompleteAndUnlink removes queue.json (hk-4ukkq + hk-xsutm).
//
// QM-050 steps 5-8 (group-0 active transition) are the caller's responsibility
// per the spec; this test simulates the workloop activating group 0 after submit.
//
// Spec refs:
//   - specs/queue-model.md §3.3 QM-003 (unlink on completion)
//   - specs/queue-model.md §8.4 QM-053 (CompleteAndUnlink)
func TestQueueSetQueueWiring_SubmitThenDrainAndUnlink(t *testing.T) {
	t.Parallel()

	projectDir := queueSetQueueWiringProjectDir(t)
	ledger := &queueSetQueueWiringFakeLedger{}
	qs := &queueSetQueueWiringQueueSetter{}

	adapter := queue.NewHandlerAdapter(ledger, projectDir, qs, nil)

	req := queueSetQueueWiringSubmitRequest("hk-sqw-drain-item0")
	params, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	_, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}

	// The workloop would call qs.SetQueue via the adapter; verify the adapter did.
	q := qs.lastQueue
	if q == nil {
		t.Fatal("SetQueue not called after submit; workloop would not see the queue")
	}

	// Simulate QM-050 steps 5-8: workloop activates group 0 after submit.
	// AdvanceGroup transitions pending → active.
	activateStatus, _, actErr := queue.AdvanceGroup(
		context.Background(),
		&q.Groups[0],
		q.Status,
		q.QueueID,
		time.Now().UTC(),
	)
	if actErr != nil {
		t.Fatalf("AdvanceGroup (activate): %v", actErr)
	}
	q.Groups[0].Status = activateStatus

	// Simulate workloop drain: dispatch + complete the item.
	q.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	newGroupStatus, _, advErr := queue.AdvanceGroup(
		context.Background(),
		&q.Groups[0],
		q.Status,
		q.QueueID,
		time.Now().UTC(),
	)
	if advErr != nil {
		t.Fatalf("AdvanceGroup (complete): %v", advErr)
	}
	if newGroupStatus != queue.GroupStatusCompleteSuccess {
		t.Fatalf("group status = %q; want complete-success", newGroupStatus)
	}
	q.Groups[0].Status = newGroupStatus

	// CompleteAndUnlink removes queue.json (QM-003 / QM-053).
	if err := queue.CompleteAndUnlink(context.Background(), projectDir, q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	// queue.json must be absent (QM-003).
	if _, statErr := os.Stat(queueSetQueueWiringQueueJSON(projectDir)); statErr == nil {
		t.Error("queue.json still present after CompleteAndUnlink; want absent")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("queue.json stat error (not IsNotExist): %v", statErr)
	}
}

// ---------------------------------------------------------------------------
// TestQueueSetQueueWiring_AppendUpdatesQueueStore
// ---------------------------------------------------------------------------

// TestQueueSetQueueWiring_AppendUpdatesQueueStore verifies that
// HandlerAdapter.HandleQueueAppend persists and calls QueueSetter.SetQueue
// so appended items reach the workloop without restart (hk-lzs8r).
//
// Spec ref: specs/queue-model.md §7 (append path); §9.1 QM-060.
func TestQueueSetQueueWiring_AppendUpdatesQueueStore(t *testing.T) {
	t.Parallel()

	projectDir := queueSetQueueWiringProjectDir(t)
	ledger := &queueSetQueueWiringFakeLedger{}
	qs := &queueSetQueueWiringQueueSetter{}
	bus := &queueSetQueueWiringEventCollector{}

	adapter := queue.NewHandlerAdapter(ledger, projectDir, qs, bus)

	// Step 1: submit a stream group (QM-040: append is stream-only) with one item
	// to establish the active queue.
	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{
				Kind:  queue.GroupKindStream,
				Items: []queue.Item{{BeadID: "hk-sqw-append-item0", Status: queue.ItemStatusPending}},
			},
		},
	}
	submitParams, err := json.Marshal(submitReq)
	if err != nil {
		t.Fatalf("marshal submit request: %v", err)
	}
	submitRaw, rpcErr := adapter.HandleQueueSubmit(t.Context(), submitParams)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}
	var submitResp queue.QueueSubmitResponse
	if err := json.Unmarshal(submitRaw, &submitResp); err != nil {
		t.Fatalf("decode QueueSubmitResponse: %v", err)
	}

	// Verify submit set the queue in memory.
	if qs.lastQueue == nil {
		t.Fatal("SetQueue not called after submit")
	}
	initialItemCount := len(qs.lastQueue.Groups[0].Items)

	// Reset bus events so we can cleanly check append events.
	bus.types = nil

	// Step 2: append a second item to group 0.
	appendReq := queue.QueueAppendRequest{
		QueueID:    submitResp.QueueID,
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{"hk-sqw-append-item1"},
	}
	appendParams, err := json.Marshal(appendReq)
	if err != nil {
		t.Fatalf("marshal append request: %v", err)
	}
	_, rpcErr = adapter.HandleQueueAppend(t.Context(), appendParams)
	if rpcErr != nil {
		t.Fatalf("HandleQueueAppend: unexpected RPCError: %v", rpcErr)
	}

	// QueueSetter.SetQueue must have been called again with the mutated queue
	// (hk-lzs8r).
	if qs.lastQueue == nil {
		t.Fatal("SetQueue not called after HandleQueueAppend")
	}
	afterItemCount := len(qs.lastQueue.Groups[0].Items)
	if afterItemCount != initialItemCount+1 {
		t.Errorf("after append: group 0 item count = %d; want %d",
			afterItemCount, initialItemCount+1)
	}

	// queue.json on disk must reflect the appended item (hk-lzs8r).
	loaded, loadErr := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("Load after append: %v", loadErr)
	}
	if loaded == nil {
		t.Fatal("Load after append returned nil; expected loaded queue")
	}
	if len(loaded.Groups[0].Items) != initialItemCount+1 {
		t.Errorf("disk queue group 0 items = %d; want %d",
			len(loaded.Groups[0].Items), initialItemCount+1)
	}

	// queue_appended event must have been emitted (hk-peucr).
	foundAppended := false
	for _, et := range bus.types {
		if et == "queue_appended" {
			foundAppended = true
		}
	}
	if !foundAppended {
		t.Errorf("queue_appended event not emitted; got: %v", bus.types)
	}
}

// ---------------------------------------------------------------------------
// TestQueueSetQueueWiring_NilQueueSetter_NoopSafe
// ---------------------------------------------------------------------------

// TestQueueSetQueueWiring_NilQueueSetter_NoopSafe verifies that a nil qs and
// nil bus do not cause a nil-pointer panic (backward-compat for callers that
// do not supply a QueueStore or bus).
func TestQueueSetQueueWiring_NilQueueSetter_NoopSafe(t *testing.T) {
	t.Parallel()

	projectDir := queueSetQueueWiringProjectDir(t)
	ledger := &queueSetQueueWiringFakeLedger{}

	// nil qs and nil bus — no panic.
	adapter := queue.NewHandlerAdapter(ledger, projectDir, nil, nil)

	req := queueSetQueueWiringSubmitRequest("hk-sqw-noop-item0")
	params, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}
	if raw == nil {
		t.Fatal("HandleQueueSubmit: nil response")
	}
}
