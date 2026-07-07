package daemon

// scenario_comms_l0_predicate_hkn0wb0_test.go — T1a L0: MatchAgentMessage
// predicate table via eventbus.ScanAfter (N1, hk-n0wb0).
//
// L0 pure-projection: write agent_message events to a JSONL fixture, scan
// with eventbus.ScanAfter, apply MatchAgentMessage — zero goroutines, zero
// net.Pipe, zero daemon. Seam under test: the ScanAfter+MatchAgentMessage
// combination that HandleCommsRecv uses for durable recv.
//
// Coverage:
//   - directed-to-me / directed-to-other
//   - broadcast (to=="*") matches any specific to-filter
//   - from-filter wildcard and exact
//   - topic-filter wildcard and exact
//   - combined and wildcard-all cases
//   - non-agent_message events are skipped by the type guard (ScanAfter
//     scans all event types; the caller applies the type check)
//
// Bead: hk-n0wb0. Part-of: comms-test-harness.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// writeEvent appends a single JSON-encoded core.Event line to path.
func writeEvent(t *testing.T, path string, evt core.Event) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()
	if err := json.NewEncoder(f).Encode(evt); err != nil {
		t.Fatalf("encode event: %v", err)
	}
}

// makeAgentMsgEvent constructs a minimal valid core.Event of type agent_message
// with the given payload. The event's EventID is a fresh UUIDv7.
func makeAgentMsgEvent(t *testing.T, p AgentMessagePayload) core.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	payloadBytes, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return core.Event{
		EventID:       core.EventID(id),
		SchemaVersion: 1,
		Type:          "agent_message",
		TimestampWall: time.Now(),
		Payload:       json.RawMessage(payloadBytes),
	}
}

// makeNonCommsEvent constructs a minimal non-agent_message event (run_completed).
func makeNonCommsEvent(t *testing.T) core.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return core.Event{
		EventID:       core.EventID(id),
		SchemaVersion: 1,
		Type:          "run_completed",
		TimestampWall: time.Now(),
		Payload:       json.RawMessage(`{}`),
	}
}

// scanAndMatch replicates the HandleCommsRecv hot path:
// ScanAfter → type guard → unmarshal → MatchAgentMessage.
// Returns the EventIDs of matched events.
func scanAndMatch(path string, to, from, topic string) []core.EventID {
	var matched []core.EventID
	for evt := range eventbus.ScanAfter(path, core.EventID(uuid.Nil)) {
		if evt.Type != "agent_message" {
			continue
		}
		var p AgentMessagePayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			continue
		}
		if MatchAgentMessage(p, to, from, topic) {
			matched = append(matched, evt.EventID)
		}
	}
	return matched
}

// containsID reports whether ids contains target.
func containsID(ids []core.EventID, target core.EventID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// TestMatchAgentMessage_L0_ScanAfter is the T1a L0 predicate-table test.
// It writes a fixed JSONL fixture (one event per addressing case) and then
// runs each filter combination through ScanAfter+MatchAgentMessage, asserting
// which events are matched and which are excluded.
func TestMatchAgentMessage_L0_ScanAfter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	// Fixture: one agent_message event per case + one non-comms event.
	// Each is written in UUIDv7 order (sleep 1ms between writes to ensure
	// distinct timestamps; UUIDv7 sub-ms ordering is monotonic but tiny
	// sleeps keep the fixture trivially readable).
	evtDirected := makeAgentMsgEvent(t, AgentMessagePayload{To: "alice", From: "bob", Body: "hi"})
	time.Sleep(time.Millisecond)
	evtOther := makeAgentMsgEvent(t, AgentMessagePayload{To: "carol", From: "bob", Body: "for carol"})
	time.Sleep(time.Millisecond)
	evtBroadcast := makeAgentMsgEvent(t, AgentMessagePayload{To: "*", From: "bob", Body: "broadcast"})
	time.Sleep(time.Millisecond)
	evtFromMatch := makeAgentMsgEvent(t, AgentMessagePayload{To: "alice", From: "dave", Body: "from dave"})
	time.Sleep(time.Millisecond)
	evtTopicMatch := makeAgentMsgEvent(t, AgentMessagePayload{To: "alice", From: "bob", Topic: "status", Body: "status msg"})
	time.Sleep(time.Millisecond)
	evtTopicOther := makeAgentMsgEvent(t, AgentMessagePayload{To: "alice", From: "bob", Topic: "other", Body: "other topic"})
	time.Sleep(time.Millisecond)
	evtCombined := makeAgentMsgEvent(t, AgentMessagePayload{To: "alice", From: "bob", Topic: "status", Body: "combined"})
	time.Sleep(time.Millisecond)
	evtNonComms := makeNonCommsEvent(t)

	for _, evt := range []core.Event{
		evtDirected, evtOther, evtBroadcast, evtFromMatch,
		evtTopicMatch, evtTopicOther, evtCombined, evtNonComms,
	} {
		writeEvent(t, path, evt)
	}

	cases := []struct {
		name  string
		to    string
		from  string
		topic string
		// wantMatch: events that MUST appear in matched results.
		wantMatch []core.EventID
		// wantExclude: events that MUST NOT appear.
		wantExclude []core.EventID
	}{
		{
			name:        "directed-to-alice-wildcard-from-topic",
			to:          "alice",
			wantMatch:   []core.EventID{evtDirected.EventID, evtBroadcast.EventID, evtFromMatch.EventID, evtTopicMatch.EventID, evtTopicOther.EventID, evtCombined.EventID},
			wantExclude: []core.EventID{evtOther.EventID},
		},
		{
			name:        "directed-to-carol",
			to:          "carol",
			wantMatch:   []core.EventID{evtOther.EventID, evtBroadcast.EventID},
			wantExclude: []core.EventID{evtDirected.EventID, evtFromMatch.EventID},
		},
		{
			name:        "broadcast-only-empty-to-filter",
			to:          "",
			from:        "",
			topic:       "",
			wantMatch:   []core.EventID{evtDirected.EventID, evtOther.EventID, evtBroadcast.EventID, evtFromMatch.EventID, evtTopicMatch.EventID, evtTopicOther.EventID, evtCombined.EventID},
			wantExclude: []core.EventID{}, // non-comms excluded by type guard
		},
		{
			name:        "from-filter-bob-only",
			to:          "alice",
			from:        "bob",
			wantMatch:   []core.EventID{evtDirected.EventID, evtBroadcast.EventID, evtTopicMatch.EventID, evtTopicOther.EventID, evtCombined.EventID},
			wantExclude: []core.EventID{evtFromMatch.EventID}, // from==dave
		},
		{
			name: "from-filter-dave",
			to:   "alice",
			from: "dave",
			// evtBroadcast: To=* passes to-filter but From=bob ≠ dave → excluded
			wantMatch:   []core.EventID{evtFromMatch.EventID},
			wantExclude: []core.EventID{evtDirected.EventID, evtBroadcast.EventID},
		},
		{
			name:  "topic-filter-status",
			to:    "alice",
			topic: "status",
			// evtBroadcast: To=* passes to-filter but Topic="" ≠ "status" → excluded
			wantMatch:   []core.EventID{evtTopicMatch.EventID, evtCombined.EventID},
			wantExclude: []core.EventID{evtDirected.EventID, evtTopicOther.EventID, evtBroadcast.EventID},
		},
		{
			name:  "combined-to-alice-from-bob-topic-status",
			to:    "alice",
			from:  "bob",
			topic: "status",
			// evtBroadcast: To=* passes, From=bob passes, but Topic="" ≠ "status" → excluded
			wantMatch:   []core.EventID{evtTopicMatch.EventID, evtCombined.EventID},
			wantExclude: []core.EventID{evtFromMatch.EventID, evtDirected.EventID, evtTopicOther.EventID, evtBroadcast.EventID},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matched := scanAndMatch(path, tc.to, tc.from, tc.topic)

			for _, want := range tc.wantMatch {
				if !containsID(matched, want) {
					t.Errorf("expected event %v in matched set but it was absent (filter to=%q from=%q topic=%q)",
						want, tc.to, tc.from, tc.topic)
				}
			}
			for _, noWant := range tc.wantExclude {
				if containsID(matched, noWant) {
					t.Errorf("event %v must not appear in matched set (filter to=%q from=%q topic=%q)",
						noWant, tc.to, tc.from, tc.topic)
				}
			}
		})
	}
}

// TestMatchAgentMessage_L0_NonCommsSkipped verifies the type guard: ScanAfter
// returns non-agent_message events but the recv hot-path skips them. The
// non-comms event must never appear in the matched set regardless of filters.
func TestMatchAgentMessage_L0_NonCommsSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	nonComms := makeNonCommsEvent(t)
	writeEvent(t, path, nonComms)

	matched := scanAndMatch(path, "", "", "") // wildcard — would match anything if type guard is absent
	for _, id := range matched {
		if id == nonComms.EventID {
			t.Errorf("non-agent_message event %v leaked into matched set; type guard broken", id)
		}
	}
	if len(matched) != 0 {
		t.Errorf("expected 0 matched events from a file containing only non-comms events, got %d", len(matched))
	}
}
