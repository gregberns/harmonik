package daemon_test

// branching_hkoe6zt_test.go — unit tests for parseBranchingSection,
// resolveParentCommit, and the factory's start_from integration.
//
// Spec refs:
//   - specs/beads-integration.md §4.3 BI-009b
//   - specs/workspace-model.md §4.2 WM-005b, WM-003
//
// Helper prefix: branchingFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-oe6zt).

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// branchingFixtureBody builds a bead description body containing a
// `## Branching` section with the given YAML content.
func branchingFixtureBody(t *testing.T, yamlContent string) string {
	t.Helper()
	return "## Summary\n\nSome work.\n\n## Branching\n\n```yaml\n" + yamlContent + "\n```\n"
}

// branchingFixtureBodyNoSection builds a bead description body with no
// `## Branching` section.
func branchingFixtureBodyNoSection(t *testing.T) string {
	t.Helper()
	return "## Summary\n\nSome work.\n\n## Implementation\n\nDo things.\n"
}

// branchingFixtureGitRepo initialises a minimal bare git repository in a temp
// directory and returns the repo root path. The repo has one commit on "main"
// so that refs/heads/main resolves. Also creates a branch "feature/foo" for
// ref-resolution tests.
func branchingFixtureGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	// Create an initial commit.
	f := filepath.Join(dir, "README")
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
		t.Fatalf("branchingFixtureGitRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	// Create a second branch.
	run("branch", "feature/foo")

	return dir
}

// branchingFixtureHEAD returns the HEAD commit SHA of the repo at dir.
func branchingFixtureHEAD(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("branchingFixtureHEAD: git rev-parse HEAD: %v", err)
	}
	return strings.TrimRight(string(out), "\n")
}

// ─────────────────────────────────────────────────────────────────────────────
// parseBranchingSection tests
// ─────────────────────────────────────────────────────────────────────────────

// TestParseBranchingSection_SectionAbsent verifies that a bead body without a
// ## Branching section returns a zero-value BranchingConfig (not an error).
func TestParseBranchingSection_SectionAbsent(t *testing.T) {
	t.Parallel()
	body := branchingFixtureBodyNoSection(t)
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error for absent section: %v", err)
	}
	if cfg.StartFrom != "" || cfg.LandsOn != "" || cfg.LandingStrategy != "" {
		t.Errorf("parseBranchingSection: expected zero-value BranchingConfig; got %+v", cfg)
	}
}

// TestParseBranchingSection_StartFrom verifies that start_from is extracted
// from the fenced YAML block correctly.
func TestParseBranchingSection_StartFrom(t *testing.T) {
	t.Parallel()
	body := branchingFixtureBody(t, "start_from: feature/foo")
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error: %v", err)
	}
	if cfg.StartFrom != "feature/foo" {
		t.Errorf("parseBranchingSection: StartFrom = %q; want %q", cfg.StartFrom, "feature/foo")
	}
}

// TestParseBranchingSection_AllFields verifies that all three fields are parsed
// from the YAML block.
func TestParseBranchingSection_AllFields(t *testing.T) {
	t.Parallel()
	body := branchingFixtureBody(t,
		"start_from: main\ntarget_branch: harmonik/integration\nlanding_strategy: cherry-pick",
	)
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error: %v", err)
	}
	if cfg.StartFrom != "main" {
		t.Errorf("StartFrom = %q; want %q", cfg.StartFrom, "main")
	}
	if cfg.LandsOn != "harmonik/integration" {
		t.Errorf("LandsOn = %q; want %q", cfg.LandsOn, "harmonik/integration")
	}
	if cfg.LandingStrategy != "cherry-pick" {
		t.Errorf("LandingStrategy = %q; want %q", cfg.LandingStrategy, "cherry-pick")
	}
}

// TestParseBranchingSection_UnknownKeysIgnored verifies that unrecognised YAML
// keys are silently ignored (forward-compatibility per BI-009b).
func TestParseBranchingSection_UnknownKeysIgnored(t *testing.T) {
	t.Parallel()
	body := branchingFixtureBody(t,
		"start_from: main\nfuture_key: some_value\nanother_future: 42",
	)
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error for unknown keys: %v", err)
	}
	if cfg.StartFrom != "main" {
		t.Errorf("StartFrom = %q; want %q", cfg.StartFrom, "main")
	}
}

// TestParseBranchingSection_MalformedYAML verifies that a malformed YAML block
// returns an error (BI-009b §Error handling).
func TestParseBranchingSection_MalformedYAML(t *testing.T) {
	t.Parallel()
	// Deliberately malformed: tab indentation in YAML block (which yaml.v3 accepts
	// in most cases) is fine, but a mapping key with no value followed by more text
	// produces a parse error.
	body := "## Branching\n\n```yaml\n: broken: yaml: [\n```\n"
	_, err := daemon.ExportedParseBranchingSection(body)
	if err == nil {
		t.Error("parseBranchingSection: expected error for malformed YAML; got nil")
	}
}

// TestParseBranchingSection_NoFencedBlock verifies that a ## Branching section
// present but containing no fenced YAML block returns an error.
func TestParseBranchingSection_NoFencedBlock(t *testing.T) {
	t.Parallel()
	body := "## Branching\n\nsome plain text here\n\n## Next\n\nstuff"
	_, err := daemon.ExportedParseBranchingSection(body)
	if err == nil {
		t.Error("parseBranchingSection: expected error when fenced block absent; got nil")
	}
}

// TestParseBranchingSection_NullValueTreatedAsAbsent verifies that a null YAML
// value is treated as absent (zero-value string) per BI-009b §Extraction.
func TestParseBranchingSection_NullValueTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	body := branchingFixtureBody(t, "start_from: ~\ntarget_branch:\n")
	cfg, err := daemon.ExportedParseBranchingSection(body)
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error for null values: %v", err)
	}
	if cfg.StartFrom != "" {
		t.Errorf("StartFrom: expected empty (null treated as absent); got %q", cfg.StartFrom)
	}
	if cfg.LandsOn != "" {
		t.Errorf("LandsOn: expected empty (null treated as absent); got %q", cfg.LandsOn)
	}
}

// TestParseBranchingSection_EmptyBody verifies that an empty bead body returns
// a zero-value config without error.
func TestParseBranchingSection_EmptyBody(t *testing.T) {
	t.Parallel()
	cfg, err := daemon.ExportedParseBranchingSection("")
	if err != nil {
		t.Fatalf("parseBranchingSection: unexpected error for empty body: %v", err)
	}
	if cfg.StartFrom != "" {
		t.Errorf("StartFrom = %q; want empty for empty body", cfg.StartFrom)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveParentCommit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveParentCommit_NoSection verifies that when the bead body has no
// ## Branching section, resolveParentCommit returns the HEAD SHA of the repo.
func TestResolveParentCommit_NoSection(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)
	wantSHA := branchingFixtureHEAD(t, dir)

	body := branchingFixtureBodyNoSection(t)
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-001", body, "")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for absent section: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want HEAD SHA %q", gotSHA, wantSHA)
	}
}

// TestResolveParentCommit_StartFromMain verifies that start_from: main resolves
// to the SHA of refs/heads/main in the test repo.
func TestResolveParentCommit_StartFromMain(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)
	wantSHA := branchingFixtureHEAD(t, dir) // main and HEAD are the same after fixture init

	body := branchingFixtureBody(t, "start_from: main")
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-002", body, "")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for start_from=main: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want main SHA %q", gotSHA, wantSHA)
	}
}

// TestResolveParentCommit_StartFromBranch verifies that start_from: feature/foo
// resolves to the correct branch SHA.
func TestResolveParentCommit_StartFromBranch(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)
	// feature/foo was branched from main at init time, so they share the same SHA.
	wantSHA := branchingFixtureHEAD(t, dir)

	body := branchingFixtureBody(t, "start_from: feature/foo")
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-003", body, "")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for start_from=feature/foo: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want branch SHA %q", gotSHA, wantSHA)
	}
}

// TestResolveParentCommit_StartFromSHA verifies that an explicit commit SHA
// passes through resolveStartFrom (bare git rev-parse path).
func TestResolveParentCommit_StartFromSHA(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)
	headSHA := branchingFixtureHEAD(t, dir)

	body := branchingFixtureBody(t, "start_from: "+headSHA)
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-004", body, "")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for explicit SHA: %v", err)
	}
	if gotSHA != headSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want %q", gotSHA, headSHA)
	}
}

// TestResolveParentCommit_MissingRef verifies that when start_from names a ref
// that does not exist locally, resolveParentCommit returns a typed
// StartFromRefError (fail-fast, no silent fallback to HEAD per WM-005b).
func TestResolveParentCommit_MissingRef(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)

	body := branchingFixtureBody(t, "start_from: nonexistent-branch-xyz")
	_, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-005", body, "")
	if err == nil {
		t.Fatal("resolveParentCommit: expected error for missing ref; got nil")
	}

	// The error chain MUST contain a StartFromRefError.
	var refErr *daemon.StartFromRefError
	if !errors.As(err, &refErr) {
		t.Errorf("resolveParentCommit: error type = %T; want *daemon.StartFromRefError in chain; err = %v", err, err)
	}
	if refErr != nil && refErr.Ref != "nonexistent-branch-xyz" {
		t.Errorf("StartFromRefError.Ref = %q; want %q", refErr.Ref, "nonexistent-branch-xyz")
	}
}

// TestResolveParentCommit_MalformedSection verifies that a present-but-malformed
// ## Branching section causes resolveParentCommit to fall back to HEAD (per
// BI-009b error handling: do not refuse to dispatch, emit warning, treat fields
// as absent).
func TestResolveParentCommit_MalformedSection(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)
	wantSHA := branchingFixtureHEAD(t, dir)

	// Malformed YAML — section present but unparseable.
	body := "## Branching\n\n```yaml\n: broken: yaml: [\n```\n"
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-006", body, "")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for malformed section (should fall back): %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want HEAD SHA %q (fallback)", gotSHA, wantSHA)
	}
}

// TestResolveParentCommit_TargetBranchUsedAsDefault verifies that when
// targetBranch is set and the bead body has no ## Branching section,
// resolveParentCommit resolves the tip of targetBranch rather than "main"
// (hk-ncwb3: worktrees must cut from the configured integration branch).
func TestResolveParentCommit_TargetBranchUsedAsDefault(t *testing.T) {
	t.Parallel()
	dir := branchingFixtureGitRepo(t)

	// "feature/foo" was created from main in the fixture and points to the same SHA.
	wantSHA := branchingFixtureHEAD(t, dir)

	body := branchingFixtureBodyNoSection(t)
	gotSHA, err := daemon.ExportedResolveParentCommit(t.Context(), dir, "test-bead-007", body, "feature/foo")
	if err != nil {
		t.Fatalf("resolveParentCommit: unexpected error for targetBranch default: %v", err)
	}
	if gotSHA != wantSHA {
		t.Errorf("resolveParentCommit: got SHA %q; want feature/foo SHA %q", gotSHA, wantSHA)
	}
}

// TestLandsOnProtectedError_Error verifies the error message format.
func TestLandsOnProtectedError_Error(t *testing.T) {
	t.Parallel()
	err := &daemon.ExportedLandsOnProtectedError{LandsOn: "main"}
	msg := err.Error()
	if !strings.Contains(msg, "main") {
		t.Errorf("LandsOnProtectedError.Error() = %q; expected to contain branch name %q", msg, "main")
	}
	if !strings.Contains(msg, "protected") {
		t.Errorf("LandsOnProtectedError.Error() = %q; expected to contain 'protected'", msg)
	}
}
