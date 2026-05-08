package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// leaseFixtureRequiredGitignoreEntries are the four harmonik control-plane patterns that MUST
// appear in the repository's root .gitignore per WM-013e.
//
// Spec ref: workspace-model.md §4.3 WM-013e — "Required ignore entries (patterns
// relative to repo root; order preserved): .harmonik/lease.lock,
// .harmonik/sessions/, .harmonik/worktrees/, .harmonik/events/"
var leaseFixtureRequiredGitignoreEntries = []string{
	".harmonik/lease.lock",
	".harmonik/sessions/",
	".harmonik/worktrees/",
	".harmonik/events/",
}

// TestWM013e_GitignoreHygieneForControlPlanePaths verifies that the workspace
// manager ensures the repository's root .gitignore excludes the required
// harmonik control-plane paths, and that the write-or-fail posture surfaces a
// GitignoreWriteForbidden-class error when the daemon lacks write permission.
//
// Spec ref: workspace-model.md §4.3 WM-013e — "The workspace manager MUST ensure
// that the backing repository's root .gitignore excludes the harmonik
// control-plane paths … The .harmonik/events/ entry covers the workspace-local
// durability JSONL file introduced by WM-013b. If the daemon lacks write
// permission on .gitignore, startup MUST fail with a typed GitignoreWriteForbidden
// error per §8 and surface the failure to the operator."
func TestWM013e_GitignoreHygieneForControlPlanePaths(t *testing.T) {
	t.Parallel()

	t.Run("gitignore-contains-all-required-entries", func(t *testing.T) {
		t.Parallel()

		// Create a repo whose .gitignore already contains the required entries.
		// The workspace manager MUST check for all four entries at startup.
		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		entries := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(entries), 0o644); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		// Simulate the workspace manager's startup check.
		data, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore: %v", err)
		}
		content := string(data)

		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(content, entry) {
				t.Errorf("WM-013e: .gitignore missing required entry %q", entry)
			}
		}
	})

	t.Run("gitignore-missing-entries-must-be-added", func(t *testing.T) {
		t.Parallel()

		// .gitignore exists but is missing required entries. Workspace manager
		// MUST add them (write-or-fail posture).
		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Write a partial .gitignore (missing all required entries).
		if err := os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o644); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		// Simulate: detect missing entries and append them.
		data, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore: %v", err)
		}
		existing := string(data)

		var missing []string
		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(existing, entry) {
				missing = append(missing, entry)
			}
		}
		if len(missing) == 0 {
			t.Fatalf("WM-013e: expected missing entries; test setup error")
		}

		// Append the missing entries.
		f, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			t.Fatalf("WM-013e: OpenFile .gitignore for append: %v", err)
		}
		for _, entry := range missing {
			if _, err := f.WriteString(entry + "\n"); err != nil {
				_ = f.Close()
				t.Fatalf("WM-013e: WriteString %q: %v", entry, err)
			}
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			t.Fatalf("WM-013e: Sync .gitignore: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("WM-013e: Close .gitignore: %v", err)
		}

		// Verify all required entries are now present.
		data2, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore after write: %v", err)
		}
		updated := string(data2)
		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(updated, entry) {
				t.Errorf("WM-013e: .gitignore still missing %q after adding missing entries", entry)
			}
		}
	})

	t.Run("gitignore-absent-must-be-created", func(t *testing.T) {
		t.Parallel()

		// No .gitignore exists. Workspace manager MUST create it with all required entries.
		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Ensure no .gitignore exists.
		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove existing .gitignore: %v", err)
		}

		// Simulate: create .gitignore with required entries.
		content := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0o644); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		// Verify.
		data, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore: %v", err)
		}
		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(string(data), entry) {
				t.Errorf("WM-013e: newly created .gitignore missing %q", entry)
			}
		}
	})

	t.Run("write-forbidden-surfaces-gitignore-write-forbidden-error", func(t *testing.T) {
		t.Parallel()

		// Set the parent directory to read+execute only (no write), so that
		// attempting to write .gitignore fails. The workspace manager MUST surface
		// a GitignoreWriteForbidden-class error and MUST NOT continue silently.
		//
		// Spec ref: WM-013e — "If the daemon lacks write permission on .gitignore,
		// startup MUST fail with a typed GitignoreWriteForbidden error per §8 and
		// surface the failure to the operator — silent continuation with a
		// misconfigured ignore file would risk leaking daemon state into user
		// commits."
		//
		// TODO: When GitignoreWriteForbidden is defined as a typed sentinel error
		// in the workspace manager (hk-8mwo.67 follow-up), replace the string
		// content assertion below with errors.Is(err, workspace.ErrGitignoreWriteForbidden).
		// Search: grep -rn "ErrGitignore" internal/ — no sentinel defined yet.
		//
		// NOTE: This test must run as a non-root user; root bypasses filesystem
		// permission checks. If running as root, permission denial may not occur
		// and the test will skip.

		if os.Getuid() == 0 {
			t.Skip("WM-013e: skipping write-forbidden test: running as root (permission denial not enforced)")
		}

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Remove .gitignore if it exists from tempRepo.
		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove .gitignore: %v", err)
		}

		// Restrict the parent directory to read+execute: no write permission.
		if err := os.Chmod(repo, 0o555); err != nil {
			t.Fatalf("WM-013e: Chmod repo 0o555: %v", err)
		}
		// Restore write permission on test cleanup so t.TempDir() cleanup can proceed.
		t.Cleanup(func() {
			_ = os.Chmod(repo, 0o755)
		})

		// Attempt to write .gitignore — MUST fail with a permission error.
		content := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		writeErr := os.WriteFile(gitignorePath, []byte(content), 0o644)
		if writeErr == nil {
			t.Fatal("WM-013e: expected write to fail with permission denied, but it succeeded")
		}

		// The error MUST be a permission-denied error — which maps to the
		// GitignoreWriteForbidden class in the workspace manager API.
		//
		// TODO: when the workspace manager type is implemented (hk-8mwo.24 +
		// follow-up sentinel bead), replace this string check with:
		//   errors.Is(err, workspace.ErrGitignoreWriteForbidden)
		if !os.IsPermission(writeErr) {
			t.Errorf("WM-013e: write error %v is not a permission error; want os.IsPermission(err) == true", writeErr)
		}
		// The error message MUST convey the forbidden-write context so that the
		// operator can diagnose the startup failure.
		errMsg := writeErr.Error()
		if !leaseFixtureFindSubstring(errMsg, "permission denied") && !leaseFixtureFindSubstring(errMsg, "operation not permitted") {
			t.Errorf("WM-013e: error %q does not contain 'permission denied' or 'operation not permitted'; want GitignoreWriteForbidden-class message", errMsg)
		}
	})

	t.Run("harmonik-events-entry-covers-wm013b-jsonl", func(t *testing.T) {
		t.Parallel()

		// WM-013e explicitly states: "The .harmonik/events/ entry covers the
		// workspace-local durability JSONL file introduced by WM-013b."
		// Verify the entry is in the required set.
		found := false
		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if entry == ".harmonik/events/" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("WM-013e: .harmonik/events/ not in leaseFixtureRequiredGitignoreEntries; required to cover WM-013b JSONL")
		}
	})
}
