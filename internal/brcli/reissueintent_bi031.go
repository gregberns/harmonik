package brcli

// reissueintent_bi031.go — BI-031 step-4 re-drive: re-issue a stale pre-state
// terminal-transition write at adapter startup.
//
// At daemon startup, GCRetiredIntents finds intent files whose bead is still at
// the pre-state for the recorded op (the prior `br` invocation was interrupted
// before it completed). The spec (beads-integration.md §4.10 BI-031 step 4)
// requires the adapter to re-issue the `br` write using the same idempotency_key
// rather than leaving the intent file for Cat 3a reconciliation.
//
// This method skips BI-030 steps 1–4 (the intent file is already durably
// written on disk from the prior run) and proceeds directly to step 5 (invoke
// `br`) and step 6 (delete the intent file on success).
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a–4f).
// Bead ref: hk-aev8t (G3 fix — step-4 re-drive missing).

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
	// Derive the br argv for this op — same args used by the original write.
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

	// Serialize with concurrent terminal writes per BI-025e (hk-hdbls).
	a.terminalMu.Lock()
	defer a.terminalMu.Unlock()

	// BI-031 step 5 re-issue: invoke br with the UnavailableRetryMax budget
	// (step 4c-transient) via RunWithDBLockedRetry.  The BI-030 intent file
	// backing provides idempotency across all retry attempts.
	retryMax, retryBase, retryCap := cfg.terminalWriteRetryParams()
	result, err := a.RunWithDBLockedRetry(
		ctx, cfg, CommandKindWrite, retryMax, retryBase, retryCap, brArgs...,
	)
	if err != nil {
		// (4d) BrUnavailable — retry budget exhausted or binary missing.
		// Intent file retained; daemon will degrade per ON-037.
		return fmt.Errorf("brcli.ReissueTerminalTransition: br unavailable (op=%s bead=%s): %w", entry.Op, entry.BeadID, err)
	}

	switch result.BrErr {
	case BrOK:
		// (4a) Write completed successfully.  BI-031 step 6: delete intent file.
		if delErr := DeleteIntentLogAndSyncParent(intentLogDir, entry.IdempotencyKey); delErr != nil {
			// Write succeeded; stale intent file will be resolved by BI-031 GC
			// on the next startup (gcIntentOpLanded will return true).
			return fmt.Errorf("brcli.ReissueTerminalTransition: step-6 delete intent (op=%s bead=%s): %w", entry.Op, entry.BeadID, delErr)
		}
		return nil

	case BrConflict:
		// (4b) A concurrent writer may have landed the transition between step 2
		// and step 4.  Re-execute step 3: re-read ShowBead and check whether the
		// bead has reached IntendedPostState.  terminalMu is already held so no
		// new concurrent writes can race during the read.
		record, showErr := a.ShowBead(ctx, entry.BeadID)
		if showErr == nil && record.Status == entry.IntendedPostState {
			// Post-state confirmed.  BI-031 step 6: delete intent file.
			// best-effort: stale file resolved by BI-031 GC on next startup if this fails.
			_ = DeleteIntentLogAndSyncParent(intentLogDir, entry.IdempotencyKey) //nolint:errcheck
			return nil
		}
		// Cannot confirm post-state — retain intent for Cat 3a auto-resolver.
		return fmt.Errorf("brcli.ReissueTerminalTransition: BrConflict (op=%s bead=%s): post-state unconfirmed — retaining intent for Cat 3a", entry.Op, entry.BeadID)

	default:
		// (4e) BrSchemaMismatch — schema drift; divergence_inconclusive.
		// (4f) BrOther — unrecognised exit; divergence_inconclusive; Cat 6b.
		// In both cases the intent file is retained so reconciliation can route
		// appropriately.
		return fmt.Errorf("brcli.ReissueTerminalTransition: op=%s bead=%s br error %s (exit %d): retaining intent for Cat 3a/6b routing", entry.Op, entry.BeadID, result.BrErr, result.ExitCode)
	}
}
