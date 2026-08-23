package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func weSoak2RepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("weSoak2RepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// TestWatchSkillKeeperMissingRedirect_WE_SOAK2_P1 asserts that
// .claude/skills/watch/SKILL.md documents autonomous redirect behaviour for
// the keeper-missing case. The file must contain "keeper-missing" AND either
// "REDIRECT" or "autonomous" in its text. (RED until P1 is fixed.)
func TestWatchSkillKeeperMissingRedirect_WE_SOAK2_P1(t *testing.T) {
	root := weSoak2RepoRoot(t)
	skillPath := filepath.Join(root, ".claude", "skills", "watch", "SKILL.md")

	raw, err := os.ReadFile(skillPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P1: cannot read %s: %v", skillPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "keeper-missing") {
		t.Errorf("P1: SKILL.md does not contain 'keeper-missing'; the autonomous-redirect behaviour is not documented")
	}

	hasRedirect := strings.Contains(content, "REDIRECT") || strings.Contains(content, "autonomous")
	if !hasRedirect {
		t.Errorf("P1: SKILL.md contains 'keeper-missing' but neither 'REDIRECT' nor 'autonomous' appears; " +
			"the skill must describe the autonomous redirect path (not a pure captain IMMEDIATE escalation)")
	}
}

// TestOpsMonitorReleaseDueDedupKeyNormalized_WE_SOAK2_P2 asserts that
// scripts/ops-monitor-check.sh contains the "_dedup_key" normalization helper
// function. (RED until P2 is fixed.)
func TestOpsMonitorReleaseDueDedupKeyNormalized_WE_SOAK2_P2(t *testing.T) {
	root := weSoak2RepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")

	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P2: cannot read %s: %v", scriptPath, err)
	}

	if !strings.Contains(string(raw), "_dedup_key") {
		t.Errorf("P2: ops-monitor-check.sh does not contain '_dedup_key'; " +
			"the release_due dedup-key normalization helper is missing")
	}
}

// TestWatchSkillPausedQueueStateTracking_WE_SOAK2_P6 asserts that
// .claude/skills/watch/SKILL.md documents the "pending_paused_queues" field
// used to track paused-queue state across ops-monitor reports. (RED until P6
// is fixed.)
func TestWatchSkillPausedQueueStateTracking_WE_SOAK2_P6(t *testing.T) {
	root := weSoak2RepoRoot(t)
	skillPath := filepath.Join(root, ".claude", "skills", "watch", "SKILL.md")

	raw, err := os.ReadFile(skillPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P6: cannot read %s: %v", skillPath, err)
	}

	if !strings.Contains(string(raw), "pending_paused_queues") {
		t.Errorf("P6: SKILL.md does not contain 'pending_paused_queues'; " +
			"the paused-queue state-tracking field is not documented")
	}
}

// TestOpsMonitorWatchPresentUsesAbsentThresh_WE_SOAK2_P5a asserts that
// ops-monitor-check.sh does NOT gate watch_present on watch_info['online'] AND
// not _watch_stale. The 'online' status uses the 120s comms TTL, which is shorter
// than the 270s watch liveness-beat interval: a healthy watch between beats reads as
// "not online" for the 150s gap (t+120 to t+270), causing false watch-down. The fix
// uses watch_absent_thresh as the sole gate — the script must contain
// "watch_present = not _watch_stale" without the 'online' condition. (RED until P5a
// is fixed; GREEN after the threshold-alignment fix lands.)
func TestOpsMonitorWatchPresentUsesAbsentThresh_WE_SOAK2_P5a(t *testing.T) {
	root := weSoak2RepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")

	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P5a: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if strings.Contains(content, "watch_info['online'] and not _watch_stale") {
		t.Errorf("P5a: ops-monitor-check.sh still gates watch_present on " +
			"watch_info['online'] — the 120s comms TTL is shorter than the 270s " +
			"liveness-beat interval; fix: use 'watch_present = not _watch_stale'")
	}

	if !strings.Contains(content, "watch_present = not _watch_stale") {
		t.Errorf("P5a: ops-monitor-check.sh does not contain " +
			"'watch_present = not _watch_stale'; " +
			"the watch_absent_thresh-only gate is missing")
	}
}

// TestOpsMonitorWatchDownBootWarmup_WE_SOAK2_P5b asserts that
// ops-monitor-check.sh suppresses watch_down during the post-join boot warmup
// window. At boot, the watch session may not have joined comms yet; without this
// suppression the dual-probe (comms absent + tmux absent) fires immediately and
// generates false watch-down immediates. The watch_down assignment must include
// watch_restart_suppressed so the existing WATCH_RESTART_SUPPRESS_WINDOW gate
// covers both watch_stalled and watch_down. (RED until P5b is fixed; GREEN after
// the warmup-suppression fix lands.)
func TestOpsMonitorWatchDownBootWarmup_WE_SOAK2_P5b(t *testing.T) {
	root := weSoak2RepoRoot(t)
	scriptPath := filepath.Join(root, "scripts", "ops-monitor-check.sh")

	raw, err := os.ReadFile(scriptPath) //nolint:gosec // path constructed from repo root, not user input
	if err != nil {
		t.Fatalf("P5b: cannot read %s: %v", scriptPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "watch_restart_suppressed") {
		t.Errorf("P5b: ops-monitor-check.sh does not contain 'watch_restart_suppressed'; " +
			"the boot-warmup suppression variable is missing")
		return
	}

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "watch_down =") && strings.Contains(line, "watch_opsmonitor_target") {
			window := line
			for j := 1; j <= 4 && i+j < len(lines); j++ {
				next := strings.TrimSpace(lines[i+j])
				window += " " + next
				if next == "" || !strings.HasSuffix(next, ")") && !strings.HasPrefix(next, "and ") {
					break
				}
			}
			if !strings.Contains(window, "watch_restart_suppressed") {
				t.Errorf("P5b: watch_down assignment block does not include "+
					"watch_restart_suppressed: %q", window)
			}
			return
		}
	}
	t.Errorf("P5b: could not find watch_down assignment line containing 'watch_opsmonitor_target'")
}
