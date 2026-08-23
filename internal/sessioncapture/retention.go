package sessioncapture

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

type sessDir struct {
	name  string
	mtime time.Time
}

func pruneSessions(ctx context.Context, root string, keepN int, maxAge time.Duration, clk substrate.ClockPort) {
	dirs, ok := collectSessionDirs(ctx, root)
	if !ok {
		return
	}
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

func selectPrunable(dirs []sessDir, keepN int, maxAge time.Duration, now time.Time) map[string]struct{} {
	toPrune := map[string]struct{}{}
	if keepN > 0 && len(dirs) > keepN {
		for _, d := range dirs[keepN:] {
			toPrune[d.name] = struct{}{}
		}
	}
	if maxAge > 0 {
		for _, d := range dirs {
			if now.Sub(d.mtime) > maxAge {
				toPrune[d.name] = struct{}{}
			}
		}
	}
	return toPrune
}
