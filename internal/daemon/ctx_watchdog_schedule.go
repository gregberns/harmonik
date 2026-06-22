package daemon

// ctx_watchdog_schedule.go — auto-registers the ctx-watchdog as a
// product-owned schedule on daemon startup (hk-sbitr).
//
// The ctx-watchdog (scripts/ctx-watchdog-launch.sh) is an independent
// Sonnet session that polls crew context gauges every 30 minutes and
// force-restarts any session over the 300k token cap. Prior to this bead
// it ran only when a captain health-tick manually re-ran the script; if the
// captain died the watchdog was never relaunched (it died ~36h on 2026-06-20,
// leaving 5 crews with zero context enforcement — paul ballooned to 669k).
//
// This file promotes the watchdog to a first-class daemon-supervised process,
// mirroring the ops-monitor pattern (opsmonitor_schedule.go). The daemon
// registers an "every@5m" schedule whose command is the watchdog launcher.
// The launcher already guards with a `tmux has-session` check
// (scripts/ctx-watchdog-launch.sh:46-49) so re-running every 5 minutes while
// the session is alive is a safe no-op; if the session dies, the next tick
// relaunches it automatically.
//
// The schedule is gated by watchdog.enabled in .harmonik/config.yaml
// (default true when absent or not configured). Disabling skips registration
// entirely so the schedule is never added to .harmonik/schedules.json.
//
// The job is registered with:
//   - every@5m: fires every 5 minutes via the ScheduleKindEvery interval kind
//   - overlap_policy=skip: a safe no-op when the session is already alive
//   - catchup=off: a single fire on restart; no burst on long downtime

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/schedule"
)

// ctxWatchdogJobID is the stable id for the ctx-watchdog scheduled job.
const ctxWatchdogJobID = "ctx-watchdog"

// ctxWatchdogJob returns the canonical ctx-watchdog scheduled job definition.
func ctxWatchdogJob() schedule.ScheduledJob {
	return schedule.ScheduledJob{
		ID: ctxWatchdogJobID,
		Schedule: schedule.Schedule{
			Kind:     schedule.ScheduleKindEvery,
			Interval: "5m",
		},
		Action: schedule.Action{
			Kind: schedule.ActionKindCommand,
			Argv: []string{"bash", "scripts/ctx-watchdog-launch.sh"},
		},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
		Catchup:       schedule.CatchupOff,
	}
}

// ensureCtxWatchdogSchedule registers the ctx-watchdog in store if it is not
// already present and enabled is true. It is idempotent: a second call (or a
// second daemon boot) with an existing entry is a no-op. When enabled is false
// the function returns without touching the store (the operator has opted out).
// Errors are logged to stderr and do not abort daemon startup — a missing
// ctx-watchdog schedule is an ops concern, not a fatal.
func ensureCtxWatchdogSchedule(store *schedule.Store, enabled bool) {
	if !enabled {
		return
	}
	if _, ok := store.Get(ctxWatchdogJobID); ok {
		return
	}
	if err := store.Add(ctxWatchdogJob()); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: ctx-watchdog: register schedule: %v\n", err)
	}
}
