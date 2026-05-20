package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// claudeWTFixtureMakeGitRepo initialises a bare-minimum git repository at root
// so that `git worktree list --porcelain` is callable (needed to classify
// worktrees as registered or unregistered).
func claudeWTFixtureMakeGitRepo(t *testing.T, root string) {
	t.Helper()
	runClaudeWTGit(t, root, "init")
	runClaudeWTGit(t, root, "-c", "user.email=test@test.com", "-c", "user.name=Test",
		"commit", "--allow-empty", "-m", "init")
}

// runClaudeWTGit runs git -C dir <args> and fails the test on error.
func runClaudeWTGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmdArgs := append([]string{"-C", dir}, args...)
	//nolint:gosec // G204: git is a system binary; args are test-controlled
	cmd := exec.CommandContext(context.Background(), "git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runClaudeWTGit git %v: %v\nout: %s", args, err, out)
	}
}

// claudeWTFixtureMakeEntry creates a directory under
// <projectDir>/.claude/worktrees/<name>/ to simulate a worktree entry.
func claudeWTFixtureMakeEntry(t *testing.T, projectDir, name string) string {
	t.Helper()
	entryPath := filepath.Join(projectDir, DefaultClaudeWorktreesSubpath, name)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(entryPath, 0o755); err != nil {
		t.Fatalf("claudeWTFixtureMakeEntry: MkdirAll %q: %v", entryPath, err)
	}
	return entryPath
}

// ──────────────────────────────────────────────────────────────────────────────
// claudeWorktreeNameValid
// ──────────────────────────────────────────────────────────────────────────────

// TestHkYhq3m_ClaudeWorktreeNameValid verifies that claudeWorktreeNameValid
// accepts canonical "agent-<hex>" names and rejects everything else.
//
// Bead ref: hk-yhq3m — daemon orphan-sweep must also walk .claude/worktrees/.
func TestHkYhq3m_ClaudeWorktreeNameValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"canonical hex", "agent-a8e49d3ccd1f65def", true},
		{"short hex", "agent-abc123", true},
		{"mixed case", "agent-A1B2C3", true},
		{"missing prefix", "a8e49d3ccd1f65def", false},
		{"empty suffix", "agent-", false},
		{"underscore in suffix", "agent-a8e4_9d3", false},
		{"dot in suffix", "agent-a8e4.9d3", false},
		{"empty string", "", false},
		{"just prefix", "agent", false},
		// .harmonik-style UUIDs are NOT swept (different namespace).
		{"harmonik uuid style", "019e4633-7c7e-7bac-b38e-4471c3e3709f", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := claudeWorktreeNameValid(tc.input)
			if got != tc.want {
				t.Errorf("claudeWorktreeNameValid(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SweepClaudeWorktrees — directory absent
// ──────────────────────────────────────────────────────────────────────────────

// TestHkYhq3m_SweepClaudeWorktrees_NoDir verifies that the sweep returns an
// empty result (no error) when .claude/worktrees/ does not exist.
//
// Bead ref: hk-yhq3m.
func TestHkYhq3m_SweepClaudeWorktrees_NoDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: unexpected error: %v", err)
	}
	if len(result.Orphans) != 0 {
		t.Errorf("Orphans = %v, want empty", result.Orphans)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", result.Removed)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SweepClaudeWorktrees — dry-run (HARMONIK_SWEEP_CLAUDE_WORKTREES unset)
// ──────────────────────────────────────────────────────────────────────────────

// TestHkYhq3m_SweepClaudeWorktrees_DryRun_ReportsOrphans verifies that in
// dry-run mode (default: HARMONIK_SWEEP_CLAUDE_WORKTREES not set to "1") the
// sweep identifies unregistered entries as orphans without deleting them.
//
// Not parallel: uses t.Setenv which requires a serial test.
//
// Bead ref: hk-yhq3m.
func TestHkYhq3m_SweepClaudeWorktrees_DryRun_ReportsOrphans(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	orphanName := "agent-deadbeef01234567"
	orphanPath := claudeWTFixtureMakeEntry(t, root, orphanName)

	t.Setenv(EnvSweepClaudeWorktrees, "")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: unexpected error: %v", err)
	}

	if len(result.Orphans) != 1 || result.Orphans[0] != orphanPath {
		t.Errorf("Orphans = %v, want [%s]", result.Orphans, orphanPath)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty (dry-run mode)", result.Removed)
	}
	if result.Enabled {
		t.Error("Enabled = true, want false (env var unset)")
	}

	// Directory must still exist — dry-run does not delete.
	if _, statErr := os.Stat(orphanPath); os.IsNotExist(statErr) {
		t.Errorf("orphan directory %q was deleted in dry-run mode; MUST NOT be deleted", orphanPath)
	}
}

// TestHkYhq3m_SweepClaudeWorktrees_SkipsNonAgentEntries verifies that entries
// not matching the "agent-<hex>" naming convention are not classified as orphans.
//
// Not parallel: uses t.Setenv which requires a serial test.
//
// Bead ref: hk-yhq3m.
func TestHkYhq3m_SweepClaudeWorktrees_SkipsNonAgentEntries(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	for _, name := range []string{"settings.json", ".gitignore", "memory"} {
		entryPath := filepath.Join(root, DefaultClaudeWorktreesSubpath, name)
		//nolint:gosec // G301: 0755 matches existing conventions
		if err := os.MkdirAll(entryPath, 0o755); err != nil {
			t.Fatalf("MkdirAll %q: %v", entryPath, err)
		}
	}

	t.Setenv(EnvSweepClaudeWorktrees, "")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: unexpected error: %v", err)
	}

	if len(result.Orphans) != 0 {
		t.Errorf("Orphans = %v, want empty (non-agent entries must be ignored)", result.Orphans)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SweepClaudeWorktrees — live mode (HARMONIK_SWEEP_CLAUDE_WORKTREES=1)
// ──────────────────────────────────────────────────────────────────────────────

// TestHkYhq3m_SweepClaudeWorktrees_Live_RemovesOrphans verifies that when
// HARMONIK_SWEEP_CLAUDE_WORKTREES=1 is set, orphan entries are removed from
// disk and listed in Removed.
//
// Not parallel: uses t.Setenv which requires a serial test.
//
// Bead ref: hk-yhq3m.
func TestHkYhq3m_SweepClaudeWorktrees_Live_RemovesOrphans(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	orphanName := "agent-cafebabe00000001"
	orphanPath := claudeWTFixtureMakeEntry(t, root, orphanName)

	// Seed a file inside the orphan directory so RemoveAll has content to delete.
	innerFile := filepath.Join(orphanPath, "session.log")
	if err := os.WriteFile(innerFile, []byte("stale session data"), 0o600); err != nil {
		t.Fatalf("WriteFile inner: %v", err)
	}

	t.Setenv(EnvSweepClaudeWorktrees, "1")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: unexpected error: %v", err)
	}

	if len(result.Orphans) != 1 || result.Orphans[0] != orphanPath {
		t.Errorf("Orphans = %v, want [%s]", result.Orphans, orphanPath)
	}
	if len(result.Removed) != 1 || result.Removed[0] != orphanPath {
		t.Errorf("Removed = %v, want [%s]", result.Removed, orphanPath)
	}
	if !result.Enabled {
		t.Error("Enabled = false, want true")
	}

	// Directory must be gone after live sweep.
	if _, statErr := os.Stat(orphanPath); !os.IsNotExist(statErr) {
		t.Errorf("orphan directory %q still exists after live sweep; MUST be deleted", orphanPath)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RunOrphanSweep integration — ClaudeWorktreesSwept field
// ──────────────────────────────────────────────────────────────────────────────

// TestHkYhq3m_RunOrphanSweep_ClaudeWorktreesSwept verifies that
// RunOrphanSweep populates ClaudeWorktreesSwept with the count of orphan
// .claude/worktrees/ entries identified by the Gap-11 sweep.
//
// Not parallel: uses t.Setenv which requires a serial test.
//
// Bead ref: hk-yhq3m.
func TestHkYhq3m_RunOrphanSweep_ClaudeWorktreesSwept(t *testing.T) {
	projectDir := daemonOrphanSweepTempProjectDir(t)
	claudeWTFixtureMakeGitRepo(t, projectDir)

	// Create two unregistered agent entries.
	claudeWTFixtureMakeEntry(t, projectDir, "agent-aabbccdd11223344")
	claudeWTFixtureMakeEntry(t, projectDir, "agent-11223344aabbccdd")

	t.Setenv(EnvSweepClaudeWorktrees, "") // dry-run

	hash := lifecycle.ComputeProjectHash(projectDir)
	daemonStart := time.Now()

	cfg := OrphanSweepConfig{
		HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
		BrLister:      &daemonOrphanSweepFakeBrLister{},
	}

	result, err := RunOrphanSweep(t.Context(), projectDir, hash, daemonStart, cfg)
	if err != nil {
		// git worktree prune on a minimal git repo may emit a non-fatal error.
		t.Logf("RunOrphanSweep: error (may be worktree prune): %v", err)
	}

	if result.ClaudeWorktreesSwept != 2 {
		t.Errorf("ClaudeWorktreesSwept = %d, want 2", result.ClaudeWorktreesSwept)
	}
}
