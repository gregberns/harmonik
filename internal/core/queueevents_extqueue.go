package core

// queueevents_extqueue.go — event-bus payload types for §8.10 queue lifecycle
// events (extqueue v0.1):
//
//   - queue_submitted                     (§8.10.1)
//   - queue_group_started                 (§8.10.2)
//   - queue_group_completed               (§8.10.3)
//   - queue_paused                        (§8.10.4)
//   - queue_appended                      (§8.10.5)
//   - queue_item_deferred_for_ledger_dep  (§8.10.6)
//   - queue_item_reconciled               (§8.10.7)
//
// Spec ref: specs/event-model.md §8.10, §6.3.
// Bead ref: hk-yslws.

// ---------------------------------------------------------------------------
// Payload structs for §8.10 events
// ---------------------------------------------------------------------------

// QueueSubmittedPayload is the typed event payload for the queue_submitted event
// (event-model.md §8.10.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss orphans the queue's execution plan per EV-016).
//
// Emitted by the queue subsystem immediately after a new queue submission is
// persisted to queue.json. The event records the queue's identity, the wall-clock
// time of submission, and the group/bead counts describing the submitted plan.
//
// # Payload fields (event-model.md §8.10.1)
//
//   - queue_id         — UUIDv7 as string per queue-model.md §4 QM-010..012
//   - submitted_at     — RFC 3339 wall-clock timestamp at submission
//   - group_count      — number of groups in the submitted plan
//   - total_bead_count — total number of bead items across all groups
//   - schema_version   — queue.json document version (distinct from envelope schema_version)
//
// Note: QueueID is a plain string until a typed QueueID alias is minted.
// TODO: replace string with typed QueueID alias once minted (tracked in hk-gkljz
// follow-up per implementer-protocol.md §Typed-alias-deferral).
type QueueSubmittedPayload struct {
	// QueueID is the daemon-minted UUIDv7 (as a string) identifying the queue
	// submission. Required (non-empty).
	QueueID string `json:"queue_id"`

	// SubmittedAt is the RFC 3339 wall-clock timestamp at submission.
	// Required (non-empty).
	SubmittedAt string `json:"submitted_at"`

	// GroupCount is the number of groups in the submitted plan. Required (>= 1).
	GroupCount int `json:"group_count"`

	// TotalBeadCount is the total number of bead items across all groups.
	// Required (>= 1).
	TotalBeadCount int `json:"total_bead_count"`

	// QueueSchemaVersion is the version of the queue.json document per
	// queue-model.md §2. Distinct from the event envelope schema_version per
	// EV-028. Required (>= 1).
	QueueSchemaVersion int `json:"schema_version"`
}

// Valid reports whether p is a well-formed QueueSubmittedPayload.
//
// Rules per event-model.md §8.10.1:
//   - QueueID must be non-empty.
//   - SubmittedAt must be non-empty.
//   - GroupCount must be >= 1.
//   - TotalBeadCount must be >= 1.
//   - QueueSchemaVersion must be >= 1.
func (p QueueSubmittedPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.SubmittedAt == "" {
		return false
	}
	if p.GroupCount < 1 {
		return false
	}
	if p.TotalBeadCount < 1 {
		return false
	}
	if p.QueueSchemaVersion < 1 {
		return false
	}
	return true
}

// QueueGroupStartedPayload is the typed event payload for the queue_group_started
// event (event-model.md §8.10.2).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — reconstructible from the predecessor group's
// queue_group_completed plus queue.json per §8.10 Section Axes).
//
// Emitted by the queue subsystem when the dispatcher begins processing a new group.
//
// # Payload fields (event-model.md §8.10.2)
//
//   - queue_id    — UUIDv7 as string
//   - group_index — zero-based index of the group
//   - group_kind  — enum: wave | stream
//   - item_count  — number of bead items in this group
//   - started_at  — RFC 3339 wall-clock timestamp
type QueueGroupStartedPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of this group within the queue.
	// Required (>= 0).
	GroupIndex int `json:"group_index"`

	// GroupKind is the kind of this group. Required; must be "wave" or "stream"
	// per queue-model.md §2.
	GroupKind string `json:"group_kind"`

	// ItemCount is the number of bead items in this group. Required (>= 1).
	ItemCount int `json:"item_count"`

	// StartedAt is the RFC 3339 wall-clock timestamp when the group began.
	// Required (non-empty).
	StartedAt string `json:"started_at"`
}

// Valid reports whether p is a well-formed QueueGroupStartedPayload.
//
// Rules per event-model.md §8.10.2:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - GroupKind must be "wave" or "stream".
//   - ItemCount must be >= 1.
//   - StartedAt must be non-empty.
func (p QueueGroupStartedPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if p.GroupKind != "wave" && p.GroupKind != "stream" {
		return false
	}
	if p.ItemCount < 1 {
		return false
	}
	if p.StartedAt == "" {
		return false
	}
	return true
}

// QueueGroupCompletedPayload is the typed event payload for the
// queue_group_completed event (event-model.md §8.10.3).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — group-boundary advance landmark per EV-016).
//
// Emitted by the queue subsystem after every item in the active group has
// reached a terminal state. The §8.1 terminal run_completed / run_failed for
// the last item of the group MUST precede this event (never follow it).
//
// # Payload fields (event-model.md §8.10.3)
//
//   - queue_id      — UUIDv7 as string
//   - group_index   — zero-based group index
//   - final_status  — enum: complete-success | complete-with-failures
//   - success_count — number of successful items
//   - fail_count    — number of failed items
//   - completed_at  — RFC 3339 wall-clock timestamp
type QueueGroupCompletedPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of this group. Required (>= 0).
	GroupIndex int `json:"group_index"`

	// FinalStatus is the completion outcome. Required; must be
	// "complete-success" or "complete-with-failures" per queue-model.md §5.
	FinalStatus string `json:"final_status"`

	// SuccessCount is the number of items that completed successfully.
	// Required (>= 0).
	SuccessCount int `json:"success_count"`

	// FailCount is the number of items that failed. Required (>= 0).
	FailCount int `json:"fail_count"`

	// CompletedAt is the RFC 3339 wall-clock timestamp when the group completed.
	// Required (non-empty).
	CompletedAt string `json:"completed_at"`
}

// Valid reports whether p is a well-formed QueueGroupCompletedPayload.
//
// Rules per event-model.md §8.10.3:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - FinalStatus must be "complete-success" or "complete-with-failures".
//   - SuccessCount must be >= 0.
//   - FailCount must be >= 0.
//   - CompletedAt must be non-empty.
func (p QueueGroupCompletedPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if p.FinalStatus != "complete-success" && p.FinalStatus != "complete-with-failures" {
		return false
	}
	if p.SuccessCount < 0 {
		return false
	}
	if p.FailCount < 0 {
		return false
	}
	if p.CompletedAt == "" {
		return false
	}
	return true
}

// QueuePausedPayload is the typed event payload for the queue_paused event
// (event-model.md §8.10.4).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — hard execution stop landmark per EV-016).
//
// Emitted by the queue subsystem when a queue is paused. Per the §8.10 ordering
// rule, when reason is "group_failure", this event MUST follow queue_group_completed
// for the same group (group_completed first, paused second).
//
// # Payload fields (event-model.md §8.10.4)
//
//   - queue_id    — UUIDv7 as string
//   - group_index — zero-based group index at which the pause occurred
//   - fail_count  — number of failed items that triggered the pause
//   - paused_at   — RFC 3339 wall-clock timestamp
//   - reason      — enum: group_failure | operator_drain
type QueuePausedPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of the group at which the pause occurred.
	// Required (>= 0).
	GroupIndex int `json:"group_index"`

	// FailCount is the number of failed items that contributed to the pause.
	// Required (>= 0).
	FailCount int `json:"fail_count"`

	// PausedAt is the RFC 3339 wall-clock timestamp when the queue was paused.
	// Required (non-empty).
	PausedAt string `json:"paused_at"`

	// Reason is the pause cause. Required; exhaustive enum at MVH:
	// "group_failure" (pause-by-failure path) or "operator_drain"
	// (operator-initiated drain). New variants require an EV-027 amendment
	// per event-model.md §8.10 queue_paused note.
	Reason string `json:"reason"`
}

// Valid reports whether p is a well-formed QueuePausedPayload.
//
// Rules per event-model.md §8.10.4:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - FailCount must be >= 0.
//   - PausedAt must be non-empty.
//   - Reason must be "group_failure" or "operator_drain".
func (p QueuePausedPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if p.FailCount < 0 {
		return false
	}
	if p.PausedAt == "" {
		return false
	}
	if p.Reason != "group_failure" && p.Reason != "operator_drain" {
		return false
	}
	return true
}

// QueueAppendedPayload is the typed event payload for the queue_appended event
// (event-model.md §8.10.5).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — reconstructible from queue.json mutation history
// per §8.10 Section Axes).
//
// Emitted by the queue subsystem when bead items are appended to an active
// stream group per queue-model.md §7. Ignored on wave groups (immutable
// post-submit). May interleave at any point on a stream group in pending or
// active status per the §8.10 ordering rule.
//
// # Payload fields (event-model.md §8.10.5)
//
//   - queue_id           — UUIDv7 as string
//   - group_index        — zero-based group index of the stream group receiving the append
//   - appended_bead_ids  — the bead IDs appended in this operation (String[])
//   - appended_at        — RFC 3339 wall-clock timestamp
type QueueAppendedPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of the stream group receiving the append.
	// Required (>= 0).
	GroupIndex int `json:"group_index"`

	// AppendedBeadIDs is the list of bead IDs appended in this operation.
	// Required (non-nil, len >= 1).
	AppendedBeadIDs []string `json:"appended_bead_ids"`

	// AppendedAt is the RFC 3339 wall-clock timestamp when the append occurred.
	// Required (non-empty).
	AppendedAt string `json:"appended_at"`
}

// Valid reports whether p is a well-formed QueueAppendedPayload.
//
// Rules per event-model.md §8.10.5:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - AppendedBeadIDs must be non-nil and non-empty.
//   - AppendedAt must be non-empty.
func (p QueueAppendedPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if len(p.AppendedBeadIDs) == 0 {
		return false
	}
	if p.AppendedAt == "" {
		return false
	}
	return true
}

// QueueItemDeferredForLedgerDepPayload is the typed event payload for the
// queue_item_deferred_for_ledger_dep event (event-model.md §8.10.6).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent
// Durability class: O (ordinary — reconstructible from ledger state plus queue.json
// per §8.10 Section Axes; the dispatcher re-evaluates eligibility from ledger
// state on each dispatch tick).
//
// Emitted by the queue subsystem at submit-time or dispatch-time when a bead
// item cannot be dispatched because a blocking ledger dependency has not yet
// reached a terminal state per queue-model.md §6 QM-020..026.
//
// # Payload fields (event-model.md §8.10.6)
//
//   - queue_id        — UUIDv7 as string
//   - group_index     — zero-based group index containing the deferred item
//   - bead_id         — the bead item that was deferred
//   - blocker_bead_id — the bead whose non-terminal state blocks dispatch
//   - detected_at     — RFC 3339 wall-clock timestamp
type QueueItemDeferredForLedgerDepPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of the group containing the deferred item.
	// Required (>= 0).
	GroupIndex int `json:"group_index"`

	// BeadID is the bead item that was deferred. Required (non-empty).
	BeadID string `json:"bead_id"`

	// BlockerBeadID is the bead whose non-terminal ledger state blocks dispatch.
	// Required (non-empty).
	BlockerBeadID string `json:"blocker_bead_id"`

	// DetectedAt is the RFC 3339 wall-clock timestamp when the deferral was
	// detected. Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed QueueItemDeferredForLedgerDepPayload.
//
// Rules per event-model.md §8.10.6:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - BeadID must be non-empty.
//   - BlockerBeadID must be non-empty.
//   - DetectedAt must be non-empty.
func (p QueueItemDeferredForLedgerDepPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.BlockerBeadID == "" {
		return false
	}
	if p.DetectedAt == "" {
		return false
	}
	return true
}

// QueueItemReconciledPayload is the typed event payload for the
// queue_item_reconciled event (event-model.md §8.10.7; added in QM-002a v0.1.1).
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent
// Durability class: F (fsync-boundary — loss could silently re-dispatch an
// already-reverted item, so the correction MUST be durable before proceeding
// per §8.10.7 and EV-016).
//
// Emitted by the queue subsystem during startup reconciliation per
// queue-model.md §3.2a QM-002a: when an item recorded as "dispatched" in
// queue.json is found to be "open" in the Beads ledger at daemon startup,
// indicating the prior claim-write succeeded for the queue but the
// corresponding Beads write was lost. The item is reverted to "pending"
// BEFORE this event is emitted.
//
// # Payload fields (event-model.md §8.10.7)
//
//   - queue_id      — UUIDv7 as string
//   - group_index   — zero-based group index containing the reconciled item
//   - bead_id       — the bead item that was reconciled and reverted to pending
//   - reason        — enum: "claim_write_lost" (exhaustive at MVH)
//   - reconciled_at — RFC 3339 wall-clock timestamp
type QueueItemReconciledPayload struct {
	// QueueID is the daemon-minted UUIDv7 string identifying the queue.
	// Required (non-empty).
	QueueID string `json:"queue_id"`

	// GroupIndex is the zero-based index of the group containing the reconciled
	// item. Required (>= 0).
	GroupIndex int `json:"group_index"`

	// BeadID is the bead item that was reconciled (reverted to pending).
	// Required (non-empty).
	BeadID string `json:"bead_id"`

	// Reason is the reconciliation cause. Required; exhaustive enum at MVH:
	// "claim_write_lost" per queue-model.md §3.2a QM-002a.
	Reason string `json:"reason"`

	// ReconciledAt is the RFC 3339 wall-clock timestamp when the reconciliation
	// correction was applied. Required (non-empty).
	ReconciledAt string `json:"reconciled_at"`
}

// Valid reports whether p is a well-formed QueueItemReconciledPayload.
//
// Rules per event-model.md §8.10.7:
//   - QueueID must be non-empty.
//   - GroupIndex must be >= 0.
//   - BeadID must be non-empty.
//   - Reason must be "claim_write_lost".
//   - ReconciledAt must be non-empty.
func (p QueueItemReconciledPayload) Valid() bool {
	if p.QueueID == "" {
		return false
	}
	if p.GroupIndex < 0 {
		return false
	}
	if p.BeadID == "" {
		return false
	}
	if p.Reason != "claim_write_lost" {
		return false
	}
	if p.ReconciledAt == "" {
		return false
	}
	return true
}
