package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
