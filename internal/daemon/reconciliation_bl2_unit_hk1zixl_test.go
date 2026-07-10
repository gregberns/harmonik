package daemon_test

// reconciliation_bl2_unit_hk1zixl_test.go — unit tests for the Cat-BL2 reactive
// bead-ledger import-failure handler (CatBL2Handler, §8.BL2).
//
// The handler subscribes to bead_sync_failed events and, for each one, retries
// `br sync --import-only` once via the configured BrPath binary:
//   - retry SUCCESS → emits bead_ledger_recovered{run_id, timestamp}
//   - retry FAILURE → emits bead_ledger_corrupt{run_id, error, timestamp} +
//                     operator_escalation_required{reason=cat_6b_auto_escalated}
//
// Seams used (no real br / git):
//   - BrPath points at a mock `br` shell script that exits 0 (success path) or
//     non-zero (failure path) — exactly the beadSyncCallWriteMockBr idiom from
//     mergetomain_beadsynccall_hkzgt4u_test.go.
//   - Emitter is a recording fake (bl2RecordingEmitter) that captures emitted
//     event types AND payload bytes so each path asserts on the concrete event.
//
// The handler's bead_sync_failed consumer is asynchronous, so the test drives
// it through an in-memory bus (Subscribe → Seal → Emit → Drain): Drain blocks
// until the async dispatch completes, making the assertion deterministic.
//
// Each test fails if the corresponding emission were removed: the success path
// asserts bead_ledger_recovered is present (and corrupt/escalation absent); the
// failure path asserts bead_ledger_corrupt AND operator_escalation_required are
// present (and recovered absent).
//
// Spec ref: specs/reconciliation/spec.md §8.BL2.
// Bead ref: hk-1zixl.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// bl2RecordingEmitter is the output seam for CatBL2Handler: it records the
// (eventType, payload) of every Emit call for assertion.
type bl2RecordingEmitter struct {
	mu       sync.Mutex
	types    []core.EventType
	payloads map[core.EventType][]byte
}

func newBL2RecordingEmitter() *bl2RecordingEmitter {
	return &bl2RecordingEmitter{payloads: make(map[core.EventType][]byte)}
}

func (e *bl2RecordingEmitter) Emit(_ context.Context, t core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.types = append(e.types, t)
	// Keep a copy — the caller may reuse the slice.
	cp := make([]byte, len(payload))
	copy(cp, payload)
	e.payloads[t] = cp
	return nil
}

func (e *bl2RecordingEmitter) has(t core.EventType) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ev := range e.types {
		if ev == t {
			return true
		}
	}
	return false
}

func (e *bl2RecordingEmitter) payload(t core.EventType) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.payloads[t]
}

// bl2WriteMockBr writes a mock `br` shell script that exits with exitCode.
// On a non-zero exit it also writes a recognizable error line to stderr so the
// failure path can assert the corrupt payload's error field is populated.
//
// Mirrors beadSyncCallWriteMockBr (mergetomain_beadsynccall_hkzgt4u_test.go).
func bl2WriteMockBr(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "br")
	var script string
	if exitCode == 0 {
		script = "#!/bin/sh\nexit 0\n"
	} else {
		script = fmt.Sprintf("#!/bin/sh\necho 'br sync: import failed — ledger corrupt' >&2\nexit %d\n", exitCode)
	}
	//nolint:gosec // G306: 0755 required for executable mock-br fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("bl2WriteMockBr: WriteFile: %v", err)
	}
	return scriptPath
}

// bl2DriveSyncFailed wires a CatBL2Handler with the given mock-br path onto an
// in-memory bus, then emits one bead_sync_failed event and drains. It returns
// the recording emitter for assertions.
func bl2DriveSyncFailed(t *testing.T, brPath, runID string) *bl2RecordingEmitter {
	t.Helper()

	emitter := newBL2RecordingEmitter()
	handler := daemon.NewCatBL2Handler(daemon.CatBL2HandlerConfig{
		ProjectDir: t.TempDir(),
		BrPath:     brPath,
		Emitter:    emitter,
		LogWriter:  os.Stderr,
	})

	bus := eventbus.NewBusImpl()
	if err := handler.Subscribe(bus); err != nil {
		t.Fatalf("CatBL2Handler.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	failPayload := core.BeadSyncFailedPayload{
		RunID:     runID,
		Error:     "initial br sync --import-only failed",
		Timestamp: "2026-06-24T12:00:00Z",
	}
	b, err := json.Marshal(failPayload)
	if err != nil {
		t.Fatalf("marshal bead_sync_failed: %v", err)
	}
	if err := bus.Emit(context.Background(), core.EventTypeBeadSyncFailed, b); err != nil {
		t.Fatalf("bus.Emit(bead_sync_failed): %v", err)
	}

	// Drain blocks until the asynchronous Cat-BL2 consumer has finished, so the
	// emitter's records are complete by the time Drain returns.
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("bus.Drain: %v", err)
	}

	return emitter
}

// TestCatBL2Handler_RetrySucceeds_EmitsLedgerRecovered verifies that when the
// `br sync --import-only` retry succeeds (mock-br exits 0), the handler emits
// bead_ledger_recovered carrying the originating run_id, and emits NEITHER
// bead_ledger_corrupt NOR operator_escalation_required.
//
// Spec ref: specs/reconciliation/spec.md §8.BL2 (success path).
// Bead ref: hk-1zixl.
func TestCatBL2Handler_RetrySucceeds_EmitsLedgerRecovered(t *testing.T) {
	t.Parallel()

	const runID = "bl2-success-run-0001"
	brPath := bl2WriteMockBr(t, 0) // retry succeeds

	emitter := bl2DriveSyncFailed(t, brPath, runID)

	if !emitter.has(core.EventTypeBeadLedgerRecovered) {
		t.Fatalf("expected %q to be emitted on successful retry, but it was not (events: %v)",
			core.EventTypeBeadLedgerRecovered, emitter.types)
	}

	// The recovered payload must carry the originating run_id.
	var recovered core.BeadLedgerRecoveredPayload
	if err := json.Unmarshal(emitter.payload(core.EventTypeBeadLedgerRecovered), &recovered); err != nil {
		t.Fatalf("unmarshal bead_ledger_recovered payload: %v", err)
	}
	if recovered.RunID != runID {
		t.Errorf("bead_ledger_recovered run_id = %q; want %q", recovered.RunID, runID)
	}
	if recovered.Timestamp == "" {
		t.Errorf("bead_ledger_recovered timestamp is empty; want RFC3339 value")
	}

	// Negative assertions: the failure events must NOT fire on the success path.
	if emitter.has(core.EventTypeBeadLedgerCorrupt) {
		t.Errorf("bead_ledger_corrupt emitted on a successful retry; must not be")
	}
	if emitter.has(core.EventTypeOperatorEscalationRequired) {
		t.Errorf("operator_escalation_required emitted on a successful retry; must not be")
	}
	if emitter.has(core.EventTypeDecisionNeeded) {
		t.Errorf("decision_needed (operator-mailbox) emitted on a successful retry; must not be")
	}
}

// TestCatBL2Handler_RetryFails_EmitsCorruptAndEscalation verifies that when the
// `br sync --import-only` retry fails persistently (mock-br exits non-zero), the
// handler emits bead_ledger_corrupt (carrying run_id + a non-empty error) AND
// operator_escalation_required (reason=cat_6b_auto_escalated), and does NOT
// emit bead_ledger_recovered.
//
// Spec ref: specs/reconciliation/spec.md §8.BL2 (persistent-failure path).
// Bead ref: hk-1zixl.
func TestCatBL2Handler_RetryFails_EmitsCorruptAndEscalation(t *testing.T) {
	t.Parallel()

	const runID = "bl2-failure-run-0001"
	brPath := bl2WriteMockBr(t, 1) // retry fails

	emitter := bl2DriveSyncFailed(t, brPath, runID)

	if !emitter.has(core.EventTypeBeadLedgerCorrupt) {
		t.Fatalf("expected %q to be emitted on persistent retry failure, but it was not (events: %v)",
			core.EventTypeBeadLedgerCorrupt, emitter.types)
	}
	if !emitter.has(core.EventTypeOperatorEscalationRequired) {
		t.Fatalf("expected %q to be emitted on persistent retry failure, but it was not (events: %v)",
			core.EventTypeOperatorEscalationRequired, emitter.types)
	}

	// hk-u4dv4: the escalation must also land in the operator-mailbox
	// projection via a decision_needed event on the reserved topic.
	if !emitter.has(core.EventTypeDecisionNeeded) {
		t.Fatalf("expected %q (operator-mailbox routing) to be emitted alongside the escalation, but it was not (events: %v)",
			core.EventTypeDecisionNeeded, emitter.types)
	}
	var decision core.DecisionNeededPayload
	if err := json.Unmarshal(emitter.payload(core.EventTypeDecisionNeeded), &decision); err != nil {
		t.Fatalf("unmarshal decision_needed payload: %v", err)
	}
	if decision.Topic != core.DecisionTopicOperatorMailbox {
		t.Errorf("decision_needed topic = %q; want %q", decision.Topic, core.DecisionTopicOperatorMailbox)
	}

	// The corrupt payload must carry the originating run_id and a non-empty error.
	var corrupt core.BeadLedgerCorruptPayload
	if err := json.Unmarshal(emitter.payload(core.EventTypeBeadLedgerCorrupt), &corrupt); err != nil {
		t.Fatalf("unmarshal bead_ledger_corrupt payload: %v", err)
	}
	if corrupt.RunID != runID {
		t.Errorf("bead_ledger_corrupt run_id = %q; want %q", corrupt.RunID, runID)
	}
	if corrupt.Error == "" {
		t.Errorf("bead_ledger_corrupt error is empty; want the failing br sync output")
	}

	// The escalation must carry the Cat 6b auto-escalated reason.
	var escalate core.OperatorEscalationRequiredPayload
	if err := json.Unmarshal(emitter.payload(core.EventTypeOperatorEscalationRequired), &escalate); err != nil {
		t.Fatalf("unmarshal operator_escalation_required payload: %v", err)
	}
	if escalate.Reason != core.OperatorEscalationReasonCat6bAutoEscalated {
		t.Errorf("operator_escalation_required reason = %q; want %q",
			escalate.Reason, core.OperatorEscalationReasonCat6bAutoEscalated)
	}

	// Negative assertion: recovery must NOT fire on the failure path.
	if emitter.has(core.EventTypeBeadLedgerRecovered) {
		t.Errorf("bead_ledger_recovered emitted on a failed retry; must not be")
	}
}
