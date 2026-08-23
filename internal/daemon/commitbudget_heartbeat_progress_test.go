package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

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
	if budget >= ceiling {
		t.Fatalf("commit budget %v is not shorter than the hard ceiling %v; this test cannot separate the two branches", budget, ceiling)
	}

	const beatsPerCommitBudget = 6
	beat := budget / beatsPerCommitBudget
	if beat >= heartbeatStalenessThreshold {
		t.Fatalf("beat interval %v (budget %v / %d) is not shorter than the staleness threshold %v; the staleness kill would fire first and this test would prove nothing",
			beat, budget, beatsPerCommitBudget, heartbeatStalenessThreshold)
	}
	killAt := beat * (beatsPerCommitBudget + 1)
	if killAt >= ceiling {
		t.Fatalf("the drive would reach %v, at or past the hard ceiling %v; this test cannot separate the two branches", killAt, ceiling)
	}

	clk := substrate.NewFakeClock(rt19cFakeClockEpoch)
	qk := newRT19cQuitKiller()
	bus := &commitBudgetHeartbeatBus{}
	wtPath := t.TempDir() // no commit ever lands here, and nothing writes a file
	noChangeTimeoutCh := make(chan struct{})

	events := make(chan core.EventEnvelope)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		pasteInjectQuitOnCommit(ctx, clk, qk, qk, wtPath, "deadbeef",
			noChangeTimeoutCh, nil, events, bus, core.RunID{})
	}()

	wallStart := time.Now()

	rt19cBlockUntil(t, clk, 1, "the commit poll ticker")

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

	if pl.Reason != "total-budget-stale" {
		t.Errorf("reason = %q, want %q — the kill must be attributed to the commit budget, not the ceiling", pl.Reason, "total-budget-stale")
	}
	if got, want := time.Duration(pl.ElapsedMS)*time.Millisecond, ceiling; got >= want {
		t.Errorf("elapsed %v reached the hard ceiling %v; the budget did not fire", got, want)
	}

	elapsed := time.Duration(pl.ElapsedMS) * time.Millisecond
	sinceProgress := time.Duration(pl.SinceLastProgressMS) * time.Millisecond
	if sinceProgress < budget {
		t.Errorf("since_last_progress = %v, want at least the budget %v (elapsed %v) — a heartbeat is being counted as progress", sinceProgress, budget, elapsed)
	}

	if wall := time.Since(wallStart); wall > rt19cWallBudget {
		t.Fatalf("drove a %v virtual budget in %v of REAL time; want well under %v", killAt, wall, rt19cWallBudget)
	}
}

func sendCommitBudgetHeartbeat(t *testing.T, events chan<- core.EventEnvelope, n int) {
	t.Helper()
	select {
	case events <- core.EventEnvelope{Type: core.EventTypeAgentHeartbeat}:
	case <-time.After(rt19cWallBudget):
		t.Fatalf("timed out (%v of REAL time) delivering heartbeat %d — the commit loop is no longer reading its event channel", rt19cWallBudget, n)
	}
}
