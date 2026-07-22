package main

import "testing"

// TestCodexSubstrateOptions_ForcesSandboxModeAtLaunch_daegv pins the hk-daegv fix:
// the codex app-server MUST be launched with the sandbox posture forced via a `-c`
// config override, not only per-thread. codex app-server (0.142/0.144) does NOT
// honor the thread/start `sandbox` field for the exec seatbelt — it runs its
// workspace-write default, under which the worktree's out-of-root git dir
// (<repo>/.git/worktrees/<id>/) is denied, so codex's own `git commit` AND its
// /bin/zsh exec_command spawn both fail (Operation not permitted) and the turn
// silently no-ops (the daemon fallback then commits). This test locks the exact
// launch argv so the override can't silently regress to the driver default
// {"app-server"}. The posture value tracks codexHeadlessSandbox by construction.
func TestCodexSubstrateOptions_ForcesSandboxModeAtLaunch_daegv(t *testing.T) {
	router := &codexWorkerRoutingRunner{requireBoundary: true}
	opts, sess := codexSubstrateOptions("codex", router)
	if sess != nil {
		t.Cleanup(func() { _ = sess.Close() }) //nolint:errcheck // test cleanup, unactionable
	}
	want := []string{"app-server", "-c", `sandbox_mode="` + codexHeadlessSandbox + `"`}
	if len(opts.Args) != len(want) {
		t.Fatalf("hk-daegv: Options.Args = %q, want %q", opts.Args, want)
	}
	for i := range want {
		if opts.Args[i] != want[i] {
			t.Fatalf("hk-daegv: Options.Args[%d] = %q, want %q (full: %q)", i, opts.Args[i], want[i], opts.Args)
		}
	}
}
