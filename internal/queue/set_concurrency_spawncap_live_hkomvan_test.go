package queue_test

// set_concurrency_spawncap_live_hkomvan_test.go — unit tests for the
// live-resize spawn-cap raise in HandleQueueSetConcurrency (hk-omvan,
// follow-up to hk-vfeeo).
//
// Coverage:
//   - RaisesCap: SetSpawnCapFunc wired + N*2 > spawnCap → cap is raised to
//     N*2 (via the wired setter) instead of being refused
//   - FallsBackToRefuse: SetSpawnCapFunc NOT wired + N*2 > spawnCap → the
//     hk-vfeeo refuse-with-detail behaviour still applies
//   - NoRaiseWhenNotOversubscribing: N*2 <= spawnCap → setter is never called

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// TestSetConcurrency_LiveResize_RaisesCap verifies that when a live spawn-cap
// setter is wired, an oversubscribing set-concurrency request raises the cap
// to N*2 instead of being refused.
func TestSetConcurrency_LiveResize_RaisesCap(t *testing.T) {
	t.Parallel()

	spawnCap := 4 // safe max = 2
	var raisedTo int
	raiseCalls := 0

	a := queue.NewHandlerAdapter(nil, "", nil, nil)
	current := 1
	a.SetConcurrencyFuncs(
		func() int { return current },
		func(n int) (int, error) {
			old := current
			current = n
			return old, nil
		},
	)
	a.SetSpawnCapFunc(func() int { return spawnCap })
	a.SetSpawnCapSetFunc(func(n int) {
		raiseCalls++
		raisedTo = n
		spawnCap = n // simulate the substrate actually resizing
	})

	params := mustMarshal(t, map[string]any{"n": 3}) // 3*2=6 > 4
	result, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error with live resize wired: %v", rpcErr)
	}
	if raiseCalls != 1 {
		t.Fatalf("spawnCapSet call count = %d, want 1", raiseCalls)
	}
	if raisedTo != 6 {
		t.Errorf("spawnCapSet called with n=%d, want 6 (N*2)", raisedTo)
	}

	var resp queue.QueueSetConcurrencyResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.NewN != 3 {
		t.Errorf("resp.NewN = %d, want 3", resp.NewN)
	}
	if resp.SpawnCap != 6 {
		t.Errorf("resp.SpawnCap = %d, want 6 (raised)", resp.SpawnCap)
	}
}

// TestSetConcurrency_LiveResize_FallsBackToRefuse verifies that when no live
// spawn-cap setter is wired, the hk-vfeeo refuse-with-detail behaviour is
// preserved (the substrate predates SetSpawnCap, or was constructed without
// WithSpawnCap).
func TestSetConcurrency_LiveResize_FallsBackToRefuse(t *testing.T) {
	t.Parallel()

	a := queue.NewHandlerAdapter(nil, "", nil, nil)
	current := 1
	a.SetConcurrencyFuncs(
		func() int { return current },
		func(n int) (int, error) {
			old := current
			current = n
			return old, nil
		},
	)
	a.SetSpawnCapFunc(func() int { return 4 }) // no SetSpawnCapSetFunc wired

	params := mustMarshal(t, map[string]any{"n": 3}) // 3*2=6 > 4
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected refuse-with-detail error when no live setter is wired, got nil")
	}
	if rpcErr.Message != "spawn_cap_exceeded" {
		t.Errorf("error message = %q, want %q", rpcErr.Message, "spawn_cap_exceeded")
	}
	if current != 1 {
		t.Errorf("concurrencySet must not have been called on refusal; current = %d, want 1", current)
	}
}

// TestSetConcurrency_LiveResize_NoRaiseWhenNotOversubscribing verifies that
// the live setter is never invoked when the request does not oversubscribe
// the existing cap.
func TestSetConcurrency_LiveResize_NoRaiseWhenNotOversubscribing(t *testing.T) {
	t.Parallel()

	raiseCalls := 0
	a := queue.NewHandlerAdapter(nil, "", nil, nil)
	current := 1
	a.SetConcurrencyFuncs(
		func() int { return current },
		func(n int) (int, error) {
			old := current
			current = n
			return old, nil
		},
	)
	a.SetSpawnCapFunc(func() int { return 10 })
	a.SetSpawnCapSetFunc(func(n int) { raiseCalls++ })

	params := mustMarshal(t, map[string]any{"n": 3}) // 3*2=6 <= 10
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	if raiseCalls != 0 {
		t.Errorf("spawnCapSet call count = %d, want 0 (not oversubscribing)", raiseCalls)
	}
}
