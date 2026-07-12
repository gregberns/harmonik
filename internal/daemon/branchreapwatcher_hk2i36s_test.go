package daemon

// branchreapwatcher_hk2i36s_test.go — unit coverage for BranchReapWatcher
// (hk-2i36s): the periodic tick has an actual caller, honours the
// RepoDir-empty no-op guard, and applies config defaults.

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

func TestBranchReapWatcher_ScanCallsReapWhenRepoDirSet(t *testing.T) {
	var gotOpts lifecycle.BranchReapOptions
	calls := 0
	w := NewBranchReapWatcher(BranchReapWatcherConfig{
		RepoDir:      "/fake/repo",
		TargetBranch: "trunk",
		OrphanMaxAge: 5 * time.Hour,
		Reap: func(_ context.Context, opts lifecycle.BranchReapOptions) (lifecycle.BranchReapResult, error) {
			calls++
			gotOpts = opts
			return lifecycle.BranchReapResult{Scanned: 2, Reaped: []string{"run/abc"}}, nil
		},
	})

	w.scan(context.Background())

	if calls != 1 {
		t.Fatalf("expected Reap to be called once, got %d", calls)
	}
	if gotOpts.RepoDir != "/fake/repo" {
		t.Fatalf("expected RepoDir to be passed through, got %q", gotOpts.RepoDir)
	}
	if gotOpts.TargetBranch != "trunk" {
		t.Fatalf("expected configured TargetBranch to be passed through, got %q", gotOpts.TargetBranch)
	}
	if gotOpts.OrphanMaxAge != 5*time.Hour {
		t.Fatalf("expected configured OrphanMaxAge to be passed through, got %v", gotOpts.OrphanMaxAge)
	}
}

func TestBranchReapWatcher_ScanNoopsWhenRepoDirEmpty(t *testing.T) {
	calls := 0
	w := NewBranchReapWatcher(BranchReapWatcherConfig{
		Reap: func(_ context.Context, _ lifecycle.BranchReapOptions) (lifecycle.BranchReapResult, error) {
			calls++
			return lifecycle.BranchReapResult{}, nil
		},
	})

	w.scan(context.Background())

	if calls != 0 {
		t.Fatalf("expected Reap not to be called with an empty RepoDir, got %d calls", calls)
	}
}

func TestBranchReapWatcher_DefaultsApplied(t *testing.T) {
	w := NewBranchReapWatcher(BranchReapWatcherConfig{RepoDir: "/fake/repo"})

	if w.cfg.TargetBranch != branchReapWatcherDefaultTargetBranch {
		t.Fatalf("expected default TargetBranch %q, got %q", branchReapWatcherDefaultTargetBranch, w.cfg.TargetBranch)
	}
	if w.cfg.ScanInterval != branchReapWatcherDefaultInterval {
		t.Fatalf("expected default ScanInterval %v, got %v", branchReapWatcherDefaultInterval, w.cfg.ScanInterval)
	}
	if w.cfg.Reap == nil {
		t.Fatal("expected default Reap func to be set to lifecycle.ReapBranches")
	}
}

func TestBranchReapWatcher_StartWatcherRunsOnTick(t *testing.T) {
	done := make(chan struct{}, 1)
	w := NewBranchReapWatcher(BranchReapWatcherConfig{
		RepoDir:      "/fake/repo",
		ScanInterval: 10 * time.Millisecond,
		Reap: func(_ context.Context, _ lifecycle.BranchReapOptions) (lifecycle.BranchReapResult, error) {
			select {
			case done <- struct{}{}:
			default:
			}
			return lifecycle.BranchReapResult{}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w.StartWatcher(ctx)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected StartWatcher's ticker to invoke Reap within 500ms")
	}
}
