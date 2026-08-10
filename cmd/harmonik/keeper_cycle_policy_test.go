package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func TestBuildCyclePolicyMatchesResolvedCommandConfig(t *testing.T) {
	resolved := ResolvedKeeperConfig{
		ActAbsTokens: 211_001, WarnAbsTokens: 199_001, ForceActAbsTokens: 238_001,
		ActPctCeil: .81, WarnPctCeil: .61,
		HandoffTimeout: 71 * time.Second, ClearSettle: 13 * time.Second,
		CyclerPollInterval: 311 * time.Millisecond, ForceRetryInterval: 83 * time.Second,
		IdleRestartAbsTokens: 144_001, IdleRestartCooldown: 17 * time.Minute,
		MaxHandoffTimeouts: 0, HoldTTL: 29 * time.Minute,
		BootGrace: 0, BootGraceSet: true,
		OperatorTurnLookback: 0, PostAnswerGrace: 0,
	}
	params := keeperBuildParams{
		AgentName: "agent-a", ProjectDir: "/project-a", ResolvedTmux: "pane-a",
		WarnPctRaw: 72, ActPctRaw: 86,
	}

	policy, env := buildCyclePolicy(resolved, params)
	wantPolicy := keeper.CyclePolicy{
		ActAbsTokens: 211_001, ActPctCeil: .81,
		WarnAbsTokens: 199_001, WarnPctCeil: .61,
		ForceActAbsTokens: 238_001,
		ActPct:            86, HandoffTimeout: 71 * time.Second, ClearSettle: 13 * time.Second,
		PollInterval: 311 * time.Millisecond, ForceRetryInterval: 83 * time.Second,
		IdleRestartAbsTokens: 144_001, IdleRestartCooldown: 17 * time.Minute,
		MaxHandoffTimeouts: 0, HoldTTL: 29 * time.Minute,
		BootGracePeriod: 0, OperatorTurnLookback: 0, PostAnswerGrace: 0,
	}
	if !reflect.DeepEqual(policy, wantPolicy) {
		t.Fatalf("policy mismatch:\n got: %#v\nwant: %#v", policy, wantPolicy)
	}
	wantEnv := keeper.CycleEnv{AgentName: "agent-a", ProjectDir: "/project-a", TmuxTarget: "pane-a"}
	if env != wantEnv {
		t.Fatalf("env = %#v; want %#v", env, wantEnv)
	}

	legacy, _ := buildKeeperConfigs(resolved, params)
	converted := keeper.LegacyCyclerConfig(policy, env)
	if legacy.MaxHandoffTimeouts != converted.MaxHandoffTimeouts ||
		legacy.BootGracePeriod != converted.BootGracePeriod ||
		legacy.OperatorTurnLookback != converted.OperatorTurnLookback ||
		legacy.PostAnswerGrace != converted.PostAnswerGrace {
		t.Fatalf("legacy command conversion changed sentinel values: %#v vs %#v", legacy, converted)
	}
}
