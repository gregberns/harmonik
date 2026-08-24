package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

type keeperBuildParams struct {
	AgentName    string
	ProjectDir   string
	ResolvedTmux string
	WindowSize   int64
	WarnOnly     bool
	RespawnCmd   string
	ForceRestart bool
	// WarnPctRaw/ActPctRaw are the raw --warn-pct/--act-pct percent values (0..100)
	// threaded into CyclerConfig.ActPct and WatcherConfig.WarnPct verbatim, exactly
	// as the inline path did.
	WarnPctRaw int
	ActPctRaw  int
	// KeeperCfg carries the config-only warn-text overrides (the only KeeperConfig
	// fields the literals read directly rather than via ResolvedKeeperConfig).
	KeeperCfg projectconfig.KeeperConfig
}

func buildKeeperConfigs(resolved ResolvedKeeperConfig, p keeperBuildParams) (keeper.CyclerConfig, keeper.WatcherConfig) {
	resolvedBootGrace := keeper.DefaultBootGracePeriod
	if resolved.BootGraceSet {
		resolvedBootGrace = resolved.BootGrace
	}

	cyclerCfg := keeper.CyclerConfig{
		AgentName:            p.AgentName,
		ProjectDir:           p.ProjectDir,
		TmuxTarget:           p.ResolvedTmux,
		ActPct:               float64(p.ActPctRaw),
		ActAbsTokens:         resolved.ActAbsTokens,
		WarnAbsTokens:        resolved.WarnAbsTokens,
		ForceActAbsTokens:    resolved.ForceActAbsTokens,
		ActPctCeil:           resolved.ActPctCeil,
		WarnPctCeil:          resolved.WarnPctCeil,
		IdleRestartAbsTokens: resolved.IdleRestartAbsTokens,
		HandoffTimeout:       resolved.HandoffTimeout,
		ClearSettle:          resolved.ClearSettle,
		PollInterval:         resolved.CyclerPollInterval,
		ForceRetryInterval:   resolved.ForceRetryInterval,
		IdleRestartCooldown:  resolved.IdleRestartCooldown,
		MaxHandoffTimeouts:   resolved.MaxHandoffTimeouts,
		HoldTTL:              resolved.HoldTTL,
		BootGracePeriod:      resolvedBootGrace,
		OperatorTurnLookback: resolved.OperatorTurnLookback,
		PostAnswerGrace:      resolved.PostAnswerGrace,
		HardBandCycleOnly:    true,
	}

	watcherCfg := keeper.WatcherConfig{
		AgentName:            p.AgentName,
		ProjectDir:           p.ProjectDir,
		WarnPct:              float64(p.WarnPctRaw),
		TmuxTarget:           p.ResolvedTmux,
		Cycler:               nil, // caller assigns the constructed *Cycler post crash-recovery
		FallbackWindowSize:   p.WindowSize,
		WarnAbsTokens:        resolved.WarnAbsTokens,
		WarnPctCeil:          resolved.WarnPctCeil,
		ActPct:               float64(p.ActPctRaw),
		ActAbsTokens:         resolved.ActAbsTokens,
		ActPctCeil:           resolved.ActPctCeil,
		WarnOnly:             p.WarnOnly,
		RespawnCmd:           p.RespawnCmd,
		PollInterval:         resolved.PollInterval,
		IdleQuiesce:          resolved.IdleQuiesce,
		Staleness:            resolved.Staleness,
		RespawnGrace:         resolved.RespawnGrace,
		RespawnCooldown:      resolved.RespawnCooldown,
		LiveRecoverGrace:     resolved.LiveRecoverGrace,
		LiveRecoverCooldown:  resolved.LiveRecoverCooldown,
		NoGaugeBackoff:       resolved.NoGaugeBackoff,
		HoldTTL:              resolved.HoldTTL,
		HardCeilingCooldown:  resolved.HardCeilingCooldown,
		BlindKeeperThreshold: resolved.BlindKeeperThreshold,
		HeartbeatMaxMisses:   resolved.HeartbeatMaxMisses,
		HardCeilingTokens:    resolved.HardCeilingAbsTokens,
		HardCeilingMode:      resolved.HardCeilingMode,
		HardCeilingRestartFn: keeperHardCeilingRestartFn(
			resolved.HardCeilingMode, p.ResolvedTmux, p.ProjectDir, p.RespawnCmd),
		WarnCooldown:  resolved.WarnCooldown,
		LiveRecoverFn: keeperLiveRecoverFn(p.WarnOnly, p.ProjectDir, p.RespawnCmd),
		// hk-vs4u: warn-text + self_service flow through the resolver (config>default).
		// DefaultWarnText = lighter advisory; ActionableWarnText = the R3 self-service
		// restart handshake (selectWarnText picks between them). crews_enabled is
		// resolved UNSET→TRUE in ResolveKeeperConfig.
		DefaultWarnText:    resolved.DefaultWarnText,
		ActionableWarnText: resolved.ActionableWarnText,
		SettleWarnText:     resolved.SettleWarnText,
		// K2 leader defer-message / K7 crew-message body overrides (config surface,
		// T2). Carried to the watcher; T3 fills/validates the leader slots and
		// wires selection. crew text stays inert until K7 activation.
		LeaderDeferText: resolved.LeaderDeferText,
		CrewDeferText:   resolved.CrewDeferText,
		// SK-034 (T4): mtime-gated per-tick re-read of keeper.warn_messages so
		// wording edits apply with no keeper bounce, strictly scoped away from
		// thresholds. Injected here because keeper may not import daemon (depguard).
		ReloadWarnMessagesFn:            keeperReloadWarnMessagesFn(p.ProjectDir),
		SelfServiceEnabled:              resolved.SelfServiceEnabled,
		SelfServiceCrewsEnabled:         resolved.SelfServiceCrewsEnabled,
		SelfServiceGraceSeconds:         resolved.SelfServiceGraceSeconds,
		SelfServiceInstructOnlyWhenIdle: resolved.SelfServiceInstructOnlyWhenIdle,
		ReapDecisions:                   true,
		ReapDecisionsCadence:            resolved.ReapDecisionsCadence,
		HeartbeatEnabled:                true,
		OperatorWarnFn:                  keeperOperatorWarnFn(p.ProjectDir, p.AgentName),
	}
	return cyclerCfg, watcherCfg
}

func constructKeeperCycler(
	policy keeper.CyclePolicy,
	env keeper.CycleEnv,
	deps keeper.CycleDeps,
) (*keeper.Cycler, error) {
	return keeper.NewCyclerWithDeps(policy, env, deps)
}

type keeperPaneWithEscape struct{ keeper.PaneWriter }

func (p keeperPaneWithEscape) SendEscape(ctx context.Context, target string) error {
	return keeper.SendEscapeKey(ctx, target)
}

type keeperRespawnFunc func(context.Context, string) error

func (f keeperRespawnFunc) ForceRestart(ctx context.Context, agent string) error {
	return f(ctx, agent)
}

func buildKeeperCycleDeps(
	cfg keeper.CyclerConfig,
	emitter keeper.Emitter,
	forceRestart func(context.Context, string) error,
) keeper.CycleDeps {
	deps := keeper.CycleDepsFromConfig(cfg, emitter)
	deps.Pane = keeperPaneWithEscape{PaneWriter: deps.Pane}
	if forceRestart != nil {
		deps.Respawn = keeperRespawnFunc(forceRestart)
	}
	return deps
}

func runKeeperSubcommand(args []string) int {
	fs := flag.NewFlagSet("keeper", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var (
		agentFlag         string
		tmuxFlag          string
		warnPctFlag       int
		actPctFlag        int
		windowSizeFlag    int64
		warnAbsTokensFlag int64
		actAbsTokensFlag  int64
		respawnCmdFlag    string
		forceRestartFlag  bool
		warnOnlyFlag      bool

		stalenessFlag       time.Duration
		idleQuiesceFlag     time.Duration
		pollIntervalFlag    time.Duration
		handoffTimeoutFlag  time.Duration
		bootGraceFlag       time.Duration
		idleFloorAbsFlag    int64
		hardCeilingAbsFlag  int64
		hardCeilingModeFlag string
	)

	fs.StringVar(&agentFlag, "agent", "", "agent name (required)")
	fs.StringVar(&tmuxFlag, "tmux", "", "tmux pane target (optional; injected into on warn crossing)")
	fs.IntVar(&warnPctFlag, "warn-pct", 0, "context-use percentage that triggers a warning (0 = unset; use abs band)")
	fs.IntVar(&actPctFlag, "act-pct", 0, "context-use percentage that triggers handoff action (0 = unset; use abs band; .managed-gated)")
	fs.Int64Var(&windowSizeFlag, "window-size", 0, "assumed context-window token size when the gauge reports WindowSize==0; 0=unset")
	fs.Int64Var(&warnAbsTokensFlag, "warn-abs-tokens", 0, "absolute-token warn threshold; 0=unset → OPERATOR-REQUIRED (reads from keeper: config block)")
	fs.Int64Var(&actAbsTokensFlag, "act-abs-tokens", 0, "absolute-token act threshold; 0=unset → OPERATOR-REQUIRED (reads from keeper: config block)")
	fs.StringVar(&respawnCmdFlag, "respawn-cmd", "", "shell command to re-launch the agent after it exits (supervised respawn path; hk-3w2)")
	fs.BoolVar(&forceRestartFlag, "force-restart", false, "opt in to the handoff-timeout hard-restart escalation (fail-closed; requires --respawn-cmd; hk-suxt)")
	fs.BoolVar(&warnOnlyFlag, "warn-only", false, "warn-only mode: emit warn events but never trigger restart, respawn, or live-pane recovery (for crew keepers; hk-yfcc)")
	fs.DurationVar(&stalenessFlag, "staleness", 0, "gauge-staleness window before the gauge is treated as absent (default 120s)")
	fs.DurationVar(&idleQuiesceFlag, "idle-quiesce", 0, "minimum gauge quiescence before the pane is considered idle (default 8s)")
	fs.DurationVar(&pollIntervalFlag, "poll-interval", 0, "watcher gauge-poll cadence (default 5s)")
	fs.DurationVar(&handoffTimeoutFlag, "handoff-timeout", 0, "cycler handoff-nonce wait (default 180s)")
	fs.DurationVar(&bootGraceFlag, "boot-grace", 0, "young-session guard window after a session_id change; 0 (explicit) disables it (default 5m)")
	fs.Int64Var(&idleFloorAbsFlag, "idle-floor-abs-tokens", 0, "idle-crew restart token floor (default 150000)")
	fs.Int64Var(&hardCeilingAbsFlag, "hard-ceiling-abs-tokens", 0, "SID-independent hard-ceiling token trigger (default 280000)")
	fs.StringVar(&hardCeilingModeFlag, "hard-ceiling-mode", "", "hard-ceiling backstop mode: off|alarm|restart (default alarm)")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 1
		}
		return 2
	}

	var warnPctSet, actPctSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "warn-pct":
			warnPctSet = true
		case "act-pct":
			actPctSet = true
		}
	})
	if warnPctSet {
		fmt.Fprintf(os.Stderr, "keeper: honoring --warn-pct %d as warn ceil %.2f of window\n", warnPctFlag, float64(warnPctFlag)/100.0)
	}
	if actPctSet {
		fmt.Fprintf(os.Stderr, "keeper: honoring --act-pct %d as act ceil %.2f of window\n", actPctFlag, float64(actPctFlag)/100.0)
	}

	resolvedAgent, code := resolveKeeperAgent(fs, "harmonik keeper", agentFlag)
	if resolvedAgent == "" {
		return code
	}
	agentFlag = resolvedAgent

	projectDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper: cannot determine working directory: %v\n", err)
		return 1
	}

	projCfg, projCfgErr := projectconfig.LoadProjectConfig(projectDir)
	if projCfgErr != nil {
		var unknownKey *projectconfig.ErrUnknownConfigKey
		if errors.As(projCfgErr, &unknownKey) {
			fmt.Fprintf(os.Stderr, "keeper: refusing to start: %v\n", projCfgErr)
			return 2
		}
		fmt.Fprintf(os.Stderr, "keeper: project config: %v (ignoring; using defaults)\n", projCfgErr)
	}
	keeperCfg := projCfg.Keeper

	var absWarnSet, absActSet bool
	var stalenessSet, idleQuiesceSet, pollIntervalSet, handoffTimeoutSet bool
	var bootGraceSet, idleFloorSet, hardCeilingAbsSet, hardCeilingModeSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "warn-abs-tokens":
			absWarnSet = true
		case "act-abs-tokens":
			absActSet = true
		case "staleness":
			stalenessSet = true
		case "idle-quiesce":
			idleQuiesceSet = true
		case "poll-interval":
			pollIntervalSet = true
		case "handoff-timeout":
			handoffTimeoutSet = true
		case "boot-grace":
			bootGraceSet = true
		case "idle-floor-abs-tokens":
			idleFloorSet = true
		case "hard-ceiling-abs-tokens":
			hardCeilingAbsSet = true
		case "hard-ceiling-mode":
			hardCeilingModeSet = true
		}
	})

	{
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			settingsPath := home + "/.claude/settings.json"
			runKeeperDoctorAtBoot(projectDir, agentFlag, settingsPath)
		}
	}

	resolved, resolveErr := ResolveKeeperConfig(KeeperFlags{
		WarnAbsTokens: warnAbsTokensFlag,
		WarnAbsSet:    absWarnSet,
		ActAbsTokens:  actAbsTokensFlag,
		ActAbsSet:     absActSet,
		WarnPct:       warnPctFlag,
		WarnPctSet:    warnPctSet,
		ActPct:        actPctFlag,
		ActPctSet:     actPctSet,
		// TIER-1 tunable flags (hk-4gtu).
		Staleness:          stalenessFlag,
		StalenessSet:       stalenessSet,
		IdleQuiesce:        idleQuiesceFlag,
		IdleQuiesceSet:     idleQuiesceSet,
		PollInterval:       pollIntervalFlag,
		PollIntervalSet:    pollIntervalSet,
		HandoffTimeout:     handoffTimeoutFlag,
		HandoffTimeoutSet:  handoffTimeoutSet,
		BootGrace:          bootGraceFlag,
		BootGraceSet:       bootGraceSet,
		IdleFloorAbsTokens: idleFloorAbsFlag,
		IdleFloorSet:       idleFloorSet,
		HardCeilingAbs:     hardCeilingAbsFlag,
		HardCeilingAbsSet:  hardCeilingAbsSet,
		HardCeilingMode:    hardCeilingModeFlag,
		HardCeilingModeSet: hardCeilingModeSet,
	}, keeperCfg, projectDir)
	if resolveErr != nil {
		var kme *KeeperConfigMissingError
		if errors.As(resolveErr, &kme) {
			fmt.Fprintf(os.Stderr, "keeper: %v\n", resolveErr)
		} else {
			fmt.Fprintf(os.Stderr, "keeper: refusing to start — %v\n", resolveErr)
		}
		emitKeeperConfigRejected(projectDir, agentFlag, resolveErr)
		return 1
	}
	resolvedWarnAbs := resolved.WarnAbsTokens
	resolvedActAbs := resolved.ActAbsTokens
	resolvedForceActAbs := resolved.ForceActAbsTokens
	resolvedActPctCeil := resolved.ActPctCeil
	resolvedWarnPctCeil := resolved.WarnPctCeil

	lock, err := keeper.AcquireLock(projectDir, agentFlag)
	if err != nil {
		if errors.Is(err, keeper.ErrLockHeld) {
			fmt.Fprintf(os.Stderr, "harmonik keeper: agent %q already has a live keeper; exiting\n", agentFlag)
			return 2
		}
		fmt.Fprintf(os.Stderr, "harmonik keeper: acquire lock: %v\n", err)
		return 1
	}
	defer func() { _ = lock.Release() }() //nolint:errcheck // best-effort on shutdown

	if !keeper.IsManaged(projectDir, agentFlag) {
		fmt.Fprintf(os.Stderr, "keeper: %s not opted-in (.managed marker missing); no-op\n", agentFlag)
		return 0
	}

	resolvedTmux := keeper.ResolveTmuxTarget(projectDir, agentFlag, tmuxFlag, nil)
	if resolvedTmux != "" && resolvedTmux != tmuxFlag {
		fmt.Fprintf(os.Stderr, "keeper: auto-resolved tmux target from convention: %q\n", resolvedTmux)
	}

	effWarn, effAct, effForce := keeper.EffectiveBandTokens(
		resolvedWarnAbs, resolvedActAbs, resolvedForceActAbs,
		resolvedWarnPctCeil, resolvedActPctCeil, windowSizeFlag)
	resolvedBootGrace := keeper.DefaultBootGracePeriod
	if resolved.BootGraceSet {
		resolvedBootGrace = resolved.BootGrace
	}
	pctNote := ""
	if warnPctSet || actPctSet {
		pctNote = fmt.Sprintf(" [pct ceils: warn=%.2f act=%.2f, tighten-only]", resolvedWarnPctCeil, resolvedActPctCeil)
	}
	warnOnlyNote := ""
	if warnOnlyFlag {
		warnOnlyNote = " [warn-only: no restart/respawn]"
	}
	fmt.Fprintf(os.Stderr,
		"keeper started for %s (effective band: warn=%d act=%d force=%d tokens%s, tmux=%q%s)\n",
		agentFlag, effWarn, effAct, effForce, pctNote, resolvedTmux, warnOnlyNote)
	fmt.Fprintf(os.Stderr,
		"keeper effective tunables: poll=%s idle_quiesce=%s staleness=%s handoff_timeout=%s boot_grace=%s "+
			"hard_ceiling=%d/%s idle_floor=%d no_gauge_backoff=%s heartbeat_max_misses=%d\n",
		resolved.PollInterval, resolved.IdleQuiesce, resolved.Staleness, resolved.HandoffTimeout, resolvedBootGrace,
		resolved.HardCeilingAbsTokens, resolved.HardCeilingMode, resolved.IdleRestartAbsTokens,
		resolved.NoGaugeBackoff, resolved.HeartbeatMaxMisses)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	emitter := keeper.NewFileEmitter(projectDir)

	cyclerCfg, cfg := buildKeeperConfigs(resolved, keeperBuildParams{
		AgentName:    agentFlag,
		ProjectDir:   projectDir,
		ResolvedTmux: resolvedTmux,
		WindowSize:   windowSizeFlag,
		WarnOnly:     warnOnlyFlag,
		RespawnCmd:   respawnCmdFlag,
		ForceRestart: forceRestartFlag,
		WarnPctRaw:   warnPctFlag,
		ActPctRaw:    actPctFlag,
		KeeperCfg:    keeperCfg,
	})

	var cycler *keeper.Cycler
	if !warnOnlyFlag {
		var constructErr error
		deps := buildKeeperCycleDeps(
			cyclerCfg,
			emitter,
			keeperForceRestartFn(forceRestartFlag, projectDir, respawnCmdFlag),
		)
		cycler, constructErr = constructKeeperCycler(
			keeper.CyclePolicyFromConfig(cyclerCfg),
			keeper.CycleEnvFromConfig(cyclerCfg),
			deps,
		)
		if constructErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper: construct cycle: %v\n", constructErr)
			return 1
		}

		if recoverErr := cycler.RecoverFromCrash(ctx); recoverErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper: crash recovery: %v\n", recoverErr)
		}
	}
	cfg.Cycler = cycler

	w := keeper.NewWatcher(cfg, emitter)
	if runErr := w.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "harmonik keeper: watcher: %v\n", runErr)
		return 1
	}

	return 0
}

func keeperForceRestartFn(forceRestart bool, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if !forceRestart || respawnCmd == "" {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

func keeperLiveRecoverFn(warnOnly bool, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if warnOnly {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

func keeperHardCeilingRestartFn(mode keeper.HardCeilingMode, tmuxTarget, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if mode != keeper.HardCeilingModeRestart || respawnCmd == "" || tmuxTarget == "" {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

func keeperOperatorWarnFn(projectDir, agentName string) func(ctx context.Context, sessionID string, tokens, warnTokens, actTokens int64) {
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	return func(ctx context.Context, _ string, tokens, warnTokens, actTokens int64) {
		msg := fmt.Sprintf("%s approaching ACT band: %dk / warn=%dk act=%dk",
			agentName, tokens/1000, warnTokens/1000, actTokens/1000)
		//nolint:gosec // G204: args are operator-supplied agentName + runtime token counts
		cmd := exec.CommandContext(ctx, exe,
			"comms", "send",
			"--from", "keeper",
			"--to", "operator",
			"--topic", "keeper-warn",
			"--", msg)
		cmd.Dir = projectDir
		if runErr := cmd.Run(); runErr != nil {
			fmt.Fprintf(os.Stderr, "keeper: operator warn comms send: %v\n", runErr)
		}
	}
}

func keeperReloadWarnMessagesFn(projectDir string) func() (keeper.WarnMessageTexts, error) {
	return func() (keeper.WarnMessageTexts, error) {
		cfg, err := projectconfig.LoadProjectConfig(projectDir)
		if err != nil {
			return keeper.WarnMessageTexts{}, err
		}
		k := cfg.Keeper
		return keeper.WarnMessageTexts{
			DefaultWarnText:    k.DefaultWarnText,
			ActionableWarnText: k.ActionableWarnText,
			SettleWarnText:     k.SettleWarnText,
			LeaderDeferText:    k.LeaderDeferText,
			CrewDeferText:      k.CrewDeferText,
		}, nil
	}
}

func newKeeperMarkerFlags(name string) (fs *flag.FlagSet, projectFlag, agentFlag *string) {
	fs = flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	projectFlag = fs.String("project", "", "project directory (default: current working directory)")
	agentFlag = fs.String("agent", "", "agent name (required)")
	return fs, projectFlag, agentFlag
}

func resolveKeeperAgent(fs *flag.FlagSet, label, agentFlag string) (string, int) {
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "%s: unexpected positional argument(s) %q — this command is flag-only; use --agent <name>\n",
			label, strings.Join(fs.Args(), " "))
		return "", 2
	}
	if agentFlag == "" {
		fmt.Fprintf(os.Stderr, "%s: --agent <name> is required\n", label)
		return "", 1
	}
	return agentFlag, 0
}

func parseKeeperMarkerArgs(label string, args []string) (agent, project string, code int) {
	fs, projectFlag, agentFlag := newKeeperMarkerFlags(label)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", "", 1
		}
		return "", "", 2
	}
	agent, code = resolveKeeperAgent(fs, "harmonik "+label, *agentFlag)
	if agent == "" {
		return "", "", code
	}
	project = *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik %s: cannot determine working directory: %v\n", label, err)
			return "", "", 1
		}
		project = wd
	}
	abs, err := normalizeProjectDir(label, project)
	if err != nil {
		return "", "", 1
	}
	project = abs
	return agent, project, 0
}

func normalizeProjectDir(label, project string) (string, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: cannot resolve project path %q: %v\n", label, project, err)
		return "", err
	}
	return abs, nil
}

func runKeeperSetDispatching(args []string) int {
	agent, project, code := parseKeeperMarkerArgs("keeper set-dispatching", args)
	if agent == "" {
		return code
	}
	if err := keeper.SetDispatching(project, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper set-dispatching: %v\n", err)
		return 1
	}
	if !keeper.LiveKeeperPresent(project, agent) {
		fmt.Fprintf(os.Stderr,
			"keeper set-dispatching: WARNING — no live keeper found for agent %q under %q "+
				"(.dispatching marker written, but no watcher is currently guarding this dispatch; "+
				"start `harmonik keeper --agent %s` or verify --project)\n",
			agent, project, agent)
	}
	return 0
}

func runKeeperClearDispatching(args []string) int {
	agent, project, code := parseKeeperMarkerArgs("keeper clear-dispatching", args)
	if agent == "" {
		return code
	}
	if err := keeper.ClearDispatching(project, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper clear-dispatching: %v\n", err)
		return 1
	}
	return 0
}

func runKeeperHold(args []string) int {
	agent, project, code := parseKeeperMarkerArgs("keeper hold", args)
	if agent == "" {
		return code
	}
	sid, err := keeper.SetHold(project, agent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper hold: %v\n", err)
		return 1
	}
	if !keeper.LiveKeeperPresent(project, agent) {
		fmt.Fprintf(os.Stderr,
			"keeper hold: WARNING — no live keeper found for agent %q under %q "+
				"(hold marker written, but no watcher is currently guarding this agent; "+
				"start `harmonik keeper --agent %s`)\n", agent, project, agent)
	}
	fmt.Printf("keeper hold: agent=%q session=%s — ACT/restart suspended (WARN still fires; auto-reverts on restart or after the TTL). Run `harmonik keeper release --agent %s` to resume early.\n", agent, sid, agent)
	return 0
}

func runKeeperRelease(args []string) int {
	agent, project, code := parseKeeperMarkerArgs("keeper release", args)
	if agent == "" {
		return code
	}
	if err := keeper.ReleaseHold(project, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper release: %v\n", err)
		return 1
	}
	fmt.Printf("keeper release: agent=%q — hold cleared; normal keeper behavior resumed.\n", agent)
	return 0
}

func runKeeperRestartNow(args []string) int {
	fs, projectFlag, agentFlag := newKeeperMarkerFlags("keeper restart-now")
	tmuxFlag := fs.String("tmux", "", "explicit tmux pane target; use the live keeper target when it does not follow the project naming convention")
	nonceFlag := fs.String("nonce", "",
		"provenance nonce carried on the [KEEPER ACK <nonce>] line and the emitted "+
			"session_keeper_restart_now event; carry-for-audit, never validated (default: rn-<ms> timestamp)")
	forceFlag := fs.Bool("force", false,
		"restart even when the agent has in-flight queue work (.dispatching marker present). "+
			"Restarting mid-run cancels the crew's in-flight tool work and can leak orphaned "+
			"processes (hk-bl2k6); use only when you know the marker is stale")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 1
		}
		return 2
	}
	agent, code := resolveKeeperAgent(fs, "harmonik keeper restart-now", *agentFlag)
	if agent == "" {
		return code
	}
	projectDir := *projectFlag
	if projectDir == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper restart-now: cannot determine working directory: %v\n", wdErr)
			return 1
		}
		projectDir = wd
	}
	absRN, absRNErr := normalizeProjectDir("keeper restart-now", projectDir)
	if absRNErr != nil {
		return 1
	}
	projectDir = absRN

	tmuxTarget := keeper.ResolveTmuxTarget(projectDir, agent, *tmuxFlag, nil)

	requestedAt := time.Now().UTC()
	nonce := *nonceFlag
	if nonce == "" {
		nonce = restartNowNonce(requestedAt)
	}
	cfg := keeper.RestartNowConfig{
		ProjectDir:  projectDir,
		AgentName:   agent,
		TmuxTarget:  tmuxTarget,
		RequestedAt: requestedAt,
		Force:       *forceFlag,
		// Durable audit record carrying the nonce (SK-030). FileEmitter appends to
		// <projectDir>/.harmonik/events/events.jsonl.
		Emitter: keeper.NewFileEmitter(projectDir),
	}
	previousSID, err := keeper.ValidateRestartNow(context.Background(), cfg, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper restart-now: %v\n", err)
		return 1
	}
	if err := startKeeperRestartDriver(projectDir, agent, tmuxTarget, previousSID, nonce); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper restart-now: start detached driver: %v\n", err)
		return 1
	}
	fmt.Printf("keeper restart-now: agent=%q nonce=%s accepted; detached driver will clear once and brief after session turnover in %q\n", agent, nonce, tmuxTarget)
	return 0
}

func startKeeperRestartDriver(projectDir, agent, target, previousSID, nonce string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(projectDir, ".harmonik", "keeper", agent+".restart-now.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "keeper", "restart-driver", "--project", projectDir, "--agent", agent, "--tmux", target, "--previous-sid", previousSID, "--nonce", nonce) //nolint:gosec // Values are argv, not a shell command.
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = cmd.Process.Release()
	return logFile.Close()
}

func runKeeperRestartDriver(args []string) int {
	fs := flag.NewFlagSet("keeper restart-driver", flag.ContinueOnError)
	project := fs.String("project", "", "")
	agent := fs.String("agent", "", "")
	target := fs.String("tmux", "", "")
	previousSID := fs.String("previous-sid", "", "")
	nonce := fs.String("nonce", "", "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	err := keeper.DriveRestartAfterReturn(context.Background(), keeper.RestartDriveConfig{
		RestartNowConfig:  keeper.RestartNowConfig{ProjectDir: *project, AgentName: *agent, TmuxTarget: *target},
		PreviousSessionID: *previousSID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "keeper restart-driver nonce=%s: %v\n", *nonce, err)
		return 1
	}
	fmt.Printf("keeper restart-driver nonce=%s: session changed and brief submitted\n", *nonce)
	return 0
}

func runKeeperPing(args []string) int {
	fs, projectFlag, agentFlag := newKeeperMarkerFlags("keeper ping")
	nonceFlag := fs.String("nonce", "", "verifiability nonce echoed in the [KEEPER ACK <nonce>] line (default: timestamp)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 1
		}
		return 2
	}
	agent, code := resolveKeeperAgent(fs, "harmonik keeper ping", *agentFlag)
	if agent == "" {
		return code
	}
	projectDir := *projectFlag
	if projectDir == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper ping: cannot determine working directory: %v\n", wdErr)
			return 1
		}
		projectDir = wd
	}
	absPing, absPingErr := normalizeProjectDir("keeper ping", projectDir)
	if absPingErr != nil {
		return 1
	}
	projectDir = absPing
	nonce := *nonceFlag
	if nonce == "" {
		nonce = restartNowNonce(time.Now().UTC())
	}
	tmuxTarget := keeper.ResolveTmuxTarget(projectDir, agent, "", nil)
	if err := keeper.Ping(context.Background(), keeper.RestartNowConfig{
		ProjectDir: projectDir,
		AgentName:  agent,
		TmuxTarget: tmuxTarget,
	}, nonce); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper ping: %v\n", err)
		return 1
	}
	fmt.Printf("keeper ping: agent=%q ack injected (nonce=%s) into %q\n", agent, nonce, tmuxTarget)
	return 0
}

func restartNowNonce(t time.Time) string {
	return fmt.Sprintf("rn-%d", t.UnixMilli())
}

func runKeeperAwaitAck(args []string) int {
	fs, projectFlag, agentFlag := newKeeperMarkerFlags("keeper await-ack")
	nonceFlag := fs.String("nonce", "", "exact verifiability nonce to match in the [KEEPER ACK <nonce>] line (required)")
	kindFlag := fs.String("kind", "ping", "handshake kind being confirmed: restart|ping (echoed into the event)")
	timeoutFlag := fs.Duration("timeout", keeper.DefaultAwaitAckTimeout, "max time to wait for the ack before timing out")
	pollFlag := fs.Duration("poll", keeper.DefaultAwaitAckPoll, "interval between capture-pane polls")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 1
		}
		return 2
	}
	agent, code := resolveKeeperAgent(fs, "harmonik keeper await-ack", *agentFlag)
	if agent == "" {
		return code
	}
	if *nonceFlag == "" {
		fmt.Fprintf(os.Stderr, "harmonik keeper await-ack: --nonce <N> is required\n")
		return 1
	}
	projectDir := *projectFlag
	if projectDir == "" {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper await-ack: cannot determine working directory: %v\n", wdErr)
			return 1
		}
		projectDir = wd
	}
	absAA, absAAErr := normalizeProjectDir("keeper await-ack", projectDir)
	if absAAErr != nil {
		return 1
	}
	projectDir = absAA

	tmuxTarget := keeper.ResolveTmuxTarget(projectDir, agent, "", nil)
	emitter := keeper.NewFileEmitter(projectDir)
	err := keeper.AwaitAck(context.Background(), keeper.AwaitAckConfig{
		AgentName:  agent,
		TmuxTarget: tmuxTarget,
		Nonce:      *nonceFlag,
		Kind:       *kindFlag,
		Timeout:    *timeoutFlag,
		Poll:       *pollFlag,
	}, emitter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper await-ack: %v\n", err)
		if errors.Is(err, keeper.ErrAckTimeout) {
			return 3
		}
		return 1
	}
	fmt.Printf("keeper await-ack: agent=%q nonce=%s ack observed in %q (keeper alive)\n", agent, *nonceFlag, tmuxTarget)
	return 0
}

const keeperTopUsage = `harmonik keeper — context watcher for a managed agent pane (session-keeper, hk-ekap1)

USAGE
  harmonik keeper config --example
  harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N] [--warn-abs-tokens N] [--act-abs-tokens N]
  harmonik keeper enable <agent> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]
  harmonik keeper doctor <agent> [--project DIR]
  harmonik keeper set-dispatching --agent <name> [--project DIR]
  harmonik keeper clear-dispatching --agent <name> [--project DIR]
  harmonik keeper hold --agent <name> [--project DIR]
  harmonik keeper release --agent <name> [--project DIR]
  harmonik keeper restart-now --agent <name> [--project DIR]
  harmonik keeper ping --agent <name> [--nonce N] [--project DIR]
  harmonik keeper await-ack --agent <name> --nonce N [--kind restart|ping] [--timeout 15s] [--poll 1s] [--project DIR]

  FLAG-ONLY (hk-5da7): every verb (and the bare watcher) names the agent ONLY via
  --agent. A positional argument is rejected with exit 2 (positionals were the
  recurring restart-now failure mode); an unrecognized flag also exits 2.

VERBS
  config --example   Print a COMPLETE, commented keeper: config block to stdout. harmonik
                     imposes NO built-in keeper defaults at runtime — every value must be
                     set by the operator or the keeper REFUSES TO START. Paste the block
                     into <project>/.harmonik/config.yaml (under schema_version: 1) and
                     tune the numbers, then (re)launch the keeper.
  enable             Wire statusLine + Stop + PreCompact stanzas into ~/.claude/settings.json
                     (idempotent, JSON-aware, backs up first, normalizes env-var names).
                     Seeds HANDOFF-<agent>.md, validates tmux pane, prints the run command.
                     .managed creation requires --yes-destructive.
                     Run 'harmonik keeper enable --help' for full usage.
  doctor             Read-only drift validator: binary currency, all 3 hooks present,
                     gauge freshness, .idle written, .managed present, ANTHROPIC_API_KEY risk.
                     Exits non-zero on any gap.  Also runs automatically at keeper BOOT.
                     Run 'harmonik keeper doctor --help' for full usage.
  set-dispatching    Write the .dispatching marker for <agent>; HoldingDispatch → true.
                     Call BEFORE submitting a batch to the daemon queue so the keeper
                     cycle defers the handoff action while queue work is in flight.
  clear-dispatching  Remove the .dispatching marker for <agent>; HoldingDispatch → false.
                     Call when all in-flight queue work has completed. Idempotent.
  hold               Suspend the ACT/restart cutoff while co-working (session-id-keyed +
                     timer backstop; auto-reverts on restart; WARN still fires).
  release            Clear the hold; resume normal keeper behavior. Idempotent.
  restart-now        Agent/captain-initiated detached clear→resume (hk-5da7).
                     Verifies the session id and a non-empty HANDOFF-<agent>.md.
                     It starts a detached driver and returns so the active tool call can
                     end. The driver sends /clear once, waits for a new SessionStart ID,
                     and then injects /session-resume into the new session.
                     FAILS LOUDLY (non-zero exit + logged reason) on no pane, an
                     unverifiable session id, or a missing or empty handoff.
                     Refs: hk-5da7 (was hk-wjzf/ON-059 marker path).
  ping               Liveness check: inject ONLY '[KEEPER ACK <nonce>] received ping'
                     into the agent's pane (no /clear, no resume). --nonce sets the
                     verifiability token (default: timestamp). Refs: hk-5da7.
  await-ack          AGENT-SIDE half of the ack handshake (hk-uldg): poll the agent's
                     pane every --poll (default 1s) for the EXACT '[KEEPER ACK <nonce>]'
                     line until match (exit 0) or --timeout (default 15s). On timeout
                     emits a durable session_keeper_ack_timeout event and exits 3. The
                     BINARY does NOT send comms — the caller (skill) sends the alert on
                     non-zero exit. --kind restart|ping is echoed into the event. The
                     match is on the EXACT nonce, so a stale ACK from another cycle never
                     matches. For restart-now an EXTERNAL watcher must run this (the firing
                     agent /clears itself). Refs: hk-uldg.

FLAGS (watcher mode)
  --agent <name>         Agent name (required); identifies the lockfile and .managed marker
  --tmux <target>        tmux pane target (optional; injected into on warn/act-pct crossing)
  --warn-pct N           Context-use percentage that triggers a warning (default 0 = unset → use abs band;
                         an explicit value is tighten-only: it can move warn EARLIER, never later, than the abs band)
  --act-pct N            Context-use percentage that triggers handoff action (default 0 = unset → use abs band; .managed-gated;
                         an explicit value is tighten-only: it can move act EARLIER, never later, than the abs band)
  --warn-abs-tokens N    Absolute-token warn threshold; OPERATOR-REQUIRED (no built-in default — set here or in
                         keeper.context_thresholds.warn_abs_tokens); effective = min(warn-abs-tokens, warn-pct% * window)
  --act-abs-tokens N     Absolute-token act threshold; OPERATOR-REQUIRED (no built-in default — set here or in
                         keeper.context_thresholds.act_abs_tokens); effective = min(act-abs-tokens, act-pct% * window)
  --respawn-cmd <cmd>    Shell command to re-launch the agent when it exits (supervised respawn; hk-3w2).
                         After the gauge goes stale for 20s and the tmux pane is idle (shell prompt),
                         the keeper runs "sh -c <cmd>" to respawn the agent. Requires --tmux.
                         A 90s cooldown prevents tight respawn loops.
                         Example: --respawn-cmd 'harmonik captain respawn ...'
  --force-restart        Opt in to the handoff-timeout hard-restart escalation (default false; hk-suxt).
                         After MaxHandoffTimeouts (3) consecutive handoff timeouts above the force
                         threshold, the keeper runs --respawn-cmd to hard-restart a permanently
                         unresponsive pane. FAIL-CLOSED: inert unless BOTH --force-restart and
                         --respawn-cmd are set; the respawn refuses on a non-UUIDv4 bound .sid.

BEHAVIOUR (Phase-2, .managed-gated)
  1. Acquires .harmonik/keeper/<agent>.lock; exits 2 if another keeper is live.
  2. Checks .harmonik/keeper/<agent>.managed; exits 0 (no-op) if absent.
  3. If managed: runs crash recovery (resume any interrupted cycle from a prior crash),
     then starts the watcher loop — polls .harmonik/keeper/<agent>.ctx every 5s.
     On the first upward crossing of --warn-pct, injects a wrap-up-warning into the
     tmux pane (if --tmux is set) and emits session_keeper_warn.
     On crossing --act-pct with CrispIdle and no in-flight dispatch, runs the
     intent-preserving handoff→/clear→resume cycle (Cycler.MaybeRun).
     Emits session_keeper_no_gauge at boot and every 120s when the gauge file is absent
     or stale (a missing statusLine.command is visible, not silent).

GAUGE SETUP
  Add to ~/.claude/settings.json (via: harmonik keeper enable <agent> ...):
    "statusLine": {
      "type": "command",
      "command": "/path/to/scripts/keeper-statusline.sh"
    }
  The command carries no HARMONIK_PROJECT= prefix (ON-058b): project routing is
  resolved at runtime from each session's inherited HARMONIK_PROJECT env var.
  The script derives the agent name from the tmux session name at runtime, so a
  single project-agnostic entry works for all projects and concurrent sessions.

EXIT CODES (watcher mode)
  0  Success (no-op or clean signal shutdown)
  1  Argument or I/O error
  2  Lock held by another live keeper

EXIT CODES (set-dispatching / clear-dispatching)
  0  Success
  1  Argument, validation, or I/O error

EXAMPLES
  harmonik keeper config --example >> .harmonik/config.yaml
  harmonik keeper --agent orchestrator
  harmonik keeper --agent flywheel --tmux harmonik:0 --warn-abs-tokens 200000 --act-abs-tokens 215000
  harmonik keeper set-dispatching --agent orchestrator
  harmonik keeper clear-dispatching --agent orchestrator
  harmonik keeper set-dispatching --agent flywheel --project /path/to/project
  harmonik keeper hold --agent captain
  harmonik keeper release --agent captain
  harmonik keeper restart-now --agent captain
  harmonik keeper ping --agent captain --nonce check-001
`
