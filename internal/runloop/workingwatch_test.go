package runloop

// workingwatch_test.go — the acceptance test for the kill half of the stall
// feeder.
//
// internal/runexec stepDispatchWorking turns EvNoChangeTimeout or
// EvHeartbeatStale into ActKillAgent. That arm had no producer AND no pump: the
// segment stopped driving the machine the moment it reached Working, so even a
// perfect producer would have fed a machine nobody was stepping. This file
// pins BOTH halves at once, through the real DispatchSegment and the real
// machine — a stall arriving during Working must reach the site's kill hook.
//
// Every test carries a control run that is NOT stalled and asserts its kill
// hook never fires. A watchdog that reaps a working agent is worse than one
// that reaps nothing, so "the kill hook fired" alone is never the criterion.
//
// Helper prefix: workingWatch.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
)

// workingWatchSegment builds a segment wired with a stall feed and a kill hook,
// drives it to Working, and returns it. The returned channel receives once per
// stall kill.
func workingWatchSegment(t *testing.T, stalls <-chan runexec.Event) (seg *DispatchSegment, killReasons <-chan string) {
	t.Helper()
	log := &hookLog{}
	seg, _ = newCharSegment(t, log, charSegConfig())
	killed := make(chan string, 4)

	seg.Adapter = segStubAdapter{ready: func(env core.EventEnvelope) bool {
		return env.Type == core.EventTypeAgentReady
	}}
	seg.OnLaunched = func(ctx context.Context) {
		if err := seg.Tap.EmitWithRunID(ctx, seg.RunID, core.EventTypeAgentReady, nil); err != nil {
			t.Errorf("relay agent_ready emit: %v", err)
		}
	}
	seg.Stalls = stalls
	seg.KillStalled = func(_ context.Context, reason string) { killed <- reason }

	final := seg.Run(context.Background())
	if final.Phase != runexec.DispatchWorking {
		t.Fatalf("segment phase = %q, want working (the stall watch only runs during Working)", final.Phase)
	}
	return seg, killed
}

// TestDispatchWorkingWatch_AFrozenAgentIsKilledAndAWorkingOneIsNot is the
// acceptance test: a stall fed during the Working phase reaches the site's kill
// hook, and a segment fed the ordinary live signals of a Working agent is not
// killed.
func TestDispatchWorkingWatch_AFrozenAgentIsKilledAndAWorkingOneIsNot(t *testing.T) {
	for _, kind := range []runexec.EventKind{runexec.EvNoChangeTimeout, runexec.EvHeartbeatStale} {
		t.Run(string(kind), func(t *testing.T) {
			frozenStalls := make(chan runexec.Event, 1)
			frozen, frozenKilled := workingWatchSegment(t, frozenStalls)
			defer frozen.StopWorkingWatch()

			workingStalls := make(chan runexec.Event, 1)
			working, workingKilled := workingWatchSegment(t, workingStalls)
			defer working.StopWorkingWatch()

			// The control is not merely left alone: it is fed the live signals a
			// Working agent produces. A watch that kills on anything that arrives
			// passes a control that receives nothing.
			workingStalls <- runexec.Event{Kind: runexec.EvHeartbeat, At: time.Unix(0, 0)}
			frozenStalls <- runexec.Event{Kind: kind, At: time.Unix(0, 0)}

			select {
			case reason := <-frozenKilled:
				if reason != string(kind) {
					t.Errorf("kill reason = %q, want %q — the operator needs to know which signature fired", reason, kind)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("a %s fed during Working never reached the kill hook, so a frozen agent runs for ever", kind)
			}

			select {
			case reason := <-workingKilled:
				t.Errorf("a segment fed only a heartbeat was killed anyway (reason %q)", reason)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

// TestDispatchWorkingWatch_AProgressSignalDoesNotKill guards the other
// direction. The Working phase carries live agent signals, and only the two
// stall kinds may kill. A commit or an outcome must leave the agent alone.
func TestDispatchWorkingWatch_AProgressSignalDoesNotKill(t *testing.T) {
	stalls := make(chan runexec.Event, 2)
	seg, killed := workingWatchSegment(t, stalls)
	defer seg.StopWorkingWatch()

	stalls <- runexec.Event{Kind: runexec.EvCommitObserved, At: time.Unix(0, 0)}
	stalls <- runexec.Event{Kind: runexec.EvHeartbeat, At: time.Unix(0, 0)}

	select {
	case reason := <-killed:
		t.Errorf("a progress signal killed the agent (reason %q)", reason)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDispatchWorkingWatch_TheAgentIsKilledOnce pins that a repeated stall does
// not re-kill. The feeder de-duplicates per (run, signature), but the two
// signatures can both fire for one run, and a second kill on a session that is
// already being reaped races the teardown.
func TestDispatchWorkingWatch_TheAgentIsKilledOnce(t *testing.T) {
	stalls := make(chan runexec.Event, 3)
	seg, killed := workingWatchSegment(t, stalls)
	defer seg.StopWorkingWatch()

	stalls <- runexec.Event{Kind: runexec.EvHeartbeatStale, At: time.Unix(0, 0)}
	stalls <- runexec.Event{Kind: runexec.EvNoChangeTimeout, At: time.Unix(0, 0)}
	stalls <- runexec.Event{Kind: runexec.EvHeartbeatStale, At: time.Unix(0, 0)}

	select {
	case <-killed:
	case <-time.After(5 * time.Second):
		t.Fatalf("the first stall never reached the kill hook")
	}
	select {
	case <-killed:
		t.Errorf("the kill hook fired more than once for one run")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestDispatchWorkingWatch_StopWaitsForAKillAlreadyRunning pins the contract the
// site's kill hook has to be written against.
//
// StopWorkingWatch JOINS the watch goroutine, so that no kill can fire after the
// caller has begun tearing the session down. The cost of that guarantee is that
// a kill hook which blocks blocks the stop, and therefore blocks the run — so
// every hook bound here must bound its own wait. The daemon's hook kills with a
// deadline for exactly this reason: its session sends SIGTERM and waits on the
// context before escalating, so an unbounded context there would let one frozen
// agent hold its run open for ever.
func TestDispatchWorkingWatch_StopWaitsForAKillAlreadyRunning(t *testing.T) {
	stalls := make(chan runexec.Event, 1)
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())
	entered := make(chan struct{})
	release := make(chan struct{})

	seg.Adapter = segStubAdapter{ready: func(env core.EventEnvelope) bool {
		return env.Type == core.EventTypeAgentReady
	}}
	seg.OnLaunched = func(ctx context.Context) {
		if err := seg.Tap.EmitWithRunID(ctx, seg.RunID, core.EventTypeAgentReady, nil); err != nil {
			t.Errorf("relay agent_ready emit: %v", err)
		}
	}
	seg.Stalls = stalls
	seg.KillStalled = func(context.Context, string) {
		close(entered)
		<-release
	}
	if final := seg.Run(context.Background()); final.Phase != runexec.DispatchWorking {
		t.Fatalf("segment phase = %q, want working", final.Phase)
	}

	stalls <- runexec.Event{Kind: runexec.EvHeartbeatStale, At: time.Unix(0, 0)}
	<-entered

	stopped := make(chan struct{})
	go func() { defer close(stopped); seg.StopWorkingWatch() }()

	select {
	case <-stopped:
		t.Fatalf("StopWorkingWatch returned while a kill was still running; a kill can outlive the teardown")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatalf("StopWorkingWatch never returned after the kill finished")
	}

	// Idempotent: a second stop on a watch that has already ended must return.
	seg.StopWorkingWatch()
}
