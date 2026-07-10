package daemon

// dashboardgate.go — the forcing gate (hk-xg6rw): a stale dashboard.json
// blocks new dispatch to captain-curated queues while never touching
// in-flight runs, the mailbox, reconcile, or any daemon-core path.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §4 (recommendation
// A+B hybrid) + §6 item 6.
// Bead ref: hk-xg6rw.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
	"github.com/gregberns/harmonik/internal/digest"
)

// dashboardGateResult is the outcome of one gate evaluation.
type dashboardGateResult struct {
	// Blocked reports whether the gate is currently tripped.
	Blocked bool
	// BlockedQueues is the set of captain-curated queue names (from
	// lanes.json's `queue` field) gated by this trip. Nil/empty when Blocked
	// is false.
	BlockedQueues map[string]bool
	// StaleSecs / MaxStaleness / UpdatedAt feed the dashboard_stale payload.
	StaleSecs    int64
	MaxStaleness time.Duration
	UpdatedAt    string
}

// evaluateDashboardGate reads the dashboard.max_staleness config, the
// operator unlock override, and dashboard.json's freshness, and reports
// whether the forcing gate should currently block new dispatch on
// captain-curated queues (scoped via lanes.json).
//
// Degradation direction on error: a config error (dashboard: block present
// but malformed/missing max_staleness) fails BLOCKING, never silently
// disabled — per the DESIGN §4 no-hardcoded-threshold, fail-loud mandate. The
// caller is expected to log the returned error loudly; it is not fatal to the
// daemon (mirrors the disk_low / other tick-scoped gates).
func evaluateDashboardGate(projectDir string, now time.Time) (dashboardGateResult, error) {
	if projectDir == "" {
		return dashboardGateResult{}, nil
	}

	cfg, cfgErr := digest.LoadDashboardGateConfig(projectDir)
	if cfgErr != nil {
		return dashboardGateResult{Blocked: true}, cfgErr
	}
	if !cfg.Configured() {
		// Operator has not opted into the dashboard: block at all — gate stays
		// off rather than forcing every project to adopt it.
		return dashboardGateResult{}, nil
	}
	if cfg.Unlock {
		return dashboardGateResult{}, nil // config kill-switch
	}

	unlock, unlockErr := dashboard.ReadUnlock(projectDir)
	if unlockErr == nil && unlock.Active(now) {
		return dashboardGateResult{}, nil // harmonik dashboard --unlock override
	}

	ds, readErr := dashboard.Read(projectDir)
	var updatedAt time.Time
	switch {
	case errors.Is(readErr, dashboard.ErrNotFound):
		// Never written: treat as maximally stale — the captain has not
		// adopted the Tier-B mechanism at all yet (DESIGN §1).
	case readErr != nil:
		return dashboardGateResult{Blocked: true}, readErr
	default:
		updatedAt = ds.Updated
	}

	age := cfg.MaxStaleness + time.Second // default: force-stale when never written
	if !updatedAt.IsZero() {
		age = now.Sub(updatedAt)
	}
	if age <= cfg.MaxStaleness {
		return dashboardGateResult{}, nil
	}

	result := dashboardGateResult{
		Blocked:       true,
		BlockedQueues: captainCuratedQueues(projectDir),
		StaleSecs:     int64(age.Seconds()),
		MaxStaleness:  cfg.MaxStaleness,
	}
	if !updatedAt.IsZero() {
		result.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	}
	return result, nil
}

// captainCuratedQueues reads .harmonik/context/lanes.json and returns the set
// of queue names referenced by any lane (DashLane.Queue) — the "captain-
// curated lanes" the DESIGN §4 gate is scoped to. Returns nil when lanes.json
// is absent or malformed: a missing/broken lanes.json fails OPEN on scoping
// (nothing gated) rather than compounding into a second failure mode — only a
// stale dashboard.json itself is the forcing signal.
func captainCuratedQueues(projectDir string) map[string]bool {
	path := filepath.Join(projectDir, lanesJSONPath)
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		return nil
	}
	var lf lanesFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil
	}
	out := make(map[string]bool, len(lf.Lanes))
	for _, l := range lf.Lanes {
		if l.Queue != "" {
			out[l.Queue] = true
		}
	}
	return out
}
