package daemon_test

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

func t5RealDBFixtureSetup(t *testing.T) (projectDir, brWrapper, beadID string) {
	t.Helper()

	realBrPath := t5RealDBLocateBr(t)

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

	createCmd := exec.CommandContext(t.Context(), brWrapper, "create",
		"T5 concurrent claim integration bead", "--status", "open",
		"--labels", "workflow:single", "--silent")
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

func t5RealDBLocateBr(t *testing.T) string {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br required for real-DB concurrent test (not on PATH); CI sets br on PATH")
	}
	return brPath
}

func t5RealDBPollBeadStatus(t *testing.T, brWrapper, beadID, targetStatus string, budget time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(t.Context(), brWrapper, "show", beadID, "--format", "json")
		out, err := cmd.Output()
		if err == nil && strings.Contains(string(out), `"`+targetStatus+`"`) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

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

	adapterA, err := brcli.NewForProject(brWrapper, projectDir)
	if err != nil {
		t.Fatalf("t5: brcli.NewForProject (A): %v", err)
	}
	adapterB, err := brcli.NewForProject(brWrapper, projectDir)
	if err != nil {
		t.Fatalf("t5: brcli.NewForProject (B): %v", err)
	}

	handlerScript := smokeFixtureHandlerScript(t)
	collectorA := &stubEventCollector{}
	collectorB := &stubEventCollector{}

	depsA := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        adapterA,
		Bus:              collectorA,
		ProjectDir:       projectDir,
		HandlerBinary:    handlerScript,
		HandlerArgs:      nil,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
	})
	depsB := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
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

	doneA := make(chan error, 1)
	go func() { doneA <- daemon.ExportedRunWorkLoop(ctxA, depsA) }()

	const claimPollBudget = 10 * time.Second
	claimed := t5RealDBPollBeadStatus(t, brWrapper, beadID, "in_progress", claimPollBudget)
	if !claimed {
		t.Fatalf("t5: bead %s did not reach in_progress within %s; loop A failed to claim", beadID, claimPollBudget)
	}

	ctxB, cancelB := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelB()

	doneB := make(chan error, 1)
	go func() { doneB <- daemon.ExportedRunWorkLoop(ctxB, depsB) }()

	const pollBudget = 20 * time.Second
	closed := t5RealDBPollBeadStatus(t, brWrapper, beadID, "closed", pollBudget)
	if !closed {
		t.Logf("t5: bead %s was not closed within %s", beadID, pollBudget)
	}

	time.Sleep(500 * time.Millisecond)

	cancelA()
	cancelB()
	for loopName, ch := range map[string]chan error{"A": doneA, "B": doneB} {
		if loopErr := awaitLoopTeardownErr(t, ch, "t5: work loop "+loopName); loopErr != nil {
			t.Errorf("t5: work loop %s returned error: %v", loopName, loopErr)
		}
	}

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

	if runStartedCount != 1 {
		t.Errorf("t5: CLAIM_GUARD_FAILED — expected exactly 1 run_started event (pre-claim guard should have blocked loop B); got %d", runStartedCount)
	}

	if !closed {
		t.Errorf("t5: bead %s was not closed; loop A failed to complete", beadID)
	}
}
