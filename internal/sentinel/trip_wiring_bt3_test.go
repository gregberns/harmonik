package sentinel_test

// trip_wiring_bt3_test.go — BT3 integration test for governor→trip wiring.
//
// This exercises the workloop's ACT-mode tick contract (flywheel-motion.md §3.5,
// FW3 hk-4toh): each tick the caller runs Evaluate, then drives trip emission off
// the resulting ActivationLevel:
//
//   - ActivationActive  → EmitTrip   (sustained low movement + opportunity)
//   - ActivationDormant → ClearTrip  (real movement resumed; clears a pending trip)
//   - ActivationWatching/Halt → no trip emission
//
// The scenario asserts the EMISSION COUNTS across a multi-tick run, end-to-end
// through the real EmitTrip/ClearTrip durability machinery (ack-state files +
// events.jsonl), not just the governor in isolation:
//
//   - a governor in sustained-low + ready state emits EXACTLY ONE trip (idempotent
//     across repeated ACTIVE ticks — not zero, not one-per-tick);
//   - a dormant governor emits NONE;
//   - a governor that then sees movement (DORMANT with a pending trip) clears it.
//
// Bead: hk-vdk4 (flywheel-BT3). Epic: hk-0oca (codename:flywheel).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// tickCounts records how many times each trip operation fired across a tick loop.
type tickCounts struct {
	emits  int
	clears int
}

// runTick mirrors the workloop's ACT-mode dispatch of trip operations for one
// governor signal. It returns the (possibly unchanged) in-flight ack token so the
// caller can thread it across ticks, exactly as the workloop holds the pending
// sentinel ack between cycles.
func runTick(
	t *testing.T,
	dir string,
	sig sentinel.GovernorSignal,
	pendingTok string,
	now time.Time,
	counts *tickCounts,
) string {
	t.Helper()
	switch sig.Level {
	case sentinel.ActivationActive:
		tok, err := sentinel.EmitTrip(context.Background(), sentinel.TripInput{
			ProjectDir:   dir,
			ReadyBeadIDs: []string{"hk-bt3wire"},
			Now:          now,
		})
		if err != nil {
			t.Fatalf("EmitTrip: %v", err)
		}
		// Idempotent emit: a brand-new token means a fresh decision_required was
		// written; an unchanged token means EmitTrip de-duplicated.
		if tok != pendingTok {
			counts.emits++
		}
		return tok
	case sentinel.ActivationDormant:
		if pendingTok != "" {
			if err := sentinel.ClearTrip(context.Background(), dir, pendingTok, now); err != nil {
				t.Fatalf("ClearTrip: %v", err)
			}
			counts.clears++
			return "" // cleared; no longer in flight
		}
		return pendingTok
	default:
		// Watching / Halt: the workloop emits no trip.
		return pendingTok
	}
}

// TestBT3_TripWiring_SustainedLowReady_EmitsExactlyOnce drives the full
// governor→trip wiring over several ticks and asserts the emission counts.
func TestBT3_TripWiring_SustainedLowReady_EmitsExactlyOnce(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// SustainedWindows=2: the first low tick stays WATCHING; the second trips.
	// No DaemonStartedAt → warmup gate disabled (suitable for tests, per the
	// GovernorState doc comment). Empty events.jsonl → zero movement = low.
	cfg := sentinel.Config{
		Window:           30 * time.Minute,
		WarmupWindow:     0,
		SustainedWindows: 2,
	}
	readyInput := sentinel.GovernorInput{
		ProjectDir:    dir,
		HasReadyBeads: true, // opportunity gate open
	}

	state := &sentinel.GovernorState{}
	var counts tickCounts
	var tok string

	// --- Tick 1: first low window → WATCHING → no emit. ---
	in1 := readyInput
	in1.Now = base
	sig1 := sentinel.Evaluate(context.Background(), state, in1, cfg)
	if sig1.Level != sentinel.ActivationWatching {
		t.Fatalf("tick1: want WATCHING (sustained gate not yet met); got %s", sig1.Level)
	}
	tok = runTick(t, dir, sig1, tok, base, &counts)

	// --- Ticks 2..5: sustained low + ready → ACTIVE → trip, but idempotent. ---
	for i := 2; i <= 5; i++ {
		now := base.Add(time.Duration(i) * time.Minute)
		in := readyInput
		in.Now = now
		sig := sentinel.Evaluate(context.Background(), state, in, cfg)
		if sig.Level != sentinel.ActivationActive {
			t.Fatalf("tick%d: want ACTIVE (sustained low + ready); got %s (consecutive=%d)",
				i, sig.Level, sig.ConsecutiveLowWindows)
		}
		tok = runTick(t, dir, sig, tok, now, &counts)
	}

	// EXACTLY ONE emit across the four ACTIVE ticks — not zero, not four.
	if counts.emits != 1 {
		t.Errorf("sustained-low+ready governor must EmitTrip exactly once; got %d", counts.emits)
	}
	if tok == "" {
		t.Fatal("expected an in-flight ack token after ACTIVE ticks")
	}
	// And on disk: exactly one decision_required event (no warn-nag).
	if drs := scanDecisionRequired(t, dir); len(drs) != 1 {
		t.Errorf("expected exactly 1 decision_required event on disk; got %d", len(drs))
	}

	// --- Movement resumes: DORMANT with a pending trip → ClearTrip exactly once. ---
	// Drive DORMANT deterministically with a one-event weight table that scores at
	// the high threshold, independent of git/events timing.
	movedNow := base.Add(10 * time.Minute)
	writeBeadClosedAt(t, dir, movedNow.Add(-1*time.Minute))
	dormantCfg := cfg
	sigDormant := sentinel.Evaluate(context.Background(), state, sentinel.GovernorInput{
		ProjectDir:    dir,
		Now:           movedNow,
		HasReadyBeads: true,
	}, dormantCfg)
	if sigDormant.Level != sentinel.ActivationDormant {
		t.Fatalf("after movement: want DORMANT; got %s (score=%d)",
			sigDormant.Level, sigDormant.Sample.MovementScore)
	}
	tok = runTick(t, dir, sigDormant, tok, movedNow, &counts)

	if counts.clears != 1 {
		t.Errorf("governor seeing movement (DORMANT + pending) must ClearTrip exactly once; got %d", counts.clears)
	}
	if tok != "" {
		t.Errorf("ack token should be cleared (empty) after ClearTrip; got %q", tok)
	}
}

// TestBT3_TripWiring_Dormant_EmitsNone verifies a governor that is DORMANT from
// the start (real movement in-window) with NO pending trip emits neither a trip
// nor a clear — the wiring is silent when there is nothing to adjudicate.
func TestBT3_TripWiring_Dormant_EmitsNone(t *testing.T) {
	t.Parallel()
	dir := makeProjectDir(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// One bead_closed in-window scores DefaultHighWeight → DORMANT.
	writeBeadClosedAt(t, dir, now.Add(-1*time.Minute))

	state := &sentinel.GovernorState{}
	cfg := sentinel.Config{Window: 30 * time.Minute, WarmupWindow: 0, SustainedWindows: 2}
	sig := sentinel.Evaluate(context.Background(), state, sentinel.GovernorInput{
		ProjectDir:    dir,
		Now:           now,
		HasReadyBeads: true,
	}, cfg)
	if sig.Level != sentinel.ActivationDormant {
		t.Fatalf("want DORMANT (movement in-window); got %s (score=%d)", sig.Level, sig.Sample.MovementScore)
	}

	var counts tickCounts
	tok := runTick(t, dir, sig, "", now, &counts) // no pending trip

	if counts.emits != 0 {
		t.Errorf("dormant governor must emit NO trip; got %d emits", counts.emits)
	}
	if counts.clears != 0 {
		t.Errorf("dormant governor with no pending trip must NOT clear; got %d clears", counts.clears)
	}
	if tok != "" {
		t.Errorf("no ack token should exist; got %q", tok)
	}
	if drs := scanDecisionRequired(t, dir); len(drs) != 0 {
		t.Errorf("dormant governor must write no decision_required event; got %d", len(drs))
	}
}

// writeBeadClosedAt appends one bead_closed event at ts to the project's
// events.jsonl. A single bead_closed scores DefaultHighWeight under the default
// weight table, which is >= the default high threshold → DORMANT.
func writeBeadClosedAt(t *testing.T, dir string, ts time.Time) {
	t.Helper()
	eventsPath := filepath.Join(dir, ".harmonik", "events", "events.jsonl")
	writeEvent(t, eventsPath, core.EventTypeBeadClosed, ts, []byte(`{}`))
}
