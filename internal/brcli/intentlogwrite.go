package brcli

// intentlogwrite.go — BI-030 steps 1–4: write IntentLogEntry to a temp file,
// fsync(temp_fd), rename(2) to canonical <key>.json, and fsync(parent_dir_fd).
//
// This file implements Steps 1–4 of the BI-030 multi-step atomic write
// protocol:
//   - Step 1: encode the IntentLogEntry as JSON and write it to a temp file
//     named "<encoded_key>.json.tmp-<rand>" in the intent-log directory.
//   - Step 2: fsync(temp_fd) — flush the file data to durable storage before
//     the file descriptor is closed (BI-030 step 2; hk-872.37.2).
//   - Step 3: rename(2) the temp file to the canonical "<encoded_key>.json"
//     path in the same directory (BI-030 step 3; hk-872.37.3).
//   - Step 4: fsync(parent_directory_fd) — ensure the directory entry for the
//     renamed file is durable on APFS / ext4-data=ordered (BI-030 step 4;
//     hk-872.37.4).
//
// Steps 5–6 (br invocation, unlink + fsync(parent_dir) on delete) are
// addressed by follow-up beads hk-872.37.5 through hk-872.37.6.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 steps 1–4; §6.1 RECORD
// IntentLogEntry; §6.2 on-disk layout.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// intentLogEntryWire is the on-disk JSON shape for an IntentLogEntry.
// It mirrors core.IntentLogEntry using the snake_case field names from
// the spec's RECORD definition (specs/beads-integration.md §6.1).
//
// The core.IntentLogEntry struct carries no JSON tags; this separate wire
// type ensures the on-disk representation is spec-compliant regardless of
// any future changes to the Go struct's exported field names.
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

// intentLogSyncFile is the fsync hook called on the open temp-file fd in
// WriteIntentLogTmp (BI-030 step 2).  Tests may replace this with a counting
// stub to assert the call was made; production code always uses the real
// (*os.File).Sync path.
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

	// Encode colons in the idempotency key to underscores for filesystem
	// portability (OQ-BI-003 — colons are forbidden on macOS HFS+).
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
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: write temp file %q: %w", path, err)
	}
	// BI-030 step 2: fsync(temp_fd) — ensure data is durable before the
	// caller proceeds to rename(2) in step 3.  A Sync failure means the data
	// may not have reached stable storage; treat it the same as a write
	// failure: remove the partial temp file and return an error.
	if err := intentLogSyncFile(f); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: fsync temp file %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: close temp file %q: %w", path, err)
	}

	return path, nil
}

// intentLogRandHex returns n cryptographically random lowercase hex characters
// using crypto/rand. Used to generate the random suffix of intent-log temp
// files (BI-030: "random suffix prevents collision under concurrent recovery").
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

// intentLogRenameFile is the rename hook used by RenameIntentLogTmpToFinal
// (BI-030 step 3). Tests may replace this with an injected stub to simulate
// rename failures; production code always uses os.Rename.
var intentLogRenameFile = func(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

// intentLogSyncDir is the fsync hook called on the open directory fd in
// FsyncIntentLogParentDir (BI-030 step 4). Tests may replace this with a
// counting stub to assert the call was made; production code always uses the
// real (*os.File).Sync path.
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
// Spec ref: specs/beads-integration.md §4.10 BI-030 step 4; hk-872.37.4.
func FsyncIntentLogParentDir(dir string) error {
	//nolint:gosec // G304: dir is the adapter-owned intent-log directory (.harmonik/beads-intents/), not user input
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("brcli.FsyncIntentLogParentDir: open dir %q: %w", dir, err)
	}
	defer func() { _ = f.Close() }()

	if err := intentLogSyncDir(f); err != nil {
		return fmt.Errorf("brcli.FsyncIntentLogParentDir: fsync dir %q: %w", dir, err)
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
func RenameIntentLogTmpToFinal(tmpPath string, dir string, idempotencyKey string) (finalPath string, err error) {
	encodedKey := strings.ReplaceAll(idempotencyKey, ":", "_")
	finalPath = filepath.Join(dir, encodedKey+".json")

	if err := intentLogRenameFile(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("brcli.RenameIntentLogTmpToFinal: rename %q -> %q: %w", tmpPath, finalPath, err)
	}
	return finalPath, nil
}
