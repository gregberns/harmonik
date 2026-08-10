package keeper

import "time"

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
