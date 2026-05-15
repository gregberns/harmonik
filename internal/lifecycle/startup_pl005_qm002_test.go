package lifecycle

// startup_pl005_qm002_test.go — tests for LoadQueueAtStartup (PL-005 step 8a,
// QM-002 + QM-002a).
//
// Helper prefix: startupQueueFixture (per implementer-protocol.md §Helper-prefix
// discipline and the bead brief for hk-fwpc0).
//
// Test scenarios:
//   (a) File absent     — LoadQueueAtStartup returns (nil, nil).
//   (b) Clean load      — Valid queue.json returns non-nil *Queue, nil error.
//   (c) Corrupt file    — queue.json exists but is unparseable; returns (nil, nil)
//                         with a warning (not ErrCorrupt propagated; treated as absent).
//   (d) QM-002a cross-check — queue.json has one item at status=dispatched and
//       Beads shows that bead as open. Assert:
//         - item reverts to pending
//         - queue_item_reconciled event fires BEFORE any dispatch-loop tick log line.
//         - Event payload carries reason=claim_write_lost.
//
// Spec ref: specs/queue-model.md §3.2 QM-002, §3.2a QM-002a.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
// Bead: hk-fwpc0.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// startupQueueFixtureProjectDir creates a temporary directory that looks like
// a harmonik project root with an initialised .harmonik/ sub-directory. All
// sibling fixtures in this file reuse this helper.
func startupQueueFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	hDir := filepath.Join(dir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(hDir, 0o755); err != nil {
		t.Fatalf("startupQueueFixtureProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// startupQueueFixtureMinimalQueue builds a minimal valid Queue for use in tests.
// All fields are populated with stable, deterministic values.
func startupQueueFixtureMinimalQueue() queue.Queue {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-1111-7000-8000-000000000001",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: core.BeadID("hk-test01"),
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: now,
			},
		},
	}
}

// startupQueueFixtureDispatchedQueue builds a Queue where group 0, item 0 has
// status=dispatched with a run_id set. Used for the QM-002a scenario (d).
func startupQueueFixtureDispatchedQueue(beadID core.BeadID) queue.Queue {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	runID := "run-0001-xxxx-xxxx-xxxx-000000000001"
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-2222-7000-8000-000000000002",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: beadID,
						Status: queue.ItemStatusDispatched,
						RunID:  &runID,
					},
				},
				CreatedAt: now,
				StartedAt: &now,
			},
		},
	}
}

// startupQueueFixtureWriteCorruptFile writes invalid JSON to queue.json in
// the project's .harmonik directory, simulating an unrecoverable file.
func startupQueueFixtureWriteCorruptFile(t *testing.T, projectDir string) {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "queue.json")
	if err := os.WriteFile(path, []byte(`{{{{not valid json`), 0o600); err != nil {
		t.Fatalf("startupQueueFixtureWriteCorruptFile: WriteFile: %v", err)
	}
}

// startupQueueFixtureWriteUnsupportedSchema writes a queue.json with an
// unrecognised schema_version to trigger the QM-002 forward-incompatible path.
func startupQueueFixtureWriteUnsupportedSchema(t *testing.T, projectDir string) {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "queue.json")
	// schema_version 99 is forward-incompatible with v0.1 which only reads {1}.
	payload := `{"schema_version":99,"queue_id":"test","submitted_at":"2026-05-15T00:00:00Z","status":"active","groups":[]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("startupQueueFixtureWriteUnsupportedSchema: WriteFile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Mock BeadLedger
// ---------------------------------------------------------------------------

// startupQueueFixtureBeadLedger is a test-double BeadLedger whose per-bead
// responses are configured at construction time.
type startupQueueFixtureBeadLedger struct {
	mu        sync.Mutex
	responses map[core.BeadID]core.CoarseStatus
	errors    map[core.BeadID]error
	calls     []core.BeadID // ShowBead call log, in order
}

// newStartupQueueFixtureLedger constructs a fake BeadLedger with pre-canned
// responses. Set statusByID to control what ShowBead returns per bead ID.
// If errByID is non-nil for a bead ID, ShowBead returns that error instead.
func newStartupQueueFixtureLedger(
	statusByID map[core.BeadID]core.CoarseStatus,
	errByID map[core.BeadID]error,
) *startupQueueFixtureBeadLedger {
	if statusByID == nil {
		statusByID = make(map[core.BeadID]core.CoarseStatus)
	}
	if errByID == nil {
		errByID = make(map[core.BeadID]error)
	}
	return &startupQueueFixtureBeadLedger{
		responses: statusByID,
		errors:    errByID,
	}
}

func (l *startupQueueFixtureBeadLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, id)

	if err, ok := l.errors[id]; ok {
		return core.BeadRecord{}, err
	}
	status, ok := l.responses[id]
	if !ok {
		return core.BeadRecord{}, errors.New("startupQueueFixtureBeadLedger: unexpected ShowBead call for " + string(id))
	}
	return core.BeadRecord{
		BeadID: id,
		Status: status,
	}, nil
}

// ShowBeadCalls returns the ordered list of bead IDs passed to ShowBead.
func (l *startupQueueFixtureBeadLedger) ShowBeadCalls() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.calls))
	copy(out, l.calls)
	return out
}

// ---------------------------------------------------------------------------
// Mock QueueEventEmitter
// ---------------------------------------------------------------------------

// startupQueueFixtureEmitter is a recording QueueEventEmitter that captures
// every emitted event in order. Tests use it to assert that
// queue_item_reconciled fires before any dispatch-loop tick log line.
type startupQueueFixtureEmitter struct {
	mu     sync.Mutex
	events []startupQueueFixtureEmittedEvent
}

// startupQueueFixtureEmittedEvent records one emission.
type startupQueueFixtureEmittedEvent struct {
	EventType core.EventType
	Payload   []byte
}

func (e *startupQueueFixtureEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, startupQueueFixtureEmittedEvent{
		EventType: eventType,
		Payload:   append([]byte(nil), payload...),
	})
	return nil
}

// Events returns a snapshot of emitted events in emission order.
func (e *startupQueueFixtureEmitter) Events() []startupQueueFixtureEmittedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]startupQueueFixtureEmittedEvent, len(e.events))
	copy(out, e.events)
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestLoadQueueAtStartup_FileAbsent covers scenario (a): no queue.json.
//
// Expect: LoadQueueAtStartup returns (nil, nil); ledger is never queried.
//
// Spec ref: queue-model.md §3.2 QM-002 — "File absent → daemon starts with no
// active queue."
func TestLoadQueueAtStartup_FileAbsent(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()
	ledger := newStartupQueueFixtureLedger(nil, nil)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if err != nil {
		t.Fatalf("scenario (a): expected nil error, got %v", err)
	}
	if gotQueue != nil {
		t.Fatalf("scenario (a): expected nil Queue, got %+v", gotQueue)
	}
	if calls := ledger.ShowBeadCalls(); len(calls) != 0 {
		t.Errorf("scenario (a): expected no ShowBead calls, got %v", calls)
	}
	if evs := emitter.Events(); len(evs) != 0 {
		t.Errorf("scenario (a): expected no emitted events, got %d", len(evs))
	}
}

// TestLoadQueueAtStartup_CleanLoad covers scenario (b): valid queue.json with no
// dispatched items.
//
// Expect: LoadQueueAtStartup returns the parsed Queue with nil error; no events
// are emitted (nothing to reconcile).
//
// Spec ref: queue-model.md §3.2 QM-002 — "File exists and parses → queue loaded."
func TestLoadQueueAtStartup_CleanLoad(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	// Write a valid queue.json with one pending item.
	q := startupQueueFixtureMinimalQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	ledger := newStartupQueueFixtureLedger(nil, nil)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if err != nil {
		t.Fatalf("scenario (b): expected nil error, got %v", err)
	}
	if gotQueue == nil {
		t.Fatal("scenario (b): expected non-nil Queue, got nil")
	}
	if gotQueue.QueueID != q.QueueID {
		t.Errorf("scenario (b): QueueID got %q, want %q", gotQueue.QueueID, q.QueueID)
	}
	if calls := ledger.ShowBeadCalls(); len(calls) != 0 {
		t.Errorf("scenario (b): no dispatched items → expected no ShowBead calls, got %v", calls)
	}
	if evs := emitter.Events(); len(evs) != 0 {
		t.Errorf("scenario (b): expected no emitted events, got %d", len(evs))
	}
}

// TestLoadQueueAtStartup_CorruptFile covers scenario (c): queue.json present but
// unparseable.
//
// Expect: LoadQueueAtStartup returns (nil, nil); no error propagated to caller;
// the corrupt file is NOT deleted.
//
// Spec ref: queue-model.md §3.2 QM-002 — "File present but unparseable → treated
// as absent; file NOT auto-deleted."
func TestLoadQueueAtStartup_CorruptFile(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	startupQueueFixtureWriteCorruptFile(t, projectDir)

	ledger := newStartupQueueFixtureLedger(nil, nil)
	emitter := &startupQueueFixtureEmitter{}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if err != nil {
		t.Fatalf("scenario (c): expected nil error, got %v", err)
	}
	if gotQueue != nil {
		t.Fatalf("scenario (c): expected nil Queue, got %+v", gotQueue)
	}

	// The corrupt file MUST NOT be deleted per QM-002.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("scenario (c): corrupt queue.json was auto-deleted; per QM-002 it MUST be left for operator inspection")
	}

	// A warning MUST have been logged.
	if !bytes.Contains(logBuf.Bytes(), []byte("unparseable")) {
		t.Errorf("scenario (c): expected warning log containing 'unparseable', got: %s", logBuf.String())
	}
}

// TestLoadQueueAtStartup_SchemaUnsupported covers the forward-incompatible branch:
// schema_version is not in the supported read-set {1}.
//
// Expect: LoadQueueAtStartup returns (nil, ErrQueueSchemaUnsupported); the
// daemon caller MUST exit with code 2.
//
// Spec ref: queue-model.md §3.2 QM-002 — "Any other schema_version …
// forward-incompatible … refuses startup with exit code 2."
func TestLoadQueueAtStartup_SchemaUnsupported(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	startupQueueFixtureWriteUnsupportedSchema(t, projectDir)

	ledger := newStartupQueueFixtureLedger(nil, nil)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if gotQueue != nil {
		t.Errorf("schema unsupported: expected nil Queue, got %+v", gotQueue)
	}
	if err == nil {
		t.Fatal("schema unsupported: expected ErrQueueSchemaUnsupported, got nil error")
	}
	if !errors.Is(err, ErrQueueSchemaUnsupported) {
		t.Errorf("schema unsupported: error %v does not wrap ErrQueueSchemaUnsupported", err)
	}
}

// TestLoadQueueAtStartup_QM002aBeadsCrossCheck covers scenario (d): the main
// QM-002a cross-check assertion.
//
// Fixture: queue.json with one item at status=dispatched; BeadLedger mock
// reports the bead as open (claim_write_lost condition).
//
// Assertions:
//  1. Item reverts to pending in the returned Queue.
//  2. queue_item_reconciled event is emitted with reason=claim_write_lost.
//  3. The event fires BEFORE any "dispatch-loop tick" log line (ordering per
//     QM-002a: check MUST complete before the dispatch loop can run).
//  4. Item's run_id is cleared on revert.
//  5. queue.json on disk reflects the reverted state (persist happened).
//
// Spec ref: queue-model.md §3.2a QM-002a.
func TestLoadQueueAtStartup_QM002aBeadsCrossCheck(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-qm002a-test01")

	// Build a queue with one dispatched item and persist it.
	q := startupQueueFixtureDispatchedQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger mock: bead is open → claim_write_lost condition.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusOpen,
		},
		nil,
	)

	// Recording emitter to capture events in order.
	emitter := &startupQueueFixtureEmitter{}

	// Logger capturing output; used to assert dispatch-loop ordering (no
	// dispatch-loop tick log lines should appear before the emitter fires).
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if err != nil {
		t.Fatalf("scenario (d): unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("scenario (d): expected non-nil Queue, got nil")
	}

	// --- Assertion 1: item status reverted to pending ---
	if len(gotQueue.Groups) == 0 || len(gotQueue.Groups[0].Items) == 0 {
		t.Fatal("scenario (d): returned queue has no items")
	}
	revertedItem := gotQueue.Groups[0].Items[0]
	if revertedItem.Status != queue.ItemStatusPending {
		t.Errorf("scenario (d): item status: got %q, want %q (claim_write_lost revert)",
			revertedItem.Status, queue.ItemStatusPending)
	}

	// --- Assertion 4: run_id cleared on revert ---
	if revertedItem.RunID != nil {
		t.Errorf("scenario (d): item.RunID: got %q, want nil after revert", *revertedItem.RunID)
	}

	// --- Assertion 2: queue_item_reconciled event emitted ---
	evs := emitter.Events()
	if len(evs) == 0 {
		t.Fatal("scenario (d): expected queue_item_reconciled event, got none")
	}

	reconcileEvIdx := -1
	for i, ev := range evs {
		if ev.EventType == core.EventTypeQueueItemReconciled {
			reconcileEvIdx = i
			break
		}
	}
	if reconcileEvIdx < 0 {
		t.Fatalf("scenario (d): no queue_item_reconciled event in emitted events: %v", evs)
	}

	// Parse and validate the event payload.
	var payload core.QueueItemReconciledPayload
	if err := json.Unmarshal(evs[reconcileEvIdx].Payload, &payload); err != nil {
		t.Fatalf("scenario (d): unmarshal QueueItemReconciledPayload: %v", err)
	}
	if payload.Reason != "claim_write_lost" {
		t.Errorf("scenario (d): payload.Reason: got %q, want %q", payload.Reason, "claim_write_lost")
	}
	if payload.BeadID != string(testBeadID) {
		t.Errorf("scenario (d): payload.BeadID: got %q, want %q", payload.BeadID, testBeadID)
	}
	if payload.QueueID != q.QueueID {
		t.Errorf("scenario (d): payload.QueueID: got %q, want %q", payload.QueueID, q.QueueID)
	}
	if !payload.Valid() {
		t.Errorf("scenario (d): QueueItemReconciledPayload.Valid() returned false: %+v", payload)
	}

	// --- Assertion 3: event fires BEFORE any dispatch-loop tick ---
	// LoadQueueAtStartup must complete (including event emission) before the
	// caller can run any dispatch-loop tick. We verify this by confirming the
	// emitter captured the event during LoadQueueAtStartup (synchronously), and
	// that the log buffer does NOT contain a "dispatch-loop tick" marker from
	// within the function itself.
	logOutput := logBuf.String()
	if bytes.Contains([]byte(logOutput), []byte("dispatch-loop tick")) {
		t.Errorf("scenario (d): 'dispatch-loop tick' found in log output; reconcile must complete before dispatch loop runs")
	}

	// --- Assertion 5: queue.json on disk reflects the reverted state ---
	diskQueue, loadErr := queue.Load(ctx, projectDir)
	if loadErr != nil {
		t.Fatalf("scenario (d): Load from disk: %v", loadErr)
	}
	if diskQueue == nil {
		t.Fatal("scenario (d): disk queue is nil after reconciliation")
	}
	if len(diskQueue.Groups) == 0 || len(diskQueue.Groups[0].Items) == 0 {
		t.Fatal("scenario (d): disk queue has no items")
	}
	diskItem := diskQueue.Groups[0].Items[0]
	if diskItem.Status != queue.ItemStatusPending {
		t.Errorf("scenario (d): disk item status: got %q, want %q", diskItem.Status, queue.ItemStatusPending)
	}
}

// TestLoadQueueAtStartup_QM002aNoRevertWhenNotOpen verifies that an item
// recorded as dispatched is NOT reverted when the Beads ledger shows a
// non-open status (e.g., in_progress). Only the open-bead case is claim_write_lost.
//
// Spec ref: queue-model.md §3.2a QM-002a — "if Beads shows open … revert."
func TestLoadQueueAtStartup_QM002aNoRevertWhenNotOpen(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-qm002a-inprog01")

	q := startupQueueFixtureDispatchedQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: bead is in_progress (not open) → no revert.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusInProgress,
		},
		nil,
	)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueue, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	if err != nil {
		t.Fatalf("no-revert: unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("no-revert: expected non-nil Queue")
	}

	item := gotQueue.Groups[0].Items[0]
	if item.Status != queue.ItemStatusDispatched {
		t.Errorf("no-revert: item status: got %q, want %q (must NOT revert in_progress bead)",
			item.Status, queue.ItemStatusDispatched)
	}
	if evs := emitter.Events(); len(evs) != 0 {
		t.Errorf("no-revert: expected no events, got %d", len(evs))
	}
}
