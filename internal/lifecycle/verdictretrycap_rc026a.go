package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

// verdictretrycap_rc026a.go — I/O layer for the Cat 3b retry attempt counter
// (RC-026a).
//
// RC-026a requires the durable attempt counter to live at
// .harmonik/reconciliation-attempts/<target_run_id>.json, written with the
// atomic temp+rename+fsync discipline mandated by workspace-model.md §4.7
// WM-026.
//
// This file provides:
//
//   - WriteVerdictAttemptAtomic — write a VerdictExecutionAttemptRecord to
//     its canonical path using WM-026 atomicity.
//   - ReadVerdictAttempt — read and parse the record; returns (nil, nil) when
//     absent (no previous retries recorded for this run).
//
// The pure logic (CheckVerdictRetryCap, VerdictRetryDecision) lives in
// internal/core/verdictretrycap_rc026a.go; the daemon's auto-resolver calls
// CheckVerdictRetryCap with the record returned by ReadVerdictAttempt, writes
// the updated record via WriteVerdictAttemptAtomic, and then emits the retry
// event.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a;
// specs/workspace-model.md §4.7 WM-026 (atomic write discipline).

// WriteVerdictAttemptAtomic writes record to the canonical path for targetRunID
// within projectDir, using the WM-026 atomic-write discipline:
//
//  1. Marshal record to JSON.
//  2. MkdirAll the parent directory (.harmonik/reconciliation-attempts/).
//  3. Write to a sibling temp file (.tmp-<pid>).
//  4. fsync the temp file (data durability before rename).
//  5. rename(2) the temp file to the canonical path (POSIX atomic within fs).
//  6. fsync the parent directory to durably record the rename.
//
// Returns an error if record.Valid() is false, or if any I/O step fails.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a;
// specs/workspace-model.md §4.7 WM-026.
func WriteVerdictAttemptAtomic(projectDir string, record *core.VerdictExecutionAttemptRecord) error {
	if record == nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: record must not be nil")
	}
	if !record.Valid() {
		return fmt.Errorf(
			"lifecycle: WriteVerdictAttemptAtomic: invalid record (target_run_id=%q attempt=%d last_attempt_at=%q)",
			record.TargetRunID, record.Attempt, record.LastAttemptAt,
		)
	}

	content, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: marshal: %w", err)
	}

	target := ReconciliationAttemptPath(projectDir, record.TargetRunID)
	dir := filepath.Dir(target)

	//nolint:gosec // G301: 0755 matches .harmonik/ subdir conventions throughout lifecycle package
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path is constructed from projectDir + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Write: %w", err)
	}

	// Step 4: fsync the temp file before rename so data is durable.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Sync (pre-rename): %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Close (pre-rename): %w", err)
	}

	// Step 5: atomic rename — POSIX rename(2) is atomic within the same filesystem.
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Rename %q → %q: %w", tmpPath, target, err)
	}

	// Step 6: fsync the parent directory to durably record the rename.
	// Best-effort on macOS/APFS per WM-026; sync error is intentionally suppressed.
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per WM-026
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("lifecycle: WriteVerdictAttemptAtomic: Close dir fd: %w", err)
	}

	return nil
}

// ReadVerdictAttempt reads and parses the Cat 3b retry attempt counter record
// from the canonical path for targetRunID within projectDir.
//
// Returns (nil, nil) when the file does not exist — the caller interprets
// absence as "no previous retries recorded" (equivalent to Attempt=0) per
// RC-026a. Returns an error for I/O or parse failures other than os.IsNotExist.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
func ReadVerdictAttempt(projectDir, targetRunID string) (*core.VerdictExecutionAttemptRecord, error) {
	path := ReconciliationAttemptPath(projectDir, targetRunID)

	//nolint:gosec // G304: path is constructed from projectDir + known relative segments, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // caller interprets nil as "no retries recorded" per RC-026a
		}
		return nil, fmt.Errorf("lifecycle: ReadVerdictAttempt: ReadFile %q: %w", path, err)
	}

	var record core.VerdictExecutionAttemptRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("lifecycle: ReadVerdictAttempt: Unmarshal %q: %w", path, err)
	}

	if !record.Valid() {
		return nil, fmt.Errorf(
			"lifecycle: ReadVerdictAttempt: invalid record at %q (target_run_id=%q attempt=%d last_attempt_at=%q)",
			path, record.TargetRunID, record.Attempt, record.LastAttemptAt,
		)
	}

	return &record, nil
}
