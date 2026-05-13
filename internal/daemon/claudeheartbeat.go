package daemon

// claudeheartbeat.go — daemon-side agent_heartbeat emitter for the claude-code
// MVH carve-out (HC-057).
//
// HC-057 permits the daemon to emit agent_heartbeat{phase:"reasoning"} on the
// handler-process's behalf when agent_type is "claude-code", because no
// dedicated harmonik-claude-handler binary exists at MVH.
//
// This file provides newDaemonHeartbeatEmitter, which binds a run's (bus,
// runID, sessionID) at construction time and returns a handler.HeartbeatEmitter
// closure that the caller passes to handler.RunHeartbeatLoop.
//
// Wiring the returned emitter into RunHeartbeatLoop (starting the goroutine) is
// the responsibility of hk-gql20.14/.15; this bead (hk-gql20.17) only provides
// the constructible emitter.
//
// Spec: specs/handler-contract.md §4.9 HC-057;
//
//	specs/claude-hook-bridge.md §4.7 CHB-019.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// daemonHeartbeatPayload is the JSON payload for a daemon-emitted
// agent_heartbeat bus event per HC-057 / CHB-019.
//
// Fields mirror handlercontract.HeartbeatMsg but are re-declared here so the
// daemon package does not depend on the internal wire-schema type for its own
// bus publication path (the bus envelope is authoritative; this is the inner
// payload only).
type daemonHeartbeatPayload struct {
	// SessionID is the stable daemon-assigned session identifier (HC-026a).
	SessionID string `json:"session_id"`
	// Phase is the execution phase, always "reasoning" for daemon-emitted
	// heartbeats per HC-057.
	Phase string `json:"phase"`
}

// newDaemonHeartbeatEmitter returns a handler.HeartbeatEmitter that emits
// agent_heartbeat{phase:"reasoning"} events on the daemon event bus with the
// run-scoped envelope (run_id stamped by bus.EmitWithRunID).
//
// The returned emitter is safe to call from any goroutine.  It is intended to
// be passed directly to handler.RunHeartbeatLoop as the emit callback.
//
// Parameters:
//   - bus:       the daemon event bus; must satisfy handlercontract.EventEmitter.
//   - runID:     the run's identifier; stamped into the bus envelope via
//     EmitWithRunID so subscribers can correlate heartbeats with their run.
//
// Spec: specs/handler-contract.md §4.9 HC-057;
//
//	specs/claude-hook-bridge.md §4.7 CHB-019.
//
// Bead: hk-gql20.17.
func newDaemonHeartbeatEmitter(bus handlercontract.EventEmitter, runID core.RunID) handler.HeartbeatEmitter {
	return func(ctx context.Context, sessionID string, phase string) error {
		pl := daemonHeartbeatPayload{
			SessionID: sessionID,
			Phase:     phase,
		}
		b, err := json.Marshal(pl)
		if err != nil {
			// Marshal of a two-string struct should never fail; surface as an
			// error so RunHeartbeatLoop can log it.
			return fmt.Errorf("daemon: heartbeat: marshal payload: %w", err)
		}
		return bus.EmitWithRunID(ctx, runID, core.EventTypeAgentHeartbeat, b)
	}
}
