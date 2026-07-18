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
	"strings"
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

// brHistoryArchiveKeep and brHistoryArchiveMaxAge bound the .br_history-archive/
// directory (hk-8vnwg). Rotation ARCHIVES old .br_history snapshots into that
// sibling dir instead of deleting them — but nothing ever pruned the archive, so
// it grew without bound (the confirmed 256 GB disk-fill root cause: 25 GiB across
// 15,072 snapshots at ~5.4 MB each, because EVERY bead close runs rotation and
// feeds the archive). These caps prune it on every rotation invocation.
//
// Policy (union — a snapshot is pruned if it is beyond keep-N OR older than
// max-age, mirroring sessioncapture/retention.go): retain the 300 most-recent
// archived snapshots (~1.6 GB worst case at 5.4 MB each) AND drop anything older
// than 7 days. Unlike rotation (which archives, preserving a rollback tier), the
// prune HARD-DELETES: the archive is already the "these are old" tier, and a
// retention cap on it is the intended second version the rotation comment
// deferred ("archived ... so that snapshots are not irreversibly destroyed on the
// first version"). Both values are conservative disk-safety knobs, surfaced here
// for the review gate to tune.
const (
	brHistoryArchiveKeep   = 300
	brHistoryArchiveMaxAge = 7 * 24 * time.Hour
)

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

	// hk-8vnwg: cap the archive on EVERY invocation, regardless of whether
	// .br_history needs rotation this call. The archive grows via every close's
	// rotation, so its prune must not be gated behind the "history over limit"
	// early returns below — a defer guarantees it runs on all return paths.
	defer pruneBrHistoryArchive(ctx, archiveDir, brHistoryArchiveKeep, brHistoryArchiveMaxAge, time.Now())

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

// pruneBrHistoryArchive hard-deletes archived br-history snapshots beyond the
// keepN most-recent (by mtime) and, when maxAge > 0, any archived snapshot older
// than maxAge (union semantics, mirroring sessioncapture/retention.go). It is
// always non-fatal — a prune failure must never block daemon startup or a bead
// close (same discipline as runBrHistoryRotationPreflight).
//
// The archive holds two files per snapshot: the ~5.4 MB
// "<name>.jsonl.archived-<ts>" payload and a tiny
// "<name>.jsonl.meta.json.archived-<ts>" sidecar. Retention is computed over the
// payload files (the disk-dominant units); when a payload is pruned its sidecar
// is removed with it. Any orphan sidecar older than maxAge is also swept so meta
// files never accumulate unbounded. Files matching neither shape are left alone
// (safer than a blind delete). Bead ref: hk-8vnwg.
func pruneBrHistoryArchive(ctx context.Context, archiveDir string, keepN int, maxAge time.Duration, now time.Time) {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(ctx, "br_history_archive_prune_read_error",
				"archive_dir", archiveDir, "error", err.Error())
		}
		return
	}

	const payloadInfix = ".jsonl.archived-"
	const sidecarInfix = ".jsonl.meta.json.archived-"

	type archived struct {
		name  string
		mtime time.Time
	}
	payloads := make([]archived, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		// Sidecars are pruned with their payload (or by the orphan age-sweep below).
		if strings.Contains(n, sidecarInfix) {
			continue
		}
		if !strings.Contains(n, payloadInfix) {
			continue // unrecognised file; leave it (safer than a blind delete).
		}
		//nolint:gosec // G304: archiveDir constructed from operator-supplied projectDir; not user input.
		info, statErr := os.Stat(filepath.Join(archiveDir, n))
		if statErr != nil {
			continue // unstat-able: leave it.
		}
		payloads = append(payloads, archived{name: n, mtime: info.ModTime()})
	}

	// Newest first.
	sort.Slice(payloads, func(i, j int) bool { return payloads[i].mtime.After(payloads[j].mtime) })

	// Select payloads to prune: beyond keepN, OR older than maxAge (union).
	toPrune := map[string]struct{}{}
	if keepN > 0 && len(payloads) > keepN {
		for _, p := range payloads[keepN:] {
			toPrune[p.name] = struct{}{}
		}
	}
	if maxAge > 0 {
		for _, p := range payloads {
			if now.Sub(p.mtime) > maxAge {
				toPrune[p.name] = struct{}{}
			}
		}
	}

	pruned := 0
	for name := range toPrune {
		if rmErr := os.Remove(filepath.Join(archiveDir, name)); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.WarnContext(ctx, "br_history_archive_prune_remove_error",
				"file", name, "error", rmErr.Error())
			continue
		}
		pruned++
		// Remove the paired sidecar: payload "<x>.jsonl.archived-<ts>" ->
		// sidecar "<x>.jsonl.meta.json.archived-<ts>".
		sidecar := strings.Replace(name, payloadInfix, sidecarInfix, 1)
		if sidecar != name {
			_ = os.Remove(filepath.Join(archiveDir, sidecar)) // best-effort; may not exist
		}
	}

	// Orphan-sidecar age sweep: a sidecar whose payload was pruned in a prior run
	// (or that never had one) is removed once older than maxAge, so metas never
	// leak. Sidecars already removed above simply fail os.Stat and are skipped.
	orphanMetas := 0
	if maxAge > 0 {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if !strings.Contains(n, sidecarInfix) {
				continue
			}
			//nolint:gosec // G304: archiveDir constructed from operator-supplied projectDir; not user input.
			info, statErr := os.Stat(filepath.Join(archiveDir, n))
			if statErr != nil {
				continue
			}
			if now.Sub(info.ModTime()) > maxAge {
				if rmErr := os.Remove(filepath.Join(archiveDir, n)); rmErr == nil {
					orphanMetas++
				}
			}
		}
	}

	if pruned > 0 || orphanMetas > 0 {
		slog.InfoContext(ctx, "br_history_archive_pruned",
			"pruned_snapshots", pruned,
			"pruned_orphan_metas", orphanMetas,
			"kept", len(payloads)-pruned,
			"keep_n", keepN,
			"max_age", maxAge.String(),
		)
	}
}
