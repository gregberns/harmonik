package core

import (
	"testing"
)

// verdictretrycap_rc026a_test.go — Tests for RC-026a Cat 3b retry cap logic.
//
// Covers:
//   - VerdictExecutionAttemptRecord.Valid() shape invariants.
//   - VerdictRetryCapDefault value.
//   - CheckVerdictRetryCap pure function for all boundary conditions:
//     nil record (first retry), mid-range, cap boundary, cap exceeded.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.

// ---- VerdictExecutionAttemptRecord.Valid ----

// TestVerdictExecutionAttemptRecord_ValidIsTrue verifies that a well-formed
// record passes Valid().
func TestVerdictExecutionAttemptRecord_ValidIsTrue(t *testing.T) {
	t.Parallel()

	r := VerdictExecutionAttemptRecord{
		TargetRunID:   "run-abc-123",
		Attempt:       1,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	if !r.Valid() {
		t.Error("VerdictExecutionAttemptRecord{...}.Valid() = false; want true for well-formed record")
	}
}

// TestVerdictExecutionAttemptRecord_EmptyTargetRunIDIsInvalid verifies that a
// record with an empty TargetRunID fails Valid().
func TestVerdictExecutionAttemptRecord_EmptyTargetRunIDIsInvalid(t *testing.T) {
	t.Parallel()

	r := VerdictExecutionAttemptRecord{
		TargetRunID:   "",
		Attempt:       1,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	if r.Valid() {
		t.Error("VerdictExecutionAttemptRecord{TargetRunID:\"\"}.Valid() = true; want false (empty TargetRunID)")
	}
}

// TestVerdictExecutionAttemptRecord_ZeroAttemptIsInvalid verifies that a
// record with Attempt=0 fails Valid().
//
// RC-026a: the attempt counter is 1-based; the file is only written after at
// least one retry, so Attempt=0 is structurally invalid on disk.
func TestVerdictExecutionAttemptRecord_ZeroAttemptIsInvalid(t *testing.T) {
	t.Parallel()

	r := VerdictExecutionAttemptRecord{
		TargetRunID:   "run-abc-123",
		Attempt:       0,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	if r.Valid() {
		t.Error("VerdictExecutionAttemptRecord{Attempt:0}.Valid() = true; want false (attempt must be >= 1)")
	}
}

// TestVerdictExecutionAttemptRecord_EmptyLastAttemptAtIsInvalid verifies that
// a record with an empty LastAttemptAt fails Valid().
func TestVerdictExecutionAttemptRecord_EmptyLastAttemptAtIsInvalid(t *testing.T) {
	t.Parallel()

	r := VerdictExecutionAttemptRecord{
		TargetRunID:   "run-abc-123",
		Attempt:       1,
		LastAttemptAt: "",
	}

	if r.Valid() {
		t.Error("VerdictExecutionAttemptRecord{LastAttemptAt:\"\"}.Valid() = true; want false (LastAttemptAt required)")
	}
}

// ---- VerdictRetryCapDefault ----

// TestVerdictRetryCapDefault_IsFive verifies that the default cap is N=5 per
// RC-026a.
func TestVerdictRetryCapDefault_IsFive(t *testing.T) {
	t.Parallel()

	const want = 5
	if VerdictRetryCapDefault != want {
		t.Errorf("VerdictRetryCapDefault = %d, want %d (RC-026a default cap N=5)", VerdictRetryCapDefault, want)
	}
}

// ---- CheckVerdictRetryCap ----

// TestCheckVerdictRetryCap_NilRecord_FirstRetryAllowed verifies that when no
// file exists yet (nil record), the first retry (attempt=1) is allowed.
//
// RC-026a: retry cap defaults to N=5; no prior retries recorded = attempt 0.
func TestCheckVerdictRetryCap_NilRecord_FirstRetryAllowed(t *testing.T) {
	t.Parallel()

	decision := CheckVerdictRetryCap(nil, VerdictRetryCapDefault)

	if !decision.Allowed {
		t.Error("CheckVerdictRetryCap(nil, 5): Allowed = false; want true (first retry should be allowed)")
	}
	if decision.CapExceeded {
		t.Error("CheckVerdictRetryCap(nil, 5): CapExceeded = true; want false for first retry")
	}
	if decision.NextAttempt != 1 {
		t.Errorf("CheckVerdictRetryCap(nil, 5): NextAttempt = %d, want 1", decision.NextAttempt)
	}
}

// TestCheckVerdictRetryCap_AtCapBoundary_LastRetryAllowed verifies that when
// the current attempt count is cap-1, the next retry (at exactly the cap) is
// still allowed.
//
// RC-026a: retry cap N=5; attempt 5 is the last allowed retry.
func TestCheckVerdictRetryCap_AtCapBoundary_LastRetryAllowed(t *testing.T) {
	t.Parallel()

	record := &VerdictExecutionAttemptRecord{
		TargetRunID:   "run-cap-boundary",
		Attempt:       4,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	decision := CheckVerdictRetryCap(record, VerdictRetryCapDefault)

	if !decision.Allowed {
		t.Error("CheckVerdictRetryCap(attempt=4, cap=5): Allowed = false; want true (attempt 5 is within cap)")
	}
	if decision.CapExceeded {
		t.Error("CheckVerdictRetryCap(attempt=4, cap=5): CapExceeded = true; want false")
	}
	if decision.NextAttempt != 5 {
		t.Errorf("CheckVerdictRetryCap(attempt=4, cap=5): NextAttempt = %d, want 5", decision.NextAttempt)
	}
}

// TestCheckVerdictRetryCap_AtCap_Exceeded verifies that when the attempt count
// equals the cap, the next call returns CapExceeded=true and Allowed=false.
//
// RC-026a: "on cap exceeded, the run escalates to Cat 6b (operator escalation)."
func TestCheckVerdictRetryCap_AtCap_Exceeded(t *testing.T) {
	t.Parallel()

	record := &VerdictExecutionAttemptRecord{
		TargetRunID:   "run-cap-exceeded",
		Attempt:       5,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	decision := CheckVerdictRetryCap(record, VerdictRetryCapDefault)

	if decision.Allowed {
		t.Error("CheckVerdictRetryCap(attempt=5, cap=5): Allowed = true; want false (cap exceeded)")
	}
	if !decision.CapExceeded {
		t.Error("CheckVerdictRetryCap(attempt=5, cap=5): CapExceeded = false; want true")
	}
	if decision.NextAttempt != 6 {
		t.Errorf("CheckVerdictRetryCap(attempt=5, cap=5): NextAttempt = %d, want 6", decision.NextAttempt)
	}
}

// TestCheckVerdictRetryCap_BeyondCap_StillExceeded verifies that when the
// attempt count already exceeds the cap, the decision continues to be
// CapExceeded=true.
//
// This can occur if the daemon is restarted after writing a > N record (e.g.,
// cap was raised then lowered, or file was corrupted and recovered with a high
// value). The check should not allow further retries.
func TestCheckVerdictRetryCap_BeyondCap_StillExceeded(t *testing.T) {
	t.Parallel()

	record := &VerdictExecutionAttemptRecord{
		TargetRunID:   "run-beyond-cap",
		Attempt:       7,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	decision := CheckVerdictRetryCap(record, VerdictRetryCapDefault)

	if decision.Allowed {
		t.Error("CheckVerdictRetryCap(attempt=7, cap=5): Allowed = true; want false (well beyond cap)")
	}
	if !decision.CapExceeded {
		t.Error("CheckVerdictRetryCap(attempt=7, cap=5): CapExceeded = false; want true")
	}
}

// TestCheckVerdictRetryCap_NextAttemptIsAlwaysCurrentPlusOne verifies that
// NextAttempt is always exactly (current + 1) for all inputs.
//
// RC-026a: the attempt field in the retry event payload MUST use the NextAttempt
// value from VerdictRetryDecision.
func TestCheckVerdictRetryCap_NextAttemptIsAlwaysCurrentPlusOne(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		current int
		want    int
	}{
		{"nil_record", 0, 1},
		{"attempt_1", 1, 2},
		{"attempt_4", 4, 5},
		{"attempt_5", 5, 6},
		{"attempt_10", 10, 11},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var record *VerdictExecutionAttemptRecord
			if tc.current > 0 {
				record = &VerdictExecutionAttemptRecord{
					TargetRunID:   "run-x",
					Attempt:       tc.current,
					LastAttemptAt: "2026-06-01T00:00:00Z",
				}
			}

			decision := CheckVerdictRetryCap(record, VerdictRetryCapDefault)

			if decision.NextAttempt != tc.want {
				t.Errorf("CheckVerdictRetryCap(current=%d).NextAttempt = %d, want %d",
					tc.current, decision.NextAttempt, tc.want)
			}
		})
	}
}

// TestCheckVerdictRetryCap_AllowedAndCapExceededAreMutuallyExclusive verifies
// that Allowed and CapExceeded are always mutually exclusive.
func TestCheckVerdictRetryCap_AllowedAndCapExceededAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	for attempt := 0; attempt <= 10; attempt++ {
		attempt := attempt
		t.Run("", func(t *testing.T) {
			t.Parallel()

			var record *VerdictExecutionAttemptRecord
			if attempt > 0 {
				record = &VerdictExecutionAttemptRecord{
					TargetRunID:   "run-mutex-check",
					Attempt:       attempt,
					LastAttemptAt: "2026-06-01T00:00:00Z",
				}
			}

			d := CheckVerdictRetryCap(record, VerdictRetryCapDefault)

			if d.Allowed && d.CapExceeded {
				t.Errorf("CheckVerdictRetryCap(attempt=%d): Allowed=true AND CapExceeded=true; must be mutually exclusive", attempt)
			}
			if !d.Allowed && !d.CapExceeded {
				t.Errorf("CheckVerdictRetryCap(attempt=%d): Allowed=false AND CapExceeded=false; one must be true", attempt)
			}
		})
	}
}

// TestCheckVerdictRetryCap_CustomCap verifies that a caller-supplied cap other
// than VerdictRetryCapDefault is honored.
func TestCheckVerdictRetryCap_CustomCap(t *testing.T) {
	t.Parallel()

	record := &VerdictExecutionAttemptRecord{
		TargetRunID:   "run-custom-cap",
		Attempt:       2,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	// cap=3: attempt 3 is the last; attempt 4 is beyond.
	d3 := CheckVerdictRetryCap(record, 3)
	if !d3.Allowed {
		t.Error("CheckVerdictRetryCap(attempt=2, cap=3): Allowed = false; want true (attempt 3 is at cap but allowed)")
	}

	record2 := &VerdictExecutionAttemptRecord{
		TargetRunID:   "run-custom-cap",
		Attempt:       3,
		LastAttemptAt: "2026-06-01T00:00:00Z",
	}

	d4 := CheckVerdictRetryCap(record2, 3)
	if d4.Allowed {
		t.Error("CheckVerdictRetryCap(attempt=3, cap=3): Allowed = true; want false (cap reached)")
	}
	if !d4.CapExceeded {
		t.Error("CheckVerdictRetryCap(attempt=3, cap=3): CapExceeded = false; want true")
	}
}
