//go:build scenario

package daemon

// scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go — //go:build scenario test
// for the normative N3 guarantee of the comms bus: at-least-once REDELIVERY plus
// CONSUMER DEDUPE-on-event_id (bead hk-pg0w5, codename:validation-net).
//
// # Coverage gap addressed
//
// The N3 guarantee (agent-comms spec §N3, FINALIZED 2026-06-01) had cursor-advance
// and presence coverage (commsrecvhandler_nnwaa_test.go, commscursor_test.go) but
// NO scenario asserting the two halves of N3 together:
//
//	(a) AT-LEAST-ONCE — the same event_id can be DELIVERED MORE THAN ONCE when the
//	    cursor-advance does not persist before the next recv (the crash/restart
//	    window the handler documents at commsrecvhandler_nnwaa.go:201-207: the
//	    cursor advances AFTER the scan returns, so a crash between scan and advance
//	    re-delivers the same batch).
//	(b) CONSUMER DEDUPE — a recipient that deduplicates on event_id MUST treat the
//	    re-delivered event_id as a no-op: exactly-once SIDE EFFECTS on top of the
//	    at-least-once transport (agent-comms SKILL.md §"Delivery guarantee" N3).
//
// # Normative contract (agent-comms spec §N3 / SKILL.md)
//
//	"A recipient that has already processed an event_id MUST treat a re-delivery
//	 of that same event_id as a no-op. Never assume exactly-once. Dedupe by
//	 event_id."
//
//	"the cursor advances AFTER the batch is returned, so a crash between delivery
//	 and cursor-advance replays the same batch."
//
// # Harness (lightest that exercises the bus — no daemon, no socket, no live bus)
//
// The comms-recv path is fully exercisable in-process against a temp events.jsonl
// + temp cursor directory, exactly as the sibling unit tests do
// (commsrecvhandler_nnwaa_test.go: writeTestEvent + newTestCommsHandler). No full
// daemon.Start composition root is needed for a bus-level N3 assertion; the
// at-least-once / dedupe contract lives entirely in the cursor-store + recv-handler
// pair (commscursor.go, commsrecvhandler_nnwaa.go) named by the bead.
//
// SAFETY: every fixture uses t.TempDir() — a unique events.jsonl and cursor dir
// per test. Nothing touches the live daemon, its socket, the live comms bus, or
// any existing tmux session. t.TempDir is auto-cleaned by the testing package.
//
// # Inducing redelivery deterministically
//
// We cannot crash an in-process daemon mid-call, so we reproduce the EXACT
// observable state a crash leaves behind: the recv handler returned a batch but
// its cursor-advance did NOT persist. We do this through the PUBLIC CursorStore
// API — capture the cursor BEFORE recv, run recv (which advances the cursor),
// then restore the cursor to its pre-scan position (rollbackCursor). The next
// recv therefore re-scans from the same position and RE-DELIVERS the identical
// event_ids. This is faithful: the handler's only N3-relevant durable side effect
// is the cursor write, and a crash between scan and advance leaves precisely the
// pre-scan cursor value on disk.
//
// Helpers in this file use the prefix "n3Scen" (N3 scenario).
// Per implementer-protocol.md §Helper-prefix discipline. The shared
// writeTestEvent / newTestCommsHandler helpers (untagged, always compiled) are
// reused from commsrecvhandler_nnwaa_test.go.
//
// Bead: hk-pg0w5. Spec: agent-comms spec §N3 (FINALIZED). Files under test:
// commscursor.go, commsrecvhandler_nnwaa.go.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// n3ScenRecv calls HandleCommsRecv for agent and returns the decoded result.
// Fails the test on transport error.
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

// n3ScenRollbackCursor restores agent's cursor to prevCursor, reproducing the
// on-disk state a crash leaves when the recv-handler's post-scan Advance never
// persisted (commsrecvhandler_nnwaa.go:201-207). When prevCursor is "" (no cursor
// existed before the scan) it removes the cursor file entirely, since "" is the
// "start of log" sentinel that CursorStore.Advance refuses to write.
//
// cursorDir is the directory passed to NewCursorStore; the cursor file for an
// agent is <cursorDir>/<agent> (commscursor.go:path).
func n3ScenRollbackCursor(t *testing.T, cs *CursorStore, cursorDir, agent, prevCursor string) {
	t.Helper()
	if prevCursor == "" {
		// No prior cursor — a crash before the FIRST advance leaves no cursor file.
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

	// Two messages directed to the agent (one directed, one broadcast).
	id1 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: agent, Body: "directive one",
	})
	id2 := writeTestEvent(t, eventsPath, "agent_message", AgentMessagePayload{
		From: "captain", To: "*", Body: "broadcast two",
	})

	// Consumer side: a dedupe-on-event_id processor that records exactly-once
	// side effects. This is the N3-compliant consumer the spec mandates.
	seen := map[string]bool{}          // dedup ledger keyed on event_id
	var sideEffects []string           // ordered record of processed bodies (one per distinct event_id)
	deliveriesByID := map[string]int{} // how many times each event_id was DELIVERED (transport)
	process := func(msgs []CommsRecvMessage) {
		for _, m := range msgs {
			deliveriesByID[m.EventID]++
			if seen[m.EventID] {
				// Re-delivery — N3 no-op. Do NOT record a side effect.
				continue
			}
			seen[m.EventID] = true
			sideEffects = append(sideEffects, m.Body)
		}
	}

	// --- First recv: cursor starts empty ("" = start of log). ---
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

	// Simulate a crash between the scan and the durable cursor-advance: the
	// handler DID advance the cursor (to id2), but that write never persisted.
	n3ScenRollbackCursor(t, cs, cursorDir, agent, prevCursor)

	// Confirm the rollback actually reverted the durable cursor (no advance survived).
	afterRollback, err := cs.Get(agent)
	if err != nil {
		t.Fatalf("Get cursor after rollback: %v", err)
	}
	if afterRollback != prevCursor {
		t.Fatalf("rollback: cursor should be back to %q, got %q", prevCursor, afterRollback)
	}

	// --- Second recv: AT-LEAST-ONCE. The same batch must be RE-DELIVERED. ---
	got2 := n3ScenRecv(t, h, agent)
	if len(got2.Messages) != 2 {
		t.Fatalf("redelivery: want the same 2 messages re-delivered, got %d", len(got2.Messages))
	}
	if got2.Messages[0].EventID != id1 || got2.Messages[1].EventID != id2 {
		t.Fatalf("redelivery: event_ids want [%s %s], got [%s %s]",
			id1, id2, got2.Messages[0].EventID, got2.Messages[1].EventID)
	}
	process(got2.Messages)

	// --- Assert (a) AT-LEAST-ONCE transport: each event_id delivered >= 2 times. ---
	if deliveriesByID[id1] < 2 {
		t.Errorf("at-least-once: id1 (%s) delivered %d times, want >= 2 (redelivery)", id1, deliveriesByID[id1])
	}
	if deliveriesByID[id2] < 2 {
		t.Errorf("at-least-once: id2 (%s) delivered %d times, want >= 2 (redelivery)", id2, deliveriesByID[id2])
	}

	// --- Assert (b) CONSUMER DEDUPE: exactly-once side effects on top of it. ---
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

	// --- After a CLEAN recv (no rollback), the cursor advances and the batch
	//     is NOT re-delivered: at-least-once does not mean infinite redelivery. ---
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

	// Persistent consumer dedup ledger — survives the daemon restart, as a real
	// agent's ledger would (SKILL.md: "or a persistent store keyed on event_id").
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

	// --- Daemon instance #1: capture pre-scan cursor, recv, then crash before
	//     the advance persists (rollback the cursor). ---
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
	// Crash before cursor-advance persisted.
	n3ScenRollbackCursor(t, cs1, cursorDir, agent, prev)

	// --- Daemon instance #2 (restart): fresh handler, same cursor dir + log.
	//     It reads the un-advanced cursor and RE-DELIVERS the same batch. ---
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

	// At-least-once: both event_ids delivered at least twice across the restart.
	if deliveriesByID[idA] < 2 || deliveriesByID[idB] < 2 {
		t.Errorf("at-least-once across restart: deliveries idA=%d idB=%d, want both >= 2",
			deliveriesByID[idA], deliveriesByID[idB])
	}
	// Consumer dedupe: exactly-once side effects despite the redelivery.
	if len(sideEffects) != 2 {
		t.Fatalf("dedupe across restart: want exactly 2 side effects, got %d: %v", len(sideEffects), sideEffects)
	}
	if sideEffects[0] != "msg A" || sideEffects[1] != "msg B" {
		t.Errorf("dedupe across restart: bodies want [msg A, msg B], got %v", sideEffects)
	}

	// A subsequent CLEAN recv (cursor now persisted at idB by daemon2's recv)
	// delivers nothing new — confirming durable cursor advance across the restart.
	got3 := n3ScenRecv(t, h2, agent)
	if len(got3.Messages) != 0 {
		t.Fatalf("post-restart clean recv: want 0 new messages, got %d", len(got3.Messages))
	}
}
