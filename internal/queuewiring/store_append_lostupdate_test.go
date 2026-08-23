package queuewiring_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

type b1OpenLedger struct{}

func (b1OpenLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func (b1OpenLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

func TestQueueAppend_ConcurrentStatusMutation_NoLostUpdate(t *testing.T) {
	t.Parallel()

	const n = 60 // iterations = seed items = appends

	projectDir := t.TempDir()
	ctx := context.Background()
	ledger := b1OpenLedger{}

	seedItems := make([]queue.Item, n)
	for i := range seedItems {
		seedItems[i] = queue.Item{
			BeadID: core.BeadID(fmt.Sprintf("b1-seed-%03d", i)),
			Status: queue.ItemStatusPending,
		}
	}
	_, q, _, rpcErr := queue.HandleQueueSubmit(ctx, queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			{Kind: queue.GroupKindStream, Items: seedItems},
		},
	}, ledger, projectDir, 0)
	require.Nil(t, rpcErr, "seed HandleQueueSubmit: %v", rpcErr)
	require.NotNil(t, q)

	qs := queuewiring.NewQueueStore()
	qs.SetQueue(q)

	adapter := queue.NewHandlerAdapter(ledger, projectDir, qs, nil)

	mutateOK := make([]bool, n)
	appendOK := make([]bool, n)

	for i := 0; i < n; i++ {
		start := make(chan struct{})
		errC := make(chan error, 2) // goroutine-safe failure capture (no require off the test goroutine)
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			lq := qs.LockForMutation()
			defer lq.Done()
			liveQ := lq.Queue()
			if liveQ == nil {
				errC <- fmt.Errorf("iteration %d: nil main queue in store", i)
				return
			}
			liveQ.Groups[0].Items[i].Status = queue.ItemStatusDispatched
			if err := queue.Persist(ctx, projectDir, liveQ); err != nil {
				t.Logf("iteration %d: status-mutation persist errored: %v", i, err)
				return
			}
			lq.SetQueue(liveQ)
			mutateOK[i] = true
		}()

		go func() {
			defer wg.Done()
			<-start
			params, err := json.Marshal(queue.QueueAppendRequest{
				GroupIndex: 0,
				BeadIDs:    []core.BeadID{core.BeadID(fmt.Sprintf("b1-app-%03d", i))},
			})
			if err != nil {
				errC <- fmt.Errorf("iteration %d: marshal append request: %w", i, err)
				return
			}
			if _, appendErr := adapter.HandleQueueAppend(ctx, params); appendErr != nil {
				t.Logf("iteration %d: append errored: %v", i, appendErr)
				return
			}
			appendOK[i] = true
		}()

		close(start)
		wg.Wait()
		close(errC)
		for err := range errC {
			require.NoError(t, err)
		}
	}

	diskQ, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	require.NoError(t, err, "load queue.json after settle")
	require.NotNil(t, diskQ)
	memQ := qs.Queue()
	require.NotNil(t, memQ)

	okMutations, okAppends := 0, 0
	for i := 0; i < n; i++ {
		if mutateOK[i] {
			okMutations++
		}
		if appendOK[i] {
			okAppends++
		}
	}
	if okMutations != n || okAppends != n {
		t.Errorf("write errors under concurrency: %d/%d status mutations and %d/%d appends succeeded (B1: unserialised Persist)",
			okMutations, n, okAppends, n)
	}

	for name, got := range map[string]*queue.Queue{"disk queue.json": diskQ, "in-memory store": memQ} {
		statusByID := make(map[core.BeadID]queue.ItemStatus, len(got.Groups[0].Items))
		for _, it := range got.Groups[0].Items {
			statusByID[it.BeadID] = it.Status
		}
		lostMutations, lostAppends := 0, 0
		for i := 0; i < n; i++ {
			seedID := core.BeadID(fmt.Sprintf("b1-seed-%03d", i))
			if mutateOK[i] && statusByID[seedID] != queue.ItemStatusDispatched {
				lostMutations++
				t.Errorf("%s: lost STATUS MUTATION: item %s status = %q, want %q",
					name, seedID, statusByID[seedID], queue.ItemStatusDispatched)
			}
			appID := core.BeadID(fmt.Sprintf("b1-app-%03d", i))
			if _, ok := statusByID[appID]; appendOK[i] && !ok {
				lostAppends++
				t.Errorf("%s: lost APPEND: item %s absent from group 0", name, appID)
			}
		}
		if lostMutations+lostAppends > 0 {
			t.Errorf("%s: two-writer lost-update — %d/%d acknowledged status mutations and %d/%d acknowledged appends lost (B1)",
				name, lostMutations, okMutations, lostAppends, okAppends)
		}
	}
}
