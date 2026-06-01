package core

// verdictretrycap_rc026a.go — Cat 3b re-execution retry cap (RC-026a).
//
// RC-026a requires that a Cat 3b verdict-execution that fails on a fresh
// staleness check (RC-024) be retried with a durable attempt counter. The
// counter is recorded in .harmonik/reconciliation-attempts/<target_run_id>.json
// (atomic temp+rename+fsync per workspace-model.md §4.7 WM-026). The retry cap
// defaults to N=5; on cap exceeded, the run escalates to Cat 6b (operator
// escalation) per spec §8.11. Each retry emits
// reconciliation_verdict_execution_retry{target_run_id, attempt}.
//
// This file declares the pure, I/O-free layer:
//
//   - VerdictExecutionAttemptRecord — JSON record written to the durable
//     attempt-counter file per RC-026a.
//   - VerdictRetryCapDefault — the default cap (N=5).
//   - VerdictRetryDecision — the outcome of CheckVerdictRetryCap.
//   - CheckVerdictRetryCap — pure function mapping an existing record and a cap
//     to a decision (allowed / cap-exceeded → Cat 6b).
//
// The actual file I/O (read/write the counter JSON atomically) is performed by
// the lifecycle package (lifecycle/verdictretrycap_rc026a.go), which consumes
// these types. The separation mirrors the CheckVerdictStaleness /
// VerdictStalenessResult split for RC-024.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a;
// specs/workspace-model.md §4.7 WM-026 (atomic write discipline).

// VerdictRetryCapDefault is the default maximum number of Cat 3b re-execution
// retries per RC-026a.
//
// When the durable attempt counter at
// .harmonik/reconciliation-attempts/<target_run_id>.json records Attempt >=
// VerdictRetryCapDefault, the next CheckVerdictRetryCap call returns
// CapExceeded=true and the run escalates to Cat 6b.
const VerdictRetryCapDefault = 5

// VerdictExecutionAttemptRecord is the JSON record stored durably at
// .harmonik/reconciliation-attempts/<target_run_id>.json per RC-026a.
//
// The file is written and read by the lifecycle package's I/O layer; the record
// is the authoritative durable counter across daemon restarts.
//
// # Structural invariants (enforced by Valid)
//
//   - TargetRunID is non-empty.
//   - Attempt is >= 1 (the record is only written after at least one retry).
//   - LastAttemptAt is non-empty (RFC 3339 wall-clock timestamp of the last write).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
type VerdictExecutionAttemptRecord struct {
	// TargetRunID is the run_id of the target run whose Cat 3b re-execution
	// is being tracked. Required (non-empty).
	TargetRunID string `json:"target_run_id"`

	// Attempt is the one-based ordinal of the last retry that was recorded.
	// The first retry is Attempt=1; the cap is N=5 per VerdictRetryCapDefault.
	// Required (must be >= 1 when the record exists on disk).
	Attempt int `json:"attempt"`

	// LastAttemptAt is the RFC 3339 wall-clock timestamp at which the record
	// was last written. Required (non-empty).
	LastAttemptAt string `json:"last_attempt_at"`
}

// Valid reports whether all structural invariants of VerdictExecutionAttemptRecord
// are satisfied.
//
// Rules:
//   - TargetRunID is non-empty.
//   - Attempt is >= 1.
//   - LastAttemptAt is non-empty.
func (r VerdictExecutionAttemptRecord) Valid() bool {
	return r.TargetRunID != "" && r.Attempt >= 1 && r.LastAttemptAt != ""
}

// VerdictRetryDecision is the outcome of CheckVerdictRetryCap.
//
// When Allowed is true, the daemon proceeds with the retry: it increments the
// attempt counter, writes the updated record durably, emits
// reconciliation_verdict_execution_retry, and re-runs the staleness check
// (RC-024) + mechanical action (RC-025).
//
// When Allowed is false (CapExceeded = true), the daemon escalates the run to
// Cat 6b (operator escalation) and emits operator_escalation_required.
//
// NextAttempt is the one-based attempt number for the upcoming retry (Allowed=true)
// or the would-be attempt that exceeded the cap (Allowed=false). It is always
// equal to the current count + 1.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
type VerdictRetryDecision struct {
	// Allowed is true when the retry is within the cap.
	// False when CapExceeded is true.
	Allowed bool

	// NextAttempt is the one-based attempt number for the next retry.
	// Equal to (current attempt count) + 1.
	// Callers MUST use this value as the attempt field of the
	// reconciliation_verdict_execution_retry event payload (RC-026a).
	NextAttempt int

	// CapExceeded is true when NextAttempt > cap, meaning the run MUST escalate
	// to Cat 6b per RC-026a. Always false when Allowed is true.
	CapExceeded bool
}

// CheckVerdictRetryCap evaluates whether a Cat 3b re-execution retry is within
// the configured cap.
//
// record is the existing durable attempt counter for the target run. Pass nil
// when no file exists yet (no previous retries recorded). cap is the maximum
// number of retries allowed; pass VerdictRetryCapDefault for the spec default.
//
// The decision fields:
//
//   - Allowed=true, CapExceeded=false: proceed with the retry at NextAttempt.
//   - Allowed=false, CapExceeded=true: escalate to Cat 6b; do not retry.
//
// CheckVerdictRetryCap does not perform any I/O. The caller is responsible for:
//  1. Reading the current record from disk before calling this function.
//  2. When Allowed=true: writing the updated record (with NextAttempt) to disk
//     before emitting the retry event.
//  3. When Allowed=false: escalating to Cat 6b.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
func CheckVerdictRetryCap(record *VerdictExecutionAttemptRecord, cap int) VerdictRetryDecision {
	current := 0
	if record != nil {
		current = record.Attempt
	}

	next := current + 1

	if next > cap {
		return VerdictRetryDecision{
			Allowed:     false,
			NextAttempt: next,
			CapExceeded: true,
		}
	}

	return VerdictRetryDecision{
		Allowed:     true,
		NextAttempt: next,
		CapExceeded: false,
	}
}
