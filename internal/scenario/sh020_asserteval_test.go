package scenario_test

// sh020_asserteval_test.go — sensor tests for SH-020 through SH-024.
//
// Covers (per specs/scenario-harness.md §10.2):
//   - SH-020: ReadEventLog (JSONL reader, torn-tail tolerance)
//   - SH-021: event_present, event_absent, exit_code assertion kinds;
//             dotted-path payload_match; shallow-merge semantics
//   - SH-022: workspace_state predicates (all five kinds)
//   - SH-023: no-short-circuit runner (EvaluateAssertions)
//   - SH-024: harness-internal-error on log corruption; bus_overflow escalation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/scenario"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func writeEventLog(t *testing.T, dir string, lines []string) string {
	t.Helper()
	logDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir event log dir: %v", err)
	}
	path := filepath.Join(logDir, "events.jsonl")
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write event log: %v", err)
	}
	return path
}

func mustMarshalEvent(t *testing.T, eventType string, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	line, err := json.Marshal(map[string]any{
		"type":    eventType,
		"payload": json.RawMessage(raw),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(line)
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@test.local"},
		{"config", "user.name", "Test"},
	} {
		//nolint:noctx // test helper: no context available for git init/config helpers
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
}

func gitCommit(t *testing.T, dir, message string) {
	t.Helper()
	dummy := filepath.Join(dir, ".keep")
	_ = os.WriteFile(dummy, []byte(""), 0o644)
	for _, args := range [][]string{
		{"add", ".keep"},
		{"commit", "-m", message},
	} {
		//nolint:noctx // test helper: no context available for git add/commit helpers
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v — %s", args, err, out)
		}
	}
}

// ptr returns a pointer to s.
func ptr(s string) *string { return &s }

// ─────────────────────────────────────────────────────────────────────────────
// SH-020 / SH-024: ReadEventLog
// ─────────────────────────────────────────────────────────────────────────────

// TestSH020_ReadEventLog_Clean verifies that a clean JSONL file (ends with \n)
// returns all events without error.
func TestSH020_ReadEventLog_Clean(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{"run_id": "abc"}),
		mustMarshalEvent(t, "agent_completed", map[string]any{"run_id": "abc"}),
		"", // trailing newline
	}
	path := writeEventLog(t, dir, lines)
	events, err := scenario.ReadEventLog(path)
	if err != nil {
		t.Fatalf("ReadEventLog: unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2", len(events))
	}
}

// TestSH020_ReadEventLog_TornTail verifies that a torn tail (partial last line
// without a trailing newline) is silently skipped per EV §6.2 / SH-024.
func TestSH020_ReadEventLog_TornTail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Two complete events followed by a partial torn tail (no trailing \n).
	content := mustMarshalEvent(t, "agent_ready", map[string]any{}) +
		"\n" +
		mustMarshalEvent(t, "agent_completed", map[string]any{}) +
		"\n" +
		`{"type":"run_complet` // torn tail — partial JSON, no \n

	logDir := filepath.Join(dir, ".harmonik", "events")
	_ = os.MkdirAll(logDir, 0o755)
	path := filepath.Join(logDir, "events.jsonl")
	_ = os.WriteFile(path, []byte(content), 0o644)

	events, err := scenario.ReadEventLog(path)
	if err != nil {
		t.Fatalf("ReadEventLog with torn tail: unexpected error: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("got %d events, want 2 (torn tail must be silently skipped)", len(events))
	}
}

// TestSH024_ReadEventLog_MissingFile verifies that a missing log file returns
// a non-nil error (SH-024 i).
func TestSH024_ReadEventLog_MissingFile(t *testing.T) {
	t.Parallel()
	_, err := scenario.ReadEventLog("/no-such/path/events.jsonl")
	if err == nil {
		t.Error("ReadEventLog with missing file: expected non-nil error, got nil")
	}
}

// TestSH024_ReadEventLog_MidFileCorruption verifies that a JSON parse error in
// the middle of the file (not the torn tail) returns a non-nil error (SH-024 iii).
func TestSH024_ReadEventLog_MidFileCorruption(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Corrupt line in the middle, not at the end.
	content := mustMarshalEvent(t, "agent_ready", map[string]any{}) +
		"\n" +
		`{"type":"CORRUPT MIDDLE` + "\n" + // incomplete JSON followed by newline = mid-file
		mustMarshalEvent(t, "agent_completed", map[string]any{}) +
		"\n"

	logDir := filepath.Join(dir, ".harmonik", "events")
	_ = os.MkdirAll(logDir, 0o755)
	path := filepath.Join(logDir, "events.jsonl")
	_ = os.WriteFile(path, []byte(content), 0o644)

	_, err := scenario.ReadEventLog(path)
	if err == nil {
		t.Error("ReadEventLog with mid-file corruption: expected non-nil error, got nil")
	}
}

// TestSH024_ReadEventLog_BusOverflow verifies that observing a bus_overflow event
// returns a non-nil error (SH-024 v).
func TestSH024_ReadEventLog_BusOverflow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{}),
		mustMarshalEvent(t, string(core.EventTypeBusOverflow), map[string]any{"shed_policy": "ordinary-dropped"}),
		mustMarshalEvent(t, "agent_completed", map[string]any{}),
		"",
	}
	path := writeEventLog(t, dir, lines)
	_, err := scenario.ReadEventLog(path)
	if err == nil {
		t.Error("ReadEventLog with bus_overflow: expected non-nil error, got nil")
	}
	if !strings.Contains(err.Error(), "bus_overflow") {
		t.Errorf("error should mention bus_overflow; got: %v", err)
	}
}

// TestSH020_ReadEventLog_Empty verifies that an empty file returns zero events.
func TestSH020_ReadEventLog_Empty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := writeEventLog(t, dir, []string{""})
	events, err := scenario.ReadEventLog(path)
	if err != nil {
		t.Fatalf("ReadEventLog empty file: unexpected error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events from empty file, want 0", len(events))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-021: event_present assertion
// ─────────────────────────────────────────────────────────────────────────────

func makeScenarioFileForEvents(events []scenario.EventExpectation) scenario.ScenarioFile {
	return scenario.ScenarioFile{
		Name:           "test",
		Description:    "test",
		WorkflowPath:   ptr("test.dot"),
		ExpectedEvents: events,
		TimeoutSecs:    30,
		CadenceTag:     "smoke",
	}
}

func readEventsFromLines(t *testing.T, lines []string) []scenario.RawEvent {
	t.Helper()
	dir := t.TempDir()
	path := writeEventLog(t, dir, lines)
	evts, err := scenario.ReadEventLog(path)
	if err != nil {
		t.Fatalf("ReadEventLog: %v", err)
	}
	return evts
}

// TestSH021_EventPresent_Pass verifies that event_present passes when the
// event type is found in the log.
func TestSH021_EventPresent_Pass(t *testing.T) {
	t.Parallel()
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{"run_id": "r1"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{Kind: scenario.EventExpectationKindPresent, Type: "agent_ready", Description: "check agent_ready"},
	})
	results, verdict, fc := scenario.EvaluateAssertions(sf, events, "")
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].Passed {
		t.Errorf("event_present should pass; got Passed=false")
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
	if fc != "" {
		t.Errorf("fc = %q, want empty", fc)
	}
}

// TestSH021_EventPresent_Fail verifies that event_present fails when the
// event type is not in the log.
func TestSH021_EventPresent_Fail(t *testing.T) {
	t.Parallel()
	lines := []string{mustMarshalEvent(t, "agent_completed", map[string]any{}), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{Kind: scenario.EventExpectationKindPresent, Type: "agent_ready", Description: "check agent_ready"},
	})
	results, verdict, fc := scenario.EvaluateAssertions(sf, events, "")
	if !(!results[0].Passed) {
		t.Error("event_present should fail when event absent")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
	if fc != scenario.FailureClassAssertionFailed {
		t.Errorf("fc = %q, want assertion-failed", fc)
	}
}

// TestSH021_EventAbsent_Pass verifies that event_absent passes when the
// event type is NOT in the log.
func TestSH021_EventAbsent_Pass(t *testing.T) {
	t.Parallel()
	lines := []string{mustMarshalEvent(t, "agent_ready", map[string]any{}), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{Kind: scenario.EventExpectationKindAbsent, Type: "outcome_emitted", Description: "check absent"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Error("event_absent should pass when event not in log")
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH021_EventAbsent_Fail verifies that event_absent fails when the
// event type IS in the log.
func TestSH021_EventAbsent_Fail(t *testing.T) {
	t.Parallel()
	lines := []string{
		mustMarshalEvent(t, "outcome_emitted", map[string]any{"outcome_status": "SUCCESS"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{Kind: scenario.EventExpectationKindAbsent, Type: "outcome_emitted", Description: "check absent"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if results[0].Passed {
		t.Error("event_absent should fail when event IS in log")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-021: dotted-path payload_match
// ─────────────────────────────────────────────────────────────────────────────

// TestSH021_PayloadMatch_DottedPath verifies dotted-path resolution.
func TestSH021_PayloadMatch_DottedPath(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"error": map[string]any{"category": "ErrStructural", "code": 42},
	}
	lines := []string{mustMarshalEvent(t, "agent_failed", payload), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{
			Kind:         scenario.EventExpectationKindPresent,
			Type:         "agent_failed",
			PayloadMatch: map[string]any{"error.category": "ErrStructural"},
			Description:  "check error.category",
		},
	})
	results, _, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Error("dotted-path payload_match should pass")
	}
}

// TestSH021_PayloadMatch_ArrayIndex verifies bracket-form array index resolution.
func TestSH021_PayloadMatch_ArrayIndex(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"items": []any{
			map[string]any{"id": "first"},
			map[string]any{"id": "second"},
		},
	}
	lines := []string{mustMarshalEvent(t, "some_event", payload), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{
			Kind:         scenario.EventExpectationKindPresent,
			Type:         "some_event",
			PayloadMatch: map[string]any{"items[1].id": "second"},
			Description:  "check items[1].id",
		},
	})
	results, _, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Error("array-index path should resolve and pass")
	}
}

// TestSH021_PayloadMatch_ShallowMerge verifies that undeclared keys in the
// actual payload don't cause a match failure (shallow-merge semantics).
func TestSH021_PayloadMatch_ShallowMerge(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"outcome_status": "SUCCESS",
		"extra_field":    "ignored",
		"nested":         map[string]any{"key": "val"},
	}
	lines := []string{mustMarshalEvent(t, "outcome_emitted", payload), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{
			Kind:         scenario.EventExpectationKindPresent,
			Type:         "outcome_emitted",
			PayloadMatch: map[string]any{"outcome_status": "SUCCESS"},
			Description:  "shallow merge — extra keys OK",
		},
	})
	results, _, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Error("shallow-merge: undeclared keys in actual payload should not cause failure")
	}
}

// TestSH021_PayloadMatch_NumericEquality verifies 1 == 1.0 under JSON numeric
// equality per SH-021.
func TestSH021_PayloadMatch_NumericEquality(t *testing.T) {
	t.Parallel()
	// JSON encodes 1 as a float64 (1.0) internally; both match under numeric equality.
	payload := map[string]any{"count": 1}
	lines := []string{mustMarshalEvent(t, "some_event", payload), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{
			Kind:         scenario.EventExpectationKindPresent,
			Type:         "some_event",
			PayloadMatch: map[string]any{"count": float64(1)},
			Description:  "numeric equality 1==1.0",
		},
	})
	results, _, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Error("numeric equality: 1 should equal 1.0 per SH-021")
	}
}

// TestSH021_PayloadMatch_MissingKey verifies that a declared key missing from
// the payload causes event_present to fail.
func TestSH021_PayloadMatch_MissingKey(t *testing.T) {
	t.Parallel()
	lines := []string{mustMarshalEvent(t, "some_event", map[string]any{"other": "val"}), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{
			Kind:         scenario.EventExpectationKindPresent,
			Type:         "some_event",
			PayloadMatch: map[string]any{"missing_key": "val"},
			Description:  "missing key should fail",
		},
	})
	results, _, _ := scenario.EvaluateAssertions(sf, events, "")
	if results[0].Passed {
		t.Error("missing payload key should cause event_present to fail")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-021: exit_code assertion
// ─────────────────────────────────────────────────────────────────────────────

func makeScenarioFileWithOutcome(expected core.OutcomeStatus) scenario.ScenarioFile {
	desc := "check outcome"
	return scenario.ScenarioFile{
		Name:         "test",
		Description:  "test",
		WorkflowPath: ptr("test.dot"),
		ExpectedOutcome: &scenario.OutcomeExpectation{
			OutcomeStatus: expected,
			Description:   desc,
		},
		TimeoutSecs: 30,
		CadenceTag:  "smoke",
	}
}

// TestSH021_ExitCode_OutcomeEmitted_Success verifies exit_code(SUCCESS) passes
// when the outcome_emitted event carries outcome_status=SUCCESS.
func TestSH021_ExitCode_OutcomeEmitted_Success(t *testing.T) {
	t.Parallel()
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{}),
		mustMarshalEvent(t, "outcome_emitted", map[string]any{"outcome_status": "SUCCESS"}),
		mustMarshalEvent(t, "run_completed", map[string]any{"run_id": "abc", "terminal_state_id": "def", "ended_at": "2026-01-01T00:00:00Z"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileWithOutcome(core.OutcomeStatusSuccess)
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].Passed {
		t.Errorf("exit_code(SUCCESS): expected Passed=true; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH021_ExitCode_Fallback_RunFailed verifies exit_code(FAIL) passes when
// there is no outcome_emitted but run_failed is present.
func TestSH021_ExitCode_Fallback_RunFailed(t *testing.T) {
	t.Parallel()
	lines := []string{
		mustMarshalEvent(t, "agent_failed", map[string]any{"error_category": "ErrStructural"}),
		mustMarshalEvent(t, "run_failed", map[string]any{"run_id": "abc", "failure_class": "handler-error", "ended_at": "2026-01-01T00:00:00Z", "reason": "crash"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileWithOutcome(core.OutcomeStatusFail)
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if !results[0].Passed {
		t.Errorf("exit_code(FAIL) with run_failed: expected Passed=true; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH021_ExitCode_Mismatch verifies exit_code fails when the actual outcome
// does not match the expected.
func TestSH021_ExitCode_Mismatch(t *testing.T) {
	t.Parallel()
	lines := []string{
		mustMarshalEvent(t, "outcome_emitted", map[string]any{"outcome_status": "FAIL"}),
		mustMarshalEvent(t, "run_failed", map[string]any{"run_id": "abc", "failure_class": "handler-error", "ended_at": "now", "reason": "x"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileWithOutcome(core.OutcomeStatusSuccess)
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if results[0].Passed {
		t.Error("exit_code(SUCCESS) with actual FAIL: expected Passed=false")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// TestSH021_ExitCode_NoTerminalEvent verifies exit_code fails gracefully when no
// terminal event is present.
func TestSH021_ExitCode_NoTerminalEvent(t *testing.T) {
	t.Parallel()
	lines := []string{mustMarshalEvent(t, "agent_ready", map[string]any{}), ""}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileWithOutcome(core.OutcomeStatusSuccess)
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if results[0].Passed {
		t.Error("exit_code with no terminal event: expected Passed=false")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-022: workspace_state predicates
// ─────────────────────────────────────────────────────────────────────────────

func makeScenarioFileWithWorkspace(preds []scenario.WorkspacePredicate) scenario.ScenarioFile {
	return scenario.ScenarioFile{
		Name:              "test",
		Description:       "test",
		WorkflowPath:      ptr("test.dot"),
		ExpectedWorkspace: preds,
		TimeoutSecs:       30,
		CadenceTag:        "smoke",
	}
}

// TestSH022_FileExists_Pass verifies file_exists passes when file is present.
func TestSH022_FileExists_Pass(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wsDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileExists, Path: "hello.txt", Description: "file exists"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if !results[0].Passed {
		t.Errorf("file_exists: expected pass; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH022_FileExists_Fail verifies file_exists fails when file is absent.
func TestSH022_FileExists_Fail(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileExists, Path: "missing.txt", Description: "missing"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("file_exists: expected fail for absent file")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// TestSH022_FileContentsEqual_Pass verifies file_contents_equal passes on match.
func TestSH022_FileContentsEqual_Pass(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	content := "hello world\n"
	_ = os.WriteFile(filepath.Join(wsDir, "out.txt"), []byte(content), 0o644)
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileContentsEqual, Path: "out.txt", Expected: ptr(content), Description: "contents equal"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if !results[0].Passed {
		t.Errorf("file_contents_equal: expected pass; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH022_FileContentsEqual_Fail verifies file_contents_equal fails on mismatch.
func TestSH022_FileContentsEqual_Fail(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(wsDir, "out.txt"), []byte("actual"), 0o644)
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileContentsEqual, Path: "out.txt", Expected: ptr("expected"), Description: "mismatch"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("file_contents_equal: expected fail on mismatch")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// TestSH022_FileContentsMatch_Pass verifies file_contents_match passes when
// RE2 pattern matches.
func TestSH022_FileContentsMatch_Pass(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(wsDir, "log.txt"), []byte("Run ID: abc-123\n"), 0o644)
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileContentsMatch, Path: "log.txt", Expected: ptr(`Run ID: [a-z0-9-]+`), Description: "regex match"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if !results[0].Passed {
		t.Errorf("file_contents_match: expected pass; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH022_FileContentsMatch_Fail verifies file_contents_match fails when
// RE2 pattern does not match.
func TestSH022_FileContentsMatch_Fail(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(wsDir, "log.txt"), []byte("no match here\n"), 0o644)
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileContentsMatch, Path: "log.txt", Expected: ptr(`^NEVER_MATCHES$`), Description: "no match"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("file_contents_match: expected fail when pattern doesn't match")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// TestSH022_GitRefAt_Pass verifies git_ref_at passes when the ref resolves to
// the expected SHA.
func TestSH022_GitRefAt_Pass(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	gitCommit(t, wsDir, "first commit")

	//nolint:noctx // test helper: no context available for rev-parse in test setup
	out, err := exec.Command("git", "-C", wsDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	headSHA := strings.TrimSpace(string(out))

	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindGitRefAt, Path: "HEAD", Expected: ptr(headSHA), Description: "HEAD at SHA"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if !results[0].Passed {
		t.Errorf("git_ref_at: expected pass; actual=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH022_GitRefAt_Fail verifies git_ref_at fails when the ref resolves to a
// different SHA.
func TestSH022_GitRefAt_Fail(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	gitCommit(t, wsDir, "first commit")

	wrongSHA := strings.Repeat("a", 40) // all-a SHA
	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindGitRefAt, Path: "HEAD", Expected: ptr(wrongSHA), Description: "wrong SHA"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("git_ref_at: expected fail when SHA doesn't match")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// TestSH022_CommitTrailerPresent_Pass verifies commit_trailer_present passes
// when the HEAD commit has the expected trailer key.
func TestSH022_CommitTrailerPresent_Pass(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	gitCommit(t, wsDir, "feat: add something\n\nHarmonik-Run-ID: abc-123")

	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindCommitTrailerPresent, Path: "HEAD", Expected: ptr("Harmonik-Run-ID"), Description: "trailer present"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if !results[0].Passed {
		t.Errorf("commit_trailer_present: expected pass; actual message=%v", results[0].ActualValue)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// TestSH022_CommitTrailerPresent_Fail verifies commit_trailer_present fails
// when the HEAD commit lacks the expected trailer key.
func TestSH022_CommitTrailerPresent_Fail(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	initGitRepo(t, wsDir)
	gitCommit(t, wsDir, "feat: add something (no trailer)")

	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindCommitTrailerPresent, Path: "HEAD", Expected: ptr("Harmonik-Run-ID"), Description: "trailer absent"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("commit_trailer_present: expected fail when trailer absent")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-022: symlink traversal rejection
// ─────────────────────────────────────────────────────────────────────────────

// TestSH022_SymlinkTraversalRejected verifies that a symlink within the workspace
// that resolves outside it causes an assertion-failed result per SH-022.
func TestSH022_SymlinkTraversalRejected(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	outsideFile := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)

	// Create a symlink inside workspace that points outside.
	symlinkPath := filepath.Join(wsDir, "escape.txt")
	if err := os.Symlink(outsideFile, symlinkPath); err != nil {
		t.Skipf("symlink creation failed (may need elevated perms): %v", err)
	}

	sf := makeScenarioFileWithWorkspace([]scenario.WorkspacePredicate{
		{Kind: scenario.WorkspacePredicateKindFileExists, Path: "escape.txt", Description: "symlink escape"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, nil, wsDir)
	if results[0].Passed {
		t.Error("symlink traversal: expected rejection (Passed=false), got Passed=true")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SH-023: no-short-circuit runner
// ─────────────────────────────────────────────────────────────────────────────

// TestSH023_NoShortCircuit verifies that all assertions are evaluated even
// after an earlier one fails.
func TestSH023_NoShortCircuit(t *testing.T) {
	t.Parallel()
	// Two event_present assertions; the first will fail, the second will pass.
	// Under no-short-circuit, both must be evaluated.
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := makeScenarioFileForEvents([]scenario.EventExpectation{
		{Kind: scenario.EventExpectationKindPresent, Type: "outcome_emitted", Description: "first: absent"},
		{Kind: scenario.EventExpectationKindPresent, Type: "agent_ready", Description: "second: present"},
	})
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, "")
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (no short-circuit per SH-023)", len(results))
	}
	if results[0].Passed {
		t.Error("first assertion should fail")
	}
	if !results[1].Passed {
		t.Error("second assertion should pass")
	}
	if verdict != scenario.ScenarioVerdictFail {
		t.Errorf("verdict = %q, want fail (first assertion failed)", verdict)
	}
}

// TestSH023_AssertionOrder verifies evaluation order: expected_events, then
// expected_workspace, then expected_outcome.
func TestSH023_AssertionOrder(t *testing.T) {
	t.Parallel()
	wsDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(wsDir, "marker.txt"), []byte("present"), 0o644)
	lines := []string{
		mustMarshalEvent(t, "agent_ready", map[string]any{}),
		mustMarshalEvent(t, "outcome_emitted", map[string]any{"outcome_status": "SUCCESS"}),
		mustMarshalEvent(t, "run_completed", map[string]any{"run_id": "abc", "terminal_state_id": "def", "ended_at": "2026-01-01T00:00:00Z"}),
		"",
	}
	events := readEventsFromLines(t, lines)
	sf := scenario.ScenarioFile{
		Name:        "order-test",
		Description: "test",
		WorkflowPath: ptr("test.dot"),
		ExpectedEvents: []scenario.EventExpectation{
			{Kind: scenario.EventExpectationKindPresent, Type: "agent_ready", Description: "evt1"},
		},
		ExpectedWorkspace: []scenario.WorkspacePredicate{
			{Kind: scenario.WorkspacePredicateKindFileExists, Path: "marker.txt", Description: "ws1"},
		},
		ExpectedOutcome: &scenario.OutcomeExpectation{
			OutcomeStatus: core.OutcomeStatusSuccess,
			Description:   "outcome1",
		},
		TimeoutSecs: 30,
		CadenceTag:  "smoke",
	}
	results, verdict, _ := scenario.EvaluateAssertions(sf, events, wsDir)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if results[0].AssertionKind != scenario.AssertionResultKindEventPresent {
		t.Errorf("results[0]: want event_present, got %q", results[0].AssertionKind)
	}
	if results[1].AssertionKind != scenario.AssertionResultKindWorkspaceState {
		t.Errorf("results[1]: want workspace_state, got %q", results[1].AssertionKind)
	}
	if results[2].AssertionKind != scenario.AssertionResultKindExitCode {
		t.Errorf("results[2]: want exit_code, got %q", results[2].AssertionKind)
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EvaluateAssertions: vacuous (no assertions) case
// ─────────────────────────────────────────────────────────────────────────────

// TestEvaluateAssertions_Vacuous verifies that a scenario with no declared
// assertions passes vacuously with an empty results slice.
func TestEvaluateAssertions_Vacuous(t *testing.T) {
	t.Parallel()
	sf := scenario.ScenarioFile{
		Name:        "vacuous",
		Description: "no assertions",
		WorkflowPath: ptr("test.dot"),
		TimeoutSecs: 30,
		CadenceTag:  "smoke",
	}
	results, verdict, fc := scenario.EvaluateAssertions(sf, nil, "")
	if len(results) != 0 {
		t.Errorf("vacuous: got %d results, want 0", len(results))
	}
	if verdict != scenario.ScenarioVerdictPass {
		t.Errorf("vacuous: verdict = %q, want pass", verdict)
	}
	if fc != "" {
		t.Errorf("vacuous: fc = %q, want empty", fc)
	}
}
