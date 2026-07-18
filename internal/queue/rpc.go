package queue

// rpc.go — JSON-RPC method handlers for the four queue control-surface methods.
//
// Each handler is a pure function: it receives the parsed request and the
// in-memory queue state, runs the appropriate pipeline, and returns a typed
// response plus an optional *RPCError. The daemon's socket dispatcher (in
// internal/daemon/socket.go) owns I/O, context propagation, and event emission.
//
// Handlers implemented here:
//   - HandleQueueSubmit   — validate → mint queue_id (UUIDv7) → Persist → QM-050
//   - HandleQueueAppend   — AppendItems → map ValidationError → response
//   - HandleQueueStatus   — Load + return envelope snapshot (QM-057)
//   - HandleQueueDryRun   — Validate ONLY; no Persist, no event emission (QM-028)
//
// Spec refs:
//   - specs/queue-model.md §2.10 (request/response RECORD shapes)
//   - specs/queue-model.md §6    (validation pipeline, QM-020..QM-029b)
//   - specs/queue-model.md §8.1  (QM-050 submit sequence)
//   - specs/process-lifecycle.md §4.4 PL-003a (method-set)
//
// Bead ref: hk-nomxl.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// QueueSetter — minimal interface so HandlerAdapter can update the daemon's
// in-memory QueueStore without importing internal/daemon (cycle prevention).
// ---------------------------------------------------------------------------

// QueueSetter is the write side of the daemon's QueueStore. HandlerAdapter
// calls SetQueue after every persist so the running workloop sees the updated
// queue without requiring a daemon restart.
//
// daemon.QueueStore satisfies this interface.
//
// ClearQueueByName removes the named slot from the in-memory store. It is
// used by HandleQueueCancel to reap a cancelled queue from the live
// QueueStore so the hk-a11re cross-queue dedup guard in the daemon's work
// loop stops treating its (now-archived) Dispatched item as a live conflict
// (hk-0mmy4).
//
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer).
// Bead ref: hk-4ukkq, hk-0mmy4.
type QueueSetter interface {
	SetQueue(q *Queue)
	ClearQueueByName(name string)
}

// LockedQueueView is a write-locked view of the daemon's QueueStore, matching
// daemon.LockedQueueStore. While the view is live (until Done), no other
// writer can mutate the store — the QM-060/QM-064 read-then-write
// serialisation surface.
//
// Bead ref: B1 (queue.json two-writer lost-update).
type LockedQueueView interface {
	LockedQueueByName(name string) *Queue
	LockedSetQueueByName(name string, q *Queue)
	LockedAllQueueNames() []string
	Done()
}

// MutationLocker is optionally implemented by the QueueSetter passed to
// NewHandlerAdapter (daemon.QueueStore implements it via LockForMutationView).
// When present, the append adapter routes its ENTIRE read-modify-write through
// the queue mutation lock instead of doing an unlocked disk Load — closing the
// two-writer lost-update race between queue-append and the workloop's
// LockForMutation status mutations (B1).
//
// Wake signals the workloop after a locked mutation (LockedSetQueueByName
// does not wake by itself).
type MutationLocker interface {
	LockForMutationView() LockedQueueView
	Wake()
}

// EventEmitter is the minimal bus interface required by HandlerAdapter to emit
// queue lifecycle events after persistence. It matches handlercontract.EventEmitter
// so that the daemon can pass the bus directly without an adapter.
//
// Bead ref: hk-peucr.
type EventEmitter interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// ---------------------------------------------------------------------------
// RPCError — typed JSON-RPC error for queue operations
// ---------------------------------------------------------------------------

// RPCError is the JSON-RPC-shaped error returned by queue method handlers when
// a request fails validation or encounters a queue-level rejection. The Code
// field carries one of the -32010..-32019 error codes defined in errors.go per
// QM-029b; the Message field carries the reason string per the PL-003b
// <error_type>{...} convention.
//
// Spec ref: specs/process-lifecycle.md §4.4 PL-003a; specs/queue-model.md §6.11a QM-029b.
type RPCError struct {
	// Code is the JSON-RPC error code from the -32010..-32019 range per QM-029b.
	Code int

	// Message is the reason string (e.g. "queue_already_active").
	Message string

	// Detail carries rule-specific context fields (bead_id, actual_status, etc.)
	// for callers that need machine-readable error context.
	Detail map[string]any
}

// Error implements the error interface. The string form is human-readable and
// not a wire-level contract.
func (e *RPCError) Error() string {
	return fmt.Sprintf("queue RPC error %d: %s (%v)", e.Code, e.Message, e.Detail)
}

// rpcErrorFromValidation converts a ValidationError into an RPCError using
// the QM-029b code mapping in JSONRPCError (errors.go).
func rpcErrorFromValidation(ve ValidationError) *RPCError {
	code, message := JSONRPCError(ve.Reason)
	return &RPCError{
		Code:    code,
		Message: message,
		Detail:  ve.Detail,
	}
}

// ---------------------------------------------------------------------------
// HandleQueueSubmit
// ---------------------------------------------------------------------------

// HandleQueueSubmit handles a queue-submit JSON-RPC request.
//
// Pipeline per specs/queue-model.md §8.1 QM-050:
//  1. Run validation pipeline per §6 (QM-020..QM-027, QM-029a order).
//  2. Mint queue_id (UUIDv7 per QM-010).
//  3. Build in-memory Queue envelope with status=active, all groups pending,
//     submitted_at and created_at stamped at accept time.
//  4. Call Persist (QM-001 atomic write).
//  5. Return QueueSubmitResponse; also returns the QM-025 LedgerDepPairs so
//     the caller can build QueueSubmittedPayload and emit events after persistence.
//
// Event emission and group_index-0 active transition (QM-050 steps 5-8) are
// the caller's responsibility per QM-063 (persist-before-emit discipline).
//
// Returns a non-nil *RPCError when the request fails validation or Persist fails.
// On Persist failure the RPCError carries code -32099 (internal error); the
// caller MUST treat ErrPersistFailed as a signal to degrade per PL-010.
//
// Spec ref: specs/queue-model.md §8.1 QM-050, §2.10, §6.
// Bead ref: hk-nomxl.
func HandleQueueSubmit(
	ctx context.Context,
	req QueueSubmitRequest,
	ledger BeadLedger,
	projectDir string,
	globalMaxConcurrent int,
) (QueueSubmitResponse, *Queue, []LedgerDepPair, *RPCError) {
	// Normalise the queue name for the per-name single-active guard and for
	// the QM-002/2.1 name-validity pre-check inside Validate.
	queueName := NormaliseQueueName(req.Name)

	// PI-070: A Pi queue MUST carry an explicit Workers cap — fail loud if absent.
	// Omitting Workers causes DefaultWorkers to inherit global max_concurrent and
	// multiply the Pi request rate. Checked before the validation pipeline so the
	// operator gets a clear, actionable error at submit time.
	if req.DefaultHarness == core.AgentTypePi && req.Workers <= 0 {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32602,
			Message: "pi_queue_missing_workers_cap",
			Detail: map[string]any{
				"error": "a Pi queue (default_harness=pi) MUST set an explicit workers cap; " +
					"omitting it silently inherits global max_concurrent and multiplies the Pi request rate (PI-070)",
				"field": "workers",
			},
		}
	}

	// Run the validation pipeline.
	vreq := ValidationRequest{
		Groups:    req.Groups,
		QueueName: queueName,
		IsAppend:  false,
	}
	// Note: the caller is responsible for loading the active queue and passing it
	// in via a wrapper; here we use projectDir to load it ourselves per QM-027.
	// Load by the normalised request name to enforce the per-name single-active guard.
	existing, loadErr := Load(ctx, projectDir, queueName)
	if loadErr != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}
	vreq.ActiveQueue = existing

	// Load other active queues for the EM-065 cross-queue double-queue guard.
	// Spec ref: specs/execution-model.md §4.14 EM-065. Bead ref: hk-xizhl.
	otherQueues, oqErr := loadOtherQueues(ctx, projectDir, queueName)
	if oqErr != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": oqErr.Error()},
		}
	}
	vreq.OtherQueues = otherQueues

	verrs, deferredPairs, err := Validate(ctx, vreq, ledger)
	if err != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": err.Error()},
		}
	}
	if len(verrs) > 0 {
		return QueueSubmitResponse{}, nil, nil, rpcErrorFromValidation(verrs[0])
	}

	// WG-045 (security): validate launch template params at the ingestion boundary.
	// Params arrive over the queue-submit RPC and are settable by any local agent;
	// they MAY carry external data. Reject malformed keys, control characters
	// (NUL/newline/tab — the highest-leverage injection primitives), and over-length
	// values BEFORE persist, so a poison value never reaches the substitution path.
	// (queue-append carries no template_params, so submit is the sole chokepoint.)
	for _, g := range req.Groups {
		for _, item := range g.Items {
			if vErr := core.ValidateTemplateParams(item.TemplateParams); vErr != nil {
				return QueueSubmitResponse{}, nil, nil, &RPCError{
					Code:    -32602, // JSON-RPC Invalid params
					Message: "invalid_template_param",
					Detail:  map[string]any{"error": vErr.Error(), "bead_id": item.BeadID},
				}
			}
		}
	}

	// Mint queue_id per QM-010.
	queueUUID, err := uuid.NewV7()
	if err != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": fmt.Sprintf("uuid.NewV7: %v", err)},
		}
	}
	queueID := queueUUID.String()
	now := time.Now().UTC()

	// Build the in-memory Queue envelope per QM-050: all groups start pending,
	// group_index 0 transitions active is deferred to caller for event ordering.
	groups := make([]Group, len(req.Groups))
	for i, g := range req.Groups {
		// Normalise submitted items: daemon-minted fields reset per §2.10.
		items := make([]Item, len(g.Items))
		for j, item := range g.Items {
			items[j] = Item{
				BeadID:     item.BeadID,
				Status:     ItemStatusPending,
				RunID:      nil,
				AppendedAt: nil,
				// Carry per-item workflow fields verbatim so the persisted queue
				// (what the workloop reads after SetQueue) retains them. Dropping
				// these here meant a per-item workflow_ref/workflow_mode never
				// reached the run, silently falling back to the embedded
				// standard-bead.dot single-reviewer workflow (hk-u6zp).
				WorkflowMode:   item.WorkflowMode,
				WorkflowRef:    item.WorkflowRef,
				Context:        item.Context,
				TemplateParams: item.TemplateParams,
			}
		}
		// Apply QM-025 deferred status to items that have an open blocker.
		deferredSet := buildDeferredSet(deferredPairs, i)
		for j := range items {
			if _, deferred := deferredSet[items[j].BeadID]; deferred {
				items[j].Status = ItemStatusDeferredForLedgerDep
			}
		}
		groups[i] = Group{
			GroupIndex:  i,
			Kind:        g.Kind,
			Status:      GroupStatusPending,
			Items:       items,
			CreatedAt:   now,
			StartedAt:   nil,
			CompletedAt: nil,
		}
	}

	q := &Queue{
		SchemaVersion: schemaVersion,
		QueueID:       queueID,
		// Name is the durable routing key; already normalised above (QM-002/2.1)
		// so the per-name slot, persistence path, and per-queue worker pool all
		// agree on the same key (NQ-A1 / NQ-B1).
		Name: queueName,
		// Workers is the per-queue dispatch ceiling (QM-066). Default a zero/absent
		// request to the global --max-concurrent; honour a positive request
		// verbatim (oversubscription permitted — the runtime global ceiling still
		// wins per QM-062). The caller (HandlerAdapter) logs oversubscription once.
		Workers: DefaultWorkers(req.Workers, globalMaxConcurrent),
		// SpendCapUSD is the OPTIONAL per-queue daily spend ceiling (NQ-X1).
		// Carried verbatim from the request: zero/absent means no per-queue cap
		// (only the global DaemonSpendMeter applies); a positive value pauses ONLY
		// this queue (paused-by-budget) when its attributed daily spend reaches the
		// cap. A cap above the global ceiling is permitted (the global ceiling still
		// wins) and logged once at submit (HandlerAdapter), mirroring Workers.
		SpendCapUSD: req.SpendCapUSD,
		// DefaultHarness is the per-queue harness-selection default (tier 2 of the
		// resolveHarness precedence walk; hk-4x3rg [C4/T6]). A valid requested
		// AgentType is carried verbatim; an invalid/empty value is normalised to
		// empty (treated as absent). Dispatch-time wiring into resolveHarness is
		// C5/T12 (hk-xhawy) — out of scope here.
		DefaultHarness: NormaliseDefaultHarness(req.DefaultHarness),
		SubmittedAt:    now,
		Groups:         groups,
		Status:         QueueStatusActive,
	}

	// Persist per QM-001 (QM-063: persist before events).
	if persistErr := Persist(ctx, projectDir, q); persistErr != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": persistErr.Error()},
		}
	}

	resp := QueueSubmitResponse{
		QueueID:    queueID,
		Status:     QueueStatusActive,
		GroupCount: len(req.Groups),
	}
	return resp, q, deferredPairs, nil
}

// ---------------------------------------------------------------------------
// HandleQueueAppend
// ---------------------------------------------------------------------------

// HandleQueueAppend handles a queue-append JSON-RPC request.
//
// Loads the active queue, validates the queue_id identity guard, then
// delegates to AppendItems (append.go) which runs the full append-path
// validation (QM-024, QM-020..QM-026, QM-025 informational) and mutates
// the queue in memory.
//
// Persist and event emission are the caller's responsibility per QM-063.
// AppendItems returns the mutated *Queue and the ordered event slice; the
// caller must Persist before emitting those events.
//
// Returns (QueueAppendResponse, mutatedQueue, events, nil) on success.
// Returns (zero, nil, nil, *RPCError) on any validation or identity failure.
//
// Spec ref: specs/queue-model.md §7, §2.10.
// Bead ref: hk-nomxl.
func HandleQueueAppend(
	ctx context.Context,
	req QueueAppendRequest,
	ledger BeadLedger,
	projectDir string,
) (QueueAppendResponse, *Queue, []core.Event, *RPCError) {
	q, rpcErr := resolveAppendTargetFromDisk(ctx, req, projectDir)
	if rpcErr != nil {
		return QueueAppendResponse{}, nil, nil, rpcErr
	}
	return HandleQueueAppendOnQueue(ctx, req, ledger, projectDir, q)
}

// resolveAppendTargetFromDisk resolves the append target queue by loading it
// from disk (bead ref: hk-1k5as):
//  1. Name given → load by name (append-by-name, hk-tigaf.8).
//  2. Name absent, QueueID given → enumerate all queues and find by UUID
//     so that --queue-id alone works for non-main queues.
//  3. Both absent → default to "main".
//
// NOTE (B1): callers holding the queue mutation lock must NOT use this —
// resolve against the LOCKED in-memory store instead (see
// HandlerAdapter.handleQueueAppendLocked) so the read-modify-write is
// serialised against concurrent status mutations.
func resolveAppendTargetFromDisk(
	ctx context.Context,
	req QueueAppendRequest,
	projectDir string,
) (*Queue, *RPCError) {
	var q *Queue
	switch {
	case req.Name != "":
		loadedQ, loadErr := Load(ctx, projectDir, NormaliseQueueName(req.Name))
		if loadErr != nil {
			return nil, &RPCError{
				Code:    -32099,
				Message: "internal_error",
				Detail:  map[string]any{"error": loadErr.Error()},
			}
		}
		q = loadedQ

	case req.QueueID != "":
		foundQ, rpcErr := findQueueByID(ctx, projectDir, req.QueueID)
		if rpcErr != nil {
			return nil, rpcErr
		}
		q = foundQ

	default:
		loadedQ, loadErr := Load(ctx, projectDir, QueueNameMain)
		if loadErr != nil {
			return nil, &RPCError{
				Code:    -32099,
				Message: "internal_error",
				Detail:  map[string]any{"error": loadErr.Error()},
			}
		}
		q = loadedQ
	}

	return q, nil
}

// HandleQueueAppendOnQueue runs the append pipeline on a PRE-LOADED queue q.
// It performs NO disk Load of the target queue — the caller owns resolution.
// The locked adapter path (B1) resolves q from the write-locked in-memory
// QueueStore so the whole read-modify-write is serialised under the queue
// mutation lock; the legacy path resolves from disk via
// resolveAppendTargetFromDisk.
//
// Semantics otherwise identical to HandleQueueAppend (identity guard, EM-065
// cross-queue guard, AppendItems, tail-index computation). Persist and event
// emission remain the caller's responsibility per QM-063.
//
// Bead ref: hk-nomxl, B1.
func HandleQueueAppendOnQueue(
	ctx context.Context,
	req QueueAppendRequest,
	ledger BeadLedger,
	projectDir string,
	q *Queue,
) (QueueAppendResponse, *Queue, []core.Event, *RPCError) {
	if q == nil {
		return QueueAppendResponse{}, nil, nil, &RPCError{
			Code:    ErrorCodeAppendTargetInvalid,
			Message: "append_target_invalid",
			Detail: map[string]any{
				"group_index":   req.GroupIndex,
				"actual_kind":   nil,
				"actual_status": nil,
			},
		}
	}

	// Identity guard: when QueueID is supplied AND Name is also supplied,
	// reject on mismatch (hk-tigaf.8). When resolved by UUID above, q.QueueID
	// already equals req.QueueID by construction — no re-check needed.
	if req.QueueID != "" && req.Name != "" && q.QueueID != req.QueueID {
		return QueueAppendResponse{}, nil, nil, &RPCError{
			Code:    ErrorCodeAppendTargetInvalid,
			Message: "append_target_invalid",
			Detail: map[string]any{
				"reason":          "queue_id_mismatch",
				"requested_queue": req.QueueID,
				"active_queue":    q.QueueID,
			},
		}
	}

	// Convert []core.BeadID → []string for AppendItems.
	beadIDStrs := make([]string, len(req.BeadIDs))
	for i, id := range req.BeadIDs {
		beadIDStrs[i] = string(id)
	}

	// Load other active queues for the EM-065 cross-queue double-queue guard.
	// Spec ref: specs/execution-model.md §4.14 EM-065. Bead ref: hk-xizhl.
	otherQueues, oqErr := loadOtherQueues(ctx, projectDir, NormaliseQueueName(q.Name))
	if oqErr != nil {
		return QueueAppendResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": oqErr.Error()},
		}
	}

	mutated, events, appendErr := AppendItems(ctx, q, req.GroupIndex, beadIDStrs, ledger, otherQueues...)
	if appendErr != nil {
		if IsValidationError(appendErr) {
			ve := appendErr.(*ValidationError)
			return QueueAppendResponse{}, nil, nil, rpcErrorFromValidation(*ve)
		}
		return QueueAppendResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": appendErr.Error()},
		}
	}

	// Compute newTailIndices: indices of appended items within the target group.
	targetGroup := mutated.Groups[req.GroupIndex]
	appendedCount := len(req.BeadIDs)
	tailStart := len(targetGroup.Items) - appendedCount
	newTailIndices := make([]int, appendedCount)
	for i := range newTailIndices {
		newTailIndices[i] = tailStart + i
	}

	resp := QueueAppendResponse{
		AppendedCount:  appendedCount,
		NewTailIndices: newTailIndices,
	}
	return resp, mutated, events, nil
}

// ---------------------------------------------------------------------------
// HandleQueueStatus
// ---------------------------------------------------------------------------

// HandleQueueStatus handles a queue-status JSON-RPC request.
//
// Loads and returns the current queue envelope.
//
// Resolution order (bead ref: hk-1k5as):
//  1. When req.Name is non-empty: load by that name.
//  2. When req.Name is empty and req.QueueID is non-empty: enumerate all
//     active queues and return the one whose QueueID matches; returns
//     {queue: null} when no match is found.
//  3. When both are absent: load the default "main" queue (QM-057 backward
//     compatibility).
//
// Returns {queue: null} when no queue is loaded (file absent or completed and
// unlinked per QM-003). MUST NOT mutate state or emit events per QM-057.
//
// Spec ref: specs/queue-model.md §8.8 QM-057, §2.10.
// Bead ref: hk-nomxl, hk-1k5as.
func HandleQueueStatus(
	ctx context.Context,
	projectDir string,
	req QueueStatusRequest,
) (QueueStatusResponse, *RPCError) {
	switch {
	case req.Name != "":
		// Name-based lookup.
		q, loadErr := Load(ctx, projectDir, NormaliseQueueName(req.Name))
		if loadErr != nil {
			return QueueStatusResponse{}, &RPCError{
				Code:    -32099,
				Message: "internal_error",
				Detail:  map[string]any{"error": loadErr.Error()},
			}
		}
		return QueueStatusResponse{Queue: q}, nil

	case req.QueueID != "":
		// UUID-based lookup: enumerate all queues and find the matching one.
		q, rpcErr := findQueueByID(ctx, projectDir, req.QueueID)
		if rpcErr != nil {
			return QueueStatusResponse{}, rpcErr
		}
		return QueueStatusResponse{Queue: q}, nil

	default:
		// Backward-compatible default: load the "main" queue.
		q, loadErr := Load(ctx, projectDir, QueueNameMain)
		if loadErr != nil {
			return QueueStatusResponse{}, &RPCError{
				Code:    -32099,
				Message: "internal_error",
				Detail:  map[string]any{"error": loadErr.Error()},
			}
		}
		return QueueStatusResponse{Queue: q}, nil
	}
}

// ---------------------------------------------------------------------------
// HandleQueueDryRun
// ---------------------------------------------------------------------------

// HandleQueueDryRun handles a queue-dry-run JSON-RPC request.
//
// Runs the full validation pipeline per §6 WITHOUT calling Persist and WITHOUT
// emitting any events (per QM-028 and §6.11). Returns the would-be Queue
// envelope and any QM-025 parallelism-narrowed notices on success.
//
// On validation failure, returns the same typed RPCError as HandleQueueSubmit
// would return per §2.10 / §6.11a.
//
// Spec ref: specs/queue-model.md §6, §2.10, QM-028.
// Bead ref: hk-nomxl.
func HandleQueueDryRun(
	ctx context.Context,
	req QueueDryRunRequest,
	ledger BeadLedger,
	projectDir string,
) (QueueDryRunResponse, *RPCError) {
	// Normalise the queue name so the per-name single-active guard (QM-027)
	// is evaluated against the correct per-name slot, not always "main".
	queueName := NormaliseQueueName(req.Name)

	// Load the active queue for QM-027 check (single-active-queue per name).
	existing, loadErr := Load(ctx, projectDir, queueName)
	if loadErr != nil {
		return QueueDryRunResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}

	// Load other active queues for the EM-065 cross-queue double-queue guard.
	// Spec ref: specs/execution-model.md §4.14 EM-065. Bead ref: hk-xizhl.
	otherQueues, oqErr := loadOtherQueues(ctx, projectDir, queueName)
	if oqErr != nil {
		return QueueDryRunResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": oqErr.Error()},
		}
	}

	vreq := ValidationRequest{
		Groups:      req.Groups,
		ActiveQueue: existing,
		QueueName:   queueName,
		IsAppend:    false,
		OtherQueues: otherQueues,
	}

	verrs, deferredPairs, err := Validate(ctx, vreq, ledger)
	if err != nil {
		return QueueDryRunResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": err.Error()},
		}
	}
	if len(verrs) > 0 {
		return QueueDryRunResponse{}, rpcErrorFromValidation(verrs[0])
	}

	// Build the resolved Queue as it would exist post-submit (per §2.10).
	// No Persist, no events per QM-028.
	now := time.Now().UTC()
	groups := make([]Group, len(req.Groups))
	for i, g := range req.Groups {
		items := make([]Item, len(g.Items))
		deferredSet := buildDeferredSet(deferredPairs, i)
		for j, item := range g.Items {
			status := ItemStatusPending
			if _, deferred := deferredSet[item.BeadID]; deferred {
				status = ItemStatusDeferredForLedgerDep
			}
			items[j] = Item{
				BeadID:     item.BeadID,
				Status:     status,
				RunID:      nil,
				AppendedAt: nil,
			}
		}
		groups[i] = Group{
			GroupIndex:  i,
			Kind:        g.Kind,
			Status:      GroupStatusPending,
			Items:       items,
			CreatedAt:   now,
			StartedAt:   nil,
			CompletedAt: nil,
		}
	}

	// Use a placeholder queue_id for the dry-run resolved envelope (per §2.10:
	// "would-be Queue envelope as it would exist post-submit"). queue_id is
	// daemon-minted at accept time so the dry-run uses a well-formed zero UUID.
	resolvedQueue := Queue{
		SchemaVersion: schemaVersion,
		QueueID:       "00000000-0000-0000-0000-000000000000",
		Name:          queueName,
		SubmittedAt:   now,
		Groups:        groups,
		Status:        QueueStatusActive,
	}

	// Build LedgerDepNotices from LedgerDepPairs.
	var notices []LedgerDepNotice
	for _, p := range deferredPairs {
		notices = append(notices, LedgerDepNotice{
			BeadID:        p.BeadID,
			BlockerBeadID: p.BlockerBeadID,
		})
	}

	return QueueDryRunResponse{
		ResolvedQueue:       resolvedQueue,
		LedgerDepNotices:    notices,
		ParallelismNarrowed: len(notices) > 0,
	}, nil
}

// ---------------------------------------------------------------------------
// findQueueByID — UUID-based queue resolution shared by status + append
// ---------------------------------------------------------------------------

// findQueueByID enumerates all active queues under projectDir and returns the
// first one whose QueueID equals queueID.  Returns (nil, nil) when no match is
// found (callers should treat this as {queue: null}).  Returns a non-nil
// *RPCError only on I/O failure.
//
// Bead ref: hk-1k5as.
func findQueueByID(ctx context.Context, projectDir, queueID string) (*Queue, *RPCError) {
	names, err := EnumerateQueueNames(projectDir)
	if err != nil {
		return nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": err.Error()},
		}
	}
	for _, name := range names {
		q, loadErr := Load(ctx, projectDir, name)
		if loadErr != nil || q == nil {
			continue
		}
		if q.QueueID == queueID {
			return q, nil
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// HandleQueueList
// ---------------------------------------------------------------------------

// HandleQueueList handles a queue-list JSON-RPC request.
//
// Enumerates all queue files under .harmonik/queues/ and returns a summary
// for each: name, queue_id, status, and item counts by status (pending,
// workers/dispatched, completed, failed).
//
// Returns an empty Queues slice (not nil) when no queue files are present.
// Does not modify state or emit events.
//
// Bead ref: hk-tigaf.8.
func HandleQueueList(
	ctx context.Context,
	projectDir string,
) (QueueListResponse, *RPCError) {
	names, err := EnumerateQueueNames(projectDir)
	if err != nil {
		return QueueListResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": err.Error()},
		}
	}

	summaries := make([]QueueSummary, 0, len(names))
	for _, name := range names {
		q, loadErr := Load(ctx, projectDir, name)
		if loadErr != nil || q == nil {
			continue
		}
		s := QueueSummary{
			Name:    q.Name,
			QueueID: q.QueueID,
			Status:  q.Status,
		}
		if s.Name == "" {
			s.Name = NormaliseQueueName(name)
		}
		for _, g := range q.Groups {
			for _, item := range g.Items {
				switch item.Status {
				case ItemStatusPending, ItemStatusDeferredForLedgerDep:
					s.PendingItems++
				case ItemStatusDispatched:
					s.Workers++
				case ItemStatusCompleted:
					s.CompletedItems++
				case ItemStatusFailed:
					s.FailedItems++
				}
			}
		}
		summaries = append(summaries, s)
	}

	return QueueListResponse{Queues: summaries}, nil
}

// ---------------------------------------------------------------------------
// HandlerAdapter — concrete QueueHandler implementation for daemon wiring
// ---------------------------------------------------------------------------

// HandlerAdapter wraps the four HandleQueue* functions and satisfies the
// daemon.QueueHandler interface. It decodes raw JSON params from the socket
// transport, delegates to the appropriate handler function, and encodes the
// response back to JSON.
//
// Create one via NewHandlerAdapter and pass it to daemon.RunSocketListener as
// the qh variadic argument.
//
// After HandleQueueSubmit and HandleQueueAppend persist to disk, the adapter
// calls qs.SetQueue so the running workloop picks up the updated queue without
// requiring a restart (hk-4ukkq / hk-lzs8r). When qs is nil the SetQueue call
// is skipped (unit-test / legacy-caller compat).
//
// Spec ref: specs/queue-model.md §2.10; specs/process-lifecycle.md §4.4 PL-003a.
// Bead ref: hk-nomxl, hk-4ukkq, hk-lzs8r, hk-peucr.
type HandlerAdapter struct {
	ledger     BeadLedger
	projectDir string
	qs         QueueSetter
	bus        EventEmitter

	// globalMaxConcurrent is the daemon-wide --max-concurrent ceiling. Used to
	// default Queue.Workers when a submit omits it (QM-066) and to detect
	// oversubscription (Workers > global cap) for the one-time warning. Zero is
	// treated as 1, matching the work-loop's effectiveMax floor.
	//
	// Bead ref: hk-tigaf.4 (NQ-B1).
	globalMaxConcurrent int

	// concurrencyGet reads the live dispatch ceiling from the daemon's
	// ConcurrencyController (hk-ohiaf). Used by HandleQueueStatus and
	// HandleQueueList to surface the current effective ceiling. Nil when the
	// controller was not wired (unit-test / legacy callers); falls back to
	// globalMaxConcurrent.
	concurrencyGet func() int

	// concurrencySet updates the live dispatch ceiling (hk-ohiaf). Nil when the
	// controller was not wired; HandleQueueSetConcurrency returns -32099 in that
	// case.
	concurrencySet func(n int) (old int, err error)

	// spawnCapGet returns the substrate's non-terminal session ceiling
	// (hk-vfeeo). HandleQueueSetConcurrency uses it to detect a request that
	// would oversubscribe the spawn cap. Nil when no cap is configured or the
	// substrate does not expose SpawnCapSize.
	spawnCapGet func() int

	// spawnCapSet live-resizes the substrate's spawn cap (hk-omvan, follow-up
	// to hk-vfeeo). When wired, HandleQueueSetConcurrency RAISES the cap to
	// satisfy an oversubscribing request instead of refusing it. Nil when the
	// substrate does not support live resize (WithSpawnCap was not passed, or
	// the substrate predates SetSpawnCap) — HandleQueueSetConcurrency falls
	// back to the hk-vfeeo refuse-with-detail behaviour in that case.
	spawnCapSet func(n int)

	// workerToggle flips the named worker's enabled state in the daemon's LIVE
	// worker registry (hk-xjbvi). It returns the resolved worker name on success
	// or an error (unknown name / no worker configured). Nil when the daemon did
	// not wire a worker registry (no .harmonik/workers.yaml); HandleWorkerSetEnabled
	// returns -32099 in that case.
	workerToggle func(name string, enabled bool) (string, error)
}

// SetGlobalMaxConcurrent records the daemon-wide --max-concurrent ceiling so
// the adapter can default Queue.Workers (QM-066) and warn on oversubscription.
// Called once by the composition root after construction (daemon.Start). When
// unset (zero) the default resolves to 1, matching the work-loop floor.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func (a *HandlerAdapter) SetGlobalMaxConcurrent(n int) {
	a.globalMaxConcurrent = n
}

// SetConcurrencyFuncs wires the live-ceiling getter and setter from the
// daemon's ConcurrencyController (hk-ohiaf). Called by daemon.Start after
// the controller is created. When not called the adapter falls back to
// globalMaxConcurrent for reads and returns an error for writes.
//
// Bead ref: hk-ohiaf.
func (a *HandlerAdapter) SetConcurrencyFuncs(get func() int, set func(int) (int, error)) {
	a.concurrencyGet = get
	a.concurrencySet = set
}

// SetSpawnCapFunc wires the spawn-cap reader from the substrate so that
// HandleQueueSetConcurrency can refuse requests that would oversubscribe the
// hardware session ceiling (hk-vfeeo). fn returns the non-terminal spawn cap
// (cap(nonTerminalSem)); 0 means uncapped. Called by daemon.Start when the
// substrate implements substrateWithSpawnCap.
//
// Bead ref: hk-vfeeo.
func (a *HandlerAdapter) SetSpawnCapFunc(fn func() int) {
	a.spawnCapGet = fn
}

// SetSpawnCapSetFunc wires the live spawn-cap resize setter from the substrate
// (hk-omvan, follow-up to hk-vfeeo). fn resizes the non-terminal spawn cap to
// n (a no-op for n <= 0). Called by daemon.Start when the substrate implements
// substrateWithSpawnCapSetter. When not called, HandleQueueSetConcurrency
// falls back to refusing an oversubscribing request (the hk-vfeeo behaviour).
//
// Bead ref: hk-omvan.
func (a *HandlerAdapter) SetSpawnCapSetFunc(fn func(n int)) {
	a.spawnCapSet = fn
}

// SetWorkerToggleFunc wires the live worker enable/disable setter from the
// daemon's worker registry (hk-xjbvi). fn flips the named worker's Enabled flag
// and returns the resolved worker name, or an error for an unknown name / no
// worker configured. Called by daemon.Start once the worker registry exists.
// When not called, HandleWorkerSetEnabled returns -32099 (no worker registry).
//
// Bead ref: hk-xjbvi.
func (a *HandlerAdapter) SetWorkerToggleFunc(fn func(name string, enabled bool) (string, error)) {
	a.workerToggle = fn
}

// DefaultWorkers resolves the effective per-queue worker count for a queue
// whose Workers field is requested as `requested`, given the daemon's global
// --max-concurrent ceiling `globalCap` (QM-066). A zero/negative request
// defaults to the global cap; a positive request is honoured verbatim (so
// oversubscription, requested > globalCap, is permitted — the runtime global
// ceiling still wins per QM-062). globalCap is floored at 1 to mirror the
// work-loop's effectiveMax.
//
// Bead ref: hk-tigaf.4 (NQ-B1).
func DefaultWorkers(requested, globalCap int) int {
	if globalCap < 1 {
		globalCap = 1
	}
	if requested <= 0 {
		return globalCap
	}
	return requested
}

// NormaliseDefaultHarness resolves the effective per-queue harness default
// from a requested value. A value that satisfies core.AgentType.Valid()
// (AR-025) is returned verbatim so it can serve as resolveHarness's tier-2
// queueDefault; any invalid value (including the empty string) is normalised
// to the empty AgentType, which the harness resolver treats as "absent" and
// falls through to the node/global tiers. This mirrors the silent-ignore
// posture of the other daemon-minted submit fields (the request carries an
// intent; the daemon owns the persisted truth).
//
// Bead ref: hk-4x3rg [C4/T6].
func NormaliseDefaultHarness(requested core.AgentType) core.AgentType {
	if requested.Valid() {
		return requested
	}
	return core.AgentType("")
}

// NewHandlerAdapter returns a *HandlerAdapter wired to ledger and projectDir.
//
// qs is optional: when non-nil, the adapter calls qs.SetQueue after each
// successful persist so the daemon workloop sees the new queue immediately
// (hk-4ukkq / hk-lzs8r). Pass nil to skip the in-memory update (unit tests
// that do not instantiate a QueueStore).
//
// bus is optional: when non-nil, the adapter emits queue lifecycle events
// (queue_submitted, queue_appended, queue_item_deferred_for_ledger_dep) after
// each persist (hk-peucr). Pass nil to suppress events.
func NewHandlerAdapter(ledger BeadLedger, projectDir string, qs QueueSetter, bus EventEmitter) *HandlerAdapter {
	return &HandlerAdapter{ledger: ledger, projectDir: projectDir, qs: qs, bus: bus}
}

// HandleQueueSubmit decodes the raw request, calls HandleQueueSubmit, and
// encodes the response. Satisfies daemon.QueueHandler.
//
// After a successful persist, the adapter calls a.qs.SetQueue so the running
// workloop picks up the new queue without a restart (hk-4ukkq). Then emits
// queue_submitted + any queue_item_deferred_for_ledger_dep events (hk-peucr).
func (a *HandlerAdapter) HandleQueueSubmit(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueSubmitRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-submit request: %v", err)},
		}
	}

	// H6 (two-writer lost-update / single-active TOCTOU fix): serialise the whole
	// submit read-modify-write — the disk Load, the QM-027 single-active check,
	// and the Persist all happen inside the pure HandleQueueSubmit, plus the
	// in-memory write-back below — under the SAME queue mutation lock B1 uses for
	// append and the workloop uses for status mutations. Without it two concurrent
	// submits for the same new queue name can both pass the single-active check
	// and both Persist to <name>.json (last-writer-wins drops one). Only test
	// harnesses wiring a nil/plain QueueSetter fall through unlocked.
	locker, hasLock := a.qs.(MutationLocker)
	var lv LockedQueueView
	if hasLock {
		lv = locker.LockForMutationView()
	}

	resp, q, ledgerDepPairs, rpcErr := HandleQueueSubmit(ctx, req, a.ledger, a.projectDir, a.globalMaxConcurrent)
	if rpcErr != nil {
		if hasLock {
			lv.Done()
		}
		return nil, rpcErr
	}

	// Thread the persisted queue into the running workloop (hk-4ukkq). Under the
	// mutation lock we MUST write back through the LOCKED view — NOT a.qs.SetQueue,
	// which re-acquires the non-reentrant queueMu and would self-deadlock (the same
	// trap B1's appendUnderLock avoids via lv.LockedSetQueueByName). Wake the
	// workloop, then release the lock before the (non-mutating) emits below.
	if q != nil {
		if hasLock {
			lv.LockedSetQueueByName(NormaliseQueueName(q.Name), q)
			locker.Wake()
		} else if a.qs != nil {
			a.qs.SetQueue(q)
		}
	}
	if hasLock {
		lv.Done()
	}

	// QM-066 oversubscription warning: a per-queue Workers count above the global
	// --max-concurrent is permitted (the runtime global ceiling still wins per
	// QM-062) but is logged ONCE here at submit so operators notice the queue can
	// never reach its requested width. Emitted to stderr (the daemon's diagnostic
	// channel); not an error.
	if q != nil && a.globalMaxConcurrent >= 1 && q.Workers > a.globalMaxConcurrent {
		fmt.Fprintf(os.Stderr,
			"daemon: queue-submit: queue %q workers=%d oversubscribes global --max-concurrent=%d; global ceiling still applies (QM-062/QM-066)\n",
			q.Name, q.Workers, a.globalMaxConcurrent)
	}

	// Emit queue_submitted event (hk-peucr). The queue has already been
	// persisted inside HandleQueueSubmit so QM-063 (persist-before-emit) is
	// satisfied.
	if a.bus != nil && q != nil {
		totalBeads := 0
		for _, g := range q.Groups {
			totalBeads += len(g.Items)
		}
		payload := core.QueueSubmittedPayload{
			QueueID:            q.QueueID,
			SubmittedAt:        q.SubmittedAt.Format(time.RFC3339),
			GroupCount:         len(q.Groups),
			TotalBeadCount:     totalBeads,
			QueueSchemaVersion: q.SchemaVersion,
		}
		if raw, err := json.Marshal(payload); err == nil {
			_ = a.bus.Emit(ctx, core.EventTypeQueueSubmitted, raw)
		}

		// Emit queue_item_deferred_for_ledger_dep for QM-025 deferred items.
		detectedAt := q.SubmittedAt.Format(time.RFC3339)
		for _, pair := range ledgerDepPairs {
			deferPayload := core.QueueItemDeferredForLedgerDepPayload{
				QueueID:       q.QueueID,
				GroupIndex:    pair.GroupIndex,
				BeadID:        string(pair.BeadID),
				BlockerBeadID: string(pair.BlockerBeadID),
				DetectedAt:    detectedAt,
			}
			if raw, err := json.Marshal(deferPayload); err == nil {
				_ = a.bus.Emit(ctx, core.EventTypeQueueItemDeferredForLedgerDep, raw)
			}
		}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-submit response: %v", err)},
		}
	}
	return data, nil
}

// appendUnderLock performs the whole queue-append read-modify-write under the
// queue mutation lock (B1: two-writer lost-update fix):
//
//  1. LockForMutationView — same lock as the workloop's LockForMutation.
//  2. Resolve the target queue from the LIVE locked in-memory store (NOT a
//     fresh disk Load, which would race concurrent status mutations). Falls
//     back to disk only when the queue is not in memory (e.g. adapter wired
//     to a fresh store) — still safe because every writer persists under
//     this same lock.
//  3. AppendItems mutates the locked queue in place (only after validation
//     passes — a rejected append leaves the store untouched).
//  4. Persist WHILE holding the lock (QM-063 persist-before-emit), write back
//     via the locked view, release, then Wake the workloop.
//
// Events are returned for the caller to emit AFTER the lock is released.
func (a *HandlerAdapter) appendUnderLock(
	ctx context.Context,
	req QueueAppendRequest,
	locker MutationLocker,
) (QueueAppendResponse, []core.Event, *RPCError) {
	lv := locker.LockForMutationView()
	defer lv.Done()

	// Resolve from the locked in-memory store (hk-1k5as resolution order).
	var q *Queue
	switch {
	case req.Name != "":
		q = lv.LockedQueueByName(NormaliseQueueName(req.Name))
	case req.QueueID != "":
		for _, name := range lv.LockedAllQueueNames() {
			if cand := lv.LockedQueueByName(name); cand != nil && cand.QueueID == req.QueueID {
				q = cand
				break
			}
		}
	default:
		q = lv.LockedQueueByName(QueueNameMain)
	}

	// Disk fallback: queue persisted but not (yet) in the in-memory store.
	if q == nil {
		diskQ, rpcErr := resolveAppendTargetFromDisk(ctx, req, a.projectDir)
		if rpcErr != nil {
			return QueueAppendResponse{}, nil, rpcErr
		}
		q = diskQ
	}

	resp, mutated, events, rpcErr := HandleQueueAppendOnQueue(ctx, req, a.ledger, a.projectDir, q)
	if rpcErr != nil {
		return QueueAppendResponse{}, nil, rpcErr
	}

	if mutated != nil {
		// QM-063: persist BEFORE emitting; still under the mutation lock so a
		// concurrent status-mutation cannot interleave and clobber this write.
		if persistErr := Persist(ctx, a.projectDir, mutated); persistErr != nil {
			return QueueAppendResponse{}, nil, &RPCError{
				Code: -32099, Message: "internal_error",
				Detail: map[string]any{"error": fmt.Sprintf("persist queue after append: %v", persistErr)},
			}
		}
		lv.LockedSetQueueByName(NormaliseQueueName(mutated.Name), mutated)
	}

	// Wake AFTER the write-back; the deferred Done releases the lock when this
	// function returns, and Wake's buffered non-blocking send is safe to fire
	// while still holding it (it only touches wakeC).
	locker.Wake()
	return resp, events, nil
}

// HandleQueueAppend decodes the raw request, runs the append pipeline, and
// encodes the response. Satisfies daemon.QueueHandler.
//
// When a.qs implements MutationLocker (daemon.QueueStore does), the entire
// read-modify-write runs under the queue mutation lock via appendUnderLock
// (B1). Otherwise the legacy unlocked disk-Load path is used. Either way the
// adapter persists the mutated queue, updates the in-memory QueueStore so the
// running workloop sees the appended items without a restart (hk-lzs8r), and
// then emits queue_appended plus any queue_item_deferred_for_ledger_dep
// events returned by AppendItems (hk-peucr).
func (a *HandlerAdapter) HandleQueueAppend(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueAppendRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-append request: %v", err)},
		}
	}
	// B1 (two-writer lost-update fix): when the QueueSetter also implements
	// MutationLocker (daemon.QueueStore does), the ENTIRE
	// read-modify-write — resolve the live queue, AppendItems, Persist, write
	// back — runs under the queue mutation lock, serialised against the
	// workloop's LockForMutation status mutations. Only test harnesses that
	// pass a nil/plain QueueSetter fall through to the legacy unlocked path.
	locker, hasLock := a.qs.(MutationLocker)

	var resp QueueAppendResponse
	var events []core.Event
	var rpcErr *RPCError
	if hasLock {
		resp, events, rpcErr = a.appendUnderLock(ctx, req, locker)
		if rpcErr != nil {
			return nil, rpcErr
		}
	} else {
		var mutated *Queue
		resp, mutated, events, rpcErr = HandleQueueAppend(ctx, req, a.ledger, a.projectDir)
		if rpcErr != nil {
			return nil, rpcErr
		}

		// Persist the mutated queue (QM-063: persist before emit) and update the
		// in-memory QueueStore so the workloop sees the appended items (hk-lzs8r).
		if mutated != nil {
			if persistErr := Persist(ctx, a.projectDir, mutated); persistErr != nil {
				return nil, &RPCError{
					Code: -32099, Message: "internal_error",
					Detail: map[string]any{"error": fmt.Sprintf("persist queue after append: %v", persistErr)},
				}
			}
			if a.qs != nil {
				a.qs.SetQueue(mutated)
			}
		}
	}

	// Emit append events returned by AppendItems (hk-peucr).
	if a.bus != nil {
		for _, evt := range events {
			raw, err := json.Marshal(evt.Payload)
			if err != nil {
				raw = evt.Payload
			}
			_ = a.bus.Emit(ctx, core.EventType(evt.Type), raw)
		}
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-append response: %v", err)},
		}
	}
	return data, nil
}

// HandleQueueStatus decodes the optional request, calls HandleQueueStatus, and
// encodes the response.  Satisfies daemon.QueueHandler.
//
// When params is nil or empty the request defaults to the zero QueueStatusRequest
// (backward-compatible: returns the "main" queue per QM-057).
func (a *HandlerAdapter) HandleQueueStatus(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueStatusRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &RPCError{
				Code: -32099, Message: "internal_error",
				Detail: map[string]any{"error": fmt.Sprintf("decode queue-status request: %v", err)},
			}
		}
	}
	resp, rpcErr := HandleQueueStatus(ctx, a.projectDir, req)
	if rpcErr != nil {
		return nil, rpcErr
	}
	// Surface the current effective ceiling (hk-ohiaf).
	if a.concurrencyGet != nil {
		resp.MaxConcurrent = a.concurrencyGet()
	} else {
		resp.MaxConcurrent = a.globalMaxConcurrent
		if resp.MaxConcurrent < 1 {
			resp.MaxConcurrent = 1
		}
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-status response: %v", err)},
		}
	}
	return data, nil
}

// HandleQueueDryRun decodes the raw request, calls HandleQueueDryRun, and
// encodes the response. Satisfies daemon.QueueHandler.
func (a *HandlerAdapter) HandleQueueDryRun(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueDryRunRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-dry-run request: %v", err)},
		}
	}
	resp, rpcErr := HandleQueueDryRun(ctx, req, a.ledger, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-dry-run response: %v", err)},
		}
	}
	return data, nil
}

// HandleQueueList calls HandleQueueList and encodes the response.
// Satisfies daemon.QueueHandler.
//
// Bead ref: hk-tigaf.8.
func (a *HandlerAdapter) HandleQueueList(ctx context.Context) (json.RawMessage, *RPCError) {
	resp, rpcErr := HandleQueueList(ctx, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	// Surface the current effective ceiling (hk-ohiaf).
	if a.concurrencyGet != nil {
		resp.MaxConcurrent = a.concurrencyGet()
	} else {
		resp.MaxConcurrent = a.globalMaxConcurrent
		if resp.MaxConcurrent < 1 {
			resp.MaxConcurrent = 1
		}
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-list response: %v", err)},
		}
	}
	return data, nil
}

// HandleQueueSetConcurrency updates the daemon's runtime dispatch ceiling.
// Satisfies daemon.QueueHandler.
//
// Decodes the N field from params, validates N >= 1, calls the wired setter,
// and returns the old and new ceiling values. Returns -32099 when the setter
// is not wired (daemon started without a ConcurrencyController).
//
// hk-omvan: when N would oversubscribe the substrate's spawn cap and the
// substrate supports a live resize, this also raises the spawn cap to
// max(currentCap, N*2) so the request succeeds — see the spawnCapSet wiring
// comment below. Otherwise falls back to the hk-vfeeo refuse-with-detail
// behaviour.
//
// Bead ref: hk-ohiaf, hk-vfeeo, hk-omvan.
func (a *HandlerAdapter) HandleQueueSetConcurrency(_ context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueSetConcurrencyRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-set-concurrency request: %v", err)},
		}
	}
	if a.concurrencySet == nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": "concurrency controller not wired; daemon may not support set-concurrency"},
		}
	}
	// hk-vfeeo / hk-omvan: each LOCAL in-flight bead occupies 2 non-terminal
	// sessions (implementer + reviewer), so the safe local dispatch ceiling is
	// spawnCap/2. Remote runs (hk-hs7ex) spawn tmux on the WORKER, not locally,
	// so they do not consume the local spawnSem — this guard protects the
	// local sub-cap only; remote slots are not counted here.
	//
	// hk-omvan: when the substrate supports a live resize (spawnCapSet wired),
	// an oversubscribing request RAISES the cap to max(currentCap, N*2) instead
	// of being refused — the operator's set-concurrency knob now scales real
	// throughput with no daemon restart. When the substrate predates live
	// resize (spawnCapSet nil), fall back to the hk-vfeeo refuse-with-detail
	// behaviour: the cap stays fixed at daemon startup (--max-concurrent × 2)
	// and raising it requires a restart with a higher value.
	spawnCap := 0
	if a.spawnCapGet != nil {
		spawnCap = a.spawnCapGet()
	}
	if spawnCap > 0 && req.N*2 > spawnCap {
		if a.spawnCapSet != nil {
			a.spawnCapSet(req.N * 2)
			spawnCap = req.N * 2
		} else {
			safeMax := spawnCap / 2
			return nil, &RPCError{
				Code: -32099, Message: "spawn_cap_exceeded",
				Detail: map[string]any{
					"error":     fmt.Sprintf("set-concurrency %d would oversubscribe the local spawn cap: each LOCAL bead needs 2 sessions, cap = %d non-terminal slots (safe local max_concurrent = %d); restart with --max-concurrent %d or HARMONIK_MAX_CONCURRENT_SESSIONS=%d to raise the cap; remote worker runs are not subject to this limit", req.N, spawnCap, safeMax, req.N, req.N*2),
					"requested": req.N,
					"spawn_cap": spawnCap,
					"safe_max":  safeMax,
				},
			}
		}
	}

	oldN, err := a.concurrencySet(req.N)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "invalid_concurrency",
			Detail: map[string]any{"error": err.Error()},
		}
	}
	resp := QueueSetConcurrencyResponse{OldN: oldN, NewN: req.N, SpawnCap: spawnCap}
	data, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-set-concurrency response: %v", marshalErr)},
		}
	}
	return data, nil
}

// HandleQueueCancel cancels the named queue on a live daemon: it archives the
// per-queue file to <name>.json.failed-<timestamp> (the same disk contract as
// the daemon-less `harmonik queue cancel` CLI path in
// internal/queue/cli/cancel.go) and then reaps the daemon's in-memory
// QueueStore slot via QueueSetter.ClearQueueByName.
//
// The reap step is the fix for hk-0mmy4: without it, a live daemon's
// in-memory copy of the cancelled queue keeps Status=active with its
// dispatched item's Status left at ItemStatusDispatched even after the file
// is archived on disk, so the hk-a11re cross-queue dedup guard in the work
// loop (workloop.go) keeps failing any fresh dispatch of the same bead from
// another queue with LastFailureReason="cross_queue_duplicate" — the queue
// looks cancelled on disk but the daemon never learns to let the bead go.
//
// Satisfies daemon.QueueHandler.
//
// Bead ref: hk-0mmy4.
func (a *HandlerAdapter) HandleQueueCancel(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueCancelRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-cancel request: %v", err)},
		}
	}
	name := NormaliseQueueName(req.Queue)

	q, loadErr := Load(ctx, a.projectDir, name)
	if loadErr != nil && !errors.Is(loadErr, ErrCorrupt) {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("cannot read queue file: %v", loadErr)},
		}
	}
	if q == nil {
		// Absent or an unparseable corrupt stub: nothing to archive, but still
		// reap any stale in-memory slot under this name so it can't wedge the
		// cross-queue dedup guard either (hk-0mmy4).
		if a.qs != nil {
			a.qs.ClearQueueByName(name)
		}
		data, marshalErr := json.Marshal(QueueCancelResponse{})
		if marshalErr != nil {
			return nil, &RPCError{
				Code: -32099, Message: "internal_error",
				Detail: map[string]any{"error": fmt.Sprintf("encode queue-cancel response: %v", marshalErr)},
			}
		}
		return data, nil
	}
	if q.Status == QueueStatusCompleted && !req.Force {
		return nil, &RPCError{
			Code: -32099, Message: "queue_already_completed",
			Detail: map[string]any{"error": fmt.Sprintf("queue %s is already completed; use --force to archive anyway", q.QueueID)},
		}
	}

	priorStatus := string(q.Status)
	if _, archErr := ArchiveFailedQueue(ctx, a.projectDir, name, time.Now()); archErr != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("cannot archive queue.json: %v", archErr)},
		}
	}
	if a.qs != nil {
		a.qs.ClearQueueByName(name)
	}

	data, marshalErr := json.Marshal(QueueCancelResponse{QueueID: q.QueueID, PriorStatus: priorStatus})
	if marshalErr != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-cancel response: %v", marshalErr)},
		}
	}
	return data, nil
}

// HandleWorkerSetEnabled flips the named worker's enabled state in the daemon's
// LIVE worker registry — the operator-facing `harmonik worker enable/disable`
// toggle (hk-xjbvi). Satisfies daemon.QueueHandler.
//
// Decodes {name, enabled} from params and calls the wired toggle func, which
// reaches registry.SetEnabledByName on the live registry. Mirrors
// HandleQueueSetConcurrency: returns -32099 when the toggle is not wired (daemon
// started without a worker registry / no .harmonik/workers.yaml), and a typed
// error when the name is unknown. On success a `worker enable` makes the worker
// selectable on the next dispatch tick with no restart.
//
// Bead ref: hk-xjbvi.
func (a *HandlerAdapter) HandleWorkerSetEnabled(_ context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req WorkerSetEnabledRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode worker-set-enabled request: %v", err)},
		}
	}
	if req.Name == "" {
		return nil, &RPCError{
			Code: -32099, Message: "invalid_worker",
			Detail: map[string]any{"error": "worker name is required"},
		}
	}
	if a.workerToggle == nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": "no remote worker registry wired; daemon has no .harmonik/workers.yaml configured"},
		}
	}
	name, err := a.workerToggle(req.Name, req.Enabled)
	if err != nil {
		return nil, &RPCError{
			Code: -32099, Message: "invalid_worker",
			Detail: map[string]any{"error": err.Error()},
		}
	}
	resp := WorkerSetEnabledResponse{Name: name, Enabled: req.Enabled}
	data, marshalErr := json.Marshal(resp)
	if marshalErr != nil {
		return nil, &RPCError{
			Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode worker-set-enabled response: %v", marshalErr)},
		}
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// loadOtherQueues — EM-065 cross-queue helper
// ---------------------------------------------------------------------------

// loadOtherQueues returns all active queues under projectDir whose name
// differs from excludeName. Used by the EM-065 cross-queue double-queue guard
// (specs/execution-model.md §4.14) in HandleQueueSubmit, HandleQueueAppend,
// and HandleQueueDryRun to populate ValidationRequest.OtherQueues.
//
// Returns nil when no other queues exist (empty queues dir or only the
// excluded name is present). Individual per-queue load failures (e.g., a
// corrupt json file for a different queue) are silently skipped: the EM-065
// guard is a best-effort pre-flight; the Beads atomic claim (BI-009) is the
// final barrier. The returned error covers only directory-level I/O failures
// (EnumerateQueueNames failure).
//
// Bead ref: hk-xizhl.
func loadOtherQueues(ctx context.Context, projectDir, excludeName string) ([]*Queue, error) {
	names, err := EnumerateQueueNames(projectDir)
	if err != nil {
		return nil, fmt.Errorf("loadOtherQueues: enumerate: %w", err)
	}
	var others []*Queue
	for _, name := range names {
		if name == excludeName {
			continue
		}
		q, loadErr := Load(ctx, projectDir, name)
		if loadErr != nil || q == nil {
			continue
		}
		others = append(others, q)
	}
	return others, nil
}

// ---------------------------------------------------------------------------
// buildDeferredSet — shared helper for submit and dry-run
// ---------------------------------------------------------------------------

// buildDeferredSet returns a map of beadID → blockerBeadID for all QM-025
// notices that apply to group groupIndex. Used by both HandleQueueSubmit and
// HandleQueueDryRun to apply deferred-for-ledger-dep status to items.
func buildDeferredSet(pairs []LedgerDepPair, groupIndex int) map[core.BeadID]core.BeadID {
	set := make(map[core.BeadID]core.BeadID)
	for _, p := range pairs {
		if p.GroupIndex == groupIndex {
			set[p.BeadID] = p.BlockerBeadID
		}
	}
	return set
}
