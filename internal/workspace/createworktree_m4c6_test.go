package workspace

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestM4C6_HonestProbeFiresIdenticallyUnderBothRunners is the M4-C6 acceptance
// pin for the STEP-0c honest-probe carry-forward (remote-substrate T3).
//
// The "honest probe" is the post-add HEAD validation in CreateWorktree: a zero
// exit from `git worktree add` is NOT trusted on its own — CreateWorktree also
// runs `git -C <wtPath> rev-parse HEAD` through the SAME runner and treats an
// empty HEAD as a transient race (cleanup-via-runner + bounded retry), rather
// than letting an un-checked-out worktree slip past create and fast-fail
// downstream (hk-iaj1w).
//
// M4-C6 requires this guard to fire IDENTICALLY on both the local and the
// SSH-runner path — the remote path must not bypass it. CreateWorktree gates the
// probe on runner PRESENCE (`cfg.runner != nil`), not runner TYPE, so any
// explicitly-installed runner — a local-transport runner OR an SSH-transport
// runner — takes the exact same probe path. This table test proves that: it
// drives the identical empty-HEAD-then-valid scenario under a local-shaped
// runner and an SSH-shaped runner and asserts the guard fingerprint is byte-for-
// byte the same. It is a regression pin against anyone adding a
// `runnerIsLocalFS`-style short-circuit that would let the local (or the SSH)
// path skip the probe and diverge.
//
// Faithful + network-free: both rows run the REAL git locally so a live branch +
// registered worktree exists for the cleanup path to tear down; only the first
// HEAD-validation rev-parse is faked to report empty. The SSH row additionally
// captures the exact `ssh <host> -- git …` argv the SSHRunner would emit for the
// probe, proving the honest probe is genuinely routed over the SSH transport (it
// is executed locally so the retry can complete without a real connection). A
// real-ssh exercise is out of scope here and belongs to the T4 scenario test.
func TestM4C6_HonestProbeFiresIdenticallyUnderBothRunners(t *testing.T) {
	t.Parallel()

	// guardFingerprint captures the observable honest-probe behaviour that must be
	// identical across the local and SSH runner paths.
	type guardFingerprint struct {
		headProbed     bool // `git -C <wt> rev-parse HEAD` ran through the runner
		worktreeAdds   int  // ≥2 ⇒ the empty-HEAD race triggered a retry
		cleanedViaRun  bool // `rm -rf <wt>` ran through the runner (not os.RemoveAll)
		branchDeleted  bool // `git -C <repo> branch -D <branch>` ran through the runner
		created        bool // CreateWorktree returned nil
		worktreeOnDisk bool // the worktree materialised after the successful retry
	}

	// run drives one empty-HEAD honest-probe scenario. When sshHost != "" the
	// probe (and every other command) is additionally recorded in its SSHRunner-
	// wrapped `ssh <host> -- …` argv shape, but executed locally so the retry can
	// complete without a network hop. Returns the fingerprint plus, for the SSH
	// row, the captured ssh-wrapped argv of the HEAD probe.
	run := func(t *testing.T, sshHost string) (guardFingerprint, []string) {
		t.Helper()

		repo, sha := tempRepo(t)
		runID := "019ec83c-m4c6-7001-0001-000000000001"
		branch := TaskBranchName(runID)
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		var (
			worktreeAddCalls int
			sshProbeArgv     []string
		)
		rr := &tmux.RecordingRunner{
			CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
				isRevParse := name == "git" && containsArg(args, "rev-parse")

				// SSH row: capture the exact argv the SSHRunner would ship over the
				// wire, proving the honest probe is routed through the SSH transport
				// (executed locally below so the retry completes without a connection).
				if sshHost != "" {
					sshCmd := tmux.SSHRunner{Host: sshHost}.Command(ctx, name, args...)
					if isRevParse && sshProbeArgv == nil {
						sshProbeArgv = sshCmd.Args
					}
				}

				// Real git for `worktree add` so a live branch + registered worktree
				// exists for the cleanup path to remove on the empty-HEAD retry.
				if name == "git" && containsArg(args, "worktree") && containsArg(args, "add") {
					worktreeAddCalls++
					return exec.CommandContext(ctx, name, args...)
				}
				// First HEAD validation reports empty (exit 0, no stdout) → the
				// honest-probe empty-HEAD path fires. The post-retry rev-parse runs
				// the real git and returns a valid SHA.
				if isRevParse && worktreeAddCalls == 1 {
					return exec.CommandContext(ctx, "sh", "-c", "exit 0")
				}
				// mkdir, rm -rf, branch -D, prune, post-retry rev-parse: real, local.
				return exec.CommandContext(ctx, name, args...)
			},
		}

		cfg := NoWorktreeRootOverride().WithRunner(rr)

		err := CreateWorktree(context.Background(), repo, runID, sha, cfg)

		fp := guardFingerprint{
			headProbed:    hasRunnerCall(rr, "git", []string{"-C", worktreePath, "rev-parse", "HEAD"}),
			worktreeAdds:  worktreeAddCalls,
			cleanedViaRun: hasRunnerCall(rr, "rm", []string{"-rf", worktreePath}),
			branchDeleted: hasRunnerCall(rr, "git", []string{"-C", repo, "branch", "-D", branch}),
			created:       err == nil,
		}
		if _, statErr := os.Stat(worktreePath); statErr == nil {
			fp.worktreeOnDisk = true
		}
		return fp, sshProbeArgv
	}

	localFP, _ := run(t, "")
	sshFP, sshProbeArgv := run(t, "deploy@builder.internal")

	// The honest probe MUST have fired under both runners: HEAD validated through
	// the runner, an empty-HEAD retry (≥2 adds), cleanup routed through the runner,
	// and a successful create with the worktree on disk.
	want := guardFingerprint{
		headProbed:     true,
		worktreeAdds:   2,
		cleanedViaRun:  true,
		branchDeleted:  true,
		created:        true,
		worktreeOnDisk: true,
	}
	if localFP != want {
		t.Errorf("M4-C6: local-runner honest-probe fingerprint = %+v, want %+v", localFP, want)
	}
	if sshFP != want {
		t.Errorf("M4-C6: ssh-runner honest-probe fingerprint = %+v, want %+v", sshFP, want)
	}

	// IDENTICAL: the SSH path must not bypass or alter the guard.
	if localFP != sshFP {
		t.Errorf("M4-C6: honest-probe guard diverged across runners:\n local = %+v\n ssh   = %+v", localFP, sshFP)
	}

	// The HEAD probe was genuinely routed over the SSH transport: the SSHRunner
	// wraps it as `ssh <host> -- git … rev-parse HEAD`.
	if len(sshProbeArgv) == 0 {
		t.Fatal("M4-C6: expected the honest probe to be routed through the SSH transport, but no ssh-wrapped argv was captured")
	}
	// exec.Cmd.Args is [ssh, <host>, --, <shell-quoted remote command>].
	if sshProbeArgv[0] != "ssh" {
		t.Errorf("M4-C6: ssh probe argv[0] = %q, want ssh (argv: %v)", sshProbeArgv[0], sshProbeArgv)
	}
	if len(sshProbeArgv) < 3 || sshProbeArgv[1] != "deploy@builder.internal" {
		t.Errorf("M4-C6: ssh probe argv[1] = %q, want host deploy@builder.internal (argv: %v)", safeArgIdx(sshProbeArgv, 1), sshProbeArgv)
	}
	if sshProbeArgv[2] != "--" {
		t.Errorf("M4-C6: ssh probe argv[2] = %q, want `--` separator (argv: %v)", sshProbeArgv[2], sshProbeArgv)
	}
	remote := sshProbeArgv[len(sshProbeArgv)-1]
	if !containsSubstring(remote, "rev-parse") || !containsSubstring(remote, "HEAD") {
		t.Errorf("M4-C6: ssh probe remote command %q is not the HEAD honest probe", remote)
	}
}
