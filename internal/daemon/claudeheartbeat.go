package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

type daemonHeartbeatPayload struct {
	// SessionID is the stable daemon-assigned session identifier (HC-026a).
	SessionID string `json:"session_id"`
	// Phase is the execution phase, always "reasoning" for daemon-emitted
	// heartbeats per HC-057.
	Phase string `json:"phase"`
}

func newDaemonHeartbeatEmitter(bus handlercontract.EventEmitter, runID core.RunID) handler.HeartbeatEmitter {
	return func(ctx context.Context, sessionID string, phase string) error {
		pl := daemonHeartbeatPayload{
			SessionID: sessionID,
			Phase:     phase,
		}
		b, err := json.Marshal(pl)
		if err != nil {
			return fmt.Errorf("daemon: heartbeat: marshal payload: %w", err)
		}
		return bus.EmitWithRunID(ctx, runID, core.EventTypeAgentHeartbeat, b)
	}
}
