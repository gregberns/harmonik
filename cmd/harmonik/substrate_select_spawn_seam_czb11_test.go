package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
)

// substrate_select_spawn_seam_czb11_test.go — hk-czb11 REAL-SEAM regression that
// KILLS the false-green class. Both prior false-greens slipped through because no
// test drove the PRODUCTION runner value through codexdriver.spawn's RemoteCwdRunner
// type-assert (driver.go:220): the driver test injected a fake runner, and the
// router test checked the router in isolation. yankee's crit3 then crashed with
// the exact `chdir <remote>: no such file or directory` — the else branch set the
// LOCAL exec.Cmd.Dir to the REMOTE worktree path.
//
// Part 1 (deterministic, primary) asserts the PRODUCTION construction. It calls
// codexSubstrateOptions — the SAME production Options builder selectSubstrate uses
// (substrate_select.go:74-75) with the SAME `&codexWorkerRoutingRunner{requireBoundary:true}`
// literal — and requires the resulting Options.Runner to satisfy codexdriver.RemoteCwdRunner,
// the EXACT assert driver.go:220 runs. A live pointer-identity trace (%p at
// construction == %p at :220, no capture/decorator re-wrap) proves this Options.Runner
// IS the value at :220, so satisfying it here is equivalent to driving the assert.
// RED without the router's CommandInDir (the object fails the assert → spawn's else
// branch → cmd.Dir = remote path), GREEN with it. No ssh/routing dependency.
func TestCodexSubstrateOptions_RunnerSatisfiesRemoteCwd_czb11(t *testing.T) {
	// EXACTLY the production construction (selectSubstrate builds this router then
	// passes it to codexSubstrateOptions).
	router := &codexWorkerRoutingRunner{requireBoundary: true}
	opts, sess := codexSubstrateOptions("codex", router)
	if sess != nil {
		t.Cleanup(func() { _ = sess.Close() }) //nolint:errcheck // test cleanup, unactionable
	}
	if _, ok := opts.Runner.(codexdriver.RemoteCwdRunner); !ok {
		t.Fatalf("hk-czb11 REGRESSION: production codexSubstrateOptions Options.Runner (%T) does NOT satisfy "+
			"codexdriver.RemoteCwdRunner — codexdriver.spawn's driver.go:220 assert takes the buggy else branch and "+
			"sets the LOCAL cmd.Dir to the REMOTE worktree path (fork/exec chdir-ENOENT)", opts.Runner)
	}
}

// TestCodexSpawnSeam_ProductionRunner_RemoteCwd_czb11 (Part 2, end-to-end) drives
// the full production path — selectSubstrate(HARMONIK_SUBSTRATE=codexdriver) → the
// real codex substrate whose Options.Runner is the composition-root
// *codexWorkerRoutingRunner, late-bound to an ssh worker via the returned
// bindRegistry — then spawns with a REMOTE worktree cwd that does not exist locally.
// With the fix, spawn applies the cwd remotely and leaves the LOCAL exec.Cmd.Dir
// unset, so cmd.Start does NOT chdir into the non-existent path. The enabled-ssh
// worker (oneWorkerRegistry, WorkerSnapshot is health-gate-free) makes the router
// route over ssh deterministically.
func TestCodexSpawnSeam_ProductionRunner_RemoteCwd_czb11(t *testing.T) {
	t.Setenv(substrateSelectEnv, "codexdriver")

	sub, bindRegistry, requireBoundary, _ := selectSubstrate(nil, "codex")
	if !requireBoundary || bindRegistry == nil {
		t.Fatalf("codex path expected requireBoundary=true + non-nil bindRegistry; got %v / %v", requireBoundary, bindRegistry == nil)
	}
	bindRegistry(oneWorkerRegistry(true)) // enabled ssh worker — production late-bind

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const remoteCwd = "/box-b/.harmonik/worktrees/run-czb11-seam/does-not-exist-locally"
	sess, err := sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		WindowName: "czb11-seam",
		Argv:       []string{"codex", "app-server"},
		Cwd:        remoteCwd,
	})
	if sess != nil {
		t.Cleanup(func() { _ = sess.Kill(context.Background()) }) //nolint:errcheck // test cleanup, unactionable
	}

	// The remote path must never become the LOCAL cmd.Dir, so cmd.Start can never
	// chdir-ENOENT into it. A real ssh transport error to the unreachable worker is
	// fine; the fork/exec chdir into the remote path is the regression.
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "no such file or directory") || strings.Contains(msg, remoteCwd) {
			t.Fatalf("hk-czb11 REGRESSION: production spawn set the LOCAL cmd.Dir to the REMOTE cwd (chdir-ENOENT): %v", err)
		}
	}
}
