package keeper

// dashboardnag_test.go — unit tests for the hk-xg6rw dashboard pre-nag.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
)

func writeNagFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newNagWatcher(projectDir string, spy *[]string) *Watcher {
	return NewWatcher(WatcherConfig{
		ProjectDir: projectDir,
		TmuxTarget: "test:0.0",
		DashboardNagInjectFn: func(_ context.Context, target, text string) error {
			*spy = append(*spy, target+"|"+text)
			return nil
		},
	}, nil)
}

// TestMaybeNagDashboardStale_NotConfigured verifies no nag fires when the
// operator never added a dashboard: config block.
func TestMaybeNagDashboardStale_NotConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var calls []string
	w := newNagWatcher(dir, &calls)

	w.maybeNagDashboardStale(context.Background(), time.Now())

	if len(calls) != 0 {
		t.Errorf("got %d injections, want 0 (dashboard: block absent)", len(calls))
	}
}

// TestMaybeNagDashboardStale_FreshNoNag verifies a comfortably-fresh
// dashboard.json (well under the approach fraction) does not nag.
func TestMaybeNagDashboardStale_FreshNoNag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNagFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 30m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-2 * time.Minute)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	var calls []string
	w := newNagWatcher(dir, &calls)
	w.maybeNagDashboardStale(context.Background(), time.Now())

	if len(calls) != 0 {
		t.Errorf("got %d injections, want 0 (dashboard.json is fresh)", len(calls))
	}
}

// TestMaybeNagDashboardStale_ApproachingNags verifies the pre-nag fires
// BEFORE the forcing gate itself would trip — at dashboardNagApproachFrac of
// max_staleness — so the gate rarely actually fires (DESIGN §4).
func TestMaybeNagDashboardStale_ApproachingNags(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNagFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-9 * time.Minute) // 90% of the 10m window
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	var calls []string
	w := newNagWatcher(dir, &calls)
	w.maybeNagDashboardStale(context.Background(), time.Now())

	if len(calls) != 1 {
		t.Fatalf("got %d injections, want 1 (approaching staleness)", len(calls))
	}
	if w.lastDashboardNagAt.IsZero() {
		t.Error("lastDashboardNagAt not stamped after a successful nag")
	}
}

// TestMaybeNagDashboardStale_CooldownSuppressesRepeat verifies the cooldown
// prevents a second nag within dashboardNagCooldown of the first.
func TestMaybeNagDashboardStale_CooldownSuppressesRepeat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNagFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-24 * time.Hour)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	var calls []string
	w := newNagWatcher(dir, &calls)
	now := time.Now()
	w.maybeNagDashboardStale(context.Background(), now)
	w.maybeNagDashboardStale(context.Background(), now.Add(1*time.Minute))

	if len(calls) != 1 {
		t.Errorf("got %d injections, want 1 (second call within cooldown)", len(calls))
	}
}

// TestMaybeNagDashboardStale_UnlockSuppressesNag verifies an active operator
// --unlock override suppresses the nag (nudging would be noise once the
// operator has already overridden the gate).
func TestMaybeNagDashboardStale_UnlockSuppressesNag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNagFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-24 * time.Hour)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := dashboard.WriteUnlock(dir, now.Add(1*time.Hour), "operator"); err != nil {
		t.Fatal(err)
	}

	var calls []string
	w := newNagWatcher(dir, &calls)
	w.maybeNagDashboardStale(context.Background(), now)

	if len(calls) != 0 {
		t.Errorf("got %d injections, want 0 (unlock override active)", len(calls))
	}
}

// TestMaybeNagDashboardStale_NoTmuxTargetIsNoop verifies the nag is skipped
// entirely (no panic, no injection) when TmuxTarget is empty — the normal
// unit-test / no-pane case.
func TestMaybeNagDashboardStale_NoTmuxTargetIsNoop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeNagFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	var calls []string
	w := NewWatcher(WatcherConfig{
		ProjectDir: dir,
		DashboardNagInjectFn: func(_ context.Context, target, text string) error {
			calls = append(calls, target+"|"+text)
			return nil
		},
	}, nil)
	w.maybeNagDashboardStale(context.Background(), time.Now())

	if len(calls) != 0 {
		t.Errorf("got %d injections, want 0 (no TmuxTarget)", len(calls))
	}
}
