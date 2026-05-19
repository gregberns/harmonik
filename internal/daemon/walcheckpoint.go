package daemon

// walcheckpoint.go — advisory WAL-checkpoint pre-flight for daemon startup.
//
// # Why
//
// SQLite WAL (write-ahead log) grows unbounded between automatic checkpoints.
// Under normal operation beads.db accumulates a WAL file as `br` issues writes;
// if the WAL is never checkpointed (e.g. between short-lived br invocations),
// write latency grows proportionally to WAL size. In dogfood-2/3/4 runs this
// manifested as `br close` taking 19.4s on a 32MB DB with a 12MB WAL — well
// above the 10s wall-clock timeout in brcli/timeout.go.
//
// A PRAGMA wal_checkpoint(TRUNCATE) issued before the first br write drops the
// WAL to 0 bytes and restores sub-second write latency. This pre-flight is
// advisory: if sqlite3 is not on PATH, or if the checkpoint fails for any other
// reason, the daemon continues (non-fatal). The operator sees a structured-log
// warning in both cases.
//
// # Threshold
//
// The check fires when beads.db-wal exists AND is larger than walCheckpointThreshold
// (1 MB). Below the threshold the overhead of spawning sqlite3 is skipped.
//
// # Config escape hatch
//
// Config.SkipWALCheckpoint = true disables the pre-flight entirely. This is
// intended for unit tests that operate on fake or absent databases and would
// be disturbed by an sqlite3 exec invocation.
//
// Bead ref: hk-5dewt.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// walCheckpointThreshold is the minimum WAL size that triggers the advisory
// checkpoint pre-flight. WALs smaller than this threshold are inexpensive to
// replay on next write, so the sqlite3 spawn is skipped.
const walCheckpointThreshold = 1 << 20 // 1 MB

// walCheckpointResult captures the outcome of a single checkpoint attempt.
type walCheckpointResult struct {
	skipped      bool
	reason       string
	sizeMB       float64
	newSizeBytes int64
	duration     time.Duration
	err          error
}

// runWALCheckpointPreflight checks whether the beads.db WAL file exceeds
// walCheckpointThreshold and, if so, runs PRAGMA wal_checkpoint(TRUNCATE) via
// the sqlite3 CLI. All outcomes (skip, success, failure) are logged via slog.
// The function is always non-fatal: it returns nil regardless of whether the
// checkpoint ran or succeeded.
//
// ctx is forwarded to exec.CommandContext so that daemon context cancellation
// is respected (e.g. on a very early SIGTERM).
//
// projectDir must be the harmonik project root (the directory that contains
// .beads/beads.db).
func runWALCheckpointPreflight(ctx context.Context, projectDir string) error {
	walPath := filepath.Join(projectDir, ".beads", "beads.db-wal")
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")

	// Stat the WAL file.
	//nolint:gosec // G304: walPath is constructed from operator-supplied projectDir; not user input.
	walInfo, statErr := os.Stat(walPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			// No WAL file — nothing to do.
			slog.InfoContext(ctx, "wal_checkpoint_skip", "reason", "wal_absent")
			return nil
		}
		// Unexpected stat error — log and continue.
		slog.WarnContext(ctx, "wal_checkpoint_stat_error",
			"wal_path", walPath,
			"error", statErr.Error(),
		)
		return nil
	}

	walSize := walInfo.Size()
	if walSize < walCheckpointThreshold {
		// WAL is small — skip the spawn.
		slog.InfoContext(ctx, "wal_checkpoint_skip",
			"reason", "below_threshold",
			"wal_size_bytes", walSize,
			"threshold_bytes", walCheckpointThreshold,
		)
		return nil
	}

	sizeMB := float64(walSize) / float64(1<<20)
	slog.WarnContext(ctx, "wal_checkpoint_started",
		"wal_path", walPath,
		"size_mb", fmt.Sprintf("%.2f", sizeMB),
	)

	// Resolve the sqlite3 binary.
	sqlite3Path, lookErr := exec.LookPath("sqlite3")
	if lookErr != nil {
		slog.WarnContext(ctx, "wal_checkpoint_skipped_no_sqlite3",
			"reason", "sqlite3_not_on_PATH",
			"wal_size_mb", fmt.Sprintf("%.2f", sizeMB),
		)
		return nil
	}

	start := time.Now()

	// Run PRAGMA wal_checkpoint(TRUNCATE) via sqlite3. The -cmd flag executes
	// the pragma and then sqlite3 exits; no further interaction is needed.
	//
	// We use exec.CommandContext so that ctx cancellation (e.g. daemon shutdown
	// before the WAL is fully checkpointed) cancels the subprocess.
	//
	//nolint:gosec // G204: sqlite3Path is resolved via exec.LookPath; dbPath is constructed from operator-supplied projectDir; not user input.
	cmd := exec.CommandContext(ctx, sqlite3Path, "-cmd", "PRAGMA wal_checkpoint(TRUNCATE);", dbPath, ".quit")
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		slog.WarnContext(ctx, "wal_checkpoint_failed",
			"error", runErr.Error(),
			"output", string(out),
			"wal_path", walPath,
			"size_mb", fmt.Sprintf("%.2f", sizeMB),
		)
		return nil
	}

	duration := time.Since(start)

	// Stat the WAL again to report the post-checkpoint size.
	var newSizeBytes int64
	//nolint:gosec // G304: walPath constructed from operator-supplied projectDir; not user input.
	if postInfo, postErr := os.Stat(walPath); postErr == nil {
		newSizeBytes = postInfo.Size()
	}

	slog.InfoContext(ctx, "wal_checkpoint_completed",
		"duration_ms", duration.Milliseconds(),
		"new_size_bytes", newSizeBytes,
		"wal_path", walPath,
	)
	return nil
}
