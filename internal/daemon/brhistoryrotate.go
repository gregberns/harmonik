package daemon

// brhistoryrotate.go — .br_history/ rotation pre-flight for daemon startup.
//
// # Why
//
// Each `br` write appends a ~1.2 MB snapshot to .beads/.br_history/. With 200+
// entries (226 MB) br must scan the entire directory on every write, pushing
// `br close` wall-clock time to ~19.5 s on the dogfood corpus — exceeding the
// 10 s timeout in brcli/timeout.go regardless of WAL state.
//
// Archiving old snapshots to .beads/.br_history-archive/ before the first br
// write restores sub-second write latency. In validation, archiving 200 entries
// dropped `br close` from 19.5 s to 0.15 s.
//
// # Policy
//
// Keep the K most recent entries (by mtime) in .beads/.br_history/.  Archive
// (rename into .beads/.br_history-archive/<name>.archived-<ts>) all older
// entries.  Archiving is preferred over hard-delete so that snapshots are not
// irreversibly destroyed on the first version.
//
// # Config escape hatch
//
// Config.SkipBrHistoryRotation = true disables the pre-flight entirely.  This
// is intended for unit tests that operate on temp directories where the history
// dir is either absent or populated with controlled fixtures.
//
// Bead ref: hk-5dewt.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// brHistoryRotationDefaultKeep is the number of most-recent snapshots to retain
// in .beads/.br_history/ during the rotation pre-flight.  All others are moved
// to .beads/.br_history-archive/.
//
// 20 entries covers a few daemon sessions of history without allowing unbounded
// growth.  The caller of runBrHistoryRotationPreflight in daemon.Start passes
// this constant; it is exported only for test readability.
const brHistoryRotationDefaultKeep = 20

// brHistoryCloseTrimKeep is the tighter per-close trim threshold applied
// immediately before each CloseBead call (hk-hypbi).  Smaller than
// brHistoryRotationDefaultKeep (20) so that in-session growth — each br write
// appends ~1.2 MB — never pushes br close past the 10 s write timeout.
//
// Root cause (hk-hypbi / hk-5dewt): startup rotation trims to 20 entries, but
// subsequent bead closes add new entries.  By ~20 dispatches the history is
// back to 40+ entries, and br close again exceeds 10 s.  Trimming to 5 before
// every close caps the scan cost at sub-second latency regardless of session length.
const brHistoryCloseTrimKeep = 5

// runBrHistoryRotationPreflight checks whether .beads/.br_history/ contains
// more than keepLatest entries and, if so, archives the oldest entries to a
// sibling .beads/.br_history-archive/ directory.
//
// Outcomes:
//   - Dir absent: logs "rotation_skipped reason=dir_absent", returns nil.
//   - Entry count ≤ keepLatest: logs "rotation_skipped", returns nil.
//   - Entry count > keepLatest: archives oldest, logs structured completion.
//
// The function is always non-fatal: any error encountered during archiving
// is logged as a warning and the function returns nil.  Daemon startup must
// not be blocked by a history-rotation failure.
//
// ctx is honoured for context cancellation (early SIGTERM during startup).
//
// projectDir must be the harmonik project root (the directory that contains
// .beads/).
func runBrHistoryRotationPreflight(ctx context.Context, projectDir string, keepLatest int) error {
	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	archiveDir := filepath.Join(projectDir, ".beads", ".br_history-archive")

	// Stat the history directory.
	//nolint:gosec // G304: historyDir constructed from operator-supplied projectDir; not user input.
	_, statErr := os.Stat(historyDir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			slog.InfoContext(ctx, "br_history_rotation_skipped", "reason", "dir_absent")
			return nil
		}
		slog.WarnContext(ctx, "br_history_rotation_stat_error",
			"history_dir", historyDir,
			"error", statErr.Error(),
		)
		return nil
	}

	// Read directory entries.
	entries, readErr := os.ReadDir(historyDir)
	if readErr != nil {
		slog.WarnContext(ctx, "br_history_rotation_read_error",
			"history_dir", historyDir,
			"error", readErr.Error(),
		)
		return nil
	}

	total := len(entries)
	if total <= keepLatest {
		slog.InfoContext(ctx, "br_history_rotation_skipped",
			"reason", "within_limit",
			"count", total,
			"keep", keepLatest,
		)
		return nil
	}

	slog.InfoContext(ctx, "br_history_rotation_started",
		"total", total,
		"keep", keepLatest,
	)

	// Stat each entry to get mtime for sorting.
	type entryWithMtime struct {
		name  string
		mtime time.Time
	}
	statted := make([]entryWithMtime, 0, total)
	for _, e := range entries {
		//nolint:gosec // G304: path constructed from operator-supplied projectDir; not user input.
		info, err := os.Stat(filepath.Join(historyDir, e.Name()))
		if err != nil {
			// Skip unstat-able entries; they won't be archived (safer).
			slog.WarnContext(ctx, "br_history_rotation_entry_stat_error",
				"entry", e.Name(),
				"error", err.Error(),
			)
			continue
		}
		statted = append(statted, entryWithMtime{name: e.Name(), mtime: info.ModTime()})
	}

	// Sort descending by mtime (newest first).
	sort.Slice(statted, func(i, j int) bool {
		return statted[i].mtime.After(statted[j].mtime)
	})

	// Determine which entries to archive: everything beyond the keepLatest newest.
	var toArchive []entryWithMtime
	if len(statted) > keepLatest {
		toArchive = statted[keepLatest:]
	}
	if len(toArchive) == 0 {
		// All surviving entries were unstat-able; nothing to archive.
		slog.InfoContext(ctx, "br_history_rotation_skipped",
			"reason", "nothing_to_archive_after_stat",
		)
		return nil
	}

	// Ensure the archive directory exists.
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(archiveDir, 0o755); mkErr != nil {
		slog.WarnContext(ctx, "br_history_rotation_mkdir_error",
			"archive_dir", archiveDir,
			"error", mkErr.Error(),
		)
		return nil
	}

	// Archive each old entry.
	start := time.Now()
	ts := start.UTC().Format("20060102T150405Z")
	archived := 0
	for _, entry := range toArchive {
		src := filepath.Join(historyDir, entry.name)
		dst := filepath.Join(archiveDir, fmt.Sprintf("%s.archived-%s", entry.name, ts))
		if renErr := os.Rename(src, dst); renErr != nil {
			slog.WarnContext(ctx, "br_history_rotation_rename_error",
				"src", src,
				"dst", dst,
				"error", renErr.Error(),
			)
			continue
		}
		archived++
	}

	duration := time.Since(start)
	remaining := total - archived

	slog.InfoContext(ctx, "br_history_rotation_completed",
		"archived", archived,
		"remaining", remaining,
		"duration_ms", duration.Milliseconds(),
	)
	return nil
}
