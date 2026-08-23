//go:build scenario

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func n3ScenRecv(t *testing.T, h *commsSendHandlerImpl, agent string) CommsRecvResult {
	t.Helper()
	payload, err := json.Marshal(CommsRecvRequest{Agent: agent})
	if err != nil {
		t.Fatalf("n3ScenRecv: marshal request: %v", err)
	}
	raw, err := h.HandleCommsRecv(context.Background(), payload)
	if err != nil {
		t.Fatalf("n3ScenRecv: HandleCommsRecv(%q): %v", agent, err)
	}
	var got CommsRecvResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("n3ScenRecv: unmarshal result: %v", err)
	}
	return got
}

func n3ScenRollbackCursor(t *testing.T, cs *CursorStore, cursorDir, agent, prevCursor string) {
	t.Helper()
	if prevCursor == "" {
		if err := os.Remove(filepath.Join(cursorDir, agent)); err != nil && !os.IsNotExist(err) {
			t.Fatalf("n3ScenRollbackCursor: remove cursor for %q: %v", agent, err)
		}
		return
	}
	if err := cs.Advance(agent, prevCursor); err != nil {
		t.Fatalf("n3ScenRollbackCursor: restore cursor for %q to %q: %v", agent, prevCursor, err)
	}
}

// TestScenarioCommsN3_AtLeastOnceRedeliveryAndConsumerDedupe asserts BOTH halves
// of the N3 guarantee in one flow:
//
//	(a) AT-LEAST-ONCE: when the cursor-advance does not persist (simulated crash
//	    between scan and advance), the next recv RE-DELIVERS the identical
//	    event_ids — the transport is at-least-once, not exactly-once.
//	(b) CONSUMER DEDUPE: a consumer that dedupes on event_id processes each
//	    distinct event_id EXACTLY ONCE despite the redelivery — the redelivered
//	    event is a no-op (exactly-once side effect).
func TestScenarioCommsN3_AtLeastOnceRedeliveryAndConsumerDedupe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")
	cs := NewCursorStore(cursorDir)
	h := newTestCommsHandler(cs, eventsPath)

	const agent = "liet"

	id1 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: agent, Body: "directive one",
	})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: "*", Body: "broadcast two",
	})

	seen := map[string]bool{}          // dedup ledger keyed on event_id
	var sideEffects []string           // ordered record of processed bodies (one per distinct event_id)
	deliveriesByID := map[string]int{} // how many times each event_id was DELIVERED (transport)
	process := func(msgs []CommsRecvMessage) {
		for _, m := range msgs {
			deliveriesByID[m.EventID]++
			if seen[m.EventID] {
				continue
			}
			seen[m.EventID] = true
			sideEffects = append(sideEffects, m.Body)
		}
	}

	prevCursor, err := cs.Get(agent)
	if err != nil {
		t.Fatalf("Get cursor before first recv: %v", err)
	}
	if prevCursor != "" {
		t.Fatalf("precondition: cursor should be empty before first recv, got %q", prevCursor)
	}
	got1 := n3ScenRecv(t, h, agent)
	if len(got1.Messages) != 2 {
		t.Fatalf("first recv: want 2 messages, got %d", len(got1.Messages))
	}
	if got1.Messages[0].EventID != id1 || got1.Messages[1].EventID != id2 {
		t.Fatalf("first recv: event_ids want [%s %s], got [%s %s]",
			id1, id2, got1.Messages[0].EventID, got1.Messages[1].EventID)
	}
	process(got1.Messages)

	n3ScenRollbackCursor(t, cs, cursorDir, agent, prevCursor)

	afterRollback, err := cs.Get(agent)
	if err != nil {
		t.Fatalf("Get cursor after rollback: %v", err)
	}
	if afterRollback != prevCursor {
		t.Fatalf("rollback: cursor should be back to %q, got %q", prevCursor, afterRollback)
	}

	got2 := n3ScenRecv(t, h, agent)
	if len(got2.Messages) != 2 {
		t.Fatalf("redelivery: want the same 2 messages re-delivered, got %d", len(got2.Messages))
	}
	if got2.Messages[0].EventID != id1 || got2.Messages[1].EventID != id2 {
		t.Fatalf("redelivery: event_ids want [%s %s], got [%s %s]",
			id1, id2, got2.Messages[0].EventID, got2.Messages[1].EventID)
	}
	process(got2.Messages)

	if deliveriesByID[id1] < 2 {
		t.Errorf("at-least-once: id1 (%s) delivered %d times, want >= 2 (redelivery)", id1, deliveriesByID[id1])
	}
	if deliveriesByID[id2] < 2 {
		t.Errorf("at-least-once: id2 (%s) delivered %d times, want >= 2 (redelivery)", id2, deliveriesByID[id2])
	}

	if len(sideEffects) != 2 {
		t.Fatalf("dedupe: want exactly 2 side effects (one per distinct event_id), got %d: %v",
			len(sideEffects), sideEffects)
	}
	if sideEffects[0] != "directive one" || sideEffects[1] != "broadcast two" {
		t.Errorf("dedupe: side-effect bodies want [%q %q], got %v",
			"directive one", "broadcast two", sideEffects)
	}
	if len(seen) != 2 {
		t.Errorf("dedupe: ledger should hold exactly 2 distinct event_ids, got %d", len(seen))
	}

	cursorNow, err := cs.Get(agent)
	if err != nil {
		t.Fatalf("Get cursor after clean recv: %v", err)
	}
	if cursorNow != id2 {
		t.Fatalf("clean recv: cursor should be at id2 (%s), got %q", id2, cursorNow)
	}
	got3 := n3ScenRecv(t, h, agent)
	if len(got3.Messages) != 0 {
		t.Fatalf("post-advance recv: want 0 messages (no redelivery once cursor persists), got %d", len(got3.Messages))
	}
}

// TestScenarioCommsN3_RedeliveryAcrossDaemonRestart asserts the at-least-once
// window across a simulated DAEMON RESTART (a fresh handler instance reading the
// same cursor directory + events.jsonl), which is the concrete N3 trigger the
// spec names ("a crash or restart before the daemon advances the cursor causes
// the same batch to be re-delivered"). A daemon that crashed before persisting
// the cursor re-delivers the same event_ids to the new instance; the consumer's
// PERSISTENT dedup ledger (keyed on event_id, surviving the restart) absorbs them.
func TestScenarioCommsN3_RedeliveryAcrossDaemonRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	cursorDir := filepath.Join(dir, "cursors")

	const agent = "liet"

	idA := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: agent, Body: "msg A",
	})
	idB := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: agent, Body: "msg B",
	})

	seen := map[string]bool{}
	var sideEffects []string
	deliveriesByID := map[string]int{}
	process := func(msgs []CommsRecvMessage) {
		for _, m := range msgs {
			deliveriesByID[m.EventID]++
			if seen[m.EventID] {
				continue // re-delivery — no-op (N3)
			}
			seen[m.EventID] = true
			sideEffects = append(sideEffects, m.Body)
		}
	}

	cs1 := NewCursorStore(cursorDir)
	h1 := newTestCommsHandler(cs1, eventsPath)
	prev, err := cs1.Get(agent)
	if err != nil {
		t.Fatalf("daemon1: Get cursor: %v", err)
	}
	got1 := n3ScenRecv(t, h1, agent)
	if len(got1.Messages) != 2 {
		t.Fatalf("daemon1 recv: want 2 messages, got %d", len(got1.Messages))
	}
	process(got1.Messages)
	n3ScenRollbackCursor(t, cs1, cursorDir, agent, prev)

	cs2 := NewCursorStore(cursorDir)
	h2 := newTestCommsHandler(cs2, eventsPath)
	got2 := n3ScenRecv(t, h2, agent)
	if len(got2.Messages) != 2 {
		t.Fatalf("daemon2 (restart) recv: want the same 2 messages re-delivered, got %d", len(got2.Messages))
	}
	if got2.Messages[0].EventID != idA || got2.Messages[1].EventID != idB {
		t.Fatalf("daemon2 redelivery: event_ids want [%s %s], got [%s %s]",
			idA, idB, got2.Messages[0].EventID, got2.Messages[1].EventID)
	}
	process(got2.Messages)

	if deliveriesByID[idA] < 2 || deliveriesByID[idB] < 2 {
		t.Errorf("at-least-once across restart: deliveries idA=%d idB=%d, want both >= 2",
			deliveriesByID[idA], deliveriesByID[idB])
	}
	if len(sideEffects) != 2 {
		t.Fatalf("dedupe across restart: want exactly 2 side effects, got %d: %v", len(sideEffects), sideEffects)
	}
	if sideEffects[0] != "msg A" || sideEffects[1] != "msg B" {
		t.Errorf("dedupe across restart: bodies want [msg A, msg B], got %v", sideEffects)
	}

	got3 := n3ScenRecv(t, h2, agent)
	if len(got3.Messages) != 0 {
		t.Fatalf("post-restart clean recv: want 0 new messages, got %d", len(got3.Messages))
	}
}
