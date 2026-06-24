package watch_test

// escalation_we3_test.go — RED→GREEN tests for WE3 (watch escalation engine).
//
// Three required assertions (task spec):
//   (a) An IMMEDIATE-class event produces exactly ONE comms send escalation to the captain.
//   (b) A PULL-DIGEST event produces ZERO comms sends and writes latest.json.
//   (c) epic_completed (LEDGER-ONLY) produces ZERO watch escalations.
//
// Done-check: these tests must be GREEN.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/watch"
)

// mockSender records calls to SendEscalation for assertion.
type mockSender struct {
	calls []string
}

func (m *mockSender) SendEscalation(summary string) error {
	m.calls = append(m.calls, summary)
	return nil
}

// escalationFixtureDir builds a temp harmonik dir and returns (harmonikDir, eventsPath).
// Reuses the ledgerFixtureDir helper shape from ledger_we2_test.go.
func escalationFixtureDir(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	eventsDir := filepath.Join(harmonikDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("escalationFixtureDir: mkdir %s: %v", eventsDir, err)
	}
	return harmonikDir, filepath.Join(eventsDir, "events.jsonl")
}

// escalationFixtureEvent builds a core.Event for the given type.
// Reuses ledgerFixtureEvent logic.
func escalationFixtureEvent(t *testing.T, evType string) core.Event {
	t.Helper()
	// Delegate to ledgerFixtureEvent which already handles UUIDv7 generation.
	return ledgerFixtureEvent(t, evType)
}

// readDigestFile reads and unmarshals .harmonik/watch/latest.json.
// Returns nil if the file does not exist.
func readDigestFile(t *testing.T, harmonikDir string) *watch.WatchDigest {
	t.Helper()
	path := filepath.Join(harmonikDir, "watch", "latest.json")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("readDigestFile: %v", err)
	}
	var d watch.WatchDigest
	if unmarshalErr := json.Unmarshal(raw, &d); unmarshalErr != nil {
		t.Fatalf("readDigestFile: unmarshal: %v", unmarshalErr)
	}
	return &d
}

// Test (a): An IMMEDIATE-class event (decision_required) produces exactly ONE
// comms send escalation with the expected summary. No second send must occur.
func TestWatchEscalation_ImmediateProducesOneWake(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	sender := &mockSender{}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: sender}

	ev := escalationFixtureEvent(t, "decision_required")
	summary := "bead hk-abc123 double-failed; decision_required — captain must decide: retry, reassign, or close?"

	if err := engine.Process(ev, summary); err != nil {
		t.Fatalf("Process(decision_required): %v", err)
	}

	// Exactly one escalation send.
	if len(sender.calls) != 1 {
		t.Fatalf("want 1 escalation send, got %d: %v", len(sender.calls), sender.calls)
	}
	if sender.calls[0] != summary {
		t.Errorf("escalation body: got %q, want %q", sender.calls[0], summary)
	}

	// No additional sends on a second unrelated IMMEDIATE event.
	ev2 := escalationFixtureEvent(t, "run_failed")
	summary2 := "run hk-xyz failed after 3 iterations — no commit; captain must decide retry or close"
	if err := engine.Process(ev2, summary2); err != nil {
		t.Fatalf("Process(run_failed): %v", err)
	}
	if len(sender.calls) != 2 {
		t.Fatalf("after second IMMEDIATE want 2 total calls, got %d", len(sender.calls))
	}
	if sender.calls[1] != summary2 {
		t.Errorf("second escalation body: got %q, want %q", sender.calls[1], summary2)
	}
}

// Test (b): A PULL-DIGEST event (run_stale — crew-staleness indicator) produces
// ZERO comms sends and writes the flag to latest.json pending_flags.
func TestWatchEscalation_PullDigestNoWake(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	sender := &mockSender{}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: sender}

	ev := escalationFixtureEvent(t, "run_stale")
	flag := "crew paul has been stale for 15m on epic hk-3ef4; may need a nudge or captain staffing decision"

	if err := engine.Process(ev, flag); err != nil {
		t.Fatalf("Process(run_stale): %v", err)
	}

	// Zero comms sends — PULL-DIGEST never wakes the captain.
	if len(sender.calls) != 0 {
		t.Errorf("PULL-DIGEST must produce zero sends, got %d: %v", len(sender.calls), sender.calls)
	}

	// latest.json must be written with the flag in pending_flags.
	d := readDigestFile(t, harmonikDir)
	if d == nil {
		t.Fatal("PULL-DIGEST: latest.json was not written")
	}
	if len(d.PendingFlags) != 1 {
		t.Fatalf("latest.json pending_flags: want 1 entry, got %d: %v", len(d.PendingFlags), d.PendingFlags)
	}
	if d.PendingFlags[0] != flag {
		t.Errorf("pending_flags[0]: got %q, want %q", d.PendingFlags[0], flag)
	}
}

// Test (c): epic_completed is LEDGER-ONLY — the watch MUST NOT escalate it
// (to avoid triple-wake: daemon QuiesceArbiter + captain direct subscribe + watch).
func TestWatchEscalation_EpicCompletedLedgerOnly(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	sender := &mockSender{}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: sender}

	ev := escalationFixtureEvent(t, "epic_completed")
	summary := "epic hk-var9b (wake-economy) completed — all child beads closed"

	if err := engine.Process(ev, summary); err != nil {
		t.Fatalf("Process(epic_completed): %v", err)
	}

	// Zero escalation sends — LEDGER-ONLY events never wake the captain via the watch.
	if len(sender.calls) != 0 {
		t.Errorf("epic_completed must produce zero escalations, got %d: %v",
			len(sender.calls), sender.calls)
	}

	// latest.json must NOT be written (LEDGER-ONLY does not accumulate to digest).
	d := readDigestFile(t, harmonikDir)
	if d != nil && len(d.PendingFlags) > 0 {
		t.Errorf("epic_completed must not add to pending_flags, got %v", d.PendingFlags)
	}
}

// TestWatchEscalation_ClassifyTable validates the taxonomy mapping for the explicitly
// named event types in §4. This is an exhaustive spot-check, not a contract test —
// the three behavioural tests above are the spec-mandated assertions.
func TestWatchEscalation_ClassifyTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		evType string
		want   watch.EscalationClass
	}{
		// IMMEDIATE
		{"decision_required", watch.EscalationImmediate},
		{"run_failed", watch.EscalationImmediate},
		{"review_bypassed", watch.EscalationImmediate},
		{"agent_failed", watch.EscalationImmediate},
		{"review_gate_anomaly", watch.EscalationImmediate},
		// LEDGER-ONLY
		{"epic_completed", watch.EscalationLedgerOnly},
		{"run_started", watch.EscalationLedgerOnly},
		{"run_completed", watch.EscalationLedgerOnly},
		{"agent_output_chunk", watch.EscalationLedgerOnly},
		{"metric", watch.EscalationLedgerOnly},
		{"agent_heartbeat", watch.EscalationLedgerOnly},
		{"session_keeper_warn", watch.EscalationLedgerOnly},
		{"session_keeper_cycle_complete", watch.EscalationLedgerOnly},
		// PULL-DIGEST (default for unregistered types)
		{"run_stale", watch.EscalationPullDigest},
		{"queue_submitted", watch.EscalationPullDigest},
		{"unknown_event_type_xyz", watch.EscalationPullDigest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.evType, func(t *testing.T) {
			t.Parallel()
			ev := core.Event{Type: tc.evType}
			got := watch.Classify(ev)
			if got != tc.want {
				t.Errorf("Classify(%q) = %d, want %d", tc.evType, got, tc.want)
			}
		})
	}
}
