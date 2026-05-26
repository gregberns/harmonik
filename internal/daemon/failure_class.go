package daemon

// failure_class.go — daemon-side failure-class post-classifier (back-fill).
//
// Implements the two-sided contract of HC-059 and EM-005c / WG-018:
//
//   - After the daemon's post-classifier runs, the Outcome record MUST carry
//     failure_class whenever outcome.status == FAIL (WG-018).
//   - Handlers emitting FAIL without failure_class get a back-filled value
//     derived from the HC-020 ErrX sentinel path (HC-059).
//   - Handler-emitted failure_class values are HINTS; the daemon's sentinel-
//     derived classification is AUTHORITATIVE on disagreement (HC-059).
//   - The value "compilation_loop" is daemon-only and MUST NOT be carried by
//     a handler; a handler-emitted compilation_loop is overridden to structural
//     (HC-059, EM-005c).
//   - Non-FAIL outcomes MUST NOT carry failure_class; the field is cleared (HC-058).
//
// Spec refs: specs/handler-contract.md §4.2a HC-058, HC-059;
//            specs/execution-model.md §4.1 EM-005c, §8;
//            specs/workflow-graph.md §7 WG-018.
// Bead: hk-ex9c4 (T-IMPL-006).

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// BackfillFailureClass applies the daemon-side failure-class post-classifier to
// o and returns the updated Outcome.  It is the mandatory post-processing step
// between Session.Wait return and any downstream cascade or event emission.
//
// Rules (HC-059 / EM-005c / WG-018):
//
//  1. Non-FAIL outcomes: FailureClass is cleared to nil (HC-058 enforcement).
//  2. FAIL + handler omitted FailureClass: back-fills from the ErrX sentinel
//     carried by sessionErr.  Emits a structured-log line at Info level for
//     observability.  Falls back to FailureClassStructural when sessionErr
//     carries no recognised sentinel.
//  3. FAIL + handler emitted FailureClass = compilation_loop: overrides to
//     structural (compilation_loop is daemon-only per HC-059); logs a
//     disagreement warning.
//  4. FAIL + handler emitted FailureClass and daemon has a sentinel class
//     (sessionErr != nil) and they differ: daemon wins; logs a disagreement
//     warning.
//  5. FAIL + handler emitted FailureClass and no daemon sentinel (sessionErr
//     == nil or unrecognised): honours the handler's value.
//
// runID and nodeID are included in structured-log records for correlation.
func BackfillFailureClass(
	ctx context.Context,
	o core.Outcome,
	sessionErr error,
	runID core.RunID,
	nodeID core.NodeID,
) core.Outcome {
	// Rule 1: Non-FAIL outcomes must not carry failure_class.
	if o.Status != core.OutcomeStatusFail {
		o.FailureClass = nil
		return o
	}

	daemonClass, hasDaemonClass := classifyFromSentinel(sessionErr)

	switch {
	case o.FailureClass == nil:
		// Rule 2: back-fill from sentinel (or structural default).
		fc := daemonClass
		if !hasDaemonClass {
			fc = core.FailureClassStructural
		}
		o.FailureClass = &fc
		slog.InfoContext(ctx, "failure_class_backfilled",
			"run_id", runID.String(),
			"node_id", string(nodeID),
			"backfilled_class", string(fc),
			"sentinel_present", hasDaemonClass,
		)

	case *o.FailureClass == core.FailureClassCompilationLoop:
		// Rule 3: compilation_loop is daemon-only; handler must not self-classify.
		overrideClass := core.FailureClassStructural
		slog.WarnContext(ctx, "failure_class_disagreement",
			"run_id", runID.String(),
			"node_id", string(nodeID),
			"handler_class", string(*o.FailureClass),
			"daemon_class", string(overrideClass),
			"reason", "compilation_loop_is_daemon_only",
		)
		o.FailureClass = &overrideClass

	case hasDaemonClass && *o.FailureClass != daemonClass:
		// Rule 4: handler and daemon disagree; daemon wins.
		slog.WarnContext(ctx, "failure_class_disagreement",
			"run_id", runID.String(),
			"node_id", string(nodeID),
			"handler_class", string(*o.FailureClass),
			"daemon_class", string(daemonClass),
		)
		o.FailureClass = &daemonClass

	default:
		// Rule 5 (and agreement case of rule 4): honour handler's value.
	}

	return o
}

// classifyFromSentinel maps a sessionErr to the FailureClass declared in
// execution-model.md §8 via the HC-020 ErrX sentinel path.
//
// Returns (class, true) when err carries a recognised sentinel, or
// (FailureClassStructural, false) when err is nil or unrecognised.
// The returned FailureClass is always valid.
//
// Note: FailureClassCompilationLoop is never returned here because
// compilation_loop is detected daemon-side at cascade evaluation (EM-043),
// not via the handler error-sentinel path.
func classifyFromSentinel(err error) (core.FailureClass, bool) {
	switch {
	case errors.Is(err, handlercontract.ErrTransient):
		return core.FailureClassTransient, true
	case errors.Is(err, handlercontract.ErrDeterministic):
		return core.FailureClassDeterministic, true
	case errors.Is(err, handlercontract.ErrCanceled):
		return core.FailureClassCanceled, true
	case errors.Is(err, handlercontract.ErrBudget):
		return core.FailureClassBudgetExhausted, true
	case errors.Is(err, handlercontract.ErrStructural):
		// Matches ErrStructural and both sub-sentinels (ErrProtocolMismatch,
		// ErrSkillProvisioningFailed) via their Unwrap chains per HC-020.
		return core.FailureClassStructural, true
	default:
		return core.FailureClassStructural, false
	}
}
