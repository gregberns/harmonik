package daemon

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

const (
	branchReapWatcherDefaultInterval = 6 * time.Hour

	branchReapWatcherDefaultTargetBranch = "main"
)

type branchReaperFunc func(ctx context.Context, opts lifecycle.BranchReapOptions) (lifecycle.BranchReapResult, error)

// BranchReapWatcherConfig holds the construction-time parameters for
// BranchReapWatcher.
type BranchReapWatcherConfig struct {
	// RepoDir is the git repository root to reap branches in. Required for the
	// sweep to do anything; empty makes scan a no-op (unit-test mode).
	RepoDir string

	// TargetBranch is the merge-check target passed to ReapBranches. Empty →
	// branchReapWatcherDefaultTargetBranch.
	TargetBranch string

	// OrphanMaxAge is passed through to ReapBranches. Zero → the
	// lifecycle package default (30 days).
	OrphanMaxAge time.Duration

	// ScanInterval is how often the background goroutine runs a reap pass.
	// Zero → branchReapWatcherDefaultInterval.
	ScanInterval time.Duration

	// Reap performs one reap pass. Nil → lifecycle.ReapBranches.
	Reap branchReaperFunc
}

// BranchReapWatcher periodically runs lifecycle.ReapBranches against RepoDir.
type BranchReapWatcher struct {
	cfg BranchReapWatcherConfig
}

// NewBranchReapWatcher constructs a BranchReapWatcher from cfg, applying
// defaults for zero-valued fields.
func NewBranchReapWatcher(cfg BranchReapWatcherConfig) *BranchReapWatcher {
	if cfg.TargetBranch == "" {
		cfg.TargetBranch = branchReapWatcherDefaultTargetBranch
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = branchReapWatcherDefaultInterval
	}
	if cfg.Reap == nil {
		cfg.Reap = lifecycle.ReapBranches
	}
	return &BranchReapWatcher{cfg: cfg}
}

// StartWatcher launches the background scan goroutine. Returns immediately;
// the goroutine runs until ctx is cancelled.
func (w *BranchReapWatcher) StartWatcher(ctx context.Context) {
	go w.loop(ctx)
}

func (w *BranchReapWatcher) loop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.scan(ctx)
		}
	}
}

func (w *BranchReapWatcher) scan(ctx context.Context) {
	if w.cfg.RepoDir == "" {
		return
	}
	result, err := w.cfg.Reap(ctx, lifecycle.BranchReapOptions{
		RepoDir:      w.cfg.RepoDir,
		TargetBranch: w.cfg.TargetBranch,
		OrphanMaxAge: w.cfg.OrphanMaxAge,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: branch-reap: %v\n", err)
		return
	}
	if len(result.Reaped) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr,
		"daemon: branch-reap: scanned %d branch(es), reaped %d, skipped %d: %v\n",
		result.Scanned, len(result.Reaped), result.Skipped, result.Reaped)
}
