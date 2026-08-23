package replay_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/replay"
)

type line struct {
	seq     byte
	evType  core.EventType
	payload core.EventPayload
}

func mkID(seq byte) core.EventID {
	var b [16]byte
	b[0] = seq
	b[6] = 0x70 // version 7 nibble (cosmetic; the harness sorts on raw bytes)
	b[8] = 0x80 // RFC 4122 variant
	return core.EventID(uuid.UUID(b))
}

func writeLog(t *testing.T, lines []line) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.Create(path) //nolint:gosec // G304: path is t.TempDir()-based; not user input.
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close log: %v", cerr)
		}
	}()

	enc := json.NewEncoder(f)
	base := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	for _, ln := range lines {
		raw, err := json.Marshal(ln.payload)
		if err != nil {
			t.Fatalf("marshal payload %s: %v", ln.evType, err)
		}
		schemaVersion := 1
		if registeredVersion, ok := core.LookupTypeSchemaVersion(ln.evType); ok {
			schemaVersion = registeredVersion
		}
		ev := core.Event{
			EventID:         mkID(ln.seq),
			SchemaVersion:   schemaVersion,
			Type:            ln.evType,
			TimestampWall:   base.Add(time.Duration(ln.seq) * time.Second),
			SourceSubsystem: "keeper",
			Payload:         json.RawMessage(raw),
		}
		if err := enc.Encode(&ev); err != nil {
			t.Fatalf("encode event %s: %v", ln.evType, err)
		}
	}
	return path
}

func appendRaw(t *testing.T, path string, obj map[string]any) {
	t.Helper()
	//nolint:gosec // G304: path is t.TempDir()-based; not user input.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open log for append: %v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close log: %v", cerr)
		}
	}()
	if err := json.NewEncoder(f).Encode(obj); err != nil {
		t.Fatalf("append raw line: %v", err)
	}
}

func violationKeys(vs []replay.Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Rule+"/"+v.CycleID)
	}
	sort.Strings(out)
	return out
}

func hs(agent, cid string) core.EventPayload {
	return core.SessionKeeperHandoffStartedPayload{AgentName: agent, CycleID: cid}
}

func hw(agent, cid string) core.EventPayload {
	return core.SessionKeeperHandoffWrittenPayload{AgentName: agent, CycleID: cid, Nonce: "n"}
}

func md(agent, cid string) core.EventPayload {
	return core.SessionKeeperModelDonePayload{AgentName: agent, CycleID: cid, Source: "idle_marker"}
}

func cs(agent, cid string) core.EventPayload {
	return core.SessionKeeperClearSentPayload{AgentName: agent, CycleID: cid, Attempt: 1}
}

func nsu(agent, cid string) core.EventPayload {
	return core.SessionKeeperNewSessionUpPayload{AgentName: agent, CycleID: cid, PrevSessionID: "old", NewSessionID: "new"}
}

func cc(agent, cid string) core.EventPayload {
	return core.SessionKeeperCycleCompletePayload{AgentName: agent, CycleID: cid, NewSessionID: "new"}
}

func cu(agent, cid string) core.EventPayload {
	return core.SessionKeeperClearUnconfirmedPayload{AgentName: agent, CycleID: cid}
}

func cp(agent, cid, reason string) core.EventPayload {
	return core.SessionKeeperCycleParkedPayload{AgentName: agent, CycleID: cid, Reason: reason}
}

// TestReplay_AcceptanceFixture is the T4 acceptance case: a fixture log with
// (a) a clean cycle, (b) an SR4-violating cycle (clear before model-done), and
// (c) an unterminated cycle. The report must flag EXACTLY the injected SR4
// violation + the unterminated cycle, and nothing else.
//
// It also exercises the COMPOSITE (agent_name, cycle_id) key: the clean cycle
// and the unterminated cycle share cycle_id "cyc-1" but differ by agent — if
// the harness keyed on cycle_id alone they would merge and the unterminated
// cycle would be hidden behind the clean cycle's terminal.
func TestReplay_AcceptanceFixture(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "cyc-1")},
		{2, core.EventTypeSessionKeeperHandoffWritten, hw("paul", "cyc-1")},
		{3, core.EventTypeSessionKeeperModelDone, md("paul", "cyc-1")},
		{4, core.EventTypeSessionKeeperClearSent, cs("paul", "cyc-1")},
		{5, core.EventTypeSessionKeeperNewSessionUp, nsu("paul", "cyc-1")},
		{6, core.EventTypeSessionKeeperCycleComplete, cc("paul", "cyc-1")},

		{11, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "cyc-2")},
		{12, core.EventTypeSessionKeeperHandoffWritten, hw("paul", "cyc-2")},
		{13, core.EventTypeSessionKeeperClearSent, cs("paul", "cyc-2")},
		{14, core.EventTypeSessionKeeperNewSessionUp, nsu("paul", "cyc-2")},
		{15, core.EventTypeSessionKeeperCycleComplete, cc("paul", "cyc-2")},

		{21, core.EventTypeSessionKeeperHandoffStarted, hs("leto", "cyc-1")},
		{22, core.EventTypeSessionKeeperHandoffWritten, hw("leto", "cyc-1")},
	}
	shuffled := make([]line, len(lines))
	for i, ln := range lines {
		shuffled[len(lines)-1-i] = ln
	}
	path := writeLog(t, shuffled)

	rep, err := replay.Replay(path, core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatalf("Replay returned error: %v", err)
	}

	if rep.Events != len(lines) {
		t.Errorf("Events = %d, want %d", rep.Events, len(lines))
	}
	if rep.Skipped != 0 || rep.Malformed != 0 {
		t.Errorf("Skipped=%d Malformed=%d, want 0/0", rep.Skipped, rep.Malformed)
	}

	got := violationKeys(rep.Violations)
	want := []string{"SR4/cyc-2", "SR9/cyc-1"} // SR4 on paul/cyc-2, unterminated leto/cyc-1
	if len(got) != len(want) {
		t.Fatalf("violations = %v, want exactly %v", rep.Violations, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("violations = %v (keys %v), want %v", rep.Violations, got, want)
		}
	}

	if !containsType(rep.RegisteredNeverObserved, core.EventTypeSessionKeeperOperatorAttached) {
		t.Errorf("RegisteredNeverObserved missing session_keeper_operator_attached; got %v", rep.RegisteredNeverObserved)
	}
}

func containsType(ts []core.EventType, want core.EventType) bool {
	for _, t := range ts {
		if t == want {
			return true
		}
	}
	return false
}

func TestReplay_SR3_ClearBeforeHandoffWritten(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("x", "c")},
		{2, core.EventTypeSessionKeeperModelDone, md("x", "c")},
		{3, core.EventTypeSessionKeeperClearSent, cs("x", "c")},
		{4, core.EventTypeSessionKeeperNewSessionUp, nsu("x", "c")},
		{5, core.EventTypeSessionKeeperCycleComplete, cc("x", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, []string{"SR3/c"})
}

func TestReplay_SR6_CompleteWithoutNewSession(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("z", "c")},
		{2, core.EventTypeSessionKeeperHandoffWritten, hw("z", "c")},
		{3, core.EventTypeSessionKeeperModelDone, md("z", "c")},
		{4, core.EventTypeSessionKeeperClearSent, cs("z", "c")},
		{5, core.EventTypeSessionKeeperCycleComplete, cc("z", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, []string{"SR6/c"})
}

func TestReplay_SR6_DegradedPathClean(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("z", "c")},
		{2, core.EventTypeSessionKeeperHandoffWritten, hw("z", "c")},
		{3, core.EventTypeSessionKeeperModelDone, md("z", "c")},
		{4, core.EventTypeSessionKeeperClearSent, cs("z", "c")},
		{5, core.EventTypeSessionKeeperClearUnconfirmed, cu("z", "c")},
		{6, core.EventTypeSessionKeeperCycleComplete, cc("z", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, nil)
}

func TestReplay_SR7_OverlappingRestart(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("y", "c-A")},
		{2, core.EventTypeSessionKeeperHandoffStarted, hs("y", "c-B")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, []string{"SR7/c-B", "SR9/c-A", "SR9/c-B"})
}

// TestReplay_ParkPendingThenResume_IsClean: the SK-025 resume path. One
// cycle_id parks with reason handoff_pending, then resumes under the SAME id
// and completes. Nothing about that is a violation — not the park, not the
// complete-after-park pair.
func TestReplay_ParkPendingThenResume_IsClean(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "c")},
		{2, core.EventTypeSessionKeeperCycleParked, cp("paul", "c", "handoff_pending")},
		{3, core.EventTypeSessionKeeperHandoffWritten, hw("paul", "c")},
		{4, core.EventTypeSessionKeeperModelDone, md("paul", "c")},
		{5, core.EventTypeSessionKeeperClearSent, cs("paul", "c")},
		{6, core.EventTypeSessionKeeperNewSessionUp, nsu("paul", "c")},
		{7, core.EventTypeSessionKeeperCycleComplete, cc("paul", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, nil)
}

// TestReplay_ParkPendingNeverResumed_IsNotUnterminated: a context request that
// stays pending is not a liveness breach. The cycle never reached
// handoff_written, so it never held restart authority and owes no terminal.
func TestReplay_ParkPendingNeverResumed_IsNotUnterminated(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("leto", "c")},
		{2, core.EventTypeSessionKeeperCycleParked, cp("leto", "c", "handoff_pending")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, nil)
}

// TestReplay_ParkForOperator_ThenFreshCycle_IsNotOverlap: after an
// operator_turn_recent park the keeper returns to Idle and mints a NEW cycle
// id on the next tick. That second cycle is a legitimate restart, not an
// overlapping one, and the parked first cycle owes no terminal.
//
// This is also the routing test: park carries the (agent_name, cycle_id) join
// key, so if cycleKey drops the payload the park never reaches a CycleState and
// this corpus reports SR7 on c-B plus SR9 on c-A.
func TestReplay_ParkForOperator_ThenFreshCycle_IsNotOverlap(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "c-A")},
		{2, core.EventTypeSessionKeeperCycleParked, cp("paul", "c-A", "operator_turn_recent")},
		{3, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "c-B")},
		{4, core.EventTypeSessionKeeperHandoffWritten, hw("paul", "c-B")},
		{5, core.EventTypeSessionKeeperModelDone, md("paul", "c-B")},
		{6, core.EventTypeSessionKeeperClearSent, cs("paul", "c-B")},
		{7, core.EventTypeSessionKeeperNewSessionUp, nsu("paul", "c-B")},
		{8, core.EventTypeSessionKeeperCycleComplete, cc("paul", "c-B")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, nil)
}

// TestReplay_ParkDoesNotExcuseAnAuthorizedRestart: a cycle that parked, resumed
// to handoff_written, and then went silent still owes a terminal. Park is the
// reason the FIRST half is forgiven; it is not a blanket excuse.
func TestReplay_ParkDoesNotExcuseAnAuthorizedRestart(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "c")},
		{2, core.EventTypeSessionKeeperCycleParked, cp("paul", "c", "handoff_pending")},
		{3, core.EventTypeSessionKeeperHandoffWritten, hw("paul", "c")},
		{4, core.EventTypeSessionKeeperModelDone, md("paul", "c")},
		{5, core.EventTypeSessionKeeperClearSent, cs("paul", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, []string{"SR9/c"})
	if got := rep.Violations[0].Detail; !strings.Contains(got, "restart authorized") {
		t.Errorf("SR9 detail = %q, want the authorized-restart case named", got)
	}
}

// TestReplay_OpenedAndAbandoned_IsStillUnterminated: handoff_started with no
// authority, no terminal, and no park recorded nothing about why the cycle
// stopped. This is the wedge shape the frozen 507-cycle baseline anchors on —
// that corpus predates handoff_written entirely, so anchoring SR9 on authority
// alone would report zero unterminated cycles across all 507.
func TestReplay_OpenedAndAbandoned_IsStillUnterminated(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("paul", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, []string{"SR9/c"})
	if got := rep.Violations[0].Detail; !strings.Contains(got, "nor a park") {
		t.Errorf("SR9 detail = %q, want the abandoned case named", got)
	}
}

func TestReplay_HistoricalCorpus_NoFalseSR6(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("old", "c")},
		{2, core.EventTypeSessionKeeperCycleComplete, cc("old", "c")},
	}
	rep, err := replay.Replay(writeLog(t, lines), core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	assertExactly(t, rep, nil)
}

func TestReplay_UnknownType_ObservationalSkips_StrictErrors(t *testing.T) {
	good := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("p", "c")},
		{2, core.EventTypeSessionKeeperHandoffWritten, hw("p", "c")},
		{3, core.EventTypeSessionKeeperModelDone, md("p", "c")},
		{4, core.EventTypeSessionKeeperClearSent, cs("p", "c")},
		{5, core.EventTypeSessionKeeperNewSessionUp, nsu("p", "c")},
		{6, core.EventTypeSessionKeeperCycleComplete, cc("p", "c")},
	}
	path := writeLog(t, good)
	appendRaw(t, path, map[string]any{
		"event_id":         mkID(7).String(),
		"schema_version":   1,
		"type":             "totally_unknown_type_xyz",
		"timestamp_wall":   "2026-07-13T12:00:07Z",
		"source_subsystem": "keeper",
		"payload":          map[string]any{"foo": "bar"},
	})

	rep, err := replay.Replay(path, core.EventID{}, false, replay.DefaultCheckers())
	if err != nil {
		t.Fatalf("observational Replay error: %v", err)
	}
	if rep.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", rep.Skipped)
	}
	assertExactly(t, rep, nil)

	_, err = replay.Replay(path, core.EventID{}, true, replay.DefaultCheckers())
	if err == nil {
		t.Fatal("strict Replay: expected an error for the unknown type, got nil")
	}
	var unk *core.DispatchUnknownEventError
	if !errors.As(err, &unk) {
		t.Errorf("strict error = %v, want *core.DispatchUnknownEventError", err)
	}
}

func TestReplay_RunStartedV1Compatibility(t *testing.T) {
	path := writeLog(t, nil)
	appendRaw(t, path, map[string]any{
		"event_id":         mkID(1).String(),
		"schema_version":   1,
		"type":             string(core.EventTypeRunStarted),
		"timestamp_wall":   "2026-08-02T12:00:01Z",
		"source_subsystem": "daemon",
		"run_id":           "01942b3c-0000-7000-8000-000000000020",
		"payload": map[string]any{
			"run_id":         "01942b3c-0000-7000-8000-000000000020",
			"bead_id":        "hk-historical",
			"workspace_path": "/tmp/historical",
			"started_at":     "2026-08-02T12:00:00Z",
		},
	})

	rep, err := replay.Replay(path, core.EventID{}, true, nil)
	if err != nil {
		t.Fatalf("strict replay of version-1 run_started: %v", err)
	}
	if rep.Malformed != 0 || rep.Events != 1 {
		t.Fatalf("replay report = %#v, want one readable historical event", rep)
	}
}

// TestReplay_Since_Watermark checks the incremental cursor: events at or before
// the watermark are excluded.
func TestReplay_Since_Watermark(t *testing.T) {
	lines := []line{
		{1, core.EventTypeSessionKeeperHandoffStarted, hs("p", "c")},
		{2, core.EventTypeSessionKeeperHandoffWritten, hw("p", "c")},
		{3, core.EventTypeSessionKeeperModelDone, md("p", "c")},
		{4, core.EventTypeSessionKeeperClearSent, cs("p", "c")},
	}
	path := writeLog(t, lines)
	rep, err := replay.Replay(path, mkID(2), false, replay.DefaultCheckers())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Events != 2 {
		t.Errorf("Events = %d, want 2 (after watermark)", rep.Events)
	}
	assertExactly(t, rep, []string{"SR3/c"})
}

func assertExactly(t *testing.T, rep replay.Report, want []string) {
	t.Helper()
	got := violationKeys(rep.Violations)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("violations = %v (keys %v), want exactly %v", rep.Violations, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("violations keys = %v, want %v", got, want)
		}
	}
}
