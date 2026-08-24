package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

var knownLiveAgents = map[string]bool{
	"flywheel":      true,
	"named-queues":  true,
	"controlpoints": true,
}

type enableConfig struct {
	agentName      string
	projectDir     string // harmonik project root
	scriptsDir     string // directory containing keeper-*.sh
	tmuxTarget     string // optional tmux pane target
	settingsPath   string // absolute path to ~/.claude/settings.json
	yesDestructive bool   // gate for .managed creation and known-live agents
}

func runKeeperEnableSubcommand(args []string) int {
	return runKeeperEnableEntry(args, os.Stdout, os.Stderr)
}

type enableArgs struct {
	agentName      string
	projectDir     string
	scriptsDir     string
	tmuxTarget     string
	yesDestructive bool
}

func parseKeeperEnableArgs(args []string, stdout, stderr io.Writer) (enableArgs, int) {
	var (
		pa        enableArgs
		agentFlag string
	)

	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if _, err := fmt.Fprint(stdout, keeperEnableUsage); err != nil {
				return enableArgs{}, 1
			}
			return enableArgs{}, 0
		case args[i] == "--agent" && i+1 < len(args):
			i++
			agentFlag = args[i]
		case strings.HasPrefix(args[i], "--agent="):
			agentFlag = strings.TrimPrefix(args[i], "--agent=")
		case args[i] == "--project" && i+1 < len(args):
			i++
			pa.projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			pa.projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--scripts-dir" && i+1 < len(args):
			i++
			pa.scriptsDir = args[i]
		case strings.HasPrefix(args[i], "--scripts-dir="):
			pa.scriptsDir = strings.TrimPrefix(args[i], "--scripts-dir=")
		case args[i] == "--tmux" && i+1 < len(args):
			i++
			pa.tmuxTarget = args[i]
		case strings.HasPrefix(args[i], "--tmux="):
			pa.tmuxTarget = strings.TrimPrefix(args[i], "--tmux=")
		case args[i] == "--yes-destructive":
			pa.yesDestructive = true
		case strings.HasPrefix(args[i], "-"):
			if _, err := fmt.Fprintf(stderr, "harmonik keeper enable: unrecognized flag %q\n", args[i]); err != nil {
				return enableArgs{}, 1
			}
			if _, err := fmt.Fprint(stderr, keeperEnableUsage); err != nil {
				return enableArgs{}, 1
			}
			return enableArgs{}, 2
		default:
			rest = append(rest, args[i])
		}
	}

	if len(rest) > 0 {
		if _, err := fmt.Fprintf(stderr,
			"harmonik keeper enable: unexpected positional argument(s) %q — this command is flag-only; use --agent <name>\n",
			strings.Join(rest, " ")); err != nil {
			return enableArgs{}, 1
		}
		if _, err := fmt.Fprint(stderr, keeperEnableUsage); err != nil {
			return enableArgs{}, 1
		}
		return enableArgs{}, 2
	}

	pa.agentName = agentFlag
	if pa.agentName == "" {
		if _, err := fmt.Fprintln(stderr, "harmonik keeper enable: --agent <name> is required"); err != nil {
			return enableArgs{}, 1
		}
		if _, err := fmt.Fprint(stderr, keeperEnableUsage); err != nil {
			return enableArgs{}, 1
		}
		return enableArgs{}, 1
	}
	return pa, -1
}

func runKeeperEnableEntry(args []string, stdout, stderr io.Writer) int {
	pa, code := parseKeeperEnableArgs(args, stdout, stderr)
	if code != -1 {
		return code
	}
	agentName := pa.agentName
	projectDir := pa.projectDir
	scriptsDir := pa.scriptsDir
	tmuxTarget := pa.tmuxTarget
	yesDestructive := pa.yesDestructive

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik keeper enable: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik keeper enable: cannot resolve project path %q: %v\n", projectDir, err); writeErr != nil {
			return 1
		}
		return 1
	}
	projectDir = absProject

	if scriptsDir == "" {
		scriptsDir = autoDetectScriptsDir(projectDir)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik keeper enable: cannot determine home directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	cfg := enableConfig{
		agentName:      agentName,
		projectDir:     projectDir,
		scriptsDir:     scriptsDir,
		tmuxTarget:     tmuxTarget,
		settingsPath:   settingsPath,
		yesDestructive: yesDestructive,
	}
	return runKeeperEnable(cfg, stdout, stderr)
}

func runKeeperEnable(cfg enableConfig, stdout, stderr io.Writer) int {
	if strings.Contains(cfg.agentName, "/") || strings.Contains(cfg.agentName, "..") {
		if err := keeperWritef(stderr, "harmonik keeper enable: agent name %q must not contain '/' or '..'\n", cfg.agentName); err != nil {
			return 1
		}
		return 1
	}
	if cfg.agentName == "" {
		if err := keeperWritef(stderr, "harmonik keeper enable: agent name is required\n"); err != nil {
			return 1
		}
		return 1
	}

	if knownLiveAgents[cfg.agentName] && !cfg.yesDestructive {
		if err := keeperWritef(stderr,
			"harmonik keeper enable: %q is a known live agent (flywheel/named-queues/controlpoints).\n"+
				"Wiring keeper hooks for a live session carries risk: a misconfigured .managed marker\n"+
				"could trigger /clear on an active orchestrator session.\n"+
				"Pass --yes-destructive to proceed.\n",
			cfg.agentName); err != nil {
			return 1
		}
		return 1
	}

	if cfg.scriptsDir == "" {
		if err := keeperWritef(stderr,
			"harmonik keeper enable: cannot locate scripts directory.\n"+
				"Pass --scripts-dir=/path/to/harmonik/scripts (the scripts/ directory in the harmonik repo).\n"); err != nil {
			return 1
		}
		return 1
	}
	requiredScripts := []string{
		"keeper-statusline.sh",
		"keeper-stop-hook.sh",
		"keeper-precompact-hook.sh",
		"keeper-sessionstart-hook.sh",
	}
	for _, name := range requiredScripts {
		p := filepath.Join(cfg.scriptsDir, name)
		if _, err := os.Stat(p); err != nil {
			if writeErr := keeperWritef(stderr, "harmonik keeper enable: script not found: %s\n  (pass --scripts-dir to specify the harmonik scripts/ directory)\n", p); writeErr != nil {
				return 1
			}
			return 1
		}
	}

	settings, err := readGlobalSettings(cfg.settingsPath)
	if err != nil {
		if writeErr := keeperWritef(stderr, "harmonik keeper enable: read %s: %v\n", cfg.settingsPath, err); writeErr != nil {
			return 1
		}
		return 1
	}

	if _, statErr := os.Stat(cfg.settingsPath); statErr == nil {
		backupPath := fmt.Sprintf("%s.bak-%s", cfg.settingsPath, time.Now().UTC().Format("20060102T150405Z"))
		if copyErr := copyFile(cfg.settingsPath, backupPath); copyErr != nil {
			if writeErr := keeperWritef(stderr, "harmonik keeper enable: backup %s → %s: %v\n", cfg.settingsPath, backupPath, copyErr); writeErr != nil {
				return 1
			}
			return 1
		}
		if err := keeperWritef(stdout, "keeper enable: backed up settings.json → %s\n", backupPath); err != nil {
			return 1
		}
	}

	statusLineCmd := filepath.Join(cfg.scriptsDir, "keeper-statusline.sh")
	stopHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-stop-hook.sh"))
	precompactHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-precompact-hook.sh"))
	sessionStartHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-sessionstart-hook.sh"))

	statusLineAction := mergeStatusLineStanza(settings, statusLineCmd)
	stopAction := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir, stopHookCmd)
	precompactAction := mergeHookStanza(settings, "PreCompact", "keeper-precompact-hook.sh", cfg.projectDir, precompactHookCmd)
	sessionStartAction := mergeHookStanza(settings, "SessionStart", "keeper-sessionstart-hook.sh", cfg.projectDir, sessionStartHookCmd)

	if _, err := fmt.Fprintf(stdout, "keeper enable: statusLine     — %s\n", statusLineAction); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "keeper enable: Stop hook      — %s\n", stopAction); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "keeper enable: PreCompact hook — %s\n", precompactAction); err != nil {
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "keeper enable: SessionStart hook — %s\n", sessionStartAction); err != nil {
		return 1
	}

	if err := writeGlobalSettings(cfg.settingsPath, settings); err != nil {
		if writeErr := keeperWritef(stderr, "harmonik keeper enable: write %s: %v\n", cfg.settingsPath, err); writeErr != nil {
			return 1
		}
		return 1
	}
	if _, err := fmt.Fprintf(stdout, "keeper enable: wrote %s\n", cfg.settingsPath); err != nil {
		return 1
	}

	handoffPath := filepath.Join(cfg.projectDir, fmt.Sprintf("HANDOFF-%s.md", cfg.agentName))
	if _, err := os.Stat(handoffPath); os.IsNotExist(err) {
		if writeErr := writeHandoffStub(handoffPath, cfg.agentName); writeErr != nil {
			if err := keeperWritef(stderr, "harmonik keeper enable: seed handoff stub: %v\n", writeErr); err != nil {
				return 1
			}
			return 1
		}
		if _, err := fmt.Fprintf(stdout, "keeper enable: seeded %s\n", handoffPath); err != nil {
			return 1
		}
	} else {
		if _, err := fmt.Fprintf(stdout, "keeper enable: %s already exists — skipping handoff seed\n", handoffPath); err != nil {
			return 1
		}
	}

	if cfg.tmuxTarget != "" {
		ok, checkErr := tmuxPaneExists(cfg.tmuxTarget)
		if checkErr != nil {
			if _, err := fmt.Fprintf(stdout, "keeper enable: tmux check skipped (%v)\n", checkErr); err != nil {
				return 1
			}
		} else if !ok {
			if _, err := fmt.Fprintf(stdout,
				"keeper enable: WARNING — tmux pane %q not found or not named.\n"+
					"  Name the pane: tmux rename-window -t %s <agent-name>\n",
				cfg.tmuxTarget, cfg.tmuxTarget); err != nil {
				return 1
			}
		} else {
			if _, err := fmt.Fprintf(stdout, "keeper enable: tmux pane %q is live\n", cfg.tmuxTarget); err != nil {
				return 1
			}
		}
	}

	if cfg.yesDestructive {
		managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".managed")
		if err := os.MkdirAll(filepath.Dir(managedPath), core.HarmonikDirMode); err != nil {
			if _, writeErr := fmt.Fprintf(stderr, "harmonik keeper enable: create keeper dir: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, err := os.Stat(managedPath); os.IsNotExist(err) {
			if writeErr := os.WriteFile(managedPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); writeErr != nil {
				if _, err := fmt.Fprintf(stderr, "harmonik keeper enable: create .managed: %v\n", writeErr); err != nil {
					return 1
				}
				return 1
			}
			if _, err := fmt.Fprintln(stdout, "keeper enable: created .managed marker (DESTRUCTIVE CONSENT — handoff cycle is now LIVE)"); err != nil {
				return 1
			}
		} else {
			if _, err := fmt.Fprintln(stdout, "keeper enable: .managed already present"); err != nil {
				return 1
			}
		}
	} else {
		if _, err := fmt.Fprintf(stdout,
			"\nkeeper enable: .managed NOT created (handoff cycle is passive).\n"+
				"  To enable LIVE handoff (DESTRUCTIVE — triggers /clear + resume):\n"+
				"    harmonik keeper enable %s --yes-destructive ...\n"+
				"  Or manually: touch %s/.harmonik/keeper/%s.managed\n",
			cfg.agentName, cfg.projectDir, cfg.agentName); err != nil {
			return 1
		}
	}

	runCmd := buildKeeperRunCmd(cfg)
	if err := keeperWritef(stdout, "\nkeeper enable: to start the keeper, run:\n  %s\n", runCmd); err != nil {
		return 1
	}

	return 0
}

func keeperWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

type doctorConfig struct {
	agentName    string
	projectDir   string
	settingsPath string
	// paneExistsFn is injected in tests to avoid real tmux calls.
	// When nil, tmuxPaneExists is used.
	paneExistsFn func(target string) (bool, error)
	// liveKeeperFn is injected in tests to avoid real flock calls.
	// When nil, keeper.LiveKeeperPresent is used.
	liveKeeperFn func(projectDir, agent string) bool
	// resolveTargetFn is injected in tests to avoid real tmux session probes.
	// When nil, keeper.ResolveTmuxTarget(projectDir, agentName, "", nil) is used.
	resolveTargetFn func(projectDir, agentName string) string
}

func runKeeperDoctorSubcommand(args []string) int {
	return runKeeperDoctorEntry(args, os.Stdout, os.Stderr)
}

type doctorArgs struct {
	agentName  string
	projectDir string
}

func parseKeeperDoctorArgs(args []string, stdout, stderr io.Writer) (doctorArgs, int) {
	var (
		da        doctorArgs
		agentFlag string
	)
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			if err := keeperWritef(stdout, "%s", keeperDoctorUsage); err != nil {
				return doctorArgs{}, 1
			}
			return doctorArgs{}, 0
		case args[i] == "--agent" && i+1 < len(args):
			i++
			agentFlag = args[i]
		case strings.HasPrefix(args[i], "--agent="):
			agentFlag = strings.TrimPrefix(args[i], "--agent=")
		case args[i] == "--project" && i+1 < len(args):
			i++
			da.projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			da.projectDir = strings.TrimPrefix(args[i], "--project=")
		case strings.HasPrefix(args[i], "-"):
			if err := keeperWritef(stderr, "harmonik keeper doctor: unrecognized flag %q\n", args[i]); err != nil {
				return doctorArgs{}, 1
			}
			if err := keeperWritef(stderr, "%s", keeperDoctorUsage); err != nil {
				return doctorArgs{}, 1
			}
			return doctorArgs{}, 2
		default:
			rest = append(rest, args[i])
		}
	}

	if len(rest) > 0 {
		if err := keeperWritef(stderr,
			"harmonik keeper doctor: unexpected positional argument(s) %q — this command is flag-only; use --agent <name>\n",
			strings.Join(rest, " ")); err != nil {
			return doctorArgs{}, 1
		}
		if err := keeperWritef(stderr, "%s", keeperDoctorUsage); err != nil {
			return doctorArgs{}, 1
		}
		return doctorArgs{}, 2
	}

	da.agentName = agentFlag
	if da.agentName == "" {
		if err := keeperWritef(stderr, "harmonik keeper doctor: --agent <name> is required\n"); err != nil {
			return doctorArgs{}, 1
		}
		if err := keeperWritef(stderr, "%s", keeperDoctorUsage); err != nil {
			return doctorArgs{}, 1
		}
		return doctorArgs{}, 1
	}
	return da, -1
}

func runKeeperDoctorEntry(args []string, stdout, stderr io.Writer) int {
	da, code := parseKeeperDoctorArgs(args, stdout, stderr)
	if code != -1 {
		return code
	}
	agentName := da.agentName
	projectDir := da.projectDir

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if writeErr := keeperWritef(stderr, "harmonik keeper doctor: cannot determine working directory: %v\n", err); writeErr != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		if writeErr := keeperWritef(stderr, "harmonik keeper doctor: cannot resolve project path %q: %v\n", projectDir, err); writeErr != nil {
			return 1
		}
		return 1
	}
	projectDir = absProject

	home, err := os.UserHomeDir()
	if err != nil {
		if writeErr := keeperWritef(stderr, "harmonik keeper doctor: cannot determine home directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	cfg := doctorConfig{
		agentName:    agentName,
		projectDir:   projectDir,
		settingsPath: settingsPath,
	}
	return runKeeperDoctor(cfg, stdout, stderr)
}

//nolint:gocognit,cyclop,funlen // A linear report keeps every check visible and independent.
func runKeeperDoctor(cfg doctorConfig, stdout, stderr io.Writer) int {
	type checkResult struct {
		name    string
		ok      bool
		message string
	}
	var results []checkResult
	var intendedExecutable string

	check := func(name string, ok bool, msg string) {
		results = append(results, checkResult{name: name, ok: ok, message: msg})
	}

	{
		projCfg, cfgErr := projectconfig.LoadProjectConfig(cfg.projectDir)
		if cfgErr != nil {
			check("config", false, fmt.Sprintf("config.yaml load error: %v — run 'harmonik keeper config --example'", cfgErr))
		} else {
			missing := checkMissingKeeperValues(KeeperFlags{}, projCfg.Keeper)
			if len(missing) > 0 {
				check("config", false, fmt.Sprintf("config.yaml missing %d required keeper key(s): %s — run 'harmonik keeper config --example'",
					len(missing), strings.Join(missing, ", ")))
			} else {
				check("config", true, "config.yaml has all required keeper keys")
			}
		}
	}

	{
		exe, lookErr := exec.LookPath("harmonik")
		if lookErr != nil {
			check("binary", false, "harmonik not found on PATH — reinstall or add to PATH")
		} else {
			var resolveErr error
			intendedExecutable, resolveErr = filepath.EvalSymlinks(exe)
			if resolveErr != nil {
				check("binary", false, fmt.Sprintf("cannot resolve harmonik binary %q: %v", exe, resolveErr))
				intendedExecutable = ""
			}
			info, statErr := os.Stat(exe)
			if statErr != nil {
				check("binary", false, fmt.Sprintf("cannot stat harmonik binary %q: %v", exe, statErr))
			} else {
				age := time.Since(info.ModTime())
				if age > 30*24*time.Hour {
					check("binary", false, fmt.Sprintf("harmonik binary at %s is %d days old — consider rebuilding (go install ./cmd/harmonik)", exe, int(age.Hours()/24)))
				} else {
					check("binary", true, fmt.Sprintf("ok (%s, %s old)", exe, formatAge(age)))
				}
			}
		}
	}

	settings, readErr := readGlobalSettings(cfg.settingsPath)
	settingsPresent := readErr == nil

	if !settingsPresent {
		check("statusLine", false, fmt.Sprintf("settings.json not found at %s — run: harmonik keeper enable %s ...", cfg.settingsPath, cfg.agentName))
	} else {
		cmd := getStatusLineCommand(settings)
		if !strings.Contains(cmd, "keeper-statusline.sh") {
			check("statusLine", false, fmt.Sprintf("keeper-statusline.sh not found in statusLine.command — run: harmonik keeper enable %s ...", cfg.agentName))
		} else {
			check("statusLine", true, "keeper-statusline.sh wired")
			if !statusLineTypeIsCommand(settings) {
				check("statusLine.type", false, `statusLine missing "type":"command" — Claude Code will reject settings.json; run: harmonik keeper enable to normalize`)
			}
			if strings.Contains(cmd, "HARMONIK_AGENT=") && !strings.Contains(cmd, "HARMONIK_AGENT=${") {
				check("statusLine.agent_pollution", false, "statusLine.command has a literal HARMONIK_AGENT= that overrides all sessions' env var (ctx pollution, hk-67k) — run: harmonik keeper enable to normalize")
			}
		}
	}

	if !settingsPresent {
		check("Stop hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
	} else {
		found, command := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir)
		if !found || command == "" {
			check("Stop hook", false, "keeper-stop-hook.sh not found in hooks.Stop for this project — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			check("Stop hook", true, "keeper-stop-hook.sh wired")
		}
	}

	if !settingsPresent {
		check("PreCompact hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
	} else {
		found, command := findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh", cfg.projectDir)
		if !found || command == "" {
			check("PreCompact hook", false, "keeper-precompact-hook.sh not found in hooks.PreCompact for this project — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			check("PreCompact hook", true, "keeper-precompact-hook.sh wired")
		}
	}

	if !settingsPresent {
		check("SessionStart hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
	} else {
		found, command := findHookForScript(settings, "SessionStart", "keeper-sessionstart-hook.sh", cfg.projectDir)
		if !found || command == "" {
			check("SessionStart hook", false, "keeper-sessionstart-hook.sh not found in hooks.SessionStart for this project — single-writer .sid channel will be absent; run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			check("SessionStart hook", true, "keeper-sessionstart-hook.sh wired (single-writer .sid channel)")
		}
	}

	{
		ctxPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".ctx")
		info, statErr := os.Stat(ctxPath)
		if statErr != nil {
			check("gauge", false, fmt.Sprintf(".ctx file absent (%s) — start Claude Code with the statusLine configured, then start the keeper", ctxPath))
		} else {
			age := time.Since(info.ModTime())
			if age > 5*time.Minute {
				check("gauge", false, fmt.Sprintf(".ctx file is %s old — gauge may be stale (is Claude Code running with statusLine?)", formatAge(age)))
			} else {
				check("gauge", true, fmt.Sprintf(".ctx fresh (%s old)", formatAge(age)))
			}
		}
	}

	{
		sid, sidMod, sidErr := keeper.ReadSessionIDFile(cfg.projectDir, cfg.agentName)
		sidPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".sid")
		switch {
		case sidErr != nil:
			check("sid channel", false, fmt.Sprintf(".sid absent (%s) — SessionStart hook has not fired yet; keeper is on the FALLBACK (latch) identity path", sidPath))
		case !keeper.IsPrimarySID(sid):
			check("sid channel", false, fmt.Sprintf(".sid present but not a primary session id (%q) — keeper is on the FALLBACK identity path", sid))
		default:
			msg := fmt.Sprintf(".sid present and well-formed (%s old)", formatAge(time.Since(sidMod)))
			if cf, _, ctxErr := keeper.ReadCtxFile(cfg.projectDir, cfg.agentName); ctxErr == nil && cf.SessionID != "" && cf.SessionID != sid {
				msg += fmt.Sprintf("; WARNING: gauge session_id %q differs — possible drift", cf.SessionID)
			}
			check("sid channel", true, msg)
		}
	}

	{
		idlePath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".idle")
		if _, statErr := os.Stat(idlePath); statErr != nil {
			check("idle marker", false, fmt.Sprintf(".idle not found (%s) — Stop hook has not fired yet (missing hook or Claude Code not stopped since hook was added)", idlePath))
		} else {
			check("idle marker", true, ".idle present (Stop hook has fired)")
		}
	}

	watcherLive := func() bool {
		fn := cfg.liveKeeperFn
		if fn == nil {
			fn = keeper.LiveKeeperPresent
		}
		return fn(cfg.projectDir, cfg.agentName)
	}()

	{
		managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".managed")
		deadlockHint := " — but NO live keeper watcher is running: the handoff cycle is DEADLOCKED, " +
			"not live (a /clear will never be driven). Start one with: harmonik keeper --agent " + cfg.agentName
		_, managedStatErr := os.Stat(managedPath)
		switch {
		case managedStatErr != nil:
			check("managed", false, ".managed marker absent — keeper is in passive mode (no handoff cycle). Add with: harmonik keeper enable --yes-destructive, or: touch "+managedPath)
		case !watcherLive:
			check("managed", false, ".managed present (handoff cycle CONSENTED)"+deadlockHint)
		default:
			managedSID, readErr := keeper.ReadManagedSessionID(cfg.projectDir, cfg.agentName)
			switch {
			case readErr != nil:
				check("managed", false, fmt.Sprintf(".managed present but unreadable: %v — keeper cannot confirm which session it is bound to", readErr))
			case managedSID != "":
				if cf, _, ctxErr := keeper.ReadCtxFile(cfg.projectDir, cfg.agentName); ctxErr == nil && cf.SessionID != "" && cf.SessionID != managedSID {
					check("managed", false, fmt.Sprintf("managed SID %q != live gauge/.sid SID %q — keeper bound to DEAD session (blind); restart watcher", managedSID, cf.SessionID))
				} else {
					check("managed", true, ".managed present (handoff cycle is LIVE)")
				}
			default:
				check("managed", true, ".managed present (handoff cycle is LIVE)")
			}
		}
	}

	if watcherLive {
		check("live-watcher", true, "live keeper process is running")
	} else {
		check("live-watcher", false, "no live keeper watcher detected — start with: harmonik keeper --agent "+cfg.agentName)
	}

	if watcherLive {
		record, recordErr := keeper.ReadRuntimeRecord(cfg.projectDir, cfg.agentName)
		lockPID, lockPIDErr := keeper.ReadLockPID(cfg.projectDir, cfg.agentName)
		switch {
		case lockPIDErr != nil:
			check("runtime-provenance", false, fmt.Sprintf("cannot read live keeper lock owner: %v", lockPIDErr))
		case recordErr != nil:
			check("runtime-provenance", false, fmt.Sprintf("live keeper has no readable runtime identity: %v — restart it with the intended binary", recordErr))
		case record.PID <= 0 || record.Executable == "" || !validSHA256(record.ExecutableSHA256) || !validSHA256(record.ConfigSHA256) || record.Commit == "" || record.StartedAt.IsZero():
			check("runtime-provenance", false, "live keeper runtime identity is incomplete — restart it")
		case record.PID != lockPID:
			check("runtime-provenance", false, fmt.Sprintf("runtime record PID %d does not own the live keeper lock (owner PID %d)", record.PID, lockPID))
		default:
			actualDigest, digestErr := keeper.FileSHA256(record.Executable)
			switch {
			case digestErr != nil:
				check("runtime-provenance", false, fmt.Sprintf("cannot digest live keeper executable %q: %v", record.Executable, digestErr))
			case actualDigest != record.ExecutableSHA256:
				check("runtime-provenance", false, "live keeper executable changed after startup — restart it")
			case intendedExecutable != "":
				intendedDigest, intendedErr := keeper.FileSHA256(intendedExecutable)
				switch {
				case intendedErr != nil:
					check("runtime-provenance", false, fmt.Sprintf("cannot digest intended binary %q: %v", intendedExecutable, intendedErr))
				case intendedDigest != record.ExecutableSHA256:
					check("runtime-provenance", false, fmt.Sprintf("live keeper PID %d runs %s (%s), not intended %s (%s)", record.PID, record.Executable, record.ExecutableSHA256[:12], intendedExecutable, intendedDigest[:12]))
				default:
					check("runtime-provenance", true, fmt.Sprintf("PID %d target=%q binary=%s commit=%s config=%s", record.PID, record.TmuxTarget, record.ExecutableSHA256[:12], record.Commit, record.ConfigSHA256[:12]))
				}
			default:
				check("runtime-provenance", false, "cannot resolve intended harmonik binary on PATH")
			}
		}
	}

	{
		resolveFn := cfg.resolveTargetFn
		if resolveFn == nil {
			resolveFn = func(pd, an string) string {
				return keeper.ResolveTmuxTarget(pd, an, "", nil)
			}
		}
		paneFn := cfg.paneExistsFn
		if paneFn == nil {
			paneFn = tmuxPaneExists
		}
		target := resolveFn(cfg.projectDir, cfg.agentName)
		switch target {
		case "":
			check("tmux-pane", true, "agent session not live — pane check not applicable")
		default:
			ok, paneErr := paneFn(target)
			switch {
			case paneErr != nil:
				check("tmux-pane", false, fmt.Sprintf("pane check failed for %q: %v", target, paneErr))
			case !ok:
				check("tmux-pane", false, fmt.Sprintf("pane %q not found — keeper inject-target is unreachable; verify the keeper was launched with a braced tmux target (${session}:agent, not $session:agent — zsh :a modifier silently rewrites unbraced form; hk-5266t)", target))
			default:
				check("tmux-pane", true, fmt.Sprintf("pane %q is live", target))
			}
		}
	}

	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		check("api-key-risk", false, "ANTHROPIC_API_KEY is set in environment — keeper-launched claude will bill the API credit pool, not the subscription. Unset it or use 'env -u ANTHROPIC_API_KEY harmonik keeper ...'")
	} else {
		check("api-key-risk", true, "ANTHROPIC_API_KEY not set in environment (good)")
	}

	{
		cyclePath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".cycle")
		// #nosec G304 -- projectDir and agentName passed the command boundary checks.
		raw, readCycleErr := os.ReadFile(cyclePath)
		switch {
		case errors.Is(readCycleErr, os.ErrNotExist):
			check("last-cycle", true, "no cycle recorded")
		case readCycleErr != nil:
			check("last-cycle", false, fmt.Sprintf("cannot read %s: %v", cyclePath, readCycleErr))
		default:
			var journal keeper.CycleJournal
			if unmarshalErr := json.Unmarshal(raw, &journal); unmarshalErr != nil {
				check("last-cycle", false, fmt.Sprintf("invalid cycle journal %s: %v", cyclePath, unmarshalErr))
			} else {
				msg := fmt.Sprintf("phase=%s cycle_id=%s", journal.Phase, journal.CycleID)
				if journal.Reason != "" {
					msg += " reason=" + journal.Reason
				}
				check("last-cycle", true, msg)
			}
		}
	}

	allOK := true
	for _, r := range results {
		symbol := "✓"
		if !r.ok {
			symbol = "✗"
			allOK = false
		}
		if _, err := fmt.Fprintf(stdout, "  %s %-20s %s\n", symbol, r.name, r.message); err != nil {
			return 1
		}
	}

	if allOK {
		if _, err := fmt.Fprintf(stdout, "\nkeeper doctor: all checks passed for agent %q\n", cfg.agentName); err != nil {
			return 1
		}
		return 0
	}
	failCount := 0
	for _, r := range results {
		if !r.ok {
			failCount++
		}
	}
	if err := keeperWritef(stderr, "\nkeeper doctor: %d check(s) failed for agent %q\n", failCount, cfg.agentName); err != nil {
		return 1
	}
	return 1
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func runKeeperDoctorAtBoot(projectDir, agentName, settingsPath string) {
	cfg := doctorConfig{
		agentName:    agentName,
		projectDir:   projectDir,
		settingsPath: settingsPath,
	}
	code := runKeeperDoctor(cfg, os.Stderr, os.Stderr)
	if code != 0 {
		if err := keeperWritef(os.Stderr, "keeper: boot doctor found gaps for agent %q (above) — some keeper features may be inactive\n", agentName); err != nil {
			return
		}
	}
}

func readGlobalSettings(settingsPath string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(settingsPath) //nolint:gosec // G304: operator-specified path
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("read %q: %w", settingsPath, err)
	}
	var m map[string]interface{}
	if jsonErr := json.Unmarshal(raw, &m); jsonErr != nil {
		return nil, fmt.Errorf("parse %q: %w", settingsPath, jsonErr)
	}
	return m, nil
}

func writeGlobalSettings(settingsPath string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil { //dirmode:allow parent of the user's ~/.claude/settings.json, not .harmonik state
		return fmt.Errorf("MkdirAll %q: %w", filepath.Dir(settingsPath), err)
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(settingsPath, content, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", settingsPath, err)
	}
	return nil
}

func mergeStatusLineStanza(settings map[string]interface{}, canonicalCmd string) string {
	existing := getStatusLineCommand(settings)

	if strings.Contains(existing, "keeper-statusline.sh") {
		if existing == canonicalCmd && statusLineTypeIsCommand(settings) {
			return "unchanged"
		}
		sl := getOrCreateStatusLine(settings)
		sl["type"] = "command"
		sl["command"] = canonicalCmd
		settings["statusLine"] = sl
		return "updated (normalized)"
	}

	sl := getOrCreateStatusLine(settings)
	sl["type"] = "command"
	sl["command"] = canonicalCmd
	settings["statusLine"] = sl
	return "added"
}

func mergeHookStanza(settings map[string]interface{}, eventName, scriptBasename, projectDir, canonicalCmd string) string {
	found, existingCmd := findHookForScript(settings, eventName, scriptBasename, projectDir)

	if found {
		if existingCmd == canonicalCmd {
			return "unchanged"
		}
		updateHookCommand(settings, eventName, scriptBasename, projectDir, canonicalCmd)
		return "updated (normalized)"
	}

	appendHookGroup(settings, eventName, canonicalCmd)
	return "added"
}

func getStatusLineCommand(settings map[string]interface{}) string {
	sl, ok := settings["statusLine"]
	if !ok || sl == nil {
		return ""
	}
	slMap, ok := sl.(map[string]interface{})
	if !ok {
		return ""
	}
	cmd, ok := slMap["command"].(string)
	if !ok {
		return ""
	}
	return cmd
}

func statusLineTypeIsCommand(settings map[string]interface{}) bool {
	sl, ok := settings["statusLine"]
	if !ok || sl == nil {
		return false
	}
	slMap, ok := sl.(map[string]interface{})
	if !ok {
		return false
	}
	t, ok := slMap["type"].(string)
	return ok && t == "command"
}

func getOrCreateStatusLine(settings map[string]interface{}) map[string]interface{} {
	sl, ok := settings["statusLine"]
	if ok {
		if slMap, ok2 := sl.(map[string]interface{}); ok2 {
			return slMap
		}
	}
	m := map[string]interface{}{}
	settings["statusLine"] = m
	return m
}

func findHookForScript(settings map[string]interface{}, eventName, scriptBasename, projectDir string) (bool, string) {
	hooksRaw, ok := settings["hooks"]
	if !ok || hooksRaw == nil {
		return false, ""
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		return false, ""
	}
	groupsRaw, ok := hooksMap[eventName]
	if !ok || groupsRaw == nil {
		return false, ""
	}
	groups, ok := groupsRaw.([]interface{})
	if !ok {
		return false, ""
	}
	for _, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		innerHooks, ok := gMap["hooks"]
		if !ok {
			continue
		}
		entries, ok := innerHooks.([]interface{})
		if !ok {
			continue
		}
		for _, e := range entries {
			eMap, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, ok := eMap["command"].(string)
			if !ok {
				continue
			}
			if !strings.Contains(cmd, scriptBasename) {
				continue
			}
			if projectDir != "" && !strings.Contains(cmd, "HARMONIK_PROJECT="+projectDir) {
				continue
			}
			return true, cmd
		}
	}
	return false, ""
}

func updateHookCommand(settings map[string]interface{}, eventName, scriptBasename, projectDir, newCmd string) {
	hooksMap, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return
	}
	groups, ok := hooksMap[eventName].([]interface{})
	if !ok {
		return
	}
	for _, g := range groups {
		gMap, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		entries, ok := gMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, e := range entries {
			eMap, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, ok := eMap["command"].(string)
			if !ok {
				continue
			}
			if !strings.Contains(cmd, scriptBasename) {
				continue
			}
			if projectDir != "" && !strings.Contains(cmd, "HARMONIK_PROJECT="+projectDir) {
				continue
			}
			eMap["command"] = newCmd
			return
		}
	}
}

func appendHookGroup(settings map[string]interface{}, eventName, cmd string) {
	hooksRaw, ok := settings["hooks"]
	if !ok || hooksRaw == nil {
		hooksRaw = map[string]interface{}{}
	}
	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		hooksMap = map[string]interface{}{}
	}

	newGroup := map[string]interface{}{
		"matcher": "",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": cmd,
			},
		},
	}

	var groups []interface{}
	if existing, exists := hooksMap[eventName]; exists {
		if arr, ok := existing.([]interface{}); ok {
			groups = arr
		}
	}
	groups = append(groups, newGroup)
	hooksMap[eventName] = groups
	settings["hooks"] = hooksMap
}

var keeperScriptNames = []string{
	"keeper-statusline.sh",
	"keeper-stop-hook.sh",
	"keeper-precompact-hook.sh",
	"keeper-sessionstart-hook.sh",
}

func autoDetectScriptsDir(hints ...string) string {
	candidates := make([]string, 0, len(hints)+3)

	for _, h := range hints {
		if h == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(h, "scripts"))
	}

	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "..", "scripts"),
			filepath.Join(filepath.Dir(exe), "scripts"),
		)
	}

	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "keeper-statusline.sh")); err == nil {
			return abs
		}
	}

	if len(hints) > 0 && hints[0] != "" {
		if dir, err := extractEmbeddedKeeperScripts(hints[0]); err == nil {
			return dir
		}
	}
	return ""
}

func extractEmbeddedKeeperScripts(projectDir string) (string, error) {
	destDir := filepath.Join(projectDir, ".harmonik", "scripts")
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		destDir = filepath.Join(home, ".harmonik", "scripts")
	}
	if err := os.MkdirAll(destDir, core.HarmonikDirMode); err != nil {
		return "", fmt.Errorf("create %s: %w", destDir, err)
	}
	for _, name := range keeperScriptNames {
		data, err := initSkillAssets.ReadFile("assets/scripts/" + name)
		if err != nil {
			return "", fmt.Errorf("read embedded scripts/%s: %w", name, err)
		}
		dest := filepath.Join(destDir, name)
		//nolint:gosec // G306: hook scripts need +x so the shell can execute them
		if err := os.WriteFile(dest, data, 0o755); err != nil {
			return "", fmt.Errorf("write %s: %w", dest, err)
		}
	}
	return destDir, nil
}

func buildKeeperRunCmd(cfg enableConfig) string {
	parts := []string{
		"harmonik", "keeper",
		"--agent", cfg.agentName,
	}
	if cfg.tmuxTarget != "" {
		parts = append(parts, "--tmux", cfg.tmuxTarget)
	}
	return strings.Join(parts, " ")
}

func writeHandoffStub(path, agentName string) error {
	content := fmt.Sprintf("# HANDOFF-%s\n\n## State\n<!-- keeper will populate this on handoff -->\n\n## Active Work\n<!-- fill in before each session -->\n", agentName)
	//nolint:gosec // G306: 0644 matches conventions for doc files
	return os.WriteFile(path, []byte(content), 0o644)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // G304: operator-supplied path
	if err != nil {
		return err
	}
	//nolint:gosec // G306: 0644 matches conventions for backup files
	return os.WriteFile(dst, data, 0o644)
}

func tmuxPaneExists(target string) (bool, error) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return false, fmt.Errorf("tmux not found on PATH: %w", err)
	}
	//nolint:gosec // G204: target is operator-supplied tmux pane address
	cmd := exec.CommandContext(context.Background(), tmuxPath, "display-message", "-t", target, "-p", "#W")
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("probe tmux pane %q: %w", target, runErr)
	}
	return true, nil
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

const keeperEnableUsage = `harmonik keeper enable --agent <name> — wire keeper stanzas into ~/.claude/settings.json

USAGE
  harmonik keeper enable --agent <name> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]

  FLAG-ONLY (hk-nbft): the agent is named ONLY via --agent. A positional argument
  is rejected with exit 2 (positionals were a recurring keeper footgun); an
  unrecognized flag also exits 2.

FLAGS
  --agent NAME         Agent name (required; e.g. orchestrator, flywheel). Must not contain '/' or '..'.
  --project DIR        Harmonik project root (default: current working directory)
  --scripts-dir DIR    Directory containing keeper-*.sh scripts (auto-detected if not specified)
  --tmux TARGET        tmux pane target for the run command and pane validation (optional)
  --yes-destructive    Enable .managed marker creation (LIVE handoff cycle) and allow
                       known live agent names (flywheel, named-queues, controlpoints)

WHAT IT DOES
  1. Validates agent name and (without --yes-destructive) refuses known live agents
  2. Backs up existing ~/.claude/settings.json
  3. Merges statusLine, Stop, PreCompact, and SessionStart hook stanzas — idempotent, normalizes env-var names
  4. Seeds HANDOFF-<agent>.md at --project if absent
  5. Validates --tmux pane exists (if --tmux is provided)
  6. If --yes-destructive: creates .harmonik/keeper/<agent>.managed (LIVE handoff consent)
  7. Prints the exact 'harmonik keeper' run command

SAFETY
  Idempotent: re-running with the same flags only updates stanzas that have drifted.
  Never creates .managed without --yes-destructive.
  .managed creation refuses flywheel/named-queues/controlpoints without --yes-destructive.
  Backup is taken before any write to settings.json.

EXIT CODES
  0  Success
  1  Argument, validation, or I/O error
  2  Unexpected positional argument or unrecognized flag (flag-only)
`

const keeperDoctorUsage = `harmonik keeper doctor --agent <name> — read-only drift validator for keeper setup

USAGE
  harmonik keeper doctor --agent <name> [--project DIR]

  FLAG-ONLY (hk-nbft): the agent is named ONLY via --agent. A positional argument
  is rejected with exit 2; an unrecognized flag also exits 2.

FLAGS
  --agent NAME    Agent name (required; e.g. orchestrator, flywheel)
  --project DIR   Harmonik project root (default: current working directory)

CHECKS (all read-only; no filesystem mutations)
  binary         harmonik binary on PATH and not stale (>30 days old)
  statusLine     keeper-statusline.sh wired in ~/.claude/settings.json
  Stop hook      keeper-stop-hook.sh wired in hooks.Stop
  PreCompact     keeper-precompact-hook.sh wired in hooks.PreCompact
  SessionStart   keeper-sessionstart-hook.sh wired in hooks.SessionStart (.sid channel)
  gauge          .harmonik/keeper/<agent>.ctx exists and is fresh (<5 min)
  sid channel    .harmonik/keeper/<agent>.sid present and a well-formed primary id
  idle marker    .harmonik/keeper/<agent>.idle has been written (Stop hook fired)
  managed        .harmonik/keeper/<agent>.managed present AND a watcher is running
                 (the marker alone is consent, not liveness — RED without a watcher)
  live-watcher   live keeper process holds the flock (watcher is actually running)
  runtime-provenance
                 live lock owner and executable digest match the runtime record and
                 intended harmonik binary on PATH; target and config digest are shown
  tmux-pane      resolved target exists; probe errors are failures, not green skips
  api-key-risk   ANTHROPIC_API_KEY not set in environment

EXIT CODES
  0  All checks passed
  1  One or more checks failed (details printed to stdout)
  2  Unexpected positional argument or unrecognized flag (flag-only)
`
