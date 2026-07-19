package runexectest_test

// relaunch10_test.go — the RT11 N=10 clean relaunch oracle (RSM-030;
// liveness-parity-design §6 item (1)): ten CONSECUTIVE clean resumed-relaunch
// cycles on ONE FakeClock timeline, each a fresh Dispatch + Run machine pair
// driven by the synthesized resumed schedule with no fault injected. All ten
// must land Done{closed} success with exactly one resume_prompt delivery and
// one run terminal — all green, all virtual time.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/replay"
	"github.com/gregberns/harmonik/internal/substrate"
)

// corpusRunsDir is the pinned RT10 corpus (extract-run-corpus goldens).
func corpusRunsDir() string {
	return filepath.Join("..", "..", "testdata", "daemon-runs", "baseline-2026-07-14", "runs")
}

// loadSummariesFrom reads every *.summary.json under dir in stable name order.
func loadSummariesFrom(t *testing.T, dir string) []replay.RunSummary {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".summary.json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("no summaries under %s", dir)
	}
	out := make([]replay.RunSummary, 0, len(names))
	for _, n := range names {
		raw, err := os.ReadFile(filepath.Join(dir, n)) //nolint:gosec // G304: test-owned corpus testdata
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		var s replay.RunSummary
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("decode %s: %v", n, err)
		}
		out = append(out, s)
	}
	return out
}

// summaryForStratum returns the corpus summary for one stratum.
func summaryForStratum(t *testing.T, stratum string) replay.RunSummary {
	t.Helper()
	for _, s := range loadSummaries(t) {
		if s.Stratum == stratum {
			return s
		}
	}
	t.Fatalf("stratum %q not in corpus", stratum)
	return replay.RunSummary{}
}

// TestRunexecOracle_CleanRelaunchN10 runs the N=10 clean relaunch oracle.
func TestRunexecOracle_CleanRelaunchN10(t *testing.T) {
	t.Parallel()
	sum := summaryForStratum(t, "review-loop-resume")
	if !sum.Resumed {
		t.Fatalf("relaunch stratum must be a resumed run: %+v", sum)
	}
	clock := substrate.NewFakeClock(time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC))
	for i := 1; i <= 10; i++ {
		res := drive(t, clock, sum, substrate.FaultConfig{})
		if !res.RunDone || res.DoneOutcome != "closed" || !res.Success {
			t.Fatalf("relaunch %d/10 not green: %+v", i, res)
		}
		if res.RunTerminals != 1 {
			t.Fatalf("relaunch %d/10: want exactly one run terminal, got %d", i, res.RunTerminals)
		}
		if res.ResumeInputs != 1 {
			t.Fatalf("relaunch %d/10: want exactly one resume_prompt delivery, got %d", i, res.ResumeInputs)
		}
		if res.Reopens != 0 {
			t.Fatalf("relaunch %d/10: clean relaunch must not reopen, got %d", i, res.Reopens)
		}
		clock.Advance(time.Minute) // gap between consecutive relaunches
	}
}
