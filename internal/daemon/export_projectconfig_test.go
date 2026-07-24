package daemon

// export_projectconfig_test.go — projectconfig.go test-seam exports.
//
// Split out of export_test.go (RT19.9, P2 E5 export_test.go split) so the
// contiguous projectconfig type-alias run (ProjectConfig / DaemonConfig /
// KeeperConfig / the raw keeper-config sub-structs / the malformed-config
// sentinels / LoadProjectConfig / KeeperBlockAbsent) and the ExportedProjectCfgOf
// accessor live in one topic file. Same package (daemon), so every daemon_test
// caller resolves daemon.ExportedX byte-identically after the move. Coordinate
// with the internal/projectconfig lift (PC).
//
// Bead: hk-ecrxy.

// ExportedProjectConfig is a type alias for ProjectConfig so tests in package
// daemon_test can reference the type directly.
//
// Bead ref: hk-bfvk7.
type (
	ExportedProjectConfig = ProjectConfig
	// ExportedErrMalformedConfigYAML is a type alias so tests can use errors.As.
	//
	// Bead ref: hk-bfvk7.
	ExportedErrMalformedConfigYAML = ErrMalformedConfigYAML
	// ExportedErrUnsupportedConfigVersion is a type alias so tests can use errors.As.
	//
	// Bead ref: hk-bfvk7.
	ExportedErrUnsupportedConfigVersion = ErrUnsupportedConfigVersion
	// ExportedErrUnknownConfigKey is a type alias so tests can use errors.As to
	// assert that an unknown key under keeper: is rejected (hk-9f3f).
	//
	// Bead ref: hk-9f3f.
	ExportedErrUnknownConfigKey = ErrUnknownConfigKey
	// ExportedErrWorkflowModeFloorViolation is a type alias so tests can use errors.As.
	//
	// Bead ref: hk-rcp7.
	ExportedErrWorkflowModeFloorViolation = ErrWorkflowModeFloorViolation
	// ExportedDaemonConfig is a type alias for DaemonConfig so tests in package
	// daemon_test can reference the type directly without importing internal types.
	//
	// Bead ref: hk-rcp7.
	ExportedDaemonConfig = DaemonConfig
	// ExportedKeeperConfig is a type alias for KeeperConfig so tests in package
	// daemon_test can reference the type directly without importing internal types.
	//
	// Bead ref: hk-lhu2.
	ExportedKeeperConfig = KeeperConfig
	// ExportedWatchdogConfig is a type alias for WatchdogConfig so tests in
	// package daemon_test can read parsed watchdog config fields directly.
	//
	// Bead ref: hk-sbitr.
	ExportedWatchdogConfig = WatchdogConfig
	// ExportedSuperviseConfig is a type alias for SuperviseConfig so tests in
	// package daemon_test can read parsed supervise config fields directly.
	ExportedSuperviseConfig = SuperviseConfig
)

// ExportedLoadProjectConfig exposes LoadProjectConfig for tests in package daemon_test.
//
// Bead ref: hk-bfvk7.
func ExportedLoadProjectConfig(repoRoot string) (ProjectConfig, error) {
	return LoadProjectConfig(repoRoot)
}

// ExportedRawKeeperConfig is a type alias for rawKeeperConfig so tests in
// package daemon_test can construct keeper-block fixtures directly.
//
// Bead ref: hk-exg3.
type (
	ExportedRawKeeperConfig = rawKeeperConfig
	// ExportedRawKeeperContextThresholds is a type alias for the nested
	// context_thresholds sub-struct so tests can set a single field.
	//
	// Bead ref: hk-exg3.
	ExportedRawKeeperContextThresholds = rawKeeperContextThresholds
	// ExportedRawKeeperWarnMessages is a type alias for the nested warn_messages
	// sub-struct so tests can set a single field.
	//
	// Bead ref: hk-exg3.
	ExportedRawKeeperWarnMessages = rawKeeperWarnMessages
	// ExportedRawKeeperHardCeiling, ...Timings, ...Cadence, ...Budgets, and
	// ...SelfService are type aliases for the keeper sub-blocks added in hk-9kgf so
	// tests can construct single-field keeper fixtures for the keeperBlockAbsent
	// per-field coverage (hk-exg3 invariant).
	//
	// Bead ref: hk-9kgf.
	ExportedRawKeeperHardCeiling = rawKeeperHardCeiling
	// ExportedRawKeeperTimings — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
	ExportedRawKeeperTimings = rawKeeperTimings
	// ExportedRawKeeperCadence — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
	ExportedRawKeeperCadence = rawKeeperCadence
	// ExportedRawKeeperBudgets — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
	ExportedRawKeeperBudgets = rawKeeperBudgets
	// ExportedRawKeeperSelfService — see ExportedRawKeeperHardCeiling. Bead ref: hk-9kgf.
	ExportedRawKeeperSelfService = rawKeeperSelfService
)

// ExportedKeeperBlockAbsent exposes keeperBlockAbsent for tests in package
// daemon_test (hk-exg3): the explicit field-by-field zero check that replaces
// the `== (rawKeeperConfig{})` empty-block sentinel.
//
// Bead ref: hk-exg3.
func ExportedKeeperBlockAbsent(raw ExportedRawKeeperConfig) bool {
	return keeperBlockAbsent(raw)
}

// ExportedProjectCfgOf returns the projectCfg field from deps for inspection.
//
// Bead ref: hk-bfvk7.
func ExportedProjectCfgOf(deps workLoopDeps) ProjectConfig {
	return deps.projectCfg
}
