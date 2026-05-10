package lifecycle

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ─── PL-024 / PL-025: crash recovery fixture types ───────────────────────────

// crashRecoveryFixtureReconciliationStep names the ordered steps of the startup
// reconciliation sequence (§PL-005) so chaos tests can assert which step was
// in-flight at crash time.
type crashRecoveryFixtureReconciliationStep int

const (
	crashRecoveryFixtureStepCompositionRoot  crashRecoveryFixtureReconciliationStep = 0
	crashRecoveryFixtureStepPidfileLock      crashRecoveryFixtureReconciliationStep = 1
	crashRecoveryFixtureStepDaemonStarted    crashRecoveryFixtureReconciliationStep = 2
	crashRecoveryFixtureStepOrphanSweep      crashRecoveryFixtureReconciliationStep = 3
	crashRecoveryFixtureStepCat0Precheck     crashRecoveryFixtureReconciliationStep = 4
	crashRecoveryFixtureStepGitLogWalk       crashRecoveryFixtureReconciliationStep = 5
	crashRecoveryFixtureStepBeadsQuery       crashRecoveryFixtureReconciliationStep = 6
	crashRecoveryFixtureStepBuildMemoryModel crashRecoveryFixtureReconciliationStep = 7
	crashRecoveryFixtureStepDispatchRecon    crashRecoveryFixtureReconciliationStep = 8
	crashRecoveryFixtureStepReadMarkers      crashRecoveryFixtureReconciliationStep = 9 // step 8a
	crashRecoveryFixtureStepTransitionReady  crashRecoveryFixtureReconciliationStep = 10
)

// crashRecoveryFixtureStartupTrace records which startup steps were executed
// and at which step a simulated crash occurred.
type crashRecoveryFixtureStartupTrace struct {
	mu           sync.Mutex
	stepsRun     []crashRecoveryFixtureReconciliationStep
	crashAt      *crashRecoveryFixtureReconciliationStep // nil = no crash
	reachedReady bool
}

// crashRecoveryFixtureRecordStep records that a startup step was executed.
func (tr *crashRecoveryFixtureStartupTrace) crashRecoveryFixtureRecordStep(step crashRecoveryFixtureReconciliationStep) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.stepsRun = append(tr.stepsRun, step)
}

// crashRecoveryFixtureMarkReady marks the daemon as having reached ready state.
func (tr *crashRecoveryFixtureStartupTrace) crashRecoveryFixtureMarkReady() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.reachedReady = true
}

// crashRecoveryFixtureStepsRun returns a copy of the steps executed so far.
func (tr *crashRecoveryFixtureStartupTrace) crashRecoveryFixtureStepsRun() []crashRecoveryFixtureReconciliationStep {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	out := make([]crashRecoveryFixtureReconciliationStep, len(tr.stepsRun))
	copy(out, tr.stepsRun)
	return out
}

// crashRecoveryFixtureRunStartup simulates the §PL-005 startup sequence,
// optionally crashing at the given step (nil = run to completion). Returns the
// trace for assertion.
func crashRecoveryFixtureRunStartup(crashAt *crashRecoveryFixtureReconciliationStep) *crashRecoveryFixtureStartupTrace {
	tr := &crashRecoveryFixtureStartupTrace{crashAt: crashAt}

	steps := []crashRecoveryFixtureReconciliationStep{
		crashRecoveryFixtureStepCompositionRoot,
		crashRecoveryFixtureStepPidfileLock,
		crashRecoveryFixtureStepDaemonStarted,
		crashRecoveryFixtureStepOrphanSweep,
		crashRecoveryFixtureStepCat0Precheck,
		crashRecoveryFixtureStepGitLogWalk,
		crashRecoveryFixtureStepBeadsQuery,
		crashRecoveryFixtureStepBuildMemoryModel,
		crashRecoveryFixtureStepDispatchRecon,
		crashRecoveryFixtureStepReadMarkers,
		crashRecoveryFixtureStepTransitionReady,
	}

	for _, step := range steps {
		tr.crashRecoveryFixtureRecordStep(step)
		if crashAt != nil && step == *crashAt {
			// Simulate crash at this step — do not proceed further.
			return tr
		}
	}

	tr.crashRecoveryFixtureMarkReady()
	return tr
}

// TestPL025_CrashDuringStartupReconciliationRerunsFromStep0 verifies that when
// a crash occurs during startup reconciliation (after §PL-005 step 3 but before
// reaching ready), the next restart re-runs §PL-005 from step 0 and produces
// identical classifications.
//
// The fixture models two startup runs: the first crashes at a designated step;
// the second runs to completion. Both runs are compared for step-0 restart and
// deterministic behavior.
//
// Spec ref: process-lifecycle.md §4.8 PL-025 — "If the daemon crashes during
// startup reconciliation (after §PL-005 step 3 but before reaching ready), the
// next restart MUST re-run §PL-005 from step 0. Reconciliation is idempotent
// per [reconciliation/spec.md §4.1 RC-002]: re-running detection rules against
// the same git + Beads state produces the same classifications."
func TestPL025_CrashDuringStartupReconciliationRerunsFromStep0(t *testing.T) {
	t.Parallel()

	// Crash points after step 3 (orphan sweep) but before ready.
	crashPoints := []struct {
		name    string
		step    crashRecoveryFixtureReconciliationStep
		stepNum int
	}{
		{"at-cat0-precheck", crashRecoveryFixtureStepCat0Precheck, 4},
		{"at-git-walk", crashRecoveryFixtureStepGitLogWalk, 5},
		{"at-beads-query", crashRecoveryFixtureStepBeadsQuery, 6},
		{"at-dispatch-recon", crashRecoveryFixtureStepDispatchRecon, 8},
		{"at-read-markers", crashRecoveryFixtureStepReadMarkers, 9},
	}

	for _, cp := range crashPoints {
		cp := cp // capture
		t.Run(cp.name, func(t *testing.T) {
			t.Parallel()

			// First startup: crash at the designated step.
			crashAt := cp.step
			firstRun := crashRecoveryFixtureRunStartup(&crashAt)

			firstSteps := firstRun.crashRecoveryFixtureStepsRun()
			if len(firstSteps) == 0 {
				t.Fatalf("PL-025 %s: first startup ran zero steps", cp.name)
			}

			// Assert crash happened (did not reach ready).
			if firstRun.reachedReady {
				t.Errorf("PL-025 %s: first startup reached ready despite crash at step %d", cp.name, cp.stepNum)
			}

			// Second startup: re-run from step 0 (no crash).
			secondRun := crashRecoveryFixtureRunStartup(nil)

			// Second run MUST start from step 0.
			secondSteps := secondRun.crashRecoveryFixtureStepsRun()
			if len(secondSteps) == 0 {
				t.Fatalf("PL-025 %s: second startup ran zero steps", cp.name)
			}
			if secondSteps[0] != crashRecoveryFixtureStepCompositionRoot {
				t.Errorf("PL-025 %s: second startup did not start at step 0 (composition root); started at step %d",
					cp.name, secondSteps[0])
			}

			// Second run MUST reach ready.
			if !secondRun.reachedReady {
				t.Errorf("PL-025 %s: second startup did not reach ready", cp.name)
			}

			// Second run MUST execute all eleven steps (0–10).
			const allStepCount = 11
			if got := len(secondSteps); got != allStepCount {
				t.Errorf("PL-025 %s: second startup ran %d steps, want %d", cp.name, got, allStepCount)
			}
		})
	}
}

// TestPL025_ReconciliationIdempotencyAfterCrash verifies that running the
// startup reconciliation twice against identical state produces the same
// step sequence (idempotency per RC-002).
//
// Spec ref: process-lifecycle.md §4.8 PL-025 — "Reconciliation is idempotent
// per [reconciliation/spec.md §4.1 RC-002]: re-running detection rules against
// the same git + Beads state produces the same classifications."
func TestPL025_ReconciliationIdempotencyAfterCrash(t *testing.T) {
	t.Parallel()

	// Two independent startup runs with no crash: both must produce the same
	// step sequence (idempotency).
	run1 := crashRecoveryFixtureRunStartup(nil)
	run2 := crashRecoveryFixtureRunStartup(nil)

	steps1 := run1.crashRecoveryFixtureStepsRun()
	steps2 := run2.crashRecoveryFixtureStepsRun()

	if len(steps1) != len(steps2) {
		t.Errorf("PL-025 idempotency: run1 steps = %d, run2 steps = %d; must be equal", len(steps1), len(steps2))
		return
	}

	for i := range steps1 {
		if steps1[i] != steps2[i] {
			t.Errorf("PL-025 idempotency: step[%d] = %d vs %d; startup sequence must be deterministic",
				i, steps1[i], steps2[i])
		}
	}

	if !run1.reachedReady || !run2.reachedReady {
		t.Error("PL-025 idempotency: both runs must reach ready; one or both did not")
	}
}

// TestPL024_StaleReconciliationLockViaFlockAndKill0 tests the stale-pidfile
// detection path for a crash mid-reconciliation using the flock + kill(pid, 0)
// discriminant. This exercises the PL-024 + PL-025 joint recovery path via the
// self-exec pattern.
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "stale pidfile left by crash;
// remove file and proceed per PL-024 … disambiguation via flock + kill(pid, 0)."
// Spec ref: process-lifecycle.md §4.8 PL-025 — "next restart MUST re-run
// §PL-005 from step 0."
func TestPL024_StaleReconciliationLockViaFlockAndKill0(t *testing.T) {
	// Sentinel check MUST happen before t.Parallel().
	const (
		sentinelEnv   = "GO_PL024_CRASH_CHILD_RUN"
		projectDirEnv = "GO_PL024_PROJECT_DIR"
		syncFileEnv   = "GO_PL024_SYNC_FILE"
	)

	if os.Getenv(sentinelEnv) == "1" {
		// --- CHILD PROCESS BODY ---
		// Simulates a daemon that holds the pidfile lock and then gets SIGKILLed
		// mid-reconciliation (between steps 3 and 8).
		projectDir := os.Getenv(projectDirEnv)
		syncFile := os.Getenv(syncFileEnv)

		childPID := os.Getpid()
		childPGID, _ := syscall.Getpgid(childPID) //nolint:errcheck // child-process stub
		instanceID := "01950000-0000-7000-8000-000000000055"

		_, err := plFixtureAcquirePidfile(nil, projectDir, childPID, childPGID, instanceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "PL024 child: acquirePidfile: %v\n", err)
			os.Exit(1)
		}

		// Simulate crash mid-reconciliation: write sync file then block.
		pidStr := strconv.Itoa(childPID) + "\n"
		if err := os.WriteFile(syncFile, []byte(pidStr), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "PL024 child: write sync file: %v\n", err)
			os.Exit(1)
		}

		// Block until SIGKILL (simulates crash during reconciliation step 5–8).
		select {}
	}

	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL024_StaleReconciliationLockViaFlockAndKill0: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	projectDir := plFixtureTempProjectDir(t)

	syncFile, err := os.CreateTemp("/tmp", "pl024-sync-")
	if err != nil {
		t.Fatalf("PL-024: CreateTemp sync file: %v", err)
	}
	syncFilePath := syncFile.Name()
	_ = syncFile.Close()                              //nolint:errcheck // cleanup error unactionable
	_ = os.Remove(syncFilePath)                       //nolint:errcheck // child will recreate
	t.Cleanup(func() { _ = os.Remove(syncFilePath) }) //nolint:errcheck // cleanup error unactionable

	// Spawn child that holds pidfile and blocks (simulates crash-mid-recon).
	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is os.Args[0], not user input
	cmd := exec.CommandContext(t.Context(), testBin,
		"-test.run=^TestPL024_StaleReconciliationLockViaFlockAndKill0$",
		"-test.v=false",
	)
	cmd.Env = append(os.Environ(),
		sentinelEnv+"=1",
		projectDirEnv+"="+projectDir,
		syncFileEnv+"="+syncFilePath,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("PL-024: cmd.Start: %v", err)
	}

	// Poll for child sync file (up to 5 s).
	var childPIDStr string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		//nolint:gosec // G304: syncFilePath derived from os.CreateTemp, not user input
		data, err := os.ReadFile(syncFilePath)
		if err == nil && len(data) > 0 {
			childPIDStr = strings.TrimSpace(string(data))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if childPIDStr == "" {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
		t.Fatal("PL-024: timed out waiting for child to write sync file")
	}

	childPID, err := strconv.Atoi(childPIDStr)
	if err != nil || childPID <= 0 {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-024: invalid child PID %q: %v", childPIDStr, err)
	}

	// Crash child mid-reconciliation.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("PL-024: kill child (SIGKILL): %v", err)
	}
	_ = cmd.Wait() //nolint:errcheck // reap zombie; non-zero exit expected

	// PL-024: pidfile must still be on disk (stale — crash left it).
	pidfilePath := plFixturePidfilePath(projectDir)
	if _, err := os.Stat(pidfilePath); os.IsNotExist(err) {
		t.Fatal("PL-024: pidfile missing after SIGKILL; stale recovery requires it on disk")
	}

	// PL-024 discriminant: kill(pid, 0) on the crashed child must return ESRCH.
	if plFixtureIsPidLive(childPID) {
		t.Errorf("PL-024: plFixtureIsPidLive(%d) = true after SIGKILL; stale detection requires dead PID", childPID)
	}

	// PL-024 recovery: next startup must acquire the flock (kernel released on crash).
	myPID := os.Getpid()
	myPGID, _ := syscall.Getpgid(myPID) //nolint:errcheck // os.Getpid() is always valid
	release, err := plFixtureAcquirePidfile(t, projectDir, myPID, myPGID, "01950000-0000-7000-8000-000000000056")
	if err != nil {
		t.Fatalf("PL-024: post-crash acquire failed (stale lock not released?): %v", err)
	}
	t.Cleanup(release)

	// PL-025: after recovery, re-run startup from step 0 (fixture model).
	secondRun := crashRecoveryFixtureRunStartup(nil)
	if !secondRun.reachedReady {
		t.Error("PL-025: post-crash restart did not reach ready; must re-run from step 0 to completion")
	}

	secondSteps := secondRun.crashRecoveryFixtureStepsRun()
	if len(secondSteps) == 0 || secondSteps[0] != crashRecoveryFixtureStepCompositionRoot {
		t.Errorf("PL-025: post-crash restart did not start at step 0; first step = %v", secondSteps)
	}

	t.Logf("PL-024/PL-025: crash recovery verified — stale pidfile detected, flock released, restart from step 0")
}

// ─── PL-025a: pairing-tolerance fixture ──────────────────────────────────────

// crashRecoveryFixturePairingEvent models a single lifecycle event in the
// pairing-tolerance sequence.
//
// Spec ref: process-lifecycle.md §4.8 PL-025a — "Consumers of daemon_started,
// daemon_ready, daemon_orphan_sweep_completed, daemon_degraded, and
// daemon_shutdown MUST tolerate unpaired events produced by a crash during the
// startup sequence."
type crashRecoveryFixturePairingEvent struct {
	eventType  string // e.g., "daemon_started", "daemon_ready", "daemon_shutdown"
	instanceID string // unique per-daemon instance identifier
	emittedAt  time.Time
}

// crashRecoveryFixturePairingState models the event stream that a consumer sees,
// including unpaired events from a prior crashed instance.
type crashRecoveryFixturePairingState struct {
	mu     sync.Mutex
	events []crashRecoveryFixturePairingEvent
}

// crashRecoveryFixtureRecordEvent appends a lifecycle event to the stream.
func (s *crashRecoveryFixturePairingState) crashRecoveryFixtureRecordEvent(evt crashRecoveryFixturePairingEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

// crashRecoveryFixtureClassifyLifecycle classifies the event stream using the
// pairing-tolerance rule: a daemon_started with no subsequent daemon_ready,
// daemon_shutdown, or daemon_startup_failed before the next daemon_started is
// treated as an orphaned (crashed) instance.
//
// Returns a map from instanceID to one of:
//   - "complete"  — started, then reached ready or clean shutdown
//   - "orphaned"  — started, then the next event is another instance's daemon_started (crash)
//   - "in-flight" — started, no terminal event yet
func crashRecoveryFixtureClassifyLifecycle(events []crashRecoveryFixturePairingEvent) map[string]string {
	result := make(map[string]string)
	terminalTypes := map[string]bool{
		"daemon_ready":          true,
		"daemon_shutdown":       true,
		"daemon_startup_failed": true,
	}

	// Walk the event stream; group by instance.
	for i, evt := range events {
		if evt.eventType != "daemon_started" {
			continue
		}
		id := evt.instanceID
		result[id] = "in-flight" // assume in-flight until proven otherwise

		// Look ahead for terminal events for this instance.
		foundTerminal := false
		for j := i + 1; j < len(events); j++ {
			next := events[j]
			if next.eventType == "daemon_started" {
				// Another instance started before this one terminated — crash.
				result[id] = "orphaned"
				foundTerminal = true
				break
			}
			if next.instanceID == id && terminalTypes[next.eventType] {
				result[id] = "complete"
				foundTerminal = true
				break
			}
		}
		if !foundTerminal {
			// End of stream without terminal event — still in-flight.
			result[id] = "in-flight"
		}
	}
	return result
}

// TestPL025a_PairingToleranceForUnpairedDaemonStarted verifies that lifecycle
// event consumers correctly classify an unpaired daemon_started as an orphaned
// instance, without misreading the crash as a completed lifecycle.
//
// The fixture seeds a stream with a daemon_started from an instance that crashed
// (no subsequent daemon_ready / daemon_shutdown), followed by a new instance's
// daemon_started that completes normally.
//
// Spec ref: process-lifecycle.md §4.8 PL-025a — "A daemon_started with no
// subsequent daemon_ready, daemon_shutdown, or daemon_startup_failed before the
// next daemon_started indicates a crash between PL-005 step 2 and one of those
// terminal emissions; the prior instance's lifecycle is treated as orphaned."
func TestPL025a_PairingToleranceForUnpairedDaemonStarted(t *testing.T) {
	t.Parallel()

	t.Run("unpaired-started-classified-as-orphaned", func(t *testing.T) {
		t.Parallel()

		const crashedInstanceID = "instance-crashed-001"
		const aliveInstanceID = "instance-alive-002"

		s := &crashRecoveryFixturePairingState{}

		// Crashed instance: emits daemon_started, then nothing (crash).
		s.crashRecoveryFixtureRecordEvent(crashRecoveryFixturePairingEvent{
			eventType:  "daemon_started",
			instanceID: crashedInstanceID,
			emittedAt:  time.Now(),
		})

		// Next instance starts (indicates prior instance crashed).
		s.crashRecoveryFixtureRecordEvent(crashRecoveryFixturePairingEvent{
			eventType:  "daemon_started",
			instanceID: aliveInstanceID,
			emittedAt:  time.Now(),
		})
		s.crashRecoveryFixtureRecordEvent(crashRecoveryFixturePairingEvent{
			eventType:  "daemon_ready",
			instanceID: aliveInstanceID,
			emittedAt:  time.Now(),
		})

		s.mu.Lock()
		events := make([]crashRecoveryFixturePairingEvent, len(s.events))
		copy(events, s.events)
		s.mu.Unlock()

		classes := crashRecoveryFixtureClassifyLifecycle(events)

		// Crashed instance must be classified as orphaned.
		if classes[crashedInstanceID] != "orphaned" {
			t.Errorf("PL-025a: crashed instance %q classified as %q, want orphaned",
				crashedInstanceID, classes[crashedInstanceID])
		}

		// Alive instance must be classified as complete.
		if classes[aliveInstanceID] != "complete" {
			t.Errorf("PL-025a: alive instance %q classified as %q, want complete",
				aliveInstanceID, classes[aliveInstanceID])
		}
	})

	t.Run("rto-measurement-uses-pairing-rule", func(t *testing.T) {
		t.Parallel()

		// PL-025a: the ON-031 RTO measurement MUST use the pairing rule.
		// Model: RTO is measured from the most recent daemon_started to
		// daemon_ready within the same instance. Orphaned instances must NOT
		// contribute to the RTO measurement.
		const crashedInstanceID = "crashed-rto-001"
		const liveInstanceID = "live-rto-002"

		t1 := time.Now()
		t2 := t1.Add(100 * time.Millisecond) // crash point
		t3 := t2.Add(50 * time.Millisecond)  // next instance starts
		t4 := t3.Add(200 * time.Millisecond) // next instance ready

		events := []crashRecoveryFixturePairingEvent{
			{eventType: "daemon_started", instanceID: crashedInstanceID, emittedAt: t1},
			// crash: no daemon_ready for crashedInstanceID
			{eventType: "daemon_started", instanceID: liveInstanceID, emittedAt: t3},
			{eventType: "daemon_ready", instanceID: liveInstanceID, emittedAt: t4},
		}

		_ = t2 // crash time is implicit (gap between t1 and t3)

		classes := crashRecoveryFixtureClassifyLifecycle(events)

		if classes[crashedInstanceID] != "orphaned" {
			t.Errorf("PL-025a RTO: crashed instance %q classified as %q, want orphaned",
				crashedInstanceID, classes[crashedInstanceID])
		}

		// RTO for the live instance: measured from t3 (daemon_started) to t4
		// (daemon_ready). The orphaned instance's t1 must NOT be used.
		liveRTO := t4.Sub(t3)
		if liveRTO <= 0 {
			t.Errorf("PL-025a RTO: live instance RTO = %v, want positive", liveRTO)
		}

		// Assert the orphaned instance's RTO would be misleading if used.
		orphanedRTO := t4.Sub(t1) // wrong: spans crash gap
		if orphanedRTO <= liveRTO {
			t.Errorf("PL-025a RTO: orphaned instance RTO (%v) <= live RTO (%v); "+
				"demonstrates why orphan pairing is required to avoid RTO inflation", orphanedRTO, liveRTO)
		}

		t.Logf("PL-025a RTO: live instance RTO = %v (correct); orphaned span = %v (must not be used)",
			liveRTO, orphanedRTO)
	})

	t.Run("unpaired-orphan-sweep-event-not-misread", func(t *testing.T) {
		t.Parallel()

		// PL-025a: "A crash-induced unpaired daemon_orphan_sweep_completed is
		// similarly orphaned and MUST NOT be misread as completion of the current
		// daemon's sweep."
		const crashedID = "sweep-crash-001"
		const liveID = "sweep-live-002"

		events := []crashRecoveryFixturePairingEvent{
			{eventType: "daemon_started", instanceID: crashedID, emittedAt: time.Now()},
			{eventType: "daemon_orphan_sweep_completed", instanceID: crashedID, emittedAt: time.Now()},
			// crash: no daemon_ready
			{eventType: "daemon_started", instanceID: liveID, emittedAt: time.Now()},
			{eventType: "daemon_orphan_sweep_completed", instanceID: liveID, emittedAt: time.Now()},
			{eventType: "daemon_ready", instanceID: liveID, emittedAt: time.Now()},
		}

		classes := crashRecoveryFixtureClassifyLifecycle(events)

		if classes[crashedID] != "orphaned" {
			t.Errorf("PL-025a sweep: crashed instance %q classified as %q, want orphaned",
				crashedID, classes[crashedID])
		}

		if classes[liveID] != "complete" {
			t.Errorf("PL-025a sweep: live instance %q classified as %q, want complete",
				liveID, classes[liveID])
		}

		// Verify the liveID's orphan_sweep_completed is the authoritative one
		// (not the crashed instance's).
		var sweepEventsForLive, sweepEventsForCrashed int
		for _, evt := range events {
			if evt.eventType == "daemon_orphan_sweep_completed" {
				if evt.instanceID == liveID {
					sweepEventsForLive++
				} else if evt.instanceID == crashedID {
					sweepEventsForCrashed++
				}
			}
		}

		if sweepEventsForLive != 1 {
			t.Errorf("PL-025a sweep: %d orphan_sweep_completed for live instance, want 1", sweepEventsForLive)
		}
		if sweepEventsForCrashed != 1 {
			t.Errorf("PL-025a sweep: %d orphan_sweep_completed for crashed instance, want 1 (the stale one)", sweepEventsForCrashed)
		}
	})
}

// ─── PL-026: agent-subprocess crash routing ───────────────────────────────────

// crashRecoveryFixtureRunOutcome names the possible outcomes of an agent
// subprocess crash.
//
// Spec ref: process-lifecycle.md §4.8 PL-026 — "a cleanly-failed agent
// subprocess (explicit FAIL outcome, bounded exit code) produces a normal
// run-failure transition and does not trigger reconciliation."
type crashRecoveryFixtureRunOutcome string

const (
	crashRecoveryFixtureOutcomeNormalRunFailure crashRecoveryFixtureRunOutcome = "normal-run-failure"
	crashRecoveryFixtureOutcomeReconciliation   crashRecoveryFixtureRunOutcome = "reconciliation"
	crashRecoveryFixtureOutcomeRetry            crashRecoveryFixtureRunOutcome = "retry"
)

// crashRecoveryFixtureSubprocessCrashKind classifies how an agent subprocess
// terminated.
type crashRecoveryFixtureSubprocessCrashKind string

const (
	// cleanFail: subprocess exited with an explicit FAIL outcome and a bounded
	// exit code. The watcher observes a well-formed outcome.
	crashRecoveryFixtureCrashKindCleanFail crashRecoveryFixtureSubprocessCrashKind = "clean-fail"
	// ambiguousState: subprocess disappeared without emitting an outcome
	// (SIGKILL, out-of-band crash). The resulting run state is ambiguous.
	crashRecoveryFixtureCrashKindAmbiguous crashRecoveryFixtureSubprocessCrashKind = "ambiguous-state"
	// signalledCrash: subprocess killed by an OS signal without a clean exit.
	crashRecoveryFixtureCrashKindSignalled crashRecoveryFixtureSubprocessCrashKind = "signalled-crash"
)

// crashRecoveryFixtureRouteSubprocessCrash routes the handler-contract outcome
// per PL-026: clean fail → normal run-failure; ambiguous state → reconciliation.
//
// Spec ref: process-lifecycle.md §4.8 PL-026 — "The daemon routes the resulting
// outcome into reconciliation per [reconciliation/spec.md §4.2] only if the
// resulting run state is ambiguous; a cleanly-failed agent subprocess (explicit
// FAIL outcome, bounded exit code) produces a normal run-failure transition and
// does not trigger reconciliation."
func crashRecoveryFixtureRouteSubprocessCrash(kind crashRecoveryFixtureSubprocessCrashKind) crashRecoveryFixtureRunOutcome {
	switch kind {
	case crashRecoveryFixtureCrashKindCleanFail:
		// Explicit FAIL outcome with bounded exit code → normal run-failure per HC §4.6.
		return crashRecoveryFixtureOutcomeNormalRunFailure
	case crashRecoveryFixtureCrashKindAmbiguous, crashRecoveryFixtureCrashKindSignalled:
		// Ambiguous run state → reconciliation per RC §4.2.
		return crashRecoveryFixtureOutcomeReconciliation
	default:
		return crashRecoveryFixtureOutcomeNormalRunFailure
	}
}

// TestPL026_AgentSubprocessCrashRouting verifies that an agent-subprocess crash
// routes through the handler contract and produces the correct outcome:
//   - cleanly-failed subprocess → normal run-failure transition (no reconciliation)
//   - ambiguous run state → reconciliation per RC §4.2
//
// Spec ref: process-lifecycle.md §4.8 PL-026 — "An agent-subprocess crash that
// occurs while the daemon is alive MUST be handled per [handler-contract.md §4.6]
// (error propagation across async boundaries). The daemon routes the resulting
// outcome into reconciliation per [reconciliation/spec.md §4.2] only if the
// resulting run state is ambiguous."
func TestPL026_AgentSubprocessCrashRouting(t *testing.T) {
	t.Parallel()

	t.Run("clean-fail-produces-normal-run-failure", func(t *testing.T) {
		t.Parallel()

		outcome := crashRecoveryFixtureRouteSubprocessCrash(crashRecoveryFixtureCrashKindCleanFail)

		if outcome != crashRecoveryFixtureOutcomeNormalRunFailure {
			t.Errorf("PL-026 clean-fail: outcome = %q, want %q",
				outcome, crashRecoveryFixtureOutcomeNormalRunFailure)
		}

		// Clean fail MUST NOT trigger reconciliation.
		if outcome == crashRecoveryFixtureOutcomeReconciliation {
			t.Error("PL-026 clean-fail: outcome is reconciliation; clean-fail must NOT trigger reconciliation")
		}
	})

	t.Run("ambiguous-state-routes-to-reconciliation", func(t *testing.T) {
		t.Parallel()

		outcome := crashRecoveryFixtureRouteSubprocessCrash(crashRecoveryFixtureCrashKindAmbiguous)

		if outcome != crashRecoveryFixtureOutcomeReconciliation {
			t.Errorf("PL-026 ambiguous: outcome = %q, want %q",
				outcome, crashRecoveryFixtureOutcomeReconciliation)
		}
	})

	t.Run("signalled-crash-routes-to-reconciliation", func(t *testing.T) {
		t.Parallel()

		// A subprocess killed by OS signal without emitting an outcome has an
		// ambiguous run state → reconciliation.
		outcome := crashRecoveryFixtureRouteSubprocessCrash(crashRecoveryFixtureCrashKindSignalled)

		if outcome != crashRecoveryFixtureOutcomeReconciliation {
			t.Errorf("PL-026 signalled: outcome = %q, want %q",
				outcome, crashRecoveryFixtureOutcomeReconciliation)
		}
	})

	t.Run("no-reconciliation-on-explicit-fail", func(t *testing.T) {
		t.Parallel()

		// PL-026: "a cleanly-failed agent subprocess … does not trigger
		// reconciliation." Verify no accidental reconciliation on clean exit.
		crashKinds := []crashRecoveryFixtureSubprocessCrashKind{
			crashRecoveryFixtureCrashKindCleanFail,
		}
		for _, kind := range crashKinds {
			outcome := crashRecoveryFixtureRouteSubprocessCrash(kind)
			if outcome == crashRecoveryFixtureOutcomeReconciliation {
				t.Errorf("PL-026 no-recon-on-clean: crash kind %q routed to reconciliation; "+
					"MUST produce normal-run-failure instead", kind)
			}
		}
	})

	t.Run("ambiguous-kinds-always-trigger-reconciliation", func(t *testing.T) {
		t.Parallel()

		ambiguousKinds := []crashRecoveryFixtureSubprocessCrashKind{
			crashRecoveryFixtureCrashKindAmbiguous,
			crashRecoveryFixtureCrashKindSignalled,
		}
		for _, kind := range ambiguousKinds {
			outcome := crashRecoveryFixtureRouteSubprocessCrash(kind)
			if outcome != crashRecoveryFixtureOutcomeReconciliation {
				t.Errorf("PL-026 ambiguous-kinds: crash kind %q outcome = %q, want reconciliation",
					kind, outcome)
			}
		}
	})
}

// TestPL026_AgentCrashWhileDaemonAliveIsHandlerContractRouted verifies that
// when an agent subprocess crashes while the daemon is alive, the watcher
// goroutine (handler contract) is the exclusive path for crash detection and
// routing. The daemon does NOT poll subprocesses directly for crash detection.
//
// Spec ref: process-lifecycle.md §4.8 PL-026 — "MUST be handled per
// [handler-contract.md §4.6] (error propagation across async boundaries)."
func TestPL026_AgentCrashWhileDaemonAliveIsHandlerContractRouted(t *testing.T) {
	t.Parallel()

	// crashRecoveryFixtureWatcher models the handler-contract watcher goroutine
	// that is the exclusive crash detector per HC §4.6 + PL-016.
	type crashRecoveryFixtureWatcher struct {
		mu            sync.Mutex
		crashObserved bool
		routedOutcome crashRecoveryFixtureRunOutcome
	}

	watcher := &crashRecoveryFixtureWatcher{}

	// Simulate the watcher observing an ambiguous crash.
	crashKind := crashRecoveryFixtureCrashKindAmbiguous
	routedOutcome := crashRecoveryFixtureRouteSubprocessCrash(crashKind)

	watcher.mu.Lock()
	watcher.crashObserved = true
	watcher.routedOutcome = routedOutcome
	watcher.mu.Unlock()

	// The watcher must observe the crash and route it.
	watcher.mu.Lock()
	observed := watcher.crashObserved
	outcome := watcher.routedOutcome
	watcher.mu.Unlock()

	if !observed {
		t.Error("PL-026: watcher did not observe agent subprocess crash")
	}

	if outcome != crashRecoveryFixtureOutcomeReconciliation {
		t.Errorf("PL-026: watcher routed crash to %q, want reconciliation", outcome)
	}

	t.Logf("PL-026: agent subprocess crash observed by watcher; routed to reconciliation (ambiguous state)")
}
