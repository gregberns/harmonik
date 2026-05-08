package brcli_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// reconciliationFixtureAuditLogJSON returns canonical JSON for a br --json
// audit log response for the given bead ID. The fixture includes:
//   - a "closed" event with a comment
//   - a "status_changed" event with old_value and new_value
//   - a "dependency_added" event with a comment
//
// This covers optional field presence on relevant events and absence on others.
func reconciliationFixtureAuditLogJSON(id string) string {
	return `{"issue_id":"` + id + `","events":[` +
		`{"id":10677,"event_type":"closed","actor":"gb","timestamp":"2026-05-08T05:38:01.761094Z","comment":"closing after review"},` +
		`{"id":10676,"event_type":"status_changed","actor":"gb","timestamp":"2026-05-08T05:38:01.761072Z","old_value":"in_progress","new_value":"closed"},` +
		`{"id":10656,"event_type":"dependency_added","actor":"gb","timestamp":"2026-05-07T10:00:00.000000Z","comment":"Added dependency on hk-872.27 (related)"}` +
		`]}`
}

// reconciliationFixtureAuditLogWithUnknownEventJSON returns a fixture that
// includes an event with an event_type value that harmonik has never seen.
// The adapter MUST pass it through without rejection (BI-007).
func reconciliationFixtureAuditLogWithUnknownEventJSON(id string) string {
	return `{"issue_id":"` + id + `","events":[` +
		`{"id":10677,"event_type":"closed","actor":"gb","timestamp":"2026-05-08T05:38:01.761094Z"},` +
		`{"id":10999,"event_type":"future_unknown_event","actor":"harmonik-daemon","timestamp":"2026-05-08T06:00:00.000000Z","comment":"some future semantics"}` +
		`]}`
}

// reconciliationFixtureAuditLogEmptyJSON returns a br audit log response with
// an empty events array.
func reconciliationFixtureAuditLogEmptyJSON(id string) string {
	return `{"issue_id":"` + id + `","events":[]}`
}

// reconciliationFixtureAuditLogNotFoundJSON returns the br error envelope for
// ISSUE_NOT_FOUND as produced by `br --json audit log <id>`.
func reconciliationFixtureAuditLogNotFoundJSON(searchedID string) string {
	return `{"error":{"code":"ISSUE_NOT_FOUND","message":"Issue not found: ` + searchedID + `","hint":"Check the bead ID and try again.","retryable":false,"context":{"searched_id":"` + searchedID + `"}}}`
}

// reconciliationFixtureAuditLogOtherErrorJSON returns a br error envelope for
// a non-NOT_FOUND error from `br --json audit log`.
func reconciliationFixtureAuditLogOtherErrorJSON() string {
	return `{"error":{"code":"INTERNAL_ERROR","message":"something went wrong internally","hint":"","retryable":true,"context":{}}}`
}

func TestAuditLogSuccess(t *testing.T) {
	id := core.BeadID("hk-872.15")
	jsonStr := reconciliationFixtureAuditLogJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), id)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error: %v", err)
	}

	if len(events) != 3 {
		t.Fatalf("len(events) = %d; want 3", len(events))
	}

	// Verify first event: "closed" with comment, no old_value/new_value.
	e0 := events[0]
	if e0.ID != 10677 {
		t.Errorf("events[0].ID = %d; want 10677", e0.ID)
	}
	if e0.EventType != "closed" {
		t.Errorf("events[0].EventType = %q; want %q", e0.EventType, "closed")
	}
	if e0.Actor != "gb" {
		t.Errorf("events[0].Actor = %q; want %q", e0.Actor, "gb")
	}
	if e0.Comment != "closing after review" {
		t.Errorf("events[0].Comment = %q; want %q", e0.Comment, "closing after review")
	}
	if e0.OldValue != "" {
		t.Errorf("events[0].OldValue = %q; want empty", e0.OldValue)
	}
	if e0.NewValue != "" {
		t.Errorf("events[0].NewValue = %q; want empty", e0.NewValue)
	}

	// Verify second event: "status_changed" with old_value / new_value.
	e1 := events[1]
	if e1.ID != 10676 {
		t.Errorf("events[1].ID = %d; want 10676", e1.ID)
	}
	if e1.EventType != "status_changed" {
		t.Errorf("events[1].EventType = %q; want %q", e1.EventType, "status_changed")
	}
	if e1.OldValue != "in_progress" {
		t.Errorf("events[1].OldValue = %q; want %q", e1.OldValue, "in_progress")
	}
	if e1.NewValue != "closed" {
		t.Errorf("events[1].NewValue = %q; want %q", e1.NewValue, "closed")
	}
	if e1.Comment != "" {
		t.Errorf("events[1].Comment = %q; want empty", e1.Comment)
	}

	// Verify third event: "dependency_added" with comment.
	e2 := events[2]
	if e2.ID != 10656 {
		t.Errorf("events[2].ID = %d; want 10656", e2.ID)
	}
	if e2.EventType != "dependency_added" {
		t.Errorf("events[2].EventType = %q; want %q", e2.EventType, "dependency_added")
	}
	if e2.Comment != "Added dependency on hk-872.27 (related)" {
		t.Errorf("events[2].Comment = %q; want %q", e2.Comment, "Added dependency on hk-872.27 (related)")
	}
}

func TestAuditLogUnknownEventTypePassthrough(t *testing.T) {
	// BI-007 tolerance: unknown event_type values MUST be returned as-is.
	id := core.BeadID("hk-872.15")
	jsonStr := reconciliationFixtureAuditLogWithUnknownEventJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), id)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("len(events) = %d; want 2", len(events))
	}

	// The second event has the unknown type — it MUST appear unchanged.
	unknownEvent := events[1]
	if unknownEvent.EventType != "future_unknown_event" {
		t.Errorf("unknown event_type = %q; want %q", unknownEvent.EventType, "future_unknown_event")
	}
	if unknownEvent.Actor != "harmonik-daemon" {
		t.Errorf("unknown event Actor = %q; want %q", unknownEvent.Actor, "harmonik-daemon")
	}
}

func TestAuditLogEmptyEvents(t *testing.T) {
	id := core.BeadID("hk-872.15")
	jsonStr := reconciliationFixtureAuditLogEmptyJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), id)
	if err != nil {
		t.Fatalf("AuditLog: unexpected error for empty events: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d; want 0", len(events))
	}
}

func TestAuditLogNotFound(t *testing.T) {
	searchedID := "nonexistent-bead"
	jsonStr := reconciliationFixtureAuditLogNotFoundJSON(searchedID)
	path := brcliFixtureMockBinary(t, jsonStr, "", 3)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.AuditLog(context.Background(), core.BeadID(searchedID))
	if err == nil {
		t.Fatal("expected ErrBeadNotFound error, got nil")
	}
	if !errors.Is(err, brcli.ErrBeadNotFound) {
		t.Errorf("errors.Is(err, ErrBeadNotFound) = false; got %v", err)
	}
}

func TestAuditLogOtherNonZeroExit(t *testing.T) {
	jsonStr := reconciliationFixtureAuditLogOtherErrorJSON()
	path := brcliFixtureMockBinary(t, jsonStr, "", 1)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.AuditLog(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected ErrBrAuditLogFailed error, got nil")
	}
	if !errors.Is(err, brcli.ErrBrAuditLogFailed) {
		t.Errorf("errors.Is(err, ErrBrAuditLogFailed) = false; got %v", err)
	}
}

func TestAuditLogMalformedJSON(t *testing.T) {
	path := brcliFixtureMockBinary(t, `not-json-at-all`, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.AuditLog(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestAuditLogTimestampParsed(t *testing.T) {
	// Verify that the RFC3339 timestamp round-trips through time.Time correctly.
	id := core.BeadID("hk-872.15")
	jsonStr := reconciliationFixtureAuditLogJSON(string(id))
	path := brcliFixtureMockBinary(t, jsonStr, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events, err := adapter.AuditLog(context.Background(), id)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}

	wantTime, parseErr := time.Parse(time.RFC3339Nano, "2026-05-08T05:38:01.761094Z")
	if parseErr != nil {
		t.Fatalf("fixture timestamp parse: %v", parseErr)
	}

	if !events[0].Timestamp.Equal(wantTime) {
		t.Errorf("events[0].Timestamp = %v; want %v", events[0].Timestamp, wantTime)
	}
}

func TestAuditLogExecFailure(t *testing.T) {
	// Use a non-existent binary to trigger exec failure.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.AuditLog(context.Background(), core.BeadID("hk-872.15"))
	if err == nil {
		t.Fatal("expected error for exec failure, got nil")
	}
}
