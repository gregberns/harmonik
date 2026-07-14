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
//     summary.json outcome + clear_unconfirmed flag. The ONE recorded
//     unterminated cycle asserts the FIXED behavior (SR9: terminal within
//     bound — complete + clear_unconfirmed), never the old wedge
//     (measurement-design §4 required-divergence).
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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
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
// must produce. Identity for the three terminal strata; the ONE unterminated
// cycle maps onto the SR9 FIX: the clear backstop converts the old wedge into
// a bounded degraded completion.
func wantReplayOutcome(t *testing.T, sum keepertwin.CycleSummary) (wantComplete, wantUnconfirmed bool) {
	t.Helper()
	stratum, err := keepertwin.Classify(sum)
	if err != nil {
		t.Fatalf("classify %s: %v", sum.CKey, err)
	}
	switch stratum {
	case keepertwin.StratumCleanComplete:
		return true, false
	case keepertwin.StratumDegradedComplete:
		return true, true
	case keepertwin.StratumAbortHandoffTimeout:
		return false, false
	case keepertwin.StratumUnterminated:
		// Required divergence (measurement-design §4): NEW terminates within
		// bound. Matching the old unterminated behavior would be a FAILURE.
		if sum.CKey != knownUnterminatedCKey {
			t.Fatalf("unexpected unterminated cycle %s (baseline pins exactly one: %s)",
				sum.CKey, knownUnterminatedCKey)
		}
		return true, true
	default:
		t.Fatalf("unknown stratum %q", stratum)
		return false, false
	}
}

// TestL1_GoldenOutcomes replays ALL 507 cycles and asserts each golden.
func TestL1_GoldenOutcomes(t *testing.T) {
	t.Parallel()
	sums := allSummaries(t)
	if len(sums) != 507 {
		t.Fatalf("corpus has %d cycles, want 507 (D7 frozen anchor)", len(sums))
	}

	gotComplete, gotAborted, gotUnconfirmed := 0, 0, 0
	for _, sum := range sums {
		wantComplete, wantUnconfirmed := wantReplayOutcome(t, sum)
		types := emittedTypes(flatReplayCycle(t, sum))

		complete := countType(types, core.EventTypeSessionKeeperCycleComplete)
		aborted := countType(types, core.EventTypeSessionKeeperCycleAborted)
		unconfirmed := countType(types, core.EventTypeSessionKeeperClearUnconfirmed)

		if complete+aborted != 1 {
			t.Fatalf("%s: want exactly 1 terminal, got complete=%d aborted=%d", sum.CKey, complete, aborted)
		}
		if wantComplete != (complete == 1) {
			t.Errorf("%s: terminal = complete:%v, want complete:%v", sum.CKey, complete == 1, wantComplete)
		}
		if wantUnconfirmed != (unconfirmed == 1) {
			t.Errorf("%s: clear_unconfirmed = %d, want present:%v", sum.CKey, unconfirmed, wantUnconfirmed)
		}
		gotComplete += complete
		gotAborted += aborted
		if unconfirmed > 0 {
			gotUnconfirmed++
		}
	}

	// Aggregate goldens = the frozen anchors shifted by the SR9 fix:
	// 427 recorded completes + the 1 fixed unterminated cycle; 347 recorded
	// degraded + the same fixed cycle (its bounded terminal is degraded).
	if gotComplete != 428 || gotAborted != 79 || gotUnconfirmed != 348 {
		t.Fatalf("aggregate replay = complete:%d aborted:%d degraded:%d, want 428/79/348",
			gotComplete, gotAborted, gotUnconfirmed)
	}
}

// TestL1_ReplayedStreamInvariants re-envelopes the full replayed emitted-event
// stream into a temp events.jsonl and runs the internal/replay harness over
// it (strict + full checkers): the NEW reactor's output must carry ZERO SR
// violations — in particular zero SR9 unterminated (§7 metric 3: 1 → 0).
func TestL1_ReplayedStreamInvariants(t *testing.T) {
	t.Parallel()
	sums := allSummaries(t)
	versions := core.AllPayloadSchemaVersions()

	path := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(path) //nolint:gosec // G304: t.TempDir-scoped
	if err != nil {
		t.Fatalf("create replayed log: %v", err)
	}
	enc := json.NewEncoder(f)

	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	var seq uint32
	for _, sum := range sums {
		for _, a := range flatReplayCycle(t, sum) {
			if a.Kind != keeper.ActEmit {
				continue
			}
			ver, ok := versions[string(a.Type)]
			if !ok {
				t.Fatalf("%s: reactor emitted unregistered event type %q", sum.CKey, a.Type)
			}
			seq++
			ev := core.Event{
				EventID:         mkEventID(seq),
				SchemaVersion:   ver,
				Type:            string(a.Type),
				TimestampWall:   base.Add(time.Duration(seq) * time.Millisecond),
				SourceSubsystem: "internal/keeper",
				Payload:         json.RawMessage(a.Payload),
			}
			if err := enc.Encode(&ev); err != nil {
				t.Fatalf("encode envelope: %v", err)
			}
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close replayed log: %v", err)
	}

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
	if rep.Events != int(seq) {
		t.Fatalf("replay saw %d events, wrote %d", rep.Events, seq)
	}
}
