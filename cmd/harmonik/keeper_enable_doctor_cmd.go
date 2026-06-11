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

// runKeeperEnableEntry parses flags and delegates to runKeeperEnable.
func runKeeperEnableEntry(args []string, stdout, stderr io.Writer) int {
	var (
		projectDir     string
		scriptsDir     string
		tmuxTarget     string
		yesDestructive bool
	)

	// manual flag parse to match existing codebase convention
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, keeperEnableUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--scripts-dir" && i+1 < len(args):
			i++
			scriptsDir = args[i]
		case strings.HasPrefix(args[i], "--scripts-dir="):
			scriptsDir = strings.TrimPrefix(args[i], "--scripts-dir=")
		case args[i] == "--tmux" && i+1 < len(args):
			i++
			tmuxTarget = args[i]
		case strings.HasPrefix(args[i], "--tmux="):
			tmuxTarget = strings.TrimPrefix(args[i], "--tmux=")
		case args[i] == "--yes-destructive":
			yesDestructive = true
		default:
			rest = append(rest, args[i])
		}
	}

	if len(rest) < 1 {
		fmt.Fprintln(stderr, "harmonik keeper enable: agent name is required")
		fmt.Fprint(stderr, keeperEnableUsage)
		return 1
	}
	agentName := rest[0]

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

	// Resolve scripts dir.
	if scriptsDir == "" {
		scriptsDir = autoDetectScriptsDir()
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
	statusLineCmd := fmt.Sprintf("HARMONIK_PROJECT=%s HARMONIK_AGENT=%s %s",
		cfg.projectDir, cfg.agentName,
		filepath.Join(cfg.scriptsDir, "keeper-statusline.sh"))
	stopHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s HARMONIK_KEEPER_AGENT=%s %s",
		cfg.projectDir, cfg.agentName,
		filepath.Join(cfg.scriptsDir, "keeper-stop-hook.sh"))
	precompactHookCmd := fmt.Sprintf("HARMONIK_PROJECT=%s HARMONIK_KEEPER_AGENT=%s %s",
		cfg.projectDir, cfg.agentName,
		filepath.Join(cfg.scriptsDir, "keeper-precompact-hook.sh"))

	// Merge stanzas (idempotent).
	statusLineAction := mergeStatusLineStanza(settings, statusLineCmd)
	stopAction := mergeHookStanza(settings, "Stop", "keeper-stop-hook.sh", stopHookCmd)
	precompactAction := mergeHookStanza(settings, "PreCompact", "keeper-precompact-hook.sh", precompactHookCmd)

	fmt.Fprintf(stdout, "keeper enable: statusLine     — %s\n", statusLineAction)
	fmt.Fprintf(stdout, "keeper enable: Stop hook      — %s\n", stopAction)
	fmt.Fprintf(stdout, "keeper enable: PreCompact hook — %s\n", precompactAction)

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
}

// runKeeperDoctorSubcommand is the entry point for `harmonik keeper doctor`.
func runKeeperDoctorSubcommand(args []string) int {
	return runKeeperDoctorEntry(args, os.Stdout, os.Stderr)
}

// runKeeperDoctorEntry parses flags and delegates to runKeeperDoctor.
func runKeeperDoctorEntry(args []string, stdout, stderr io.Writer) int {
	var projectDir string

	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, keeperDoctorUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		default:
			rest = append(rest, args[i])
		}
	}

	if len(rest) < 1 {
		fmt.Fprintln(stderr, "harmonik keeper doctor: agent name is required")
		fmt.Fprint(stderr, keeperDoctorUsage)
		return 1
	}
	agentName := rest[0]

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
				// Sub-check: correct env-var name.
				if !strings.Contains(cmd, "HARMONIK_AGENT=") {
					check("statusLine.envvar", false, "statusLine.command missing HARMONIK_AGENT= — run: harmonik keeper enable to normalize")
				}
				// Sub-check: required "type":"command" field (hk-hs1). Without it
				// Claude Code rejects the whole settings.json and disables all hooks.
				if !statusLineTypeIsCommand(settings) {
					check("statusLine.type", false, `statusLine missing "type":"command" — Claude Code will reject settings.json; run: harmonik keeper enable to normalize`)
				}
			}
		}
	}

	// 3. Stop hook present.
	{
		if !settingsPresent {
			check("Stop hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			found, cmd := findHookForScript(settings, "Stop", "keeper-stop-hook.sh")
			if !found {
				check("Stop hook", false, "keeper-stop-hook.sh not found in hooks.Stop — run: harmonik keeper enable "+cfg.agentName+" ...")
			} else {
				check("Stop hook", true, "keeper-stop-hook.sh wired")
				if !strings.Contains(cmd, "HARMONIK_KEEPER_AGENT=") {
					check("Stop hook.envvar", false, "Stop hook command uses wrong env-var (want HARMONIK_KEEPER_AGENT=) — run: harmonik keeper enable to normalize")
				}
			}
		}
	}

	// 4. PreCompact hook present.
	{
		if !settingsPresent {
			check("PreCompact hook", false, "settings.json absent — run: harmonik keeper enable "+cfg.agentName+" ...")
		} else {
			found, cmd := findHookForScript(settings, "PreCompact", "keeper-precompact-hook.sh")
			if !found {
				check("PreCompact hook", false, "keeper-precompact-hook.sh not found in hooks.PreCompact — run: harmonik keeper enable "+cfg.agentName+" ...")
			} else {
				check("PreCompact hook", true, "keeper-precompact-hook.sh wired")
				if !strings.Contains(cmd, "HARMONIK_KEEPER_AGENT=") {
					check("PreCompact hook.envvar", false, "PreCompact hook command uses wrong env-var (want HARMONIK_KEEPER_AGENT=) — run: harmonik keeper enable to normalize")
				}
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

	// 6. .idle ever written (Stop hook has fired at least once).
	{
		idlePath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".idle")
		if _, statErr := os.Stat(idlePath); statErr != nil {
			check("idle marker", false, fmt.Sprintf(".idle not found (%s) — Stop hook has not fired yet (missing hook or Claude Code not stopped since hook was added)", idlePath))
		} else {
			check("idle marker", true, ".idle present (Stop hook has fired)")
		}
	}

	// 7. .managed present.
	{
		managedPath := filepath.Join(cfg.projectDir, ".harmonik", "keeper", cfg.agentName+".managed")
		if _, statErr := os.Stat(managedPath); statErr != nil {
			check("managed", false, ".managed marker absent — keeper is in passive mode (no handoff cycle). Add with: harmonik keeper enable --yes-destructive, or: touch "+managedPath)
		} else {
			check("managed", true, ".managed present (handoff cycle is LIVE)")
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
// Returns a short action string.
func mergeHookStanza(settings map[string]interface{}, eventName, scriptBasename, canonicalCmd string) string {
	found, existingCmd := findHookForScript(settings, eventName, scriptBasename)

	if found {
		if existingCmd == canonicalCmd {
			return "unchanged"
		}
		// Update existing entry to normalized form.
		updateHookCommand(settings, eventName, scriptBasename, canonicalCmd)
		return "updated (normalized)"
	}

	// Add new matcher group.
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
// scriptBasename.  Returns (true, matchingCommand) if found.
func findHookForScript(settings map[string]interface{}, eventName, scriptBasename string) (bool, string) {
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
			if strings.Contains(cmd, scriptBasename) {
				return true, cmd
			}
		}
	}
	return false, ""
}

// updateHookCommand replaces the command in the first entry that contains scriptBasename.
func updateHookCommand(settings map[string]interface{}, eventName, scriptBasename, newCmd string) {
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
			if strings.Contains(cmd, scriptBasename) {
				eMap["command"] = newCmd
				return
			}
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

// autoDetectScriptsDir tries to find the keeper scripts by looking relative to
// the running harmonik binary.  Returns "" if no scripts directory is found.
func autoDetectScriptsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// Resolve symlinks so that `go install`'d binaries find the real path.
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}

	// Common layouts: binary in bin/ adjacent to scripts/ (source tree or install).
	candidates := []string{
		filepath.Join(filepath.Dir(exe), "..", "scripts"),
		filepath.Join(filepath.Dir(exe), "scripts"),
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
	return ""
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

const keeperEnableUsage = `harmonik keeper enable <agent> — wire keeper stanzas into ~/.claude/settings.json

USAGE
  harmonik keeper enable <agent> [--project DIR] [--scripts-dir DIR] [--tmux TARGET] [--yes-destructive]

ARGUMENTS
  <agent>   Agent name (e.g. orchestrator, flywheel). Must not contain '/' or '..'.

FLAGS
  --project DIR        Harmonik project root (default: current working directory)
  --scripts-dir DIR    Directory containing keeper-*.sh scripts (auto-detected if not specified)
  --tmux TARGET        tmux pane target for the run command and pane validation (optional)
  --yes-destructive    Enable .managed marker creation (LIVE handoff cycle) and allow
                       known live agent names (flywheel, named-queues, controlpoints)

WHAT IT DOES
  1. Validates agent name and (without --yes-destructive) refuses known live agents
  2. Backs up existing ~/.claude/settings.json
  3. Merges statusLine, Stop hook, PreCompact hook stanzas — idempotent, normalizes env-var names
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
`

const keeperDoctorUsage = `harmonik keeper doctor <agent> — read-only drift validator for keeper setup

USAGE
  harmonik keeper doctor <agent> [--project DIR]

ARGUMENTS
  <agent>   Agent name to check (e.g. orchestrator, flywheel)

FLAGS
  --project DIR   Harmonik project root (default: current working directory)

CHECKS (all read-only; no filesystem mutations)
  binary         harmonik binary on PATH and not stale (>30 days old)
  statusLine     keeper-statusline.sh wired in ~/.claude/settings.json
  Stop hook      keeper-stop-hook.sh wired in hooks.Stop
  PreCompact     keeper-precompact-hook.sh wired in hooks.PreCompact
  gauge          .harmonik/keeper/<agent>.ctx exists and is fresh (<5 min)
  idle marker    .harmonik/keeper/<agent>.idle has been written (Stop hook fired)
  managed        .harmonik/keeper/<agent>.managed present (handoff cycle live)
  api-key-risk   ANTHROPIC_API_KEY not set in environment

EXIT CODES
  0  All checks passed
  1  One or more checks failed (details printed to stdout)
`
