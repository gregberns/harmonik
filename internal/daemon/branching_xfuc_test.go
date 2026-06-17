package daemon_test

// branching_xfuc_test.go — unit tests for cross-repo dispatch safelist
// validation introduced by hk-xfuc.
//
// Tests are namespaced with the _xfuc suffix per the concurrency note in the
// bead description (sibling hk-s20z runs concurrently; helper names must not
// collide).

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestIsInAllowedRepos_xfuc verifies the safelist membership check.
func TestIsInAllowedRepos_xfuc(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		targetRepo   string
		allowedRepos []string
		want         bool
	}{
		{
			name:         "empty_safelist_returns_false",
			targetRepo:   "/Users/gb/github/kerf",
			allowedRepos: nil,
			want:         false,
		},
		{
			name:         "empty_safelist_slice_returns_false",
			targetRepo:   "/Users/gb/github/kerf",
			allowedRepos: []string{},
			want:         false,
		},
		{
			name:         "exact_match_returns_true",
			targetRepo:   "/Users/gb/github/kerf",
			allowedRepos: []string{"/Users/gb/github/kerf"},
			want:         true,
		},
		{
			name:         "match_in_multi_entry_list",
			targetRepo:   "/Users/gb/github/kerf",
			allowedRepos: []string{"/Users/gb/github/other", "/Users/gb/github/kerf"},
			want:         true,
		},
		{
			name:         "no_match_in_multi_entry_list",
			targetRepo:   "/Users/gb/github/unknown",
			allowedRepos: []string{"/Users/gb/github/kerf", "/Users/gb/github/other"},
			want:         false,
		},
		{
			name:         "prefix_does_not_match",
			targetRepo:   "/Users/gb/github/kerf-extra",
			allowedRepos: []string{"/Users/gb/github/kerf"},
			want:         false,
		},
		{
			name:         "trailing_slash_does_not_match",
			targetRepo:   "/Users/gb/github/kerf/",
			allowedRepos: []string{"/Users/gb/github/kerf"},
			want:         false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedIsInAllowedRepos(tc.targetRepo, tc.allowedRepos)
			if got != tc.want {
				t.Errorf("IsInAllowedRepos(%q, %v) = %v; want %v",
					tc.targetRepo, tc.allowedRepos, got, tc.want)
			}
		})
	}
}

// TestCrossRepoUnsafeError_xfuc verifies that CrossRepoUnsafeError produces a
// useful error message and satisfies errors.As.
func TestCrossRepoUnsafeError_xfuc(t *testing.T) {
	t.Parallel()

	err := &daemon.ExportedCrossRepoUnsafeError{
		TargetRepo: "/Users/gb/github/kerf",
		ProjectDir: "/Users/gb/github/harmonik",
	}

	msg := err.Error()
	if msg == "" {
		t.Fatal("CrossRepoUnsafeError.Error() returned empty string")
	}

	if !containsStr_xfuc(msg, "/Users/gb/github/kerf") {
		t.Errorf("error message does not mention target_repo: %q", msg)
	}
	if !containsStr_xfuc(msg, "/Users/gb/github/harmonik") {
		t.Errorf("error message does not mention project_dir: %q", msg)
	}

	// errors.As must work through a wrapped error.
	wrapped := fmt.Errorf("wrapping: %w", err)
	var target *daemon.ExportedCrossRepoUnsafeError
	if !errors.As(wrapped, &target) {
		t.Error("errors.As(wrapped, *CrossRepoUnsafeError) = false; want true")
	}
	if target.TargetRepo != "/Users/gb/github/kerf" {
		t.Errorf("target.TargetRepo = %q; want /Users/gb/github/kerf", target.TargetRepo)
	}
}

// TestParseBranchingSection_targetRepo_xfuc verifies that target_repo: in a
// bead's ## Branching section is extracted correctly. This is the tier-1 parse
// path used by the early cross-repo determination in beadRunOne (hk-xfuc).
func TestParseBranchingSection_targetRepo_xfuc(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		body           string
		wantTargetRepo string
	}{
		{
			name:           "no_branching_section",
			body:           "## Task Description\n\nDo something.\n",
			wantTargetRepo: "",
		},
		{
			name:           "branching_with_target_repo",
			body:           "## Task Description\n\nDo something.\n\n## Branching\n\n```yaml\ntarget_repo: /Users/gb/github/kerf\n```\n",
			wantTargetRepo: "/Users/gb/github/kerf",
		},
		{
			name:           "branching_with_target_repo_and_other_keys",
			body:           "## Branching\n\n```yaml\ntarget_branch: integration\ntarget_repo: /Users/gb/github/kerf\n```\n",
			wantTargetRepo: "/Users/gb/github/kerf",
		},
		{
			name:           "branching_without_target_repo",
			body:           "## Branching\n\n```yaml\nlands_on: feature-branch\n```\n",
			wantTargetRepo: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, _ := daemon.ExportedParseBranchingSection(tc.body)
			if cfg.TargetRepo != tc.wantTargetRepo {
				t.Errorf("TargetRepo = %q; want %q", cfg.TargetRepo, tc.wantTargetRepo)
			}
		})
	}
}

// TestAllowedRepos_configParsed_xfuc verifies that allowed_repos: in
// .harmonik/config.yaml is parsed into DaemonConfig.AllowedRepos (hk-xfuc).
func TestAllowedRepos_configParsed_xfuc(t *testing.T) {
	t.Parallel()

	dir := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  allowed_repos:
    - /Users/gb/github/kerf
    - /Users/gb/github/other
`)
	cfg, err := daemon.ExportedLoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if len(cfg.Daemon.AllowedRepos) != 2 {
		t.Fatalf("AllowedRepos: want 2 entries, got %d: %v", len(cfg.Daemon.AllowedRepos), cfg.Daemon.AllowedRepos)
	}
	if cfg.Daemon.AllowedRepos[0] != "/Users/gb/github/kerf" {
		t.Errorf("AllowedRepos[0]: want /Users/gb/github/kerf, got %q", cfg.Daemon.AllowedRepos[0])
	}
	if cfg.Daemon.AllowedRepos[1] != "/Users/gb/github/other" {
		t.Errorf("AllowedRepos[1]: want /Users/gb/github/other, got %q", cfg.Daemon.AllowedRepos[1])
	}
}

// TestAllowedRepos_absentMeansEmpty_xfuc verifies that omitting allowed_repos:
// yields an empty slice (no cross-repo dispatch allowed by default).
func TestAllowedRepos_absentMeansEmpty_xfuc(t *testing.T) {
	t.Parallel()

	dir := projCfgFixtureDir(t, `
schema_version: 1
daemon:
  max_concurrent: 2
`)
	cfg, err := daemon.ExportedLoadProjectConfig(dir)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if len(cfg.Daemon.AllowedRepos) != 0 {
		t.Errorf("AllowedRepos: want empty, got %v", cfg.Daemon.AllowedRepos)
	}
}

// containsStr_xfuc is a simple substring helper namespaced with the _xfuc
// suffix to avoid collisions with same-named helpers in sibling test files.
func containsStr_xfuc(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
