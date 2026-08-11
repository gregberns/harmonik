package keeper_test

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func mustNewCycler(cfg keeper.CyclerConfig, emitter keeper.Emitter) *keeper.Cycler {
	return mustNewCyclerWithDeps(cfg, emitter, nil)
}

func mustNewCyclerWithDeps(
	cfg keeper.CyclerConfig,
	emitter keeper.Emitter,
	modify func(*keeper.CycleDeps),
) *keeper.Cycler {
	deps := keeper.CycleDepsFromConfig(cfg, emitter)
	deps.Operator = testOperatorProbe(func(string) bool { return false })
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

func (s testContextWithClear) ClearPrecompactTrigger() error { return s.clear() }

func (h testHandoffWithModTime) ModTime() (time.Time, bool) {
	return h.modTime(h.Path())
}

type testActivityWithTurns struct {
	keeper.ActivityProbe
	dir  string
	turn func(string, string, string) (time.Time, bool)
}

func (a testActivityWithTurns) LastUserTurn(sid string) (time.Time, bool) {
	return a.turn(a.dir, sid, "user")
}

func (a testActivityWithTurns) LastAssistantTurn(sid string) (time.Time, bool) {
	return a.turn(a.dir, sid, "assistant")
}
