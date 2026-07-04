package daemon

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// detectAndEmitSupervisorRevival scans the durable JSONL event log to determine
// whether the previous daemon session ended without a graceful daemon_shutdown
// event. If so, emits supervisor_revival{cause: unexpected_exit} with the prior
// session's PID and binary_commit_hash for postmortem correlation.
//
// Must be called AFTER daemon_started has been emitted and fsynced for the
// current session (daemon_started is F-class). By the time this runs the current
// session's daemon_started is already in the log, so the prior session is the
// second-to-last daemon_started block observed during the scan.
//
// Detection algorithm: scan all events in file order, grouping into daemon
// sessions delimited by daemon_started. Within each session window, record
// whether a daemon_shutdown event appeared. If the prior session (second-to-last)
// has no daemon_shutdown, the prior daemon was terminated uncleanly (SIGKILL,
// OOM, panic with no defer-recover), and supervisor_revival is emitted.
//
// Non-fatal: a missing JSONL file, a scan error, or an emit failure all result
// in a silent return so startup is never blocked. This is purely observability.
//
// Spec ref: specs/event-model.md §8.7.20.
// Bead ref: hk-rnkuy.
func detectAndEmitSupervisorRevival(ctx context.Context, eventsPath string, bus handlercontract.EventEmitter) {
	if eventsPath == "" {
		return
	}

	type daemonSession struct {
		pid         int
		hash        string
		hasShutdown bool
	}

	var sessions []daemonSession
	var cur *daemonSession

	for ev := range eventbus.ScanAfter(eventsPath, core.EventID{}) {
		switch core.EventType(ev.Type) {
		case core.EventTypeDaemonStarted:
			sessions = append(sessions, daemonSession{})
			cur = &sessions[len(sessions)-1]
			var p core.DaemonStartedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				cur.pid = p.PID
				cur.hash = p.BinaryCommitHash
			}
		case core.EventTypeDaemonShutdown:
			if cur != nil {
				cur.hasShutdown = true
			}
		}
	}

	// Need at least two sessions: the prior one and the current one.
	if len(sessions) < 2 {
		return
	}

	prior := sessions[len(sessions)-2]
	if prior.hasShutdown {
		return // prior session ended with a graceful daemon_shutdown
	}

	// Prior session ended without daemon_shutdown → unexpected death.
	payload := core.SupervisorRevivalPayload{
		RevivedAt:             time.Now().UTC().Format(time.RFC3339),
		Cause:                 core.SupervisorRevivalCauseUnexpectedExit,
		PriorPID:              prior.pid,
		PriorBinaryCommitHash: prior.hash,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeSupervisorRevival, payloadBytes)
}
