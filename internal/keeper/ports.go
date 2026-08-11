package keeper

import (
	"context"
	"time"
)

// ports.go — the five named ports + RespawnPort (T6, session-keeper-design §1,
// D10). The cycle core (cycle.go) depends exclusively on these interfaces; the
// CyclerConfig function-fields remain as WIRING INPUTS that the fn* adapters
// below fold into the ports, so existing construction sites and tests keep
// working while T7's pure Step reactor drives every side effect through a port.
//
// The six-port set (SK-001..007 / SK-R1):
//   - PanePort    — the tmux write/read boundary (§1a)
//   - GaugePort   — file-state reads + managed-session write-back + the
//     per-tick GateSnapshot burst (§1b)
//   - HandoffPort — handoff file + cycle journal (§1c)
//   - EmitterPort — = keeper.Emitter, verbatim (§1d)
//   - ClockPort   — = substrate.ClockPort, required by reference (§1e / D4);
//     it stays the CyclerConfig.Clock field
//   - RespawnPort — the one-method kill+respawn escalation (PL-021d / D10)

// PanePort is the tmux boundary. Inject MUST follow PL-021d (load-buffer +
// paste-buffer write discipline); Capture is keeper-only (PL-021b §5 forbids
// the daemon this read). SK-R11.
type PanePort interface {
	Inject(ctx context.Context, target, text string) error
	SendEscape(ctx context.Context, target string) error
	SetEnv(ctx context.Context, target, key, value string) error
	Capture(ctx context.Context, target string) (string, error)
	OperatorAttached(target string) bool
}

// RespawnPort is the kill+respawn escalation fired after MaxHandoffTimeouts
// consecutive handoff timeouts above the force threshold (hk-qoz). It is a
// process-lifecycle effect, not a pane write, so it is its own one-method port
// rather than bloating PanePort (D10).
type RespawnPort interface {
	ForceRestart(ctx context.Context, agent string) error
}

// GateSnapshot is the per-tick read-burst of the seven gate-predicate inputs
// (session-keeper-design §3a). The shell samples it ONCE per cycle entry via
// GaugePort.Snapshot instead of seven scattered live reads mid-ladder, so the
// T7 pure Step never touches a port: the ladder becomes a pure function of
// (State, Event-carried GateSnapshot).
//
// Zero-value semantics: LastUserTurnAt / LastAssistantTurnAt are zero when no
// qualifying transcript turn exists OR the corresponding feature is disabled
// (OperatorTurnLookback / PostAnswerGrace == 0 — the adapter skips the heavier
// transcript tail-scan entirely, matching today's lazy gate reads).
type GateSnapshot struct {
	Managed             bool
	CrispIdle           bool
	HoldingDispatch     bool
	Sleeping            bool
	Held                bool
	OperatorAttached    bool
	LastUserTurnAt      time.Time // Gate 5d input
	LastAssistantTurnAt time.Time // Gate 5e input
}

// GaugePort is the keeper's file-state universe (.ctx/.sid/.managed/markers/
// transcript) and the write-back that keeps the watcher bound.
type GaugePort interface {
	ReadGauge() (*CtxFile, time.Time, error) // .ctx (+ .sid overlay when primary UUIDv4)
	SetManagedSession(sessionID string) error
	ClearPrecompactTrigger() error
	// Snapshot performs the one gate-input read-burst per tick; the gate ladder
	// reads ONLY the returned value, never a port.
	Snapshot(sessionID string) GateSnapshot
	// IdleMarkerModTime reports the Stop-hook .idle marker's mtime and whether
	// it exists — the PRIMARY model-done source (T8, SK-014 / design §5). The
	// shell reads it on AwaitModelDone detection ticks; the first mtime ≥
	// t_nonce (strict, no crispIdleTolerance) is ModelDone{source:"idle_marker"}.
	IdleMarkerModTime() (time.Time, bool)
	// LastAssistantTurn reports the most recent real assistant transcript turn
	// for the session — the model-done BACKSTOP source (SK-014) for agents
	// whose Stop hook is not wired: a turn timestamp ≥ t_nonce is
	// ModelDone{source:"transcript_turn"}. Unlike Snapshot's Gate-5e read this
	// is NOT gated on PostAnswerGrace — model-done detection needs it always.
	LastAssistantTurn(sessionID string) (time.Time, bool)
}

// HandoffPort is the handoff-file + cycle-journal filesystem surface. The
// journal is retained for its crash-recovery role (RecoverFromCrash); phase
// vocabulary stays byte-identical (D10).
type HandoffPort interface {
	HandoffPath() string
	ReadHandoff() (string, error)
	HandoffModTime() (time.Time, bool)
	// TruncateHandoff SCRUBS the keeper nonce marker(s) from the handoff file,
	// preserving the crew-authored body. Historical name (hk-4tjyj).
	TruncateHandoff() error
	WriteJournal(j *CycleJournal) error
	ReadJournal() (*CycleJournal, error)
}

// EmitterPort is the durable-bus port — the existing keeper.Emitter verbatim
// (D10 / SK-R1: already the exact EmitWithRunID subset of
// handlercontract.EventEmitter, so every EventEmitter and eventbus.EventBus
// satisfies it with zero adaptation).
type EmitterPort = Emitter
