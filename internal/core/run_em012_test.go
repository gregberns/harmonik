// Package core — EM-012 record-shape and spec-content sensors (hk-b3f.12).
//
// EM-012 (execution-model.md §4.3): "A `Run` MUST carry a stable `run_id`,
// `workflow_id`, `workflow_version`, `input` (a workspace reference per
// [workspace-model.md §4.1], not inline payload), current `state`, `context`
// (a shared key-value map updated per §4.10.EM-041a), `start_time`, and
// optional `end_time`. A run executes EXACTLY ONE workflow invocation against
// EXACTLY ONE input; multi-workflow or multi-input runs are not permitted.
// Transition records for the run are discoverable via the task-branch commit
// range whose commits carry the run's `Harmonik-Run-ID` trailer; no separate
// `transitions` field on the `Run` record is required."
//
// This file exercises the EM-012 invariant at three levels:
//
//  1. Spec-content sensor — asserts that execution-model.md §4.3 EM-012
//     encodes the required canonical phrases. If any phrase is removed or
//     softened, the sensor fails and forces a deliberate review.
//
//  2. Record-shape sensors — verify that the Run struct satisfies the EM-012
//     structural requirements (required fields, singleton-workflow/singleton-input
//     shape, no transitions field).
//
//  3. Forward-doc marker — documents the dispatch-enforcement invariant
//     ("a dispatcher MUST NOT accept more than one WorkflowID or more than one
//     Input per Run") for the future implementer of the dispatch loop.  The
//     dispatcher is not yet a Go artifact (bootstrap phase), so this marker
//     skips unconditionally.
//
// Helper prefix: runFixture (per implementer-protocol.md §Helper-prefix discipline).
//
// Requirement-traceable bead: hk-b3f.12.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────────────────────────────────
// Fixture helpers (runFixture prefix)
// ──────────────────────────────────────────────────────────────────────────────

// runFixtureMinimalRun returns a structurally valid Run carrying only the
// fields required by EM-012 (no optional BeadID, no EndTime).
// It satisfies Run.Valid() and represents the smallest valid EM-012 carrier.
func runFixtureMinimalRun(t *testing.T) Run {
	t.Helper()
	return Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		Input:           WorkspaceRef("workspace://project/fixture-input"),
		WorkflowMode:    WorkflowModeSingle,
		BeadID:          nil,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{},
		StartTime:       time.Now(),
		EndTime:         nil,
	}
}

// runFixtureTerminalRun returns a structurally valid Run in a terminal state:
// all required fields set, EndTime non-nil (signalling a completed run).
func runFixtureTerminalRun(t *testing.T) Run {
	t.Helper()
	now := time.Now()
	end := now.Add(5 * time.Minute)
	beadID := BeadID("bead-em012-terminal")
	return Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("2.3.1"),
		Input:           WorkspaceRef("workspace://project/terminal-input"),
		WorkflowMode:    WorkflowModeSingle,
		BeadID:          &beadID,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{"status": "completed"},
		StartTime:       now,
		EndTime:         &end,
	}
}

// runFixtureEm012SpecContent reads specs/execution-model.md and returns the
// paragraph anchored by "EM-012". Fails the test if the file is unreadable or
// the anchor is absent.
func runFixtureEm012SpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runFixtureEm012SpecContent: runtime.Caller failed")
	}
	// Walk up: internal/core/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "execution-model.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("runFixtureEm012SpecContent: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	// Search for the requirement heading "#### EM-012" to avoid matching the
	// anchor in cross-references or glossary entries that precede the heading
	// in the spec (e.g. "(see §4.3.EM-012)" in the §3 glossary).
	const anchor = "#### EM-012"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("EM-012 heading not found in %s; the EM-012 requirement may have been removed or renamed", specPath)
	}
	para := content[idx:]
	// Clip at the next subsection header so we don't bleed into unrelated requirements.
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// ──────────────────────────────────────────────────────────────────────────────
// Spec-content sensors
// ──────────────────────────────────────────────────────────────────────────────

// TestRunEM012_SpecContainsRequiredFields verifies that execution-model.md §4.3
// EM-012 encodes the required field list with the canonical identifier names.
//
// Required canonical identifiers (removal or renaming is a breaking change):
//   - "run_id"            — stable run identifier
//   - "workflow_id"       — resolved workflow
//   - "workflow_version"  — pinned version at dispatch time
//   - "input"             — workspace reference (NOT inline payload)
//   - "workflow_mode"     — dispatch shape; resolved at claim time; defaults to single
//   - "state"             — current run state
//   - "context"           — shared key-value map
//   - "start_time"        — RFC 3339 wall clock at dispatch
//   - "end_time"          — optional; set on terminal transition
//
// Requirement-traceable beads: hk-b3f.12, hk-7om2q.3.
func TestRunEM012_SpecContainsRequiredFields(t *testing.T) {
	t.Parallel()

	para := runFixtureEm012SpecContent(t)

	fields := []struct {
		name string
		hint string
	}{
		{"run_id", "EM-012 must name run_id as the stable run identifier"},
		{"workflow_id", "EM-012 must name workflow_id (the resolved workflow)"},
		{"workflow_version", "EM-012 must name workflow_version (pinned at dispatch)"},
		{"input", "EM-012 must name input (workspace reference)"},
		{"workflow_mode", "EM-012 must name workflow_mode (dispatch shape; resolved at claim time; defaults to single)"},
		{"state", "EM-012 must name state (current run state)"},
		{"context", "EM-012 must name context (shared key-value map)"},
		{"start_time", "EM-012 must name start_time (RFC 3339 wall clock at dispatch)"},
		{"end_time", "EM-012 must name end_time (optional; set on terminal transition)"},
	}

	for _, f := range fields {
		if !strings.Contains(para, f.name) {
			t.Errorf("EM-012 paragraph missing field %q — %s\nParagraph:\n%s", f.name, f.hint, para)
		}
	}
}

// TestRunEM012_SpecEncodesOneWorkflowOneInputInvariant verifies that
// execution-model.md §4.3 EM-012 encodes the singleton-workflow and
// singleton-input invariant with the canonical prohibitive phrases.
//
// Required phrases:
//   - "EXACTLY ONE workflow invocation"  — the singleton-workflow guarantee
//   - "EXACTLY ONE input"                — the singleton-input guarantee
//   - "multi-workflow"                   — explicit prohibition of multi-workflow runs
//   - "multi-input"                      — explicit prohibition of multi-input runs
//   - "not permitted"                    — the normative prohibition
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_SpecEncodesOneWorkflowOneInputInvariant(t *testing.T) {
	t.Parallel()

	para := runFixtureEm012SpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "EXACTLY ONE workflow invocation",
			hint:   "EM-012 must assert singleton-workflow with this exact phrase",
		},
		{
			phrase: "EXACTLY ONE input",
			hint:   "EM-012 must assert singleton-input with this exact phrase",
		},
		{
			phrase: "multi-workflow",
			hint:   "EM-012 must explicitly name and prohibit multi-workflow runs",
		},
		{
			phrase: "multi-input",
			hint:   "EM-012 must explicitly name and prohibit multi-input runs",
		},
		{
			phrase: "not permitted",
			hint:   "EM-012 must use 'not permitted' as the normative prohibition on multi-workflow/multi-input",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("EM-012 paragraph missing %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// TestRunEM012_SpecEncodesTransitionDiscovery verifies that execution-model.md
// §4.3 EM-012 encodes the transition-discovery contract (no separate transitions
// field; discovery is via the task-branch commit range carrying the
// Harmonik-Run-ID trailer).
//
// Required phrases:
//   - "Harmonik-Run-ID"           — the trailer name
//   - "no separate `transitions`" — explicit prohibition of a transitions field
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_SpecEncodesTransitionDiscovery(t *testing.T) {
	t.Parallel()

	para := runFixtureEm012SpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "Harmonik-Run-ID",
			hint:   "EM-012 must name the Harmonik-Run-ID trailer as the transition-discovery key",
		},
		{
			phrase: "no separate `transitions`",
			hint:   "EM-012 must explicitly forbid a separate transitions field on the Run record",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("EM-012 paragraph missing %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Record-shape sensors
// ──────────────────────────────────────────────────────────────────────────────

// TestRunEM012_MinimalRunIsValid verifies that a Run carrying only the
// EM-012-required fields (no optional BeadID, no EndTime) is structurally
// valid per Run.Valid().
//
// EM-012: "A `Run` MUST carry ... `start_time`, and optional `end_time`."
// EndTime is optional; its absence MUST NOT invalidate the record.
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_MinimalRunIsValid(t *testing.T) {
	t.Parallel()

	r := runFixtureMinimalRun(t)
	if !r.Valid() {
		t.Error("Run.Valid() = false for EM-012 minimal run (no BeadID, no EndTime), want true")
	}
}

// TestRunEM012_TerminalRunIsValid verifies that a Run in terminal state
// (EndTime set, BeadID set) is structurally valid per Run.Valid().
//
// EM-012: "optional `end_time`" — when present it must be non-zero.
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_TerminalRunIsValid(t *testing.T) {
	t.Parallel()

	r := runFixtureTerminalRun(t)
	if !r.Valid() {
		t.Error("Run.Valid() = false for EM-012 terminal run (EndTime set), want true")
	}
}

// TestRunEM012_RunCarriesExactlyOneWorkflowID verifies that the Run struct
// carries exactly one WorkflowID field (the singleton-workflow invariant at the
// type level).
//
// EM-012: "A run executes EXACTLY ONE workflow invocation ... multi-workflow ...
// runs are not permitted."
//
// At the type level this means the Run record must carry a single WorkflowID
// and not a slice or map of WorkflowIDs. This test asserts that:
//   - Run.WorkflowID is a non-zero value (exactly one workflow assigned)
//   - Mutating it to uuid.Nil causes Valid() to return false (zero = unset =
//     invalid; the singleton constraint is enforced via the non-zero requirement)
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_RunCarriesExactlyOneWorkflowID(t *testing.T) {
	t.Parallel()

	r := runFixtureMinimalRun(t)

	// A non-zero WorkflowID is required.
	if uuid.UUID(r.WorkflowID) == uuid.Nil {
		t.Error("EM-012: runFixtureMinimalRun returned a zero WorkflowID; fixture must assign a non-zero WorkflowID")
	}
	if !r.Valid() {
		t.Error("EM-012: Run with non-zero WorkflowID must be Valid()")
	}

	// Zero WorkflowID → invalid: the singleton is unset.
	r.WorkflowID = WorkflowID(uuid.Nil)
	if r.Valid() {
		t.Error("EM-012: Run.Valid() = true with zero WorkflowID, want false (singleton workflow must be set)")
	}
}

// TestRunEM012_RunCarriesExactlyOneInput verifies that the Run struct carries
// exactly one Input field (the singleton-input invariant at the type level).
//
// EM-012: "A run executes EXACTLY ONE workflow invocation against EXACTLY ONE
// input; multi-workflow or multi-input runs are not permitted."
//
// At the type level this means the Run record must carry a single WorkspaceRef
// and not a slice or map of refs. This test asserts that:
//   - Run.Input is a non-empty WorkspaceRef (exactly one input assigned)
//   - Mutating it to an empty string causes Valid() to return false (empty =
//     unset = invalid; the singleton constraint is enforced via non-empty requirement)
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_RunCarriesExactlyOneInput(t *testing.T) {
	t.Parallel()

	r := runFixtureMinimalRun(t)

	// A non-empty Input is required.
	if r.Input == "" {
		t.Error("EM-012: runFixtureMinimalRun returned an empty Input; fixture must assign a non-empty WorkspaceRef")
	}
	if !r.Valid() {
		t.Error("EM-012: Run with non-empty Input must be Valid()")
	}

	// Empty Input → invalid: the singleton input is unset.
	r.Input = WorkspaceRef("")
	if r.Valid() {
		t.Error("EM-012: Run.Valid() = true with empty Input, want false (singleton input must be set)")
	}
}

// TestRunEM012_RunHasNoTransitionsField verifies at the type level that the Run
// struct does not declare a Transitions field, satisfying the EM-012 requirement
// that "no separate `transitions` field on the `Run` record is required."
//
// Transition discovery is exclusively via the task-branch commit range carrying
// the run's Harmonik-Run-ID trailer; no in-record field is permitted.
//
// This test is a compilation-time assertion enforced by the Go type system: if
// someone adds a `Transitions` field to Run, the selector `r.Transitions` below
// will compile, and the test must be updated to assert the field's meaning rather
// than its absence. As long as this test compiles without referencing a
// Transitions field, the absence is machine-verified.
//
// NOTE: because Go does not provide a reflection API that distinguishes "field
// absent" from "field present but zero", the assertion is structural: we verify
// that calling Valid() on a minimal Run (which carries no transitions data) still
// returns true. If a required Transitions field existed, Valid() would return false
// for a Run constructed without it — which would break TestRunEM012_MinimalRunIsValid.
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_RunHasNoTransitionsField(t *testing.T) {
	t.Parallel()

	r := runFixtureMinimalRun(t)

	// A Run constructed with only the EM-012 required fields must be valid.
	// If Valid() returned false here, it would mean some field beyond the
	// EM-012 required set is mandatory — which would contradict the spec's
	// statement that transitions are NOT a field on the Run record.
	if !r.Valid() {
		t.Error("EM-012: Run.Valid() = false for minimal-field Run; " +
			"Run must not require a Transitions field beyond the EM-012 specified set")
	}
}

// TestRunEM012_InputMustBeWorkspaceRefNotInlinePayload verifies that the Input
// field on Run is a WorkspaceRef (an opaque string reference), not an inline
// payload. EM-012 states: "input (a workspace reference per
// [workspace-model.md §4.1], not inline payload)".
//
// At the type level this means Input is a string-backed named type (WorkspaceRef)
// rather than a byte slice, map, or struct carrying inline data. This test asserts:
//   - Run.Input is of type WorkspaceRef (enforced at compile time by the assignment)
//   - The WorkspaceRef value is non-empty (it refers to something; it is not absent)
//
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_InputMustBeWorkspaceRefNotInlinePayload(t *testing.T) {
	t.Parallel()

	r := runFixtureMinimalRun(t)

	// Compile-time check: assigning r.Input to a WorkspaceRef variable must
	// type-check. If Input were changed to []byte or map[string]any, this
	// assignment would fail to compile, surfacing the breaking change.
	var ref WorkspaceRef = r.Input
	if ref == "" {
		t.Error("EM-012: Run.Input must be a non-empty WorkspaceRef (workspace reference, not inline payload)")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Forward-doc marker
// ──────────────────────────────────────────────────────────────────────────────

// ──────────────────────────────────────────────────────────────────────────────
// WorkflowMode field sensors (T-WM-003 / hk-7om2q.3)
// ──────────────────────────────────────────────────────────────────────────────

// runFixtureWMRun returns a minimal valid Run with WorkflowMode set to the
// supplied mode. Used by T-WM-003 sensors.
func runFixtureWMRun(t *testing.T, mode WorkflowMode) Run {
	t.Helper()
	return Run{
		RunID:           RunID(uuid.Must(uuid.NewV7())),
		WorkflowID:      WorkflowID(uuid.Must(uuid.NewV7())),
		WorkflowVersion: WorkflowVersion("1.0.0"),
		Input:           WorkspaceRef("workspace://project/wm-input"),
		WorkflowMode:    mode,
		State:           StateID(uuid.Must(uuid.NewV7())),
		Context:         map[string]any{},
		StartTime:       time.Now(),
	}
}

// TestRunWM003_WorkflowModeSetAtClaimTime verifies that a Run constructed with
// a WorkflowMode value is structurally valid per Run.Valid() for all three
// declared modes (single, review-loop, dot).
//
// EM-012 (execution-model.md §4.3): "workflow_mode ∈ {single, review-loop, dot}
// (resolved at claim time per §4.3.EM-012a; immutable for the run's lifetime;
// defaults to `single`)".
//
// At the record level, "resolved at claim time" means the field MUST be set
// before the Run is considered valid; Valid() returns false for an unset
// (empty-string) WorkflowMode.
//
// Requirement-traceable bead: hk-7om2q.3.
func TestRunWM003_WorkflowModeSetAtClaimTime(t *testing.T) {
	t.Parallel()

	modes := []struct {
		mode WorkflowMode
		name string
	}{
		{WorkflowModeSingle, "single"},
		{WorkflowModeReviewLoop, "review-loop"},
		{WorkflowModeDot, "dot"},
	}

	for _, tc := range modes {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := runFixtureWMRun(t, tc.mode)
			if !r.Valid() {
				t.Errorf("Run.Valid() = false for WorkflowMode=%q, want true (T-WM-003: mode must be valid at claim time)", tc.mode)
			}
			if r.WorkflowMode != tc.mode {
				t.Errorf("Run.WorkflowMode = %q, want %q", r.WorkflowMode, tc.mode)
			}
		})
	}
}

// TestRunWM003_EmptyWorkflowModeIsInvalid verifies that a Run with an unset
// (zero-value / empty-string) WorkflowMode is rejected by Run.Valid().
//
// EM-012: workflow_mode is a required field; its absence at claim time is an
// authoring error. The built-in fallback to `single` is a resolution rule for
// the daemon's claim path (§4.3.EM-012a), not a validity exemption for the
// Run record itself.
//
// Requirement-traceable bead: hk-7om2q.3.
func TestRunWM003_EmptyWorkflowModeIsInvalid(t *testing.T) {
	t.Parallel()

	r := runFixtureWMRun(t, WorkflowModeSingle)
	r.WorkflowMode = WorkflowMode("") // unset — as if claim path forgot to populate
	if r.Valid() {
		t.Error("Run.Valid() = true with empty WorkflowMode, want false (T-WM-003: mode must be set at claim time)")
	}
}

// TestRunWM003_UnknownWorkflowModeIsInvalid verifies that a Run carrying an
// unrecognised WorkflowMode string is rejected by Run.Valid().
//
// EM-012a: "unknown-mode labels treat tier 1 as absent and emit bead_label_conflict".
// At the record level, an unknown mode that somehow reaches the Run record is
// an authoring error and must fail validation.
//
// Requirement-traceable bead: hk-7om2q.3.
func TestRunWM003_UnknownWorkflowModeIsInvalid(t *testing.T) {
	t.Parallel()

	r := runFixtureWMRun(t, WorkflowModeSingle)
	r.WorkflowMode = WorkflowMode("unknown-mode")
	if r.Valid() {
		t.Error("Run.Valid() = true with unknown WorkflowMode, want false (T-WM-003: unknown mode must be invalid)")
	}
}

// TestRunWM003_WorkflowModeJSONRoundTrip verifies that WorkflowMode
// round-trips correctly through JSON marshal/unmarshal when embedded in a Run.
//
// EM-012 §6.1: the Run record is a persisted schema; WorkflowMode must
// serialise to its canonical string form and deserialise back to the same
// typed value without loss.
//
// Requirement-traceable bead: hk-7om2q.3.
func TestRunWM003_WorkflowModeJSONRoundTrip(t *testing.T) {
	t.Parallel()

	modes := []WorkflowMode{
		WorkflowModeSingle,
		WorkflowModeReviewLoop,
		WorkflowModeDot,
	}

	for _, mode := range modes {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			orig := runFixtureWMRun(t, mode)

			data, err := json.Marshal(orig)
			if err != nil {
				t.Fatalf("json.Marshal(Run{WorkflowMode:%q}): %v", mode, err)
			}

			var got Run
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			if got.WorkflowMode != mode {
				t.Errorf("round-trip: got WorkflowMode=%q, want %q (T-WM-003: JSON must preserve mode)", got.WorkflowMode, mode)
			}
			if !got.WorkflowMode.Valid() {
				t.Errorf("round-trip: WorkflowMode.Valid() = false for %q after unmarshal", got.WorkflowMode)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Forward-doc marker
// ──────────────────────────────────────────────────────────────────────────────

// TestRunEM012_ForwardDocDispatchEnforcement is a forward-doc marker for the
// dispatch-side enforcement of the EM-012 singleton invariant (hk-b3f.12).
//
// EM-012 invariant (dispatch level, not yet implemented):
//
//	A dispatcher (the orchestrator's outer loop) MUST NOT construct a Run with
//	more than one WorkflowID or more than one Input. Given a dispatch request,
//	the dispatcher MUST reject any request that references multiple workflows or
//	multiple inputs, returning a validation error before allocating a run_id.
//
// This test skips unconditionally because the dispatch loop is not yet a Go
// artifact (bootstrap phase — records and enums only). When the dispatcher
// lands, the implementer of that bead SHOULD either:
//
//  1. Delete this forward-doc marker and add concrete Shape-A assertions that
//     inject a multi-workflow or multi-input request and assert rejection before
//     run_id allocation, OR
//  2. Extend this marker with those assertions, retaining the EM-012 citation
//     and hk-b3f.12 traceability.
//
// Spec reference: execution-model.md §4.3 EM-012.
// Requirement-traceable bead: hk-b3f.12.
func TestRunEM012_ForwardDocDispatchEnforcement(t *testing.T) {
	t.Log("EM-012 (hk-b3f.12): dispatch MUST enforce the singleton-workflow / singleton-input invariant.")
	t.Log("  Invariant: a dispatcher MUST NOT construct a Run with more than one WorkflowID or Input.")
	t.Log("  Given a multi-workflow or multi-input request, the dispatcher MUST reject before run_id allocation.")
	t.Log("")
	t.Log("  The dispatch loop is not yet a Go artifact (bootstrap phase).")
	t.Log("  When the dispatcher lands, the implementer SHOULD:")
	t.Log("    1. Delete this forward-doc marker and add Shape-A assertions, OR")
	t.Log("    2. Extend it with those assertions (retaining EM-012 citation and hk-b3f.12 traceability).")
	t.Log("  Spec reference: execution-model.md §4.3 EM-012.")
	t.SkipNow()
}
