package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// persistCounter makes concurrent in-process Persist calls for the same queue
// mint distinct temp filenames. Keyed only on PID, two goroutines racing to
// persist the same queue would derive an identical tmpPath and the second
// O_EXCL create would fail with ErrPersistFailed. The per-write counter (plus a
// random suffix as a belt-and-suspenders guard against PID reuse across
// processes) makes each temp name unique. Bead ref: W4 mega-review §c.
var persistCounter atomic.Uint64

// uniqueTmpSuffix returns a per-write-unique suffix for an atomic-write temp
// file: PID + a monotonically-increasing in-process counter + a short random
// token. The counter guarantees uniqueness among concurrent goroutines in this
// process; the random token guards against collisions across processes that
// happen to reuse a PID.
func uniqueTmpSuffix() string {
	n := persistCounter.Add(1)
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unexpected; the PID+counter alone still
		// guarantees in-process uniqueness, so fall back to those.
		return fmt.Sprintf("%d-%d", os.Getpid(), n)
	}
	return fmt.Sprintf("%d-%d-%s", os.Getpid(), n, hex.EncodeToString(b[:]))
}

// queueFileName is the legacy singleton filename, kept for migration and docs.
const queueFileName = "queue.json"

// queuesSubDir is the subdirectory under .harmonik where per-queue files live.
// Spec ref: specs/queue-model.md §2.9.
const queuesSubDir = "queues"

// maxQueueFileBytes is the 1 MiB persistence size bound per QM-004.
const maxQueueFileBytes = 1 << 20 // 1 MiB = 1048576 bytes

// ErrCorrupt is returned by Load when the per-queue file exists but cannot
// be parsed (invalid JSON or schema_version mismatch per QM-002).
var ErrCorrupt = fmt.Errorf("queue: queue file is present but unparseable")

// ErrPersistFailed is returned by Persist when any step in the QM-001
// atomic-write sequence fails (write, fsync, rename, or parent-dir fsync).
//
// The daemon caller MUST treat this as a signal to refuse further queue
// mutations, emit infrastructure_unavailable{failed_prerequisite:
// queue_write_error}, and transition to degraded state per PL-010.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
var ErrPersistFailed = fmt.Errorf("queue: atomic write to queue file failed")

// ErrTooLarge is returned by Persist when the marshalled queue envelope
// exceeds the 1 MiB size bound per QM-004.
var ErrTooLarge = fmt.Errorf("queue: queue file would exceed 1 MiB size bound (QM-004)")

// queuePath returns the canonical per-queue path under projectDir.
// name MUST be normalised (non-empty, valid per QM-002/2.1).
//
// Spec ref: specs/queue-model.md §2.9 — ".harmonik/queues/<name>.json".
func queuePath(projectDir, name string) string {
	return queuesDir(projectDir) + "/" + name + ".json"
}

// queuesDir returns the .harmonik/queues directory under projectDir.
func queuesDir(projectDir string) string {
	return projectDir + "/.harmonik/" + queuesSubDir
}

// legacyQueuePath returns the pre-NQ-A2 singleton path .harmonik/queue.json.
// Used only by MigrateFromLegacy.
func legacyQueuePath(projectDir string) string {
	return projectDir + "/.harmonik/" + queueFileName
}

// harmonikDir returns the .harmonik directory path under projectDir.
func harmonikDir(projectDir string) string {
	return projectDir + "/.harmonik"
}

// Persist atomically writes q to .harmonik/queues/<name>.json using the
// WM-026 four-step sequence: (i) marshal to JSON; (ii) write to sibling temp
// file; (iii) fsync temp; (iv) rename to canonical path; (v) fsync parent
// directory.
//
// The queue name is derived from q.Name (normalised to QueueNameMain if empty).
// The .harmonik/queues/ directory is created automatically (MkdirAll,
// idempotent).
//
// If the marshalled size exceeds 1 MiB, Persist returns ErrTooLarge without
// writing anything (QM-004).
//
// On any I/O error during the atomic-write sequence, Persist wraps the
// underlying error with ErrPersistFailed. The daemon caller MUST treat any
// ErrPersistFailed return as a queue-write-error event trigger and degrade per
// PL-010; this function does not emit the event itself (that is the daemon
// caller's responsibility, wired at T70).
//
// The context is accepted for future cancellation; it is checked before the
// write begins but not polled during syscalls.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
// Spec ref: specs/workspace-model.md §4.7 WM-026.
func Persist(_ context.Context, projectDir string, q *Queue) error {
	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("%w: marshal: %w", ErrPersistFailed, err)
	}

	// QM-004: enforce 1 MiB size bound before touching disk.
	if len(data) > maxQueueFileBytes {
		return ErrTooLarge
	}

	qDir := queuesDir(projectDir)
	// Ensure .harmonik/queues/ exists. MkdirAll is idempotent; 0o755 matches
	// existing .harmonik dir conventions per the workspace-model.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(qDir, 0o755); err != nil {
		return fmt.Errorf("%w: mkdir queues: %w", ErrPersistFailed, err)
	}

	name := NormaliseQueueName(q.Name)
	target := queuePath(projectDir, name)
	tmpPath := fmt.Sprintf("%s.tmp-%s", target, uniqueTmpSuffix())

	// Step 2: create and write to sibling temp file.
	//nolint:gosec // G304: tmpPath derived from projectDir (.harmonik/queues/) + Getpid
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: create temp %q: %w", ErrPersistFailed, tmpPath, err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on write failure
		return fmt.Errorf("%w: write temp %q: %w", ErrPersistFailed, tmpPath, err)
	}

	// Step 3: fsync temp file so data is durable before rename.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on sync failure
		return fmt.Errorf("%w: fsync temp %q: %w", ErrPersistFailed, tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on close failure
		return fmt.Errorf("%w: close temp %q: %w", ErrPersistFailed, tmpPath, err)
	}

	// Step 4: rename temp → target (atomic within same filesystem).
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on rename failure
		return fmt.Errorf("%w: rename %q → %q: %w", ErrPersistFailed, tmpPath, target, err)
	}

	// Step 5: fsync parent directory (.harmonik/queues/) so the rename is durable.
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		return fmt.Errorf("%w: open parent dir %q: %w", ErrPersistFailed, qDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("%w: fsync parent dir %q: %w", ErrPersistFailed, qDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("%w: close parent dir %q: %w", ErrPersistFailed, qDir, err)
	}

	return nil
}

// Load reads .harmonik/queues/<name>.json and returns the parsed Queue.
//
// Three outcomes per QM-002:
//   - File exists and parses cleanly → returns (q, nil).
//   - File absent → returns (nil, nil); the daemon starts with no active queue
//     for that name.
//   - File present but unparseable → returns (nil, ErrCorrupt); the file is NOT
//     auto-deleted; operator inspection must come first.
//
// name MUST be normalised (non-empty); use NormaliseQueueName.
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
func Load(_ context.Context, projectDir, name string) (*Queue, error) {
	path := queuePath(projectDir, name)
	//nolint:gosec // G304: path derived from projectDir (.harmonik/queues/<name>.json)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: Load: read %q: %w", path, err)
	}

	q, err := UnmarshalQueue(data)
	if err != nil {
		// File is present but unparseable: return ErrCorrupt per QM-002.
		// Wrap so callers can inspect via errors.Is.
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return &q, nil
}

// CompleteAndUnlink implements the QM-053 completion sequence for a queue that
// has just had its last group reach complete-success.
//
// It performs the three persistence steps of specs/queue-model.md §8.4 QM-053
// in the required order:
//
//  1. Transition q.Status to QueueStatusCompleted and persist via QM-001
//     (Persist). This makes the completed status durable before the file is
//     removed.
//  2. Unlink .harmonik/queues/<name>.json and fsync(parent_directory_fd) per
//     QM-003 (Unlink).
//
// The caller MUST have already emitted queue_group_completed (the durable
// landmark per QM-033) before calling CompleteAndUnlink, and MUST clear its
// in-memory queue reference after CompleteAndUnlink returns nil (step 4 of
// QM-053). No separate queue_completed event is emitted by this function; the
// final queue_group_completed event is the spec-designated durable landmark
// per QM-033.
//
// CompleteAndUnlink is idempotent with respect to the unlink step: if the
// persist succeeds but the process crashes before Unlink completes, a retry
// will re-persist (no-op unless status changed) and re-unlink (file absent is
// treated as success per Unlink).
//
// Returns ErrPersistFailed (wrapping the underlying error) if the atomic write
// fails. Returns an error from Unlink if the remove or parent-dir fsync fails.
// Both errors MUST be treated by the daemon caller as signals to degrade per
// PL-010.
//
// Spec ref: specs/queue-model.md §8.4 QM-053.
// Spec ref: specs/queue-model.md §3.3 QM-003.
func CompleteAndUnlink(ctx context.Context, projectDir string, q *Queue) error {
	// Step 1: transition status to completed and persist (QM-053 steps 1+2).
	q.Status = QueueStatusCompleted
	if err := Persist(ctx, projectDir, q); err != nil {
		return fmt.Errorf("queue: CompleteAndUnlink: persist completed status: %w", err)
	}

	// Step 2: unlink the per-queue file and fsync parent dir (QM-053 step 3 / QM-003).
	name := NormaliseQueueName(q.Name)
	if err := Unlink(ctx, projectDir, name); err != nil {
		return fmt.Errorf("queue: CompleteAndUnlink: unlink: %w", err)
	}
	return nil
}

// CancelQueueOnShutdown transitions q to QueueStatusCancelled, persists it, and
// archives the per-queue file to .harmonik/queues/<name>.json.cancelled-<timestamp>
// so that the next harmonik run invocation finds no blocking active queue. The
// in-memory queue pointer is updated in-place.
//
// This is the canonical shutdown drain for the SIGINT / operator-cancel path
// (hk-ppt32). The caller (runWorkLoop) invokes it only when the queue is still
// in a non-terminal state (active) at ctx-cancel time.
//
// Returns nil when:
//   - The persist succeeds and the file is renamed.
//   - q is nil (no-op: nothing to cancel).
//
// Returns an error when Persist or the rename fails; the caller logs the error
// but continues shutdown regardless.
//
// Spec ref: specs/queue-model.md §8 (shutdown drain, hk-ppt32).
func CancelQueueOnShutdown(ctx context.Context, projectDir string, q *Queue) error {
	if q == nil {
		return nil
	}
	q.Status = QueueStatusCancelled
	if err := Persist(ctx, projectDir, q); err != nil {
		return fmt.Errorf("queue: CancelQueueOnShutdown: persist: %w", err)
	}
	// Rename per-queue file → <name>.json.cancelled-<ts> so Load() returns nil
	// on the next harmonik run invocation (QM-027 guard bypassed cleanly).
	name := NormaliseQueueName(q.Name)
	src := queuePath(projectDir, name)
	ts := time.Now().UTC().Format("20060102150405")
	dst := src + ".cancelled-" + ts
	if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: CancelQueueOnShutdown: rename to %q: %w", dst, err)
	}
	// fsync parent directory so the rename is durable.
	qDir := queuesDir(projectDir)
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		return fmt.Errorf("queue: CancelQueueOnShutdown: open parent dir %q: %w", qDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("queue: CancelQueueOnShutdown: fsync parent dir %q: %w", qDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("queue: CancelQueueOnShutdown: close parent dir %q: %w", qDir, err)
	}
	return nil
}

// ArchiveFailedQueue renames .harmonik/queues/<name>.json to
// .harmonik/queues/<name>.json.failed-<timestamp> so that a subsequent
// `harmonik run` invocation finds no active queue file and can proceed without
// manual cleanup.
//
// The timestamp is formatted as yyyymmddHHMMSS (UTC) to be filesystem-safe
// and monotonically ordered.
//
// If the per-queue file does not exist, ArchiveFailedQueue is a no-op (returns
// nil). The archive path is returned on success for logging/diagnostics.
//
// name MUST be normalised (non-empty); use NormaliseQueueName.
//
// Spec ref: hk-ly4w5 — auto-archive queue file on paused-by-failure.
func ArchiveFailedQueue(_ context.Context, projectDir, name string, t time.Time) (string, error) {
	src := queuePath(projectDir, name)
	ts := t.UTC().Format("20060102150405")
	dst := src + ".failed-" + ts

	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Already gone — treat as success.
			return "", nil
		}
		return "", fmt.Errorf("queue: ArchiveFailedQueue: rename %q → %q: %w", src, dst, err)
	}

	// fsync parent directory so the rename is durable.
	qDir := queuesDir(projectDir)
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: open parent dir %q: %w", qDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: fsync parent dir %q: %w", qDir, err)
	}
	if err := dir.Close(); err != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: close parent dir %q: %w", qDir, err)
	}
	return dst, nil
}

// Unlink removes .harmonik/queues/<name>.json and fsyncs the parent directory
// for durability. Called when the queue transitions to status=completed per
// QM-003.
//
// A missing file is treated as success (idempotent: the caller may retry on
// daemon restart).
//
// name MUST be normalised (non-empty); use NormaliseQueueName.
//
// Spec ref: specs/queue-model.md §3.3 QM-003.
func Unlink(_ context.Context, projectDir, name string) error {
	target := queuePath(projectDir, name)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: Unlink: remove %q: %w", target, err)
	}

	qDir := queuesDir(projectDir)
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		// queues/ dir may not exist if queue was never persisted; treat as success.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("queue: Unlink: open parent dir %q: %w", qDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("queue: Unlink: fsync parent dir %q: %w", qDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("queue: Unlink: close parent dir %q: %w", qDir, err)
	}
	return nil
}

// MigrateFromLegacy checks for the legacy .harmonik/queue.json singleton and,
// if found, migrates it to .harmonik/queues/main.json (the QueueNameMain slot).
//
// Migration steps:
//  1. Read .harmonik/queue.json. If absent, return nil (no-op).
//  2. Ensure .harmonik/queues/ exists.
//  3. If .harmonik/queues/main.json already exists, skip the write (a prior
//     migration completed but the legacy file was not removed; just remove it).
//  4. Atomically write the content to .harmonik/queues/main.json via the
//     QM-001 rename dance.
//  5. Remove .harmonik/queue.json.
//  6. Fsync .harmonik/ and .harmonik/queues/ for durability.
//
// MigrateFromLegacy is idempotent: if called again after a successful migration
// the legacy file is absent and the function returns nil immediately.
//
// Bead ref: hk-tigaf.3.
func MigrateFromLegacy(_ context.Context, projectDir string) error {
	legacyPath := legacyQueuePath(projectDir)
	//nolint:gosec // G304: legacyPath is daemon-internal .harmonik/queue.json
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // no legacy file — nothing to do
		}
		return fmt.Errorf("queue: MigrateFromLegacy: read legacy file: %w", err)
	}

	qDir := queuesDir(projectDir)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(qDir, 0o755); err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: mkdir queues: %w", err)
	}

	targetPath := queuePath(projectDir, QueueNameMain)

	// If main.json already exists, a prior migration wrote it; just remove the
	// legacy file.
	if _, statErr := os.Stat(targetPath); statErr == nil {
		if removeErr := os.Remove(legacyPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("queue: MigrateFromLegacy: remove legacy after existing main: %w", removeErr)
		}
		return nil
	}

	// Atomic write of legacy content to main.json via rename dance.
	tmpPath := fmt.Sprintf("%s.tmp-migrate-%s", targetPath, uniqueTmpSuffix())
	//nolint:gosec // G304: tmpPath derived from projectDir + Getpid
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: create tmp %q: %w", tmpPath, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("queue: MigrateFromLegacy: write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("queue: MigrateFromLegacy: fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("queue: MigrateFromLegacy: close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("queue: MigrateFromLegacy: rename tmp → main.json: %w", err)
	}

	// Fsync queues/ so the new file is durable.
	if err := fsyncDir(qDir); err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: fsync queues dir: %w", err)
	}

	// Remove legacy file and fsync .harmonik/ so the deletion is durable.
	if err := os.Remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: MigrateFromLegacy: remove legacy file: %w", err)
	}
	if err := fsyncDir(harmonikDir(projectDir)); err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: fsync .harmonik dir: %w", err)
	}

	return nil
}

// EnumerateQueueNames returns the names of all queues present in
// .harmonik/queues/ by listing files matching the pattern <name>.json (no
// .tmp-*, .failed-*, or .cancelled-* suffixes).
//
// Returns nil (empty slice) when the queues/ directory does not exist.
// Returns an error only on unexpected I/O failures (permission denied, etc.).
//
// Bead ref: hk-tigaf.3.
func EnumerateQueueNames(projectDir string) ([]string, error) {
	qDir := queuesDir(projectDir)
	entries, err := os.ReadDir(qDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: EnumerateQueueNames: readdir %q: %w", qDir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Accept only plain <name>.json entries; skip tmp/failed/cancelled archives.
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.Contains(name, ".tmp-") ||
			strings.Contains(name, ".failed-") ||
			strings.Contains(name, ".cancelled-") {
			continue
		}
		queueName := filepath.Base(strings.TrimSuffix(name, ".json"))
		names = append(names, queueName)
	}
	return names, nil
}

// fsyncDir opens dir and calls Sync on it. Returns an error if open or sync fails.
func fsyncDir(dir string) error {
	//nolint:gosec // G304: caller-verified daemon-internal path
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
