package brcli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// terminaltransition_bi010.go — BI-010 terminal-transition write surface.
//
// Implements the four terminal-transition write methods on Adapter:
//   - ClaimBead  — claim:  open → in_progress   (run_started dispatch)
//   - CloseBead  — close:  in_progress → closed  (run_completed + merge)
//   - ReopenBead — reopen: closed → open         (reopen-bead verdict / transient failure)
//   - ResetBead  — reset:  in_progress → open    (daemon startup orphan-sweep per BI-010d)
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
//   - reopen: `br update <bead_id> --status open`
//   - reset:  `br update <bead_id> --status open`
//
// All writes route through RunWithDBLockedRetry (BI-025c retry discipline).
// The transition kind is CommandKindWrite (10s default timeout).
//
// Spec refs: specs/beads-integration.md §4.4 BI-010; §4.4 BI-010d (reset);
// §4.10 BI-029, BI-030, BI-031; §6.1 RECORD IntentLogEntry.

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
	// Terminal-transition writes use UnavailableRetryMax (10) instead of
	// DBLockedRetryMax (3): dogfood run hk-75rij showed that 3 retries are
	// insufficient under concurrent kerf/agent SQLite contention (hk-ekz5v).
	// The BI-030 intent-log backing provides idempotency across all retries.
	// cfg.terminalWriteRetryParams() applies operator defaults but allows tests
	// to inject small values to keep retry-exhaustion scenarios fast.
	// Spec ref: beads-integration.md §4.10 BI-031 step (4c-transient).
	retryMax, retryBase, retryCap := cfg.terminalWriteRetryParams()
	result, err := a.RunWithDBLockedRetry(
		ctx,
		cfg,
		CommandKindWrite,
		retryMax,
		retryBase,
		retryCap,
		brArgs...,
	)
	if err != nil {
		// Retry budget exhausted (BrUnavailable) or exec failure.
		// Idempotency check (hk-cw4sx): under concurrent --wave, N workers may
		// call br close simultaneously; one succeeds while others exhaust retries.
		// If ShowBead confirms the bead is already in the intended post-state,
		// the failure is a false-negative — treat as success and delete intent file.
		// Only check on BrUnavailable (retry exhaustion); propagate context
		// cancellation and exec errors without the extra ShowBead round-trip.
		if errors.Is(err, BrUnavailable) {
			if record, showErr := a.ShowBead(ctx, beadID); showErr == nil && record.Status == intendedPost {
				_ = DeleteIntentLogAndSyncParent(intentLogDir, ikey) //nolint:errcheck // best-effort; stale file resolved by BI-031 on next startup
				return nil
			}
		}
		// Intent file retained for BI-031 recovery.
		return fmt.Errorf("brcli.terminalTransitionWrite: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		// Non-retriable non-zero exit (e.g. BrConflict, BrNotFound).
		// Same idempotency check: a concurrent write may have already landed
		// (hk-cw4sx). If ShowBead confirms the intended post-state, treat as success.
		if record, showErr := a.ShowBead(ctx, beadID); showErr == nil && record.Status == intendedPost {
			_ = DeleteIntentLogAndSyncParent(intentLogDir, ikey) //nolint:errcheck // best-effort; stale file resolved by BI-031 on next startup
			return nil
		}
		// Intent file retained for BI-031 recovery.
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
	// Serialize terminal-transition writes to prevent concurrent `br` invocations
	// from racing on the SQLite .write.lock (hk-hdbls).
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	claimErr := a.terminalTransitionWrite(
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
	if claimErr != nil {
		// hk-amed0: br --claim rejects beads that already have an assignee
		// (exit 4, "Validation failed: claim: issue <id> already assigned to <name>").
		// This happens when a crew creates a bead with `br create --assignee <crew>` —
		// a normal pattern. Fall back to `br update --status in_progress` which
		// transitions the status without requiring the assignee to be unset.
		// The same intent-log entry (same ikey) covers both writes, so BI-030
		// idempotency is maintained.
		if strings.Contains(claimErr.Error(), "already assigned") {
			fmt.Printf("brcli.ClaimBead: %s --claim rejected (pre-assigned assignee); retrying with --status in_progress (hk-amed0)\n", beadID)
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
	// Write ownership sentinel (best-effort; non-fatal if projectDir is empty
	// or the write fails — falls back to intent-log provenance signal).
	_ = writeBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; see hk-11xkn
	return nil
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
	// Serialize terminal-transition writes to prevent concurrent `br` invocations
	// from racing on the SQLite .write.lock (hk-hdbls). The lock covers both the
	// close write and the needs-attention label write so the two br calls are
	// atomic from a contention perspective.
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

	// Delete ownership sentinel (best-effort; bead is no longer in_progress).
	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn

	if !needsAttention {
		return nil
	}

	// Apply needs-attention label: operator-drain marker per EM-015e / ON-009a.
	// Routes through RunWithDBLockedRetry with UnavailableRetryMax (10) —
	// same retry discipline as the close write (hk-ekz5v). The label write is
	// a separate br invocation; if it fails the bead is already closed but the
	// operator-drain marker is absent — the caller MUST treat this as an error
	// (the bead could be re-dispatched without operator triage, violating
	// ON-009a's no-auto-retry constraint).
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
		return fmt.Errorf("brcli.CloseBead: br label add needs-attention failed: %s (exit %d): stderr=%q",
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
	// Serialize terminal-transition writes to prevent concurrent `br` invocations
	// from racing on the SQLite .write.lock (hk-hdbls).
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
	// Delete ownership sentinel (best-effort; bead is no longer in_progress).
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
	// Serialize terminal-transition writes to prevent concurrent `br` invocations
	// from racing on the SQLite .write.lock (hk-hdbls).
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	ikey := core.ResetBeadIdempotencyKey(projectHash, beadID, daemonStartNS)

	// BI-030 step 1: build IntentLogEntry for reset.
	// RunID and TransitionID are zero-valued: a startup-sweep reset has no
	// associated in-flight run or transition. IntentLogEntry.Valid() permits
	// zero UUIDs when Op == TerminalOpReset per BI-010d.
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

	// BI-030 steps 1–2: write intent file to temp + fsync(temp_fd).
	tmpPath, err := WriteIntentLogTmp(intentLogDir, entry)
	if err != nil {
		return fmt.Errorf("brcli.ResetBead: write intent tmp: %w", err)
	}

	// BI-030 step 3: rename(2) temp → canonical path.
	_, err = RenameIntentLogTmpToFinal(tmpPath, intentLogDir, ikey)
	if err != nil {
		_ = intentLogUnlinkFile(tmpPath) //nolint:errcheck // best-effort cleanup; rename failed
		return fmt.Errorf("brcli.ResetBead: rename intent to final: %w", err)
	}

	// BI-030 step 4: fsync(parent_dir_fd) — durability before subprocess.
	if err := FsyncIntentLogParentDir(intentLogDir); err != nil {
		return fmt.Errorf("brcli.ResetBead: fsync intent dir: %w", err)
	}

	// BI-030 step 5: invoke `br update <bead_id> --status open`.
	// Uses the same argv as ReopenBead: `br update --status open` is the only
	// br command that reliably transitions in_progress → open (hk-wdeen).
	// Uses UnavailableRetryMax (10) — same rationale as terminalTransitionWrite
	// (hk-ekz5v): terminal writes back intent-log idempotency across retries.
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
		// Subprocess not launched; intent file retained for BI-031 recovery.
		return fmt.Errorf("brcli.ResetBead: br exec: %w", err)
	}
	if result.BrErr != BrOK {
		// br returned a non-zero exit code; intent file retained for BI-031 recovery.
		return fmt.Errorf("brcli.ResetBead: br update --status open failed: %s (exit %d): stderr=%q",
			result.BrErr, result.ExitCode, result.Stderr)
	}

	// BI-030 step 6: delete intent file + fsync(parent_dir_fd) on success.
	if err := DeleteIntentLogAndSyncParent(intentLogDir, ikey); err != nil {
		// Non-fatal: the write succeeded; the stale intent file will be
		// resolved as a no-op by the BI-031 status-check-before-reissue
		// protocol on next startup.
		return fmt.Errorf("brcli.ResetBead: delete intent file: %w", err)
	}

	// Delete ownership sentinel (best-effort; bead is back to open).
	_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn

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
	// Serialize terminal-transition writes to prevent concurrent `br` invocations
	// from racing on the SQLite .write.lock (hk-hdbls). The lock covers both the
	// close write and the needs-attention label write.
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
		return fmt.Errorf("brcli.SweepCloseBead: br close %s failed: %s (exit %d): stderr=%q",
			beadID, result.BrErr, result.ExitCode, string(result.Stderr))
	}

	// Flag the auto-closed bead for operator review (H3). A Cat 3c close is a
	// daemon inference, not an explicit sign-off, so mark it needs-attention.
	labelResult, labelErr := a.RunWithDBLockedRetry(
		ctx, cfg, CommandKindWrite,
		UnavailableRetryMax, UnavailableRetryBase, UnavailableRetryCap,
		"label", "add", string(beadID), "-l", "needs-attention",
	)
	if labelErr != nil {
		return fmt.Errorf("brcli.SweepCloseBead: br label add needs-attention: %w", labelErr)
	}
	if labelResult.BrErr != BrOK {
		return fmt.Errorf("brcli.SweepCloseBead: br label add needs-attention failed: %s (exit %d): stderr=%q",
			labelResult.BrErr, labelResult.ExitCode, string(labelResult.Stderr))
	}
	return nil
}
