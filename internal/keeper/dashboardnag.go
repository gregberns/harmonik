package keeper

import (
	"context"
	"errors"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/keeper/panehost"
)

const dashboardNagApproachFrac = 0.8

const dashboardNagCooldown = 10 * time.Minute

const dashboardNagText = "[KEEPER NAG] dashboard.json is stale or nearing its staleness window. " +
	"Refresh .harmonik/context/dashboard.json (updated + priorities) soon — " +
	"the fleet stops staffing new work on captain-curated queues once it trips."

func (w *Watcher) maybeNagDashboardStale(ctx context.Context, now time.Time) {
	if w.cfg.TmuxTarget == "" {
		return
	}
	if !w.lastDashboardNagAt.IsZero() && now.Sub(w.lastDashboardNagAt) < dashboardNagCooldown {
		return
	}

	cfg, cfgErr := digest.LoadDashboardGateConfig(w.cfg.ProjectDir)
	if cfgErr != nil {
		w.injectDashboardNag(ctx, now)
		return
	}
	if !cfg.Configured() {
		return
	}

	unlock, unlockErr := dashboard.ReadUnlock(w.cfg.ProjectDir)
	if unlockErr != nil {
		w.injectDashboardNag(ctx, now)
		return
	}
	if unlock.Active(now) {
		return
	}

	ds, readErr := dashboard.Read(w.cfg.ProjectDir)
	var updatedAt time.Time
	switch {
	case errors.Is(readErr, dashboard.ErrNotFound):
	case readErr != nil:
		w.injectDashboardNag(ctx, now)
		return
	default:
		updatedAt = ds.Updated
	}

	age := cfg.MaxStaleness + time.Second
	if !updatedAt.IsZero() {
		age = now.Sub(updatedAt)
	}
	approachThreshold := time.Duration(float64(cfg.MaxStaleness) * dashboardNagApproachFrac)
	if age < approachThreshold {
		return
	}

	w.injectDashboardNag(ctx, now)
}

func (w *Watcher) injectDashboardNag(ctx context.Context, now time.Time) {
	inject := w.cfg.DashboardNagInjectFn
	if inject == nil {
		ph := w.cfg.paneHost()
		inject = func(ctx context.Context, target, text string) error {
			return ph.Inject(ctx, panehost.Target(target), text)
		}
	}
	if err := inject(ctx, w.cfg.TmuxTarget, dashboardNagText); err != nil {
		return
	}
	w.lastDashboardNagAt = now
}
