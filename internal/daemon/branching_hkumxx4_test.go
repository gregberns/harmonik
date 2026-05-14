package daemon_test

// branching_hkumxx4_test.go — unit tests for resolveBranching, wiring the
// WM-005b three-tier precedence chain (bead body → project YAML → spec default).
//
// Spec refs:
//   - specs/workspace-model.md §4.2 WM-005b
//   - specs/beads-integration.md §4.3 BI-009b
//
// Helper prefix: resolvingFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-umxx4).

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// resolvingFixtureTmpDir creates a temporary directory for use as a fake
// project root (no real .harmonik/branching.yaml is created — tests that need
// one create it explicitly).
func resolvingFixtureTmpDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// resolvingFixtureWriteBranchingYAML writes a .harmonik/branching.yaml file
// under root with the given content. Callers pass raw YAML content.
func resolvingFixtureWriteBranchingYAML(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("resolvingFixtureWriteBranchingYAML: MkdirAll %s: %v", dir, err)
	}
	p := filepath.Join(dir, "branching.yaml")
	//nolint:gosec // G304: path is constructed from t.TempDir() — test fixture
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("resolvingFixtureWriteBranchingYAML: WriteFile %s: %v", p, err)
	}
}

// resolvingFixtureBeadBody returns a bead description body containing a
// ## Branching section with the given YAML fields.
func resolvingFixtureBeadBody(t *testing.T, yamlContent string) string {
	t.Helper()
	return "## Summary\n\nSome work.\n\n## Branching\n\n```yaml\n" + yamlContent + "\n```\n"
}

// resolvingFixtureEmptyBody returns a bead body with no ## Branching section.
func resolvingFixtureEmptyBody(t *testing.T) string {
	t.Helper()
	return "## Summary\n\nSome work.\n\n## Implementation\n\nDo things.\n"
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveBranching unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestResolveBranching_BeadSetsStartFromOnly verifies that when the bead body
// sets only start_from, project YAML fills lands_on and landing_strategy.
// This is the canonical three-tier merge scenario from the brief.
func TestResolveBranching_BeadSetsStartFromOnly(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t)

	resolvingFixtureWriteBranchingYAML(t, root,
		"version: 1\ndefaults:\n  lands_on: harmonik/integration\n  landing_strategy: cherry-pick\n",
	)

	body := resolvingFixtureBeadBody(t, "start_from: feature/foo")

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err != nil {
		t.Fatalf("resolveBranching: unexpected error: %v", err)
	}
	if cfg.StartFrom != "feature/foo" {
		t.Errorf("StartFrom = %q; want %q", cfg.StartFrom, "feature/foo")
	}
	if cfg.LandsOn != "harmonik/integration" {
		t.Errorf("LandsOn = %q; want %q (from project YAML)", cfg.LandsOn, "harmonik/integration")
	}
	if cfg.LandingStrategy != "cherry-pick" {
		t.Errorf("LandingStrategy = %q; want %q (from project YAML)", cfg.LandingStrategy, "cherry-pick")
	}
}

// TestResolveBranching_BeadSetsAllThree verifies that when the bead body sets
// all three fields, the project YAML is ignored for those fields.
func TestResolveBranching_BeadSetsAllThree(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t)

	// Project YAML sets different values; they should be overridden by the bead body.
	resolvingFixtureWriteBranchingYAML(t, root,
		"version: 1\ndefaults:\n  start_from: develop\n  lands_on: harmonik/integration\n  landing_strategy: cherry-pick\n",
	)

	body := resolvingFixtureBeadBody(t,
		"start_from: main\ntarget_branch: release\nlanding_strategy: squash",
	)

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err != nil {
		t.Fatalf("resolveBranching: unexpected error: %v", err)
	}
	if cfg.StartFrom != "main" {
		t.Errorf("StartFrom = %q; want %q (bead body wins)", cfg.StartFrom, "main")
	}
	if cfg.LandsOn != "release" {
		t.Errorf("LandsOn = %q; want %q (bead body wins)", cfg.LandsOn, "release")
	}
	if cfg.LandingStrategy != "squash" {
		t.Errorf("LandingStrategy = %q; want %q (bead body wins)", cfg.LandingStrategy, "squash")
	}
}

// TestResolveBranching_EmptyBeadAndNoYAML verifies that when the bead body has
// no ## Branching section and .harmonik/branching.yaml is absent, all three
// fields resolve to the spec defaults (start_from=main, lands_on=main,
// landing_strategy=squash).
func TestResolveBranching_EmptyBeadAndNoYAML(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t) // no branching.yaml created

	body := resolvingFixtureEmptyBody(t)

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err != nil {
		t.Fatalf("resolveBranching: unexpected error for absent section+file: %v", err)
	}
	if cfg.StartFrom != "main" {
		t.Errorf("StartFrom = %q; want spec default %q", cfg.StartFrom, "main")
	}
	if cfg.LandsOn != "main" {
		t.Errorf("LandsOn = %q; want spec default %q", cfg.LandsOn, "main")
	}
	if cfg.LandingStrategy != "squash" {
		t.Errorf("LandingStrategy = %q; want spec default %q", cfg.LandingStrategy, "squash")
	}
}

// TestResolveBranching_MalformedProjectYAML verifies that a malformed
// .harmonik/branching.yaml causes resolveBranching to return a typed
// ErrProjectBranchingConfig error (fail-fast; must NOT silently fall back
// to spec defaults per hk-umxx4 judgment call).
func TestResolveBranching_MalformedProjectYAML(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t)

	// Write a deliberately invalid YAML file.
	resolvingFixtureWriteBranchingYAML(t, root, ": broken: yaml: [\n")

	body := resolvingFixtureEmptyBody(t)

	_, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err == nil {
		t.Fatal("resolveBranching: expected error for malformed project YAML; got nil")
	}

	var projErr *daemon.ExportedErrProjectBranchingConfig
	if !errors.As(err, &projErr) {
		t.Errorf("resolveBranching: error type = %T; want *daemon.ErrProjectBranchingConfig in chain; err = %v", err, err)
	}
}

// TestResolveBranching_FileAbsentSpecDefaultsApplied verifies that when
// .harmonik/branching.yaml does not exist (empty project root), spec defaults
// fill all unset fields without error.
func TestResolveBranching_FileAbsentSpecDefaultsApplied(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t) // no .harmonik dir at all

	body := resolvingFixtureBeadBody(t, "start_from: feature/bar")

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err != nil {
		t.Fatalf("resolveBranching: unexpected error when file absent: %v", err)
	}
	// Bead body supplies start_from; spec defaults fill the rest.
	if cfg.StartFrom != "feature/bar" {
		t.Errorf("StartFrom = %q; want %q", cfg.StartFrom, "feature/bar")
	}
	if cfg.LandsOn != "main" {
		t.Errorf("LandsOn = %q; want spec default %q", cfg.LandsOn, "main")
	}
	if cfg.LandingStrategy != "squash" {
		t.Errorf("LandingStrategy = %q; want spec default %q", cfg.LandingStrategy, "squash")
	}
}

// TestResolveBranching_ProjectYAMLPartialFillsGaps verifies that partial project
// YAML (only lands_on set) fills the gap left by an absent bead body field, and
// the remaining gap is filled by the spec default.
func TestResolveBranching_ProjectYAMLPartialFillsGaps(t *testing.T) {
	t.Parallel()
	root := resolvingFixtureTmpDir(t)

	// Only lands_on set in project YAML.
	resolvingFixtureWriteBranchingYAML(t, root,
		"version: 1\ndefaults:\n  lands_on: harmonik/release\n",
	)

	body := resolvingFixtureEmptyBody(t) // no bead body fields

	cfg, err := daemon.ExportedResolveBranching(t.Context(), body, root)
	if err != nil {
		t.Fatalf("resolveBranching: unexpected error: %v", err)
	}
	if cfg.StartFrom != "main" {
		t.Errorf("StartFrom = %q; want spec default %q", cfg.StartFrom, "main")
	}
	if cfg.LandsOn != "harmonik/release" {
		t.Errorf("LandsOn = %q; want %q (from project YAML)", cfg.LandsOn, "harmonik/release")
	}
	if cfg.LandingStrategy != "squash" {
		t.Errorf("LandingStrategy = %q; want spec default %q", cfg.LandingStrategy, "squash")
	}
}
