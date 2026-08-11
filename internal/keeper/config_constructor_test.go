package keeper

func mustNewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	deps := CycleDepsFromConfig(cfg, emitter)
	deps.Operator = operatorProbeFunc(func(string) bool { return false })
	cycler, err := NewCyclerWithDeps(
		CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), deps,
	)
	if err != nil {
		panic(err)
	}
	return cycler
}
