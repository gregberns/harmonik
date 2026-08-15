package keepertest_test

// Shared helpers for the keeper L0–L3 tiers (T10; measurement-design §3).
// Corpus-path resolution follows the codex l1 idiom (runtime.Caller-relative;
// RS-019: the idiom is part of the copied template, not substrate code).

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/keepertwin"
	"github.com/gregberns/harmonik/internal/substrate"
)

// knownUnterminatedCKey is the ONE recorded unterminated baseline cycle
// (measurement-design §2.4 / §7 metric 3). The NEW reactor must FIX it —
// terminate within bound — never match the old wedge.
const knownUnterminatedCKey = "kk-test|cyc-20260610T215853-000004"

// corpusRoot resolves testdata/keeper-cycles/baseline-2026-07-13 relative to
// this source file.
func corpusRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..",
		"testdata", "keeper-cycles", "baseline-2026-07-13")
}

// corpusCyclesDir resolves the per-cycle corpus directory.
func corpusCyclesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(corpusRoot(t), "cycles")
}

// loadSummary reads one golden summary.json.
func loadSummary(t *testing.T, path string) keepertwin.CycleSummary {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // G304: test-owned corpus testdata
	if err != nil {
		t.Fatalf("read summary %s: %v", path, err)
	}
	var sum keepertwin.CycleSummary
	if err := json.Unmarshal(raw, &sum); err != nil {
		t.Fatalf("parse summary %s: %v", path, err)
	}
	return sum
}

// summaryFiles returns the sorted list of *.summary.json basenames.
func summaryFiles(t *testing.T) []string {
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
	return names
}

// allSummaries loads every corpus cycle summary, sorted by filename.
func allSummaries(t *testing.T) []keepertwin.CycleSummary {
	t.Helper()
	dir := corpusCyclesDir(t)
	names := summaryFiles(t)
	sums := make([]keepertwin.CycleSummary, 0, len(names))
	for _, name := range names {
		sums = append(sums, loadSummary(t, filepath.Join(dir, name)))
	}
	return sums
}

// pickPerStratum returns the lexically-first corpus cycle of each stratum.
func pickPerStratum(t *testing.T) map[keepertwin.Stratum]keepertwin.CycleSummary {
	t.Helper()
	dir := corpusCyclesDir(t)
	picked := make(map[keepertwin.Stratum]keepertwin.CycleSummary, 4)
	for _, name := range summaryFiles(t) {
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
		t.Fatalf("corpus missing strata: picked %d of 4", len(picked))
	}
	return picked
}

// testConfig is the explicit-scalar reactor config for replay (NewCycle does
// not apply defaults; values mirror keeper's documented defaults).
func testConfig(agent string) *keeper.CyclerConfig {
	return &keeper.CyclerConfig{
		AgentName:            agent,
		TmuxTarget:           "keepertest:0", // non-empty → full injection action sequence
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

// flatReplayCycle replays sum through the FLAT pipe (pre-scheduled TimerFired
// lines, T9 shape): corpus summary → SynthesizeStimulus → EncodeStimulus →
// keepertwin.Twin → pure reactor → FakeEffector. Boundary-outcome fidelity
// only (measurement-design §2.2 note): interior re-inject counts are NOT
// live-faithful on this path — the L2 discrete-event harness owns those.
func flatReplayCycle(t *testing.T, sum keepertwin.CycleSummary) []keeper.Action {
	t.Helper()
	events, err := keepertwin.SynthesizeStimulus(sum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", sum.CKey, err)
	}
	raw, err := keepertwin.EncodeStimulus(events)
	if err != nil {
		t.Fatalf("encode %s: %v", sum.CKey, err)
	}

	// Wall-clock backstop only: converts a genuine code-hang into a failure.
	// The replay itself is virtual-time and completes in microseconds.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	twin := keepertwin.New(bytes.NewReader(raw), keepertwin.FaultConfig{})
	cyc := keeper.NewCycle(testConfig(sum.AgentName))
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

// emittedTypes collects the emitted event types from recorded actions.
func emittedTypes(actions []keeper.Action) []core.EventType {
	var types []core.EventType
	for _, a := range actions {
		if a.Kind == keeper.ActEmit {
			types = append(types, a.Type)
		}
	}
	return types
}

// countType counts occurrences of one emitted event type.
func countType(types []core.EventType, want core.EventType) int {
	n := 0
	for _, tp := range types {
		if tp == want {
			n++
		}
	}
	return n
}

// ─── Cycle outcomes ──────────────────────────────────────────────────────────

// cycleOutcome is what ONE keeper cycle ended with, carried as one value.
//
// The tiers used to carry this as a pair of booleans (wantComplete,
// wantUnconfirmed). That pair can spell (complete=false, unconfirmed=true),
// which names nothing the machine has ever produced, and it cannot spell a
// park at all — so when the handoff-timeout edge stopped aborting and started
// parking (session-keeper.md §8.4), every assertion built on it read the new
// behavior as "no outcome". One value with four inhabitants removes both
// problems (PRINCIPLES §2).
type cycleOutcome string

const (
	// outcomeComplete is the clean terminal (§8.1): /clear confirmed by a new
	// session id, brief injected.
	outcomeComplete cycleOutcome = "cycle_complete"
	// outcomeDegradedComplete is the §8.3 degraded terminal: the clear
	// backstop expired without a session change, and the cycle completes
	// anyway. Still exactly one terminal.
	outcomeDegradedComplete cycleOutcome = "cycle_complete+clear_unconfirmed"
	// outcomeParkedPending is the §8.4 SUSPENSION: the observation window
	// closed before a marked handoff arrived. The request stays live and the
	// SAME cycle id can resume and complete later (SK-025).
	outcomeParkedPending cycleOutcome = "cycle_parked{handoff_pending}"
	// outcomeParkedOperator is the §8.4 final park: a recent real operator
	// turn holds the restart effects back (SK-026). This cycle does not
	// resume; the next one mints a fresh id.
	outcomeParkedOperator cycleOutcome = "cycle_parked{operator_turn_recent}"
)

// isCompletion reports whether an outcome is one of the two completions. Use
// it where the claim is "the destructive tail finished", clean or degraded.
func isCompletion(o cycleOutcome) bool {
	return o == outcomeComplete || o == outcomeDegradedComplete
}

// parkReason decodes the reason a cycle_parked emit carries and refuses any
// value outside the §8.4 pair. The reason is not a detail on this event: it is
// what separates a suspension the same cycle id resumes from an end, so a park
// with an empty or unknown reason is a defect and fails here.
func parkReason(t *testing.T, a keeper.Action) string {
	t.Helper()
	var p core.SessionKeeperCycleParkedPayload
	if err := json.Unmarshal(a.Payload, &p); err != nil {
		t.Fatalf("decode parked payload: %v", err)
	}
	switch p.Reason {
	case "handoff_pending", "operator_turn_recent":
		return p.Reason
	default:
		t.Fatalf("cycle_parked reason = %q, want handoff_pending or operator_turn_recent "+
			"(§8.4: those two are the complete set; any other value is a defect)", p.Reason)
		return ""
	}
}

// cycleEndings reduces an emitted action stream to the outcomes it recorded,
// in emit order. An ending is any emit that returns the machine to Idle:
// cycle_complete, cycle_parked (either flavor), and cycle_aborted.
//
// cycle_aborted has had no producer since keeper checkpoints became
// agent-paced (§8.2). It is decoded here anyway, with its reason, so that a
// regression which revives it is NAMED in the failure message instead of
// arriving as an unexplained extra outcome.
func cycleEndings(t *testing.T, actions []keeper.Action) []cycleOutcome {
	t.Helper()
	var out []cycleOutcome
	unconfirmed := false
	for _, a := range actions {
		if a.Kind != keeper.ActEmit {
			continue
		}
		switch a.Type {
		case core.EventTypeSessionKeeperClearUnconfirmed:
			unconfirmed = true
		case core.EventTypeSessionKeeperCycleComplete:
			if unconfirmed {
				out = append(out, outcomeDegradedComplete)
			} else {
				out = append(out, outcomeComplete)
			}
			unconfirmed = false
		case core.EventTypeSessionKeeperCycleParked:
			out = append(out, cycleOutcome("cycle_parked{"+parkReason(t, a)+"}"))
		case core.EventTypeSessionKeeperCycleAborted:
			var p core.SessionKeeperCycleAbortedPayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode aborted payload: %v", err)
			}
			out = append(out, cycleOutcome("cycle_aborted{"+p.Reason+"}"))
		default:
			// Interior events (handoff_started, handoff_written, model_done,
			// clear_sent, new_session_up, cycle_recovered) end nothing.
		}
	}
	return out
}

// soleOutcome asserts the stream recorded exactly one cycle ending and returns
// it. Zero endings is the silence SK-INV-005 forbids; two is an overlap.
func soleOutcome(t *testing.T, actions []keeper.Action, ckey string) cycleOutcome {
	t.Helper()
	got := cycleEndings(t, actions)
	if len(got) != 1 {
		t.Fatalf("%s: want exactly 1 cycle ending, got %d %v (emitted %v)",
			ckey, len(got), got, emittedTypes(actions))
	}
	return got[0]
}

// assertOutcome asserts the stream recorded exactly one cycle ending and that
// it is want.
func assertOutcome(t *testing.T, actions []keeper.Action, ckey string, want cycleOutcome) {
	t.Helper()
	if got := soleOutcome(t, actions, ckey); got != want {
		t.Fatalf("%s: outcome = %s, want %s (emitted %v)", ckey, got, want, emittedTypes(actions))
	}
}

// writeReplayedStream replays ALL corpus cycles through the flat pipe and
// re-envelopes every emitted event into an events.jsonl at path (the T10
// envelope-writer, shared by TestL1_ReplayedStreamInvariants and the T13
// out-of-band metrics export). Envelopes carry deterministic, ordering-
// controlled EventIDs and virtual timestamps so the file is byte-stable
// across runs. Returns the number of envelopes written.
func writeReplayedStream(t *testing.T, path string) int {
	t.Helper()
	sums := allSummaries(t)
	versions := core.AllPayloadSchemaVersions()

	f, err := os.Create(path) //nolint:gosec // G304: test-owned output path
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
			ver, ok := versions[a.Type]
			if !ok {
				t.Fatalf("%s: reactor emitted unregistered event type %q", sum.CKey, a.Type)
			}
			seq++
			ev := core.Event{
				EventID:         mkEventID(seq),
				SchemaVersion:   ver,
				Type:            a.Type,
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
	return int(seq)
}

// mkEventID builds a deterministic, ordering-controlled UUIDv7-shaped EventID
// whose big-endian counter in the leading bytes drives the internal/replay
// EventID sort (the replay_test fixture idiom, widened to a uint32 counter).
func mkEventID(seq uint32) core.EventID {
	var b [16]byte
	binary.BigEndian.PutUint32(b[:4], seq)
	b[6] = 0x70 // version 7 nibble (cosmetic; the harness sorts on raw bytes)
	b[8] = 0x80 // RFC 4122 variant
	return core.EventID(uuid.UUID(b))
}
