package queue_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

type appendIntentStore struct {
	mu    sync.Mutex
	q     *queue.Queue
	wakes int
}

func (s *appendIntentStore) SetQueue(q *queue.Queue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.q = q
}

func (*appendIntentStore) ClearQueueByName(string) {}

func (s *appendIntentStore) Wake() {
	s.wakes++
}

func (s *appendIntentStore) LockForMutationView() queue.LockedQueueView {
	s.mu.Lock()
	return appendIntentView{s: s}
}

type appendIntentView struct{ s *appendIntentStore }

func (v appendIntentView) LockedQueueByName(name string) *queue.Queue {
	if v.s.q != nil && queue.NormaliseQueueName(v.s.q.Name) == queue.NormaliseQueueName(name) {
		return v.s.q
	}
	return nil
}

func (v appendIntentView) LockedSetQueueByName(_ string, q *queue.Queue) { v.s.q = q }
func (v appendIntentView) LockedAllQueueNames() []string                 { return []string{"main"} }
func (v appendIntentView) Done()                                         { v.s.mu.Unlock() }

type appendIntentBus struct {
	t          *testing.T
	projectDir string
	beadID     core.BeadID
	calls      int
}

func (b *appendIntentBus) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	b.t.Helper()
	q, err := queue.Load(ctx, b.projectDir, queue.QueueNameMain)
	if err != nil {
		b.t.Fatalf("Load during Emit: %v", err)
	}
	if q == nil || len(q.Groups) != 1 || len(q.Groups[0].Items) != 1 || q.Groups[0].Items[0].BeadID != b.beadID {
		b.t.Fatalf("Emit ran before appended queue was durable: queue=%+v", q)
	}
	if eventType != core.EventTypeQueueAppended {
		b.t.Fatalf("event type = %q, want %q", eventType, core.EventTypeQueueAppended)
	}
	var got core.QueueAppendedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		b.t.Fatalf("decode append payload: %v", err)
	}
	b.calls++
	return nil
}

func appendIntentQueue() *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0197b5e1-0000-7000-8000-000000000001",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
		}},
	}
}

func appendIntentRequest(t *testing.T, beadID core.BeadID) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(queue.QueueAppendRequest{
		Name:       queue.QueueNameMain,
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{beadID},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return raw
}

func TestHandleQueueAppendPersistsBeforeItEmitsIntent(t *testing.T) {
	const beadID = core.BeadID("hk-append-intent-order")
	projectDir := rpcFixtureTempProjectDir(t)
	store := &appendIntentStore{q: appendIntentQueue()}
	bus := &appendIntentBus{t: t, projectDir: projectDir, beadID: beadID}
	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger(beadID), projectDir, store, bus)

	if _, rpcErr := adapter.HandleQueueAppend(t.Context(), appendIntentRequest(t, beadID)); rpcErr != nil {
		t.Fatalf("HandleQueueAppend: %+v", rpcErr)
	}
	if bus.calls != 1 {
		t.Fatalf("emit calls = %d, want 1", bus.calls)
	}
	if store.wakes != 1 {
		t.Fatalf("wake calls = %d, want 1", store.wakes)
	}
}

func TestHandleQueueAppendPersistFailureEmitsNoIntent(t *testing.T) {
	const beadID = core.BeadID("hk-append-intent-fail")
	root := t.TempDir()
	projectDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(projectDir, []byte("file"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	store := &appendIntentStore{q: appendIntentQueue()}
	bus := &appendIntentBus{t: t, projectDir: projectDir, beadID: beadID}
	adapter := queue.NewHandlerAdapter(rpcFixtureOpenLedger(beadID), projectDir, store, bus)

	if _, rpcErr := adapter.HandleQueueAppend(t.Context(), appendIntentRequest(t, beadID)); rpcErr == nil {
		t.Fatal("HandleQueueAppend succeeded, want persist failure")
	}
	if bus.calls != 0 {
		t.Fatalf("emit calls = %d, want 0 after persist failure", bus.calls)
	}
	if len(store.q.Groups[0].Items) != 0 {
		t.Fatalf("live queue changed after persist failure: %+v", store.q.Groups[0].Items)
	}
}
