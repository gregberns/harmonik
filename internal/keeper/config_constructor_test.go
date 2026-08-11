package keeper

import (
	"context"
	"time"
)

func mustNewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	deps := CycleDepsFromConfig(cfg, emitter)
	deps.Operator = operatorProbeFunc(func(string) bool { return false })
	deps.Managed = boolProbe(func() bool { return true })
	deps.Dispatch = boolProbe(func() bool { return false })
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

type configTestPane struct{ PaneWriter }

func (configTestPane) SetEnv(context.Context, string, string, string) error { return nil }

type configTestContext struct{ ContextStore }

func (configTestContext) SetManagedSession(string) error { return nil }

type configTestActivity struct{ ActivityProbe }

func (configTestActivity) IdleMarkerModTime() (time.Time, bool) { return time.Now(), true }
