package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// queueFileName is the canonical on-disk name for the queue envelope per
// specs/queue-model.md §2.9.
const queueFileName = "queue.json"

// maxQueueFileBytes is the 1 MiB persistence size bound per QM-004.
const maxQueueFileBytes = 1 << 20 // 1 MiB = 1048576 bytes

// ErrCorrupt is returned by Load when the queue.json file exists but cannot
// be parsed (invalid JSON or schema_version mismatch per QM-002).
var ErrCorrupt = fmt.Errorf("queue: queue.json is present but unparseable")

// ErrPersistFailed is returned by Persist when any step in the QM-001
// atomic-write sequence fails (write, fsync, rename, or parent-dir fsync).
//
// The daemon caller MUST treat this as a signal to refuse further queue
// mutations, emit infrastructure_unavailable{failed_prerequisite:
// queue_write_error}, and transition to degraded state per PL-010.
//
// Spec ref: specs/queue-model.md §3.1 QM-001.
var ErrPersistFailed = fmt.Errorf("queue: atomic write to queue.json failed")

// ErrTooLarge is returned by Persist when the marshalled queue envelope
// exceeds the 1 MiB size bound per QM-004.
var ErrTooLarge = fmt.Errorf("queue: queue.json would exceed 1 MiB size bound (QM-004)")

// queuePath returns the canonical path to queue.json under projectDir.
//
// Spec ref: specs/queue-model.md §2.9 — ".harmonik/queue.json".
func queuePath(projectDir string) string {
	return projectDir + "/.harmonik/" + queueFileName
}

// harmonikDir returns the .harmonik directory path under projectDir.
func harmonikDir(projectDir string) string {
	return projectDir + "/.harmonik"
}

// Persist atomically writes q to .harmonik/queue.json using the WM-026
// four-step sequence: (i) marshal to JSON; (ii) write to sibling temp file;
// (iii) fsync temp; (iv) rename to canonical path; (v) fsync parent directory.
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

	hDir := harmonikDir(projectDir)
	// Ensure .harmonik/ exists. MkdirAll is idempotent; 0o755 matches existing
	// .harmonik dir conventions per the workspace-model.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(hDir, 0o755); err != nil {
		return fmt.Errorf("%w: mkdir .harmonik: %w", ErrPersistFailed, err)
	}

	target := queuePath(projectDir)
	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())

	// Step 2: create and write to sibling temp file.
	//nolint:gosec // G304: tmpPath derived from projectDir (.harmonik dir) + Getpid
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

	// Step 5: fsync parent directory so the rename is durable.
	//nolint:gosec // G304: hDir is the daemon-internal .harmonik directory
	dir, err := os.Open(hDir)
	if err != nil {
		return fmt.Errorf("%w: open parent dir %q: %w", ErrPersistFailed, hDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("%w: fsync parent dir %q: %w", ErrPersistFailed, hDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("%w: close parent dir %q: %w", ErrPersistFailed, hDir, err)
	}

	return nil
}

// Load reads .harmonik/queue.json and returns the parsed Queue.
//
// Three outcomes per QM-002:
//   - File exists and parses cleanly → returns (q, nil).
//   - File absent → returns (nil, nil); the daemon starts with no active queue.
//   - File present but unparseable → returns (nil, ErrCorrupt); the file is NOT
//     auto-deleted; operator inspection must come first.
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
func Load(_ context.Context, projectDir string) (*Queue, error) {
	path := queuePath(projectDir)
	//nolint:gosec // G304: path derived from projectDir (.harmonik/queue.json)
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
//  2. Unlink .harmonik/queue.json and fsync(parent_directory_fd) per QM-003
//     (Unlink).
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

	// Step 2: unlink queue.json and fsync parent dir (QM-053 step 3 / QM-003).
	if err := Unlink(ctx, projectDir); err != nil {
		return fmt.Errorf("queue: CompleteAndUnlink: unlink: %w", err)
	}
	return nil
}

// CancelQueueOnShutdown transitions q to QueueStatusCancelled, persists it, and
// archives the file to .harmonik/queue.json.cancelled-<timestamp> so that the
// next harmonik run invocation finds no blocking active queue. The in-memory
// queue pointer is updated in-place.
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
	// Rename queue.json → queue.json.cancelled-<ts> so Load() returns nil on
	// the next harmonik run invocation (QM-027 guard bypassed cleanly).
	src := queuePath(projectDir)
	ts := time.Now().UTC().Format("20060102150405")
	dst := src + ".cancelled-" + ts
	if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: CancelQueueOnShutdown: rename to %q: %w", dst, err)
	}
	// fsync parent directory so the rename is durable.
	hDir := harmonikDir(projectDir)
	//nolint:gosec // G304: hDir is the daemon-internal .harmonik directory
	dir, err := os.Open(hDir)
	if err != nil {
		return fmt.Errorf("queue: CancelQueueOnShutdown: open parent dir %q: %w", hDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("queue: CancelQueueOnShutdown: fsync parent dir %q: %w", hDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("queue: CancelQueueOnShutdown: close parent dir %q: %w", hDir, err)
	}
	return nil
}

// ArchiveFailedQueue renames .harmonik/queue.json to
// .harmonik/queue.json.failed-<timestamp> so that a subsequent `harmonik run`
// invocation finds no active queue file and can proceed without manual cleanup.
//
// The timestamp is formatted as yyyymmddHHMMSS (UTC) to be filesystem-safe
// and monotonically ordered.
//
// If queue.json does not exist, ArchiveFailedQueue is a no-op (returns nil).
// The archive path is returned on success for logging/diagnostics.
//
// Spec ref: hk-ly4w5 — auto-archive queue.json on paused-by-failure.
func ArchiveFailedQueue(_ context.Context, projectDir string, t time.Time) (string, error) {
	src := queuePath(projectDir)
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
	hDir := harmonikDir(projectDir)
	//nolint:gosec // G304: hDir is the daemon-internal .harmonik directory
	dir, err := os.Open(hDir)
	if err != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: open parent dir %q: %w", hDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: fsync parent dir %q: %w", hDir, err)
	}
	if err := dir.Close(); err != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: close parent dir %q: %w", hDir, err)
	}
	return dst, nil
}

// Unlink removes .harmonik/queue.json and fsyncs the parent directory for
// durability. Called when the queue transitions to status=completed per QM-003.
//
// A missing file is treated as success (idempotent: the caller may retry on
// daemon restart).
//
// Spec ref: specs/queue-model.md §3.3 QM-003.
func Unlink(_ context.Context, projectDir string) error {
	target := queuePath(projectDir)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: Unlink: remove %q: %w", target, err)
	}

	hDir := harmonikDir(projectDir)
	//nolint:gosec // G304: hDir is the daemon-internal .harmonik directory
	dir, err := os.Open(hDir)
	if err != nil {
		return fmt.Errorf("queue: Unlink: open parent dir %q: %w", hDir, err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("queue: Unlink: fsync parent dir %q: %w", hDir, err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("queue: Unlink: close parent dir %q: %w", hDir, err)
	}
	return nil
}
