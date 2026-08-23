package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const walCheckpointThreshold = 1 << 20 // 1 MB

type walCheckpointResult struct {
	skipped      bool
	reason       string
	sizeMB       float64
	newSizeBytes int64
	duration     time.Duration
	err          error
}

func runWALCheckpointPreflight(ctx context.Context, projectDir string) error {
	walPath := filepath.Join(projectDir, ".beads", "beads.db-wal")
	dbPath := filepath.Join(projectDir, ".beads", "beads.db")

	walInfo, statErr := os.Stat(walPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			slog.InfoContext(ctx, "wal_checkpoint_skip", "reason", "wal_absent")
			return nil
		}
		slog.WarnContext(ctx, "wal_checkpoint_stat_error",
			"wal_path", walPath,
			"error", statErr.Error(),
		)
		return nil
	}

	walSize := walInfo.Size()
	if walSize < walCheckpointThreshold {
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

	var newSizeBytes int64
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
