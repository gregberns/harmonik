package daemon_test

// t5_realdb_concurrent_test.go — T5 real-DB concurrent ClaimBead integration test.
//
// Bead: hk-5wbzj — "Test gap: concurrent work-loop stub does not enforce
// ClaimBead exclusion — production SQLite atomics unverified by test"
//
// Scope: exercise two concurrent work loops against a real beads SQLite DB to
// characterise the actual behaviour of `br update --claim` under concurrency.
//
// Finding (discovered while implementing this bead):
//   `br update --claim` (as of br v0.1.x) does NOT reject a second concurrent
//   claim on an already-in_progress bead — both concurrent claim calls return
//   exit 0 when they race tightly enough on the same SQLite WAL transaction.
//   This means the "production SQLite atomics prevent double-dispatch" assumption
//   in the bead description is INCORRECT for the current br version.
//
//   Implications:
//   - Two concurrent harmonik work loops CAN double-dispatch a single bead.
//   - The stub-based TestT4_ConcurrentLoops' comment ("production ClaimBead
//     is atomic and prevents double-claim; stub does not enforce this") was
//     written under a false assumption.
//   - A future br version or a harmonik-side concurrency guard (e.g., a
//     "check status before dispatch" step) would be needed to fix this gap.
//
// This test:
//   1. Exercises the real end-to-end path (real br binary, real SQLite DB, two
//      real work loop goroutines).
//   2. Documents the observed double-dispatch behaviour by recording event
//      counts from both loops.
//   3. Serves as a regression guard: if br ever starts rejecting duplicate
//      claims (run_completed count drops to 1), the test log will capture that.
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

// t5RealDBPollBeadClosed polls `br show <id>` at 20 ms intervals for up to
// budget and returns true if the bead reaches "closed" status.
func t5RealDBPollBeadClosed(t *testing.T, brWrapper, beadID string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		//nolint:gosec // G204: br args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil && strings.Contains(string(out), `"closed"`) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// TestT4RealDB_ConcurrentClaimExclusion
// ─────────────────────────────────────────────────────────────────────────────

// TestT4RealDB_ConcurrentClaimExclusion exercises two concurrent work loops
// against a real beads SQLite DB and records the claim-exclusion behaviour of
// `br update --claim`.
//
// Design:
//   - Both loops share the same beads.db via two independent brcli.Adapter
//     instances (same wrapper script, same --db path).
//   - One ready bead is seeded. Both loops may call Ready() and receive it
//     before either completes ClaimBead.
//   - After both loops settle, we record how many run_completed events were
//     emitted across both collectors. This characterises whether `br update
//     --claim` provides claim exclusion in practice.
//
// Observed behaviour (br v0.1.45):
//
//	`br update --claim` does NOT reject a concurrent second claim on an
//	already-in_progress bead; both calls return exit 0. Consequently, double-
//	dispatch is possible when two loops race on the same bead. This test
//	documents that finding and will detect if br's behaviour changes.
//
// The test PASSES regardless of whether double-dispatch occurs: it is a
// documentation/characterisation test, not a strict correctness assertion.
// DOUBLE-DISPATCH is logged as a finding. If br is later fixed to reject
// duplicate claims, the test log will capture the improvement.
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
		BrAdapter:     adapterA,
		Bus:           collectorA,
		ProjectDir:    projectDir,
		HandlerBinary: handlerScript,
		HandlerArgs:   nil,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})
	depsB := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:     adapterB,
		Bus:           collectorB,
		ProjectDir:    projectDir,
		HandlerBinary: handlerScript,
		HandlerArgs:   nil,
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	doneA := make(chan error, 1)
	doneB := make(chan error, 1)
	go func() { doneA <- daemon.ExportedRunWorkLoop(ctx, depsA) }()
	go func() { doneB <- daemon.ExportedRunWorkLoop(ctx, depsB) }()

	// Poll until at least one loop closes the bead (or budget expires).
	const pollBudget = 20 * time.Second
	closed := t5RealDBPollBeadClosed(t, brWrapper, beadID, pollBudget)
	if !closed {
		t.Logf("t5: bead %s was not closed within %s; both loops may have failed to claim", beadID, pollBudget)
	}

	// Give both loops a moment to settle after the bead is closed.
	time.Sleep(300 * time.Millisecond)

	// Cancel both loops and wait for clean exit.
	cancel()
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

	// ─── Characterise the claim-exclusion behaviour ───────────────────────────
	//
	// Count run_started and run_completed/run_failed events across both loops.
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

	if !closed {
		// Neither loop closed the bead — something more fundamental failed.
		t.Errorf("t5: bead %s was not closed; check br wrapper and DB setup", beadID)
		return
	}

	// Characterise the outcome.
	switch {
	case runStartedCount == 1 && runCompletedCount == 1:
		// Ideal case: exactly one loop claimed and closed the bead.
		t.Log("t5: CLAIM_EXCLUSIVE — bead dispatched exactly once; " +
			"`br update --claim` provided exclusion in this run")

	case runStartedCount > 1:
		// Double-dispatch occurred: both loops successfully claimed the bead.
		// As of br v0.1.45, this is the observed behaviour when two loops race
		// on the same bead — `br update --claim` does not reject the second
		// concurrent claim.
		//
		// This is NOT a test failure (the test is a characterisation test) but
		// it IS a known safety gap. The Logf call ensures the finding appears in
		// test output for any future reviewer.
		t.Logf("t5: FINDING_DOUBLE_DISPATCH — %d run_started events observed; "+
			"`br update --claim` (br v0.1.x) does not prevent concurrent claim "+
			"by a second work loop; production harmonik currently relies on a "+
			"single-process MVH constraint (one work loop per daemon) to avoid "+
			"this; concurrent runs are a post-MVH unlock that will require a "+
			"harmonik-side claim guard or a br-level fix", runStartedCount)

	case runStartedCount == 0:
		t.Errorf("t5: no run_started event emitted; neither loop dispatched the bead; "+
			"closed=%v events_A=%v events_B=%v", closed, collectorA.eventTypes(), collectorB.eventTypes())
	}
}
