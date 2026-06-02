package main

// comms_log_hkonn1x_test.go — binding tests for `harmonik comms log` (hk-onn1x T5).
//
// Verifies the read-only operator view of agent_message events:
//   - Scans events.jsonl; filters by type, --to, --from, --topic, --since (both forms).
//   - Does NOT advance any cursor.
//   - No daemon connection required.
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/agent-comms/05-spec-draft.md §2.4.
// Bead ref: hk-onn1x (T5).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// commsLogFixture prepares a temp project directory with a populated events.jsonl.
// Returns the project directory path.
func commsLogFixture(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("commsLogFixture: mkdir: %v", err)
	}
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	f, err := os.Create(eventsPath)
	if err != nil {
		t.Fatalf("commsLogFixture: create events.jsonl: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, writeErr := fmt.Fprintln(f, line); writeErr != nil {
			t.Fatalf("commsLogFixture: write: %v", writeErr)
		}
	}
	return dir
}

// commsLogEvent builds a minimal agent_message JSONL line.
// eventID must be a UUIDv7 string. ts is the wall timestamp.
func commsLogEvent(eventID, ts, from, to, topic, body string) string {
	p := map[string]any{
		"from": from,
		"to":   to,
		"body": body,
	}
	if topic != "" {
		p["topic"] = topic
	}
	payload, _ := json.Marshal(p)
	ev := map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             "agent_message",
		"timestamp_wall":   ts,
		"source_subsystem": "daemon.comms",
		"payload":          json.RawMessage(payload),
	}
	line, _ := json.Marshal(ev)
	return string(line)
}

// commsLogNonCommsEvent builds a non-agent_message JSONL line that must be ignored.
func commsLogNonCommsEvent(eventID, ts string) string {
	ev := map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             "run_started",
		"timestamp_wall":   ts,
		"source_subsystem": "daemon",
		"payload":          json.RawMessage(`{}`),
	}
	line, _ := json.Marshal(ev)
	return string(line)
}

// captureCommsLog runs runCommsLogSubcommand with args, capturing stdout.
// Returns (stdout, exitCode).
func captureCommsLog(t *testing.T, args []string) (string, int) {
	t.Helper()
	// Redirect stdout.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureCommsLog: pipe: %v", err)
	}
	os.Stdout = w

	code := runCommsLogSubcommand(args)

	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), code
}

// TestCommsLogNoEvents verifies that an empty events.jsonl exits 0 with no output.
func TestCommsLogNoEvents(t *testing.T) {

	dir := commsLogFixture(t, nil)
	out, code := captureCommsLog(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout for no events, got: %q", out)
	}
}

// TestCommsLogFiltersType verifies that non-agent_message events are excluded.
func TestCommsLogFiltersType(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogNonCommsEvent("01965b00-0000-7000-8000-000000000001", ts),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "alice", "bob", "", "hello"),
	}
	dir := commsLogFixture(t, lines)
	out, code := captureCommsLog(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	lineCount := len(strings.Split(strings.TrimSpace(out), "\n"))
	if lineCount != 1 {
		t.Errorf("expected 1 line (agent_message only), got %d: %q", lineCount, out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected 'alice' in output, got: %q", out)
	}
}

// TestCommsLogFromFilter verifies --from filters by sender.
func TestCommsLogFromFilter(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "", "from alice"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "charlie", "bob", "", "from charlie"),
	}
	dir := commsLogFixture(t, lines)

	out, code := captureCommsLog(t, []string{"--project", dir, "--from", "alice"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "from alice") {
		t.Errorf("expected 'from alice' in output, got: %q", out)
	}
	if strings.Contains(out, "from charlie") {
		t.Errorf("expected 'from charlie' to be filtered out, got: %q", out)
	}
}

// TestCommsLogToFilter verifies --to filters by recipient, including broadcast.
func TestCommsLogToFilter(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "", "to bob"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "alice", "*", "", "broadcast"),
		commsLogEvent("01965b00-0000-7000-8000-000000000003", ts, "alice", "charlie", "", "to charlie"),
	}
	dir := commsLogFixture(t, lines)

	out, code := captureCommsLog(t, []string{"--project", dir, "--to", "bob"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	// "to bob" and broadcast "*" both match --to bob.
	if !strings.Contains(out, "to bob") {
		t.Errorf("expected 'to bob' in output, got: %q", out)
	}
	if !strings.Contains(out, "broadcast") {
		t.Errorf("expected broadcast message in output (--to includes *), got: %q", out)
	}
	if strings.Contains(out, "to charlie") {
		t.Errorf("expected 'to charlie' to be filtered out, got: %q", out)
	}
}

// TestCommsLogTopicFilter verifies --topic filters by topic.
func TestCommsLogTopicFilter(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "status", "pong"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "alice", "bob", "work", "task done"),
	}
	dir := commsLogFixture(t, lines)

	out, code := captureCommsLog(t, []string{"--project", dir, "--topic", "status"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "pong") {
		t.Errorf("expected 'pong' in output, got: %q", out)
	}
	if strings.Contains(out, "task done") {
		t.Errorf("expected 'task done' to be filtered out, got: %q", out)
	}
}

// TestCommsLogSinceEventID verifies --since with an event_id skips events at or before it.
func TestCommsLogSinceEventID(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "", "first"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "alice", "bob", "", "second"),
		commsLogEvent("01965b00-0000-7000-8000-000000000003", ts, "alice", "bob", "", "third"),
	}
	dir := commsLogFixture(t, lines)

	// --since the first event_id should return only second and third.
	out, code := captureCommsLog(t, []string{"--project", dir, "--since", "01965b00-0000-7000-8000-000000000001"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.Contains(out, "first") {
		t.Errorf("expected 'first' to be skipped (at the --since boundary), got: %q", out)
	}
	if !strings.Contains(out, "second") {
		t.Errorf("expected 'second' in output, got: %q", out)
	}
	if !strings.Contains(out, "third") {
		t.Errorf("expected 'third' in output, got: %q", out)
	}
}

// TestCommsLogSinceDuration verifies --since with a duration skips events outside the window.
func TestCommsLogSinceDuration(t *testing.T) {

	// "old" event is 2 hours ago; "recent" event is 5 minutes ago.
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", old, "alice", "bob", "", "old message"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", recent, "alice", "bob", "", "recent message"),
	}
	dir := commsLogFixture(t, lines)

	// --since 1h should exclude the 2h-old event and include the 5m-old event.
	out, code := captureCommsLog(t, []string{"--project", dir, "--since", "1h"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.Contains(out, "old message") {
		t.Errorf("expected 'old message' (2h ago) to be excluded with --since 1h, got: %q", out)
	}
	if !strings.Contains(out, "recent message") {
		t.Errorf("expected 'recent message' (5m ago) in output with --since 1h, got: %q", out)
	}
}

// TestCommsLogJSONOutput verifies --json emits one valid JSON object per matched event.
func TestCommsLogJSONOutput(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "status", "hello"),
		commsLogEvent("01965b00-0000-7000-8000-000000000002", ts, "charlie", "dave", "", "world"),
	}
	dir := commsLogFixture(t, lines)

	out, code := captureCommsLog(t, []string{"--project", dir, "--json"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	jsonLines := strings.Split(strings.TrimSpace(out), "\n")
	if len(jsonLines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %q", len(jsonLines), out)
	}
	for i, jl := range jsonLines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(jl), &ev); err != nil {
			t.Errorf("line %d is not valid JSON: %v — %q", i, err, jl)
		}
		if ev["type"] != "agent_message" {
			t.Errorf("line %d: expected type 'agent_message', got %v", i, ev["type"])
		}
	}
}

// TestCommsLogHumanReadableFormat verifies the human-readable output includes expected fields.
func TestCommsLogHumanReadableFormat(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "orchestrator", "worker", "task", "do the thing"),
	}
	dir := commsLogFixture(t, lines)

	out, code := captureCommsLog(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	// Human-readable must include timestamp, from, to, topic, body.
	for _, want := range []string{"orchestrator", "worker", "task", "do the thing"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in human-readable output, got: %q", want, out)
		}
	}
}

// TestCommsLogUnknownFlagError verifies that an unknown flag exits with code 1.
func TestCommsLogUnknownFlagError(t *testing.T) {

	dir := commsLogFixture(t, nil)
	_, code := captureCommsLog(t, []string{"--project", dir, "--unknown-flag"})
	if code != 1 {
		t.Fatalf("expected exit 1 for unknown flag, got %d", code)
	}
}

// TestCommsLogInvalidSince verifies that a non-UUID, non-duration --since value exits 1.
func TestCommsLogInvalidSince(t *testing.T) {

	dir := commsLogFixture(t, nil)
	_, code := captureCommsLog(t, []string{"--project", dir, "--since", "not-a-uuid-or-duration"})
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid --since, got %d", code)
	}
}

// TestCommsLogDoesNotAdvanceCursor verifies that running comms log leaves no cursor state.
// Cursors live at .harmonik/comms/cursors/<name>; that directory must remain absent.
func TestCommsLogDoesNotAdvanceCursor(t *testing.T) {

	ts := "2026-06-01T10:00:00Z"
	lines := []string{
		commsLogEvent("01965b00-0000-7000-8000-000000000001", ts, "alice", "bob", "", "hello"),
	}
	dir := commsLogFixture(t, lines)

	_, code := captureCommsLog(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	cursorsDir := filepath.Join(dir, ".harmonik", "comms", "cursors")
	if _, err := os.Stat(cursorsDir); err == nil {
		t.Errorf("comms log must not create cursor directory at %s", cursorsDir)
	}
}
