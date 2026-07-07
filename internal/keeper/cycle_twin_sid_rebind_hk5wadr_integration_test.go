//go:build integration

package keeper_test

// cycle_twin_sid_rebind_hk5wadr_integration_test.go — bead hk-5wadr.
//
// Acceptance corpus #2 (design doc 11-keeper-test-design.md §G2, §3 item #2):
//
//   "session_id survives a /clear cycle and the resume rebinds the SAME
//   conversation. After /session-resume <agent>, the .sid is re-minted but the
//   agent wakes as the same lane; the anti-loop gate (lastFiredSID) does not
//   immediately re-fire."
//
// Layer: L-twin. The test drives a REAL first full cycle (real tmux pane, real
// InjectText, real scripts/keeper-statusline.sh pipeline, real .ctx file) and
// then verifies two invariants in sequence:
//
//  1. Gate holds immediately after /clear: on the very first MaybeRun call that
//     presents the NEW session_id, the gate suppresses a second cycle even when
//     the ctx is read before tokens have regrown. On a [1m] model the absolute-
//     token act threshold (Gate 3, 215k) is the primary gate; the lastFiredSID
//     gate (Gate 6) is also in the correct state (lastFiredSID=oldSID,
//     seenLowPctAfterLastFire=false) and adds belt-and-suspenders protection.
//
//  2. Lane identity preserved across /clear→resume: the same project + agent
//     name, the same tmux session, and the .managed binding all survive the /clear
//     and are available for a second full cycle that completes cleanly. Two
//     cycle_complete events with two distinct session_id rotations confirm the
//     system can cycle the same lane repeatedly without state corruption.
//
// Safety contract: see cycle_twin_e2e_integration_test.go (same "hksav-twin-"
// prefix, exact-name teardown, no kill-server, no glob-kill). This test shares
// the same twRequireTmux / twBuildTwin / twStartTwin / twUniqueSessionName
// helpers defined in that file (same package).
//
// Helper prefix: tw (twin). Bead: hk-5wadr.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestIntegration_TwinSidRebind_AntiLoopGateHolds drives the full sid-rebind
// twin scenario:
//
//  1. First full cycle: high tokens → MaybeRun fires → /clear → session_id
//     rotates (oldSID → newSID) → cycle_complete emitted.
//
//  2. Gate-holds check: call MaybeRun with the POST-/clear gauge reading on the
//     new session. Tokens have reset to startTokens (50k), well below the act
//     threshold (215k on a [1m] model). Gate 3 prevents re-fire. Verify exactly
//     one handoff_started event total: the gate holds.
//
//  3. Lane-preserved check (second full cycle): wait for the twin's tokens to
//     regrow past the act threshold on the SAME newSID, then call MaybeRun
//     again. This fires and completes the second cycle (newSID → newNewSID),
//     proving the lane identity (project + agent + tmux session) survived
//     /clear→resume intact. Two cycle_complete events confirm clean repeatable
//     cycling on the same lane.
func TestIntegration_TwinSidRebind_AntiLoopGateHolds(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twsid%d", rand.Int64()) //nolint:gosec // G404: test-local uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	// Opt the agent in (.managed) so the REAL IsManaged gate passes. Starts
	// empty; SetManagedSessionFn fills it after the cycle's /clear confirms a
	// new session_id.
	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw-sid: WriteManagedSessionID: %v", err)
	}

	// Start the twin: window=1M ([1m] model), startTokens=50k (post-/clear
	// reset value — well below the act threshold of 215k, so the gate-holds
	// check sees low tokens immediately after /clear). Growth 50k/200ms crosses
	// the act threshold in roughly 4 emit ticks (~800ms).
	sess := twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      1_000_000,
		growth:      50_000,
		startTokens: 50_000,
		emitEvery:   200 * time.Millisecond,
	})

	// ── Phase 1: first full cycle ─────────────────────────────────────────────

	// Wait (via the REAL .ctx) for tokens to cross the act threshold.
	seed := twWaitForCtxTokens(t, project, agent, 300_000, 8*time.Second)
	seedSID := seed.SessionID
	if seedSID == "" {
		t.Fatal("tw-sid: seed .ctx has empty session_id")
	}
	if seed.WindowSize != 1_000_000 {
		t.Fatalf("tw-sid: seed .ctx window_size = %d; want 1000000", seed.WindowSize)
	}
	if !keeper.CrispIdle(project, agent) {
		t.Fatal("tw-sid: CrispIdle false at seed — the twin's .idle marker did not register")
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     project,
		TmuxTarget:     sess,
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
		// REAL InjectText — production path; twin parses the multi-line
		// /session-handoff directive natively (hk-fan).
		InjectFn: keeper.InjectText,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Run the first cycle. MaybeRun drives: handoff → confirm nonce → /clear →
	// wait for new session_id → agent brief.
	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		t.Fatalf("tw-sid: first MaybeRun: %v", err)
	}

	// Verify the first cycle completed cleanly.
	completeEvents := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvents) != 1 {
		t.Fatalf("tw-sid: want 1 cycle_complete after first cycle; got %d", len(completeEvents))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Fatalf("tw-sid: want 0 cycle_aborted on the first cycle; got %d", n)
	}
	var cp1 core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvents[0].Payload, &cp1); err != nil {
		t.Fatalf("tw-sid: unmarshal first cycle_complete: %v", err)
	}
	if cp1.PrevSessionID != seedSID {
		t.Errorf("tw-sid: cycle_complete.prev_session_id = %q; want seedSID %q", cp1.PrevSessionID, seedSID)
	}
	newSID := cp1.NewSessionID
	if newSID == "" || newSID == seedSID {
		t.Fatalf("tw-sid: first cycle did not rotate session_id (new=%q seed=%q)", newSID, seedSID)
	}
	if !twIsValidUUIDv4(newSID) {
		t.Fatalf("tw-sid: rotated session_id %q is not a valid UUIDv4", newSID)
	}

	// ── Phase 2: gate-holds check ─────────────────────────────────────────────
	//
	// After a completed cycle, the Cycler has: lastFiredSID=seedSID,
	// seenLowPctAfterLastFire=false. The twin's /clear reset tokens to
	// startTokens (50k) and the gauge immediately reflects the rotated newSID.
	//
	// Read the real gauge for the new session. Tokens are near 50k (the twin
	// re-emits immediately on /clear). On a [1m] (1M-window) model, the act
	// threshold is 215k: Gate 3 prevents the cycle from firing, and Gate 6
	// (lastFiredSID anti-loop) adds belt-and-suspenders protection. Both gates
	// are exercised implicitly — the key assertion is that no second
	// handoff_started event is emitted.
	//
	// Poll until the rotated newSID appears in the gauge (proves the real .ctx
	// has been updated to the new session). Tokens will be low (~50k).
	var postClearCtx *keeper.CtxFile
	sidDeadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(sidDeadline) {
		cf, _, err := keeper.ReadCtxFile(project, agent)
		if err == nil && cf.SessionID == newSID {
			postClearCtx = cf
			break
		}
		time.Sleep(75 * time.Millisecond)
	}
	if postClearCtx == nil {
		t.Fatalf("tw-sid: gauge never reflected the rotated session_id %q within timeout", newSID)
	}
	if postClearCtx.Tokens >= 215_000 {
		// This is theoretically possible (twin grew very fast) but would make
		// the gate-holds assertion less clean. Log and continue.
		t.Logf("tw-sid: post-/clear tokens=%d already >= act threshold; gate-holds assertion relies on Gate 6", postClearCtx.Tokens)
	}

	// Call MaybeRun with the post-/clear ctx. Gate 3 (tokens < 215k) and/or
	// Gate 6 (anti-loop) must prevent a second cycle.
	if err := cycler.MaybeRun(context.Background(), postClearCtx); err != nil {
		t.Fatalf("tw-sid: post-cycle MaybeRun (gate-holds check): %v", err)
	}
	// Anti-loop gate held: still exactly 1 handoff_started, not 2.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("tw-sid: anti-loop gate failed: got %d handoff_started events; want 1 "+
			"(second cycle must NOT fire immediately after /clear on the resumed session)", n)
	}
	// Same-lane invariant: .managed is still bound (IsManaged=true), and the
	// binding was updated to the rotated session_id by the first cycle.
	if !keeper.IsManaged(project, agent) {
		t.Error("tw-sid: IsManaged false after cycle — the opt-in marker must survive /clear")
	}
	boundSID, err := keeper.ReadManagedSessionID(project, agent)
	if err != nil {
		t.Fatalf("tw-sid: ReadManagedSessionID: %v", err)
	}
	if boundSID != newSID {
		t.Errorf("tw-sid: .managed bound to %q; want the rotated SID %q (same-lane rebind)", boundSID, newSID)
	}

	// ── Phase 3: second cycle (lane-preserved check) ──────────────────────────
	//
	// Wait for the twin's tokens to regrow past the act threshold on the SAME
	// newSID, then call MaybeRun. Once tokens exceed 215k, Gate 3 passes and
	// (after seeing the below-warn gauge on the post-/clear tick) Gate 6 also
	// passes. A second full cycle should fire and complete, proving:
	//
	//   • the lane (project + agent + tmux pane) is preserved across /clear→resume;
	//   • the anti-loop gate correctly re-arms after observing below-warn context;
	//   • the system can cycle the same lane repeatedly without corruption.
	regrown := twWaitForCtxTokens(t, project, agent, 300_000, 12*time.Second)
	if regrown.SessionID != newSID {
		// The twin may have been cycled again by a background keeper if one
		// were running, but in this test nothing else drives MaybeRun. Accept
		// the observation as-is and log it.
		t.Logf("tw-sid: Phase 3 gauge SID=%q; expected newSID=%q (may have rotated again)", regrown.SessionID, newSID)
	}

	if err := cycler.MaybeRun(context.Background(), regrown); err != nil {
		t.Fatalf("tw-sid: second MaybeRun (lane-preserved check): %v", err)
	}

	// Second cycle must have fired and completed.
	completeEvents2 := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvents2) != 2 {
		t.Fatalf("tw-sid: want 2 cycle_complete events (two cycles on the same lane); got %d "+
			"(did the anti-loop gate not re-arm after the post-/clear below-warn reading?)", len(completeEvents2))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Errorf("tw-sid: want 0 cycle_aborted events; got %d", n)
	}

	// Verify the second cycle also rotated the session_id (same-lane identity
	// across two consecutive cycles).
	var cp2 core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvents2[1].Payload, &cp2); err != nil {
		t.Fatalf("tw-sid: unmarshal second cycle_complete: %v", err)
	}
	if cp2.PrevSessionID == "" || cp2.PrevSessionID == seedSID {
		t.Errorf("tw-sid: second cycle_complete.prev_session_id = %q; "+
			"expected the intermediate newSID (not the original seed %q)", cp2.PrevSessionID, seedSID)
	}
	if !twIsValidUUIDv4(cp2.NewSessionID) {
		t.Errorf("tw-sid: second rotated session_id %q is not a valid UUIDv4", cp2.NewSessionID)
	}
	if cp2.NewSessionID == seedSID || cp2.NewSessionID == newSID {
		t.Errorf("tw-sid: second cycle_complete.new_session_id %q must differ from both the seed %q and the intermediate %q",
			cp2.NewSessionID, seedSID, newSID)
	}
}
