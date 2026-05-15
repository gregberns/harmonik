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
