package brcli

import (
	"context"
	"fmt"
)

// SyncFlushOnly invokes `br sync --flush-only` to export the SQLite database
// to JSONL. It is called at daemon startup BEFORE any ShowBead / ListInFlightBeads
// queries run (QM-002a / QM-002b reconciliation) to ensure the ledger is settled
// and the database is accessible.
//
// Background (hk-n2y, logmine F40): immediately after a daemon restart the
// SQLite database may be momentarily locked (by a lingering write transaction
// from the previous process). During that window every `br show` invocation
// returns exit 3 with empty stdout, which the reconciliation loop logs as
// ~31 consecutive "QM-002b Class C: ShowBead failed (br exit 3)" warnings.
// Completing a `br sync --flush-only` before the reconciliation loop starts
// forces a full database round-trip and clears the transient lock, so the
// subsequent ShowBead calls succeed without noise.
//
// Error semantics:
//   - Exec failure (br not on PATH, fork error) → wrapped error
//   - Non-zero br exit (any reason)             → wrapped ErrBrSyncFailed
//
// The caller SHOULD treat a non-nil return as a non-fatal warning: the
// reconciliation loop continues regardless (it tolerates ShowBead failures),
// so a sync failure degrades gracefully to the pre-F40 behaviour.
func (a *Adapter) SyncFlushOnly(ctx context.Context) error {
	result, err := a.Run(ctx, "sync", "--flush-only")
	if err != nil {
		return fmt.Errorf("brcli.SyncFlushOnly: exec failed: %w", err)
	}
	if result.ExitCode != 0 {
		truncated := result.Stderr
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		if len(truncated) == 0 {
			truncated = result.Stdout
			if len(truncated) > 200 {
				truncated = truncated[:200]
			}
		}
		return fmt.Errorf("brcli.SyncFlushOnly: br sync --flush-only exited %d: %s: %w",
			result.ExitCode,
			string(truncated),
			ErrBrSyncFailed,
		)
	}
	return nil
}

// ErrBrSyncFailed is returned by SyncFlushOnly when `br sync` exits non-zero.
var ErrBrSyncFailed = fmt.Errorf("brcli: br sync failed")
