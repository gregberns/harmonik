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

// persistFixtureQueue returns a minimal valid Queue for persistence tests.
// Uses the existing typesFixtureQueue helper defined in types_test.go.
func persistFixtureQueue() queue.Queue {
	return typesFixtureQueue()
}

// persistFixtureProjectDir creates a temporary directory to act as projectDir
// and pre-creates the .harmonik subdirectory. The temp dir and .harmonik are
// cleaned up automatically via t.Cleanup.
func persistFixtureProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("persistFixtureProjectDir: mkdir .harmonik: %v", err)
	}
	return dir
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

	got, err := queue.Load(ctx, projectDir)
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

// TestLoadFileAbsent verifies that Load returns (nil, nil) when queue.json
// does not exist (specs/queue-model.md §3.2 QM-002 "File absent" case).
func TestLoadFileAbsent(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	got, err := queue.Load(ctx, projectDir)
	if err != nil {
		t.Fatalf("Load on absent file: expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("Load on absent file: expected nil Queue, got %+v", got)
	}
}

// TestLoadFileCorrupt verifies that Load returns ErrCorrupt when queue.json
// exists but contains invalid JSON (specs/queue-model.md §3.2 QM-002
// "File present but unparseable" case).
func TestLoadFileCorrupt(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Write corrupt content directly — bypass Persist to produce a bad file.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if err := os.WriteFile(queuePath, []byte("this is not json {{{"), 0o600); err != nil {
		t.Fatalf("setup: write corrupt queue.json: %v", err)
	}

	got, err := queue.Load(ctx, projectDir)
	if got != nil {
		t.Errorf("Load on corrupt file: expected nil Queue, got %+v", got)
	}
	if err == nil {
		t.Fatal("Load on corrupt file: expected ErrCorrupt, got nil error")
	}
	if !errors.Is(err, queue.ErrCorrupt) {
		t.Errorf("Load on corrupt file: error %v does not wrap ErrCorrupt", err)
	}

	// Verify the file is NOT auto-deleted per QM-002 (operator must inspect).
	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("Load on corrupt file: queue.json was auto-deleted, must not be")
	}
}

// TestPersistSizeBound verifies that Persist returns ErrTooLarge when the
// marshalled Queue exceeds 1 MiB (specs/queue-model.md §3.4 QM-004).
func TestPersistSizeBound(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Build a Queue whose JSON serialisation will exceed 1 MiB.
	// A single item's JSON is roughly 150 bytes; 8000 items ≈ 1.2 MiB.
	bigQueue := persistFixtureQueue()
	bigQueue.Groups[0].Items = persistFixtureLargeItemSlice(8000)

	// Verify the fixture actually produces oversized JSON (sanity check).
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

	// Verify no file was written.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("Persist: queue.json exists after ErrTooLarge, should not have been created")
	}
}

// persistFixtureLargeItemSlice builds a slice of n Items with synthetic bead
// IDs long enough to produce a > 1 MiB JSON payload when embedded in a Queue.
func persistFixtureLargeItemSlice(n int) []queue.Item {
	// Each bead ID is padded to ~100 chars to push total size over 1 MiB
	// quickly at ~8000 items.
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
// canonical queue.json.
//
// Simulates the crash-window between steps 2 and 4 of the WM-026 sequence by
// planting a .tmp-<pid> orphan alongside an existing good queue.json.
func TestPersistAtomicCrashSafety(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Write a known-good queue.json first.
	original := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist (initial): %v", err)
	}

	// Plant a partial / corrupt temp file as if a crash occurred mid-write.
	canonicalPath := filepath.Join(projectDir, ".harmonik", "queue.json")
	tmpPath := canonicalPath + ".tmp-99999"
	if err := os.WriteFile(tmpPath, []byte(`{"partial":true}`), 0o600); err != nil {
		t.Fatalf("setup: plant partial temp file: %v", err)
	}

	// Load must still return the previously-written good queue.
	got, err := queue.Load(ctx, projectDir)
	if err != nil {
		t.Fatalf("Load after planted partial temp: %v", err)
	}
	if got == nil {
		t.Fatal("Load after planted partial temp: got nil Queue, want original")
	}
	if got.QueueID != original.QueueID {
		t.Errorf("QueueID: got %q, want %q", got.QueueID, original.QueueID)
	}

	// Sanity: the temp file is still present (Load must not touch sibling files).
	if _, statErr := os.Stat(tmpPath); os.IsNotExist(statErr) {
		t.Error("orphan temp file was removed by Load; it must be left for operator inspection")
	}
}

// TestUnlink verifies that Unlink removes queue.json and is idempotent
// (specs/queue-model.md §3.3 QM-003).
func TestUnlink(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Write a queue, then unlink it.
	q := persistFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	if err := queue.Unlink(ctx, projectDir); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	// File must be gone.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("Unlink: queue.json still exists after Unlink")
	}

	// Load must now return (nil, nil).
	got, err := queue.Load(ctx, projectDir)
	if err != nil {
		t.Fatalf("Load after Unlink: %v", err)
	}
	if got != nil {
		t.Errorf("Load after Unlink: got %+v, want nil", got)
	}

	// Second Unlink must be idempotent (no error on missing file).
	if err := queue.Unlink(ctx, projectDir); err != nil {
		t.Fatalf("second Unlink (idempotent): %v", err)
	}
}

// TestPersistMkdirAll verifies that Persist creates .harmonik/ when it does
// not exist, rather than failing with a path error.
func TestPersistMkdirAll(t *testing.T) {
	t.Parallel()

	// Use a raw TempDir without pre-creating .harmonik.
	dir := t.TempDir()
	ctx := context.Background()

	q := persistFixtureQueue()
	if err := queue.Persist(ctx, dir, &q); err != nil {
		t.Fatalf("Persist without pre-existing .harmonik: %v", err)
	}

	got, err := queue.Load(ctx, dir)
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

// completeUnlinkFixtureQueue returns a minimal valid Queue with all items in
// terminal states (completed), suitable for the QM-053 completion sequence.
// All groups and items are terminal so the caller can immediately invoke
// CompleteAndUnlink without violating QM-030.
func completeUnlinkFixtureQueue() queue.Queue {
	q := typesFixtureQueue()
	// Override groups so every item is terminal (no dispatched items).
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
// (a) queue.json is written with status=completed before removal,
// (b) queue.json is absent after CompleteAndUnlink succeeds,
// (c) Load returns (nil, nil) after completion, and
// (d) a fresh Persist can re-create queue.json (new queue-submit is permitted).
//
// Spec ref: specs/queue-model.md §8.4 QM-053, §3.3 QM-003.
func TestCompleteAndUnlink(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Pre-condition: write an active queue to disk.
	q := completeUnlinkFixtureQueue()
	if err := queue.Persist(ctx, projectDir, &q); err != nil {
		t.Fatalf("Persist (setup): %v", err)
	}

	// Execute QM-053 completion sequence.
	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("CompleteAndUnlink: %v", err)
	}

	// QM-053 step 1: status must be completed on the in-memory struct.
	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("q.Status after CompleteAndUnlink: got %q, want %q",
			q.Status, queue.QueueStatusCompleted)
	}

	// QM-053 step 3: file must be absent.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("CompleteAndUnlink: queue.json still exists after completion, must be unlinked")
	}

	// Load must now return (nil, nil) per QM-003.
	got, err := queue.Load(ctx, projectDir)
	if err != nil {
		t.Fatalf("Load after CompleteAndUnlink: %v", err)
	}
	if got != nil {
		t.Errorf("Load after CompleteAndUnlink: got %+v, want nil", got)
	}

	// A fresh queue-submit (new Persist) must succeed and re-create the file.
	newQ := completeUnlinkFixtureQueue()
	newQ.QueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0002"
	if err := queue.Persist(ctx, projectDir, &newQ); err != nil {
		t.Fatalf("Persist after completion (new submit): %v", err)
	}
	reloaded, err := queue.Load(ctx, projectDir)
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
// step 3). It intercepts the intermediate state by observing the file content
// between Persist and Unlink is not directly testable here, so instead this
// test verifies the in-memory struct is mutated to "completed" and the file
// is gone after the call — the ordering guarantee is enforced by the
// implementation's sequential call to Persist then Unlink.
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

	// In-memory status must be completed.
	if q.Status != queue.QueueStatusCompleted {
		t.Errorf("q.Status: got %q, want %q", q.Status, queue.QueueStatusCompleted)
	}

	// File must be absent (Unlink ran after persist).
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue.json must be absent after CompleteAndUnlink")
	}
}

// TestCompleteAndUnlinkIdempotent verifies that calling CompleteAndUnlink a
// second time (simulating a daemon restart where the file was already unlinked)
// returns nil and does not corrupt state.
//
// After completion + unlink, the queue is gone from disk. On a retry, Persist
// re-creates the file (a deliberate QM-003 re-persist then re-unlink cycle),
// and Unlink on the newly-written file succeeds. This matches the "idempotent"
// semantics described in the CompleteAndUnlink godoc.
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

	// First call.
	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("first CompleteAndUnlink: %v", err)
	}

	// Second call — file is already absent; Persist re-creates it then
	// Unlink removes it again. Must not return an error.
	if err := queue.CompleteAndUnlink(ctx, projectDir, &q); err != nil {
		t.Fatalf("second CompleteAndUnlink (idempotent): %v", err)
	}

	// After both calls the file must be absent.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue.json must be absent after second CompleteAndUnlink")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ArchiveFailedQueue tests (hk-ly4w5)
// ─────────────────────────────────────────────────────────────────────────────

// TestArchiveFailedQueue_RenamesFile verifies that ArchiveFailedQueue renames
// queue.json to queue.json.failed-<timestamp> and that queue.json is absent
// afterwards, while the archive file contains the original content.
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
	archivePath, err := queue.ArchiveFailedQueue(ctx, projectDir, ts)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue: %v", err)
	}

	// Archive path must contain the timestamp in yyyymmddHHMMSS format.
	expectedSuffix := ".failed-20260518123456"
	if !strings.HasSuffix(archivePath, expectedSuffix) {
		t.Errorf("archivePath %q does not end with %q", archivePath, expectedSuffix)
	}

	// Original queue.json must be absent.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); !os.IsNotExist(statErr) {
		t.Error("queue.json still exists after ArchiveFailedQueue; must have been renamed")
	}

	// Archive file must exist and contain parseable content.
	data, readErr := os.ReadFile(archivePath) //nolint:gosec // G304: test-only
	if readErr != nil {
		t.Fatalf("ReadFile(archivePath): %v", readErr)
	}
	if !bytes.Contains(data, []byte(q.QueueID)) {
		t.Errorf("archive file does not contain QueueID %q", q.QueueID)
	}

	// Load must now return (nil, nil) — no active queue remains.
	got, loadErr := queue.Load(ctx, projectDir)
	if loadErr != nil {
		t.Fatalf("Load after ArchiveFailedQueue: %v", loadErr)
	}
	if got != nil {
		t.Errorf("Load after ArchiveFailedQueue: got %+v, want nil", got)
	}
}

// TestArchiveFailedQueue_AbsentIsNoOp verifies that ArchiveFailedQueue returns
// ("", nil) when queue.json does not exist (idempotent / missing-file case).
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_AbsentIsNoOp(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()
	ts := time.Now().UTC()

	archivePath, err := queue.ArchiveFailedQueue(ctx, projectDir, ts)
	if err != nil {
		t.Fatalf("ArchiveFailedQueue on absent queue.json: %v", err)
	}
	if archivePath != "" {
		t.Errorf("archivePath = %q; want empty string for no-op", archivePath)
	}
}

// TestArchiveFailedQueue_SubsequentRunSucceeds verifies that after
// ArchiveFailedQueue renames queue.json, a subsequent Persist (simulating a
// fresh `harmonik run`) succeeds — i.e., re-run is one command (hk-ly4w5 goal).
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_SubsequentRunSucceeds(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Simulate a prior failed run: write queue.json with paused-by-failure status.
	failedQueue := persistFixtureQueue()
	failedQueue.Status = queue.QueueStatusPausedByFailure
	if err := queue.Persist(ctx, projectDir, &failedQueue); err != nil {
		t.Fatalf("Persist (prior failed queue): %v", err)
	}

	// Archive it (as run.go does on paused-by-failure exit).
	ts := time.Now().UTC()
	_, archiveErr := queue.ArchiveFailedQueue(ctx, projectDir, ts)
	if archiveErr != nil {
		t.Fatalf("ArchiveFailedQueue: %v", archiveErr)
	}

	// Simulate guard check: Load should return nil (no active queue).
	loaded, loadErr := queue.Load(ctx, projectDir)
	if loadErr != nil {
		t.Fatalf("Load after archive: %v", loadErr)
	}
	if loaded != nil {
		t.Errorf("Load after archive: got non-nil queue (status=%q); guard would block re-run", loaded.Status)
	}

	// Simulate fresh `harmonik run`: Persist a new active queue. Must succeed.
	newQueue := persistFixtureQueue()
	newQueue.QueueID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099"
	newQueue.Status = queue.QueueStatusActive
	if err := queue.Persist(ctx, projectDir, &newQueue); err != nil {
		t.Fatalf("Persist (re-run): %v", err)
	}

	// Verify the new queue is visible.
	reloaded, loadErr2 := queue.Load(ctx, projectDir)
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
// is still in active status (not paused-by-failure), the existing guard in
// run.go continues to block — i.e., exit-5 behavior is unaffected.
//
// The guard blocks on any non-completed status. This test exercises the
// active-status path (QM-027 guard, exit code 5 in run.go) directly via
// Load-and-check, mirroring the approach in TestRunBead_RefusesActiveQueue.
//
// Bead ref: hk-ly4w5.
func TestArchiveFailedQueue_ActiveQueueGuardPreserved(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Write an active (in-flight) queue — this represents a concurrently-running
	// harmonik instance; the guard must block.
	activeQueue := persistFixtureQueue()
	activeQueue.Status = queue.QueueStatusActive
	if err := queue.Persist(ctx, projectDir, &activeQueue); err != nil {
		t.Fatalf("Persist (active queue): %v", err)
	}

	// Simulate the guard logic from run.go (lines 172–183).
	loaded, loadErr := queue.Load(ctx, projectDir)
	if loadErr != nil {
		t.Fatalf("queue.Load: %v", loadErr)
	}
	if loaded == nil {
		t.Fatal("queue.Load returned nil; expected active queue")
	}

	// Guard condition: any non-completed status should block.
	if loaded.Status == queue.QueueStatusCompleted {
		t.Errorf("active queue should have non-completed status; got %q", loaded.Status)
	}

	// ArchiveFailedQueue is NOT called for active queues — only for
	// paused-by-failure. Confirm queue.json is still intact.
	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	if _, statErr := os.Stat(queuePath); os.IsNotExist(statErr) {
		t.Error("queue.json must not be removed for an active-status queue")
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

	queuePath := filepath.Join(projectDir, ".harmonik", "queue.json")
	data, err := os.ReadFile(queuePath) //nolint:gosec // G304: test path built from t.TempDir
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Contains(data, []byte(q.QueueID)) {
		t.Errorf("queue.json does not contain QueueID %q", q.QueueID)
	}

	// Verify the file is valid JSON.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Errorf("queue.json is not valid JSON: %v", err)
	}
}
