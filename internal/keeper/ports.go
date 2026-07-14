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
// transcript) and the one write-back that keeps the watcher bound.
//
// SetHold is here (not on the §1b four-method read surface) because the Gate-5d
// auto-hold is a GaugePort-owned marker write the reactor must be able to drive
// as an action (§3b maps the SetHold action to GaugePort).
type GaugePort interface {
	ReadGauge() (*CtxFile, time.Time, error) // .ctx (+ .sid overlay when primary UUIDv4)
	SetManagedSession(sessionID string) error
	ClearPrecompactTrigger() error
	SetHold() (string, error) // Gate 5d auto-hold marker write (hk-74iyd)
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
	TruncateHandoff() error
	WriteJournal(j *CycleJournal) error
	ReadJournal() (*CycleJournal, error)
}

// EmitterPort is the durable-bus port — the existing keeper.Emitter verbatim
// (D10 / SK-R1: already the exact EmitWithRunID subset of
// handlercontract.EventEmitter, so every EventEmitter and eventbus.EventBus
// satisfies it with zero adaptation).
type EmitterPort = Emitter

// ─── fn-field adapters ──────────────────────────────────────────────────────
// The production adapters: they wire the (defaulted) CyclerConfig function
// fields onto the ports, so runtime behavior is unchanged and every existing
// fn-field test fake keeps working. Built by NewCycler AFTER applyDefaults, so
// every fn they reach is non-nil (except SendEscapeFn / ForceRestartFn, whose
// nil semantics are preserved below).

// fnPane adapts the pane-boundary fn-fields to PanePort.
type fnPane struct{ cfg *CyclerConfig }

func (p fnPane) Inject(ctx context.Context, target, text string) error {
	return p.cfg.InjectFn(ctx, target, text)
}

// SendEscape preserves the SendEscapeFn nil semantics: nil → no Escape sent.
func (p fnPane) SendEscape(ctx context.Context, target string) error {
	if p.cfg.SendEscapeFn == nil {
		return nil
	}
	return p.cfg.SendEscapeFn(ctx, target)
}

func (p fnPane) SetEnv(ctx context.Context, target, key, value string) error {
	return p.cfg.SetTmuxEnvFn(ctx, target, key, value)
}

// Capture delegates to the production capture-pane read (awaitack.go). There is
// no CyclerConfig fn-field for it — the auto-cycle path never captures; the
// method exists so the ONE production PanePort also serves the restart-now /
// await-ack surface (design §1a maps AwaitAckConfig.Capture here).
func (p fnPane) Capture(ctx context.Context, target string) (string, error) {
	return CaptureTmuxPane(ctx, target)
}

func (p fnPane) OperatorAttached(target string) bool {
	return p.cfg.OperatorAttachedFn(target)
}

// fnGauge adapts the file-state fn-fields to GaugePort.
type fnGauge struct{ cfg *CyclerConfig }

func (g fnGauge) ReadGauge() (*CtxFile, time.Time, error) {
	return g.cfg.ReadGaugeFn(g.cfg.ProjectDir, g.cfg.AgentName)
}

func (g fnGauge) SetManagedSession(sessionID string) error {
	return g.cfg.SetManagedSessionFn(g.cfg.ProjectDir, g.cfg.AgentName, sessionID)
}

func (g fnGauge) ClearPrecompactTrigger() error {
	return g.cfg.ClearPrecompactTriggerFn(g.cfg.ProjectDir, g.cfg.AgentName)
}

// SetHold routes the Gate-5d auto-hold marker stamp through the cycle Clock
// (T5-seam fold: gates.go marker stamps honor the determinism port).
func (g fnGauge) SetHold() (string, error) {
	return setHoldAt(g.cfg.ProjectDir, g.cfg.AgentName, g.cfg.Clock)
}

// Snapshot performs the per-tick gate-input read-burst. Guard parity with the
// old scattered reads is preserved exactly:
//   - Sleeping is only probed with a non-empty sessionID (the old Gate-5b
//     guard) — the raw IsSleeping fail-closed "" → true is a caller-side
//     concern the ladder never hit;
//   - OperatorAttached is only probed when TmuxTarget is non-empty (the old
//     operatorAttached() guard — no pane, nothing to race);
//   - transcript turns are only tail-scanned when the corresponding feature
//     (OperatorTurnLookback / PostAnswerGrace) is enabled AND a sessionID
//     exists, matching the old Gate-5d/5e lazy reads.
func (g fnGauge) Snapshot(sessionID string) GateSnapshot {
	cfg := g.cfg
	s := GateSnapshot{
		Managed:         cfg.IsManagedFn(cfg.ProjectDir, cfg.AgentName),
		CrispIdle:       cfg.CrispIdleFn(cfg.ProjectDir, cfg.AgentName),
		HoldingDispatch: cfg.HoldingDispatchFn(cfg.ProjectDir, cfg.AgentName),
		Held:            cfg.HeldCheckFn(cfg.ProjectDir, cfg.AgentName),
	}
	if sessionID != "" {
		s.Sleeping = cfg.SleepingCheckFn(cfg.ProjectDir, sessionID)
	}
	if cfg.TmuxTarget != "" {
		s.OperatorAttached = cfg.OperatorAttachedFn(cfg.TmuxTarget)
	}
	if sessionID != "" && (cfg.OperatorTurnLookback > 0 || cfg.PostAnswerGrace > 0) {
		turn := cfg.recentTurnFn()
		dir := cfg.resolvedTranscriptDir()
		if cfg.OperatorTurnLookback > 0 {
			if t, ok := turn(dir, sessionID, "user"); ok {
				s.LastUserTurnAt = t
			}
		}
		if cfg.PostAnswerGrace > 0 {
			if t, ok := turn(dir, sessionID, "assistant"); ok {
				s.LastAssistantTurnAt = t
			}
		}
	}
	return s
}

// IdleMarkerModTime delegates to the (defaulted) IdleMarkerModTimeFn — the
// os.Stat of .harmonik/keeper/<agent>.idle in production.
func (g fnGauge) IdleMarkerModTime() (time.Time, bool) {
	return g.cfg.IdleMarkerModTimeFn(g.cfg.ProjectDir, g.cfg.AgentName)
}

// LastAssistantTurn tail-scans the session transcript for the most recent
// real assistant turn (the SK-014 backstop). Empty sessionID → no transcript
// to scan (zero, false).
func (g fnGauge) LastAssistantTurn(sessionID string) (time.Time, bool) {
	if sessionID == "" {
		return time.Time{}, false
	}
	return g.cfg.recentTurnFn()(g.cfg.resolvedTranscriptDir(), sessionID, "assistant")
}

// fnHandoff adapts the handoff-file + journal fn-fields to HandoffPort. Paths
// are computed per call (never cached), matching the old call sites.
type fnHandoff struct{ cfg *CyclerConfig }

func (h fnHandoff) HandoffPath() string {
	return h.cfg.HandoffFilePath(h.cfg.ProjectDir, h.cfg.AgentName)
}

func (h fnHandoff) ReadHandoff() (string, error) {
	return h.cfg.ReadHandoff(h.HandoffPath())
}

func (h fnHandoff) HandoffModTime() (time.Time, bool) {
	return h.cfg.HandoffModTimeFn(h.HandoffPath())
}

func (h fnHandoff) TruncateHandoff() error {
	return h.cfg.TruncateHandoffFn(h.HandoffPath())
}

func (h fnHandoff) WriteJournal(j *CycleJournal) error {
	return h.cfg.WriteJournalFn(h.journalPath(), j)
}

func (h fnHandoff) ReadJournal() (*CycleJournal, error) {
	return h.cfg.ReadJournalFn(h.journalPath())
}

// journalPath is the cycle-journal location (byte-identical to the old
// Cycler.journalPath): .harmonik/keeper/<agent>.cycle.
func (h fnHandoff) journalPath() string {
	return journalFilePath(h.cfg.ProjectDir, h.cfg.AgentName)
}

// fnRespawn adapts ForceRestartFn to RespawnPort. Constructed only when the
// fn is non-nil (NewCycler leaves the port nil otherwise, preserving the
// dormant-escalation default).
type fnRespawn struct {
	fn func(ctx context.Context, agentName string) error
}

func (r fnRespawn) ForceRestart(ctx context.Context, agent string) error {
	return r.fn(ctx, agent)
}
