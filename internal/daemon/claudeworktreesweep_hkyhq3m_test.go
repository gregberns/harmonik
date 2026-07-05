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

// ──────────────────────────────────────────────────────────────────────────────
// SweepClaudeWorktrees — age-based removal (hk-qe736)
// ──────────────────────────────────────────────────────────────────────────────

// TestHkQe736_SweepClaudeWorktrees_AgedOrphan_RemovedWithoutEnvVar verifies
// that an unregistered orphan whose mtime is older than the max-age threshold
// is removed even when HARMONIK_SWEEP_CLAUDE_WORKTREES is not set.
//
// Bead ref: hk-qe736 — logmine worktree leak fix.
func TestHkQe736_SweepClaudeWorktrees_AgedOrphan_RemovedWithoutEnvVar(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	orphanName := "agent-a1b2c3d4e5f60001"
	orphanPath := claudeWTFixtureMakeEntry(t, root, orphanName)

	// Set mtime to 4 days ago (above the default 3-day threshold).
	oldTime := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(orphanPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	t.Setenv(EnvSweepClaudeWorktrees, "")      // env var NOT set
	t.Setenv(EnvClaudeWorktreeMaxAgeDays, "3") // explicit threshold

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: %v", err)
	}

	if len(result.Orphans) != 1 || result.Orphans[0] != orphanPath {
		t.Errorf("Orphans = %v, want [%s]", result.Orphans, orphanPath)
	}
	if len(result.Removed) != 1 || result.Removed[0] != orphanPath {
		t.Errorf("Removed = %v, want [%s] (age-based removal must fire without env var)", result.Removed, orphanPath)
	}
	if _, statErr := os.Stat(orphanPath); !os.IsNotExist(statErr) {
		t.Errorf("aged orphan %q still exists; MUST be deleted by age-based GC", orphanPath)
	}
}

// TestHkQe736_SweepClaudeWorktrees_FreshOrphan_DryRunWithoutEnvVar verifies
// that a FRESH unregistered orphan (below max-age) is NOT removed when env
// var is unset — the dry-run-for-recent default is preserved.
//
// Bead ref: hk-qe736.
func TestHkQe736_SweepClaudeWorktrees_FreshOrphan_DryRunWithoutEnvVar(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	orphanName := "agent-a1b2c3d4e5f60002"
	orphanPath := claudeWTFixtureMakeEntry(t, root, orphanName)
	// mtime is "now" — fresh, below any reasonable threshold.

	t.Setenv(EnvSweepClaudeWorktrees, "")
	t.Setenv(EnvClaudeWorktreeMaxAgeDays, "3")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: %v", err)
	}

	if len(result.Orphans) != 1 {
		t.Errorf("Orphans = %v, want 1", result.Orphans)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty (fresh orphan must NOT be removed in dry-run)", result.Removed)
	}
	if _, statErr := os.Stat(orphanPath); os.IsNotExist(statErr) {
		t.Errorf("fresh orphan %q was deleted; MUST NOT be deleted in dry-run", orphanPath)
	}
}

// TestHkQe736_SweepClaudeWorktrees_StaleLocked_ForcedRemoved verifies that a
// git-locked worktree older than the max-age threshold is force-removed via
// `git worktree remove --force --force`, even without the env var.
//
// This covers the recurring incident: agent-a974b0ef, locked, ~11 days old,
// never reaped because the old code skipped all registered entries.
//
// Bead ref: hk-qe736.
func TestHkQe736_SweepClaudeWorktrees_StaleLocked_ForcedRemoved(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	wtName := "agent-a0b1c2d3e4f50001"
	wtPath := filepath.Join(root, DefaultClaudeWorktreesSubpath, wtName)
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a real git worktree and lock it (simulating a Claude Code agent worktree).
	runClaudeWTGit(t, root, "worktree", "add", "-b", "test-hkqe736-locked", wtPath)
	runClaudeWTGit(t, root, "worktree", "lock", wtPath)

	// Make the worktree appear old (4 days > 3-day threshold).
	oldTime := time.Now().Add(-4 * 24 * time.Hour)
	if err := os.Chtimes(wtPath, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	t.Setenv(EnvSweepClaudeWorktrees, "")
	t.Setenv(EnvClaudeWorktreeMaxAgeDays, "3")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: %v", err)
	}

	if len(result.Orphans) != 1 || result.Orphans[0] != wtPath {
		t.Errorf("Orphans = %v, want [%s]", result.Orphans, wtPath)
	}
	if len(result.Removed) != 1 || result.Removed[0] != wtPath {
		t.Errorf("Removed = %v, want [%s] (stale-locked must be force-removed)", result.Removed, wtPath)
	}
	if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
		t.Errorf("locked worktree %q still exists; MUST be deleted by force-remove", wtPath)
	}
}

// TestHkQe736_SweepClaudeWorktrees_LockedFresh_Skipped verifies that a
// git-locked worktree BELOW the age threshold is NOT touched (active agent).
//
// Bead ref: hk-qe736.
func TestHkQe736_SweepClaudeWorktrees_LockedFresh_Skipped(t *testing.T) {
	root := t.TempDir()
	claudeWTFixtureMakeGitRepo(t, root)

	wtName := "agent-a0b1c2d3e4f50002"
	wtPath := filepath.Join(root, DefaultClaudeWorktreesSubpath, wtName)
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runClaudeWTGit(t, root, "worktree", "add", "-b", "test-hkqe736-fresh", wtPath)
	runClaudeWTGit(t, root, "worktree", "lock", wtPath)
	// mtime is "now" — fresh, active agent.

	t.Setenv(EnvSweepClaudeWorktrees, "1") // even with env var set, fresh locked = skip
	t.Setenv(EnvClaudeWorktreeMaxAgeDays, "3")

	result, err := SweepClaudeWorktrees(t.Context(), root, nil)
	if err != nil {
		t.Fatalf("SweepClaudeWorktrees: %v", err)
	}

	if len(result.Orphans) != 0 {
		t.Errorf("Orphans = %v, want empty (fresh locked worktree must not be classified as orphan)", result.Orphans)
	}
	if len(result.Removed) != 0 {
		t.Errorf("Removed = %v, want empty", result.Removed)
	}
	if _, statErr := os.Stat(wtPath); os.IsNotExist(statErr) {
		t.Errorf("fresh locked worktree %q was deleted; MUST NOT be touched", wtPath)
	}
}
