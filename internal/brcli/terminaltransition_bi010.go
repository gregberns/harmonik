package brcli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

func (a *Adapter) postStateIsOurs(op core.TerminalOp, beadID core.BeadID, timeoutKills int) bool {
	if op != core.TerminalOpClaim {
		return true
	}
	if timeoutKills > 0 {
		return true
	}
	return beadsOwnedSentinelExists(a.projectDir, string(beadID))
}

// IntentLogEntrySchemaVersion is the schema version stamped on every
// adapter-owned intent-log entry written under .harmonik/beads-intents/.
// Production callers derive that directory's absolute path via
// lifecycle.BeadsIntentsDir(projectDir).
const IntentLogEntrySchemaVersion = 1

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
	ikey := core.IdempotencyKey(runID, transitionID, op)

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

	tmpPath, err := WriteIntentLogTmp(intentLogDir, entry)
	if err != nil {
		return fmt.Errorf("brcli.terminalTransitionWrite: write intent tmp: %w", err)
	}

	_, err = RenameIntentLogTmpToFinal(tmpPath, intentLogDir, ikey)
	if err != nil {
		_ = intentLogUnlinkFile(tmpPath) //nolint:errcheck // best-effort cleanup; rename failed
		return fmt.Errorf("brcli.terminalTransitionWrite: rename intent to final: %w", err)
	}

	if err := FsyncIntentLogParentDir(intentLogDir); err != nil {
		return fmt.Errorf("brcli.terminalTransitionWrite: fsync intent dir: %w", err)
	}

	retryMax, retryBase, retryCap := cfg.terminalWriteRetryParams()
	result, timeoutKills, err := a.runWithDBLockedRetryTimeoutKills(
		ctx,
		cfg,
		CommandKindWrite,
		retryMax,
		retryBase,
		retryCap,
		brArgs...,
	)
	if err != nil {
		if errors.Is(err, BrUnavailable) {
			if record, showErr := a.ShowBead(ctx, beadID); showErr == nil && record.Status == intendedPost &&
				a.postStateIsOurs(op, beadID, timeoutKills) {
				_ = DeleteIntentLogAndSyncParent(intentLogDir, ikey) //nolint:errcheck // best-effort; stale file resolved by BI-031 on next startup
				return nil
			}
		}
		return fmt.Errorf("brcli.terminalTransitionWrite: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		if record, showErr := a.ShowBead(ctx, beadID); showErr == nil && record.Status == intendedPost &&
			a.postStateIsOurs(op, beadID, timeoutKills) {
			_ = DeleteIntentLogAndSyncParent(intentLogDir, ikey) //nolint:errcheck // best-effort; stale file resolved by BI-031 on next startup
			return nil
		}
		return &TerminalWriteError{Op: string(op), Result: result}
	}

	if err := DeleteIntentLogAndSyncParent(intentLogDir, ikey); err != nil {
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
// On success, ClaimBead also writes a per-bead ownership sentinel file at
// .harmonik/beads-owned/<bead-id>. The sentinel outlives the claim intent file
// (deleted in step 6) and provides an independent provenance signal for the
// PL-006 sixth-bullet orphan sweep. The write is best-effort and non-fatal —
// a failure degrades to the existing intent-log signal.
//
// Spec: beads-integration.md §4.4 BI-010 (claim); §4.4 BI-010a (status table);
// §4.10 BI-029, BI-030; process-lifecycle.md §4.5 PL-006 sixth bullet;
// §4.4 PL-006a. Bead ref: hk-11xkn.
func (a *Adapter) ClaimBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	claimErr := classifyClaimRefusal(a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpClaim,
		core.CoarseStatusInProgress,
		[]string{"update", string(beadID), "--claim"},
	))
	if claimErr != nil {
		if errors.Is(claimErr, ErrClaimAlreadyAssigned) {
			holder, allowed := a.claimFallbackAllowed(ctx, beadID)
			if !allowed {
				return fmt.Errorf(
					"brcli.ClaimBead: %s is assigned to %q and is not open: refusing the --status in_progress fallback: %w",
					beadID, holder, claimErr,
				)
			}
			if fallbackErr := a.terminalTransitionWrite(
				ctx,
				intentLogDir,
				cfg,
				runID,
				transitionID,
				beadID,
				core.TerminalOpClaim,
				core.CoarseStatusInProgress,
				[]string{"update", string(beadID), "--status", "in_progress"},
			); fallbackErr == nil {
				_ = writeBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; see hk-11xkn
				return nil
			}
		}
		return claimErr
	}
	_ = writeBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; see hk-11xkn
	return nil
}

func (a *Adapter) claimFallbackAllowed(ctx context.Context, beadID core.BeadID) (holder string, allowed bool) {
	record, err := a.ShowBead(ctx, beadID)
	if err != nil {
		return "", false
	}
	if record.Status != core.CoarseStatusOpen {
		return record.Assignee, false
	}
	return record.Assignee, true
}

// CloseBead issues the BI-010 close write: in_progress → closed.
//
// Emitted when a run reaches terminal success AND the task branch has merged
// per workspace-model.md §4.5 WM-007. May also be emitted by the Cat 3c
// auto-resolver per BI-010b.
//
// When needsAttention is true, CloseBead applies the "needs-attention" label
// to the bead immediately after the close write succeeds. This is the
// operator-drain marker used by the review-loop close path when the cycle
// terminates without an APPROVE verdict (cap-hit, BLOCK, or no-progress
// early-exit) per execution-model.md §4.3.EM-015e and operator-nfr.md
// §4.3.ON-009a. The label write uses `br label add <bead_id> -l
// needs-attention` and routes through RunWithDBLockedRetry.
//
// When needsAttention is false, CloseBead issues the standard close write with
// no label mutation (the APPROVE success path).
//
// The full BI-030 intent-log protocol is applied to the close write.
//
// Spec: beads-integration.md §4.4 BI-010 (close); §4.4 BI-010a (status table);
// §4.4 BI-010b (reconciliation-driven writes); §4.10 BI-029, BI-030;
// §4.3.13 BI-013a (needs-attention exclusion from ready-work query);
// execution-model.md §4.3.EM-015e; operator-nfr.md §4.3.ON-009a.
func (a *Adapter) CloseBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
	needsAttention bool,
) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	if err := a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpClose,
		core.CoarseStatusClosed,
		[]string{"close", string(beadID)},
	); err != nil {
		return err
	}

	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn

	if !needsAttention {
		return nil
	}

	result, err := a.RunWithDBLockedRetry(
		ctx,
		cfg,
		CommandKindWrite,
		UnavailableRetryMax,
		UnavailableRetryBase,
		UnavailableRetryCap,
		"label", "add", string(beadID), "-l", "needs-attention",
	)
	if err != nil {
		return fmt.Errorf("brcli.CloseBead: br label add needs-attention: %w", err)
	}
	if result.BrErr != BrOK {
		return fmt.Errorf("brcli.CloseBead: br label add needs-attention failed: %w (exit %d): stderr=%q",
			result.BrErr, result.ExitCode, result.Stderr)
	}
	return nil
}

// ReopenBead issues the BI-010 reopen write: any active state → open.
//
// reason is a short human-readable string describing why the bead was
// reopened (e.g. "exit=1 run_id=<uuid>"). When non-empty it is passed as
// `br reopen --reason <reason>` so the operator can read it via `br show`
// without grepping the JSONL log (hk-amuzn). When empty the flag is omitted.
//
// Emitted on transient failure with no in-run retry available, or when a
// `reopen-bead` verdict is issued by a reconciliation investigator per
// reconciliation/spec.md §4.5 RC-020 / RC-025.
//
// `br update <bead_id> --status open` is used rather than `br reopen` because
// `br reopen` only handles the closed→open transition and silently skips beads
// that are already in_progress (e.g. after SIGINT/SIGTERM kills the handler
// mid-run). `br update --status open` works for both in_progress→open and
// closed→open, making ReopenBead reliable for crash-recovery (hk-wdeen).
//
// The full BI-030 intent-log protocol is applied. On success, the ownership
// sentinel (if any) is deleted best-effort — the bead is no longer in_progress.
//
// Spec: beads-integration.md §4.4 BI-010 (reopen); §4.4 BI-010a (status table);
// §4.10 BI-029, BI-030. Bead ref: hk-11xkn.
func (a *Adapter) ReopenBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	runID core.RunID,
	transitionID core.TransitionID,
	beadID core.BeadID,
	reason string,
) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	args := []string{"update", string(beadID), "--status", "open"}
	if reason != "" {
		args = append(args, "--notes", reason)
	}
	if err := a.terminalTransitionWrite(
		ctx,
		intentLogDir,
		cfg,
		runID,
		transitionID,
		beadID,
		core.TerminalOpReopen,
		core.CoarseStatusOpen,
		args,
	); err != nil {
		return err
	}
	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn
	return nil
}

// ResetBead issues the BI-010d reset write: in_progress → open.
//
// ResetBead is issued exclusively by the daemon startup orphan-sweep (PL-006
// extended per hk-iuaed.2) to reset stale in_progress beads belonging to this
// project. It MUST NOT be called from an in-flight run.
//
// The idempotency key for a reset write is distinct from other terminal ops:
//
//	<project_hash>:<bead_id>:reset:<daemon_start_ns>
//
// daemonStartNS scopes the key to a single daemon lifetime. Two restarts of the
// same daemon on the same project produce distinct keys, preventing a surviving
// intent file from one restart from being misclassified as ambiguous by the
// BI-031 crash-recovery scan of the next restart.
//
// The br argv is `br update <bead_id> --status open` — the same form used by
// ReopenBead — because `br update --status open` is the only `br` command that
// reliably transitions any active state (including in_progress) to open.
//
// The full BI-030 intent-log protocol is applied. The IntentLogEntry written has
// zero-valued RunID and TransitionID fields (valid per IntentLogEntry.Valid() when
// Op == TerminalOpReset) because a startup-sweep reset has no associated run or
// transition.
//
// Conflict handling follows the same BI-031 status-check protocol as
// claim/close/reopen: BrConflict retries through the status-check before reissue.
//
// Spec: beads-integration.md §4.4 BI-010d; §4.4 BI-010a (reset row);
// §4.10 BI-029, BI-030; §6.1 ENUM TerminalOp.
func (a *Adapter) ResetBead(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	beadID core.BeadID,
	projectHash core.ProjectHash,
	daemonStartNS int64,
) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	ikey := core.ResetBeadIdempotencyKey(projectHash, beadID, daemonStartNS)

	entry := core.IntentLogEntry{
		IdempotencyKey:    ikey,
		Op:                core.TerminalOpReset,
		BeadID:            beadID,
		IntendedPostState: core.CoarseStatusOpen,
		RequestedAt:       time.Now().UTC(),
		SchemaVersion:     IntentLogEntrySchemaVersion,
	}
	if !entry.Valid() {
		return fmt.Errorf("brcli.ResetBead: constructed IntentLogEntry failed Valid(): %+v", entry)
	}

	tmpPath, err := WriteIntentLogTmp(intentLogDir, entry)
	if err != nil {
		return fmt.Errorf("brcli.ResetBead: write intent tmp: %w", err)
	}

	_, err = RenameIntentLogTmpToFinal(tmpPath, intentLogDir, ikey)
	if err != nil {
		_ = intentLogUnlinkFile(tmpPath) //nolint:errcheck // best-effort cleanup; rename failed
		return fmt.Errorf("brcli.ResetBead: rename intent to final: %w", err)
	}

	if err := FsyncIntentLogParentDir(intentLogDir); err != nil {
		return fmt.Errorf("brcli.ResetBead: fsync intent dir: %w", err)
	}

	result, err := a.RunWithDBLockedRetry(
		ctx,
		cfg,
		CommandKindWrite,
		UnavailableRetryMax,
		UnavailableRetryBase,
		UnavailableRetryCap,
		"update", string(beadID), "--status", "open",
	)
	if err != nil {
		return fmt.Errorf("brcli.ResetBead: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		return fmt.Errorf("brcli.ResetBead: br update --status open failed: %w (exit %d): stderr=%q",
			result.BrErr, result.ExitCode, result.Stderr)
	}

	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn

	if err := DeleteIntentLogAndSyncParent(intentLogDir, ikey); err != nil {
		return fmt.Errorf("brcli.ResetBead: delete intent file: %w", err)
	}

	return nil
}

// SweepCloseBead issues a direct `br close <beadID>` WITHOUT the BI-030
// intent-log protocol. It is the write surface for the Cat 3c auto-reconciler
// (hk-lgtq2): closing a subsumed bead that is IN_PROGRESS but whose
// implementation has already merged to the target branch.
//
// Unlike CloseBead, there is no associated in-flight run — no RunID or
// TransitionID exists — so the BI-030 intent-log protocol (steps 1–6)
// cannot be applied. Idempotency is provided at the Beads level: a closed
// bead will not appear in the next startup's `br list --status in_progress`
// query, so a crash after `br close` succeeds but before the sweep completes
// simply means the bead is already closed on the next startup.
//
// After a successful close SweepCloseBead applies the "needs-attention" label
// (H3): a Cat 3c auto-close is a DAEMON inference — "a trailer-bearing commit is
// present and unreverted" — not an explicit operator/reviewer sign-off, so the
// closed bead is flagged for operator triage rather than treated as a clean DONE.
// The label write mirrors CloseBead's needs-attention path and routes through the
// same DB-locked retry; if it fails the bead is already closed but unflagged, so
// the caller MUST treat that as an error.
//
// Implements lifecycle.BeadCat3cCloser.
//
// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
func (a *Adapter) SweepCloseBead(
	ctx context.Context,
	cfg TimeoutConfig,
	beadID core.BeadID,
) error {
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	result, err := a.RunWithDBLockedRetry(
		ctx, cfg, CommandKindWrite,
		DBLockedRetryMax, DBLockedRetryBase, DBLockedRetryCap,
		"close", string(beadID),
	)
	if err != nil {
		return fmt.Errorf("brcli.SweepCloseBead: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		return fmt.Errorf("brcli.SweepCloseBead: br close %s failed: %w (exit %d): stderr=%q",
			beadID, result.BrErr, result.ExitCode, string(result.Stderr))
	}

	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn

	labelResult, labelErr := a.RunWithDBLockedRetry(
		ctx, cfg, CommandKindWrite,
		UnavailableRetryMax, UnavailableRetryBase, UnavailableRetryCap,
		"label", "add", string(beadID), "-l", "needs-attention",
	)
	if labelErr != nil {
		return fmt.Errorf("brcli.SweepCloseBead: br label add needs-attention: %w", labelErr)
	}
	if labelResult.BrErr != BrOK {
		return fmt.Errorf("brcli.SweepCloseBead: br label add needs-attention failed: %w (exit %d): stderr=%q",
			labelResult.BrErr, labelResult.ExitCode, string(labelResult.Stderr))
	}
	return nil
}
