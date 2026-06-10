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

	"github.com/gregberns/harmonik/internal/brcli"
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

// startupQueueFixtureWriteCorruptFile writes invalid JSON to the per-queue file
// .harmonik/queues/main.json, simulating an unrecoverable queue file (NQ-A2
// per-queue persistence).
func startupQueueFixtureWriteCorruptFile(t *testing.T, projectDir string) {
	t.Helper()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
		t.Fatalf("startupQueueFixtureWriteCorruptFile: MkdirAll queues: %v", err)
	}
	path := filepath.Join(queuesDir, "main.json")
	if err := os.WriteFile(path, []byte(`{{{{not valid json`), 0o600); err != nil {
		t.Fatalf("startupQueueFixtureWriteCorruptFile: WriteFile: %v", err)
	}
}

// startupQueueFixtureWriteUnsupportedSchema writes a per-queue file with an
// unrecognised schema_version to trigger the QM-002 forward-incompatible path.
func startupQueueFixtureWriteUnsupportedSchema(t *testing.T, projectDir string) {
	t.Helper()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o755); err != nil {
		t.Fatalf("startupQueueFixtureWriteUnsupportedSchema: MkdirAll queues: %v", err)
	}
	path := filepath.Join(queuesDir, "main.json")
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
	mu          sync.Mutex
	responses   map[core.BeadID]core.CoarseStatus
	errors      map[core.BeadID]error
	calls       []core.BeadID // ShowBead call log, in order
	inFlight    []core.BeadRecord
	inFlightErr error
}

// newStartupQueueFixtureLedger constructs a fake BeadLedger with pre-canned
// responses. Set statusByID to control what ShowBead returns per bead ID.
// If errByID is non-nil for a bead ID, ShowBead returns that error instead.
// inFlightBeads controls the return value of ListInFlightBeads (nil → empty).
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

// withInFlight sets the slice returned by ListInFlightBeads.
func (l *startupQueueFixtureBeadLedger) withInFlight(records []core.BeadRecord) *startupQueueFixtureBeadLedger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inFlight = records
	return l
}

// withInFlightErr sets the error returned by ListInFlightBeads.
func (l *startupQueueFixtureBeadLedger) withInFlightErr(err error) *startupQueueFixtureBeadLedger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inFlightErr = err
	return l
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

// ListInFlightBeads returns the pre-configured in-flight bead records.
func (l *startupQueueFixtureBeadLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlightErr != nil {
		return nil, l.inFlightErr
	}
	out := make([]core.BeadRecord, len(l.inFlight))
	copy(out, l.inFlight)
	return out, nil
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
// Test helpers
// ---------------------------------------------------------------------------

// firstOrNil returns the first element of qs, or nil when qs is empty.
// Used to adapt tests after LoadQueueAtStartup changed to return []*queue.Queue.
func firstOrNil(qs []*queue.Queue) *queue.Queue {
	if len(qs) == 0 {
		return nil
	}
	return qs[0]
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestLoadQueueAtStartup_FileAbsent covers scenario (a): no queue file.
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

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
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
// are emitted (the pending bead is open in the ledger, so no mismatch fires).
//
// Note: QM-002b Class A calls ShowBead for pending items to check if the ledger
// already shows closed. The ledger mock is configured with CoarseStatusOpen so
// no mismatch is triggered.
//
// Spec ref: queue-model.md §3.2 QM-002 — "File exists and parses → queue loaded."
func TestLoadQueueAtStartup_CleanLoad(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	// Write a valid queue.json with one pending item (bead ID from the fixture).
	q := startupQueueFixtureMinimalQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// QM-002b Class A scans pending items via ShowBead; configure the ledger to
	// return CoarseStatusOpen so no mismatch is triggered.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			core.BeadID("hk-test01"): core.CoarseStatusOpen,
		},
		nil,
	)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("scenario (b): expected nil error, got %v", err)
	}
	if gotQueue == nil {
		t.Fatal("scenario (b): expected non-nil Queue, got nil")
	}
	if gotQueue.QueueID != q.QueueID {
		t.Errorf("scenario (b): QueueID got %q, want %q", gotQueue.QueueID, q.QueueID)
	}
	if evs := emitter.Events(); len(evs) != 0 {
		t.Errorf("scenario (b): no mismatch items → expected no emitted events, got %d", len(evs))
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

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("scenario (c): expected nil error, got %v", err)
	}
	if gotQueue != nil {
		t.Fatalf("scenario (c): expected nil Queue, got %+v", gotQueue)
	}

	// The corrupt file MUST NOT be deleted per QM-002.
	queuePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("scenario (c): corrupt queue file was auto-deleted; per QM-002 it MUST be left for operator inspection")
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

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
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

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
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
	diskQueue, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
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

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
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

// ---------------------------------------------------------------------------
// QM-002b three-way reconciliation fixtures
// ---------------------------------------------------------------------------

// startupQueueFixturePendingQueue builds a Queue where group 0, item 0 has
// status=pending. Used for the Class A mismatch test.
func startupQueueFixturePendingQueue(beadID core.BeadID) queue.Queue {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-3333-7000-8000-000000000003",
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
						Status: queue.ItemStatusPending,
					},
				},
				CreatedAt: now,
			},
		},
	}
}

// startupQueueFixtureCompletedQueue builds a Queue where group 0, item 0 has
// status=completed. Used for the Class C mismatch test.
func startupQueueFixtureCompletedQueue(beadID core.BeadID) queue.Queue {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-4444-7000-8000-000000000004",
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
						Status: queue.ItemStatusCompleted,
					},
				},
				CreatedAt: now,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// QM-002b tests
// ---------------------------------------------------------------------------

// TestLoadQueueAtStartup_QM002b_ClassA_BeadClosedQueuePending covers the Class A
// mismatch: queue item is pending but the Beads ledger reports the bead as closed.
//
// Assertions:
//  1. Item status is advanced to completed in the returned Queue.
//  2. Queue.json on disk reflects the completed status (persist happened).
//  3. A reconciliation_mismatch_observed event is emitted with
//     mismatch_class=bead_closed_queue_pending.
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class A.
// Bead: hk-nvfvj.
func TestLoadQueueAtStartup_QM002b_ClassA_BeadClosedQueuePending(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-nvfvj-classA-01")

	q := startupQueueFixturePendingQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: bead is closed → Class A condition.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusClosed,
		},
		nil,
	)

	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("Class A: unexpected error: %v", err)
	}
	// After Class A reconciliation the single item is advanced to completed, making
	// all items terminal. reconcileQueueTerminalState then detects this and calls
	// CompleteAndUnlink (F5/hk-qkahq), so the returned queue must be nil.
	if gotQueue != nil {
		t.Errorf("Class A: expected nil queue (cleaned up by F5 after all items completed), got status=%q", gotQueue.Status)
	}

	// Assertion 1: queue file unlinked (CompleteAndUnlink ran).
	queueFilePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFilePath); !os.IsNotExist(statErr) {
		t.Error("Class A: queue file still exists; expected it to be unlinked after all items completed")
	}

	// Assertion 2: reconciliation_mismatch_observed event emitted (fires before cleanup).
	evs := emitter.Events()
	if len(evs) == 0 {
		t.Fatal("Class A: expected reconciliation_mismatch_observed event, got none")
	}
	found := false
	for _, ev := range evs {
		if ev.EventType != core.EventTypeReconciliationMismatchObserved {
			continue
		}
		var p core.ReconciliationMismatchObservedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &p); unmarshalErr != nil {
			t.Fatalf("Class A: unmarshal payload: %v", unmarshalErr)
		}
		if p.MismatchClass != "bead_closed_queue_pending" {
			t.Errorf("Class A: mismatch_class: got %q, want bead_closed_queue_pending", p.MismatchClass)
		}
		if p.BeadID != string(testBeadID) {
			t.Errorf("Class A: bead_id: got %q, want %q", p.BeadID, testBeadID)
		}
		if !p.Valid() {
			t.Errorf("Class A: ReconciliationMismatchObservedPayload.Valid() false: %+v", p)
		}
		found = true
		break
	}
	if !found {
		t.Error("Class A: reconciliation_mismatch_observed event not found in emitted events")
	}
}

// TestLoadQueueAtStartup_QM002b_ClassB_BeadInProgressQueueAbsent covers the Class B
// mismatch: the Beads ledger reports a bead as in_progress but there is no queue
// item for it.
//
// Assertions:
//  1. No queue mutation occurs (no items to mutate for an absent bead).
//  2. A reconciliation_mismatch_observed event is emitted with
//     mismatch_class=bead_inprogress_queue_absent.
//  3. Event payload has group_index=-1 and queue_id="".
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class B.
// Bead: hk-nvfvj.
func TestLoadQueueAtStartup_QM002b_ClassB_BeadInProgressQueueAbsent(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const orphanBeadID = core.BeadID("hk-nvfvj-classB-orphan")

	// Queue has a different bead (not the orphan).
	q := startupQueueFixturePendingQueue(core.BeadID("hk-nvfvj-classB-known"))
	// The known bead is pending with ledger=open (no mismatch).
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			core.BeadID("hk-nvfvj-classB-known"): core.CoarseStatusOpen,
		},
		nil,
	)
	// ListInFlightBeads returns the orphan bead.
	ledger.withInFlight([]core.BeadRecord{
		{
			BeadID:   orphanBeadID,
			Status:   core.CoarseStatusInProgress,
			Title:    "orphan bead",
			BeadType: "task",
		},
	})

	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("Class B: unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("Class B: expected non-nil Queue")
	}

	// Assertion 1: known queue item is untouched.
	if gotQueue.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Errorf("Class B: known item status mutated unexpectedly: got %q, want pending",
			gotQueue.Groups[0].Items[0].Status)
	}

	// Assertion 2: reconciliation_mismatch_observed event emitted for orphan.
	evs := emitter.Events()
	found := false
	for _, ev := range evs {
		if ev.EventType != core.EventTypeReconciliationMismatchObserved {
			continue
		}
		var p core.ReconciliationMismatchObservedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &p); unmarshalErr != nil {
			t.Fatalf("Class B: unmarshal payload: %v", unmarshalErr)
		}
		if p.BeadID != string(orphanBeadID) {
			continue
		}
		if p.MismatchClass != "bead_inprogress_queue_absent" {
			t.Errorf("Class B: mismatch_class: got %q, want bead_inprogress_queue_absent", p.MismatchClass)
		}
		// Assertion 3: group_index=-1 and queue_id="".
		if p.GroupIndex != -1 {
			t.Errorf("Class B: group_index: got %d, want -1", p.GroupIndex)
		}
		if p.QueueID != "" {
			t.Errorf("Class B: queue_id: got %q, want empty", p.QueueID)
		}
		if !p.Valid() {
			t.Errorf("Class B: ReconciliationMismatchObservedPayload.Valid() false: %+v", p)
		}
		found = true
		break
	}
	if !found {
		t.Errorf("Class B: reconciliation_mismatch_observed event for orphan %q not found in %d events",
			orphanBeadID, len(evs))
	}
}

// TestLoadQueueAtStartup_QM002b_ClassAPrime_DispatchedBeadClosed covers the Class A'
// mismatch: queue item is dispatched but the Beads ledger reports the bead as
// closed/tombstone (daemon restart abandoned the goroutine; bead closed via another
// path such as a sibling queue).
//
// Assertions:
//  1. Item status is advanced to completed in the returned Queue.
//  2. Queue.json on disk reflects the completed status (persist happened).
//  3. A reconciliation_mismatch_observed event is emitted with
//     mismatch_class=bead_closed_queue_dispatched.
//  4. A dispatched item whose bead is still open/in_progress is NOT advanced
//     (Class A' only fires on closed/tombstone).
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class A'.
// Bead: hk-z0pmi.
func TestLoadQueueAtStartup_QM002b_ClassAPrime_DispatchedBeadClosed(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-z0pmi-classAprime-01")

	// A dispatched queue item (QM-002a fixture already builds this shape).
	q := startupQueueFixtureDispatchedQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: bead is closed → Class A' condition.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusClosed,
		},
		nil,
	)

	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("Class A': unexpected error: %v", err)
	}
	// After Class A' reconciliation the single item is advanced to completed, making
	// all items terminal. reconcileQueueTerminalState then detects this and calls
	// CompleteAndUnlink (F5/hk-qkahq), so the returned queue must be nil.
	if gotQueue != nil {
		t.Errorf("Class A': expected nil queue (cleaned up by F5 after all items completed), got status=%q", gotQueue.Status)
	}

	// Assertion 1: queue file unlinked (CompleteAndUnlink ran).
	queueFilePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFilePath); !os.IsNotExist(statErr) {
		t.Error("Class A': queue file still exists; expected it to be unlinked after all items completed")
	}

	// Assertion 2: reconciliation_mismatch_observed event with bead_closed_queue_dispatched.
	evs := emitter.Events()
	if len(evs) == 0 {
		t.Fatal("Class A': expected reconciliation_mismatch_observed event, got none")
	}
	found := false
	for _, ev := range evs {
		if ev.EventType != core.EventTypeReconciliationMismatchObserved {
			continue
		}
		var p core.ReconciliationMismatchObservedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &p); unmarshalErr != nil {
			t.Fatalf("Class A': unmarshal payload: %v", unmarshalErr)
		}
		if p.MismatchClass != "bead_closed_queue_dispatched" {
			t.Errorf("Class A': mismatch_class: got %q, want bead_closed_queue_dispatched", p.MismatchClass)
		}
		if p.BeadID != string(testBeadID) {
			t.Errorf("Class A': bead_id: got %q, want %q", p.BeadID, testBeadID)
		}
		if p.QueueStatus != "dispatched" {
			t.Errorf("Class A': queue_status: got %q, want dispatched", p.QueueStatus)
		}
		if !p.Valid() {
			t.Errorf("Class A': ReconciliationMismatchObservedPayload.Valid() false: %+v", p)
		}
		found = true
		break
	}
	if !found {
		t.Error("Class A': reconciliation_mismatch_observed event not found in emitted events")
	}
}

// TestLoadQueueAtStartup_QM002b_ClassAPrime_NoAdvanceWhenBeadOpen verifies that a
// dispatched item is NOT advanced when the bead is still open or in_progress.
// Class A' only fires on closed/tombstone.
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class A'.
// Bead: hk-z0pmi.
func TestLoadQueueAtStartup_QM002b_ClassAPrime_NoAdvanceWhenBeadOpen(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-z0pmi-classAprime-open")

	q := startupQueueFixtureDispatchedQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: bead is in_progress → Class A' must NOT fire.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusInProgress,
		},
		nil,
	)

	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("Class A' no-advance: unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("Class A' no-advance: expected non-nil Queue")
	}

	item := gotQueue.Groups[0].Items[0]
	if item.Status != queue.ItemStatusDispatched {
		t.Errorf("Class A' no-advance: item status: got %q, want dispatched (must NOT advance in_progress bead)",
			item.Status)
	}
	if evs := emitter.Events(); len(evs) != 0 {
		t.Errorf("Class A' no-advance: expected no events, got %d", len(evs))
	}
}

// TestLoadQueueAtStartup_QM002b_ClassC_BeadClosedQueueInProgress covers the Class C
// mismatch: queue item is completed/failed but the Beads ledger still shows in_progress.
//
// Assertions:
//  1. No queue mutation occurs (queue-side terminal is already set).
//  2. A reconciliation_mismatch_observed event is emitted with
//     mismatch_class=bead_closed_queue_inprogress.
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class C.
// Bead: hk-nvfvj.
func TestLoadQueueAtStartup_QM002b_ClassC_BeadClosedQueueInProgress(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const testBeadID = core.BeadID("hk-nvfvj-classC-01")

	q := startupQueueFixtureCompletedQueue(testBeadID)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: bead is in_progress → Class C condition.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			testBeadID: core.CoarseStatusInProgress,
		},
		nil,
	)

	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("Class C: unexpected error: %v", err)
	}
	// The Class C fixture has the item already completed and the group still active.
	// reconcileQueueTerminalState (F5/hk-qkahq) advances the group and cleans up
	// the queue via CompleteAndUnlink, so the returned queue must be nil.
	if gotQueue != nil {
		t.Errorf("Class C: expected nil queue (cleaned up by F5 after all items completed), got status=%q", gotQueue.Status)
	}

	// Assertion 1: queue file unlinked (CompleteAndUnlink ran).
	queueFilePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFilePath); !os.IsNotExist(statErr) {
		t.Error("Class C: queue file still exists; expected it to be unlinked after all items completed")
	}

	// Assertion 2: reconciliation_mismatch_observed event emitted (fires before cleanup).
	evs := emitter.Events()
	found := false
	for _, ev := range evs {
		if ev.EventType != core.EventTypeReconciliationMismatchObserved {
			continue
		}
		var p core.ReconciliationMismatchObservedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &p); unmarshalErr != nil {
			t.Fatalf("Class C: unmarshal payload: %v", unmarshalErr)
		}
		if p.MismatchClass != "bead_closed_queue_inprogress" {
			t.Errorf("Class C: mismatch_class: got %q, want bead_closed_queue_inprogress", p.MismatchClass)
		}
		if p.BeadID != string(testBeadID) {
			t.Errorf("Class C: bead_id: got %q, want %q", p.BeadID, testBeadID)
		}
		if p.QueueStatus != string(queue.ItemStatusCompleted) {
			t.Errorf("Class C: queue_status: got %q, want %q", p.QueueStatus, queue.ItemStatusCompleted)
		}
		if !p.Valid() {
			t.Errorf("Class C: ReconciliationMismatchObservedPayload.Valid() false: %+v", p)
		}
		found = true
		break
	}
	if !found {
		t.Error("Class C: reconciliation_mismatch_observed event not found in emitted events")
	}
}

// ---------------------------------------------------------------------------
// Class B reap fake
// ---------------------------------------------------------------------------

// startupQueueFixtureFakeResetter is a test-double BeadResetter that records
// every ResetBead call.
type startupQueueFixtureFakeResetter struct {
	mu    sync.Mutex
	calls []startupQueueFixtureResetCall
	err   error // if non-nil, returned for every ResetBead call
}

type startupQueueFixtureResetCall struct {
	BeadID core.BeadID
}

func (r *startupQueueFixtureFakeResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, startupQueueFixtureResetCall{BeadID: beadID})
	return r.err
}

func (r *startupQueueFixtureFakeResetter) Calls() []startupQueueFixtureResetCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]startupQueueFixtureResetCall, len(r.calls))
	copy(out, r.calls)
	return out
}

// ---------------------------------------------------------------------------
// Class B reap test
// ---------------------------------------------------------------------------

// TestLoadQueueAtStartup_QM002b_ClassB_Reaping covers the hk-5pg37 fix: when
// a bead is in_progress in the ledger but has no queue item (Class B orphan),
// and a QM002bReapConfig is provided, LoadQueueAtStartup MUST call
// ResetBead to transition the bead from in_progress → open.
//
// Assertions:
//  1. ResetBead is called exactly once for the orphan bead.
//  2. The reset is called with the correct beadID.
//  3. The mismatch event is still emitted (existing Class B behaviour preserved).
//  4. No error is returned from LoadQueueAtStartup.
//
// Spec ref: specs/queue-model.md §3.2b QM-002b — Class B reap.
// Bead ref: hk-5pg37.
func TestLoadQueueAtStartup_QM002b_ClassB_Reaping(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const orphanBeadID = core.BeadID("hk-5pg37-classB-reap-orphan")

	// Active queue has a different bead — not the orphan.
	q := startupQueueFixturePendingQueue(core.BeadID("hk-5pg37-classB-reap-known"))
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// Ledger: the known queue bead is open; the orphan is in_progress with no queue item.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			core.BeadID("hk-5pg37-classB-reap-known"): core.CoarseStatusOpen,
		},
		nil,
	)
	ledger.withInFlight([]core.BeadRecord{
		{
			BeadID:   orphanBeadID,
			Status:   core.CoarseStatusInProgress,
			Title:    "orphan bead (queue-cancelled)",
			BeadType: "task",
		},
	})

	resetter := &startupQueueFixtureFakeResetter{}
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	reapCfg := &QM002bReapConfig{
		Resetter:      resetter,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		ProjectHash:   core.ProjectHash("test-project-hash"),
		DaemonStartNS: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).UnixNano(),
	}

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger, reapCfg)
	if err != nil {
		t.Fatalf("Class B reap: unexpected error: %v", err)
	}
	if len(gotQueues) == 0 {
		t.Fatal("Class B reap: expected non-nil Queue")
	}

	// Assertion 1 & 2: ResetBead called exactly once for the orphan bead.
	resetCalls := resetter.Calls()
	if len(resetCalls) != 1 {
		t.Fatalf("Class B reap: expected 1 ResetBead call, got %d", len(resetCalls))
	}
	if resetCalls[0].BeadID != orphanBeadID {
		t.Errorf("Class B reap: ResetBead called with bead_id=%q, want %q",
			resetCalls[0].BeadID, orphanBeadID)
	}

	// Assertion 3: mismatch event still emitted (observability preserved).
	evs := emitter.Events()
	found := false
	for _, ev := range evs {
		if ev.EventType != core.EventTypeReconciliationMismatchObserved {
			continue
		}
		var p core.ReconciliationMismatchObservedPayload
		if unmarshalErr := json.Unmarshal(ev.Payload, &p); unmarshalErr != nil {
			continue
		}
		if p.BeadID == string(orphanBeadID) && p.MismatchClass == "bead_inprogress_queue_absent" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Class B reap: reconciliation_mismatch_observed event for orphan not found")
	}
}

// ---------------------------------------------------------------------------
// F5 (hk-qkahq): stale active-marker reconciliation tests
// ---------------------------------------------------------------------------

// startupQueueFixtureAllItemsCompletedQueue builds a Queue where the queue
// status is active, the group status is active, but ALL items are completed.
// This simulates a daemon killed after items finished but before
// CompleteAndUnlink ran.
func startupQueueFixtureAllItemsCompletedQueue(beadIDs ...core.BeadID) queue.Queue {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID: id,
			Status: queue.ItemStatusCompleted,
		}
	}
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-f501-7000-8000-000000000001",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}
}

// startupQueueFixtureAllItemsFailedQueue builds a Queue where the queue
// status is active, the group status is active, but ALL items are failed.
// This simulates a daemon killed after items ran with failures but before
// paused-by-failure transition ran.
func startupQueueFixtureAllItemsFailedQueue(beadIDs ...core.BeadID) queue.Queue {
	now := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	items := make([]queue.Item, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = queue.Item{
			BeadID: id,
			Status: queue.ItemStatusFailed,
		}
	}
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019605a0-f502-7000-8000-000000000002",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}
}

// TestLoadQueueAtStartup_F5_AllItemsCompleted verifies scenario (e):
// queue file has status=active, group active, all items completed — the stale
// active-marker left by a killed daemon is cleared at startup.
//
// Assertions:
//  1. LoadQueueAtStartup returns nil queue (file cleaned up, don't load).
//  2. The queue file (.harmonik/queues/main.json) no longer exists on disk.
//
// Bead ref: hk-qkahq (logmine F5).
func TestLoadQueueAtStartup_F5_AllItemsCompleted(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const beadA = core.BeadID("hk-f5-done-01")
	const beadB = core.BeadID("hk-f5-done-02")

	q := startupQueueFixtureAllItemsCompletedQueue(beadA, beadB)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// No ledger calls expected: items are already completed, reconcileDispatchedItems
	// and reconcileThreeWay skip them; reconcileQueueTerminalState doesn't need ledger.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			beadA: core.CoarseStatusClosed,
			beadB: core.CoarseStatusClosed,
		},
		nil,
	)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("F5 all-completed: unexpected error: %v", err)
	}
	// Queue should be cleaned up — caller must NOT load it into QueueStore.
	if gotQueue != nil {
		t.Errorf("F5 all-completed: expected nil queue (cleaned up), got queue with status=%q", gotQueue.Status)
	}

	// Queue file should be unlinked.
	queueFilePath := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFilePath); !os.IsNotExist(statErr) {
		t.Error("F5 all-completed: queue file still exists after active-marker cleanup; expected it to be unlinked")
	}
}

// TestLoadQueueAtStartup_F5_AllItemsFailed verifies scenario (f):
// queue file has status=active, group active, all items failed — the stale
// active-marker is demoted to paused-by-failure so QM-027 allows new submits.
//
// Assertions:
//  1. LoadQueueAtStartup returns the queue (non-nil).
//  2. Returned queue has status=paused-by-failure.
//  3. The queue file on disk reflects status=paused-by-failure.
//
// Bead ref: hk-qkahq (logmine F5).
func TestLoadQueueAtStartup_F5_AllItemsFailed(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	const beadA = core.BeadID("hk-f5-fail-01")

	q := startupQueueFixtureAllItemsFailedQueue(beadA)
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	// QM-002b Class C: queue item is failed (terminal) but ledger may be in_progress.
	// Configuring closed so Class C doesn't fire a mismatch event.
	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			beadA: core.CoarseStatusClosed,
		},
		nil,
	)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("F5 all-failed: unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("F5 all-failed: expected non-nil queue, got nil")
	}

	// Queue should be demoted to paused-by-failure.
	if gotQueue.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("F5 all-failed: status: got %q, want %q", gotQueue.Status, queue.QueueStatusPausedByFailure)
	}

	// Group should be advanced to complete-with-failures.
	if len(gotQueue.Groups) == 0 {
		t.Fatal("F5 all-failed: no groups in returned queue")
	}
	if gotQueue.Groups[0].Status != queue.GroupStatusCompleteWithFailures {
		t.Errorf("F5 all-failed: group[0].Status: got %q, want %q",
			gotQueue.Groups[0].Status, queue.GroupStatusCompleteWithFailures)
	}

	// Disk state must reflect paused-by-failure.
	diskQueue, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("F5 all-failed: Load from disk: %v", loadErr)
	}
	if diskQueue == nil {
		t.Fatal("F5 all-failed: disk queue is nil after reconciliation")
	}
	if diskQueue.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("F5 all-failed: disk queue status: got %q, want %q",
			diskQueue.Status, queue.QueueStatusPausedByFailure)
	}
}

// TestLoadQueueAtStartup_F5_PendingItemsNotAffected verifies that a queue with
// pending items is NOT cleaned up by reconcileQueueTerminalState — only
// all-terminal groups are advanced.
//
// Bead ref: hk-qkahq (logmine F5).
func TestLoadQueueAtStartup_F5_PendingItemsNotAffected(t *testing.T) {
	t.Parallel()

	projectDir := startupQueueFixtureProjectDir(t)
	ctx := context.Background()

	// Queue with one pending item — not a stale active-marker case.
	const beadA = core.BeadID("hk-f5-pending-01")
	q := startupQueueFixtureMinimalQueue()
	q.Groups[0].Items[0].BeadID = beadA
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("setup: Persist: %v", err)
	}

	ledger := newStartupQueueFixtureLedger(
		map[core.BeadID]core.CoarseStatus{
			beadA: core.CoarseStatusOpen,
		},
		nil,
	)
	emitter := &startupQueueFixtureEmitter{}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	gotQueues, err := LoadQueueAtStartup(ctx, projectDir, ledger, emitter, logger)
	gotQueue := firstOrNil(gotQueues)
	if err != nil {
		t.Fatalf("F5 pending-not-affected: unexpected error: %v", err)
	}
	if gotQueue == nil {
		t.Fatal("F5 pending-not-affected: expected non-nil queue (still has pending work), got nil")
	}

	// Queue and group status must be unchanged.
	if gotQueue.Status != queue.QueueStatusActive {
		t.Errorf("F5 pending-not-affected: queue status: got %q, want %q",
			gotQueue.Status, queue.QueueStatusActive)
	}
	if gotQueue.Groups[0].Status != queue.GroupStatusActive {
		t.Errorf("F5 pending-not-affected: group[0].Status: got %q, want %q",
			gotQueue.Groups[0].Status, queue.GroupStatusActive)
	}
}
