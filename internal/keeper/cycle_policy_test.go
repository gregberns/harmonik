package keeper

import (
	"reflect"
	"testing"
	"time"
)

func TestDefaultCyclePolicyMatchesResolvedCyclerConfig(t *testing.T) {
	var cfg CyclerConfig
	cfg.applyDefaults()
	want := policyProjectionWithoutResolving(cfg)
	if got := DefaultCyclePolicy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultCyclePolicy drifted from CyclerConfig defaults:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestCyclePolicyFromConfigPreservesOverridesAndSentinels(t *testing.T) {
	cfg := CyclerConfig{
		ActAbsTokens:         123_000,
		ActPctCeil:           .61,
		BootGracePeriod:      7 * time.Minute,
		OperatorTurnLookback: 9 * time.Minute,
		PostAnswerGrace:      17 * time.Second,
	}
	got := CyclePolicyFromConfig(cfg)
	if got.ActAbsTokens != cfg.ActAbsTokens || got.ActPctCeil != cfg.ActPctCeil {
		t.Fatalf("explicit thresholds lost: %+v", got)
	}
	if got.ForceActAbsTokens != cfg.ActAbsTokens+DefaultForceActAbsOffset {
		t.Fatalf("derived force threshold = %d", got.ForceActAbsTokens)
	}
	if got.BootGracePeriod != cfg.BootGracePeriod || got.MaxBootGraceTotal != 2*cfg.BootGracePeriod {
		t.Fatalf("boot grace derivation lost: %+v", got)
	}
	if got.OperatorTurnLookback != cfg.OperatorTurnLookback || got.PostAnswerGrace != cfg.PostAnswerGrace {
		t.Fatalf("transcript policy lost: %+v", got)
	}

	disabled := DefaultCyclePolicy()
	if disabled.BootGracePeriod != 0 || disabled.MaxBootGraceTotal != 0 ||
		disabled.OperatorTurnLookback != 0 || disabled.PostAnswerGrace != 0 {
		t.Fatalf("library-disabled sentinels changed: %+v", disabled)
	}
}

func TestCycleEnvFromConfigContainsOnlyIdentity(t *testing.T) {
	cfg := CyclerConfig{
		AgentName: "alpha", ProjectDir: "/project", TmuxTarget: "pane",
		TranscriptDir: "/transcripts", ActPct: 73,
	}
	want := CycleEnv{"alpha", "/project", "pane", "/transcripts"}
	if got := CycleEnvFromConfig(cfg); got != want {
		t.Fatalf("CycleEnvFromConfig() = %+v, want %+v", got, want)
	}
}

func policyProjectionWithoutResolving(cfg CyclerConfig) CyclePolicy {
	return CyclePolicy{
		ActAbsTokens: cfg.ActAbsTokens, ActPctCeil: cfg.ActPctCeil,
		WarnAbsTokens: cfg.WarnAbsTokens, WarnPctCeil: cfg.WarnPctCeil,
		ForceActAbsTokens: cfg.ForceActAbsTokens, ForceActPctCeil: cfg.ForceActPctCeil,
		ActPct: cfg.ActPct, WarnPct: cfg.WarnPct, ForceActPct: cfg.ForceActPct,
		HandoffTimeout: cfg.HandoffTimeout, ClearSettle: cfg.ClearSettle,
		PollInterval: cfg.PollInterval, ClearConfirmBackstop: cfg.ClearConfirmBackstop,
		ClearConfirmRetries: cfg.ClearConfirmRetries, ModelDoneTimeout: cfg.ModelDoneTimeout,
		ForceRetryInterval: cfg.ForceRetryInterval, IdleRestartAbsTokens: cfg.IdleRestartAbsTokens,
		IdleRestartCooldown: cfg.IdleRestartCooldown, BootGracePeriod: cfg.BootGracePeriod,
		MaxBootGraceTotal: cfg.MaxBootGraceTotal, MaxHandoffTimeouts: cfg.MaxHandoffTimeouts,
		HoldTTL: cfg.HoldTTL, OperatorTurnLookback: cfg.OperatorTurnLookback,
		PostAnswerGrace: cfg.PostAnswerGrace,
	}
}
