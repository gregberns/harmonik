package main

// watch_stall_liveness_hkgioct_test.go — RED acceptance gate for bead hk-gioct.
//
// Problem: ops-monitor watch-up falsely fires watch-stalled whenever the subscribe
// stream is alive (watch_zombie=false) and the watch is busy in a long agent turn or
// beats on a cadence longer than the stall window. Alerts #1–#5 on 2026-07-05 were
// all false positives of this kind.
//
// Fix: add a stream-liveness gate (watch_stream_live) keyed on last_watch_msg_ts
// and watch_zombie_threshold. When the stream is alive (watch sent a message within
// the zombie threshold), a frozen cursor is a long agent turn — the counter holds
// rather than incrementing. The stall counter only increments when the stream appears
// dead (same evidence that triggers watch_zombie).
//
// Three behavioral properties:
//
//   P1 — watch_stream_live is computed from last_watch_msg_ts and watch_zombie_threshold.
//        The variable must use the zombie_threshold as the liveness window so the two
//        flags are complementary (stream alive ↔ not zombie, for last_watch_msg_ts > 0).
//
//   P2 — When watch_stream_live is True, the stall counter holds at its current value
//        (prev_watch_stall_misses) rather than incrementing. The Python block must
//        contain the guard branch that sets new_watch_stall_misses = prev_watch_stall_misses
//        when watch_stream_live is True.
//
//   P3 — watch_stream_live is included in the snapshot dict for observability, so the
//        captain can read it from latest.json when diagnosing watch health.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hkgioct_repoRoot returns the absolute path to the repository root.
func hkgioct_repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hkgioct_repoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// TestWatchStallStreamLivenessVar_GIOCT_P1 asserts that ops-monitor-check.sh
// computes watch_stream_live from last_watch_msg_ts and watch_zombie_threshold.
// The variable must appear in the Python analysis block with both those inputs.
// (RED until hk-gioct fix lands.)
func TestWatchStallStreamLivenessVar_GIOCT_P1(t *testing.T) {
	root := hkgioct_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P1: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "watch_stream_live") {
		t.Errorf("P1: 'watch_stream_live' not found in ops-monitor-check.sh; " +
			"the stream-liveness gate variable is missing")
		return
	}

	// The variable must be computed using both last_watch_msg_ts and watch_zombie_threshold.
	if !strings.Contains(content, "last_watch_msg_ts > 0") {
		t.Errorf("P1: watch_stream_live computation does not check 'last_watch_msg_ts > 0'; " +
			"new sessions (no messages yet) should have stream_live=False")
	}
	if !strings.Contains(content, "watch_zombie_threshold") ||
		!strings.Contains(content, "ts_epoch - last_watch_msg_ts") {
		t.Errorf("P1: watch_stream_live does not compare elapsed time against watch_zombie_threshold; " +
			"the liveness window must use the zombie threshold so the two flags are complementary")
	}
}

// TestWatchStallCounterHoldsWhenStreamLive_GIOCT_P2 asserts that the stall counter
// holds at prev_watch_stall_misses (not incrementing) when watch_stream_live is True.
// The Python block must contain the guard:
//
//	if watch_stream_live:
//	    new_watch_stall_misses = prev_watch_stall_misses
//
// Without this, the counter would increment on every frozen-cursor tick regardless
// of stream liveness, producing false watch-stalled alerts during long agent turns.
// (RED until hk-gioct fix lands.)
func TestWatchStallCounterHoldsWhenStreamLive_GIOCT_P2(t *testing.T) {
	root := hkgioct_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P2: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	// The guard must be present: when stream alive, hold the counter.
	if !strings.Contains(content, "watch_stream_live") {
		t.Errorf("P2: 'watch_stream_live' not found; stream-liveness gate missing entirely")
		return
	}

	// Check that the hold assignment exists: new_watch_stall_misses = prev_watch_stall_misses
	// under a watch_stream_live branch (not in the increment branch).
	lines := strings.Split(content, "\n")
	foundLiveGuard := false
	foundHoldAssign := false
	inLiveBranch := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "if watch_stream_live:") {
			foundLiveGuard = true
			inLiveBranch = true
			continue
		}
		if inLiveBranch {
			if strings.Contains(trimmed, "new_watch_stall_misses = prev_watch_stall_misses") {
				foundHoldAssign = true
			}
			// The hold branch is just one line; the else/next branch ends it.
			if strings.HasPrefix(trimmed, "else:") || strings.HasPrefix(trimmed, "new_watch_stall_misses = prev_watch_stall_misses + 1") {
				inLiveBranch = false
			}
		}
	}
	if !foundLiveGuard {
		t.Errorf("P2: 'if watch_stream_live:' guard not found in script; " +
			"the stream-liveness hold branch is missing")
	}
	if !foundHoldAssign {
		t.Errorf("P2: 'new_watch_stall_misses = prev_watch_stall_misses' hold assignment " +
			"not found under the watch_stream_live guard; the counter increments unconditionally")
	}
}

// TestWatchStreamLiveInSnapshot_GIOCT_P3 asserts that watch_stream_live is included
// in the snapshot dict written to latest.json. This allows the captain to observe
// stream liveness when reading the ops-monitor snapshot. (RED until hk-gioct fix lands.)
func TestWatchStreamLiveInSnapshot_GIOCT_P3(t *testing.T) {
	root := hkgioct_repoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P3: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "'watch_stream_live': watch_stream_live") {
		t.Errorf("P3: snapshot dict does not include 'watch_stream_live': watch_stream_live; " +
			"the stream-liveness flag is not exposed in latest.json for captain observability")
	}
}
