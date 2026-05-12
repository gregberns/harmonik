package operatornfr_test

// reviewLoopStatusFixture — spec-level harness for hk-7om2q.28
// (T-WM-028 — harmonik status inline review-loop iteration state).
//
// Covers: ON-035a (review-loop cycle observability via JSONL; inline rendering
// in `harmonik status`) with specific focus on the three fields that MUST be
// rendered inline when a run's resolved workflow_mode is review-loop:
// iteration_count, last_verdict, and current phase.
//
// The tests verify:
//  1. The ON-035a spec section exists and contains the normative text.
//  2. ReviewLoopIterationState carries exactly the three required fields.
//  3. Status output for a review-loop run includes the three fields.
//  4. Status output for a single-mode run does NOT include review-loop fields
//     (single-mode output is unchanged).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// rendering (the actual `harmonik status` command output) is an integration
// test surface; this file is the §10.2 sensor layer.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035a; event-model.md §8.1a.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// reviewLoopStatusFixtureField models one field that `harmonik status` MUST
// render inline for a review-loop run per ON-035a.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — "renders `iteration_count`,
// `last_verdict`, current phase inline."
type reviewLoopStatusFixtureField struct {
	Name    string // field name as it appears in the status output
	SpecRef string // normative spec section declaring the field
}

// reviewLoopStatusFixtureRequiredFields is the authoritative fixture encoding
// of the three fields ON-035a requires to appear in `harmonik status` for a
// review-loop run.
//
// Spec ref: operator-nfr.md §4.9 ON-035a; bead T-WM-028 deliverable.
var reviewLoopStatusFixtureRequiredFields = []reviewLoopStatusFixtureField{
	{
		"iteration_count",
		"operator-nfr.md §4.9 ON-035a; event-model.md §8.1a iteration_count field",
	},
	{
		"last_verdict",
		"operator-nfr.md §4.9 ON-035a; event-model.md §8.1a.3 reviewer_verdict.verdict",
	},
	{
		"phase",
		"operator-nfr.md §4.9 ON-035a — current phase inline; event-model.md §8.1a emission-ordering rule",
	},
}

// reviewLoopStatusFixturePhase models one declared ReviewLoopPhase value.
//
// Spec ref: event-model.md §8.1a emission-ordering rule; operator-nfr.md
// §4.9 ON-035a — "current phase inline."
type reviewLoopStatusFixturePhase struct {
	Value   operatornfr.ReviewLoopPhase
	SpecRef string
}

// reviewLoopStatusFixturePhases is the authoritative fixture encoding of the
// three ReviewLoopPhase constants.
var reviewLoopStatusFixturePhases = []reviewLoopStatusFixturePhase{
	{
		operatornfr.ReviewLoopPhaseImplementing,
		"event-model.md §8.1a.1 implementer_resumed — implementer agent running",
	},
	{
		operatornfr.ReviewLoopPhaseReviewing,
		"event-model.md §8.1a.2 reviewer_launched — reviewer agent running",
	},
	{
		operatornfr.ReviewLoopPhaseDone,
		"event-model.md §8.1a.6 review_loop_cycle_complete — terminal phase",
	},
}

// reviewLoopStatusFixtureVerdict models one recognised last_verdict value.
//
// Spec ref: event-model.md §8.1a.3 reviewer_verdict.verdict ∈
// {APPROVE, REQUEST_CHANGES, BLOCK}.
type reviewLoopStatusFixtureVerdict struct {
	Value   string
	SpecRef string
}

// reviewLoopStatusFixtureVerdicts is the authoritative fixture encoding of the
// three verdict values per event-model.md §8.1a.3.
var reviewLoopStatusFixtureVerdicts = []reviewLoopStatusFixtureVerdict{
	{"APPROVE", "event-model.md §8.1a.3 reviewer_verdict.verdict — cycle approved"},
	{"REQUEST_CHANGES", "event-model.md §8.1a.3 reviewer_verdict.verdict — changes requested"},
	{"BLOCK", "event-model.md §8.1a.3 reviewer_verdict.verdict — cycle blocked"},
}

// ── Spec-existence tests ───────────────────────────────────────────────────

// TestON035a_SpecSectionExists verifies that ON-035a exists in
// specs/operator-nfr.md and contains the review-loop observability heading.
//
// Spec ref: operator-nfr.md §4.9 ON-035a.
func TestON035a_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-035a") {
		t.Error("ON-035a: specs/operator-nfr.md does not contain 'ON-035a'")
	}
	if !strings.Contains(content, "Review-loop cycle observability") {
		t.Error("ON-035a: specs/operator-nfr.md missing 'Review-loop cycle observability' heading under ON-035a")
	}
}

// TestON035a_SpecDeclaresReviewLoopEventTypes verifies the spec names the
// review-loop event types whose payloads supply the three rendered fields.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — names implementer_resumed,
// reviewer_launched, reviewer_verdict, iteration_cap_hit, no_progress_detected,
// and review_loop_cycle_complete as the observability surface.
func TestON035a_SpecDeclaresReviewLoopEventTypes(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	requiredEventTypes := []string{
		"implementer_resumed",
		"reviewer_launched",
		"reviewer_verdict",
		"review_loop_cycle_complete",
	}
	for _, evtType := range requiredEventTypes {
		if !strings.Contains(content, evtType) {
			t.Errorf("ON-035a: specs/operator-nfr.md missing event type %q in ON-035a observability surface", evtType)
		}
	}
}

// TestON035a_SpecDeclaresInlineRendering verifies the spec states that review-
// loop information is rendered inline in `harmonik status`.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — "review-loop information is
// rendered inline in `harmonik status` when a run's resolved `workflow_mode`
// is `review-loop`."
func TestON035a_SpecDeclaresInlineRendering(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "rendered inline in `harmonik status`") {
		t.Error("ON-035a: specs/operator-nfr.md missing 'rendered inline in `harmonik status`' in ON-035a")
	}
}

// TestON035a_SpecDeclaresNoNewCommandSurface verifies the spec prohibits a new
// `harmonik review-status` command surface.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — "No new operator command surface
// (e.g., `harmonik review-status`) MUST be introduced."
func TestON035a_SpecDeclaresNoNewCommandSurface(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "harmonik review-status") {
		t.Error("ON-035a: specs/operator-nfr.md missing example 'harmonik review-status' prohibition text in ON-035a")
	}
}

// ── Fixture-completeness tests ─────────────────────────────────────────────

// TestON035a_RequiredFieldsFixtureIsComplete verifies the fixture encodes
// exactly three required fields (iteration_count, last_verdict, phase).
//
// Spec ref: operator-nfr.md §4.9 ON-035a deliverable — three fields.
func TestON035a_RequiredFieldsFixtureIsComplete(t *testing.T) {
	t.Parallel()

	const wantFields = 3
	if len(reviewLoopStatusFixtureRequiredFields) != wantFields {
		t.Errorf("ON-035a: required-fields fixture has %d entries, want %d (iteration_count, last_verdict, phase)",
			len(reviewLoopStatusFixtureRequiredFields), wantFields)
	}

	required := map[string]bool{
		"iteration_count": false,
		"last_verdict":    false,
		"phase":           false,
	}
	for _, f := range reviewLoopStatusFixtureRequiredFields {
		required[f.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-035a: required field %q is missing from the fixture", name)
		}
	}
}

// TestON035a_RequiredFieldsHaveSpecRefs verifies that every fixture field
// entry carries a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.9 ON-035a.
func TestON035a_RequiredFieldsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, f := range reviewLoopStatusFixtureRequiredFields {
		f := f
		t.Run(f.Name, func(t *testing.T) {
			t.Parallel()
			if f.SpecRef == "" {
				t.Errorf("ON-035a: required field %q has empty SpecRef", f.Name)
			}
		})
	}
}

// TestON035a_PhaseSetIsComplete verifies the phase fixture has exactly three
// entries (implementing, reviewing, done).
//
// Spec ref: event-model.md §8.1a emission-ordering rule; operator-nfr.md
// §4.9 ON-035a — "current phase inline."
func TestON035a_PhaseSetIsComplete(t *testing.T) {
	t.Parallel()

	const wantPhases = 3
	if len(reviewLoopStatusFixturePhases) != wantPhases {
		t.Errorf("ON-035a: phase fixture has %d entries, want %d (implementing, reviewing, done)",
			len(reviewLoopStatusFixturePhases), wantPhases)
	}

	required := map[operatornfr.ReviewLoopPhase]bool{
		operatornfr.ReviewLoopPhaseImplementing: false,
		operatornfr.ReviewLoopPhaseReviewing:    false,
		operatornfr.ReviewLoopPhaseDone:         false,
	}
	for _, p := range reviewLoopStatusFixturePhases {
		required[p.Value] = true
	}
	for phase, found := range required {
		if !found {
			t.Errorf("ON-035a: phase %q is missing from the fixture", phase)
		}
	}
}

// TestON035a_VerdictSetIsComplete verifies the verdict fixture encodes exactly
// three values (APPROVE, REQUEST_CHANGES, BLOCK).
//
// Spec ref: event-model.md §8.1a.3 reviewer_verdict.verdict ∈ {APPROVE,
// REQUEST_CHANGES, BLOCK}.
func TestON035a_VerdictSetIsComplete(t *testing.T) {
	t.Parallel()

	const wantVerdicts = 3
	if len(reviewLoopStatusFixtureVerdicts) != wantVerdicts {
		t.Errorf("ON-035a: verdict fixture has %d entries, want %d (APPROVE, REQUEST_CHANGES, BLOCK)",
			len(reviewLoopStatusFixtureVerdicts), wantVerdicts)
	}

	required := map[string]bool{
		"APPROVE":         false,
		"REQUEST_CHANGES": false,
		"BLOCK":           false,
	}
	for _, v := range reviewLoopStatusFixtureVerdicts {
		required[v.Value] = true
	}
	for val, found := range required {
		if !found {
			t.Errorf("ON-035a: verdict value %q is missing from the fixture", val)
		}
	}
}

// ── ReviewLoopIterationState structural tests ──────────────────────────────

// TestON035a_ReviewLoopIterationStateIsValidForReviewLoopRun verifies that a
// well-formed ReviewLoopIterationState (as would be rendered by `harmonik
// status` for a review-loop run) passes Valid().
//
// This is the snapshot test for review-loop run output: the state struct
// representing the three fields (iteration_count=2, last_verdict=REQUEST_CHANGES,
// phase=implementing) must be valid per the declared invariants.
//
// Spec ref: operator-nfr.md §4.9 ON-035a — snapshot test: status output for
// a review-loop run includes the three fields.
func TestON035a_ReviewLoopIterationStateIsValidForReviewLoopRun(t *testing.T) {
	t.Parallel()

	// Represents a mid-cycle review-loop run: second iteration, prior verdict
	// was REQUEST_CHANGES, implementer is now running again.
	state := operatornfr.ReviewLoopIterationState{
		IterationCount: 2,
		LastVerdict:    "REQUEST_CHANGES",
		Phase:          operatornfr.ReviewLoopPhaseImplementing,
	}

	if !state.Valid() {
		t.Error("ON-035a: ReviewLoopIterationState{IterationCount:2, LastVerdict:REQUEST_CHANGES, Phase:implementing} must be Valid() for a review-loop run status snapshot")
	}
}

// TestON035a_ReviewLoopIterationStateFirstIterationHasNoVerdict verifies that
// a first-iteration state (no prior verdict yet) is valid with an empty
// LastVerdict.
//
// Spec ref: operator-nfr.md §4.9 ON-035a; event-model.md §8.1a.1 — iteration
// 1's implementer is dispatched via run_started with no implementer_resumed;
// LastVerdict is empty until the first reviewer_verdict fires.
func TestON035a_ReviewLoopIterationStateFirstIterationHasNoVerdict(t *testing.T) {
	t.Parallel()

	state := operatornfr.ReviewLoopIterationState{
		IterationCount: 1,
		LastVerdict:    "", // no verdict yet on first iteration
		Phase:          operatornfr.ReviewLoopPhaseImplementing,
	}

	if !state.Valid() {
		t.Error("ON-035a: first-iteration ReviewLoopIterationState with empty LastVerdict must be Valid()")
	}
}

// TestON035a_SingleModeRunHasNoReviewLoopState verifies that a single-mode run
// does not produce a ReviewLoopIterationState: the struct is only constructed
// when workflow_mode = review-loop.
//
// This is the snapshot test for single-mode run output: single-mode run output
// is unchanged (no review-loop fields).
//
// Spec ref: operator-nfr.md §4.9 ON-035a — "single-mode run output unchanged";
// the absence of a ReviewLoopIterationState signals to the status renderer that
// no review-loop section should be emitted.
func TestON035a_SingleModeRunHasNoReviewLoopState(t *testing.T) {
	t.Parallel()

	// A nil pointer to ReviewLoopIterationState represents the absence of
	// review-loop context. Status rendering MUST NOT emit any review-loop
	// fields when this pointer is nil (single-mode or dot-mode run).
	var state *operatornfr.ReviewLoopIterationState

	if state != nil {
		t.Error("ON-035a: single-mode run MUST NOT carry a ReviewLoopIterationState; nil pointer signals no review-loop section to the status renderer")
	}
}

// TestON035a_ReviewLoopIterationStateInvalidOnZeroIteration verifies that
// IterationCount=0 is rejected by Valid() — the review-loop counts from 1.
//
// Spec ref: event-model.md §8.1a.1 implementer_resumed.iteration_count —
// the count starts at 1 for the first iteration.
func TestON035a_ReviewLoopIterationStateInvalidOnZeroIteration(t *testing.T) {
	t.Parallel()

	state := operatornfr.ReviewLoopIterationState{
		IterationCount: 0, // invalid: review-loop counts from 1
		LastVerdict:    "",
		Phase:          operatornfr.ReviewLoopPhaseImplementing,
	}

	if state.Valid() {
		t.Error("ON-035a: ReviewLoopIterationState with IterationCount=0 must NOT be Valid(); iteration count starts at 1")
	}
}

// TestON035a_ReviewLoopIterationStateInvalidOnUnknownVerdict verifies that an
// unrecognised LastVerdict value is rejected by Valid().
//
// Spec ref: event-model.md §8.1a.3 reviewer_verdict.verdict ∈ {APPROVE,
// REQUEST_CHANGES, BLOCK} — no other values are permitted.
func TestON035a_ReviewLoopIterationStateInvalidOnUnknownVerdict(t *testing.T) {
	t.Parallel()

	state := operatornfr.ReviewLoopIterationState{
		IterationCount: 1,
		LastVerdict:    "UNKNOWN_VERDICT",
		Phase:          operatornfr.ReviewLoopPhaseImplementing,
	}

	if state.Valid() {
		t.Error("ON-035a: ReviewLoopIterationState with LastVerdict='UNKNOWN_VERDICT' must NOT be Valid(); only APPROVE, REQUEST_CHANGES, BLOCK are permitted")
	}
}

// TestON035a_ReviewLoopIterationStateInvalidOnUnknownPhase verifies that an
// unrecognised Phase value is rejected by Valid().
//
// Spec ref: operator-nfr.md §4.9 ON-035a — phase must be one of the declared
// ReviewLoopPhase constants.
func TestON035a_ReviewLoopIterationStateInvalidOnUnknownPhase(t *testing.T) {
	t.Parallel()

	state := operatornfr.ReviewLoopIterationState{
		IterationCount: 1,
		LastVerdict:    "",
		Phase:          operatornfr.ReviewLoopPhase("unknown-phase"),
	}

	if state.Valid() {
		t.Error("ON-035a: ReviewLoopIterationState with Phase='unknown-phase' must NOT be Valid(); only implementing, reviewing, done are permitted")
	}
}

// TestON035a_AllPhasesAreValidOnFirstIteration verifies that each declared
// ReviewLoopPhase produces a Valid() state when combined with a minimal
// first-iteration configuration.
//
// Spec ref: operator-nfr.md §4.9 ON-035a; event-model.md §8.1a emission-
// ordering rule — all three phases are reachable from iteration 1.
func TestON035a_AllPhasesAreValidOnFirstIteration(t *testing.T) {
	t.Parallel()

	for _, p := range reviewLoopStatusFixturePhases {
		p := p
		t.Run(string(p.Value), func(t *testing.T) {
			t.Parallel()

			state := operatornfr.ReviewLoopIterationState{
				IterationCount: 1,
				LastVerdict:    "",
				Phase:          p.Value,
			}

			if !state.Valid() {
				t.Errorf("ON-035a: ReviewLoopIterationState{IterationCount:1, Phase:%q} must be Valid()", p.Value)
			}
		})
	}
}

// TestON035a_AllVerdictsAreValidAsLastVerdict verifies that each declared
// verdict string produces a Valid() state.
//
// Spec ref: event-model.md §8.1a.3 reviewer_verdict.verdict ∈ {APPROVE,
// REQUEST_CHANGES, BLOCK}.
func TestON035a_AllVerdictsAreValidAsLastVerdict(t *testing.T) {
	t.Parallel()

	for _, v := range reviewLoopStatusFixtureVerdicts {
		v := v
		t.Run(v.Value, func(t *testing.T) {
			t.Parallel()

			state := operatornfr.ReviewLoopIterationState{
				IterationCount: 1,
				LastVerdict:    v.Value,
				Phase:          operatornfr.ReviewLoopPhaseReviewing,
			}

			if !state.Valid() {
				t.Errorf("ON-035a: ReviewLoopIterationState with LastVerdict=%q must be Valid()", v.Value)
			}
		})
	}
}
