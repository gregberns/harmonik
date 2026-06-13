package daemon

// decisionsprojection_test.go — unit tests for decisionsProjection (hitl-decisions
// component K3, bead hk-qed).
//
// Coverage (per 07-tasks.md K3 acceptance criteria):
//   - A synthetic events.jsonl with N decision_needed, some resolved/withdrawn
//     for a subset, and a DUPLICATE event_id → projection returns EXACTLY the
//     expected open set (resolved/withdrawn removed; key on decision_id;
//     dedupe honored).
//   - PURITY: runs against a log file with no daemon (no socket dial); a missing
//     file yields an empty map.
//   - Field-fidelity: the Decision value carries the rendered fields verbatim
//     from the decision_needed payload.
//
// These helpers use the prefix "dproj" (decisions-projection) per the
// helper-prefix discipline so they do not collide with other daemon-package
// test helpers.
//
// Bead ref: hk-qed (K3).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// dprojEvent builds one EV-001 JSONL envelope line for the given event_id,
// type, and decoded payload object. The payload is marshalled into the
// envelope's "payload" raw-JSON field exactly as the daemon writes it.
func dprojEvent(eventID, evType string, payload any) string {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("dprojEvent: marshal payload: %v", err))
	}
	ev := map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   "2026-06-13T00:00:00Z",
		"source_subsystem": "daemon.decisions",
		"payload":          json.RawMessage(payloadBytes),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		panic(fmt.Sprintf("dprojEvent: marshal envelope: %v", err))
	}
	return string(line)
}

// dprojNeeded builds a decision_needed event line. The event_id IS the
// decision_id (SPEC §1).
func dprojNeeded(eventID, question string, options []string, blockedAgent, contextLink string) string {
	return dprojEvent(eventID, "decision_needed", map[string]any{
		"question":      question,
		"options":       options,
		"blocked_agent": blockedAgent,
		"context_link":  contextLink,
	})
}

// dprojResolved builds a decision_resolved event line keyed (via payload) on
// decisionID. Its own event_id is distinct from decisionID (SPEC C7).
func dprojResolved(eventID, decisionID, chosenOption string) string {
	return dprojEvent(eventID, "decision_resolved", map[string]any{
		"decision_id":   decisionID,
		"chosen_option": chosenOption,
		"resolver":      "operator",
	})
}

// dprojWithdrawn builds a decision_withdrawn event line keyed (via payload) on
// decisionID.
func dprojWithdrawn(eventID, decisionID, reason, by string) string {
	return dprojEvent(eventID, "decision_withdrawn", map[string]any{
		"decision_id": decisionID,
		"reason":      reason,
		"by":          by,
	})
}

// dprojBuildEventsFile writes a temp project events.jsonl containing the given
// lines and returns the events.jsonl path.
func dprojBuildEventsFile(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("dprojBuildEventsFile: mkdir: %v", err)
	}
	path := filepath.Join(eventsDir, "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("dprojBuildEventsFile: create: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, werr := fmt.Fprintln(f, line); werr != nil {
			t.Fatalf("dprojBuildEventsFile: write: %v", werr)
		}
	}
	return path
}

// dprojOpenKeys returns the sorted decision_id keys of the open set, for stable
// set comparison.
func dprojOpenKeys(open map[string]Decision) []string {
	keys := make([]string, 0, len(open))
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Canonical UUIDv7-shaped event_ids (lexicographic == chronological for the
// scan). The suffix encodes role: 1xxx = decision_needed (the decision_id),
// 2xxx = a resolved terminal, 3xxx = a withdrawn terminal.
const (
	dprojD1 = "01965b00-0000-7000-8000-0000000010a1" // needed → stays open
	dprojD2 = "01965b00-0000-7000-8000-0000000010a2" // needed → resolved
	dprojD3 = "01965b00-0000-7000-8000-0000000010a3" // needed → withdrawn
	dprojD4 = "01965b00-0000-7000-8000-0000000010a4" // needed → stays open

	dprojR2 = "01965b00-0000-7000-8000-0000000020b2" // resolves dprojD2
	dprojW3 = "01965b00-0000-7000-8000-0000000030c3" // withdraws dprojD3
)

// TestDecisionsProjection_OpenSet verifies the core fold: N decision_needed,
// one resolved, one withdrawn, plus a DUPLICATE decision_needed event_id →
// the open set is exactly {D1, D4} (D2 resolved out, D3 withdrawn out, the
// duplicate D1 folded once).
func TestDecisionsProjection_OpenSet(t *testing.T) {
	lines := []string{
		dprojNeeded(dprojD1, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa"),
		dprojNeeded(dprojD2, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb"),
		dprojNeeded(dprojD3, "Approve spend", []string{"approve", "deny"}, "carol", "hk-ccc"),
		dprojNeeded(dprojD4, "Rename field?", []string{"keep", "rename"}, "dave", "hk-ddd"),
		// DUPLICATE delivery of the D1 decision_needed event (same event_id) —
		// at-least-once (N2). Must fold once: D1 stays a single open entry.
		dprojNeeded(dprojD1, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa"),
		// Resolve D2 (keyed by payload.decision_id == dprojD2).
		dprojResolved(dprojR2, dprojD2, "eu"),
		// Withdraw D3 (keyed by payload.decision_id == dprojD3).
		dprojWithdrawn(dprojW3, dprojD3, "self_obsoleted", "carol"),
	}
	eventsPath := dprojBuildEventsFile(t, lines)

	open := decisionsProjection(eventsPath)

	wantKeys := []string{dprojD1, dprojD4}
	gotKeys := dprojOpenKeys(open)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("open set keys = %v, want %v", gotKeys, wantKeys)
	}

	// D1 field fidelity — rendered fields copied verbatim from the payload, and
	// the duplicate did not corrupt or double-apply.
	d1, ok := open[dprojD1]
	if !ok {
		t.Fatal("D1 missing from open set")
	}
	if d1.DecisionID != dprojD1 {
		t.Errorf("D1.DecisionID = %q, want %q", d1.DecisionID, dprojD1)
	}
	if d1.Question != "Ship to prod?" {
		t.Errorf("D1.Question = %q, want %q", d1.Question, "Ship to prod?")
	}
	if !reflect.DeepEqual(d1.Options, []string{"yes", "no"}) {
		t.Errorf("D1.Options = %v, want [yes no]", d1.Options)
	}
	if d1.BlockedAgent != "alice" {
		t.Errorf("D1.BlockedAgent = %q, want %q", d1.BlockedAgent, "alice")
	}
	if d1.ContextLink != "hk-aaa" {
		t.Errorf("D1.ContextLink = %q, want %q", d1.ContextLink, "hk-aaa")
	}

	// Negative assertions: resolved/withdrawn dropped out entirely.
	if _, present := open[dprojD2]; present {
		t.Errorf("D2 (resolved) must NOT be in the open set")
	}
	if _, present := open[dprojD3]; present {
		t.Errorf("D3 (withdrawn) must NOT be in the open set")
	}
}

// TestDecisionsProjection_DuplicateTerminalIdempotent verifies N2/N3 at the
// projection level: a re-delivered decision_resolved (same terminal event_id)
// and a resolve for an unknown decision_id are both no-ops — the open set is
// unaffected beyond the single legitimate removal.
func TestDecisionsProjection_DuplicateTerminalIdempotent(t *testing.T) {
	const unknownID = "01965b00-0000-7000-8000-00000000dead"
	lines := []string{
		dprojNeeded(dprojD1, "q1", []string{"a", "b"}, "alice", ""),
		dprojNeeded(dprojD2, "q2", []string{"a", "b"}, "bob", ""),
		dprojResolved(dprojR2, dprojD2, "a"),
		// Duplicate delivery of the SAME resolve event_id — folded once (N2).
		dprojResolved(dprojR2, dprojD2, "a"),
		// Resolve an UNKNOWN decision_id — no-op (N3 projection-level).
		dprojResolved("01965b00-0000-7000-8000-00000000beef", unknownID, "a"),
	}
	eventsPath := dprojBuildEventsFile(t, lines)

	open := decisionsProjection(eventsPath)

	wantKeys := []string{dprojD1}
	gotKeys := dprojOpenKeys(open)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("open set keys = %v, want %v (duplicate/unknown terminal must be no-ops)", gotKeys, wantKeys)
	}
}

// TestDecisionsProjection_MissingFileEmpty verifies the purity / no-daemon
// path: a missing events.jsonl yields a non-nil empty map (de-risks S6).
func TestDecisionsProjection_MissingFileEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, ".harmonik", "events", "events.jsonl")

	open := decisionsProjection(missing)
	if open == nil {
		t.Fatal("decisionsProjection on a missing file returned nil; want empty non-nil map")
	}
	if len(open) != 0 {
		t.Fatalf("decisionsProjection on a missing file returned %d entries, want 0", len(open))
	}
}

// TestDecisionsProjection_Pure_NoDaemonNoSocket asserts the projection runs to
// completion against an on-disk log with NO daemon, NO socket, and NO side
// effects: the input file is byte-for-byte unchanged after projection (EV-020
// read-side purity; de-risks S6 "renders with no aggregator process").
func TestDecisionsProjection_Pure_NoDaemonNoSocket(t *testing.T) {
	lines := []string{
		dprojNeeded(dprojD1, "q1", []string{"a", "b"}, "alice", "hk-aaa"),
		dprojNeeded(dprojD2, "q2", []string{"a", "b"}, "bob", "hk-bbb"),
		dprojResolved(dprojR2, dprojD2, "a"),
	}
	eventsPath := dprojBuildEventsFile(t, lines)

	before, err := os.ReadFile(eventsPath) //nolint:gosec // G304: t.TempDir()-based; not user input
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	open := decisionsProjection(eventsPath)
	if len(open) != 1 {
		t.Fatalf("open set size = %d, want 1", len(open))
	}

	after, err := os.ReadFile(eventsPath) //nolint:gosec // G304: t.TempDir()-based; not user input
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Error("decisionsProjection mutated the events.jsonl log; it must be read-side pure (EV-020)")
	}
}
