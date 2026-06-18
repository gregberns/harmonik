package daemon

// opsmonitor_schedule.go — auto-registers the ops-monitor fleet watchdog as a
// product-owned schedule on daemon startup (hk-7xr9).
//
// ops-monitor-check.sh was previously run as a hand-rolled tmux bash loop or OS
// cron job (both blocked by macOS TCC). This file replaces that stopgap: the
// daemon registers a "command" action for ops-monitor-check.sh on every startup
// (idempotent: skipped when already present). The schedule is stored in
// .harmonik/schedules.json and picked up by the in-loop runScheduleTick, so it
// survives daemon restarts without any external process.
//
// The job is registered with:
//   - every@5m: fires every 5 minutes via the ScheduleKindEvery interval kind
//   - overlap_policy=skip: skips if the previous run is still alive (pid check)
//   - catchup=off: a single fire on restart; no burst on long downtime
//
// The script runs with cwd=projectDir (set by fireCommandAction) and uses
// HK_PROJECT=${pwd} defaulting to that cwd when the env var is absent.

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/schedule"
)

// opsMonitorJobID is the stable id for the ops-monitor scheduled job.
const opsMonitorJobID = "ops-monitor"

// opsMonitorJob returns the canonical ops-monitor scheduled job definition.
func opsMonitorJob() schedule.ScheduledJob {
	return schedule.ScheduledJob{
		ID: opsMonitorJobID,
		Schedule: schedule.Schedule{
			Kind:     schedule.ScheduleKindEvery,
			Interval: "5m",
		},
		Action: schedule.Action{
			Kind: schedule.ActionKindCommand,
			Argv: []string{"bash", "scripts/ops-monitor-check.sh"},
		},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
		Catchup:       schedule.CatchupOff,
	}
}

// ensureOpsMonitorSchedule registers the ops-monitor watchdog in store if it is
// not already present. It is idempotent: a second call (or a second daemon boot)
// with an existing entry is a no-op. Errors are logged to stderr and do not abort
// daemon startup — a missing ops-monitor schedule is an ops concern, not a fatal.
func ensureOpsMonitorSchedule(store *schedule.Store) {
	if _, ok := store.Get(opsMonitorJobID); ok {
		return
	}
	if err := store.Add(opsMonitorJob()); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: ops-monitor: register schedule: %v\n", err)
	}
}
