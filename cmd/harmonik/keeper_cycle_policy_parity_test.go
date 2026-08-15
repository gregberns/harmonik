package main

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func TestBuildKeeperConfigsProjectsResolvedCyclePolicy(t *testing.T) {
	resolved := ResolvedKeeperConfig{
		ActAbsTokens: 201_001, WarnAbsTokens: 181_002, ForceActAbsTokens: 226_003,
		ActPctCeil: .83, WarnPctCeil: .71, IdleRestartAbsTokens: 151_004,
		HandoffTimeout: 4 * time.Minute, ClearSettle: 4 * time.Second,
		CyclerPollInterval: 350 * time.Millisecond, ForceRetryInterval: 91 * time.Second,
		IdleRestartCooldown: 31 * time.Minute, MaxHandoffTimeouts: 4,
		HoldTTL: 46 * time.Minute, BootGrace: 7 * time.Minute, BootGraceSet: true,
		OperatorTurnLookback: 6 * time.Minute, PostAnswerGrace: 41 * time.Second,
	}
	cfg, _ := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName: "policy-agent", ProjectDir: "/tmp/policy-project",
		ResolvedTmux: "policy:0", ActPctRaw: 84,
	})
	p := keeper.CyclePolicyFromConfig(cfg)

	wantDurations := map[string]struct{ got, want time.Duration }{
		"handoff timeout":   {p.HandoffTimeout, resolved.HandoffTimeout},
		"clear settle":      {p.ClearSettle, resolved.ClearSettle},
		"poll interval":     {p.PollInterval, resolved.CyclerPollInterval},
		"force retry":       {p.ForceRetryInterval, resolved.ForceRetryInterval},
		"idle cooldown":     {p.IdleRestartCooldown, resolved.IdleRestartCooldown},
		"hold ttl":          {p.HoldTTL, resolved.HoldTTL},
		"boot grace":        {p.BootGracePeriod, resolved.BootGrace},
		"operator lookback": {p.OperatorTurnLookback, resolved.OperatorTurnLookback},
		"post-answer grace": {p.PostAnswerGrace, resolved.PostAnswerGrace},
	}
	for name, pair := range wantDurations {
		if pair.got != pair.want {
			t.Errorf("%s = %s, want %s", name, pair.got, pair.want)
		}
	}
	if p.ActAbsTokens != resolved.ActAbsTokens || p.WarnAbsTokens != resolved.WarnAbsTokens ||
		p.ForceActAbsTokens != resolved.ForceActAbsTokens || p.IdleRestartAbsTokens != resolved.IdleRestartAbsTokens {
		t.Errorf("absolute thresholds did not survive projection: %+v", p)
	}
	if p.ActPctCeil != resolved.ActPctCeil || p.WarnPctCeil != resolved.WarnPctCeil || p.ActPct != 84 {
		t.Errorf("percentage thresholds did not survive projection: %+v", p)
	}
	if p.MaxHandoffTimeouts != resolved.MaxHandoffTimeouts {
		t.Errorf("MaxHandoffTimeouts = %d, want %d", p.MaxHandoffTimeouts, resolved.MaxHandoffTimeouts)
	}
	if p.MaxBootGraceTotal != 2*resolved.BootGrace {
		t.Errorf("MaxBootGraceTotal = %s, want %s", p.MaxBootGraceTotal, 2*resolved.BootGrace)
	}
	if !p.HardBandCycleOnly {
		t.Fatal("production policy must reserve automatic cycle entry for the hard band")
	}
}

func TestBuildKeeperConfigsPreservesDisabledBootGrace(t *testing.T) {
	cfg, _ := buildKeeperConfigs(ResolvedKeeperConfig{BootGraceSet: true}, keeperBuildParams{})
	if got := keeper.CyclePolicyFromConfig(cfg).BootGracePeriod; got != 0 {
		t.Fatalf("BootGracePeriod = %s, want disabled zero", got)
	}
}
