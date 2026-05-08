package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// traceFixtureState returns a fully-populated valid State for use in Trace fixtures.
func traceFixtureState(t *testing.T) State {
	t.Helper()

	return State{
		StateID:   StateID(uuid.Must(uuid.NewV7())),
		RunID:     RunID(uuid.Must(uuid.NewV7())),
		NodeID:    "build-node",
		EnteredAt: time.Now(),
		TransitionHistory: CommitRange{
			FirstCommitSHA: "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
			LastCommitSHA:  "ffff6666aaaa7777bbbb8888cccc9999dddd0000",
		},
	}
}

// traceFixture returns a fully-populated Trace with all 11 fields set to valid
// non-zero values, including optional pointer fields set to non-nil.
func traceFixture(t *testing.T) Trace {
	t.Helper()

	confidence := 0.87
	return Trace{
		PriorState:       traceFixtureState(t),
		ActorRole:        ActorRoleBuilder,
		CandidateActions: []string{"action-a", "action-b", "action-c"},
		ChosenAction:     "action-a",
		PolicyVersion:    PolicyVersion("v1.0.0"),
		ParameterVector:  map[string]any{"temperature": 0.7, "max_tokens": 4096},
		Evidence:         map[string]any{"lint_passed": true, "test_count": 42},
		Outcome:          OutcomeStatusSuccess,
		VerifierMetrics:  map[string]any{"coverage": 0.95, "warnings": 0},
		NextState:        traceFixtureState(t),
		Confidence:       &confidence,
	}
}

// TestTraceValid_AllFieldsPopulated verifies that a fully-populated Trace with
// all 11 fields set passes Valid().
func TestTraceValid_AllFieldsPopulated(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	if !tr.Valid() {
		t.Error("Valid() = false for fully-populated Trace, want true")
	}
}

// TestTraceValid_OptionalFieldsNil verifies that a Trace with all optional
// fields (ParameterVector, Evidence, VerifierMetrics, Confidence) set to nil
// still passes Valid().
func TestTraceValid_OptionalFieldsNil(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ParameterVector = nil
	tr.Evidence = nil
	tr.VerifierMetrics = nil
	tr.Confidence = nil
	if !tr.Valid() {
		t.Error("Valid() = false with all optional fields nil, want true")
	}
}

// TestTraceValid_EmptyCandidateActionsIsValid verifies that an empty (but non-nil)
// CandidateActions slice is valid for deterministic-dispatch transitions.
func TestTraceValid_EmptyCandidateActionsIsValid(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.CandidateActions = []string{}
	if !tr.Valid() {
		t.Error("Valid() = false with empty (non-nil) CandidateActions, want true")
	}
}

// TestTraceValid_NilCandidateActionsIsInvalid verifies that a nil CandidateActions
// slice fails Valid().
func TestTraceValid_NilCandidateActionsIsInvalid(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.CandidateActions = nil
	if tr.Valid() {
		t.Error("Valid() = true with nil CandidateActions, want false")
	}
}

// TestTraceValid_InvalidPriorState verifies that an invalid PriorState fails Valid().
func TestTraceValid_InvalidPriorState(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.PriorState = State{} // zero State is invalid
	if tr.Valid() {
		t.Error("Valid() = true with zero PriorState, want false")
	}
}

// TestTraceValid_EmptyActorRole verifies that an empty ActorRole fails Valid().
// (Kept for regression coverage; see actorrole_test.go for full ActorRole test suite.)
func TestTraceValid_EmptyActorRole(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ActorRole = ActorRole("")
	if tr.Valid() {
		t.Error("Valid() = true with empty ActorRole, want false")
	}
}

// TestTraceValid_EmptyChosenAction verifies that an empty ChosenAction fails Valid().
func TestTraceValid_EmptyChosenAction(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ChosenAction = ""
	if tr.Valid() {
		t.Error("Valid() = true with empty ChosenAction, want false")
	}
}

// TestTraceValid_EmptyPolicyVersion verifies that an empty PolicyVersion fails Valid().
// (Kept for regression coverage; see policyversion_test.go for full PolicyVersion test suite.)
func TestTraceValid_EmptyPolicyVersion(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.PolicyVersion = PolicyVersion("")
	if tr.Valid() {
		t.Error("Valid() = true with empty PolicyVersion, want false")
	}
}

// TestTraceValid_InvalidOutcome verifies that an invalid (unknown) OutcomeStatus
// fails Valid().
func TestTraceValid_InvalidOutcome(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.Outcome = OutcomeStatus("UNKNOWN_STATUS")
	if tr.Valid() {
		t.Error("Valid() = true with unknown OutcomeStatus, want false")
	}
}

// TestTraceValid_InvalidNextState verifies that an invalid NextState fails Valid().
func TestTraceValid_InvalidNextState(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.NextState = State{} // zero State is invalid
	if tr.Valid() {
		t.Error("Valid() = true with zero NextState, want false")
	}
}

// TestTraceValid_ConfidenceAtZero verifies that Confidence = 0.0 is valid (lower bound).
func TestTraceValid_ConfidenceAtZero(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	c := 0.0
	tr.Confidence = &c
	if !tr.Valid() {
		t.Error("Valid() = false with Confidence = 0.0, want true (0.0 is the lower bound)")
	}
}

// TestTraceValid_ConfidenceAtOne verifies that Confidence = 1.0 is valid (upper bound).
func TestTraceValid_ConfidenceAtOne(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	c := 1.0
	tr.Confidence = &c
	if !tr.Valid() {
		t.Error("Valid() = false with Confidence = 1.0, want true (1.0 is the upper bound)")
	}
}

// TestTraceValid_ConfidenceNegative verifies that a negative Confidence fails Valid().
func TestTraceValid_ConfidenceNegative(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	c := -0.01
	tr.Confidence = &c
	if tr.Valid() {
		t.Error("Valid() = true with Confidence = -0.01 (negative), want false")
	}
}

// TestTraceValid_ConfidenceAboveOne verifies that Confidence > 1.0 fails Valid().
func TestTraceValid_ConfidenceAboveOne(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	c := 1.01
	tr.Confidence = &c
	if tr.Valid() {
		t.Error("Valid() = true with Confidence = 1.01 (above 1.0), want false")
	}
}

// TestTraceValid_AllOutcomeStatusValues verifies Valid() for every declared
// OutcomeStatus value on the Outcome field.
func TestTraceValid_AllOutcomeStatusValues(t *testing.T) {
	t.Parallel()

	validStatuses := []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
		OutcomeStatusPartialSuccess,
	}

	for _, s := range validStatuses {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			tr := traceFixture(t)
			tr.Outcome = s
			if !tr.Valid() {
				t.Errorf("Valid() = false with Outcome = %q, want true", s)
			}
		})
	}
}

// TestTraceJSONRoundTrip verifies that a fully-populated Trace survives a
// json.Marshal / json.Unmarshal round-trip with all fields preserved.
// Traces are stored as JSON sibling files per execution-model.md §4.4.EM-018.
func TestTraceJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := traceFixture(t)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded Trace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded Trace fails Valid(), want true")
	}

	// Spot-check scalar fields that must survive the round-trip exactly.
	if decoded.ActorRole != original.ActorRole {
		t.Errorf("ActorRole: got %q, want %q", string(decoded.ActorRole), string(original.ActorRole))
	}
	if decoded.ChosenAction != original.ChosenAction {
		t.Errorf("ChosenAction: got %q, want %q", decoded.ChosenAction, original.ChosenAction)
	}
	if decoded.PolicyVersion != original.PolicyVersion {
		t.Errorf("PolicyVersion: got %q, want %q", string(decoded.PolicyVersion), string(original.PolicyVersion))
	}
	if decoded.Outcome != original.Outcome {
		t.Errorf("Outcome: got %q, want %q", decoded.Outcome, original.Outcome)
	}
	if decoded.Confidence == nil {
		t.Error("Confidence is nil after round-trip, want non-nil")
	} else if *decoded.Confidence != *original.Confidence {
		t.Errorf("Confidence: got %v, want %v", *decoded.Confidence, *original.Confidence)
	}
	if len(decoded.CandidateActions) != len(original.CandidateActions) {
		t.Errorf("CandidateActions length: got %d, want %d",
			len(decoded.CandidateActions), len(original.CandidateActions))
	}
}

// TestTraceJSONRoundTrip_NilOptionals verifies that a Trace with all optional
// fields nil also survives the round-trip without introducing spurious non-nil values.
func TestTraceJSONRoundTrip_NilOptionals(t *testing.T) {
	t.Parallel()

	original := traceFixture(t)
	original.ParameterVector = nil
	original.Evidence = nil
	original.VerifierMetrics = nil
	original.Confidence = nil

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded Trace
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded Trace (nil optionals) fails Valid(), want true")
	}
	if decoded.Confidence != nil {
		t.Errorf("Confidence: got non-nil after round-trip, want nil")
	}
}
