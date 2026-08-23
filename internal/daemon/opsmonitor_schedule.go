package daemon

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/schedule"
)

const opsMonitorJobID = "ops-monitor"

const (
	opsMonitorDefaultInterval = "5m"
	opsMonitorDefaultScript   = "scripts/ops-monitor-check.sh"
)

func opsMonitorJob(cfg projectconfig.OpsmonitorConfig) schedule.ScheduledJob {
	interval := cfg.Interval
	if interval == "" {
		interval = opsMonitorDefaultInterval
	}
	scriptPath := cfg.ScriptPath
	if scriptPath == "" {
		scriptPath = opsMonitorDefaultScript
	}
	return schedule.ScheduledJob{
		ID: opsMonitorJobID,
		Schedule: schedule.Schedule{
			Kind:     schedule.ScheduleKindEvery,
			Interval: interval,
		},
		Action: schedule.Action{
			Kind: schedule.ActionKindCommand,
			Argv: []string{"bash", scriptPath},
		},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
		Catchup:       schedule.CatchupOff,
	}
}

func ensureOpsMonitorSchedule(store *schedule.Store, cfg projectconfig.OpsmonitorConfig) {
	if _, ok := store.Get(opsMonitorJobID); ok {
		return
	}
	if err := store.Add(opsMonitorJob(cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: ops-monitor: register schedule: %v\n", err)
	}
}
