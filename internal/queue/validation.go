package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/gregberns/harmonik/internal/core"
)

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
	// to an agent_type whose handler is currently paused. Per the normative
	// handler-pause spec (specs/handler-pause.md §6 HP-025),
	// the daemon rejects queue-submit when any bead's resolved agent_type is
	// paused; the detail map includes agent_type and the affected bead_ids.
	// JSON-RPC error code -32018 per QM-029b (previously reserved slot).
	ReasonHandlerPaused QueueValidationReason = "handler_paused"

	// ReasonQueueNameInvalid — QM-002/2.1 queue-naming rule: the name field
	// in a queue-submit request does not satisfy [a-z0-9-], 1–64 chars.
	// JSON-RPC error code -32019 per QM-029b.
	// Bead ref: hk-tigaf.2.
	ReasonQueueNameInvalid QueueValidationReason = "queue_name_invalid"
)

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

// LedgerDepPair is one parallelism-narrowed notice: bead BeadID is blocked on
// BlockerBeadID within the same group per QM-025. These are collected and
// returned alongside a nil error when validation passes.
type LedgerDepPair struct {
	BeadID        core.BeadID
	BlockerBeadID core.BeadID
	GroupIndex    int
}

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

// HandlerPauseChecker is the minimal seam between the validation pipeline and
// the daemon's handler-pause controller (specs/handler-pause.md §7).
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

const maxQueueNameLen = 64

var queueNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// NormaliseQueueName returns name if non-empty, else QueueNameMain ("main").
// Used by callers to apply the default-to-main rule before validation.
//
// Bead ref: hk-tigaf.2.
func NormaliseQueueName(name string) string {
	if name == "" {
		return QueueNameMain
	}
	return name
}

// ValidateQueueName reports whether name satisfies the QM-002/2.1 naming rule:
// 1–64 chars of [a-z0-9-]. Returns (true, "") on pass; (false, detail) on fail.
// An empty name is valid here because callers normalise before calling.
//
// Bead ref: hk-tigaf.2.
func ValidateQueueName(name string) (ok bool, detail string) {
	if name == "" {
		return false, "name must not be empty after normalisation"
	}
	if len(name) > maxQueueNameLen {
		return false, fmt.Sprintf("name length %d exceeds max %d", len(name), maxQueueNameLen)
	}
	if !queueNameRE.MatchString(name) {
		return false, "name must match [a-z0-9-]+"
	}
	return true, ""
}

// ValidationRequest carries the parameters for a single validation pass.
// It is created by the JSON-RPC method handlers (T60/hk-nomxl) before calling
// Validate; the bead body and spec ref live there.
type ValidationRequest struct {
	// Groups is the ordered list of groups being submitted or appended.
	// For queue-submit this is the full set; for queue-append this is the
	// single target group (after the append target has been located).
	Groups []Group

	// ActiveQueue is the daemon's current in-memory queue for the requested
	// queue name, or nil if no queue with that name is loaded. Used for
	// QM-027 (single-active-queue-per-name) and QM-026 (size bound).
	//
	// For queue-submit, the caller looks up the queue by QueueName in
	// QueueStore before building ValidationRequest; QM-027 then checks only
	// this per-name slot, not a global singleton.
	ActiveQueue *Queue

	// OtherQueues holds all active queues OTHER than the target queue, loaded
	// by the caller for the EM-065 cross-queue double-queue guard. When
	// non-nil, Validate checks submitted/appended beads against non-terminal
	// items in these queues and rejects with ReasonBeadAlreadyDispatched if
	// any bead is already pending/dispatched elsewhere.
	//
	// Spec ref: specs/execution-model.md §4.14 EM-065. Bead ref: hk-xizhl.
	OtherQueues []*Queue

	// QueueName is the normalised (non-empty) queue name being submitted.
	// Set by the caller after applying NormaliseQueueName. Used by the
	// QM-002/2.1 name-validity pre-check inserted before QM-027.
	// Ignored when IsAppend is true (append does not carry a queue name).
	//
	// Bead ref: hk-tigaf.2.
	QueueName string

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
	// Spec ref: specs/handler-pause.md §6 HP-025; queue-model.md §8.3a QM-052a.
	PauseChecker HandlerPauseChecker
}

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
// specs/handler-pause.md §6 HP-025 (QM-052a).
func Validate(ctx context.Context, req ValidationRequest, ledger BeadLedger) ([]ValidationError, []LedgerDepPair, error) {
	if !req.IsAppend && req.QueueName != "" {
		if ok, detail := ValidateQueueName(req.QueueName); !ok {
			return []ValidationError{
				{
					Reason: ReasonQueueNameInvalid,
					Detail: map[string]any{
						"name":   req.QueueName,
						"detail": detail,
					},
				},
			}, nil, nil
		}
	}

	if !req.IsAppend {
		if req.ActiveQueue != nil &&
			req.ActiveQueue.Status != QueueStatusCompleted &&
			req.ActiveQueue.Status != QueueStatusPausedByFailure &&
			req.ActiveQueue.Status != "" {
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

	var allBeadIDs []core.BeadID
	for _, g := range req.Groups {
		for _, item := range g.Items {
			allBeadIDs = append(allBeadIDs, item.BeadID)
		}
	}

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

	{
		em065Queued := make(map[core.BeadID]string)

		if req.IsAppend && req.ActiveQueue != nil {
			for gi, g := range req.ActiveQueue.Groups {
				if gi == req.AppendGroupIndex {
					continue
				}
				for _, item := range g.Items {
					if item.Status != ItemStatusCompleted && item.Status != ItemStatusFailed {
						em065Queued[item.BeadID] = fmt.Sprintf("queue %q group %d", req.ActiveQueue.Name, gi)
					}
				}
			}
		}

		for _, oq := range req.OtherQueues {
			if oq == nil {
				continue
			}
			for _, g := range oq.Groups {
				for _, item := range g.Items {
					if item.Status != ItemStatusCompleted && item.Status != ItemStatusFailed {
						if _, exists := em065Queued[item.BeadID]; !exists {
							em065Queued[item.BeadID] = fmt.Sprintf("queue %q", oq.Name)
						}
					}
				}
			}
		}

		for _, id := range allBeadIDs {
			if source, dup := em065Queued[id]; dup {
				return []ValidationError{{
					Reason: ReasonBeadAlreadyDispatched,
					Detail: map[string]any{
						"bead_id":        string(id),
						"existing_queue": source,
					},
				}}, nil, nil
			}
		}
	}

	if req.PauseChecker != nil {
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

	seen := make(map[core.BeadID]struct{})
	for _, g := range req.Groups {
		intraGroupSeen := make(map[core.BeadID]struct{})
		for _, item := range g.Items {
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
				if !blocks {
					continue
				}
				blockerStatus, bsErr := ledger.LookupStatus(ctx, a)
				if bsErr != nil {
					return nil, nil, fmt.Errorf("QM-025 ledger status %q: %w", a, bsErr)
				}
				if blockerStatus != BeadStatusOpen && blockerStatus != BeadStatusInProgress {
					continue // blocker already resolved — b does not need deferral
				}
				notices = append(notices, LedgerDepPair{
					BeadID:        b,
					BlockerBeadID: a,
					GroupIndex:    groupIndex,
				})
			}
		}
	}

	return nil, notices, nil
}

func buildProposedQueue(req ValidationRequest) Queue {
	if !req.IsAppend {
		proposed := NewActiveQueue(Queue{
			SchemaVersion: 1,
			QueueID:       "00000000-0000-0000-0000-000000000000",
			Groups:        req.Groups,
		})
		return *CloneQueue(&proposed)
	}
	if req.ActiveQueue == nil {
		return Queue{}
	}
	proposed := CloneQueue(req.ActiveQueue)
	if req.AppendGroupIndex >= 0 && req.AppendGroupIndex < len(proposed.Groups) {
		g := proposed.Groups[req.AppendGroupIndex]
		for _, ng := range req.Groups {
			g.Items = append(g.Items, cloneQueueGroup(ng).Items...)
		}
		proposed.Groups[req.AppendGroupIndex] = g
	}
	return *proposed
}
