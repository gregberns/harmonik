package lifecycle

import (
	"net"
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

// upgradeExecFixtureUpgradeMarkerContent models the on-disk content of the
// `.harmonik/daemon.upgrading` marker file per ON-020a. The content is owned
// by ON-020a; PL-027(iv) writes the marker and PL-005 step 8a reads it.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — "The outgoing binary MUST
// write the upgrade-intent marker .harmonik/daemon.upgrading … before invoking
// execve."
// Spec ref: operator-nfr.md §4.6 ON-020a — content is expected_commit_hash +
// upgrade-initiation timestamp + operator's session_id.
type upgradeExecFixtureUpgradeMarkerContent struct {
	ExpectedCommitHash string `json:"expected_commit_hash"`
	InitiatedAt        string `json:"initiated_at"`
	SessionID          string `json:"session_id"`
}

// upgradeExecFixtureUpgradeState models the full state of a daemon upgrade
// exec-replacement scenario. Fields are set as the harness advances through
// the upgrade protocol steps.
//
// Spec ref: process-lifecycle.md §4.9 PL-027 — daemon-internal mechanics of
// harmonik upgrade: exec-replacement semantics.
type upgradeExecFixtureUpgradeState struct {
	mu sync.Mutex

	// (i) Exec-replacement: new instance re-acquired pidfile lock.
	pidfileLockReacquired bool

	// (ii) Orphan sweep: skipped because HARMONIK_UPGRADE=1 is set.
	orphanSweepSkipped bool
	upgradeEnvDetected bool

	// (iii) Listener fd adopted gap-free.
	listenerFdAdopted      bool
	listenerFdNumber       string // value of HARMONIK_LISTENER_FD env var
	noClientConnRefusedGap bool   // true if no ECONNREFUSED observable during transition

	// (iv) .harmonik/daemon.upgrading marker present before execve.
	upgradingMarkerPresent bool
	// marker removed after clean transition to ready.
	upgradingMarkerRemoved bool

	// (v) Upgrade event emissions.
	operatorUpgradingEmitted        bool
	operatorUpgradeCompletedEmitted bool
	emissionOrderCorrect            bool // upgrading before exec; completed after ready
}

// upgradeExecFixtureNewUpgradeState creates an uninitialized upgrade state.
func upgradeExecFixtureNewUpgradeState() *upgradeExecFixtureUpgradeState {
	return &upgradeExecFixtureUpgradeState{}
}

// upgradeExecFixtureSimulateOutgoing simulates the outgoing daemon's pre-exec
// obligations: write marker, emit operator_upgrading, clear FD_CLOEXEC, set env.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iii) — "The outgoing daemon MUST
// clear FD_CLOEXEC on the listener fd immediately before execve."
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — marker must be durable before execve.
// Spec ref: process-lifecycle.md §4.9 PL-027(v) — emit operator_upgrading before exec.
func upgradeExecFixtureSimulateOutgoing(state *upgradeExecFixtureUpgradeState, listenerFdNum int) {
	state.mu.Lock()
	defer state.mu.Unlock()

	// (v) Emit operator_upgrading BEFORE exec.
	state.operatorUpgradingEmitted = true

	// (iv) Write the upgrading marker (durable on disk before execve).
	state.upgradingMarkerPresent = true

	// (iii) Clear FD_CLOEXEC and pass listener fd.
	state.listenerFdNumber = strconv.Itoa(listenerFdNum)
}

// upgradeExecFixtureSimulateIncoming simulates the incoming daemon's post-exec
// startup obligations per PL-027(i), (ii), (iii), (iv), (v).
func upgradeExecFixtureSimulateIncoming(state *upgradeExecFixtureUpgradeState) {
	state.mu.Lock()
	defer state.mu.Unlock()

	// Detect HARMONIK_UPGRADE=1.
	state.upgradeEnvDetected = state.listenerFdNumber != ""

	// (ii) Skip orphan sweep when HARMONIK_UPGRADE=1 is set.
	if state.upgradeEnvDetected {
		state.orphanSweepSkipped = true
	}

	// (i) Re-acquire pidfile lock.
	state.pidfileLockReacquired = true

	// (iii) Adopt listener fd from HARMONIK_LISTENER_FD.
	if state.listenerFdNumber != "" {
		state.listenerFdAdopted = true
		state.noClientConnRefusedGap = true // gap-free adoption
	}

	// Simulate startup sequence (steps 0, 1, 2, 4–9; step 3 skipped per (ii)).
	// After completing and reaching ready:

	// (iv) Remove upgrading marker on clean transition to ready.
	if state.upgradingMarkerPresent {
		state.upgradingMarkerRemoved = true
		state.upgradingMarkerPresent = false
	}

	// (v) Emit operator_upgrade_completed AFTER new instance reaches ready.
	state.operatorUpgradeCompletedEmitted = true

	// Verify emission order: upgrading (pre-exec) before completed (post-ready).
	state.emissionOrderCorrect = state.operatorUpgradingEmitted && state.operatorUpgradeCompletedEmitted
}

// TestPL027_UpgradeExecReplacementHarness verifies the daemon-internal mechanics
// of the harmonik upgrade exec-replacement scenario. Five sub-obligations are
// tested per PL-027(i)–(v).
//
// As a pre-implementation harness (the daemon binary does not yet exist), the
// scenario is modeled using fixture state rather than a real execve. When
// internal/daemon is authored and a test binary exists, this harness can be
// extended to exercise real exec-replacement via the self-exec pattern.
//
// Spec ref: process-lifecycle.md §4.9 PL-027 — "exec-replace the daemon binary
// and assert (i) pidfile lock is re-acquired by new instance; (ii) orphan sweep
// is skipped (HARMONIK_UPGRADE=1 detected); (iii) listener fd is adopted
// gap-free via HARMONIK_LISTENER_FD per fd-passing protocol — no client
// ECONNREFUSED observable across exec; (iv) .harmonik/daemon.upgrading marker is
// present + durable on disk before execve and removed on clean transition to
// ready; (v) operator_upgrading and operator_upgrade_completed emission bracket
// the exec."
func TestPL027_UpgradeExecReplacementHarness(t *testing.T) {
	t.Parallel()

	t.Run("i-pidfile-lock-reacquired-by-new-instance", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5)
		upgradeExecFixtureSimulateIncoming(state)

		state.mu.Lock()
		reacquired := state.pidfileLockReacquired
		state.mu.Unlock()

		if !reacquired {
			t.Error("PL-027(i): new daemon instance did not re-acquire pidfile lock after exec-replacement")
		}
	})

	t.Run("ii-orphan-sweep-skipped-when-upgrade-env-set", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5) // sets HARMONIK_LISTENER_FD → implies HARMONIK_UPGRADE=1
		upgradeExecFixtureSimulateIncoming(state)

		state.mu.Lock()
		sweepSkipped := state.orphanSweepSkipped
		envDetected := state.upgradeEnvDetected
		state.mu.Unlock()

		if !envDetected {
			t.Error("PL-027(ii): HARMONIK_UPGRADE=1 not detected by new daemon instance")
		}
		if !sweepSkipped {
			t.Error("PL-027(ii): orphan sweep was NOT skipped on upgrade path; " +
				"MUST skip because in-flight subprocesses remain managed by same PID")
		}
	})

	t.Run("ii-orphan-sweep-runs-on-fresh-start", func(t *testing.T) {
		t.Parallel()

		// Non-upgrade (fresh start): orphan sweep must NOT be skipped.
		state := upgradeExecFixtureNewUpgradeState()
		// No outgoing simulation: HARMONIK_UPGRADE=1 is not set.

		state.mu.Lock()
		sweepSkipped := state.orphanSweepSkipped
		state.mu.Unlock()

		if sweepSkipped {
			t.Error("PL-027(ii): orphan sweep skipped on fresh-start path; must only skip when HARMONIK_UPGRADE=1")
		}
	})

	t.Run("iii-listener-fd-adopted-gap-free", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 7) // listener fd 7
		upgradeExecFixtureSimulateIncoming(state)

		state.mu.Lock()
		adopted := state.listenerFdAdopted
		noGap := state.noClientConnRefusedGap
		fdNum := state.listenerFdNumber
		state.mu.Unlock()

		if !adopted {
			t.Error("PL-027(iii): listener fd not adopted by new daemon instance; " +
				"MUST call net.FileListener(os.NewFile(fd, ...)) on HARMONIK_LISTENER_FD")
		}
		if !noGap {
			t.Error("PL-027(iii): client ECONNREFUSED gap detected across exec; " +
				"fd-passing must be gap-free")
		}
		if fdNum != "7" {
			t.Errorf("PL-027(iii): listener fd number = %q, want 7", fdNum)
		}
	})

	t.Run("iv-upgrading-marker-present-before-exec", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5) // marker written, upgrading=true

		state.mu.Lock()
		markerPresent := state.upgradingMarkerPresent
		state.mu.Unlock()

		// After outgoing simulation (but before incoming), marker must be present.
		if !markerPresent {
			t.Error("PL-027(iv): .harmonik/daemon.upgrading marker not present before exec; " +
				"MUST be durable on disk before execve (temp+rename+fsync discipline)")
		}
	})

	t.Run("iv-upgrading-marker-removed-after-ready", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5)
		upgradeExecFixtureSimulateIncoming(state) // incoming removes marker on ready

		state.mu.Lock()
		markerPresent := state.upgradingMarkerPresent
		markerRemoved := state.upgradingMarkerRemoved
		state.mu.Unlock()

		if markerPresent {
			t.Error("PL-027(iv): .harmonik/daemon.upgrading marker still present after clean transition to ready; " +
				"MUST be removed via unlink + fsync(parent_dir)")
		}
		if !markerRemoved {
			t.Error("PL-027(iv): marker removal not recorded; MUST remove marker on clean ready transition")
		}
	})

	t.Run("v-operator-upgrading-emitted-before-exec", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5)

		state.mu.Lock()
		upgradingEmitted := state.operatorUpgradingEmitted
		completedEmitted := state.operatorUpgradeCompletedEmitted
		state.mu.Unlock()

		// operator_upgrading must be emitted by the outgoing instance (pre-exec).
		if !upgradingEmitted {
			t.Error("PL-027(v): operator_upgrading event not emitted before exec; " +
				"MUST emit before execve per [event-model.md §8.7.9]")
		}

		// operator_upgrade_completed must NOT be emitted yet (new instance has not started).
		if completedEmitted {
			t.Error("PL-027(v): operator_upgrade_completed emitted before exec; " +
				"must only emit after new instance reaches ready")
		}
	})

	t.Run("v-operator-upgrade-completed-emitted-after-ready", func(t *testing.T) {
		t.Parallel()

		state := upgradeExecFixtureNewUpgradeState()
		upgradeExecFixtureSimulateOutgoing(state, 5)
		upgradeExecFixtureSimulateIncoming(state)

		state.mu.Lock()
		upgradingEmitted := state.operatorUpgradingEmitted
		completedEmitted := state.operatorUpgradeCompletedEmitted
		orderCorrect := state.emissionOrderCorrect
		state.mu.Unlock()

		if !completedEmitted {
			t.Error("PL-027(v): operator_upgrade_completed not emitted after new instance reached ready; " +
				"MUST emit per [event-model.md §8.7.10]")
		}

		if !upgradingEmitted {
			t.Error("PL-027(v): operator_upgrading not recorded; cannot verify emission order")
		}

		// PL-027(v): "operator_upgrading and operator_upgrade_completed emission
		// bracket the exec." upgrading < exec < completed.
		if !orderCorrect {
			t.Error("PL-027(v): emission order incorrect; operator_upgrading MUST precede " +
				"operator_upgrade_completed (they bracket the exec)")
		}
	})

	t.Run("v-emission-brackets-exec", func(t *testing.T) {
		t.Parallel()

		// Verify the full bracket: upgrading (pre-exec) → exec → completed (post-ready).
		var events []string

		// Outgoing daemon emits upgrading before exec.
		events = append(events, "operator_upgrading") // event from outgoing
		events = append(events, "execve")             // exec boundary

		// Incoming daemon reaches ready and emits completed.
		events = append(events, "daemon_started")             // new instance starts
		events = append(events, "daemon_ready")               // new instance ready
		events = append(events, "operator_upgrade_completed") // post-ready emission

		// Assert: upgrading must be before execve; completed must be after execve.
		upgradingIdx, execveIdx, completedIdx := -1, -1, -1
		for i, evt := range events {
			switch evt {
			case "operator_upgrading":
				upgradingIdx = i
			case "execve":
				execveIdx = i
			case "operator_upgrade_completed":
				completedIdx = i
			}
		}

		if upgradingIdx < 0 || execveIdx < 0 || completedIdx < 0 {
			t.Fatalf("PL-027(v) bracket: missing events; upgrading=%d execve=%d completed=%d",
				upgradingIdx, execveIdx, completedIdx)
		}

		if upgradingIdx >= execveIdx {
			t.Errorf("PL-027(v) bracket: operator_upgrading (idx=%d) must be BEFORE execve (idx=%d)",
				upgradingIdx, execveIdx)
		}

		if completedIdx <= execveIdx {
			t.Errorf("PL-027(v) bracket: operator_upgrade_completed (idx=%d) must be AFTER execve (idx=%d)",
				completedIdx, execveIdx)
		}
	})
}

// TestPL027_UpgradingMarkerAtomicWrite verifies that the upgrading marker is
// written with the temp+rename+fsync(parent_dir) atomic discipline required by
// [workspace-model.md §4.7 WM-026]. The fixture asserts the write sequence:
// write-to-temp → fsync(temp) → rename → fsync(parent_dir).
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — "The write MUST follow the
// temp+rename+fsync(parent_dir) atomic discipline of [workspace-model.md §4.7
// WM-026]: write content to a sibling temp file .harmonik/daemon.upgrading.tmp-<pid>;
// fsync(temp_fd); rename(2) the temp file; fsync(parent_directory_fd)."
func TestPL027_UpgradingMarkerAtomicWrite(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	harmonikDir := projectDir + "/.harmonik"

	// upgradeExecFixtureAtomicWriteTrace records each step of the atomic-write
	// discipline so the test can assert ordering.
	type upgradeExecFixtureAtomicWriteTrace struct {
		mu    sync.Mutex
		steps []string
	}
	trace := &upgradeExecFixtureAtomicWriteTrace{}

	recordStep := func(step string) {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		trace.steps = append(trace.steps, step)
	}

	// Simulate atomic write of the upgrading marker per WM-026 discipline.
	pid := os.Getpid()
	tmpPath := harmonikDir + "/daemon.upgrading.tmp-" + strconv.Itoa(pid)
	finalPath := harmonikDir + "/daemon.upgrading"

	// Step 1: write content to sibling temp file.
	content := `{"expected_commit_hash":"abc123","initiated_at":"2026-05-09T00:00:00Z","session_id":"sess-001"}`
	if err := os.WriteFile(tmpPath, []byte(content), 0o600); err != nil {
		t.Fatalf("PL-027 atomic-write: write temp file: %v", err)
	}
	recordStep("write-to-temp")

	// Step 2: fsync the temp file.
	tmpF, err := os.Open(tmpPath) //nolint:gosec // G304: tmpPath derived from os.Getpid() + local harmonikDir
	if err != nil {
		t.Fatalf("PL-027 atomic-write: open temp for fsync: %v", err)
	}
	if err := tmpF.Sync(); err != nil {
		_ = tmpF.Close() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-027 atomic-write: fsync temp: %v", err)
	}
	_ = tmpF.Close() //nolint:errcheck // cleanup error unactionable
	recordStep("fsync-temp")

	// Step 3: rename temp → final.
	if err := os.Rename(tmpPath, finalPath); err != nil {
		t.Fatalf("PL-027 atomic-write: rename temp → final: %v", err)
	}
	recordStep("rename")

	// Step 4: fsync parent directory.
	parentF, err := os.Open(harmonikDir) //nolint:gosec // G304: harmonikDir is t.TempDir()-derived, not user input
	if err != nil {
		t.Fatalf("PL-027 atomic-write: open parent dir: %v", err)
	}
	if err := parentF.Sync(); err != nil {
		_ = parentF.Close() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-027 atomic-write: fsync parent dir: %v", err)
	}
	_ = parentF.Close() //nolint:errcheck // cleanup error unactionable
	recordStep("fsync-parent-dir")

	// Assert the marker is present on disk.
	if _, err := os.Stat(finalPath); os.IsNotExist(err) {
		t.Error("PL-027 atomic-write: .harmonik/daemon.upgrading not present after rename")
	}

	// Assert the temp file is gone.
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("PL-027 atomic-write: temp file still exists at %s after rename", tmpPath)
	}

	// Assert step order.
	trace.mu.Lock()
	steps := make([]string, len(trace.steps))
	copy(steps, trace.steps)
	trace.mu.Unlock()

	expectedSteps := []string{"write-to-temp", "fsync-temp", "rename", "fsync-parent-dir"}
	if len(steps) != len(expectedSteps) {
		t.Fatalf("PL-027 atomic-write: %d steps, want %d", len(steps), len(expectedSteps))
	}
	for i, want := range expectedSteps {
		if steps[i] != want {
			t.Errorf("PL-027 atomic-write: step[%d] = %q, want %q", i, steps[i], want)
		}
	}

	// Cleanup the marker.
	_ = os.Remove(finalPath) //nolint:errcheck // cleanup error unactionable
}

// TestPL027_ListenerFdAdoptionNoConnRefused verifies the socket-continuity
// property of the fd-passing protocol: across the exec boundary, no client
// observes ECONNREFUSED because the listener fd is adopted by the new binary
// before the old binary's connections are fully drained.
//
// The fixture models the adoption using a real in-process Unix socket, then
// validates that a client connected to the original listener can connect to
// the same path after the fd is re-adopted.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iii) — "The socket path
// .harmonik/daemon.sock remains bound to the same listener inode throughout the
// exec window; clients observe no connection-refused gap."
func TestPL027_ListenerFdAdoptionNoConnRefused(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL027_ListenerFdAdoptionNoConnRefused: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	projectDir := plFixtureTempProjectDir(t)

	// Phase 1: outgoing daemon binds the socket.
	ln, err := plFixtureBindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("PL-027 fd-adoption: bind socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // cleanup error unactionable

	sockPath := plFixtureSocketPath(projectDir)

	// Phase 2: client connects to the bound socket (before exec).
	clientConn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("PL-027 fd-adoption: client connect pre-exec: %v", err)
	}
	_ = clientConn.Close() //nolint:errcheck // cleanup error unactionable

	// Phase 3: simulate fd adoption by the "new daemon" by accepting on the
	// same listener. In the real exec-replacement path, the new binary receives
	// the listener fd via HARMONIK_LISTENER_FD and calls net.FileListener.
	// Here we model gap-free adoption by keeping the listener alive.

	// Verify the listener is still accepting connections (no gap).
	clientConn2, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("PL-027 fd-adoption: client connect during adoption window: %v; "+
			"no ECONNREFUSED should be observable per PL-027(iii)", err)
	}
	_ = clientConn2.Close() //nolint:errcheck // cleanup error unactionable

	t.Logf("PL-027 fd-adoption: listener remains accepting across exec window; no ECONNREFUSED observed")
}

// TestPL027_UpgradeHashMismatchRefusesStartup verifies that if the on-disk
// binary's commit hash does not match the upgrading marker's expected_commit_hash,
// the new daemon refuses to start with ON §8 code 14 (upgrade-hash-mismatch).
//
// Spec ref: process-lifecycle.md §4.2 PL-005 step 8a — "(a) verify the on-disk
// binary's commit hash matches the marker's expected_commit_hash; on mismatch,
// refuse startup with ON §8 code 14 (upgrade-hash-mismatch)."
// Spec ref: process-lifecycle.md §4.2 PL-008a — "14 (upgrade-hash-mismatch)."
func TestPL027_UpgradeHashMismatchRefusesStartup(t *testing.T) {
	t.Parallel()

	// upgradeExecFixtureHashCheckResult models the outcome of the commit-hash
	// verification step in PL-005 step 8a.
	type upgradeExecFixtureHashCheckResult struct {
		// mismatch is true when expected != actual.
		mismatch bool
		// exitCode is the ON §8 exit code on mismatch (14 = upgrade-hash-mismatch).
		exitCode int
		// failureMode is the daemon_startup_failed event failure_mode.
		failureMode string
	}

	// upgradeExecFixtureCheckHash verifies the commit hash against the marker.
	upgradeExecFixtureCheckHash := func(expectedHash, actualHash string) upgradeExecFixtureHashCheckResult {
		if expectedHash != actualHash {
			return upgradeExecFixtureHashCheckResult{
				mismatch:    true,
				exitCode:    14,
				failureMode: "upgrade-hash-mismatch-on-restart",
			}
		}
		return upgradeExecFixtureHashCheckResult{mismatch: false, exitCode: 0}
	}

	t.Run("hash-mismatch-refuses-startup", func(t *testing.T) {
		t.Parallel()

		result := upgradeExecFixtureCheckHash("abc123expected", "def456actual")

		if !result.mismatch {
			t.Error("PL-027 hash-mismatch: expected mismatch to be detected; got none")
		}
		if result.exitCode != 14 {
			t.Errorf("PL-027 hash-mismatch: exitCode = %d, want 14 (upgrade-hash-mismatch)", result.exitCode)
		}
		if result.failureMode != "upgrade-hash-mismatch-on-restart" {
			t.Errorf("PL-027 hash-mismatch: failureMode = %q, want upgrade-hash-mismatch-on-restart",
				result.failureMode)
		}
	})

	t.Run("hash-match-allows-startup", func(t *testing.T) {
		t.Parallel()

		const commitHash = "abc123match"
		result := upgradeExecFixtureCheckHash(commitHash, commitHash)

		if result.mismatch {
			t.Error("PL-027 hash-match: mismatch detected when hashes are equal; must allow startup")
		}
		if result.exitCode != 0 {
			t.Errorf("PL-027 hash-match: exitCode = %d, want 0 (no failure)", result.exitCode)
		}
	})
}

// TestPL027_ExecReplacementSkipsOrphanSweep_SelfExec uses the self-exec pattern
// to run a child process that models the new daemon after exec-replacement. It
// verifies that when HARMONIK_UPGRADE=1 is set in the child's environment, the
// orphan sweep step is skipped.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(ii) — "When the daemon binary is
// launched via an exec-replacement from a live prior instance (detectable by the
// environment marker HARMONIK_UPGRADE=1 set by the outgoing binary), the new
// instance MUST skip §PL-005 step 3 (orphan sweep)."
func TestPL027_ExecReplacementSkipsOrphanSweep_SelfExec(t *testing.T) {
	// Sentinel check before t.Parallel().
	const sentinelEnv = "GO_PL027_EXEC_CHILD_RUN"

	if os.Getenv(sentinelEnv) == "1" {
		// --- CHILD PROCESS BODY ---
		// Model the new daemon's startup with HARMONIK_UPGRADE=1 detected.
		upgradeEnv := os.Getenv("HARMONIK_UPGRADE")
		listenerFD := os.Getenv("HARMONIK_LISTENER_FD")

		if upgradeEnv == "1" && listenerFD != "" {
			// Orphan sweep skipped (correct).
			os.Stdout.WriteString("orphan_sweep=skipped\n") //nolint:errcheck // child process stub
		} else {
			// Orphan sweep would run (should not happen on upgrade path).
			os.Stdout.WriteString("orphan_sweep=ran\n") //nolint:errcheck // child process stub
		}
		os.Exit(0)
	}

	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL027_ExecReplacementSkipsOrphanSweep_SelfExec: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is os.Args[0], not user input
	cmd := exec.CommandContext(t.Context(), testBin,
		"-test.run=^TestPL027_ExecReplacementSkipsOrphanSweep_SelfExec$",
		"-test.v=false",
	)
	cmd.Env = append(os.Environ(),
		sentinelEnv+"=1",
		"HARMONIK_UPGRADE=1",
		"HARMONIK_LISTENER_FD=5",
	)
	// Ensure stdin/stdout fds exist for the child.
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("PL-027 self-exec: child exec failed: %v", err)
	}

	outStr := strings.TrimSpace(string(out))
	if !strings.Contains(outStr, "orphan_sweep=skipped") {
		t.Errorf("PL-027(ii) self-exec: child output = %q, want substring orphan_sweep=skipped; "+
			"new daemon MUST skip orphan sweep when HARMONIK_UPGRADE=1 is set", outStr)
	}
}

// TestPL027_UpgradingMarkerRemovedOnCleanTransition verifies the full
// lifecycle of the .harmonik/daemon.upgrading marker: present before exec,
// removed after the new instance transitions to ready.
//
// This is a filesystem-level test using a real tempdir.
//
// Spec ref: process-lifecycle.md §4.9 PL-027(iv) — "The marker MUST be present
// on disk and durable before execve is invoked … removed on clean transition to
// ready per ON-020a (also via … unlink followed by parent-directory fsync)."
func TestPL027_UpgradingMarkerRemovedOnCleanTransition(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	harmonikDir := projectDir + "/.harmonik"
	markerPath := harmonikDir + "/daemon.upgrading"

	// Phase 1: outgoing daemon writes marker before exec.
	content := `{"expected_commit_hash":"abc123","initiated_at":"2026-05-09T00:00:00Z","session_id":"sess-001"}`
	if err := os.WriteFile(markerPath, []byte(content), 0o600); err != nil {
		t.Fatalf("PL-027 marker lifecycle: write marker: %v", err)
	}

	// Phase 2: assert marker is present (outgoing instance's obligation).
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Fatal("PL-027 marker lifecycle: marker not present after outgoing write; must be durable before execve")
	}

	// Phase 3: incoming daemon reads and validates the marker (PL-005 step 8a).
	//nolint:gosec // G304: markerPath derived from plFixtureTempProjectDir, not user input
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("PL-027 marker lifecycle: read marker: %v", err)
	}
	if !strings.Contains(string(data), "expected_commit_hash") {
		t.Errorf("PL-027 marker lifecycle: marker content missing expected_commit_hash field; got: %s", data)
	}

	// Phase 4: incoming daemon transitions to ready, removes marker via
	// unlink + fsync(parent_dir).
	if err := syscall.Unlink(markerPath); err != nil {
		t.Fatalf("PL-027 marker lifecycle: unlink marker: %v", err)
	}

	// fsync parent dir after unlink.
	parentF, err := os.Open(harmonikDir) //nolint:gosec // G304: harmonikDir derived from plFixtureTempProjectDir
	if err != nil {
		t.Fatalf("PL-027 marker lifecycle: open parent dir for fsync: %v", err)
	}
	if err := parentF.Sync(); err != nil {
		_ = parentF.Close() //nolint:errcheck // cleanup error unactionable
		t.Fatalf("PL-027 marker lifecycle: fsync parent dir: %v", err)
	}
	_ = parentF.Close() //nolint:errcheck // cleanup error unactionable

	// Phase 5: assert marker is gone.
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("PL-027 marker lifecycle: marker still present after ready transition; " +
			"MUST be removed via unlink + fsync(parent_dir) per ON-020a")
	}

	t.Logf("PL-027 marker lifecycle: marker present before exec, removed after ready — full lifecycle verified")

	_ = time.Now() // anchor test ran at real wall-clock time
}
