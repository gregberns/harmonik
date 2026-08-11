package keeper_test

import "github.com/gregberns/harmonik/internal/keeper"

func mustNewCycler(cfg keeper.CyclerConfig, emitter keeper.Emitter) *keeper.Cycler {
	cycler, err := keeper.NewCyclerWithDeps(
		keeper.CyclePolicyFromConfig(cfg),
		keeper.CycleEnvFromConfig(cfg),
		keeper.CycleDepsFromConfig(cfg, emitter),
	)
	if err != nil {
		panic(err)
	}
	return cycler
}
