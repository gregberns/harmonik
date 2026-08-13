package main

// promote_lostpushrace_z0bms_test.go — `harmonik promote` must not call a lost
// push race terminal.
//
// The defect (hk-z0bms): promote carried its own copy of the push-refusal test
// that internal/runmerge had already replaced for hk-lhdqo —
//
//	isNonFF := strings.Contains(pushOutStr, "non-fast-forward") ||
//		strings.Contains(pushOutStr, "[rejected]")
//
// Git refuses the loser of a concurrent push with "[remote rejected] ...
// (failed to update ref)". That string does NOT contain "[rejected]": there is
// a "remote " between the bracket and the word. So promote matched neither
// token, gave up on attempt 1, and never ran the fetch-and-rebase retry its own
// loop exists for.
//
// This test does not assert against a captured string. It makes git produce the
// refusal, and it drives the real subcommand end to end. The origin's
// pre-receive hook moves refs/heads/<target> out from under the incoming push,
// so receive-pack fails the ref transaction with git's real wording:
//
//	remote: error: cannot lock ref 'refs/heads/main': is at <new> but expected <old>
//	 ! [remote rejected] HEAD -> main (failed to update ref)
//
// The hook fires on every push, so the retries also lose and the promotion ends
// at exit 4 either way. The exit code is therefore NOT the signal — the attempt
// count is. With the defective predicate promote stops after ONE attempt; with
// the shared predicate it spends its whole 3-attempt budget, and each retry
// fetches and rebases first. Assert on that, and the test goes red the moment
// the two-token test comes back.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// promoteLostRaceRepo builds a work repository with a bare origin whose
// pre-receive hook advances refs/heads/main on every push. It returns the work
// directory and the SHA of a local commit that is NOT on origin — the commit
// the test asks promote to promote.
//
// The hook unsets GIT_QUARANTINE_PATH because receive-pack refuses ref writes
// from inside the push quarantine. Without that line the hook's update-ref
// fails, the push succeeds, and the test silently measures nothing.
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
	// promote amends the cherry-pick to stamp a Harmonik-Bead-ID trailer, and
	// an amend needs a committer identity in the repository it runs in.
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
	// The hermetic test harness sets a global `core.hooksPath =` (empty) and an
	// empty init.templateDir, which turns hooks OFF for every fixture
	// repository in the suite. That is the right default — it keeps the
	// operator's hooks out of the tests — but this fixture's whole mechanism is
	// a hook, so re-enable them for this one repository only.
	hooksDir := filepath.Join(originDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", hooksDir, err)
	}
	git(originDir, "config", "core.hooksPath", hooksDir)
	// #nosec G306 -- a git hook must be executable; this path is under t.TempDir().
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatalf("write pre-receive hook: %v", err)
	}

	// The commit to promote: local only, and a clean cherry-pick onto the tip
	// of origin/main.
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

	// The premise of the test: git really did refuse with the wording that the
	// old two-token predicate misses. If git ever changes this, the test must
	// say so rather than quietly passing on a different failure.
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
