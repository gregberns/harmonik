package brcli

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ReissueTerminalTransition re-issues the `br` write described by an existing
// intent-log entry (BI-031 step 4). entry is the IntentLogEntry read from the
// stale intent file; the intent file is assumed already durably on disk from the
// prior run (BI-030 steps 1–4 done). Only the `br` invocation (step 5) and the
// intent-file delete on success (step 6) are performed here.
//
// The method uses the same retrial budget as other terminal-transition writes
// (UnavailableRetryMax = 10 per BI-031 step 4c-transient). cfg zero value
// applies the BI-025c defaults.
//
// Return semantics (nil = success; intent file deleted):
//
//	(4a) BrOK          — step 6 delete intent file + return nil.
//	(4b) BrConflict    — re-read ShowBead; if at IntendedPostState delete +
//	                      return nil; else return error (retain for Cat 3a).
//	(4c/4c-transient)  — handled by RunWithDBLockedRetry retry budget; on
//	                      exhaustion wraps BrUnavailable → (4d).
//	(4d) BrUnavailable — return error (intent retained; daemon degraded).
//	(4e) BrSchemaMismatch / (4f) BrOther — return error (intent retained for
//	                      Cat 3a / Cat 6b routing).
//
// ReissueTerminalTransition acquires terminalMu for the duration of the br
// invocation, consistent with the BI-025e terminal-write serialization rule.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a–4f).
func (a *Adapter) ReissueTerminalTransition(
	ctx context.Context,
	intentLogDir string,
	cfg TimeoutConfig,
	entry core.IntentLogEntry,
) error {
	var brArgs []string
	switch entry.Op {
	case core.TerminalOpClaim:
		brArgs = []string{"update", string(entry.BeadID), "--claim"}
	case core.TerminalOpClose:
		brArgs = []string{"close", string(entry.BeadID)}
	case core.TerminalOpReopen:
		brArgs = []string{"update", string(entry.BeadID), "--status", "open"}
	case core.TerminalOpReset:
		brArgs = []string{"update", string(entry.BeadID), "--status", "open"}
	default:
		return fmt.Errorf("brcli.ReissueTerminalTransition: unsupported op %q for bead %s", entry.Op, entry.BeadID)
	}

	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	retryMax, retryBase, retryCap := cfg.terminalWriteRetryParams()
	result, err := a.RunWithDBLockedRetry(
		ctx, cfg, CommandKindWrite, retryMax, retryBase, retryCap, brArgs...,
	)
	if err != nil {
		return fmt.Errorf("brcli.ReissueTerminalTransition: br unavailable (op=%s bead=%s): %w", entry.Op, entry.BeadID, err)
	}

	switch result.BrErr {
	case BrOK:
		a.syncOwnershipSentinel(entry.Op, entry.BeadID)
		if delErr := DeleteIntentLogAndSyncParent(intentLogDir, entry.IdempotencyKey); delErr != nil {
			return fmt.Errorf("brcli.ReissueTerminalTransition: step-6 delete intent (op=%s bead=%s): %w", entry.Op, entry.BeadID, delErr)
		}
		return nil

	case BrConflict:
		record, showErr := a.ShowBead(ctx, entry.BeadID)
		if showErr == nil && record.Status == entry.IntendedPostState {
			a.syncOwnershipSentinel(entry.Op, entry.BeadID)
			_ = DeleteIntentLogAndSyncParent(intentLogDir, entry.IdempotencyKey) //nolint:errcheck // best-effort; startup GC resolves a retained intent
			return nil
		}
		return fmt.Errorf("brcli.ReissueTerminalTransition: BrConflict (op=%s bead=%s): post-state unconfirmed — retaining intent for Cat 3a", entry.Op, entry.BeadID)

	default:
		return fmt.Errorf("brcli.ReissueTerminalTransition: op=%s bead=%s br error %w (exit %d): retaining intent for Cat 3a/6b routing", entry.Op, entry.BeadID, result.BrErr, result.ExitCode)
	}
}

func (a *Adapter) syncOwnershipSentinel(op core.TerminalOp, beadID core.BeadID) {
	switch op {
	case core.TerminalOpClaim:
		_ = writeBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn
	case core.TerminalOpClose, core.TerminalOpReopen, core.TerminalOpReset:
		_ = deleteBeadsOwnedSentinel(a.projectDir, string(beadID)) //nolint:errcheck // best-effort; hk-11xkn
	}
}
