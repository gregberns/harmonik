package core

import "fmt"

// ReconciliationCategory is the 11-value classification enum for every
// in-flight run that the harmonik reconciliation system processes at restart
// or store-divergence time, as defined in
// specs/reconciliation/schemas.md §6.1 ENUM ReconciliationCategory and backed
// by the 11-category taxonomy in specs/reconciliation/spec.md §8.
//
// The 11 values span the full detection space:
//
//	cat-0  infrastructure unavailable
//	cat-1  idempotent rerun
//	cat-2  non-idempotent in-flight
//	cat-3  store disagreement (generic)
//	cat-3a torn Beads write
//	cat-3b verdict-unexecuted
//	cat-3c inverse premature-close
//	cat-4  recoverable known state
//	cat-5  clean restart
//	cat-6a integrity violation, LLM-triageable
//	cat-6b integrity violation, mechanically unrecoverable
//
// The enum is harmonik-owned and closed: an observer encountering an unknown
// ReconciliationCategory MUST reject the enclosing record; no silent fallback
// is permitted per specs/reconciliation/schemas.md §6.1.
type ReconciliationCategory string

// ReconciliationCategory constants per specs/reconciliation/schemas.md §6.1.
const (
	// ReconciliationCategoryCat0 means infrastructure unavailable (br --version
	// fails, Beads SQLite locked, git index locked, .harmonik/ unwritable, or
	// filesystem full). Per §8 / §6.3 Cat 0: halt classification + degraded
	// status; auto-resolved by wait-and-retry.
	ReconciliationCategoryCat0 ReconciliationCategory = "cat-0"

	// ReconciliationCategoryCat1 means the last checkpoint's node has
	// idempotency_class = idempotent; safe to auto-resume by re-spawning.
	ReconciliationCategoryCat1 ReconciliationCategory = "cat-1"

	// ReconciliationCategoryCat2 means the node is non-idempotent (or
	// recoverable-non-idempotent), bead is in_progress, and no terminal
	// event has been observed since the last checkpoint. Requires an
	// investigator workflow.
	ReconciliationCategoryCat2 ReconciliationCategory = "cat-2"

	// ReconciliationCategoryCat3 means inter-store disagreement not matching
	// any of the 3a/3b/3c sub-categories. Requires an investigator workflow
	// with git-wins orientation per RC-INV-001.
	ReconciliationCategoryCat3 ReconciliationCategory = "cat-3"

	// ReconciliationCategoryCat3a means a torn Beads write: an intent-log
	// entry is present and the bead's current coarse-status is neither the
	// pre-state nor the post-state of the intended transition. Auto-resolved
	// via adapter status-check-before-reissue (BI-031b).
	ReconciliationCategoryCat3a ReconciliationCategory = "cat-3a"

	// ReconciliationCategoryCat3b means verdict-unexecuted: the investigator
	// task branch has a reconciliation_verdict_emitted commit with no
	// subsequent Harmonik-Verdict-Executed trailer. Auto-resolved via RC-026
	// re-execution (staleness-checked).
	ReconciliationCategoryCat3b ReconciliationCategory = "cat-3b"

	// ReconciliationCategoryCat3c means inverse premature-close: a merge
	// commit on the target branch records a success terminal state for run R,
	// but the bead for R is still in_progress with no subsequent in-flight
	// checkpoints. Auto-resolved via direct accept-close-with-note + close.
	ReconciliationCategoryCat3c ReconciliationCategory = "cat-3c"

	// ReconciliationCategoryCat4 means the agent was in a well-defined
	// retry/backoff state at crash (rate-limited, waiting for human gate).
	// Auto-resumed by re-arming the retry or gate.
	ReconciliationCategoryCat4 ReconciliationCategory = "cat-4"

	// ReconciliationCategoryCat5 means clean restart: nothing is in-flight
	// for this run (includes orphaned branches from prior reopened runs per
	// RC-010). No action needed; proceeds to ready.
	ReconciliationCategoryCat5 ReconciliationCategory = "cat-5"

	// ReconciliationCategoryCat6a means an integrity violation that an LLM
	// investigator can triage (workspace missing with transition-record
	// absent, trailer/sibling-file mismatch, uncommitted git-in-progress op,
	// or bead in_progress with two+ task branches each advertising a run ID
	// without a verdict-executed marker). Requires an investigator workflow;
	// default verdict is escalate-to-human, which the investigator may
	// downgrade.
	ReconciliationCategoryCat6a ReconciliationCategory = "cat-6a"

	// ReconciliationCategoryCat6b means an integrity violation that cannot be
	// mechanically recovered (JSONL corrupt past a byte offset, JSONL
	// referencing a checkpoint hash missing from the git object database, or
	// git fsck failure). Auto-escalated to the operator without spawning an
	// investigator.
	ReconciliationCategoryCat6b ReconciliationCategory = "cat-6b"
)

// Valid reports whether c is one of the eleven declared ReconciliationCategory
// constants. The enum is harmonik-owned and closed; unknown values are never
// valid.
func (c ReconciliationCategory) Valid() bool {
	switch c {
	case ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ReconciliationCategory
// serialises correctly in JSON and YAML.
// It rejects any value that is not one of the eleven declared constants.
func (c ReconciliationCategory) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("reconciliationcategory: unknown value %q", string(c))
	}
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the eleven declared constants.
// Per specs/reconciliation/schemas.md §6.1, unknown ReconciliationCategory
// values must be rejected; callers MUST NOT silently degrade to a default
// category.
func (c *ReconciliationCategory) UnmarshalText(text []byte) error {
	v := ReconciliationCategory(text)
	if !v.Valid() {
		return fmt.Errorf(
			"reconciliationcategory: unknown value %q;"+
				" must be one of cat-0, cat-1, cat-2, cat-3, cat-3a, cat-3b, cat-3c,"+
				" cat-4, cat-5, cat-6a, cat-6b",
			string(text),
		)
	}
	*c = v
	return nil
}
