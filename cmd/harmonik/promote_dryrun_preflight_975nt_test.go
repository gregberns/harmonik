package main

// promote_dryrun_preflight_975nt_test.go — `promote --dry-run` must depend on
// its inputs.
//
// The defect (hk-promote-dryrun-validates-nothing-975nt): the dry run printed
// the same four-line plan for a commit that does not exist, for a branch that
// does not exist, and for a real promotion. It exited 0 every time. A command
// that cannot fail cannot answer the question an operator runs it to answer,
// so the operator learned the inputs were wrong during the real promotion.
//
// These tests drive the real entry point against a real git repository with a
// real bare origin. They cover the three cases that matter together: a commit
// that does not resolve, a branch that is absent on origin, and a valid
// promotion that still passes. The last one is not a formality. A fix that
// refused everything would pass the first two and be worse than the defect.
//
// Two more tests hold the boundaries: the dry run writes nothing, and a
// promotion that policy refuses keeps its own message and its own exit code.

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// promoteDryRunRepo builds a work repository with one commit on main and a bare
// origin that holds the same branch. It returns the work directory and the SHA
// of that commit.
func promoteDryRunRepo(t *testing.T) (workDir, sha string) {
	t.Helper()

	root := t.TempDir()
	originDir := filepath.Join(root, "origin.git")
	workDir = filepath.Join(root, "work")

	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: git args are test-controlled
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	for _, dir := range []string{originDir, workDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	git(originDir, "init", "--bare", "-b", "main")
	git(workDir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(workDir, "a.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	git(workDir, "add", "a.txt")
	git(workDir, "commit", "-m", "first commit (hk-abc123)")
	git(workDir, "remote", "add", "origin", originDir)
	git(workDir, "push", "origin", "main")

	return workDir, git(workDir, "rev-parse", "HEAD")
}

// capturePromoteIO runs the promote subcommand with os.Stdout and os.Stderr
// redirected, and returns everything both streams received plus the exit code.
func capturePromoteIO(t *testing.T, args []string) (output string, exitCode int) {
	t.Helper()

	oldStdout, oldStderr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan string, 1)
	go func() {
		buf, readErr := io.ReadAll(r)
		if readErr != nil {
			done <- "read pipe: " + readErr.Error()
			return
		}
		done <- string(buf)
	}()

	exitCode = runPromoteSubcommand(args)

	os.Stdout, os.Stderr = oldStdout, oldStderr
	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("close writer: %v", closeErr)
	}
	output = <-done
	if closeErr := r.Close(); closeErr != nil {
		t.Fatalf("close reader: %v", closeErr)
	}
	return output, exitCode
}

func TestPromoteDryRun_RefusesInputsThatDoNotExist(t *testing.T) {
	workDir, sha := promoteDryRunRepo(t)
	emptyDir := t.TempDir()

	tests := []struct {
		name     string
		args     []string
		wantText string
	}{
		{
			name:     "commit does not resolve",
			args:     []string{"--project", workDir, "--dry-run", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
			wantText: `commit "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" does not resolve`,
		},
		{
			name:     "sha argument is not a revision at all",
			args:     []string{"--project", workDir, "--dry-run", "not a ref at all"},
			wantText: `commit "not a ref at all" does not resolve`,
		},
		{
			name:     "target branch is absent on origin",
			args:     []string{"--project", workDir, "--dry-run", "--target", "no/such/branch", sha},
			wantText: `branch "no/such/branch" does not exist on origin`,
		},
		{
			name:     "project is not a git repository",
			args:     []string{"--project", emptyDir, "--dry-run", sha},
			wantText: "is not a git repository",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			output, code := capturePromoteIO(t, tc.args)
			if code == 0 {
				t.Errorf("promote --dry-run exited 0 on an input that does not exist; the dry run must fail here.\noutput:\n%s", output)
			}
			if !strings.Contains(output, tc.wantText) {
				t.Errorf("promote --dry-run did not report the missing input.\nwant text: %s\ngot output:\n%s", tc.wantText, output)
			}
			// The plan describes work the promotion cannot start. It must not print.
			if strings.Contains(output, "would push") {
				t.Errorf("promote --dry-run printed the push plan for an input that does not exist.\noutput:\n%s", output)
			}
		})
	}
}

// TestPromoteDryRun_AcceptsRealCommitAndBranch is the control. A fix that
// refuses every input passes the test above and helps nobody.
func TestPromoteDryRun_AcceptsRealCommitAndBranch(t *testing.T) {
	workDir, sha := promoteDryRunRepo(t)

	output, code := capturePromoteIO(t, []string{"--project", workDir, "--dry-run", sha})
	if code != 0 {
		t.Fatalf("promote --dry-run on a real commit and a real branch exited %d, want 0.\noutput:\n%s", code, output)
	}
	for _, want := range []string{
		"would cherry-pick " + sha,
		`onto "main"`,
		"would push: git push origin HEAD:main",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("promote --dry-run left the plan out.\nwant text: %s\ngot output:\n%s", want, output)
		}
	}
}

// TestPromoteDryRun_WritesNothing holds the other half of the contract. The
// checks read the repository and ask origin for its branch list. They must not
// fetch, move a ref, or create a worktree.
func TestPromoteDryRun_WritesNothing(t *testing.T) {
	workDir, sha := promoteDryRunRepo(t)

	gitOut := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", workDir}, args...)...) //nolint:gosec // G204: git args are test-controlled
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}

	// Drop the remote-tracking ref the fixture push created. A `git fetch`
	// puts it back, so its absence at the end is the evidence that the dry run
	// did not fetch. Without this step a fetch changes no ref and the check
	// below cannot see it.
	gitOut("update-ref", "-d", "refs/remotes/origin/main")

	refsBefore := gitOut("show-ref")
	worktreesBefore := gitOut("worktree", "list")

	if _, code := capturePromoteIO(t, []string{"--project", workDir, "--dry-run", sha}); code != 0 {
		t.Fatalf("promote --dry-run exited %d, want 0", code)
	}

	if refsAfter := gitOut("show-ref"); refsAfter != refsBefore {
		t.Errorf("promote --dry-run changed the refs.\nbefore:\n%s\nafter:\n%s", refsBefore, refsAfter)
	}
	if worktreesAfter := gitOut("worktree", "list"); worktreesAfter != worktreesBefore {
		t.Errorf("promote --dry-run created a worktree.\nbefore:\n%s\nafter:\n%s", worktreesBefore, worktreesAfter)
	}
	if strings.Contains(gitOut("show-ref"), "refs/remotes/origin/main") {
		t.Error("promote --dry-run fetched from origin. A dry run reads the remote and writes no ref.")
	}
}

// TestPromoteDryRun_ProtectedTargetKeepsItsOwnAnswer separates the two ways a
// dry run fails. An input that does not exist is exit 1. A promotion that the
// protection gate refuses is exit 5, and it names the branch and the way out.
func TestPromoteDryRun_ProtectedTargetKeepsItsOwnAnswer(t *testing.T) {
	workDir, sha := promoteDryRunRepo(t)

	output, code := capturePromoteIO(t, []string{
		"--project", workDir, "--dry-run", "--protect-branch", "main", sha,
	})
	if code != 5 {
		t.Fatalf("promote --dry-run on a protected target exited %d, want 5.\noutput:\n%s", code, output)
	}
	if !strings.Contains(output, "is a protected branch") {
		t.Errorf("promote --dry-run did not report the protection refusal.\noutput:\n%s", output)
	}
	if strings.Contains(output, "does not resolve") || strings.Contains(output, "does not exist on origin") {
		t.Errorf("promote --dry-run reported a policy refusal as a missing input.\noutput:\n%s", output)
	}
}
