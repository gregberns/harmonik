package daemon_test

// stalewatch_neverSpawnedTimeout_hk8gixi_test.go — unit tests for the
// per-bead never_spawned_timeout label override (hk-8gixi).
//
// The never-spawned reaper fires when launch_initiated was seen but agent_ready
// was not seen within the effective timeout.  Operators can override the global
// NeverSpawnedReaperTimeout (default 30 min) on a per-bead basis by adding a
// "never_spawned_timeout=<seconds>" label to the bead — useful for long-running
// DOT implement nodes that legitimately need more than 30 min before first
// agent_ready.
//
// Test coverage:
//   - TestBeadNeverSpawnedTimeout_Default          — no label → default
//   - TestBeadNeverSpawnedTimeout_EqualSignForm    — "never_spawned_timeout=3600" → 1h
//   - TestBeadNeverSpawnedTimeout_ColonForm        — "never_spawned_timeout:3600" → 1h
//   - TestBeadNeverSpawnedTimeout_ZeroIgnored      — "=0" → falls back to default
//   - TestBeadNeverSpawnedTimeout_NegativeIgnored  — "=-1" → falls back to default
//   - TestBeadNeverSpawnedTimeout_InvalidIgnored   — "=abc" → falls back to default
//   - TestBeadNeverSpawnedTimeout_LastWins         — duplicate labels → first wins
//   - TestBeadNeverSpawnedTimeout_PerRunOverride   — override applied in reaper

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ─────────────────────────────────────────────────────────────────────────────
// beadNeverSpawnedTimeout parser
// ─────────────────────────────────────────────────────────────────────────────

func TestBeadNeverSpawnedTimeout_Default(t *testing.T) {
	t.Parallel()
	got := daemon.ExportedBeadNeverSpawnedTimeout(nil, 30*time.Minute)
	if got != 30*time.Minute {
		t.Errorf("got %v, want 30m", got)
	}
}

func TestBeadNeverSpawnedTimeout_EqualSignForm(t *testing.T) {
	t.Parallel()
	labels := []string{"priority=high", "never_spawned_timeout=3600", "team=foo"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != time.Hour {
		t.Errorf("got %v, want 1h", got)
	}
}

func TestBeadNeverSpawnedTimeout_ColonForm(t *testing.T) {
	t.Parallel()
	labels := []string{"never_spawned_timeout:7200"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != 2*time.Hour {
		t.Errorf("got %v, want 2h", got)
	}
}

func TestBeadNeverSpawnedTimeout_ZeroIgnored(t *testing.T) {
	t.Parallel()
	labels := []string{"never_spawned_timeout=0"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != 30*time.Minute {
		t.Errorf("zero value should fall back to default; got %v, want 30m", got)
	}
}

func TestBeadNeverSpawnedTimeout_NegativeIgnored(t *testing.T) {
	t.Parallel()
	labels := []string{"never_spawned_timeout=-1"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != 30*time.Minute {
		t.Errorf("negative value should fall back to default; got %v, want 30m", got)
	}
}

func TestBeadNeverSpawnedTimeout_InvalidIgnored(t *testing.T) {
	t.Parallel()
	labels := []string{"never_spawned_timeout=abc"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != 30*time.Minute {
		t.Errorf("non-numeric value should fall back to default; got %v, want 30m", got)
	}
}

func TestBeadNeverSpawnedTimeout_FirstLabelWins(t *testing.T) {
	t.Parallel()
	// When multiple labels match, the first one encountered wins.
	labels := []string{"never_spawned_timeout=3600", "never_spawned_timeout=7200"}
	got := daemon.ExportedBeadNeverSpawnedTimeout(labels, 30*time.Minute)
	if got != time.Hour {
		t.Errorf("first label should win; got %v, want 1h", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-run override integration: label on RunHandle.Labels raises the reaper
// ceiling for that specific run.
// ─────────────────────────────────────────────────────────────────────────────

// bntNewRunID returns a UUIDv7-based RunID for these tests.
func bntNewRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("bntNewRunID: NewV7: %v", err)
	}
	return core.RunID(u)
}

// bntBuildWatcher builds a StaleWatcher with a mutable clock and the given
// global reaper timeout.
func bntBuildWatcher(t *testing.T, reg *daemon.RunRegistry, globalTimeout time.Duration, clk *mutableClock) *daemon.StaleWatcher {
	t.Helper()
	sfb := staleFixtureNewBus(t)
	unsealed := eventbus.NewBusImpl()
	w := daemon.NewStaleWatcher(daemon.StaleWatcherConfig{
		SubscribeBus:              unsealed,
		Emitter:                   sfb.bus,
		Registry:                  reg,
		StaleAfter:                24 * time.Hour,
		ScanInterval:              time.Hour,
		NeverSpawnedReaperTimeout: globalTimeout,
		Now:                       clk.Now,
	})
	if err := w.Subscribe(); err != nil {
		t.Fatalf("bntBuildWatcher: Subscribe: %v", err)
	}
	if err := unsealed.Seal(); err != nil {
		t.Fatalf("bntBuildWatcher: Seal: %v", err)
	}
	return w
}

// TestBeadNeverSpawnedTimeout_PerRunOverride verifies that a bead with
// "never_spawned_timeout=3600" is NOT reaped at the global 30-min default but
// IS reaped after its own 60-min override elapses.
func TestBeadNeverSpawnedTimeout_PerRunOverride(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-8gixi-override",
		StartedAt: epoch,
		Labels:    []string{"never_spawned_timeout=3600"}, // 1-hour override
		Cancel:    func() { cancelCalled.Store(true) },
	}
	runID := bntNewRunID(t)
	reg.Register(runID, handle)

	// Global timeout is the default 30 min.
	w := bntBuildWatcher(t, reg, 30*time.Minute, clk)

	// Emit launch_initiated at t=0.
	clk.Set(epoch)
	runIDCopy := runID
	evt := core.Event{
		EventID: core.EventID(uuid.Must(uuid.NewV7())),
		Type:    string(core.EventTypeLaunchInitiated),
		RunID:   &runIDCopy,
	}
	daemon.ExportedStalewatchObserve(w, context.Background(), evt)

	// Advance to 31 min (past global default, still under per-bead 60 min).
	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("reaper should NOT fire at 31min when per-bead override is 60min")
	}

	// Advance to 61 min (past per-bead 60-min override).
	clk.Set(epoch.Add(61 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if !cancelCalled.Load() {
		t.Error("reaper should fire at 61min when per-bead override is 60min")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("handle.aborted should be true after reaper fires")
	}
}
