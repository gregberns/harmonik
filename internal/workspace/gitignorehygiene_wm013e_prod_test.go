package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM013e_EnsureGitignoreHygiene verifies the production EnsureGitignoreHygiene
// helper per workspace-model.md §4.3 WM-013e.
//
// Spec ref: workspace-model.md §4.3 WM-013e — "The workspace manager MUST ensure
// that the backing repository's root .gitignore excludes the harmonik control-plane
// paths. … At daemon startup workspace manager MUST check root .gitignore for these
// entries; if missing, daemon MUST add them AND stage + commit on dedicated branch
// `harmonik/gitignore-init` BEFORE creating any worktree."
func TestWM013e_EnsureGitignoreHygiene(t *testing.T) {
	t.Parallel()

	t.Run("idempotent-when-all-entries-present", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Write all required entries.
		content := strings.Join(RequiredGitignoreEntries, "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile .gitignore: %v", err)
		}

		// EnsureGitignoreHygiene must succeed without modifying the file.
		if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
			t.Fatalf("WM-013e: EnsureGitignoreHygiene (idempotent): %v", err)
		}

		// File content must be unchanged.
		got, err := os.ReadFile(gitignorePath) //nolint:gosec // G304: controlled test path
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore: %v", err)
		}
		if string(got) != content {
			t.Errorf("WM-013e: .gitignore content changed unexpectedly:\ngot:  %q\nwant: %q", got, content)
		}
	})

	t.Run("adds-missing-entries-when-gitignore-absent", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Ensure .gitignore does not exist.
		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove .gitignore: %v", err)
		}

		if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
			t.Fatalf("WM-013e: EnsureGitignoreHygiene (absent .gitignore): %v", err)
		}

		got, err := os.ReadFile(gitignorePath) //nolint:gosec // G304: controlled test path
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore after ensure: %v", err)
		}
		content := string(got)
		for _, entry := range RequiredGitignoreEntries {
			if !gitignoreEntryPresent(content, entry) {
				t.Errorf("WM-013e: .gitignore missing required entry %q after ensure", entry)
			}
		}
	})

	t.Run("adds-only-missing-entries-preserves-existing", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Write a partial .gitignore (only first two entries).
		partial := strings.Join(RequiredGitignoreEntries[:2], "\n") + "\n"
		if err := os.WriteFile(gitignorePath, []byte(partial), 0o644); err != nil {
			t.Fatalf("WM-013e: WriteFile .gitignore: %v", err)
		}

		if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
			t.Fatalf("WM-013e: EnsureGitignoreHygiene (partial .gitignore): %v", err)
		}

		got, err := os.ReadFile(gitignorePath) //nolint:gosec // G304: controlled test path
		if err != nil {
			t.Fatalf("WM-013e: ReadFile .gitignore after ensure: %v", err)
		}
		content := string(got)

		// All four entries must be present.
		for _, entry := range RequiredGitignoreEntries {
			if !gitignoreEntryPresent(content, entry) {
				t.Errorf("WM-013e: .gitignore missing %q after ensure", entry)
			}
		}

		// The original content must still be present (no data loss).
		for _, entry := range RequiredGitignoreEntries[:2] {
			if !strings.Contains(content, entry) {
				t.Errorf("WM-013e: existing entry %q was removed", entry)
			}
		}
	})

	t.Run("missing-entries-returns-correct-set", func(t *testing.T) {
		t.Parallel()

		// MissingGitignoreEntries helper reports the correct missing set.
		cases := []struct {
			content string
			want    []string
		}{
			{
				content: "",
				want:    RequiredGitignoreEntries,
			},
			{
				content: strings.Join(RequiredGitignoreEntries, "\n") + "\n",
				want:    nil,
			},
			{
				content: ".harmonik/lease.lock\n",
				want:    RequiredGitignoreEntries[1:], // missing 3 of 4
			},
		}

		for _, tc := range cases {
			got := MissingGitignoreEntries(tc.content)
			if len(got) != len(tc.want) {
				t.Errorf("MissingGitignoreEntries(%q) len = %d, want %d; got %v",
					tc.content, len(got), len(tc.want), got)
				continue
			}
			for i, entry := range got {
				if entry != tc.want[i] {
					t.Errorf("MissingGitignoreEntries: entry[%d] = %q, want %q", i, entry, tc.want[i])
				}
			}
		}
	})

	t.Run("required-entries-constant-matches-spec", func(t *testing.T) {
		t.Parallel()

		// RequiredGitignoreEntries must match the four spec-canonical patterns
		// per WM-013e (order preserved).
		wantEntries := []string{
			".harmonik/lease.lock",
			".harmonik/sessions/",
			".harmonik/worktrees/",
			".harmonik/events/",
		}
		if len(RequiredGitignoreEntries) != len(wantEntries) {
			t.Fatalf("WM-013e: RequiredGitignoreEntries len = %d, want %d",
				len(RequiredGitignoreEntries), len(wantEntries))
		}
		for i, entry := range RequiredGitignoreEntries {
			if entry != wantEntries[i] {
				t.Errorf("WM-013e: RequiredGitignoreEntries[%d] = %q, want %q", i, entry, wantEntries[i])
			}
		}
	})

	t.Run("write-forbidden-returns-err-gitignore-write-forbidden", func(t *testing.T) {
		t.Parallel()

		if os.Getuid() == 0 {
			t.Skip("WM-013e: skipping: running as root (permission denial not enforced)")
		}

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")

		// Remove .gitignore if it exists.
		if err := os.Remove(gitignorePath); err != nil && !os.IsNotExist(err) {
			t.Fatalf("WM-013e: Remove .gitignore: %v", err)
		}

		// Restrict the repo directory to prevent writing .gitignore.
		if err := os.Chmod(repo, 0o555); err != nil {
			t.Fatalf("WM-013e: Chmod repo 0o555: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(repo, 0o755) })

		err := EnsureGitignoreHygiene(t.Context(), repo)
		if err == nil {
			t.Fatal("WM-013e: expected error on write-forbidden, got nil")
		}
		if !errors.Is(err, ErrGitignoreWriteForbidden) {
			t.Errorf("WM-013e: expected ErrGitignoreWriteForbidden, got: %v", err)
		}
	})
}
