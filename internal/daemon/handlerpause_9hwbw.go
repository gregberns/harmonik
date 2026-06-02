package daemon

// handlerpause_9hwbw.go — HandlerPauseController: in-memory pause-state +
// in-flight-bead freeze-list (hk-9hwbw).
//
// HandlerPauseController is the single-writer-disciplined component that
// owns per-handler-type pause state inside the daemon.  It implements the
// queue.HandlerPauseChecker interface so the queue-submit validation path
// (hk-siuo2 / QM-052a) can gate submissions when a handler is paused.
//
// Architecture placement: internal/daemon/ (composition root).
// Rationale: the controller fans into three cross-subsystem surfaces —
// eventbus.EventBus (event emission), the RunRegistry snapshot (freeze-list),
// and queue.HandlerPauseChecker (queue-submit gate) — making the composition
// root the narrowest package that can legally see all three without introducing
// a new cycle.  A dedicated internal/handlerpause/ package would need to
// import internal/daemon for the RunRegistry, which would be a cycle.
//
// Persistence hook-point: hk-m0k0a will wire .harmonik/handler-state.json
// load/save here.  See the PERSISTENCE NOTE comments throughout this file
// for the exact seam.  At MVH state is in-memory only; daemon restart resets
// all handlers to live.
//
// Spec ref: specs/handler-pause.md §7, §8, §9.
// Event types: core.EventTypeHandlerPaused, core.EventTypeHandlerResumed (§8.11).
// Interface: queue.HandlerPauseChecker (hk-siuo2).
// Bead ref: hk-9hwbw.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queue"
)

// Verify HandlerPauseController satisfies queue.HandlerPauseChecker at compile
// time.  This is load-bearing: if the interface changes (e.g. hk-siuo2 adds a
// method) the compiler will catch the drift here.
var _ queue.HandlerPauseChecker = (*HandlerPauseController)(nil)

// ---------------------------------------------------------------------------
// InFlightBeadRecord — one entry in the freeze-list snapshot
// ---------------------------------------------------------------------------

// InFlightBeadRecord is a single entry in the in_flight_at_pause freeze-list
// captured when a handler type is paused.
//
// Per specs/handler-pause.md §9 HP-050, in-flight beads are NOT interrupted at
// pause time; the freeze-list is a snapshot for operator visibility only.
//
// Fields match the in_flight_at_pause array shape in .harmonik/handler-state.json §5.1.
type InFlightBeadRecord struct {
	// RunID is the run identifier of the in-flight run.
	// UUIDv7 string as produced by core.RunID.String().
	RunID string `json:"run_id"`

	// BeadID is the bead dispatched in this run.
	BeadID string `json:"bead_id"`

	// DispatchedAt is the wall-clock time the run was registered.
	// RFC 3339 / time.RFC3339Nano format.
	DispatchedAt string `json:"dispatched_at"`
}

// ---------------------------------------------------------------------------
// handlerEntry — internal state for one agent type
// ---------------------------------------------------------------------------

// pauseStatus is the per-handler-type pause state enum.
type pauseStatus int8

const (
	pauseStatusLive   pauseStatus = 0 // handler is live (accepting dispatch)
	pauseStatusPaused pauseStatus = 1 // handler is paused
)

// handlerEntry holds the mutable state for one agent type.
// All reads and writes MUST be performed while the controller's mu lock is held.
type handlerEntry struct {
	// status is the current pause status for this handler type.
	status pauseStatus

	// cause is the structured pause cause from the most recent Pause call.
	// nil when status == pauseStatusLive.
	cause *core.HandlerPauseCause

	// inFlightAtPause is the freeze-list snapshot taken at the most recent
	// Pause call.  Empty when status == pauseStatusLive.
	inFlightAtPause []InFlightBeadRecord

	// pausedEpoch is the monotonic pause→resume cycle counter.
	// Starts at 0 (never paused); incremented to 1 on the first Pause call,
	// to 2 on the second Pause after a Resume, and so on.
	// Matches the PausedEpoch field on HandlerPausedPayload / HandlerResumedPayload.
	pausedEpoch int
}

// ---------------------------------------------------------------------------
// HandlerPauseStatusSnapshot — operator-visible snapshot
// ---------------------------------------------------------------------------

// HandlerPauseStatusSnapshot is the point-in-time view of one handler type's
// pause state returned by HandlerPauseController.Status.
//
// Used by the `harmonik handler status` CLI (hk-39ryh) and by hk-m0k0a for
// persistence serialisation.
type HandlerPauseStatusSnapshot struct {
	// AgentType is the handler type this snapshot describes.
	AgentType core.AgentType `json:"agent_type"`

	// Paused is true when the handler is currently paused.
	Paused bool `json:"paused"`

	// Cause is the structured pause cause.  nil / absent when Paused is false.
	Cause *core.HandlerPauseCause `json:"cause,omitempty"`

	// InFlightAtPause is the freeze-list captured when the handler was last
	// paused.  Empty when Paused is false.
	InFlightAtPause []InFlightBeadRecord `json:"in_flight_at_pause,omitempty"`

	// PausedEpoch is the monotonic pause→resume counter.  0 means the handler
	// has never been paused in this daemon session.
	PausedEpoch int `json:"paused_epoch"`
}

// ---------------------------------------------------------------------------
// HandlerPauseController
// ---------------------------------------------------------------------------

// HandlerPauseController is the daemon-singleton component that tracks
// per-handler-type pause state and maintains the in-flight-bead freeze-list.
//
// # Lock discipline (HP-035)
//
// mu is a sync.RWMutex.  Write paths (Pause, Resume, SetPersistFn) acquire the
// full write lock (Lock/Unlock).  Read paths (IsPaused, PausedEpochFor) acquire
// only the read lock (RLock/RUnlock), allowing multiple concurrent dispatcher
// goroutines to check pause state without contention.  Status also uses the
// write lock because it snapshots mutable sub-slices (freeze-list, cause) whose
// copying is not safe under a concurrent write.  snapshotAllLocked and
// snapshotEntryLocked are called only while the write lock is already held.
//
// Spec ref: specs/handler-pause.md §7.2 HP-035 ("IsPaused() MUST be lock-free
// for readers via RWMutex read-lock").
//
// # Persistence seam (hk-m0k0a)
//
// At MVH the controller is in-memory only.  hk-m0k0a will inject a
// PersistFunc at construction time; Pause and Resume call it under the lock
// before emitting bus events.  See PERSISTENCE NOTE comments.
//
// # Concurrency safety
//
// All exported methods are safe for concurrent use.
type HandlerPauseController struct {
	mu sync.RWMutex

	// handlers maps agent_type → mutable state entry.
	// Entries are created on first Pause; absent entries default to live.
	handlers map[core.AgentType]*handlerEntry

	// bus is the in-process event bus used to emit handler_paused /
	// handler_resumed events.
	bus eventbus.EventBus

	// PERSISTENCE NOTE (hk-m0k0a):
	// persistFn, when non-nil, is called inside mu to persist the current
	// state to .harmonik/handler-state.json before bus events are emitted.
	// At MVH persistFn is always nil (no-op).  hk-m0k0a will inject this
	// at daemon.Start alongside the load-on-startup path.
	persistFn func(ctx context.Context, snapshots []HandlerPauseStatusSnapshot) error

	// adapter is the Adapter used to call Diagnose on pause-trip and resume
	// (HC-014a diagnostic seam).  When nil, Diagnose is skipped.
	// Injected via SetAdapter after construction; nil until wired.
	// Spec: specs/handler-contract.md §4.3a HC-014a.  Bead: hk-tvsl7.
	adapter handlercontract.Adapter
}

// NewHandlerPauseController returns a ready-to-use HandlerPauseController.
//
// bus MUST be non-nil.  It is used to emit handler_paused and handler_resumed
// events on state transitions.
//
// persistFn, when non-nil, is called inside mu on every state mutation (Pause
// and Resume) to persist the full snapshot to .harmonik/handler-state.json
// before bus events are emitted.  Pass nil to disable persistence (unit-test
// mode or when no ProjectDir is configured).
//
// Bead ref: hk-m0k0a.
func NewHandlerPauseController(bus eventbus.EventBus, persistFn func(ctx context.Context, snapshots []HandlerPauseStatusSnapshot) error) *HandlerPauseController {
	return &HandlerPauseController{
		handlers:  make(map[core.AgentType]*handlerEntry),
		bus:       bus,
		persistFn: persistFn,
	}
}

// SetPersistFn patches the controller's persist function after construction.
//
// The controller is intentionally constructed with persistFn=nil before
// bus.Seal() so HandlerPausePolicyGoroutine.Subscribe can reference it pre-Seal.
// After Seal the composition root (daemon.Start) resolves the .harmonik dir and
// calls SetPersistFn to wire in the real persist hook before LoadHandlerPauseState
// runs.
//
// Calling SetPersistFn is safe: no Pause/Resume call can have occurred yet
// because the bus is sealed but no events have been emitted at the point
// daemon.Start invokes this.  No mu lock is taken here — the assignment is
// single-writer before any bus consumers can fire.
//
// Bead ref: hk-37zy8, hk-m0k0a.
func (c *HandlerPauseController) SetPersistFn(fn func(ctx context.Context, snapshots []HandlerPauseStatusSnapshot) error) {
	c.mu.Lock()
	c.persistFn = fn
	c.mu.Unlock()
}

// SetAdapter injects the Adapter whose Diagnose method the controller calls on
// pause-trip and Resume (HC-014a diagnostic seam).
//
// Must be called before the first Pause or Resume call.  Uses mu for safety
// even though callers typically call this before any events fire.
//
// Spec: specs/handler-contract.md §4.3a HC-014a.
// Bead: hk-tvsl7.
func (c *HandlerPauseController) SetAdapter(adapter handlercontract.Adapter) {
	c.mu.Lock()
	c.adapter = adapter
	c.mu.Unlock()
}

// runDiagnose calls adapter.Diagnose (HC-014a) and returns the result.
//
// Returns (report, true) on success.  Returns (zero, false) when the adapter
// is nil, returns ErrDeterministic (not supported), or returns any other error.
// Must NOT be called while mu is held (Diagnose may block on I/O).
func (c *HandlerPauseController) runDiagnose(ctx context.Context) (handlercontract.DiagnosticReport, bool) {
	c.mu.RLock()
	adapter := c.adapter
	c.mu.RUnlock()

	if adapter == nil {
		return handlercontract.DiagnosticReport{}, false
	}
	report, err := adapter.Diagnose(ctx)
	if err != nil {
		return handlercontract.DiagnosticReport{}, false
	}
	return report, true
}

// ---------------------------------------------------------------------------
// Pause — trip the handler-type pause state
// ---------------------------------------------------------------------------

// Pause records a pause for agentType with the given cause and in-flight bead
// snapshot, then emits a handler_paused event on the bus.
//
// If agentType is already paused, Pause is a no-op (returns nil).  The
// single-writer contract (QM-060 analog) ensures Pause calls from concurrent
// goroutines serialize; if two goroutines race to trip the same handler type
// the first one wins and the second is a no-op.
//
// inFlight is the caller-supplied freeze-list snapshot: the set of runs for
// agentType that were in flight at the moment the pause was triggered.  The
// caller (daemon policy goroutine) is responsible for querying RunRegistry and
// filtering by agent type before calling Pause.  inFlight may be empty or nil.
//
// Per specs/handler-pause.md §9 HP-050, in-flight runs are NOT interrupted.
// The freeze-list is recorded for operator visibility only.
//
// The handler_paused event is emitted AFTER state mutation and (when wired)
// persistence, per the §4 event-flow ordering.
func (c *HandlerPauseController) Pause(
	ctx context.Context,
	agentType core.AgentType,
	cause core.HandlerPauseCause,
	inFlight []InFlightBeadRecord,
) error {
	if !agentType.Valid() {
		return fmt.Errorf("HandlerPauseController.Pause: invalid agent_type %q", string(agentType))
	}
	if !cause.Valid() {
		return fmt.Errorf("HandlerPauseController.Pause: invalid cause for agent_type %q", string(agentType))
	}

	// HC-014a: invoke Diagnose seam before acquiring the lock (Diagnose may
	// block on I/O; lock should not be held across I/O).  If the adapter is
	// not wired or returns ErrDeterministic, ok=false and we skip enrichment.
	if report, ok := c.runDiagnose(ctx); ok {
		cause.DiagnosticMessage = report.Message
	}

	c.mu.Lock()

	entry := c.getOrCreate(agentType)
	if entry.status == pauseStatusPaused {
		// Already paused — single-writer no-op.
		c.mu.Unlock()
		return nil
	}

	// Mutate state.
	entry.status = pauseStatusPaused
	causeCopy := cause
	entry.cause = &causeCopy
	entry.pausedEpoch++
	epoch := entry.pausedEpoch

	// Snapshot freeze-list (defensive copy so the caller's slice is not aliased).
	if len(inFlight) > 0 {
		entry.inFlightAtPause = make([]InFlightBeadRecord, len(inFlight))
		copy(entry.inFlightAtPause, inFlight)
	} else {
		entry.inFlightAtPause = nil
	}

	inFlightCount := len(entry.inFlightAtPause)

	// Persist under the lock before emitting the bus event (hk-m0k0a).
	// Rationale: writing inside the lock is the simplest safe option; the
	// controller already owns the lock and disk write latency (~ms) is
	// acceptable at the low call frequency of operator-driven pauses.
	if c.persistFn != nil {
		if persistErr := c.persistFn(ctx, c.snapshotAllLocked()); persistErr != nil {
			c.mu.Unlock()
			return fmt.Errorf("HandlerPauseController.Pause: persist: %w", persistErr)
		}
	}

	c.mu.Unlock()

	// Emit handler_paused event (outside the lock — bus.Emit may block on I/O
	// for fsync-boundary events; holding the lock across I/O would serialize
	// all pause/resume/check calls unnecessarily).
	payload := core.HandlerPausedPayload{
		AgentType:     agentType,
		Cause:         cause,
		InFlightCount: inFlightCount,
		PausedEpoch:   epoch,
	}
	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Errorf("HandlerPauseController.Pause: marshal handler_paused payload: %w", marshalErr)
	}
	if emitErr := c.bus.Emit(ctx, core.EventTypeHandlerPaused, payloadJSON); emitErr != nil {
		return fmt.Errorf("HandlerPauseController.Pause: emit handler_paused: %w", emitErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resume — clear the handler-type pause state
// ---------------------------------------------------------------------------

// Resume clears the pause for agentType and emits a handler_resumed event.
//
// If agentType is not currently paused, Resume returns ErrHandlerNotPaused.
// Callers that want idempotent behaviour (e.g. `harmonik handler resume
// --force`) should check the error type and swallow ErrHandlerNotPaused.
//
// The handler_resumed event is emitted AFTER state mutation and (when wired)
// persistence, per the §4 event-flow ordering.
//
// resumedBy identifies the initiator of the resume.  At MVH the only value is
// core.HandlerResumedByOperator.
func (c *HandlerPauseController) Resume(
	ctx context.Context,
	agentType core.AgentType,
	resumedBy core.HandlerResumedBy,
) error {
	if !agentType.Valid() {
		return fmt.Errorf("HandlerPauseController.Resume: invalid agent_type %q", string(agentType))
	}
	if !resumedBy.Valid() {
		return fmt.Errorf("HandlerPauseController.Resume: invalid resumedBy %q for agent_type %q", string(resumedBy), string(agentType))
	}

	c.mu.Lock()

	entry, exists := c.handlers[agentType]
	if !exists || entry.status != pauseStatusPaused {
		c.mu.Unlock()
		return &ErrHandlerNotPaused{AgentType: agentType}
	}

	// Capture the prior cause and epoch for the event payload.
	priorCause := *entry.cause
	epoch := entry.pausedEpoch

	// Clear state.
	entry.status = pauseStatusLive
	entry.cause = nil
	entry.inFlightAtPause = nil
	// pausedEpoch is NOT reset — it is monotonically increasing to support the
	// dispatcher's dedup contract (queue_item_held_for_handler_pause §8.11.3).

	// Persist under the lock before emitting the bus event (hk-m0k0a).
	if c.persistFn != nil {
		if persistErr := c.persistFn(ctx, c.snapshotAllLocked()); persistErr != nil {
			c.mu.Unlock()
			return fmt.Errorf("HandlerPauseController.Resume: persist: %w", persistErr)
		}
	}

	c.mu.Unlock()

	// HC-014a: invoke Diagnose on Resume to verify the triggering condition has
	// cleared.  At MVH the result is informational only; Resume proceeds
	// regardless of Healthy.  Post-MVH the controller MAY gate Resume on
	// Healthy=true (spec §4.3a HC-014a).
	_, _ = c.runDiagnose(ctx) // result is logged post-MVH; ignored at MVH

	// Emit handler_resumed event (outside the lock).
	payload := core.HandlerResumedPayload{
		AgentType:   agentType,
		By:          resumedBy,
		PriorCause:  priorCause,
		PausedEpoch: epoch,
	}
	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Errorf("HandlerPauseController.Resume: marshal handler_resumed payload: %w", marshalErr)
	}
	if emitErr := c.bus.Emit(ctx, core.EventTypeHandlerResumed, payloadJSON); emitErr != nil {
		return fmt.Errorf("HandlerPauseController.Resume: emit handler_resumed: %w", emitErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// IsPaused — query pause status
// ---------------------------------------------------------------------------

// IsPaused reports whether the handler for agentType is currently paused.
// Returns false for unknown handler types (default-live per §5.3 "absent ⇒ all live").
//
// Uses a read lock per HP-035 so concurrent dispatch-loop goroutines can call
// IsPaused simultaneously without contention on write paths (Pause/Resume).
//
// Safe for concurrent use.
func (c *HandlerPauseController) IsPaused(agentType core.AgentType) bool {
	c.mu.RLock()
	entry, exists := c.handlers[agentType]
	var paused bool
	if exists {
		paused = entry.status == pauseStatusPaused
	}
	c.mu.RUnlock()
	return paused
}

// PausedEpochFor returns (epoch, true) when agentType is currently paused, or
// (0, false) when it is live.  Both the paused flag and epoch are read under
// the same lock acquisition so callers get a consistent snapshot.
//
// Used by the dispatch loop (workloop.go) to implement the dedup contract for
// queue_item_held_for_handler_pause events: the dispatcher records
// (beadID, epoch) and emits at-most-once per pair per §8.11.3.
//
// Safe for concurrent use.
func (c *HandlerPauseController) PausedEpochFor(agentType core.AgentType) (epoch int, paused bool) {
	c.mu.RLock()
	entry, exists := c.handlers[agentType]
	if exists && entry.status == pauseStatusPaused {
		epoch = entry.pausedEpoch
		paused = true
	}
	c.mu.RUnlock()
	return epoch, paused
}

// ---------------------------------------------------------------------------
// queue.HandlerPauseChecker implementation
// ---------------------------------------------------------------------------

// ResolvedAgentType implements queue.HandlerPauseChecker.
//
// At MVH, all beads use the same agent type (claude-code).  This method
// returns core.AgentTypeClaudeCode unconditionally; a richer per-bead
// resolution (reading the bead's DOT node attribute or handler-contract
// dispatch table) is deferred to post-MVH.
//
// FUTURE: when per-bead agent-type resolution is wired, replace the body
// of this method with a lookup against the bead ledger / dispatch table.
func (c *HandlerPauseController) ResolvedAgentType(_ context.Context, _ core.BeadID) (core.AgentType, error) {
	return core.AgentTypeClaudeCode, nil
}

// IsHandlerPaused implements queue.HandlerPauseChecker.
//
// Returns false for unknown agent types (default-live).
func (c *HandlerPauseController) IsHandlerPaused(_ context.Context, agentType core.AgentType) (bool, error) {
	return c.IsPaused(agentType), nil
}

// ---------------------------------------------------------------------------
// Status — point-in-time snapshot for CLI + persistence
// ---------------------------------------------------------------------------

// Status returns a point-in-time snapshot of the pause state for all known
// handler types.
//
// When agentType is non-empty, Status returns a single-element slice for that
// handler type (or an empty slice if the type has never been mentioned to the
// controller).  When agentType is empty (""), Status returns snapshots for all
// known handler types.
//
// Used by `harmonik handler status` (hk-39ryh) and by hk-m0k0a for persistence.
func (c *HandlerPauseController) Status(agentType core.AgentType) []HandlerPauseStatusSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	if agentType != "" {
		entry, exists := c.handlers[agentType]
		if !exists {
			return nil
		}
		return []HandlerPauseStatusSnapshot{c.snapshotEntryLocked(agentType, entry)}
	}

	// All known handler types.
	out := make([]HandlerPauseStatusSnapshot, 0, len(c.handlers))
	for at, entry := range c.handlers {
		out = append(out, c.snapshotEntryLocked(at, entry))
	}
	return out
}

// snapshotEntryLocked builds a HandlerPauseStatusSnapshot from an entry.
// MUST be called while mu is held.
func (c *HandlerPauseController) snapshotEntryLocked(at core.AgentType, entry *handlerEntry) HandlerPauseStatusSnapshot {
	snap := HandlerPauseStatusSnapshot{
		AgentType:   at,
		Paused:      entry.status == pauseStatusPaused,
		PausedEpoch: entry.pausedEpoch,
	}
	if entry.cause != nil {
		causeCopy := *entry.cause
		snap.Cause = &causeCopy
	}
	if len(entry.inFlightAtPause) > 0 {
		snap.InFlightAtPause = make([]InFlightBeadRecord, len(entry.inFlightAtPause))
		copy(snap.InFlightAtPause, entry.inFlightAtPause)
	}
	return snap
}

// snapshotAllLocked returns snapshots for all known handler types.
// MUST be called while mu is held.
// Used by persistFn to capture the full state for serialisation (hk-m0k0a).
func (c *HandlerPauseController) snapshotAllLocked() []HandlerPauseStatusSnapshot {
	out := make([]HandlerPauseStatusSnapshot, 0, len(c.handlers))
	for at, entry := range c.handlers {
		out = append(out, c.snapshotEntryLocked(at, entry))
	}
	return out
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// getOrCreate returns the handlerEntry for agentType, creating one if absent.
// MUST be called while mu is held.
func (c *HandlerPauseController) getOrCreate(agentType core.AgentType) *handlerEntry {
	entry, exists := c.handlers[agentType]
	if !exists {
		entry = &handlerEntry{}
		c.handlers[agentType] = entry
	}
	return entry
}

// ---------------------------------------------------------------------------
// ErrHandlerNotPaused — typed error for Resume on a live handler
// ---------------------------------------------------------------------------

// ErrHandlerNotPaused is returned by Resume when the target handler type is
// not currently paused.
//
// The `harmonik handler resume` CLI (hk-ejyku) uses this to distinguish
// "already live" (exit 3 per §7) from other errors.
type ErrHandlerNotPaused struct {
	AgentType core.AgentType
}

// Error implements the error interface.
func (e *ErrHandlerNotPaused) Error() string {
	return fmt.Sprintf("handler %q is not currently paused", string(e.AgentType))
}

// ---------------------------------------------------------------------------
// InFlightBeadRecordFromRunHandle — helper for the caller-side freeze-list
// ---------------------------------------------------------------------------

// InFlightBeadRecordFromRunHandle builds an InFlightBeadRecord from a
// RunHandle for use in the Pause freeze-list argument.
//
// The daemon policy goroutine (hk-37zy8) calls this for each RunHandle whose
// agent type matches the handler being paused.  The agentType field is not on
// RunHandle (RunHandle is agent-type-agnostic), so the caller supplies it
// separately.
//
// runID is the RunID key under which handle was registered in RunRegistry.
func InFlightBeadRecordFromRunHandle(runID core.RunID, handle *RunHandle) InFlightBeadRecord {
	return InFlightBeadRecord{
		RunID:        runID.String(),
		BeadID:       string(handle.BeadID),
		DispatchedAt: handle.StartedAt.UTC().Format(time.RFC3339Nano),
	}
}
