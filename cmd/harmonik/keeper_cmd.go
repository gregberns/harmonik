package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// keeperBuildParams carries the per-invocation, side-effect-free inputs the
// keeper Cycler/Watcher config literals are built from. It exists so the
// resolve→construct path that `harmonik keeper` uses (ResolveKeeperConfig +
// buildKeeperConfigs) is reachable from a test WITHOUT spinning the full
// runKeeperSubcommand (which acquires a lock, runs doctor, and blocks on signals).
// Refs: hk-yy57 (live config-driven-thresholds E2E test seam).
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
	KeeperCfg daemon.KeeperConfig
}

// buildKeeperConfigs builds the CyclerConfig and WatcherConfig literals the keeper
// start path constructs, from the already-resolved ResolvedKeeperConfig and the
// per-invocation params. It is SIDE-EFFECT-FREE: it returns the two config literals
// (data) and does NOT call NewCycler / RecoverFromCrash / NewWatcher — the caller
// (runKeeperSubcommand) owns those side effects so behaviour is byte-identical to
// the prior inline construction. The returned WatcherConfig.Cycler is left nil; the
// caller assigns the constructed *Cycler after crash recovery. Factored out (no
// behaviour change) so the config-driven-threshold resolution + construction is
// testable end-to-end without the lock/doctor/signal machinery. Refs: hk-yy57.
func buildKeeperConfigs(resolved ResolvedKeeperConfig, p keeperBuildParams) (keeper.CyclerConfig, keeper.WatcherConfig) {
	// hk-4gtu: BootGrace is fed at the Cycler construction site (never via
	// applyDefaults). When neither flag nor config set it, use DefaultBootGracePeriod
	// (5m); when set, honor the configured value VERBATIM including the 0 = disabled
	// sentinel.
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
		SendEscapeFn:         keeper.SendEscapeKey,
		BootGracePeriod:      resolvedBootGrace,
		ForceRestartFn:       keeperForceRestartFn(p.ForceRestart, p.ProjectDir, p.RespawnCmd),
		OperatorTurnLookback: resolved.OperatorTurnLookback,
		PostAnswerGrace:      resolved.PostAnswerGrace,
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
		DefaultWarnText:                 resolved.DefaultWarnText,
		ActionableWarnText:              resolved.ActionableWarnText,
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

// runKeeperSubcommand implements `harmonik keeper`.
//
// Flags:
//
//	--agent <name>        agent name (required); identifies the lockfile and .managed marker
//	--tmux <target>       tmux pane target (optional; injected into on warn/act crossing)
//	--warn-pct N          context-use percentage that triggers a warning (default 0 = unset → use abs band; tighten-only)
//	--act-pct N           context-use percentage that triggers handoff action (default 0 = unset → use abs band; .managed-gated; tighten-only)
//	--window-size N       assumed context-window token size when gauge reports WindowSize==0; 0=unset
//	--warn-abs-tokens N   absolute-token warn threshold; 0=unset → OPERATOR-REQUIRED (reads from config)
//	--act-abs-tokens N    absolute-token act threshold; 0=unset → OPERATOR-REQUIRED (reads from config)
//
// Behaviour (Phase-2, .managed-gated):
//  1. Acquire .harmonik/keeper/<agent>.lock; exit 2 if another live keeper holds it.
//  2. Check .harmonik/keeper/<agent>.managed; if absent, log no-op message and exit 0.
//  3. If present: run crash recovery (resume any interrupted cycle from a prior crash),
//     then start the watcher loop. On the first upward crossing of warn-pct, inject a
//     wrap-up-warning prompt into the managed pane (via --tmux) and emit
//     session_keeper_warn. On crossing act-pct with CrispIdle and no in-flight dispatch,
//     run the intent-preserving handoff→/clear→resume cycle. Emit session_keeper_no_gauge
//     when the gauge file is absent or stale. Block until SIGINT/SIGTERM.
//
// Exit codes:
//
//	0  — exited cleanly (no-op or signal shutdown)
//	1  — argument or I/O error
//	2  — lock already held by another keeper
//
// Spec ref: codename:session-keeper (hk-ekap1); beads hk-8vzek, hk-22i70, hk-lm9it.
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

		// TIER-1 tunable flags (hk-4gtu): high-traffic knobs get a CLI flag
		// (FLAG > CONFIG > DEFAULT). Long-tail cadence/budget stays config-only
		// but is still THREADED from config into the Watcher/Cycler literals.
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
	// W7 (hk-x7s): default 0 = UNSET → use the abs band. The advertised 80/90
	// defaults were never applied (only an EXPLICITLY-set flag flows through the
	// pct-ceil seam via fs.Visit), so a reader trusting the help text got a silent
	// no-op. Defaulting to 0 makes "unset → abs band" honest; an explicit value is
	// still honored (and tighten-only clamped) below.
	fs.IntVar(&warnPctFlag, "warn-pct", 0, "context-use percentage that triggers a warning (0 = unset; use abs band)")
	fs.IntVar(&actPctFlag, "act-pct", 0, "context-use percentage that triggers handoff action (0 = unset; use abs band; .managed-gated)")
	fs.Int64Var(&windowSizeFlag, "window-size", 0, "assumed context-window token size when the gauge reports WindowSize==0; 0=unset")
	fs.Int64Var(&warnAbsTokensFlag, "warn-abs-tokens", 0, "absolute-token warn threshold; 0=unset → OPERATOR-REQUIRED (reads from keeper: config block)")
	fs.Int64Var(&actAbsTokensFlag, "act-abs-tokens", 0, "absolute-token act threshold; 0=unset → OPERATOR-REQUIRED (reads from keeper: config block)")
	fs.StringVar(&respawnCmdFlag, "respawn-cmd", "", "shell command to re-launch the agent after it exits (supervised respawn path; hk-3w2)")
	fs.BoolVar(&forceRestartFlag, "force-restart", false, "opt in to the handoff-timeout hard-restart escalation (fail-closed; requires --respawn-cmd; hk-suxt)")
	fs.BoolVar(&warnOnlyFlag, "warn-only", false, "warn-only mode: emit warn events but never trigger restart, respawn, or live-pane recovery (for crew keepers; hk-yfcc)")
	// TIER-1 tunable flags (hk-4gtu). Each FLAG > CONFIG > DEFAULT; 0/"" = unset → defer.
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
		// Unrecognized flag (incl. a stray leading-dash token): loud exit 2.
		return 2
	}

	// hk-5da7: HONOR explicitly-set --warn-pct/--act-pct instead of silently
	// ignoring them. Previously these flags were inert on 1M-window models — the
	// gate consulted only the hardcoded pct-CEILS (0.70/0.85) and abs caps
	// (200k/215k), so on a 1M window the abs cap always won and the operator's
	// `--warn-pct 30 --act-pct 35` did nothing. We now feed an explicit pct flag
	// in as the pct-ceil (pct/100), so it flows through the SAME min(abs, ceil*window)
	// band logic. The EARLIER of the two thresholds fires, so a lower pct than the
	// abs default restarts sooner (the operator's intent) and a higher one is
	// harmlessly capped by abs. The threshold math itself is unchanged — this only
	// routes the flag into the existing pctCeil seam. Refs: hk-odhh, hk-5da7.
	var warnPctSet, actPctSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "warn-pct":
			warnPctSet = true
		case "act-pct":
			actPctSet = true
		}
	})
	// Emit the honoring acknowledgment EARLY (before agent resolution) so it is
	// visible even when the command later fails for another reason — this is the
	// loud, no-silent-misconfig signal the old inert warning used to be.
	if warnPctSet {
		fmt.Fprintf(os.Stderr, "keeper: honoring --warn-pct %d as warn ceil %.2f of window\n", warnPctFlag, float64(warnPctFlag)/100.0)
	}
	if actPctSet {
		fmt.Fprintf(os.Stderr, "keeper: honoring --act-pct %d as act ceil %.2f of window\n", actPctFlag, float64(actPctFlag)/100.0)
	}

	// Resolve the target agent: FLAG-ONLY (hk-5da7). --agent is required; a
	// positional argument is rejected with exit 2.
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

	// Load .harmonik/config.yaml keeper: block for threshold + text defaults.
	// Errors are non-fatal (logged to stderr); missing file is silently a no-op.
	// Precedence: CLI flag > config.yaml > compiled default (applied in applyDefaults).
	projCfg, projCfgErr := daemon.LoadProjectConfig(projectDir)
	if projCfgErr != nil {
		// hk-9f3f (operator decision): an unknown / typo'd key under the keeper:
		// block is a HARD ERROR — `harmonik keeper` REFUSES to start so the
		// operator notices and fixes the key, rather than silently running on
		// defaults with their intended config dropped. Other loader errors remain
		// non-fatal (logged; defaults used) per the prior keeper-startup contract.
		var unknownKey *daemon.ErrUnknownConfigKey
		if errors.As(projCfgErr, &unknownKey) {
			fmt.Fprintf(os.Stderr, "keeper: refusing to start: %v\n", projCfgErr)
			return 2
		}
		fmt.Fprintf(os.Stderr, "keeper: project config: %v (ignoring; using defaults)\n", projCfgErr)
	}
	keeperCfg := projCfg.Keeper

	// Detect which threshold flags were explicitly set by the caller so we can
	// distinguish "caller passed 0" from "caller omitted the flag".
	var absWarnSet, absActSet bool
	// TIER-1 explicit-set detection (hk-4gtu): so a 0 flag can override config.
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

	// Pre-flight doctor: runs BEFORE ResolveKeeperConfig so config gaps are
	// visible in the keeper's output even when the config-missing-key error
	// causes an early exit. Non-fatal: the resolve below is the hard gate.
	// The new "config" check in runKeeperDoctor directly reports missing keys.
	// Refs: hk-zou19.
	{
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			settingsPath := home + "/.claude/settings.json"
			runKeeperDoctorAtBoot(projectDir, agentFlag, settingsPath)
		}
	}

	// Resolve the effective threshold band through the SINGLE precedence resolver
	// (hk-4pnv): FLAG > CONFIG > DEFAULT per field, tighten-only pct, force-act
	// precedence (abs wins over offset), defaults read from internal/keeper's
	// exported Default* consts. FAIL-LOUD (operator decision): on ANY bad config
	// value, bad CLI flag, pct>1, or a cross-field band inversion the resolver
	// returns a *KeeperConfigError — we log it and REFUSE to start. We do NOT
	// silently default and do NOT revert the block to compiled defaults: a
	// misconfiguration MUST be learned, never masked. This matches the daemon
	// block's fail-fast posture.
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
		// The missing-value error is self-contained ("keeper: refusing to start — …");
		// the bad-value error is a bare "keeper config: …", so prefix it. Avoid a
		// doubled "refusing to start" for the missing-value class.
		var kme *KeeperConfigMissingError
		if errors.As(resolveErr, &kme) {
			fmt.Fprintf(os.Stderr, "keeper: %v\n", resolveErr)
		} else {
			fmt.Fprintf(os.Stderr, "keeper: refusing to start — %v\n", resolveErr)
		}
		// Surface a durable event too: the keeper events.jsonl is reachable here
		// (FileEmitter derives its path from projectDir), so a fail-loud
		// misconfiguration is not stderr-only.
		emitKeeperConfigRejected(projectDir, agentFlag, resolveErr)
		return 1
	}
	resolvedWarnAbs := resolved.WarnAbsTokens
	resolvedActAbs := resolved.ActAbsTokens
	resolvedForceActAbs := resolved.ForceActAbsTokens
	resolvedActPctCeil := resolved.ActPctCeil
	resolvedWarnPctCeil := resolved.WarnPctCeil

	// Step 1: acquire single-keeper lockfile.
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

	// Step 2: check .managed opt-in guard (fail-safe: absent = no-op).
	if !keeper.IsManaged(projectDir, agentFlag) {
		fmt.Fprintf(os.Stderr, "keeper: %s not opted-in (.managed marker missing); no-op\n", agentFlag)
		return 0
	}

	// Step 3: resolve the effective tmux target.
	// If --tmux was provided, use it as-is. Otherwise attempt to auto-derive the
	// session name from the harmonik convention: "harmonik-<hash12>-<agent>".
	resolvedTmux := keeper.ResolveTmuxTarget(projectDir, agentFlag, tmuxFlag, nil)
	if resolvedTmux != "" && resolvedTmux != tmuxFlag {
		fmt.Fprintf(os.Stderr, "keeper: auto-resolved tmux target from convention: %q\n", resolvedTmux)
	}

	// Step 4: agent is managed — start the watcher and block until signal.
	// W7 (hk-x7s): print the EFFECTIVE resolved band (the abs tokens the gate
	// actually fires on, tighten-only-clamped by any explicit pct ceil) rather than
	// the raw pct flag — the old banner printed warn-pct=80/act-pct=90 even when the
	// abs band is what fires, which is exactly the misleading-default class W7 closes.
	// windowSizeFlag is the startup fallback (0 = resolved at runtime from the gauge);
	// when 0 the pct ceil is applied later, so the banner shows the abs values.
	effWarn, effAct, effForce := keeper.EffectiveBandTokens(
		resolvedWarnAbs, resolvedActAbs, resolvedForceActAbs,
		resolvedWarnPctCeil, resolvedActPctCeil, windowSizeFlag)
	// hk-4gtu: BootGrace is fed at the Cycler construction site (never via
	// applyDefaults, per the opt-in-per-construction-site contract in thresholds.go).
	// When neither flag nor config set it, use DefaultBootGracePeriod (5m); when
	// set, honor the configured value VERBATIM including the 0 = disabled sentinel.
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
	// hk-4gtu: report the EFFECTIVE resolved timing/cadence/budget so a
	// misconfiguration (or an applied tunable) is visible at boot, not silent.
	fmt.Fprintf(os.Stderr,
		"keeper effective tunables: poll=%s idle_quiesce=%s staleness=%s handoff_timeout=%s boot_grace=%s "+
			"hard_ceiling=%d/%s idle_floor=%d no_gauge_backoff=%s heartbeat_max_misses=%d\n",
		resolved.PollInterval, resolved.IdleQuiesce, resolved.Staleness, resolved.HandoffTimeout, resolvedBootGrace,
		resolved.HardCeilingAbsTokens, resolved.HardCeilingMode, resolved.IdleRestartAbsTokens,
		resolved.NoGaugeBackoff, resolved.HeartbeatMaxMisses)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	emitter := keeper.NewFileEmitter(projectDir)

	// hk-yy57: build the Cycler/Watcher config LITERALS through the shared,
	// side-effect-free buildKeeperConfigs helper (resolve→construct seam). The
	// side-effecting NewCycler / RecoverFromCrash / NewWatcher calls stay here so
	// behaviour is byte-identical to the prior inline construction.
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

	// In warn-only mode skip the cycler entirely — no handoff/clear/resume cycles.
	// Refs: hk-yfcc.
	var cycler *keeper.Cycler
	if !warnOnlyFlag {
		cycler = keeper.NewCycler(cyclerCfg, emitter)

		// Crash recovery: if a previous keeper was killed mid-cycle, self-heal before
		// starting the watcher loop (resume any interrupted /clear, or abort cleanly).
		if recoverErr := cycler.RecoverFromCrash(ctx); recoverErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik keeper: crash recovery: %v\n", recoverErr)
		}
	}
	// Assign the constructed *Cycler (nil in warn-only mode) after crash recovery.
	cfg.Cycler = cycler

	w := keeper.NewWatcher(cfg, emitter)
	if runErr := w.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
		fmt.Fprintf(os.Stderr, "harmonik keeper: watcher: %v\n", runErr)
		return 1
	}

	return 0
}

// keeperForceRestartFn returns the ForceRestartFn to wire into CyclerConfig for
// the handoff-timeout hard-restart escalation (cycle.go:767). It is FAIL-CLOSED:
// nil — the escalation stays dormant and behaviour is byte-identical to today —
// UNLESS the operator BOTH opts in with --force-restart AND supplies a
// --respawn-cmd to launch from. The returned closure reuses
// NewLiveRecoverViaRespawn, which re-verifies the bound .sid identity at the
// moment of firing and refuses (returns ErrLiveRecoverIdentityUntrusted, no
// restart) on a non-UUIDv4 — force-restart is the most destructive keeper action.
// Refs: hk-suxt (wire dormant ForceRestartFn), hk-qoz (escalation path).
func keeperForceRestartFn(forceRestart bool, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if !forceRestart || respawnCmd == "" {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

// keeperLiveRecoverFn returns the LiveRecoverFn for the watcher. In warn-only
// mode this is always nil — crew keepers never trigger live-pane recovery.
// Outside warn-only mode it falls through to NewLiveRecoverViaRespawn (nil when
// --respawn-cmd is empty). Refs: hk-yfcc, hk-75mr.
func keeperLiveRecoverFn(warnOnly bool, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if warnOnly {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

// keeperHardCeilingRestartFn returns the HardCeilingRestartFn to wire into
// WatcherConfig for the SID-independent blind-path hard-ceiling auto-restart
// (watcher.go Backstop 2). It is FAIL-CLOSED: nil — the ceiling stays alarm-only,
// behaviour-identical to the dormant-failsafe state hk-746u captured — UNLESS the
// operator selects restart mode AND supplies a --respawn-cmd to launch from AND a
// durable pane target is resolvable. The returned closure reuses
// NewLiveRecoverViaRespawn, which re-verifies the bound .sid identity at the moment
// of firing and refuses (no restart) on a non-UUIDv4 — the auto-restart is the most
// destructive keeper action on the blind path. When mode != restart this is nil,
// so the gate (which calls the fn ONLY in restart mode) never reaches a non-restart
// path; the explicit mode check here is belt-and-suspenders. Refs: hk-z8d0 (wire
// the dormant ceiling restart), resolves hk-746u.
func keeperHardCeilingRestartFn(mode keeper.HardCeilingMode, tmuxTarget, projectDir, respawnCmd string) func(ctx context.Context, agentName string) error {
	if mode != keeper.HardCeilingModeRestart || respawnCmd == "" || tmuxTarget == "" {
		return nil
	}
	return keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmd)
}

// keeperOperatorWarnFn returns the OperatorWarnFn for WatcherConfig backstop B
// (hk-ehm8s): at each warn-threshold upward crossing the keeper emits an
// out-of-band notification via `harmonik comms send --to operator --topic
// keeper-warn`, so operators watching via remote-control / iOS / a detached pane
// see the warning before any ACT cycle fires. Best-effort: a comms-send error
// (e.g. daemon not running) is printed to stderr and pane injection continues
// unaffected. os.Executable resolves the current binary so the same binary
// handles the comms send (no PATH lookup needed). Refs: hk-ehm8s.
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

// newKeeperMarkerFlags builds the flag set shared by the keeper marker/action
// subcommands (set-dispatching, clear-dispatching, restart-now, ping): a
// --project override and the required --agent. Keeping the registration in one
// place guarantees parser parity and gives tests a single seam.
func newKeeperMarkerFlags(name string) (fs *flag.FlagSet, projectFlag, agentFlag *string) {
	fs = flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	projectFlag = fs.String("project", "", "project directory (default: current working directory)")
	agentFlag = fs.String("agent", "", "agent name (required)")
	return fs, projectFlag, agentFlag
}

// resolveKeeperAgent resolves the target agent for a keeper subcommand from the
// already-parsed flag set. FLAG-ONLY (hk-5da7): the agent MUST be supplied via
// --agent; ANY positional argument is rejected with exit 2. Positional args were
// the recurring restart-now failure mode (a positional silently took the place
// of --agent and routed to the wrong project), so they are no longer accepted.
// Returns (agent, 0) on success; ("", code) when the caller should return code
// (2 = unexpected positional/stray token, 1 = no --agent supplied).
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

// parseKeeperMarkerArgs parses args for a marker subcommand and resolves the
// target agent + project directory. Returns (agent, project, 0) on success, or
// ("", "", code) when the caller should return code immediately.
func parseKeeperMarkerArgs(label string, args []string) (agent, project string, code int) {
	fs, projectFlag, agentFlag := newKeeperMarkerFlags(label)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", "", 1
		}
		// Unrecognized flag (incl. a stray leading-dash token): loud exit 2.
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
	// W5 (hk-x7s / round-1 bug-2a): Abs-normalize the project dir so a relative
	// --project (or a worktree CWD) resolves to the SAME .harmonik/keeper/ dir the
	// watcher uses (it derives projectDir from os.Getwd(), always absolute). enable
	// and doctor already normalize; the marker verbs (set/clear/restart-now) skipped
	// it, so two commands could "agree" on --project yet touch different files. The
	// os.Getwd() default is intentionally KEPT (load-bearing for the live captain /
	// dispatch scripts — adversary 08 §W5); only the normalization is added.
	abs, err := normalizeProjectDir(label, project)
	if err != nil {
		return "", "", 1
	}
	project = abs
	return agent, project, 0
}

// normalizeProjectDir applies filepath.Abs to a keeper marker-verb project dir so
// every marker verb resolves to the SAME .harmonik/keeper/ directory the watcher
// uses (the watcher's projectDir comes from os.Getwd(), always absolute). It is
// the single chokepoint for the W5 relative-path parity fix; on error it logs to
// stderr and returns the error so the caller can return exit 1.
func normalizeProjectDir(label, project string) (string, error) {
	abs, err := filepath.Abs(project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik %s: cannot resolve project path %q: %v\n", label, project, err)
		return "", err
	}
	return abs, nil
}

// runKeeperSetDispatching implements `harmonik keeper set-dispatching <agent>`.
//
// Writes .harmonik/keeper/<agent>.dispatching so HoldingDispatch returns true.
// The orchestrator calls this before submitting a batch to the daemon queue so
// the session-keeper cycle defers the handoff action until dispatch completes.
//
// Exit codes:
//
//	0  — marker written successfully
//	1  — argument error, path-traversal validation failure, or I/O error
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-rc51s.
func runKeeperSetDispatching(args []string) int {
	agent, project, code := parseKeeperMarkerArgs("keeper set-dispatching", args)
	if agent == "" {
		return code
	}
	if err := keeper.SetDispatching(project, agent); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper set-dispatching: %v\n", err)
		return 1
	}
	// W5 (hk-x7s) fail-open WARNING: the marker write above always succeeds (exit 0),
	// even when no keeper is watching the resolved project+agent — so the operator can
	// get a green exit while the dispatch is actually unguarded. Emit a non-fatal
	// stderr warning when no live keeper holds the <agent>.lock for this dir, so
	// "I set the marker but nobody's watching" is visible. NOT a hard-fail: a keeper
	// may legitimately start later (adversary 08 §W5 — advisory only, exit stays 0).
	if !keeper.LiveKeeperPresent(project, agent) {
		fmt.Fprintf(os.Stderr,
			"keeper set-dispatching: WARNING — no live keeper found for agent %q under %q "+
				"(.dispatching marker written, but no watcher is currently guarding this dispatch; "+
				"start `harmonik keeper --agent %s` or verify --project)\n",
			agent, project, agent)
	}
	return 0
}

// runKeeperClearDispatching implements `harmonik keeper clear-dispatching <agent>`.
//
// Removes .harmonik/keeper/<agent>.dispatching so HoldingDispatch returns false.
// Idempotent: an already-absent marker is not an error. The orchestrator calls
// this once all in-flight queue work has completed so the session-keeper cycle
// may resume normal threshold checks.
//
// Exit codes:
//
//	0  — marker removed (or was already absent)
//	1  — argument error, path-traversal validation failure, or I/O error
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-rc51s.
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

// runKeeperHold implements `harmonik keeper hold --agent <name>`.
// Writes .harmonik/keeper/<agent>.hold.<sessionID> (keyed by the live .sid
// session-id, RFC3339-timestamped) so the keeper suspends the ACT/restart cutoff
// while the operator co-works. AUTO-REVERTS: the session-id is re-minted on /clear
// so the hold can never survive a restart, and a timer backstop expires it.
// WARN still fires under hold. Refs: hk-9waz.
//
// Exit codes: 0 — hold written; 1 — no trustworthy live session / arg / I/O error;
// 2 — unexpected positional (flag-only).
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
	// Fail-open advisory (mirrors set-dispatching): a hold with no live keeper
	// watching is meaningless — surface it but keep exit 0.
	if !keeper.LiveKeeperPresent(project, agent) {
		fmt.Fprintf(os.Stderr,
			"keeper hold: WARNING — no live keeper found for agent %q under %q "+
				"(hold marker written, but no watcher is currently guarding this agent; "+
				"start `harmonik keeper --agent %s`)\n", agent, project, agent)
	}
	fmt.Printf("keeper hold: agent=%q session=%s — ACT/restart suspended (WARN still fires; auto-reverts on restart or after the TTL). Run `harmonik keeper release --agent %s` to resume early.\n", agent, sid, agent)
	return 0
}

// runKeeperRelease implements `harmonik keeper release --agent <name>`.
// Removes the agent's hold marker(s) so normal keeper behavior resumes.
// Idempotent: an already-absent hold is not an error. Refs: hk-9waz.
//
// Exit codes: 0 — released (or already clear); 1 — arg/validation/I/O error;
// 2 — unexpected positional (flag-only).
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

// runKeeperRestartNow implements `harmonik keeper restart-now --agent <name>`.
//
// SIMPLIFIED (hk-5da7): this runs the restart SYNCHRONOUSLY in-process — verify
// the session id, ONE handoff-freshness check, inject an ACK line (so the agent
// can verify receipt), then inject /clear and /session-resume. It does NOT write
// a marker for a watcher to pick up — that indirection was the silent-no-op bug
// (marker written under the wrong project dir, watcher polled elsewhere). Every
// step logs at INFO/WARN and any failure returns a non-zero exit with the reason.
//
// Exit codes:
//
//	0  — restart driven (ack + /clear + /session-resume injected)
//	1  — argument error, no pane, unverifiable session id, missing/stale handoff,
//	     or an injection failure (the log names which step)
//	2  — unexpected positional argument (flag-only)
//
// Refs: hk-5da7, hk-wjzf, hk-xjlq, ON-059.
func runKeeperRestartNow(args []string) int {
	agent, projectFlag, code := parseKeeperMarkerArgs("keeper restart-now", args)
	if agent == "" {
		return code
	}

	// Resolve the tmux pane the same way the watcher does (convention-derived
	// when not explicit). restart-now has no --tmux flag — it is always the
	// agent's own pane.
	tmuxTarget := keeper.ResolveTmuxTarget(projectFlag, agent, "", nil)

	requestedAt := time.Now().UTC()
	nonce := restartNowNonce(requestedAt)
	err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
		ProjectDir:  projectFlag,
		AgentName:   agent,
		TmuxTarget:  tmuxTarget,
		RequestedAt: requestedAt,
	}, nonce)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper restart-now: %v\n", err)
		return 1
	}
	// Print the nonce so an external watcher can match the injected
	// '[KEEPER ACK <nonce>] received restart' line in the pane scrollback.
	fmt.Printf("keeper restart-now: agent=%q nonce=%s restart driven (ack + /clear + agent brief --wake keeper-restart injected into %q)\n",
		agent, nonce, tmuxTarget)
	return 0
}

// runKeeperPing implements `harmonik keeper ping --agent <name> [--nonce N]`.
//
// Injects ONLY the verifiability ACK line into the agent's pane (no /clear, no
// resume) so the agent can confirm the keeper is alive and reachable. Refs: hk-5da7.
//
// Exit codes:
//
//	0  — ack injected
//	1  — argument error, no pane, or injection failure
//	2  — unexpected positional argument (flag-only)
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
	// W5 (hk-x7s): Abs-normalize for parity with the watcher / enable / doctor.
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

// restartNowNonce derives a compact verifiability nonce from the request time.
// The agent that fired restart-now matches the [KEEPER ACK <nonce>] line on this
// token; uniqueness within a session is all that is required, so a millisecond
// timestamp suffices.
func restartNowNonce(t time.Time) string {
	return fmt.Sprintf("rn-%d", t.UnixMilli())
}

// runKeeperAwaitAck implements
// `harmonik keeper await-ack --agent <name> --nonce <N> [--kind restart|ping]
//
//	[--timeout 15s] [--poll 1s] [--project DIR]`.
//
// It is the AGENT-SIDE half of the ACK handshake (hk-uldg): resolve the agent's
// pane, poll `tmux capture-pane` every --poll for the exact line
// `[KEEPER ACK <nonce>]` until match (exit 0) or timeout. On timeout it emits a
// durable session_keeper_ack_timeout event (via the keeper FileEmitter) and
// exits 3. The BINARY does NOT send comms — the calling skill owns the comms
// alert (design §3, operator-confirmed).
//
// Exit codes:
//
//	0  — ack observed within the timeout (keeper proven alive)
//	1  — argument error (missing --agent / --nonce) or working-dir error
//	2  — unexpected positional argument or unrecognized flag (flag-only)
//	3  — timeout: no [KEEPER ACK <nonce>] observed (event emitted), OR no pane
//	     could be resolved, OR capture-pane failed repeatedly
//
// Refs: hk-uldg; design plans/2026-06-20-keeper-architecture-critique/18-design-agent-side-ack.md.
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
	// W5 (hk-x7s): Abs-normalize for parity with the watcher / enable / doctor.
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
		// Timeout (incl. no-pane / repeated capture failure): exit 3 so the
		// caller can distinguish it from flag misuse (2) and confirmed-alive (0).
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
  restart-now        Agent/captain-initiated SYNCHRONOUS clear→resume (hk-5da7).
                     Verifies the session id (lowercase UUIDv4), checks HANDOFF-<agent>.md
                     exists and is fresh (written within 10 min — run /session-handoff
                     first), then injects an ACK line, /clear, and /session-resume into
                     the agent's pane — all in THIS process, no marker, no watcher poll.
                     FAILS LOUDLY (non-zero exit + logged reason) on no pane, an
                     unverifiable session id, or a missing/stale handoff. The injected
                     '[KEEPER ACK <nonce>] received restart' line lets a watcher verify
                     receipt. Refs: hk-5da7 (was hk-wjzf/ON-059 marker path).
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
