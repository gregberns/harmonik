package daemon

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/schedule"
)

const ctxWatchdogJobID = "ctx-watchdog"

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
