package keeper

import (
	"context"
	"time"
)

// NewCycler keeps the old construction surface while package tests migrate.
// Production does not compile this adapter.
func NewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	cfg.applyDefaults()
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	c := &Cycler{cfg: cfg, emitter: emitter, cycleIDs: cycleIDFunc(cfg.CycleIDGen)}
	pane := c.cfg.Pane
	if pane == nil {
		pane = fnPane{cfg: &c.cfg}
	}
	gauge := c.cfg.Gauge
	if gauge == nil {
		gauge = fnGauge{cfg: &c.cfg}
	}
	handoff := c.cfg.Handoff
	if handoff == nil {
		handoff = fnHandoff{cfg: &c.cfg}
	}
	c.pane = pane
	c.context = gauge
	c.activity = legacyActivityProbe{port: gauge}
	c.snapshot = gauge.Snapshot
	c.handoff = legacyHandoffDocument{port: handoff}
	c.journal = legacyJournalStore{port: handoff}
	c.respawn = c.cfg.Respawn
	if c.respawn == nil && c.cfg.ForceRestartFn != nil {
		c.respawn = fnRespawn{fn: c.cfg.ForceRestartFn}
	}
	c.cfg.hasRespawn = c.respawn != nil
	c.machine = NewCycle(&c.cfg)
	return c
}

type legacyActivityProbe struct{ port GaugePort }

func (a legacyActivityProbe) IdleMarkerModTime() (time.Time, bool) {
	return a.port.IdleMarkerModTime()
}

func (a legacyActivityProbe) LastUserTurn(string) (time.Time, bool) {
	return time.Time{}, false
}

func (a legacyActivityProbe) LastAssistantTurn(sid string) (time.Time, bool) {
	return a.port.LastAssistantTurn(sid)
}

type legacyHandoffDocument struct{ port HandoffPort }

func (a legacyHandoffDocument) Path() string               { return a.port.HandoffPath() }
func (a legacyHandoffDocument) Read() (string, error)      { return a.port.ReadHandoff() }
func (a legacyHandoffDocument) ModTime() (time.Time, bool) { return a.port.HandoffModTime() }
func (a legacyHandoffDocument) ScrubNonce() error          { return a.port.TruncateHandoff() }

type legacyJournalStore struct{ port HandoffPort }

func (a legacyJournalStore) Write(j *CycleJournal) error  { return a.port.WriteJournal(j) }
func (a legacyJournalStore) Read() (*CycleJournal, error) { return a.port.ReadJournal() }

type fnPane struct{ cfg *CyclerConfig }

func (p fnPane) Inject(ctx context.Context, target, text string) error {
	return p.cfg.InjectFn(ctx, target, text)
}
func (p fnPane) SendEscape(ctx context.Context, target string) error {
	if p.cfg.SendEscapeFn == nil {
		return nil
	}
	return p.cfg.SendEscapeFn(ctx, target)
}
func (p fnPane) SetEnv(ctx context.Context, target, key, value string) error {
	return p.cfg.SetTmuxEnvFn(ctx, target, key, value)
}
func (p fnPane) Capture(ctx context.Context, target string) (string, error) {
	return CaptureTmuxPane(ctx, target)
}
func (p fnPane) OperatorAttached(target string) bool { return p.cfg.OperatorAttachedFn(target) }

type fnGauge struct{ cfg *CyclerConfig }

func (g fnGauge) ReadGauge() (*CtxFile, time.Time, error) {
	return g.cfg.ReadGaugeFn(g.cfg.ProjectDir, g.cfg.AgentName)
}
func (g fnGauge) SetManagedSession(sid string) error {
	return g.cfg.SetManagedSessionFn(g.cfg.ProjectDir, g.cfg.AgentName, sid)
}
func (g fnGauge) ClearPrecompactTrigger() error {
	return g.cfg.ClearPrecompactTriggerFn(g.cfg.ProjectDir, g.cfg.AgentName)
}
func (g fnGauge) Snapshot(sid string) GateSnapshot {
	cfg := g.cfg
	s := GateSnapshot{
		Managed:         cfg.IsManagedFn(cfg.ProjectDir, cfg.AgentName),
		CrispIdle:       cfg.CrispIdleFn(cfg.ProjectDir, cfg.AgentName),
		HoldingDispatch: cfg.HoldingDispatchFn(cfg.ProjectDir, cfg.AgentName),
		Held:            cfg.HeldCheckFn(cfg.ProjectDir, cfg.AgentName),
	}
	if sid != "" {
		s.Sleeping = cfg.SleepingCheckFn(cfg.ProjectDir, sid)
	}
	if cfg.TmuxTarget != "" {
		s.OperatorAttached = cfg.OperatorAttachedFn(cfg.TmuxTarget)
	}
	if sid != "" && (cfg.OperatorTurnLookback > 0 || cfg.PostAnswerGrace > 0) {
		turn, dir := cfg.recentTurnFn(), cfg.resolvedTranscriptDir()
		if cfg.OperatorTurnLookback > 0 {
			s.LastUserTurnAt, _ = turn(dir, sid, "user")
		}
		if cfg.PostAnswerGrace > 0 {
			s.LastAssistantTurnAt, _ = turn(dir, sid, "assistant")
		}
	}
	return s
}
func (g fnGauge) IdleMarkerModTime() (time.Time, bool) {
	return g.cfg.IdleMarkerModTimeFn(g.cfg.ProjectDir, g.cfg.AgentName)
}
func (g fnGauge) LastAssistantTurn(sid string) (time.Time, bool) {
	if sid == "" {
		return time.Time{}, false
	}
	return g.cfg.recentTurnFn()(g.cfg.resolvedTranscriptDir(), sid, "assistant")
}
func (g fnGauge) LastUserTurn(sid string) (time.Time, bool) {
	if sid == "" {
		return time.Time{}, false
	}
	return g.cfg.recentTurnFn()(g.cfg.resolvedTranscriptDir(), sid, "user")
}

type fnHandoff struct{ cfg *CyclerConfig }

func (h fnHandoff) HandoffPath() string {
	return h.cfg.HandoffFilePath(h.cfg.ProjectDir, h.cfg.AgentName)
}
func (h fnHandoff) ReadHandoff() (string, error)      { return h.cfg.ReadHandoff(h.HandoffPath()) }
func (h fnHandoff) HandoffModTime() (time.Time, bool) { return h.cfg.HandoffModTimeFn(h.HandoffPath()) }
func (h fnHandoff) TruncateHandoff() error            { return h.cfg.TruncateHandoffFn(h.HandoffPath()) }
func (h fnHandoff) journalPath() string               { return journalFilePath(h.cfg.ProjectDir, h.cfg.AgentName) }
func (h fnHandoff) WriteJournal(j *CycleJournal) error {
	return h.cfg.WriteJournalFn(h.journalPath(), j)
}
func (h fnHandoff) ReadJournal() (*CycleJournal, error) { return h.cfg.ReadJournalFn(h.journalPath()) }

type fnRespawn struct {
	fn func(context.Context, string) error
}

func (r fnRespawn) ForceRestart(ctx context.Context, agent string) error { return r.fn(ctx, agent) }
