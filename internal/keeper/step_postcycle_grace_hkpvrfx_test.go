package keeper

// step_postcycle_grace_hkpvrfx_test.go — hk-pvrfx: Gate 6's forced-clear retry
// exception must be anchored on the LAST CYCLE TERMINAL, not only on the cycle
// START stamp (LastForcedAttemptAt).
//
// Live failure this pins (crew chani, 2026-07-22): cycle 000003 completed
// 20:11:41 via the clear_unconfirmed path; cycle 000004 opened 20:11:42 on the
// SAME session id, injected /session-handoff into a session that was still
// booting, and aborted 300s later on handoff_timeout. Boot grace could not
// help — clear_unconfirmed completes with no new session id observed, so
// CurrentSessionIDSince never moves.
//
// Every test here drives the PRODUCTION reactor entry (Cycle.Step) — the same
// call the shell makes on every gauge tick — never gateAntiLoopSuppresses or
// forceRetrySuppresses in isolation.

import (
	"testing"
	"time"
)

const (
	pcgSID = "sess-chani"
	// Below ForceActPct (95) but above ActPct (90): a cycle entered here never
	// stamps LastForcedAttemptAt, which is the zero-value fall-through the bead
	// names.
	pcgActPct = 92.0
	// At/above ForceActPct: the force-retry escape hatch is in play.
	pcgForcePct = 97.0
)

// postCycleGraceConfig is a defaulted CyclerConfig on the pct-fallback band
// (act 90 / warn 80 / force 95) with the production ForceRetryInterval (120s).
// BootGracePeriod stays 0 (the package default, opt-in per construction site),
// so nothing but Gate 6 can suppress the re-fire under test.
func postCycleGraceConfig() *CyclerConfig {
	cfg := &CyclerConfig{
		AgentName:  "chani",
		ProjectDir: "/nonexistent",
		TmuxTarget: "fake-pane",
		ActPct:     90.0,
		WarnPct:    80.0,
	}
	cfg.applyDefaults()
	return cfg
}

// pcgTick builds a gauge tick whose gate snapshot passes every gate except the
// ones under test (managed + crisp idle, nothing held / sleeping / dispatching).
func pcgTick(at time.Time, sid string, pct float64, cycleID string) Event {
	return Event{
		Kind:    EvGaugeTick,
		At:      at,
		CF:      &CtxFile{Pct: pct, SessionID: sid},
		Gates:   GateSnapshot{Managed: true, CrispIdle: true},
		CycleID: cycleID,
	}
}

// pcgFired reports whether a batch of actions opened a cycle (the handoff
// injection is the observable "the keeper touched the pane" effect).
func pcgFired(actions []Action) bool {
	for _, a := range actions {
		if a.Kind == ActInjectHandoffCmd {
			return true
		}
	}
	return false
}

// pcgRunCompleteCycle drives one full cycle on the chani shape: gauge-tick
// entry → nonce confirmed → model done → clear backstop expires
// (clear_unconfirmed, NO new session id) → Briefing → Idle/complete. Returns
// the terminal instant.
func pcgRunCompleteCycle(t *testing.T, m *Cycle, open time.Time, dur time.Duration, entryPct float64, cycleID string) time.Time {
	t.Helper()
	if actions := m.Step(pcgTick(open, pcgSID, entryPct, cycleID)); !pcgFired(actions) {
		t.Fatalf("setup: entry tick at pct=%v did not open a cycle; actions=%v", entryPct, kinds(actions))
	}
	m.Step(Event{Kind: EvNonceObserved, CycleID: cycleID, At: open.Add(time.Second)})
	m.Step(Event{Kind: EvModelDone, CycleID: cycleID, Source: "idle_marker", At: open.Add(2 * time.Second)})
	end := open.Add(dur)
	m.Step(Event{Kind: EvTimerFired, CycleID: cycleID, Timer: TimerClearBackstop, At: end})

	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "complete" {
		t.Fatalf("setup: after clear-backstop expiry phase=%v terminal=%q; want idle/complete", st.Phase, st.LastTerminal)
	}
	return end
}

// pcgRunAbortedCycle drives one full cycle to the ABORT terminal (handoff
// timeout with no fresh handoff). Returns the terminal instant.
func pcgRunAbortedCycle(t *testing.T, m *Cycle, open time.Time, dur time.Duration, entryPct float64, cycleID string) time.Time {
	t.Helper()
	if actions := m.Step(pcgTick(open, pcgSID, entryPct, cycleID)); !pcgFired(actions) {
		t.Fatalf("setup: entry tick at pct=%v did not open a cycle; actions=%v", entryPct, kinds(actions))
	}
	end := open.Add(dur)
	m.Step(Event{Kind: EvTimerFired, CycleID: cycleID, Timer: TimerHandoffTimeout, At: end})

	st := m.State()
	if st.Phase != PhaseIdle || st.LastTerminal != "aborted" {
		t.Fatalf("setup: after handoff timeout phase=%v terminal=%q; want idle/aborted", st.Phase, st.LastTerminal)
	}
	return end
}

// TestStep_PostCycleGrace_ChaniRefire_ZeroForcedStamp is the exact chani
// reproduction and the bead's named defect: the completed cycle entered BELOW
// the force threshold, so LastForcedAttemptAt is still zero; one second after
// the terminal the gauge reads above the force threshold on the SAME session id.
//
// Pre-fix, Gate 6 evaluated `!LastForcedAttemptAt.IsZero() && …` — a MISSING
// timestamp read as "not suppressed" and the cycle re-fired immediately.
func TestStep_PostCycleGrace_ChaniRefire_ZeroForcedStamp(t *testing.T) {
	t.Parallel()
	cfg := postCycleGraceConfig()
	m := NewCycle(cfg)
	open := time.Unix(1_700_000_000, 0)

	// Cycle 000003: entered at 92% (above act, below force) and completed 5m41s
	// later via clear_unconfirmed — no new session id was ever observed.
	end := pcgRunCompleteCycle(t, m, open, 5*time.Minute+41*time.Second, pcgActPct, "cyc-000003")

	if st := m.State(); !st.LastForcedAttemptAt.IsZero() {
		t.Fatalf("precondition broken: LastForcedAttemptAt=%v; the chani case requires it to stay ZERO", st.LastForcedAttemptAt)
	}
	if st := m.State(); !st.CurrentSessionIDSince.IsZero() {
		t.Fatalf("precondition broken: CurrentSessionIDSince=%v; boot grace must NOT be able to cover this case", st.CurrentSessionIDSince)
	}

	// Cycle 000004 attempt, 1s after the terminal, same session id, now above
	// the force threshold. MUST be suppressed — the session is still booting.
	actions := m.Step(pcgTick(end.Add(time.Second), pcgSID, pcgForcePct, "cyc-000004"))
	if pcgFired(actions) {
		t.Fatalf("re-fire 1s after cycle terminal was NOT suppressed (actions=%v); chani: cycle 000004 opened 20:11:42 on the same sid and aborted 300s later", kinds(actions))
	}
	if st := m.State(); st.Phase != PhaseIdle {
		t.Fatalf("phase=%v after suppressed tick; want idle", st.Phase)
	}

	// …and the opposite failure (hk-220lv: a keeper that never restarts a
	// filling session) must NOT be introduced: once ForceRetryInterval has
	// elapsed since the TERMINAL, the forced-clear retry still fires.
	after := end.Add(cfg.ForceRetryInterval + time.Second)
	actions = m.Step(pcgTick(after, pcgSID, pcgForcePct, "cyc-000005"))
	if !pcgFired(actions) {
		t.Fatalf("forced-clear retry did NOT fire %v after the terminal (actions=%v); the grace must expire, never wedge", cfg.ForceRetryInterval+time.Second, kinds(actions))
	}
}

// TestStep_PostCycleGrace_StaleForcedStamp covers the second hole, which the
// bead does not name: LastForcedAttemptAt IS stamped, but at cycle START. A
// cycle's own wall time (handoff 300s + model-done 60s + clear backstop 150s)
// routinely exceeds ForceRetryInterval (120s), so the interval is already fully
// spent by the time the cycle reaches its terminal and the very next tick
// re-fires. Anchoring on the terminal closes it.
func TestStep_PostCycleGrace_StaleForcedStamp(t *testing.T) {
	t.Parallel()
	cfg := postCycleGraceConfig()
	m := NewCycle(cfg)
	open := time.Unix(1_700_000_000, 0)

	// Entered ABOVE the force threshold → LastForcedAttemptAt = open. The cycle
	// then runs 400s, well past the 120s ForceRetryInterval.
	const cycleDur = 400 * time.Second
	end := pcgRunCompleteCycle(t, m, open, cycleDur, pcgForcePct, "cyc-stale-001")

	st := m.State()
	if !st.LastForcedAttemptAt.Equal(open) {
		t.Fatalf("precondition broken: LastForcedAttemptAt=%v; want the cycle-start stamp %v", st.LastForcedAttemptAt, open)
	}
	if cycleDur <= cfg.ForceRetryInterval {
		t.Fatalf("precondition broken: cycle duration %v must exceed ForceRetryInterval %v", cycleDur, cfg.ForceRetryInterval)
	}

	actions := m.Step(pcgTick(end.Add(time.Second), pcgSID, pcgForcePct, "cyc-stale-002"))
	if pcgFired(actions) {
		t.Fatalf("re-fire 1s after a long cycle's terminal was NOT suppressed (actions=%v); the start-anchored interval was already spent by the cycle itself", kinds(actions))
	}

	after := end.Add(cfg.ForceRetryInterval + time.Second)
	if actions := m.Step(pcgTick(after, pcgSID, pcgForcePct, "cyc-stale-003")); !pcgFired(actions) {
		t.Fatalf("forced-clear retry did NOT fire %v after the terminal (actions=%v)", cfg.ForceRetryInterval+time.Second, kinds(actions))
	}
}

// TestStep_PostCycleGrace_AbortTerminalAnchors proves the ABORT terminal
// anchors the grace too. An abort leaves the pane in exactly the state the
// grace protects — the agent never answered /session-handoff — so re-injecting
// one tick later is the same wasted cycle.
func TestStep_PostCycleGrace_AbortTerminalAnchors(t *testing.T) {
	t.Parallel()
	cfg := postCycleGraceConfig()
	m := NewCycle(cfg)
	open := time.Unix(1_700_000_000, 0)

	end := pcgRunAbortedCycle(t, m, open, cfg.HandoffTimeout, pcgActPct, "cyc-abort-001")
	if st := m.State(); !st.LastForcedAttemptAt.IsZero() {
		t.Fatalf("precondition broken: LastForcedAttemptAt=%v; want zero (entry was below the force threshold)", st.LastForcedAttemptAt)
	}

	actions := m.Step(pcgTick(end.Add(time.Second), pcgSID, pcgForcePct, "cyc-abort-002"))
	if pcgFired(actions) {
		t.Fatalf("re-fire 1s after an abort terminal was NOT suppressed (actions=%v)", kinds(actions))
	}

	after := end.Add(cfg.ForceRetryInterval + time.Second)
	if actions := m.Step(pcgTick(after, pcgSID, pcgForcePct, "cyc-abort-003")); !pcgFired(actions) {
		t.Fatalf("forced-clear retry did NOT fire %v after the abort terminal (actions=%v)", cfg.ForceRetryInterval+time.Second, kinds(actions))
	}
}

// TestStep_PostCycleGrace_DoesNotBlockLegitimateNewSession is the anti-regression
// guard for the OPPOSITE failure (hk-220lv). The post-cycle anchor must gate ONLY
// the force-retry escape hatch: a genuinely new session id that has been observed
// below the warn threshold (the normal re-arm) must still fire the moment it
// crosses the act threshold, even seconds after the previous cycle's terminal.
func TestStep_PostCycleGrace_DoesNotBlockLegitimateNewSession(t *testing.T) {
	t.Parallel()
	cfg := postCycleGraceConfig()
	m := NewCycle(cfg)
	open := time.Unix(1_700_000_000, 0)

	end := pcgRunCompleteCycle(t, m, open, 5*time.Minute, pcgActPct, "cyc-new-001")

	// The new session comes up quiet: re-arms SeenLowPctAfterLastFire.
	const newSID = "sess-chani-2"
	if actions := m.Step(pcgTick(end.Add(2*time.Second), newSID, 50.0, "")); pcgFired(actions) {
		t.Fatalf("a below-warn tick opened a cycle; actions=%v", kinds(actions))
	}
	if st := m.State(); !st.SeenLowPctAfterLastFire {
		t.Fatal("setup: SeenLowPctAfterLastFire did not re-arm on the new session id")
	}

	// It fills fast and crosses the act threshold 5s after the previous cycle's
	// terminal — well inside ForceRetryInterval. It MUST still fire.
	actions := m.Step(pcgTick(end.Add(5*time.Second), newSID, pcgActPct, "cyc-new-002"))
	if !pcgFired(actions) {
		t.Fatalf("legitimate restart on a re-armed new session id was suppressed (actions=%v); the post-cycle anchor must gate only the force-retry escape hatch", kinds(actions))
	}
}
