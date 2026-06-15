package workers

// tunnelfailed.go — worker_tunnel_failed event payload and emission helper
// (remote-substrate gap #7, bead 3 — tunnel readiness gate).
//
// Emitted when the per-run `ssh -N -R` reverse tunnel's worker-side socket
// fails to become live within the bounded readiness window after the tunnel
// process is started and BEFORE the implementer agent is launched. The hook
// relay retries only on daemon_not_ready, not on a dial failure, so launching
// the agent before the forward is live would produce a silent
// bridge_dial_failed → agent_ready_timeout. On this event the run is NOT
// launched and the bead is reopened for re-dispatch.
//
// Bead ref: hk-rs-tunnel-readiness-cc1w.

import (
	"context"
	"encoding/json"
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
	if err := core.RegisterEventType("worker_tunnel_failed", func() core.EventPayload { return &WorkerTunnelFailedPayload{} }); err != nil {
		panic("workers: init: register worker_tunnel_failed: " + err.Error())
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
	_ = emit(ctx, core.EventTypeWorkerTunnelFailed, b)
}
