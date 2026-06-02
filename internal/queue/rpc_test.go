package queue_test

// rpc_test.go — unit tests for the four queue JSON-RPC handler functions and
// the HandlerAdapter.
//
// Coverage:
//   - HandleQueueSubmit: happy path + validation-error path (QM-027, single active queue)
//   - HandleQueueAppend: happy path + validation-error path (QM-024, append-target-invalid)
//   - HandleQueueStatus: queue present + queue absent (nil → {queue: null})
//   - HandleQueueDryRun: happy path + validation-error path (QM-020, bead-not-found)
//
// Test helper prefix: rpcFixture
//
// Spec ref: specs/queue-model.md §2.10, §6, §8.1.
// Bead ref: hk-nomxl.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Fake ledger for rpc_test.go
// ---------------------------------------------------------------------------

// rpcFixtureFakeLedger is a fake BeadLedger for RPC handler tests.
// It maps bead IDs to statuses and records edges for QM-025.
type rpcFixtureFakeLedger struct {
	statuses map[core.BeadID]queue.BeadStatus
	edges    map[[2]core.BeadID]bool
}

func (f *rpcFixtureFakeLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s, ok := f.statuses[id]; ok {
		return s, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (f *rpcFixtureFakeLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return f.edges[[2]core.BeadID{blocker, blocked}], nil
}

// rpcFixtureOpenLedger returns a fake ledger where the given IDs are all open.
func rpcFixtureOpenLedger(ids ...core.BeadID) queue.BeadLedger {
	statuses := make(map[core.BeadID]queue.BeadStatus)
	for _, id := range ids {
		statuses[id] = queue.BeadStatusOpen
	}
	return &rpcFixtureFakeLedger{
		statuses: statuses,
		edges:    make(map[[2]core.BeadID]bool),
	}
}

// rpcFixtureTempProjectDir creates a temporary project directory with a
// .harmonik subdirectory and returns the project root path.
func rpcFixtureTempProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("rpcFixtureTempProjectDir: MkdirAll: %v", err)
	}
	return dir
}

// rpcFixtureWaveGroup returns a wave Group containing items for each bead ID,
// all with ItemStatus pending and no timestamps set.
func rpcFixtureWaveGroup(groupIndex int, ids ...core.BeadID) queue.Group {
	items := make([]queue.Item, len(ids))
	for i, id := range ids {
		items[i] = queue.Item{BeadID: id, Status: queue.ItemStatusPending}
	}
	return queue.Group{
		GroupIndex: groupIndex,
		Kind:       queue.GroupKindWave,
		Status:     queue.GroupStatusPending,
		Items:      items,
		CreatedAt:  time.Now().UTC(),
	}
}

// rpcFixtureStreamGroup returns a stream Group for append tests.
func rpcFixtureStreamGroup(groupIndex int, ids ...core.BeadID) queue.Group {
	g := rpcFixtureWaveGroup(groupIndex, ids...)
	g.Kind = queue.GroupKindStream
	return g
}

// ---------------------------------------------------------------------------
// HandleQueueSubmit
// ---------------------------------------------------------------------------

// TestHandleQueueSubmit_HappyPath verifies that a valid queue-submit request
// mints a queue_id, returns status=active, and persists queue.json.
func TestHandleQueueSubmit_HappyPath(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc001"
	const beadB core.BeadID = "hk-rpc002"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups: []queue.Group{
			rpcFixtureWaveGroup(0, beadA, beadB),
		},
	}

	resp, q, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}

	// queue_id must be non-empty and 36 chars (UUID canonical form).
	if len(resp.QueueID) != 36 {
		t.Errorf("QueueID = %q, want 36-char UUID", resp.QueueID)
	}
	if resp.Status != queue.QueueStatusActive {
		t.Errorf("Status = %q, want %q", resp.Status, queue.QueueStatusActive)
	}
	if resp.GroupCount != 1 {
		t.Errorf("GroupCount = %d, want 1", resp.GroupCount)
	}

	// The returned *Queue must be non-nil and have status=active.
	if q == nil {
		t.Fatal("returned *Queue is nil")
	}
	if q.Status != queue.QueueStatusActive {
		t.Errorf("returned queue status = %q, want %q", q.Status, queue.QueueStatusActive)
	}

	// queues/main.json must exist on disk (NQ-A2 per-queue persistence).
	queueFile := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if _, statErr := os.Stat(queueFile); statErr != nil {
		t.Errorf("queues/main.json not found after submit: %v", statErr)
	}
}

// TestHandleQueueSubmit_ValidationError_AlreadyActive verifies that submitting
// when a queue is already active returns RPCError with code -32010 (QM-027).
func TestHandleQueueSubmit_ValidationError_AlreadyActive(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc010"
	const beadB core.BeadID = "hk-rpc011"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	req := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}

	// First submit succeeds.
	_, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("first submit: unexpected RPCError: %v", rpcErr)
	}

	// Second submit with different bead must fail with queue_already_active.
	req2 := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadB)},
	}
	_, _, _, rpcErr2 := queue.HandleQueueSubmit(t.Context(), req2, ledger, projectDir, 1)
	if rpcErr2 == nil {
		t.Fatal("second submit: expected RPCError, got nil")
	}
	if rpcErr2.Code != queue.ErrorCodeQueueAlreadyActive {
		t.Errorf("second submit: RPCError.Code = %d, want %d (queue_already_active)",
			rpcErr2.Code, queue.ErrorCodeQueueAlreadyActive)
	}
}

// ---------------------------------------------------------------------------
// HandleQueueAppend
// ---------------------------------------------------------------------------

// TestHandleQueueAppend_HappyPath verifies that appending to a stream group
// returns the correct appended_count and new_tail_indices.
func TestHandleQueueAppend_HappyPath(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc020"
	const beadB core.BeadID = "hk-rpc021"
	const beadC core.BeadID = "hk-rpc022"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB, beadC)

	// Submit a queue with a stream group containing beadA.
	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadA)},
	}
	submitResp, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), submitReq, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("setup submit: unexpected RPCError: %v", rpcErr)
	}

	// Append beadB and beadC.
	appendReq := queue.QueueAppendRequest{
		QueueID:    submitResp.QueueID,
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{beadB, beadC},
	}
	appendResp, mutated, _, rpcErr2 := queue.HandleQueueAppend(t.Context(), appendReq, ledger, projectDir)
	if rpcErr2 != nil {
		t.Fatalf("HandleQueueAppend: unexpected RPCError: %v", rpcErr2)
	}

	if appendResp.AppendedCount != 2 {
		t.Errorf("AppendedCount = %d, want 2", appendResp.AppendedCount)
	}
	if len(appendResp.NewTailIndices) != 2 {
		t.Errorf("len(NewTailIndices) = %d, want 2", len(appendResp.NewTailIndices))
	}
	// beadA was at index 0; new items should start at index 1.
	if appendResp.NewTailIndices[0] != 1 {
		t.Errorf("NewTailIndices[0] = %d, want 1", appendResp.NewTailIndices[0])
	}
	if appendResp.NewTailIndices[1] != 2 {
		t.Errorf("NewTailIndices[1] = %d, want 2", appendResp.NewTailIndices[1])
	}
	if mutated == nil {
		t.Fatal("mutated queue is nil")
	}
}

// TestHandleQueueAppend_ValidationError_NoQueue verifies that appending when
// no queue is loaded returns RPCError with code -32011 (append_target_invalid).
func TestHandleQueueAppend_ValidationError_NoQueue(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc030"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA)

	req := queue.QueueAppendRequest{
		QueueID:    "00000000-0000-0000-0000-000000000000",
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{beadA},
	}
	_, _, _, rpcErr := queue.HandleQueueAppend(t.Context(), req, ledger, projectDir)
	if rpcErr == nil {
		t.Fatal("expected RPCError, got nil")
	}
	if rpcErr.Code != queue.ErrorCodeAppendTargetInvalid {
		t.Errorf("RPCError.Code = %d, want %d (append_target_invalid)",
			rpcErr.Code, queue.ErrorCodeAppendTargetInvalid)
	}
}

// ---------------------------------------------------------------------------
// HandleQueueStatus
// ---------------------------------------------------------------------------

// TestHandleQueueStatus_NoQueue verifies that status returns {queue: null}
// when no queue is loaded.
func TestHandleQueueStatus_NoQueue(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)

	resp, rpcErr := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{})
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: unexpected RPCError: %v", rpcErr)
	}
	if resp.Queue != nil {
		t.Errorf("Queue = %v, want nil (no queue loaded)", resp.Queue)
	}
}

// TestHandleQueueStatus_WithActiveQueue verifies that status returns the queue
// envelope when a queue has been submitted.
func TestHandleQueueStatus_WithActiveQueue(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc040"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA)

	// Submit a queue.
	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}
	submitResp, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), submitReq, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("setup submit: unexpected RPCError: %v", rpcErr)
	}

	resp, rpcErr2 := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{})
	if rpcErr2 != nil {
		t.Fatalf("HandleQueueStatus: unexpected RPCError: %v", rpcErr2)
	}
	if resp.Queue == nil {
		t.Fatal("Queue is nil, want non-nil envelope")
	}
	if resp.Queue.QueueID != submitResp.QueueID {
		t.Errorf("Queue.QueueID = %q, want %q", resp.Queue.QueueID, submitResp.QueueID)
	}
	if resp.Queue.Status != queue.QueueStatusActive {
		t.Errorf("Queue.Status = %q, want %q", resp.Queue.Status, queue.QueueStatusActive)
	}
}

// TestHandleQueueStatus_ByName verifies that status returns the correct queue
// when a name is supplied (hk-1k5as: named-queue status routing).
func TestHandleQueueStatus_ByName(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc-sbn-a"
	const beadB core.BeadID = "hk-rpc-sbn-b"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	// Submit to "alpha" named queue.
	alphaResp, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "alpha",
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("submit alpha: unexpected RPCError: %v", rpcErr)
	}

	// Submit to "beta" named queue.
	betaResp, _, _, rpcErr2 := queue.HandleQueueSubmit(t.Context(), queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "beta",
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadB)},
	}, ledger, projectDir, 1)
	if rpcErr2 != nil {
		t.Fatalf("submit beta: unexpected RPCError: %v", rpcErr2)
	}

	// Status with Name="alpha" must return alpha's queue.
	respAlpha, rpcErr3 := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{Name: "alpha"})
	if rpcErr3 != nil {
		t.Fatalf("HandleQueueStatus(alpha): unexpected RPCError: %v", rpcErr3)
	}
	if respAlpha.Queue == nil {
		t.Fatal("status(alpha): Queue is nil, want non-nil")
	}
	if respAlpha.Queue.QueueID != alphaResp.QueueID {
		t.Errorf("status(alpha): QueueID = %q, want %q", respAlpha.Queue.QueueID, alphaResp.QueueID)
	}

	// Status with Name="beta" must return beta's queue.
	respBeta, rpcErr4 := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{Name: "beta"})
	if rpcErr4 != nil {
		t.Fatalf("HandleQueueStatus(beta): unexpected RPCError: %v", rpcErr4)
	}
	if respBeta.Queue == nil {
		t.Fatal("status(beta): Queue is nil, want non-nil")
	}
	if respBeta.Queue.QueueID != betaResp.QueueID {
		t.Errorf("status(beta): QueueID = %q, want %q", respBeta.Queue.QueueID, betaResp.QueueID)
	}
}

// TestHandleQueueStatus_ByQueueID verifies that status returns the correct
// queue when resolved by UUID (hk-1k5as).
func TestHandleQueueStatus_ByQueueID(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc-sbq-a"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA)

	// Submit to a named queue.
	submitResp, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "flywheel",
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("submit flywheel: unexpected RPCError: %v", rpcErr)
	}

	// Status by UUID must find the flywheel queue without specifying its name.
	resp, rpcErr2 := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{QueueID: submitResp.QueueID})
	if rpcErr2 != nil {
		t.Fatalf("HandleQueueStatus(queue_id): unexpected RPCError: %v", rpcErr2)
	}
	if resp.Queue == nil {
		t.Fatal("status(queue_id): Queue is nil, want non-nil (UUID should resolve flywheel queue)")
	}
	if resp.Queue.QueueID != submitResp.QueueID {
		t.Errorf("status(queue_id): QueueID = %q, want %q", resp.Queue.QueueID, submitResp.QueueID)
	}
	if resp.Queue.Name != "flywheel" {
		t.Errorf("status(queue_id): Name = %q, want %q", resp.Queue.Name, "flywheel")
	}
}

// TestHandleQueueStatus_ByQueueID_NotFound verifies that status returns
// {queue: null} when no queue matches the given UUID.
func TestHandleQueueStatus_ByQueueID_NotFound(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)

	resp, rpcErr := queue.HandleQueueStatus(t.Context(), projectDir, queue.QueueStatusRequest{
		QueueID: "00000000-0000-7000-8000-000000000000",
	})
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus(unknown uuid): unexpected RPCError: %v", rpcErr)
	}
	if resp.Queue != nil {
		t.Errorf("Queue = %v, want nil for unknown queue_id", resp.Queue)
	}
}

// TestHandleQueueAppend_ByQueueID_NonMainQueue verifies that append resolves a
// non-main queue by UUID when --queue-id is given without --queue (hk-1k5as).
func TestHandleQueueAppend_ByQueueID_NonMainQueue(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc-abq-a"
	const beadB core.BeadID = "hk-rpc-abq-b"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	// Submit a stream group to a non-main named queue.
	submitResp, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "flywheel",
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadA)},
	}, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("submit flywheel: unexpected RPCError: %v", rpcErr)
	}

	// Append using only queue_id (no name) — previously this would fail with
	// queue_id_mismatch because it defaulted to loading "main" (hk-1k5as fix).
	appendResp, _, _, rpcErr2 := queue.HandleQueueAppend(t.Context(), queue.QueueAppendRequest{
		QueueID:    submitResp.QueueID,
		GroupIndex: 0,
		BeadIDs:    []core.BeadID{beadB},
	}, ledger, projectDir)
	if rpcErr2 != nil {
		t.Fatalf("HandleQueueAppend(queue_id only): unexpected RPCError: code=%d msg=%s",
			rpcErr2.Code, rpcErr2.Message)
	}
	if appendResp.AppendedCount != 1 {
		t.Errorf("AppendedCount = %d, want 1", appendResp.AppendedCount)
	}
}

// ---------------------------------------------------------------------------
// HandleQueueDryRun
// ---------------------------------------------------------------------------

// TestHandleQueueDryRun_HappyPath verifies that a valid dry-run request returns
// the resolved queue envelope without persisting queue.json.
func TestHandleQueueDryRun_HappyPath(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc050"
	const beadB core.BeadID = "hk-rpc051"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	req := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, beadA, beadB)},
	}

	resp, rpcErr := queue.HandleQueueDryRun(t.Context(), req, ledger, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueDryRun: unexpected RPCError: %v", rpcErr)
	}

	// ResolvedQueue must be a well-formed envelope with the correct group count.
	if len(resp.ResolvedQueue.Groups) != 1 {
		t.Errorf("ResolvedQueue.Groups count = %d, want 1", len(resp.ResolvedQueue.Groups))
	}
	if resp.ResolvedQueue.Status != queue.QueueStatusActive {
		t.Errorf("ResolvedQueue.Status = %q, want %q", resp.ResolvedQueue.Status, queue.QueueStatusActive)
	}
	if resp.ParallelismNarrowed {
		t.Errorf("ParallelismNarrowed = true, want false (no blocks edges)")
	}

	// queue.json MUST NOT be written (dry-run must not persist per QM-028).
	queueFile := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queueFile); statErr == nil {
		t.Error("queue.json written by dry-run, want no file (QM-028: dry-run must not persist)")
	}
}

// TestHandleQueueDryRun_ValidationError_BeadNotFound verifies that a dry-run
// with an unknown bead_id returns RPCError with code -32013 (bead_not_found).
func TestHandleQueueDryRun_ValidationError_BeadNotFound(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	// Empty ledger → no beads known → bead_not_found.
	ledger := rpcFixtureOpenLedger()

	req := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureWaveGroup(0, "hk-unknown")},
	}

	_, rpcErr := queue.HandleQueueDryRun(t.Context(), req, ledger, projectDir)
	if rpcErr == nil {
		t.Fatal("expected RPCError, got nil")
	}
	if rpcErr.Code != queue.ErrorCodeBeadNotFound {
		t.Errorf("RPCError.Code = %d, want %d (bead_not_found)",
			rpcErr.Code, queue.ErrorCodeBeadNotFound)
	}
}

// TestHandleQueueDryRun_NamedQueue_IgnoresMainActive is the regression test for
// hk-40r9b: dry-run targeting a non-main named queue must NOT be rejected with
// queue_already_active when the "main" queue is active.
//
// Pre-fix, HandleQueueDryRun always called Load(..., QueueNameMain), so an
// active "main" queue triggered QM-027 regardless of the requested name.
func TestHandleQueueDryRun_NamedQueue_IgnoresMainActive(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc052"
	const beadB core.BeadID = "hk-rpc053"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	// Establish an active "main" queue so QM-027 would falsely fire if the dry-run
	// checks the wrong per-name slot.
	mainReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadA)},
	}
	if _, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), mainReq, ledger, projectDir, 1); rpcErr != nil {
		t.Fatalf("setup: submit main queue: unexpected RPCError: %v", rpcErr)
	}

	// Dry-run targeting "extqueue" must succeed — "main" is a different per-name
	// slot and must not trigger QM-027 here.
	dryReq := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Name:          "extqueue",
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadB)},
	}
	resp, rpcErr := queue.HandleQueueDryRun(t.Context(), dryReq, ledger, projectDir)
	if rpcErr != nil {
		t.Fatalf("dry-run for extqueue with active main: unexpected RPCError (code=%d %s): want success",
			rpcErr.Code, rpcErr.Message)
	}

	// ResolvedQueue.Name must reflect the requested queue name.
	if resp.ResolvedQueue.Name != "extqueue" {
		t.Errorf("ResolvedQueue.Name = %q, want %q", resp.ResolvedQueue.Name, "extqueue")
	}

	// No file must have been written for "extqueue" (dry-run must not persist per QM-028).
	if _, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", "extqueue.json")); statErr == nil {
		t.Error("extqueue.json written by dry-run, want no file (QM-028: dry-run must not persist)")
	}
}

// TestHandleQueueDryRun_NamedQueue_AlreadyActive verifies that dry-run correctly
// rejects a submit when the targeted named queue itself is already active.
func TestHandleQueueDryRun_NamedQueue_AlreadyActive(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-rpc054"
	const beadB core.BeadID = "hk-rpc055"

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA, beadB)

	// Establish an active "extqueue" queue via a real submit.
	submitReq := queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          "extqueue",
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadA)},
	}
	if _, _, _, rpcErr := queue.HandleQueueSubmit(t.Context(), submitReq, ledger, projectDir, 1); rpcErr != nil {
		t.Fatalf("setup: submit extqueue: unexpected RPCError: %v", rpcErr)
	}

	// Dry-run targeting the same "extqueue" must be rejected with queue_already_active.
	dryReq := queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Name:          "extqueue",
		Groups:        []queue.Group{rpcFixtureStreamGroup(0, beadB)},
	}
	_, rpcErr := queue.HandleQueueDryRun(t.Context(), dryReq, ledger, projectDir)
	if rpcErr == nil {
		t.Fatal("dry-run for active extqueue: expected RPCError, got nil")
	}
	if rpcErr.Code != queue.ErrorCodeQueueAlreadyActive {
		t.Errorf("RPCError.Code = %d, want %d (queue_already_active)",
			rpcErr.Code, queue.ErrorCodeQueueAlreadyActive)
	}
}

// ---------------------------------------------------------------------------
// HandlerAdapter round-trip (JSON encode/decode)
// ---------------------------------------------------------------------------

// TestHandlerAdapter_QueueStatus_RoundTrip verifies that HandlerAdapter.HandleQueueStatus
// returns a JSON-encoded QueueStatusResponse that can be decoded back.
func TestHandlerAdapter_QueueStatus_RoundTrip(t *testing.T) {
	t.Parallel()

	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger()
	adapter := queue.NewHandlerAdapter(ledger, projectDir, nil, nil)

	raw, rpcErr := adapter.HandleQueueStatus(t.Context(), nil)
	if rpcErr != nil {
		t.Fatalf("HandleQueueStatus: unexpected RPCError: %v", rpcErr)
	}
	if raw == nil {
		t.Fatal("HandleQueueStatus: nil JSON result")
	}

	// Decode into QueueStatusResponse to verify shape.
	var statusResp queue.QueueStatusResponse
	if err := json.Unmarshal(raw, &statusResp); err != nil {
		t.Fatalf("decode QueueStatusResponse: %v", err)
	}
	if statusResp.Queue != nil {
		t.Errorf("Queue = %v, want nil (no queue loaded)", statusResp.Queue)
	}
}
