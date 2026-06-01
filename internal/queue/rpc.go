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
// Spec ref: specs/queue-model.md §9.1 QM-060 (single-writer).
// Bead ref: hk-4ukkq.
type QueueSetter interface {
	SetQueue(q *Queue)
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
	// Run the validation pipeline.
	vreq := ValidationRequest{
		Groups:      req.Groups,
		ActiveQueue: nil, // caller should pass active queue; for now no-queue path
		IsAppend:    false,
	}
	// Note: the caller is responsible for loading the active queue and passing it
	// in via a wrapper; here we use projectDir to load it ourselves per QM-027.
	// Load by the normalised request name to enforce the per-name single-active guard.
	existing, loadErr := Load(ctx, projectDir, NormaliseQueueName(req.Name))
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
		// Name is the durable routing key; normalise empty → "main" (QM-002/2.1)
		// so the per-name slot, persistence path, and per-queue worker pool all
		// agree on the same key (NQ-A1 / NQ-B1).
		Name: NormaliseQueueName(req.Name),
		// Workers is the per-queue dispatch ceiling (QM-066). Default a zero/absent
		// request to the global --max-concurrent; honour a positive request
		// verbatim (oversubscription permitted — the runtime global ceiling still
		// wins per QM-062). The caller (HandlerAdapter) logs oversubscription once.
		Workers:     DefaultWorkers(req.Workers, globalMaxConcurrent),
		SubmittedAt: now,
		Groups:      groups,
		Status:      QueueStatusActive,
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
	// Resolve queue by name when QueueID is absent (append-by-name, hk-tigaf.8).
	loadName := QueueNameMain
	if req.Name != "" {
		loadName = NormaliseQueueName(req.Name)
	}
	q, loadErr := Load(ctx, projectDir, loadName)
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

	// Identity guard: when QueueID is supplied, reject on mismatch; when absent,
	// name-resolved queue_id is accepted without an explicit guard (hk-tigaf.8).
	if req.QueueID != "" && q.QueueID != req.QueueID {
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
	q, loadErr := Load(ctx, projectDir, QueueNameMain)
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
	existing, loadErr := Load(ctx, projectDir, QueueNameMain)
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
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-submit request: %v", err)}}
	}
	resp, q, ledgerDepPairs, rpcErr := HandleQueueSubmit(ctx, req, a.ledger, a.projectDir, a.globalMaxConcurrent)
	if rpcErr != nil {
		return nil, rpcErr
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

	// Thread the persisted queue into the running workloop (hk-4ukkq).
	if a.qs != nil && q != nil {
		a.qs.SetQueue(q)
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
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-submit response: %v", err)}}
	}
	return data, nil
}

// HandleQueueAppend decodes the raw request, calls HandleQueueAppend, and
// encodes the response. Satisfies daemon.QueueHandler.
//
// After AppendItems mutates the in-memory queue, the adapter persists it and
// calls a.qs.SetQueue so the running workloop sees the appended items without
// a restart (hk-lzs8r). Then emits queue_appended and any
// queue_item_deferred_for_ledger_dep events returned by AppendItems (hk-peucr).
func (a *HandlerAdapter) HandleQueueAppend(ctx context.Context, params json.RawMessage) (json.RawMessage, *RPCError) {
	var req QueueAppendRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("decode queue-append request: %v", err)}}
	}
	resp, mutated, events, rpcErr := HandleQueueAppend(ctx, req, a.ledger, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}

	// Persist the mutated queue (QM-063: persist before emit) and update the
	// in-memory QueueStore so the workloop sees the appended items (hk-lzs8r).
	if mutated != nil {
		if persistErr := Persist(ctx, a.projectDir, mutated); persistErr != nil {
			return nil, &RPCError{Code: -32099, Message: "internal_error",
				Detail: map[string]any{"error": fmt.Sprintf("persist queue after append: %v", persistErr)}}
		}
		if a.qs != nil {
			a.qs.SetQueue(mutated)
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

// HandleQueueList calls HandleQueueList and encodes the response.
// Satisfies daemon.QueueHandler.
//
// Bead ref: hk-tigaf.8.
func (a *HandlerAdapter) HandleQueueList(ctx context.Context) (json.RawMessage, *RPCError) {
	resp, rpcErr := HandleQueueList(ctx, a.projectDir)
	if rpcErr != nil {
		return nil, rpcErr
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return nil, &RPCError{Code: -32099, Message: "internal_error",
			Detail: map[string]any{"error": fmt.Sprintf("encode queue-list response: %v", err)}}
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
