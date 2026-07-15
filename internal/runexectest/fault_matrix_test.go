package runexectest_test

// fault_matrix_test.go — the RT11 substrate.Twin fault matrix (RSM-030;
// liveness-parity-design §6 items (2); RSM-INV-001/002).
//
// Dimensions: the 6 corpus strata's synthesized schedules × 4 substrate fault
// modes (drop_after / stall / truncate / dup) × every 1-based EventN position
// of the schedule, plus one clean (FaultNone) cell per stratum. Every cell
// must reach the Run terminal — Done{closed} or Done{reopened} with exactly
// one ActEmitRunTerminal — within the bounded VIRTUAL window; a reopened
// terminal must carry a non-empty reopen reason. Silence fails the cell
// (pumpToTerminal converts "in flight with nothing armed" into a failure; the
// step guard converts a livelock into a failure; go test -timeout is the
// outermost backstop). Required pass rate: 100% — invariants, not statistics.
//
// ENTRY-FORECLOSED CELLS (keeper T12 precedent): FaultStall@1 withholds the
// entire dispatch stream — start_dispatch never arrives, no session exists,
// no reactor deadline was ever armed. Those cells assert the no-entry shape
// (zero delivered, dispatch Idle, zero terminals) instead; the liveness half
// is still proven (a hang would trip the harness guards).
//
// HEADLINE: TestRunexecFaultMatrix_StallAfterResume — the resumed relaunch
// stream stalls after the resume input_ack (the session is Working, no
// reactor timer is armed). The shell-side frozen commit watchdog (M3-D3)
// feeds heartbeat_stale, the Dispatch machine lands Stalled, and the Run
// machine terminates on the fail-closed reopen spine — a terminal, never
// silence, within the virtual bound.

import (
	"fmt"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/replay"
	"github.com/gregberns/harmonik/internal/substrate"
)

// matrixModes enumerates the four injected fault modes.
var matrixModes = []struct {
	name string
	mode substrate.FaultMode
}{
	{"drop_after", substrate.FaultDropAfter},
	{"stall", substrate.FaultStall},
	{"truncate", substrate.FaultTruncate},
	{"dup", substrate.FaultDup},
}

// virtualBound is the never-silence window: schedule span + ready timeout +
// kill-reap + the input-ack retry budget + the shell watchdog ceiling + slack.
// Every non-foreclosed cell must terminate within it (virtual time).
const virtualBound = harnessStaleAfter + harnessReadyTimeout + harnessReadyKillReap +
	3*harnessInputAck + 5*time.Minute

// matrixStart anchors the shared virtual timeline.
var matrixStart = time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

// assertCellTerminal is the uniform per-cell assertion shape.
func assertCellTerminal(t *testing.T, res driveResult) {
	t.Helper()
	if res.EntryForclose {
		// The documented no-entry shape: nothing delivered, no session, no
		// terminal to demand.
		if res.Delivered != 0 || res.RunTerminals != 0 || res.Reopens != 0 {
			t.Fatalf("entry-foreclosed cell has activity: %+v", res)
		}
		return
	}
	if !res.RunDone {
		t.Fatalf("cell did not reach the Run terminal: %+v", res)
	}
	if res.DoneOutcome != "closed" && res.DoneOutcome != "reopened" {
		t.Fatalf("unexpected terminal outcome %q", res.DoneOutcome)
	}
	if res.RunTerminals != 1 {
		t.Fatalf("want exactly one ActEmitRunTerminal, got %d", res.RunTerminals)
	}
	if res.DoneOutcome == "reopened" {
		if res.Reopens != 1 {
			t.Fatalf("reopened terminal wants exactly one ActReopenBead, got %d", res.Reopens)
		}
		if res.ReopenReason == "" {
			t.Fatal("reopened terminal carries an empty reopen reason")
		}
	}
	if res.Elapsed > virtualBound {
		t.Fatalf("terminal outside the virtual bound: %v > %v", res.Elapsed, virtualBound)
	}
}

// TestRunexecFaultMatrix is the full matrix: 6 strata × 4 modes × every
// stimulus position, plus a clean cell per stratum. 100% terminal-never-
// silence required.
func TestRunexecFaultMatrix(t *testing.T) {
	t.Parallel()
	for _, sum := range loadSummaries(t) {
		steps := len(replay.SynthesizeSchedule(sum).Steps)
		t.Run(sum.Stratum+"/clean", func(t *testing.T) {
			t.Parallel()
			clock := substrate.NewFakeClock(matrixStart)
			res := drive(t, clock, sum, substrate.FaultConfig{})
			assertCellTerminal(t, res)
			if !res.RunDone {
				t.Fatal("clean cell must terminate")
			}
		})
		for _, m := range matrixModes {
			for n := 1; n <= steps; n++ {
				name := fmt.Sprintf("%s/%s@%d", sum.Stratum, m.name, n)
				fault := substrate.FaultConfig{Mode: m.mode, EventN: n}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					clock := substrate.NewFakeClock(matrixStart)
					assertCellTerminal(t, drive(t, clock, sum, fault))
				})
			}
		}
	}
}

// TestRunexecFaultMatrix_StallAfterResume is the RT11 headline cell, pinned by
// name: the resumed relaunch stream (review-loop-resume stratum) stalls
// immediately after the resume input_ack — the exact SR9/hk-incident shape the
// resume-liveness fix bounds. The Run machine MUST land a failure-class
// terminal (Done{reopened}) within the virtual bound.
func TestRunexecFaultMatrix_StallAfterResume(t *testing.T) {
	t.Parallel()
	sum := summaryForStratum(t, "review-loop-resume")
	sched := replay.SynthesizeSchedule(sum)
	// Locate the input_ack step; stall on the NEXT event.
	ackIdx := -1
	for i, st := range sched.Steps {
		if st.Kind == "input_ack" {
			ackIdx = i
		}
	}
	if ackIdx < 0 || ackIdx+1 >= len(sched.Steps) {
		t.Fatalf("resumed schedule has no post-ack step to stall on: %+v", sched.Steps)
	}
	clock := substrate.NewFakeClock(matrixStart)
	res := drive(t, clock, sum, substrate.FaultConfig{
		Mode: substrate.FaultStall, EventN: ackIdx + 2, // 1-based, event after the ack
	})
	assertCellTerminal(t, res)
	if !res.RunDone || res.DoneOutcome != "reopened" {
		t.Fatalf("stall-after-resume must land the fail-closed reopen terminal, got %+v", res)
	}
	if res.ResumeInputs != 1 {
		t.Fatalf("resumed session must deliver exactly one resume_prompt, got %d", res.ResumeInputs)
	}
	if res.Elapsed > virtualBound {
		t.Fatalf("headline cell exceeded the virtual bound: %v", res.Elapsed)
	}
}
