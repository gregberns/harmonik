package sessioncapture

// Retention for the session corpus: a keep-N / age-prune rule over the
// ${workspace_path}/.harmonik/sessions/ directory, mirroring the
// brhistoryrotate (keep the K most-recent by mtime) and orphansweep
// (age-based reap) precedents. This is the load-bearing guard against the
// unrotated large-events.jsonl defect: the corpus MUST NOT grow unbounded.
//
// Unlike brhistoryrotate (which archives), pruned session dirs are hard-deleted
// — capture corpora are ephemeral record→replay material, not an audit trail.
// Every step is best-effort and non-fatal: a retention failure never blocks a
// capture (AIS-INV-002 spirit — capture is never load-bearing).

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// sessDir is one candidate session directory with its mtime (recency key).
type sessDir struct {
	name  string
	mtime time.Time
}

// pruneSessions removes session directories under root beyond the keepN
// most-recent (by mtime) and, when maxAge > 0, any directory older than maxAge
// measured against clk.Now. It is always non-fatal.
//
// Only immediate subdirectories of root are considered session dirs; the
// CAPTURE-LOG ledger file (and any other non-dir sibling) is never touched.
func pruneSessions(ctx context.Context, root string, keepN int, maxAge time.Duration, clk substrate.ClockPort) {
	dirs, ok := collectSessionDirs(ctx, root)
	if !ok {
		return
	}
	// Newest first.
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].mtime.After(dirs[j].mtime) })

	toPrune := selectPrunable(dirs, keepN, maxAge, clk.Now())

	pruned := 0
	for name := range toPrune {
		if rmErr := os.RemoveAll(filepath.Join(root, name)); rmErr != nil {
			slog.WarnContext(ctx, "sessioncapture_retention_remove_error", "dir", name, "error", rmErr.Error())
			continue
		}
		pruned++
	}
	if pruned > 0 {
		slog.InfoContext(ctx, "sessioncapture_retention_pruned",
			"pruned", pruned,
			"kept", len(dirs)-pruned,
			"keep_n", keepN,
			"max_age", maxAge.String(),
		)
	}
}

// collectSessionDirs lists the immediate subdirectories of root with their
// mtimes. ok is false only when root cannot be read (nothing to prune).
func collectSessionDirs(ctx context.Context, root string) (dirs []sessDir, ok bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.WarnContext(ctx, "sessioncapture_retention_read_error", "root", root, "error", err.Error())
		}
		return nil, false
	}
	dirs = make([]sessDir, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue // skip CAPTURE-LOG.md and any stray files.
		}
		info, statErr := os.Stat(filepath.Join(root, e.Name()))
		if statErr != nil {
			continue // unstat-able: leave it (safer than a blind delete).
		}
		dirs = append(dirs, sessDir{name: e.Name(), mtime: info.ModTime()})
	}
	return dirs, true
}

// selectPrunable applies the keep-N and age arms to a recency-sorted (newest
// first) dir list, returning the set of names to remove.
func selectPrunable(dirs []sessDir, keepN int, maxAge time.Duration, now time.Time) map[string]struct{} {
	toPrune := map[string]struct{}{}
	// keep-N arm: everything beyond the N newest.
	if keepN > 0 && len(dirs) > keepN {
		for _, d := range dirs[keepN:] {
			toPrune[d.name] = struct{}{}
		}
	}
	// age arm: anything older than maxAge (regardless of position).
	if maxAge > 0 {
		for _, d := range dirs {
			if now.Sub(d.mtime) > maxAge {
				toPrune[d.name] = struct{}{}
			}
		}
	}
	return toPrune
}
