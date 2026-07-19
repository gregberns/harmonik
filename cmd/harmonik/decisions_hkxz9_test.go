package main

// decisions_hkxz9_test.go — unit tests for the agent-side `harmonik decisions`
// CLI (hitl-decisions component K2, bead hk-xz9).
//
// Coverage (per 07-tasks.md K2 acceptance criteria, fast-gate level — the live
// raise→answer→wake end-to-end is the hk-rz4 scenario, a later bead):
//   - verb/flag parsing: raise requires --question + ≥1 --option; withdraw
//     requires exactly one <id> + a valid --reason; missing-socket → exit 17.
//   - the client-side re-project fold (decisionsClientProjection) matches K3's
//     open-set semantics on a synthetic log (add on needed/event_id, remove on
//     terminal/decision_id, dedupe on event_id — N2).
//   - the "already-terminal → return immediately" check (decisionTerminalInLog)
//     finds a logged terminal and applies first-writer-wins (N3).
//   - decisionTerminalFromEvent matches a live stream event by decision_id.
//
// Helpers use the prefix "dx9" so they do not collide with other cmd/harmonik
// test helpers (e.g. the comms_* tests).
//
// Bead ref: hk-xz9 (K2).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// decisionRow is one open decision in the client-side projection (the cmd/harmonik
// mirror of internal/daemon.Decision — kept separate because the package boundary
// forbids importing the daemon type). Test-only: it backs decisionsClientProjection,
// the parity fold that this file's tests assert against K3's open-set semantics.
type decisionRow struct {
	DecisionID   string
	Question     string
	Options      []string
	BlockedAgent string
	ContextLink  string
}

// decisionsClientProjection is the client-side equivalent of K3's
// decisionsProjection (internal/daemon, un-importable from cmd/harmonik). It is
// the SAME fold: ADD on decision_needed keyed by the event's own event_id;
// REMOVE on decision_resolved/decision_withdrawn keyed by payload.decision_id;
// dedupe on event_id (N2). Returns the OPEN set keyed by decision_id.
//
// It lives in the test file because production code has no reader: the wait path
// uses the lighter decisionTerminalInLog (one decision, terminal only). This full
// fold exists solely to assert, in this file's tests, that the K2-side semantics
// match K3's.
func decisionsClientProjection(eventsPath string) map[string]decisionRow {
	var zeroID core.EventID
	open := make(map[string]decisionRow)
	seen := make(map[string]struct{})
	for evt := range eventbus.ScanAfter(eventsPath, zeroID) {
		evID := evt.EventID.String()
		if _, dup := seen[evID]; dup {
			continue
		}
		switch evt.Type {
		case string(core.EventTypeDecisionNeeded):
			var p core.DecisionNeededPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				continue
			}
			seen[evID] = struct{}{}
			open[evID] = decisionRow{
				DecisionID:   evID,
				Question:     p.Question,
				Options:      p.Options,
				BlockedAgent: p.BlockedAgent,
				ContextLink:  p.ContextLink,
			}
		case string(core.EventTypeDecisionResolved):
			var p core.DecisionResolvedPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)
		case string(core.EventTypeDecisionWithdrawn):
			var p core.DecisionWithdrawnPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				continue
			}
			if p.DecisionID == "" {
				continue
			}
			seen[evID] = struct{}{}
			delete(open, p.DecisionID)
		}
	}
	return open
}

// Canonical UUIDv7-shaped event_ids (lexicographic == chronological). Suffix
// encodes role: 1xxx = decision_needed (the decision_id), 2xxx = resolved,
// 3xxx = withdrawn.
const (
	dx9D1 = "01965b00-0000-7000-8000-00000000a0a1" // needed → stays open
	dx9D2 = "01965b00-0000-7000-8000-00000000a0a2" // needed → resolved
	dx9D3 = "01965b00-0000-7000-8000-00000000a0a3" // needed → withdrawn
	dx9D4 = "01965b00-0000-7000-8000-00000000a0a4" // needed → stays open

	dx9R2  = "01965b00-0000-7000-8000-00000000b0b2" // resolves dx9D2
	dx9R2b = "01965b00-0000-7000-8000-00000000b0c2" // SECOND resolve of dx9D2 (N3 no-op)
	dx9W3  = "01965b00-0000-7000-8000-00000000c0c3" // withdraws dx9D3
)

// dx9Event builds one EV-001 JSONL envelope line, mirroring how the daemon
// writes events (and the K3 test's dprojEvent).
func dx9Event(eventID, evType string, payload any) string {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("dx9Event: marshal payload: %v", err))
	}
	ev := map[string]any{
		"event_id":         eventID,
		"schema_version":   1,
		"type":             evType,
		"timestamp_wall":   "2026-06-13T00:00:00Z",
		"source_subsystem": "test",
		"payload":          json.RawMessage(payloadBytes),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		panic(fmt.Sprintf("dx9Event: marshal envelope: %v", err))
	}
	return string(line)
}

func dx9Needed(eventID, question string, options []string, blockedAgent, contextLink string) string {
	return dx9Event(eventID, "decision_needed", map[string]any{
		"question":      question,
		"options":       options,
		"blocked_agent": blockedAgent,
		"context_link":  contextLink,
	})
}

func dx9Resolved(eventID, decisionID, chosenOption string) string {
	return dx9Event(eventID, "decision_resolved", map[string]any{
		"decision_id":   decisionID,
		"chosen_option": chosenOption,
		"resolver":      "operator",
	})
}

func dx9Withdrawn(eventID, decisionID, reason, by string) string {
	return dx9Event(eventID, "decision_withdrawn", map[string]any{
		"decision_id": decisionID,
		"reason":      reason,
		"by":          by,
	})
}

// dx9BuildEventsFile writes a temp events.jsonl with the given lines and returns
// the path.
func dx9BuildEventsFile(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("dx9BuildEventsFile: mkdir: %v", err)
	}
	path := filepath.Join(eventsDir, "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("dx9BuildEventsFile: create: %v", err)
	}
	defer func() { _ = f.Close() }()
	for _, line := range lines {
		if _, werr := fmt.Fprintln(f, line); werr != nil {
			t.Fatalf("dx9BuildEventsFile: write: %v", werr)
		}
	}
	return path
}

func dx9OpenKeys(open map[string]decisionRow) []string {
	keys := make([]string, 0, len(open))
	for k := range open {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ----------------------------------------------------------------------------
// Client-side re-project fold parity (matches K3 decisionsProjection semantics)
// ----------------------------------------------------------------------------

func TestDecisionsClientProjection_OpenSet(t *testing.T) {
	lines := []string{
		dx9Needed(dx9D1, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa"),
		dx9Needed(dx9D2, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb"),
		dx9Needed(dx9D3, "Approve spend", []string{"approve", "deny"}, "carol", "hk-ccc"),
		dx9Needed(dx9D4, "Rename field?", []string{"keep", "rename"}, "dave", "hk-ddd"),
		// DUPLICATE delivery of the D1 decision_needed event (N2): fold once.
		dx9Needed(dx9D1, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa"),
		// Resolve D2, withdraw D3 (keyed by payload.decision_id).
		dx9Resolved(dx9R2, dx9D2, "eu"),
		dx9Withdrawn(dx9W3, dx9D3, "self_obsoleted", "carol"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	open := decisionsClientProjection(eventsPath)

	wantKeys := []string{dx9D1, dx9D4}
	gotKeys := dx9OpenKeys(open)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("client open set = %v, want %v", gotKeys, wantKeys)
	}

	d1 := open[dx9D1]
	if d1.DecisionID != dx9D1 || d1.Question != "Ship to prod?" || d1.BlockedAgent != "alice" {
		t.Errorf("D1 fields wrong: %+v", d1)
	}
	if !reflect.DeepEqual(d1.Options, []string{"yes", "no"}) {
		t.Errorf("D1.Options = %v, want [yes no]", d1.Options)
	}
}

func TestDecisionsClientProjection_MissingFileEmpty(t *testing.T) {
	open := decisionsClientProjection(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if len(open) != 0 {
		t.Fatalf("missing file should yield empty open set, got %d entries", len(open))
	}
}

// ----------------------------------------------------------------------------
// Already-terminal scan (the §4 step-3 "return immediately" branch)
// ----------------------------------------------------------------------------

func TestDecisionTerminalInLog_Resolved(t *testing.T) {
	lines := []string{
		dx9Needed(dx9D2, "Pick region", []string{"us", "eu"}, "bob", "hk-bbb"),
		dx9Resolved(dx9R2, dx9D2, "eu"),
		// A SECOND resolve of the same decision_id — N3 first-writer-wins: the
		// first ("eu") must win; this later one must NOT change the outcome.
		dx9Resolved(dx9R2b, dx9D2, "us"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	term, ok := decisionTerminalInLog(eventsPath, dx9D2)
	if !ok {
		t.Fatal("expected D2 to be terminal in log")
	}
	if !term.Resolved {
		t.Fatal("expected a resolved terminal")
	}
	if term.ChosenOption != "eu" {
		t.Errorf("first-writer-wins (N3): chosen_option = %q, want %q", term.ChosenOption, "eu")
	}
}

func TestDecisionTerminalInLog_Withdrawn(t *testing.T) {
	lines := []string{
		dx9Needed(dx9D3, "Approve spend", []string{"approve", "deny"}, "carol", "hk-ccc"),
		dx9Withdrawn(dx9W3, dx9D3, "self_obsoleted", "carol"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	term, ok := decisionTerminalInLog(eventsPath, dx9D3)
	if !ok {
		t.Fatal("expected D3 to be terminal (withdrawn) in log")
	}
	if term.Resolved {
		t.Fatal("expected a withdrawn terminal, got resolved")
	}
	if term.Reason != "self_obsoleted" {
		t.Errorf("withdrawal reason = %q, want self_obsoleted", term.Reason)
	}
}

func TestDecisionTerminalInLog_StillOpen(t *testing.T) {
	lines := []string{
		dx9Needed(dx9D1, "Ship to prod?", []string{"yes", "no"}, "alice", "hk-aaa"),
	}
	eventsPath := dx9BuildEventsFile(t, lines)

	if _, ok := decisionTerminalInLog(eventsPath, dx9D1); ok {
		t.Fatal("open decision must NOT report a terminal (would skip the blocked-wait)")
	}
}

// ----------------------------------------------------------------------------
// Live-stream terminal matcher
// ----------------------------------------------------------------------------

func TestDecisionTerminalFromEvent(t *testing.T) {
	resolved := core.Event{
		Type:    string(core.EventTypeDecisionResolved),
		Payload: mustJSON(t, core.DecisionResolvedPayload{DecisionID: dx9D2, ChosenOption: "eu"}),
	}
	if term, ok := decisionTerminalFromEvent(resolved, dx9D2); !ok || !term.Resolved || term.ChosenOption != "eu" {
		t.Errorf("resolved match failed: ok=%v term=%+v", ok, term)
	}
	// Wrong decision_id → no match (the wait filters by decision_id).
	if _, ok := decisionTerminalFromEvent(resolved, dx9D1); ok {
		t.Error("event for a different decision_id must NOT match")
	}

	withdrawn := core.Event{
		Type:    string(core.EventTypeDecisionWithdrawn),
		Payload: mustJSON(t, core.DecisionWithdrawnPayload{DecisionID: dx9D3, Reason: core.DecisionWithdrawnReasonSelfObsoleted}),
	}
	if term, ok := decisionTerminalFromEvent(withdrawn, dx9D3); !ok || term.Resolved || term.Reason != "self_obsoleted" {
		t.Errorf("withdrawn match failed: ok=%v term=%+v", ok, term)
	}

	// A non-terminal type (e.g. heartbeat / decision_needed) never matches.
	heartbeat := core.Event{Type: "heartbeat", Payload: json.RawMessage(`{}`)}
	if _, ok := decisionTerminalFromEvent(heartbeat, dx9D2); ok {
		t.Error("heartbeat must NOT match as a terminal")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return b
}

// ----------------------------------------------------------------------------
// Verb / flag parsing
// ----------------------------------------------------------------------------

func TestDecisionsRaise_RequiresQuestionAndOption(t *testing.T) {
	// No flags → missing --question → exit 1.
	if rc := runDecisionsRaiseSubcommand([]string{"--option", "a"}); rc != 1 {
		t.Errorf("raise without --question: rc = %d, want 1", rc)
	}
	// --question but no --option → exit 1.
	if rc := runDecisionsRaiseSubcommand([]string{"--question", "Q?"}); rc != 1 {
		t.Errorf("raise without --option: rc = %d, want 1", rc)
	}
}

func TestDecisionsRaise_MissingDaemonExit17(t *testing.T) {
	// Well-formed flags but a project with no daemon socket → exit 17.
	dir := t.TempDir()
	rc := runDecisionsRaiseSubcommand([]string{
		"--question", "Ship?", "--option", "yes", "--option", "no",
		"--project", dir,
	})
	if rc != 17 {
		t.Errorf("raise with no daemon: rc = %d, want 17", rc)
	}
}

func TestDecisionsWithdraw_RequiresIDAndValidReason(t *testing.T) {
	// No positional id → exit 1.
	if rc := runDecisionsWithdrawSubcommand([]string{"--reason", "self_obsoleted"}); rc != 1 {
		t.Errorf("withdraw without id: rc = %d, want 1", rc)
	}
	// Two positionals → exit 1.
	if rc := runDecisionsWithdrawSubcommand([]string{"id1", "id2"}); rc != 1 {
		t.Errorf("withdraw with two ids: rc = %d, want 1", rc)
	}
	// Invalid reason → exit 1 (rejected before any dial).
	if rc := runDecisionsWithdrawSubcommand([]string{dx9D1, "--reason", "bogus"}); rc != 1 {
		t.Errorf("withdraw with bogus reason: rc = %d, want 1", rc)
	}
}

func TestDecisionsWithdraw_MissingDaemonExit17(t *testing.T) {
	dir := t.TempDir()
	rc := runDecisionsWithdrawSubcommand([]string{dx9D1, "--reason", "self_obsoleted", "--project", dir})
	if rc != 17 {
		t.Errorf("withdraw with no daemon: rc = %d, want 17", rc)
	}
}

func TestDecisionsWait_RequiresExactlyOneID(t *testing.T) {
	if rc := runDecisionsWaitSubcommand([]string{}); rc != 1 {
		t.Errorf("wait without id: rc = %d, want 1", rc)
	}
	if rc := runDecisionsWaitSubcommand([]string{"id1", "id2"}); rc != 1 {
		t.Errorf("wait with two ids: rc = %d, want 1", rc)
	}
}

// TestDecisionsSubcommand_Routing covers the verb switch: unknown verb → exit 2,
// help → exit 0, and the K4 operator verbs (list/show/answer) are now ROUTED —
// they MUST NOT return the unrecognised-verb code (2). With no daemon socket and
// no args, list dials and gets exit 17 (socket absent); show/answer hit their
// arg-validation (exit 1) before dialling. The contract this test asserts is
// "no longer 2" (i.e. the verb is recognised and dispatched), per the K4 bead.
func TestDecisionsSubcommand_Routing(t *testing.T) {
	if rc := runDecisionsSubcommand([]string{"--help"}); rc != 0 {
		t.Errorf("decisions --help: rc = %d, want 0", rc)
	}
	if rc := runDecisionsSubcommand([]string{"bogus-verb"}); rc != 2 {
		t.Errorf("decisions bogus-verb: rc = %d, want 2", rc)
	}
	// list/show/answer are K4 — now routed. The only forbidden outcome is the
	// unrecognised-verb exit code 2: getting anything else proves they routed.
	// Point each at an absent socket so list cannot hang on a real daemon.
	absentSock := filepath.Join(t.TempDir(), "no-daemon.sock")
	cases := []struct {
		verb string
		args []string
	}{
		// list → routes, dials the (absent) socket → exit 17 (NOT 2).
		{"list", []string{"list", "--socket", absentSock}},
		// show with no id → routes, arg-validation → exit 1 (NOT 2).
		{"show", []string{"show"}},
		// answer with no args → routes, arg-validation → exit 1 (NOT 2).
		{"answer", []string{"answer"}},
	}
	for _, c := range cases {
		if rc := runDecisionsSubcommand(c.args); rc == 2 {
			t.Errorf("decisions %s (K4): rc = 2 (unrecognised) — verb is not routed", c.verb)
		}
	}
}
