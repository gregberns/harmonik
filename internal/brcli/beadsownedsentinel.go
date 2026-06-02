package brcli

// beadsownedsentinel.go — per-bead ownership sentinel file helpers for the
// beads-owned/ directory.
//
// The beads-owned/ directory (under .harmonik/) holds zero-byte files named
// by bead ID. A file's presence asserts that THIS project's daemon has
// successfully claimed the bead at least once. The sentinel outlives the
// BI-030 claim intent file (deleted on claim success per BI-030 step 6) and
// provides an independent provenance signal for the PL-006 sixth-bullet orphan
// sweep when all intent files have been cleared by prior crash-recovery runs.
//
// Lifecycle:
//   - Written in ClaimBead after terminalTransitionWrite returns nil (claim succeeded).
//   - Deleted (best-effort) in CloseBead, ReopenBead, ResetBead after success.
//
// All operations are best-effort and non-fatal: a missing or unwritable
// beads-owned/ directory degrades to the existing intent-log provenance signal;
// a stale sentinel left behind by a failed delete is handled by the next
// successful close/reopen/reset.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (provenance OR clause);
// §4.4 PL-006a (project_hash discipline).
// Bead ref: hk-11xkn.

import (
	"fmt"
	"os"
	"path/filepath"
)

// beadsOwnedSubdir mirrors the constant in lifecycle/daemonpaths.go. Kept
// private and duplicated here to avoid a lifecycle → brcli → lifecycle import
// cycle (lifecycle already imports brcli).
const beadsOwnedSubdir = "beads-owned"

// beadsOwnedDir returns the .harmonik/beads-owned/ path for projectDir.
// Returns an empty string when projectDir is empty (test callers using New).
func beadsOwnedDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	return filepath.Join(projectDir, ".harmonik", beadsOwnedSubdir)
}

// beadsOwnedSentinelPath returns the full path of the sentinel file for
// beadID under ownedDir. beadID is used verbatim as the filename; it is an
// opaque project-scoped identifier per BI-008a and does not contain path
// separators.
func beadsOwnedSentinelPath(ownedDir, beadID string) string {
	return filepath.Join(ownedDir, beadID)
}

// writeBeadsOwnedSentinel creates the ownership sentinel file for beadID under
// projectDir. Creates the beads-owned/ directory if it does not already exist.
//
// This is a best-effort call: callers MUST treat any returned error as
// non-fatal and fall back gracefully to the existing intent-log provenance
// signal.
func writeBeadsOwnedSentinel(projectDir, beadID string) error {
	dir := beadsOwnedDir(projectDir)
	if dir == "" {
		return nil // test caller with no projectDir — skip
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // 0755 matches existing .harmonik dir conventions
		return fmt.Errorf("brcli.writeBeadsOwnedSentinel: MkdirAll %q: %w", dir, err)
	}
	sentinelPath := beadsOwnedSentinelPath(dir, beadID)
	//nolint:gosec // G304: path is constructed from operator-controlled projectDir + bead ID (opaque, no separators)
	f, err := os.OpenFile(sentinelPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("brcli.writeBeadsOwnedSentinel: create %q: %w", sentinelPath, err)
	}
	return f.Close()
}

// deleteBeadsOwnedSentinel removes the ownership sentinel file for beadID
// under projectDir. A missing file (os.ErrNotExist) is treated as success —
// idempotent.
//
// This is a best-effort call: callers MUST treat any returned error as
// non-fatal. A stale sentinel left behind by a failed delete will be resolved
// by the next successful close/reopen/reset.
func deleteBeadsOwnedSentinel(projectDir, beadID string) error {
	dir := beadsOwnedDir(projectDir)
	if dir == "" {
		return nil // test caller with no projectDir — skip
	}
	sentinelPath := beadsOwnedSentinelPath(dir, beadID)
	if err := os.Remove(sentinelPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("brcli.deleteBeadsOwnedSentinel: Remove %q: %w", sentinelPath, err)
	}
	return nil
}
