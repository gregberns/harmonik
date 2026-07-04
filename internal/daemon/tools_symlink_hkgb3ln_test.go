package daemon_test

// tools_symlink_hkgb3ln_test.go — regression for hk-gb3ln: productionWorktreeFactory
// must symlink the project-root .tools/ directory into every worktree so that
// Makefile fmt/lint targets (gci, golangci-lint, gofumpt) can find their pinned
// binaries. Without the symlink, agents running `make fmt` or `make check-fast`
// silently skip formatting and linting because .tools/ is gitignored and absent
// from worktrees created by `git worktree add`.
//
// Helper prefix: hkgb3ln (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-gb3ln.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestProductionWorktreeFactory_ToolsSymlink verifies that productionWorktreeFactory
// creates a .tools symlink inside the new worktree pointing to the project root's
// .tools directory when it exists (hk-gb3ln).
//
// Scenarios covered:
//  1. .tools/ present in project root → worktree gets a .tools symlink; the
//     symlink target resolves to the project root .tools/ and a sentinel file
//     inside it is readable through the link.
//  2. .tools/ absent from project root → no symlink is created; worktree
//     creation still succeeds without error.
//
// Not parallel: subtests use t.Setenv(HARMONIK_CLAUDE_CONFIG_PATH).
func TestProductionWorktreeFactory_ToolsSymlink(t *testing.T) {
	t.Run("tools-present-symlink-created", func(t *testing.T) {
		projectDir, _ := workloopFixtureProjectDir(t)
		workloopFixtureGitRepo(t, projectDir)

		// Redirect EnsureWorktreeTrust away from ~/.claude.json so this test does
		// not race with a running daemon (same pattern as other hk-* tests).
		claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
		t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

		// Create a .tools/ directory in the project root with a sentinel binary.
		toolsDir := filepath.Join(projectDir, ".tools")
		//nolint:gosec // G301: test-only temp directory
		if err := os.MkdirAll(toolsDir, 0o755); err != nil {
			t.Fatalf("MkdirAll .tools: %v", err)
		}
		sentinelPath := filepath.Join(toolsDir, "gci")
		//nolint:gosec // G306: sentinel file is world-readable
		if err := os.WriteFile(sentinelPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("WriteFile sentinel: %v", err)
		}

		headSHA := hkgb3lnResolveHEAD(t, projectDir)

		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(t.Context(), projectDir, "hkgb3ln-tools-present", headSHA)
		if err != nil {
			t.Fatalf("productionWorktreeFactory: %v", err)
		}
		defer cleanup()

		// Verify .tools is a symlink inside the worktree.
		linkPath := filepath.Join(wtPath, ".tools")
		info, statErr := os.Lstat(linkPath)
		if statErr != nil {
			t.Fatalf(".tools not present in worktree: %v", statErr)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf(".tools in worktree is not a symlink (mode=%v)", info.Mode())
		}

		// Verify the symlink resolves to the project-root .tools/.
		target, readErr := os.Readlink(linkPath)
		if readErr != nil {
			t.Fatalf("Readlink .tools: %v", readErr)
		}
		if target != toolsDir {
			t.Errorf("symlink target = %q, want %q", target, toolsDir)
		}

		// Verify the sentinel file is reachable through the symlink.
		reachable := filepath.Join(linkPath, "gci")
		if _, reachErr := os.Stat(reachable); reachErr != nil {
			t.Errorf("sentinel file not reachable through symlink: %v", reachErr)
		}
	})

	t.Run("tools-absent-no-symlink", func(t *testing.T) {
		projectDir, _ := workloopFixtureProjectDir(t)
		workloopFixtureGitRepo(t, projectDir)

		claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
		t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

		// Deliberately do NOT create .tools/ in projectDir.

		headSHA := hkgb3lnResolveHEAD(t, projectDir)

		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(t.Context(), projectDir, "hkgb3ln-tools-absent", headSHA)
		if err != nil {
			t.Fatalf("productionWorktreeFactory: %v", err)
		}
		defer cleanup()

		// Verify no .tools entry exists in the worktree (no phantom symlink).
		linkPath := filepath.Join(wtPath, ".tools")
		if _, statErr := os.Lstat(linkPath); statErr == nil {
			t.Errorf(".tools unexpectedly present in worktree when project .tools/ is absent")
		}
	})
}

// hkgb3lnResolveHEAD resolves the HEAD SHA of the git repo at dir.
func hkgb3lnResolveHEAD(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("hkgb3lnResolveHEAD: git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}
