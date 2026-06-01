package core

// verdictdiscovery_rc026.go — Verdict-execution discovery on restart (RC-026).
//
// RC-026 requires the startup detector of [process-lifecycle.md §4.2 PL-005]
// to classify a reconciliation workflow as resolved ONLY if BOTH the verdict
// commit AND the verdict-executed commit are present on the investigator's
// branch. A branch with a verdict commit but no verdict-executed commit MUST be
// classified as Cat 3b (verdict-unexecuted) with the dedicated auto-resolver
// re-attempting the verdict's mechanical action under a fresh staleness check
// (RC-024).
//
// This file declares:
//
//   - VerdictDiscoveryState — the three observable branch states the startup
//     detector resolves per RC-026.
//   - BranchVerdictEvidence — the minimal branch-inspection input for the
//     discovery function (has-verdict-commit, has-verdict-executed-commit).
//   - DiscoverVerdictExecution — pure, I/O-free function mapping branch evidence
//     to a VerdictDiscoveryState and the resulting ReconciliationCategory. The
//     daemon's startup detector calls this per investigator branch BEFORE
//     dispatching ordinary workflows.
//   - ReconciliationClassificationGate — records the RC-026 startup-ordering
//     rule: reconciliation classification MUST complete before the daemon
//     transitions to `ready`; ordinary dispatch is gated behind detection
//     completion. See OQ-RC-003 for the fail-open escalation open question.
//
// This is a pure, I/O-free layer. The actual branch inspection (git log +
// trailer parsing), Cat 3b re-execution, and ready-transition gating are
// performed by the daemon's reconciliation startup pass (RC-026, PL-005
// step 7), which consumes this logic. The separation mirrors the
// CheckVerdictStaleness / VerdictStalenessResult split for RC-024.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026;
// specs/reconciliation/schemas.md §6.3 (Cat 3b row);
// specs/process-lifecycle.md §4.2 PL-005.

// VerdictDiscoveryState is the result of inspecting an investigator task branch
// for the presence of the verdict commit and the verdict-executed commit per
// RC-026.
//
// Three states cover the full detection space for RC-026:
//
//   - VerdictDiscoveryStateClean — neither a verdict commit nor a
//     verdict-executed commit is present. No prior reconciliation verdict was
//     durably emitted for this target run. Classified as Cat 5 (clean restart).
//
//   - VerdictDiscoveryStateCat3b — a verdict commit IS present AND no
//     verdict-executed commit follows it on the same branch. The daemon's
//     mechanical action was not durably recorded. Classified as Cat 3b;
//     auto-resolver re-executes under a fresh RC-024 staleness check.
//
//   - VerdictDiscoveryStateResolved — both a verdict commit AND a
//     verdict-executed commit are present. The mechanical action is durably
//     recorded; the reconciliation workflow is fully resolved.
//
// Spec ref: specs/reconciliation/spec.md §8.5 (Cat 3b detection rule);
// RC-002b (lock-file treatment based on verdict-executed-commit presence);
// RC-026.
type VerdictDiscoveryState string

const (
	// VerdictDiscoveryStateClean means the investigator task branch has neither
	// a verdict commit nor a verdict-executed commit. The reconciliation workflow
	// either never reached the investigator, or crashed before the verdict commit
	// was emitted. On restart this run is classified Cat 5 (clean re-dispatch)
	// per RC-003b: no durable evidence of an emitted verdict exists, so the
	// outer run may be re-classified from scratch.
	VerdictDiscoveryStateClean VerdictDiscoveryState = "clean"

	// VerdictDiscoveryStateCat3b means the investigator task branch has a verdict
	// commit (Harmonik-Workflow-Class: reconciliation + reconciliation_verdict_emitted
	// evidence) AND does NOT have a subsequent Harmonik-Verdict-Executed: true
	// commit. The daemon crashed after emitting the verdict but before or during
	// the mechanical action. Auto-resolver re-executes per RC-026 under a fresh
	// RC-024 staleness check. RC-026a imposes a retry cap of N=5.
	VerdictDiscoveryStateCat3b VerdictDiscoveryState = "cat-3b"

	// VerdictDiscoveryStateResolved means the investigator task branch has both
	// a verdict commit AND a subsequent Harmonik-Verdict-Executed: true commit.
	// The mechanical action is durably recorded. The startup detector treats this
	// run as complete and does NOT re-dispatch a reconciliation workflow.
	VerdictDiscoveryStateResolved VerdictDiscoveryState = "resolved"
)

// Valid reports whether s is one of the three declared VerdictDiscoveryState
// constants.
func (s VerdictDiscoveryState) Valid() bool {
	switch s {
	case VerdictDiscoveryStateClean,
		VerdictDiscoveryStateCat3b,
		VerdictDiscoveryStateResolved:
		return true
	default:
		return false
	}
}

// BranchVerdictEvidence captures the two commit-presence signals that the
// RC-026 startup detector reads from an investigator task branch.
//
// Both fields are determined by the daemon's branch-inspection pass (git log +
// trailer parsing); they are passed to DiscoverVerdictExecution, which maps
// them to a VerdictDiscoveryState without performing any I/O itself.
//
// Spec ref: specs/reconciliation/spec.md §8.5 (Cat 3b detection rule);
// specs/reconciliation/schemas.md §6.4 (Harmonik-Verdict-Executed trailer);
// RC-026.
type BranchVerdictEvidence struct {
	// HasVerdictCommit is true when the investigator task branch contains at
	// least one commit carrying the Harmonik-Workflow-Class: reconciliation
	// trailer AND evidence of a reconciliation_verdict_emitted event (i.e., the
	// verdict commit per RC-002). This is the branch-inspection proxy for "the
	// investigator ran to completion and emitted a verdict."
	HasVerdictCommit bool

	// HasVerdictExecutedCommit is true when the investigator task branch
	// contains at least one commit with the Harmonik-Verdict-Executed: true
	// trailer (schemas.md §6.4), as a descendant of the verdict commit. This is
	// the durable marker written by the daemon's verdict-executor after the
	// mechanical action succeeds (RC-025).
	//
	// HasVerdictExecutedCommit MUST be false when HasVerdictCommit is false —
	// a verdict-executed commit without a verdict commit is structurally
	// impossible and is treated as Cat 6a (integrity violation) by the detector.
	// DiscoverVerdictExecution does not enforce this cross-check; callers are
	// responsible for routing the structurally-impossible case through Cat 6a.
	HasVerdictExecutedCommit bool
}

// DiscoverVerdictExecution maps branch-level evidence to the RC-026
// VerdictDiscoveryState and the resulting ReconciliationCategory.
//
// It is a pure, I/O-free function. The daemon's startup detector calls this
// once per investigator task branch to decide whether to classify the run as
// Cat 3b (auto-resolver) or resolved (no action needed).
//
// Mapping per specs/reconciliation/schemas.md §6.3 Cat 3b row and RC-026:
//
//   - Neither verdict commit nor verdict-executed commit →
//     (VerdictDiscoveryStateClean, ReconciliationCategoryCat5):
//     clean restart; no reconciliation action.
//
//   - Verdict commit present, no verdict-executed commit →
//     (VerdictDiscoveryStateCat3b, ReconciliationCategoryCat3b):
//     auto-resolver re-executes under fresh RC-024 staleness check.
//
//   - Both verdict commit and verdict-executed commit present →
//     (VerdictDiscoveryStateResolved, ReconciliationCategoryCat5):
//     resolved; startup detector takes no further action. Callers MUST check
//     VerdictDiscoveryState == VerdictDiscoveryStateResolved before treating
//     the returned category as an actionable classification.
//
// Spec ref: specs/reconciliation/spec.md §8.5 (Cat 3b); RC-026;
// specs/reconciliation/schemas.md §6.3 (Cat 3b row).
func DiscoverVerdictExecution(e BranchVerdictEvidence) (VerdictDiscoveryState, ReconciliationCategory) {
	switch {
	case !e.HasVerdictCommit:
		// No verdict commit: clean branch. Treat as Cat 5 (clean restart).
		// A reconciliation workflow either never reached the investigator or
		// crashed before the verdict commit was emitted; no Cat 3b trigger.
		return VerdictDiscoveryStateClean, ReconciliationCategoryCat5

	case !e.HasVerdictExecutedCommit:
		// Verdict commit present, no verdict-executed commit: Cat 3b.
		// The daemon crashed after emitting the verdict but before or during
		// the mechanical action. Auto-resolver re-executes per RC-026.
		return VerdictDiscoveryStateCat3b, ReconciliationCategoryCat3b

	default:
		// Both commits present: fully resolved.
		// Cat 5 is returned as the "no further action" sentinel.
		// Callers MUST check VerdictDiscoveryState == VerdictDiscoveryStateResolved.
		return VerdictDiscoveryStateResolved, ReconciliationCategoryCat5
	}
}

// ReconciliationClassificationGate encodes the RC-026 startup-ordering rule:
// the daemon's reconciliation-workflow classification pass MUST complete
// BEFORE the daemon transitions to `ready` and before any ordinary workflow
// dispatches. Ordinary dispatch is gated behind detection completion.
//
// This type is a structural anchor for the ordering constraint. The daemon's
// startup sequence (PL-005 step 7) enforces it at runtime. Tests verify the
// shape invariants without requiring live daemon plumbing.
//
// The OQRc003FailOpenPolicy field records the open question from OQ-RC-003: if
// reconciliation cannot classify beyond Cat 0, the conservative default is to
// refuse to reach `ready`. Fail-open to `degraded` + operator escalation is
// tracked but not chosen at this spec version.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026;
// specs/process-lifecycle.md §4.2 PL-005 step 7; OQ-RC-003.
type ReconciliationClassificationGate struct {
	// ClassificationPassBeforeReady is the normative description of the startup
	// ordering rule: reconciliation detectors run before the daemon transitions
	// to `ready`. Non-empty required.
	ClassificationPassBeforeReady string

	// OrdinaryDispatchGatedBy is the normative description of what ordinary
	// workflow dispatch depends on before it may proceed. Non-empty required.
	OrdinaryDispatchGatedBy string

	// OQRc003FailOpenPolicy documents the OQ-RC-003 open question on fail-open
	// escalation. Non-empty required.
	OQRc003FailOpenPolicy string
}

// Valid reports whether all fields of ReconciliationClassificationGate are
// non-empty.
func (g ReconciliationClassificationGate) Valid() bool {
	return g.ClassificationPassBeforeReady != "" &&
		g.OrdinaryDispatchGatedBy != "" &&
		g.OQRc003FailOpenPolicy != ""
}

// DefaultReconciliationClassificationGate returns the canonical gate record
// for the RC-026 startup-ordering rule.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026;
// specs/process-lifecycle.md §4.2 PL-005 step 7; OQ-RC-003.
func DefaultReconciliationClassificationGate() ReconciliationClassificationGate {
	return ReconciliationClassificationGate{
		ClassificationPassBeforeReady: "reconciliation-workflow classification pass (RC-026) MUST complete before daemon transitions to ready (PL-005 step 7)",
		OrdinaryDispatchGatedBy:       "ordinary workflow dispatch is gated behind reconciliation classification completion per RC-026",
		OQRc003FailOpenPolicy:         "OQ-RC-003: conservative default = refuse to reach ready when reconciliation cannot classify beyond Cat 0; fail-open option (degraded + operator escalation) is tracked but not chosen at spec v0.4.4",
	}
}
