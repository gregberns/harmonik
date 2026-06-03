package main

// comms_presence_who_hk6vwi3_test.go — unit tests for ComputePresenceRegistry and
// the "comms who" state machine (hk-6vwi3 fix #1 + state machine).
//
// Test coverage per task spec:
//   (a) sent message 30s ago => online despite no beat in 200s (fix #1: agent_message activity)
//   (b) receive-only agent with refresh beat within 120s => online
//   (c) last seen 5m ago => GetPresenceState returns Stale, not absent
//   (d) after 'leave' beat => offline immediately, never stale
//   (e) GetPresenceState state-machine boundaries
//
// Bead ref: hk-6vwi3.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// presenceTestEvent builds a JSONL event line for the given type with the given payload.
func presenceTestEvent(eventID, ts, evType string, payload map[string]any) string {
	payloadBytes, _ := json.Marshal(payload)
	ev := map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   ts,
		"source_subsystem": "daemon.comms",
		"payload":          json.RawMessage(payloadBytes),
	}
	line, _ := json.Marshal(ev)
	return string(line)
}

// presenceJoinEvent emits an agent_presence join/online event.
func presenceJoinEvent(eventID, ts, agent string) string {
	return presenceTestEvent(eventID, ts, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    "online",
		"last_seen": ts,
		"reason":    "join",
	})
}

// presenceRefreshEvent emits an agent_presence refresh/online event.
func presenceRefreshEvent(eventID, ts, agent string) string {
	return presenceTestEvent(eventID, ts, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    "online",
		"last_seen": ts,
		"reason":    "refresh",
	})
}

// presenceLeaveEvent emits an agent_presence leave/offline event.
func presenceLeaveEvent(eventID, ts, agent string) string {
	return presenceTestEvent(eventID, ts, "agent_presence", map[string]any{
		"agent":     agent,
		"status":    "offline",
		"last_seen": ts,
		"reason":    "leave",
	})
}

// messageEvent emits an agent_message event with from/to/body fields.
func messageEvent(eventID, ts, from, to string) string {
	return presenceTestEvent(eventID, ts, "agent_message", map[string]any{
		"from": from,
		"to":   to,
		"body": "ping",
	})
}

// buildEventsFile creates a temp project dir with events.jsonl containing the given lines.
func buildEventsFile(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("buildEventsFile: mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(eventsDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("buildEventsFile: create: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, writeErr := fmt.Fprintln(f, line); writeErr != nil {
			t.Fatalf("buildEventsFile: write: %v", writeErr)
		}
	}
	return filepath.Join(dir, ".harmonik", "events", "events.jsonl")
}

// captureCommsWho runs runCommsWhoSubcommand and captures stdout.
func captureCommsWho(t *testing.T, projectDir string, jsonOut bool) (string, int) {
	t.Helper()
	args := []string{"--project", projectDir}
	if jsonOut {
		args = append(args, "--json")
	}
	// commsLogFixture already creates .harmonik/events/; runCommsWhoSubcommand
	// expects the project dir (not the events dir). We receive projectDir here.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureCommsWho: pipe: %v", err)
	}
	os.Stdout = w
	code := runCommsWhoSubcommand(args)
	_ = w.Close()
	os.Stdout = oldStdout
	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, readErr := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if readErr != nil {
			break
		}
	}
	_ = r.Close()
	return buf.String(), code
}

// extractProjectDir returns the directory containing .harmonik/ given the eventsPath.
func extractProjectDir(eventsPath string) string {
	// eventsPath = .../proj/.harmonik/events/events.jsonl
	// we want .../proj
	return filepath.Dir(filepath.Dir(filepath.Dir(eventsPath)))
}

// ---------------------------------------------------------------------------
// Test (a): activity-derived liveness — sent message 30s ago, beat 200s ago
// ---------------------------------------------------------------------------

// TestPresenceWho_ActivityDerivedLiveness verifies fix #1: an agent whose latest
// presence beat is 200s old (beyond presenceTTL=120s) but who sent a message
// 30s ago is reported as online by ComputePresenceRegistry.
func TestPresenceWho_ActivityDerivedLiveness(t *testing.T) {
	beatTS := time.Now().Add(-200 * time.Second).UTC().Format(time.RFC3339)
	msgTS := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)

	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", beatTS, "alice"),
		messageEvent("01965b00-0000-7000-8000-000000000002", msgTS, "alice", "bob"),
	}
	eventsPath := buildEventsFile(t, lines)
	registry := ComputePresenceRegistry(eventsPath)

	rec, ok := registry["alice"]
	if !ok {
		t.Fatal("alice not in presence registry")
	}
	if GetPresenceState(rec) != PresenceStateOnline {
		t.Errorf("expected alice to be Online (sent msg 30s ago); got state %d, effective_last_seen=%v",
			GetPresenceState(rec), rec.EffectiveLastSeen)
	}
}

// ---------------------------------------------------------------------------
// Test (b): receive-only agent with refresh beat within 120s => online
// ---------------------------------------------------------------------------

// TestPresenceWho_RefreshBeatKeepsOnline verifies that a refresh beat emitted
// within the presenceTTL window (e.g., by a comms-recv handler — fix #2) keeps
// a receive-only agent visible as online.
func TestPresenceWho_RefreshBeatKeepsOnline(t *testing.T) {
	// Agent never sent a message but received a refresh beat 30s ago.
	beatTS := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)

	lines := []string{
		presenceRefreshEvent("01965b00-0000-7000-8000-000000000001", beatTS, "bob"),
	}
	eventsPath := buildEventsFile(t, lines)
	registry := ComputePresenceRegistry(eventsPath)

	rec, ok := registry["bob"]
	if !ok {
		t.Fatal("bob not in presence registry")
	}
	if GetPresenceState(rec) != PresenceStateOnline {
		t.Errorf("expected bob to be Online (refresh beat 30s ago); got state %d", GetPresenceState(rec))
	}
}

// ---------------------------------------------------------------------------
// Test (c): stale agent (5m ago) shows as stale, not absent
// ---------------------------------------------------------------------------

// TestPresenceWho_StaleAgent verifies that an agent last seen 5 minutes ago
// (beyond presenceTTL=120s but within presenceStaleCutoff=10m) is in state
// PresenceStateStale — not offline or absent — and appears in "comms who" output.
func TestPresenceWho_StaleAgent(t *testing.T) {
	beatTS := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", beatTS, "charlie"),
	}
	eventsPath := buildEventsFile(t, lines)
	registry := ComputePresenceRegistry(eventsPath)

	rec, ok := registry["charlie"]
	if !ok {
		t.Fatal("charlie not in presence registry")
	}
	if GetPresenceState(rec) != PresenceStateStale {
		t.Errorf("expected charlie to be Stale (5m ago); got state %d", GetPresenceState(rec))
	}

	// comms who must include charlie with a "stale" annotation.
	projDir := extractProjectDir(eventsPath)
	out, code := captureCommsWho(t, projDir, false)
	if code != 0 {
		t.Fatalf("comms who: expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "charlie") {
		t.Errorf("comms who: stale agent 'charlie' must appear in output; got: %q", out)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("comms who: stale agent must have 'stale' annotation; got: %q", out)
	}
}

// TestPresenceWho_StaleAgentJSON verifies the JSON output includes status:"stale".
func TestPresenceWho_StaleAgentJSON(t *testing.T) {
	beatTS := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)

	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", beatTS, "charlie"),
	}
	eventsPath := buildEventsFile(t, lines)
	projDir := extractProjectDir(eventsPath)

	out, code := captureCommsWho(t, projDir, true)
	if code != 0 {
		t.Fatalf("comms who --json: expected exit 0, got %d", code)
	}
	if !strings.Contains(out, `"status":"stale"`) {
		t.Errorf("comms who --json: expected status=stale; got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Test (d): leave beat => immediately offline, never stale
// ---------------------------------------------------------------------------

// TestPresenceWho_LeaveShortCircuits verifies that an explicit leave beat
// (status=offline) causes the agent to be reported as PresenceStateOffline
// immediately — not stale — even if the beat was recent.
func TestPresenceWho_LeaveShortCircuits(t *testing.T) {
	joinTS := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	leaveTS := time.Now().Add(-1 * time.Second).UTC().Format(time.RFC3339)

	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", joinTS, "dave"),
		presenceLeaveEvent("01965b00-0000-7000-8000-000000000002", leaveTS, "dave"),
	}
	eventsPath := buildEventsFile(t, lines)
	registry := ComputePresenceRegistry(eventsPath)

	rec, ok := registry["dave"]
	if !ok {
		t.Fatal("dave not in presence registry")
	}
	if GetPresenceState(rec) != PresenceStateOffline {
		t.Errorf("expected dave to be Offline after leave; got state %d", GetPresenceState(rec))
	}
}

// TestPresenceWho_LeaveNotInWhoOutput verifies that an agent with a leave beat
// does NOT appear in "comms who" output.
func TestPresenceWho_LeaveNotInWhoOutput(t *testing.T) {
	joinTS := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	leaveTS := time.Now().Add(-1 * time.Second).UTC().Format(time.RFC3339)

	lines := []string{
		presenceJoinEvent("01965b00-0000-7000-8000-000000000001", joinTS, "dave"),
		presenceLeaveEvent("01965b00-0000-7000-8000-000000000002", leaveTS, "dave"),
	}
	eventsPath := buildEventsFile(t, lines)
	projDir := extractProjectDir(eventsPath)

	out, code := captureCommsWho(t, projDir, false)
	if code != 0 {
		t.Fatalf("comms who: expected exit 0, got %d", code)
	}
	if strings.Contains(out, "dave") {
		t.Errorf("comms who: departed agent 'dave' must NOT appear; got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// State machine boundary tests
// ---------------------------------------------------------------------------

// TestGetPresenceState_Boundaries verifies the exact cutoffs between states.
func TestGetPresenceState_Boundaries(t *testing.T) {
	cases := []struct {
		name     string
		rec      PresenceRecord
		wantState PresenceState
	}{
		{
			name: "online just inside TTL",
			rec: PresenceRecord{
				Agent:             "agent",
				Status:            "online",
				EffectiveLastSeen: time.Now().Add(-100 * time.Second),
			},
			wantState: PresenceStateOnline,
		},
		{
			name: "stale just past TTL",
			rec: PresenceRecord{
				Agent:             "agent",
				Status:            "online",
				EffectiveLastSeen: time.Now().Add(-130 * time.Second),
			},
			wantState: PresenceStateStale,
		},
		{
			name: "offline past stale cutoff",
			rec: PresenceRecord{
				Agent:             "agent",
				Status:            "online",
				EffectiveLastSeen: time.Now().Add(-11 * time.Minute),
			},
			wantState: PresenceStateOffline,
		},
		{
			name: "offline on leave beat regardless of recency",
			rec: PresenceRecord{
				Agent:             "agent",
				Status:            "offline",
				EffectiveLastSeen: time.Now().Add(-1 * time.Second),
			},
			wantState: PresenceStateOffline,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetPresenceState(tc.rec)
			if got != tc.wantState {
				t.Errorf("GetPresenceState(%v) = %d, want %d", tc.name, got, tc.wantState)
			}
		})
	}
}

// TestPresenceWho_NoAgents verifies that an empty events.jsonl exits 0 silently.
func TestPresenceWho_NoAgents(t *testing.T) {
	eventsPath := buildEventsFile(t, nil)
	projDir := extractProjectDir(eventsPath)
	_, code := captureCommsWho(t, projDir, false)
	if code != 0 {
		t.Fatalf("comms who empty: expected exit 0, got %d", code)
	}
}

// TestPresenceWho_ActivityAloneDoesNotCreateEntry verifies that an agent_message
// with no agent_presence events does NOT create a registry entry (fix #1 only
// extends existing entries; it does not synthesize ghost agents).
func TestPresenceWho_ActivityAloneDoesNotCreateEntry(t *testing.T) {
	msgTS := time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)

	lines := []string{
		messageEvent("01965b00-0000-7000-8000-000000000001", msgTS, "ghost", "other"),
	}
	eventsPath := buildEventsFile(t, lines)
	registry := ComputePresenceRegistry(eventsPath)

	if _, ok := registry["ghost"]; ok {
		t.Error("agent_message alone must NOT create a presence entry (no join beat)")
	}
}
