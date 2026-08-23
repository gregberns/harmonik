package queue_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

type capFixture struct {
	adapter *queue.HandlerAdapter
	// cap is the live spawn cap, in non-terminal session slots.
	cap int
	// setCalls counts resize calls, so a test can tell "left alone" from
	// "re-set to the value it already had".
	setCalls int
	// concurrency is the dispatch ceiling the controller holds.
	concurrency int
}

func newCapFixture(startupCap, hostCeiling int) *capFixture {
	f := &capFixture{cap: startupCap, concurrency: startupCap / 2}
	f.adapter = queue.NewHandlerAdapter(nil, "", nil, nil)
	f.adapter.SetConcurrencyFuncs(
		func() int { return f.concurrency },
		func(n int) (int, error) {
			old := f.concurrency
			f.concurrency = n
			return old, nil
		},
	)
	f.adapter.SetSpawnCapFunc(func() int { return f.cap })
	f.adapter.SetSpawnCapSetFunc(func(n int) {
		f.cap = n
		f.setCalls++
	})
	f.adapter.SetSpawnCapBounds(startupCap, hostCeiling)
	return f
}

func (f *capFixture) setConcurrency(t *testing.T, n int) (reportedCap int, rpcErr *queue.RPCError) {
	t.Helper()
	params, err := json.Marshal(map[string]any{"n": n})
	if err != nil {
		t.Fatalf("marshal set-concurrency request: %v", err)
	}
	raw, rpcErr := f.adapter.HandleQueueSetConcurrency(context.Background(), params)
	if rpcErr != nil {
		return 0, rpcErr
	}
	var resp struct {
		SpawnCap int `json:"spawn_cap"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode set-concurrency response: %v", err)
	}
	return resp.SpawnCap, nil
}

// TestSetConcurrency_CapComesBackDown is the ratchet itself, in the shape the
// assessor reproduced against a live daemon: raise, then undo, then check what
// the daemon is actually holding rather than what it printed.
func TestSetConcurrency_CapComesBackDown(t *testing.T) {
	f := newCapFixture(2, 64) // daemon started at max_concurrent 1

	if _, rpcErr := f.setConcurrency(t, 8); rpcErr != nil {
		t.Fatalf("set-concurrency 8 was refused under a ceiling of 64: %+v", rpcErr.Detail)
	}
	if f.cap != 16 {
		t.Fatalf("after set-concurrency 8 the spawn cap is %d, want 16 (2 sessions per bead)", f.cap)
	}

	reported, rpcErr := f.setConcurrency(t, 1)
	if rpcErr != nil {
		t.Fatalf("set-concurrency 1 was refused: %+v", rpcErr.Detail)
	}
	if f.cap != 2 {
		t.Errorf("the spawn cap RATCHETED: after undoing back to 1 it still holds %d slots, want 2 — the operator's undo did not reach the thing that matters", f.cap)
	}
	if reported != f.cap {
		t.Errorf("the response reports spawn_cap %d while the daemon holds %d; a readback the operator cannot act on is worse than none", reported, f.cap)
	}
}

// TestSetConcurrency_LowNCannotDiscardTheOperatorsFloor covers the defect a
// naive ratchet fix would introduce. The operator declared a cap of 40 at
// startup with HARMONIK_MAX_CONCURRENT_SESSIONS. A later set-concurrency 1 must
// not throw that away.
func TestSetConcurrency_LowNCannotDiscardTheOperatorsFloor(t *testing.T) {
	f := newCapFixture(40, 64) // operator declared 40 slots at startup

	if _, rpcErr := f.setConcurrency(t, 1); rpcErr != nil {
		t.Fatalf("set-concurrency 1 was refused: %+v", rpcErr.Detail)
	}
	if f.cap != 40 {
		t.Errorf("set-concurrency 1 took the spawn cap to %d, discarding the operator's declared startup cap of 40", f.cap)
	}
	if f.setCalls != 0 {
		t.Errorf("the handler resized the cap %d times when the target equalled the cap already in force; it should leave it alone", f.setCalls)
	}
}

// TestSetConcurrency_RaiseAboveHostBoundIsRefused is the typo the bead was
// filed for. It must be refused, and the refusal must tell the operator the
// bound, the safe value, and how to declare a higher one deliberately.
func TestSetConcurrency_RaiseAboveHostBoundIsRefused(t *testing.T) {
	f := newCapFixture(2, 64)

	_, rpcErr := f.setConcurrency(t, 999999)
	if rpcErr == nil {
		t.Fatalf("set-concurrency 999999 was ACCEPTED against a host bound of 64 slots; the daemon now holds %d", f.cap)
	}
	if f.cap != 2 {
		t.Errorf("a refused request still moved the spawn cap to %d, want it untouched at 2", f.cap)
	}
	if f.concurrency != 1 {
		t.Errorf("a refused request still moved max_concurrent to %d, want it untouched at 1", f.concurrency)
	}
	if got := rpcErr.Message; !strings.HasPrefix(got, "spawn_cap_exceeded") {
		t.Errorf("refusal message is %q, want it to start with spawn_cap_exceeded — the token existing surface callers match on", got)
	}
	if got, want := rpcErr.Detail["host_bound"], 64; got != want {
		t.Errorf("refusal reports host_bound %v, want %v", got, want)
	}
	if got, want := rpcErr.Detail["safe_max"], 32; got != want {
		t.Errorf("refusal reports safe_max %v, want %v (the bound, not the request)", got, want)
	}
}

// TestSetConcurrency_RaiseWithinHostBoundStillWorks pins that the bound did not
// quietly revert hk-omvan. Scaling real throughput with no daemon restart is
// the whole point of the live resize, and a fix that broke it would trade one
// defect for a worse one.
func TestSetConcurrency_RaiseWithinHostBoundStillWorks(t *testing.T) {
	f := newCapFixture(2, 64)

	reported, rpcErr := f.setConcurrency(t, 32) // exactly the bound: 64 slots
	if rpcErr != nil {
		t.Fatalf("set-concurrency 32 was refused at exactly the host bound of 64 slots: %+v", rpcErr.Detail)
	}
	if f.cap != 64 {
		t.Errorf("spawn cap is %d after raising to the bound, want 64", f.cap)
	}
	if reported != 64 {
		t.Errorf("response reports spawn_cap %d, want 64", reported)
	}
}

// TestSetConcurrency_CeilingNeverRefusesTheOperatorsOwnFloor covers a host
// whose measured bound is BELOW what the operator explicitly asked for at
// startup. Refusing there would refuse the operator's own declared
// configuration, so the floor wins.
func TestSetConcurrency_CeilingNeverRefusesTheOperatorsOwnFloor(t *testing.T) {
	f := newCapFixture(200, 16) // operator declared 200 slots on a small box

	if _, rpcErr := f.setConcurrency(t, 100); rpcErr != nil {
		t.Fatalf("set-concurrency 100 was refused although the operator declared 200 slots at startup: %+v", rpcErr.Detail)
	}
	if f.cap != 200 {
		t.Errorf("spawn cap is %d, want the operator's declared 200 held", f.cap)
	}

	if _, rpcErr := f.setConcurrency(t, 101); rpcErr == nil {
		t.Errorf("set-concurrency 101 (202 slots) was accepted; the ceiling should sit at the operator's floor of 200, not vanish")
	}
}

// TestSetConcurrency_NoLiveResizeStillRefusesWithTheOlderDetail pins the
// pre-hk-omvan substrate, where the cap is fixed at startup. The refusal there
// is the wording the ceiling refusal above was modelled on, and it must keep
// working unchanged.
func TestSetConcurrency_NoLiveResizeStillRefusesWithTheOlderDetail(t *testing.T) {
	f := &capFixture{cap: 4, concurrency: 2}
	f.adapter = queue.NewHandlerAdapter(nil, "", nil, nil)
	f.adapter.SetConcurrencyFuncs(
		func() int { return f.concurrency },
		func(n int) (int, error) { old := f.concurrency; f.concurrency = n; return old, nil },
	)
	f.adapter.SetSpawnCapFunc(func() int { return f.cap })

	_, rpcErr := f.setConcurrency(t, 8)
	if rpcErr == nil {
		t.Fatalf("a substrate with no live resize accepted a request that oversubscribes its fixed cap")
	}
	if got := rpcErr.Message; !strings.HasPrefix(got, "spawn_cap_exceeded") {
		t.Errorf("refusal message is %q, want it to start with spawn_cap_exceeded", got)
	}
	if got, want := rpcErr.Detail["safe_max"], 2; got != want {
		t.Errorf("refusal reports safe_max %v, want %v", got, want)
	}

	if _, rpcErr := f.setConcurrency(t, 1); rpcErr != nil {
		t.Errorf("set-concurrency 1 was refused on a fixed-cap substrate: %+v", rpcErr.Detail)
	}
}
