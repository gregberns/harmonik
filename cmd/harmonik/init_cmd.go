package main

// init_cmd.go — `harmonik init` subcommand implementation.
//
// # Purpose (hk-y171w, PL-029)
//
// First-time bootstrap of a new project for harmonik. Creates the .harmonik/
// directory structure, provisions fleet skills and scaffold files from the
// binary-embedded asset bundle, writes project config files, initialises the
// beads database, renders AGENTS.md from the embedded template, and (optionally)
// starts the supervisor.
//
// # Steps
//
//  1. Precondition check (git repo present, binaries on PATH).
//  2. Create .harmonik/ subdirectories (events/, worktrees/, beads-intents/,
//     comms/, crew/, keeper/, queues/, intent/).
//  3. Run `br init --prefix <prefix>` to initialise the beads database
//     (skipped when .beads/ already exists, unless --force).
//  4. Write .harmonik/config.yaml (project-level daemon defaults).
//  5. Write .harmonik/branching.yaml (branching defaults).
//  6. Write .harmonik/.gitignore (excludes runtime files from git).
//  7. Provision 9 fleet skills from the embedded asset bundle →
//     .claude/skills/{captain,crew-launch,keeper,harmonik-dispatch,
//     harmonik-lifecycle,agent-comms,beads-cli,major-issue-fanout,orchestrator}.
//  8. Write scaffold files from the embedded asset bundle →
//     AGENT_INDEX.md, STATUS.md (TASKS.md retired — hk-5qey).
//  8a. Scaffold .harmonik/context/ tier files (project.yaml, captain-lanes.md,
//     roadmap.md) + seed HANDOFF.md at the repo root from embedded templates.
//  9. Render embedded AGENTS.template.md → AGENTS.md (substitutes
//     $PROJECT_DIR and $TARGET_BRANCH). AGENTS.md is the three-kinds ROUTER
//     (precedence + per-role load map + harmonik:managed markers).
// 10. Symlink CLAUDE.md → AGENTS.md.
// 11. Seed the goal-keeper schedule job in .harmonik/schedules.json (FW5,
//     hk-z25w) — every 1h command backstop; idle-trigger wiring is FW6.
// 12. (Optional) Run `harmonik supervise start --watch-restart` unless
//     --no-supervise is passed.
// 13. (Optional) Smoke test when --smoke is passed.
//
// # Idempotency
//
// By default each step is skipped when its output artifact already exists.
// Pass --force to overwrite existing files (br init --force, file overwrites).
//
// # --target-branch
//
// The real fail-closed enforcement of the target branch is owned by the daemon's
// own boot guard (branching.yaml lands_on → TargetBranch with flag > file >
// default precedence per WM-005b, plus protect-branch / forbid-default-main
// enforcement). init passes --target-branch through to config.yaml and
// branching.yaml without imposing its own guard.
//
// Bead refs: hk-y171w, hk-7iyh (fleet-portability T11), hk-da3k (fleet-portability T12).

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/schedule"
)

// initSkewHintFn is the injectable hook that prints the stale-assets hint after
// provisioning. Set to PrintSkewHintIfStale in production; can be overridden in
// tests that do not want filesystem side-effects.
var initSkewHintFn func(projectDir string, stderr io.Writer) = PrintSkewHintIfStale

// runInitSubcommand dispatches `harmonik init [flags]`.
//
// Exit codes:
//
//	0  — success
//	1  — argument, precondition, or I/O error
func runInitSubcommand(args []string) int {
	return runInit(args, os.Stdout, os.Stderr)
}

// runInit is the testable core of the init subcommand.
func runInit(args []string, stdout, stderr io.Writer) int {
	var (
		projectDir   string
		targetBranch string
		prefix       string
		doctorOnly   bool
		force        bool
		smoke        bool
		noSupervise  bool
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, initUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--target-branch" && i+1 < len(args):
			i++
			targetBranch = args[i]
		case strings.HasPrefix(args[i], "--target-branch="):
			targetBranch = strings.TrimPrefix(args[i], "--target-branch=")
		case args[i] == "--prefix" && i+1 < len(args):
			i++
			prefix = args[i]
		case strings.HasPrefix(args[i], "--prefix="):
			prefix = strings.TrimPrefix(args[i], "--prefix=")
		case args[i] == "--doctor":
			doctorOnly = true
		case args[i] == "--force":
			force = true
		case args[i] == "--smoke":
			smoke = true
		case args[i] == "--no-supervise":
			noSupervise = true
		}
	}

	// Resolve project directory.
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik init: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik init: cannot resolve project path %q: %v\n", projectDir, err)
		return 1
	}
	projectDir = absProject

	// Default values.
	if targetBranch == "" {
		targetBranch = "main"
	}
	if prefix == "" {
		prefix = deriveBeadPrefix(projectDir)
	}

	// Run doctor checks.
	if ok := runDoctorChecks(projectDir, stdout, stderr); !ok {
		return 1
	}
	if doctorOnly {
		fmt.Fprintln(stdout, "harmonik init --doctor: all checks passed")
		return 0
	}

	fmt.Fprintf(stdout, "harmonik init: bootstrapping project at %s (target-branch: %s)\n", projectDir, targetBranch)

	// Step 3: create .harmonik/ subdirectories.
	if code := mkdirAll(projectDir, stderr); code != 0 {
		return code
	}

	// Step 4: run br init --prefix.
	if code := runBrInit(projectDir, prefix, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 5: write .harmonik/config.yaml. The remote_control_prefix defaults to
	// the SAME value passed to `br init --prefix` so the beads prefix and the
	// Claude RC label prefix match out of the box (hk-igpg).
	if code := writeConfigYAML(projectDir, targetBranch, prefix, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 6: write .harmonik/branching.yaml.
	if code := writeBranchingYAML(projectDir, targetBranch, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 7: write .harmonik/.gitignore.
	if code := writeHarmonikGitignore(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 8: provision 8 fleet skills from the embedded asset bundle.
	if code := provisionSkills(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 10: write scaffold files (AGENT_INDEX.md, STATUS.md).
	if code := provisionScaffolds(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 10b: scaffold .harmonik/context/ tier files + seed HANDOFF.md.
	if code := provisionContextTiers(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 11: render AGENTS.md from embedded template.
	if code := renderAgentsMD(projectDir, targetBranch, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 12: symlink CLAUDE.md → AGENTS.md.
	if code := ensureClaudeMDSymlink(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 12b: seed the goal-keeper schedule job (flywheel FW5, hk-z25w).
	if code := seedGoalKeeperSchedule(projectDir, force, stdout, stderr); code != 0 {
		return code
	}

	// Step 13: start supervisor (optional).
	if !noSupervise {
		if code := maybeStartSupervise(projectDir, stdout, stderr); code != 0 {
			// Non-fatal: supervisor start failure is logged but does not abort init.
			fmt.Fprintf(stderr, "harmonik init: warning: supervisor start skipped (see above). Start manually with: harmonik supervise start --watch-restart\n")
		}
	}

	// Step 14: smoke test (optional).
	if smoke {
		if code := runSmokeTest(projectDir, stdout, stderr); code != 0 {
			return code
		}
	}

	// Emit the stale-assets hint when existing managed files are behind the
	// running binary. Fires on re-init of an existing project (--no-force skips
	// provisioning but the operator needs to know to run sync-assets).
	// Best-effort: errors and zero-change results are silently suppressed.
	if initSkewHintFn != nil {
		initSkewHintFn(projectDir, stderr)
	}

	fmt.Fprintln(stdout, "harmonik init: done")
	return 0
}

// deriveBeadPrefix derives a short lowercase bead prefix from a project
// directory path. It takes the base name, splits on word boundaries
// (hyphens, underscores, spaces, dots), and returns a slug:
//   - ≥2 words → leading letter of each word, up to 4 characters
//   - 1 word   → first 2 alphanumeric characters
//
// Falls back to "hk" only when the directory base name contains no usable
// alphanumeric characters.
func deriveBeadPrefix(projectDir string) string {
	base := strings.ToLower(filepath.Base(projectDir))
	words := strings.FieldsFunc(base, func(r rune) bool {
		return !('a' <= r && r <= 'z') && !('0' <= r && r <= '9')
	})
	if len(words) == 0 {
		return "hk"
	}
	if len(words) >= 2 {
		var slug strings.Builder
		for _, w := range words {
			if len(w) > 0 {
				slug.WriteByte(w[0])
			}
			if slug.Len() >= 4 {
				break
			}
		}
		if slug.Len() >= 2 {
			return slug.String()
		}
	}
	// Single word (or too-short initials): use first 2 characters.
	word := words[0]
	if len(word) >= 2 {
		return word[:2]
	}
	return word + "x"
}

// runDoctorChecks verifies prerequisites and reports results.
// Returns true when all checks pass.
func runDoctorChecks(projectDir string, stdout, stderr io.Writer) bool {
	ok := true

	// Check: project dir exists.
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(stderr, "harmonik init: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		ok = false
	}

	// Check: git repo.
	gitDir := filepath.Join(projectDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		// Also check for worktrees (where .git is a file, not a directory).
		gitFile := filepath.Join(projectDir, ".git")
		info, ferr := os.Stat(gitFile)
		if ferr != nil || info.IsDir() == false {
			// More accurate: try `git -C <dir> rev-parse --git-dir`
			cmd := exec.Command("git", "-C", projectDir, "rev-parse", "--git-dir") //nolint:gosec // G204: projectDir is operator-controlled
			if runErr := cmd.Run(); runErr != nil {
				fmt.Fprintf(stderr, "harmonik init: %q is not a git repository (run git init first)\n", projectDir)
				ok = false
			}
		}
	}

	// Check: br on PATH.
	if _, err := exec.LookPath("br"); err != nil {
		fmt.Fprintf(stderr, "harmonik init: 'br' (beads CLI) not found on PATH — install beads_rust first\n")
		ok = false
	}

	// Check: harmonik on PATH.
	if _, err := exec.LookPath("harmonik"); err != nil {
		fmt.Fprintf(stderr, "harmonik init: 'harmonik' not found on PATH — ensure the binary is installed\n")
		ok = false
	}

	if ok {
		fmt.Fprintln(stdout, "harmonik init: precondition checks passed")
	}
	return ok
}

// mkdirAll creates the required .harmonik/ subdirectories (PL-029d).
func mkdirAll(projectDir string, stderr io.Writer) int {
	dirs := []string{
		filepath.Join(projectDir, ".harmonik"),
		filepath.Join(projectDir, ".harmonik", "events"),
		filepath.Join(projectDir, ".harmonik", "worktrees"),
		filepath.Join(projectDir, ".harmonik", "beads-intents"),
		filepath.Join(projectDir, ".harmonik", "comms"),
		filepath.Join(projectDir, ".harmonik", "crew"),
		filepath.Join(projectDir, ".harmonik", "keeper"),
		filepath.Join(projectDir, ".harmonik", "queues"),
		filepath.Join(projectDir, ".harmonik", "intent"),
	}
	for _, d := range dirs {
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(d, 0o755); err != nil {
			fmt.Fprintf(stderr, "harmonik init: mkdir %s: %v\n", d, err)
			return 1
		}
	}
	return 0
}

// runBrInit runs `br init --prefix <prefix>` in the project directory.
// Skipped when .beads/ already exists and force is false.
func runBrInit(projectDir, prefix string, force bool, stdout, stderr io.Writer) int {
	beadsDir := filepath.Join(projectDir, ".beads")
	if _, err := os.Stat(beadsDir); err == nil && !force {
		fmt.Fprintln(stdout, "harmonik init: .beads/ already exists — skipping br init (use --force to reinitialize)")
		return 0
	}

	brPath, err := exec.LookPath("br")
	if err != nil {
		fmt.Fprintf(stderr, "harmonik init: 'br' not on PATH — cannot initialise beads database\n")
		return 1
	}

	brArgs := []string{"init", "--prefix", prefix}
	if force {
		brArgs = append(brArgs, "--force")
	}
	//nolint:gosec // G204: brPath from LookPath; brArgs constructed from validated inputs
	cmd := exec.Command(brPath, brArgs...)
	cmd.Dir = projectDir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "harmonik init: br init failed: %v\n", err)
		return 1
	}
	return 0
}

// configYAMLContent is the project-level daemon config template.
//
// schema_version: 1 is emitted UNCOMMENTED so the file loads. parseProjectConfig
// requires schema_version == 1 once any daemon:/keeper: block is present (else it
// returns ErrUnsupportedConfigVersion). With it present, a third party may
// uncomment a SINGLE keeper line below to override one tunable without tripping
// the version check (hk-vxn8).
//
// The keeper: block is emitted COMPLETE and UNCOMMENTED (keeperConfigExampleYAML,
// the shared source of truth with `harmonik keeper config --example`). harmonik
// imposes NO built-in keeper defaults at runtime — every value must be set by the
// operator or the keeper refuses to start — so a generated project ships with a
// complete, valid, operator-editable keeper block. writeConfigYAML appends the
// shared block after this daemon/sentinel template.
const configYAMLContent = `# harmonik project configuration
# Generated by: harmonik init
# Spec ref: hk-y171w (harmonik init bootstrap), hk-9kgf (keeper config schema), hk-vxn8 (commented keeper template)
schema_version: 1
version: 1
daemon:
  # Branch harmonik merges completed bead branches into.
  target_branch: %s
  # Maximum number of beads dispatched concurrently.
  # Disk/CPU knee is ~4–5 on a 10-core machine.
  max_concurrent: 4
  # Default workflow mode: single, review-loop, dot
  workflow_mode: dot
  # Per-project prefix folded into Claude Code --remote-control session LABELS
  # (e.g. "%[2]s" -> "%[2]s-captain", "%[2]s-paul") so concurrent projects are
  # distinguishable in the global Remote-Control session picker. Defaults to the
  # beads issue prefix. Empty = bare label (the agent name verbatim). Cosmetic
  # only: HARMONIK_AGENT / tmux name / session-id stay bare. (hk-igpg)
  remote_control_prefix: %[2]s

# sentinel: configures the flywheel movement governor (flywheel-motion.md §7).
# Every field EXCEPT liveness_no_progress_n is optional (compiled defaults shown
# below as comments). liveness_no_progress_n is REQUIRED with no compiled default
# (hk-drygf): the daemon refuses to boot without it, so init emits it uncommented.
# Spec ref: flywheel-motion.md §7; bead hk-w0rm.
sentinel:
  # G-liveness self-kill: consecutive evaluation cycles with zero terminal progress
  # before the governor halts dispatch and pages (§6.1). REQUIRED — no compiled
  # default (hk-drygf); set 0 to explicitly disable the gate. Observe mode (the
  # default when 'mode' is unset below) makes this observe-only — the halt gains
  # teeth only under 'mode: act'.
  liveness_no_progress_n: 10

  # Sliding-window duration for terminal-progress movement scoring (§1.2).
  # window: 30m

  # Cold-start watermark: governor suppressed until this much time has elapsed
  # since daemon start (§1.4). Prevents false trips during the warm-up period.
  # warmup_window: 30m

  # Number of consecutive low-movement windows required before the governor
  # trips (§1.4). Prevents single-lull false alarms.
  # sustained_windows: 2

  # Per-event-type weight table (§1.1). Terminal-progress events carry weight;
  # starts/chatter carry 0. reviewer_verdict counts only on APPROVE verdict.
  # movement_weights:
  #   bead_closed: 10
  #   run_completed: 10
  #   reviewer_verdict: 10

  # Decaying TTL for operator-attached and operator-dialogue suppression (§3.2).
  # suppression_ttl: 10m

  # Inner guard that expires operator-attached suppression when no new
  # session_keeper_operator_attached events arrive within this window.
  # Must be ≤ suppression_ttl to be a meaningful inner guard.
  # attached_inactive_timeout: 5m

  # Operator-forced suppression label. Requires phase_flag_expiry (mandatory).
  # phase_flag: ""
  # phase_flag_expiry: ""

  # (liveness_no_progress_n is set uncommented at the top of this block — it is
  # REQUIRED with no compiled default, so it cannot live here as a comment.)

  # Per-class completion definition (§5.2). Default "merged" means done when
  # the Refs: trailer lands on origin/main. Phase-2 classes may supply a
  # deploy+verify command here.
  # done_definition:
  #   default: merged

# keeper: configures the per-session context-fill watcher (session-keeper).
# Spec refs: hk-9kgf (config schema). harmonik imposes NO built-in keeper defaults
# at runtime — EVERY value below must be set by the operator or the keeper refuses
# to start. The COMPLETE block below is generated from the same source of truth as
# 'harmonik keeper config --example'; tune the numbers, don't delete keys.
# Token/count fields are plain numbers; ALL duration fields are Go duration STRINGS
# ("5m", "120s") — a bare number is REJECTED.
`

// writeConfigYAML writes .harmonik/config.yaml. rcPrefix is written as
// daemon.remote_control_prefix; init passes the same value given to
// `br init --prefix` so the beads prefix and the Claude RC label prefix match
// out of the box (hk-igpg).
func writeConfigYAML(projectDir, targetBranch, rcPrefix string, force bool, stdout, stderr io.Writer) int {
	path := filepath.Join(projectDir, ".harmonik", "config.yaml")
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintln(stdout, "harmonik init: .harmonik/config.yaml already exists — skipping (use --force to overwrite)")
		return 0
	}
	// Append the COMPLETE keeper: and harnesses.pi: blocks from their single
	// sources of truth (shared with `harmonik keeper config --example` and
	// `harmonik pi config --example`) so a generated project starts with a valid,
	// operator-editable config: keeper has no runtime defaults, and folding in the
	// harnesses.pi block lets the daemon dispatch the Pi harness out of the box
	// (the operator tunes the suggested provider/model/api_key_env). YAML key order
	// is not semantic, so appending harnesses.pi after keeper is fine.
	content := fmt.Sprintf(configYAMLContent, targetBranch, rcPrefix) +
		keeperConfigExampleYAML() +
		piConfigExampleYAML()
	//nolint:gosec // G306: config file readable by owner only; 0644 matches conventions
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(stderr, "harmonik init: write .harmonik/config.yaml: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: wrote .harmonik/config.yaml")
	return 0
}

// branchingYAMLContent is the branching defaults template.
const branchingYAMLContent = `# harmonik branching defaults
# Generated by: harmonik init
# Spec ref: specs/workspace-model.md §4.2 WM-005b
version: 1
defaults:
  # Git ref the worktree branch is cut from (spec default: main).
  start_from: %s
  # Git ref the completed bead branch is merged into (spec default: main).
  lands_on: %s
  # Merge strategy: squash or cherry-pick (spec default: squash).
  landing_strategy: squash
`

// writeBranchingYAML writes .harmonik/branching.yaml.
func writeBranchingYAML(projectDir, targetBranch string, force bool, stdout, stderr io.Writer) int {
	path := filepath.Join(projectDir, ".harmonik", "branching.yaml")
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintln(stdout, "harmonik init: .harmonik/branching.yaml already exists — skipping (use --force to overwrite)")
		return 0
	}
	content := fmt.Sprintf(branchingYAMLContent, targetBranch, targetBranch)
	//nolint:gosec // G306
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(stderr, "harmonik init: write .harmonik/branching.yaml: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: wrote .harmonik/branching.yaml")
	return 0
}

// harmonikGitignoreContent lists the runtime files that should not be committed.
const harmonikGitignoreContent = `# harmonik runtime files — not committed
# Generated by: harmonik init
daemon.pid
daemon.sock
events/
worktrees/
cognition/
beads-intents/
queue.json
comms/
crew/
keeper/
queues/
# goal-keeper schedule flock sidecar (runtime lock — not committed)
schedules.json.lock
# review-loop verdict files (hk-znou: must not be committed onto run branches)
review.json
review.iter-*.json
`

// writeHarmonikGitignore writes .harmonik/.gitignore.
func writeHarmonikGitignore(projectDir string, force bool, stdout, stderr io.Writer) int {
	path := filepath.Join(projectDir, ".harmonik", ".gitignore")
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Fprintln(stdout, "harmonik init: .harmonik/.gitignore already exists — skipping (use --force to overwrite)")
		return 0
	}
	//nolint:gosec // G306
	if err := os.WriteFile(path, []byte(harmonikGitignoreContent), 0o644); err != nil {
		fmt.Fprintf(stderr, "harmonik init: write .harmonik/.gitignore: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: wrote .harmonik/.gitignore")
	return 0
}

// renderAgentsMD reads the embedded AGENTS.template.md (foreign-repo variant of
// the three-kinds ROUTER: precedence + per-role load map + harmonik:managed
// agents-router markers), substitutes $PROJECT_DIR and $TARGET_BRANCH, and
// writes AGENTS.md at the project root. Skipped when AGENTS.md already exists
// and force is false.
func renderAgentsMD(projectDir, targetBranch string, force bool, stdout, stderr io.Writer) int {
	outPath := filepath.Join(projectDir, "AGENTS.md")
	if _, err := os.Stat(outPath); err == nil && !force {
		fmt.Fprintln(stdout, "harmonik init: AGENTS.md already exists — skipping (use --force to overwrite)")
		return 0
	}

	data, err := initSkillAssets.ReadFile("assets/templates/AGENTS.template.md")
	if err != nil {
		fmt.Fprintf(stderr, "harmonik init: read embedded AGENTS.template.md: %v\n", err)
		return 1
	}

	rendered := strings.ReplaceAll(string(data), "$PROJECT_DIR", projectDir)
	rendered = strings.ReplaceAll(rendered, "$TARGET_BRANCH", targetBranch)

	//nolint:gosec // G306
	if err := os.WriteFile(outPath, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(stderr, "harmonik init: write AGENTS.md: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: wrote AGENTS.md")
	return 0
}

// provisionSkills extracts the 9 fleet skills from the embedded asset bundle
// into the project's .claude/skills/ directory (PL-029a).
// Idempotent: skips files that already exist unless force is true.
// Does NOT delete or overwrite sibling skill directories (PL-029e).
func provisionSkills(projectDir string, force bool, stdout, stderr io.Writer) int {
	skillsRoot := filepath.Join(projectDir, ".claude", "skills")
	//nolint:gosec // G301: 0755 for .claude/skills/
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		fmt.Fprintf(stderr, "harmonik init: mkdir .claude/skills: %v\n", err)
		return 1
	}

	skillEntries, err := initSkillAssets.ReadDir("assets/skills")
	if err != nil {
		fmt.Fprintf(stderr, "harmonik init: read embedded assets/skills: %v\n", err)
		return 1
	}

	for _, skillEntry := range skillEntries {
		if !skillEntry.IsDir() {
			continue
		}
		skillName := skillEntry.Name()
		skillDir := filepath.Join(skillsRoot, skillName)
		//nolint:gosec // G301
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			fmt.Fprintf(stderr, "harmonik init: mkdir .claude/skills/%s: %v\n", skillName, err)
			return 1
		}

		fileEntries, err := initSkillAssets.ReadDir("assets/skills/" + skillName)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik init: read embedded skill %s: %v\n", skillName, err)
			return 1
		}

		for _, fileEntry := range fileEntries {
			if fileEntry.IsDir() {
				continue
			}
			destPath := filepath.Join(skillDir, fileEntry.Name())
			if _, statErr := os.Stat(destPath); statErr == nil && !force {
				continue
			}
			content, err := initSkillAssets.ReadFile("assets/skills/" + skillName + "/" + fileEntry.Name())
			if err != nil {
				fmt.Fprintf(stderr, "harmonik init: read embedded skill file %s/%s: %v\n", skillName, fileEntry.Name(), err)
				return 1
			}
			//nolint:gosec // G306
			if err := os.WriteFile(destPath, content, 0o644); err != nil {
				fmt.Fprintf(stderr, "harmonik init: write .claude/skills/%s/%s: %v\n", skillName, fileEntry.Name(), err)
				return 1
			}
		}
		fmt.Fprintf(stdout, "harmonik init: provisioned .claude/skills/%s\n", skillName)
	}
	return 0
}

// provisionScaffolds writes the minimal scaffold files (AGENT_INDEX.md,
// STATUS.md) from the embedded asset bundle (PL-029c).
// Skipped when each file already exists and force is false.
//
// TASKS.md is NOT scaffolded — it is retired in the three-kinds instruction
// model (hk-5qey). This-session work lives in HANDOFF.md (tier-1) and the
// lane/epic tracker in .harmonik/context/captain-lanes.md (tier-2), both
// rendered by provisionContextTiers below.
func provisionScaffolds(projectDir string, force bool, stdout, stderr io.Writer) int {
	scaffolds := []string{"AGENT_INDEX.md", "STATUS.md"}
	for _, name := range scaffolds {
		outPath := filepath.Join(projectDir, name)
		if _, err := os.Stat(outPath); err == nil && !force {
			fmt.Fprintf(stdout, "harmonik init: %s already exists — skipping (use --force to overwrite)\n", name)
			continue
		}
		content, err := initSkillAssets.ReadFile("assets/scaffolds/" + name)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik init: read embedded scaffold %s: %v\n", name, err)
			return 1
		}
		//nolint:gosec // G306
		if err := os.WriteFile(outPath, content, 0o644); err != nil {
			fmt.Fprintf(stderr, "harmonik init: write %s: %v\n", name, err)
			return 1
		}
		fmt.Fprintf(stdout, "harmonik init: wrote %s\n", name)
	}
	return 0
}

// provisionContextTiers scaffolds the operational-state tier files from the
// embedded asset bundle (three-kinds instruction model, hk-5qey). It creates
// .harmonik/context/ and renders the tier templates:
//
//	assets/context/project.yaml.tmpl       → .harmonik/context/project.yaml   (tier-3)
//	assets/context/captain-lanes.md.tmpl   → .harmonik/context/captain-lanes.md (tier-2)
//	assets/context/roadmap.md.tmpl         → .harmonik/context/roadmap.md      (tier-4)
//	assets/context/HANDOFF.md.tmpl         → HANDOFF.md (repo root)            (tier-1)
//
// HANDOFF.md is a this-session file and lives at the repo root (where the
// captain/orchestrator expects it), NOT under .harmonik/context/.
//
// Idempotent: each output is skipped when it already exists and force is false.
func provisionContextTiers(projectDir string, force bool, stdout, stderr io.Writer) int {
	contextDir := filepath.Join(projectDir, ".harmonik", "context")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "harmonik init: mkdir .harmonik/context: %v\n", err)
		return 1
	}

	tiers := []struct {
		tmpl    string // asset path under assets/context/
		outPath string // destination, relative to projectDir
		label   string // for stdout messaging
	}{
		{"project.yaml.tmpl", filepath.Join(".harmonik", "context", "project.yaml"), ".harmonik/context/project.yaml"},
		{"captain-lanes.md.tmpl", filepath.Join(".harmonik", "context", "captain-lanes.md"), ".harmonik/context/captain-lanes.md"},
		{"roadmap.md.tmpl", filepath.Join(".harmonik", "context", "roadmap.md"), ".harmonik/context/roadmap.md"},
		{"HANDOFF.md.tmpl", "HANDOFF.md", "HANDOFF.md"},
	}

	for _, t := range tiers {
		outPath := filepath.Join(projectDir, t.outPath)
		if _, err := os.Stat(outPath); err == nil && !force {
			fmt.Fprintf(stdout, "harmonik init: %s already exists — skipping (use --force to overwrite)\n", t.label)
			continue
		}
		content, err := initSkillAssets.ReadFile("assets/context/" + t.tmpl)
		if err != nil {
			fmt.Fprintf(stderr, "harmonik init: read embedded context template %s: %v\n", t.tmpl, err)
			return 1
		}
		//nolint:gosec // G306
		if err := os.WriteFile(outPath, content, 0o644); err != nil {
			fmt.Fprintf(stderr, "harmonik init: write %s: %v\n", t.label, err)
			return 1
		}
		fmt.Fprintf(stdout, "harmonik init: wrote %s\n", t.label)
	}
	return 0
}

// ensureClaudeMDSymlink creates CLAUDE.md → AGENTS.md at the project root.
// Idempotent: skips when the symlink already points correctly.
// With --force: removes and recreates the symlink.
func ensureClaudeMDSymlink(projectDir string, force bool, stdout, stderr io.Writer) int {
	claudePath := filepath.Join(projectDir, "CLAUDE.md")
	target := "AGENTS.md" // relative symlink

	if info, err := os.Lstat(claudePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			// Check if it already points to AGENTS.md.
			existing, lerr := os.Readlink(claudePath)
			if lerr == nil && existing == target {
				fmt.Fprintln(stdout, "harmonik init: CLAUDE.md → AGENTS.md symlink already exists — skipping")
				return 0
			}
		}
		if !force {
			fmt.Fprintln(stdout, "harmonik init: CLAUDE.md already exists (not a symlink to AGENTS.md) — skipping (use --force to replace)")
			return 0
		}
		if err := os.Remove(claudePath); err != nil {
			fmt.Fprintf(stderr, "harmonik init: remove existing CLAUDE.md: %v\n", err)
			return 1
		}
	}

	if err := os.Symlink(target, claudePath); err != nil {
		fmt.Fprintf(stderr, "harmonik init: create CLAUDE.md symlink: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: created CLAUDE.md → AGENTS.md symlink")
	return 0
}

// maybeStartSupervise attempts to start the supervisor. Non-fatal: logs a
// warning and returns 1 when the daemon is not yet running (exit 17 from
// `harmonik supervise start`), since the operator may start it later.
func maybeStartSupervise(projectDir string, stdout, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		exe = "harmonik"
	}
	//nolint:gosec // G204: exe from os.Executable; projectDir operator-controlled
	cmd := exec.Command(exe, "supervise", "start", "--project", projectDir, "--watch-restart")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 17 {
				fmt.Fprintf(stderr, "harmonik init: daemon not running yet — supervisor start deferred (start daemon first, then run: harmonik supervise start --watch-restart)\n")
				return 1
			}
		}
		fmt.Fprintf(stderr, "harmonik init: supervise start: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: supervisor started")
	return 0
}

// runSmokeTest performs a basic post-init sanity check.
func runSmokeTest(projectDir string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stdout, "harmonik init --smoke: running sanity checks...")

	// Check: .harmonik/ exists.
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if _, err := os.Stat(harmonikDir); err != nil {
		fmt.Fprintf(stderr, "harmonik init --smoke: .harmonik/ missing: %v\n", err)
		return 1
	}

	// Check: .harmonik/config.yaml exists.
	if _, err := os.Stat(filepath.Join(harmonikDir, "config.yaml")); err != nil {
		fmt.Fprintf(stderr, "harmonik init --smoke: .harmonik/config.yaml missing\n")
		return 1
	}

	// Check: .harmonik/branching.yaml exists.
	if _, err := os.Stat(filepath.Join(harmonikDir, "branching.yaml")); err != nil {
		fmt.Fprintf(stderr, "harmonik init --smoke: .harmonik/branching.yaml missing\n")
		return 1
	}

	// Check: AGENTS.md exists.
	if _, err := os.Stat(filepath.Join(projectDir, "AGENTS.md")); err != nil {
		fmt.Fprintf(stderr, "harmonik init --smoke: AGENTS.md missing\n")
		return 1
	}

	// Check: br list exits 0 (database readable).
	brPath, err := exec.LookPath("br")
	if err == nil {
		//nolint:gosec // G204: brPath from LookPath
		brCmd := exec.Command(brPath, "list", "--status=open", "-q")
		brCmd.Dir = projectDir
		if runErr := brCmd.Run(); runErr != nil {
			fmt.Fprintf(stderr, "harmonik init --smoke: 'br list' failed: %v\n", runErr)
			return 1
		}
	}

	fmt.Fprintln(stdout, "harmonik init --smoke: all checks passed")
	return 0
}

// seedGoalKeeperSchedule registers the goal-keeper recurring job in
// .harmonik/schedules.json (flywheel FW5, hk-z25w). The job runs
// `harmonik goal-keeper --project <dir>` on an every-1h backstop cadence.
// Idempotent: skips if the job already exists and force is false.
func seedGoalKeeperSchedule(projectDir string, force bool, stdout, stderr io.Writer) int {
	store := schedule.NewStore(projectDir)
	if err := store.Load(); err != nil {
		fmt.Fprintf(stderr, "harmonik init: load schedules: %v\n", err)
		return 1
	}
	if _, ok := store.Get("goal-keeper"); ok && !force {
		fmt.Fprintln(stdout, "harmonik init: goal-keeper schedule already registered — skipping (use --force to overwrite)")
		return 0
	}
	job := schedule.ScheduledJob{
		ID: "goal-keeper",
		Schedule: schedule.Schedule{
			Kind:     schedule.ScheduleKindEvery,
			Interval: "1h",
		},
		Action: schedule.Action{
			Kind: schedule.ActionKindCommand,
			Argv: []string{"harmonik", "goal-keeper", "--project", projectDir},
		},
		Enabled: true,
	}
	if err := store.Add(job); err != nil {
		fmt.Fprintf(stderr, "harmonik init: seed goal-keeper schedule: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik init: registered goal-keeper schedule (every 1h)")
	return 0
}

const initUsage = `harmonik init — bootstrap a new project for use with harmonik

USAGE
  harmonik init [--project DIR] [--target-branch BRANCH] [--prefix PREFIX]
                [--doctor] [--force] [--smoke] [--no-supervise]

FLAGS
  --project DIR             Project directory (default: current working directory)
  --target-branch BRANCH    Branch harmonik merges completed work into (default: main)
  --prefix PREFIX           Bead ID prefix for 'br init' (default: derived from project directory name)
  --doctor                  Run precondition checks only; do not modify anything
  --force                   Overwrite existing files and reinitialise br database
  --smoke                   Run a smoke test after init to verify the setup
  --no-supervise            Skip 'harmonik supervise start --watch-restart'

WHAT IT DOES
  1. Checks preconditions (git repo, br/harmonik on PATH)
  2. Creates .harmonik/ directory structure (events/, worktrees/, beads-intents/,
     comms/, crew/, keeper/, queues/, intent/)
  3. Initialises the beads database: br init --prefix <PREFIX>
  4. Writes .harmonik/config.yaml (project-level daemon defaults)
  5. Writes .harmonik/branching.yaml (branching strategy defaults)
  6. Writes .harmonik/.gitignore (excludes runtime files)
  7. Provisions 9 fleet skills from the binary-embedded asset bundle →
     .claude/skills/{captain,crew-launch,keeper,harmonik-dispatch,
     harmonik-lifecycle,agent-comms,beads-cli,major-issue-fanout,orchestrator}
  8. Writes scaffold files: AGENT_INDEX.md, STATUS.md
 8a. Scaffolds .harmonik/context/ tier files (project.yaml, captain-lanes.md,
     roadmap.md) and seeds HANDOFF.md from embedded templates
  9. Renders embedded AGENTS.template.md (three-kinds router) → AGENTS.md
 10. Creates CLAUDE.md → AGENTS.md symlink
 11. Seeds goal-keeper schedule job in .harmonik/schedules.json (every 1h backstop)
 12. Starts the supervisor: harmonik supervise start --watch-restart
 13. (--smoke) Runs basic sanity checks

IDEMPOTENCY
  Each step is skipped when its output artifact already exists.
  Pass --force to overwrite existing files.

EXIT CODES
   0  Success
   1  Argument, precondition, or I/O error

EXAMPLES
  harmonik init
  harmonik init --project /path/to/project
  harmonik init --target-branch integration
  harmonik init --doctor
  harmonik init --force --smoke
  harmonik init --no-supervise --prefix bd
`
