package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/agentlaunch"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const captainSplashDismissDelay = 750 * time.Millisecond

const captainModel = "opus"

type captainLaunchRunFn func(cmd *exec.Cmd) error

type keeperEnableFn func(cfg enableConfig, stdout, stderr io.Writer) int

var captainReapPriorWatchers reapPriorAgentWatchersFn = reapPriorAgentWatchers

func runCaptainTmux(cmd *exec.Cmd) error { return cmd.Run() }

type captainTmuxOps interface {
	// SessionExists reports whether a tmux session named sess is live. Used by
	// the D7 pre-flight to decide reap-then-recreate vs. plain create.
	SessionExists(ctx context.Context, sess string) (bool, error)
	// KillSession tears down a stale session (D7 reap). Idempotent.
	KillSession(ctx context.Context, sess string) error
	// SpawnKeeperWindow adds the sibling keeper window running the keeper argv
	// built from opts. Returns the window Outcome (Err non-nil on failure —
	// the captain WARNS but stays bootable, mirroring the bash launcher).
	SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome
	// AgentPanePID reads the PID of the captain agent window's active pane, for
	// captain.pid. Returns (0, err) when the pane PID can't be resolved.
	AgentPanePID(ctx context.Context, sess string) (int, error)
	// AgentPaneAlive reports whether the captain agent window's pane is backed by
	// a LIVE process — i.e. there is a real captain running, not just a keeper
	// window keeping the tmux session alive. Used by the D7 pre-flight to
	// distinguish "reap a stale session (agent dead)" from "refuse to clobber a
	// live captain". Resolves the agent pane PID and signal-0 probes it. Returns
	// (false, err) only when liveness could not be determined at all; a resolvable
	// but dead/absent pane returns (false, nil).
	AgentPaneAlive(ctx context.Context, sess string) (bool, error)
	// PasteSeedToAgentPane delivers the boot seed to the captain's agent pane
	// after launch so the captain knows to run `harmonik agent brief`. Best-effort:
	// failures WARN to stderr but never block the launch (mirrors crewstart.go
	// pasteCrewMission, T10/hk-02jsj).
	PasteSeedToAgentPane(ctx context.Context, sessionID, paneTarget string)
}

type osCaptainTmuxOps struct {
	adapter ltmux.OSAdapter
}

func (o osCaptainTmuxOps) SessionExists(ctx context.Context, sess string) (bool, error) {
	sessions, err := o.adapter.ListSessions(ctx)
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s == sess {
			return true, nil
		}
	}
	return false, nil
}

func (o osCaptainTmuxOps) KillSession(ctx context.Context, sess string) error {
	return o.adapter.KillSession(ctx, sess)
}

func (o osCaptainTmuxOps) SpawnKeeperWindow(ctx context.Context, opts agentlaunch.KeeperWindowOpts) ltmux.Outcome {
	return agentlaunch.SpawnKeeperWindow(ctx, o.adapter, opts)
}

func (o osCaptainTmuxOps) AgentPanePID(ctx context.Context, sess string) (int, error) {
	return o.adapter.WindowPanePID(ctx, ltmux.WindowHandle(sess+":"+ltmux.WindowAgent))
}

// AgentPaneAlive resolves the agent pane PID and signal-0 probes it. A signal-0
// kill checks process existence WITHOUT delivering a signal: nil => the process
// is alive; an error (ESRCH / permission) => not a live, killable process.
//
// If the agent window is absent (the session exists but has no "agent" window —
// the keeper-outlived-the-agent case the D7 reap targets), WindowPanePID returns
// ErrNoSession / a zero pid: that is reported as (false, nil) — NOT a hard error
// — so the caller treats it as "agent dead, safe to reap". A genuine probe
// failure (couldn't even resolve the pid for some other reason) returns
// (false, err) so the caller can decide conservatively.
func (o osCaptainTmuxOps) AgentPaneAlive(ctx context.Context, sess string) (bool, error) {
	pid, err := o.AgentPanePID(ctx, sess)
	if err != nil {
		if errors.Is(err, ltmux.ErrNoSession) {
			return false, nil
		}
		return false, err
	}
	if pid <= 0 {
		return false, nil
	}
	if perr := syscall.Kill(pid, 0); perr != nil {
		switch {
		case errors.Is(perr, syscall.ESRCH):
			return false, nil
		case errors.Is(perr, syscall.EPERM):
			return true, nil
		default:
			return false, fmt.Errorf("probe agent pane pid %d in session %q: %w", pid, sess, perr)
		}
	}
	return true, nil
}

func captainBootBufferName(sessionID string) string {
	return ltmux.BufferName(sessionID, "captain-boot")
}

// PasteSeedToAgentPane delivers the boot seed to the captain's agent pane via
// the bracketed-paste mechanism (mirrors crewstart.go pasteCrewMission).
//
// Message: "Please run `harmonik agent brief` and begin your operating loop."
// HARMONIK_AGENT is set by the launcher so brief auto-resolves the captain type.
//
// Best-effort: errors WARN to stderr but never block the launch (T10/hk-02jsj).
func (o osCaptainTmuxOps) PasteSeedToAgentPane(ctx context.Context, sessionID, paneTarget string) {
	if err := o.adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed splash dismiss: %v\n", err)
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(captainSplashDismissDelay):
	}
	bufName := captainBootBufferName(sessionID)
	const bootSeedMsg = "Please run `harmonik agent brief` and begin your operating loop.\n"
	if err := o.adapter.WriteToPane(ctx, bufName, paneTarget, []byte(bootSeedMsg)); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed paste: %v\n", err)
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(captainSplashDismissDelay):
	}
	if err := o.adapter.SendKeysEnter(ctx, paneTarget); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: boot-seed submit: %v\n", err)
	}
}

func buildCaptainTmuxCmd(name, tmuxSession, sessionID, rcPrefix string) *exec.Cmd {
	return exec.Command(
		"tmux", "new-session", "-d",
		"-s", tmuxSession,
		"-n", ltmux.WindowAgent,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--model", captainModel,
		"--remote-control", crewrun.JoinRemoteControlName(rcPrefix, name),
		"--session-id", sessionID,
	)
}

func runCaptainSubcommand(subArgs []string) int {
	if len(subArgs) >= 1 && subArgs[0] == "respawn" {
		return runCaptainRespawnSubcommand(subArgs[1:])
	}
	return runCaptainLaunchWithOps(subArgs, runCaptainTmux, runKeeperEnable, osCaptainTmuxOps{adapter: ltmux.OSAdapter{}})
}

func buildCaptainKeeperConfig(name, projectDir string) (enableConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return enableConfig{}, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return enableConfig{
		agentName:    name,
		projectDir:   projectDir,
		scriptsDir:   autoDetectScriptsDir(projectDir),
		settingsPath: filepath.Join(home, ".claude", "settings.json"),
	}, nil
}

func ensureBootAssets(projectDir string, stdout, stderr io.Writer) error {
	if code := provisionSkills(projectDir, false, stdout, stderr); code != 0 {
		if _, err := fmt.Fprintf(stderr, "harmonik: warning: skill provisioning failed (code %d) — agent may lack .claude/skills/\n", code); err != nil {
			return err
		}
	}
	if code := provisionScaffolds(projectDir, false, stdout, stderr); code != 0 {
		if _, err := fmt.Fprintf(stderr, "harmonik: warning: scaffold provisioning failed (code %d)\n", code); err != nil {
			return err
		}
	}
	if code := provisionContextTiers(projectDir, false, stdout, stderr); code != 0 {
		if _, err := fmt.Fprintf(stderr, "harmonik: warning: context-tier provisioning failed (code %d)\n", code); err != nil {
			return err
		}
	}
	targetBranch := "main"
	if pc, err := projectconfig.LoadProjectConfig(projectDir); err == nil && pc.Daemon.TargetBranch != "" {
		targetBranch = pc.Daemon.TargetBranch
	}
	if code := renderAgentsMD(projectDir, targetBranch, false, stdout, stderr); code != 0 {
		if _, err := fmt.Fprintf(stderr, "harmonik: warning: AGENTS.md provisioning failed (code %d)\n", code); err != nil {
			return err
		}
	}
	return nil
}

func captainTmuxSessionName(explicitTmux, project string) (string, error) {
	if explicitTmux != "" {
		return explicitTmux, nil
	}
	realDir, err := filepath.EvalSymlinks(project)
	if err != nil {
		realDir = project
	}
	hash := lifecycle.ComputeProjectHash(realDir)
	return lifecycle.TmuxSessionName(hash, "captain"), nil
}

func runCaptainLaunchWithOps(subArgs []string, run captainLaunchRunFn, enableKeeper keeperEnableFn, ops captainTmuxOps) int {
	fs := flag.NewFlagSet("captain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity")
	tmuxFlag := fs.String("tmux", "", "tmux session name (default: harmonik-<project-hash>-captain)")
	projectFlag := fs.String("project", "", "project directory (default: current working directory)")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to launch with (minted when absent)")
	noKeeperFlag := fs.Bool("no-keeper", false, "skip wiring the keeper hooks into ~/.claude/settings.json")
	warnAbsFlag := fs.Int64("warn-abs-tokens", 0, "keeper WARN band (absolute tokens); 0 = unset → use operator config")
	actAbsFlag := fs.Int64("act-abs-tokens", 0, "keeper ACT/restart band (absolute tokens); 0 = unset → use operator config")
	const rcPrefixUnset = "\x00"
	rcPrefixFlag := fs.String("rc-prefix", rcPrefixUnset, "per-project --remote-control label prefix (default: daemon.remote_control_prefix from .harmonik/config.yaml; empty = bare label)")

	if err := fs.Parse(subArgs); err != nil {
		return 1
	}

	name := *nameFlag
	if name == "" {
		fmt.Fprintln(os.Stderr, "harmonik captain: --name must not be empty")
		return 1
	}

	project := *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: cannot determine working directory: %v\n", err)
			return 1
		}
		project = wd
	}

	rcPrefix := *rcPrefixFlag
	if rcPrefix == rcPrefixUnset {
		rcPrefix = ""
		if pc, perr := projectconfig.LoadProjectConfig(project); perr == nil {
			rcPrefix = pc.Daemon.RemoteControlPrefix
		} else {
			fmt.Fprintf(os.Stderr, "harmonik captain: could not load .harmonik/config.yaml for rc-prefix (%v) — launching with a bare --remote-control label\n", perr)
		}
	}

	tmuxSession, err := captainTmuxSessionName(*tmuxFlag, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: cannot resolve tmux session name: %v\n", err)
		return 1
	}

	sessionID := *sessionIDFlag
	if sessionID == "" {
		sessionID = uuid.New().String()
	} else if !keeper.IsPrimarySID(sessionID) {
		fmt.Fprintf(os.Stderr, "harmonik captain: --session-id %q is not a canonical lowercase UUIDv4 "+
			"(interactive captain/crew sessions use UUIDv4; the keeper's clear→resume cycle requires it)\n", sessionID)
		return 1
	}

	ctx := context.Background()

	if exists, lerr := ops.SessionExists(ctx, tmuxSession); lerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: could not check for an existing session %q: %v — proceeding\n", tmuxSession, lerr)
	} else if exists {
		alive, aerr := ops.AgentPaneAlive(ctx, tmuxSession)
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: could not determine whether the captain in tmux session %q is live (%v); "+
				"refusing to reap it. Stop it first (or pass --tmux <name> to run a second one).\n", tmuxSession, aerr)
			return 1
		}
		if alive {
			fmt.Fprintf(os.Stderr, "harmonik captain: a live captain is already running in tmux session %q; "+
				"stop it first (or pass --tmux <name> to run a second one).\n", tmuxSession)
			return 1
		}
		fmt.Printf("captain: existing tmux session %q found with a DEAD/absent agent pane (keeper outlived a stopped agent) — reaping before recreate (D7)\n", tmuxSession)
		if kerr := ops.KillSession(ctx, tmuxSession); kerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: failed to reap stale session %q: %v\n", tmuxSession, kerr)
			return 1
		}
	}

	captainReapPriorWatchers(name)

	if err := ensureBootAssets(project, os.Stdout, os.Stderr); err != nil {
		return 1
	}

	if !*noKeeperFlag {
		cfg, cerr := buildCaptainKeeperConfig(name, project)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: %v\n", cerr)
			return 1
		}
		if rc := enableKeeper(cfg, os.Stdout, os.Stderr); rc != 0 {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper enable returned %d — launching anyway; "+
				"run `harmonik keeper enable %s` manually to wire keeper hooks\n", rc, name)
		}
	}

	cmd := buildCaptainTmuxCmd(name, tmuxSession, sessionID, rcPrefix)
	if rerr := run(cmd); rerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: launch tmux session %q: %v\n", tmuxSession, rerr)
		return 1
	}

	agentPane := tmuxSession + ":" + ltmux.WindowAgent
	ops.PasteSeedToAgentPane(ctx, sessionID, agentPane)

	if werr := writeCaptainSentinelAndPID(ctx, ops, project, tmuxSession); werr != nil {
		fmt.Fprintf(os.Stderr, "harmonik captain: %v — the daemon orphan sweep may reap this captain "+
			"until the sentinel/pid is written; re-run the launcher or `harmonik supervise` to refresh\n", werr)
	}

	if !*noKeeperFlag {
		keeperBin, exErr := os.Executable()
		if exErr != nil {
			keeperBin = "harmonik" // fallback: rely on PATH
		}
		respawnCmd := captainRespawnCmdString(keeperBin, name, tmuxSession, sessionID, project)
		outcome := ops.SpawnKeeperWindow(ctx, agentlaunch.KeeperWindowOpts{
			KeeperBin:     keeperBin,
			AgentName:     name,
			Session:       tmuxSession,
			ProjectDir:    project,
			WarnOnly:      false, // captain is FORCE-CUT: full warn→act→restart band.
			WarnAbsTokens: *warnAbsFlag,
			ActAbsTokens:  *actAbsFlag,
			RespawnCmd:    respawnCmd, // hk-z1rj: `harmonik captain respawn …` (ES3).
		})
		if outcome.Err != nil {
			fmt.Fprintf(os.Stderr, "harmonik captain: keeper watcher window failed (%v) — launching anyway; "+
				"the captain has NO warn/act/restart watcher until you arm one manually with "+
				"`harmonik keeper --agent %s --tmux %s:%s` "+
				"(the band comes from the keeper: block in .harmonik/config.yaml — run "+
				"`harmonik keeper config --example` if it is unset)\n",
				outcome.Err, name, tmuxSession, ltmux.WindowAgent)
		}
	}

	fmt.Printf("captain launched: name=%q tmux=%q session_id=%s project=%q (keeper band from operator config)\n",
		name, tmuxSession, sessionID, project)
	if *noKeeperFlag {
		fmt.Printf("captain launched; keeper wiring skipped (--no-keeper) — run `harmonik keeper enable %s` and arm a watcher to wire warn/act.\n", name)
	} else {
		fmt.Printf("captain launched; keeper hooks wired + watcher armed in sibling '%s:%s' window (pass --no-keeper to skip).\n", tmuxSession, ltmux.WindowKeeper)
	}
	return 0
}

func writeCaptainSentinelAndPID(ctx context.Context, ops captainTmuxOps, project, tmuxSession string) error {
	cognitionDir := filepath.Join(project, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("create cognition dir %q: %w", cognitionDir, err)
	}
	sentinelPath := filepath.Join(cognitionDir, "captain.sentinel")
	if err := os.WriteFile(sentinelPath, []byte("schema_version=1\n"), 0o600); err != nil {
		return fmt.Errorf("write captain.sentinel: %w", err)
	}

	pid, perr := ops.AgentPanePID(ctx, tmuxSession)
	if perr != nil || pid <= 0 {
		return fmt.Errorf("captain.sentinel written but could not resolve agent pane PID for captain.pid: %w", perr)
	}
	pidPath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		return fmt.Errorf("write captain.pid: %w", err)
	}
	return nil
}
