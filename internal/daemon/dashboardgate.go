package daemon

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
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

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

func (g *dashboardGate) blockedQueueSet() map[string]bool {
	if g == nil {
		return nil
	}
	return g.blockedQueues
}

func (g *dashboardGate) tick(ctx context.Context, projectDir string, bus handlercontract.EventEmitter, now time.Time) {
	if g == nil || now.Sub(g.lastEval) < dashboardGateEvalInterval {
		return
	}
	g.lastEval = now

	result, gateErr := evaluateDashboardGate(projectDir, now)
	if gateErr != nil {
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
			_ = bus.Emit(ctx, core.EventTypeDashboardStale, raw) //nolint:errcheck // best-effort observability emit
		}
	case !result.Blocked && g.wasBlocked:
		g.wasBlocked = false
		payload := core.DashboardRefreshedPayload{
			Reason:     "refreshed",
			UpdatedAt:  result.UpdatedAt,
			DetectedAt: now.UTC().Format(time.RFC3339),
		}
		if raw, mErr := json.Marshal(payload); mErr == nil {
			_ = bus.Emit(ctx, core.EventTypeDashboardRefreshed, raw) //nolint:errcheck // best-effort observability emit
		}
	}
}

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

func evaluateDashboardGate(projectDir string, now time.Time) (dashboardGateResult, error) {
	if projectDir == "" {
		return dashboardGateResult{}, nil
	}

	cfg, cfgErr := digest.LoadDashboardGateConfig(projectDir)
	if cfgErr != nil {
		return dashboardGateResult{Blocked: true, BlockedQueues: captainCuratedQueues(projectDir)}, cfgErr
	}
	if !cfg.Configured() {
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
	case readErr != nil:
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
