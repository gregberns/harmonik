package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// wipcapture_rc019_test.go — tests for CaptureWIP and WriteWIPCapture.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-019.

// TestRC019_extractUntrackedFiles verifies that extractUntrackedFiles correctly
// parses `git status --porcelain -z` output and returns only untracked paths.
func TestRC019_extractUntrackedFiles(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		input    string
		expected []string
	}

	cases := []testCase{
		{
			name:     "empty",
			input:    "",
			expected: nil,
		},
		{
			name:     "only_modified",
			input:    "M  file.go\x00 M other.go\x00",
			expected: nil,
		},
		{
			name:     "single_untracked",
			input:    "?? new-file.go\x00",
			expected: []string{"new-file.go"},
		},
		{
			name:     "mixed",
			input:    "M  tracked.go\x00?? untracked.go\x00?? another.go\x00",
			expected: []string{"untracked.go", "another.go"},
		},
		{
			name:     "path_with_spaces_and_quotes",
			input:    "?? has space \"quoted\".go\x00",
			expected: []string{`has space "quoted".go`},
		},
		{
			name: "rename_original_path_skipped",
			// -z rename entry carries the original path as an extra
			// NUL-terminated field; it must not be misread as an entry.
			input:    "R  new-name.go\x00old-name.go\x00?? untracked.go\x00",
			expected: []string{"untracked.go"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractUntrackedFiles(tc.input)
			if len(got) != len(tc.expected) {
				t.Errorf("extractUntrackedFiles(%q): got %v (len %d), want %v (len %d)",
					tc.input, got, len(got), tc.expected, len(tc.expected))
				return
			}
			for i, path := range got {
				if path != tc.expected[i] {
					t.Errorf("extractUntrackedFiles(%q)[%d]: got %q, want %q",
						tc.input, i, path, tc.expected[i])
				}
			}
		})
	}
}

// TestRC019_WriteWIPCapture_WritesFilesCorrectly verifies that WriteWIPCapture
// creates the three canonical files in the destination directory.
func TestRC019_WriteWIPCapture_WritesFilesCorrectly(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	capture := core.WIPCapture{
		WorktreePath:       "/outer/run/worktree",
		GitStatusPorcelain: "M  file.go\n?? new.go\n",
		GitDiff:            "diff --git a/file.go b/file.go\n--- a/file.go\n+++ b/file.go\n",
		UntrackedFiles:     []string{"new.go"},
	}

	if err := WriteWIPCapture(capture, destDir); err != nil {
		t.Fatalf("WriteWIPCapture: unexpected error: %v", err)
	}

	// Verify git-status.txt was written with the expected content.
	statusPath := filepath.Join(destDir, core.WIPCaptureStatusFile)
	statusBytes, err := os.ReadFile(statusPath) //nolint:gosec // destDir is t.TempDir()
	if err != nil {
		t.Fatalf("WriteWIPCapture: %s not written: %v", core.WIPCaptureStatusFile, err)
	}
	if string(statusBytes) != capture.GitStatusPorcelain {
		t.Errorf("WriteWIPCapture: %s content = %q, want %q",
			core.WIPCaptureStatusFile, string(statusBytes), capture.GitStatusPorcelain)
	}

	// Verify git-diff.patch was written.
	diffPath := filepath.Join(destDir, core.WIPCaptureDiffFile)
	diffBytes, err := os.ReadFile(diffPath) //nolint:gosec // destDir is t.TempDir()
	if err != nil {
		t.Fatalf("WriteWIPCapture: %s not written: %v", core.WIPCaptureDiffFile, err)
	}
	if string(diffBytes) != capture.GitDiff {
		t.Errorf("WriteWIPCapture: %s content = %q, want %q",
			core.WIPCaptureDiffFile, string(diffBytes), capture.GitDiff)
	}

	// Verify untracked-files.txt was written with newline-separated paths.
	untrackedPath := filepath.Join(destDir, core.WIPCaptureUntrackedFile)
	untrackedBytes, err := os.ReadFile(untrackedPath) //nolint:gosec // destDir is t.TempDir()
	if err != nil {
		t.Fatalf("WriteWIPCapture: %s not written: %v", core.WIPCaptureUntrackedFile, err)
	}
	untrackedContent := string(untrackedBytes)
	if !strings.Contains(untrackedContent, "new.go") {
		t.Errorf("WriteWIPCapture: %s does not contain expected path %q; got %q",
			core.WIPCaptureUntrackedFile, "new.go", untrackedContent)
	}
}

// TestRC019_WriteWIPCapture_SkipsEmptyFiles verifies that WriteWIPCapture does
// not create files for empty content (avoids cluttering the wip-capture
// directory with empty placeholder files).
func TestRC019_WriteWIPCapture_SkipsEmptyFiles(t *testing.T) {
	t.Parallel()

	destDir := t.TempDir()
	capture := core.WIPCapture{
		WorktreePath:       "/outer/run/worktree",
		GitStatusPorcelain: "", // empty — clean worktree
		GitDiff:            "", // empty — no changes
		UntrackedFiles:     nil,
	}

	if err := WriteWIPCapture(capture, destDir); err != nil {
		t.Fatalf("WriteWIPCapture: unexpected error on clean capture: %v", err)
	}

	// No files should be written for an empty capture.
	for _, name := range []string{core.WIPCaptureStatusFile, core.WIPCaptureDiffFile, core.WIPCaptureUntrackedFile} {
		p := filepath.Join(destDir, name)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("WriteWIPCapture: file %q was written for empty content, expected no file", name)
		}
	}
}

// TestRC019_CaptureWIP_RejectsNonExistentPath verifies that CaptureWIP returns
// an error when the worktree path does not exist or is not a git repository.
func TestRC019_CaptureWIP_RejectsNonExistentPath(t *testing.T) {
	t.Parallel()

	_, err := CaptureWIP("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("RC-019: CaptureWIP on non-existent path should return error")
	}
}

// TestRC019_CaptureWIP_CleanWorktreeHasNoWIP verifies that CaptureWIP run in
// a clean git repository returns a WIPCapture with HasWIP()=false.
func TestRC019_CaptureWIP_CleanWorktreeHasNoWIP(t *testing.T) {
	t.Parallel()

	repoDir := wipCaptureTestInitGitRepo(t)
	wipCaptureTestGitCmd(t, repoDir, "commit", "--allow-empty", "-m", "initial commit")

	capture, err := CaptureWIP(repoDir)
	if err != nil {
		t.Fatalf("RC-019: CaptureWIP on clean repo: %v", err)
	}
	if !capture.Valid() {
		t.Error("RC-019: CaptureWIP returned invalid WIPCapture for clean repo")
	}
	if capture.HasWIP() {
		t.Errorf("RC-019: CaptureWIP on clean repo: HasWIP()=true, want false; "+
			"status=%q, untracked=%v", capture.GitStatusPorcelain, capture.UntrackedFiles)
	}
}

// TestRC019_CaptureWIP_DirtyWorktreeHasWIP verifies that CaptureWIP detects a
// modified tracked file and returns HasWIP()=true.
func TestRC019_CaptureWIP_DirtyWorktreeHasWIP(t *testing.T) {
	t.Parallel()

	repoDir := wipCaptureTestInitGitRepo(t)

	// Write a file, add and commit it, then modify it to create WIP.
	filePath := filepath.Join(repoDir, "tracked.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile tracked.go: %v", err)
	}
	wipCaptureTestGitCmd(t, repoDir, "add", "tracked.go")
	wipCaptureTestGitCmd(t, repoDir, "commit", "-m", "initial commit")

	// Modify the file without staging to create WIP.
	if err := os.WriteFile(filePath, []byte("package main // modified\n"), 0o644); err != nil {
		t.Fatalf("WriteFile tracked.go (modify): %v", err)
	}

	capture, err := CaptureWIP(repoDir)
	if err != nil {
		t.Fatalf("RC-019: CaptureWIP on dirty repo: %v", err)
	}
	if !capture.HasWIP() {
		t.Errorf("RC-019: CaptureWIP on dirty repo: HasWIP()=false, want true; "+
			"status=%q", capture.GitStatusPorcelain)
	}
}

// TestRC019_CaptureWIP_UntrackedFileDetected verifies that CaptureWIP detects
// an untracked file and populates UntrackedFiles.
func TestRC019_CaptureWIP_UntrackedFileDetected(t *testing.T) {
	t.Parallel()

	repoDir := wipCaptureTestInitGitRepo(t)
	wipCaptureTestGitCmd(t, repoDir, "commit", "--allow-empty", "-m", "initial commit")

	// Create an untracked file.
	untrackedPath := filepath.Join(repoDir, "untracked.go")
	if err := os.WriteFile(untrackedPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile untracked.go: %v", err)
	}

	capture, err := CaptureWIP(repoDir)
	if err != nil {
		t.Fatalf("RC-019: CaptureWIP with untracked file: %v", err)
	}
	if !capture.HasWIP() {
		t.Error("RC-019: CaptureWIP with untracked file: HasWIP()=false, want true")
	}
	found := false
	for _, f := range capture.UntrackedFiles {
		if strings.Contains(f, "untracked.go") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RC-019: CaptureWIP: untracked.go not in UntrackedFiles=%v", capture.UntrackedFiles)
	}
}

// wipCaptureTestInitGitRepo creates a temporary directory, initialises a git
// repository in it, and returns the repo path.
func wipCaptureTestInitGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	wipCaptureTestGitCmd(t, dir, "init")
	wipCaptureTestGitCmd(t, dir, "config", "user.email", "test@example.com")
	wipCaptureTestGitCmd(t, dir, "config", "user.name", "Test")
	return dir
}

// wipCaptureTestGitCmd runs a git subcommand in dir and fails the test on error.
func wipCaptureTestGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:noctx // test helper
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
