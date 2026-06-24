package queue_test

// set_concurrency_spawncap_hkvfeeo_test.go — unit tests for spawn-cap
// enforcement in HandleQueueSetConcurrency (hk-vfeeo).
//
// Coverage:
//   - SetCapRefuse: set-concurrency N*2 > spawnCap → error with spawn_cap_exceeded
//   - SetCapExact:  set-concurrency N*2 == spawnCap → allowed
//   - SetCapBelow:  set-concurrency N*2 < spawnCap → allowed
//   - SetCapUncapped: no spawnCapGet wired → any value allowed
//   - SetCapZero:   spawnCapGet returns 0 (uncapped substrate) → any value allowed
//   - ResponseSpawnCap: successful set-concurrency includes spawn_cap in response

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// spawnCapFixture builds a HandlerAdapter with a fake ledger, ConcurrencyFuncs,
// and an optional SpawnCapFunc.
func spawnCapFixture(initialN, spawnCap int) *queue.HandlerAdapter {
	tmpDir := "" // adapter can't run file ops so no projectDir needed for these tests
	// Use an existing exported constructor pattern from rpc_test.
	// We need a real adapter; use the exported constructor.
	a := queue.NewHandlerAdapter(nil, tmpDir, nil, nil)
	current := initialN
	a.SetConcurrencyFuncs(
		func() int { return current },
		func(n int) (int, error) {
			old := current
			current = n
			return old, nil
		},
	)
	if spawnCap > 0 {
		a.SetSpawnCapFunc(func() int { return spawnCap })
	}
	return a
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// TestSetConcurrency_SpawnCapRefuse verifies that set-concurrency N where
// N*2 > spawnCap is rejected with a spawn_cap_exceeded error.
func TestSetConcurrency_SpawnCapRefuse(t *testing.T) {
	t.Parallel()

	// spawnCap=4 → safe max = 2; requesting 3 → 3*2=6 > 4 → error
	a := spawnCapFixture(1, 4)
	params := mustMarshal(t, map[string]any{"n": 3})
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr == nil {
		t.Fatal("expected error for N*2 > spawnCap, got nil")
	}
	if rpcErr.Message != "spawn_cap_exceeded" {
		t.Errorf("error message = %q, want %q", rpcErr.Message, "spawn_cap_exceeded")
	}
	if rpcErr.Detail["spawn_cap"] != 4 {
		t.Errorf("detail[spawn_cap] = %v, want 4", rpcErr.Detail["spawn_cap"])
	}
	if rpcErr.Detail["safe_max"] != 2 {
		t.Errorf("detail[safe_max] = %v, want 2", rpcErr.Detail["safe_max"])
	}
}

// TestSetConcurrency_SpawnCapExact verifies that N*2 == spawnCap is allowed.
func TestSetConcurrency_SpawnCapExact(t *testing.T) {
	t.Parallel()

	// spawnCap=4 → safe max = 2; requesting 2 → 2*2=4 == 4 → OK
	a := spawnCapFixture(1, 4)
	params := mustMarshal(t, map[string]any{"n": 2})
	result, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error for N*2==spawnCap: %v", rpcErr)
	}
	var resp queue.QueueSetConcurrencyResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.NewN != 2 {
		t.Errorf("resp.NewN = %d, want 2", resp.NewN)
	}
	if resp.SpawnCap != 4 {
		t.Errorf("resp.SpawnCap = %d, want 4", resp.SpawnCap)
	}
}

// TestSetConcurrency_SpawnCapBelow verifies that N*2 < spawnCap is allowed.
func TestSetConcurrency_SpawnCapBelow(t *testing.T) {
	t.Parallel()

	// spawnCap=6 → safe max = 3; requesting 2 → 2*2=4 < 6 → OK
	a := spawnCapFixture(1, 6)
	params := mustMarshal(t, map[string]any{"n": 2})
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error for N*2 < spawnCap: %v", rpcErr)
	}
}

// TestSetConcurrency_SpawnCapUncapped verifies that when no SpawnCapFunc is
// wired, any concurrency value is accepted.
func TestSetConcurrency_SpawnCapUncapped(t *testing.T) {
	t.Parallel()

	a := spawnCapFixture(1, 0) // 0 = no SpawnCapFunc wired
	params := mustMarshal(t, map[string]any{"n": 100})
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error when uncapped: %v", rpcErr)
	}
}

// TestSetConcurrency_SpawnCapZero verifies that when SpawnCapFunc returns 0
// (uncapped substrate), any concurrency value is accepted.
func TestSetConcurrency_SpawnCapZero(t *testing.T) {
	t.Parallel()

	a := spawnCapFixture(1, 1) // will be replaced
	a.SetSpawnCapFunc(func() int { return 0 })
	params := mustMarshal(t, map[string]any{"n": 50})
	_, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error when spawnCap==0: %v", rpcErr)
	}
}

// TestSetConcurrency_ResponseIncludesSpawnCap verifies that the response
// includes spawn_cap on successful set-concurrency.
func TestSetConcurrency_ResponseIncludesSpawnCap(t *testing.T) {
	t.Parallel()

	// spawnCap=10 → safe max = 5; requesting 3 → 3*2=6 < 10 → OK
	a := spawnCapFixture(2, 10)
	params := mustMarshal(t, map[string]any{"n": 3})
	result, rpcErr := a.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		t.Fatalf("unexpected error: %v", rpcErr)
	}
	var resp queue.QueueSetConcurrencyResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.OldN != 2 {
		t.Errorf("resp.OldN = %d, want 2", resp.OldN)
	}
	if resp.NewN != 3 {
		t.Errorf("resp.NewN = %d, want 3", resp.NewN)
	}
	if resp.SpawnCap != 10 {
		t.Errorf("resp.SpawnCap = %d, want 10", resp.SpawnCap)
	}
}
