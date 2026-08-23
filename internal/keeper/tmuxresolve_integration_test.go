//go:build integration

package keeper

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"
)

func tsiRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tsi: tmux not found on PATH; skipping real-tmux integration test")
	}
}

func tsiUniqueSessionName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("liet-2ojne-test-%d-%d", rand.Int64(), rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance
}

func tsiStartSession(t *testing.T, name string) {
	t.Helper()

	ctx := context.Background()

	if out, err := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", name, "sleep", "300").CombinedOutput(); err != nil {
		t.Fatalf("tsi: failed to create throwaway session %q: %v (output: %s)", name, err, out)
	}

	t.Cleanup(func() {
		out, err := exec.CommandContext(context.Background(), "tmux", "kill-session", "-t", name).CombinedOutput()
		if err != nil {
			t.Logf("tsi cleanup: kill-session %q returned %v (output: %s) — likely already gone", name, err, out)
		}
	})
}

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

	name := tsiUniqueSessionName(t)

	if tmuxSessionLive(name) {
		t.Fatalf("tsi: tmuxSessionLive(%q) reported live BEFORE the session was created", name)
	}

	tsiStartSession(t, name)
	if !tmuxSessionLive(name) {
		t.Fatalf("tsi: tmuxSessionLive(%q) reported absent for a session that IS live", name)
	}

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
//   - session live  → ResolveTmuxTarget returns the derived name, targeting the
//     AGENT window's active pane ("<derived>:agent" — see the windowAgent /
//     "Priority 2: bare convention" doc on ResolveTmuxTarget; a keeper running
//     in its own sibling "keeper" window must inject/measure the agent window,
//     never itself).
//   - session killed → ResolveTmuxTarget returns "" (resolution fails safe).
func TestIntegration_ResolveTmuxTarget_RealLivePath(t *testing.T) {
	tsiRequireTmux(t)

	dir := t.TempDir()
	agent := fmt.Sprintf("liet2ojne%d", rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance

	derived := HarmonikSessionName(dir, agent)

	if derived == "harmonik-a3dc45482890-default" {
		t.Fatalf("tsi: derived name %q collides with a known real session — aborting for safety", derived)
	}

	if got := ResolveTmuxTarget(dir, agent, "", nil); got != "" {
		t.Fatalf("tsi: ResolveTmuxTarget returned %q before session creation; want \"\"", got)
	}

	tsiStartSession(t, derived)
	wantLive := derived + ":" + windowAgent
	if got := ResolveTmuxTarget(dir, agent, "", nil); got != wantLive {
		t.Fatalf("tsi: ResolveTmuxTarget(real path) = %q; want %q (live session not detected)", got, wantLive)
	}

	tsiKillSession(t, derived)
	if got := ResolveTmuxTarget(dir, agent, "", nil); got != "" {
		t.Fatalf("tsi: ResolveTmuxTarget(real path) = %q after kill; want \"\" (should fail safe)", got)
	}
}
