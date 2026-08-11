package keeper_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func mustNewCycler(cfg keeper.CyclerConfig, emitter keeper.Emitter) *keeper.Cycler {
	return mustNewCyclerWithDeps(cfg, emitter, nil)
}

func mustNewCyclerWithIdle(cfg keeper.CyclerConfig, emitter keeper.Emitter, idle bool) *keeper.Cycler {
	return mustNewCyclerWithDeps(cfg, emitter, func(deps *keeper.CycleDeps) {
		deps.Idle = testIdleProbe(idle)
	})
}

type testCycleOverrides struct {
	CycleIDs     func() string
	HandoffPath  func(string, string) string
	Inject       func(context.Context, string, string) error
	Gauge        func(string, string) (*keeper.CtxFile, time.Time, error)
	HandoffRead  func(string) (string, error)
	HandoffScrub func(string) error
	JournalWrite func(string, *keeper.CycleJournal) error
}

func mustNewCyclerWithOverrides(cfg keeper.CyclerConfig, emitter keeper.Emitter, o testCycleOverrides) *keeper.Cycler {
	return mustNewCyclerWithOverridesAndDeps(cfg, emitter, o, nil)
}

func mustNewCyclerWithOverridesAndIdle(cfg keeper.CyclerConfig, emitter keeper.Emitter, o testCycleOverrides, idle bool) *keeper.Cycler {
	return mustNewCyclerWithOverridesAndDeps(cfg, emitter, o, func(deps *keeper.CycleDeps) {
		deps.Idle = testIdleProbe(idle)
	})
}

func mustNewCyclerWithOverridesAndDeps(cfg keeper.CyclerConfig, emitter keeper.Emitter, o testCycleOverrides, modify func(*keeper.CycleDeps)) *keeper.Cycler {
	return mustNewCyclerWithDeps(cfg, emitter, func(deps *keeper.CycleDeps) {
		applyTestCycleOverrides(deps, cfg, o)
		if modify != nil {
			modify(deps)
		}
	})
}

func applyTestCycleOverrides(deps *keeper.CycleDeps, cfg keeper.CyclerConfig, o testCycleOverrides) {
	if o.CycleIDs != nil {
		deps.CycleIDs = testCycleIDFunc(o.CycleIDs)
	}
	if o.Inject != nil {
		deps.Pane = testPaneWithInject{PaneWriter: deps.Pane, inject: o.Inject}
	}
	if o.Gauge != nil {
		deps.Context = testContextWithGauge{ContextStore: deps.Context, read: func() (*keeper.CtxFile, time.Time, error) {
			return o.Gauge(cfg.ProjectDir, cfg.AgentName)
		}}
	}
	if o.HandoffPath != nil || o.HandoffRead != nil || o.HandoffScrub != nil {
		deps.Handoff = testHandoffOverrides{HandoffDocument: deps.Handoff, cfg: cfg, overrides: o}
	}
	if o.JournalWrite != nil {
		deps.Journal = testJournalWithWrite{CycleJournalStore: deps.Journal, write: func(j *keeper.CycleJournal) error {
			path := filepath.Join(cfg.ProjectDir, ".harmonik", "keeper", cfg.AgentName+".cycle.json")
			return o.JournalWrite(path, j)
		}}
	}
}

type testCycleIDFunc func() string

func (f testCycleIDFunc) Next() string { return f() }

type testPaneWithInject struct {
	keeper.PaneWriter
	inject func(context.Context, string, string) error
}

func (p testPaneWithInject) Inject(ctx context.Context, target, text string) error {
	return p.inject(ctx, target, text)
}

type testContextWithGauge struct {
	keeper.ContextStore
	read func() (*keeper.CtxFile, time.Time, error)
}

func (c testContextWithGauge) ReadGauge() (*keeper.CtxFile, time.Time, error) { return c.read() }

type testHandoffOverrides struct {
	keeper.HandoffDocument
	cfg       keeper.CyclerConfig
	overrides testCycleOverrides
}

func (h testHandoffOverrides) Path() string {
	if h.overrides.HandoffPath != nil {
		return h.overrides.HandoffPath(h.cfg.ProjectDir, h.cfg.AgentName)
	}
	return h.HandoffDocument.Path()
}
func (h testHandoffOverrides) Read() (string, error) {
	if h.overrides.HandoffRead != nil {
		return h.overrides.HandoffRead(h.Path())
	}
	return h.HandoffDocument.Read()
}
func (h testHandoffOverrides) ModTime() (time.Time, bool) {
	info, err := os.Stat(h.Path())
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}
func (h testHandoffOverrides) ScrubNonce() error {
	if h.overrides.HandoffScrub != nil {
		return h.overrides.HandoffScrub(h.Path())
	}
	return h.HandoffDocument.ScrubNonce()
}

type testJournalWithWrite struct {
	keeper.CycleJournalStore
	write func(*keeper.CycleJournal) error
}

func (j testJournalWithWrite) Write(v *keeper.CycleJournal) error { return j.write(v) }

func mustNewCyclerWithDeps(
	cfg keeper.CyclerConfig,
	emitter keeper.Emitter,
	modify func(*keeper.CycleDeps),
) *keeper.Cycler {
	deps := keeper.CycleDepsFromConfig(cfg, emitter)
	deps.Operator = testOperatorProbe(func(string) bool { return false })
	deps.Managed = testManagedProbe(true)
	deps.Dispatch = testDispatchProbe(false)
	deps.Idle = testIdleProbe(true)
	deps.Pane = testPaneWithEnv{PaneWriter: deps.Pane}
	deps.Context = testContextWithManaged{ContextStore: deps.Context}
	deps.Activity = testActivityWithIdle{ActivityProbe: deps.Activity, idleMarker: func() (time.Time, bool) { return time.Now(), true }}
	if modify != nil {
		modify(&deps)
	}
	cycler, err := keeper.NewCyclerWithDeps(
		keeper.CyclePolicyFromConfig(cfg),
		keeper.CycleEnvFromConfig(cfg),
		deps,
	)
	if err != nil {
		panic(err)
	}
	return cycler
}

type testPaneWithEscape struct {
	keeper.PaneWriter
	sendEscape func(context.Context, string) error
}

type testPaneWithEnv struct {
	keeper.PaneWriter
	setEnv func(context.Context, string, string, string) error
}

func (p testPaneWithEnv) SetEnv(ctx context.Context, target, key, value string) error {
	if p.setEnv == nil {
		return nil
	}
	return p.setEnv(ctx, target, key, value)
}

func (p testPaneWithEscape) SendEscape(ctx context.Context, target string) error {
	return p.sendEscape(ctx, target)
}

type testRespawnFunc func(context.Context, string) error

func (f testRespawnFunc) ForceRestart(ctx context.Context, agent string) error {
	return f(ctx, agent)
}

type testSleepProbe func(string) bool

func (f testSleepProbe) Sleeping(sid string) bool { return f(sid) }

type testHoldProbe func() bool

func (f testHoldProbe) Held() bool { return f() }

type testManagedProbe bool

func (p testManagedProbe) IsManaged() bool { return bool(p) }

type testDispatchProbe bool

func (p testDispatchProbe) HoldingDispatch() bool { return bool(p) }

type testDispatchProbeFunc func() bool

func (f testDispatchProbeFunc) HoldingDispatch() bool { return f() }

type testIdleProbe bool

func (p testIdleProbe) CrispIdle() bool { return bool(p) }

type testOperatorProbe func(string) bool

func (f testOperatorProbe) Attached(target string) bool { return f(target) }

type testHandoffWithModTime struct {
	keeper.HandoffDocument
	modTime func(string) (time.Time, bool)
}

type testJournalStore struct {
	write func(*keeper.CycleJournal) error
	read  func() (*keeper.CycleJournal, error)
}

func (s testJournalStore) Write(j *keeper.CycleJournal) error  { return s.write(j) }
func (s testJournalStore) Read() (*keeper.CycleJournal, error) { return s.read() }

type testContextWithClear struct {
	keeper.ContextStore
	clear func() error
}

type testContextWithManaged struct {
	keeper.ContextStore
	setManaged func(string) error
}

func (s testContextWithManaged) SetManagedSession(sid string) error {
	if s.setManaged == nil {
		return nil
	}
	return s.setManaged(sid)
}

func (s testContextWithClear) ClearPrecompactTrigger() error { return s.clear() }

func (h testHandoffWithModTime) ModTime() (time.Time, bool) {
	return h.modTime(h.Path())
}

type testActivityWithTurns struct {
	keeper.ActivityProbe
	dir  string
	turn func(string, string, string) (time.Time, bool)
}

type testActivityWithIdle struct {
	keeper.ActivityProbe
	idleMarker func() (time.Time, bool)
}

func (a testActivityWithIdle) IdleMarkerModTime() (time.Time, bool) {
	return a.idleMarker()
}

func (a testActivityWithTurns) LastUserTurn(sid string) (time.Time, bool) {
	return a.turn(a.dir, sid, "user")
}

func (a testActivityWithTurns) LastAssistantTurn(sid string) (time.Time, bool) {
	return a.turn(a.dir, sid, "assistant")
}
