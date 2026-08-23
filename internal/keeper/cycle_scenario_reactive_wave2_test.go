package keeper_test

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
	managedBinding := "stale-binding-sentinel"

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

	if !rs.sawClear() {
		t.Fatal("/clear was not injected; expected it after handoff confirmation (writeNonce=true)")
	}

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

	if managedBinding != "" {
		t.Errorf("managed binding = %q; want \"\" (cleared for .sid rebind; not a bogus new SID)", managedBinding)
	}

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

	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/)

	var mu sync.Mutex
	var order []string
	escapeFn := func(_ context.Context, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, "escape")
		return nil
	}
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
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: injectFn, Gauge: rs.readGauge, JournalWrite: jc.write}
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
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Pane = testPaneWithEscape{PaneWriter: deps.Pane, sendEscape: escapeFn}
		deps.Idle = testIdleProbe(false)
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			managedBinding = sid
			return nil
		}}
	})

	cf := &keeper.CtxFile{Pct: 97.0, Tokens: 390_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

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
		t.Errorf("pane port did not receive Escape; order = %v", snap)
	}
	if handoffIdx == -1 {
		t.Fatalf("/session-handoff was never injected; order = %v", snap)
	}
	if escapeIdx >= handoffIdx {
		t.Errorf("Escape (idx=%d) must precede /session-handoff inject (idx=%d); order=%v", escapeIdx, handoffIdx, snap)
	}

	if !rs.sawClear() {
		t.Fatal("/clear was never injected on the force path — nonce gate may have been skipped")
	}
	if cause := rs.flipCause(); cause != "/clear" {
		t.Errorf("SID flip caused by %q; want exactly \"/clear\" (force path must still gate /clear on the nonce)", cause)
	}
	if rs.sidViolatedCausality() {
		t.Error("a new SID appeared before /clear was injected — nonce gate / causality violated on force path")
	}

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

	cf2 := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s2}
	if err := cycler.MaybeRun(ctx, cf2); err != nil {
		t.Fatalf("tick 2 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Errorf("after tick 2 (S2 high, not yet re-armed): cycle_complete = %d; want 1 (suppressed)", n)
	}

	cf3 := &keeper.CtxFile{Pct: 40.0, Tokens: 60_000, WindowSize: 1_000_000, SessionID: s2}
	if err := cycler.MaybeRun(ctx, cf3); err != nil {
		t.Fatalf("tick 3 MaybeRun: %v", err)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 1 {
		t.Errorf("after tick 3 (S2 below warn): cycle_complete = %d; want 1 (re-arm observation, no fire)", n)
	}

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
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: rs.inject, Gauge: rs.readGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 500 * time.Millisecond,
		ClearSettle:    300 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Idle = testIdleProbe(false)
		deps.Context = testContextWithClear{ContextStore: deps.Context, clear: func() error {
			markerCleared = true
			return nil
		}}
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			managedBinding = sid
			return nil
		}}
	})

	cf := &keeper.CtxFile{Pct: 50.0, Tokens: 100_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
		t.Fatalf("RunForPrecompact: %v", err)
	}

	pcEvents := em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked)
	if len(pcEvents) != 1 {
		t.Fatalf("want 1 precompact_blocked; got %d", len(pcEvents))
	}
	if got := precompactAction(t, pcEvents[0]); got != "cycle_triggered" {
		t.Errorf("precompact action = %q; want \"cycle_triggered\" (CrispIdle/act gates must be skipped)", got)
	}

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

	if !markerCleared {
		t.Error("context store did not clear the .precompact marker after the cycle")
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

	rs := newReactiveSession(s1, s2, true /*writeNonce*/, true /*flipOnClear*/).
		withClearDelay(clearDelay)

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
	cfgOverrides := testCycleOverrides{CycleIDs: func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: witnessInject, Gauge: rs.readGauge, JournalWrite: jc.write}
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
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Handoff = testHandoffWithModTime{HandoffDocument: deps.Handoff, modTime: rs.handoffModTime}
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			mu.Lock()
			defer mu.Unlock()
			managedBinding = sid
			return nil
		}}
	})

	cf := &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	if !briefInjected.Load() {
		t.Fatal("agent brief was never injected")
	}

	if !briefSawFlippedSID.Load() {
		t.Fatal("agent brief was injected BEFORE the gauge session_id rotated to S2 — the clear->brief hand-off is not hard-gated (hk-vdqe2 regression)")
	}

	clearCount := 0
	for _, cmd := range rs.snapshotInjected() {
		if cmd == "/clear" {
			clearCount++
		}
	}
	if clearCount < 2 {
		t.Errorf("/clear injected %d time(s); want >=2 (defensive retry within the backstop window)", clearCount)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperClearUnconfirmed)); n != 0 {
		t.Errorf("want 0 clear_unconfirmed (confirmation should land within the %s backstop); got %d", clearConfirmBackstop, n)
	}

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
