package codex_test

// codexnowork_hk368i4_test.go — the hk-368i4 no-work detector.
//
// The detector is a pure function of (outcome, phase duration, floor), so these
// tests assert the DECISION directly and contain no wall-clock timing: nothing
// here sleeps, and nothing measures elapsed time. A loaded box cannot change
// the result.
//
// The measured oracle these cases are drawn from (implementer_phase_complete
// events on lima's isolated scratch project, cross-checked against india's
// codex gate runs):
//
//	commit_landed=FALSE:  5.19s  4.00s  3.38s  3.27s
//	commit_landed=TRUE:  28.9s  31.9s  39.3s  55.3s  44.1s  53.5s
//
// Bead ref: hk-368i4.

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/harness/codex"
)

func TestCodexNoWorkSuspected_hk368i4(t *testing.T) {
	t.Parallel()

	const floor = 0 // use the shipped default

	cases := []struct {
		name     string
		outcome  codex.ExportedCodexRefsOutcome
		duration time.Duration
		override time.Duration
		want     bool
	}{
		// Every observed no-work run, at its measured duration.
		{"observed no-work 3.27s", codex.ExportedCodexRefsNoChange, 3270 * time.Millisecond, floor, true},
		{"observed no-work 3.38s", codex.ExportedCodexRefsNoChange, 3380 * time.Millisecond, floor, true},
		{"observed no-work 4.00s", codex.ExportedCodexRefsNoChange, 4 * time.Second, floor, true},
		{"observed no-work 5.19s", codex.ExportedCodexRefsNoChange, 5190 * time.Millisecond, floor, true},

		// A slow run that still produced nothing is NOT flagged: the duration
		// signal is absent, so the detector has no corroboration and stays quiet.
		// The run still fails through the no-commit guard.
		{"no-change but slow — not flagged", codex.ExportedCodexRefsNoChange, 40 * time.Second, floor, false},
		{"no-change exactly at floor — not flagged", codex.ExportedCodexRefsNoChange, codex.ExportedCodexNoWorkDurationFloorDefault, floor, false},

		// THE SAFETY CASE from the bead: a legitimately trivial bead completes
		// fast. It still commits, so it never reaches codexRefsNoChange and is
		// never flagged, whatever its duration. This is why pairing the two
		// signals is safe when duration alone would not be.
		{"fast run that committed", codex.ExportedCodexRefsCommitted, 1 * time.Second, floor, false},
		{"fast run that amended", codex.ExportedCodexRefsAmended, 1 * time.Second, floor, false},
		{"fast run already carrying the trailer", codex.ExportedCodexRefsAlreadyPresent, 1 * time.Second, floor, false},

		// An unmeasured duration is not evidence of anything. Without this the
		// detector would fire on every run whose clock plumbing is missing.
		{"zero duration — unmeasured, never flags", codex.ExportedCodexRefsNoChange, 0, floor, false},
		{"negative duration — unmeasured, never flags", codex.ExportedCodexRefsNoChange, -1 * time.Second, floor, false},

		// The floor is tunable, and tuning moves the boundary in both directions.
		{"override raises the floor above a real run", codex.ExportedCodexRefsNoChange, 20 * time.Second, 30 * time.Second, true},
		{"override lowers the floor below a no-work run", codex.ExportedCodexRefsNoChange, 4 * time.Second, 2 * time.Second, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codex.ExportedCodexNoWorkSuspected(tc.outcome, tc.duration, tc.override)
			if got != tc.want {
				t.Errorf("codexNoWorkSuspected(%v, %v, override=%v) = %v; want %v",
					tc.outcome, tc.duration, tc.override, got, tc.want)
			}
		})
	}
}

// TestCodexNoWorkFloor_hk368i4 pins the override-resolution rule: a zero or
// negative override falls back to the default, so a zero-valued deps struct
// (production) behaves exactly as the shipped default.
func TestCodexNoWorkFloor_hk368i4(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		override time.Duration
		want     time.Duration
	}{
		{"zero falls back to default", 0, codex.ExportedCodexNoWorkDurationFloorDefault},
		{"negative falls back to default", -5 * time.Second, codex.ExportedCodexNoWorkDurationFloorDefault},
		{"positive override wins", 25 * time.Second, 25 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := codex.ExportedCodexNoWorkFloor(tc.override); got != tc.want {
				t.Errorf("codexNoWorkFloor(%v) = %v; want %v", tc.override, got, tc.want)
			}
		})
	}
}

// TestCodexNoWorkFloorDefault_SitsInTheMeasuredGap_hk368i4 asserts the shipped
// default separates the two measured populations. If someone re-tunes the floor
// on a hunch, this fails and points them back at the oracle.
//
// The bead is explicit that the sample is small and from one box, so this is
// NOT a claim that 10s is calibrated — only that whatever value ships must
// still separate the data we have.
func TestCodexNoWorkFloorDefault_SitsInTheMeasuredGap_hk368i4(t *testing.T) {
	t.Parallel()

	noWork := []time.Duration{
		3270 * time.Millisecond, 3380 * time.Millisecond, 4 * time.Second, 5190 * time.Millisecond,
	}
	realWork := []time.Duration{
		28900 * time.Millisecond, 31900 * time.Millisecond, 39300 * time.Millisecond,
		55300 * time.Millisecond, 44100 * time.Millisecond, 53500 * time.Millisecond,
	}

	floor := codex.ExportedCodexNoWorkDurationFloorDefault
	for _, d := range noWork {
		if d >= floor {
			t.Errorf("measured no-work run %v is NOT below the floor %v — the floor no longer separates the populations", d, floor)
		}
	}
	for _, d := range realWork {
		if d < floor {
			t.Errorf("measured real-work run %v IS below the floor %v — the floor would flag a real run", d, floor)
		}
	}
}
