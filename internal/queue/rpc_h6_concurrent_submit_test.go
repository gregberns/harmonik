package queue_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

type h6FakeLocker struct{ mu sync.Mutex }

// SetQueue acquires mu, exactly like the real daemon.QueueStore whose queueMu is
// a non-reentrant sync.RWMutex. This is deliberate: if the submit path ever
// regresses to calling a.qs.SetQueue WHILE holding LockForMutationView, this
// test would deadlock — catching the self-deadlock class the adversarial review
// found (production held the lock and called SetQueue, re-locking queueMu).
func (f *h6FakeLocker) SetQueue(*queue.Queue) {
	f.mu.Lock()
	f.mu.Unlock() //nolint:staticcheck,gocritic // SA2001/badLock: intentional lock/unlock to model non-reentrant re-lock
}
func (f *h6FakeLocker) ClearQueueByName(string) {}
func (f *h6FakeLocker) Wake()                   {}
func (f *h6FakeLocker) LockForMutationView() queue.LockedQueueView {
	f.mu.Lock()
	return h6FakeView{f}
}

type h6FakeView struct{ f *h6FakeLocker }

func (h6FakeView) LockedQueueByName(string) *queue.Queue     { return nil }
func (h6FakeView) LockedSetQueueByName(string, *queue.Queue) {}
func (h6FakeView) LockedAllQueueNames() []string             { return nil }
func (v h6FakeView) Done()                                   { v.f.mu.Unlock() }

func TestHandlerAdapter_ConcurrentSubmit_SameName_ExactlyOneWins(t *testing.T) {
	t.Parallel()

	const n = 8
	beads := make([]core.BeadID, n)
	for i := range beads {
		beads[i] = core.BeadID("hk-h6-" + string(rune('a'+i)))
	}

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beads...)

	adapter := queue.NewHandlerAdapter(ledger, projectDir, &h6FakeLocker{}, nil)

	params := make([]json.RawMessage, n)
	for i := range params {
		req := queue.QueueSubmitRequest{
			SchemaVersion: 1,
			Groups:        []queue.Group{rpcFixtureWaveGroup(beads[i])},
		}
		raw, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal req %d: %v", i, err)
		}
		params[i] = raw
	}

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
		alreadyN  int
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(p json.RawMessage) {
			defer wg.Done()
			<-start
			_, rpcErr := adapter.HandleQueueSubmit(t.Context(), p)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case rpcErr == nil:
				successes++
			case rpcErr.Code == queue.ErrorCodeQueueAlreadyActive:
				alreadyN++
			default:
				t.Errorf("unexpected RPCError: code=%d msg=%q", rpcErr.Code, rpcErr.Message)
			}
		}(params[i])
	}
	close(start)
	wg.Wait()

	if successes != 1 {
		t.Fatalf("concurrent submits for the same new queue name: got %d successes, want exactly 1 "+
			"(H6: unserialised RMW lets multiple submits both Persist, last-writer-wins)", successes)
	}
	if alreadyN != n-1 {
		t.Fatalf("got %d queue_already_active rejections, want %d", alreadyN, n-1)
	}
}
