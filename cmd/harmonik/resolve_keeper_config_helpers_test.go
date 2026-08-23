package main

import (
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

func completeTestKeeperConfig() projectconfig.KeeperConfig {
	cfg := projectconfig.KeeperConfig{
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
		// hk-74iyd: conversation-aware ACT suppression.
		OperatorTurnLookback: 5 * time.Minute,
		PostAnswerGrace:      30 * time.Second,
		// ── budgets ──
		HeartbeatMaxMisses: keeper.DefaultMaxHeartbeatMisses,
		MaxHandoffTimeouts: keeper.DefaultMaxHandoffTimeouts,
	}
	cfg.Present = projectconfig.KeeperConfigPresence{
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
		OperatorTurnLookback: true, // hk-74iyd
		PostAnswerGrace:      true, // hk-74iyd
		HeartbeatMaxMisses:   true,
		MaxHandoffTimeouts:   true,
	}
	return cfg
}
