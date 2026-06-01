package core

// budgetexhaustion_rc018.go — Budget exhaustion terminates with fallback verdict (RC-018).
//
// RC-018 requires that on wall-clock budget exhaustion the daemon execute a
// deterministic 5-step sequence:
//
//  1. Terminate investigator subprocess (SIGTERM, then SIGKILL after HC-018 interval).
//  2. Wait for watcher-observation of process termination per HC-011.
//  3. Emit reconciliation_budget_exhausted (class F per event-model.md §8.4.3).
//  4. Emit fallback escalate-to-human verdict (class F per RC-021).
//  5. Verdict-executor (RC-025a) consumes fallback as if investigator-emitted.
//
// Steps (3) and (4) are NOT atomic but each is an fsync-boundary write. A crash
// between them leaves the budget_exhausted event durably written with no
// subsequent verdict commit; the next daemon startup detects this state and
// routes through the Cat 3b retry cap (RC-026a).
//
// This file declares the pure, I/O-free layer:
//
//   - BudgetExhaustionStep — typed label for each of the five steps.
//   - BudgetExhaustionHandlerStep — struct pairing a step label with its
//     canonical description (for documentation and crash-recovery invariant tests).
//   - BudgetExhaustionHandlerSequence — returns the canonical ordered slice of
//     all five steps.
//   - SynthesizeBudgetExhaustionFallbackVerdict — pure function that constructs
//     the fallback VerdictEvent (escalate-to-human) that RC-018 requires the
//     daemon to emit at step (4). The synthesized event is structurally identical
//     to an investigator-emitted escalate-to-human, satisfying the
//     "indistinguishable" requirement of RC-018.
//
// Actual process-signal delivery (steps 1–2), event emission (steps 3–4), and
// verdict-executor dispatch (step 5) are performed by the daemon layer, which
// consumes these types and function. The separation mirrors the
// PlanForVerdict / verdict-executor split for RC-025.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018;
// specs/handler-contract.md §4.3 HC-018 (SIGTERM-to-SIGKILL interval);
// specs/handler-contract.md §4.3 HC-011 (watcher process-exit observation);
// specs/event-model.md §8.4.3 (durability class F).
//
// Refs: hk-63oh.27

import (
	"fmt"

	"github.com/google/uuid"
)

// BudgetExhaustionStep is the typed label for one step of the RC-018
// budget-exhaustion handler sequence.
type BudgetExhaustionStep string

const (
	// BudgetExhaustionStepTerminateSubprocess is step (1): send SIGTERM to the
	// investigator subprocess, then SIGKILL after the HC-018 grace interval.
	// Precedes all durable writes — no fsync-boundary event is emitted here.
	BudgetExhaustionStepTerminateSubprocess BudgetExhaustionStep = "1-sigterm-sigkill-investigator"

	// BudgetExhaustionStepWaitForWatcher is step (2): wait for the
	// daemon-owned watcher goroutine (HC-011) to observe process termination
	// and publish the terminal session event to the in-process event bus.
	// This step completes before any event is written to the JSONL log.
	BudgetExhaustionStepWaitForWatcher BudgetExhaustionStep = "2-wait-for-process-termination"

	// BudgetExhaustionStepEmitBudgetExhausted is step (3): write and fsync
	// the reconciliation_budget_exhausted event (class F per event-model.md
	// §8.4.3) to the JSONL event log. This is the first fsync-boundary write.
	// Crash after this step leaves the budget_exhausted event durable with no
	// subsequent verdict commit → routes through Cat 3b retry cap (RC-026a)
	// on next daemon startup.
	BudgetExhaustionStepEmitBudgetExhausted BudgetExhaustionStep = "3-emit-budget-exhausted"

	// BudgetExhaustionStepEmitFallbackVerdict is step (4): write and fsync
	// the fallback escalate-to-human verdict (class F per RC-021) to the
	// JSONL event log. This is the second fsync-boundary write. The verdict
	// event is structurally identical to an investigator-emitted
	// escalate-to-human (RC-018 indistinguishability requirement).
	BudgetExhaustionStepEmitFallbackVerdict BudgetExhaustionStep = "4-emit-fallback-verdict"

	// BudgetExhaustionStepVerdictExecutorConsumes is step (5): the
	// verdict-executor (RC-025a) picks up the fallback verdict event and
	// applies the escalate-to-human mechanical action per schemas.md §6.2,
	// exactly as it would for an investigator-emitted verdict.
	BudgetExhaustionStepVerdictExecutorConsumes BudgetExhaustionStep = "5-verdict-executor-consumes"
)

// BudgetExhaustionHandlerStep pairs a step label with its canonical
// human-readable description. The description documents the crash-recovery
// invariant for each step — specifically whether a crash at this step leaves
// durable state and which recovery path that state triggers.
type BudgetExhaustionHandlerStep struct {
	// Label is the typed BudgetExhaustionStep constant for this step.
	// Required (must be a valid constant).
	Label BudgetExhaustionStep

	// Description is the canonical prose description of the step and its
	// crash-recovery invariant. Required (non-empty).
	Description string

	// IsFsyncBoundary is true when this step writes a durably-persisted event
	// (class F per event-model.md §8.4.3). A crash immediately after an
	// fsync-boundary step leaves a durable record in the JSONL log that the
	// startup detector can inspect.
	//
	// Steps (3) and (4) are fsync-boundary. Steps (1), (2), and (5) are not.
	IsFsyncBoundary bool
}

// BudgetExhaustionHandlerSequence returns the canonical ordered slice of all
// five RC-018 budget-exhaustion handler steps.
//
// The returned slice is the normative documentation of the step ordering per
// RC-018 — any reordering is a spec-level amendment per RC-009 / architecture.md
// §4.6. Tests verify the count and ordering.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.
func BudgetExhaustionHandlerSequence() []BudgetExhaustionHandlerStep {
	return []BudgetExhaustionHandlerStep{
		{
			Label:           BudgetExhaustionStepTerminateSubprocess,
			Description:     "SIGTERM investigator subprocess; SIGKILL after HC-018 interval; no durable write; crash here routes Cat 5 (no prior commit)",
			IsFsyncBoundary: false,
		},
		{
			Label:           BudgetExhaustionStepWaitForWatcher,
			Description:     "wait for HC-011 watcher goroutine to observe process exit and publish terminal session event to event bus; no durable write",
			IsFsyncBoundary: false,
		},
		{
			Label:           BudgetExhaustionStepEmitBudgetExhausted,
			Description:     "emit reconciliation_budget_exhausted (class F, fsync-boundary); crash after this step leaves budget_exhausted event durable with no verdict commit → Cat 3b retry cap (RC-026a) on restart",
			IsFsyncBoundary: true,
		},
		{
			Label:           BudgetExhaustionStepEmitFallbackVerdict,
			Description:     "emit fallback escalate-to-human verdict event (class F, fsync-boundary); structurally identical to investigator-emitted escalate-to-human per RC-018 indistinguishability requirement",
			IsFsyncBoundary: true,
		},
		{
			Label:           BudgetExhaustionStepVerdictExecutorConsumes,
			Description:     "verdict-executor (RC-025a) consumes fallback verdict as if investigator-emitted; applies escalate-to-human mechanical action per schemas.md §6.2",
			IsFsyncBoundary: false,
		},
	}
}

// SynthesizeBudgetExhaustionFallbackVerdict constructs the fallback VerdictEvent
// that the daemon emits at step (4) of the RC-018 budget-exhaustion handler.
//
// The returned VerdictEvent is structurally identical to an investigator-emitted
// escalate-to-human, satisfying the RC-018 requirement that "this verdict MUST
// be indistinguishable from an investigator-emitted escalate-to-human in the
// operator-facing surface." The verdict-executor (RC-025a) consumes it via the
// same path as any investigator-emitted verdict.
//
// Parameters:
//   - reconciliationRunID: the RunID of the reconciliation workflow whose budget
//     was exhausted. Used as InvestigatorRunID in the synthesized event (the
//     reconciliation workflow acts as its own investigator in the budget-exhaustion
//     path). Must not be the zero RunID.
//   - targetRunID: the RunID of the outer run being reconciled. Must not be the
//     zero RunID.
//   - snapshot: the SnapshotToken that was captured at investigator dispatch time.
//     Must be valid (snapshot.Valid() == true).
//
// Returns an error when any parameter is invalid.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-018.
func SynthesizeBudgetExhaustionFallbackVerdict(
	reconciliationRunID RunID,
	targetRunID RunID,
	snapshot SnapshotToken,
) (VerdictEvent, error) {
	if uuid.UUID(reconciliationRunID) == uuid.Nil {
		return VerdictEvent{}, fmt.Errorf("rc018: reconciliationRunID must not be the zero RunID")
	}
	if uuid.UUID(targetRunID) == uuid.Nil {
		return VerdictEvent{}, fmt.Errorf("rc018: targetRunID must not be the zero RunID")
	}
	if !snapshot.Valid() {
		return VerdictEvent{}, fmt.Errorf("rc018: snapshot must be valid (SnapshotToken.Valid() == true)")
	}

	return VerdictEvent{
		Verdict:           VerdictEscalateToHuman,
		InvestigatorRunID: uuid.UUID(reconciliationRunID),
		TargetRunID:       uuid.UUID(targetRunID),
		SnapshotToken:     snapshot,
		SchemaVersion:     1,
	}, nil
}
