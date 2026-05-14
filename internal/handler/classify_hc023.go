// Package handler — mechanism-tagged error classification (HC-023).
//
// Spec: specs/handler-contract.md §4.5.HC-023, §8.
//
// HC-023 requires that mapping a subprocess exit state or adapter-detected
// condition to a sentinel class is DETERMINISTIC from structured fields only.
// No cognition participates: no string-matching on error messages, no
// heuristic scoring, no LLM inference. The full decision tree is encoded
// here as a pure function over typed inputs; the result is one of the five
// primary sentinel errors defined in errors.go.
package handler

import "fmt"

// AdapterCondition is the typed return from an adapter's per-agent-type
// classification heuristic.  Adapters MUST return one of the declared
// constants; returning a value outside the set is a programming error.
//
// The adapter's heuristic may inspect the subprocess's structured error
// payload, transport-layer error type, or provisioning result — but MUST
// NOT perform any semantic interpretation (that is reconciliation-investigator
// territory per HC-023).
//
// Spec: specs/handler-contract.md §4.5.HC-023, §8.1–§8.3.
type AdapterCondition int

const (
	// AdapterConditionNone means the adapter did not detect a special
	// condition; classification falls through to exit-code and context rules.
	AdapterConditionNone AdapterCondition = iota

	// AdapterConditionTransient means the adapter determined the failure is
	// network-transient, rate-limit-transient, or a recoverable provisioning
	// error (§8.1).  Maps to ErrTransient.
	AdapterConditionTransient

	// AdapterConditionStructural means the adapter determined the failure is
	// structural — the plan cannot proceed without a plan change (§8.2).
	// Maps to ErrStructural (or a sub-sentinel if the call site has finer
	// sub-sentinel identity; ClassifyExitState returns ErrStructural here
	// since it operates below the sub-sentinel layer).
	AdapterConditionStructural

	// AdapterConditionDeterministic means the adapter determined the failure
	// will recur on any retry of the same plan; retrying is futile (§8.3).
	// Maps to ErrDeterministic.
	AdapterConditionDeterministic
)

// ExitState captures the structured fields from a subprocess termination.
// All fields are plain data; no error-message strings, no inferred meanings.
//
// Callers populate ExitState from cmd.Wait() and the watcher's session book-
// keeping, then call ClassifyExitState to obtain the sentinel error.
//
// Spec: specs/handler-contract.md §4.5.HC-023, §4.6.HC-024, §8.
type ExitState struct {
	// ExitCode is the subprocess exit code as reported by os/exec.ExitError.
	// 0 denotes a clean exit; -1 denotes an abnormal termination where the
	// platform did not supply a numeric code (e.g. SIGKILL on Linux surfaces
	// exit code -1 from Go's ExitError.ExitCode()).
	ExitCode int

	// OutcomeEmitted is true iff the watcher received a valid outcome_emitted
	// progress-stream message before subprocess termination (HC-008).
	// When true and ExitCode == 0, the session completed cleanly.
	// When true and ExitCode != 0, the dirty-exit / post-outcome shutdown
	// window rules apply (HC-008a) — still classified as completed, handled
	// by the caller before invoking ClassifyExitState (ClassifyExitState is
	// only called for failure paths).
	OutcomeEmitted bool

	// CtxCanceled is true iff the session's context was canceled (by an
	// operator drain, budget policy, or explicit cancellation signal) before
	// the subprocess exited.  Per HC-018 and §8.4, context cancellation
	// supersedes all other classification: the sentinel is ErrCanceled.
	CtxCanceled bool

	// AdapterResult is the typed condition returned by the per-agent-type
	// adapter's classification heuristic (HC-013).  When non-None, it takes
	// priority over exit-code-based rules (below CtxCanceled in priority).
	AdapterResult AdapterCondition
}

// ClassifyExitState maps a structured ExitState to exactly one primary
// sentinel error per HC-023.
//
// Priority order (highest wins):
//  1. CtxCanceled == true → ErrCanceled (§8.4; supersedes everything).
//  2. AdapterResult == Transient → ErrTransient (§8.1).
//  3. AdapterResult == Deterministic → ErrDeterministic (§8.3).
//  4. AdapterResult == Structural → ErrStructural (§8.2).
//  5. ExitCode == 0 with no outcome_emitted → ErrStructural (§8.2;
//     subprocess exited cleanly but produced no outcome — structural gap).
//  6. ExitCode != 0 → ErrStructural (§8.2; crash without outcome).
//  7. Default fallback → ErrStructural (conservative; guards against a
//     zero-value ExitState that the caller should never produce).
//
// The function is a pure, total, no-side-effect function.  Every code path
// returns a non-nil error that wraps exactly one primary sentinel.
//
// Spec: specs/handler-contract.md §4.5.HC-023, §8.1–§8.4.
func ClassifyExitState(s ExitState) error {
	// Priority 1: operator/policy cancellation supersedes all other signals.
	if s.CtxCanceled {
		return fmt.Errorf("handler: classify: context canceled: %w", ErrCanceled)
	}

	// Priority 2–4: adapter-returned typed condition.
	switch s.AdapterResult {
	case AdapterConditionTransient:
		return fmt.Errorf("handler: classify: adapter transient: %w", ErrTransient)
	case AdapterConditionDeterministic:
		return fmt.Errorf("handler: classify: adapter deterministic: %w", ErrDeterministic)
	case AdapterConditionStructural:
		return fmt.Errorf("handler: classify: adapter structural: %w", ErrStructural)
	}

	// Priority 5–6: exit-code-based rules when adapter reports no condition.
	// Both zero-exit-without-outcome and non-zero exit are structural: the plan
	// produced no outcome and requires a re-plan, not a retry (§8.2).
	return fmt.Errorf("handler: classify: exit code %d: %w", s.ExitCode, ErrStructural)
}
