//go:build integration

package keeper

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

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
	deps.Context = configTestContextWithGauge{ContextStore: configTestContext{ContextStore: deps.Context}, read: func() (*CtxFile, time.Time, error) {
		return o.gauge(cfg.ProjectDir, cfg.AgentName)
	}}
	deps.Activity = configTestActivity{ActivityProbe: deps.Activity}
	deps.CycleIDs = cycleIDFunc(o.cycleIDs)
	deps.Handoff = configTestHandoff{HandoffDocument: deps.Handoff, cfg: cfg, overrides: o}
	deps.Journal = configTestJournal{CycleJournalStore: deps.Journal, cfg: cfg, write: o.journal}
	cycler, err := NewCyclerWithDeps(CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), deps)
	if err != nil {
		return nil
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
	info, err := os.Stat(h.Path())
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
func (h configTestHandoff) ScrubNonce() error { return h.overrides.scrub(h.Path()) }

type configTestJournal struct {
	CycleJournalStore
	cfg   CyclerConfig
	write func(string, *CycleJournal) error
}

func (j configTestJournal) Write(v *CycleJournal) error {
	path := filepath.Join(j.cfg.ProjectDir, ".harmonik", "keeper", j.cfg.AgentName+".cycle.json")
	return j.write(path, v)
}
