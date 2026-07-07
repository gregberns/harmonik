package main

// watch_stall_recover_hkghcqn_test.go — RED acceptance gate for bead hk-ghcqn.
//
// Problem: ops-monitor detects watch-stalled (cursor frozen + actionable events
// past it + stream dead) but only pages the captain with "no self-healing path".
// The captain must then hand-nudge the pane every ~20 min, inverting the watch's
// purpose (it exists to CUT captain wakes, but while wedged it ADDS them).
//
// Fix: on watch-stalled with watch tmux session alive, ops-monitor auto-injects
// Enter into the watch pane (the idle-submit-wedge recovery action) subject to a
// cooldown, before/alongside escalating to the captain. Three behavioral properties:
//
//   P1 — do_nudge is computed from watch_stalled, watch_tmux_alive, and a
//        cooldown (watch_nudge_cooldown / last_watch_nudge_ts). The guard must use
//        all three inputs so nudges are gated by: stall confirmed + session alive
//        + enough time since the last nudge.
//
//   P2 — The shell stall-heal block invokes `tmux send-keys` on the watch session
//        when DO_NUDGE == "true". The block must reference the crew-watch session
//        target and send Enter so the blocked /rc auto-submit is unblocked.
//
//   P3 — do_nudge is included in the snapshot dict so the captain can observe
//        whether a nudge was attempted this tick when reading latest.json.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hkghcqn_repoRoot returns the absolute path to the repository root.
func hkghcqn_repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hkghcqn_repoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// TestWatchStallNudgeVar_GHCQN_P1 asserts that ops-monitor-check.sh computes
// do_nudge from watch_stalled, watch_tmux_alive, and a cooldown.
// The variable must use all three inputs: stall confirmed, session alive, and
// enough time since last nudge. (RED until hk-ghcqn fix lands.)
func TestWatchStallNudgeVar_GHCQN_P1(t *testing.T) {
	root := hkghcqn_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P1: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "do_nudge") {
		t.Errorf("P1: 'do_nudge' not found in ops-monitor-check.sh; " +
			"the stall-heal nudge flag is missing entirely")
		return
	}

	// Must gate on watch_stalled.
	if !strings.Contains(content, "watch_stalled") {
		t.Errorf("P1: do_nudge computation does not reference 'watch_stalled'; " +
			"nudges must only fire when the stall threshold is met")
	}

	// Must gate on watch_tmux_alive (session must exist to nudge it).
	if !strings.Contains(content, "watch_tmux_alive") {
		t.Errorf("P1: do_nudge computation does not reference 'watch_tmux_alive'; " +
			"no point injecting Enter if the watch session does not exist")
	}

	// Must gate on a cooldown: watch_nudge_cooldown and last_watch_nudge_ts (or
	// prev_last_watch_nudge_ts) must both appear so nudges don't fire every tick.
	if !strings.Contains(content, "watch_nudge_cooldown") {
		t.Errorf("P1: 'watch_nudge_cooldown' not found; nudges need a minimum gap " +
			"between attempts to avoid hammering a permanently-wedged pane")
	}
	if !strings.Contains(content, "watch_nudge_ts") {
		t.Errorf("P1: 'watch_nudge_ts' not found; the cooldown requires tracking " +
			"when the last nudge was sent (prev_last_watch_nudge_ts / new_last_watch_nudge_ts)")
	}
}

// TestWatchStallShellNudgeBlock_GHCQN_P2 asserts that the shell stall-heal block
// uses `tmux send-keys` on the crew-watch session to inject Enter when
// DO_NUDGE == "true". Without this, the do_nudge flag exists in the snapshot but
// no recovery action is ever taken. (RED until hk-ghcqn fix lands.)
func TestWatchStallShellNudgeBlock_GHCQN_P2(t *testing.T) {
	root := hkghcqn_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P2: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	// The shell block must extract DO_NUDGE from the snapshot.
	if !strings.Contains(content, "DO_NUDGE") {
		t.Errorf("P2: 'DO_NUDGE' shell variable not found; the script must extract " +
			"do_nudge from the Python analysis output to drive the nudge action")
		return
	}

	// The guard must check DO_NUDGE == "true".
	if !strings.Contains(content, `"$DO_NUDGE" == "true"`) {
		t.Errorf("P2: DO_NUDGE guard '\"$DO_NUDGE\" == \"true\"' not found; " +
			"the tmux send-keys block must be conditional on DO_NUDGE")
	}

	// Must call tmux send-keys to inject Enter.
	if !strings.Contains(content, "tmux send-keys") {
		t.Errorf("P2: 'tmux send-keys' not found in script; " +
			"the stall-heal block must inject Enter into the watch pane")
		return
	}

	// Must target the crew-watch session by name.
	if !strings.Contains(content, "crew-watch") {
		t.Errorf("P2: 'crew-watch' not referenced in tmux send-keys block; " +
			"the nudge must target the harmonik crew-watch session specifically")
	}

	// Must send Enter (the key that unblocks the /rc idle-submit-wedge).
	if !strings.Contains(content, "Enter") {
		t.Errorf("P2: 'Enter' key not sent in tmux send-keys block; " +
			"Enter is the key that unblocks the /rc idle-submit-wedge on the watch pane")
	}
}

// TestWatchNudgeInSnapshot_GHCQN_P3 asserts that do_nudge is included in the
// snapshot dict written to latest.json. This allows the captain to observe whether
// ops-monitor attempted a stall recovery this tick. (RED until hk-ghcqn fix lands.)
func TestWatchNudgeInSnapshot_GHCQN_P3(t *testing.T) {
	root := hkghcqn_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P3: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "'do_nudge': do_nudge") {
		t.Errorf("P3: snapshot dict does not include 'do_nudge': do_nudge; " +
			"the nudge flag must be exposed in latest.json so the captain can " +
			"confirm that auto-recovery was attempted when diagnosing watch health")
	}
}
