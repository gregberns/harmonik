package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/keeper"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

type captainRespawnRunFn func(cmd *exec.Cmd) ([]byte, error)

func runCaptainRespawnTmux(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }

func buildCaptainRespawnWindowCmd(name, tmuxTarget, sessionID, rcPrefix string) *exec.Cmd {
	//nolint:gosec // G204: executable and argv shape are fixed; dynamic values remain distinct argv elements
	return exec.CommandContext(context.Background(),
		"tmux", "respawn-window", "-k",
		"-t", tmuxTarget,
		"-e", "HARMONIK_AGENT="+name,
		"claude", "--dangerously-skip-permissions",
		"--model", captainModel,
		"--remote-control", crewrun.JoinRemoteControlName(rcPrefix, name),
		"--resume", sessionID,
	)
}

func buildCaptainPanePIDCmd(tmuxTarget string) *exec.Cmd {
	return exec.CommandContext(context.Background(),
		"tmux", "display-message", "-p",
		"-t", tmuxTarget,
		"#{pane_pid}",
	)
}

func captainRespawnTarget(tmuxFlag string) string {
	if strings.Contains(tmuxFlag, ":") {
		return tmuxFlag
	}
	return tmuxFlag + ":" + ltmux.WindowAgent
}

func runCaptainRespawnSubcommand(subArgs []string) int {
	return runCaptainRespawn(subArgs, runCaptainRespawnTmux, os.Stdout, os.Stderr)
}

func runCaptainRespawn(subArgs []string, run captainRespawnRunFn, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("captain respawn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nameFlag := fs.String("name", "captain", "captain --remote-control / comms identity to respawn")
	tmuxFlag := fs.String("tmux", "", "tmux session (or session:window) target of the captain agent window")
	sessionIDFlag := fs.String("session-id", "", "stable UUIDv4 session id to --resume (NOT --session-id: resume keeps the same conversation)")
	projectFlag := fs.String("project", "", "project directory holding .harmonik/cognition/captain.pid (default: current working directory)")
	const rcPrefixUnset = "\x00"
	rcPrefixFlag := fs.String("rc-prefix", rcPrefixUnset, "per-project --remote-control label prefix (default: daemon.remote_control_prefix from .harmonik/config.yaml; empty = bare label)")

	if err := fs.Parse(subArgs); err != nil {
		return 1
	}

	name := *nameFlag
	if name == "" {
		if _, err := fmt.Fprintln(stderr, "harmonik captain respawn: --name must not be empty"); err != nil {
			return 1
		}
		return 1
	}
	if *tmuxFlag == "" {
		if _, err := fmt.Fprintln(stderr, "harmonik captain respawn: --tmux is required (the captain agent-window target)"); err != nil {
			return 1
		}
		return 1
	}
	sessionID := *sessionIDFlag
	if sessionID == "" {
		if _, err := fmt.Fprintln(stderr, "harmonik captain respawn: --session-id is required (the minted SID to --resume)"); err != nil {
			return 1
		}
		return 1
	}
	if !keeper.IsPrimarySID(sessionID) {
		if _, err := fmt.Fprintf(stderr, "harmonik captain respawn: --session-id %q is not a canonical lowercase UUIDv4 "+
			"(the keeper's resume binding requires it)\n", sessionID); err != nil {
			return 1
		}
		return 1
	}

	project := *projectFlag
	if project == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik captain respawn: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
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
			if _, writeErr := fmt.Fprintf(stderr, "harmonik captain respawn: could not load .harmonik/config.yaml for rc-prefix (%v) — respawning with a bare --remote-control label\n", perr); writeErr != nil {
				return 1
			}
		}
	}

	target := captainRespawnTarget(*tmuxFlag)

	if _, err := run(buildCaptainRespawnWindowCmd(name, target, sessionID, rcPrefix)); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik captain respawn: respawn-window %q: %v\n", target, err); writeErr != nil {
			return 1
		}
		return 1
	}

	if err := refreshCaptainPID(run, project, target); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik captain respawn: %v — the daemon orphan sweep may reap this captain "+
			"until captain.pid is refreshed\n", err); writeErr != nil {
			return 1
		}
	}

	if _, err := fmt.Fprintf(stdout, "captain respawn: agent window %q relaunched with --resume %s (agent window only, keeper window survives, no dup keeper)\n", target, sessionID); err != nil {
		return 1
	}
	return 0
}

func refreshCaptainPID(run captainRespawnRunFn, project, target string) error {
	out, err := run(buildCaptainPanePIDCmd(target))
	if err != nil {
		return fmt.Errorf("read agent pane PID for captain.pid: %w", err)
	}
	pid := strings.TrimSpace(string(out))
	if pid == "" {
		return fmt.Errorf("read agent pane PID for captain.pid: empty pane_pid")
	}
	cognitionDir := filepath.Join(project, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("create cognition dir %q: %w", cognitionDir, err)
	}
	pidPath := filepath.Join(cognitionDir, "captain.pid")
	if err := os.WriteFile(pidPath, []byte(pid+"\n"), 0o600); err != nil {
		return fmt.Errorf("write captain.pid: %w", err)
	}
	return nil
}

func captainRespawnCmdString(keeperBin, name, tmuxSession, sessionID, project string) string {
	agentTarget := tmuxSession + ":" + ltmux.WindowAgent
	argv := []string{
		keeperBin, "captain", "respawn",
		"--name", name,
		"--tmux", agentTarget,
		"--session-id", sessionID,
		"--project", project,
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuoteRespawnArg(a)
	}
	return strings.Join(quoted, " ")
}

func shellQuoteRespawnArg(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`*?[]&;|<>(){}") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
