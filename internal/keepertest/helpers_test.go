package keepertest_test

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

const knownUnterminatedCKey = "kk-test|cyc-20260610T215853-000004"

func corpusRoot(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..",
		"testdata", "keeper-cycles", "baseline-2026-07-13")
}

func corpusCyclesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(corpusRoot(t), "cycles")
}

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

type cycleOutcome string

const (
	outcomeComplete cycleOutcome = "cycle_complete"
	// outcomeClearUnconfirmed is a terminal failed restart. The keeper sent
	// one clear, but it did not observe a new session. It must not send a brief
	// or claim completion.
	outcomeClearUnconfirmed cycleOutcome = "cycle_aborted{clear_unconfirmed}"
	// outcomeParkedPending is the §8.4 SUSPENSION: the observation window
	// closed before a marked handoff arrived. The request stays live and the
	// SAME cycle id can resume and complete later (SK-025).
	outcomeParkedPending cycleOutcome = "cycle_parked{handoff_pending}"
	// outcomeParkedOperator is the §8.4 final park: a recent real operator
	// turn holds the restart effects back (SK-026). This cycle does not
	// resume; the next one mints a fresh id.
	outcomeParkedOperator cycleOutcome = "cycle_parked{operator_turn_recent}"
)

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

func cycleEndings(t *testing.T, actions []keeper.Action) []cycleOutcome {
	t.Helper()
	var out []cycleOutcome
	for _, a := range actions {
		if a.Kind != keeper.ActEmit {
			continue
		}
		switch a.Type {
		case core.EventTypeSessionKeeperClearUnconfirmed:
			// This is the diagnostic observation. cycle_aborted is the terminal.
		case core.EventTypeSessionKeeperCycleComplete:
			out = append(out, outcomeComplete)
		case core.EventTypeSessionKeeperCycleParked:
			out = append(out, cycleOutcome("cycle_parked{"+parkReason(t, a)+"}"))
		case core.EventTypeSessionKeeperCycleAborted:
			var p core.SessionKeeperCycleAbortedPayload
			if err := json.Unmarshal(a.Payload, &p); err != nil {
				t.Fatalf("decode aborted payload: %v", err)
			}
			out = append(out, cycleOutcome("cycle_aborted{"+p.Reason+"}"))
		default:
		}
	}
	return out
}

func soleOutcome(t *testing.T, actions []keeper.Action, ckey string) cycleOutcome {
	t.Helper()
	got := cycleEndings(t, actions)
	if len(got) != 1 {
		t.Fatalf("%s: want exactly 1 cycle ending, got %d %v (emitted %v)",
			ckey, len(got), got, emittedTypes(actions))
	}
	return got[0]
}

func assertOutcome(t *testing.T, actions []keeper.Action, ckey string, want cycleOutcome) {
	t.Helper()
	if got := soleOutcome(t, actions, ckey); got != want {
		t.Fatalf("%s: outcome = %s, want %s (emitted %v)", ckey, got, want, emittedTypes(actions))
	}
}

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

func mkEventID(seq uint32) core.EventID {
	var b [16]byte
	binary.BigEndian.PutUint32(b[:4], seq)
	b[6] = 0x70 // version 7 nibble (cosmetic; the harness sorts on raw bytes)
	b[8] = 0x80 // RFC 4122 variant
	return core.EventID(uuid.UUID(b))
}
