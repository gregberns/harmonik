package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// WorkerTunnelFailedPayload is the typed event payload for the
// worker_tunnel_failed event.
//
// RunID/BeadID identify the run whose reverse tunnel never came up; WorkerName/
// WorkerHost identify the remote worker; SocketPath is the worker-side per-run
// socket the tunnel was binding (the path the readiness gate polled); Detail
// carries the timeout/probe error.
type WorkerTunnelFailedPayload struct {
	RunID      string `json:"run_id"`
	BeadID     string `json:"bead_id"`
	WorkerName string `json:"worker_name"`
	WorkerHost string `json:"worker_host"`
	SocketPath string `json:"socket_path"`
	Detail     string `json:"detail"`
	DetectedAt string `json:"detected_at"`
}

func init() {
	if err := core.RegisterEventType(core.EventTypeWorkerTunnelFailed, func() core.EventPayload { return &WorkerTunnelFailedPayload{} }); err != nil {
		panic("workers: init: register worker_tunnel_failed: " + err.Error())
	}
	if err := core.RegisterPayloadCompatEntry(core.PayloadCompatEntry{
		TypeName:          core.EventTypeWorkerTunnelFailed,
		CurrentVersion:    1,
		CompatWindowHolds: true,
		AdditiveOnly:      true,
	}); err != nil {
		panic("workers: init: register worker_tunnel_failed compatibility: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

// EmitWorkerTunnelFailedEvent marshals and emits a worker_tunnel_failed event
// via emit. No-op when emit is nil (mirrors EmitWorkerOfflineEvent).
func EmitWorkerTunnelFailedEvent(ctx context.Context, runID, beadID, workerName, workerHost, socketPath, detail string, emit EmitFunc) {
	if emit == nil {
		return
	}
	p := WorkerTunnelFailedPayload{
		RunID:      runID,
		BeadID:     beadID,
		WorkerName: workerName,
		WorkerHost: workerHost,
		SocketPath: socketPath,
		Detail:     detail,
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := emit(ctx, core.EventTypeWorkerTunnelFailed, b); err != nil {
		slog.ErrorContext(ctx, "worker event emit failed", "event_type", core.EventTypeWorkerTunnelFailed, "error", err)
	}
}
