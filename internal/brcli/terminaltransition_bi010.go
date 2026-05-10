package brcli

import (
	"context"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// terminaltransition_bi010.go — BI-010 terminal-transition write surface.
//
// Implements the three terminal-transition write methods on Adapter:
//   - ClaimBead  — claim:  open → in_progress   (run_started dispatch)
//   - CloseBead  — close:  in_progress → closed  (run_completed + merge)
//   - ReopenBead — reopen: closed → open         (reopen-bead verdict / transient failure)
//
// Each method follows the BI-030 idempotency protocol (steps 1–6):
//  1. Build IntentLogEntry.
//  2. Write intent file to temp file + fsync(temp_fd).
//  3. rename(2) temp → canonical <encoded_key>.json.
//  4. fsync(parent_dir_fd).
//  5. Invoke `br` with the appropriate status-change argv.
//  6. On success: unlink intent file + fsync(parent_dir_fd).
//
// br argv:
//   - claim:  `br update <bead_id> --claim`   (atomic assignee=actor + status=in_progress)
//   - close:  `br close <bead_id>`
//   - reopen: `br reopen <bead_id>`
//
// All writes route through RunWithDBLockedRetry (BI-025c retry discipline).
// The transition kind is CommandKindWrite (10s default timeout).
//
// Spec refs: specs/beads-integration.md §4.4 BI-010; §4.10 BI-029, BI-030,
// BI-031; §6.1 RECORD IntentLogEntry.

// IntentLogDir is the adapter-owned intent-log directory path relative to the
// .harmonik/ directory. Production callers derive the absolute path via
// lifecycle.BeadsIntentsDir(projectDir).
const IntentLogEntrySchemaVersion = 1

// terminalTransitionWrite executes a full BI-030 terminal-transition write for
// the given op against beadID. It is the shared implementation underlying
// ClaimBead, CloseBead, and ReopenBead.
//
// Parameters:
//   - ctx             — governs the `br` subprocess lifetime.
//   - intentLogDir    — absolute path to .harmonik/beads-intents/.
//   - cfg             — BI-025c timeout configuration (write timeout applies).
//   - runID           — UUIDv7 identifier of the dispatching run.
//   - transitionID    — UUIDv7 identifier of the dispatch transition.
//   - beadID          — target bead (opaque per BI-008a).
//   - op              — one of TerminalOpClaim, TerminalOpClose, TerminalOpReopen.
//   - intendedPost    — the Beads status expected after the write.
//   - brArgs          — the `br` CLI args for this op (e.g. ["update", "<id>", "--claim"]).
//
// Returns nil on success. On `br` subprocess failure, the intent file is
// retained for BI-031 crash-recovery (the intent file is the crash-recovery
// sentinel; callers MUST NOT retry without a fresh idempotency key unless they
// implement the BI-031 status-check-before-reissue protocol).
func (a *Adapter) terminalTransitionWrite(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
	op core.TerminalOp,
	intendedPost core.CoarseStatus,
	brArgs []string,
) error {
	// BI-029: derive deterministic idempotency key.
	ikey := core.IdempotencyKey(runID, transitionID, op)

	// BI-030 step 1: build IntentLogEntry.
	entry := core.IntentLogEntry{
		IdempotencyKey:    ikey,
		RunID:             runID,
		TransitionID:      transitionID,
		Op:                op,
		BeadID:            beadID,
		IntendedPostState: intendedPost,
		RequestedAt:       time.Now().UTC(),
		SchemaVersion:     IntentLogEntrySchemaVersion,
	}
	if !entry.Valid() {
		return fmt.Errorf("brcli.terminalTransitionWrite: constructed IntentLogEntry failed Valid(): %+v", entry)
	}

	// BI-030 steps 1–2: write intent file to temp + fsync(temp_fd).
	tmpPath, err := WriteIntentLogTmp(intentLogDir, entry)
	if err != nil {
		return fmt.Errorf("brcli.terminalTransitionWrite: write intent tmp: %w", err)
	}

	// BI-030 step 3: rename(2) temp → canonical path.
	_, err = RenameIntentLogTmpToFinal(tmpPath, intentLogDir, ikey)
	if err != nil {
		// Temp file may still exist; best-effort cleanup.
		_ = intentLogUnlinkFile(tmpPath) //nolint:errcheck // best-effort cleanup; rename failed
		return fmt.Errorf("brcli.terminalTransitionWrite: rename intent to final: %w", err)
	}

	// BI-030 step 4: fsync(parent_dir_fd) — durability before subprocess.
	if err := FsyncIntentLogParentDir(intentLogDir); err != nil {
		return fmt.Errorf("brcli.terminalTransitionWrite: fsync intent dir: %w", err)
	}

	// BI-030 step 5: invoke `br` with the status-change argv.
	result, err := a.RunWithDBLockedRetry(
		ctx,
		cfg,
		CommandKindWrite,
		DBLockedRetryMax,
		DBLockedRetryBase,
		DBLockedRetryCap,
		brArgs...,
	)
	if err != nil {
		// Subprocess not launched; intent file retained for BI-031 recovery.
		return fmt.Errorf("brcli.terminalTransitionWrite: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		// br returned a non-zero exit code; intent file retained for BI-031 recovery.
		return fmt.Errorf("brcli.terminalTransitionWrite: br %s failed: %s (exit %d): stderr=%q",
			op, result.BrErr, result.ExitCode, result.Stderr)
	}

	// BI-030 step 6: delete intent file + fsync(parent_dir_fd) on success.
	if err := DeleteIntentLogAndSyncParent(intentLogDir, ikey); err != nil {
		// Non-fatal: the write succeeded; the stale intent file will be
		// resolved as a no-op by the BI-031 status-check-before-reissue
		// protocol on next startup.
		return fmt.Errorf("brcli.terminalTransitionWrite: delete intent file: %w", err)
	}

	return nil
}

// ClaimBead issues the BI-010 claim write: open → in_progress.
//
// The `br update <bead_id> --claim` form is used: it atomically sets
// status=in_progress AND assignee=actor, matching the BI-010a claim row.
//
// The full BI-030 intent-log protocol is applied: intent file is written
// before the br invocation and deleted on success.
//
// Spec: beads-integration.md §4.4 BI-010 (claim); §4.4 BI-010a (status table);
// §4.10 BI-029, BI-030.
func (a *Adapter) ClaimBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
) error {
	return a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpClaim,
		core.CoarseStatusInProgress,
		[]string{"update", string(beadID), "--claim"},
	)
}

// CloseBead issues the BI-010 close write: in_progress → closed.
//
// Emitted when a run reaches terminal success AND the task branch has merged
// per workspace-model.md §4.5 WM-007. May also be emitted by the Cat 3c
// auto-resolver per BI-010b.
//
// The full BI-030 intent-log protocol is applied.
//
// Spec: beads-integration.md §4.4 BI-010 (close); §4.4 BI-010a (status table);
// §4.4 BI-010b (reconciliation-driven writes); §4.10 BI-029, BI-030.
func (a *Adapter) CloseBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
) error {
	return a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpClose,
		core.CoarseStatusClosed,
		[]string{"close", string(beadID)},
	)
}

// ReopenBead issues the BI-010 reopen write: closed → open.
//
// Emitted on transient failure with no in-run retry available, or when a
// `reopen-bead` verdict is issued by a reconciliation investigator per
// reconciliation/spec.md §4.5 RC-020 / RC-025.
//
// The full BI-030 intent-log protocol is applied.
//
// Spec: beads-integration.md §4.4 BI-010 (reopen); §4.4 BI-010a (status table);
// §4.10 BI-029, BI-030.
func (a *Adapter) ReopenBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
) error {
	return a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpReopen,
		core.CoarseStatusOpen,
		[]string{"reopen", string(beadID)},
	)
}
