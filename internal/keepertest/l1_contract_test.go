package keepertest_test

// L1 contract tier — the 507-cycle corpus golden replay (T10; RS-017 L1;
// measurement-design §3 "L1 contract" row). This is the PERMANENT regression
// net (D13): it needs only the new reactor, no old-path scaffold.
//
// Three contracts:
//
//  1. TestL1_RecordedCorpusDecodesStrict — every recorded OUTPUT envelope in
//     the corpus decodes through the internal/replay typed-decode harness
//     (EV-U3/D6) in STRICT mode, and the SR checkers characterize the frozen
//     baseline exactly: ONE SR9 unterminated violation (the known
//     kk-test cycle), zero other violations.
//
//  2. TestL1_GoldenOutcomes — every cycle's synthesized INPUT schedule
//     replayed through Twin → reactor → FakeEffector reproduces its golden
//     summary.json outcome. Two strata assert a FIXED behavior instead of the
//     recorded one (measurement-design §4 required-divergence): the ONE
//     unterminated cycle must terminate within bound (SR9 — complete +
//     clear_unconfirmed) rather than wedge, and the 79 handoff-timeout aborts
//     must SUSPEND (cycle_parked{handoff_pending}, same cycle id still
//     eligible) rather than fail, because a late handoff is pending work and
//     not a failure (SK-025, session-keeper.md §8.2/§8.4).
//
//  3. TestL1_ReplayedStreamInvariants — the full replayed emitted-event
//     stream, re-enveloped and written to an events.jsonl, passes
//     replay.Replay in strict mode with the full SR3/SR4/SR6/SR7/SR9 checker
//     set with ZERO violations, and its aggregate counts match the frozen
//     anchors shifted by the SR9 fix (supports the §7 metric commands 2–6).
//
// Replay mode: FLAT stimulus schedules (pre-scheduled TimerFired lines, the
// T9 shape). Justification (measurement-design §2.2 note): these L1 goldens
// are BOUNDARY goldens — terminal outcome, degraded flag, and interior
// FIRST-OCCURRENCE order (which the flat schedule preserves: handoff_written
// < model_done < clear_sent < terminal). They deliberately do NOT assert
// per-cycle interior attempt COUNTS (clear_settle re-injects), which the flat
// schedule cannot reproduce — those live in the L2 discrete-event tier.

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

	// Frozen-baseline characterization: exactly the ONE known unterminated
	// cycle, nothing else (manifest anchors: unterminated:1, 0 dup terminals).
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

// wantReplayOutcome maps a golden summary onto the outcome the NEW reactor
// must produce. Identity for the two completion strata; the other two are
// REQUIRED DIVERGENCES from what the corpus recorded (measurement-design §4),
// and reproducing the recorded behavior would be a failure in both.
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
		// Required divergence: the recorded cycle ABORTED when the marked
		// handoff did not arrive in the window. A late handoff is pending
		// work and not a failure (SK-025), so the new reactor SUSPENDS the
		// same request instead — cycle_parked{handoff_pending}, no /clear, and
		// the same cycle id stays eligible to resume. A cycle_aborted here
		// would mean the retired producer came back (§8.2).
		return outcomeParkedPending
	case keepertwin.StratumUnterminated:
		// Required divergence: NEW terminates within bound. Matching the old
		// unterminated behavior would be a FAILURE.
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

	// The population is counted from what the reactor ACTUALLY emitted, never
	// from the per-cycle expectation just checked, so the aggregate stays an
	// independent anchor rather than a restatement of the strata.
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

	// Aggregate goldens, derived from the frozen manifest anchors (canary_test
	// frozenAnchors: 507 cycles = 80 clean + 347 degraded + 79 handoff-timeout
	// aborts + 1 unterminated) and the two required divergences:
	//
	//	clean completes           80 = 80 recorded clean
	//	degraded completes       348 = 347 recorded degraded + the 1 fixed
	//	                               unterminated cycle, whose bounded
	//	                               terminal is a degraded completion (SR9)
	//	parked{handoff_pending}   79 = the 79 recorded aborts, which now
	//	                               suspend instead of failing (SK-025)
	//
	// The old anchor read 428 completes / 79 aborted / 348 degraded. 428 was
	// 80+348 counted as one bucket, and the 79 moved from aborted to parked;
	// no cycle changed which PATH it takes, only what the tail of that path
	// is named.
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
