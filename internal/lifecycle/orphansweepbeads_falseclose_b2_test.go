package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestHasMergeCommitForBead_B2FalseClose is the B2 acceptance oracle for the
// Cat-3c subsumption false-close bug. It seeds a real temporary git repo and
// asserts the scanner's verdict directly against the seeded commits' content —
// NOT against any reconcile/close-path status (the close path cannot be its own
// oracle).
//
// Cases:
//   - docs-only commit that MENTIONS the bead id in its BODY (no trailer) → FALSE.
//   - genuine Harmonik-Bead-ID trailer but diff touches only *.md            → FALSE.
//   - genuine trailer + a source-file (non-docs) diff                        → TRUE.
func TestHasMergeCommitForBead_B2FalseClose(t *testing.T) {
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: test-only; args are hard-coded literals.
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeFile := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "commit.gpgsign", "false")

	// Baseline commit so the branch exists.
	writeFile("README.md", "seed\n")
	runGit("add", "-A")
	runGit("commit", "-m", "chore: seed")

	const (
		mentionBead = core.BeadID("hk-mention")
		docsBead    = core.BeadID("hk-docsonly")
		realBead    = core.BeadID("hk-real")
	)

	// Case 1: docs-only commit that MENTIONS the bead id in the body but carries
	// no Harmonik-Bead-ID trailer. Must NOT match.
	writeFile("docs/notes.md", "note about "+string(mentionBead)+"\n")
	runGit("add", "-A")
	runGit("commit", "-m", "docs: discussing "+string(mentionBead)+" in prose")

	// Case 2: genuine trailer but diff touches only *.md.
	writeFile("docs/changelog.md", "changelog\n")
	runGit("add", "-A")
	runGit("commit", "-m", "docs: changelog\n\nHarmonik-Bead-ID: "+string(docsBead))

	// Case 3: genuine trailer + a real source (non-docs) file.
	writeFile("internal/feature/feature.go", "package feature\n")
	runGit("add", "-A")
	runGit("commit", "-m", "feat: implement feature\n\nHarmonik-Bead-ID: "+string(realBead))

	scanner := GitMergeCommitScanner{ProjectDir: repo, TargetBranch: "main"}

	cases := []struct {
		name string
		bead core.BeadID
		want bool
	}{
		{"docs_body_mention_no_trailer", mentionBead, false},
		{"genuine_trailer_docs_only_diff", docsBead, false},
		{"genuine_trailer_source_diff", realBead, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanner.HasMergeCommitForBead(context.Background(), tc.bead)
			if err != nil {
				t.Fatalf("HasMergeCommitForBead(%s): unexpected error: %v", tc.bead, err)
			}
			if got != tc.want {
				t.Errorf("HasMergeCommitForBead(%s) = %v, want %v", tc.bead, got, tc.want)
			}
		})
	}
}
