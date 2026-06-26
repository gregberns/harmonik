package watch_test

// ledger_we10_test.go — RED→GREEN tests for WE10 watch ledger polish.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/watch"
)

func ledgerFixtureEventWithPayload(t *testing.T, evType string, payload map[string]any) core.Event {
	t.Helper()
	ev := ledgerFixtureEvent(t, evType)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("ledgerFixtureEventWithPayload: marshal: %v", err)
	}
	ev.Payload = raw
	return ev
}

func TestWatchLedger_QueryEventsFiltersByLaneMetadataWithoutCursorWrites(t *testing.T) {
	t.Parallel()

	_, harmonikDir, eventsPath := ledgerFixtureDir(t)

	paulA := ledgerFixtureEventWithPayload(t, "run_started", map[string]any{
		"crew":    "paul",
		"epic_id": "hk-var9b",
		"lane":    "wake-economy",
	})
	gurney := ledgerFixtureEventWithPayload(t, "run_failed", map[string]any{
		"owning_epic_assignee": "gurney",
		"owning_epic_id":       "hk-6l941",
		"lane":                 "remote-test-pyramid",
	})
	paulB := ledgerFixtureEventWithPayload(t, "run_completed", map[string]any{
		"owning_epic_assignee": "paul",
		"owning_epic_id":       "hk-var9b",
		"queue":                "wake-economy",
	})
	ledgerFixtureAppend(t, eventsPath, []core.Event{paulA, gurney, paulB})

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	got, err := ledger.QueryEvents(eventsPath, watch.LaneQuery{Crew: "paul", Epic: "hk-var9b", Lane: "wake-economy"})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("QueryEvents: want 2 paul wake-economy events, got %d: %v", len(got), got)
	}
	if got[0].EventID != paulA.EventID || got[1].EventID != paulB.EventID {
		t.Fatalf("QueryEvents order/filter mismatch: got [%s %s], want [%s %s]",
			got[0].EventID, got[1].EventID, paulA.EventID, paulB.EventID)
	}

	if cursor := readCursorFile(t, harmonikDir); cursor != "" {
		t.Fatalf("QueryEvents must not advance watch cursor, got cursor %q", cursor)
	}
	recvCursorDir := filepath.Join(harmonikDir, "comms", "cursors")
	if _, statErr := os.Stat(recvCursorDir); !os.IsNotExist(statErr) {
		t.Fatalf("QueryEvents must not create comms recv cursor dir, stat err=%v", statErr)
	}
}

func TestWatchEscalation_OpsMonitorReceiptRefinesDigestEventDriven(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)
	opsDir := filepath.Join(harmonikDir, "ops-monitor")
	if err := os.MkdirAll(opsDir, 0o755); err != nil {
		t.Fatalf("mkdir ops-monitor: %v", err)
	}
	opsReport := []byte(`{"ts":"2026-06-25T00:00:00Z","checks":{"watch":{"status":"flagged"}}}`)
	if err := os.WriteFile(filepath.Join(opsDir, "latest.json"), opsReport, 0o644); err != nil {
		t.Fatalf("write ops-monitor latest: %v", err)
	}

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: &mockSender{}}

	timerLikeEvent := ledgerFixtureEventWithPayload(t, "agent_message", map[string]any{
		"from":  "ops-monitor",
		"to":    "watch",
		"topic": "status",
		"body":  "ops-monitor: all clear",
	})
	handled, err := engine.ProcessOpsMonitorReceipt(timerLikeEvent)
	if err != nil {
		t.Fatalf("ProcessOpsMonitorReceipt(non-digest): %v", err)
	}
	if handled {
		t.Fatal("non-[IMMEDIATE]/[DIGEST] ops-monitor message must not be handled")
	}
	if d := readDigestFile(t, harmonikDir); d != nil {
		t.Fatalf("digest should not be written before a receipt signal, got %+v", d)
	}

	receiptEvent := ledgerFixtureEventWithPayload(t, "agent_message", map[string]any{
		"from":  "ops-monitor",
		"to":    "watch",
		"topic": "status",
		"body":  "[DIGEST] ops-monitor: watch-stalled cleared | see .harmonik/ops-monitor/latest.json",
	})
	handled, err = engine.ProcessOpsMonitorReceipt(receiptEvent)
	if err != nil {
		t.Fatalf("ProcessOpsMonitorReceipt([DIGEST]): %v", err)
	}
	if !handled {
		t.Fatal("[DIGEST] ops-monitor message should be handled")
	}

	d := readDigestFile(t, harmonikDir)
	if d == nil {
		t.Fatal("ops-monitor receipt did not write watch latest.json")
	}
	if d.LastOpsMonitorReceipt == nil {
		t.Fatal("watch latest.json missing last_ops_monitor_receipt")
	}
	if d.LastOpsMonitorReceipt.Kind != "DIGEST" {
		t.Fatalf("receipt kind: got %q, want DIGEST", d.LastOpsMonitorReceipt.Kind)
	}
	if d.LastOpsMonitorReceipt.EventID != receiptEvent.EventID.String() {
		t.Fatalf("receipt event_id: got %q, want %q", d.LastOpsMonitorReceipt.EventID, receiptEvent.EventID.String())
	}
	var gotReport, wantReport any
	if err := json.Unmarshal(d.LastOpsMonitorReceipt.Report, &gotReport); err != nil {
		t.Fatalf("unmarshal got report: %v", err)
	}
	if err := json.Unmarshal(opsReport, &wantReport); err != nil {
		t.Fatalf("unmarshal want report: %v", err)
	}
	if gotJSON, wantJSON := mustMarshalJSON(t, gotReport), mustMarshalJSON(t, wantReport); gotJSON != wantJSON {
		t.Fatalf("receipt report: got %s, want %s", gotJSON, wantJSON)
	}

	recvCursorDir := filepath.Join(harmonikDir, "comms", "cursors")
	if _, statErr := os.Stat(recvCursorDir); !os.IsNotExist(statErr) {
		t.Fatalf("ProcessOpsMonitorReceipt must not create comms recv cursor dir, stat err=%v", statErr)
	}
}

func mustMarshalJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(b)
}
