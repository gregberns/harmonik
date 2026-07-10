package keeper

// dashboardnag.go — the DESIGN §4 soft pre-nag (recommendation B): on each
// keeper tick, when dashboard.json is approaching or past its configured
// staleness window, nudge the captain's own pane BEFORE the hk-xg6rw forcing
// gate trips — so the gate rarely actually fires. Soft advisory only: a
// wedged captain that ignores the nudge still gets caught by the daemon-side
// gate (internal/daemon/dashboardgate.go), which is the hard backstop.
//
// Uses internal/digest + internal/dashboard directly, NOT internal/daemon —
// internal/keeper importing internal/daemon is banned by the depguard
// component matrix (.golangci.yml), so the keeper watcher stays independent
// of the daemon package.
//
// Spec ref: plans/2026-07-03-operator-dashboard/DESIGN.md §4 (recommendation
// A+B hybrid).
// Bead ref: hk-xg6rw.

import (
	"context"
	"errors"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
	"github.com/gregberns/harmonik/internal/digest"
)

// dashboardNagApproachFrac is the fraction of dashboard.max_staleness at which
// the pre-nag starts firing — before the forcing gate itself trips at 100%.
// 0.8 gives the captain a visible warning window without nagging on every
// normal refresh cadence.
const dashboardNagApproachFrac = 0.8

// dashboardNagCooldown is the minimum interval between successive pre-nag
// injections, so a genuinely wedged captain gets repeated nudges without
// being spammed every PollInterval tick.
const dashboardNagCooldown = 10 * time.Minute

// dashboardNagText is the advisory injected into the captain's pane. Distinct
// from wrapUpWarningText (context budget) — this is about the captain-curated
// planning file going stale.
const dashboardNagText = "[KEEPER NAG] dashboard.json is stale or nearing its staleness window. " +
	"Refresh .harmonik/context/dashboard.json (updated + priorities) soon — " +
	"the fleet stops staffing new work on captain-curated queues once it trips."

// maybeNagDashboardStale checks dashboard.json's freshness against the
// dashboard: config block and, when approaching or past the staleness window,
// injects dashboardNagText into w.cfg.TmuxTarget.
//
// No-op when: the dashboard: block is not configured (operator never opted
// in), TmuxTarget is empty (unit tests / no pane to nudge), the operator has
// already applied the unlock override, or the cooldown has not elapsed since
// the last nag.
//
// Errors are non-fatal and never returned: a config/read failure degrades to
// "nag anyway" (fail loud toward nudging, mirroring the daemon gate's
// fail-blocking direction) rather than silently skipping — but the watcher's
// main tick loop must never stall on a dashboard-file read failure.
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

	if unlock, _ := dashboard.ReadUnlock(w.cfg.ProjectDir); unlock.Active(now) {
		return
	}

	ds, readErr := dashboard.Read(w.cfg.ProjectDir)
	var updatedAt time.Time
	switch {
	case errors.Is(readErr, dashboard.ErrNotFound):
		// never written: treat as maximally stale, same as the daemon gate.
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

// injectDashboardNag delivers dashboardNagText and, on success, stamps
// lastDashboardNagAt so the cooldown gate takes effect. Injection failures are
// best-effort: the forcing gate is the hard backstop, so a dropped nudge is
// not fatal.
func (w *Watcher) injectDashboardNag(ctx context.Context, now time.Time) {
	inject := w.cfg.DashboardNagInjectFn
	if inject == nil {
		inject = InjectText
	}
	if err := inject(ctx, w.cfg.TmuxTarget, dashboardNagText); err != nil {
		return
	}
	w.lastDashboardNagAt = now
}
