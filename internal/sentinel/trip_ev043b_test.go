package sentinel_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// makeProjectDir creates a minimal .harmonik project directory for testing.
func makeProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{".harmonik/events", ".harmonik/decision_acks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return dir
}

// readAckFile reads and parses a sentinel ack-state file by ack_token.
func readAckFile(t *testing.T, projectDir, ackToken string) map[string]interface{} {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "decision_acks", ackToken)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ack file %s: %v", path, err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse ack file: %v", err)
	}
	return m
}

// zeroID is the nil EventID used to scan from the beginning of events.jsonl.
var zeroID = core.EventID{}

// scanEventTypes returns the ordered list of event types from events.jsonl.
func scanEventTypes(t *testing.T, projectDir string) []string {
	t.Helper()
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	var types []string
	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		types = append(types, ev.Type)
	}
	return types
}

// scanDecisionRequired returns the payloads of all decision_required events.
func scanDecisionRequired(t *testing.T, projectDir string) []map[string]interface{} {
	t.Helper()
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	var out []map[string]interface{}
	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		if ev.Type != "decision_required" {
			continue
		}
		var p map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("parse decision_required payload: %v", err)
		}
		out = append(out, p)
	}
	return out
}

// scanDecisionAcknowledged returns the payloads of all decision_acknowledged events.
func scanDecisionAcknowledged(t *testing.T, projectDir string) []map[string]interface{} {
	t.Helper()
	eventsPath := filepath.Join(projectDir, ".harmonik", "events", "events.jsonl")
	var out []map[string]interface{}
	for ev := range eventbus.ScanAfter(eventsPath, zeroID) {
		if ev.Type != "decision_acknowledged" {
			continue
		}
		var p map[string]interface{}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatalf("parse decision_acknowledged payload: %v", err)
		}
		out = append(out, p)
	}
	return out
}

// TestEmitTrip_WritesAckFileAndEvent verifies that EmitTrip writes the ack-state
// file and a decision_required event on first call.
func TestEmitTrip_WritesAckFileAndEvent(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:        dir,
		ReadyBeadIDs:      []string{"hk-aaa", "hk-bbb"},
		HasUndeployedTail: true,
		Now:               now,
	})
	if err != nil {
		t.Fatalf("EmitTrip: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty ack_token")
	}

	// Ack-state file must exist with status=pending.
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "pending" {
		t.Errorf("ack status: got %q, want %q", ack["status"], "pending")
	}
	if ack["subject_kind"] != "queue" {
		t.Errorf("subject_kind: got %q, want %q", ack["subject_kind"], "queue")
	}
	if ack["subject_id"] != "sentinel" {
		t.Errorf("subject_id: got %q, want %q", ack["subject_id"], "sentinel")
	}
	reason, _ := ack["reason"].(string)
	if !strings.Contains(reason, "hk-aaa") {
		t.Errorf("reason should name ready bead IDs; got %q", reason)
	}
	if !strings.Contains(reason, "undeployed tail") {
		t.Errorf("reason should mention undeployed tail; got %q", reason)
	}

	// One decision_required event must be in events.jsonl.
	events := scanDecisionRequired(t, dir)
	if len(events) != 1 {
		t.Fatalf("expected 1 decision_required event; got %d", len(events))
	}
	p := events[0]
	if p["ack_token"] != tok {
		t.Errorf("event ack_token: got %q, want %q", p["ack_token"], tok)
	}
	subj, _ := p["subject"].(map[string]interface{})
	if subj == nil || subj["id"] != "sentinel" {
		t.Errorf("event subject.id: got %v, want %q", subj, "sentinel")
	}
}

// TestEmitTrip_Idempotent verifies that a second EmitTrip call returns the
// existing ack_token and writes no additional ack files or events.
func TestEmitTrip_Idempotent(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	in := sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ccc"},
		Now:          now,
	}

	tok1, err := sentinel.EmitTrip(context.Background(), in)
	if err != nil || tok1 == "" {
		t.Fatalf("first EmitTrip: tok=%q err=%v", tok1, err)
	}

	tok2, err := sentinel.EmitTrip(context.Background(), in)
	if err != nil || tok2 == "" {
		t.Fatalf("second EmitTrip: tok=%q err=%v", tok2, err)
	}

	if tok1 != tok2 {
		t.Errorf("idempotency violated: tok1=%q tok2=%q", tok1, tok2)
	}

	// Exactly one decision_required event in the log.
	events := scanDecisionRequired(t, dir)
	if len(events) != 1 {
		t.Errorf("expected 1 decision_required event after 2 calls; got %d", len(events))
	}

	// Exactly one ack file in decision_acks/.
	acksDir := filepath.Join(dir, ".harmonik", "decision_acks")
	entries, _ := os.ReadDir(acksDir)
	if len(entries) != 1 {
		t.Errorf("expected 1 ack file; got %d", len(entries))
	}
}

// TestClearTrip_MarksAcknowledgedAndAppendsEvent verifies that ClearTrip
// updates the ack file to acknowledged and appends a decision_acknowledged event.
func TestClearTrip_MarksAcknowledgedAndAppendsEvent(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ddd"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	clearTime := now.Add(30 * time.Minute)
	if err := sentinel.ClearTrip(context.Background(), dir, tok, clearTime); err != nil {
		t.Fatalf("ClearTrip: %v", err)
	}

	// Ack file must be acknowledged.
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("ack status after clear: got %q, want %q", ack["status"], "acknowledged")
	}

	// A decision_acknowledged event must be in events.jsonl.
	acked := scanDecisionAcknowledged(t, dir)
	if len(acked) != 1 {
		t.Fatalf("expected 1 decision_acknowledged event; got %d", len(acked))
	}
	if acked[0]["ack_token"] != tok {
		t.Errorf("acknowledged ack_token: got %q, want %q", acked[0]["ack_token"], tok)
	}
	if acked[0]["ack_method"] != "governor_movement" {
		t.Errorf("ack_method: got %q, want %q", acked[0]["ack_method"], "governor_movement")
	}
}

// TestClearTrip_AbsentAckFile verifies that ClearTrip is a no-op when the
// ack file does not exist (prevents an error on daemon restart after manual cleanup).
func TestClearTrip_AbsentAckFile(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if err := sentinel.ClearTrip(context.Background(), dir, "nonexistent-token", now); err != nil {
		t.Errorf("ClearTrip on absent token: want nil error; got %v", err)
	}
}

// TestClearPendingTrip_RoundTrip verifies that EmitTrip followed by
// ClearPendingTrip marks the ack file acknowledged with ack_method="operator"
// and appends a decision_acknowledged event (bead hk-kgwv).
func TestClearPendingTrip_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ggg"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	clearTime := now.Add(15 * time.Minute)
	cleared, err := sentinel.ClearPendingTrip(context.Background(), dir, clearTime)
	if err != nil {
		t.Fatalf("ClearPendingTrip: %v", err)
	}
	if cleared != tok {
		t.Errorf("ClearPendingTrip returned %q, want %q", cleared, tok)
	}

	// Ack file must be acknowledged.
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("ack status: got %q, want %q", ack["status"], "acknowledged")
	}

	// decision_acknowledged event must carry ack_method="operator".
	acked := scanDecisionAcknowledged(t, dir)
	if len(acked) != 1 {
		t.Fatalf("expected 1 decision_acknowledged event; got %d", len(acked))
	}
	if acked[0]["ack_token"] != tok {
		t.Errorf("ack_token: got %q, want %q", acked[0]["ack_token"], tok)
	}
	if acked[0]["ack_method"] != "operator" {
		t.Errorf("ack_method: got %q, want %q", acked[0]["ack_method"], "operator")
	}
}

// TestClearPendingTrip_NoPendingTrip verifies that ClearPendingTrip is a
// no-op when no pending sentinel exception exists (idempotent).
func TestClearPendingTrip_NoPendingTrip(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.ClearPendingTrip(context.Background(), dir, now)
	if err != nil {
		t.Errorf("ClearPendingTrip with no pending trip: want nil error; got %v", err)
	}
	if tok != "" {
		t.Errorf("ClearPendingTrip with no pending trip: want empty token; got %q", tok)
	}
}

// TestEmitTripClearTripRoundTrip verifies the full trip→clear cycle:
// after clear, a new EmitTrip can write a fresh exception (no stale pending).
func TestEmitTripClearTripRoundTrip(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// First trip.
	tok1, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-eee"},
		Now:          now,
	})
	if err != nil || tok1 == "" {
		t.Fatalf("first EmitTrip: %v", err)
	}

	// Clear the trip.
	if err := sentinel.ClearTrip(context.Background(), dir, tok1, now.Add(30*time.Minute)); err != nil {
		t.Fatalf("ClearTrip: %v", err)
	}

	// Second trip — no pending exception remains, so a new one is written.
	tok2, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-fff"},
		Now:          now.Add(time.Hour),
	})
	if err != nil || tok2 == "" {
		t.Fatalf("second EmitTrip: %v", err)
	}
	if tok1 == tok2 {
		t.Errorf("expected fresh ack_token after clear; got same token %q", tok1)
	}

	// Two decision_required events total.
	events := scanDecisionRequired(t, dir)
	if len(events) != 2 {
		t.Errorf("expected 2 decision_required events after round-trip; got %d", len(events))
	}

	// Verify event order: 2 decision_required + 1 decision_acknowledged.
	allTypes := scanEventTypes(t, dir)
	if len(allTypes) != 3 {
		t.Errorf("expected 3 total events; got %d: %v", len(allTypes), allTypes)
	}
}
