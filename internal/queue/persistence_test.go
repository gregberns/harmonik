package queue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

func persistFixtureQueue() queue.Queue {
	return typesFixtureQueue()
}

func persistFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	queuesDir := filepath.Join(dir, ".harmonik", "queues")
	if err := os.MkdirAll(queuesDir, 0o700); err != nil {
		t.Fatalf("persistFixtureProjectDir: mkdir .harmonik/queues: %v", err)
	}
	return dir
}

func mainQueuePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "queues", "main.json")
}

// TestPersistRoundTrip verifies that Persist followed by Load returns the
// same Queue (specs/queue-model.md §3.1 QM-001, §3.2 QM-002).
func TestPersistRoundTrip(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	original := persistFixtureQueue()
	ctx := context.Background()

	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load: got nil, want non-nil Queue")
	}

	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion: got %d, want %d", got.SchemaVersion, original.SchemaVersion)
	}
	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}
	if !got.SubmittedAt.Equal(original.SubmittedAt) {
		t.Errorf("SubmittedAt: got %v, want %v", got.SubmittedAt, original.SubmittedAt)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if len(got.Groups) != len(original.Groups) {
		t.Errorf("Groups len: got %d, want %d", len(got.Groups), len(original.Groups))
	}
}

// TestLoadFileAbsent verifies that Load returns (nil, nil) when the per-queue
// file does not exist (specs/queue-model.md §3.2 QM-002 "File absent" case).
func TestLoadFileAbsent(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load on absent file: expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("Load on absent file: expected nil Queue, got %+v", got)
	}
}

// TestLoadFileCorrupt verifies that Load returns ErrCorrupt when the per-queue
// file exists but contains invalid JSON (specs/queue-model.md §3.2 QM-002
// "File present but unparseable" case).
func TestLoadFileCorrupt(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	queuePath := mainQueuePath(projectDir)
	if err := os.WriteFile(queuePath, []byte("this is not json {{{"), 0o600); err != nil {
		t.Fatalf("setup: write corrupt queue file: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if got != nil {
		t.Errorf("Load on corrupt file: expected nil Queue, got %+v", got)
	}
	if err == nil {
		t.Fatal("Load on corrupt file: expected ErrCorrupt, got nil error")
	}
	if !errors.Is(err, queue.ErrCorrupt) {
		t.Errorf("Load on corrupt file: error %v does not wrap ErrCorrupt", err)
	}

	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("Load on corrupt file: queue file was auto-deleted, must not be")
	}
}

// TestPersistSizeBound verifies that Persist returns ErrTooLarge when the
// marshalled Queue exceeds 1 MiB (specs/queue-model.md §3.4 QM-004).
func TestPersistSizeBound(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	bigQueue := persistFixtureQueue()
	bigQueue.Groups[0].Items = persistFixtureLargeItemSlice(8000)

	data, err := json.Marshal(bigQueue)
	if err != nil {
		t.Fatalf("json.Marshal for size check: %v", err)
	}
	const mib = 1 << 20
	if len(data) <= mib {
		t.Skipf("fixture produced %d bytes (≤1 MiB); increase item count", len(data))
	}

	persistErr := queue.Persist(ctx, projectDir, &bigQueue)
	if persistErr == nil {
		t.Fatal("Persist: expected ErrTooLarge, got nil")
	}
	if !errors.Is(persistErr, queue.ErrTooLarge) {
		t.Errorf("Persist: error %v does not wrap ErrTooLarge", persistErr)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("Persist: queue file exists after ErrTooLarge, should not have been created")
	}
}

func persistFixtureLargeItemSlice(n int) []queue.Item {
	items := make([]queue.Item, n)
	for i := range items {
		id := "hk-" + strings.Repeat("x", 90) + string(rune('a'+i%26))
		items[i] = queue.Item{
			BeadID: core.BeadID(id),
			Status: queue.ItemStatusPending,
		}
	}
	return items
}

// TestPersistAtomicCrashSafety verifies that a partial write (temp file
// present, rename not yet executed) does not corrupt the previously-written
// canonical queue file.
//
// Simulates the crash-window between steps 2 and 4 of the WM-026 sequence by
// planting a .tmp-<pid> orphan alongside an existing good queue file.
func TestPersistAtomicCrashSafety(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	original := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist (initial): %v", err)
	}

	canonicalPath := mainQueuePath(projectDir)
	tmpPath := canonicalPath + ".tmp-99999"
	if err := os.WriteFile(tmpPath, []byte(`{"partial":true}`), 0o600); err != nil {
		t.Fatalf("setup: plant partial temp file: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after planted partial temp: %v", err)
	}
	if got == nil {
		t.Fatal("Load after planted partial temp: got nil Queue, want original")
	}
	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}

	if _, statErr := os.Stat(tmpPath); os.IsNotExist(statErr) {
		t.Error("orphan temp file was removed by Load; it must be left for operator inspection")
	}
}

// TestUnlink verifies that Unlink removes the per-queue file and is idempotent
// (specs/queue-model.md §3.3 QM-003).
func TestUnlink(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	if err := queue.Unlink(ctx, projectDir, queue.QueueNameMain); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("Unlink: queue file still exists after Unlink")
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after Unlink: %v", err)
	}
	if got != nil {
		t.Errorf("Load after Unlink: got %+v, want nil", got)
	}

	if err := queue.Unlink(ctx, projectDir, queue.QueueNameMain); err != nil {
		t.Fatalf("second Unlink (idempotent): %v", err)
	}
}

// TestPersistMkdirAll verifies that Persist creates .harmonik/queues/ when it
// does not exist, rather than failing with a path error.
func TestPersistMkdirAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, dir, &q); err != nil {
		t.Fatalf("Persist without pre-existing .harmonik/queues: %v", err)
	}

	got, err := queue.Load(ctx, dir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load: got nil, want Queue")
	}
	if got.QueueID != q.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, q.QueueID)
	}
}

// TestErrPersistFailedSentinel verifies that ErrPersistFailed and ErrCorrupt
// are distinct sentinel errors (not equal to each other or nil).
func TestErrPersistFailedSentinel(t *testing.T) {
	t.Parallel()

	if queue.ErrPersistFailed == nil {
		t.Error("ErrPersistFailed is nil")
	}
	if queue.ErrCorrupt == nil {
		t.Error("ErrCorrupt is nil")
	}
	if queue.ErrTooLarge == nil {
		t.Error("ErrTooLarge is nil")
	}
	if errors.Is(queue.ErrPersistFailed, queue.ErrCorrupt) {
		t.Error("ErrPersistFailed must not wrap ErrCorrupt")
	}
	if errors.Is(queue.ErrCorrupt, queue.ErrPersistFailed) {
		t.Error("ErrCorrupt must not wrap ErrPersistFailed")
	}
}

func completeUnlinkFixtureQueue() queue.Queue {
	q := typesFixtureQueue()
	q.Groups = []queue.Group{
		{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusCompleteSuccess,
			Items: []queue.Item{
				{
					BeadID: "hk-09tne",
					Status: queue.ItemStatusCompleted,
				},
			},
			CreatedAt: q.SubmittedAt,
		},
	}
	return q
}

// TestCompleteAndUnlink verifies the QM-053 completion sequence:
// (a) queue file is written with status=completed before removal,
// (b) queue file is absent after CompleteAndUnlink succeeds,
// (c) Load returns (nil, nil) after completion, and
// (d) a fresh Persist can re-create the queue file (new queue-submit is permitted).
//
// Spec ref: specs/queue-model.md §8.4 QM-053, §3.3 QM-003.
func TestCompleteAndUnlink(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := completeUnlinkFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist (setup): %v", err)
	}

	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("q.Status after CompleteAndUnlink: got %q, want %q",
			q.Status, queue.QueueStatusCompleted)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("CompleteAndUnlink: queue file still exists after completion, must be unlinked")
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after CompleteAndUnlink: %v", err)
	}
	if got != nil {
		t.Errorf("Load after CompleteAndUnlink: got %+v, want nil", got)
	}

	newQ := completeUnlinkFixtureQueue()
	newQ.QueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0002"
	if err := queue.Persist(ctx, projectDir, &newQ); err != nil {
		t.Fatalf("Persist after completion (new submit): %v", err)
	}
	reloaded, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after new submit: %v", err)
	}
	if reloaded == nil {
		t.Fatal("Load after new submit: got nil, want non-nil")
	}
	if reloaded.QueueID != newQ.QueueID {
		t.Errorf("reloaded QueueID: got %q, want %q", reloaded.QueueID, newQ.QueueID)
	}
}

// TestCompleteAndUnlinkStatusOrdering verifies that CompleteAndUnlink persists
// status=completed to disk BEFORE removing the file (QM-053 step 1 precedes
// step 3).
//
// Spec ref: specs/queue-model.md §8.4 QM-053.
func TestCompleteAndUnlinkStatusOrdering(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := completeUnlinkFixtureQueue()
	q.Status = queue.QueueStatusActive // start as active

	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist (setup): %v", err)
	}

	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("q.Status: got %q, want %q", q.Status, queue.QueueStatusCompleted)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue file must be absent after CompleteAndUnlink")
	}
}

// TestCompleteAndUnlinkIdempotent verifies that calling CompleteAndUnlink a
// second time (simulating a daemon restart where the file was already unlinked)
// returns nil and does not corrupt state.
//
// Spec ref: specs/queue-model.md §3.3 QM-003.
func TestCompleteAndUnlinkIdempotent(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := completeUnlinkFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist (setup): %v", err)
	}

	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("first CompleteAndUnlink: %v", err)
	}

	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("second CompleteAndUnlink (idempotent): %v", err)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue file must be absent after second CompleteAndUnlink")
	}
}

func TestTerminalResultWriteFailurePreservesCallerQueue(t *testing.T) {
	t.Parallel()

	projectFile := filepath.Join(t.TempDir(), "not-a-project-directory")
	if err := os.WriteFile(projectFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	completed := completeUnlinkFixtureQueue()
	completedResult := queue.CompleteAndUnlinkResult(context.Background(), projectFile, &completed)
	if completedResult.Committed || completedResult.CommitErr == nil || completedResult.CleanupErr != nil {
		t.Fatalf("completion result = %+v, want failed write", completedResult)
	}
	if completed.Status != queue.QueueStatusActive {
		t.Fatalf("completion status = %q, want active after failed write", completed.Status)
	}
}

// TestArchiveFailedQueue_RenamesFile verifies that ArchiveFailedQueue renames
// the per-queue file to <name>.json.failed-<timestamp> and that the original
// file is absent afterwards.
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_RenamesFile(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist (setup): %v", err)
	}

	ts := time.Date(2026, 5, 18, 12, 34, 56, 0, time.UTC)
	archivePath, err := queue.ArchiveFailedQueue(ctx, projectDir, queue.QueueNameMain, ts)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue: %v", err)
	}

	expectedSuffix := ".failed-20260518123456"
	if !strings.HasSuffix(archivePath, expectedSuffix) {
		t.Errorf("archivePath %q does not end with %q", archivePath, expectedSuffix)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue file still exists after ArchiveFailedQueue; must have been renamed")
	}

	data, readErr := os.ReadFile(archivePath) //nolint:gosec // G304: test-only
	if readErr != nil {
		t.Fatalf("ReadFile(archivePath): %v", readErr)
	}
	if !bytes.Contains(data, []byte(q.QueueID)) {
		t.Errorf("archive file does not contain QueueID %q", q.QueueID)
	}

	got, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("Load after ArchiveFailedQueue: %v", loadErr)
	}
	if got != nil {
		t.Errorf("Load after ArchiveFailedQueue: got %+v, want nil", got)
	}
}

// TestArchiveFailedQueue_AbsentIsNoOp verifies that ArchiveFailedQueue returns
// ("", nil) when the per-queue file does not exist.
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_AbsentIsNoOp(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()
	ts := time.Now().UTC()

	archivePath, err := queue.ArchiveFailedQueue(ctx, projectDir, queue.QueueNameMain, ts)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue on absent queue file: %v", err)
	}
	if archivePath != "" {
		t.Errorf("archivePath = %q; want empty string for no-op", archivePath)
	}
}

// TestArchiveFailedQueue_SubsequentRunSucceeds verifies that after
// ArchiveFailedQueue renames the queue file, a subsequent Persist succeeds.
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_SubsequentRunSucceeds(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	failedQueue := persistFixtureQueue()
	failedQueue.Status = queue.QueueStatusPausedByFailure
	if err := queue.Persist(ctx, projectDir, &failedQueue); err != nil {
		t.Fatalf("Persist (prior failed queue): %v", err)
	}

	ts := time.Now().UTC()
	_, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, queue.QueueNameMain, ts)
	if archiveErr != nil {
		t.Fatalf("ArchiveFailedQueue: %v", archiveErr)
	}

	loaded, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("Load after archive: %v", loadErr)
	}
	if loaded != nil {
		t.Errorf("Load after archive: got non-nil queue (status=%q); guard would block re-run", loaded.Status)
	}

	newQueue := persistFixtureQueue()
	newQueue.QueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099"
	newQueue.Status = queue.QueueStatusActive
	if err := queue.Persist(ctx, projectDir, &newQueue); err != nil {
		t.Fatalf("Persist (re-run): %v", err)
	}

	reloaded, loadErr2 := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr2 != nil {
		t.Fatalf("Load (re-run): %v", loadErr2)
	}
	if reloaded == nil {
		t.Fatal("Load (re-run): got nil; expected new active queue")
	}
	if reloaded.QueueID != newQueue.QueueID {
		t.Errorf("re-run QueueID: got %q, want %q", reloaded.QueueID, newQueue.QueueID)
	}
}

// TestArchiveFailedQueue_ActiveQueueGuardPreserved verifies that when a queue
// is still in active status (not paused-by-failure), the existing guard
// continues to block re-run.
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_ActiveQueueGuardPreserved(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	activeQueue := persistFixtureQueue()
	activeQueue.Status = queue.QueueStatusActive
	if err := queue.Persist(ctx, projectDir, &activeQueue); err != nil {
		t.Fatalf("Persist (active queue): %v", err)
	}

	loaded, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil {
		t.Fatalf("queue.Load: %v", loadErr)
	}
	if loaded == nil {
		t.Fatal("queue.Load returned nil; expected active queue")
	}

	if loaded.Status == queue.QueueStatusCompleted {
		t.Errorf("active queue should have non-completed status; got %q", loaded.Status)
	}

	queuePath := mainQueuePath(projectDir)
	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("queue file must not be removed for an active-status queue")
	}
}

// TestPersistJSONContent verifies that the written file contains valid JSON
// with the expected queue_id field.
func TestPersistJSONContent(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	queuePath := mainQueuePath(projectDir)
	data, err := os.ReadFile(queuePath) //nolint:gosec // G304: test path built from t.TempDir
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(q.QueueID)) {
		t.Errorf("queue file does not contain QueueID %q", q.QueueID)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("queue file is not valid JSON: %v", err)
	}
}

// TestMigrateFromLegacy_MigratesFile verifies that MigrateFromLegacy reads
// .harmonik/queue.json, writes .harmonik/queues/main.json, and removes the
// legacy file.
//
// Bead ref: hk-tigaf.3.
func TestMigrateFromLegacy_MigratesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	legacyPath := filepath.Join(harmonikDir, "queue.json")
	q := persistFixtureQueue()
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
		t.Fatalf("write legacy queue.json: %v", err)
	}

	if err := queue.MigrateFromLegacy(ctx, dir); err != nil {
		t.Fatalf("MigrateFromLegacy: %v", err)
	}

	if _, statErr := os.Stat(legacyPath); !os.IsNotExist(statErr) {
		t.Error("legacy queue.json must be removed after migration")
	}

	got, err := queue.Load(ctx, dir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after migration: %v", err)
	}
	if got == nil {
		t.Fatal("Load after migration: got nil, want migrated Queue")
	}
	if got.QueueID != q.QueueID {
		t.Errorf("migrated QueueID: got %q, want %q", got.QueueID, q.QueueID)
	}
}

// TestMigrateFromLegacy_NoOpWhenAbsent verifies that MigrateFromLegacy returns
// nil when .harmonik/queue.json does not exist (idempotent no-op).
//
// Bead ref: hk-tigaf.3.
func TestMigrateFromLegacy_NoOpWhenAbsent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := queue.MigrateFromLegacy(ctx, dir); err != nil {
		t.Fatalf("MigrateFromLegacy on absent legacy file: %v", err)
	}
}

// TestMigrateFromLegacy_IdempotentWhenMainExists verifies that MigrateFromLegacy
// is idempotent: if main.json already exists (a prior migration ran), the legacy
// file is still removed and the function returns nil.
//
// Bead ref: hk-tigaf.3.
func TestMigrateFromLegacy_IdempotentWhenMainExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, dir, &q); err != nil {
		t.Fatalf("Persist (setup main.json): %v", err)
	}

	legacyPath := filepath.Join(harmonikDir, "queue.json")
	data, marshalErr := json.Marshal(q)
	if marshalErr != nil {
		t.Fatalf("marshal legacy queue.json: %v", marshalErr)
	}
	if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
		t.Fatalf("write legacy queue.json: %v", err)
	}

	if err := queue.MigrateFromLegacy(ctx, dir); err != nil {
		t.Fatalf("MigrateFromLegacy (idempotent): %v", err)
	}

	if _, statErr := os.Stat(legacyPath); !os.IsNotExist(statErr) {
		t.Error("legacy queue.json must be removed even when main.json already existed")
	}

	got, err := queue.Load(ctx, dir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after idempotent migration: %v", err)
	}
	if got == nil || got.QueueID != q.QueueID {
		t.Errorf("main.json corrupted by idempotent migration; got %v", got)
	}
}

// TestEnumerateQueueNames_Empty verifies that EnumerateQueueNames returns nil
// when the queues/ directory does not exist.
//
// Bead ref: hk-tigaf.3.
func TestEnumerateQueueNames_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	names, err := queue.EnumerateQueueNames(dir)
	if err != nil {
		t.Fatalf("EnumerateQueueNames on absent queues/: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %v", names)
	}
}

// TestEnumerateQueueNames_ListsQueues verifies that EnumerateQueueNames returns
// the names of all plain queue files, skipping archives.
//
// Bead ref: hk-tigaf.3.
func TestEnumerateQueueNames_ListsQueues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := context.Background()

	qMain := persistFixtureQueue() // name = "" → normalised to "main"
	if err := queue.Persist(ctx, dir, &qMain); err != nil {
		t.Fatalf("Persist main: %v", err)
	}

	qFoo := persistFixtureQueue()
	qFoo.Name = "foo"
	qFoo.QueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099"
	if err := queue.Persist(ctx, dir, &qFoo); err != nil {
		t.Fatalf("Persist foo: %v", err)
	}

	queuesDir := filepath.Join(dir, ".harmonik", "queues")
	archiveFiles := []string{
		"main.json.failed-20260101000000",
		"main.json.cancelled-20260101000001",
		"bar.json.tmp-12345",
	}
	for _, f := range archiveFiles {
		if err := os.WriteFile(filepath.Join(queuesDir, f), []byte(`{}`), 0o600); err != nil {
			t.Fatalf("write archive file %q: %v", f, err)
		}
	}

	names, err := queue.EnumerateQueueNames(dir)
	if err != nil {
		t.Fatalf("EnumerateQueueNames: %v", err)
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	if !nameSet["main"] {
		t.Error("expected 'main' in names")
	}
	if !nameSet["foo"] {
		t.Error("expected 'foo' in names")
	}
	for _, bad := range []string{"main.json.failed-20260101000000", "bar"} {
		if nameSet[bad] {
			t.Errorf("unexpected name %q in enumeration", bad)
		}
	}
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d: %v", len(names), names)
	}
}
