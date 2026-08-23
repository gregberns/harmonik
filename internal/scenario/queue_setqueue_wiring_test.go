package scenario

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

func queueSetQueueWiringQueueJSON(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

type queueSetQueueWiringFakeLedger struct{}

func (f *queueSetQueueWiringFakeLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (f *queueSetQueueWiringFakeLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

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

type queueSetQueueWiringEventCollector struct {
	types []string
}

func (e *queueSetQueueWiringEventCollector) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	e.types = append(e.types, string(eventType))
	return nil
}

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

	if qs.lastQueue == nil {
		t.Fatal("QueueSetter.SetQueue not called after HandleQueueSubmit")
	}
	if qs.lastQueue.Status != queue.QueueStatusActive {
		t.Errorf("SetQueue queue.Status = %q; want active", qs.lastQueue.Status)
	}
	if len(qs.lastQueue.Groups) != 1 {
		t.Fatalf("SetQueue queue.Groups len = %d; want 1", len(qs.lastQueue.Groups))
	}

	if _, statErr := os.Stat(queueSetQueueWiringQueueJSON(projectDir)); statErr != nil {
		t.Errorf("queue.json absent after submit: %v", statErr)
	}

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

	q := qs.lastQueue
	if q == nil {
		t.Fatal("SetQueue not called after submit; workloop would not see the queue")
	}

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

	if err := queue.CompleteAndUnlink(context.Background(), projectDir, q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	if _, statErr := os.Stat(queueSetQueueWiringQueueJSON(projectDir)); statErr == nil {
		t.Error("queue.json still present after CompleteAndUnlink; want absent")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("queue.json stat error (not IsNotExist): %v", statErr)
	}
}

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

	if qs.lastQueue == nil {
		t.Fatal("SetQueue not called after submit")
	}
	initialItemCount := len(qs.lastQueue.Groups[0].Items)

	bus.types = nil

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

	if qs.lastQueue == nil {
		t.Fatal("SetQueue not called after HandleQueueAppend")
	}
	afterItemCount := len(qs.lastQueue.Groups[0].Items)
	if afterItemCount != initialItemCount+1 {
		t.Errorf("after append: group 0 item count = %d; want %d",
			afterItemCount, initialItemCount+1)
	}

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

// TestQueueSetQueueWiring_NilQueueSetter_NoopSafe verifies that a nil qs and
// nil bus do not cause a nil-pointer panic (backward-compat for callers that
// do not supply a QueueStore or bus).
func TestQueueSetQueueWiring_NilQueueSetter_NoopSafe(t *testing.T) {
	t.Parallel()

	projectDir := queueSetQueueWiringProjectDir(t)
	ledger := &queueSetQueueWiringFakeLedger{}

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
