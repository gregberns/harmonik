package daemon

// dashboardgate.go — the forcing gate (hk-xg6rw): a stale dashboard.json
// blocks new dispatch to captain-curated queues while never touching
// in-flight runs, the mailbox, reconcile, or any daemon-core path.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §4 (recommendation
// A+B hybrid) + §6 item 6.
// Bead ref: hk-xg6rw.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dashboard"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// dashboardGate is the forcing gate's per-loop mutable state, owned solely by
// the runWorkLoop goroutine — no locking.
//
// A nil *dashboardGate is the OFF state: `subsystems.dashboard_gate.enabled:
// false` means this is never constructed, the loop never evaluates the gate, and
// selectNextQueue is handed a nil blocked-queue set. Every method below tolerates
// a nil receiver, so the nil-guard question is answered once here rather than at
// each call site in the loop.
//
// This gate is the ONLY route by which internal/dashboard reaches package daemon,
// and the only reason the core dispatch loop reads the captain's lanes.json — so
// it is exactly the kind of non-core entanglement CHARTER §4 says must be absent
// when switched off, not merely inert.
type dashboardGate struct {
	// lastEval rate-limits evaluation to dashboardGateEvalInterval. The gate
	// reads three JSON files plus config.yaml, so it must not run every tick.
	lastEval time.Time

	// wasBlocked persists across ticks so dashboard_stale / dashboard_refreshed
	// emit on the transition edge only, not on every evaluation.
	wasBlocked bool

	// blockedQueues is the most recent gate verdict, consulted by selectNextQueue.
	blockedQueues map[string]bool

	// logW receives the fail-loud config-error line. Never nil after construction.
	logW io.Writer
}

// newDashboardGateIfEnabled builds the forcing gate's per-loop state, or returns
// nil when `subsystems.dashboard_gate.enabled: false` partitions it away.
//
// The partition is announced on logW: a silent partition is indistinguishable
// from a config that did not take effect. An absent subsystems: block enables the
// gate, so a deployment without one behaves exactly as it did before.
func newDashboardGateIfEnabled(pc projectconfig.ProjectConfig, logW io.Writer) *dashboardGate {
	if logW == nil {
		logW = os.Stderr
	}
	if !pc.Subsystems.Enabled(projectconfig.SubsystemDashboardGate) {
		fmt.Fprintf(logW, "daemon: subsystem %q disabled by .harmonik/config.yaml; dashboard forcing gate not constructed\n", //nolint:errcheck // best-effort stderr status log
			projectconfig.SubsystemDashboardGate)
		return nil
	}
	return &dashboardGate{logW: logW}
}

// blockedQueueSet is the set of captain-curated queues currently withheld from
// NEW item dispatch. Nil on an absent gate — nothing is withheld, which is the
// same answer selectNextQueue already gets when the gate is untripped.
func (g *dashboardGate) blockedQueueSet() map[string]bool {
	if g == nil {
		return nil
	}
	return g.blockedQueues
}

// tick re-evaluates the forcing gate if the rate limit has elapsed, updating the
// blocked-queue set and emitting dashboard_stale / dashboard_refreshed on the
// transition edge. No-op on a nil (absent) gate.
//
// Only NEW item dispatch on captain-curated queues is affected: in-flight runs,
// the mailbox, reconcile, and every daemon-core path are untouched.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §4.
// Bead ref: hk-xg6rw.
func (g *dashboardGate) tick(ctx context.Context, deps workLoopDeps, now time.Time) {
	if g == nil || now.Sub(g.lastEval) < dashboardGateEvalInterval {
		return
	}
	g.lastEval = now

	result, gateErr := evaluateDashboardGate(deps.projectDir, now)
	if gateErr != nil {
		// Fail loud (the DESIGN §4 no-hardcoded-threshold mandate) but not fatal:
		// the result already degrades to Blocked=true, the fail-safe direction.
		fmt.Fprintf(g.logW, "daemon: workloop: dashboard gate: config error, failing closed: %v\n", gateErr) //nolint:errcheck // best-effort stderr status log
	}
	g.blockedQueues = result.BlockedQueues

	switch {
	case result.Blocked && !g.wasBlocked:
		g.wasBlocked = true
		blockedNames := make([]string, 0, len(result.BlockedQueues))
		for name := range result.BlockedQueues {
			blockedNames = append(blockedNames, name)
		}
		sort.Strings(blockedNames)
		payload := core.DashboardStalePayload{
			MaxStalenessSecs: int64(result.MaxStaleness.Seconds()),
			StaleSecs:        result.StaleSecs,
			UpdatedAt:        result.UpdatedAt,
			BlockedQueues:    blockedNames,
			DetectedAt:       now.UTC().Format(time.RFC3339),
		}
		if raw, mErr := json.Marshal(payload); mErr == nil {
			_ = deps.bus.Emit(ctx, core.EventTypeDashboardStale, raw) //nolint:errcheck // best-effort observability emit
		}
	case !result.Blocked && g.wasBlocked:
		g.wasBlocked = false
		payload := core.DashboardRefreshedPayload{
			Reason:     "refreshed",
			UpdatedAt:  result.UpdatedAt,
			DetectedAt: now.UTC().Format(time.RFC3339),
		}
		if raw, mErr := json.Marshal(payload); mErr == nil {
			_ = deps.bus.Emit(ctx, core.EventTypeDashboardRefreshed, raw) //nolint:errcheck // best-effort observability emit
		}
	}
}

// dashboardGateResult is the outcome of one gate evaluation.
type dashboardGateResult struct {
	// Blocked reports whether the gate is currently tripped.
	Blocked bool
	// BlockedQueues is the set of captain-curated queue names (from
	// lanes.json's `queue` field) gated by this trip. Populated whenever
	// Blocked is true — including the config/read error paths — because
	// selectNextQueue (workloop.go) gates dispatch off THIS MAP alone, not
	// off Blocked; a nil map here would silently defeat the fail-loud
	// mandate. Nil/empty when Blocked is false, or when lanes.json itself is
	// absent/malformed (captainCuratedQueues fails open on scoping only).
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
		// BlockedQueues MUST be populated here, not left nil: selectNextQueue
		// (workloop.go) gates dispatch off BlockedQueues alone, not off
		// Blocked. A nil map on a config error would silently NOT force
		// anything — exactly the fail-open gap the DESIGN §4 fail-loud
		// mandate forbids.
		return dashboardGateResult{Blocked: true, BlockedQueues: captainCuratedQueues(projectDir)}, cfgErr
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
		// Same fail-loud requirement as the cfgErr branch above: populate
		// BlockedQueues so the dispatch gate actually engages.
		return dashboardGateResult{Blocked: true, BlockedQueues: captainCuratedQueues(projectDir)}, readErr
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
