package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// WorkerOfflinePayload is the typed event payload for the worker_offline event (§8.16).
//
// Phase distinguishes where the failure was detected:
//   - "spawn"    — SSH failure during code-sync step a (fetch-base) or step b/c
//     (push/fetch run-branch), before the run is fully underway.
//   - "liveness" — SSH failure during mid-run pgrep/ps liveness probe via
//     PaneHasActiveProcess.
//
// The run recovers via the existing run_stale path (reopen on next dispatch).
type WorkerOfflinePayload struct {
	WorkerName string `json:"worker_name"`
	WorkerHost string `json:"worker_host"`
	// Phase is "spawn" or "liveness".
	Phase      string `json:"phase"`
	Detail     string `json:"detail"`
	DetectedAt string `json:"detected_at"`
}

func init() {
	if err := core.RegisterEventType(core.EventTypeWorkerOffline, func() core.EventPayload { return &WorkerOfflinePayload{} }); err != nil {
		panic("workers: init: register worker_offline: " + err.Error())
	}
	if err := core.RegisterPayloadCompatEntry(core.PayloadCompatEntry{
		TypeName:          core.EventTypeWorkerOffline,
		CurrentVersion:    1,
		CompatWindowHolds: true,
		AdditiveOnly:      true,
	}); err != nil {
		panic("workers: init: register worker_offline compatibility: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

// EmitWorkerOfflineEvent marshals and emits a worker_offline event via emit.
// No-op when emit is nil.
func EmitWorkerOfflineEvent(ctx context.Context, workerName, host, phase, detail string, emit EmitFunc) {
	if emit == nil {
		return
	}
	p := WorkerOfflinePayload{
		WorkerName: workerName,
		WorkerHost: host,
		Phase:      phase,
		Detail:     detail,
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := emit(ctx, core.EventTypeWorkerOffline, b); err != nil {
		slog.ErrorContext(ctx, "worker event emit failed", "event_type", core.EventTypeWorkerOffline, "error", err)
	}
}
