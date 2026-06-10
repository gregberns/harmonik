package main

// comms_conflict_hkz0f02_test.go — unit tests for two-captains conflict detection
// (hk-z0f02: per-session identity binding / warn when two live sessions claim one name).
//
// Test coverage:
//   (a) conflict detected: name online with different session_id → warning returned
//   (b) same session_id: no conflict → empty warning
//   (c) empty sessionID: conflict detection skipped → empty warning (graceful degradation)
//   (d) name offline (leave beat): no conflict → empty warning
//   (e) name not in registry: no conflict → empty warning
//   (f) registry entry has no session_id: no conflict → empty warning
//   (g) session_id tracked in PresenceRecord from agent_presence event
//   (h) session_id carried forward from earlier beat when later beat omits it
//
// Bead ref: hk-z0f02.

import (
	"testing"
	"time"
)

// presenceJoinEventWithSession emits an agent_presence join/online event that
// includes a session_id field.
func presenceJoinEventWithSession(eventID, ts, agent, sessionID string) string {
	payload := map[string]any{
		"agent":      agent,
		"status":     "online",
		"last_seen":  ts,
		"reason":     "join",
		"session_id": sessionID,
	}
	return presenceTestEvent(eventID, ts, "agent_presence", payload)
}

// presenceRefreshEventNoSession emits a refresh beat without session_id (simulates
// recv-path refresh beats that don't carry a session token).
func presenceRefreshEventNoSession(eventID, ts, agent string) string {
	return presenceTestEvent(eventID, ts, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    "online",
		"last_seen": ts,
		"reason":    "refresh",
	})
}

// ---------------------------------------------------------------------------
// (a) conflict detected
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_ConflictDetected(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", ts, "captain", "session-A"),
	}
	eventsPath := buildEventsFile(t, lines)

	warn := checkCommsNameConflict(eventsPath, "captain", "session-B")
	if warn == "" {
		t.Error("expected a conflict warning when different session_id claims the same name; got empty")
	}
}

// ---------------------------------------------------------------------------
// (b) same session_id — no conflict
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_SameSession(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", ts, "captain", "session-A"),
	}
	eventsPath := buildEventsFile(t, lines)

	warn := checkCommsNameConflict(eventsPath, "captain", "session-A")
	if warn != "" {
		t.Errorf("expected no warning for same session_id; got %q", warn)
	}
}

// ---------------------------------------------------------------------------
// (c) empty sessionID — conflict detection skipped
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_EmptySessionID(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", ts, "captain", "session-A"),
	}
	eventsPath := buildEventsFile(t, lines)

	warn := checkCommsNameConflict(eventsPath, "captain", "")
	if warn != "" {
		t.Errorf("expected no warning when sessionID is empty (graceful degradation); got %q", warn)
	}
}

// ---------------------------------------------------------------------------
// (d) name offline after leave beat — no conflict
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_OfflineAfterLeave(t *testing.T) {
	joinTS := time.Now().Add(-20 * time.Second).UTC().Format(time.RFC3339)
	leaveTS := time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", joinTS, "captain", "session-A"),
		presenceLeaveEvent("01965b00-0000-7000-8000-000000000002", leaveTS, "captain"),
	}
	eventsPath := buildEventsFile(t, lines)

	warn := checkCommsNameConflict(eventsPath, "captain", "session-B")
	if warn != "" {
		t.Errorf("expected no conflict warning after leave beat; got %q", warn)
	}
}

// ---------------------------------------------------------------------------
// (e) name not in registry — no conflict
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_NameAbsent(t *testing.T) {
	eventsPath := buildEventsFile(t, nil)
	warn := checkCommsNameConflict(eventsPath, "nobody", "session-X")
	if warn != "" {
		t.Errorf("expected no warning for unknown name; got %q", warn)
	}
}

// ---------------------------------------------------------------------------
// (f) registry entry has no session_id — no conflict (unknown session)
// ---------------------------------------------------------------------------

func TestCheckCommsNameConflict_RegistryNoSessionID(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	// Use plain presenceJoinEvent (no session_id in payload).
	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", ts, "captain"),
	}
	eventsPath := buildEventsFile(t, lines)

	warn := checkCommsNameConflict(eventsPath, "captain", "session-B")
	if warn != "" {
		t.Errorf("expected no warning when registry entry has no session_id; got %q", warn)
	}
}

// ---------------------------------------------------------------------------
// (g) session_id tracked in PresenceRecord from agent_presence event
// ---------------------------------------------------------------------------

func TestPresenceRegistry_SessionIDTracked(t *testing.T) {
	ts := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", ts, "captain", "my-session-123"),
	}
	eventsPath := buildEventsFile(t, lines)

	registry := ComputePresenceRegistry(eventsPath)
	rec, ok := registry["captain"]
	if !ok {
		t.Fatal("captain not in presence registry")
	}
	if rec.SessionID != "my-session-123" {
		t.Errorf("expected SessionID=%q; got %q", "my-session-123", rec.SessionID)
	}
}

// ---------------------------------------------------------------------------
// (h) session_id carried forward when later beat omits it (recv-refresh beats)
// ---------------------------------------------------------------------------

func TestPresenceRegistry_SessionIDCarriedForward(t *testing.T) {
	joinTS := time.Now().Add(-20 * time.Second).UTC().Format(time.RFC3339)
	refreshTS := time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339)
	lines := []string{
		presenceJoinEventWithSession("01965b00-0000-7000-8000-000000000001", joinTS, "captain", "my-session-abc"),
		// Refresh beat without session_id (simulates recv-path auto-refresh).
		presenceRefreshEventNoSession("01965b00-0000-7000-8000-000000000002", refreshTS, "captain"),
	}
	eventsPath := buildEventsFile(t, lines)

	registry := ComputePresenceRegistry(eventsPath)
	rec, ok := registry["captain"]
	if !ok {
		t.Fatal("captain not in presence registry")
	}
	if rec.SessionID != "my-session-abc" {
		t.Errorf("session_id must carry forward from earlier beat; got %q", rec.SessionID)
	}
}
