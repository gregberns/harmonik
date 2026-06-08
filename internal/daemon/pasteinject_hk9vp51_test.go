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
// controls PaneHasActiveProcess.
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
// active pane that emits NO heartbeats survives the per-progress budget (liveness
// keeps extending it) but is killed once the hard ceiling elapses, and emits the
// implementer_budget_exceeded diagnostic with reason "hard-ceiling".
func TestPasteInjectQuitOnCommit_HardCeilingKills(t *testing.T) {
	restore := hk9vp51SetBudget(
		5*time.Millisecond,   // poll interval
		40*time.Millisecond,  // per-progress budget (extended by liveness)
		120*time.Millisecond, // hard ceiling (fires after ~120ms)
		5*time.Second,        // staleness (suppressed by liveness — must not fire)
		5*time.Second,        // launch window (suppressed by liveness)
		10*time.Millisecond,  // kill delay
	)
	defer restore()

	wtPath, headSHA := hk7srrdWorktree(t)
	qs := &hk9vp51LivenessQuitSender{}
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
