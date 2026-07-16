package daemon

// mergetomain_pushrace_t7_test.go — F4 (M4-C5 / T7) forced lost-push race.
//
// The F4 relocation moves `git push origin <target>` OUTSIDE the mergeq exclusion
// domain (Phase B). Correctness on a lost race therefore comes from Phase D
// re-validating INSIDE the domain (CAS-rollback + fetch + re-base to origin) and
// re-attempting, NOT from holding the lock across the network push.
//
// This test forces origin to advance in the exact window the relocation opens —
// between the local prepare and the push — by wrapping `git` with a shim that,
// on the FIRST `git push`, injects a diverging (non-conflicting) commit into
// origin out-of-band before forwarding the daemon's push. That first push is
// therefore rejected non-fast-forward; the driver must fetch, re-base the
// run-branch onto the new origin tip, and retry the push successfully.
//
// Assertions:
//   (A) the merge succeeds (re-enter-on-conflict retry worked);
//   (B) origin's main contains BOTH the out-of-band race commit and the run work;
//   (C) local main matches origin main after the retry.
//
// Bead/task: M4-C5 / T7 (F4 push relocation); companions hk-svieq (retry taxonomy).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/mergeq"
	"github.com/gregberns/harmonik/internal/workspace"
)

// pushRaceSetupRepo builds a project repo with a bare origin and a run-branch one
// commit ahead of main, returning projectDir, originDir, and the run's RunID. It
// mirrors rsmInvSetupRepo but also hands back originDir so the push shim can
// advance it out-of-band.
func pushRaceSetupRepo(t *testing.T) (projectDir, originDir string, runID core.RunID) {
	t.Helper()
	projectDir = t.TempDir()
	rsmInvGit(t, projectDir, "init", "--initial-branch=main")
	rsmInvGit(t, projectDir, "config", "user.email", "daemon@harmonik.local")
	rsmInvGit(t, projectDir, "config", "user.name", "Harmonik Test")
	rsmInvWriteFile(t, filepath.Join(projectDir, "README"), "init\n")
	rsmInvGit(t, projectDir, "add", ".")
	rsmInvGit(t, projectDir, "commit", "-m", "init")

	originDir = t.TempDir()
	rsmInvGit(t, originDir, "init", "--bare", "--initial-branch=main")
	rsmInvGit(t, projectDir, "remote", "add", "origin", originDir)
	rsmInvGit(t, projectDir, "push", "origin", "main")

	runID = core.RunID(uuid.MustParse("0190a000-0000-7000-8000-0000000a0777"))
	runBranch := workspace.TaskBranchName(runID.String())
	rsmInvGit(t, projectDir, "branch", runBranch, "main")
	tmpWt := filepath.Join(t.TempDir(), "runwt")
	rsmInvGit(t, projectDir, "worktree", "add", tmpWt, runBranch)
	rsmInvWriteFile(t, filepath.Join(tmpWt, "work.txt"), "agent work\n")
	rsmInvGit(t, tmpWt, "add", "work.txt")
	rsmInvGit(t, tmpWt, "commit", "-m", "agent commit")
	rsmInvGit(t, projectDir, "worktree", "remove", "--force", tmpWt)
	return projectDir, originDir, runID
}

// pushRaceInstallShim prepends a PATH `git` shim that, on the FIRST `git push`,
// clones origin into a scratch dir, commits a diverging race.txt, and pushes it
// back to origin BEFORE forwarding the daemon's push — deterministically forcing
// a non-fast-forward rejection on the first Phase-B push. Later pushes forward
// straight through so the retry succeeds.
func pushRaceInstallShim(t *testing.T, originDir string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	shimDir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "pushed.marker")
	scratch := t.TempDir()
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = push ] && [ ! -f \"" + marker + "\" ]; then\n" +
		"  : > \"" + marker + "\"\n" +
		"  CL=\"" + scratch + "/clone\"\n" +
		"  \"" + realGit + "\" clone -q \"" + originDir + "\" \"$CL\"\n" +
		"  \"" + realGit + "\" -C \"$CL\" config user.email race@harmonik.local\n" +
		"  \"" + realGit + "\" -C \"$CL\" config user.name Race\n" +
		"  echo race > \"$CL/race.txt\"\n" +
		"  \"" + realGit + "\" -C \"$CL\" add race.txt\n" +
		"  \"" + realGit + "\" -C \"$CL\" commit -q -m 'out-of-band race'\n" +
		"  \"" + realGit + "\" -C \"$CL\" push -q origin main\n" +
		"fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	rsmInvWriteShim(t, shimDir, "git", body)
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestMergeToMain_ForcedLostPushRace_ReEntersAndRetries proves the F4
// re-enter-on-conflict rule: a push that loses a race (origin advanced in the
// Phase-B window) re-enters the exclusion domain, re-prepares, and succeeds.
func TestMergeToMain_ForcedLostPushRace_ReEntersAndRetries(t *testing.T) {
	projectDir, originDir, runID := pushRaceSetupRepo(t)
	pushRaceInstallShim(t, originDir)

	q := mergeq.New(nil)
	qctx, qcancel := context.WithCancel(context.Background())
	q.Start(qctx)
	t.Cleanup(qcancel)

	out := mergeRunBranchToMain(context.Background(), q.Submit, projectDir, runID, &noopEmitter{},
		core.BeadID("hk-t7-pushrace"), "", "main", nil, "")

	// (A) Merge succeeded despite the forced first-push non-FF rejection.
	if !out.success {
		t.Fatalf("forced lost-push race did not recover: reason=%q noChange=%v", out.reason, out.noChange)
	}

	// (B) origin main contains BOTH the race commit and the run work.
	for _, want := range []string{"race.txt", "work.txt"} {
		lsCmd := exec.CommandContext(t.Context(), "git", "cat-file", "-e", "main:"+want)
		lsCmd.Dir = originDir
		if err := lsCmd.Run(); err != nil {
			t.Errorf("origin main missing %q after retry (work or race commit lost): %v", want, err)
		}
	}

	// (C) local main == origin main after the retry push.
	localMain := pushRaceRevParse(t, projectDir, "refs/heads/main")
	originMain := pushRaceRevParse(t, originDir, "refs/heads/main")
	if localMain != originMain {
		t.Errorf("local main %s != origin main %s after retry", localMain[:8], originMain[:8])
	}
}

func pushRaceRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pushRaceRevParse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimRight(string(out), "\n")
}
