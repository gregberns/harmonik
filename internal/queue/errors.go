package queue

// ---------------------------------------------------------------------------
// JSON-RPC error-code constants for QueueValidationReason (QM-029b)
// ---------------------------------------------------------------------------
//
// The range -32010..-32019 is reserved for queue-model per
// specs/process-lifecycle.md §4.4 PL-003a. Each constant maps 1:1 to a
// QueueValidationReason enum value per the normative table in
// specs/queue-model.md §6.11a QM-029b. These are wire-level constants: once
// assigned they are immutable; changes require a spec amendment.
//
// -32019 remains reserved for a future QueueValidationReason addition within
// the v0.1 error-code block. Do not assign it without a spec amendment and
// a QM-029b table update.

const (
	// ErrorCodeQueueAlreadyActive is the JSON-RPC error code for
	// ReasonQueueAlreadyActive (QM-027 — single active queue, submit-only).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeQueueAlreadyActive = -32010

	// ErrorCodeAppendTargetInvalid is the JSON-RPC error code for
	// ReasonAppendTargetInvalid (QM-024 — target kind/status wrong).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeAppendTargetInvalid = -32011

	// ErrorCodeQueueNotAdvancing is the JSON-RPC error code for
	// ReasonQueueNotAdvancing (QM-024 — queue paused).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeQueueNotAdvancing = -32012

	// ErrorCodeBeadNotFound is the JSON-RPC error code for
	// ReasonBeadNotFound (QM-020 — bead existence check).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeBeadNotFound = -32013

	// ErrorCodeBeadNotOpen is the JSON-RPC error code for
	// ReasonBeadNotOpen (QM-021 — bead status check).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeBeadNotOpen = -32014

	// ErrorCodeBeadAlreadyDispatched is the JSON-RPC error code for
	// ReasonBeadAlreadyDispatched (QM-022 — no double dispatch).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeBeadAlreadyDispatched = -32015

	// ErrorCodeDuplicateBeadID is the JSON-RPC error code for
	// ReasonDuplicateBeadID (QM-023 — duplicate bead IDs).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeDuplicateBeadID = -32016

	// ErrorCodeQueueTooLarge is the JSON-RPC error code for
	// ReasonQueueTooLarge (QM-026 — 1 MiB persisted-size bound).
	// Spec ref: queue-model.md §6.11a QM-029b.
	ErrorCodeQueueTooLarge = -32017

	// ErrorCodeHandlerPaused is the JSON-RPC error code for
	// ReasonHandlerPaused (QM-052a — handler-pause queue-submit gate).
	// Allocated from the previously-reserved -32018 slot per QM-029b.
	// Spec ref: queue-model.md §6.11a QM-029b; specs/handler-pause.md §6 HP-025.
	ErrorCodeHandlerPaused = -32018

	// ErrorCodeQueueNameInvalid is the JSON-RPC error code for
	// ReasonQueueNameInvalid (QM-002/2.1 queue-naming rule: [a-z0-9-], 1–64 chars).
	// Allocated from the previously-reserved -32019 slot per QM-029b.
	// Bead ref: hk-tigaf.2.
	ErrorCodeQueueNameInvalid = -32019
)

// ---------------------------------------------------------------------------
// JSONRPCError — map QueueValidationReason to (code, message)
// ---------------------------------------------------------------------------

// JSONRPCError returns the JSON-RPC error code and a default message string
// for the given QueueValidationReason. Both the code and the message string
// are stable wire constants per QM-029b. The caller is responsible for
// populating the JSON-RPC error data field with the ValidationError.Detail
// map.
//
// Spec ref: queue-model.md §6.11a QM-029b; process-lifecycle.md §4.4 PL-003a.
func JSONRPCError(reason QueueValidationReason) (code int, message string) {
	switch reason {
	case ReasonQueueAlreadyActive:
		return ErrorCodeQueueAlreadyActive, "queue_already_active"
	case ReasonAppendTargetInvalid:
		return ErrorCodeAppendTargetInvalid, "append_target_invalid"
	case ReasonQueueNotAdvancing:
		return ErrorCodeQueueNotAdvancing, "queue_not_advancing"
	case ReasonBeadNotFound:
		return ErrorCodeBeadNotFound, "bead_not_found"
	case ReasonBeadNotOpen:
		return ErrorCodeBeadNotOpen, "bead_not_open"
	case ReasonBeadAlreadyDispatched:
		return ErrorCodeBeadAlreadyDispatched, "bead_already_dispatched"
	case ReasonDuplicateBeadID:
		return ErrorCodeDuplicateBeadID, "duplicate_bead_id"
	case ReasonQueueTooLarge:
		return ErrorCodeQueueTooLarge, "queue_too_large"
	case ReasonHandlerPaused:
		return ErrorCodeHandlerPaused, "handler_paused"
	case ReasonQueueNameInvalid:
		return ErrorCodeQueueNameInvalid, "queue_name_invalid"
	default:
		return -32099, "unknown_validation_reason"
	}
}
