package keeper

// NewCycler keeps the old construction surface while package tests migrate.
// Production does not compile this adapter.
func NewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	cycler, err := NewCyclerWithDeps(
		CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), CycleDepsFromConfig(cfg, emitter),
	)
	if err != nil {
		panic(err)
	}
	return cycler
}
