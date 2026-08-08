package queue_test

// rpc_submit_payload_provenance_test.go — the queue_submitted payload MUST be
// computed from the queue as it stood BEFORE publication (hk-e7y44).
//
// # Why this test exists, and why it does not use the race detector
//
// HandlerAdapter.HandleQueueSubmit publishes the queue into the shared store and
// wakes the work loop. From that moment the work loop owns the object and writes
// q.Groups[i].Status through daemon.activateFirstPendingGroupLocked. The adapter
// used to build its queue_submitted payload AFTER that handover, and counted the
// beads with a range over q.Groups. A range copies each Group by value, Status
// included, so the count was a read of a field another goroutine was writing.
//
// The race detector does catch the original defect, but only sometimes: the
// scenario-tier test that reported it failed 4 times in 8 isolated runs. A
// coin-flip is a weak guard against a reintroduction. These tests assert the
// same property deterministically and without -race, so they gate on every run
// of the ordinary test target rather than on a scheduler coincidence.
//
// # What is asserted
//
// The property is PROVENANCE: the payload comes from the pre-publish queue. To
// make provenance observable, the fakes below mutate the queue destructively at
// the instant of publication — they clear q.Groups. That is deliberately not
// what the real scheduler does (it only flips a group's Status, which leaves
// both counts unchanged). It is an amplifier: any payload field still being read
// from the shared object after publication collapses to a zero value and the
// test fails loudly. Pre-fix these read group_count 0 and total_bead_count 0.
//
// Helper prefix: provenance (this file).
//
// Spec refs:
//   - specs/queue-model.md §8.1 QM-050 (submit sequence)
//   - specs/event-model.md  §8.10.1    (queue_submitted payload shape — QM-050
//     hands event payload schemas to event-model, so the shape is NOT owned by
//     queue-model §2.10, which is the JSON-RPC request/response schemas)
//   - specs/queue-model.md §9.1 QM-060 (single-writer)

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// provenanceCollector records the payload of every emitted event, keyed by type.
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

// provenanceLocker is a QueueSetter + MutationLocker double that models the
// PRODUCTION submit path. LockedSetQueueByName is the handover point, so it
// clears the queue's groups there: after this call the adapter must not read
// anything through the queue pointer.
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

// provenancePlainSetter is a QueueSetter WITHOUT MutationLocker, which selects
// the adapter's unlocked fallback path. It wipes the groups for the same reason.
type provenancePlainSetter struct{}

func (provenancePlainSetter) SetQueue(q *queue.Queue) {
	if q != nil {
		q.Groups = nil
	}
}

func (provenancePlainSetter) ClearQueueByName(string) {}

// provenanceSubmitOneGroupThreeBeads submits a single wave group of three beads through
// the adapter and returns the collected events.
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

	// The RPC response is marshalled AFTER the publish block. That is safe only
	// because QueueSubmitResponse carries scalars and never aliases into the
	// queue. Assert its count as well, so a future field that DID alias fails
	// here instead of becoming a second race in the same function.
	var resp queue.QueueSubmitResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal submit response: %v", err)
	}
	if resp.GroupCount != 1 {
		t.Errorf("response group_count = %d, want 1 — the response saw the published queue", resp.GroupCount)
	}
	return bus
}

// provenanceAssertCountsFromPrePublish checks the two payload fields that the
// pre-fix code read through the published pointer.
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
