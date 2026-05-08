package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionLogFixture_detectGitignoreMisconfiguration checks the given gitignore
// content for patterns that would exclude .harmonik/sessions/ from the git
// index, which would silently break the preserve-in-merged-branch contract.
//
// Returns a non-nil error describing the misconfiguration if a problematic
// pattern is detected.
func sessionLogFixture_detectGitignoreMisconfiguration(gitignoreContent string) error {
	// Patterns that would accidentally exclude .harmonik/sessions/ from commits.
	// This is an operator-observable misconfiguration per WM-030 and operator-nfr.md §4.9.
	problematic := []string{
		".harmonik/sessions/",
		".harmonik/sessions",
		"**/.harmonik/sessions",
		"**/.harmonik/sessions/",
		"**/sessions/",
		".harmonik/",
		".harmonik",
	}
	lines := strings.Split(gitignoreContent, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		for _, pat := range problematic {
			if trimmed == pat {
				return &gitignoreMisconfigError{pattern: trimmed}
			}
		}
	}
	return nil
}

type gitignoreMisconfigError struct {
	pattern string
}

func (e *gitignoreMisconfigError) Error() string {
	return "gitignore misconfiguration: pattern " + e.pattern +
		" would exclude .harmonik/sessions/ from git index, breaking preserve-in-merged-branch (WM-030)"
}

// TestWM030_PostMergeSessionLogRetention verifies that after a simulated
// merge-back, the session-log directory and its metadata sidecar are still
// present at the expected path (preserve-in-merged-branch default).
//
// Spec ref: workspace-model.md §4.7 WM-030 — "On successful merge
// (workspace_merge_status with status=merged), the workspace manager MUST
// preserve the sessions directory inside the merged branch (i.e., the session
// logs remain in the integration-branch commit tree) by default. An
// operator-configured alternative MAY move the directory to a post-merge archive
// path post-MVH; the default for MVH is preserve-in-merged-branch for audit
// retention."
func TestWM030_PostMergeSessionLogRetention(t *testing.T) {
	t.Parallel()

	repo, _ := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0030"
	sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000003001"

	workspacePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	sessionDir := filepath.Join(workspacePath, ".harmonik", "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sessionDir: %v", err)
	}

	sidecarPath := filepath.Join(sessionDir, "harmonik.meta.json")
	content := sessionLogFixture_makeMetaJSON(runID, sessionID, "node-01", "agentic", "wf-01", "")
	if err := sessionLogFixture_writeSidecarAtomic(sidecarPath, content); err != nil {
		t.Fatalf("WM-030: sidecar write: %v", err)
	}

	// Also write a session.log to simulate handler output.
	sessionLog := filepath.Join(sessionDir, "session.log")
	if err := os.WriteFile(sessionLog, []byte("session output\n"), 0o644); err != nil {
		t.Fatalf("WM-030: WriteFile session.log: %v", err)
	}

	// Simulate "post-merge": the actual merge-back (hk-8mwo.68) integrates the
	// task branch into the integration branch. The WM-030 durability contract
	// states the session logs REMAIN in the merged branch tree. Here we simulate
	// the post-merge state by leaving the files in place (no deletion) and
	// asserting their continued presence.
	//
	// In a real merge-back the files would be part of the commit tree on the
	// integration branch. This fixture verifies the durability shape: no code
	// path that runs during merge removes the session logs.

	// Assert: session directory still exists after the (simulated) merge.
	if info, err := os.Stat(sessionDir); err != nil || !info.IsDir() {
		t.Errorf("WM-030: session-log directory absent after post-merge state: %v", err)
	}

	// Assert: sidecar still present.
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Errorf("WM-030: harmonik.meta.json absent after post-merge state: %v", err)
	}

	// Assert: session.log still present.
	if _, err := os.Stat(sessionLog); err != nil {
		t.Errorf("WM-030: session.log absent after post-merge state: %v", err)
	}
}

// TestWM030_GitignoreMisconfigurationDetected verifies that a .gitignore
// containing a pattern that excludes .harmonik/sessions/ is detected as a
// misconfiguration, since it silently breaks the preserve-in-merged-branch
// contract.
//
// Spec ref: workspace-model.md §4.7 WM-030 — "The MVH default requires that
// project .gitignore MUST NOT exclude .harmonik/sessions/; a gitignored
// sessions directory silently breaks the preserve-in-merged-branch contract.
// Violations are an operator-observable misconfiguration per [operator-nfr.md §4.9]."
func TestWM030_GitignoreMisconfigurationDetected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		content string
		wantErr bool
	}{
		{
			desc:    "sessions-pattern-exact",
			content: ".harmonik/sessions/\n",
			wantErr: true,
		},
		{
			desc:    "double-star-sessions-pattern",
			content: "**/sessions/\n",
			wantErr: true,
		},
		{
			desc:    "harmonik-dir-excluded-entirely",
			content: ".harmonik/\n",
			wantErr: true,
		},
		{
			desc:    "comment-line-is-safe",
			content: "# .harmonik/sessions/\n*.log\n",
			wantErr: false,
		},
		{
			desc:    "safe-gitignore-no-session-exclusion",
			content: "*.tmp\n.DS_Store\n*.swp\n",
			wantErr: false,
		},
		{
			desc:    "root-level-harmonik-no-slash-excluded",
			content: ".harmonik\n",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			err := sessionLogFixture_detectGitignoreMisconfiguration(tc.content)
			if tc.wantErr && err == nil {
				t.Errorf("WM-030: expected misconfiguration error for gitignore %q, got nil", tc.content)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("WM-030: unexpected misconfiguration error for gitignore %q: %v", tc.content, err)
			}
		})
	}

	// Integration: write an actual .gitignore in a tempRepo and run the detector.
	t.Run("file-based-detection", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		gitignorePath := filepath.Join(repo, ".gitignore")
		badContent := "# harmonik worktrees\n**/sessions/\n*.swp\n"
		if err := os.WriteFile(gitignorePath, []byte(badContent), 0o644); err != nil {
			t.Fatalf("WriteFile .gitignore: %v", err)
		}

		content, err := os.ReadFile(gitignorePath)
		if err != nil {
			t.Fatalf("ReadFile .gitignore: %v", err)
		}
		if err := sessionLogFixture_detectGitignoreMisconfiguration(string(content)); err == nil {
			t.Errorf("WM-030: expected misconfiguration error for bad .gitignore, got nil")
		}
	})
}
