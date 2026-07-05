package main

// keeper_enable_doctor_cmd.go — `harmonik keeper enable` and `harmonik keeper doctor`
//
// enable  — IDEMPOTENT wiring of the 3 keeper stanzas into ~/.claude/settings.json
//           (statusLine + Stop hook + PreCompact hook), seed a handoff stub, and
//           print the exact run command.  Never creates .managed for real agents
//           unless --yes-destructive is passed.  Refuses known live agent names
//           (flywheel, named-queues, controlpoints) without --yes-destructive.
//
// doctor  — READ-ONLY drift validator; exits non-zero on any gap.  Also runs at
//           keeper BOOT as a loud diagnostic when a required stanza is missing.
//
// Spec ref: codename:session-keeper (hk-ekap1); bead hk-kzqml.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/keeper"
)

// knownLiveAgents are production agents where an accidental /clear cycle could
// destroy in-progress work.  enable refuses these without --yes-destructive.
var knownLiveAgents = map[string]bool{
	"flywheel":      true,
	"named-queues":  true,
	"controlpoints": true,
}

// ── enableConfig ─────────────────────────────────────────────────────────────

// enableConfig is the injectable configuration for runKeeperEnable.
// Fields that would normally come from the environment (home dir, settings path)
// are explicit so tests can override them without touching the real filesystem.
type enableConfig struct {
	agentName      string
	projectDir     string // harmonik project root
	scriptsDir     string // directory containing keeper-*.sh
	tmuxTarget     string // optional tmux pane target
	settingsPath   string // absolute path to ~/.claude/settings.json
	yesDestructive bool   // gate for .managed creation and known-live agents
}

// runKeeperEnableSubcommand is the entry point for `harmonik keeper enable`.
func runKeeperEnableSubcommand(args []string) int {
	return runKeeperEnableEntry(args, os.Stdout, os.Stderr)
}

// enableArgs holds the parsed `keeper enable` argument vector.
type enableArgs struct {
	agentName      string
	projectDir     string
	scriptsDir     string
	tmuxTarget     string
	yesDestructive bool
}

// parseKeeperEnableArgs parses the enable argument vector. FLAG-ONLY (hk-nbft):
// the agent name MUST be supplied via `--agent <name>` / `--agent=<name>`. ANY
// positional argument is rejected LOUDLY (exit 2) with the same message every
// other keeper verb uses (resolveKeeperAgent) — positionals were the recurring
// footgun where a bare token silently took the place of --agent (e.g.
// `enable captain` parsed the agent then failed downstream on scripts-dir). An
// UNRECOGNIZED leading-dash token is likewise rejected (exit 2) — the CLASS-A
// bug where `doctor --agent X` checked an agent named "--agent". Every existing
// recognized flag (including --yes-destructive, the regression that failed the
// original hk-psds) is enumerated and kept; the reject now covers unrecognized
// flags AND positional arguments.
//
// The returned code is a control signal, not just an exit code:
//
//	-1 → parse OK, caller should proceed
//	 0 → --help printed, caller should return 0
//	 1 → missing agent name (no --agent supplied)
//	 2 → unrecognized leading-dash flag OR unexpected positional argument
func parseKeeperEnableArgs(args []string, stdout, stderr io.Writer) (enableArgs, int) {
	var (
		pa        enableArgs
		agentFlag string
	)

	// manual flag parse to match existing codebase convention
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, keeperEnableUsage)
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
			// Unrecognized leading-dash token: reject loudly instead of silently
			// treating it as the positional agent name (the CLASS-A false-green).
			// This catch-all MUST stay AFTER every recognized flag case above so
			// --yes-destructive et al. are not swept into the reject.
			fmt.Fprintf(stderr, "harmonik keeper enable: unrecognized flag %q\n", args[i])
			fmt.Fprint(stderr, keeperEnableUsage)
			return enableArgs{}, 2
		default:
			rest = append(rest, args[i])
		}
	}

	// FLAG-ONLY (hk-nbft): any positional argument is rejected with the SAME
	// message resolveKeeperAgent uses for the other keeper verbs, exit 2.
	if len(rest) > 0 {
		fmt.Fprintf(stderr,
			"harmonik keeper enable: unexpected positional argument(s) %q — this command is flag-only; use --agent <name>\n",
			strings.Join(rest, " "))
		fmt.Fprint(stderr, keeperEnableUsage)
		return enableArgs{}, 2
	}

	pa.agentName = agentFlag
	if pa.agentName == "" {
		fmt.Fprintln(stderr, "harmonik keeper enable: --agent <name> is required")
		fmt.Fprint(stderr, keeperEnableUsage)
		return enableArgs{}, 1
	}
	return pa, -1
}

// runKeeperEnableEntry parses flags and delegates to runKeeperEnable.
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

	// Resolve project dir.
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik keeper enable: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik keeper enable: cannot resolve project path %q: %v\n", projectDir, err)
		return 1
	}
	projectDir = absProject

	// Resolve scripts dir. projectDir (resolved above) is the primary hint so a
	// `go install`'d binary run from the repo still finds <project>/scripts.
	if scriptsDir == "" {
		scriptsDir = autoDetectScriptsDir(projectDir)
	}

	// Resolve settings path.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "harmonik keeper enable: cannot determine home directory: %v\n", err)
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

// runKeeperEnable is the testable core of `harmonik keeper enable`.
func runKeeperEnable(cfg enableConfig, stdout, stderr io.Writer) int {
	// Validate agent name (no path traversal).
	if strings.Contains(cfg.agentName, "/") || strings.Contains(cfg.agentName, "..") {
		fmt.Fprintf(stderr, "harmonik keeper enable: agent name %q must not contain '/' or '..'\n", cfg.agentName)
		return 1
	}
	if cfg.agentName == "" {
		fmt.Fprintln(stderr, "harmonik keeper enable: agent name is required")
		return 1
	}

	// Guard: refuse known live agents without --yes-destructive.
	if knownLiveAgents[cfg.agentName] && !cfg.yesDestructive {
		fmt.Fprintf(stderr,
			"harmonik keeper enable: %q is a known live agent (flywheel/named-queues/controlpoints).\n"+
				"Wiring keeper hooks for a live session carries risk: a misconfigured .managed marker\n"+
				"could trigger /clear on an active orchestrator session.\n"+
				"Pass --yes-destructive to proceed.\n",
			cfg.agentName)
		return 1
	}

	// Validate scripts dir and verify scripts exist.
	if cfg.scriptsDir == "" {
		fmt.Fprintf(stderr,
			"harmonik keeper enable: cannot locate scripts directory.\n"+
				"Pass --scripts-dir=/path/to/harmonik/scripts (the scripts/ directory in the harmonik repo).\n")
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
			fmt.Fprintf(stderr, "harmonik keeper enable: script not found: %s\n  (pass --scripts-dir to specify the harmonik scripts/ directory)\n", p)
			return 1
		}
	}

	// Read existing settings.json (or start fresh).
	settings, err := readGlobalSettings(cfg.settingsPath)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik keeper enable: read %s: %v\n", cfg.settingsPath, err)
		return 1
	}

	// Back up existing file if it has content.
	if _, statErr := os.Stat(cfg.settingsPath); statErr == nil {
		backupPath := fmt.Sprintf("%s.bak-%s", cfg.settingsPath, time.Now().UTC().Format("20060102T150405Z"))
		if copyErr := copyFile(cfg.settingsPath, backupPath); copyErr != nil {
			fmt.Fprintf(stderr, "harmonik keeper enable: backup %s → %s: %v\n", cfg.settingsPath, backupPath, copyErr)
			return 1
		}
		fmt.Fprintf(stdout, "keeper enable: backed up settings.json → %s\n", backupPath)
	}

	// Build canonical commands.
	// Agent name is NOT embedded — scripts derive it from the tmux session name at
	// runtime so a single global ~/.claude/settings.json entry works for all concurrent
	// sessions on the machine without perturbing peer agents (hk-nm32w).
	//
	// ON-058b: statusLine is project-agnostic — no HARMONIK_PROJECT= prefix.
	// Runtime routing is via the inherited HARMONIK_PROJECT env var; the script
	// uses PROJECT="${HARMONIK_PROJECT:-${PWD}}" to resolve the correct project.
	// All projects on the machine converge on the identical bare-path stanza.
	statusLineCmd := filepath.Join(cfg.scriptsDir, "keeper-statusline.sh")
	// ON-058a: Stop/PreCompact hook commands retain HARMONIK_PROJECT= so dedup can
	// match on the (basename, HARMONIK_PROJECT=<projectDir>) pair — two distinct
	// projects produce two sibling groups in the hooks array and coexist without
	// one project's enable perturbing the other's group.
	stopHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-stop-hook.sh"))
	precompactHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-precompact-hook.sh"))
	// SessionStart hook (hk-8prq): same project-keyed, agent-free command shape
	// as Stop/PreCompact — HARMONIK_PROJECT= for ON-058a dedup, NO literal
	// HARMONIK_AGENT= (the script derives the agent from the inherited env / tmux
	// name, avoiding the hk-67k ctx-pollution regression). Provisioning the hook
	// here is what makes the single-writer <agent>.sid channel exist for EVERY
	// keeper-managed session.
	sessionStartHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s %s",
		cfg.projectDir,
		filepath.Join(cfg.scriptsDir, "keeper-sessionstart-hook.sh"))

	// Merge stanzas (idempotent).
	statusLineAction := mergeStatusLineStanza(settings, statusLineCmd)
	stopAction := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir, stopHookCmd)
	precompactAction := mergeHookStanza(settings, "PreCompact", "keeper-precompact-hook.sh", cfg.projectDir, precompactHookCmd)
	sessionStartAction := mergeHookStanza(settings, "SessionStart", "keeper-sessionstart-hook.sh", cfg.projectDir, sessionStartHookCmd)

	fmt.Fprintf(stdout, "keeper enable: statusLine     — %s\n", statusLineAction)
	fmt.Fprintf(stdout, "keeper enable: Stop hook      — %s\n", stopAction)
	fmt.Fprintf(stdout, "keeper enable: PreCompact hook — %s\n", precompactAction)
	fmt.Fprintf(stdout, "keeper enable: SessionStart hook — %s\n", sessionStartAction)

	// Write updated settings.json.
	if err := writeGlobalSettings(cfg.settingsPath, settings); err != nil {
		fmt.Fprintf(stderr, "harmonik keeper enable: write %s: %v\n", cfg.settingsPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "keeper enable: wrote %s\n", cfg.settingsPath)

	// Seed HANDOFF-<agent>.md if absent.
	handoffPath := filepath.Join(cfg.projectDir, fmt.Sprintf("HANDOFF-%s.md", cfg.agentName))
	if _, err := os.Stat(handoffPath); os.IsNotExist(err) {
		if writeErr := writeHandoffStub(handoffPath, cfg.agentName); writeErr != nil {
			fmt.Fprintf(stderr, "harmonik keeper enable: seed handoff stub: %v\n", writeErr)
			return 1
		}
		fmt.Fprintf(stdout, "keeper enable: seeded %s\n", handoffPath)
	} else {
		fmt.Fprintf(stdout, "keeper enable: %s already exists — skipping handoff seed\n", handoffPath)
	}

	// Validate tmux pane if --tmux was provided.
	if cfg.tmuxTarget != "" {
		ok, checkErr := tmuxPaneExists(cfg.tmuxTarget)
		if checkErr != nil {
			fmt.Fprintf(stdout, "keeper enable: tmux check skipped (%v)\n", checkErr)
		} else if !ok {
			fmt.Fprintf(stdout,
				"keeper enable: WARNING — tmux pane %q not found or not named.\n"+
					"  Name the pane: tmux rename-window -t %s <agent-name>\n",
				cfg.tmuxTarget, cfg.tmuxTarget)
		} else {
			fmt.Fprintf(stdout, "keeper enable: tmux pane %q is live\n", cfg.tmuxTarget)
		}
	}

	// .managed gating.
	if cfg.yesDestructive {
		managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".managed")
		if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
			fmt.Fprintf(stderr, "harmonik keeper enable: create keeper dir: %v\n", err)
			return 1
		}
		if _, err := os.Stat(managedPath); os.IsNotExist(err) {
			//nolint:gosec // G306: 0600 — keeper-owned marker, no world-read needed
			if writeErr := os.WriteFile(managedPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); writeErr != nil {
				fmt.Fprintf(stderr, "harmonik keeper enable: create .managed: %v\n", writeErr)
				return 1
			}
			fmt.Fprintf(stdout, "keeper enable: created .managed marker (DESTRUCTIVE CONSENT — handoff cycle is now LIVE)\n")
		} else {
			fmt.Fprintf(stdout, "keeper enable: .managed already present\n")
		}
	} else {
		fmt.Fprintf(stdout,
			"\nkeeper enable: .managed NOT created (handoff cycle is passive).\n"+
				"  To enable LIVE handoff (DESTRUCTIVE — triggers /clear + resume):\n"+
				"    harmonik keeper enable %s --yes-destructive ...\n"+
				"  Or manually: touch %s/.harmonik/keeper/%s.managed\n",
			cfg.agentName, cfg.projectDir, cfg.agentName)
	}

	// Print the run command.
	runCmd := buildKeeperRunCmd(cfg)
	fmt.Fprintf(stdout, "\nkeeper enable: to start the keeper, run:\n  %s\n", runCmd)

	return 0
}

// ── doctorConfig ─────────────────────────────────────────────────────────────

// doctorConfig is the injectable configuration for runKeeperDoctor.
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

// runKeeperDoctorSubcommand is the entry point for `harmonik keeper doctor`.
func runKeeperDoctorSubcommand(args []string) int {
	return runKeeperDoctorEntry(args, os.Stdout, os.Stderr)
}

// doctorArgs holds the parsed `keeper doctor` argument vector.
type doctorArgs struct {
	agentName  string
	projectDir string
}

// parseKeeperDoctorArgs parses the doctor argument vector. FLAG-ONLY (hk-nbft),
// with the SAME parity as enable: the agent name MUST come via `--agent <name>` /
// `--agent=<name>`. ANY positional argument is rejected loudly (exit 2) with the
// shared resolveKeeperAgent message — `doctor captain` previously accepted the
// positional and exited 0 (false-green at keeper boot). An unrecognized
// leading-dash token is likewise rejected (exit 2). The return code follows the
// same control-signal convention as parseKeeperEnableArgs (-1 proceed, 0 help,
// 1 missing agent, 2 unknown flag OR unexpected positional).
func parseKeeperDoctorArgs(args []string, stdout, stderr io.Writer) (doctorArgs, int) {
	var (
		da        doctorArgs
		agentFlag string
	)
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, keeperDoctorUsage)
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
			// Unrecognized leading-dash token: reject loudly. THE CLASS-A killer —
			// `doctor --agent X` previously checked an agent literally named
			// "--agent" (false-green doctor at keeper boot; captain hit this live).
			fmt.Fprintf(stderr, "harmonik keeper doctor: unrecognized flag %q\n", args[i])
			fmt.Fprint(stderr, keeperDoctorUsage)
			return doctorArgs{}, 2
		default:
			rest = append(rest, args[i])
		}
	}

	// FLAG-ONLY (hk-nbft): any positional argument is rejected with the SAME
	// message resolveKeeperAgent uses for the other keeper verbs, exit 2.
	if len(rest) > 0 {
		fmt.Fprintf(stderr,
			"harmonik keeper doctor: unexpected positional argument(s) %q — this command is flag-only; use --agent <name>\n",
			strings.Join(rest, " "))
		fmt.Fprint(stderr, keeperDoctorUsage)
		return doctorArgs{}, 2
	}

	da.agentName = agentFlag
	if da.agentName == "" {
		fmt.Fprintln(stderr, "harmonik keeper doctor: --agent <name> is required")
		fmt.Fprint(stderr, keeperDoctorUsage)
		return doctorArgs{}, 1
	}
	return da, -1
}

// runKeeperDoctorEntry parses flags and delegates to runKeeperDoctor.
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
			fmt.Fprintf(stderr, "harmonik keeper doctor: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik keeper doctor: cannot resolve project path %q: %v\n", projectDir, err)
		return 1
	}
	projectDir = absProject

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "harmonik keeper doctor: cannot determine home directory: %v\n", err)
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

// runKeeperDoctor is the testable core of `harmonik keeper doctor`.
// It is also called by runKeeperSubcommand at keeper boot for loud diagnostics.
// Exits non-zero when any check fails.
func runKeeperDoctor(cfg doctorConfig, stdout, stderr io.Writer) int {
	type checkResult struct {
		name    string
		ok      bool
		message string
	}
	var results []checkResult

	check := func(name string, ok bool, msg string) {
		results = append(results, checkResult{name: name, ok: ok, message: msg})
	}

	// 0. Config completeness: .harmonik/config.yaml has all required keeper keys.
	// Missing file = all keys absent (run `harmonik keeper config --example`).
	// A load error (malformed YAML, unsupported version) is also a hard gap.
	// Uses empty KeeperFlags so the check reflects the config alone, matching
	// the typical crew keeper invocation (no per-key CLI flags). Refs: hk-zou19.
	{
		projCfg, cfgErr := daemon.LoadProjectConfig(cfg.projectDir)
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

	// 1. Binary currency: harmonik on PATH and executable.
	{
		exe, lookErr := exec.LookPath("harmonik")
		if lookErr != nil {
			check("binary", false, "harmonik not found on PATH — reinstall or add to PATH")
		} else {
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

	// Read settings.json once for hook checks.
	settings, readErr := readGlobalSettings(cfg.settingsPath)
	settingsPresent := readErr == nil

	// 2. statusLine stanza present.
	{
		if !settingsPresent {
			check("statusLine", false, fmt.Sprintf("settings.json not found at %s — run: harmonik keeper enable %s ...", cfg.settingsPath, cfg.agentName))
		} else {
			cmd := getStatusLineCommand(settings)
			if !strings.Contains(cmd, "keeper-statusline.sh") {
				check("statusLine", false, fmt.Sprintf("keeper-statusline.sh not found in statusLine.command — run: harmonik keeper enable %s ...", cfg.agentName))
			} else {
				check("statusLine", true, "keeper-statusline.sh wired")
				// Sub-check: required "type":"command" field (hk-hs1). Without it
				// Claude Code rejects the whole settings.json and disables all hooks.
				if !statusLineTypeIsCommand(settings) {
					check("statusLine.type", false, `statusLine missing "type":"command" — Claude Code will reject settings.json; run: harmonik keeper enable to normalize`)
				}
				// Sub-check: ctx pollution canary (hk-67k). A literal HARMONIK_AGENT=<name>
				// in the command overrides the inherited env var for ALL concurrent Claude
				// Code sessions — every session writes the same .ctx file, clobbering the
				// keeper's session-binding. Shell-expansion form (${HARMONIK_AGENT:-...}) is
				// acceptable but unnecessary; the agent name is already derived from the tmux
				// session name. Run `harmonik keeper enable` to write a clean command.
				if strings.Contains(cmd, "HARMONIK_AGENT=") && !strings.Contains(cmd, "HARMONIK_AGENT=${") {
					check("statusLine.agent_pollution", false, "statusLine.command has a literal HARMONIK_AGENT= that overrides all sessions' env var (ctx pollution, hk-67k) — run: harmonik keeper enable to normalize")
				}
			}
		}
	}

	// 3. Stop hook present for THIS project (ON-058a: matched on (basename, HARMONIK_PROJECT=<projectDir>) pair).
	{
		if !settingsPresent {
			check("Stop hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			found, _ := findHookForScript(settings, "Stop", "keeper-stop-hook.sh", cfg.projectDir)
			if !found {
				check("Stop hook", false, "keeper-stop-hook.sh not found in hooks.Stop for this project — run: harmonik keeper enable "+cfg.agentName+" ...")
			} else {
				check("Stop hook", true, "keeper-stop-hook.sh wired")
			}
		}
	}

	// 4. PreCompact hook present for THIS project (ON-058a: matched on (basename, HARMONIK_PROJECT=<projectDir>) pair).
	{
		if !settingsPresent {
			check("PreCompact hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			found, _ := findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh", cfg.projectDir)
			if !found {
				check("PreCompact hook", false, "keeper-precompact-hook.sh not found in hooks.PreCompact for this project — run: harmonik keeper enable "+cfg.agentName+" ...")
			} else {
				check("PreCompact hook", true, "keeper-precompact-hook.sh wired")
			}
		}
	}

	// 4b. SessionStart hook present for THIS project (hk-8prq). A keeper-managed
	// session WITHOUT this hook produces no <agent>.sid, so the keeper silently
	// degrades to the fallback identity path — flag it so the provisioning gap is
	// visible. (Matched on the (basename, HARMONIK_PROJECT=<projectDir>) pair.)
	{
		if !settingsPresent {
			check("SessionStart hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			found, _ := findHookForScript(settings, "SessionStart", "keeper-sessionstart-hook.sh", cfg.projectDir)
			if !found {
				check("SessionStart hook", false, "keeper-sessionstart-hook.sh not found in hooks.SessionStart for this project — single-writer .sid channel will be absent; run: harmonik keeper enable "+cfg.agentName+" ...")
			} else {
				check("SessionStart hook", true, "keeper-sessionstart-hook.sh wired (single-writer .sid channel)")
			}
		}
	}

	// 5. Gauge freshness: has <agent>.ctx been written?
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

	// 5b. .sid channel: has the SessionStart hook written <agent>.sid, and is it
	// a well-formed primary id? (hk-8prq) Absent or malformed → the keeper is on
	// the FALLBACK identity path (not an error, but flagged so the operator can
	// confirm the SessionStart hook is provisioned and has fired). When present
	// and well-formed, report its age and whether it AGREES with the gauge's
	// session_id — a disagreement is the value-drift signal a time-based TTL
	// cannot capture (the hook writes .sid only at session boundaries).
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
			// Cross-check against the gauge's authoritative-or-fallback id. Since
			// ReadCtxFile applies the .sid override, agreement here means the gauge
			// either carries the same id or was overridden — a mismatch can only
			// arise if the gauge held a different id the override rejected.
			if cf, _, ctxErr := keeper.ReadCtxFile(cfg.projectDir, cfg.agentName); ctxErr == nil && cf.SessionID != "" && cf.SessionID != sid {
				msg += fmt.Sprintf("; WARNING: gauge session_id %q differs — possible drift", cf.SessionID)
			}
			check("sid channel", true, msg)
		}
	}

	// 6. .idle ever written (Stop hook has fired at least once).
	{
		idlePath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".idle")
		if _, statErr := os.Stat(idlePath); statErr != nil {
			check("idle marker", false, fmt.Sprintf(".idle not found (%s) — Stop hook has not fired yet (missing hook or Claude Code not stopped since hook was added)", idlePath))
		} else {
			check("idle marker", true, ".idle present (Stop hook has fired)")
		}
	}

	// 7. .managed present; and when both managed and live SIDs are set, they must agree.
	// A mismatch means the keeper is bound to a dead session and will never act (blind).
	{
		managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".managed")
		if _, statErr := os.Stat(managedPath); statErr != nil {
			check("managed", false, ".managed marker absent — keeper is in passive mode (no handoff cycle). Add with: harmonik keeper enable --yes-destructive, or: touch "+managedPath)
		} else {
			managedSID, _ := keeper.ReadManagedSessionID(cfg.projectDir, cfg.agentName)
			if managedSID != "" {
				if cf, _, ctxErr := keeper.ReadCtxFile(cfg.projectDir, cfg.agentName); ctxErr == nil && cf.SessionID != "" && cf.SessionID != managedSID {
					check("managed", false, fmt.Sprintf("managed SID %q != live gauge/.sid SID %q — keeper bound to DEAD session (blind); restart watcher", managedSID, cf.SessionID))
				} else {
					check("managed", true, ".managed present (handoff cycle is LIVE)")
				}
			} else {
				check("managed", true, ".managed present (handoff cycle is LIVE)")
			}
		}
	}

	// 7c. Live keeper watcher: does a live keeper process hold the exclusive flock?
	// Uses LiveKeeperPresent (shared-flock probe) so it correctly distinguishes a
	// running keeper from a stale corpse lockfile.
	{
		fn := cfg.liveKeeperFn
		if fn == nil {
			fn = keeper.LiveKeeperPresent
		}
		if fn(cfg.projectDir, cfg.agentName) {
			check("live-watcher", true, "live keeper process is running")
		} else {
			check("live-watcher", false, "no live keeper watcher detected — start with: harmonik keeper --agent "+cfg.agentName)
		}
	}

	// 7d. Tmux pane liveness: verify the auto-resolved keeper inject-target
	// (<session>:agent) is a reachable pane. An absent session is not a failure
	// (the agent may simply not be running); only a live session with a missing
	// pane is flagged — that is the zsh :a modifier bug (hk-5266t): an unbraced
	// $session:agent in a zsh context gets its `:a` suffix treated as the
	// absolute-path modifier, rewriting the target to abspath($session)+"gent",
	// a nonexistent pane that silently swallows every keeper inject attempt.
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
		if target == "" {
			check("tmux-pane", true, "agent session not live — pane check skipped")
		} else {
			ok, paneErr := paneFn(target)
			if paneErr != nil {
				check("tmux-pane", true, fmt.Sprintf("pane check skipped (%v)", paneErr))
			} else if !ok {
				check("tmux-pane", false, fmt.Sprintf("pane %q not found — keeper inject-target is unreachable; verify the keeper was launched with a braced tmux target (${session}:agent, not $session:agent — zsh :a modifier silently rewrites unbraced form; hk-5266t)", target))
			} else {
				check("tmux-pane", true, fmt.Sprintf("pane %q is live", target))
			}
		}
	}

	// 8. ANTHROPIC_API_KEY-in-env risk.
	{
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			check("api-key-risk", false, "ANTHROPIC_API_KEY is set in environment — keeper-launched claude will bill the API credit pool, not the subscription. Unset it or use 'env -u ANTHROPIC_API_KEY harmonik keeper ...'")
		} else {
			check("api-key-risk", true, "ANTHROPIC_API_KEY not set in environment (good)")
		}
	}

	// 9. captain-tools bash-launcher drift guard — RETIRED (ES8 / hk-877k).
	// The bash captain launcher was deleted once native `harmonik start captain`
	// + `harmonik captain respawn` fully replaced it; there is no longer an
	// embedded script to compare a deployed copy against.

	// Print results.
	allOK := true
	for _, r := range results {
		symbol := "✓"
		if !r.ok {
			symbol = "✗"
			allOK = false
		}
		fmt.Fprintf(stdout, "  %s %-20s %s\n", symbol, r.name, r.message)
	}

	if allOK {
		fmt.Fprintf(stdout, "\nkeeper doctor: all checks passed for agent %q\n", cfg.agentName)
		return 0
	}
	failCount := 0
	for _, r := range results {
		if !r.ok {
			failCount++
		}
	}
	fmt.Fprintf(stderr, "\nkeeper doctor: %d check(s) failed for agent %q\n", failCount, cfg.agentName)
	return 1
}

// runKeeperDoctorAtBoot runs doctor at keeper boot and logs warnings to stderr.
// Non-fatal: the watcher still starts even if checks fail.
func runKeeperDoctorAtBoot(projectDir, agentName, settingsPath string) {
	cfg := doctorConfig{
		agentName:    agentName,
		projectDir:   projectDir,
		settingsPath: settingsPath,
	}
	// Use a prefix writer to mark all output as boot-time diagnostics.
	code := runKeeperDoctor(cfg, os.Stderr, os.Stderr)
	if code != 0 {
		fmt.Fprintf(os.Stderr, "keeper: boot doctor found gaps for agent %q (above) — some keeper features may be inactive\n", agentName)
	}
}

// ── Settings JSON helpers ─────────────────────────────────────────────────────

// readGlobalSettings reads and parses a Claude Code settings.json file.
// Returns an empty map if the file is absent.
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

// writeGlobalSettings writes a settings map to the given path.
// Creates the parent directory if needed. NOT atomic (suitable for user's
// home-dir settings file; backup is taken by the caller first).
func writeGlobalSettings(settingsPath string, settings map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("MkdirAll %q: %w", filepath.Dir(settingsPath), err)
	}
	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	content = append(content, '\n')
	//nolint:gosec // G306: 0644 matches conventions for user config files
	if err := os.WriteFile(settingsPath, content, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", settingsPath, err)
	}
	return nil
}

// mergeStatusLineStanza upserts the statusLine stanza in settings.
// Returns a short action string ("added", "updated", "unchanged").
func mergeStatusLineStanza(settings map[string]interface{}, canonicalCmd string) string {
	existing := getStatusLineCommand(settings)

	if strings.Contains(existing, "keeper-statusline.sh") {
		if existing == canonicalCmd && statusLineTypeIsCommand(settings) {
			return "unchanged"
		}
		// Stale, non-normalized, or missing the required "type":"command" — update
		// in place. The "type" field is mandatory: Claude Code rejects the entire
		// settings.json (disabling ALL hooks) if statusLine lacks it (hk-hs1).
		sl := getOrCreateStatusLine(settings)
		sl["type"] = "command"
		sl["command"] = canonicalCmd
		settings["statusLine"] = sl
		return "updated (normalized)"
	}

	// Add new stanza. "type":"command" is required alongside "command" — Claude
	// Code rejects a statusLine missing it and disables all hooks (hk-hs1).
	sl := getOrCreateStatusLine(settings)
	sl["type"] = "command"
	sl["command"] = canonicalCmd
	settings["statusLine"] = sl
	return "added"
}

// mergeHookStanza upserts a keeper hook stanza for the given event name.
// scriptBasename is used for deduplication (e.g. "keeper-stop-hook.sh").
// projectDir is the harmonik project root for ON-058a project-keyed dedup:
// an existing group matches only when BOTH the script basename AND
// "HARMONIK_PROJECT=<projectDir>" appear in its command. A group with the same
// basename but a different HARMONIK_PROJECT value is left untouched and a new
// sibling group is appended — enabling two distinct projects to coexist in the
// same hooks array (ON-058a(1–2)).
// Returns a short action string.
func mergeHookStanza(settings map[string]interface{}, eventName, scriptBasename, projectDir, canonicalCmd string) string {
	found, existingCmd := findHookForScript(settings, eventName, scriptBasename, projectDir)

	if found {
		if existingCmd == canonicalCmd {
			return "unchanged"
		}
		// Update existing entry to normalized form (same project, stale command).
		updateHookCommand(settings, eventName, scriptBasename, projectDir, canonicalCmd)
		return "updated (normalized)"
	}

	// Add new matcher group (no matching (basename, project) pair found).
	appendHookGroup(settings, eventName, canonicalCmd)
	return "added"
}

// getStatusLineCommand returns the current statusLine.command value, or "".
func getStatusLineCommand(settings map[string]interface{}) string {
	sl, ok := settings["statusLine"]
	if !ok || sl == nil {
		return ""
	}
	slMap, ok := sl.(map[string]interface{})
	if !ok {
		return ""
	}
	cmd, _ := slMap["command"].(string)
	return cmd
}

// statusLineTypeIsCommand reports whether the statusLine stanza already carries
// the required "type":"command" field. Claude Code rejects a statusLine that
// lacks it (disabling ALL hooks), so enable must normalize any stanza missing
// it even when the command string is otherwise canonical (hk-hs1).
func statusLineTypeIsCommand(settings map[string]interface{}) bool {
	sl, ok := settings["statusLine"]
	if !ok || sl == nil {
		return false
	}
	slMap, ok := sl.(map[string]interface{})
	if !ok {
		return false
	}
	t, _ := slMap["type"].(string)
	return t == "command"
}

// getOrCreateStatusLine returns the statusLine map, creating it if absent.
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

// findHookForScript scans hooks[eventName] for any entry whose command contains
// scriptBasename.  When projectDir is non-empty, the entry must also contain
// "HARMONIK_PROJECT=<projectDir>" — the ON-058a project-keyed dedup predicate.
// A basename match with a different HARMONIK_PROJECT value does NOT match.
// Returns (true, matchingCommand) if found.
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
			cmd, _ := eMap["command"].(string)
			if !strings.Contains(cmd, scriptBasename) {
				continue
			}
			// ON-058a: when projectDir is set, require the HARMONIK_PROJECT= value
			// to match THIS project's root.  A basename match for a peer project's
			// group must NOT be treated as a match for this project.
			if projectDir != "" && !strings.Contains(cmd, "HARMONIK_PROJECT="+projectDir) {
				continue
			}
			return true, cmd
		}
	}
	return false, ""
}

// updateHookCommand replaces the command in the first entry that contains scriptBasename
// AND (when projectDir is non-empty) contains "HARMONIK_PROJECT=<projectDir>".
// This is the ON-058a in-place normalize path: only the group matching THIS project's
// (basename, HARMONIK_PROJECT) pair is rewritten; peer projects' groups are untouched.
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
			cmd, _ := eMap["command"].(string)
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

// appendHookGroup adds a new matcher group to hooks[eventName].
func appendHookGroup(settings map[string]interface{}, eventName, cmd string) {
	// Ensure hooks map exists.
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

// ── Misc helpers ──────────────────────────────────────────────────────────────

// keeperScriptNames is the canonical list of keeper hook script filenames.
var keeperScriptNames = []string{
	"keeper-statusline.sh",
	"keeper-stop-hook.sh",
	"keeper-precompact-hook.sh",
	"keeper-sessionstart-hook.sh",
}

// autoDetectScriptsDir tries to find the keeper scripts. It checks, in order:
// any caller-supplied hint dir's scripts/ subdir (the project root / cwd), then
// paths relative to the running harmonik binary. If still not found, it extracts
// the embedded copies to <projectDir>/.harmonik/scripts/ and returns that path.
//
// The binary-relative candidates only work when harmonik is built into the repo
// (./bin/harmonik adjacent to ./scripts). A `go install`'d binary lands in
// $GOPATH/bin with no scripts/ nearby; the embedded-extract fallback handles that
// case so `harmonik start captain` works on any foreign project without --scripts-dir.
func autoDetectScriptsDir(hints ...string) string {
	var candidates []string

	// Caller hints first (project root, cwd) — these win for go-install'd binaries.
	for _, h := range hints {
		if h == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(h, "scripts"))
	}

	if exe, err := os.Executable(); err == nil {
		// Resolve symlinks so that `go install`'d binaries find the real path.
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		// Common layouts: binary in bin/ adjacent to scripts/ (source-tree build).
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

	// Fallback: extract the embedded keeper scripts to <projectDir>/.harmonik/scripts/
	// so a go-install'd binary on a foreign project can still wire keeper hooks.
	if len(hints) > 0 && hints[0] != "" {
		if dir, err := extractEmbeddedKeeperScripts(hints[0]); err == nil {
			return dir
		}
	}
	return ""
}

// extractEmbeddedKeeperScripts extracts the 4 embedded keeper hook scripts to a
// stable directory and returns that path. It is called as a fallback by
// autoDetectScriptsDir when no on-disk scripts/ directory is found.
//
// Extraction target: ~/.harmonik/scripts/ (preferred) so the wired hook path
// survives worktree and temp-directory cleanup. When HOME is unavailable the
// fallback is <projectDir>/.harmonik/scripts/ (the original behaviour). Writing
// to a worktree path is what caused the "missing temp path" stop-hook error:
// worktrees are cleaned up after merging, leaving the hook pointing at a deleted
// path. hk-xjr1n.
func extractEmbeddedKeeperScripts(projectDir string) (string, error) {
	// Prefer ~/.harmonik/scripts/ so the path outlives any worktree lifecycle.
	destDir := filepath.Join(projectDir, ".harmonik", "scripts")
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		destDir = filepath.Join(home, ".harmonik", "scripts")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
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

// buildKeeperRunCmd returns the `harmonik keeper` run command string for the
// given config, suitable for printing to the user.
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

// writeHandoffStub writes a minimal HANDOFF-<agent>.md stub at path.
func writeHandoffStub(path, agentName string) error {
	content := fmt.Sprintf("# HANDOFF-%s\n\n## State\n<!-- keeper will populate this on handoff -->\n\n## Active Work\n<!-- fill in before each session -->\n", agentName)
	//nolint:gosec // G306: 0644 matches conventions for doc files
	return os.WriteFile(path, []byte(content), 0o644)
}

// copyFile copies src to dst.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // G304: operator-supplied path
	if err != nil {
		return err
	}
	//nolint:gosec // G306: 0644 matches conventions for backup files
	return os.WriteFile(dst, data, 0o644)
}

// tmuxPaneExists reports whether a tmux pane with the given target exists.
// Returns (false, nil) when tmux is not installed.
func tmuxPaneExists(target string) (bool, error) {
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return false, fmt.Errorf("tmux not found on PATH")
	}
	//nolint:gosec // G204: target is operator-supplied tmux pane address
	cmd := exec.Command(tmuxPath, "display-message", "-t", target, "-p", "#W")
	if runErr := cmd.Run(); runErr != nil {
		return false, nil
	}
	return true, nil
}

// formatAge returns a human-readable age string.
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

// ── Usage strings ─────────────────────────────────────────────────────────────

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
  managed        .harmonik/keeper/<agent>.managed present (handoff cycle live)
  live-watcher   live keeper process holds the flock (watcher is actually running)
  api-key-risk   ANTHROPIC_API_KEY not set in environment

EXIT CODES
  0  All checks passed
  1  One or more checks failed (details printed to stdout)
  2  Unexpected positional argument or unrecognized flag (flag-only)
`
