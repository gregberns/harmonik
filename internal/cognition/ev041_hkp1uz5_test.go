package cognition_test

// ev041_hkp1uz5_test.go — scenario tests for specs/event-model.md §4.11 EV-041
// (git-done-but-no-terminal-event heuristic for subscribe consumers).
//
// EV-041 specifies that after K consecutive heartbeats where a bead_id has
// disappeared from active_runs without a terminal event, the consumer SHOULD
// git-check `git log --all --grep "Harmonik-Run-ID: <run_id>"`. A merged commit
// without a terminal event = daemon crashed mid-terminal-emission; the consumer
// treats git completion as authoritative and synthesizes a Tier-1 reaction.
//
// Scenarios exercised:
//
//	EV041-S1: bead disappears for K consecutive heartbeats without terminal event
//	  → returned as git-check trigger after the K-th absent heartbeat
//
//	EV041-S2: terminal event recorded before K absent heartbeats
//	  → NEVER returned as a trigger (MarkTerminal suppresses it)
//
//	EV041-S3: bead absent for K-1 heartbeats (not yet at threshold)
//	  → NOT returned as a trigger yet
//
//	EV041-S4: bead reappears and then disappears again
//	  → absence count resets on reappearance; full K required again
//
//	EV041-S5: CheckGitForRunID finds a commit with Harmonik-Run-ID trailer
//	  → returns (true, nil)
//
//	EV041-S6: CheckGitForRunID finds no matching commit
//	  → returns (false, nil)
//
//	EV041-S7: isolation — a commit for a different run_id does not satisfy the
//	  check for this run_id
//
// Spec ref: specs/event-model.md §4.11 EV-041.
// Bead: hk-p1uz5.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/cognition"
)

// ─────────────────────────────────────────────────────────────────────────────
// Git fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// ev041GitFixtureSetup creates a minimal local git repo for CheckGitForRunID tests.
// Returns the repo directory path.
func ev041GitFixtureSetup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: args are constant strings in tests.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("ev041Fixture: git %v: %v\n%s", args, err, out)
		}
	}

	runGit("init", "--initial-branch=main")
	runGit("config", "user.email", "test@harmonik.local")
	runGit("config", "user.name", "Harmonik EV041 Test")

	f := filepath.Join(dir, "README")
	if err := os.WriteFile(f, []byte("ev041 fixture\n"), 0o644); err != nil {
		t.Fatalf("ev041Fixture: write README: %v", err)
	}
	runGit("add", "README")
	runGit("commit", "-m", "Initial commit")

	return dir
}

// ev041CommitWithRunIDTrailer adds a commit carrying the given runID as a
// Harmonik-Run-ID trailer.
func ev041CommitWithRunIDTrailer(t *testing.T, repoDir, runID string) {
	t.Helper()

	runGit := func(args ...string) string {
		t.Helper()
		//nolint:gosec // G204: args are constant strings; repoDir is t.TempDir()-based.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ev041CommitWithRunIDTrailer: git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	marker := filepath.Join(repoDir, "ev041-marker-"+runID+".txt")
	if err := os.WriteFile(marker, []byte("run: "+runID+"\n"), 0o644); err != nil {
		t.Fatalf("ev041CommitWithRunIDTrailer: write marker: %v", err)
	}
	runGit("add", marker)
	msg := "feat: ev041 test\n\nHarmonik-Run-ID: " + runID
	runGit("commit", "-m", msg)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tracker tests — EV041-S1 through EV041-S4
// ─────────────────────────────────────────────────────────────────────────────

// TestEV041_S1_DisappearsForKHeartbeats_ReturnsTrigger verifies that a bead_id
// that appeared in active_runs and then disappears for K consecutive heartbeats
// without a terminal event is returned as a git-check trigger.
//
// Spec ref: EV-041 — "after K consecutive heartbeats a bead_id has disappeared
// from active_runs yet no terminal event has been processed … consumer SHOULD
// git-check the missing run_id."
// Bead: hk-p1uz5.
func TestEV041_S1_DisappearsForKHeartbeats_ReturnsTrigger(t *testing.T) {
	t.Parallel()

	const beadID = "hk-ev041-s1"
	const K = 2
	tr := cognition.NewGitDoneHeuristicTracker(K)

	// Heartbeat 1: bead is active.
	triggers := tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadID, AgeSeconds: 10}})
	if len(triggers) != 0 {
		t.Errorf("EV041-S1: heartbeat 1 (bead active): got triggers %v, want none", triggers)
	}

	// Heartbeat 2: bead disappears (absence count = 1, below K=2).
	triggers = tr.ProcessHeartbeat(nil)
	if len(triggers) != 0 {
		t.Errorf("EV041-S1: heartbeat 2 (first absence): got triggers %v, want none (below K=%d)", triggers, K)
	}

	// Heartbeat 3: bead still absent (absence count = 2 = K → trigger).
	triggers = tr.ProcessHeartbeat(nil)
	found := false
	for _, id := range triggers {
		if id == beadID {
			found = true
		}
	}
	if !found {
		t.Errorf("EV041-S1 FAIL: heartbeat 3 (K-th absence): bead %q not in triggers %v; "+
			"must appear after K=%d consecutive absent heartbeats (EV-041)", beadID, triggers, K)
	}

	t.Logf("EV041-S1 PASS: bead %q triggered after K=%d consecutive absent heartbeats", beadID, K)
}

// TestEV041_S2_TerminalEventSuppressesTrigger verifies that MarkTerminal
// prevents a bead_id from ever being returned as a trigger, even after K absent
// heartbeats.
//
// Spec ref: EV-041 — "yet no terminal event has been processed for that run
// since the watermark."
// Bead: hk-p1uz5.
func TestEV041_S2_TerminalEventSuppressesTrigger(t *testing.T) {
	t.Parallel()

	const beadID = "hk-ev041-s2"
	const K = 2
	tr := cognition.NewGitDoneHeuristicTracker(K)

	tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadID, AgeSeconds: 5}})
	tr.MarkTerminal(beadID)

	// K+1 subsequent heartbeats with bead absent.
	for i := 0; i < K+1; i++ {
		triggers := tr.ProcessHeartbeat(nil)
		for _, id := range triggers {
			if id == beadID {
				t.Errorf("EV041-S2 FAIL: heartbeat %d after MarkTerminal: bead %q in triggers; "+
					"MUST NOT trigger after terminal event observed (EV-041)", i+1, beadID)
			}
		}
	}

	t.Logf("EV041-S2 PASS: MarkTerminal suppressed bead %q across %d heartbeats", beadID, K+1)
}

// TestEV041_S3_BelowThresholdNotTriggered verifies that a bead absent for K-1
// consecutive heartbeats is NOT yet returned as a trigger.
//
// Spec ref: EV-041 — "K consecutive heartbeats" threshold must be fully reached.
// Bead: hk-p1uz5.
func TestEV041_S3_BelowThresholdNotTriggered(t *testing.T) {
	t.Parallel()

	const beadID = "hk-ev041-s3"
	const K = 3
	tr := cognition.NewGitDoneHeuristicTracker(K)

	tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadID}})

	for i := 0; i < K-1; i++ {
		triggers := tr.ProcessHeartbeat(nil)
		for _, id := range triggers {
			if id == beadID {
				t.Errorf("EV041-S3 FAIL: heartbeat %d (only %d/%d absent): bead %q triggered prematurely; "+
					"must NOT trigger before K=%d consecutive absent heartbeats (EV-041)", i+1, i+1, K, beadID, K)
			}
		}
	}

	t.Logf("EV041-S3 PASS: bead %q not triggered after only K-1=%d absent heartbeats", beadID, K-1)
}

// TestEV041_S4_ReappearanceResetsCount verifies that when a bead reappears in
// active_runs and then disappears again, the absence count resets, requiring a
// full K absent heartbeats to trigger again.
//
// Spec ref: EV-041 — consecutive absence is the trigger; reappearance breaks
// the streak.
// Bead: hk-p1uz5.
func TestEV041_S4_ReappearanceResetsCount(t *testing.T) {
	t.Parallel()

	const beadID = "hk-ev041-s4"
	const K = 2
	tr := cognition.NewGitDoneHeuristicTracker(K)

	active := []cognition.HeartbeatActiveRun{{BeadID: beadID}}

	// Bead appears, disappears once (count=1 < K=2), reappears.
	tr.ProcessHeartbeat(active)
	triggers := tr.ProcessHeartbeat(nil) // absence count = 1
	for _, id := range triggers {
		if id == beadID {
			t.Errorf("EV041-S4: first absence: bead %q triggered with count=1 < K=%d", beadID, K)
		}
	}
	tr.ProcessHeartbeat(active) // reappears → count resets to 0

	// First absence after reset: count=1 < K=2 → no trigger.
	triggers = tr.ProcessHeartbeat(nil)
	for _, id := range triggers {
		if id == beadID {
			t.Errorf("EV041-S4: post-reset first absence: bead %q triggered prematurely; "+
				"count should be 1 after reset, K=%d", beadID, K)
		}
	}

	// Second consecutive absence after reset: count=2=K → trigger.
	triggers = tr.ProcessHeartbeat(nil)
	found := false
	for _, id := range triggers {
		if id == beadID {
			found = true
		}
	}
	if !found {
		t.Errorf("EV041-S4 FAIL: post-reset K-th absence: bead %q not in triggers %v; "+
			"must trigger after K=%d absent heartbeats following reappearance reset (EV-041)", beadID, triggers, K)
	}

	t.Logf("EV041-S4 PASS: reappearance reset absence count; bead %q re-triggered after K=%d", beadID, K)
}

// TestEV041_MultipleBeads verifies that the tracker handles multiple bead_ids
// independently; one bead reaching the threshold does not affect others.
//
// Spec ref: EV-041 — per-bead tracking, not aggregate.
// Bead: hk-p1uz5.
func TestEV041_MultipleBeads(t *testing.T) {
	t.Parallel()

	const (
		beadA = "hk-ev041-multi-a"
		beadB = "hk-ev041-multi-b"
		beadC = "hk-ev041-multi-c"
	)
	const K = 2
	tr := cognition.NewGitDoneHeuristicTracker(K)

	// All three appear in heartbeat 1.
	tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{
		{BeadID: beadA},
		{BeadID: beadB},
		{BeadID: beadC},
	})

	// heartbeat 2: beadA disappears; beadB and beadC remain.
	tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadB}, {BeadID: beadC}})

	// Record terminal event for beadB.
	tr.MarkTerminal(beadB)

	// heartbeat 3: beadA absent (count=2=K); beadB absent but terminal; beadC still active.
	triggers := tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadC}})

	triggerSet := make(map[string]struct{}, len(triggers))
	for _, id := range triggers {
		triggerSet[id] = struct{}{}
	}

	if _, ok := triggerSet[beadA]; !ok {
		t.Errorf("EV041-multi: beadA %q missing from triggers %v; "+
			"must trigger after K=%d consecutive absent heartbeats", beadA, triggers, K)
	}
	if _, ok := triggerSet[beadB]; ok {
		t.Errorf("EV041-multi: beadB %q in triggers after MarkTerminal; "+
			"MUST NOT trigger when terminal event observed", beadB)
	}
	if _, ok := triggerSet[beadC]; ok {
		t.Errorf("EV041-multi: beadC %q in triggers while still active; "+
			"must NOT trigger an active bead", beadC)
	}

	t.Logf("EV041-multi PASS: independent per-bead tracking: triggers=%v", triggers)
}

// TestEV041_DefaultKIsTwo verifies that NewGitDoneHeuristicTracker with K≤0
// defaults to 2, matching the spec suggestion.
//
// Spec ref: EV-041 — "suggested K=2."
// Bead: hk-p1uz5.
func TestEV041_DefaultKIsTwo(t *testing.T) {
	t.Parallel()

	const beadID = "hk-ev041-default-k"
	tr := cognition.NewGitDoneHeuristicTracker(0) // 0 → normalized to 2

	tr.ProcessHeartbeat([]cognition.HeartbeatActiveRun{{BeadID: beadID}})

	// One absence: should NOT trigger (default K=2).
	triggers := tr.ProcessHeartbeat(nil)
	for _, id := range triggers {
		if id == beadID {
			t.Errorf("EV041-defaultK: triggered after only 1 absence; default K should be 2 (EV-041)")
		}
	}

	// Two absences: should trigger.
	triggers = tr.ProcessHeartbeat(nil)
	found := false
	for _, id := range triggers {
		if id == beadID {
			found = true
		}
	}
	if !found {
		t.Errorf("EV041-defaultK FAIL: bead %q not in triggers %v after 2 absences; "+
			"default K=2 must trigger on 2nd consecutive absent heartbeat (EV-041)", beadID, triggers)
	}

	t.Logf("EV041-defaultK PASS: K=0 normalized to 2; trigger fired correctly")
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckGitForRunID tests — EV041-S5 through EV041-S7
// ─────────────────────────────────────────────────────────────────────────────

// TestEV041_S5_CheckGitForRunID_Found verifies that CheckGitForRunID returns
// (true, nil) when a commit carrying the Harmonik-Run-ID trailer exists in the
// git history.
//
// Spec ref: EV-041 — "git log --all --grep 'Harmonik-Run-ID: <run_id>'; A
// merged commit without a terminal event = daemon crashed mid-terminal-emission."
// Bead: hk-p1uz5.
func TestEV041_S5_CheckGitForRunID_Found(t *testing.T) {
	t.Parallel()

	dir := ev041GitFixtureSetup(t)
	const runID = "019e0000-0000-7000-0000-000000000005"

	ev041CommitWithRunIDTrailer(t, dir, runID)

	found, err := cognition.CheckGitForRunID(t.Context(), dir, runID)
	if err != nil {
		t.Fatalf("EV041-S5: CheckGitForRunID: %v", err)
	}
	if !found {
		t.Errorf("EV041-S5 FAIL: CheckGitForRunID returned false for run_id %q; "+
			"a commit with Harmonik-Run-ID: %[1]s exists — must return true (EV-041)", runID)
	}

	t.Logf("EV041-S5 PASS: CheckGitForRunID found commit for run_id %q", runID)
}

// TestEV041_S6_CheckGitForRunID_NotFound verifies that CheckGitForRunID returns
// (false, nil) when no commit carries the given run_id.
//
// Spec ref: EV-041 — git check is an observational heuristic; absence is valid.
// Bead: hk-p1uz5.
func TestEV041_S6_CheckGitForRunID_NotFound(t *testing.T) {
	t.Parallel()

	dir := ev041GitFixtureSetup(t)
	const absentRunID = "019e0000-0000-7000-0000-000000000006"

	found, err := cognition.CheckGitForRunID(t.Context(), dir, absentRunID)
	if err != nil {
		t.Fatalf("EV041-S6: CheckGitForRunID: %v", err)
	}
	if found {
		t.Errorf("EV041-S6 FAIL: CheckGitForRunID returned true for absent run_id %q; "+
			"no commit with this run_id exists — must return false (EV-041)", absentRunID)
	}

	t.Logf("EV041-S6 PASS: CheckGitForRunID correctly returned false for absent run_id %q", absentRunID)
}

// TestEV041_S7_CheckGitForRunID_Isolation verifies that a commit carrying one
// run_id does not satisfy the check for a different run_id.
//
// Spec ref: EV-041 — grep is keyed to the specific run_id; cross-run confusion
// would produce false Tier-1 reactions.
// Bead: hk-p1uz5.
func TestEV041_S7_CheckGitForRunID_Isolation(t *testing.T) {
	t.Parallel()

	dir := ev041GitFixtureSetup(t)
	const runIDPresent = "019e0000-0000-7000-0000-000000000007"
	const runIDAbsent = "019e0000-0000-7000-0000-000000000008"

	ev041CommitWithRunIDTrailer(t, dir, runIDPresent)

	foundPresent, err := cognition.CheckGitForRunID(t.Context(), dir, runIDPresent)
	if err != nil {
		t.Fatalf("EV041-S7: CheckGitForRunID(present): %v", err)
	}
	if !foundPresent {
		t.Errorf("EV041-S7: run_id %q not found; must return true (positive control)", runIDPresent)
	}

	foundAbsent, err := cognition.CheckGitForRunID(t.Context(), dir, runIDAbsent)
	if err != nil {
		t.Fatalf("EV041-S7: CheckGitForRunID(absent): %v", err)
	}
	if foundAbsent {
		t.Errorf("EV041-S7 FAIL: run_id %q returned true due to commit for different run_id %q; "+
			"check must be isolated to the specific run_id (EV-041)", runIDAbsent, runIDPresent)
	}

	t.Logf("EV041-S7 PASS: commit for %q does not satisfy check for %q", runIDPresent, runIDAbsent)
}
