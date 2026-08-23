package brcli

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

type intentLogEntryWire struct {
	IdempotencyKey    string    `json:"idempotency_key"`
	RunID             string    `json:"run_id"`
	TransitionID      string    `json:"transition_id"`
	Op                string    `json:"op"`
	BeadID            string    `json:"bead_id"`
	IntendedPostState string    `json:"intended_post_state"`
	RequestedAt       time.Time `json:"requested_at"`
	SchemaVersion     int       `json:"schema_version"`
}

var intentLogSyncFile = func(f *os.File) error { return f.Sync() }

// WriteIntentLogTmp encodes entry as JSON, writes it to a temp file in dir,
// and fsyncs the file fd before close — implementing BI-030 steps 1 and 2.
//
// The temp file is named:
//
//	<encoded_key>.json.tmp-<rand>
//
// where <encoded_key> is entry.IdempotencyKey with colons replaced by
// underscores (filesystem-portability per specs/beads-integration.md §6.2
// OQ-BI-003: colons are permitted on Linux but forbidden on macOS HFS+), and
// <rand> is 8 cryptographically random lowercase hex characters (crypto/rand).
//
// The temp file is created with mode 0600 via O_CREATE|O_EXCL; the exclusive
// flag guards against accidental collision on the random suffix.
//
// After the data is written, WriteIntentLogTmp calls f.Sync() (fsync(2)) on
// the open file descriptor before closing it (BI-030 step 2). This ensures
// the file contents are durable on disk before the caller proceeds to rename
// (step 3). A Sync failure is treated the same as a write failure: the temp
// file is removed and a non-nil error is returned with an empty tmpPath.
//
// On success, WriteIntentLogTmp returns the absolute path of the fsynced temp
// file. The file is NOT yet renamed to its final path — that is BI-030 step 3,
// addressed by hk-872.37.3.
//
// Returns an error (non-nil tmpPath = "") on any of: invalid entry, random
// suffix generation failure, JSON encoding failure, O_EXCL open failure,
// write failure, or fsync failure.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 steps 1–2; §6.2.
func WriteIntentLogTmp(dir string, entry core.IntentLogEntry) (tmpPath string, err error) {
	if !entry.Valid() {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: entry is invalid: %+v", entry)
	}

	wire := intentLogEntryWire{
		IdempotencyKey:    entry.IdempotencyKey,
		RunID:             entry.RunID.String(),
		TransitionID:      entry.TransitionID.String(),
		Op:                string(entry.Op),
		BeadID:            string(entry.BeadID),
		IntendedPostState: string(entry.IntendedPostState),
		RequestedAt:       entry.RequestedAt,
		SchemaVersion:     entry.SchemaVersion,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: json.Marshal: %w", err)
	}

	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")

	randSuffix, err := intentLogRandHex(8)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: random suffix: %w", err)
	}

	name := encodedKey + ".json.tmp-" + randSuffix
	path := filepath.Join(dir, name)

	//nolint:gosec // G304: dir is the adapter-owned intent-log directory (.harmonik/beads-intents/), not user input
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: create temp file %q: %w", path, err)
	}

	if _, err := f.Write(data); err != nil {
		return "", errors.Join(
			fmt.Errorf("brcli.WriteIntentLogTmp: write temp file %q: %w", path, err),
			closeAndRemoveIntentLogTmp(f, path),
		)
	}
	if err := intentLogSyncFile(f); err != nil {
		return "", errors.Join(
			fmt.Errorf("brcli.WriteIntentLogTmp: fsync temp file %q: %w", path, err),
			closeAndRemoveIntentLogTmp(f, path),
		)
	}
	if err := f.Close(); err != nil {
		return "", errors.Join(
			fmt.Errorf("brcli.WriteIntentLogTmp: close temp file %q: %w", path, err),
			removeIntentLogTmp(path),
		)
	}

	return path, nil
}

func closeAndRemoveIntentLogTmp(f *os.File, path string) error {
	return errors.Join(f.Close(), removeIntentLogTmp(path))
}

func removeIntentLogTmp(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temp file %q: %w", path, err)
	}
	return nil
}

func intentLogRandHex(n int) (string, error) {
	const hexChars = "0123456789abcdef"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(hexChars))))
		if err != nil {
			return "", err
		}
		out[i] = hexChars[idx.Int64()]
	}
	return string(out), nil
}

var intentLogRenameFile = os.Rename

var intentLogSyncDir = func(f *os.File) error { return f.Sync() }

// FsyncIntentLogParentDir opens the intent-log directory at dir read-only,
// calls fsync(2) on the directory file descriptor, and closes it — implementing
// BI-030 step 4.
//
// This step is REQUIRED to ensure that the directory entry created by the
// preceding rename(2) (step 3) is durable on APFS and ext4-data=ordered
// filesystems. Without this fsync, a power-loss after step 3 can lose the
// rename, leaving the intent file absent from the directory on remount.
//
// The directory is opened read-only (os.O_RDONLY) because fsync on a directory
// does not write data — it only flushes the directory's metadata (entry list)
// to stable storage.
//
// On success, FsyncIntentLogParentDir returns nil. On any failure (open,
// fsync, or close), it returns a non-nil error wrapped with the brcli-package
// prefix and the directory path.
//
// BI-030 step 5 ordering invariant: the caller MUST invoke `br` (step 5) only
// after FsyncIntentLogParentDir returns nil. Invoking `br` before step 4
// completes risks a power-loss window in which the intent file is absent from
// the directory on remount, defeating crash-recovery (BI-031).
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 steps 4–5; hk-872.37.4,
// hk-872.37.5.
func FsyncIntentLogParentDir(dir string) error {
	//nolint:gosec // G304: dir is the adapter-owned intent-log directory (.harmonik/beads-intents/), not user input
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("brcli.FsyncIntentLogParentDir: open dir %q: %w", dir, err)
	}
	if err := intentLogSyncDir(f); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return errors.Join(
				fmt.Errorf("brcli.FsyncIntentLogParentDir: fsync dir %q: %w", dir, err),
				fmt.Errorf("brcli.FsyncIntentLogParentDir: close dir %q: %w", dir, closeErr),
			)
		}
		return fmt.Errorf("brcli.FsyncIntentLogParentDir: fsync dir %q: %w", dir, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("brcli.FsyncIntentLogParentDir: close dir %q: %w", dir, err)
	}
	return nil
}

// RenameIntentLogTmpToFinal atomically renames the fsynced temp file at
// tmpPath to the canonical intent-log filename "<encoded_key>.json" in dir —
// implementing BI-030 step 3.
//
// The canonical filename is constructed as:
//
//	<encoded_key>.json
//
// where <encoded_key> is idempotencyKey with colons replaced by underscores
// (same encoding as WriteIntentLogTmp; filesystem-portability per §6.2
// OQ-BI-003 — colons are forbidden on macOS HFS+).
//
// The rename is performed via os.Rename, which maps to POSIX rename(2). On
// POSIX systems, rename(2) is atomic at the filesystem layer when source and
// destination share the same parent directory — which is guaranteed here since
// both tmpPath and the final path are constructed under dir.
//
// If the canonical file already exists (e.g., a retry after a crash between
// step 3 and step 4), os.Rename overwrites it. Overwrite is structurally safe
// because the canonical filename is derived from a deterministic idempotency
// key, so any pre-existing file at that path encodes the same intent.
//
// On success, RenameIntentLogTmpToFinal returns the final (canonical) path and
// a nil error. The temp file at tmpPath no longer exists.
//
// On failure, the rename is not applied and tmpPath remains on disk. The error
// is wrapped with context identifying the source and destination paths. The
// caller (BI-030 step 4 onwards) MUST NOT proceed if this step returns an
// error.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 step 3; §6.2 on-disk layout.
func RenameIntentLogTmpToFinal(tmpPath, dir, idempotencyKey string) (finalPath string, err error) {
	encodedKey := strings.ReplaceAll(idempotencyKey, ":", "_")
	finalPath = filepath.Join(dir, encodedKey+".json")

	if err := intentLogRenameFile(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("brcli.RenameIntentLogTmpToFinal: rename %q -> %q: %w", tmpPath, finalPath, err)
	}
	return finalPath, nil
}

var intentLogUnlinkFile = os.Remove

// DeleteIntentLogAndSyncParent unlinks the canonical intent-log file for
// idempotencyKey in dir, then calls FsyncIntentLogParentDir to flush the
// parent directory's metadata to durable storage — implementing BI-030 step 6.
//
// The canonical filename is constructed as:
//
//	<encoded_key>.json
//
// where <encoded_key> is idempotencyKey with colons replaced by underscores
// (same encoding as WriteIntentLogTmp and RenameIntentLogTmpToFinal;
// filesystem-portability per §6.2 OQ-BI-003 — colons are forbidden on macOS
// HFS+). Accepting (dir, idempotencyKey) rather than a pre-built path keeps
// the colon-encoding rule in one place and matches the (dir, idempotencyKey)
// signature already established by RenameIntentLogTmpToFinal.
//
// This function MUST be called only after `br` returns successfully (BI-030
// step 5 ordering invariant). Calling it before step 5 would delete the intent
// file that crash-recovery (BI-031) relies on.
//
// The two-step delete sequence is REQUIRED per BI-030:
//  1. unlink(intent_file) — removes the directory entry.
//  2. fsync(parent_directory_fd) — ensures the removal is durable. Without
//     this fsync, a power-loss after unlink can leave the intent file visible
//     on remount, causing the BI-031 crash-recovery scan to misclassify the
//     already-completed transition as a Cat 3a torn-write (false positive).
//
// FsyncIntentLogParentDir is reused for step 2; the intentLogSyncDir hook
// applies, making the parent-dir fsync stubable in tests.
//
// On success, DeleteIntentLogAndSyncParent returns nil. On any failure (unlink
// or parent-dir fsync), it returns a non-nil error wrapped with the brcli-
// package prefix, the file path, and the failed operation.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 delete sequence steps 1–2;
// hk-872.37.6.
func DeleteIntentLogAndSyncParent(dir, idempotencyKey string) error {
	encodedKey := strings.ReplaceAll(idempotencyKey, ":", "_")
	intentPath := filepath.Join(dir, encodedKey+".json")

	if err := intentLogUnlinkFile(intentPath); err != nil {
		return fmt.Errorf("brcli.DeleteIntentLogAndSyncParent: unlink %q: %w", intentPath, err)
	}

	if err := FsyncIntentLogParentDir(dir); err != nil {
		return fmt.Errorf("brcli.DeleteIntentLogAndSyncParent: %w", err)
	}
	return nil
}
