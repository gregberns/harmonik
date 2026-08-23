package daemon

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/schedule"
)

const watchLivenessPingJobID = "watch-liveness-ping"

const watchVerifyServicesJobID = "watch-verify-services"

func ensureWatchLivenessSchedule(store *schedule.Store, watchCfg projectconfig.WatchConfig, _ string) {
	target := watchCfg.StatusTarget
	if target == "" {
		target = "captain"
	}

	livenessPingBody := watchCfg.LivenessPingBody
	if livenessPingBody == "" {
		livenessPingBody = watchLivenessPingJobID
	}
	verifyServicesBody := watchCfg.VerifyServicesBody
	if verifyServicesBody == "" {
		verifyServicesBody = watchVerifyServicesJobID
	}

	if watchCfg.LivenessInterval != "" {
		if _, ok := store.Get(watchLivenessPingJobID); !ok {
			job := schedule.ScheduledJob{
				ID: watchLivenessPingJobID,
				Schedule: schedule.Schedule{
					Kind:     schedule.ScheduleKindEvery,
					Interval: watchCfg.LivenessInterval,
				},
				Action: schedule.Action{
					Kind:  schedule.ActionKindCommsSend,
					To:    target,
					Body:  livenessPingBody,
					Topic: "liveness",
				},
				Enabled:       true,
				OverlapPolicy: schedule.OverlapPolicyAllow,
				Catchup:       schedule.CatchupOff,
			}
			if err := store.Add(job); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: watch-liveness: register liveness-ping schedule: %v\n", err)
			}
		}
	}

	if watchCfg.DigestInterval != "" {
		if _, ok := store.Get(watchVerifyServicesJobID); !ok {
			job := schedule.ScheduledJob{
				ID: watchVerifyServicesJobID,
				Schedule: schedule.Schedule{
					Kind:     schedule.ScheduleKindEvery,
					Interval: watchCfg.DigestInterval,
				},
				Action: schedule.Action{
					Kind:  schedule.ActionKindCommsSend,
					To:    target,
					Body:  verifyServicesBody,
					Topic: "liveness",
				},
				Enabled:       true,
				OverlapPolicy: schedule.OverlapPolicyAllow,
				Catchup:       schedule.CatchupOff,
			}
			if err := store.Add(job); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: watch-liveness: register verify-services schedule: %v\n", err)
			}
		}
	}
}
