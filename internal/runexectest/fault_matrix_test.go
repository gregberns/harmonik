package runexectest_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/replay"
	"github.com/gregberns/harmonik/internal/substrate"
)

var matrixModes = []struct {
	name string
	mode substrate.FaultMode
}{
	{"drop_after", substrate.FaultDropAfter},
	{"stall", substrate.FaultStall},
	{"truncate", substrate.FaultTruncate},
	{"dup", substrate.FaultDup},
}

const virtualBound = harnessStaleAfter + harnessReadyTimeout + harnessReadyKillReap +
	3*harnessInputAck + 5*time.Minute

var matrixStart = time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

func assertCellTerminal(t *testing.T, res driveResult) {
	t.Helper()
	if res.EntryForclose {
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
