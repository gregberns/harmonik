package daemon

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

const brHistoryRotationDefaultKeep = 20

const brHistoryCloseTrimKeep = 5

const (
	brHistoryArchiveKeep   = 300
	brHistoryArchiveMaxAge = 7 * 24 * time.Hour
)

func runBrHistoryRotationPreflight(ctx context.Context, projectDir string, keepLatest int) error {
	historyDir := filepath.Join(projectDir, ".beads", ".br_history")
	archiveDir := filepath.Join(projectDir, ".beads", ".br_history-archive")

	defer pruneBrHistoryArchive(ctx, archiveDir, brHistoryArchiveKeep, brHistoryArchiveMaxAge, time.Now())

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

	type entryWithMtime struct {
		name  string
		mtime time.Time
	}
	statted := make([]entryWithMtime, 0, total)
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(historyDir, e.Name()))
		if err != nil {
			slog.WarnContext(ctx, "br_history_rotation_entry_stat_error",
				"entry", e.Name(),
				"error", err.Error(),
			)
			continue
		}
		statted = append(statted, entryWithMtime{name: e.Name(), mtime: info.ModTime()})
	}

	sort.Slice(statted, func(i, j int) bool {
		return statted[i].mtime.After(statted[j].mtime)
	})

	var toArchive []entryWithMtime
	if len(statted) > keepLatest {
		toArchive = statted[keepLatest:]
	}
	if len(toArchive) == 0 {
		slog.InfoContext(ctx, "br_history_rotation_skipped",
			"reason", "nothing_to_archive_after_stat",
		)
		return nil
	}

	// Ensure the archive directory exists.
	//nolint:gosec // G301: 0755 matches the .beads/ dir conventions
	if mkErr := os.MkdirAll(archiveDir, 0o755); mkErr != nil { //dirmode:allow not a .harmonik state dir: .beads/.br_history-archive lives under .beads/, whose mode is br's convention, not harmonik's
		slog.WarnContext(ctx, "br_history_rotation_mkdir_error",
			"archive_dir", archiveDir,
			"error", mkErr.Error(),
		)
		return nil
	}

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
//
//nolint:gocognit,cyclop // pruneBrHistoryArchive is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
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
		if strings.Contains(n, sidecarInfix) {
			continue
		}
		if !strings.Contains(n, payloadInfix) {
			continue // unrecognised file; leave it (safer than a blind delete).
		}
		info, statErr := os.Stat(filepath.Join(archiveDir, n))
		if statErr != nil {
			continue // unstat-able: leave it.
		}
		payloads = append(payloads, archived{name: n, mtime: info.ModTime()})
	}

	sort.Slice(payloads, func(i, j int) bool { return payloads[i].mtime.After(payloads[j].mtime) })

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
		sidecar := strings.Replace(name, payloadInfix, sidecarInfix, 1)
		if sidecar != name {
			_ = os.Remove(filepath.Join(archiveDir, sidecar)) //nolint:errcheck // best-effort cleanup; sidecar may not exist
		}
	}

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
