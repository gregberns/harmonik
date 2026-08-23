package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/branching"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/workspace"
)

// BranchingConfig holds the per-bead branching fields extracted from the
// `## Branching` section per BI-009b. All fields are optional; a zero-value
// BranchingConfig means the section was absent or all keys were omitted,
// and the WM-005b precedence chain falls through to project-level or spec-level
// defaults.
type BranchingConfig struct {
	// StartFrom is the git ref (branch name or commit SHA) from which the task
	// branch is cut per WM-005b. Empty means absent → fall through to next tier.
	StartFrom string

	// LandsOn is the git branch onto which the task branch is landed per WM-005b
	// (spec vocabulary; bead body YAML key is target_branch per BI-009b).
	// Empty means absent → spec-level default is "main" per WM-005b.
	// Consumed by hk-icgp1 (landing strategy).
	LandsOn string

	// LandingStrategy controls squash vs cherry-pick landing per WM-019b.
	// Empty means absent → spec-level default is "squash".
	// Consumed by hk-icgp1 (landing strategy).
	LandingStrategy string

	// TargetRepo is the absolute path of a non-harmonik repository that this
	// bead's fix should land in. When set, the daemon CANNOT dispatch the bead
	// (cross-repo dispatch is not yet implemented — hk-3r3); beadRunOne reopens
	// the bead with a CrossRepoUnsupportedError so the operator knows to apply
	// the fix out-of-band. See docs/cross-repo-dispatch.md for the design note.
	TargetRepo string
}

const (
	specDefaultStartFrom       = "main"
	specDefaultLandsOn         = "main"
	specDefaultLandingStrategy = "squash"
)

// ErrProjectBranchingConfig is the typed error returned by resolveBranching
// when the project-level .harmonik/branching.yaml is present but malformed.
// Malformed YAML is operator-detectable and must NOT silently fall back to
// spec defaults (judgment call per hk-umxx4 brief).
type ErrProjectBranchingConfig struct {
	Cause error
}

func (e *ErrProjectBranchingConfig) Error() string {
	return fmt.Sprintf("daemon: project branching config error: %v", e.Cause)
}

func (e *ErrProjectBranchingConfig) Unwrap() error { return e.Cause }

func resolveBranching(ctx context.Context, beadBody, projectRoot, targetBranch string) (BranchingConfig, error) {
	beadCfg, parseErr := parseBranchingSection(beadBody)
	if parseErr != nil {
		warnBeadBodyParseError(ctx, parseErr)
	}
	return resolveBranchingFrom(ctx, beadCfg, projectRoot, targetBranch)
}

func warnBeadBodyParseError(ctx context.Context, parseErr error) {
	slog.WarnContext(ctx, "bead_body_parse_error",
		"subsystem", "beads-adapter",
		"parse_error", parseErr.Error(),
	)
}

func resolveBranchingFrom(_ context.Context, beadCfg BranchingConfig, projectRoot, targetBranch string) (BranchingConfig, error) {
	projDefaults, loadErr := branching.LoadCached(projectRoot)
	if loadErr != nil {
		return BranchingConfig{}, &ErrProjectBranchingConfig{Cause: loadErr}
	}

	return resolveBranchingWithDefaults(beadCfg, projDefaults, targetBranch), nil
}

func resolveBranchingWithDefaults(beadCfg BranchingConfig, projDefaults branching.Defaults, targetBranch string) BranchingConfig {
	specStartFrom := targetBranch
	if specStartFrom == "" {
		specStartFrom = specDefaultStartFrom
	}
	specLandsOn := targetBranch
	if specLandsOn == "" {
		specLandsOn = specDefaultLandsOn
	}

	return BranchingConfig{
		StartFrom:       firstNonEmpty(beadCfg.StartFrom, projDefaults.StartFrom, specStartFrom),
		LandsOn:         firstNonEmpty(beadCfg.LandsOn, projDefaults.LandsOn, specLandsOn),
		LandingStrategy: firstNonEmpty(beadCfg.LandingStrategy, string(projDefaults.LandingStrategy), specDefaultLandingStrategy),
		TargetRepo:      beadCfg.TargetRepo,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type branchingYAMLShape struct {
	StartFrom       string `yaml:"start_from"`
	LandsOn         string `yaml:"target_branch"` // bead body key is target_branch; spec vocab is lands_on
	LandingStrategy string `yaml:"landing_strategy"`
	// target_repo declares an out-of-repo landing target (hk-3r3). When present
	// the daemon reopens the bead with CrossRepoUnsupportedError — cross-repo
	// dispatch is not yet implemented. See docs/cross-repo-dispatch.md.
	TargetRepo string `yaml:"target_repo"`
}

func parseBranchingSection(beadBody string) (BranchingConfig, error) {
	const heading = "## Branching"
	headingIdx := -1
	for i, line := range splitLines(beadBody) {
		if strings.TrimRight(line, "\r") == heading {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		return BranchingConfig{}, nil
	}

	lines := splitLines(beadBody)
	sectionLines := lines[headingIdx+1:]
	for i, line := range sectionLines {
		trimmed := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmed, "## ") {
			sectionLines = sectionLines[:i]
			break
		}
	}
	sectionBody := strings.Join(sectionLines, "\n")

	yamlContent, found := extractFencedYAML(sectionBody)
	if !found {
		return BranchingConfig{}, fmt.Errorf(
			"parseBranchingSection: ## Branching section present but no fenced ```yaml block found")
	}

	var shape branchingYAMLShape
	if err := yaml.Unmarshal([]byte(yamlContent), &shape); err != nil {
		return BranchingConfig{}, fmt.Errorf(
			"parseBranchingSection: malformed YAML in ## Branching section: %w", err)
	}

	return BranchingConfig(shape), nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func extractFencedYAML(body string) (string, bool) {
	const openFence = "```yaml"
	const closeFence = "```"

	lines := splitLines(body)
	inBlock := false
	var contentLines []string

	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		if !inBlock {
			if trimmed == openFence {
				inBlock = true
				contentLines = contentLines[:0]
			}
			continue
		}
		if trimmed == closeFence {
			return strings.Join(contentLines, "\n"), true
		}
		contentLines = append(contentLines, line)
	}
	return "", false
}

func resolveStartFrom(ctx context.Context, repoRoot, ref string) (string, error) {
	sha, err := gitprobe.RevParse(ctx, repoRoot, "refs/heads/"+ref)
	if err == nil {
		return sha, nil
	}

	sha, err = gitprobe.RevParse(ctx, repoRoot, ref)
	if err == nil {
		return sha, nil
	}

	return "", &StartFromRefError{Ref: ref, Cause: err}
}

// StartFromRefError is the typed error returned by resolveStartFrom when the
// named ref cannot be resolved in the local repository.
type StartFromRefError struct {
	// Ref is the start_from value from the bead body.
	Ref string
	// Cause is the underlying git error from the last rev-parse attempt.
	Cause error
}

func (e *StartFromRefError) Error() string {
	return fmt.Sprintf("daemon: start_from ref %q not found in local repository: %v", e.Ref, e.Cause)
}

func (e *StartFromRefError) Unwrap() error { return e.Cause }

func resolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody, targetBranch string) (string, error) {
	beadCfg, parseErr := parseBranchingSection(beadBody)
	if parseErr != nil {
		warnBeadBodyParseError(ctx, parseErr)
	}
	plan, err := resolveBranchPlan(ctx, repoRoot, beadID, beadCfg, targetBranch, "")
	if err != nil {
		return "", err
	}
	return plan.ParentSHA, nil
}

type branchPlan struct {
	// Config is the merged WM-005b config: start_from, lands_on, landing
	// strategy and target repo.
	Config BranchingConfig

	// ParentSHA is Config.StartFrom resolved to a commit in repoRoot. It is a
	// branch tip in the common case, so it is time-varying: a sibling run that
	// merges moves it.
	ParentSHA string
}

func resolveBranchPlan(ctx context.Context, repoRoot, beadID string, beadCfg BranchingConfig, targetBranch, parentBeadID string) (branchPlan, error) {
	defaultBranch := targetBranch
	if defaultBranch == "" {
		defaultBranch = specDefaultStartFrom
	}
	if parentBeadID != "" {
		var deriveErr error
		defaultBranch, deriveErr = workspace.IntegrationBranchName(ctx, parentBeadID)
		if deriveErr != nil {
			return branchPlan{}, fmt.Errorf("daemon: resolveParentCommit for bead %s: derive parent integration branch: %w", beadID, deriveErr)
		}
	}

	cfg, resolveErr := resolveBranchingFrom(ctx, beadCfg, repoRoot, defaultBranch)
	if resolveErr != nil {
		return branchPlan{}, fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, resolveErr)
	}
	if parentBeadID != "" && (cfg.StartFrom == defaultBranch || cfg.LandsOn == defaultBranch) {
		base := targetBranch
		if base == "" {
			base = specDefaultStartFrom
		}
		if ensureErr := workspace.EnsureIntegrationBranch(ctx, repoRoot, defaultBranch, base); ensureErr != nil {
			return branchPlan{}, fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, ensureErr)
		}
	}

	sha, err := resolveStartFrom(ctx, repoRoot, cfg.StartFrom)
	if err != nil {
		return branchPlan{Config: cfg}, fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, err)
	}
	return branchPlan{Config: cfg, ParentSHA: sha}, nil
}

// CrossRepoUnsupportedError is the typed error returned when a bead's
// ## Branching section declares a target_repo that differs from the daemon's
// projectDir. Cross-repo dispatch is not yet implemented (hk-3r3); the operator
// must apply the fix out-of-band. See docs/cross-repo-dispatch.md.
//
// Deprecated: superseded by CrossRepoUnsafeError for the safelist-check path
// (hk-xfuc). CrossRepoUnsupportedError is retained for legacy compatibility.
type CrossRepoUnsupportedError struct {
	TargetRepo string
	ProjectDir string
}

func (e *CrossRepoUnsupportedError) Error() string {
	return fmt.Sprintf(
		"daemon: bead declares target_repo %q but daemon supervises %q; cross-repo dispatch not yet implemented (hk-3r3) — apply fix out-of-band, see docs/cross-repo-dispatch.md",
		e.TargetRepo, e.ProjectDir)
}

// CrossRepoUnsafeError is the typed error returned when a bead's
// ## Branching section declares a target_repo that is not in the daemon's
// allowed_repos safelist (hk-xfuc). The operator must add the repo to
// .harmonik/config.yaml under daemon.allowed_repos before dispatching
// cross-repo beads. See docs/cross-repo-dispatch.md.
type CrossRepoUnsafeError struct {
	TargetRepo string
	ProjectDir string
}

func (e *CrossRepoUnsafeError) Error() string {
	return fmt.Sprintf(
		"daemon: bead declares target_repo %q (daemon supervises %q) but it is not in the "+
			"allowed_repos safelist; add it to .harmonik/config.yaml daemon.allowed_repos "+
			"to enable cross-repo dispatch (hk-xfuc) — see docs/cross-repo-dispatch.md",
		e.TargetRepo, e.ProjectDir)
}

func isInAllowedRepos(targetRepo string, allowedRepos []string) bool {
	for _, r := range allowedRepos {
		if r == targetRepo {
			return true
		}
	}
	return false
}

// LandsOnProtectedError is the typed error returned when a bead's resolved
// lands_on branch is in the daemon's ProtectBranches set. A bead may NARROW
// its landing target (e.g. target=integration, bead says integration/sub)
// but must NEVER widen it to a protected branch (hk-ncwb3).
type LandsOnProtectedError struct {
	// LandsOn is the resolved lands_on value from the three-tier chain.
	LandsOn string
}

func (e *LandsOnProtectedError) Error() string {
	return fmt.Sprintf("daemon: bead lands_on %q is a protected branch; refusing dispatch (hk-ncwb3)", e.LandsOn)
}

// LandsOnRefError is the typed error returned by landTaskBranch when the
// resolved lands_on ref cannot be found in the local repository. Mirrors the
// StartFromRefError shape per the brief's judgment-call directive.
type LandsOnRefError struct {
	// Ref is the lands_on value after WM-005b resolution.
	Ref string
	// Cause is the underlying git error from the last rev-parse attempt.
	Cause error
}

func (e *LandsOnRefError) Error() string {
	return fmt.Sprintf("daemon: lands_on ref %q not found in local repository: %v", e.Ref, e.Cause)
}

func (e *LandsOnRefError) Unwrap() error { return e.Cause }

func resolveLandsOn(cfg BranchingConfig) string {
	if cfg.LandsOn != "" {
		return cfg.LandsOn
	}
	return specDefaultLandsOn
}

func landTaskBranch(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID string, cfg BranchingConfig) error {
	landsOn := resolveLandsOn(cfg)

	_, err := gitprobe.RevParse(ctx, repoRoot, "refs/heads/"+landsOn)
	if err != nil {
		_, err2 := gitprobe.RevParse(ctx, repoRoot, landsOn)
		if err2 != nil {
			return &LandsOnRefError{Ref: landsOn, Cause: err2}
		}
	}

	switch cfg.LandingStrategy {
	case "cherry-pick":
		return cherryPickLanding(ctx, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID)
	default:
		return squashLanding(ctx, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID)
	}
}

func squashLanding(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID string) error {
	mergeCmd := exec.CommandContext(ctx, "git", "merge", "--squash", "--strategy=ort", taskBranch)
	mergeCmd.Dir = mergeWorktreeDir
	mergeOut, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr != nil {
		return fmt.Errorf("daemon: squashLanding: git merge --squash %s onto %s: %w\n%s",
			taskBranch, landsOn, mergeErr, mergeOut)
	}

	msg := synthesizeMergeCommitMessage(taskBranch, runID, beadID)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", msg)
	commitCmd.Dir = mergeWorktreeDir
	commitOut, commitErr := commitCmd.CombinedOutput()
	if commitErr != nil {
		return fmt.Errorf("daemon: squashLanding: git commit after squash of %s: %w\n%s",
			taskBranch, commitErr, commitOut)
	}
	return nil
}

func cherryPickLanding(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID string) error {
	mergeBaseCmd := exec.CommandContext(ctx, "git", "merge-base", landsOn, taskBranch)
	mergeBaseCmd.Dir = repoRoot
	mergeBaseOut, mergeBaseErr := mergeBaseCmd.Output()
	if mergeBaseErr != nil {
		return fmt.Errorf("daemon: cherryPickLanding: git merge-base %s %s: %w",
			landsOn, taskBranch, mergeBaseErr)
	}
	mergeBase := strings.TrimRight(string(mergeBaseOut), "\n")

	taskTipCmd := exec.CommandContext(ctx, "git", "rev-parse", taskBranch)
	taskTipCmd.Dir = repoRoot
	taskTipOut, taskTipErr := taskTipCmd.Output()
	if taskTipErr != nil {
		return fmt.Errorf("daemon: cherryPickLanding: git rev-parse %s: %w", taskBranch, taskTipErr)
	}
	taskTip := strings.TrimRight(string(taskTipOut), "\n")

	if taskTip == mergeBase {
		return fmt.Errorf("daemon: cherryPickLanding: task branch %q has no commits beyond merge-base with %q (all-mechanical branch); must escalate per WM-019b",
			taskBranch, landsOn)
	}

	pickCmd := exec.CommandContext(ctx, "git", "cherry-pick", "--strategy=ort",
		mergeBase+".."+taskBranch)
	pickCmd.Dir = mergeWorktreeDir
	pickOut, pickErr := pickCmd.CombinedOutput()
	if pickErr != nil {
		return fmt.Errorf("daemon: cherryPickLanding: git cherry-pick --strategy=ort %s..%s onto %s: %w\n%s",
			mergeBase, taskBranch, landsOn, pickErr, pickOut)
	}

	slog.InfoContext(ctx, "cherry_pick_landing_complete",
		"task_branch", taskBranch,
		"lands_on", landsOn,
		"run_id", runID,
		"bead_id", beadID,
	)
	return nil
}

func synthesizeMergeCommitMessage(taskBranch, runID, beadID string) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "squash(%s): task branch landing\n", taskBranch)
	b.WriteString("\n")
	fmt.Fprintf(&b, "Harmonik-Run-ID: %s\n", runID)
	if beadID != "" {
		fmt.Fprintf(&b, "Harmonik-Bead-ID: %s\n", beadID)
	}
	return b.String()
}
