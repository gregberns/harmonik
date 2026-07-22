package main

import (
	"testing"

	"github.com/gregberns/harmonik/internal/codexdriver"
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
// (substrate_select.go:74-80) with the SAME `&codexWorkerRoutingRunner{requireBoundary:false}`
// literal — false since hk-tckw3.1 deliberately dropped the fail-closed fence —
// and requires the resulting Options.Runner to satisfy codexdriver.RemoteCwdRunner,
// the EXACT assert driver.go:220 runs. A live pointer-identity trace (%p at
// construction == %p at :220, no capture/decorator re-wrap) proves this Options.Runner
// IS the value at :220, so satisfying it here is equivalent to driving the assert.
// RED without the router's CommandInDir (the object fails the assert → spawn's else
// branch → cmd.Dir = remote path), GREEN with it. No ssh/routing dependency.
func TestCodexSubstrateOptions_RunnerSatisfiesRemoteCwd_czb11(t *testing.T) {
	// EXACTLY the production construction (selectSubstrate builds this router then
	// passes it to codexSubstrateOptions). requireBoundary tracks production at
	// substrate_select.go:78, which hk-tckw3.1 set to false. The assert below is
	// structural (does Options.Runner satisfy RemoteCwdRunner) and so is independent
	// of this field's value — but the literal must still MATCH production, or the
	// "EXACTLY the production construction" claim above quietly stops being true.
	router := &codexWorkerRoutingRunner{requireBoundary: false}
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

// Part 2 (TestCodexSpawnSeam_ProductionRunner_RemoteCwd_czb11) was REMOVED by
// hk-5vapm. It drove the full selectSubstrate -> late-bind-to-ssh-worker ->
// SpawnWindow-with-remote-cwd path, whose premise is ssh-per-node routing —
// SCRAPPED by decision D4. It had also become unrunnable on its own terms: its
// precondition asserted requireBoundary=true, which hk-tckw3.1 deliberately made
// false, so it failed before reaching the behaviour it meant to test.
//
// The hk-czb11 contract it existed to protect is NOT lost: Part 1 above drives the
// SAME production Options.Runner through the SAME codexdriver.RemoteCwdRunner
// assert that driver.go:220 performs, deterministically and with no ssh
// dependency. The local-cwd branch stays covered by
// substrate_select_commandindir_czb11_test.go.
