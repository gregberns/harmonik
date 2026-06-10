//go:build integration

package keeper

// tmuxresolve_integration_test.go — integration test (build tag: integration)
// that exercises the REAL tmux liveness-probe path in tmuxSessionLive /
// ResolveTmuxTarget against a live tmux server.
//
// # Why this file exists
//
// The unit tests in tmuxresolve_test.go cover name-equality, multi-agent,
// --tmux override, and the live/absent/empty cases via an INJECTED stub
// (sessionExistsFn). The stub never invokes the real tmux subprocess, so the
// production resolution path — tmuxSessionLive shelling out to tmux and
// interpreting its exit code — was unexercised (hk-2ojne, from re-review of
// hk-dopv3 / c5c2c439).
//
// Writing this test surfaced a latent bug: the original implementation used
// `tmux display-message -t <name> -p "#W"`, which exits 0 even for a
// NONEXISTENT target (it silently falls back to the current client's session),
// so it reported a false positive whenever a tmux server had any attached
// client — the normal daemon-under-supervisor environment. The fix (same
// commit) switches tmuxSessionLive to `tmux has-session -t "=<name>"`, which
// exits non-zero for an absent session and uses the "=" exact-match anchor to
// avoid tmux's default prefix/fuzzy matching. This test is what proves the
// real path now behaves correctly.
//
// This file closes that gap. It is gated behind `//go:build integration`
// (matching the repo convention in test/integration/integration_stub.go and
// internal/daemon/handlerpause_integration_lz485_test.go), so the daemon's
// per-bead commit gate (default `go test ./...`, which does NOT pass
// -tags=integration) skips it. Run it with:
//
//	go test -tags=integration -run TestIntegration_ ./internal/keeper/...
//	# or the umbrella target:
//	make check-full   # runs `go test -race -tags=integration ./...`
//
// # Safety contract (load-bearing)
//
// This test creates and destroys ONLY its own uniquely-named throwaway tmux
// sessions. Session names are derived from t.TempDir() (unique per test run)
// and a random suffix, and every teardown kills sessions BY EXACT NAME via
// `tmux kill-session -t <name>`. There is NO kill-server, NO glob/pattern
// kill, and NO list-and-kill. It can never touch hk-daemon-supervise,
// harmonik-*, hk-crew-*, *-flywheel, harmonik-pi/main/kerf, or any other
// pre-existing session. If `tmux` is not on PATH the whole test t.Skip()s.
//
// Bead: hk-2ojne. Helper prefix: tsi (tmux-session-integration).

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"
)

// tsiRequireTmux skips the calling test (gracefully) when the tmux binary is
// not installed. The integration path is meaningless without a real tmux.
func tsiRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tsi: tmux not found on PATH; skipping real-tmux integration test")
	}
}

// tsiUniqueSessionName returns a throwaway session name guaranteed not to
// collide with any real harmonik/captain/crew session. The name embeds the
// bead id and a random suffix and uses a "liet-2ojne-test-" prefix that no
// harmonik machinery ever produces.
func tsiUniqueSessionName(t *testing.T) string {
	t.Helper()
	// rand/v2 (non-crypto) is fine for a test-local unique session suffix.
	return fmt.Sprintf("liet-2ojne-test-%d-%d", rand.Int64(), rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance
}

// tsiStartSession creates a detached tmux session with the EXACT supplied name
// and registers a t.Cleanup that kills THAT session by name (and only that
// session). It fails the test if creation does not take effect.
func tsiStartSession(t *testing.T, name string) {
	t.Helper()

	ctx := context.Background()

	// `-d` detached, `-s <name>` exact session name; sleep keeps the session's
	// only pane alive so the session does not exit immediately.
	if out, err := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", name, "sleep", "300").CombinedOutput(); err != nil {
		t.Fatalf("tsi: failed to create throwaway session %q: %v (output: %s)", name, err, out)
	}

	// Teardown kills ONLY this exact session by name. Safe under -race and on
	// early t.Fatalf because t.Cleanup runs regardless. Killing an
	// already-dead session is a no-op, so its error is intentionally ignored.
	t.Cleanup(func() {
		out, err := exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", name).CombinedOutput()
		if err != nil {
			// Not fatal: the session may already be gone (a test that killed it
			// mid-run). Log for diagnosis only.
			t.Logf("tsi cleanup: kill-session %q returned %v (output: %s) — likely already gone", name, err, out)
		}
	})
}

// tsiKillSession kills the named session by EXACT name. Used mid-test to drive
// tmuxSessionLive from live → absent. Fails the test if the kill errors.
func tsiKillSession(t *testing.T, name string) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", name).CombinedOutput()
	if err != nil {
		t.Fatalf("tsi: failed to kill session %q: %v (output: %s)", name, err, out)
	}
}

// TestIntegration_TmuxSessionLive_RealProbePath exercises the REAL tmux
// liveness-probe subprocess inside the unexported tmuxSessionLive helper:
//
//  1. A name that was never created → tmuxSessionLive reports false (the probe
//     subprocess exits non-zero). This is the assertion the original
//     display-message implementation FAILED, because display-message exits 0
//     for an absent target.
//  2. A unique throwaway session is created → tmuxSessionLive reports true.
//  3. The session is killed by name → tmuxSessionLive reports false (live →
//     absent transition through the real subprocess).
//
// This is the in-package (package keeper) test that can reach the unexported
// helper; the public ResolveTmuxTarget seam is covered separately below.
func TestIntegration_TmuxSessionLive_RealProbePath(t *testing.T) {
	tsiRequireTmux(t)

	// Not parallel: each session has a unique name so there is no shared
	// mutable state, but keeping it serial avoids spamming the tmux server.
	name := tsiUniqueSessionName(t)

	// (2) Never-created name must report absent up front. (Sanity: ensures a
	// false negative isn't masking a constant-true bug.)
	if tmuxSessionLive(name) {
		t.Fatalf("tsi: tmuxSessionLive(%q) reported live BEFORE the session was created", name)
	}

	// (1) Create it for real, then assert live via the real subprocess.
	tsiStartSession(t, name)
	if !tmuxSessionLive(name) {
		t.Fatalf("tsi: tmuxSessionLive(%q) reported absent for a session that IS live", name)
	}

	// (3) Kill it and assert the real probe subprocess now sees it absent
	// (live → absent transition).
	tsiKillSession(t, name)
	if tmuxSessionLive(name) {
		t.Fatalf("tsi: tmuxSessionLive(%q) still reported live after kill-session", name)
	}
}

// TestIntegration_ResolveTmuxTarget_RealLivePath drives the PUBLIC
// ResolveTmuxTarget end-to-end with sessionExistsFn == nil, so resolution runs
// through the real tmuxSessionLive subprocess (no stub).
//
// To make the convention branch match a real session safely, the test creates
// a session under the exact name ResolveTmuxTarget derives —
// HarmonikSessionName(tmpDir, agent). Because tmpDir is a t.TempDir() path
// unique to this test run, the resulting hash (and therefore the session name)
// cannot collide with the real harmonik daemon session
// (harmonik-<realProjectHash>-default) or any other live session. The session
// is still torn down by exact name in t.Cleanup.
//
// Assertions:
//   - session live  → ResolveTmuxTarget returns the derived name.
//   - session killed → ResolveTmuxTarget returns "" (resolution fails safe).
func TestIntegration_ResolveTmuxTarget_RealLivePath(t *testing.T) {
	tsiRequireTmux(t)

	dir := t.TempDir()
	// Unique agent name further guarantees no collision with real sessions.
	agent := fmt.Sprintf("liet2ojne%d", rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance

	// Name ResolveTmuxTarget will derive and probe via the real subprocess.
	derived := HarmonikSessionName(dir, agent)

	// Defensive guard: make sure we are NOT about to shadow the real daemon
	// session name. (Astronomically unlikely given the unique tmpDir+agent,
	// but fail loudly rather than risk touching a live session.)
	if derived == "harmonik-a3dc45482890-default" {
		t.Fatalf("tsi: derived name %q collides with a known real session — aborting for safety", derived)
	}

	// Before creating: real-path resolution must return "" (session absent).
	if got := ResolveTmuxTarget(dir, agent, "", nil); got != "" {
		t.Fatalf("tsi: ResolveTmuxTarget returned %q before session creation; want \"\"", got)
	}

	// Create the throwaway session under the derived name, then resolve.
	tsiStartSession(t, derived)
	if got := ResolveTmuxTarget(dir, agent, "", nil); got != derived {
		t.Fatalf("tsi: ResolveTmuxTarget(real path) = %q; want %q (live session not detected)", got, derived)
	}

	// Kill it and confirm resolution fails safe back to "".
	tsiKillSession(t, derived)
	if got := ResolveTmuxTarget(dir, agent, "", nil); got != "" {
		t.Fatalf("tsi: ResolveTmuxTarget(real path) = %q after kill; want \"\" (should fail safe)", got)
	}
}
