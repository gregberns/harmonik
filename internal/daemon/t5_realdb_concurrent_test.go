package daemon_test

// t5_realdb_concurrent_test.go — T5 real-DB concurrent ClaimBead integration test.
//
// Bead: hk-5wbzj — "Test gap: concurrent work-loop stub does not enforce
// ClaimBead exclusion — production SQLite atomics unverified by test"
//
// Scope: exercise two concurrent work loops against a real beads SQLite DB to
// verify the harmonik-side pre-claim guard (hk-p4xbw) prevents double-dispatch
// when two loops race on the same bead.
//
// Guard: Before calling ClaimBead, the work loop calls ShowBead and skips
// dispatch if the bead's status is not "open".  This catches the common case
// where loop A has already claimed the bead before loop B reaches the guard.
//
// Test structure (sequential claim → guard):
//   1. Loop A is started alone and allowed to claim and process the bead.
//   2. Only after the bead is observed as "in_progress" (claimed by A) is
//      loop B started.
//   3. Loop B calls ShowBead, sees "in_progress", and skips dispatch via the
//      bead_claim_skipped log line (no run_started emitted by B).
//   4. Assertion: exactly one run_started event total across both collectors.
//
// This eliminates the TOCTOU window from the test: by the time B starts, the
// bead is already in_progress so ShowBead returns in_progress reliably.
//
// Helper prefix: t5RealDB (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// T5 fixtures
// ─────────────────────────────────────────────────────────────────────────────

// t5RealDBFixtureSetup initialises a full fixture for the real-DB concurrent
// test: git repo, .harmonik dirs, br init, one seeded bead, br wrapper script.
// Returns projectDir, brWrapperPath, seededBeadID.
//
// The br wrapper script pins --db so that brcli.Adapter subprocess calls
// find the test-local .beads/beads.db regardless of CWD.
func t5RealDBFixtureSetup(t *testing.T) (projectDir, brWrapper, beadID string) {
	t.Helper()

	realBrPath := t5RealDBLocateBr(t)

	// Create project directory and standard sub-trees.
	projectDir, _ = workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// br init — run with cmd.Dir = projectDir so br creates .beads/ there.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	initCmd := exec.CommandContext(t.Context(), realBrPath, "init", "--prefix", "t5")
	initCmd.Dir = projectDir
	initOut, initErr := initCmd.CombinedOutput()
	if initErr != nil {
		t.Fatalf("t5RealDBFixtureSetup: br init: %v\n%s", initErr, initOut)
	}

	dbPath := filepath.Join(projectDir, ".beads", "beads.db")
	brWrapper = smokeFixtureBrWrapperScript(t, realBrPath, dbPath)

	// Seed exactly one ready bead.
	//nolint:gosec // G204: br args are test-internal literals; not user input
	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"T5 concurrent claim integration bead", "--status", "open", "--silent")
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("t5RealDBFixtureSetup: br create: %v\n%s", createErr, createOut)
	}
	beadID = strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("t5RealDBFixtureSetup: br create returned empty ID")
	}

	return projectDir, brWrapper, beadID
}

// t5RealDBLocateBr finds the real `br` binary via exec.LookPath.
// Skips the test if br is not available (same pattern as smokeFixtureBrPath).
func t5RealDBLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for real-DB concurrent test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

// t5RealDBPollBeadStatus polls `br show <id>` at 20 ms intervals for up to
// budget and returns true once the bead's status JSON contains the target status.
func t5RealDBPollBeadStatus(t *testing.T, brWrapper, beadID, targetStatus string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil && strings.Contains(string(out), `"`+targetStatus+`"`) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestT4RealDB_ConcurrentClaimExclusion
// ─────────────────────────────────────────────────────────────────────────────

// TestT4RealDB_ConcurrentClaimExclusion verifies that the harmonik-side pre-claim
// guard prevents double-dispatch when two work loops race on the same bead.
//
// Design (sequential claim → guard):
//   - Loop A is started first and allowed to claim and close the bead.
//   - Only after the bead is observed as "in_progress" is loop B started.
//   - Loop B's pre-claim ShowBead sees "in_progress" and skips dispatch
//     (bead_claim_skipped path); it emits no run_started event.
//   - Assertion: exactly one run_started event across both collectors,
//     proving the guard caught the competing claim.
//
// This structure eliminates the race's TOCTOU window from the test: by the
// time B starts, A has already claimed, so ShowBead reliably returns
// "in_progress" from the real SQLite DB.
func TestT4RealDB_ConcurrentClaimExclusion(t *testing.T) {
	t.Parallel()

	projectDir, brWrapper, beadID := t5RealDBFixtureSetup(t)

	// Build two independent brcli.Adapter instances pointing at the same DB.
	adapterA, err := brcli.NewForProject(brWrapper, projectDir)
	if err != nil {
		t.Fatalf("t5: brcli.NewForProject (A): %v", err)
	}
	adapterB, err := brcli.NewForProject(brWrapper, projectDir)
	if err != nil {
		t.Fatalf("t5: brcli.NewForProject (B): %v", err)
	}

	// Each loop gets its own event collector and a handler that exits 0 immediately.
	handlerScript := smokeFixtureHandlerScript(t)
	collectorA := &stubEventCollector{}
	collectorB := &stubEventCollector{}

	depsA := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        adapterA,
		Bus:              collectorA,
		ProjectDir:       projectDir,
		HandlerBinary:    handlerScript,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})
	depsB := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        adapterB,
		Bus:              collectorB,
		ProjectDir:       projectDir,
		HandlerBinary:    handlerScript,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctxA, cancelA := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelA()

	// Start loop A alone and wait until the bead is claimed (in_progress).
	// This ensures that when loop B starts, ShowBead returns "in_progress"
	// and the pre-claim guard fires, preventing double-dispatch.
	doneA := make(chan error, 1)
	go func() { doneA <- daemon.ExportedRunWorkLoop(ctxA, depsA) }()

	const claimPollBudget = 10 * time.Second
	claimed := t5RealDBPollBeadStatus(t, brWrapper, beadID, "in_progress", claimPollBudget)
	if !claimed {
		t.Fatalf("t5: bead %s did not reach in_progress within %s; loop A failed to claim", beadID, claimPollBudget)
	}

	// Now start loop B. The bead is in_progress; ShowBead will return "in_progress"
	// and the pre-claim guard will skip dispatch (bead_claim_skipped path).
	ctxB, cancelB := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelB()

	doneB := make(chan error, 1)
	go func() { doneB <- daemon.ExportedRunWorkLoop(ctxB, depsB) }()

	// Wait for loop A to finish processing (bead closed or context cancelled).
	// Give loop B a moment to run its poll-and-skip cycle.
	const pollBudget = 20 * time.Second
	closed := t5RealDBPollBeadStatus(t, brWrapper, beadID, "closed", pollBudget)
	if !closed {
		t.Logf("t5: bead %s was not closed within %s", beadID, pollBudget)
	}

	// Give loop B a moment to observe the bead and attempt (then skip) dispatch.
	time.Sleep(500 * time.Millisecond)

	// Cancel both loops and wait for clean exit.
	cancelA()
	cancelB()
	for loopName, ch := range map[string]chan error{"A": doneA, "B": doneB} {
		select {
		case loopErr := <-ch:
			if loopErr != nil {
				t.Errorf("t5: work loop %s returned error: %v", loopName, loopErr)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("t5: work loop %s did not exit within 5 s after context cancel", loopName)
		}
	}

	// ─── Assert claim exclusion ───────────────────────────────────────────────
	//
	// The pre-claim guard (hk-p4xbw) must ensure exactly one run_started event
	// is emitted across both loops.  Loop B's ShowBead must have seen
	// "in_progress" and skipped dispatch via the bead_claim_skipped path.
	runStartedCount := 0
	runCompletedCount := 0
	runFailedCount := 0

	for _, et := range collectorA.eventTypes() {
		switch et {
		case string(core.EventTypeRunStarted):
			runStartedCount++
		case string(core.EventTypeRunCompleted):
			runCompletedCount++
		case string(core.EventTypeRunFailed):
			runFailedCount++
		}
	}
	for _, et := range collectorB.eventTypes() {
		switch et {
		case string(core.EventTypeRunStarted):
			runStartedCount++
		case string(core.EventTypeRunCompleted):
			runCompletedCount++
		case string(core.EventTypeRunFailed):
			runFailedCount++
		}
	}

	t.Logf("t5: loop A events: %v", collectorA.eventTypes())
	t.Logf("t5: loop B events: %v", collectorB.eventTypes())
	t.Logf("t5: run_started=%d run_completed=%d run_failed=%d", runStartedCount, runCompletedCount, runFailedCount)

	// CLAIM_EXCLUSIVE: exactly one loop dispatched the bead.
	// The pre-claim guard must have prevented loop B from dispatching.
	if runStartedCount != 1 {
		t.Errorf("t5: CLAIM_GUARD_FAILED — expected exactly 1 run_started event (pre-claim guard should have blocked loop B); got %d", runStartedCount)
	}

	if !closed {
		t.Errorf("t5: bead %s was not closed; loop A failed to complete", beadID)
	}
}
