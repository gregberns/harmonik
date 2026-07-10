package daemon

// dashboardgate_test.go — unit tests for the hk-xg6rw forcing-gate evaluator.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
)

func writeGateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEvaluateDashboardGate_NotConfigured verifies the gate is off (Blocked
// false, nil error) when the operator never added a dashboard: block —
// adopting the mechanism must be opt-in.
func TestEvaluateDashboardGate_NotConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	result, err := evaluateDashboardGate(dir, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Error("Blocked: got true, want false (dashboard: block absent)")
	}
}

// TestEvaluateDashboardGate_MissingMaxStaleness verifies the fail-loud +
// fail-blocking contract: a dashboard: block present without max_staleness
// must trip the gate (Blocked true) AND return a non-nil error, never
// silently disable.
func TestEvaluateDashboardGate_MissingMaxStaleness(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  unlock: false\n")

	result, err := evaluateDashboardGate(dir, time.Now())
	if err == nil {
		t.Fatal("expected error for missing max_staleness, got nil")
	}
	if !result.Blocked {
		t.Error("Blocked: got false, want true (fail-blocking on config error)")
	}
}

// TestEvaluateDashboardGate_FreshNotBlocked verifies a recently-updated
// dashboard.json does not trip the gate.
func TestEvaluateDashboardGate_FreshNotBlocked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 30m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-5 * time.Minute)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	result, err := evaluateDashboardGate(dir, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Error("Blocked: got true, want false (dashboard.json is fresh)")
	}
}

// TestEvaluateDashboardGate_StaleBlocksAndScopesToLanes verifies a
// past-window dashboard.json trips the gate and BlockedQueues is populated
// from lanes.json's queue field — scoping to captain-curated lanes only.
func TestEvaluateDashboardGate_StaleBlocksAndScopesToLanes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-1 * time.Hour)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	writeGateFile(t, filepath.Join(dir, ".harmonik", "context", "lanes.json"), `{
		"schema_version": 1,
		"lanes": [
			{"lane": "pi-sandbox", "queue": "main", "status": "active"},
			{"lane": "other", "queue": "investigate", "status": "active"}
		]
	}`)

	result, err := evaluateDashboardGate(dir, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Fatal("Blocked: got false, want true (dashboard.json is stale)")
	}
	if !result.BlockedQueues["main"] || !result.BlockedQueues["investigate"] {
		t.Errorf("BlockedQueues: got %v, want main+investigate gated", result.BlockedQueues)
	}
}

// TestEvaluateDashboardGate_ConfigUnlockOverride verifies the config
// kill-switch (dashboard.unlock: true) disables the gate regardless of
// staleness.
func TestEvaluateDashboardGate_ConfigUnlockOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n  unlock: true\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-24 * time.Hour)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	result, err := evaluateDashboardGate(dir, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Error("Blocked: got true, want false (config kill-switch active)")
	}
}

// TestEvaluateDashboardGate_CLIUnlockOverride verifies the
// harmonik dashboard --unlock override (dashboard-unlock.json) disables the
// gate until it expires.
func TestEvaluateDashboardGate_CLIUnlockOverride(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	ds := dashboard.Default()
	ds.Updated = time.Now().Add(-24 * time.Hour)
	if err := dashboard.Write(dir, ds); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if err := dashboard.WriteUnlock(dir, now.Add(1*time.Hour), "operator"); err != nil {
		t.Fatal(err)
	}

	result, err := evaluateDashboardGate(dir, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Blocked {
		t.Error("Blocked: got true, want false (CLI unlock override active)")
	}

	// After expiry, the gate resumes.
	result2, err2 := evaluateDashboardGate(dir, now.Add(2*time.Hour))
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if !result2.Blocked {
		t.Error("Blocked: got false after unlock expiry, want true")
	}
}

// TestEvaluateDashboardGate_NeverWrittenIsMaximallyStale verifies that a
// missing dashboard.json (captain never adopted the Tier-B mechanism) is
// treated as stale once the operator has opted into the dashboard: config
// block — the gate must not silently pass just because nothing was ever
// written.
func TestEvaluateDashboardGate_NeverWrittenIsMaximallyStale(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGateFile(t, filepath.Join(dir, ".harmonik", "config.yaml"), "dashboard:\n  max_staleness: 10m\n")

	result, err := evaluateDashboardGate(dir, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Blocked {
		t.Error("Blocked: got false, want true (dashboard.json never written)")
	}
}
