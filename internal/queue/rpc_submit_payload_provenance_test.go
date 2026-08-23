package queue_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

type provenanceCollector struct {
	mu       sync.Mutex
	payloads map[core.EventType][]byte
}

func newProvenanceCollector() *provenanceCollector {
	return &provenanceCollector{payloads: make(map[core.EventType][]byte)}
}

func (c *provenanceCollector) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	stored := make([]byte, len(payload))
	copy(stored, payload)
	c.payloads[eventType] = stored
	return nil
}

func (c *provenanceCollector) submitted(t *testing.T) core.QueueSubmittedPayload {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.payloads[core.EventTypeQueueSubmitted]
	if !ok {
		t.Fatalf("no %s event was emitted", core.EventTypeQueueSubmitted)
	}
	var got core.QueueSubmittedPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %s payload: %v", core.EventTypeQueueSubmitted, err)
	}
	return got
}

type provenanceLocker struct {
	mu sync.Mutex
}

func (f *provenanceLocker) SetQueue(*queue.Queue)   {}
func (f *provenanceLocker) ClearQueueByName(string) {}
func (f *provenanceLocker) Wake()                   {}
func (f *provenanceLocker) LockForMutationView() queue.LockedQueueView {
	f.mu.Lock()
	return provenanceView{f}
}

type provenanceView struct{ f *provenanceLocker }

func (provenanceView) LockedQueueByName(string) *queue.Queue { return nil }

// LockedSetQueueByName stands in for the moment the work loop gains access to
// the queue. It wipes the groups so any post-publish read is visible.
func (provenanceView) LockedSetQueueByName(_ string, q *queue.Queue) {
	if q != nil {
		q.Groups = nil
	}
}

func (provenanceView) LockedAllQueueNames() []string { return nil }
func (v provenanceView) Done()                       { v.f.mu.Unlock() }

type provenancePlainSetter struct{}

func (provenancePlainSetter) SetQueue(q *queue.Queue) {
	if q != nil {
		q.Groups = nil
	}
}

func (provenancePlainSetter) ClearQueueByName(string) {}

func provenanceSubmitOneGroupThreeBeads(t *testing.T, qs queue.QueueSetter) *provenanceCollector {
	t.Helper()

	beads := []core.BeadID{"hk-prov-a", "hk-prov-b", "hk-prov-c"}
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beads...)
	bus := newProvenanceCollector()

	adapter := queue.NewHandlerAdapter(ledger, projectDir, qs, bus)

	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(beads...)},
	}
	params, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal submit request: %v", err)
	}

	raw, rpcErr := adapter.HandleQueueSubmit(t.Context(), params)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}

	var resp queue.QueueSubmitResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal submit response: %v", err)
	}
	if resp.GroupCount != 1 {
		t.Errorf("response group_count = %d, want 1 — the response saw the published queue", resp.GroupCount)
	}
	return bus
}

func provenanceAssertCountsFromPrePublish(t *testing.T, got core.QueueSubmittedPayload) {
	t.Helper()
	if got.GroupCount != 1 {
		t.Errorf("group_count = %d, want 1 — the payload was built from the published queue, after the store took it", got.GroupCount)
	}
	if got.TotalBeadCount != 3 {
		t.Errorf("total_bead_count = %d, want 3 — the payload was built from the published queue, after the store took it", got.TotalBeadCount)
	}
	if got.QueueID == "" {
		t.Error("queue_id is empty, want the minted id")
	}
	if got.SubmittedAt == "" {
		t.Error("submitted_at is empty, want the accept-time stamp")
	}
}

// TestHandleQueueSubmit_PayloadBuiltBeforePublish_LockedPath covers the path the
// daemon uses: the QueueSetter also implements MutationLocker.
func TestHandleQueueSubmit_PayloadBuiltBeforePublish_LockedPath(t *testing.T) {
	t.Parallel()

	bus := provenanceSubmitOneGroupThreeBeads(t, &provenanceLocker{})
	provenanceAssertCountsFromPrePublish(t, bus.submitted(t))
}

// TestHandleQueueSubmit_PayloadBuiltBeforePublish_UnlockedFallback covers the
// path taken when the QueueSetter does not implement MutationLocker. That path
// holds no lock at all, so it was the more exposed of the two.
func TestHandleQueueSubmit_PayloadBuiltBeforePublish_UnlockedFallback(t *testing.T) {
	t.Parallel()

	bus := provenanceSubmitOneGroupThreeBeads(t, provenancePlainSetter{})
	provenanceAssertCountsFromPrePublish(t, bus.submitted(t))
}
