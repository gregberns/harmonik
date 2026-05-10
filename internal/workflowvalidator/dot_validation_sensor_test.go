package workflowvalidator

// dot_validation_sensor_test.go — sensor tests for EM-040.
//
// EM-040 requires: any agent path that produces a DOT workflow MUST call
// PreRunValidator.Validate before submitting the workflow to the daemon.
// Submission paths that skip validation are structural violations of the
// centralized-controller principle.
//
// Because the daemon submission RPC does not yet exist (it is declared in
// process-lifecycle.md §4.10 and will land in a later bead), these tests
// enforce the invariant at the validator API boundary using a test-double
// submit path. The sensor pattern: a stub submitter records whether Validate
// was called before Submit; tests assert that valid DOT always goes through
// Validate before any Submit call, and that DOT which fails Validate never
// reaches Submit.
//
// Helper prefix: dotValidationFixture (per implementer-protocol.md §Helper-prefix
// discipline; differs from preRunValidatorFixture used in validator_test.go so
// the two bead surfaces stay cleanly separated per hk-b3f.53 / hk-b3f.52 note).
//
// Spec ref: specs/execution-model.md §4.9.EM-040.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"testing"
)

// --- Fixture helpers (prefix: dotValidationFixture) ---

// dotValidationFixtureStubSubmitter is a test double that records whether
// Validate was called before Submit and whether Submit was ever called.
//
// Usage in tests: call dotValidationFixtureNewStubSubmitter, pass the
// validator under test to the submission path under test, then assert on
// the stub's observed call sequence.
type dotValidationFixtureStubSubmitter struct {
	// validateCalled is set to true by the test harness when Validate succeeds.
	// It simulates the agent recording "I ran validation" before proceeding.
	validateCalled bool
	// submitCalled is set to true when Submit is called.
	submitCalled bool
	// submittedWithValidation is true iff Submit was called after validateCalled
	// was set — i.e., the caller honoured the EM-040 validate-before-submit rule.
	submittedWithValidation bool
	// dotSrc is the last DOT source passed to Submit.
	dotSrc string
}

// dotValidationFixtureNewStubSubmitter returns a zeroed stub submitter.
func dotValidationFixtureNewStubSubmitter() *dotValidationFixtureStubSubmitter {
	return &dotValidationFixtureStubSubmitter{}
}

// recordValidateOK marks that Validate was called and returned nil (success).
// Call this immediately after a successful v.Validate(dotSrc) call.
func (s *dotValidationFixtureStubSubmitter) recordValidateOK() {
	s.validateCalled = true
}

// submit simulates the daemon submission RPC (process-lifecycle.md §4.10).
// It records whether validateCalled was set at the time of the call.
func (s *dotValidationFixtureStubSubmitter) submit(dotSrc string) {
	s.submitCalled = true
	s.dotSrc = dotSrc
	s.submittedWithValidation = s.validateCalled
}

// dotValidationFixtureValidateThenSubmit is the canonical compliant agent path:
//  1. Produce a DOT document.
//  2. Run PreRunValidator.Validate.
//  3. Only on success, submit to the daemon.
//
// This is the reference implementation of EM-040 from the agent side.
// Returns the validation error if Validate fails; nil on success.
func dotValidationFixtureValidateThenSubmit(
	v *PreRunValidator,
	dotSrc string,
	stub *dotValidationFixtureStubSubmitter,
) error {
	err := v.Validate(dotSrc)
	if err != nil {
		// Validation failed: do NOT call submit. EM-040 — submission paths
		// skipping validation are structural violations; here the agent path
		// correctly gates submit behind a successful Validate.
		return err
	}
	stub.recordValidateOK()
	stub.submit(dotSrc)
	return nil
}

// dotValidationFixtureSubmitWithoutValidate is the NON-COMPLIANT agent path:
// it calls submit directly without running Validate first.
//
// This function exists ONLY to test that the sensor detects the violation.
// No production code should use this pattern.
func dotValidationFixtureSubmitWithoutValidate(
	dotSrc string,
	stub *dotValidationFixtureStubSubmitter,
) {
	// Skips Validate — structural violation of EM-040.
	stub.submit(dotSrc)
}

// dotValidationFixtureMinimalValidDOT returns a minimal valid single-node
// workflow DOT string with all required attributes, suitable for EM-040
// sensor tests that need DOT which passes the pre-run validator.
func dotValidationFixtureMinimalValidDOT() string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2b-0040-7000-8000-000000000040"
        name              = "dot-validation-sensor-fixture"
        version           = "0.1.0"
        start_node_id     = "start"
        terminal_node_ids = "end"
    ]

    start [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    end [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    start -> end [ordering_key = "a"]
}`
}

// dotValidationFixtureInvalidDOT returns a DOT string that fails the pre-run
// validator (missing start_node_id). Used by tests that verify the agent path
// correctly blocks submission when Validate fails.
func dotValidationFixtureInvalidDOT() string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2b-0040-7000-8000-000000000041"
        name              = "dot-validation-invalid-fixture"
        version           = "0.1.0"
        terminal_node_ids = "end"
    ]

    end [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]
}`
}

// --- Tests: EM-040 validate-before-submit invariant ---

// TestDotValidation_CompliantPathValidatesBeforeSubmit asserts that the
// canonical compliant agent path (dotValidationFixtureValidateThenSubmit)
// always calls Validate before Submit for valid DOT.
//
// Sensor assertion: stub.submittedWithValidation MUST be true.
func TestDotValidation_CompliantPathValidatesBeforeSubmit(t *testing.T) {
	t.Parallel()
	v := New(nil, nil)
	stub := dotValidationFixtureNewStubSubmitter()

	err := dotValidationFixtureValidateThenSubmit(v, dotValidationFixtureMinimalValidDOT(), stub)
	if err != nil {
		t.Fatalf("dotValidationFixtureValidateThenSubmit returned unexpected error: %v", err)
	}
	if !stub.submitCalled {
		t.Fatal("submit was not called; expected compliant path to submit valid DOT")
	}
	if !stub.submittedWithValidation {
		// EM-040 violation: submit was reached without Validate having been called.
		t.Fatal("EM-040 violation: submit was called without prior Validate (submittedWithValidation=false)")
	}
}

// TestDotValidation_CompliantPathBlocksSubmitOnValidationFailure asserts that
// the canonical compliant agent path does NOT call Submit when Validate fails.
//
// Sensor assertion: stub.submitCalled MUST be false when DOT is invalid.
func TestDotValidation_CompliantPathBlocksSubmitOnValidationFailure(t *testing.T) {
	t.Parallel()
	v := New(nil, nil)
	stub := dotValidationFixtureNewStubSubmitter()

	err := dotValidationFixtureValidateThenSubmit(v, dotValidationFixtureInvalidDOT(), stub)
	if err == nil {
		t.Fatal("dotValidationFixtureValidateThenSubmit returned nil for invalid DOT; want validation error")
	}
	if stub.submitCalled {
		// EM-040 violation: submit must not be reached when Validate fails.
		t.Fatal("EM-040 violation: submit was called even though Validate returned an error")
	}
}

// TestDotValidation_NonCompliantPathDetected asserts that the sensor correctly
// identifies a non-compliant path (submit without prior Validate).
//
// This test documents the invariant: submittedWithValidation is false when
// the agent skips validation entirely (the structural violation EM-040 forbids).
func TestDotValidation_NonCompliantPathDetected(t *testing.T) {
	t.Parallel()
	stub := dotValidationFixtureNewStubSubmitter()

	// Non-compliant path: skip Validate, call Submit directly.
	dotValidationFixtureSubmitWithoutValidate(dotValidationFixtureMinimalValidDOT(), stub)

	if !stub.submitCalled {
		t.Fatal("test harness error: submit was not called by non-compliant path")
	}
	if stub.submittedWithValidation {
		// The sensor must detect the absence of a prior Validate call.
		t.Fatal("sensor failed to detect EM-040 violation: submittedWithValidation is true even though Validate was never called")
	}
}

// TestDotValidation_ValidateMustPrecedeSubmitSequence asserts the strict ordering
// constraint: Validate MUST be called before Submit in any compliant path.
//
// This test verifies the sensor's call-sequence tracking by running the two
// operations in the wrong order (submit first, then validate) and confirming
// that submittedWithValidation is false (the submit happened without prior
// validation, regardless of whether Validate eventually succeeds later).
func TestDotValidation_ValidateMustPrecedeSubmitSequence(t *testing.T) {
	t.Parallel()
	v := New(nil, nil)
	stub := dotValidationFixtureNewStubSubmitter()
	dotSrc := dotValidationFixtureMinimalValidDOT()

	// Wrong order: submit first, then validate.
	stub.submit(dotSrc)
	err := v.Validate(dotSrc)
	if err != nil {
		t.Fatalf("Validate returned unexpected error: %v", err)
	}
	stub.recordValidateOK()

	// The submit already happened without prior validation; the sensor must
	// report the violation regardless of subsequent Validate success.
	if stub.submittedWithValidation {
		t.Fatal("sensor failed to detect ordering violation: submittedWithValidation is true even though submit preceded Validate")
	}
}

// TestDotValidation_ValidateReturnsNilForValidDOT is a contract sanity check:
// the fixture DOT used in the sensor tests MUST itself pass the pre-run
// validator. If this test fails, the fixture DOT is broken and all sensor
// tests above are testing the wrong invariant.
func TestDotValidation_ValidateReturnsNilForValidDOT(t *testing.T) {
	t.Parallel()
	v := New(nil, nil)
	if err := v.Validate(dotValidationFixtureMinimalValidDOT()); err != nil {
		t.Errorf("dotValidationFixtureMinimalValidDOT() does not pass the validator: %v", err)
	}
}

// TestDotValidation_ValidateReturnsErrorForInvalidDOT is a contract sanity check:
// the invalid fixture DOT used in the sensor tests MUST fail the pre-run
// validator. If this test fails, the fixture is broken and the blocking sensor
// test above is not actually testing a failing-validation scenario.
func TestDotValidation_ValidateReturnsErrorForInvalidDOT(t *testing.T) {
	t.Parallel()
	v := New(nil, nil)
	if err := v.Validate(dotValidationFixtureInvalidDOT()); err == nil {
		t.Error("dotValidationFixtureInvalidDOT() unexpectedly passed the validator; fixture must produce a validation error")
	}
}
