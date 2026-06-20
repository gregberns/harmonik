package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// runKeeperSubcommand implements `harmonik keeper`.
//
// Flags:
//
//	--agent <name>        agent name (required); identifies the lockfile and .managed marker
//	--tmux <target>       tmux pane target (optional; injected into on warn/act crossing)
//	--warn-pct N          context-use percentage that triggers a warning (default 80)
//	--act-pct N           context-use percentage that triggers handoff action (default 90; .managed-gated)
//	--window-size N       assumed context-window token size when gauge reports WindowSize==0 (default 200000)
//	--warn-abs-tokens N   absolute-token warn threshold (default 200000)
//	--act-abs-tokens N    absolute-token act threshold (default 215000)
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
	)

	fs.StringVar(&agentFlag, "agent", "", "agent name (required)")
	fs.StringVar(&tmuxFlag, "tmux", "", "tmux pane target (optional; injected into on warn crossing)")
	fs.IntVar(&warnPctFlag, "warn-pct", 80, "context-use percentage that triggers a warning")
	fs.IntVar(&actPctFlag, "act-pct", 90, "context-use percentage that triggers handoff action (.managed-gated)")
	fs.Int64Var(&windowSizeFlag, "window-size", 0, "assumed context-window token size when the gauge reports WindowSize==0 (default 200000)")
	fs.Int64Var(&warnAbsTokensFlag, "warn-abs-tokens", 0, "absolute-token warn threshold (default 200000)")
	fs.Int64Var(&actAbsTokensFlag, "act-abs-tokens", 0, "absolute-token act threshold (default 215000)")
	fs.StringVar(&respawnCmdFlag, "respawn-cmd", "", "shell command to re-launch the agent after it exits (supervised respawn path; hk-3w2)")
	fs.BoolVar(&forceRestartFlag, "force-restart", false, "opt in to the handoff-timeout hard-restart escalation (fail-closed; requires --respawn-cmd; hk-suxt)")

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
		fmt.Fprintf(os.Stderr, "keeper: project config: %v (ignoring; using defaults)\n", projCfgErr)
	}
	keeperCfg := projCfg.Keeper

	// Detect which threshold flags were explicitly set by the caller so we can
	// distinguish "caller passed 0" from "caller omitted the flag".
	var absWarnSet, absActSet bool
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "warn-abs-tokens":
			absWarnSet = true
		case "act-abs-tokens":
			absActSet = true
		}
	})

	// Resolve effective thresholds: CLI flag > config.yaml > compiled default (0 → applyDefaults).
	resolvedWarnAbs := warnAbsTokensFlag
	if !absWarnSet && keeperCfg.WarnAbsTokens > 0 {
		resolvedWarnAbs = keeperCfg.WarnAbsTokens
	}
	resolvedActAbs := actAbsTokensFlag
	if !absActSet && keeperCfg.ActAbsTokens > 0 {
		resolvedActAbs = keeperCfg.ActAbsTokens
	}
	// force_act_abs_tokens has no CLI flag; config wins over computed default.
	resolvedForceActAbs := int64(0)
	if keeperCfg.ForceActAbsTokens > 0 {
		resolvedForceActAbs = keeperCfg.ForceActAbsTokens
	}
	// pct ceils: an explicitly-set --warn-pct/--act-pct WINS (hk-5da7, honored as
	// pct/100), else config.yaml, else compiled default (0 → applyDefaults fills
	// 0.70/0.85). Honoring the CLI pct as the ceil is what makes the flag effective
	// on a 1M window: min(abs, (pct/100)*window) fires whichever is earlier.
	resolvedActPctCeil := keeperCfg.ActPctCeil   // 0 if not set → applyDefaults fills 0.85
	resolvedWarnPctCeil := keeperCfg.WarnPctCeil // 0 if not set → applyDefaults fills 0.70
	if warnPctSet {
		resolvedWarnPctCeil = float64(warnPctFlag) / 100.0
	}
	if actPctSet {
		resolvedActPctCeil = float64(actPctFlag) / 100.0
	}

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

	// Step 2: run doctor at boot as a loud diagnostic (non-fatal).
	{
		home, homeErr := os.UserHomeDir()
		if homeErr == nil {
			settingsPath := home + "/.claude/settings.json"
			runKeeperDoctorAtBoot(projectDir, agentFlag, settingsPath)
		}
	}

	// Step 3: check .managed opt-in guard (fail-safe: absent = no-op).
	if !keeper.IsManaged(projectDir, agentFlag) {
		fmt.Fprintf(os.Stderr, "keeper: %s not opted-in (.managed marker missing); no-op\n", agentFlag)
		return 0
	}

	// Step 4: resolve the effective tmux target.
	// If --tmux was provided, use it as-is. Otherwise attempt to auto-derive the
	// session name from the harmonik convention: "harmonik-<hash12>-<agent>".
	resolvedTmux := keeper.ResolveTmuxTarget(projectDir, agentFlag, tmuxFlag, nil)
	if resolvedTmux != "" && resolvedTmux != tmuxFlag {
		fmt.Fprintf(os.Stderr, "keeper: auto-resolved tmux target from convention: %q\n", resolvedTmux)
	}

	// Step 5: agent is managed — start the watcher and block until signal.
	fmt.Fprintf(os.Stderr, "keeper started for %s (warn-pct=%d, act-pct=%d, tmux=%q)\n",
		agentFlag, warnPctFlag, actPctFlag, resolvedTmux)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	emitter := keeper.NewFileEmitter(projectDir)

	cycler := keeper.NewCycler(keeper.CyclerConfig{
		AgentName:         agentFlag,
		ProjectDir:        projectDir,
		TmuxTarget:        resolvedTmux,
		ActPct:            float64(actPctFlag),
		ActAbsTokens:      resolvedActAbs,
		WarnAbsTokens:     resolvedWarnAbs,
		ForceActAbsTokens: resolvedForceActAbs,
		ActPctCeil:        resolvedActPctCeil,
		WarnPctCeil:       resolvedWarnPctCeil,
		SendEscapeFn:      keeper.SendEscapeKey,
		BootGracePeriod:   keeper.DefaultBootGracePeriod, // young-session guard (hk-4f8/hk-8hr1): defer cycles during post-/session-resume boot
		// hk-suxt: activate the handoff-timeout hard-restart escalation
		// (cycle.go:767, dormant until now because CyclerConfig.ForceRestartFn was
		// never populated in production). Fail-closed: nil unless the operator BOTH
		// opts in with --force-restart AND supplies a --respawn-cmd to launch from.
		// MaxHandoffTimeouts defaults to 3 (applyDefaults), so a non-nil fn alone
		// enables the escalation. Thresholds are unchanged (operator HARD-NO).
		ForceRestartFn: keeperForceRestartFn(forceRestartFlag, projectDir, respawnCmdFlag),
	}, emitter)

	// Crash recovery: if a previous keeper was killed mid-cycle, self-heal before
	// starting the watcher loop (resume any interrupted /clear, or abort cleanly).
	if recoverErr := cycler.RecoverFromCrash(ctx); recoverErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik keeper: crash recovery: %v\n", recoverErr)
	}

	cfg := keeper.WatcherConfig{
		AgentName:          agentFlag,
		ProjectDir:         projectDir,
		WarnPct:            float64(warnPctFlag),
		TmuxTarget:         resolvedTmux,
		Cycler:             cycler,
		FallbackWindowSize: windowSizeFlag,
		WarnAbsTokens:      resolvedWarnAbs,
		WarnPctCeil:        resolvedWarnPctCeil,
		RespawnCmd:         respawnCmdFlag,
		// hk-75mr: gauge-INDEPENDENT live-pane recovery. When the gauge is stale
		// past LiveRecoverGrace (default 5m) but the pane is still ALIVE (agent hung
		// mid-turn), no operator is attached, the agent is not blocked on a
		// decision, and the bound .sid identity is a valid UUIDv4, fire a gated
		// ForceRestart via the operator-supplied --respawn-cmd (the same launch
		// command the idle-respawn path uses; the closure re-verifies identity and
		// refuses on a non-UUIDv4 .sid). Nil when --respawn-cmd is empty → disabled.
		LiveRecoverFn: keeper.NewLiveRecoverViaRespawn(projectDir, respawnCmdFlag),
		// Warn text overrides from config.yaml (empty = use compiled defaults).
		DefaultWarnText:  keeperCfg.DefaultWarnText,
		OnDemandWarnText: keeperCfg.OnDemandWarnText,
		// hitl-decisions K5 (hk-061): the keeper watch tick is the SOLE emitter of
		// decision_withdrawn(orphaned, by=keeper). Enable the orphan reaper on the
		// standalone keeper; it reuses the FileEmitter (appends to the same
		// events.jsonl) and derives EventsJSONLPath from ProjectDir in applyDefaults.
		ReapDecisions: true,
		// hk-81wk: keep the gauge live independent of statusLine repaint. Once the
		// gauge ages toward Staleness while the tmux pane is still alive, the watcher
		// re-writes .ctx with a fresh ts (transcript-derived token count when
		// available), so a live agent's gauge NEVER goes stale — the dominant
		// no_gauge:stale failure that killed both keeper triggers.
		HeartbeatEnabled: true,
	}
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
	return agent, project, 0
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
	fmt.Printf("keeper restart-now: agent=%q nonce=%s restart driven (ack + /clear + /session-resume injected into %q)\n",
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

const keeperTopUsage = `harmonik keeper — context watcher for a managed agent pane (session-keeper, hk-ekap1)

USAGE
  harmonik keeper --agent <name> [--tmux <target>] [--warn-pct N] [--act-pct N] [--warn-abs-tokens N] [--act-abs-tokens N]
  harmonik keeper enable <agent> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]
  harmonik keeper doctor <agent> [--project DIR]
  harmonik keeper set-dispatching --agent <name> [--project DIR]
  harmonik keeper clear-dispatching --agent <name> [--project DIR]
  harmonik keeper restart-now --agent <name> [--project DIR]
  harmonik keeper ping --agent <name> [--nonce N] [--project DIR]

  FLAG-ONLY (hk-5da7): every verb (and the bare watcher) names the agent ONLY via
  --agent. A positional argument is rejected with exit 2 (positionals were the
  recurring restart-now failure mode); an unrecognized flag also exits 2.

VERBS
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

FLAGS (watcher mode)
  --agent <name>         Agent name (required); identifies the lockfile and .managed marker
  --tmux <target>        tmux pane target (optional; injected into on warn/act-pct crossing)
  --warn-pct N           Context-use percentage that triggers a warning (default 80)
  --act-pct N            Context-use percentage that triggers handoff action (default 90; .managed-gated)
  --warn-abs-tokens N    Absolute-token warn threshold (default 200000); effective = min(warn-abs-tokens, warn-pct% * window)
  --act-abs-tokens N     Absolute-token act threshold (default 215000); effective = min(act-abs-tokens, act-pct% * window)
  --respawn-cmd <cmd>    Shell command to re-launch the agent when it exits (supervised respawn; hk-3w2).
                         After the gauge goes stale for 20s and the tmux pane is idle (shell prompt),
                         the keeper runs "sh -c <cmd>" to respawn the agent. Requires --tmux.
                         A 90s cooldown prevents tight respawn loops.
                         Example: --respawn-cmd '~/.claude/captain-tools/captain-launch.sh'
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
  harmonik keeper --agent orchestrator
  harmonik keeper --agent flywheel --tmux harmonik:0 --warn-abs-tokens 200000 --act-abs-tokens 215000
  harmonik keeper set-dispatching --agent orchestrator
  harmonik keeper clear-dispatching --agent orchestrator
  harmonik keeper set-dispatching --agent flywheel --project /path/to/project
  harmonik keeper restart-now --agent captain
  harmonik keeper ping --agent captain --nonce check-001
`
