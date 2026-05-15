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
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
)

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
) (QueueSubmitResponse, *Queue, []LedgerDepPair, *RPCError) {
	// Run the validation pipeline.
	vreq := ValidationRequest{
		Groups:      req.Groups,
		ActiveQueue: nil, // caller should pass active queue; for now no-queue path
		IsAppend:    false,
	}
	// Note: the caller is responsible for loading the active queue and passing it
	// in via a wrapper; here we use projectDir to load it ourselves per QM-027.
	existing, loadErr := Load(ctx, projectDir)
	if loadErr != nil {
		return QueueSubmitResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}
	vreq.ActiveQueue = existing

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
		SubmittedAt:   now,
		Groups:        groups,
		Status:        QueueStatusActive,
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
	q, loadErr := Load(ctx, projectDir)
	if loadErr != nil {
		return QueueAppendResponse{}, nil, nil, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}
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

	// Identity guard: reject if queue_id does not match.
	if q.QueueID != req.QueueID {
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

	mutated, events, appendErr := AppendItems(ctx, q, req.GroupIndex, beadIDStrs, ledger)
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
// Loads and returns the current queue envelope from .harmonik/queue.json.
// Returns {queue: null} when no queue is loaded (file absent or completed and
// unlinked per QM-003). MUST NOT mutate state or emit events per QM-057.
//
// Spec ref: specs/queue-model.md §8.8 QM-057, §2.10.
// Bead ref: hk-nomxl.
func HandleQueueStatus(
	ctx context.Context,
	projectDir string,
) (QueueStatusResponse, *RPCError) {
	q, loadErr := Load(ctx, projectDir)
	if loadErr != nil {
		return QueueStatusResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}
	// q is nil when no queue is loaded; QueueStatusResponse.Queue is *Queue so
	// a nil value marshals to JSON null per §2.10.
	return QueueStatusResponse{Queue: q}, nil
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
	// Load the active queue for QM-027 check (single-active-queue).
	existing, loadErr := Load(ctx, projectDir)
	if loadErr != nil {
		return QueueDryRunResponse{}, &RPCError{
			Code:    -32099,
			Message: "internal_error",
			Detail:  map[string]any{"error": loadErr.Error()},
		}
	}

	vreq := ValidationRequest{
		Groups:      req.Groups,
		ActiveQueue: existing,
		IsAppend:    false,
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
// Spec ref: specs/queue-model.md §2.10; specs/process-lifecycle.md §4.4 PL-003a.
// Bead ref: hk-nomxl.
type HandlerAdapter struct {
	ledger     BeadLedger
	projectDir string
}

// NewHandlerAdapter returns a *HandlerAdapter wired to ledger and projectDir.
func NewHandlerAdapter(ledger BeadLedger, projectDir string) *HandlerAdapter {
	return &HandlerAdapter{ledger: ledger, projectDir: projectDir}
}

// HandleQueueSubmit decodes the raw request, calls HandleQueueSubmit, and
// encodes the response. Satisfies daemon.QueueHandler.
func (a *HandlerAdapter) HandleQueueSubmit(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueSubmitRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-submit request: %v", err)}}
	}
	resp, _, _, rpcErr := HandleQueueSubmit(ctx, req, a.ledger, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-submit response: %v", err)}}
	}
	return data, nil
}

// HandleQueueAppend decodes the raw request, calls HandleQueueAppend, and
// encodes the response. Satisfies daemon.QueueHandler.
func (a *HandlerAdapter) HandleQueueAppend(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueAppendRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-append request: %v", err)}}
	}
	resp, _, _, rpcErr := HandleQueueAppend(ctx, req, a.ledger, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-append response: %v", err)}}
	}
	return data, nil
}

// HandleQueueStatus calls HandleQueueStatus and encodes the response.
// Satisfies daemon.QueueHandler.
func (a *HandlerAdapter) HandleQueueStatus(ctx context.Context) (json.RawMessage, *RPCError) {
	resp, rpcErr := HandleQueueStatus(ctx, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-status response: %v", err)}}
	}
	return data, nil
}

// HandleQueueDryRun decodes the raw request, calls HandleQueueDryRun, and
// encodes the response. Satisfies daemon.QueueHandler.
func (a *HandlerAdapter) HandleQueueDryRun(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueDryRunRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-dry-run request: %v", err)}}
	}
	resp, rpcErr := HandleQueueDryRun(ctx, req, a.ledger, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-dry-run response: %v", err)}}
	}
	return data, nil
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
