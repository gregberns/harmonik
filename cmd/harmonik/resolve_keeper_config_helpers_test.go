package main

import (
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// resolve_keeper_config_helpers_test.go — shared test fixtures for the resolver.
//
// Operator-philosophy change: ResolveKeeperConfig imposes NO runtime defaults — an
// unset required value aggregates into a *KeeperConfigMissingError. So almost every
// resolver test needs a COMPLETE, valid daemon.KeeperConfig as its baseline, then
// overrides the one field under test. completeTestKeeperConfig is that baseline; it
// uses the keeper.Default* consts as suggested values (the SAME numbers the
// `keeper config --example` template ships) and sets every Present flag so the
// missing-value gate passes.

// completeTestKeeperConfig returns a daemon.KeeperConfig with EVERY operator-required
// keeper value set to its suggested (keeper.Default*) value and the corresponding
// Present flag true. Resolving it (with empty flags) yields zero missing-value errors
// and a valid band. Tests start from this and override the field(s) under test.
func completeTestKeeperConfig() daemon.KeeperConfig {
	cfg := daemon.KeeperConfig{
		// ── thresholds ──
		WarnAbsTokens:      keeper.DefaultWarnAbsTokens,
		ActAbsTokens:       keeper.DefaultActAbsTokens,
		ForceActAbsTokens:  keeper.DefaultActAbsTokens + keeper.DefaultForceActAbsOffset,
		IdleFloorAbsTokens: keeper.DefaultIdleRestartAbsTokens,
		WarnPctCeil:        keeper.DefaultWarnPctCeil,
		ActPctCeil:         keeper.DefaultActPctCeil,
		// ── hard_ceiling ──
		HardCeilingMode:      "alarm",
		HardCeilingAbsTokens: keeper.HardCeilingAbsTokens,
		// ── timings ──
		PollInterval:       keeper.DefaultPollInterval,
		CyclerPollInterval: keeper.DefaultCyclerPollInterval,
		IdleQuiesce:        keeper.DefaultIdleQuiesce,
		Staleness:          keeper.DefaultStaleness,
		HandoffTimeout:     keeper.DefaultHandoffTimeout,
		ClearSettle:        keeper.DefaultClearSettle,
		BootGrace:          5 * time.Minute,
		// ── cadence ──
		WarnCooldown:               keeper.DefaultWarnCooldown,
		NoGaugeBackoff:             keeper.DefaultNoGaugeBackoff,
		RespawnGrace:               keeper.DefaultRespawnGrace,
		RespawnCooldown:            keeper.DefaultRespawnCooldown,
		LiveRecoverGrace:           keeper.DefaultLiveRecoverGrace,
		LiveRecoverCooldown:        keeper.DefaultLiveRecoverCooldown,
		ForceRetryInterval:         keeper.DefaultForceRetryInterval,
		IdleRestartCooldown:        keeper.DefaultIdleRestartCooldown,
		CadenceHardCeilingCooldown: keeper.DefaultHardCeilingCooldown,
		BlindKeeperThreshold:       keeper.DefaultBlindKeeperThreshold,
		HoldTTL:                    keeper.DefaultHoldTTL,
		// ── budgets ──
		HeartbeatMaxMisses: keeper.DefaultMaxHeartbeatMisses,
		MaxHandoffTimeouts: keeper.DefaultMaxHandoffTimeouts,
	}
	cfg.Present = daemon.KeeperConfigPresence{
		WarnAbsTokens:        true,
		ActAbsTokens:         true,
		ForceActAbsTokens:    true,
		IdleFloorAbsTokens:   true,
		WarnPctCeil:          true,
		ActPctCeil:           true,
		HardCeilingMode:      true,
		HardCeilingAbsTokens: true,
		PollInterval:         true,
		CyclerPollInterval:   true,
		IdleQuiesce:          true,
		Staleness:            true,
		HandoffTimeout:       true,
		ClearSettle:          true,
		BootGrace:            true,
		WarnCooldown:         true,
		NoGaugeBackoff:       true,
		RespawnGrace:         true,
		RespawnCooldown:      true,
		LiveRecoverGrace:     true,
		LiveRecoverCooldown:  true,
		ForceRetryInterval:   true,
		IdleRestartCooldown:  true,
		HardCeilingCooldown:  true,
		BlindKeeperThreshold: true,
		HoldTTL:              true,
		HeartbeatMaxMisses:   true,
		MaxHandoffTimeouts:   true,
	}
	return cfg
}
