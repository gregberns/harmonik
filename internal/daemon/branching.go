package daemon

// branching.go — per-bead branching configuration parser and ref resolver.
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
// NOTE on stale bead vocabulary: the bead body for hk-oe6zt uses the old key
// `base_branch` (pre-spec terminology). The spec (WM-005b, BI-009b) now uses
// `start_from`. Per implementer-protocol.md PATH-DISCREPANCY RULE (spec
// content wins), the code uses `start_from` throughout. The stale bead body
// vocabulary is a cosmetic artifact; no follow-up bead is warranted for that
// alone.
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009b
//   - specs/workspace-model.md §4.2 WM-005b, WM-003
//
// Bead: hk-oe6zt.

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
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

	// TargetBranch is the git branch onto which the task branch is landed per
	// WM-005b. Empty means absent → WM-006 derivation applies.
	// OUT OF SCOPE for hk-oe6zt — field parsed but not consumed by the factory
	// in this bead. Consumed by hk-icgp1 (landing strategy, wave-2).
	TargetBranch string

	// LandingStrategy controls squash vs cherry-pick landing per WM-019b.
	// OUT OF SCOPE for hk-oe6zt — field parsed but not consumed by the factory
	// in this bead. Consumed by hk-icgp1 (landing strategy, wave-2).
	LandingStrategy string
}

// branchingYAMLShape is the flat key-value shape of the fenced YAML block per
// BI-009b §Content shape. Unrecognised keys are silently ignored by yaml.v3
// when the struct carries only known fields (yaml.v3 does not error on extras
// when Decoder.KnownFields is not called).
type branchingYAMLShape struct {
	StartFrom       string `yaml:"start_from"`
	TargetBranch    string `yaml:"target_branch"`
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
		TargetBranch:    shape.TargetBranch,
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
// Precedence (highest → lowest):
//  1. Per-bead start_from from the `## Branching` section (BI-009b).
//  2. Spec-level default: HEAD of the project repository (WM-003).
//
// Project-level `.harmonik/branching.yaml` default (tier 2 in WM-005b) is
// OUT OF SCOPE for this bead (hk-zy9s3, dispatched in parallel).
//
// When start_from is present but the ref cannot be resolved locally, this
// function returns a *StartFromRefError — the bead is reopened by the caller.
// This is intentional fail-fast behaviour: a user who explicitly named a ref
// expects it to resolve; silent fallback to HEAD would silently mis-branch.
//
// When the ## Branching section is present but malformed, the function emits a
// structured-log warning (BI-009b §Error handling), treats start_from as absent,
// and falls back to HEAD.
func resolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody string) (string, error) {
	cfg, parseErr := parseBranchingSection(beadBody)
	if parseErr != nil {
		// BI-009b §Error handling: malformed section → warn + fall through to
		// spec-level default. Do NOT refuse to dispatch the bead.
		slog.WarnContext(ctx, "bead_body_parse_error",
			"subsystem", "beads-adapter",
			"bead_id", beadID,
			"parse_error", parseErr.Error(),
		)
		// cfg is zero-value; StartFrom is "".
	}

	if cfg.StartFrom != "" {
		sha, err := resolveStartFrom(ctx, repoRoot, cfg.StartFrom)
		if err != nil {
			// Fail-fast: explicit start_from ref that cannot be resolved → surface
			// error to caller for bead reopen. Do NOT silently fall back to HEAD.
			return "", fmt.Errorf("daemon: resolveParentCommit for bead %s: %w", beadID, err)
		}
		return sha, nil
	}

	// No start_from (absent or fell through): use HEAD per WM-003 / spec-level
	// default. Mirrors the pre-bead-branching behaviour of beadRunOne.
	return resolveHEAD(ctx, repoRoot)
}
