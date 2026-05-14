package daemon

// branching.go — per-bead branching configuration parser, ref resolver, and
// task-branch landing helper.
//
// Implements beads-integration.md §4.3 BI-009b parse contract:
// reads the `## Branching` section from a bead description body, extracts the
// fenced YAML block, and returns a BranchingConfig for use in the WM-005b
// precedence chain.
//
// Implements workspace-model.md §4.2 WM-005b ref resolution:
// resolveStartFrom converts a start_from git ref (branch name or SHA) to the
// commit SHA used as <parent_commit> in git worktree add -b.
//
// Implements workspace-model.md §4.5 WM-019b landing strategy selector:
// landTaskBranch dispatches to squash-merge or cherry-pick based on
// BranchingConfig.LandingStrategy, defaulting to squash.
//
// NOTE on vocabulary: the bead body YAML key is `target_branch` (BI-009b), but
// the spec and Go identifier use `lands_on` / LandsOn throughout (WM-005b,
// WM-019b). The YAML struct tag preserves the bead-body key; all Go identifiers
// use the spec vocabulary.
//
// NOTE on stale bead vocabulary: the bead body for hk-oe6zt uses the old key
// `base_branch` (pre-spec terminology). The spec (WM-005b, BI-009b) now uses
// `start_from`. Per implementer-protocol.md PATH-DISCREPANCY RULE (spec
// content wins), the code uses `start_from` throughout. The stale bead body
// vocabulary is a cosmetic artifact; no follow-up bead is warranted for that
// alone.
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009b
//   - specs/workspace-model.md §4.2 WM-005b, WM-003, §4.5 WM-019, WM-019b
//
// Beads: hk-oe6zt (parser + start_from resolver), hk-icgp1 (landing selector).

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/branching"
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
}

// Spec-level defaults per WM-005b (lowest precedence in the three-tier chain).
// These constants live here so callers outside branching.go need not import the
// spec text to know what the defaults are.
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

// resolveBranching applies the WM-005b three-tier precedence chain and returns
// a fully-merged BranchingConfig:
//
//  1. Bead body (parseBranchingSection) — highest precedence.
//  2. Project defaults (.harmonik/branching.yaml via branching.LoadCached).
//  3. Spec defaults (start_from=main, lands_on=main, landing_strategy=squash).
//
// Tier-1 parse errors: if the ## Branching section is present but malformed,
// the bead-body fields are treated as absent per BI-009b §Error handling and a
// structured-log warning is emitted. Tier-1 parse errors are NOT propagated to
// the caller.
//
// Tier-2 load errors: if .harmonik/branching.yaml is present but malformed,
// resolveBranching returns a wrapped *ErrProjectBranchingConfig. This is
// fail-fast behaviour: a malformed project config is operator-detectable and
// MUST NOT silently fall through to spec defaults.
//
// Tier-2 file absent: branching.LoadCached returns zero-value Defaults + nil
// error; resolveBranching continues to tier-3 spec defaults for any field not
// set by tier-1.
//
// Spec ref: specs/workspace-model.md §4.2 WM-005b.
// Bead: hk-umxx4.
func resolveBranching(ctx context.Context, beadBody, projectRoot string) (BranchingConfig, error) {
	// Tier 1: bead body.
	beadCfg, parseErr := parseBranchingSection(beadBody)
	if parseErr != nil {
		// BI-009b §Error handling: malformed section → warn + treat as absent.
		// The caller's bead ID is not available here; structured-log without it.
		slog.WarnContext(ctx, "bead_body_parse_error",
			"subsystem", "beads-adapter",
			"parse_error", parseErr.Error(),
		)
		// beadCfg is zero-value; all fields fall through.
	}

	// Tier 2: project-level .harmonik/branching.yaml defaults.
	projDefaults, loadErr := branching.LoadCached(projectRoot)
	if loadErr != nil {
		// Fail-fast: malformed YAML is operator-detectable.
		return BranchingConfig{}, &ErrProjectBranchingConfig{Cause: loadErr}
	}

	// Merge: bead-body fields win; project defaults fill unset slots; spec
	// defaults fill anything still unset.
	merged := BranchingConfig{
		StartFrom:       firstNonEmpty(beadCfg.StartFrom, projDefaults.StartFrom, specDefaultStartFrom),
		LandsOn:         firstNonEmpty(beadCfg.LandsOn, projDefaults.LandsOn, specDefaultLandsOn),
		LandingStrategy: firstNonEmpty(beadCfg.LandingStrategy, string(projDefaults.LandingStrategy), specDefaultLandingStrategy),
	}
	return merged, nil
}

// firstNonEmpty returns the first non-empty string from vals.
// If all are empty, returns "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// branchingYAMLShape is the flat key-value shape of the fenced YAML block per
// BI-009b §Content shape. Unrecognised keys are silently ignored by yaml.v3
// when the struct carries only known fields (yaml.v3 does not error on extras
// when Decoder.KnownFields is not called).
type branchingYAMLShape struct {
	StartFrom       string `yaml:"start_from"`
	LandsOn         string `yaml:"target_branch"` // bead body key is target_branch; spec vocab is lands_on
	LandingStrategy string `yaml:"landing_strategy"`
}

// parseBranchingSection extracts per-bead branching configuration from a bead
// description body per BI-009b.
//
// Returns a zero-value BranchingConfig when the `## Branching` section is
// absent — this is not an error; the caller falls through to project-level and
// spec-level defaults.
//
// Returns a non-nil error only when the section is PRESENT but unparseable (no
// fenced YAML block, malformed YAML, or nested YAML structure). In that case
// the caller MUST treat the BranchingConfig as absent (BI-009b §Error handling)
// and emit a bead_body_parse_error observability event.
//
// bead_body_parse_error event emission is the caller's responsibility; this
// function is a pure parser with no side effects.
func parseBranchingSection(beadBody string) (BranchingConfig, error) {
	// ── Detection (BI-009b §Detection) ────────────────────────────────────────
	//
	// Find "## Branching" on a line by itself.
	const heading = "## Branching"
	headingIdx := -1
	for i, line := range splitLines(beadBody) {
		if strings.TrimRight(line, "\r") == heading {
			headingIdx = i
			break
		}
	}
	if headingIdx == -1 {
		// Section absent — not an error.
		return BranchingConfig{}, nil
	}

	// ── Section extraction ────────────────────────────────────────────────────
	//
	// Section body = lines from headingIdx+1 to the next "## " heading (or EOF).
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

	// ── Fenced YAML block extraction (BI-009b §Content shape) ─────────────────
	//
	// The block MUST be delimited by ```yaml and ``` with no leading spaces.
	yamlContent, found := extractFencedYAML(sectionBody)
	if !found {
		return BranchingConfig{}, fmt.Errorf(
			"parseBranchingSection: ## Branching section present but no fenced ```yaml block found")
	}

	// ── YAML parse ────────────────────────────────────────────────────────────
	//
	// Use a strict-shape struct so unrecognised keys are silently ignored and
	// nested structure is not permitted by the struct shape (all fields are
	// string scalars). A malformed YAML block is a parse error per BI-009b.
	var shape branchingYAMLShape
	if err := yaml.Unmarshal([]byte(yamlContent), &shape); err != nil {
		return BranchingConfig{}, fmt.Errorf(
			"parseBranchingSection: malformed YAML in ## Branching section: %w", err)
	}

	// ── Absent-value normalisation (BI-009b §Extraction) ─────────────────────
	//
	// A key present with a null or empty-string value MUST be treated as absent.
	// yaml.Unmarshal already leaves the field as its zero value ("") when the
	// YAML value is null or empty, so no extra normalisation is needed.

	return BranchingConfig{
		StartFrom:       shape.StartFrom,
		LandsOn:         shape.LandsOn,
		LandingStrategy: shape.LandingStrategy,
	}, nil
}

// splitLines splits s on newline boundaries, preserving empty trailing lines
// so that line-index addressing is stable.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// extractFencedYAML finds the first ```yaml / ``` fenced block in body and
// returns the content between the fences (exclusive of fence lines).
// Returns ("", false) when no such block is present.
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
		// Inside block: check for close fence.
		if trimmed == closeFence {
			return strings.Join(contentLines, "\n"), true
		}
		contentLines = append(contentLines, line)
	}
	return "", false
}

// resolveStartFrom converts a start_from git ref (branch name or commit SHA)
// to its commit SHA by querying the local git repository at repoRoot.
//
// Resolution order per WM-005b (local refs only; no network fetch for MVH):
//  1. Try `git rev-parse refs/heads/<ref>` — exact branch name match.
//  2. Fall back to `git rev-parse <ref>` — covers explicit SHAs and
//     refs/remotes/origin/<ref> if <ref> is already a full refspec.
//
// No network fetch is performed. Fetching is deferred refinement (post-MVH).
//
// When the ref does not resolve locally, returns a typed StartFromRefError
// wrapping the underlying git error so the caller can emit a clear operator
// message and reopen the bead.
func resolveStartFrom(ctx context.Context, repoRoot, ref string) (string, error) {
	// Attempt 1: refs/heads/<ref> (precise branch-name lookup).
	sha, err := gitRevParse(ctx, repoRoot, "refs/heads/"+ref)
	if err == nil {
		return sha, nil
	}

	// Attempt 2: bare ref (covers explicit SHAs and full refspecs like origin/foo).
	sha, err = gitRevParse(ctx, repoRoot, ref)
	if err == nil {
		return sha, nil
	}

	// Both attempts failed: ref is not present locally.
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

// gitRevParse runs `git rev-parse <ref>` in repoRoot and returns the trimmed
// SHA on success. On non-zero exit it returns an error.
func gitRevParse(ctx context.Context, repoRoot, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", ref, err)
	}
	sha := strings.TrimRight(string(out), "\n")
	if sha == "" {
		return "", fmt.Errorf("git rev-parse %s: empty output", ref)
	}
	return sha, nil
}

// resolveParentCommit resolves the parent_commit SHA for worktree creation per
// WM-005b. It is the integration point called by beadRunOne before passing the
// SHA to the worktreeFactory.
//
// Precedence via resolveBranching (highest → lowest):
//  1. Per-bead start_from from the `## Branching` section (BI-009b).
//  2. Project-level default from .harmonik/branching.yaml (WM-005b tier 2).
//  3. Spec-level default: "main" (WM-005b).
//
// When start_from resolves to "main" (from any tier) and "main" exists locally,
// the returned SHA is the tip of refs/heads/main. When start_from is not
// resolvable, a *StartFromRefError is returned — the bead is reopened by the
// caller (fail-fast; no silent fallback per WM-005b).
//
// When .harmonik/branching.yaml is present but malformed, resolveBranching
// returns an *ErrProjectBranchingConfig — the bead is reopened (fail-fast;
// operator must fix the config before beads can be dispatched).
//
// When the ## Branching section is present but malformed, resolveBranching
// emits a structured-log warning and treats bead-body fields as absent; it
// falls through to project and spec defaults (BI-009b §Error handling).
func resolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody string) (string, error) {
	cfg, resolveErr := resolveBranching(ctx, beadBody, repoRoot)
	if resolveErr != nil {
		// Fail-fast: project config malformed → surface to caller for bead reopen.
		return "", fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, resolveErr)
	}

	// cfg.StartFrom is always non-empty after resolveBranching (spec default "main"
	// fills any unset tier). Resolve the ref to a commit SHA.
	sha, err := resolveStartFrom(ctx, repoRoot, cfg.StartFrom)
	if err != nil {
		// Fail-fast: start_from ref cannot be resolved locally.
		return "", fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, err)
	}
	return sha, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Landing strategy — hk-icgp1
// ─────────────────────────────────────────────────────────────────────────────

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

// resolveLandsOn returns the effective target ref for the merge-back.
// It receives a BranchingConfig that has already been merged by resolveBranching
// (all three tiers applied), so cfg.LandsOn is always non-empty. The fallback
// to specDefaultLandsOn is retained as a safety net for callers that supply an
// ad-hoc BranchingConfig directly (e.g. in tests).
func resolveLandsOn(cfg BranchingConfig) string {
	if cfg.LandsOn != "" {
		return cfg.LandsOn
	}
	return specDefaultLandsOn
}

// landTaskBranch executes the merge-back from taskBranch onto the target ref
// resolved from cfg per WM-005b / WM-019b. It operates inside mergeWorktreeDir
// (a scratch merge-worktree created by the caller per WM-019a option (b)).
//
// Dispatch:
//   - cfg.LandingStrategy == "cherry-pick" → cherryPickLanding (WM-019b).
//   - cfg.LandingStrategy == "" or "squash" → squashLanding (WM-019, default).
//
// The landing target ref is resolved via resolveLandsOn and validated to exist
// locally. If the ref cannot be found, a *LandsOnRefError is returned (fail-fast
// per the brief's judgment-call: explicit lands_on must resolve; no silent fallback).
//
// On merge conflict (non-zero git exit or conflict markers in status output) the
// error is returned as-is for the caller to surface via the WM-020 / WM-024
// conflict path — no new conflict route is invented here.
//
// Spec refs: WM-019, WM-019b, WM-020.
// Bead: hk-icgp1.
func landTaskBranch(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID string, cfg BranchingConfig) error {
	landsOn := resolveLandsOn(cfg)

	// Validate that lands_on resolves locally (fail-fast per judgment call).
	_, err := gitRevParse(ctx, repoRoot, "refs/heads/"+landsOn)
	if err != nil {
		// Try bare ref (explicit SHA or full refspec).
		_, err2 := gitRevParse(ctx, repoRoot, landsOn)
		if err2 != nil {
			return &LandsOnRefError{Ref: landsOn, Cause: err2}
		}
	}

	switch cfg.LandingStrategy {
	case "cherry-pick":
		return cherryPickLanding(ctx, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID)
	default:
		// "" or "squash" → squash (preserves existing behaviour, default per WM-019b).
		return squashLanding(ctx, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID)
	}
}

// squashLanding implements the squash merge-back per WM-019:
//
//	git merge --squash --strategy=ort <taskBranch>
//	git commit -m "<synthesized message with trailers>"
//
// The merge executes inside mergeWorktreeDir (a scratch worktree checked out at
// landsOn per WM-019a option (b)). The synthesized commit carries
// Harmonik-Run-ID and Harmonik-Bead-ID trailers per WM-019.
//
// Non-zero exit from git merge is returned as an error; the caller surfaces this
// via the WM-020 conflict path.
func squashLanding(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID string) error {
	// Step 1: git merge --squash --strategy=ort <taskBranch>
	mergeCmd := exec.CommandContext(ctx, "git", "merge", "--squash", "--strategy=ort", taskBranch)
	mergeCmd.Dir = mergeWorktreeDir
	mergeOut, mergeErr := mergeCmd.CombinedOutput()
	if mergeErr != nil {
		return fmt.Errorf("daemon: squashLanding: git merge --squash %s onto %s: %w\n%s",
			taskBranch, landsOn, mergeErr, mergeOut)
	}

	// Step 2: git commit with synthesized message + Harmonik trailers per WM-019.
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

// cherryPickLanding implements the cherry-pick landing per WM-019b:
//
//	git cherry-pick --strategy=ort <startCommit>..<taskBranch>
//
// The range form (exclusive lower bound) walks all checkpoint commits on
// taskBranch that are not reachable from the merge-base of taskBranch and
// landsOn. Per WM-019b, each cherry-picked commit retains the
// Harmonik-Run-ID and Harmonik-Bead-ID trailers from the source checkpoint;
// the committer is rewritten to the daemon identity (this is git's default
// behaviour when cherry-picking: author is preserved, committer is the person
// running the command — here the daemon process identity).
//
// WM-019b: "The `--strategy=ort` flag MUST be passed to each cherry-pick
// invocation." The cherry-pick range form passes it once and git applies it
// to all commits in the range.
//
// WM-019b: an all-mechanical task branch (no checkpoint commits) MUST NOT
// attempt cherry-pick; it escalates directly. We detect this by checking
// whether the cherry-pick range is empty before running the command.
//
// Conflict detection and escalation per WM-020 apply per-commit. Non-zero exit
// from git cherry-pick is returned as an error for the caller's WM-024 path.
func cherryPickLanding(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, landsOn, runID, beadID string) error {
	// Find the merge-base of taskBranch and landsOn. The cherry-pick range
	// starts from the merge-base (exclusive) to avoid replaying commits that
	// are already on landsOn.
	mergeBaseCmd := exec.CommandContext(ctx, "git", "merge-base", landsOn, taskBranch)
	mergeBaseCmd.Dir = repoRoot
	mergeBaseOut, mergeBaseErr := mergeBaseCmd.Output()
	if mergeBaseErr != nil {
		return fmt.Errorf("daemon: cherryPickLanding: git merge-base %s %s: %w",
			landsOn, taskBranch, mergeBaseErr)
	}
	mergeBase := strings.TrimRight(string(mergeBaseOut), "\n")

	// Check for empty range: if taskBranch tip == merge-base, there are no
	// commits to cherry-pick (all-mechanical branch). Escalate directly per WM-019b.
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

	// Cherry-pick the range mergeBase..taskBranch (exclusive lower bound) with
	// --strategy=ort per WM-019b. The range form handles multiple commits in
	// commit order.
	pickCmd := exec.CommandContext(ctx, "git", "cherry-pick", "--strategy=ort",
		mergeBase+".."+taskBranch)
	pickCmd.Dir = mergeWorktreeDir
	pickOut, pickErr := pickCmd.CombinedOutput()
	if pickErr != nil {
		return fmt.Errorf("daemon: cherryPickLanding: git cherry-pick --strategy=ort %s..%s onto %s: %w\n%s",
			mergeBase, taskBranch, landsOn, pickErr, pickOut)
	}

	// Log the run/bead provenance that WM-019b requires each cherry-picked
	// commit to retain. The trailers were already present on the source
	// checkpoint commits per EM-017; cherry-pick preserves them natively.
	// We emit a structured-log line here for observability.
	slog.InfoContext(ctx, "cherry_pick_landing_complete",
		"task_branch", taskBranch,
		"lands_on", landsOn,
		"run_id", runID,
		"bead_id", beadID,
	)
	return nil
}

// synthesizeMergeCommitMessage builds the squash-merge commit message per WM-019.
//
// The message carries a brief summary line plus Harmonik-Run-ID and
// Harmonik-Bead-ID trailers. When beadID is empty the bead trailer is omitted.
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
