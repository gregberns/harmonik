package sentinel_test

// trip_ac4_test.go — unit tests for AC4: legitimate-halt clear path.
//
// Spec ref: flywheel-motion.md §2.2 clause 2.
// Bead ref: hk-jvul. Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/sentinel"
)

// TestRecordLegitimateHalt_MarksAcknowledgedWithHaltReason verifies that
// RecordLegitimateHalt marks the ack file acknowledged with
// ack_method="legitimate_halt" and persists the halt_reason.
func TestRecordLegitimateHalt_MarksAcknowledgedWithHaltReason(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4a"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	haltTime := now.Add(10 * time.Minute)
	cleared, err := sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: "ENOSPC: disk full on /dev/sda1",
		Now:        haltTime,
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt: %v", err)
	}
	if cleared != tok {
		t.Errorf("cleared token: got %q, want %q", cleared, tok)
	}

	// Ack file must be acknowledged with ack_method=legitimate_halt.
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("ack status: got %q, want %q", ack["status"], "acknowledged")
	}
	if ack["ack_method"] != "legitimate_halt" {
		t.Errorf("ack_method: got %q, want %q", ack["ack_method"], "legitimate_halt")
	}
	if ack["halt_reason"] != "ENOSPC: disk full on /dev/sda1" {
		t.Errorf("halt_reason: got %q, want %q", ack["halt_reason"], "ENOSPC: disk full on /dev/sda1")
	}
}

// TestRecordLegitimateHalt_AppendsDecisionAcknowledgedEvent verifies that
// RecordLegitimateHalt appends a decision_acknowledged event with
// ack_method="legitimate_halt", halt_reason, and readjudicate=true.
func TestRecordLegitimateHalt_AppendsDecisionAcknowledgedEvent(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4b"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	_, err = sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: "infra: gb-mbp SSH unreachable",
		Now:        now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt: %v", err)
	}

	acked := scanDecisionAcknowledged(t, dir)
	if len(acked) != 1 {
		t.Fatalf("expected 1 decision_acknowledged event; got %d", len(acked))
	}
	p := acked[0]
	if p["ack_token"] != tok {
		t.Errorf("ack_token: got %q, want %q", p["ack_token"], tok)
	}
	if p["ack_method"] != "legitimate_halt" {
		t.Errorf("ack_method: got %q, want %q", p["ack_method"], "legitimate_halt")
	}
	if p["halt_reason"] != "infra: gb-mbp SSH unreachable" {
		t.Errorf("halt_reason: got %q, want %q", p["halt_reason"], "infra: gb-mbp SSH unreachable")
	}
	if readjudicate, ok := p["readjudicate"].(bool); !ok || !readjudicate {
		t.Errorf("readjudicate: got %v, want true", p["readjudicate"])
	}
}

// TestRecordLegitimateHalt_ExplicitToken verifies that RecordLegitimateHalt
// uses an explicitly supplied AckToken rather than scanning for the pending one.
func TestRecordLegitimateHalt_ExplicitToken(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4c"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	cleared, err := sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		AckToken:   tok,
		HaltReason: "explicit token test",
		Now:        now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt with explicit token: %v", err)
	}
	if cleared != tok {
		t.Errorf("cleared: got %q, want %q", cleared, tok)
	}
	ack := readAckFile(t, dir, tok)
	if ack["status"] != "acknowledged" {
		t.Errorf("status: got %q, want acknowledged", ack["status"])
	}
}

// TestRecordLegitimateHalt_EmptyReasonReturnsError verifies that an empty
// halt_reason is rejected. An empty reason is indistinguishable from a bare
// self-ack, which spec §2.2 forbids.
func TestRecordLegitimateHalt_EmptyReasonReturnsError(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	for _, reason := range []string{"", "   ", "\t"} {
		_, err := sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
			ProjectDir: dir,
			HaltReason: reason,
			Now:        now,
		})
		if err == nil {
			t.Errorf("RecordLegitimateHalt(reason=%q): want error for empty reason; got nil", reason)
		}
		if !strings.Contains(err.Error(), "non-empty") {
			t.Errorf("error message should mention 'non-empty'; got: %v", err)
		}
	}
}

// TestRecordLegitimateHalt_NoPendingTripIsNoop verifies that RecordLegitimateHalt
// returns ("", nil) when no pending sentinel exception exists.
func TestRecordLegitimateHalt_NoPendingTripIsNoop(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: "ENOSPC",
		Now:        now,
	})
	if err != nil {
		t.Errorf("RecordLegitimateHalt with no pending trip: want nil error; got %v", err)
	}
	if tok != "" {
		t.Errorf("RecordLegitimateHalt with no pending trip: want empty token; got %q", tok)
	}
}

// TestRecordLegitimateHalt_AllowsNewTripAfterClear verifies that after a
// legitimate-halt clear, EmitTrip can write a fresh exception (no stale pending).
// This is the "next-pass re-adjudication" mechanism: the governor evaluates on the
// next tick and emits a new trip if movement is still low.
func TestRecordLegitimateHalt_AllowsNewTripAfterClear(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// First trip.
	tok1, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4d"},
		Now:          now,
	})
	if err != nil || tok1 == "" {
		t.Fatalf("first EmitTrip: tok=%q err=%v", tok1, err)
	}

	// Legitimate-halt clears the exception.
	_, err = sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: "ENOSPC: cleared for re-adjudication",
		Now:        now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt: %v", err)
	}

	// Second EmitTrip must write a FRESH exception (not return the old token).
	tok2, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4d"},
		Now:          now.Add(20 * time.Minute),
	})
	if err != nil || tok2 == "" {
		t.Fatalf("second EmitTrip: tok=%q err=%v", tok2, err)
	}
	if tok1 == tok2 {
		t.Errorf("expected fresh ack_token after legitimate-halt clear; got same token %q", tok1)
	}

	// New ack file must be pending.
	ack2 := readAckFile(t, dir, tok2)
	if ack2["status"] != "pending" {
		t.Errorf("new trip status: got %q, want pending", ack2["status"])
	}

	// Events: decision_required + decision_acknowledged(legitimate_halt) + decision_required.
	allTypes := scanEventTypes(t, dir)
	if len(allTypes) != 3 {
		t.Errorf("expected 3 events; got %d: %v", len(allTypes), allTypes)
	}

	// Verify event order.
	wantTypes := []string{"decision_required", "decision_acknowledged", "decision_required"}
	for i, want := range wantTypes {
		if i >= len(allTypes) {
			break
		}
		if allTypes[i] != want {
			t.Errorf("event[%d]: got %q, want %q", i, allTypes[i], want)
		}
	}

	// The second decision_required event must name the ready beads.
	allReq := scanDecisionRequired(t, dir)
	if len(allReq) != 2 {
		t.Errorf("expected 2 decision_required events; got %d", len(allReq))
	}
}

// TestIsTripAcknowledged_PendingReturnsFalse verifies that IsTripAcknowledged
// returns false for a pending (not-yet-acknowledged) trip.
func TestIsTripAcknowledged_PendingReturnsFalse(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4e"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	acked, err := sentinel.IsTripAcknowledged(dir, tok)
	if err != nil {
		t.Fatalf("IsTripAcknowledged: %v", err)
	}
	if acked {
		t.Error("IsTripAcknowledged: pending trip returned true; want false")
	}
}

// TestIsTripAcknowledged_AcknowledgedReturnsTrue verifies that IsTripAcknowledged
// returns true after a RecordLegitimateHalt or ClearTrip acknowledgment.
func TestIsTripAcknowledged_AcknowledgedReturnsTrue(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4f"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	_, err = sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: "ENOSPC",
		Now:        now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt: %v", err)
	}

	acked, err := sentinel.IsTripAcknowledged(dir, tok)
	if err != nil {
		t.Fatalf("IsTripAcknowledged after RecordLegitimateHalt: %v", err)
	}
	if !acked {
		t.Error("IsTripAcknowledged: acknowledged trip returned false; want true")
	}
}

// TestIsTripAcknowledged_AbsentReturnsFalse verifies that IsTripAcknowledged
// returns false (not an error) when the ack file does not exist.
func TestIsTripAcknowledged_AbsentReturnsFalse(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)

	acked, err := sentinel.IsTripAcknowledged(dir, "nonexistent-token")
	if err != nil {
		t.Fatalf("IsTripAcknowledged on absent file: want nil error; got %v", err)
	}
	if acked {
		t.Error("IsTripAcknowledged on absent file: want false; got true")
	}
}

// TestRecordLegitimateHalt_HaltReasonStoredVerbatim verifies that the halt
// reason is stored verbatim (not truncated, escaped, or transformed) in both
// the ack file and the events.jsonl payload.
func TestRecordLegitimateHalt_HaltReasonStoredVerbatim(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	const longReason = "ENOSPC: /dev/sda1 (95% used); root volume has only 512 MiB free after a runaway agent-mail log; operator will reap the log and restart before next adjudication pass"

	tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
		ProjectDir:   dir,
		ReadyBeadIDs: []string{"hk-ac4g"},
		Now:          now,
	})
	if err != nil || tok == "" {
		t.Fatalf("EmitTrip: tok=%q err=%v", tok, err)
	}

	_, err = sentinel.RecordLegitimateHalt(context.Background(), sentinel.LegitimateHaltInput{
		ProjectDir: dir,
		HaltReason: longReason,
		Now:        now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("RecordLegitimateHalt: %v", err)
	}

	// Check ack file.
	ack := readAckFile(t, dir, tok)
	if ack["halt_reason"] != longReason {
		t.Errorf("ack file halt_reason: got %q\nwant %q", ack["halt_reason"], longReason)
	}

	// Check events.jsonl payload.
	acked := scanDecisionAcknowledged(t, dir)
	if len(acked) != 1 {
		t.Fatalf("expected 1 decision_acknowledged event; got %d", len(acked))
	}
	// The halt_reason field is stored as JSON; unmarshal to compare.
	raw, _ := json.Marshal(acked[0]["halt_reason"])
	var gotReason string
	_ = json.Unmarshal(raw, &gotReason)
	if gotReason != longReason {
		t.Errorf("event halt_reason: got %q\nwant %q", gotReason, longReason)
	}
}
