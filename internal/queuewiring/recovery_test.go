package queuewiring

// recovery_test.go — claims defended for QueueStore.RecoverFailed (QM-052b).
//
// Each test name states the claim it defends. The claims are:
//   - a paused-by-failure queue comes back active with its failed items re-armed
//   - each refusal path returns its own typed reason and wire code
//   - the ledger preflight runs BEFORE any mutation, so a refused preflight
//     leaves the queue exactly as it was

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// stubRecoveryLedger answers LookupStatus from a fixed map. A bead absent from
// the map answers with err when err is non-nil, otherwise BeadStatusNotFound.
type stubRecoveryLedger struct {
	status map[core.BeadID]queue.BeadStatus
	err    error
}

func (s stubRecoveryLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if s.err != nil {
		return "", s.err
	}
	if got, ok := s.status[id]; ok {
		return got, nil
	}
	return queue.BeadStatusNotFound, nil
}

// failureParkedFixture builds a store holding one queue parked at
// paused-by-failure with one failed item and one completed item, plus a project
// dir whose canonical queue file matches.
func failureParkedFixture(t *testing.T) (store *QueueStore, projectDir string) {
	t.Helper()
	projectDir = t.TempDir()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0100",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusPausedByFailure,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusCompleteWithFailures,
			CreatedAt:  time.Unix(0, 0).UTC(),
			Items: []queue.Item{
				{BeadID: "hk-done", Status: queue.ItemStatusCompleted},
				{
					BeadID:            "hk-broke",
					Status:            queue.ItemStatusFailed,
					Attempts:          queue.MaxItemAttempts,
					LastFailureReason: "max_attempts_exceeded",
				},
			},
		}},
	}
	store = NewQueueStore()
	store.SetQueueByName(queue.QueueNameMain, q)

	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projectDir, ".harmonik", "queues", queue.QueueNameMain+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return store, projectDir
}

// recoveryFacts carries the context fields of a refusal. It is a plain value,
// not an error, so a test that only cares about the reason may ignore it.
type recoveryFacts struct {
	ObservedStatus queue.QueueStatus
	BeadID         core.BeadID
	BeadStatus     queue.BeadStatus
}

// requireRecoveryReason asserts err is a *queue.RecoveryError carrying want,
// and that its wire code is the code allocated to want.
func requireRecoveryReason(t *testing.T, err error, want queue.RecoveryReason, wantCode int) recoveryFacts {
	t.Helper()
	var rec *queue.RecoveryError
	if !errors.As(err, &rec) {
		t.Fatalf("want *queue.RecoveryError, got %T: %v", err, err)
	}
	if rec.Reason != want {
		t.Fatalf("reason = %q, want %q", rec.Reason, want)
	}
	if rec.Code() != wantCode {
		t.Fatalf("code = %d, want %d", rec.Code(), wantCode)
	}
	return recoveryFacts{ObservedStatus: rec.ObservedStatus, BeadID: rec.BeadID, BeadStatus: rec.BeadStatus}
}

func TestRecoverFailed_ReturnsQueueToActiveAndRearmsFailedItems(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	outcome, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	})
	if err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}
	if len(outcome.Rearmed) != 1 || outcome.Rearmed[0] != "hk-broke" {
		t.Fatalf("rearmed = %v, want [hk-broke]", outcome.Rearmed)
	}

	got := store.QueueByName(queue.QueueNameMain)
	if got.Status != queue.QueueStatusActive {
		t.Fatalf("queue status = %q, want %q", got.Status, queue.QueueStatusActive)
	}
	if got.Groups[0].Status != queue.GroupStatusActive {
		t.Fatalf("group status = %q, want %q", got.Groups[0].Status, queue.GroupStatusActive)
	}
	broke := got.Groups[0].Items[1]
	if broke.Status != queue.ItemStatusPending {
		t.Fatalf("failed item status = %q, want %q", broke.Status, queue.ItemStatusPending)
	}
	if broke.Attempts != 0 {
		t.Fatalf("failed item attempts = %d, want 0", broke.Attempts)
	}
	if broke.LastFailureReason != "" {
		t.Fatalf("failed item last_failure_reason = %q, want empty", broke.LastFailureReason)
	}
	if done := got.Groups[0].Items[0]; done.Status != queue.ItemStatusCompleted {
		t.Fatalf("completed item was disturbed: status = %q", done.Status)
	}
}

func TestRecoverFailed_PersistsTheRecoveredQueueToDisk(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	if _, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	}); err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}

	canonical := filepath.Join(projectDir, ".harmonik", "queues", queue.QueueNameMain+".json")
	raw, err := os.ReadFile(canonical) //nolint:gosec // the path is built from t.TempDir()
	if err != nil {
		t.Fatalf("read canonical queue: %v", err)
	}
	var onDisk queue.Queue
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal canonical queue: %v", err)
	}
	if onDisk.Status != queue.QueueStatusActive {
		t.Fatalf("on-disk status = %q, want %q — recovery returned before it was durable",
			onDisk.Status, queue.QueueStatusActive)
	}
}

func TestRecoverFailed_RefusesMissingQueueWithQueueNotFound(t *testing.T) {
	t.Parallel()
	store := NewQueueStore()

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: t.TempDir(),
		Name:       "no-such-queue",
	})
	requireRecoveryReason(t, err, queue.RecoveryReasonQueueNotFound, -32030)
}

func TestRecoverFailed_RefusesNonFailurePauseWithQueueNotRecoverable(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)
	active := store.QueueByName(queue.QueueNameMain)
	active.Status = queue.QueueStatusPausedByDrain
	store.SetQueueByName(queue.QueueNameMain, active)

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
	})
	facts := requireRecoveryReason(t, err, queue.RecoveryReasonQueueNotRecoverable, -32031)
	if facts.ObservedStatus != queue.QueueStatusPausedByDrain {
		t.Fatalf("observed status = %q, want %q", facts.ObservedStatus, queue.QueueStatusPausedByDrain)
	}
}

func TestRecoverFailed_RefusesNonOpenBeadBeforeTouchingTheQueue(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusInProgress}},
	})
	facts := requireRecoveryReason(t, err, queue.RecoveryReasonBeadNotOpen, -32033)
	if facts.BeadID != "hk-broke" {
		t.Fatalf("bead id = %q, want hk-broke", facts.BeadID)
	}
	if facts.BeadStatus != queue.BeadStatusInProgress {
		t.Fatalf("bead status = %q, want %q", facts.BeadStatus, queue.BeadStatusInProgress)
	}

	// The preflight runs before any mutation, so the park must be intact.
	got := store.QueueByName(queue.QueueNameMain)
	if got.Status != queue.QueueStatusPausedByFailure {
		t.Fatalf("queue status = %q, want it left at %q", got.Status, queue.QueueStatusPausedByFailure)
	}
	if got.Groups[0].Items[1].Status != queue.ItemStatusFailed {
		t.Fatalf("failed item was re-armed by a refused recovery: %q", got.Groups[0].Items[1].Status)
	}
}

func TestRecoverFailed_RefusesLedgerReadErrorWithRecoveryReadFailed(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{err: errors.New("br unreachable")},
	})
	requireRecoveryReason(t, err, queue.RecoveryReasonReadFailed, -32034)
}

func TestRecoverFailed_RefusesUnwritableProjectDirWithRecoveryWriteFailed(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	// Make the queues directory unwritable so the atomic replacement fails at
	// I/O rather than being refused before it.
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // a directory needs the execute bit to stay traversable
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(queuesDir, 0o700) }) //nolint:errcheck,gosec // best-effort restore of a temp dir

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	})
	requireRecoveryReason(t, err, queue.RecoveryReasonWriteFailed, -32035)

	if got := store.QueueByName(queue.QueueNameMain); got.Status != queue.QueueStatusPausedByFailure {
		t.Fatalf("queue status = %q after a failed write, want it left at %q",
			got.Status, queue.QueueStatusPausedByFailure)
	}
}

// quarantineByFailedWrite parks a real QM-001 quarantine on the fixture queue
// by letting one recovery write fail against a read-only queues directory, then
// repairs the directory. It returns with the queue quarantined and still parked
// at paused-by-failure.
//
// It seeds the quarantine through the production write path on purpose. Writing
// the quarantine map directly would prove the refusal branch and nothing about
// whether a failed write reaches it.
func quarantineByFailedWrite(t *testing.T, store *QueueStore, projectDir string) {
	t.Helper()
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // a directory needs the execute bit to stay traversable
		t.Fatalf("chmod queues dir: %v", err)
	}
	if _, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir, Name: queue.QueueNameMain,
	}); err == nil {
		t.Fatal("seed write was expected to fail")
	}
	if err := os.Chmod(queuesDir, 0o700); err != nil { //nolint:gosec // a directory needs the execute bit to stay traversable
		t.Fatalf("restore queues dir: %v", err)
	}
	if store.QuarantineReason(queue.QueueNameMain) == nil {
		t.Fatal("the failed write left no quarantine; the rest of this test would prove nothing")
	}
}

// QM-059: a quarantine clears only through an explicit durable recovery
// classification, and failed recovery is the one operation allowed to act on a
// quarantined queue that is parked at paused-by-failure. Without this the
// queue has no exit at all: every ordinary transaction is refused and no
// operator command can reopen it short of restarting the daemon.
func TestRecoverFailed_ClearsAnIOQuarantineOnTheQueueItRecovers(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)
	quarantineByFailedWrite(t, store, projectDir)

	outcome, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	})
	if err != nil {
		t.Fatalf("RecoverFailed over a quarantined paused-by-failure queue: %v", err)
	}
	if outcome.Receipt.ReceiptID == "" {
		t.Fatal("recovery reported success with no receipt ID")
	}
	if reason := store.QuarantineReason(queue.QueueNameMain); reason != nil {
		t.Fatalf("QuarantineReason = %v after a durable recovery, want nil", reason)
	}
	if got := store.QueueByName(queue.QueueNameMain); got.Status != queue.QueueStatusActive {
		t.Fatalf("queue status = %q, want %q", got.Status, queue.QueueStatusActive)
	}
}

// The other half of QM-059: the permission is scoped to paused-by-failure. A
// quarantined queue in any other status is still refused, and it is refused
// with its own reason rather than the generic not-recoverable one, so an
// operator can tell "wrong status" from "this queue is shut".
func TestRecoverFailed_RefusesAQuarantinedQueueThatIsNotPausedByFailure(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)
	quarantineByFailedWrite(t, store, projectDir)

	drained := store.QueueByName(queue.QueueNameMain)
	drained.Status = queue.QueueStatusPausedByDrain
	store.queueMu.Lock()
	store.queues[queue.QueueNameMain] = drained
	store.generations[queue.QueueNameMain]++
	store.queueMu.Unlock()

	_, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	})
	requireRecoveryReason(t, err, queue.RecoveryReasonQueueQuarantined, -32032)
}

// QM-058a: repeating the request reads back the receipt already named by the
// queue. It mints nothing and re-arms nothing a second time. An operator who
// runs the command twice must not get a second recovery of items that another
// pass has since dispatched.
func TestRecoverFailed_RepeatingTheRequestReturnsTheSameReceiptAndMutatesNothing(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)
	beads := stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}}

	first, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir, Name: queue.QueueNameMain, Beads: beads,
	})
	if err != nil {
		t.Fatalf("first RecoverFailed: %v", err)
	}
	if first.AlreadyRecovered {
		t.Fatal("the first recovery reported itself as already recovered")
	}

	second, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir, Name: queue.QueueNameMain, Beads: beads,
	})
	if err != nil {
		t.Fatalf("second RecoverFailed: %v", err)
	}
	if !second.AlreadyRecovered {
		t.Fatal("the second recovery did not report itself as a repeat")
	}
	if second.Receipt.ReceiptID != first.Receipt.ReceiptID {
		t.Fatalf("second receipt ID = %q, want the first receipt %q — a repeat minted a new recovery",
			second.Receipt.ReceiptID, first.Receipt.ReceiptID)
	}
}

// The receipt is what makes a recovery re-checkable after the daemon is gone.
// It has to be a file on disk, named by the recovered queue, and it has to name
// the items that were re-armed. A receipt that is only an in-memory value
// proves nothing after a restart.
func TestRecoverFailed_WritesTheReceiptToDiskNamedByTheRecoveredQueue(t *testing.T) {
	t.Parallel()
	store, projectDir := failureParkedFixture(t)

	outcome, err := store.RecoverFailed(context.Background(), FailedRecoveryRequest{
		ProjectDir: projectDir,
		Name:       queue.QueueNameMain,
		Beads:      stubRecoveryLedger{status: map[core.BeadID]queue.BeadStatus{"hk-broke": queue.BeadStatusOpen}},
	})
	if err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}

	recovered := store.QueueByName(queue.QueueNameMain)
	if recovered.FailedRecoveryReceiptID == nil {
		t.Fatal("the recovered queue names no receipt ID; nothing later can find its receipt")
	}
	if *recovered.FailedRecoveryReceiptID != outcome.Receipt.ReceiptID {
		t.Fatalf("queue receipt ID = %q, outcome receipt ID = %q — they must be the same receipt",
			*recovered.FailedRecoveryReceiptID, outcome.Receipt.ReceiptID)
	}

	onDisk, _, readErr := queue.ReadFailedRecoveryReceipt(projectDir, *recovered)
	if readErr != nil {
		t.Fatalf("ReadFailedRecoveryReceipt: %v", readErr)
	}
	if onDisk.ReceiptID != outcome.Receipt.ReceiptID {
		t.Fatalf("on-disk receipt ID = %q, want %q", onDisk.ReceiptID, outcome.Receipt.ReceiptID)
	}
	if len(onDisk.RecoveredItems) != 1 || onDisk.RecoveredItems[0].BeadID != "hk-broke" {
		t.Fatalf("on-disk recovered items = %v, want the one re-armed bead", onDisk.RecoveredItems)
	}
}
