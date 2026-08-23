package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func promoteLostRaceRepo(t *testing.T) (workDir, sha string) {
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
	git(workDir, "config", "user.name", "t")
	git(workDir, "config", "user.email", "t@t")

	if err := os.WriteFile(filepath.Join(workDir, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("write base.txt: %v", err)
	}
	git(workDir, "add", "base.txt")
	git(workDir, "commit", "-m", "base commit")
	git(workDir, "remote", "add", "origin", originDir)
	git(workDir, "push", "origin", "main")

	hook := `#!/bin/sh
unset GIT_QUARANTINE_PATH
export GIT_AUTHOR_NAME=r GIT_AUTHOR_EMAIL=r@r GIT_COMMITTER_NAME=r GIT_COMMITTER_EMAIL=r@r
cur=$(git rev-parse refs/heads/main)
tree=$(git rev-parse refs/heads/main^{tree})
new=$(git commit-tree -p "$cur" -m "concurrent push wins the race" "$tree")
git update-ref refs/heads/main "$new"
exit 0
`
	hooksDir := filepath.Join(originDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", hooksDir, err)
	}
	git(originDir, "config", "core.hooksPath", hooksDir)
	// #nosec G306 -- a git hook must be executable; this path is under t.TempDir().
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatalf("write pre-receive hook: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workDir, "promoted.txt"), []byte("promoted\n"), 0o600); err != nil {
		t.Fatalf("write promoted.txt: %v", err)
	}
	git(workDir, "add", "promoted.txt")
	git(workDir, "commit", "-m", "the commit under promotion (hk-z0bms)")

	return workDir, git(workDir, "rev-parse", "HEAD")
}

// TestPromotePush_LostRaceSpendsItsRetryBudget drives push-mode against an
// origin that always wins the ref race. The promotion cannot succeed, and it is
// not meant to: what is under test is whether promote recognises the refusal as
// one worth re-preparing for.
func TestPromotePush_LostRaceSpendsItsRetryBudget(t *testing.T) {
	workDir, sha := promoteLostRaceRepo(t)

	output, code := capturePromoteIO(t, []string{"--project", workDir, sha})

	if !strings.Contains(output, "[remote rejected]") || !strings.Contains(output, "cannot lock ref") {
		t.Fatalf("the fixture did not produce a lost-race refusal; promote said:\n%s", output)
	}
	if strings.Contains(output, "[rejected]") {
		t.Fatalf("the refusal carries a bare [rejected] token, so it no longer distinguishes the "+
			"old predicate from the new one; promote said:\n%s", output)
	}

	if !strings.Contains(output, "fetching and rebasing") {
		t.Errorf("promote treated a lost push race as terminal instead of re-preparing; it said:\n%s", output)
	}
	if !strings.Contains(output, "push failed (attempt 3/3)") {
		t.Errorf("promote did not spend its 3-attempt retry budget on a recoverable refusal; it said:\n%s", output)
	}
	if code != 4 {
		t.Errorf("exit code = %d, want 4 (push failed after all retries)", code)
	}
}
