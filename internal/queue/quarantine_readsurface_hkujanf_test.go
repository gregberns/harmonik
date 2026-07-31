package queue_test

// quarantine_readsurface_hkujanf_test.go — the queue read commands report a
// quarantined queue.
//
// HandleQueueList and HandleQueueStatus answer from the queue files on disk. A
// quarantine lives in daemon memory, so the disk shows nothing: a queue that
// will never accept another write serialises exactly like a healthy one. The
// HandlerAdapter closes that gap by reading the quarantine off the QueueSetter
// it was already given. These tests pin that wiring, because the marker in the
// CLI renderer is dead code without it.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
// Bead ref: hk-ujanf.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// quarantineFixtureStore is a QueueSetter that also reports quarantines, the
// same pair of roles the daemon's real QueueStore plays.
type quarantineFixtureStore struct {
	shut map[string]error
}

func (s *quarantineFixtureStore) SetQueue(*queue.Queue)   {}
func (s *quarantineFixtureStore) ClearQueueByName(string) {}
func (s *quarantineFixtureStore) QuarantineReason(n string) error {
	return s.shut[n]
}

// quarantineFixturePersist writes one queue file so the disk-reading handlers
// find it.
func quarantineFixturePersist(t *testing.T, projectDir, name string, bead core.BeadID) {
	t.Helper()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-00000000" + name[:4],
		Name:          name,
		Status:        queue.QueueStatusActive,
		Groups:        []queue.Group{rpcFixtureWaveGroup(bead)},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist %s: %v", name, err)
	}
}

// The listing must carry the reason for the shut queue and leave the healthy
// one clean. Both queues are active on disk with pending items, so nothing else
// in the row separates them.
func TestHandlerAdapter_QueueListReportsTheQuarantine(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	quarantineFixturePersist(t, projectDir, "main", "hk-shut")
	quarantineFixturePersist(t, projectDir, "paul", "hk-healthy")

	store := &quarantineFixtureStore{shut: map[string]error{
		"main": errors.New("rename queue file: no space left on device"),
	}}
	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("hk-shut", "hk-healthy"), projectDir, store, nil)

	raw, rpcErr := adapter.HandleQueueList(t.Context())
	if rpcErr != nil {
		t.Fatalf("HandleQueueList: %+v", rpcErr)
	}
	var resp queue.QueueListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	reasons := make(map[string]string, len(resp.Queues))
	for _, summary := range resp.Queues {
		reasons[summary.Name] = summary.QuarantineReason
	}
	if got := reasons["main"]; got != "rename queue file: no space left on device" {
		t.Errorf("main quarantine_reason = %q; want the store's cause — the row reads as healthy without it", got)
	}
	if got := reasons["paul"]; got != "" {
		t.Errorf("paul quarantine_reason = %q; want empty — a healthy queue must not be marked", got)
	}
}

// queue-status had the same gap. It resolves one queue by name or by queue_id,
// so the lookup keys off the resolved queue rather than the request.
func TestHandlerAdapter_QueueStatusReportsTheQuarantine(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	quarantineFixturePersist(t, projectDir, "main", "hk-shut")

	store := &quarantineFixtureStore{shut: map[string]error{
		"main": errors.New("rename queue file: no space left on device"),
	}}
	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("hk-shut"), projectDir, store, nil)

	raw, rpcErr := adapter.HandleQueueStatus(t.Context(), json.RawMessage(`{"name":"main"}`))
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: %+v", rpcErr)
	}
	var resp queue.QueueStatusResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.QuarantineReason != "rename queue file: no space left on device" {
		t.Errorf("quarantine_reason = %q; want the store's cause", resp.QuarantineReason)
	}
}

// A QueueSetter that reports no quarantine leaves the response clean, and a
// QueueSetter that cannot report one at all must not break the command. Test
// harnesses and legacy callers wire the plain interface.
func TestHandlerAdapter_QueueListWithoutAQuarantineReader(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	quarantineFixturePersist(t, projectDir, "main", "hk-healthy")

	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger("hk-healthy"), projectDir, nil, nil)

	raw, rpcErr := adapter.HandleQueueList(t.Context())
	if rpcErr != nil {
		t.Fatalf("HandleQueueList: %+v", rpcErr)
	}
	var resp queue.QueueListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Queues) != 1 {
		t.Fatalf("queues = %d; want 1", len(resp.Queues))
	}
	if resp.Queues[0].QuarantineReason != "" {
		t.Errorf("quarantine_reason = %q; want empty", resp.Queues[0].QuarantineReason)
	}
}
