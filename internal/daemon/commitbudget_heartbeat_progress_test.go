package daemon

// commitbudget_heartbeat_progress_test.go — the commit budget advances on
// evidence of work, never only on evidence that a process exists.
//
// The defect this pins: the agent_heartbeat branch of pasteInjectQuitOnCommit
// extended the 30-minute commit budget on every beat, and the beat is a fixed
// 5-minute timer that stops only when the agent process exits. A 5-minute beat
// that always reopens a 30-minute window is a window that cannot close, so every
// wedged claude run survived to the 90-minute hard ceiling and was then recorded
// under whatever cause fired there. The 79-minute silent stall on
// hk-stop-hook-failure-wedges-run-dc5z6 is that timing.
//
// The test drives virtual time on a FakeClock, in the RT19c style, and reuses the
// rt19c* stubs and waiters from workingphasewatchdog_rt19c_test.go.
//
import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// commitBudgetHeartbeatBus records the last payload emitted per event type. It satisfies
// handlercontract.EventEmitter and nothing else.
type commitBudgetHeartbeatBus struct {
	mu       sync.Mutex
	payloads map[core.EventType][]byte
}

func (b *commitBudgetHeartbeatBus) Emit(_ context.Context, t core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.payloads == nil {
		b.payloads = make(map[core.EventType][]byte)
	}
	b.payloads[t] = append([]byte(nil), payload...)
	return nil
}

func (b *commitBudgetHeartbeatBus) EmitWithRunID(ctx context.Context, _ core.RunID, t core.EventType, p []byte) error {
	return b.Emit(ctx, t, p)
}

func (b *commitBudgetHeartbeatBus) last(t core.EventType) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.payloads[t]...)
}

// TestCW0faHeartbeatDoesNotExtendCommitBudget delivers a heartbeat to a session
// that never commits and never touches its working tree, then advances past the
// commit budget but well short of the hard ceiling. The kill must fire, and its
// diagnostic must name the BUDGET as the cause.
//
// Against the old code this test hangs and fails at rt19cAwait: the drained
// heartbeat pushed the budget out to 61 virtual minutes, so no kill could fire at
// 31, and the run went on to the 90-minute ceiling.
//
// This test does NOT call t.Parallel and does not rewrite the timing globals. It
// reads two of them, and only a sequential test can read them safely — the
// millisecond-tuned tests in this package rewrite the same vars from their own
// parallel phase.
func TestCW0faHeartbeatDoesNotExtendCommitBudget(t *testing.T) {
	budget := commitPollTimeout
	ceiling := commitHardCeiling
	// The test needs an instant that is past the budget and short of the ceiling.
	// Production is 30 vs 90 minutes. Fail loudly rather than silently drive the
	// hard-ceiling branch and assert the wrong thing.
	if budget >= ceiling {
		t.Fatalf("commit budget %v is not shorter than the hard ceiling %v; this test cannot separate the two branches", budget, ceiling)
	}

	// beats is the beat interval this test drives. It has to clear two other
	// clocks or the run dies of something else and proves nothing: a beat must
	// arrive before the launch-verification window closes, and beats must not fall
	// further apart than the staleness threshold.
	const beatsPerCommitBudget = 6
	beat := budget / beatsPerCommitBudget
	if beat >= heartbeatStalenessThreshold {
		t.Fatalf("beat interval %v (budget %v / %d) is not shorter than the staleness threshold %v; the staleness kill would fire first and this test would prove nothing",
			beat, budget, beatsPerCommitBudget, heartbeatStalenessThreshold)
	}
	// The launch-verification window needs no guard here: the first beat is sent
	// before any virtual time passes, so that window is already satisfied when the
	// drive below starts.
	// The last beat lands exactly ON the budget, and one more interval carries the
	// loop past it. Under the old behaviour that beat pushed the window out by a
	// further budget; under the new one it moves nothing.
	killAt := beat * (beatsPerCommitBudget + 1)
	if killAt >= ceiling {
		t.Fatalf("the drive would reach %v, at or past the hard ceiling %v; this test cannot separate the two branches", killAt, ceiling)
	}

	clk := substrate.NewFakeClock(rt19cFakeClockEpoch)
	qk := newRT19cQuitKiller()
	bus := &commitBudgetHeartbeatBus{}
	wtPath := t.TempDir() // no commit ever lands here, and nothing writes a file
	noChangeTimeoutCh := make(chan struct{})

	// UNBUFFERED on purpose. A send completes only when the loop has taken the
	// beat, so each beat is known to be delivered at the virtual instant this test
	// intends — and that instant is the whole point. A buffered channel lets the
	// loop drain every beat at virtual time zero, where extending the window by a
	// budget from "now" and not extending it at all are the same thing, and the
	// test cannot tell the fix from the defect.
	events := make(chan core.EventEnvelope)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// briefDelivered is nil: no brief gate, and the hk-1too stale-pane
		// fast-fail needs a fired brief, so it stays off too.
		pasteInjectQuitOnCommit(ctx, clk, qk, qk, wtPath, "deadbeef",
			noChangeTimeoutCh, nil, events, bus, core.RunID{})
	}()

	wallStart := time.Now()

	rt19cBlockUntil(t, clk, 1, "the commit poll ticker")

	// Beat, wait, beat, wait — a live agent whose process is up and whose work is
	// going nowhere. Nothing here writes to the working tree or the pane, so there
	// is never any evidence of progress but the beat itself.
	for i := 0; i <= beatsPerCommitBudget; i++ {
		sendCommitBudgetHeartbeat(t, events, i)
		clk.Advance(beat)
	}

	rt19cAwait(t, qk.quitSent, "/quit at the commit budget despite a fresh heartbeat")

	rt19cBlockUntil(t, clk, 2, "the poll ticker plus the noChange kill-delay sleeper")
	clk.Advance(rt19cOvershoot)
	rt19cAwait(t, qk.killed, "Kill after the noChange kill delay")
	rt19cAwait(t, noChangeTimeoutCh, "noChangeTimeoutCh to close")
	rt19cAwait(t, done, "pasteInjectQuitOnCommit to return")

	if quits, kills := qk.counts(); quits != 1 || kills != 1 {
		t.Fatalf("quits=%d kills=%d; want exactly 1 each", quits, kills)
	}

	raw := bus.last(core.EventTypeImplementerBudgetExceeded)
	if len(raw) == 0 {
		t.Fatalf("no %s diagnostic emitted; the kill cannot explain itself", core.EventTypeImplementerBudgetExceeded)
	}
	var pl core.ImplementerBudgetExceededPayload
	if err := json.Unmarshal(raw, &pl); err != nil {
		t.Fatalf("unmarshal %s payload: %v", core.EventTypeImplementerBudgetExceeded, err)
	}

	// The reason must name the budget. "hard-ceiling" here would mean the budget
	// was extended after all and the ceiling did the work — the exact confusion
	// that sent the 79-minute stall to the wrong subsystem.
	if pl.Reason != "total-budget-stale" {
		t.Errorf("reason = %q, want %q — the kill must be attributed to the commit budget, not the ceiling", pl.Reason, "total-budget-stale")
	}
	if got, want := time.Duration(pl.ElapsedMS)*time.Millisecond, ceiling; got >= want {
		t.Errorf("elapsed %v reached the hard ceiling %v; the budget did not fire", got, want)
	}

	// The load-bearing assertion. A heartbeat arrived during this window, and the
	// diagnostic must still report the whole window as time without progress. The
	// old code reported the age of the last beat here — never more than the beat
	// interval, whatever the session was doing.
	elapsed := time.Duration(pl.ElapsedMS) * time.Millisecond
	sinceProgress := time.Duration(pl.SinceLastProgressMS) * time.Millisecond
	if sinceProgress < budget {
		t.Errorf("since_last_progress = %v, want at least the budget %v (elapsed %v) — a heartbeat is being counted as progress", sinceProgress, budget, elapsed)
	}

	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual budget in %v of REAL time; want well under %v", killAt, wall, rt19cWallBudget)
	}
}

// sendCommitBudgetHeartbeat delivers one agent_heartbeat and fails the test if the loop does
// not take it within the real-time budget.
//
// The bound is what makes a regression legible. On an unbuffered channel a send
// to a loop that has already killed and returned blocks forever, which would
// otherwise hang the whole test binary on its -timeout and take every other
// internal/daemon test down with it.
func sendCommitBudgetHeartbeat(t *testing.T, events chan<- core.EventEnvelope, n int) {
	t.Helper()
	select {
	case events <- core.EventEnvelope{Type: core.EventTypeAgentHeartbeat}:
	case <-time.After(rt19cWallBudget):
		t.Fatalf("timed out (%v of REAL time) delivering heartbeat %d — the commit loop is no longer reading its event channel", rt19cWallBudget, n)
	}
}
