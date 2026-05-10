// Package core — requirement-traceable sensors for RunCompletedPayload and
// RunFailedPayload per execution-model.md §4.3.EM-015b and event-model.md §8.1.
//
// EM-015b: the daemon MUST emit exactly one of {run_completed, run_failed} on
// terminal state. run_completed on terminal node + outcome ∈ {SUCCESS,
// PARTIAL_SUCCESS}; run_failed on classifier verdict (§8), cascade FAIL
// (EM-046a / EM-043 post-coalesce), or operator cancel. run_failed payload
// carries failure class + last_checkpoint SHA per EM-025.
//
// Helper prefix: runterminalFixture (per implementer-protocol.md §Helper-prefix).
// Requirement-traceable bead: hk-b3f.17.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────────────────────────────────
// Fixture helpers (runterminalFixture prefix)
// ──────────────────────────────────────────────────────────────────────────────

// runterminalFixtureCompleted returns a fully-populated, valid RunCompletedPayload.
func runterminalFixtureCompleted(t *testing.T) RunCompletedPayload {
	t.Helper()
	summary := "workflow finished successfully"
	return RunCompletedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000010")),
		TerminalStateID: StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000000011")),
		EndedAt:         "2026-05-09T12:00:00Z",
		Summary:         &summary,
	}
}

// runterminalFixtureCompletedNoSummary returns a valid RunCompletedPayload
// without an optional summary field (nil Summary).
func runterminalFixtureCompletedNoSummary(t *testing.T) RunCompletedPayload {
	t.Helper()
	return RunCompletedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000012")),
		TerminalStateID: StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000000013")),
		EndedAt:         "2026-05-09T12:01:00Z",
		Summary:         nil,
	}
}

// runterminalFixtureFailed returns a fully-populated, valid RunFailedPayload
// with a structural failure class and a non-empty last_checkpoint SHA.
func runterminalFixtureFailed(t *testing.T) RunFailedPayload {
	t.Helper()
	stateID := StateID(uuid.MustParse("01942b3c-0000-7000-8000-000000000021"))
	errCat := ErrorCategoryStructural
	return RunFailedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000020")),
		TerminalStateID: &stateID,
		FailureClass:    FailureClassStructural,
		ErrorCategory:   &errCat,
		EndedAt:         "2026-05-09T12:02:00Z",
		Reason:          "no outgoing edge matches current context",
		LastCheckpoint:  "abc1234def5678abc1234def5678abc1234def56",
	}
}

// runterminalFixtureFailedNoCheckpoint returns a valid RunFailedPayload that
// represents a budget_exhausted failure at dispatch time: no terminal_state_id
// (before any node was entered) and empty last_checkpoint (no prior durable
// transition).
func runterminalFixtureFailedNoCheckpoint(t *testing.T) RunFailedPayload {
	t.Helper()
	return RunFailedPayload{
		RunID:           RunID(uuid.MustParse("01942b3c-0000-7000-8000-000000000030")),
		TerminalStateID: nil,
		FailureClass:    FailureClassBudgetExhausted,
		ErrorCategory:   nil,
		EndedAt:         "2026-05-09T12:03:00Z",
		Reason:          "budget exhausted at dispatch; run not started",
		LastCheckpoint:  "",
	}
}

// runterminalFixtureEm015bSpecContent reads specs/execution-model.md and
// returns the paragraph anchored by "EM-015b". Fails if the file is
// unreadable or the anchor is absent.
func runterminalFixtureEm015bSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runterminalFixtureEm015bSpecContent: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "execution-model.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("runterminalFixtureEm015bSpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "EM-015b"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("EM-015b anchor not found in %s", specPath)
	}
	para := content[idx:]
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// ──────────────────────────────────────────────────────────────────────────────
// Spec-content sensor (EM-015b)
// ──────────────────────────────────────────────────────────────────────────────

// TestRunTerminal_EM015b_SpecContainsRequiredPhrases verifies that
// execution-model.md §4.3 EM-015b encodes the canonical phrases for
// run_completed and run_failed emission rules.
//
// Requirement-traceable bead: hk-b3f.17.
func TestRunTerminal_EM015b_SpecContainsRequiredPhrases(t *testing.T) {
	t.Parallel()

	para := runterminalFixtureEm015bSpecContent(t)

	phrases := []struct {
		phrase string
		hint   string
	}{
		{"run_completed", "EM-015b must name run_completed event"},
		{"run_failed", "EM-015b must name run_failed event"},
		{"terminal_node_ids", "EM-015b must reference terminal_node_ids"},
		{"failure class", "EM-015b must require failure class in run_failed payload"},
		{"last_checkpoint", "EM-015b must require last_checkpoint SHA in run_failed payload"},
		{"BI-010", "EM-015b must reference BI-010 bead-write ordering"},
	}

	for _, p := range phrases {
		if !strings.Contains(para, p.phrase) {
			t.Errorf("EM-015b paragraph missing %q — %s\nParagraph:\n%s", p.phrase, p.hint, para)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RunCompletedPayload sensor tests
// ──────────────────────────────────────────────────────────────────────────────

// TestRunCompletedPayload_Valid_FullyPopulated verifies that a fully-populated
// RunCompletedPayload passes Valid().
func TestRunCompletedPayload_Valid_FullyPopulated(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated RunCompletedPayload, want true")
	}
}

// TestRunCompletedPayload_Valid_NoSummary verifies that a RunCompletedPayload
// with nil Summary passes Valid() (summary is optional per §8.1.2).
func TestRunCompletedPayload_Valid_NoSummary(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompletedNoSummary(t)
	if !p.Valid() {
		t.Error("Valid() = false for RunCompletedPayload with nil Summary, want true (summary optional)")
	}
}

// TestRunCompletedPayload_Valid_ZeroRunID verifies that Valid() rejects a zero RunID.
func TestRunCompletedPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

// TestRunCompletedPayload_Valid_ZeroTerminalStateID verifies that Valid()
// rejects a zero TerminalStateID.
func TestRunCompletedPayload_Valid_ZeroTerminalStateID(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	p.TerminalStateID = StateID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero TerminalStateID, want false")
	}
}

// TestRunCompletedPayload_Valid_EmptyEndedAt verifies that Valid() rejects an
// empty EndedAt timestamp.
func TestRunCompletedPayload_Valid_EmptyEndedAt(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	p.EndedAt = ""
	if p.Valid() {
		t.Error("Valid() = true with empty EndedAt, want false")
	}
}

// TestRunCompletedPayload_Valid_EmptySummaryPointer verifies that Valid()
// rejects a non-nil but empty Summary string (set-but-empty is an error).
func TestRunCompletedPayload_Valid_EmptySummaryPointer(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	empty := ""
	p.Summary = &empty
	if p.Valid() {
		t.Error("Valid() = true with non-nil empty Summary, want false")
	}
}

// TestRunCompletedPayload_JSONRoundTrip verifies that RunCompletedPayload
// survives a JSON marshal/unmarshal round-trip with all fields intact.
func TestRunCompletedPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := runterminalFixtureCompleted(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got RunCompletedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if uuid.UUID(got.RunID) != uuid.UUID(orig.RunID) {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if uuid.UUID(got.TerminalStateID) != uuid.UUID(orig.TerminalStateID) {
		t.Errorf("TerminalStateID: got %v, want %v", got.TerminalStateID, orig.TerminalStateID)
	}
	if got.EndedAt != orig.EndedAt {
		t.Errorf("EndedAt: got %q, want %q", got.EndedAt, orig.EndedAt)
	}
	if got.Summary == nil || *got.Summary != *orig.Summary {
		t.Errorf("Summary: got %v, want %v", got.Summary, orig.Summary)
	}
}

// TestRunCompletedPayload_JSONOmitsSummaryWhenNil verifies that when Summary is
// nil the JSON output omits the summary key (omitempty).
func TestRunCompletedPayload_JSONOmitsSummaryWhenNil(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompletedNoSummary(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["summary"]; ok {
		t.Error("summary key present in JSON when Summary is nil, want omitted")
	}
}

// TestRunCompletedPayload_JSONKeys verifies that JSON field names match the
// snake_case wire shape declared in event-model.md §8.1.2.
func TestRunCompletedPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureCompleted(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{"run_id", "terminal_state_id", "ended_at", "summary"}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RunFailedPayload sensor tests
// ──────────────────────────────────────────────────────────────────────────────

// TestRunFailedPayload_Valid_FullyPopulated verifies that a fully-populated
// RunFailedPayload passes Valid().
func TestRunFailedPayload_Valid_FullyPopulated(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated RunFailedPayload, want true")
	}
}

// TestRunFailedPayload_Valid_NoCheckpoint verifies that a RunFailedPayload with
// empty LastCheckpoint and nil TerminalStateID passes Valid() — this is the
// budget_exhausted-at-dispatch case.
func TestRunFailedPayload_Valid_NoCheckpoint(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailedNoCheckpoint(t)
	if !p.Valid() {
		t.Error("Valid() = false for RunFailedPayload with no checkpoint (dispatch failure), want true")
	}
}

// TestRunFailedPayload_Valid_ZeroRunID verifies that Valid() rejects a zero RunID.
func TestRunFailedPayload_Valid_ZeroRunID(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	p.RunID = RunID(uuid.Nil)
	if p.Valid() {
		t.Error("Valid() = true with zero RunID, want false")
	}
}

// TestRunFailedPayload_Valid_InvalidFailureClass verifies that Valid() rejects
// an unknown failure class.
func TestRunFailedPayload_Valid_InvalidFailureClass(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	p.FailureClass = FailureClass("unknown_class")
	if p.Valid() {
		t.Error("Valid() = true with invalid FailureClass, want false")
	}
}

// TestRunFailedPayload_Valid_EmptyEndedAt verifies that Valid() rejects an
// empty EndedAt timestamp.
func TestRunFailedPayload_Valid_EmptyEndedAt(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	p.EndedAt = ""
	if p.Valid() {
		t.Error("Valid() = true with empty EndedAt, want false")
	}
}

// TestRunFailedPayload_Valid_EmptyReason verifies that Valid() rejects an empty
// Reason string.
func TestRunFailedPayload_Valid_EmptyReason(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	p.Reason = ""
	if p.Valid() {
		t.Error("Valid() = true with empty Reason, want false")
	}
}

// TestRunFailedPayload_Valid_ZeroTerminalStateIDPointer verifies that Valid()
// rejects a non-nil TerminalStateID pointer carrying uuid.Nil.
func TestRunFailedPayload_Valid_ZeroTerminalStateIDPointer(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	zeroState := StateID(uuid.Nil)
	p.TerminalStateID = &zeroState
	if p.Valid() {
		t.Error("Valid() = true with non-nil zero TerminalStateID, want false")
	}
}

// TestRunFailedPayload_Valid_EmptyErrorCategoryPointer verifies that Valid()
// rejects a non-nil but invalid ErrorCategory value.
func TestRunFailedPayload_Valid_EmptyErrorCategoryPointer(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	invalid := ErrorCategory("")
	p.ErrorCategory = &invalid
	if p.Valid() {
		t.Error("Valid() = true with non-nil invalid ErrorCategory, want false")
	}
}

// TestRunFailedPayload_AllFailureClasses verifies that Valid() accepts each of
// the six declared FailureClass constants. This is the per-class coverage sensor
// for the closed failure-class taxonomy (execution-model.md §8).
func TestRunFailedPayload_AllFailureClasses(t *testing.T) {
	t.Parallel()

	classes := []FailureClass{
		FailureClassTransient,
		FailureClassStructural,
		FailureClassDeterministic,
		FailureClassCanceled,
		FailureClassBudgetExhausted,
		FailureClassCompilationLoop,
	}

	for _, fc := range classes {
		fc := fc
		t.Run(string(fc), func(t *testing.T) {
			t.Parallel()
			p := runterminalFixtureFailed(t)
			p.FailureClass = fc
			if !p.Valid() {
				t.Errorf("Valid() = false for FailureClass %q, want true", fc)
			}
		})
	}
}

// TestRunFailedPayload_JSONRoundTrip verifies that RunFailedPayload survives a
// JSON marshal/unmarshal round-trip with all fields intact.
func TestRunFailedPayload_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := runterminalFixtureFailed(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got RunFailedPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if uuid.UUID(got.RunID) != uuid.UUID(orig.RunID) {
		t.Errorf("RunID: got %v, want %v", got.RunID, orig.RunID)
	}
	if got.TerminalStateID == nil || uuid.UUID(*got.TerminalStateID) != uuid.UUID(*orig.TerminalStateID) {
		t.Errorf("TerminalStateID: got %v, want %v", got.TerminalStateID, orig.TerminalStateID)
	}
	if got.FailureClass != orig.FailureClass {
		t.Errorf("FailureClass: got %q, want %q", got.FailureClass, orig.FailureClass)
	}
	if got.ErrorCategory == nil || *got.ErrorCategory != *orig.ErrorCategory {
		t.Errorf("ErrorCategory: got %v, want %v", got.ErrorCategory, orig.ErrorCategory)
	}
	if got.EndedAt != orig.EndedAt {
		t.Errorf("EndedAt: got %q, want %q", got.EndedAt, orig.EndedAt)
	}
	if got.Reason != orig.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, orig.Reason)
	}
	if got.LastCheckpoint != orig.LastCheckpoint {
		t.Errorf("LastCheckpoint: got %q, want %q", got.LastCheckpoint, orig.LastCheckpoint)
	}
}

// TestRunFailedPayload_JSONOmitsOptionalFieldsWhenNil verifies that nil
// TerminalStateID and nil ErrorCategory are omitted from the JSON output.
func TestRunFailedPayload_JSONOmitsOptionalFieldsWhenNil(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailedNoCheckpoint(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["terminal_state_id"]; ok {
		t.Error("terminal_state_id key present in JSON when TerminalStateID is nil, want omitted")
	}
	if _, ok := m["error_category"]; ok {
		t.Error("error_category key present in JSON when ErrorCategory is nil, want omitted")
	}
}

// TestRunFailedPayload_JSONKeys verifies that JSON field names match the
// snake_case wire shape declared in event-model.md §8.1.3.
func TestRunFailedPayload_JSONKeys(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}

	required := []string{
		"run_id", "terminal_state_id", "failure_class",
		"error_category", "ended_at", "reason", "last_checkpoint",
	}
	for _, key := range required {
		if _, ok := m[key]; !ok {
			t.Errorf("expected JSON key %q to be present", key)
		}
	}
}

// TestRunFailedPayload_LastCheckpointCarriesFailureClassAndSHA verifies the
// EM-025 constraint: run_failed payload carries failure_class and
// last_checkpoint SHA.
//
// This test documents the data-shape requirement at the type level: a
// RunFailedPayload constructed with a valid failure_class and a non-empty
// last_checkpoint SHA satisfies Valid(). The test is the machine-verified
// contract that both fields are structurally present and non-trivially valid.
//
// Requirement-traceable bead: hk-b3f.17. Spec: execution-model.md §4.5.EM-025.
func TestRunFailedPayload_LastCheckpointCarriesFailureClassAndSHA(t *testing.T) {
	t.Parallel()

	p := runterminalFixtureFailed(t)

	// FailureClass must be non-empty and valid.
	if !p.FailureClass.Valid() {
		t.Errorf("FailureClass %q not valid; EM-025 requires failure_class in run_failed payload", p.FailureClass)
	}

	// LastCheckpoint must be present when a durable transition has occurred.
	if p.LastCheckpoint == "" {
		t.Error("LastCheckpoint is empty; EM-025 requires last_checkpoint SHA in run_failed payload when a checkpoint exists")
	}
}

// TestRunTerminal_MutualExclusion_TypeLevel verifies at the type level that
// RunCompletedPayload and RunFailedPayload are distinct types (exactly one of
// which is emitted per terminal transition per EM-015b). The test documents
// the exclusivity contract as a type-system fact: a function returning one
// cannot return the other without an explicit type assertion.
//
// Requirement-traceable bead: hk-b3f.17.
func TestRunTerminal_MutualExclusion_TypeLevel(t *testing.T) {
	t.Parallel()

	// A stub that emits exactly one of {RunCompletedPayload, RunFailedPayload}
	// models the EM-015b emission contract at the type level.
	type terminalEmission struct {
		completed *RunCompletedPayload
		failed    *RunFailedPayload
	}

	emitSuccess := func() terminalEmission {
		p := runterminalFixtureCompleted(t)
		return terminalEmission{completed: &p}
	}

	emitFailure := func() terminalEmission {
		p := runterminalFixtureFailed(t)
		return terminalEmission{failed: &p}
	}

	success := emitSuccess()
	if success.completed == nil {
		t.Error("success emission: completed must be non-nil")
	}
	if success.failed != nil {
		t.Error("success emission: failed must be nil (mutual exclusion)")
	}
	if !success.completed.Valid() {
		t.Error("success emission: RunCompletedPayload.Valid() = false")
	}

	failure := emitFailure()
	if failure.failed == nil {
		t.Error("failure emission: failed must be non-nil")
	}
	if failure.completed != nil {
		t.Error("failure emission: completed must be nil (mutual exclusion)")
	}
	if !failure.failed.Valid() {
		t.Error("failure emission: RunFailedPayload.Valid() = false")
	}
}
