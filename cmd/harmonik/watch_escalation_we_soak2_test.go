package main

// watch_escalation_we_soak2_test.go — RED acceptance gate for bead hk-8yh32.1
// (WE-SOAK-2/B1: watch-skill escalation hardening, ops-monitor dedup, paused-
// queue state tracking).
//
// Three behavioral properties are verified:
//
//   P1 — keeper-missing → autonomous REDIRECT documented in watch/SKILL.md.
//        The skill must describe how to handle a missing keeper without
//        immediately escalating to the captain. A "REDIRECT" or "autonomous"
//        directive must appear near the "keeper-missing" keyword.
//
//   P2 — ops-monitor-check.sh normalises the release_due dedup key via a
//        dedicated helper function named "_dedup_key".
//
//   P6 — watch/SKILL.md documents the "pending_paused_queues" field used to
//        track paused-queue state across successive ops-monitor reports.
//
// All three tests are RED until the corresponding fixes land. They contain no
// exec.Command calls; they are pure file-content assertions.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// weSoak2RepoRoot returns the absolute path to the repository root by walking
// two directories up from this source file's location:
//
//	cmd/harmonik/watch_escalation_we_soak2_test.go
//	  ↑ cmd/harmonik/
//	  ↑↑ (repo root)
func weSoak2RepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("weSoak2RepoRoot: runtime.Caller(0) failed")
	}
	// thisFile → .../cmd/harmonik/watch_escalation_we_soak2_test.go
	// Dir    → .../cmd/harmonik
	// Dir    → .../cmd
	// Dir    → ... (repo root)
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
