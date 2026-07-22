package crewrun

// wire.go — the crew-start / crew-stop RPC wire contract (C2).
//
// Moved verbatim out of internal/daemon/crewstart.go by P2 unit E2 (slice E2a);
// the daemon-side handler that implements CrewHandler stays in internal/daemon.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1, §3.5.
// Bead ref: hk-5tg5o.

import (
	"context"
	"encoding/json"
)

// CrewHandler is the interface the daemon registers to process crew-start and
// crew-stop socket ops.
//
// Registered in daemon.go like CommsSendHandler; dispatched from socket.go's op
// switch for "crew-start" and "crew-stop" ops.
//
// Spec ref: c2-spec.md §3.1 (daemon RPC rationale).
// Bead ref: hk-5tg5o.
type CrewHandler interface {
	// HandleCrewStart processes one crew-start payload. Returns JSON-encoded
	// CrewStartResult on success.
	HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)

	// HandleCrewStop processes one crew-stop payload. Returns JSON-encoded stop
	// confirmation on success.
	HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// CrewStartRequest is the wire payload for a "crew-start" socket op.
//
// Spec ref: c2-spec.md §3.1.
type CrewStartRequest struct {
	// Name is the crew member identifier (charset [a-z0-9-], 1–64 chars).
	Name string `json:"name"`
	// Queue is the named queue the crew member is bound to.
	Queue string `json:"queue"`
	// MissionPath is the path to the handoff file the crew seeds its boot loop from.
	MissionPath string `json:"mission_path"`
	// Type is the agent type folder name (e.g. "admiral", "watch", "crew"). When
	// empty the daemon derives it from a same-named type folder (oversight
	// singletons launch with name == type); an unresolved type reads as the
	// default "crew" via Record.EffectiveType(). Stamping it durably lets the
	// SD-3 reaper honour the manifest lifecycle.persistent flag (hk-dy5gw).
	Type string `json:"type,omitempty"`
	// Harness is the CLI --harness override, or "" when the flag was absent.
	// Highest-precedence tier of the crew-scoped harness resolver (hk-l63b9):
	// flag > mission harness: front-matter > per-crew config > default "claude".
	// This is a SEPARATE resolution chain from the worker per-bead resolveHarness
	// (harnessresolve.go) — a crew has no bead to carry a harness:<type> label.
	Harness string `json:"harness,omitempty"`
}

// CrewStopRequest is the wire payload for a "crew-stop" socket op.
//
// Spec ref: c2-spec.md §3.5.
type CrewStopRequest struct {
	// Name is the crew member to stop.
	Name string `json:"name"`
	// PauseQueue, when true, halts dispatch on the crew's named queue after teardown.
	PauseQueue bool `json:"pause_queue,omitempty"`
}

// CrewStartResult is the SocketResponse.Result payload for a successful crew-start.
//
// Spec ref: c2-spec.md AC-1 (prints the minted session_id).
type CrewStartResult struct {
	// SessionID is the minted (or resumed) session UUID.
	SessionID string `json:"session_id"`
	// Name echoes the crew member name.
	Name string `json:"name"`
}
