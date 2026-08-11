package keeper

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

func mustNewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	deps := CycleDepsFromConfig(cfg, emitter)
	deps.Operator = operatorProbeFunc(func(string) bool { return false })
	deps.Managed = boolProbe(func() bool { return true })
	deps.Dispatch = boolProbe(func() bool { return false })
	deps.Idle = boolProbe(func() bool { return true })
	deps.Pane = configTestPane{PaneWriter: deps.Pane}
	deps.Context = configTestContext{ContextStore: deps.Context}
	deps.Activity = configTestActivity{ActivityProbe: deps.Activity}
	cycler, err := NewCyclerWithDeps(
		CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), deps,
	)
	if err != nil {
		panic(err)
	}
	return cycler
}

type configTestOverrides struct {
	cycleIDs func() string
	path     func(string, string) string
	read     func(string) (string, error)
	scrub    func(string) error
	inject   func(context.Context, string, string) error
	gauge    func(string, string) (*CtxFile, time.Time, error)
	journal  func(string, *CycleJournal) error
}

func mustNewCyclerWithConfigOverrides(cfg CyclerConfig, emitter Emitter, o configTestOverrides) *Cycler {
	deps := CycleDepsFromConfig(cfg, emitter)
	deps.Operator = operatorProbeFunc(func(string) bool { return false })
	deps.Managed = boolProbe(func() bool { return true })
	deps.Dispatch = boolProbe(func() bool { return false })
	deps.Idle = boolProbe(func() bool { return true })
	deps.Pane = configTestPaneWithInject{PaneWriter: configTestPane{PaneWriter: deps.Pane}, inject: o.inject}
	deps.Context = configTestContextWithGauge{ContextStore: configTestContext{ContextStore: deps.Context}, read: func() (*CtxFile, time.Time, error) { return o.gauge(cfg.ProjectDir, cfg.AgentName) }}
	deps.Activity = configTestActivity{ActivityProbe: deps.Activity}
	deps.CycleIDs = cycleIDFunc(o.cycleIDs)
	deps.Handoff = configTestHandoff{HandoffDocument: deps.Handoff, cfg: cfg, overrides: o}
	deps.Journal = configTestJournal{CycleJournalStore: deps.Journal, cfg: cfg, write: o.journal}
	cycler, err := NewCyclerWithDeps(CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), deps)
	if err != nil {
		panic(err)
	}
	return cycler
}

type configTestPaneWithInject struct {
	PaneWriter
	inject func(context.Context, string, string) error
}

func (p configTestPaneWithInject) Inject(ctx context.Context, target, text string) error {
	return p.inject(ctx, target, text)
}

type configTestContextWithGauge struct {
	ContextStore
	read func() (*CtxFile, time.Time, error)
}

func (c configTestContextWithGauge) ReadGauge() (*CtxFile, time.Time, error) { return c.read() }

type configTestHandoff struct {
	HandoffDocument
	cfg       CyclerConfig
	overrides configTestOverrides
}

func (h configTestHandoff) Path() string          { return h.overrides.path(h.cfg.ProjectDir, h.cfg.AgentName) }
func (h configTestHandoff) Read() (string, error) { return h.overrides.read(h.Path()) }
func (h configTestHandoff) ModTime() (time.Time, bool) {
	i, e := os.Stat(h.Path())
	if e != nil {
		return time.Time{}, false
	}
	return i.ModTime(), true
}
func (h configTestHandoff) ScrubNonce() error { return h.overrides.scrub(h.Path()) }

type configTestJournal struct {
	CycleJournalStore
	cfg   CyclerConfig
	write func(string, *CycleJournal) error
}

func (j configTestJournal) Write(v *CycleJournal) error {
	return j.write(filepath.Join(j.cfg.ProjectDir, ".harmonik", "keeper", j.cfg.AgentName+".cycle.json"), v)
}

type configTestPane struct{ PaneWriter }

func (configTestPane) SetEnv(context.Context, string, string, string) error { return nil }

type configTestContext struct{ ContextStore }

func (configTestContext) SetManagedSession(string) error { return nil }

type configTestActivity struct{ ActivityProbe }

func (configTestActivity) IdleMarkerModTime() (time.Time, bool) { return time.Now(), true }
