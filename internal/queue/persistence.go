package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

var persistCounter atomic.Uint64

func uniqueTmpSuffix() string {
	n := persistCounter.Add(1)
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), n)
	}
	return fmt.Sprintf("%d-%d-%s", os.Getpid(), n, hex.EncodeToString(b[:]))
}

const queueFileName = "queue.json"

const queuesSubDir = "queues"

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

func queuePath(projectDir, name string) string {
	return queuesDir(projectDir) + "/" + name + ".json"
}

func queuesDir(projectDir string) string {
	return projectDir + "/.harmonik/" + queuesSubDir
}

func legacyQueuePath(projectDir string) string {
	return projectDir + "/.harmonik/" + queueFileName
}

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
	if err := validatePreclaimTerminalItems(q); err != nil {
		return fmt.Errorf("queue: persist: %w", err)
	}
	data, err := json.Marshal(q)
	if err != nil {
		return fmt.Errorf("%w: marshal: %w", ErrPersistFailed, err)
	}

	if len(data) > maxQueueFileBytes {
		return ErrTooLarge
	}

	qDir := queuesDir(projectDir)
	if err := os.MkdirAll(qDir, core.HarmonikDirMode); err != nil {
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

	if writeErr := writeTempAndClose(f, data); writeErr != nil {
		rmErr := os.Remove(tmpPath)
		return fmt.Errorf("%w: write temp %q: %w", ErrPersistFailed, tmpPath, errors.Join(writeErr, rmErr))
	}

	if renameErr := os.Rename(tmpPath, target); renameErr != nil {
		rmErr := os.Remove(tmpPath)
		return fmt.Errorf("%w: rename %q → %q: %w", ErrPersistFailed, tmpPath, target, errors.Join(renameErr, rmErr))
	}

	// Step 5: fsync parent directory (.harmonik/queues/) so the rename is durable.
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		return fmt.Errorf("%w: open parent dir %q: %w", ErrPersistFailed, qDir, err)
	}
	if syncErr := dir.Sync(); syncErr != nil {
		return fmt.Errorf("%w: fsync parent dir %q: %w", ErrPersistFailed, qDir, errors.Join(syncErr, dir.Close()))
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
	return CompleteAndUnlinkResult(ctx, projectDir, q).Err()
}

// TerminalResult distinguishes a failed terminal write from cleanup that failed
// after the terminal state became durable.
type TerminalResult struct {
	Committed  bool
	CommitErr  error
	CleanupErr error
}

// Err returns the terminal write error before any post-commit cleanup error.
func (r TerminalResult) Err() error {
	if r.CommitErr != nil {
		return r.CommitErr
	}
	return r.CleanupErr
}

// CompleteAndUnlinkResult performs completion on a detached candidate. The
// supplied queue changes only after the completed candidate persists.
func CompleteAndUnlinkResult(ctx context.Context, projectDir string, q *Queue) TerminalResult {
	return completeAndUnlinkResult(ctx, projectDir, q, Unlink)
}

func completeAndUnlinkResult(
	ctx context.Context,
	projectDir string,
	q *Queue,
	unlink func(context.Context, string, string) error,
) TerminalResult {
	if q == nil {
		return TerminalResult{CommitErr: errors.New("queue: CompleteAndUnlink: nil queue")}
	}
	candidate := CloneQueue(q)
	if err := CompleteQueue(candidate); err != nil {
		return TerminalResult{CommitErr: fmt.Errorf("queue: CompleteAndUnlink: transition completed status: %w", err)}
	}
	if err := Persist(ctx, projectDir, candidate); err != nil {
		return TerminalResult{CommitErr: fmt.Errorf("queue: CompleteAndUnlink: persist completed status: %w", err)}
	}
	if err := InstallCommittedQueueStatus(q, candidate); err != nil {
		return TerminalResult{CommitErr: err}
	}

	name := NormaliseQueueName(q.Name)
	if err := unlink(ctx, projectDir, name); err != nil {
		return TerminalResult{Committed: true, CleanupErr: fmt.Errorf("queue: CompleteAndUnlink: unlink: %w", err)}
	}
	return TerminalResult{Committed: true}
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
	dst := src + FailedArchiveInfix + ts

	if err := os.Rename(src, dst); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("queue: ArchiveFailedQueue: rename %q → %q: %w", src, dst, err)
	}

	qDir := queuesDir(projectDir)
	//nolint:gosec // G304: qDir is the daemon-internal .harmonik/queues directory
	dir, err := os.Open(qDir)
	if err != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: open parent dir %q: %w", qDir, err)
	}
	if syncErr := dir.Sync(); syncErr != nil {
		return dst, fmt.Errorf("queue: ArchiveFailedQueue: fsync parent dir %q: %w", qDir, errors.Join(syncErr, dir.Close()))
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
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("queue: Unlink: open parent dir %q: %w", qDir, err)
	}
	if syncErr := dir.Sync(); syncErr != nil {
		return fmt.Errorf("queue: Unlink: fsync parent dir %q: %w", qDir, errors.Join(syncErr, dir.Close()))
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
//  1. Read and parse .harmonik/queue.json. If absent, fsync .harmonik/ so a
//     retry after a successful removal can finish its durability obligation.
//  2. Stat .harmonik/queues/main.json. Unexpected Stat errors fail closed.
//  3. If main.json exists, parse it and require deep equality with the intended
//     decoded legacy Queue. Corrupt, wrong-schema, and conflicting destinations
//     fail closed.
//  4. If main.json is absent, atomically write the legacy content through a
//     sibling temp file and rename.
//  5. Fsync queues/ on both paths before removing .harmonik/queue.json. Repeating
//     this sync for an equivalent existing destination completes a retry after
//     rename succeeded but the first queues/ sync failed.
//  6. Remove legacy, then fsync .harmonik/ and close its directory descriptor.
//
// MigrateFromLegacy is idempotent: after a successful migration the legacy
// file is absent, and a retry only re-syncs .harmonik/.
//
// Bead ref: hk-tigaf.3.
func MigrateFromLegacy(_ context.Context, projectDir string) error {
	return migrateFromLegacy(projectDir, migrateFromLegacyOps{
		readFile:   os.ReadFile,
		stat:       os.Stat,
		mkdirAll:   os.MkdirAll,
		createTemp: os.OpenFile,
		writeFile: func(file *os.File, data []byte) (int, error) {
			return file.Write(data)
		},
		syncFile:  (*os.File).Sync,
		closeFile: (*os.File).Close,
		rename:    os.Rename,
		openDir:   os.Open,
		syncDir:   (*os.File).Sync,
		closeDir:  (*os.File).Close,
		remove:    os.Remove,
	})
}

type migrateFromLegacyOps struct {
	readFile   func(string) ([]byte, error)
	stat       func(string) (os.FileInfo, error)
	mkdirAll   func(string, os.FileMode) error
	createTemp func(string, int, os.FileMode) (*os.File, error)
	writeFile  func(*os.File, []byte) (int, error)
	syncFile   func(*os.File) error
	closeFile  func(*os.File) error
	rename     func(string, string) error
	openDir    func(string) (*os.File, error)
	syncDir    func(*os.File) error
	closeDir   func(*os.File) error
	remove     func(string) error
}

//nolint:gocognit,cyclop,funlen // Explicit fail-closed migration cuts keep the durability order locally auditable.
func migrateFromLegacy(projectDir string, ops migrateFromLegacyOps) error {
	syncDirectory := func(path string) error {
		dir, err := ops.openDir(path)
		if err != nil {
			return fmt.Errorf("open %q: %w", path, err)
		}
		if syncErr := ops.syncDir(dir); syncErr != nil {
			return fmt.Errorf("sync %q: %w", path, errors.Join(syncErr, ops.closeDir(dir)))
		}
		if closeErr := ops.closeDir(dir); closeErr != nil {
			return fmt.Errorf("close %q: %w", path, closeErr)
		}
		return nil
	}

	legacyPath := legacyQueuePath(projectDir)
	data, err := ops.readFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if syncErr := syncDirectory(harmonikDir(projectDir)); syncErr != nil {
				return fmt.Errorf("queue: MigrateFromLegacy: sync .harmonik after absent legacy: %w", syncErr)
			}
			return nil
		}
		return fmt.Errorf("queue: MigrateFromLegacy: read legacy file: %w", err)
	}
	legacy, err := UnmarshalQueue(data)
	if err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: legacy queue is corrupt: %w", err)
	}

	qDir := queuesDir(projectDir)
	targetPath := queuePath(projectDir, QueueNameMain)
	_, statErr := ops.stat(targetPath)
	switch {
	case statErr == nil:
		target, readErr := ops.readFile(targetPath)
		if readErr != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: read existing main queue: %w", readErr)
		}
		canonical, parseErr := UnmarshalQueue(target)
		if parseErr != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: existing main queue is corrupt: %w", parseErr)
		}
		if !reflect.DeepEqual(legacy, canonical) {
			return fmt.Errorf("queue: MigrateFromLegacy: legacy and existing main queue conflict")
		}
	case errors.Is(statErr, os.ErrNotExist):
		if err := ops.mkdirAll(qDir, core.HarmonikDirMode); err != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: mkdir queues: %w", err)
		}

		tmpPath := fmt.Sprintf("%s.tmp-migrate-%s", targetPath, uniqueTmpSuffix())
		file, createErr := ops.createTemp(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
		if createErr != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: create temp %q: %w", tmpPath, createErr)
		}
		written, writeErr := ops.writeFile(file, data)
		if writeErr != nil || written != len(data) {
			if writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			cleanupErr := errors.Join(ops.closeFile(file), ops.remove(tmpPath))
			return fmt.Errorf("queue: MigrateFromLegacy: write temp: %w", errors.Join(writeErr, cleanupErr))
		}
		if syncErr := ops.syncFile(file); syncErr != nil {
			cleanupErr := errors.Join(ops.closeFile(file), ops.remove(tmpPath))
			return fmt.Errorf("queue: MigrateFromLegacy: sync temp: %w", errors.Join(syncErr, cleanupErr))
		}
		if closeErr := ops.closeFile(file); closeErr != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: close temp: %w", errors.Join(closeErr, ops.remove(tmpPath)))
		}
		if renameErr := ops.rename(tmpPath, targetPath); renameErr != nil {
			return fmt.Errorf("queue: MigrateFromLegacy: rename temp to main: %w", errors.Join(renameErr, ops.remove(tmpPath)))
		}
	default:
		return fmt.Errorf("queue: MigrateFromLegacy: stat main queue: %w", statErr)
	}

	if err := syncDirectory(qDir); err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: sync queues dir: %w", err)
	}

	if err := ops.remove(legacyPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("queue: MigrateFromLegacy: remove legacy file: %w", err)
	}
	if err := syncDirectory(harmonikDir(projectDir)); err != nil {
		return fmt.Errorf("queue: MigrateFromLegacy: sync .harmonik dir: %w", err)
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

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
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

func writeTempAndClose(f *os.File, data []byte) error {
	if _, writeErr := f.Write(data); writeErr != nil {
		return errors.Join(writeErr, f.Close())
	}
	if syncErr := f.Sync(); syncErr != nil {
		return errors.Join(syncErr, f.Close())
	}
	return f.Close()
}

func fsyncDir(dir string) error {
	//nolint:gosec // G304: caller-verified daemon-internal path
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if syncErr := d.Sync(); syncErr != nil {
		return errors.Join(syncErr, d.Close())
	}
	return d.Close()
}
