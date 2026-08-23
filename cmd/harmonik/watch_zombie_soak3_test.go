package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func soak3RepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("soak3RepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

func readScript(t *testing.T, root string) string {
	t.Helper()
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")
	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", scriptPath, err)
	}
	return string(raw)
}

// TestWatchDownInFlapRetain_SOAK3_P7 asserts that 'watch-down' is included in the
// FLAP_RETAIN_PREFIXES tuple. Without this, each keeper-restart cycle drops the
// watch-down state from alerted_immediate and re-fires it as a fresh count=1 IMMEDIATE,
// bypassing the 30-min persistence throttle. (RED until P7 is fixed.)
func TestWatchDownInFlapRetain_SOAK3_P7(t *testing.T) {
	root := soak3RepoRoot(t)
	content := readScript(t, root)

	if !strings.Contains(content, "'watch-down'") {
		t.Errorf("P7: FLAP_RETAIN_PREFIXES does not include 'watch-down'; " +
			"watch-down will re-fire as fresh count=1 on every restart cycle, " +
			"bypassing the persistence throttle")
		return
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FLAP_RETAIN_PREFIXES") && strings.Contains(trimmed, "watch-down") {
			return // found: P7 passes
		}
	}
	t.Errorf("P7: 'watch-down' appears in script but not in the FLAP_RETAIN_PREFIXES assignment; " +
		"check that it is in the tuple on the FLAP_RETAIN_PREFIXES = (...) line")
}

// TestWatchZombieSignal_SOAK3_P8 asserts that ops-monitor-check.sh:
//
//	(a) includes 'watch-zombie' in FLAP_RETAIN_PREFIXES,
//	(b) includes 'watch-zombie' in CRITICAL_PREFIXES (5-min re-alert cadence),
//	(c) normalises the 'watch-zombie:silent=<N>s' dynamic suffix in _dedup_key.
//
// (RED until P8 is fixed.)
func TestWatchZombieSignal_SOAK3_P8(t *testing.T) {
	root := soak3RepoRoot(t)
	content := readScript(t, root)

	lines := strings.Split(content, "\n")
	flapOK := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "FLAP_RETAIN_PREFIXES") && strings.Contains(trimmed, "watch-zombie") {
			flapOK = true
			break
		}
	}
	if !flapOK {
		t.Errorf("P8a: 'watch-zombie' is not in the FLAP_RETAIN_PREFIXES assignment; " +
			"brief activity from the zombie will reset the persistence clock")
	}

	critOK := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "CRITICAL_PREFIXES") && strings.Contains(trimmed, "watch-zombie") {
			critOK = true
			break
		}
	}
	if !critOK {
		t.Errorf("P8b: 'watch-zombie' is not in the CRITICAL_PREFIXES tuple; " +
			"it will use the non-critical 30-min cooldown instead of 5-min")
	}

	if !strings.Contains(content, "watch-zombie:") || !strings.Contains(content, "return 'watch-zombie'") {
		t.Errorf("P8c: _dedup_key does not normalise 'watch-zombie:<suffix>' to 'watch-zombie'; " +
			"each tick will use a different dedup key, bypassing the cooldown")
	}
}

// TestWatchZombieCarryForward_SOAK3_P9 asserts that ops-monitor-check.sh:
//
//	(a) loads last_watch_msg_ts from state.json (PREV_LAST_WATCH_MSG_TS),
//	(b) initialises the Python variable to the carry-forward value before the events scan,
//	(c) saves last_watch_msg_ts to new_state so it persists across ticks.
//
// Without carry-forward, a zombie silent for >256KB of events has last_watch_msg_ts==0
// and the zombie check mistakenly treats it as a fresh session (not yet a zombie).
// (RED until P9 is fixed.)
func TestWatchZombieCarryForward_SOAK3_P9(t *testing.T) {
	root := soak3RepoRoot(t)
	content := readScript(t, root)

	if !strings.Contains(content, "PREV_LAST_WATCH_MSG_TS") {
		t.Errorf("P9a: PREV_LAST_WATCH_MSG_TS is not loaded from state.json; " +
			"carry-forward of last_watch_msg_ts across ticks is missing")
	}

	if !strings.Contains(content, "last_watch_msg_ts = prev_last_watch_msg_ts") {
		t.Errorf("P9b: Python block does not initialise 'last_watch_msg_ts = prev_last_watch_msg_ts'; " +
			"a zombie silent for >256KB of events will not be detected")
	}

	if !strings.Contains(content, "'last_watch_msg_ts': last_watch_msg_ts") {
		t.Errorf("P9c: new_state does not include 'last_watch_msg_ts'; " +
			"carry-forward will not persist to the next tick")
	}
}

// TestWatchZombieSelfHeal_SOAK3_P10 asserts that ops-monitor-check.sh includes a
// self-heal block that kills and restarts the zombie watch session when zombie is
// confirmed and the stall is persistent (tier-3). Without this, a zombie watch can
// stay wedged indefinitely requiring manual intervention. (RED until P10 is fixed.)
func TestWatchZombieSelfHeal_SOAK3_P10(t *testing.T) {
	root := soak3RepoRoot(t)
	content := readScript(t, root)

	if !strings.Contains(content, "DO_SELFHEAL") {
		t.Errorf("P10: DO_SELFHEAL variable is missing; " +
			"the self-heal logic that kills and restarts a zombie watch session is absent")
	}
	if !strings.Contains(content, "harmonik start crew watch") {
		t.Errorf("P10: 'harmonik start crew watch' is missing; " +
			"the self-heal block must restart the watch session after killing the zombie")
	}
	if !strings.Contains(content, "kill-session") {
		t.Errorf("P10: tmux kill-session is missing; " +
			"the self-heal must kill the zombie tmux session before restarting")
	}
}
