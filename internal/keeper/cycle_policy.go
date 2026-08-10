package keeper

import "time"

// CyclePolicy contains the values that control cycle decisions. It contains no
// resource identity or effect dependencies.
type CyclePolicy struct {
	ActAbsTokens         int64
	ActPctCeil           float64
	WarnAbsTokens        int64
	WarnPctCeil          float64
	ForceActAbsTokens    int64
	ForceActPctCeil      float64
	ActPct               float64
	WarnPct              float64
	ForceActPct          float64
	HandoffTimeout       time.Duration
	ClearSettle          time.Duration
	PollInterval         time.Duration
	ClearConfirmBackstop time.Duration
	ClearConfirmRetries  int
	ModelDoneTimeout     time.Duration
	ForceRetryInterval   time.Duration
	IdleRestartAbsTokens int64
	IdleRestartCooldown  time.Duration
	BootGracePeriod      time.Duration
	MaxBootGraceTotal    time.Duration
	MaxHandoffTimeouts   int
	HoldTTL              time.Duration
	OperatorTurnLookback time.Duration
	PostAnswerGrace      time.Duration
	hasRespawn           bool
}

// CycleEnv identifies the resources used by one keeper cycle.
type CycleEnv struct {
	AgentName     string
	ProjectDir    string
	TmuxTarget    string
	TranscriptDir string
}

// DefaultCyclePolicy returns the library policy produced by a zero-valued
// CyclerConfig. Boot grace and transcript gates stay disabled.
func DefaultCyclePolicy() CyclePolicy {
	return CyclePolicy{
		ActAbsTokens: DefaultActAbsTokens, ActPctCeil: DefaultActPctCeil,
		WarnAbsTokens: DefaultWarnAbsTokens, WarnPctCeil: DefaultWarnPctCeil,
		ForceActAbsTokens: DefaultActAbsTokens + DefaultForceActAbsOffset,
		ForceActPctCeil:   defaultActPctCeil + defaultForceActPctCeilOffset,
		ActPct:            defaultActPct, WarnPct: defaultWarnPct,
		ForceActPct:    defaultActPct + defaultForceActPctOffset,
		HandoffTimeout: DefaultHandoffTimeout, ClearSettle: DefaultClearSettle,
		PollInterval:         DefaultCyclerPollInterval,
		ClearConfirmBackstop: DefaultClearConfirmBackstop,
		ClearConfirmRetries:  DefaultClearConfirmRetries,
		ModelDoneTimeout:     DefaultModelDoneTimeout,
		ForceRetryInterval:   DefaultForceRetryInterval,
		IdleRestartAbsTokens: DefaultIdleRestartAbsTokens,
		IdleRestartCooldown:  DefaultIdleRestartCooldown,
		MaxHandoffTimeouts:   DefaultMaxHandoffTimeouts, HoldTTL: DefaultHoldTTL,
	}
}

func cyclePolicyFromConfig(cfg CyclerConfig) CyclePolicy {
	return CyclePolicy{
		ActAbsTokens: cfg.ActAbsTokens, ActPctCeil: cfg.ActPctCeil,
		WarnAbsTokens: cfg.WarnAbsTokens, WarnPctCeil: cfg.WarnPctCeil,
		ForceActAbsTokens: cfg.ForceActAbsTokens, ForceActPctCeil: cfg.ForceActPctCeil,
		ActPct: cfg.ActPct, WarnPct: cfg.WarnPct, ForceActPct: cfg.ForceActPct,
		HandoffTimeout: cfg.HandoffTimeout, ClearSettle: cfg.ClearSettle,
		PollInterval: cfg.PollInterval, ClearConfirmBackstop: cfg.ClearConfirmBackstop,
		ClearConfirmRetries: cfg.ClearConfirmRetries, ModelDoneTimeout: cfg.ModelDoneTimeout,
		ForceRetryInterval:   cfg.ForceRetryInterval,
		IdleRestartAbsTokens: cfg.IdleRestartAbsTokens, IdleRestartCooldown: cfg.IdleRestartCooldown,
		BootGracePeriod: cfg.BootGracePeriod, MaxBootGraceTotal: cfg.MaxBootGraceTotal,
		MaxHandoffTimeouts: cfg.MaxHandoffTimeouts, HoldTTL: cfg.HoldTTL,
		OperatorTurnLookback: cfg.OperatorTurnLookback, PostAnswerGrace: cfg.PostAnswerGrace,
		hasRespawn: cfg.hasRespawn,
	}
}

func cycleEnvFromConfig(cfg CyclerConfig) CycleEnv {
	return CycleEnv{
		AgentName: cfg.AgentName, ProjectDir: cfg.ProjectDir,
		TmuxTarget: cfg.TmuxTarget, TranscriptDir: cfg.TranscriptDir,
	}
}

// LegacyCyclerConfig converts policy and environment values into the scalar
// part of the migration adapter. Effect dependencies remain unset.
func LegacyCyclerConfig(policy CyclePolicy, env CycleEnv) CyclerConfig {
	return CyclerConfig{
		AgentName: env.AgentName, ProjectDir: env.ProjectDir, TmuxTarget: env.TmuxTarget,
		TranscriptDir: env.TranscriptDir,
		ActAbsTokens:  policy.ActAbsTokens, ActPctCeil: policy.ActPctCeil,
		WarnAbsTokens: policy.WarnAbsTokens, WarnPctCeil: policy.WarnPctCeil,
		ForceActAbsTokens: policy.ForceActAbsTokens, ForceActPctCeil: policy.ForceActPctCeil,
		ActPct: policy.ActPct, WarnPct: policy.WarnPct, ForceActPct: policy.ForceActPct,
		HandoffTimeout: policy.HandoffTimeout, ClearSettle: policy.ClearSettle,
		PollInterval: policy.PollInterval, ClearConfirmBackstop: policy.ClearConfirmBackstop,
		ClearConfirmRetries: policy.ClearConfirmRetries, ModelDoneTimeout: policy.ModelDoneTimeout,
		ForceRetryInterval:   policy.ForceRetryInterval,
		IdleRestartAbsTokens: policy.IdleRestartAbsTokens, IdleRestartCooldown: policy.IdleRestartCooldown,
		BootGracePeriod: policy.BootGracePeriod, MaxBootGraceTotal: policy.MaxBootGraceTotal,
		MaxHandoffTimeouts: policy.MaxHandoffTimeouts, HoldTTL: policy.HoldTTL,
		OperatorTurnLookback: policy.OperatorTurnLookback, PostAnswerGrace: policy.PostAnswerGrace,
		hasRespawn: policy.hasRespawn,
	}
}

func (p *CyclePolicy) actThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(p.ActAbsTokens, p.ActPctCeil, windowSize)
}

func (p *CyclePolicy) warnThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(p.WarnAbsTokens, p.WarnPctCeil, windowSize)
}

func (p *CyclePolicy) belowActThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Tokens < p.actThreshold(cf.WindowSize)
	}
	return cf.Pct < p.ActPct
}

func (p *CyclePolicy) belowWarnThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Pct < p.WarnPct || cf.Tokens < p.warnThreshold(cf.WindowSize)
	}
	return cf.Pct < p.WarnPct
}

func (p *CyclePolicy) forceActThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(p.ForceActAbsTokens, p.ForceActPctCeil, windowSize)
}

func (p *CyclePolicy) aboveForceThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Tokens >= p.forceActThreshold(cf.WindowSize)
	}
	return cf.Pct >= p.ForceActPct
}
