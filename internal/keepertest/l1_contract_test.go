package keepertest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/replay"
)

// TestL1_RecordedCorpusDecodesStrict runs the internal/replay harness over
// every recorded per-cycle .jsonl in STRICT mode (DecodePayloadStrict: an
// unknown type or unknown payload field is a hard finding) with the full
// checker set, and asserts the frozen-baseline characterization.
func TestL1_RecordedCorpusDecodesStrict(t *testing.T) {
	t.Parallel()
	dir := corpusCyclesDir(t)
	names := summaryFiles(t)
	if len(names) == 0 {
		t.Fatal("empty corpus")
	}

	var sr9Unterminated []string
	var otherViolations []string
	totalEvents := 0

	for _, name := range names {
		sum := loadSummary(t, filepath.Join(dir, name))
		cyclePath := filepath.Join(dir, strings.TrimSuffix(name, ".summary.json")+".jsonl")

		rep, err := replay.Replay(cyclePath, core.EventID{}, true, replay.DefaultCheckers())
		if err != nil {
			t.Fatalf("%s: strict replay failed (writer/reader drift?): %v", name, err)
		}
		if rep.Events != sum.EventCount {
			t.Errorf("%s: replay saw %d events, summary says %d", name, rep.Events, sum.EventCount)
		}
		if rep.Skipped != 0 || rep.Malformed != 0 {
			t.Errorf("%s: skipped=%d malformed=%d, want 0/0", name, rep.Skipped, rep.Malformed)
		}
		if len(rep.SchemaMismatches) != 0 {
			t.Errorf("%s: %d envelope schema mismatches", name, len(rep.SchemaMismatches))
		}
		totalEvents += rep.Events
		for _, v := range rep.Violations {
			if v.Rule == "SR9" && strings.Contains(v.Detail, "unterminated cycle") {
				sr9Unterminated = append(sr9Unterminated, sum.CKey)
			} else {
				otherViolations = append(otherViolations, name+": "+v.Rule+": "+v.Detail)
			}
		}
	}

	if len(sr9Unterminated) != 1 || sr9Unterminated[0] != knownUnterminatedCKey {
		t.Fatalf("recorded-baseline SR9 unterminated = %v, want exactly [%s]",
			sr9Unterminated, knownUnterminatedCKey)
	}
	if len(otherViolations) != 0 {
		t.Fatalf("recorded baseline has unexpected violations:\n%s", strings.Join(otherViolations, "\n"))
	}
	if totalEvents == 0 {
		t.Fatal("no events decoded from corpus")
	}
}

func wantReplayOutcome(t *testing.T, sum keepertwin.CycleSummary) cycleOutcome {
	t.Helper()
	stratum, err := keepertwin.Classify(sum)
	if err != nil {
		t.Fatalf("classify %s: %v", sum.CKey, err)
	}
	switch stratum {
	case keepertwin.StratumCleanComplete:
		return outcomeComplete
	case keepertwin.StratumDegradedComplete:
		return outcomeDegradedComplete
	case keepertwin.StratumAbortHandoffTimeout:
		return outcomeParkedPending
	case keepertwin.StratumUnterminated:
		if sum.CKey != knownUnterminatedCKey {
			t.Fatalf("unexpected unterminated cycle %s (baseline pins exactly one: %s)",
				sum.CKey, knownUnterminatedCKey)
		}
		return outcomeDegradedComplete
	default:
		t.Fatalf("unknown stratum %q", stratum)
		return ""
	}
}

// TestL1_GoldenOutcomes replays ALL 507 cycles and asserts each golden.
func TestL1_GoldenOutcomes(t *testing.T) {
	t.Parallel()
	sums := allSummaries(t)
	if len(sums) != 507 {
		t.Fatalf("corpus has %d cycles, want 507 (D7 frozen anchor)", len(sums))
	}

	got := map[cycleOutcome]int{}
	for _, sum := range sums {
		actions := flatReplayCycle(t, sum)
		outcome := soleOutcome(t, actions, sum.CKey)
		if want := wantReplayOutcome(t, sum); outcome != want {
			t.Errorf("%s: outcome = %s, want %s (emitted %v)",
				sum.CKey, outcome, want, emittedTypes(actions))
		}
		got[outcome]++
	}

	want := map[cycleOutcome]int{
		outcomeComplete:         80,
		outcomeDegradedComplete: 348,
		outcomeParkedPending:    79,
	}
	if len(got) != len(want) {
		t.Fatalf("aggregate replay produced outcomes %v, want exactly %v", got, want)
	}
	for outcome, n := range want {
		if got[outcome] != n {
			t.Fatalf("aggregate replay = %v, want %v", got, want)
		}
	}
}

// TestL1_ReplayedStreamInvariants re-envelopes the full replayed emitted-event
// stream into a temp events.jsonl and runs the internal/replay harness over
// it (strict + full checkers): the NEW reactor's output must carry ZERO SR
// violations — in particular zero SR9 unterminated (§7 metric 3: 1 → 0).
func TestL1_ReplayedStreamInvariants(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	written := writeReplayedStream(t, path)

	rep, err := replay.Replay(path, core.EventID{}, true, replay.DefaultCheckers())
	if err != nil {
		t.Fatalf("strict replay of the replayed stream failed: %v", err)
	}
	if len(rep.Violations) != 0 {
		msgs := make([]string, 0, len(rep.Violations))
		for _, v := range rep.Violations {
			msgs = append(msgs, v.Rule+"/"+v.CycleID+": "+v.Detail)
		}
		t.Fatalf("replayed stream has %d SR violations (want 0 — SR9 fix + SR3/4/6/7):\n%s",
			len(rep.Violations), strings.Join(msgs, "\n"))
	}
	if rep.Skipped != 0 || rep.Malformed != 0 || len(rep.SchemaMismatches) != 0 {
		t.Fatalf("replayed stream: skipped=%d malformed=%d mismatches=%d, want all 0",
			rep.Skipped, rep.Malformed, len(rep.SchemaMismatches))
	}
	if rep.Events != written {
		t.Fatalf("replay saw %d events, wrote %d", rep.Events, written)
	}
}
