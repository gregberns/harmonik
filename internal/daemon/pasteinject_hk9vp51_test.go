package daemon_test

// pasteinject_hk9vp51_test.go — unit tests for the progress-aware commit budget
// and the implementer_budget_exceeded diagnostic in pasteInjectQuitOnCommit
// (hk-9vp51).
//
// Problem addressed: commitPollTimeout was a FLAT 30-min wall-clock deadline
// that guillotined an implementer that was genuinely working but slow to commit
// (e.g. a deep go-test loop).  Such a session keeps its tmux pane "active"
// (PaneHasActiveProcess returns true) and may emit periodic agent_heartbeat
// events, slipping the 180s launch-window and 8m heartbeat-staleness checks
// (both reset every tick on pane-activity) — yet the flat totalDeadline still
// killed it at 30m, silently, as no_commit.
//
// The fix makes the commit budget PROGRESS-aware: each genuine progress signal
// (an agent_heartbeat) extends the per-progress budget (commitPollTimeout), with
// a separate absolute hard ceiling (commitHardCeiling) that is never extended so
// a truly-hung-but-pane-active session is still eventually killed.  A diagnostic
// implementer_budget_exceeded event is emitted at the kill (run_id, elapsed,
// since-last-progress, reason).
//
// Test matrix:
//   - GuillotineReproduce: a progressing pane (heartbeats arriving faster than
//     the per-progress budget) must NOT be killed within a window that exceeds
//     the per-progress budget.  This is the reproduce-before-fix test: under the
//     OLD flat-deadline logic the kill fired at commitPollTimeout regardless of
//     heartbeats (FAIL); under the fix the deadline is extended on each heartbeat
//     (PASS).
//   - HardCeilingKills: an active pane with NO progress signals (heartbeats)
//     survives the per-progress budget (extended by liveness) but is killed once
//     the absolute hard ceiling elapses; the implementer_budget_exceeded event is
//     emitted with reason "hard-ceiling".
//   - StaleBudgetKillsAndEmits: an inactive pane (no liveness) past the
//     per-progress budget is killed and emits implementer_budget_exceeded.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hk9vp51Killer records Kill calls.
type hk9vp51Killer struct {
	calls atomic.Int64
}

func (k *hk9vp51Killer) Kill(_ context.Context) error {
	k.calls.Add(1)
	return nil
}

// hk9vp51LivenessQuitSender implements quitSender + paneLivenessChecker; alive
// controls PaneHasActiveProcess.  Does NOT implement paneOutputSizer, so the
// hk-ukx budget-extension check sees no pane output growth and fires the kill at
// commitPollTimeout (the 30-min ceiling that hk-ukx enforces).
type hk9vp51LivenessQuitSender struct {
	quitCalls atomic.Int64
	alive     atomic.Bool
}

func (q *hk9vp51LivenessQuitSender) SendQuitToLastPane(_ context.Context) error {
	q.quitCalls.Add(1)
	return nil
}

func (q *hk9vp51LivenessQuitSender) PaneHasActiveProcess(_ context.Context) bool {
	return q.alive.Load()
}

var _ daemon.PaneLivenessCheckerExported = (*hk9vp51LivenessQuitSender)(nil)

// hk9vp51GrowingOutputQuitSender implements quitSender + paneLivenessChecker +
// paneOutputSizer.  PaneOutputFingerprint returns an incrementing counter so
// every call reports new output (simulating a session actively streaming to the
// pane, e.g. a go-test loop or long LLM response).  This satisfies the hk-ukx
// activity requirement, allowing the totalDeadline to be extended until the hard
// ceiling fires.
type hk9vp51GrowingOutputQuitSender struct {
	hk9vp51LivenessQuitSender
	outputCounter atomic.Int64
}

func (q *hk9vp51GrowingOutputQuitSender) PaneOutputFingerprint(_ context.Context) (string, bool) {
	n := q.outputCounter.Add(1)
	return fmt.Sprintf("%d 0", n), true
}

var _ daemon.PaneOutputSizerExported = (*hk9vp51GrowingOutputQuitSender)(nil)

// hk9vp51SetBudget overrides the commit-budget timing package vars and returns a
// restore function.  postQuitKillGrace is pinned to 1h so the post-commit
// watchdog never interferes.
//
//   - pollInterval  — git HEAD check cadence
//   - budget        — commitPollTimeout (per-progress budget window)
//   - hardCeiling   — commitHardCeiling (absolute backstop, never extended)
//   - staleness     — heartbeatStalenessThreshold
//   - launchWindow  — launchHeartbeatTimeout
//   - killDelay     — noChangeKillDelay
func hk9vp51SetBudget(pollInterval, budget, hardCeiling, staleness, launchWindow, killDelay time.Duration) func() {
	origPoll := *daemon.ExportedCommitPollInterval
	origBudget := *daemon.ExportedCommitPollTimeout
	origHard := *daemon.ExportedCommitHardCeiling
	origStale := *daemon.ExportedHeartbeatStalenessThreshold
	origLaunch := *daemon.ExportedLaunchHeartbeatTimeout
	origKill := *daemon.ExportedNoChangeKillDelay
	origPostQuit := *daemon.ExportedPostQuitKillGrace
	*daemon.ExportedCommitPollInterval = pollInterval
	*daemon.ExportedCommitPollTimeout = budget
	*daemon.ExportedCommitHardCeiling = hardCeiling
	*daemon.ExportedHeartbeatStalenessThreshold = staleness
	*daemon.ExportedLaunchHeartbeatTimeout = launchWindow
	*daemon.ExportedNoChangeKillDelay = killDelay
	*daemon.ExportedPostQuitKillGrace = 1 * time.Hour
	return func() {
		*daemon.ExportedCommitPollInterval = origPoll
		*daemon.ExportedCommitPollTimeout = origBudget
		*daemon.ExportedCommitHardCeiling = origHard
		*daemon.ExportedHeartbeatStalenessThreshold = origStale
		*daemon.ExportedLaunchHeartbeatTimeout = origLaunch
		*daemon.ExportedNoChangeKillDelay = origKill
		*daemon.ExportedPostQuitKillGrace = origPostQuit
	}
}

// TestPasteInjectQuitOnCommit_GuillotineReproduce is the reproduce-before-fix
// test (hk-9vp51).  A session that is genuinely progressing — emitting heartbeats
// faster than the per-progress budget — must NOT be guillotined within a window
// that is several times the per-progress budget.
//
// BEFORE the fix: commitPollTimeout was a flat totalDeadline never extended by
// heartbeats, so the kill fired at ~budget regardless of progress → this test
// would observe a Kill and FAIL.
//
// AFTER the fix: each heartbeat extends totalDeadline by another budget window,
// so no kill fires within the observation window → PASS.
func TestPasteInjectQuitOnCommit_GuillotineReproduce(t *testing.T) {
	restore := hk9vp51SetBudget(
		5*time.Millisecond,  // poll interval
		40*time.Millisecond, // per-progress budget (short)
		10*time.Second,      // hard ceiling (far away — must not fire in window)
		40*time.Millisecond, // staleness (kept in step with budget)
		5*time.Second,       // launch window (heartbeat arrives early)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk9vp51LivenessQuitSender{}
	qs.alive.Store(true) // pane stays active throughout (genuine work)
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 64)

	// Observation window = 300ms ≈ 7.5× the per-progress budget.  Under the OLD
	// flat deadline the kill would have fired by ~40ms.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	// Pump heartbeats every 15ms (well inside the 40ms budget) so every budget
	// window is refreshed by a genuine progress signal.
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case eventCh <- hk7srrdHeartbeatEnv():
				default:
				}
			}
		}
	}()

	// Runs until ctx times out (no kill expected).
	daemon.ExportedPasteInjectQuitOnCommit(ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh)

	if got := kl.calls.Load(); got != 0 {
		t.Errorf("Kill calls: want 0 (progressing session must NOT be guillotined), got %d", got)
	}
	select {
	case <-noChangeCh:
		t.Fatal("noChangeTimeoutCh was closed — a progressing session was killed (guillotine not fixed)")
	default:
		// expected
	}
}

// TestPasteInjectQuitOnCommit_HardCeilingKills verifies the absolute backstop: an
// active pane that emits NO heartbeats but IS producing pane output (simulating
// a go-test loop or active LLM streaming) survives the per-progress budget
// (hk-ukx activity check: pane output growth extends the deadline) but is killed
// once the hard ceiling elapses, and emits the implementer_budget_exceeded
// diagnostic with reason "hard-ceiling".
//
// hk-ukx note: the stub uses hk9vp51GrowingOutputQuitSender (implements
// paneOutputSizer with an incrementing counter) so the budget-extension's
// activity check passes (pane output growing → extend), allowing the hard
// ceiling to be the ultimate kill trigger.  A stub WITHOUT pane output growth
// would fire the 30-min ceiling instead (see TestPasteInjectCommitBudget_IdleActivePane_HKukx).
func TestPasteInjectQuitOnCommit_HardCeilingKills(t *testing.T) {
	restore := hk9vp51SetBudget(
		5*time.Millisecond,   // poll interval
		40*time.Millisecond,  // per-progress budget (extended by pane output growth)
		120*time.Millisecond, // hard ceiling (fires after ~120ms)
		5*time.Second,        // staleness (suppressed by liveness — must not fire)
		5*time.Second,        // launch window (launch deadline beyond hard ceiling → not reached)
		10*time.Millisecond,  // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	// Growing-output stub: pane active + output fingerprint changes every call →
	// hk-ukx activity check extends totalDeadline, so hard ceiling is the kill.
	qs := &hk9vp51GrowingOutputQuitSender{}
	qs.alive.Store(true) // pane active but never commits and never beats
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{})
	eventCh := make(chan core.EventEnvelope, 4)
	bus := &stubEventCollector{}
	runID := core.RunID{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	daemon.ExportedPasteInjectQuitOnCommitWithBus(
		ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, eventCh, bus, runID)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (hard ceiling must fire), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after hard-ceiling kill")
	}

	pl := hk9vp51FindBudgetExceeded(t, bus)
	if pl.Reason != "hard-ceiling" {
		t.Errorf("implementer_budget_exceeded reason: want %q, got %q", "hard-ceiling", pl.Reason)
	}
	if pl.ElapsedMS <= 0 {
		t.Errorf("implementer_budget_exceeded elapsed_ms: want > 0, got %d", pl.ElapsedMS)
	}
	if !pl.Valid() {
		t.Errorf("implementer_budget_exceeded payload invalid: %+v", pl)
	}
}

// TestPasteInjectQuitOnCommit_StaleBudgetKillsAndEmits verifies that an INACTIVE
// pane (no liveness checker reporting active) past the per-progress budget is
// killed and emits implementer_budget_exceeded with reason "total-budget-stale".
func TestPasteInjectQuitOnCommit_StaleBudgetKillsAndEmits(t *testing.T) {
	restore := hk9vp51SetBudget(
		5*time.Millisecond,  // poll interval
		40*time.Millisecond, // per-progress budget
		10*time.Second,      // hard ceiling (budget wins first because pane dead)
		5*time.Second,       // staleness (eventCh nil → skipped)
		5*time.Second,       // launch window (eventCh nil → skipped)
		10*time.Millisecond, // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk9vp51LivenessQuitSender{}
	qs.alive.Store(false) // pane dead → budget kill fires (not extended)
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{})
	bus := &stubEventCollector{}
	runID := core.RunID{}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// eventCh nil → heartbeat/launch checks skipped; only the wall-clock budget
	// path (now progress-aware) can fire.
	daemon.ExportedPasteInjectQuitOnCommitWithBus(
		ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil, bus, runID)

	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (inactive pane past budget must be killed), got %d", got)
	}
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed after stale-budget kill")
	}

	pl := hk9vp51FindBudgetExceeded(t, bus)
	if pl.Reason != "total-budget-stale" {
		t.Errorf("implementer_budget_exceeded reason: want %q, got %q", "total-budget-stale", pl.Reason)
	}
	if !pl.Valid() {
		t.Errorf("implementer_budget_exceeded payload invalid: %+v", pl)
	}
}

// TestPasteInjectCommitBudget_IdleActivePane_HKukx is the hk-ukx regression:
// an active pane with NO observable progress (no pane output growth, no worktree
// change, no heartbeat) must be killed at commitPollTimeout, NOT extended to the
// hard ceiling.
//
// Scenario (2026-06-12 incident): two runs hung at launch_initiated for 54–67
// minutes because the idle Claude pane had an active child process
// (PaneHasActiveProcess=true), causing the per-progress budget to be extended
// indefinitely (up to the 90-min hard ceiling).  The fix (hk-ukx) requires
// observable progress (worktree change OR pane output growth) before extending
// the budget.  An idle Claude waiting for input has a stable fingerprint and is
// killed at the 30-min commitPollTimeout boundary, not 90 min.
//
// This test uses hk9vp51LivenessQuitSender (PaneHasActiveProcess=true, NO
// paneOutputSizer) to simulate the idle-Claude scenario.  The kill must fire at
// ~budget (40ms), not at hardCeiling (120ms), with reason "total-budget-stale-active".
func TestPasteInjectCommitBudget_IdleActivePane_HKukx(t *testing.T) {
	restore := hk9vp51SetBudget(
		5*time.Millisecond,   // poll interval
		40*time.Millisecond,  // per-progress budget — must be the kill trigger
		120*time.Millisecond, // hard ceiling — must NOT be reached (kill fires earlier)
		5*time.Second,        // staleness (eventCh nil → skipped)
		5*time.Second,        // launch window (launch deadline beyond budget → not the trigger)
		5*time.Millisecond,   // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	// Idle-Claude stub: pane active (alive=true) but NO paneOutputSizer → the
	// hk-ukx activity check sees no progress → kills at commitPollTimeout.
	qs := &hk9vp51LivenessQuitSender{}
	qs.alive.Store(true) // active pane, idle agent (no progress)
	kl := &hk9vp51Killer{}
	noChangeCh := make(chan struct{})
	bus := &stubEventCollector{}
	runID := core.RunID{}

	// Budget 40ms + kill delay 5ms + 3× poll = ~60ms max.  3s is the safety net.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	// nil eventCh: heartbeat/launch checks skipped; only commit-budget check fires.
	daemon.ExportedPasteInjectQuitOnCommitWithBus(
		ctx, qs, kl, wtPath, headSHA, noChangeCh, nil, nil, bus, runID)
	elapsed := time.Since(start)

	// Kill MUST have fired (ceiling enforced).
	if got := kl.calls.Load(); got != 1 {
		t.Errorf("Kill calls: want 1 (idle active pane must be killed at commitPollTimeout), got %d", got)
	}
	// noChangeTimeoutCh MUST be closed.
	select {
	case <-noChangeCh:
		// expected
	default:
		t.Fatal("noChangeTimeoutCh was NOT closed — slot never freed (hk-ukx regression)")
	}
	// Kill must fire well before the hard ceiling (120ms), demonstrating the
	// 30-min ceiling is enforced.  Allow 2× budget for scheduler jitter.
	if elapsed >= 100*time.Millisecond {
		t.Errorf("kill fired after %v — too close to hard ceiling (%v); 30-min ceiling not enforced (hk-ukx)",
			elapsed, 120*time.Millisecond)
	}
	// Reason must be the new "active but no observable progress" tag.
	pl := hk9vp51FindBudgetExceeded(t, bus)
	if pl.Reason != "total-budget-stale-active" {
		t.Errorf("implementer_budget_exceeded reason: want %q, got %q",
			"total-budget-stale-active", pl.Reason)
	}
	if !pl.Valid() {
		t.Errorf("implementer_budget_exceeded payload invalid: %+v", pl)
	}
}

// hk9vp51FindBudgetExceeded extracts the single implementer_budget_exceeded
// payload from the collector, failing the test if it is absent or duplicated.
func hk9vp51FindBudgetExceeded(t *testing.T, bus *stubEventCollector) core.ImplementerBudgetExceededPayload {
	t.Helper()
	var found []core.ImplementerBudgetExceededPayload
	for _, ev := range bus.allEvents() {
		if ev.EventType != string(core.EventTypeImplementerBudgetExceeded) {
			continue
		}
		var pl core.ImplementerBudgetExceededPayload
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("unmarshal implementer_budget_exceeded payload: %v", err)
		}
		found = append(found, pl)
	}
	if len(found) != 1 {
		t.Fatalf("implementer_budget_exceeded events: want exactly 1, got %d (types=%v)",
			len(found), bus.eventTypes())
	}
	return found[0]
}
