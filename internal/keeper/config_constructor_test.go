package keeper

func mustNewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	cycler, err := NewCyclerWithDeps(
		CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), CycleDepsFromConfig(cfg, emitter),
	)
	if err != nil {
		panic(err)
	}
	return cycler
}
