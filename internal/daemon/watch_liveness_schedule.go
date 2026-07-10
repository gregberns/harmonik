package daemon

// watch_liveness_schedule.go — auto-registers the watch mutual-liveness ping
// and verify-services-up schedules on daemon startup (hk-we6-watch-scheduled-send-6onfu).
//
// WE6: the watch sends a periodic liveness ping to captain using the NATIVE
// comms-send schedule action (ActionKindCommsSend) — no bash -c wrapper.
// Intervals come entirely from WatchConfig (watch.liveness_interval and
// watch.digest_interval in .harmonik/config.yaml): NO Go-literal interval is
// embedded here. This is the hard rule distinguishing WE6 from the
// opsmonitor_schedule.go / ctx_watchdog_schedule.go precedents which hardcode "5m".
//
// Registration is SKIPPED when an interval field is empty (not yet configured).
// The fail-loud gate is checkMissingWatchValues (cmd/harmonik/resolve_watch_config.go):
// it refuses to start the watch session until both interval keys are present,
// ensuring that once the watch runs the schedules are already registered by the daemon.

import (
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/schedule"
)

// watchLivenessPingJobID is the stable id for the watch→captain liveness ping job.
const watchLivenessPingJobID = "watch-liveness-ping"

// watchVerifyServicesJobID is the stable id for the watch verify-services-up job.
const watchVerifyServicesJobID = "watch-verify-services"

// ensureWatchLivenessSchedule registers the watch liveness ping and
// verify-services-up schedules in store when they are not already present.
// It is idempotent: a second call (or a second daemon boot) with existing
// entries is a no-op.
//
// The liveness ping interval comes from watchCfg.LivenessInterval
// (watch.liveness_interval in config.yaml). The verify-services interval comes
// from watchCfg.DigestInterval (watch.digest_interval). Neither key has a Go
// literal fallback: when absent, registration is skipped and the watch session
// boot gate (checkMissingWatchValues) will refuse to start the watch until the
// operator supplies both values.
//
// The liveness ping target defaults to "captain" when watchCfg.StatusTarget is
// empty, matching the WE7 §7 exception for target keys. The message bodies
// default to "watch-liveness-ping" / "watch-verify-services" when
// watchCfg.LivenessPingBody / watchCfg.VerifyServicesBody are empty, matching
// the same §7 exception (not fail-loud).
//
// Errors are logged to stderr and do not abort daemon startup.
func ensureWatchLivenessSchedule(store *schedule.Store, watchCfg WatchConfig, _ string) {
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
