package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

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
		switch ev.Type {
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
		default:
		}
	}

	if len(sessions) < 2 {
		return
	}

	prior := sessions[len(sessions)-2]
	if prior.hasShutdown {
		return // prior session ended with a graceful daemon_shutdown
	}

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
	if emitErr := bus.Emit(ctx, core.EventTypeSupervisorRevival, payloadBytes); emitErr != nil {
		slog.WarnContext(ctx, "daemon: emit supervisor_revival failed", "err", emitErr, "prior_pid", prior.pid)
	}
}
