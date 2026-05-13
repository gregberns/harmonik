package daemon_test

// hookrelay_waitforoutcome_hkgql2020_test.go — tests for hookSessionStore.WaitForOutcome
// (hk-gql20.20).
//
// Covers:
//   1. Outcome already present when WaitForOutcome is called → returns immediately.
//   2. WaitForOutcome blocks, then unblocks on later outcome arrival.
//   3. ctx cancel before outcome arrives → returns ctx.Err().
//   4. Multiple concurrent waiters on the same session all unblock.
//
// Helper prefix: waitOutcomeFixture (implementer-protocol.md §Helper-prefix discipline;
// bead hk-gql20.20).

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// waitOutcomeFixtureMakePayload returns a JSON payload for use in WaitForOutcome tests.
func waitOutcomeFixtureMakePayload(t *testing.T, kind, summary string) json.RawMessage {
	t.Helper()
	pl, err := json.Marshal(map[string]string{"kind": kind, "summary": summary})
	if err != nil {
		t.Fatalf("waitOutcomeFixtureMakePayload: marshal: %v", err)
	}
	return pl
}

// waitOutcomeFixtureUnmarshal unmarshals a RawMessage into a map and fails the
// test on error.
func waitOutcomeFixtureUnmarshal(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("waitOutcomeFixtureUnmarshal: %v", err)
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: Outcome present before WaitForOutcome → returns immediately
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitForOutcome_AlreadyPresent verifies that when an outcome_emitted message
// has already arrived before WaitForOutcome is called, the method returns the
// payload immediately without blocking.
func TestWaitForOutcome_AlreadyPresent(t *testing.T) {
	const runID = "run-wait-present-01"
	const sessionID = "claude-sess-wait-present-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	// Deliver an outcome before waiting.
	payload := waitOutcomeFixtureMakePayload(t, "WORK_COMPLETE", "pre-existing outcome")
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", payload)
	status, reason := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", status, reason)
	}

	// WaitForOutcome should return immediately.
	done := make(chan struct{})
	var gotRaw json.RawMessage
	var gotErr error
	go func() {
		gotRaw, gotErr = daemon.ExportedHookWaitForOutcome(t.Context(), store, runID, sessionID)
		close(done)
	}()

	select {
	case <-done:
		// returned promptly — expected
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not return promptly when outcome was already present")
	}

	if gotErr != nil {
		t.Fatalf("WaitForOutcome: unexpected error: %v", gotErr)
	}
	if gotRaw == nil {
		t.Fatal("WaitForOutcome: got nil payload, want non-nil")
	}
	m := waitOutcomeFixtureUnmarshal(t, gotRaw)
	if m["summary"] != "pre-existing outcome" {
		t.Errorf("summary=%q, want %q", m["summary"], "pre-existing outcome")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: WaitForOutcome blocks, then unblocks on later outcome arrival
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitForOutcome_BlocksThenUnblocks verifies that WaitForOutcome blocks
// when no outcome is present, then returns the payload once outcome_emitted
// arrives via the store.
func TestWaitForOutcome_BlocksThenUnblocks(t *testing.T) {
	const runID = "run-wait-block-01"
	const sessionID = "claude-sess-wait-block-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	type result struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		raw, err := daemon.ExportedHookWaitForOutcome(t.Context(), store, runID, sessionID)
		resultCh <- result{raw: raw, err: err}
	}()

	// Give the goroutine time to enter the blocking select.
	time.Sleep(20 * time.Millisecond)

	// Deliver the outcome now.
	payload := waitOutcomeFixtureMakePayload(t, "WORK_COMPLETE", "delayed outcome")
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", payload)
	status, reason := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", status, reason)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatalf("WaitForOutcome: unexpected error: %v", res.err)
		}
		if res.raw == nil {
			t.Fatal("WaitForOutcome: got nil payload, want non-nil")
		}
		m := waitOutcomeFixtureUnmarshal(t, res.raw)
		if m["summary"] != "delayed outcome" {
			t.Errorf("summary=%q, want %q", m["summary"], "delayed outcome")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not unblock after outcome arrived")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: ctx cancel before outcome arrives → returns ctx.Err()
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitForOutcome_CtxCancel verifies that when ctx is cancelled before any
// outcome arrives, WaitForOutcome returns (nil, ctx.Err()).
func TestWaitForOutcome_CtxCancel(t *testing.T) {
	const runID = "run-wait-cancel-01"
	const sessionID = "claude-sess-wait-cancel-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	ctx, cancel := context.WithCancel(context.Background())

	type result struct {
		raw json.RawMessage
		err error
	}
	resultCh := make(chan result, 1)

	go func() {
		raw, err := daemon.ExportedHookWaitForOutcome(ctx, store, runID, sessionID)
		resultCh <- result{raw: raw, err: err}
	}()

	// Give the goroutine time to enter the blocking select.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case res := <-resultCh:
		if res.raw != nil {
			t.Errorf("WaitForOutcome: got non-nil payload on cancel, want nil")
		}
		if res.err == nil {
			t.Fatal("WaitForOutcome: expected ctx.Err(), got nil")
		}
		if res.err != context.Canceled {
			t.Errorf("WaitForOutcome: err=%v, want context.Canceled", res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForOutcome did not return after ctx cancel")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4: Multiple concurrent waiters all unblock
// ─────────────────────────────────────────────────────────────────────────────

// TestWaitForOutcome_MultipleWaiters verifies that multiple concurrent
// WaitForOutcome callers for the same session key all receive the outcome
// payload when it arrives.
func TestWaitForOutcome_MultipleWaiters(t *testing.T) {
	const runID = "run-wait-multi-01"
	const sessionID = "claude-sess-wait-multi-01"
	const numWaiters = 5

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	type result struct {
		raw json.RawMessage
		err error
	}
	results := make([]chan result, numWaiters)
	for i := range results {
		results[i] = make(chan result, 1)
	}

	var wg sync.WaitGroup
	for i := 0; i < numWaiters; i++ {
		wg.Add(1)
		ch := results[i]
		go func() {
			defer wg.Done()
			raw, err := daemon.ExportedHookWaitForOutcome(t.Context(), store, runID, sessionID)
			ch <- result{raw: raw, err: err}
		}()
	}

	// Give all goroutines time to enter the blocking select.
	time.Sleep(30 * time.Millisecond)

	// Deliver the outcome.
	payload := waitOutcomeFixtureMakePayload(t, "WORK_COMPLETE", "broadcast outcome")
	env := hookRelayFixtureMakeEnvelope(runID, sessionID, "outcome_emitted", payload)
	status, reason := daemon.ExportedHookDispatch(store, env)
	if status != "ok" {
		t.Fatalf("dispatch: status=%q reason=%q, want ok", status, reason)
	}

	// Collect all results.
	timeout := time.After(2 * time.Second)
	for i, ch := range results {
		select {
		case res := <-ch:
			if res.err != nil {
				t.Errorf("waiter %d: unexpected error: %v", i, res.err)
				continue
			}
			if res.raw == nil {
				t.Errorf("waiter %d: got nil payload, want non-nil", i)
				continue
			}
			m := waitOutcomeFixtureUnmarshal(t, res.raw)
			if m["summary"] != "broadcast outcome" {
				t.Errorf("waiter %d: summary=%q, want %q", i, m["summary"], "broadcast outcome")
			}
		case <-timeout:
			t.Errorf("waiter %d: did not unblock within timeout", i)
		}
	}
}
