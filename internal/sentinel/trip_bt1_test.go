package sentinel_test

// trip_bt1_test.go — BT1 unit-gap tests for sentinel trip emission.
//
// Covers: no-warn-nag over ≥5 governor cycles, and the never-clears-on-bare-
// self-ack invariant (ack FILE is the durability authority; events.jsonl is
// only the observational record).
//
// Bead: hk-tbg8 (flywheel-BT1). Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// TestEmitTrip_NoWarnNag_OverFiveCycles verifies that ≥5 consecutive EmitTrip
// calls — simulating one call per governor cycle while a trip is already pending —
// produce exactly ONE decision_required event and ONE ack file.
//
// Without idempotency the operator's events log would accumulate one notification
// per cycle ("warn-nag"). The spec requires a single exception per trip period.
// Spec: B2 "no-warn-nag over ≥5 cycles".
func TestEmitTrip_NoWarnNag_OverFiveCycles(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	in := sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-bt1a"},
		Now:          now,
	}

	var firstTok string
	for i := 0; i < 6; i++ {
		tok, err := sentinel.EmitTrip(context.Background(), in)
		if err != nil {
			t.Fatalf("cycle %d: EmitTrip: %v", i, err)
		}
		if i == 0 {
			firstTok = tok
		} else if tok != firstTok {
			t.Errorf("cycle %d: ack_token changed (%q → %q) — idempotency violated", i, firstTok, tok)
		}
	}

	// Exactly 1 decision_required event, not 6.
	events := scanDecisionRequired(t, dir)
	if len(events) != 1 {
		t.Errorf("expected 1 decision_required event after 6 cycles; got %d", len(events))
	}

	// Exactly 1 ack file in decision_acks/.
	acksDir := filepath.Join(dir, ".harmonik", "decision_acks")
	entries, err := os.ReadDir(acksDir)
	if err != nil {
		t.Fatalf("read decision_acks/: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 ack file after 6 EmitTrip calls; got %d", len(entries))
	}

	// The trip is still pending — no self-clear occurred across cycles.
	ack := readAckFile(t, dir, firstTok)
	if ack["status"] != "pending" {
		t.Errorf("pending trip must not self-clear across cycles; got status=%q", ack["status"])
	}
}

// TestEmitTrip_NeverClearsOnBareSelfAck verifies that writing a fake
// decision_acknowledged event directly into events.jsonl does NOT change the
// ack-state file from "pending" to "acknowledged".
//
// The ack FILE is the durability authority (EV-043a). Events.jsonl is the
// observational record only. A "bare self-ack" — an agent writing a
// decision_acknowledged line without real movement and without calling ClearTrip
// — cannot unilaterally clear the sentinel block.
//
// Spec: B2 "never-clears-on-bare-self-ack" (pairs with A8/AC4, hk-jvul).
func TestEmitTrip_NeverClearsOnBareSelfAck(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-bt1b"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	// Simulate a "bare self-ack": append a fake decision_acknowledged event
	// directly to events.jsonl without calling ClearTrip.
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	fakePayload, _ := json.Marshal(map[string]interface{}{
		"ack_token":  tok,
		"subject":    map[string]interface{}{"kind": "queue", "id": "sentinel"},
		"ack_method": "self_ack",
		"acked_at":   now.UTC().Format(time.RFC3339),
	})
	fakeEvent, _ := json.Marshal(map[string]interface{}{
		"event_id":         "00000000-0000-0000-0000-000000000001",
		"schema_version":   1,
		"type":             "decision_acknowledged",
		"timestamp_wall":   now.UTC().Format(time.RFC3339),
		"source_subsystem": "test",
		"payload":          json.RawMessage(fakePayload),
	})
	f, openErr := os.OpenFile(eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if openErr != nil {
		t.Fatalf("open events.jsonl: %v", openErr)
	}
	fmt.Fprintf(f, "%s\n", fakeEvent)
	_ = f.Close()

	// The ack FILE must still be "pending" — the JSONL event has no authority
	// over the file-backed durability anchor.
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "pending" {
		t.Errorf("bare self-ack (events.jsonl only) must not change ack file status; got %q (want %q)",
			ack["status"], "pending")
	}

	// A subsequent EmitTrip must still return the same token (still pending).
	tok2, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-bt1b"},
		Now:          now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second EmitTrip after self-ack: %v", err)
	}
	if tok2 != tok {
		t.Errorf("after self-ack, EmitTrip returned new token %q (want %q) — trip was incorrectly cleared", tok2, tok)
	}
}
