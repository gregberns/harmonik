package twinparity_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/twinparity"
)

// recordingTB is a testing.TB that intercepts failure calls instead of failing
// the enclosing test. It embeds a real testing.TB solely to satisfy the
// unexported testing.TB.private() method (testing.TB is not implementable
// otherwise); the failure methods are overridden to record, not abort. This
// lets negative tests assert that AssertStreamEquivalent REPORTS a failure
// without a real t.Fatal aborting the suite.
type recordingTB struct {
	testing.TB
	failed bool
	msgs   []string
}

func (r *recordingTB) Helper() {}
func (r *recordingTB) Errorf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Error(args ...any) { r.failed = true }
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *recordingTB) Fatal(args ...any) { r.failed = true }

func (r *recordingTB) lastMsg() string {
	if len(r.msgs) == 0 {
		return ""
	}
	return r.msgs[len(r.msgs)-1]
}

func loadOrFail(t *testing.T, path string) twinparity.Stream {
	t.Helper()
	s, err := twinparity.LoadStream(path)
	if err != nil {
		t.Fatalf("LoadStream(%s): %v", path, err)
	}
	return s
}

// ─────────────────────────────────────────────────────────────────────────────
// Dual-field kind extraction (load-bearing)
// ─────────────────────────────────────────────────────────────────────────────

func TestDualFieldKindExtraction(t *testing.T) {
	// event_type-bearing envelope (real capture) → kind from event_type.
	envelope := `{"event_type":"outcome_emitted","type":"ignored_raw","outcome_status":"success","node_id":"n1","event_id":"11111111-1111-1111-1111-111111111111","timestamp":"2026-05-14T10:00:00Z"}`
	// type-only raw wire line → kind falls back to type.
	rawLine := `{"type":"outcome_emitted","outcome_status":"success","node_id":"n1"}`

	s, err := twinparity.LoadStreamLines([]string{envelope, rawLine})
	if err != nil {
		t.Fatalf("LoadStreamLines: %v", err)
	}
	if len(s.Events) != 2 {
		t.Fatalf("want 2 events, got %d", len(s.Events))
	}
	if s.Events[0].Kind != "outcome_emitted" {
		t.Errorf("envelope: want kind outcome_emitted (from event_type, NOT the embedded type), got %q", s.Events[0].Kind)
	}
	if s.Events[1].Kind != "outcome_emitted" {
		t.Errorf("raw line: want kind outcome_emitted (fallback to type), got %q", s.Events[1].Kind)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy path: equivalent streams pass
// ─────────────────────────────────────────────────────────────────────────────

func TestEquivalentStreamsPass(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{})
	if rec.failed {
		t.Fatalf("identical streams should be equivalent; got failure: %s", rec.lastMsg())
	}
}

func TestReviewLoopReachesAllTerminalKinds(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "review-loop.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "review-loop.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{})
	if rec.failed {
		t.Fatalf("review-loop stream should reach all TerminalKinds; got failure: %s", rec.lastMsg())
	}
}

// A twin's terminal spine appears as a subsequence in a real stream that has
// extra interleaved events (AllowExtra behavior).
func TestExtraEventsToleratedAsSubsequence(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "review-loop.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{})
	if rec.failed {
		t.Fatalf("terminal spine should be an ordered subsequence despite extra reviewer events; got: %s", rec.lastMsg())
	}
}

// TestRealEmissionOrderPasses is the regression lock: a fixture encoded in the
// TRUE durable emission order (outcome_emitted → bead_closed → run_completed)
// must PASS the DEFAULT spine. This prevents the suite from being self-
// consistent with a wrong spine (the F1 false-negative): a mis-ordered default
// spine that only ever compared a fixture against itself would pass its own
// tests while rejecting every real durable capture.
func TestRealEmissionOrderPasses(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "real-emission-order.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "real-emission-order.events.jsonl"))

	rec := &recordingTB{TB: t}
	// Default spine (no Kinds override) = TerminalKinds durable triad.
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{})
	if rec.failed {
		t.Fatalf("real durable emission order must PASS the default spine; got failure: %s", rec.lastMsg())
	}
	// Assert the default spine is exactly the durable triad, in order.
	want := []string{"outcome_emitted", "bead_closed", "run_completed"}
	if len(twinparity.TerminalKinds) != len(want) {
		t.Fatalf("TerminalKinds must be the 3-kind durable triad, got %v", twinparity.TerminalKinds)
	}
	for i := range want {
		if twinparity.TerminalKinds[i] != want[i] {
			t.Errorf("TerminalKinds[%d] = %q, want %q (durable emission order)", i, twinparity.TerminalKinds[i], want[i])
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Negative: a dropped durable terminal FAILS AssertStreamEquivalent(default)
// ─────────────────────────────────────────────────────────────────────────────

func TestDroppedDurableTerminalFails(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "mutated", "dropped-durable-terminal.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{
		Kinds: twinparity.TerminalKinds,
	})
	if !rec.failed {
		t.Fatalf("a dropped durable terminal (bead_closed) must fail the equivalence assertion")
	}
	if !strings.Contains(rec.lastMsg(), "bead_closed") {
		t.Errorf("failure message must name the first divergence (bead_closed); got: %s", rec.lastMsg())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Negative: a reordered durable triad FAILS the subsequence engine
// ─────────────────────────────────────────────────────────────────────────────

func TestReorderedDurableFails(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	// bead_closed appears BEFORE outcome_emitted — the reverse of the durable
	// order — so the default spine's outcome_emitted→bead_closed subsequence
	// cannot be satisfied.
	realStream := loadOrFail(t, filepath.Join("testdata", "mutated", "reordered-durable.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{})
	if !rec.failed {
		t.Fatalf("reordered durable triad must fail the subsequence engine")
	}
	if !strings.Contains(rec.lastMsg(), "subsequence") && !strings.Contains(rec.lastMsg(), "bead_closed") {
		t.Errorf("failure message should name the ordering divergence; got: %s", rec.lastMsg())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Payload-field divergence names the first field
// ─────────────────────────────────────────────────────────────────────────────

func TestPayloadFieldDivergenceReported(t *testing.T) {
	twinLine := `{"event_type":"outcome_emitted","outcome_status":"success","node_id":"n1"}`
	realLine := `{"event_type":"outcome_emitted","outcome_status":"failure","node_id":"n1"}`
	twin, err := twinparity.LoadStreamLines([]string{twinLine})
	if err != nil {
		t.Fatal(err)
	}
	realStream, err := twinparity.LoadStreamLines([]string{realLine})
	if err != nil {
		t.Fatal(err)
	}

	rec := &recordingTB{TB: t}
	twinparity.AssertStreamEquivalent(rec, twin, realStream, twinparity.EquivOptions{
		Kinds: []string{"outcome_emitted"},
	})
	if !rec.failed {
		t.Fatalf("differing outcome_status must fail equivalence")
	}
	msg := rec.lastMsg()
	if !strings.Contains(msg, "outcome_status") || !strings.Contains(msg, "success") || !strings.Contains(msg, "failure") {
		t.Errorf("failure must name field + expected/got; got: %s", msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Volatile-field dropping
// ─────────────────────────────────────────────────────────────────────────────

func TestVolatileFieldsDropped(t *testing.T) {
	line := `{"event_type":"outcome_emitted","outcome_status":"success","node_id":"n1","run_id":"r","event_id":"e","session_log_path":"/x/y","emitted_at":"2026-05-14T10:00:00Z"}`
	s, err := twinparity.LoadStreamLines([]string{line})
	if err != nil {
		t.Fatal(err)
	}
	f := s.Events[0].Fields
	if _, ok := f["run_id"]; ok {
		t.Error("run_id must be dropped")
	}
	if _, ok := f["session_log_path"]; ok {
		t.Error("session_log_* must be dropped")
	}
	if _, ok := f["emitted_at"]; ok {
		t.Error("*_at must be dropped")
	}
	if f["outcome_status"] != "success" || f["node_id"] != "n1" {
		t.Errorf("stable whitelist fields must be retained; got %v", f)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Timing tolerance
// ─────────────────────────────────────────────────────────────────────────────

func TestTimingWithinTolerancePasses(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	realStream := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertTimingWithinTolerance(rec, twin, realStream, twinparity.DefaultTimingEdges, 1*time.Second)
	if rec.failed {
		t.Fatalf("identical timing should pass; got: %s", rec.lastMsg())
	}
}

func TestTimingOutsideToleranceFails(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	// review-loop's outcome→hook is ~1s but agent_ready→outcome differs vs
	// happy-path; use a tiny tolerance to force a failure on the edge delta.
	realStream := loadOrFail(t, filepath.Join("testdata", "review-loop.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertTimingWithinTolerance(rec, twin, realStream,
		[]twinparity.TimingEdge{{From: "agent_ready", To: "outcome_emitted"}}, 1*time.Millisecond)
	if !rec.failed {
		t.Fatalf("agent_ready→outcome latency differs beyond 1ms; must fail")
	}
}

func TestTimingMissingEndpointFails(t *testing.T) {
	twin := loadOrFail(t, filepath.Join("testdata", "happy-path.events.jsonl"))
	// dropped-durable-terminal has no bead_closed, so the bead_closed endpoint
	// is absent in the real stream.
	realStream := loadOrFail(t, filepath.Join("testdata", "mutated", "dropped-durable-terminal.events.jsonl"))

	rec := &recordingTB{TB: t}
	twinparity.AssertTimingWithinTolerance(rec, twin, realStream,
		[]twinparity.TimingEdge{{From: "bead_closed", To: "run_completed"}}, 1*time.Second)
	if !rec.failed {
		t.Fatalf("absent hook_fired endpoint must fail the timing assertion")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Vocabulary
// ─────────────────────────────────────────────────────────────────────────────

func TestVocabularyAssembly(t *testing.T) {
	if !twinparity.IsKnownKind("outcome_emitted") {
		t.Error("outcome_emitted must be a known kind (core taxonomy)")
	}
	if !twinparity.IsKnownKind("agent_ready") {
		t.Error("agent_ready must be a known kind (progress-msg vocabulary)")
	}
	if twinparity.IsKnownKind("not_a_real_kind") {
		t.Error("bogus kind must not be known")
	}
	if len(twinparity.KnownKinds()) == 0 {
		t.Error("KnownKinds must be non-empty")
	}
	// Named sets sanity.
	if len(twinparity.TerminalKinds) != 3 {
		t.Errorf("TerminalKinds should have 3 entries (durable triad), got %d", len(twinparity.TerminalKinds))
	}
	if len(twinparity.AnomalyKinds) != 4 {
		t.Errorf("AnomalyKinds should have 4 entries, got %d", len(twinparity.AnomalyKinds))
	}
}
