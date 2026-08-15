package keepertwin_test

// T9 round-trip smoke test (measurement-design §2): for one real corpus cycle
// per stratum, synthesize the input schedule, encode it, replay it through
// substrate.Twin via keeperCodec, drive the pure keeper reactor, and assert a
// terminal is reached (never silence) with the golden outcome. The exhaustive
// 507-cycle L1 golden test is T10; this proves the corpus → synthesizer →
// codec → Twin → reactor pipe end to end.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/substrate"
)

// corpusCyclesDir resolves testdata/keeper-cycles/baseline-2026-07-13/cycles
// relative to this source file (the codex l1 idiom).
func corpusCyclesDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..",
		"testdata", "keeper-cycles", "baseline-2026-07-13", "cycles")
}

func loadSummary(t *testing.T, path string) keepertwin.CycleSummary {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path is test-owned corpus testdata
	if err != nil {
		t.Fatalf("read summary %s: %v", path, err)
	}
	var sum keepertwin.CycleSummary
	if err := json.Unmarshal(raw, &sum); err != nil {
		t.Fatalf("parse summary %s: %v", path, err)
	}
	return sum
}

// pickPerStratum returns the lexically-first corpus cycle of each stratum.
func pickPerStratum(t *testing.T) map[keepertwin.Stratum]keepertwin.CycleSummary {
	t.Helper()
	dir := corpusCyclesDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir %s (run scripts/extract-keeper-corpus.py?): %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".summary.json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	picked := make(map[keepertwin.Stratum]keepertwin.CycleSummary, 4)
	for _, name := range names {
		if len(picked) == 4 {
			break
		}
		sum := loadSummary(t, filepath.Join(dir, name))
		stratum, err := keepertwin.Classify(sum)
		if err != nil {
			t.Fatalf("classify %s: %v", name, err)
		}
		if _, ok := picked[stratum]; !ok {
			picked[stratum] = sum
		}
	}
	if len(picked) != 4 {
		t.Fatalf("corpus missing strata: picked %d of 4 (%v)", len(picked), picked)
	}
	return picked
}

// twinConfig is the explicit-scalar reactor config for replay (NewCycle does
// not apply defaults; the values mirror keeper's documented defaults).
func twinConfig(agent string) *keeper.CyclerConfig {
	return &keeper.CyclerConfig{
		AgentName:            agent,
		TmuxTarget:           "twin:0", // non-empty → full injection action sequence
		ActPct:               90,
		WarnPct:              80,
		ForceActPct:          95,
		HandoffTimeout:       keeper.DefaultHandoffTimeout,
		ClearSettle:          keeper.DefaultClearSettle,
		ClearConfirmBackstop: keeper.DefaultClearConfirmBackstop,
		ClearConfirmRetries:  keeper.DefaultClearConfirmRetries,
		ModelDoneTimeout:     keeper.DefaultModelDoneTimeout,
	}
}

// runCycle replays sum through the full pipe and returns the recorded actions.
func runCycle(t *testing.T, sum keepertwin.CycleSummary) []keeper.Action {
	t.Helper()
	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", sum.CKey, err)
	}
	raw, err := keepertwin.EncodeStimulus(events)
	if err != nil {
		t.Fatalf("encode %s: %v", sum.CKey, err)
	}

	// Wall-clock backstop only (converts a genuine code-hang into a failure;
	// the replay itself is virtual-time and completes in microseconds).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	twin := keepertwin.New(bytes.NewReader(raw), keepertwin.FaultConfig{})
	cyc := keeper.NewCycle(twinConfig(sum.AgentName))
	eff := &substrate.FakeEffector[keeper.Action]{}
	if err := cyc.Run(ctx, twin, eff); err != nil {
		t.Fatalf("run %s: %v", sum.CKey, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("run %s: wall-clock backstop hit (silence bug)", sum.CKey)
	}
	if cyc.InCycle() {
		t.Fatalf("run %s: reactor still in-cycle after stimulus exhausted (no terminal)", sum.CKey)
	}
	return eff.Actions()
}

// emittedTypes collects the emitted event types from the recorded actions.
func emittedTypes(actions []keeper.Action) []core.EventType {
	var types []core.EventType
	for _, a := range actions {
		if a.Kind == keeper.ActEmit {
			types = append(types, a.Type)
		}
	}
	return types
}

func countType(types []core.EventType, want core.EventType) int {
	n := 0
	for _, tp := range types {
		if tp == want {
			n++
		}
	}
	return n
}

func TestTwinRoundTrip_TerminalPerStratum(t *testing.T) {
	picked := pickPerStratum(t)

	cases := []struct {
		stratum         keepertwin.Stratum
		wantComplete    bool
		wantUnconfirmed bool
		// wantParked says the replay must NOT terminate. The cycle suspends
		// with cycle_parked{handoff_pending} and stays resumable under the same
		// cycle id. cycle_complete must not appear on such a replay.
		wantParked bool
	}{
		{stratum: keepertwin.StratumCleanComplete, wantComplete: true},
		{stratum: keepertwin.StratumDegradedComplete, wantComplete: true, wantUnconfirmed: true},
		// The 79 recorded handoff-timeout cycles. The OLD keeper terminated
		// them; the NEW one parks them (see synthesizer.go, the
		// abort_handoff_timeout row).
		{stratum: keepertwin.StratumAbortHandoffTimeout, wantParked: true},
		// The 1 recorded unterminated cycle: NEW must terminate within bound —
		// the clear_backstop converts the old wedge into a degraded completion
		// (SR9 fix; required divergence per measurement-design §4).
		{stratum: keepertwin.StratumUnterminated, wantComplete: true, wantUnconfirmed: true},
	}
	for _, tc := range cases {
		t.Run(string(tc.stratum), func(t *testing.T) {
			sum := picked[tc.stratum]
			types := emittedTypes(runCycle(t, sum))

			complete := countType(types, core.EventTypeSessionKeeperCycleComplete)
			parked := countType(types, core.EventTypeSessionKeeperCycleParked)
			unconfirmed := countType(types, core.EventTypeSessionKeeperClearUnconfirmed)

			if tc.wantComplete && complete != 1 {
				t.Fatalf("%s: want exactly 1 cycle_complete, got %d (types %v)",
					sum.CKey, complete, types)
			}
			if tc.wantParked {
				if parked != 1 {
					t.Fatalf("%s: want exactly 1 cycle_parked, got %d (types %v)",
						sum.CKey, parked, types)
				}
				if complete != 0 {
					t.Errorf("%s: a parked cycle must not also complete; got %d cycle_complete (types %v)",
						sum.CKey, complete, types)
				}
			} else if parked != 0 {
				t.Errorf("%s: unexpected cycle_parked (%d) (types %v)", sum.CKey, parked, types)
			}
			if tc.wantUnconfirmed && unconfirmed != 1 {
				t.Errorf("%s: want clear_unconfirmed, got %d", sum.CKey, unconfirmed)
			}
			if !tc.wantUnconfirmed && unconfirmed != 0 {
				t.Errorf("%s: unexpected clear_unconfirmed", sum.CKey)
			}
		})
	}
}

// TestTwinRoundTrip_HandoffTimeoutParksPendingOnTheSameCycleID asserts that a
// recorded handoff-timeout cycle replays to a SUSPENSION, not a terminal: one
// cycle_parked, reason handoff_pending, on the recorded cycle id.
//
// COVERAGE LOST HERE, on purpose. This test used to be
// TestTwinRoundTrip_AbortReason, and it round-tripped a RECORDED value: the
// corpus summary.json says abort_reason "handoff_timeout" for all 79 of these
// cycles, and the test read that recorded string back out of the replayed
// payload. That is gone. The new emission carries "handoff_pending", which the
// reactor invents at the park edge; no field of the corpus produced it. So the
// reason assertion below is now a constant check, and only the cycle id is
// still round-tripped from the recording. Metric 4 of measurement-design
// ("aborts always explicit-reasoned") no longer has a test.
func TestTwinRoundTrip_HandoffTimeoutParksPendingOnTheSameCycleID(t *testing.T) {
	sum := pickPerStratum(t)[keepertwin.StratumAbortHandoffTimeout]
	found := 0
	for _, a := range runCycle(t, sum) {
		if a.Kind != keeper.ActEmit || a.Type != core.EventTypeSessionKeeperCycleParked {
			continue
		}
		found++
		var p core.SessionKeeperCycleParkedPayload
		if err := json.Unmarshal(a.Payload, &p); err != nil {
			t.Fatalf("decode parked payload: %v", err)
		}
		if p.Reason != "handoff_pending" {
			t.Fatalf("park reason = %q, want handoff_pending (the resumable flavor)", p.Reason)
		}
		// The one value still carried across from the recording.
		if p.CycleID != sum.CycleID {
			t.Fatalf("park cycle_id = %q, want the recorded %q", p.CycleID, sum.CycleID)
		}
	}
	if found != 1 {
		t.Fatalf("want exactly 1 cycle_parked emit, got %d", found)
	}
}

// TestTwinRoundTrip_SR4Ordering asserts the synthesized replay preserves SR4:
// no InjectClear action before the model_done emit.
func TestTwinRoundTrip_SR4Ordering(t *testing.T) {
	sum := pickPerStratum(t)[keepertwin.StratumCleanComplete]
	modelDoneSeen := false
	for _, a := range runCycle(t, sum) {
		switch {
		case a.Kind == keeper.ActEmit && a.Type == core.EventTypeSessionKeeperModelDone:
			modelDoneSeen = true
		case a.Kind == keeper.ActInjectClear && !modelDoneSeen:
			t.Fatal("SR4 violated: InjectClear before model_done emit")
		}
	}
	if !modelDoneSeen {
		t.Fatal("no model_done emit in clean-complete replay")
	}
}

// TestKeeperCodec_MalformedLineFatal asserts a corrupt stimulus line is FATAL:
// the twin emits the transport-error event and closes (never silence).
func TestKeeperCodec_MalformedLineFatal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	twin := keepertwin.New(strings.NewReader("this is not json\n"), keepertwin.FaultConfig{})
	got := make([]keeper.Event, 0, 2)
	for ev := range twin.Events(ctx) {
		got = append(got, ev)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 event (transport error), got %d: %v", len(got), got)
	}
	if got[0].Kind != keepertwin.EvTwinTransportError {
		t.Fatalf("want %s, got %s", keepertwin.EvTwinTransportError, got[0].Kind)
	}
}

// TestSynthesize_UnknownStratumRejected asserts an out-of-vocabulary summary is
// an error, never silently synthesized.
func TestSynthesize_UnknownStratumRejected(t *testing.T) {
	_, err := keepertwin.SynthesizeStimulus(keepertwin.CycleSummary{
		CKey: "x|y", CycleID: "y", Outcome: "aborted", AbortReason: "mystery",
	})
	if err == nil {
		t.Fatal("want error for unknown abort reason, got nil")
	}
}
