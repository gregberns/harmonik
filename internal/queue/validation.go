package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// QueueValidationReason enum (specs/queue-model.md §6.10 QM-029)
// ---------------------------------------------------------------------------

// QueueValidationReason is the set of typed failure reasons returned by
// Validate. The string values are wire-level constants per QM-029; additions
// require a spec amendment and a QM-029b error-code allocation.
type QueueValidationReason string

const (
	// ReasonQueueAlreadyActive — QM-027: a queue whose status is not completed
	// already exists; only one active queue is permitted per submit.
	// JSON-RPC error code -32010 per QM-029b.
	ReasonQueueAlreadyActive QueueValidationReason = "queue_already_active"

	// ReasonAppendTargetInvalid — QM-024: the target group_index does not
	// reference a stream group in pending or active status.
	// JSON-RPC error code -32011 per QM-029b.
	ReasonAppendTargetInvalid QueueValidationReason = "append_target_invalid"

	// ReasonQueueNotAdvancing — QM-024: queue is paused-by-failure or
	// paused-by-drain; appends are rejected while the queue is not advancing.
	// JSON-RPC error code -32012 per QM-029b.
	ReasonQueueNotAdvancing QueueValidationReason = "queue_not_advancing"

	// ReasonBeadNotFound — QM-020: a bead_id in the request does not exist in
	// the Beads ledger.
	// JSON-RPC error code -32013 per QM-029b.
	ReasonBeadNotFound QueueValidationReason = "bead_not_found"

	// ReasonBeadNotOpen — QM-021: a bead_id exists but its status is not "open".
	// JSON-RPC error code -32014 per QM-029b.
	ReasonBeadNotOpen QueueValidationReason = "bead_not_open"

	// ReasonBeadAlreadyDispatched — QM-022: a bead_id is already in_progress
	// in the Beads ledger from any source.
	// JSON-RPC error code -32015 per QM-029b.
	ReasonBeadAlreadyDispatched QueueValidationReason = "bead_already_dispatched"

	// ReasonDuplicateBeadID — QM-023: a bead_id appears more than once in the
	// request or in the target group.
	// JSON-RPC error code -32016 per QM-029b.
	ReasonDuplicateBeadID QueueValidationReason = "duplicate_bead_id"

	// ReasonQueueTooLarge — QM-026: the proposed mutation would cause
	// queue.json to exceed 1 MiB (1048576 bytes).
	// JSON-RPC error code -32017 per QM-029b.
	ReasonQueueTooLarge QueueValidationReason = "queue_too_large"

	// ReasonHandlerPaused — QM-052a: one or more beads in the request resolve
	// to an agent_type whose handler is currently paused. Per the handler-pause
	// spec (docs/components/internal/handler-pause-and-resume.md Appendix A.1),
	// the daemon rejects queue-submit when any bead's resolved agent_type is
	// paused; the detail map includes agent_type and the affected bead_ids.
	// JSON-RPC error code -32018 per QM-029b (previously reserved slot).
	ReasonHandlerPaused QueueValidationReason = "handler_paused"
)

// ---------------------------------------------------------------------------
// ValidationError (typed error shape)
// ---------------------------------------------------------------------------

// ValidationError is a single typed validation failure from the pipeline
// (specs/queue-model.md §6). The Reason field drives the JSON-RPC error code
// per QM-029b; Detail carries rule-specific fields for the caller.
//
// Per QM-028, validation failures MUST NOT emit events; they surface only on
// the JSON-RPC response.
type ValidationError struct {
	// Reason is one of the QueueValidationReason enum values per QM-029.
	Reason QueueValidationReason

	// Detail contains rule-specific context fields (bead_id, actual_status,
	// proposed_bytes, etc.) in a form suitable for JSON marshalling into the
	// JSON-RPC error payload.
	Detail map[string]any
}

// Error implements the error interface. The string form is human-readable and
// not a wire-level contract; callers that need machine-readable output should
// use the Reason and Detail fields directly.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("queue validation failed: %s (%v)", e.Reason, e.Detail)
}

// ---------------------------------------------------------------------------
// LedgerDepPair records a single QM-025 informational notice
// ---------------------------------------------------------------------------

// LedgerDepPair is one parallelism-narrowed notice: bead BeadID is blocked on
// BlockerBeadID within the same group per QM-025. These are collected and
// returned alongside a nil error when validation passes.
type LedgerDepPair struct {
	BeadID        core.BeadID
	BlockerBeadID core.BeadID
	GroupIndex    int
}

// ---------------------------------------------------------------------------
// BeadLedger interface — minimal seam for QM-020, QM-021, QM-022, QM-025
// ---------------------------------------------------------------------------

// BeadStatus is the ledger-reported lifecycle state of a bead.
// Only the values "open", "in_progress", and "not_found" are consumed by the
// validation pipeline; the underlying Beads ledger may carry additional states
// per beads-integration.md §4.3.
type BeadStatus string

const (
	// BeadStatusOpen means the bead exists and is open for dispatch per QM-021.
	BeadStatusOpen BeadStatus = "open"

	// BeadStatusInProgress means the bead is already being executed per QM-022.
	BeadStatusInProgress BeadStatus = "in_progress"

	// BeadStatusNotFound is returned by BeadLedger.LookupStatus when the bead
	// ID does not exist in the ledger per QM-020.
	BeadStatusNotFound BeadStatus = "not_found"
)

// BeadLedger is the minimal seam between the validation pipeline and the Beads
// ledger (specs/beads-integration.md §4.3 / §4.5). Production code wires this
// to the beads adapter; tests use a fake.
//
// All methods MUST be safe for concurrent use. The context carries deadlines
// from the enclosing JSON-RPC request.
type BeadLedger interface {
	// LookupStatus returns the ledger status for id. Returns
	// BeadStatusNotFound when the bead does not exist in the ledger.
	LookupStatus(ctx context.Context, id core.BeadID) (BeadStatus, error)

	// BlocksEdge reports whether the Beads ledger declares a "blocks" edge from
	// blocker to blocked (i.e., blocker must complete before blocked may start)
	// per beads-integration.md §4.3 BI-006. Returns false if either bead is
	// unknown or no such edge exists.
	BlocksEdge(ctx context.Context, blocker, blocked core.BeadID) (bool, error)
}

// ---------------------------------------------------------------------------
// HandlerPauseChecker — minimal seam for QM-052a handler-pause validation
// ---------------------------------------------------------------------------

// HandlerPauseChecker is the minimal seam between the validation pipeline and
// the daemon's handler-pause controller (docs/components/internal/handler-pause-and-resume.md).
//
// When non-nil in a ValidationRequest, Validate evaluates QM-052a: any bead
// whose resolved agent_type maps to a currently-paused handler causes
// ReasonHandlerPaused. When nil, QM-052a is skipped (the hk-9hwbw
// HandlerPauseController is not yet wired; nil == check disabled).
//
// All methods MUST be safe for concurrent use. The context carries deadlines
// from the enclosing JSON-RPC request.
type HandlerPauseChecker interface {
	// ResolvedAgentType returns the agent_type that would be used to dispatch
	// bead id. Returns an error if the bead's agent_type cannot be determined.
	ResolvedAgentType(ctx context.Context, id core.BeadID) (core.AgentType, error)

	// IsHandlerPaused reports whether the handler for agentType is currently
	// paused. Returns false if the handler is live or unknown.
	IsHandlerPaused(ctx context.Context, agentType core.AgentType) (bool, error)
}

// ---------------------------------------------------------------------------
// ValidationRequest — input shape for Validate
// ---------------------------------------------------------------------------

// ValidationRequest carries the parameters for a single validation pass.
// It is created by the JSON-RPC method handlers (T60/hk-nomxl) before calling
// Validate; the bead body and spec ref live there.
type ValidationRequest struct {
	// Groups is the ordered list of groups being submitted or appended.
	// For queue-submit this is the full set; for queue-append this is the
	// single target group (after the append target has been located).
	Groups []Group

	// ActiveQueue is the daemon's current in-memory queue, or nil if no queue
	// is loaded. Used for QM-027 (single-active-queue) and QM-026 (size bound).
	ActiveQueue *Queue

	// IsAppend distinguishes queue-append from queue-submit. When true,
	// QM-027 is skipped (submit-only) and QM-024 is evaluated instead.
	IsAppend bool

	// AppendGroupIndex is the 0-based target group_index for queue-append.
	// Ignored when IsAppend is false.
	AppendGroupIndex int

	// PauseChecker is the optional handler-pause seam for QM-052a validation.
	// When non-nil, Validate checks each bead's resolved agent_type against the
	// handler-pause controller and rejects with ReasonHandlerPaused if any
	// handler is paused. When nil, QM-052a is skipped (controller not yet wired).
	//
	// Spec ref: handler-pause-and-resume.md Appendix A.1; queue-model.md §8.3a QM-052a.
	PauseChecker HandlerPauseChecker
}

// ---------------------------------------------------------------------------
// Validate — 9-rule pipeline (QM-029a order + QM-052a)
// ---------------------------------------------------------------------------

// maxQueueJSON is the persisted-size limit per QM-026 / QM-004: 1 MiB.
const maxQueueJSON = 1048576

// Validate runs the validation rules in QM-029a order against req. It
// returns a single-element slice on the first failing rule (first-failure
// short-circuit per QM-029a), an empty slice on pass, and nil when QM-025
// informational notices are collected.
//
// QM-025 (parallelism-narrowed) is informational; it is collected into the
// returned LedgerDepPairs slice but never causes a ValidationError.
//
// Order: QM-027 (submit-only) → QM-024 (append-only) → QM-020 → QM-021 →
// QM-022 → QM-052a (handler-pause, optional) → QM-023 → QM-026 →
// QM-025 (informational, last).
//
// Spec ref: queue-model.md §6 QM-020..QM-027, QM-029, QM-029a;
// handler-pause-and-resume.md Appendix A.1 (QM-052a).
func Validate(ctx context.Context, req ValidationRequest, ledger BeadLedger) ([]ValidationError, []LedgerDepPair, error) {
	// --- QM-027: single active queue (submit-only) ---------------------------
	if !req.IsAppend {
		if req.ActiveQueue != nil && req.ActiveQueue.Status != QueueStatusCompleted {
			return []ValidationError{
				{
					Reason: ReasonQueueAlreadyActive,
					Detail: map[string]any{
						"existing_queue_id": req.ActiveQueue.QueueID,
						"existing_status":   string(req.ActiveQueue.Status),
					},
				},
			}, nil, nil
		}
	}

	// --- QM-024: append target validity (append-only) -----------------------
	if req.IsAppend {
		if req.ActiveQueue == nil {
			return []ValidationError{
				{
					Reason: ReasonAppendTargetInvalid,
					Detail: map[string]any{
						"group_index":   req.AppendGroupIndex,
						"actual_kind":   nil,
						"actual_status": nil,
					},
				},
			}, nil, nil
		}
		// Check queue advancing status first.
		if req.ActiveQueue.Status == QueueStatusPausedByFailure ||
			req.ActiveQueue.Status == QueueStatusPausedByDrain {
			return []ValidationError{
				{
					Reason: ReasonQueueNotAdvancing,
					Detail: map[string]any{
						"queue_status": string(req.ActiveQueue.Status),
					},
				},
			}, nil, nil
		}
		// Validate the target group.
		idx := req.AppendGroupIndex
		if idx < 0 || idx >= len(req.ActiveQueue.Groups) {
			return []ValidationError{
				{
					Reason: ReasonAppendTargetInvalid,
					Detail: map[string]any{
						"group_index":   idx,
						"actual_kind":   nil,
						"actual_status": nil,
					},
				},
			}, nil, nil
		}
		target := req.ActiveQueue.Groups[idx]
		if target.Kind != GroupKindStream {
			return []ValidationError{
				{
					Reason: ReasonAppendTargetInvalid,
					Detail: map[string]any{
						"group_index":   idx,
						"actual_kind":   string(target.Kind),
						"actual_status": string(target.Status),
					},
				},
			}, nil, nil
		}
		if target.Status != GroupStatusPending && target.Status != GroupStatusActive {
			return []ValidationError{
				{
					Reason: ReasonAppendTargetInvalid,
					Detail: map[string]any{
						"group_index":   idx,
						"actual_kind":   string(target.Kind),
						"actual_status": string(target.Status),
					},
				},
			}, nil, nil
		}
	}

	// Collect all bead IDs across groups for existence/status/duplicate checks.
	// For append, the groups slice contains only the appended items; for submit
	// it contains all submitted groups.
	var allBeadIDs []core.BeadID
	for _, g := range req.Groups {
		for _, item := range g.Items {
			allBeadIDs = append(allBeadIDs, item.BeadID)
		}
	}

	// --- QM-020: bead existence ---------------------------------------------
	for _, id := range allBeadIDs {
		status, err := ledger.LookupStatus(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("QM-020 ledger lookup %q: %w", id, err)
		}
		if status == BeadStatusNotFound {
			return []ValidationError{
				{
					Reason: ReasonBeadNotFound,
					Detail: map[string]any{
						"bead_id": string(id),
					},
				},
			}, nil, nil
		}
	}

	// --- QM-021: bead status (must be open) ---------------------------------
	// Per QM-029a, QM-021 runs before QM-022. To preserve distinct reason codes,
	// QM-021 rejects beads whose status is neither open nor in_progress
	// (in_progress is reserved for QM-022's bead_already_dispatched reason).
	// Any other non-open status (closed, blocked, deferred, draft, etc.) surfaces
	// here as bead_not_open.
	for _, id := range allBeadIDs {
		status, err := ledger.LookupStatus(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("QM-021 ledger lookup %q: %w", id, err)
		}
		if status != BeadStatusOpen && status != BeadStatusInProgress {
			return []ValidationError{
				{
					Reason: ReasonBeadNotOpen,
					Detail: map[string]any{
						"bead_id":       string(id),
						"actual_status": string(status),
					},
				},
			}, nil, nil
		}
	}

	// --- QM-022: no double dispatch (must not be in_progress) ---------------
	// QM-022 fires after QM-021; at this point every bead is either open or
	// in_progress. Reject in_progress beads with the distinct bead_already_dispatched
	// reason per QM-022.
	for _, id := range allBeadIDs {
		status, err := ledger.LookupStatus(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("QM-022 ledger lookup %q: %w", id, err)
		}
		if status == BeadStatusInProgress {
			return []ValidationError{
				{
					Reason: ReasonBeadAlreadyDispatched,
					Detail: map[string]any{
						"bead_id": string(id),
					},
				},
			}, nil, nil
		}
	}

	// --- QM-052a: handler-pause check (optional seam) -----------------------
	// When PauseChecker is wired (hk-9hwbw HandlerPauseController), reject any
	// bead whose resolved agent_type maps to a currently-paused handler.
	// Orthogonal to paused-by-failure: the queue status is NOT changed here.
	// Per Appendix A.1, this is a submit-time gate — the bead never enters the
	// queue; the caller must retry after the handler is resumed.
	//
	// Spec ref: handler-pause-and-resume.md Appendix A.1; queue-model.md §8.3a QM-052a.
	if req.PauseChecker != nil {
		// Walk beads in order; stop at the first paused agent_type (first-failure
		// short-circuit per QM-029a). Collect all bead_ids for that agent_type
		// in the detail map for operator diagnostics.
		type pauseHit struct {
			agentType core.AgentType
			beadIDs   []string
		}
		var hit *pauseHit
		for _, id := range allBeadIDs {
			at, atErr := req.PauseChecker.ResolvedAgentType(ctx, id)
			if atErr != nil {
				return nil, nil, fmt.Errorf("QM-052a ResolvedAgentType %q: %w", id, atErr)
			}
			paused, pErr := req.PauseChecker.IsHandlerPaused(ctx, at)
			if pErr != nil {
				return nil, nil, fmt.Errorf("QM-052a IsHandlerPaused %q: %w", at, pErr)
			}
			if paused {
				if hit == nil {
					// First paused agent_type found; collect all beads in the submission
					// that resolve to this same paused handler for the detail map.
					h := &pauseHit{agentType: at}
					for _, id2 := range allBeadIDs {
						at2, at2Err := req.PauseChecker.ResolvedAgentType(ctx, id2)
						if at2Err != nil {
							return nil, nil, fmt.Errorf("QM-052a ResolvedAgentType (collection) %q: %w", id2, at2Err)
						}
						if at2 == at {
							h.beadIDs = append(h.beadIDs, string(id2))
						}
					}
					hit = h
				}
				break
			}
		}
		if hit != nil {
			return []ValidationError{
				{
					Reason: ReasonHandlerPaused,
					Detail: map[string]any{
						"agent_type": string(hit.agentType),
						"bead_ids":   hit.beadIDs,
					},
				},
			}, nil, nil
		}
	}

	// --- QM-023: no duplicates (cross-group or intra-group) -----------------
	// For submit: bead_id MUST NOT appear in more than one group AND not more
	// than once within a group.
	// For append: bead_id MUST NOT appear more than once in the appended set
	// AND MUST NOT already appear as a non-terminal item in the target group.
	seen := make(map[core.BeadID]struct{})
	for _, g := range req.Groups {
		intraGroupSeen := make(map[core.BeadID]struct{})
		for _, item := range g.Items {
			// Intra-group duplicate check.
			if _, dup := intraGroupSeen[item.BeadID]; dup {
				return []ValidationError{
					{
						Reason: ReasonDuplicateBeadID,
						Detail: map[string]any{
							"bead_id": string(item.BeadID),
						},
					},
				}, nil, nil
			}
			intraGroupSeen[item.BeadID] = struct{}{}

			// Cross-group duplicate check (submit only — for append the outer
			// loop has a single group so this catches the intra-append case).
			if _, dup := seen[item.BeadID]; dup {
				return []ValidationError{
					{
						Reason: ReasonDuplicateBeadID,
						Detail: map[string]any{
							"bead_id": string(item.BeadID),
						},
					},
				}, nil, nil
			}
			seen[item.BeadID] = struct{}{}
		}
	}

	// For append: check that no submitted bead already exists as a non-terminal
	// item in the target group.
	if req.IsAppend && req.ActiveQueue != nil {
		idx := req.AppendGroupIndex
		if idx >= 0 && idx < len(req.ActiveQueue.Groups) {
			target := req.ActiveQueue.Groups[idx]
			existingNonTerminal := make(map[core.BeadID]struct{})
			for _, item := range target.Items {
				if item.Status != ItemStatusCompleted && item.Status != ItemStatusFailed {
					existingNonTerminal[item.BeadID] = struct{}{}
				}
			}
			for _, id := range allBeadIDs {
				if _, dup := existingNonTerminal[id]; dup {
					return []ValidationError{
						{
							Reason: ReasonDuplicateBeadID,
							Detail: map[string]any{
								"bead_id": string(id),
							},
						},
					}, nil, nil
				}
			}
		}
	}

	// --- QM-026: persisted-size bound (1 MiB) --------------------------------
	// Build the would-be Queue envelope in memory and check its marshalled size.
	proposedQueue := buildProposedQueue(req)
	data, err := json.Marshal(proposedQueue)
	if err != nil {
		return nil, nil, fmt.Errorf("QM-026 marshal proposed queue: %w", err)
	}
	if len(data) > maxQueueJSON {
		return []ValidationError{
			{
				Reason: ReasonQueueTooLarge,
				Detail: map[string]any{
					"proposed_bytes": len(data),
					"limit":          maxQueueJSON,
				},
			},
		}, nil, nil
	}

	// --- QM-025: parallelism-narrowed (informational, last) -----------------
	// Collect blocks edges within each submitted group. This pass never fails
	// validation; it returns informational LedgerDepPairs to the caller.
	var notices []LedgerDepPair
	for gi, g := range req.Groups {
		groupIndex := gi
		if req.IsAppend {
			groupIndex = req.AppendGroupIndex
		}
		for i := 0; i < len(g.Items); i++ {
			for j := 0; j < len(g.Items); j++ {
				if i == j {
					continue
				}
				a := g.Items[i].BeadID
				b := g.Items[j].BeadID
				blocks, bErr := ledger.BlocksEdge(ctx, a, b)
				if bErr != nil {
					return nil, nil, fmt.Errorf("QM-025 ledger blocks-edge %q→%q: %w", a, b, bErr)
				}
				if blocks {
					// a blocks b: b is the blocked item.
					notices = append(notices, LedgerDepPair{
						BeadID:        b,
						BlockerBeadID: a,
						GroupIndex:    groupIndex,
					})
				}
			}
		}
	}

	return nil, notices, nil
}

// ---------------------------------------------------------------------------
// buildProposedQueue — construct the would-be Queue for QM-026
// ---------------------------------------------------------------------------

// buildProposedQueue assembles the Queue envelope that would result if req
// were accepted, for the purpose of the QM-026 size check. It does NOT
// persist anything.
func buildProposedQueue(req ValidationRequest) Queue {
	if !req.IsAppend {
		// Submit: the proposed queue is entirely from the request.
		return Queue{
			SchemaVersion: 1,
			QueueID:       "00000000-0000-0000-0000-000000000000",
			Status:        QueueStatusActive,
			Groups:        req.Groups,
		}
	}
	// Append: clone the active queue and append to the target group.
	if req.ActiveQueue == nil {
		return Queue{}
	}
	proposed := *req.ActiveQueue
	groups := make([]Group, len(proposed.Groups))
	copy(groups, proposed.Groups)
	if req.AppendGroupIndex >= 0 && req.AppendGroupIndex < len(groups) {
		g := groups[req.AppendGroupIndex]
		existingItems := make([]Item, len(g.Items))
		copy(existingItems, g.Items)
		for _, ng := range req.Groups {
			existingItems = append(existingItems, ng.Items...)
		}
		g.Items = existingItems
		groups[req.AppendGroupIndex] = g
	}
	proposed.Groups = groups
	return proposed
}
