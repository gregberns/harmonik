package digest

import (
	"errors"
	"testing"
	"time"
)

// TestParseDashboardGateConfig_Absent verifies that a config.yaml with no
// dashboard: block resolves to a disabled gate (no error) — the operator has
// not opted into the forcing mechanism (hk-xg6rw).
func TestParseDashboardGateConfig_Absent(t *testing.T) {
	t.Parallel()

	cfg, err := parseDashboardGateConfig([]byte(`
daemon:
  max_concurrent: 3
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Configured() {
		t.Error("Configured: got true, want false (no dashboard: block)")
	}
	if cfg.MaxStaleness != 0 {
		t.Errorf("MaxStaleness: got %v, want 0", cfg.MaxStaleness)
	}
}

// TestParseDashboardGateConfig_Configured verifies max_staleness and unlock
// parse correctly when the dashboard: block is present.
func TestParseDashboardGateConfig_Configured(t *testing.T) {
	t.Parallel()

	cfg, err := parseDashboardGateConfig([]byte(`
dashboard:
  max_staleness: 45m
  unlock: true
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Configured() {
		t.Error("Configured: got false, want true")
	}
	if cfg.MaxStaleness != 45*time.Minute {
		t.Errorf("MaxStaleness: got %v, want 45m", cfg.MaxStaleness)
	}
	if !cfg.Unlock {
		t.Error("Unlock: got false, want true")
	}
}

// TestParseDashboardGateConfig_MissingMaxStaleness verifies the fail-loud
// contract: a dashboard: block present without max_staleness must error, not
// silently default to an unbounded staleness window (no-hardcoded-threshold
// mandate, hk-drygf precedent).
func TestParseDashboardGateConfig_MissingMaxStaleness(t *testing.T) {
	t.Parallel()

	_, err := parseDashboardGateConfig([]byte(`
dashboard:
  unlock: false
`))
	var missing *ErrMissingDashboardMaxStaleness
	if !errors.As(err, &missing) {
		t.Fatalf("got err=%v, want *ErrMissingDashboardMaxStaleness", err)
	}
}

// TestParseDashboardGateConfig_BadDuration verifies a malformed max_staleness
// value fails parsing rather than silently coercing.
func TestParseDashboardGateConfig_BadDuration(t *testing.T) {
	t.Parallel()

	_, err := parseDashboardGateConfig([]byte(`
dashboard:
  max_staleness: not-a-duration
`))
	if err == nil {
		t.Fatal("expected error for malformed max_staleness, got nil")
	}
}
