package keeper

import "time"

// CycleEnv identifies the external resources used by one keeper cycle. It has
// no policy values and no effect functions.
type CycleEnv struct {
	AgentName     string
	ProjectDir    string
	TmuxTarget    string
	TranscriptDir string
}

// CyclePolicy contains the values that control cycle decisions and timing. It
// has no resource identity and no effect functions.
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
}

// DefaultCyclePolicy returns the keeper library defaults. Production-only
// values remain disabled until the command layer resolves project config.
func DefaultCyclePolicy() CyclePolicy {
	return CyclePolicyFromConfig(CyclerConfig{})
}

// CyclePolicyFromConfig resolves a copy of cfg and projects only policy fields.
// Command wiring calls this after it applies project configuration.
func CyclePolicyFromConfig(cfg CyclerConfig) CyclePolicy {
	cfg.applyDefaults()
	return CyclePolicy{
		ActAbsTokens:         cfg.ActAbsTokens,
		ActPctCeil:           cfg.ActPctCeil,
		WarnAbsTokens:        cfg.WarnAbsTokens,
		WarnPctCeil:          cfg.WarnPctCeil,
		ForceActAbsTokens:    cfg.ForceActAbsTokens,
		ForceActPctCeil:      cfg.ForceActPctCeil,
		ActPct:               cfg.ActPct,
		WarnPct:              cfg.WarnPct,
		ForceActPct:          cfg.ForceActPct,
		HandoffTimeout:       cfg.HandoffTimeout,
		ClearSettle:          cfg.ClearSettle,
		PollInterval:         cfg.PollInterval,
		ClearConfirmBackstop: cfg.ClearConfirmBackstop,
		ClearConfirmRetries:  cfg.ClearConfirmRetries,
		ModelDoneTimeout:     cfg.ModelDoneTimeout,
		ForceRetryInterval:   cfg.ForceRetryInterval,
		IdleRestartAbsTokens: cfg.IdleRestartAbsTokens,
		IdleRestartCooldown:  cfg.IdleRestartCooldown,
		BootGracePeriod:      cfg.BootGracePeriod,
		MaxBootGraceTotal:    cfg.MaxBootGraceTotal,
		MaxHandoffTimeouts:   cfg.MaxHandoffTimeouts,
		HoldTTL:              cfg.HoldTTL,
		OperatorTurnLookback: cfg.OperatorTurnLookback,
		PostAnswerGrace:      cfg.PostAnswerGrace,
	}
}

// CycleEnvFromConfig projects resource identity without resolving effects.
func CycleEnvFromConfig(cfg CyclerConfig) CycleEnv {
	return CycleEnv{
		AgentName:     cfg.AgentName,
		ProjectDir:    cfg.ProjectDir,
		TmuxTarget:    cfg.TmuxTarget,
		TranscriptDir: cfg.TranscriptDir,
	}
}
