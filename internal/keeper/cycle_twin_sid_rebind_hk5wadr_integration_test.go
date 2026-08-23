//go:build integration

package keeper_test

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

	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw-sid: WriteManagedSessionID: %v", err)
	}

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
	cfgOverrides := testCycleOverrides{Inject:

	// REAL InjectText — production path; twin parses the multi-line
	// /session-handoff directive natively (hk-fan).
	keeper.InjectText}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     project,
		TmuxTarget:     sess,
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			return keeper.WriteManagedSessionID(project, agent, sid)
		}}
	})

	stopWatch := make(chan struct{})
	resetCh := make(chan int64, 1)
	go twWatchForReset(project, agent, seedSID, stopWatch, resetCh)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		close(stopWatch)
		t.Fatalf("tw-sid: first MaybeRun: %v", err)
	}
	close(stopWatch)
	minPostClearTokens := <-resetCh

	completeEvents := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvents) != 1 {
		t.Fatalf("tw-sid: want 1 cycle_complete after first cycle; got %d", len(completeEvents))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Fatalf("tw-sid: want 0 cycle_parked on the first cycle; got %d", n)
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

	if minPostClearTokens < 0 {
		t.Fatalf("tw-sid: watcher never observed a reading on the rotated session_id %q", newSID)
	}
	postClearCtx := &keeper.CtxFile{
		Tokens:     minPostClearTokens,
		WindowSize: seed.WindowSize,
		Pct:        float64(minPostClearTokens) / float64(seed.WindowSize) * 100.0,
		SessionID:  newSID,
	}
	if postClearCtx.Tokens >= 215_000 {
		t.Logf("tw-sid: post-/clear tokens=%d already >= act threshold; gate-holds assertion relies on Gate 6", postClearCtx.Tokens)
	}

	if err := cycler.MaybeRun(context.Background(), postClearCtx); err != nil {
		t.Fatalf("tw-sid: post-cycle MaybeRun (gate-holds check): %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("tw-sid: anti-loop gate failed: got %d handoff_started events; want 1 "+
			"(second cycle must NOT fire immediately after /clear on the resumed session)", n)
	}
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

	regrown := twWaitForCtxTokens(t, project, agent, 300_000, 12*time.Second)
	if regrown.SessionID != newSID {
		t.Logf("tw-sid: Phase 3 gauge SID=%q; expected newSID=%q (may have rotated again)", regrown.SessionID, newSID)
	}

	if err := cycler.MaybeRun(context.Background(), regrown); err != nil {
		t.Fatalf("tw-sid: second MaybeRun (lane-preserved check): %v", err)
	}

	completeEvents2 := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvents2) != 2 {
		t.Fatalf("tw-sid: want 2 cycle_complete events (two cycles on the same lane); got %d "+
			"(did the anti-loop gate not re-arm after the post-/clear below-warn reading?)", len(completeEvents2))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Errorf("tw-sid: want 0 cycle_parked events across the two cycles; got %d", n)
	}

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
