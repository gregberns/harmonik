package handlercontract

// outcomeDelivery — per-bead helper prefix for test helpers in outcomedelivery_test.go.
// (The prefix declaration is here as a godoc anchor per implementer-protocol.md
// §Helper-prefix discipline.)

// OutcomeEmittedMsg is the on-wire NDJSON message the handler subprocess emits
// as the final progress-stream message (specs/handler-contract.md §4.2.HC-008).
//
// The daemon-side watcher decodes this struct from the progress-stream line
// whose "type" field equals "outcome_emitted", then translates it into the
// outcome_emitted bus event per §6.4 and event-model.md §8.1.8.
//
// # Wire fields
//
// Normative field list per event-model.md §8.1.8:
//
//   - type             — always "outcome_emitted"; not stored (used for dispatch).
//   - run_id           — identifies the run this outcome belongs to.
//   - session_id       — the session that produced the outcome.
//   - node_id          — the workflow node that ran.
//   - outcome_status   — one of the four OutcomeStatus values (SUCCESS / FAIL /
//     RETRY / PARTIAL_SUCCESS per execution-model.md §4.1.EM-005).
//   - outcome_kind     — OutcomeKind discriminator per execution-model.md
//     §4.1.EM-005a (v0.3.3).  Defaults to "default" when absent on-wire.
//   - preferred_label  — optional routing hint; nil when absent.
//   - suggested_next_ids — optional ordered list of node IDs; nil when absent.
//
// The watcher MUST copy OutcomeStatus, OutcomeKind, PreferredLabel, and
// SuggestedNextIDs from this message into the core.Outcome it returns from
// Session.Wait without rewriting (handler-contract.md §4.2.HC-008,
// execution-model.md §4.1.EM-005a).
//
// JSON tags match the on-wire field names defined in event-model.md §8.1.8.
type OutcomeEmittedMsg struct {
	// Type is always "outcome_emitted"; retained for round-trip fidelity.
	// The watcher dispatches on this field before decoding into OutcomeEmittedMsg.
	Type string `json:"type"`

	// RunID identifies the run this outcome belongs to (event-model.md §8.1.8).
	RunID string `json:"run_id"`

	// SessionID is the session that produced the outcome (event-model.md §8.1.8).
	SessionID string `json:"session_id"`

	// NodeID is the workflow node that ran (event-model.md §8.1.8).
	NodeID string `json:"node_id"`

	// OutcomeStatus is the result status — one of SUCCESS / FAIL / RETRY /
	// PARTIAL_SUCCESS per execution-model.md §4.1.EM-005 and §6.1 ENUM.
	// Watcher MUST reject on-wire values not in the declared set.
	OutcomeStatus string `json:"outcome_status"`

	// OutcomeKind is the discriminator for the outcome payload envelope per
	// execution-model.md §4.1.EM-005a (v0.3.3).  The daemon MUST set
	// core.Outcome.Kind from this field without rewriting.
	// When absent on-wire, the watcher MUST treat it as "default".
	OutcomeKind string `json:"outcome_kind,omitempty"`

	// PreferredLabel is an optional routing hint (execution-model.md §4.10.EM-041).
	// Nil when absent on-wire.
	PreferredLabel *string `json:"preferred_label,omitempty"`

	// SuggestedNextIDs is an ordered list of node IDs the handler recommends as
	// next routing targets (execution-model.md §4.10.EM-041).  Nil when absent.
	SuggestedNextIDs []string `json:"suggested_next_ids,omitempty"`
}

// OutcomeDeliveryState tracks whether the watcher has received and published
// the outcome_emitted progress-stream message for the current session.
//
// This state is the key predicate for HC-008's exit-classification rule:
//
//   - exit 0  after OutcomeDelivered  → clean shutdown (session closes normally)
//   - exit 0  without OutcomeDelivered → treated as crash (structural failure)
//   - exit ≠0 after OutcomeDelivered  → dirty exit in shutdown window; outcome
//     durable (handled by HC-008a; watcher emits agent_completed with
//     shutdown_exit_code)
//   - exit ≠0 without OutcomeDelivered → crash; watcher emits agent_failed
//     (ErrStructural, sub-reason crash_without_outcome) per HC-024
//
// The watcher advances state from OutcomeNotYetDelivered to OutcomeDelivered
// exactly once — when it publishes the outcome_emitted bus event.  It is a
// daemon defect to observe subprocess exit before applying the transition.
//
// Cite: specs/handler-contract.md §4.2.HC-008, §4.6.HC-024.
type OutcomeDeliveryState bool

const (
	// OutcomeNotYetDelivered is the initial state: the watcher has not yet
	// received and published the outcome_emitted progress-stream message.
	OutcomeNotYetDelivered OutcomeDeliveryState = false

	// OutcomeDelivered is the terminal state: the watcher has received and
	// published the outcome_emitted progress-stream message to the bus.
	// Silent-hang detection is suspended from this point (HC-008a).
	OutcomeDelivered OutcomeDeliveryState = true
)

// SubworkflowBoundaryEmitSubReason is the sub_reason field value the watcher
// MUST use in the agent_failed payload when a handler emits outcome_emitted on
// a node whose type is NodeTypeSubWorkflow.
//
// Sub-workflow nodes are graph-level expansion constructs that MUST NOT be
// dispatched to a handler subprocess; the daemon calls SubWorkflowRunner.Run
// instead of Handler.Launch for such nodes (handler-contract.md §4.2a HC-058).
// If outcome_emitted is received while cfg.NodeType == NodeTypeSubWorkflow, the
// watcher MUST reject it as a structural error.
//
// Error class: ErrStructural.
// Cite: specs/handler-contract.md §4.2a HC-061.
const SubworkflowBoundaryEmitSubReason = "subworkflow_boundary_emit"

// CrashWithoutOutcomeSubReason is the sub_reason field value the watcher MUST
// include in the agent_failed payload when a subprocess exits (any exit code)
// without having delivered outcome_emitted per HC-024.
//
// The sub-reason covers both "crashed" (non-zero exit) and "clean-exit-without-
// outcome" (exit 0) cases; the distinction is carried in the exit_code payload
// field for operator observability.  The spec's §8.2 sub_reason list is
// non-exhaustive; this value is implementation-specific to HC-024.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §8.2.
const CrashWithoutOutcomeSubReason = "crash_without_outcome"

// CrashAgentFailedPayload is the structured agent_failed payload descriptor
// for the subprocess-crash-without-outcome case.
//
// The watcher uses these three strings to populate the agent_failed progress-
// stream message that it emits to the bus when ClassifyExit returns a non-nil
// error (specs/handler-contract.md §4.6.HC-024, §4.5, §8.2):
//
//   - ErrorCategory maps to the `error_category` wire field; it is the string
//     form of the sentinel returned by ClassifyExit, obtained via Class().
//   - Reason is the human-readable event kind; "crash_without_outcome" for this
//     case.
//   - SubReason equals CrashWithoutOutcomeSubReason for the crash case; empty
//     string when the struct is zero (no failure).
//
// Zero value (all fields empty) means no failure — callers MUST NOT emit
// agent_failed when ErrorCategory is empty.
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020, §8.2.
type CrashAgentFailedPayload struct {
	// ErrorCategory is the mapped sentinel class string per §4.5 / §8.
	// One of: "transient", "structural", "deterministic", "canceled", "budget".
	ErrorCategory string

	// Reason is the primary reason code placed in the agent_failed payload.
	Reason string

	// SubReason is the optional sub-reason code; empty when not applicable.
	SubReason string
}

// ClassifyCrash combines ClassifyExit with the agent_failed payload mapping
// required by HC-024.
//
// It returns a non-zero CrashAgentFailedPayload when exitCode/state indicates a
// failure that requires the watcher to emit agent_failed, and a zero
// CrashAgentFailedPayload (ErrorCategory == "") when no failure event is needed
// (clean shutdown after outcome delivery, or dirty-exit inside the shutdown
// window where HC-008a applies).
//
// Classification table (specs/handler-contract.md §4.2.HC-008, §4.6.HC-024):
//
//   - exitCode == 0 AND state == OutcomeDelivered   → zero payload (clean shutdown)
//   - exitCode != 0 AND state == OutcomeDelivered   → zero payload (HC-008a governs)
//   - exitCode == 0 AND state == OutcomeNotYetDelivered → {structural, crash_without_outcome}
//   - exitCode != 0 AND state == OutcomeNotYetDelivered → {structural, crash_without_outcome}
//
// The watcher MUST:
//  1. Call ClassifyCrash after subprocess Wait() returns.
//  2. If payload.ErrorCategory != "", emit agent_failed carrying payload fields.
//  3. If payload.ErrorCategory == "", emit agent_completed (carrying exit_code
//     when exitCode != 0, per HC-008a).
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020, §8.2.
func ClassifyCrash(exitCode int, state OutcomeDeliveryState) CrashAgentFailedPayload {
	err := ClassifyExit(exitCode, state)
	if err == nil {
		// Clean shutdown or dirty-exit inside the shutdown window (HC-008a).
		// Caller emits agent_completed; no agent_failed needed.
		return CrashAgentFailedPayload{}
	}
	// outcome_emitted was never published. Both exit-0 (handler bug) and
	// exit-nonzero (crash) map to ErrStructural per ClassifyExit.
	return CrashAgentFailedPayload{
		ErrorCategory: Class(err),
		Reason:        CrashWithoutOutcomeSubReason,
		SubReason:     CrashWithoutOutcomeSubReason,
	}
}

// ClassifyExit applies HC-008's exit-classification rule.
//
// It returns nil when the exit is expected (clean shutdown after outcome
// delivery), or a typed sentinel error for all failure cases.
//
// Classification table (specs/handler-contract.md §4.2.HC-008, §4.6.HC-024):
//
//   - exitCode == 0 AND state == OutcomeDelivered   → nil (clean shutdown)
//   - exitCode == 0 AND state == OutcomeNotYetDelivered → ErrStructural
//     (subprocess exited cleanly before delivering outcome; this is a handler
//     bug — the wire protocol requires outcome_emitted before exit)
//   - exitCode != 0 AND state == OutcomeDelivered   → nil (dirty exit inside
//     shutdown window; outcome is durable; HC-008a governs the terminal event
//     choice; ClassifyExit does NOT emit agent_completed — caller does)
//   - exitCode != 0 AND state == OutcomeNotYetDelivered → ErrStructural
//     (crash without outcome; watcher emits agent_failed per HC-024)
//
// Callers that need the full agent_failed payload should use ClassifyCrash
// instead, which returns the structured CrashAgentFailedPayload directly.
//
// Cite: specs/handler-contract.md §4.2.HC-008, §4.6.HC-024.
func ClassifyExit(exitCode int, state OutcomeDeliveryState) error {
	if state == OutcomeDelivered {
		// Outcome is durable regardless of exit code.  A non-zero exit is a
		// dirty exit inside the post-outcome shutdown window; HC-008a governs.
		return nil
	}
	// outcome_emitted was never published; any exit (clean or crash) is a
	// structural failure — the handler violated the wire protocol.
	return ErrStructural
}
