package keeper

// antiloop_postcycle_hkpvrfx_test.go — hk-pvrfx: Gate 6's forced-clear retry
// escape hatch must be anchored on the instant the previous cycle ENDED, not on
// the (force-only, cycle-START) LastForcedAttemptAt stamp.
//
// Pre-fix Gate 6 read:
//
//	return !s.LastForcedAttemptAt.IsZero() && at.Sub(s.LastForcedAttemptAt) < cfg.ForceRetryInterval
//
// which had two independent holes, each of which let a brand-new cycle open one
// poll tick (~1s) after the previous one reached its terminal, on the SAME
// session id:
//
//  1. ZERO fall-through — the prior cycle entered below the force threshold, so
//     LastForcedAttemptAt was never stamped and the missing timestamp read as
//     "not suppressed".
//  2. STALE anchor — a cycle's own wall time routinely exceeds
//     ForceRetryInterval, so a start-anchored window was already spent by the
//     time the cycle finished.
//
// Live evidence (crew chani): cycle 000003 completed 20:11:41 via the
// clear_unconfirmed path; cycle 000004 opened 20:11:42 on the same session id,
// injected /session-handoff into a still-booting session, and aborted 300s
// later on handoff_timeout.

import (
	"testing"
	"time"
)

// pvrfxConfig is a defaulted reactor config with an explicit warn/act/force band
// and an explicit ForceRetryInterval, so the Pct fallback path (Tokens == 0)
// decides every threshold in these tests.
func pvrfxConfig() *CyclerConfig {
	cfg := &CyclerConfig{
		AgentName:          "pvrfx-agent",
		ProjectDir:         "/nonexistent",
		TmuxTarget:         "fake-pane",
		WarnPct:            80.0,
		ActPct:             90.0,
		ForceActPct:        95.0,
		ForceRetryInterval: pvrfxForceRetr,
	}
	cfg.applyDefaults()
	return cfg
}

const (
	pvrfxSID       = "sess-pvrfx"
	pvrfxNewSID    = "sess-pvrfx-new"
	pvrfxForceRetr = 120 * time.Second
)

// aboveForce is a gauge reading past ForceActPct (95) — the ONLY band in which
// Gate 6's escape hatch is reachable at all.
func pvrfxAboveForce(sid string) *CtxFile { return &CtxFile{Pct: 97.0, SessionID: sid} }

// belowForce is above the act threshold (90) but below force (95).
func pvrfxBelowForce(sid string) *CtxFile { return &CtxFile{Pct: 92.0, SessionID: sid} }

// ── Unit: the pure Gate 6 predicate ─────────────────────────────────────────

// TestGate6_ZeroForcedAttempt_SuppressedWithinPostCycleGrace is hole 1: the
// previous cycle entered BELOW the force threshold, so LastForcedAttemptAt is
// still zero. Pre-fix the missing stamp fell through as "not suppressed" and an
// immediate same-SID re-fire was allowed.
func TestGate6_ZeroForcedAttempt_SuppressedWithinPostCycleGrace(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	end := time.Unix(1_700_000_000, 0)

	s := CycleState{
		Phase:        PhaseIdle,
		LastFiredSID: pvrfxSID,
		// The prior cycle was never above force → never stamped.
		LastForcedAttemptAt: time.Time{},
		LastCycleEndedAt:    end,
	}

	if !gateAntiLoopSuppresses(cfg, s, end.Add(time.Second), pvrfxAboveForce(pvrfxSID)) {
		t.Fatal("same-SID re-fire 1s after the terminal was NOT suppressed; " +
			"the zero-value LastForcedAttemptAt fall-through is still open")
	}
}

// TestGate6_StaleStartAnchor_SuppressedWithinPostCycleGrace is hole 2: the
// prior cycle DID stamp LastForcedAttemptAt, but at cycle START — and the cycle
// then ran longer than ForceRetryInterval, so a start-anchored window was
// already fully spent at the terminal.
func TestGate6_StaleStartAnchor_SuppressedWithinPostCycleGrace(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	end := time.Unix(1_700_000_000, 0)
	start := end.Add(-400 * time.Second) // cycle ran 400s > ForceRetryInterval (120s)

	s := CycleState{
		Phase:               PhaseIdle,
		LastFiredSID:        pvrfxSID,
		LastForcedAttemptAt: start,
		LastCycleEndedAt:    end,
	}

	if !gateAntiLoopSuppresses(cfg, s, end.Add(time.Second), pvrfxAboveForce(pvrfxSID)) {
		t.Fatalf("same-SID re-fire 1s after a %s-long cycle was NOT suppressed; "+
			"the retry window is still anchored on the cycle START stamp",
			end.Sub(start))
	}
}

// TestGate6_PostCycleGraceExpires proves the grace is a WINDOW, not a wedge: a
// session genuinely stuck above the force threshold still gets its retry, one
// ForceRetryInterval after the previous cycle ENDED. Without this the spurious-
// fire bug would be traded for a never-fires bug.
func TestGate6_PostCycleGraceExpires(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	end := time.Unix(1_700_000_000, 0)

	s := CycleState{
		Phase:            PhaseIdle,
		LastFiredSID:     pvrfxSID,
		LastCycleEndedAt: end,
	}

	cases := []struct {
		name string
		at   time.Time
		want bool // want suppressed
	}{
		{"at the terminal", end, true},
		{"one tick after", end.Add(time.Second), true},
		{"one ns before the window closes", end.Add(pvrfxForceRetr - time.Nanosecond), true},
		{"exactly at the window edge", end.Add(pvrfxForceRetr), false},
		{"well past the window", end.Add(pvrfxForceRetr + time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gateAntiLoopSuppresses(cfg, s, tc.at, pvrfxAboveForce(pvrfxSID))
			if got != tc.want {
				t.Fatalf("suppressed at %s = %v; want %v",
					tc.at.Sub(end), got, tc.want)
			}
		})
	}
}

// TestGate6_NewSIDAboveForce_AlsoCoveredByPostCycleGrace covers the OTHER
// branch that reaches the escape hatch: a novel session id that has not yet
// been observed below the warn threshold. Boot-grace cannot protect this case —
// bootGraceHolds is force-path EXEMPT — so post-cycle grace is the only thing
// keeping the keeper off a just-restarted session whose gauge still reads high.
func TestGate6_NewSIDAboveForce_AlsoCoveredByPostCycleGrace(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	end := time.Unix(1_700_000_000, 0)

	s := CycleState{
		Phase:                   PhaseIdle,
		LastFiredSID:            pvrfxSID,
		SeenLowPctAfterLastFire: false, // never seen low on the new sid yet
		LastCycleEndedAt:        end,
	}

	if !gateAntiLoopSuppresses(cfg, s, end.Add(time.Second), pvrfxAboveForce(pvrfxNewSID)) {
		t.Fatal("novel-SID re-fire 1s after the terminal was NOT suppressed")
	}
	if gateAntiLoopSuppresses(cfg, s, end.Add(pvrfxForceRetr), pvrfxAboveForce(pvrfxNewSID)) {
		t.Fatal("novel-SID retry still suppressed after the window closed")
	}
}

// TestGate6_UnchangedPaths pins the Gate 6 behaviour the fix must NOT disturb.
func TestGate6_UnchangedPaths(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	end := time.Unix(1_700_000_000, 0)
	late := end.Add(pvrfxForceRetr + time.Hour) // far outside every window

	cases := []struct {
		name string
		s    CycleState
		cf   *CtxFile
		at   time.Time
		want bool
	}{
		{
			name: "no cycle has ever fired → never suppressed",
			s:    CycleState{Phase: PhaseIdle},
			cf:   pvrfxAboveForce(pvrfxSID),
			at:   end,
			want: false,
		},
		{
			name: "same sid below force → suppressed outright, window irrelevant",
			s:    CycleState{Phase: PhaseIdle, LastFiredSID: pvrfxSID, LastCycleEndedAt: end},
			cf:   pvrfxBelowForce(pvrfxSID),
			at:   late,
			want: true,
		},
		{
			name: "novel sid below force, not re-armed → suppressed outright",
			s:    CycleState{Phase: PhaseIdle, LastFiredSID: pvrfxSID, LastCycleEndedAt: end},
			cf:   pvrfxBelowForce(pvrfxNewSID),
			at:   late,
			want: true,
		},
		{
			name: "novel sid, re-armed by a below-warn reading → falls through",
			s: CycleState{
				Phase: PhaseIdle, LastFiredSID: pvrfxSID,
				SeenLowPctAfterLastFire: true, LastCycleEndedAt: end,
			},
			cf:   pvrfxBelowForce(pvrfxNewSID),
			at:   end.Add(time.Second), // INSIDE the post-cycle window
			want: false,
		},
		{
			name: "re-armed novel sid above force → falls through inside the window too",
			s: CycleState{
				Phase: PhaseIdle, LastFiredSID: pvrfxSID,
				SeenLowPctAfterLastFire: true, LastCycleEndedAt: end,
			},
			cf:   pvrfxAboveForce(pvrfxNewSID),
			at:   end.Add(time.Second),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gateAntiLoopSuppresses(cfg, tc.s, tc.at, tc.cf); got != tc.want {
				t.Fatalf("suppressed = %v; want %v", got, tc.want)
			}
		})
	}
}

// TestGate6_BootGraceStillGovernsItsOwnWindow pins that the post-cycle grace did
// not leak into (or displace) the boot-grace gate: boot-grace still holds a
// below-force young session and is still force-path exempt.
func TestGate6_BootGraceStillGovernsItsOwnWindow(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	cfg.BootGracePeriod = 5 * time.Minute
	cfg.MaxBootGraceTotal = 10 * time.Minute
	since := time.Unix(1_700_000_000, 0)

	s := CycleState{
		Phase:                 PhaseIdle,
		CurrentSessionIDSince: since,
		// A terminal happened long ago: post-cycle grace is fully expired, so
		// anything holding here is boot-grace and only boot-grace.
		LastCycleEndedAt: since.Add(-time.Hour),
	}

	if !bootGraceHolds(cfg, s, since.Add(time.Minute), pvrfxBelowForce(pvrfxNewSID)) {
		t.Error("boot-grace no longer holds a young below-force session")
	}
	if bootGraceHolds(cfg, s, since.Add(time.Minute), pvrfxAboveForce(pvrfxNewSID)) {
		t.Error("boot-grace is no longer force-path exempt")
	}
	if bootGraceHolds(cfg, s, since.Add(6*time.Minute), pvrfxBelowForce(pvrfxNewSID)) {
		t.Error("boot-grace no longer expires after BootGracePeriod")
	}
}

// ── Reactor-level: the live-evidence scenario, end to end ───────────────────

// pvrfxGaugeTick builds a full-ladder-passing GaugeTick entry event.
func pvrfxGaugeTick(at time.Time, cf *CtxFile, cycleID string) Event {
	return Event{
		Kind:    EvGaugeTick,
		At:      at,
		CF:      cf,
		Gates:   GateSnapshot{Managed: true, CrispIdle: true},
		CycleID: cycleID,
	}
}

// opened reports whether the emitted batch opened a cycle (the fatal
// journal("opened") write is the unambiguous cycle-open marker).
func pvrfxOpened(actions []Action) bool {
	for _, a := range actions {
		if a.Kind == ActWriteJournal && a.Journal.Phase == "opened" {
			return true
		}
	}
	return false
}

// pvrfxDriveClearUnconfirmed drives one full cycle from an Idle GaugeTick to the
// clear_unconfirmed terminal — the exact path crew chani's cycle 000003 took: a
// completed cycle that reaches stepBriefing with NO new session id observed, so
// CurrentSessionIDSince never moves and boot-grace can never cover the re-fire.
// Returns the terminal instant.
func pvrfxDriveClearUnconfirmed(t *testing.T, m *Cycle, start time.Time, cf *CtxFile, cycleID string) time.Time {
	t.Helper()

	if got := m.Step(pvrfxGaugeTick(start, cf, cycleID)); !pvrfxOpened(got) {
		t.Fatalf("cycle %s did not open at %v", cycleID, start)
	}
	if m.State().Phase != PhaseAwaitingHandoff {
		t.Fatalf("phase after open = %v; want %v", m.State().Phase, PhaseAwaitingHandoff)
	}
	nonceAt := start.Add(20 * time.Second)
	m.Step(Event{Kind: EvNonceObserved, At: nonceAt})
	if m.State().Phase != PhaseAwaitModelDone {
		t.Fatalf("phase after nonce = %v; want %v", m.State().Phase, PhaseAwaitModelDone)
	}
	doneAt := nonceAt.Add(30 * time.Second)
	m.Step(Event{Kind: EvModelDone, At: doneAt, Source: "idle_marker"})
	if m.State().Phase != PhaseClearing {
		t.Fatalf("phase after model-done = %v; want %v", m.State().Phase, PhaseClearing)
	}
	// Backstop exhaustion → clear_unconfirmed → briefing terminal. The whole
	// cycle spans 200s here: LONGER than ForceRetryInterval (120s), which is
	// what made the cycle-START anchor useless.
	endAt := doneAt.Add(150 * time.Second)
	m.Step(Event{Kind: EvTimerFired, At: endAt, Timer: TimerClearBackstop})
	if m.State().Phase != PhaseIdle {
		t.Fatalf("phase after backstop = %v; want %v", m.State().Phase, PhaseIdle)
	}
	if m.State().LastTerminal != "complete" {
		t.Fatalf("terminal = %q; want %q", m.State().LastTerminal, "complete")
	}
	return endAt
}

// TestReactor_ClearUnconfirmedTerminal_NoImmediateRefire is the live-evidence
// regression: a cycle completes via clear_unconfirmed, and the very next poll
// tick (~1s later) on the SAME session id must not open cycle N+1.
func TestReactor_ClearUnconfirmedTerminal_NoImmediateRefire(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	m := NewCycle(cfg)
	start := time.Unix(1_700_000_000, 0)
	cf := pvrfxAboveForce(pvrfxSID)

	end := pvrfxDriveClearUnconfirmed(t, m, start, cf, "cyc-000003")

	if got := m.State().LastCycleEndedAt; !got.Equal(end) {
		t.Fatalf("LastCycleEndedAt = %v; want the terminal instant %v", got, end)
	}

	// One poll tick later, same sid, gauge still high (the /clear never
	// confirmed): cycle 000004 must NOT open.
	tick := end.Add(time.Second)
	if got := m.Step(pvrfxGaugeTick(tick, cf, "cyc-000004")); pvrfxOpened(got) {
		t.Fatal("cycle 000004 opened 1s after cycle 000003's terminal on the same session id")
	}
	if m.State().Phase != PhaseIdle {
		t.Fatalf("phase = %v after the suppressed tick; want %v", m.State().Phase, PhaseIdle)
	}

	// Still suppressed just before the window closes.
	if got := m.Step(pvrfxGaugeTick(end.Add(pvrfxForceRetr-time.Second), cf, "cyc-000004")); pvrfxOpened(got) {
		t.Fatal("cycle re-opened before the post-cycle window closed")
	}

	// …and the escape hatch still works once the window closes: a session
	// genuinely stuck above the force threshold is never wedged.
	if got := m.Step(pvrfxGaugeTick(end.Add(pvrfxForceRetr), cf, "cyc-000004")); !pvrfxOpened(got) {
		t.Fatal("forced-clear retry never fired after the post-cycle window closed")
	}
}

// TestReactor_ClearUnconfirmedTerminal_BelowForceEntry_NoImmediateRefire is the
// same scenario through hole 1 specifically: the cycle ENTERS below the force
// threshold (so LastForcedAttemptAt is never stamped) and the gauge only crosses
// force afterwards. Pre-fix this was the pure zero-value fall-through.
func TestReactor_ClearUnconfirmedTerminal_BelowForceEntry_NoImmediateRefire(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	m := NewCycle(cfg)
	start := time.Unix(1_700_000_000, 0)

	end := pvrfxDriveClearUnconfirmed(t, m, start, pvrfxBelowForce(pvrfxSID), "cyc-000003")

	if got := m.State().LastForcedAttemptAt; !got.IsZero() {
		t.Fatalf("LastForcedAttemptAt = %v; want zero (the cycle entered below force)", got)
	}

	// The gauge has since crossed the force threshold, so Gate 6 reaches the
	// escape hatch — with a zero start-anchor.
	tick := end.Add(time.Second)
	if got := m.Step(pvrfxGaugeTick(tick, pvrfxAboveForce(pvrfxSID), "cyc-000004")); pvrfxOpened(got) {
		t.Fatal("zero-value fall-through: a cycle opened 1s after the previous terminal")
	}
	if got := m.Step(pvrfxGaugeTick(end.Add(pvrfxForceRetr), pvrfxAboveForce(pvrfxSID), "cyc-000004")); !pvrfxOpened(got) {
		t.Fatal("forced-clear retry never fired after the post-cycle window closed")
	}
}

// TestReactor_AbortTerminal_RetryAnchoredOnTerminal pins the abort terminal
// (hk-qoz's own path): the retry window runs from the abort, not from the
// cycle's start — a HandoffTimeout-long cycle used to spend the entire window
// before it even finished.
func TestReactor_AbortTerminal_RetryAnchoredOnTerminal(t *testing.T) {
	t.Parallel()
	cfg := pvrfxConfig()
	m := NewCycle(cfg)
	start := time.Unix(1_700_000_000, 0)
	cf := pvrfxAboveForce(pvrfxSID)

	if got := m.Step(pvrfxGaugeTick(start, cf, "cyc-abort-1")); !pvrfxOpened(got) {
		t.Fatal("cycle did not open")
	}
	// HandoffTimeout expiry with no fresh handoff → abort. 300s > 120s window.
	abortAt := start.Add(300 * time.Second)
	m.Step(Event{Kind: EvTimerFired, At: abortAt, Timer: TimerHandoffTimeout})
	st := m.State()
	if st.LastTerminal != "aborted" {
		t.Fatalf("terminal = %q; want %q", st.LastTerminal, "aborted")
	}
	if !st.LastCycleEndedAt.Equal(abortAt) {
		t.Fatalf("LastCycleEndedAt = %v; want the abort instant %v", st.LastCycleEndedAt, abortAt)
	}
	if !st.LastForcedAttemptAt.Equal(start) {
		t.Fatalf("LastForcedAttemptAt = %v; want the cycle-start instant %v", st.LastForcedAttemptAt, start)
	}

	// 1s after the abort — pre-fix the start-anchored window (start+120s) was
	// long gone, so this fired immediately.
	if got := m.Step(pvrfxGaugeTick(abortAt.Add(time.Second), cf, "cyc-abort-2")); pvrfxOpened(got) {
		t.Fatal("a retry opened 1s after the abort; the window is still start-anchored")
	}
	// The hk-qoz guarantee survives: the retry does happen, one interval after
	// the abort.
	if got := m.Step(pvrfxGaugeTick(abortAt.Add(pvrfxForceRetr), cf, "cyc-abort-2")); !pvrfxOpened(got) {
		t.Fatal("forced-clear retry never fired after the post-cycle window closed")
	}
}
