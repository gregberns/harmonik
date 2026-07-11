package keeper_test

// cycle_scenario_reactive_wave2_test.go — a SECOND wave of offline reactive
// scenario tests for the session keeper, built on the SAME reactive harness as
// cycle_scenario_reactive_test.go (type reactiveSession in
// cycle_reactive_harness_test.go). These prove END-TO-END, through a session
// fake that MUTATES gauge + handoff state in reaction to the injected command,
// what the cycle_test.go / precompact_test.go unit tests fake with call-count
// gauges:
//
//   1. ClearSettleUnconfirmed   — /clear IS injected but no new SID ever
//      appears (flipOnClear=false). The non-fatal clear_unconfirmed path:
//      clear_unconfirmed emitted, managed binding cleared (empty) so the
//      .sid channel can rebind it, and complete carries NO bogus new SID.
//   2. ForcedClearAboveHardThreshold — context above the FORCE threshold with
//      CrispIdle FALSE: cycle fires anyway (CrispIdle bypassed), Escape lands
//      BEFORE /session-handoff, and /clear is STILL gated on the nonce.
//   3. AntiLoopReArm — full reactive cycle to complete (S1→S2), suppressed on
//      a second high-context tick on S2, re-armed only after a below-warn
//      reading on S2. Multi-tick, causally driven.
//   4. PreCompactBackstop — the RunForPrecompact path runs the cycle while
//      SKIPPING the CrispIdle/act gates and clears the .precompact marker.
//
// NOTE — scenario "CrashRecoveryReplaysResume" (phase=cleared → /session-resume
// on recovery) is intentionally OMITTED: it would exactly duplicate the
// existing unit test TestCycler_BootRecovery_PhaseCleared (cycle_test.go ~707),
// which already asserts the resume injection, journal→complete, and
// cycle_recovered{phase_at_crash:"cleared"}. RecoverFromCrash has no reactive
// seam (it reads the journal and injects once; it polls neither gauge nor
// handoff), so the reactive harness adds no causal/end-to-end value there.
//
// Fast offline unit tests — NO build tag. All harness symbols are reused from
// cycle_reactive_harness_test.go; this file declares NONE of them.

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestKeeperCycle_ClearSettleUnconfirmed drives the cycle through the reactive
// harness with flipOnClear=false: the handoff IS confirmed (writeNonce=true) so
// /clear DOES get injected, but the /clear reaction never rotates the gauge SID,
// so waitForNewSessionID times out (ClearSettle) and the cycle takes the
// non-fatal clear-unconfirmed path.
//
// This proves END-TO-END what the unit tests only fake on a gauge call-count:
//   - /clear WAS injected (the handoff was confirmed, so the safety gate opened).
//   - session_keeper_clear_unconfirmed is emitted (no new SID observed).
//   - the .managed binding is cleared to "" so the .sid channel can rebind it
//     on the next session-start signal (Refs: hk-igt, hk-uxu).
//   - cycle_complete still fires but carries an EMPTY new_session_id — the
//     cycle does NOT fabricate a bogus new SID.
func TestKeeperCycle_ClearSettleUnconfirmed(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-clearsettle-agent"
		cycleID = "cyc-reactive-clearsettle-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	// Seed the binding non-empty so an assertion of "" is meaningful (the cycle
	// must actively clear it, not merely leave it untouched).
	managedBinding := "stale-binding-sentinel"

	// writeNonce=true → handoff confirms → /clear IS injected.
	// flipOnClear=false → /clear NEVER rotates the SID → ClearSettle times out.
	rs := newReactiveSession(s1, s2, true /*writeNonce*/, false /*flipOnClear*/)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		500*time.Millisecond, // handoffTimeout (handoff confirms quickly)
		30*time.Millisecond,  // clearSettle — SHRUNK; no SID ever appears so it times out fast
	)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) /clear WAS injected — the handoff confirmed, so the safety gate opened.
	if !rs.sawClear() {
		t.Fatal("/clear was not injected; expected it after handoff confirmation (writeNonce=true)")
	}

	// (b) session_keeper_clear_unconfirmed emitted exactly once (no new SID seen).
	unconfirmed := em.EventsOfType(core.EventTypeSessionKeeperClearUnconfirmed)
	if len(unconfirmed) != 1 {
		t.Fatalf("want 1 clear_unconfirmed; got %d", len(unconfirmed))
	}
	var up core.SessionKeeperClearUnconfirmedPayload
	if err := json.Unmarshal(unconfirmed[0].Payload, &up); err != nil {
		t.Fatalf("unmarshal clear_unconfirmed: %v", err)
	}
	if up.SessionID != s1 {
		t.Errorf("clear_unconfirmed.session_id = %q; want %q (S1 — never rotated)", up.SessionID, s1)
	}

	// (c) the .managed binding is CLEARED ("") so the .sid channel can rebind it.
	if managedBinding != "" {
		t.Errorf("managed binding = %q; want \"\" (cleared for .sid rebind; not a bogus new SID)", managedBinding)
	}

	// (d) cycle_complete fired but carries an EMPTY new_session_id — no bogus SID.
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != s1 {
		t.Errorf("cycle_complete.prev_session_id = %q; want %q (S1)", cp.PrevSessionID, s1)
	}
	if cp.NewSessionID != "" {
		t.Errorf("cycle_complete.new_session_id = %q; want \"\" (no SID confirmed — must NOT fabricate %q)", cp.NewSessionID, s2)
	}

	// (e) the live gauge SID never rotated (flipOnClear=false): stays S1.
	if rs.liveSID() != s1 {
		t.Errorf("live gauge SID = %q; want %q (S1 — flipOnClear=false)", rs.liveSID(), s1)
	}
}

// TestKeeperCycle_ForcedClearAboveHardThreshold drives the cycle through the
// reactive harness with CrispIdle FALSE and context at/above the hard FORCE
// threshold (Tokens >= ForceActAbsTokens). It proves END-TO-END:
//   - the cycle fires ANYWAY (the CrispIdle gate is bypassed above the force
//     threshold; Refs: hk-0uu).
//   - Escape is sent BEFORE the /session-handoff inject (a single ordered
//     witness captures both, Refs: hk-qoz).
//   - /clear is STILL gated on the nonce: with writeNonce=true the handoff
//     confirms and the cycle reaches /clear and completes; the nonce gate is
//     NOT skipped on the force path. The causal SID flip (S1→S2) is driven by
//     /clear through the reactive harness.
//
// This adds value over the call-count unit test TestCycler_ForcedClear_EscapeInjected
// by (a) driving the gauge flip REACTIVELY through /clear and (b) asserting the
// nonce gate is honored on the force path (the unit test does not check the
// nonce-confirmed→/clear linkage).
func TestKeeperCycle_ForcedClearAboveHardThreshold(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-force-agent"
		cycleID = "cyc-reactive-force-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	// writeNonce=true (so /clear is reachable — proving the nonce gate is NOT
	// skipped on the force path) and flipOnClear=true (so /clear causally
	// rotates S1→S2).
	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)

	// Single ordered witness recording "escape" and "inject:<prefix>" in call order.
	var mu sync.Mutex
	var order []string
	escapeFn := func(_ context.Context, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "escape")
		return nil
	}
	// Wrap the reactive inject so the witness records ordering while the harness
	// still mutates gauge/handoff state (the reaction is what makes /clear causal).
	injectFn := func(ctx context.Context, target, text string) error {
		mu.Lock()
		prefix := text
		if len(prefix) > 20 {
			prefix = prefix[:20]
		}
		order = append(order, "inject:"+prefix)
		mu.Unlock()
		return rs.inject(ctx, target, text)
	}

	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		ForceActPct:    95.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    300 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       rs.readHandoff,
		TruncateHandoffFn: rs.truncate,
		InjectFn:          injectFn,
		ReadGaugeFn:       rs.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return false }, // perpetually busy → force path
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sid string) error {
			managedBinding = sid
			return nil
		},
		SendEscapeFn: escapeFn,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Tokens at/above the default ForceActAbsTokens (240_000) with CrispIdle=false.
	cf := &keeper.CtxFile{Pct: 97.0, Tokens: 390_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) The cycle fired despite CrispIdle=false (full happy-path phases).
	wantPhases := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	got := jc.snapshot()
	if len(got) != len(wantPhases) {
		t.Fatalf("journal phases = %v; want %v (cycle must fire despite CrispIdle=false)", got, wantPhases)
	}
	for i, p := range wantPhases {
		if got[i] != p {
			t.Errorf("phase[%d] = %q; want %q", i, got[i], p)
		}
	}

	// (b) Escape lands BEFORE the /session-handoff inject (ordered witness).
	mu.Lock()
	snap := make([]string, len(order))
	copy(snap, order)
	mu.Unlock()
	escapeIdx, handoffIdx := -1, -1
	for i, e := range snap {
		if e == "escape" && escapeIdx == -1 {
			escapeIdx = i
		}
		if containsSubstr(e, "inject:/session-handoff") && handoffIdx == -1 {
			handoffIdx = i
		}
	}
	if escapeIdx == -1 {
		t.Errorf("SendEscapeFn was never called; order = %v", snap)
	}
	if handoffIdx == -1 {
		t.Fatalf("/session-handoff was never injected; order = %v", snap)
	}
	if escapeIdx >= handoffIdx {
		t.Errorf("Escape (idx=%d) must precede /session-handoff inject (idx=%d); order=%v", escapeIdx, handoffIdx, snap)
	}

	// (c) /clear was STILL gated on the nonce — it ran only because the handoff
	// confirmed. The causal witness proves the SID flip was caused by /clear, so
	// /clear (a) actually ran and (b) is what rotated S1→S2 on the force path.
	if !rs.sawClear() {
		t.Fatal("/clear was never injected on the force path — nonce gate may have been skipped")
	}
	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip caused by %q; want exactly \"/clear\" (force path must still gate /clear on the nonce)", cause)
	}
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared before /clear was injected — nonce gate / causality violated on force path")
	}

	// (d) cycle_complete carries S1→S2 and the binding updated to S2.
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != s1 || cp.NewSessionID != s2 {
		t.Errorf("cycle_complete = {prev:%q new:%q}; want {prev:%q new:%q}", cp.PrevSessionID, cp.NewSessionID, s1, s2)
	}
	if managedBinding != s2 {
		t.Errorf("managed binding = %q; want %q (S2)", managedBinding, s2)
	}
}

// TestKeeperCycle_AntiLoopReArm drives a FULL reactive cycle to completion
// (S1→S2 caused by /clear) and then proves the suppress/re-arm contract across
// multiple ticks on the new session S2:
//   - tick again on S2 with context still ABOVE warn → NO second cycle fires
//     (anti-loop suppression: cycle_complete count stays 1).
//   - drop the gauge BELOW warn on S2 (re-arm observation), then raise it again
//     above act → a second cycle now fires (cycle_complete count becomes 2).
//
// This genuinely needs the reactive multi-tick harness: the first cycle's
// S1→S2 flip is CAUSED by /clear (not faked), and the suppression/re-arm is
// exercised against the real post-clear session identity.
func TestKeeperCycle_AntiLoopReArm(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-antiloop-agent"
		cycleID = "cyc-reactive-antiloop-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managedBinding,
		500*time.Millisecond, // handoffTimeout
		300*time.Millisecond, // clearSettle
	)

	ctx := context.Background()

	// Tick 1: high context on S1 → full cycle fires; /clear rotates S1→S2.
	cf1 := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(ctx, cf1); err != nil {
		t.Fatalf("tick 1 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Fatalf("after tick 1: cycle_complete = %d; want 1", n)
	}
	if rs.flipCause() != "/clear" {
		t.Fatalf("tick 1: SID flip caused by %q; want \"/clear\"", rs.flipCause())
	}

	// Tick 2: NEW session S2 but context still ABOVE warn → suppressed (re-arm
	// requires a below-warn observation on S2 first). No second cycle.
	cf2 := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s2}
	if err := cycler.MaybeRun(ctx, cf2); err != nil {
		t.Fatalf("tick 2 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Errorf("after tick 2 (S2 high, not yet re-armed): cycle_complete = %d; want 1 (suppressed)", n)
	}

	// Tick 3: drop BELOW warn on S2 — this is the re-arm observation. The cycle
	// must NOT fire here (below act threshold), but the cycler is now re-armed.
	cf3 := &keeper.CtxFile{Pct: 40.0, Tokens: 60_000, WindowSize: 1_000_000, SessionID: s2}
	if err := cycler.MaybeRun(ctx, cf3); err != nil {
		t.Fatalf("tick 3 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Errorf("after tick 3 (S2 below warn): cycle_complete = %d; want 1 (re-arm observation, no fire)", n)
	}

	// Tick 4: raise context above act on S2 → a SECOND cycle now fires (re-armed).
	cf4 := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s2}
	if err := cycler.MaybeRun(ctx, cf4); err != nil {
		t.Fatalf("tick 4 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 2 {
		t.Errorf("after tick 4 (S2 re-armed, high): cycle_complete = %d; want 2 (re-armed → fires)", n)
	}
}

// TestKeeperCycle_PreCompactBackstop drives the RunForPrecompact path through
// the reactive harness with CrispIdle FALSE and context BELOW the act threshold
// — conditions under which MaybeRun would NOT fire. It proves END-TO-END that
// the precompact backstop:
//   - runs the cycle anyway (SKIPPING the CrispIdle and act-threshold gates).
//   - drives a causal S1→S2 flip via /clear through the reactive harness.
//   - clears the .precompact marker afterward.
//
// This adds value over TestRunForPrecompact_HappyPath (which fakes the gauge on
// a call count and never actively proves the CrispIdle/act gates are skipped)
// by setting CrispIdle=false AND context below act, then asserting the cycle
// fires regardless and the marker is cleared.
func TestKeeperCycle_PreCompactBackstop(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-precompact-agent"
		cycleID = "cyc-reactive-precompact-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)

	var markerCleared bool
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    300 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       rs.readHandoff,
		TruncateHandoffFn: rs.truncate,
		InjectFn:          rs.inject,
		ReadGaugeFn:       rs.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return false }, // NOT idle — precompact must skip this gate
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sid string) error {
			managedBinding = sid
			return nil
		},
		ClearPrecompactTriggerFn: func(_, _ string) error {
			markerCleared = true
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)

	// Context BELOW the act threshold (pct 50, well under ActPct=90) AND
	// CrispIdle=false. MaybeRun would NOT fire on this; RunForPrecompact must.
	cf := &keeper.CtxFile{Pct: 50.0, Tokens: 100_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("RunForPrecompact: %v", err)
	}

	// (a) precompact_blocked emitted with action "cycle_triggered" (gates skipped).
	pcEvents := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(pcEvents) != 1 {
		t.Fatalf("want 1 precompact_blocked; got %d", len(pcEvents))
	}
	if got := precompactAction(t, pcEvents[0]); got != "cycle_triggered" {
		t.Errorf("precompact action = %q; want \"cycle_triggered\" (CrispIdle/act gates must be skipped)", got)
	}

	// (b) the cycle ran to completion despite CrispIdle=false and below-act context.
	wantPhases := []string{"opened", "handoff_injected", "confirmed", "cleared", "resumed", "complete"}
	got := jc.snapshot()
	if len(got) != len(wantPhases) {
		t.Fatalf("journal phases = %v; want %v (precompact must run cycle skipping CrispIdle/act)", got, wantPhases)
	}
	for i, p := range wantPhases {
		if got[i] != p {
			t.Errorf("phase[%d] = %q; want %q", i, got[i], p)
		}
	}

	// (c) the SID flip was CAUSED by /clear (reactive end-to-end).
	if rs.flipCause() != "/clear" {
		t.Errorf("SID flip caused by %q; want \"/clear\"", rs.flipCause())
	}
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.NewSessionID != s2 {
		t.Errorf("cycle_complete.new_session_id = %q; want %q (S2 — caused by /clear)", cp.NewSessionID, s2)
	}
	if managedBinding != s2 {
		t.Errorf("managed binding = %q; want %q (S2)", managedBinding, s2)
	}

	// (d) the .precompact marker was cleared afterward.
	if !markerCleared {
		t.Error("ClearPrecompactTriggerFn was never called; the .precompact marker must be cleared after the cycle")
	}
}

// TestKeeperCycle_ClearBriefHardGate_SlowClear is the PERMANENT regression
// scenario for hk-vdqe2: a keeper session handoff where /clear takes noticeably
// longer to be consumed than a single ClearSettle poll window (operator observed
// 1-2 minutes on a slow/busy pane). Before the hk-vdqe2 fix, Step 5's poll was
// best-effort-only: it timed out after ONE ClearSettle window and Step 6 fired
// the agent-brief injection regardless, keying the brief into the still-
// uncleared context (the reported bug — the brief arrived concatenated to
// /clear's own stdout).
//
// The reactive harness models the slow /clear via withClearDelay: /clear is
// injected immediately (as always), but the gauge's session_id only rotates on
// a background timer well AFTER a single ClearSettle window elapses (and well
// BEFORE the ClearConfirmBackstop hard cap). This proves the fix end-to-end,
// not just against a unit-level call-count fake:
//
//   - the brief is injected ONLY AFTER the new session_id (S2) is observed live
//     in the gauge — never before (the hard gate).
//   - /clear is re-injected at least once as part of the bounded retry (proving
//     Step 5 actually retried rather than firing on the first miss).
//   - the cycle still completes cleanly with prev=S1/new=S2 and NO
//     clear_unconfirmed (the backstop was never exhausted — confirmation
//     eventually landed).
//
// ACCEPTANCE: this test is RED against the pre-fix completeCycleTail (single
// ClearSettle poll, no retry loop) because the brief fires before the delayed
// SID flip; GREEN once the hk-vdqe2 hard-gate retry loop lands. It is a
// standing part of the keeper reactive scenario suite (runs every time, no
// build tag), not a one-off.
func TestKeeperCycle_ClearBriefHardGate_SlowClear(t *testing.T) {
	t.Parallel()

	const (
		agent   = "reactive-hardgate-agent"
		cycleID = "cyc-reactive-hardgate-001"

		clearSettle          = 20 * time.Millisecond  // deliberately SHORT single poll window
		clearDelay           = 150 * time.Millisecond // /clear takes far longer than clearSettle to land
		clearConfirmBackstop = 500 * time.Millisecond // generous vs. clearDelay — confirmation must land within it
		clearConfirmRetries  = 40
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managedBinding string

	// writeNonce=true (handoff confirms promptly) + flipOnClear=true, but the
	// flip is DELAYED past a single clearSettle window via withClearDelay.
	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/).
		withClearDelay(clearDelay)

	// briefSawFlippedSID is set at the moment the brief command is injected,
	// recording whether the gauge had ALREADY rotated to S2 by then. This is
	// the load-bearing hard-gate witness: it must be true (never false, never
	// "brief injected while still S1").
	var briefInjected atomic.Bool
	var briefSawFlippedSID atomic.Bool
	witnessInject := func(ctx context.Context, target, text string) error {
		if containsSubstr(text, "agent brief") {
			briefInjected.Store(true)
			briefSawFlippedSID.Store(rs.liveSID() == s2)
		}
		return rs.inject(ctx, target, text)
	}

	var mu sync.Mutex
	cfg := keeper.CyclerConfig{
		AgentName:            agent,
		ProjectDir:           t.TempDir(),
		TmuxTarget:           "fake-pane",
		ActPct:               90.0,
		WarnPct:              80.0,
		HandoffTimeout:       500 * time.Millisecond,
		ClearSettle:          clearSettle,
		PollInterval:         5 * time.Millisecond,
		ClearConfirmBackstop: clearConfirmBackstop,
		ClearConfirmRetries:  clearConfirmRetries,
		CycleIDGen:           func() string { return cycleID },
		IsManagedFn:          func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       rs.readHandoff,
		HandoffModTimeFn:  rs.handoffModTime,
		TruncateHandoffFn: rs.truncate,
		InjectFn:          witnessInject,
		ReadGaugeFn:       rs.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sid string) error {
			mu.Lock()
			defer mu.Unlock()
			managedBinding = sid
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// (a) the brief WAS injected at all.
	if !briefInjected.Load() {
		t.Fatal("agent brief was never injected")
	}

	// (b) THE HARD GATE: the brief must have been injected only after the gauge
	// had already rotated to S2. This is the exact race from the bug report —
	// pre-fix, this is false (brief fires during the still-S1 window).
	if !briefSawFlippedSID.Load() {
		t.Fatal("agent brief was injected BEFORE the gauge session_id rotated to S2 — the clear->brief hand-off is not hard-gated (hk-vdqe2 regression)")
	}

	// (c) /clear was re-injected at least once (proving Step 5 actually retried
	// past the first missed ClearSettle poll rather than firing immediately).
	clearCount := 0
	for _, cmd := range rs.snapshotInjected() {
		if cmd == "/clear" {
			clearCount++
		}
	}
	if clearCount < 2 {
		t.Errorf("/clear injected %d time(s); want >=2 (defensive retry within the backstop window)", clearCount)
	}

	// (d) confirmation landed within the backstop: NO clear_unconfirmed emitted.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperClearUnconfirmed)); n != 0 {
		t.Errorf("want 0 clear_unconfirmed (confirmation should land within the %s backstop); got %d", clearConfirmBackstop, n)
	}

	// (e) the cycle completed cleanly with prev=S1/new=S2.
	completeEvts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(completeEvts) != 1 {
		t.Fatalf("want 1 cycle_complete; got %d", len(completeEvts))
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(completeEvts[0].Payload, &cp); err != nil {
		t.Fatalf("unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != s1 || cp.NewSessionID != s2 {
		t.Errorf("cycle_complete = {prev:%q new:%q}; want {prev:%q new:%q}", cp.PrevSessionID, cp.NewSessionID, s1, s2)
	}
	if managedBinding != s2 {
		t.Errorf("managed binding = %q; want %q (S2)", managedBinding, s2)
	}
}
