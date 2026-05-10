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
// Callers MUST check the returned error and emit the appropriate bus event:
//   - nil + OutcomeDelivered   → emit agent_completed (optionally carrying
//     shutdown_exit_code when exitCode != 0, per HC-008a)
//   - ErrStructural            → emit agent_failed{class="structural",
//     sub_reason="crash_without_outcome"} per HC-024
//
// The sub-reason "crash_without_outcome" covers both the clean-exit-without-
// outcome and crash-without-outcome cases; the distinction is recorded in the
// exit_code payload field for operator observability.
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
