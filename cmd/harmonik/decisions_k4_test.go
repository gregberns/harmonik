package main

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func dk4Presence(t *testing.T, eventID, agent, status, lastSeen, reason string) string {
	return dx9Event(t, eventID, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    status,
		"last_seen": lastSeen,
		"reason":    reason,
	})
}

// TestFlagOrphanedPending_OfflineFlaggedOthersNot: of three open decisions, only
// the one whose blocked_agent emitted a `leave` (offline) beat — or is past the
// 10-min cutoff — is flagged orphaned-pending. An Online agent and an agent with
// no presence record are NOT flagged.
func TestFlagOrphanedPending_OfflineFlaggedOthersNot(t *testing.T) {
	now := time.Now().UTC()
	fresh := now.Format(time.RFC3339)
	stale := now.Add(-15 * time.Minute).Format(time.RFC3339)

	lines := []string{
		dx9Needed(t, dx9D1, " Q-alice", []string{"a", "b"}, "alice", "hk-a"),
		dx9Needed(t, dx9D2, "Q-bob", []string{"c", "d"}, "bob", "hk-b"),
		dx9Needed(t, dx9D3, "Q-carol", []string{"e", "f"}, "carol", "hk-c"),
		dx9Needed(t, dx9D4, "Q-dave", []string{"g", "h"}, "dave", "hk-d"),
		dk4Presence(t, "01965b00-0000-7000-8000-0000000e0e01", "alice", "offline", fresh, "leave"),
		dk4Presence(t, "01965b00-0000-7000-8000-0000000e0e02", "bob", "online", fresh, "join"),
		dk4Presence(t, "01965b00-0000-7000-8000-0000000e0e03", "carol", "online", stale, "join"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	items := []decisionListItem{
		{DecisionID: dx9D1, Question: "Q-alice", Options: []string{"a", "b"}, BlockedAgent: "alice"},
		{DecisionID: dx9D2, Question: "Q-bob", Options: []string{"c", "d"}, BlockedAgent: "bob"},
		{DecisionID: dx9D3, Question: "Q-carol", Options: []string{"e", "f"}, BlockedAgent: "carol"},
		{DecisionID: dx9D4, Question: "Q-dave", Options: []string{"g", "h"}, BlockedAgent: "dave"},
	}

	rows := flagOrphanedPending(items, eventsPath)
	got := make(map[string]bool, len(rows))
	for _, r := range rows {
		got[r.DecisionID] = r.OrphanedPending
	}

	if !got[dx9D1] {
		t.Errorf("alice (explicit leave/offline) should be orphaned-pending")
	}
	if got[dx9D2] {
		t.Errorf("bob (fresh online) should NOT be orphaned-pending")
	}
	if !got[dx9D3] {
		t.Errorf("carol (online 15m ago, past 10m cutoff) should be orphaned-pending")
	}
	if got[dx9D4] {
		t.Errorf("dave (no presence record) should NOT be orphaned-pending (no evidence gone)")
	}
}

// TestFlagOrphanedPending_NoEmit: computing the orphaned-pending flag reads the
// durable log and writes NOTHING — the events.jsonl is byte-for-byte unchanged
// (N9 read-pure / S6). A `list` call MUST NOT emit any event.
func TestFlagOrphanedPending_NoEmit(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	lines := []string{
		dx9Needed(t, dx9D1, "Q1", []string{"a", "b"}, "alice", ""),
		dk4Presence(t, "01965b00-0000-7000-8000-0000000e0e01", "alice", "offline", now, "leave"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	//nolint:gosec // G304: eventsPath is created by this test's temporary event-log fixture
	before, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events before: %v", err)
	}

	items := []decisionListItem{{DecisionID: dx9D1, BlockedAgent: "alice", Options: []string{"a", "b"}}}
	rows := flagOrphanedPending(items, eventsPath)
	if len(rows) != 1 || !rows[0].OrphanedPending {
		t.Fatalf("expected alice flagged orphaned-pending, got %+v", rows)
	}

	//nolint:gosec // G304: eventsPath is created by this test's temporary event-log fixture
	after, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events after: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("flagOrphanedPending mutated events.jsonl — the flag must be read-pure (N9/S6), no emit")
	}
}

// TestFlagOrphanedPending_EmptyBlockedAgentNotFlagged: a decision with an empty
// blocked_agent is never flagged (nothing to liveness-check).
func TestFlagOrphanedPending_EmptyBlockedAgentNotFlagged(t *testing.T) {
	eventsPath := dx9BuildEventsFile(t, []string{
		dx9Needed(t, dx9D1, "Q1", []string{"a", "b"}, "", ""),
	})
	rows := flagOrphanedPending([]decisionListItem{{DecisionID: dx9D1, Options: []string{"a", "b"}}}, eventsPath)
	if len(rows) != 1 || rows[0].OrphanedPending {
		t.Errorf("empty blocked_agent must not be flagged, got %+v", rows)
	}
}
