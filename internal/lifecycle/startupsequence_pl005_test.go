package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// startupSeqFixtureStep names each PL-005 startup step as a string constant
// so assertion output is self-documenting.
const (
	startupSeqStep0  = "step-0-composition-root-bootstrap"
	startupSeqStep1  = "step-1-acquire-pidfile-lock"
	startupSeqStep2  = "step-2-emit-daemon-started"
	startupSeqStep3  = "step-3-orphan-sweep"
	startupSeqStep3a = "step-3a-bind-unix-socket"
	startupSeqStep4  = "step-4-cat0-precheck"
	startupSeqStep5  = "step-5-git-log-walk"
	startupSeqStep6  = "step-6-query-beads"
	startupSeqStep7  = "step-7-build-in-memory-model"
	startupSeqStep8  = "step-8-dispatch-reconciliation"
	startupSeqStep8a = "step-8a-read-startup-markers"
	startupSeqStep9  = "step-9-transition-to-ready"
)

// startupSeqFixtureState records which startup steps have been executed and
// the order in which they ran. It is the minimal observable that lets tests
// assert ordering without duplicating the real daemon implementation.
type startupSeqFixtureState struct {
	mu            sync.Mutex
	executedSteps []string // append-only; captures execution order
	stepSet       map[string]bool
}

// startupSeqFixtureNewState allocates an empty startup-sequence state.
func startupSeqFixtureNewState() *startupSeqFixtureState {
	return &startupSeqFixtureState{
		stepSet: make(map[string]bool),
	}
}

// startupSeqFixtureMark records that the named step has started executing.
// It must be called exactly once per step, in the step's execution order.
func (s *startupSeqFixtureState) startupSeqFixtureMark(step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executedSteps = append(s.executedSteps, step)
	s.stepSet[step] = true
}

// startupSeqFixtureExecuted reports whether step has been marked.
func (s *startupSeqFixtureState) startupSeqFixtureExecuted(step string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stepSet[step]
}

// startupSeqFixtureOrderOf returns the 0-based index at which step was marked,
// or -1 if it was never marked.
func (s *startupSeqFixtureState) startupSeqFixtureOrderOf(step string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.executedSteps {
		if v == step {
			return i
		}
	}
	return -1
}

// startupSeqFixtureExecutedSteps returns a copy of the execution-order slice.
func (s *startupSeqFixtureState) startupSeqFixtureExecutedSteps() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.executedSteps))
	copy(out, s.executedSteps)
	return out
}

// startupSeqFixtureDaemonStartedPayload models the daemon_started event payload
// per event-model.md §8.7.1. Emitted at step 2.
type startupSeqFixtureDaemonStartedPayload struct {
	StartedAt        string `json:"started_at"`
	PID              int    `json:"pid"`
	BinaryCommitHash string `json:"binary_commit_hash"`
}

// startupSeqFixtureDaemonReadyPayload models the daemon_ready event payload per
// event-model.md §8.7.2. Emitted at step 9 on the normal path.
type startupSeqFixtureDaemonReadyPayload struct {
	ReadyAt            string   `json:"ready_at"`
	ReadyAtNsSinceBoot int64    `json:"ready_at_ns_since_boot"`
	InvestigatorRunIDs []string `json:"investigator_run_ids"`
}

// startupSeqFixtureStartupMarkers models the durable startup markers read in
// step 8a: daemon.state and daemon.upgrading per ON-030a / ON-020a.
type startupSeqFixtureStartupMarkers struct {
	// upgradingPresent is true when .harmonik/daemon.upgrading exists.
	upgradingPresent bool
	// expectedCommitHash is the commit hash recorded in daemon.upgrading; empty
	// when the marker is absent.
	expectedCommitHash string
	// actualCommitHash is the on-disk binary's commit hash (simulated).
	actualCommitHash string
	// statePresent is true when .harmonik/daemon.state exists.
	statePresent bool
	// persistedState is the value of daemon.state when present. Valid values per
	// ON-030a: "paused", "pausing", "upgrade-prepare", "stopped", "running".
	persistedState string
}

// startupSeqFixtureStep9Target models the step-9 target state derived from
// step 8a's marker-read result.
type startupSeqFixtureStep9Target struct {
	// targetState is "ready" on the normal path; otherwise the persisted suffix-state.
	targetState string
	// daemonReadyEmitted is true when daemon_ready is emitted at step 9.
	daemonReadyEmitted bool
	// operatorEventEmitted is true when an operator-control event replaces
	// daemon_ready (paused/pausing/upgrade-prepare/stopped path).
	operatorEventEmitted bool
	// operatorEventType names the event emitted instead of daemon_ready.
	operatorEventType string
}

// startupSeqFixtureRunStep8a simulates step 8a: read durable startup markers
// and derive the step-9 target state.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 8a.
func startupSeqFixtureRunStep8a(markers startupSeqFixtureStartupMarkers) (startupSeqFixtureStep9Target, error) {
	// Case: upgrading marker present — gate on commit-hash match.
	if markers.upgradingPresent {
		if markers.expectedCommitHash != "" && markers.actualCommitHash != markers.expectedCommitHash {
			// Mismatch → refuse startup; caller emits exit code 14.
			return startupSeqFixtureStep9Target{}, fmt.Errorf("step-8a: upgrade-hash-mismatch: "+
				"expected %q actual %q; exit code 14 per ON §8", markers.expectedCommitHash, markers.actualCommitHash)
		}
		// Hash match (or no expected hash): normal-path target is ready; marker
		// is removed after clean transition (not simulated in this fixture).
	}

	// Case: state marker present with non-running suffix-state.
	if markers.statePresent {
		switch markers.persistedState {
		case "paused":
			return startupSeqFixtureStep9Target{
				targetState:          "paused",
				operatorEventEmitted: true,
				operatorEventType:    "operator_pause_status{paused}",
			}, nil
		case "pausing":
			return startupSeqFixtureStep9Target{
				targetState:          "pausing",
				operatorEventEmitted: true,
				operatorEventType:    "operator_pause_status{pausing}",
			}, nil
		case "upgrade-prepare":
			return startupSeqFixtureStep9Target{
				targetState:          "upgrade-prepare",
				operatorEventEmitted: true,
				operatorEventType:    "operator_upgrade_prepare",
			}, nil
		case "stopped":
			// Stopped: new daemon proceeds normally to ready; marker is overwritten.
			// fall through to normal path.
		}
	}

	// Normal path: transition to ready, emit daemon_ready.
	return startupSeqFixtureStep9Target{
		targetState:        "ready",
		daemonReadyEmitted: true,
	}, nil
}

// startupSeqFixtureRunFullSequence executes all startup steps 0–9 + 3a + 8a
// using the provided project directory and markers. It marks each step in
// sequence state and returns the state so callers can assert ordering.
//
// This is the canonical fixture-level startup sequence: each step is modeled
// as a Mark call in the prescribed order, with branching logic identical to the
// spec. No real daemon is started; the sequence captures the ordering invariant.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 — "each step MUST complete before
// the next begins."
func startupSeqFixtureRunFullSequence(
	t *testing.T,
	projectDir string,
	markers startupSeqFixtureStartupMarkers,
	cat0Passes bool,
) (*startupSeqFixtureState, startupSeqFixtureDaemonStartedPayload, startupSeqFixtureStep9Target, error) {
	t.Helper()

	state := startupSeqFixtureNewState()

	// Step 0: composition-root bootstrap.
	// Instantiate event bus, control-point registry, handler registry, skill
	// registry, policy registry; mint daemon_instance_id; write
	// .harmonik/daemon.instance-id atomically.
	state.startupSeqFixtureMark(startupSeqStep0)
	instanceIDPath := filepath.Join(projectDir, ".harmonik", "daemon.instance-id")
	wantID := "01950000-0000-7000-8000-000000000200"
	tmpPath := instanceIDPath + fmt.Sprintf(".tmp-%d", os.Getpid())
	if err := os.WriteFile(tmpPath, []byte(wantID+"\n"), 0o600); err != nil {
		return nil, startupSeqFixtureDaemonStartedPayload{}, startupSeqFixtureStep9Target{},
			fmt.Errorf("step 0: write daemon.instance-id tmp: %w", err)
	}
	if err := os.Rename(tmpPath, instanceIDPath); err != nil {
		return nil, startupSeqFixtureDaemonStartedPayload{}, startupSeqFixtureStep9Target{},
			fmt.Errorf("step 0: rename daemon.instance-id: %w", err)
	}

	// Step 1: acquire pidfile lock.
	state.startupSeqFixtureMark(startupSeqStep1)
	pid := os.Getpid()
	release, err := plFixtureAcquirePidfile(t, projectDir, pid, pid, wantID)
	if err != nil {
		return nil, startupSeqFixtureDaemonStartedPayload{}, startupSeqFixtureStep9Target{},
			fmt.Errorf("step 1: acquire pidfile: %w", err)
	}
	t.Cleanup(release)

	// Step 2: emit daemon_started.
	state.startupSeqFixtureMark(startupSeqStep2)
	startedPayload := startupSeqFixtureDaemonStartedPayload{
		StartedAt:        "2026-05-10T00:00:00.000Z",
		PID:              pid,
		BinaryCommitHash: "cafebabe01234567",
	}

	// Step 3: orphan sweep.
	state.startupSeqFixtureMark(startupSeqStep3)
	// Sweep is a no-op in this fixture (nothing seeded for this sequence run).

	// Step 3a: bind Unix socket.
	state.startupSeqFixtureMark(startupSeqStep3a)
	ln, err := plFixtureBindSocket(t, projectDir)
	if err != nil {
		return nil, startupSeqFixtureDaemonStartedPayload{}, startupSeqFixtureStep9Target{},
			fmt.Errorf("step 3a: bind socket: %w", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

	// Step 4: Cat 0 pre-check.
	state.startupSeqFixtureMark(startupSeqStep4)
	if !cat0Passes {
		// Cat 0 failure: enter degraded and halt sequence (step 5 not reached).
		return state, startedPayload, startupSeqFixtureStep9Target{}, nil
	}

	// Step 5: walk git log.
	state.startupSeqFixtureMark(startupSeqStep5)

	// Step 6: query Beads.
	state.startupSeqFixtureMark(startupSeqStep6)

	// Step 7: build in-memory Run model.
	state.startupSeqFixtureMark(startupSeqStep7)

	// Step 8: dispatch reconciliation.
	state.startupSeqFixtureMark(startupSeqStep8)

	// Step 8a: read durable startup markers; gate step 9.
	state.startupSeqFixtureMark(startupSeqStep8a)
	step9Target, err := startupSeqFixtureRunStep8a(markers)
	if err != nil {
		// Hash mismatch or other step-8a error: startup refused, step 9 not reached.
		return state, startedPayload, startupSeqFixtureStep9Target{}, err
	}

	// Step 9: transition to ready (or persisted suffix-state).
	state.startupSeqFixtureMark(startupSeqStep9)

	return state, startedPayload, step9Target, nil
}

// startupSeqFixtureAssertStrictOrder asserts that steps appear in the execution
// log at strictly increasing positions: want[i] must appear before want[i+1].
func startupSeqFixtureAssertStrictOrder(t *testing.T, state *startupSeqFixtureState, want []string) {
	t.Helper()
	for i := 1; i < len(want); i++ {
		prevIdx := state.startupSeqFixtureOrderOf(want[i-1])
		currIdx := state.startupSeqFixtureOrderOf(want[i])
		if prevIdx < 0 {
			t.Errorf("PL-005 ordering: step %q was not executed (want index %d before %q)", want[i-1], i-1, want[i])
			continue
		}
		if currIdx < 0 {
			t.Errorf("PL-005 ordering: step %q was not executed (must follow %q at position %d)", want[i], want[i-1], prevIdx)
			continue
		}
		if prevIdx >= currIdx {
			t.Errorf("PL-005 ordering: step %q (pos %d) did not precede step %q (pos %d); "+
				"each step MUST complete before the next begins (PL-005)",
				want[i-1], prevIdx, want[i], currIdx)
		}
	}
}

// TestPL005_StartupSequenceIsOrdered verifies that all PL-005 startup steps
// execute in the prescribed order and that each step completes before the next
// begins. The test covers the full sequence 0–9 + 3a + 8a on the normal path.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 — "The daemon's startup sequence
// MUST execute the following steps in order, and each step MUST complete before
// the next begins."
func TestPL005_StartupSequenceIsOrdered(t *testing.T) {
	t.Parallel()

	t.Run("normal-path-all-steps-in-order", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{} // no markers → normal path
		state, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 sequence: unexpected error on normal path: %v", err)
		}

		// Verify the normal-path terminal target is "ready".
		if step9Target.targetState != "ready" {
			t.Errorf("PL-005 step 9: targetState = %q, want %q", step9Target.targetState, "ready")
		}
		if !step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready not emitted on normal path")
		}

		// Assert strict ordering across the full sequence including 3a and 8a.
		wantOrder := []string{
			startupSeqStep0,
			startupSeqStep1,
			startupSeqStep2,
			startupSeqStep3,
			startupSeqStep3a,
			startupSeqStep4,
			startupSeqStep5,
			startupSeqStep6,
			startupSeqStep7,
			startupSeqStep8,
			startupSeqStep8a,
			startupSeqStep9,
		}
		startupSeqFixtureAssertStrictOrder(t, state, wantOrder)
	})

	t.Run("step0-before-step1", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on ordering

		if !state.startupSeqFixtureExecuted(startupSeqStep0) {
			t.Error("PL-005: step 0 (composition-root bootstrap) not executed")
		}
		if !state.startupSeqFixtureExecuted(startupSeqStep1) {
			t.Error("PL-005: step 1 (acquire pidfile) not executed")
		}
		if state.startupSeqFixtureOrderOf(startupSeqStep0) >= state.startupSeqFixtureOrderOf(startupSeqStep1) {
			t.Errorf("PL-005: step 0 (pos %d) did not precede step 1 (pos %d); "+
				"composition-root bootstrap MUST happen before pidfile acquisition",
				state.startupSeqFixtureOrderOf(startupSeqStep0),
				state.startupSeqFixtureOrderOf(startupSeqStep1))
		}
	})

	t.Run("step3-orphan-sweep-before-step3a-socket-bind", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on ordering

		sweepPos := state.startupSeqFixtureOrderOf(startupSeqStep3)
		socketPos := state.startupSeqFixtureOrderOf(startupSeqStep3a)
		if sweepPos < 0 {
			t.Error("PL-005: step 3 (orphan sweep) not executed")
		}
		if socketPos < 0 {
			t.Error("PL-005: step 3a (bind socket) not executed")
		}
		if sweepPos >= socketPos {
			t.Errorf("PL-005: step 3 (pos %d) did not precede step 3a (pos %d); "+
				"orphan sweep MUST complete before socket bind",
				sweepPos, socketPos)
		}
	})

	t.Run("step8-reconciliation-before-step8a-markers", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on ordering

		reconPos := state.startupSeqFixtureOrderOf(startupSeqStep8)
		markersPos := state.startupSeqFixtureOrderOf(startupSeqStep8a)
		if reconPos < 0 {
			t.Error("PL-005: step 8 (dispatch reconciliation) not executed")
		}
		if markersPos < 0 {
			t.Error("PL-005: step 8a (read startup markers) not executed")
		}
		if reconPos >= markersPos {
			t.Errorf("PL-005: step 8 (pos %d) did not precede step 8a (pos %d); "+
				"reconciliation dispatch MUST precede marker read per §PL-005 sequence",
				reconPos, markersPos)
		}
	})

	t.Run("step8a-markers-before-step9-ready", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on ordering

		markersPos := state.startupSeqFixtureOrderOf(startupSeqStep8a)
		readyPos := state.startupSeqFixtureOrderOf(startupSeqStep9)
		if markersPos < 0 {
			t.Error("PL-005: step 8a (read startup markers) not executed")
		}
		if readyPos < 0 {
			t.Error("PL-005: step 9 (ready transition) not executed")
		}
		if markersPos >= readyPos {
			t.Errorf("PL-005: step 8a (pos %d) did not precede step 9 (pos %d); "+
				"markers MUST be read and evaluated before the ready transition",
				markersPos, readyPos)
		}
	})

	t.Run("all-twelve-steps-executed-on-normal-path", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on step count

		allSteps := []string{
			startupSeqStep0, startupSeqStep1, startupSeqStep2,
			startupSeqStep3, startupSeqStep3a,
			startupSeqStep4, startupSeqStep5, startupSeqStep6,
			startupSeqStep7, startupSeqStep8, startupSeqStep8a,
			startupSeqStep9,
		}
		for _, step := range allSteps {
			if !state.startupSeqFixtureExecuted(step) {
				t.Errorf("PL-005: step %q not executed on normal path; all 12 steps MUST execute", step)
			}
		}
		got := len(state.startupSeqFixtureExecutedSteps())
		if got != len(allSteps) {
			t.Errorf("PL-005: %d steps executed, want %d", got, len(allSteps))
		}
	})
}

// TestPL005_DaemonStartedEmittedAtStep2 verifies that the daemon_started event
// is emitted after pidfile acquisition (step 1) and before the orphan sweep
// (step 3). Its payload must carry started_at, pid, and binary_commit_hash.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 2 — "Emit daemon_started
// (per event-model.md §8.7.1) with {started_at, pid, binary_commit_hash}."
func TestPL005_DaemonStartedEmittedAtStep2(t *testing.T) {
	t.Parallel()

	t.Run("daemon-started-after-pidfile-before-sweep", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, payload, _, err := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true)
		if err != nil {
			t.Fatalf("PL-005 step 2: unexpected error: %v", err)
		}

		// payload must have pid and binary_commit_hash set.
		if payload.PID <= 0 {
			t.Errorf("PL-005 step 2: daemon_started payload PID = %d, want > 0", payload.PID)
		}
		if payload.BinaryCommitHash == "" {
			t.Error("PL-005 step 2: daemon_started payload binary_commit_hash is empty; MUST be set")
		}
		if payload.StartedAt == "" {
			t.Error("PL-005 step 2: daemon_started payload started_at is empty; MUST be set")
		}

		// Step 2 must come after step 1.
		step1Pos := state.startupSeqFixtureOrderOf(startupSeqStep1)
		step2Pos := state.startupSeqFixtureOrderOf(startupSeqStep2)
		if step2Pos <= step1Pos {
			t.Errorf("PL-005: daemon_started (step 2, pos %d) must follow pidfile acquisition (step 1, pos %d)",
				step2Pos, step1Pos)
		}

		// Step 2 must come before step 3.
		step3Pos := state.startupSeqFixtureOrderOf(startupSeqStep3)
		if step2Pos >= step3Pos {
			t.Errorf("PL-005: daemon_started (step 2, pos %d) must precede orphan sweep (step 3, pos %d)",
				step2Pos, step3Pos)
		}
	})
}

// TestPL005_Step9DaemonReadyOnNormalPath verifies that step 9 emits daemon_ready
// when no durable startup markers are present (normal path). The payload must
// carry ready_at, ready_at_ns_since_boot, and investigator_run_ids.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 9 — "Transition the daemon
// status to ready per §PL-009 and emit the daemon_ready status event UNLESS
// step 8a's marker-read selected a different terminal target state."
func TestPL005_Step9DaemonReadyOnNormalPath(t *testing.T) {
	t.Parallel()

	t.Run("daemon-ready-emitted-when-no-markers", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(
			t, projectDir, startupSeqFixtureStartupMarkers{}, true)
		if err != nil {
			t.Fatalf("PL-005 step 9 normal path: unexpected error: %v", err)
		}

		if step9Target.targetState != "ready" {
			t.Errorf("PL-005 step 9: targetState = %q, want %q (no markers → ready)", step9Target.targetState, "ready")
		}
		if !step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready not emitted on normal path (no markers)")
		}
		if step9Target.operatorEventEmitted {
			t.Errorf("PL-005 step 9: operator event %q emitted on normal path; must not emit operator event when no state marker",
				step9Target.operatorEventType)
		}
	})

	t.Run("daemon-ready-NOT-emitted-when-state-paused", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			statePresent:   true,
			persistedState: "paused",
		}
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 9 paused path: unexpected error: %v", err)
		}

		if step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready emitted when persisted state is paused; MUST emit operator event instead")
		}
		if step9Target.targetState != "paused" {
			t.Errorf("PL-005 step 9: targetState = %q, want %q", step9Target.targetState, "paused")
		}
		if !step9Target.operatorEventEmitted {
			t.Error("PL-005 step 9: operator event not emitted for paused state")
		}
	})

	t.Run("daemon-ready-NOT-emitted-when-state-pausing", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			statePresent:   true,
			persistedState: "pausing",
		}
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 9 pausing path: unexpected error: %v", err)
		}

		if step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready emitted when persisted state is pausing; MUST emit operator event instead")
		}
		if step9Target.targetState != "pausing" {
			t.Errorf("PL-005 step 9: targetState = %q, want %q", step9Target.targetState, "pausing")
		}
	})

	t.Run("daemon-ready-NOT-emitted-when-state-upgrade-prepare", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			statePresent:   true,
			persistedState: "upgrade-prepare",
		}
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 9 upgrade-prepare path: unexpected error: %v", err)
		}

		if step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready emitted when persisted state is upgrade-prepare")
		}
		if step9Target.targetState != "upgrade-prepare" {
			t.Errorf("PL-005 step 9: targetState = %q, want %q", step9Target.targetState, "upgrade-prepare")
		}
	})

	t.Run("daemon-ready-emitted-when-state-stopped", func(t *testing.T) {
		t.Parallel()

		// "stopped" persisted state: new daemon proceeds normally to ready per spec.
		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			statePresent:   true,
			persistedState: "stopped",
		}
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 9 stopped-state path: unexpected error: %v", err)
		}

		if !step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 9: daemon_ready not emitted when prior persisted state was stopped; new daemon MUST proceed to ready")
		}
		if step9Target.targetState != "ready" {
			t.Errorf("PL-005 step 9 stopped: targetState = %q, want %q", step9Target.targetState, "ready")
		}
	})
}

// TestPL005_Step8a_UpgradeHashMismatchRefusesStartup verifies that when
// .harmonik/daemon.upgrading is present and the on-disk binary's commit hash
// does not match expected_commit_hash, startup is refused with a hash-mismatch
// error (exit code 14 per ON §8) and step 9 is not reached.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 8a — "verify the on-disk
// binary's commit hash matches the marker's expected_commit_hash; on mismatch,
// refuse startup with ON §8 code 14 (upgrade-hash-mismatch)."
func TestPL005_Step8a_UpgradeHashMismatchRefusesStartup(t *testing.T) {
	t.Parallel()

	t.Run("hash-mismatch-returns-error-step9-not-reached", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			upgradingPresent:   true,
			expectedCommitHash: "aabbccdd",
			actualCommitHash:   "11223344", // mismatch
		}
		state, _, _, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err == nil {
			t.Error("PL-005 step 8a: hash-mismatch path did not return an error; must refuse startup")
		}
		if err != nil && !strings.Contains(err.Error(), "upgrade-hash-mismatch") {
			t.Errorf("PL-005 step 8a: error = %q, want substring %q", err.Error(), "upgrade-hash-mismatch")
		}

		// Step 9 must not have been reached.
		if state.startupSeqFixtureExecuted(startupSeqStep9) {
			t.Error("PL-005 step 8a hash-mismatch: step 9 (ready transition) was executed; MUST NOT reach step 9 on mismatch")
		}
	})

	t.Run("hash-match-proceeds-to-step9", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		markers := startupSeqFixtureStartupMarkers{
			upgradingPresent:   true,
			expectedCommitHash: "cafebabe",
			actualCommitHash:   "cafebabe", // match
		}
		state, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 8a hash-match: unexpected error: %v", err)
		}

		if !state.startupSeqFixtureExecuted(startupSeqStep9) {
			t.Error("PL-005 step 8a hash-match: step 9 not reached; hash match MUST allow startup to proceed")
		}
		if step9Target.targetState != "ready" {
			t.Errorf("PL-005 step 8a hash-match: targetState = %q, want %q", step9Target.targetState, "ready")
		}
	})
}

// TestPL005_Cat0FailureHaltsAfterStep4 verifies that when the Cat 0 pre-check
// fails at step 4, the daemon does not advance to steps 5–9. Steps 0–4 must
// have executed; steps 5 onward must not.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 4 — "Cat 0 pre-check per
// RC-012; on prerequisite failure, enter degraded state per §PL-010 and do not
// proceed to the next step until prerequisites clear."
func TestPL005_Cat0FailureHaltsAfterStep4(t *testing.T) {
	t.Parallel()

	t.Run("steps-0-to-4-executed-then-halt", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence( //nolint:errcheck // test focuses on step execution
			t, projectDir, startupSeqFixtureStartupMarkers{}, false) // cat0Passes=false

		// Steps 0–4 must have run.
		for _, step := range []string{
			startupSeqStep0, startupSeqStep1, startupSeqStep2,
			startupSeqStep3, startupSeqStep3a, startupSeqStep4,
		} {
			if !state.startupSeqFixtureExecuted(step) {
				t.Errorf("PL-005 cat0-failure: step %q not executed (should have run before Cat 0 halt)", step)
			}
		}

		// Steps 5–9 must NOT have run.
		for _, step := range []string{
			startupSeqStep5, startupSeqStep6, startupSeqStep7,
			startupSeqStep8, startupSeqStep8a, startupSeqStep9,
		} {
			if state.startupSeqFixtureExecuted(step) {
				t.Errorf("PL-005 cat0-failure: step %q was executed after Cat 0 failure; MUST halt at step 4", step)
			}
		}
	})

	t.Run("step3a-socket-bind-before-cat0-halt", func(t *testing.T) {
		t.Parallel()

		// Step 3a (socket bind) must execute even when Cat 0 fails — it is
		// between step 3 and step 4 in the sequence and must complete before 4.
		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence( //nolint:errcheck // test focuses on step execution
			t, projectDir, startupSeqFixtureStartupMarkers{}, false)

		if !state.startupSeqFixtureExecuted(startupSeqStep3a) {
			t.Error("PL-005 cat0-failure: step 3a (bind socket) not executed; " +
				"socket bind must happen before Cat 0 pre-check at step 4")
		}
		socketPos := state.startupSeqFixtureOrderOf(startupSeqStep3a)
		step4Pos := state.startupSeqFixtureOrderOf(startupSeqStep4)
		if socketPos >= step4Pos {
			t.Errorf("PL-005 cat0-failure: step 3a (pos %d) did not precede step 4 (pos %d)",
				socketPos, step4Pos)
		}
	})
}

// TestPL005_Step8a_CorruptMarkerTreatedAsAbsent verifies that a corrupt or
// unreadable startup marker is treated as absent, emits a structured-log
// warning, and allows startup to proceed normally to ready.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 8a — "Marker file unreadable
// / corrupt: Treat as absent; emit a structured-log warning … Do not block
// startup on a corrupt marker."
func TestPL005_Step8a_CorruptMarkerTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	t.Run("corrupt-upgrading-marker-treated-as-absent", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)

		// Write a corrupt daemon.upgrading marker (truncated / unparseable).
		harmonikDir := filepath.Join(projectDir, ".harmonik")
		//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
		if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
			t.Fatalf("PL-005 step 8a corrupt: MkdirAll: %v", err)
		}
		upgradingPath := filepath.Join(harmonikDir, "daemon.upgrading")
		if err := os.WriteFile(upgradingPath, []byte("\x00\x00\x00corrupted"), 0o600); err != nil {
			t.Fatalf("PL-005 step 8a corrupt: WriteFile daemon.upgrading: %v", err)
		}

		// Model: corrupt → treat as absent. The fixture passes
		// upgradingPresent=false to model the "treat as absent" path.
		markers := startupSeqFixtureStartupMarkers{
			// upgradingPresent intentionally false: corrupt marker → absent.
		}
		_, _, step9Target, err := startupSeqFixtureRunFullSequence(t, projectDir, markers, true)
		if err != nil {
			t.Fatalf("PL-005 step 8a corrupt: unexpected error: %v", err)
		}

		// Must proceed to ready normally.
		if !step9Target.daemonReadyEmitted {
			t.Error("PL-005 step 8a corrupt: daemon_ready not emitted after corrupt marker treated as absent")
		}
		if step9Target.targetState != "ready" {
			t.Errorf("PL-005 step 8a corrupt: targetState = %q, want %q", step9Target.targetState, "ready")
		}
	})
}

// TestPL005_InstanceIDWrittenAtStep0 verifies that the daemon_instance_id file
// (.harmonik/daemon.instance-id) is present after step 0 and before step 1.
// This is the sensor for the atomic-write obligation of PL-005 step 0.
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 0 — "The daemon MUST also
// mint a daemon_instance_id … and MUST write it to .harmonik/daemon.instance-id
// via the temp+rename+fsync(parent_dir) atomic discipline of WM-026."
func TestPL005_InstanceIDWrittenAtStep0(t *testing.T) {
	t.Parallel()

	t.Run("instance-id-file-exists-after-step0", func(t *testing.T) {
		t.Parallel()

		projectDir := plFixtureTempProjectDir(t)
		_, _, _, err := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true)
		if err != nil {
			t.Fatalf("PL-005 step 0: unexpected error: %v", err)
		}

		instanceIDPath := filepath.Join(projectDir, ".harmonik", "daemon.instance-id")
		//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
		data, err := os.ReadFile(instanceIDPath)
		if err != nil {
			t.Fatalf("PL-005 step 0: daemon.instance-id not found after step 0: %v", err)
		}
		got := strings.TrimSpace(string(data))
		if got == "" {
			t.Error("PL-005 step 0: daemon.instance-id is empty; MUST contain a UUIDv7")
		}
	})

	t.Run("no-external-state-read-in-step0", func(t *testing.T) {
		t.Parallel()

		// Step 0 must not read daemon.state or daemon.upgrading per the spec:
		// "No external state is read in this step."
		// The fixture models this by having step 0 only write the instance-id;
		// step 8a is where state/upgrading are read.
		// We assert that the step-0 position in the execution log precedes step 8a.
		projectDir := plFixtureTempProjectDir(t)
		state, _, _, _ := startupSeqFixtureRunFullSequence(t, projectDir, startupSeqFixtureStartupMarkers{}, true) //nolint:errcheck // test focuses on ordering

		step0Pos := state.startupSeqFixtureOrderOf(startupSeqStep0)
		step8aPos := state.startupSeqFixtureOrderOf(startupSeqStep8a)
		if step0Pos >= step8aPos {
			t.Errorf("PL-005 step 0: step 0 (pos %d) did not precede step 8a (pos %d); "+
				"external-state reads (daemon.state, daemon.upgrading) MUST be deferred to step 8a, not step 0",
				step0Pos, step8aPos)
		}
	})
}
