package keeper

import "context"

func mustNewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	deps := CycleDepsFromConfig(cfg, emitter)
	deps.Operator = operatorProbeFunc(func(string) bool { return false })
	deps.Managed = boolProbe(func() bool { return true })
	deps.Pane = configTestPane{PaneWriter: deps.Pane}
	deps.Context = configTestContext{ContextStore: deps.Context}
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
