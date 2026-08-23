package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var leaseFixtureRequiredGitignoreEntries = []string{
	".harmonik/lease.lock",
	".harmonik/sessions/",
	".harmonik/worktrees/",
	".harmonik/events/",
	".harmonik/review.json",
	".harmonik/review.iter-*.json",
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

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		entries := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(entries), 0o600); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		data := mustReadFile(t, gitignorePath)
		content := string(data)

		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(content, entry) {
				t.Errorf("WM-013e: .gitignore missing required entry %q", entry)
			}
		}
	})

	t.Run("gitignore-missing-entries-must-be-added", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		if err := os.WriteFile(gitignorePath, []byte("*.log\n*.tmp\n"), 0o600); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		data := mustReadFile(t, gitignorePath)
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
		// #nosec G304 -- gitignorePath is a test-controlled path under t.TempDir.
		f, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			t.Fatalf("WM-013e: OpenFile .gitignore for append: %v", err)
		}
		for _, entry := range missing {
			if _, err := f.WriteString(entry + "\n"); err != nil {
				t.Fatalf("WM-013e: WriteString %q: %v", entry, withCleanupErrs(err, f.Close()))
			}
		}
		if err := f.Sync(); err != nil {
			t.Fatalf("WM-013e: Sync .gitignore: %v", withCleanupErrs(err, f.Close()))
		}
		if err := f.Close(); err != nil {
			t.Fatalf("WM-013e: Close .gitignore: %v", err)
		}

		data2 := mustReadFile(t, gitignorePath)
		updated := string(data2)
		for _, entry := range leaseFixtureRequiredGitignoreEntries {
			if !leaseFixtureFindSubstring(updated, entry) {
				t.Errorf("WM-013e: .gitignore still missing %q after adding missing entries", entry)
			}
		}
	})

	t.Run("gitignore-absent-must-be-created", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove existing .gitignore: %v", err)
		}

		content := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0o600); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		data := mustReadFile(t, gitignorePath)
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

		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove .gitignore: %v", err)
		}

		// Restrict the parent directory to read+execute: no write permission.
		// #nosec G302 -- test fixture intentionally removes write permission.
		if err := os.Chmod(repo, 0o555); err != nil {
			t.Fatalf("WM-013e: Chmod repo 0o555: %v", err)
		}
		t.Cleanup(func() {
			// #nosec G302 -- restores private test-fixture directory permissions.
			if err := os.Chmod(repo, 0o700); err != nil {
				t.Errorf("WM-013e: restore repo write permission: %v", err)
			}
		})

		content := strings.Join(leaseFixtureRequiredGitignoreEntries, "\n") + "\n"
		writeErr := os.WriteFile(gitignorePath, []byte(content), 0o600)
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
		errMsg := writeErr.Error()
		if !leaseFixtureFindSubstring(errMsg, "permission denied") && !leaseFixtureFindSubstring(errMsg, "operation not permitted") {
			t.Errorf("WM-013e: error %q does not contain 'permission denied' or 'operation not permitted'; want GitignoreWriteForbidden-class message", errMsg)
		}
	})

	t.Run("harmonik-events-entry-covers-wm013b-jsonl", func(t *testing.T) {
		t.Parallel()

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
