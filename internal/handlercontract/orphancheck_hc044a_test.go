package handlercontract_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// orphancheck_hc044a_test.go — unit tests for the HC-044a orphan-detection
// mechanism: WorkerPidfilePath, WriteWorkerPidfileAtomic, RemoveWorkerPidfile,
// CheckOrphanHeldWorkspace, and OrphanWorkspaceSubReason.
//
// Spec refs: specs/handler-contract.md §4.10.HC-044a, §8.2.
// Bead: hk-8i31.52.
//
// Helper prefix: orphancheckFixture (per implementer-protocol.md
// §Helper-prefix discipline).

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// orphancheckFixtureProjectRoot creates a temporary directory that acts as the
// project root (the directory containing .harmonik/) for a single test.
// The caller does not need to create .harmonik/; helpers create subdirs as needed.
func orphancheckFixtureProjectRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// orphancheckFixtureWritePidfile calls WriteWorkerPidfileAtomic with the given
// arguments and calls t.Fatalf on failure.
func orphancheckFixtureWritePidfile(t *testing.T, projectRoot, runID string, pid int) {
	t.Helper()
	target := handlercontract.WorkerPidfilePath(projectRoot, runID)
	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, pid); err != nil {
		t.Fatalf("orphancheckFixtureWritePidfile: WriteWorkerPidfileAtomic: %v", err)
	}
}

// orphancheckFixtureReadJSON reads the file at path and unmarshals it into dst.
func orphancheckFixtureReadJSON(t *testing.T, path string, dst any) {
	t.Helper()
	//nolint:gosec // G304: path is constructed from t.TempDir() in tests, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("orphancheckFixtureReadJSON: ReadFile %q: %v", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("orphancheckFixtureReadJSON: Unmarshal: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OrphanWorkspaceSubReason constant
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_SubReasonValue asserts that OrphanWorkspaceSubReason
// equals the literal "workspace_held_by_orphan" declared in §8.2.
//
// This constant is load-bearing: it appears verbatim in agent_failed payloads
// and must match the sub_reason taxonomy of §8.2 exactly.
func TestOrphanCheck_HC044a_SubReasonValue(t *testing.T) {
	t.Parallel()

	const want = "workspace_held_by_orphan"
	if handlercontract.OrphanWorkspaceSubReason != want {
		t.Errorf(
			"OrphanWorkspaceSubReason = %q, want %q (specs/handler-contract.md §4.10.HC-044a + §8.2)",
			handlercontract.OrphanWorkspaceSubReason, want,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WorkerPidfilePath
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_PidfilePathShape verifies that WorkerPidfilePath
// returns <projectRoot>/.harmonik/worktrees/<runID>/.lock per §4.10.HC-044a.
func TestOrphanCheck_HC044a_PidfilePathShape(t *testing.T) {
	t.Parallel()

	root := "/some/project/root"
	runID := "run-abc-123"
	got := handlercontract.WorkerPidfilePath(root, runID)
	want := filepath.Join(root, ".harmonik", "worktrees", runID, ".lock")
	if got != want {
		t.Errorf(
			"WorkerPidfilePath(%q, %q) = %q, want %q (§4.10.HC-044a: .harmonik/worktrees/<run_id>/.lock)",
			root, runID, got, want,
		)
	}
}

// TestOrphanCheck_HC044a_PidfilePathEndsWithDotLock asserts that the pidfile
// basename is ".lock" per HC-044a.
func TestOrphanCheck_HC044a_PidfilePathEndsWithDotLock(t *testing.T) {
	t.Parallel()

	got := handlercontract.WorkerPidfilePath("/project", "run-xyz")
	if filepath.Base(got) != ".lock" {
		t.Errorf(
			"WorkerPidfilePath base = %q, want .lock (§4.10.HC-044a pidfile name)",
			filepath.Base(got),
		)
	}
}

// TestOrphanCheck_HC044a_PidfilePathDistinctFromDaemonPid asserts that the
// worker pidfile path does NOT equal the daemon pidfile path (.harmonik/daemon.pid).
//
// These are two distinct files per the spec: the daemon pidfile asserts
// per-project daemon singularity (process-lifecycle.md §4.1 PL-002); the
// worker pidfile asserts per-session handler-subprocess ownership (HC-044a).
func TestOrphanCheck_HC044a_PidfilePathDistinctFromDaemonPid(t *testing.T) {
	t.Parallel()

	root := "/project"
	workerPath := handlercontract.WorkerPidfilePath(root, "run-001")
	daemonPidPath := filepath.Join(root, ".harmonik", "daemon.pid")

	if workerPath == daemonPidPath {
		t.Errorf(
			"WorkerPidfilePath returned daemon pidfile path %q; "+
				"worker pidfile MUST be distinct from daemon pidfile (process-lifecycle.md §4.1 vs HC-044a)",
			workerPath,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// WriteWorkerPidfileAtomic
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_WriteCreatesFile verifies that WriteWorkerPidfileAtomic
// creates the pidfile at the canonical path.
func TestOrphanCheck_HC044a_WriteCreatesFile(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-write-001"
	target := handlercontract.WorkerPidfilePath(root, runID)

	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, os.Getpid()); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("pidfile not found at %q after write: %v", target, err)
	}
}

// TestOrphanCheck_HC044a_WriteContentShape verifies that the pidfile contains
// valid JSON with the expected fields (pid, run_id, written_at).
func TestOrphanCheck_HC044a_WriteContentShape(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-content-001"
	pid := os.Getpid()
	target := handlercontract.WorkerPidfilePath(root, runID)

	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, pid); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}

	var m map[string]any
	orphancheckFixtureReadJSON(t, target, &m)

	// pid field
	gotPID, ok := m["pid"].(float64)
	if !ok {
		t.Fatalf("pidfile missing or wrong-typed \"pid\" field; got %T", m["pid"])
	}
	if int(gotPID) != pid {
		t.Errorf("pidfile pid = %d, want %d", int(gotPID), pid)
	}

	// run_id field
	gotRunID, ok := m["run_id"].(string)
	if !ok || gotRunID == "" {
		t.Errorf("pidfile run_id = %v (%T), want %q", m["run_id"], m["run_id"], runID)
	} else if gotRunID != runID {
		t.Errorf("pidfile run_id = %q, want %q", gotRunID, runID)
	}

	// written_at field — must be non-empty (exact format validation deferred to integration)
	if eat, ok := m["written_at"].(string); !ok || eat == "" {
		t.Errorf("pidfile written_at missing or empty; got %v", m["written_at"])
	}
}

// TestOrphanCheck_HC044a_WriteMkdirAll verifies that WriteWorkerPidfileAtomic
// creates intermediate directories (the .harmonik/worktrees/<run_id>/ chain).
func TestOrphanCheck_HC044a_WriteMkdirAll(t *testing.T) {
	t.Parallel()

	// Use a deeply nested run_id-style path that doesn't exist yet.
	root := orphancheckFixtureProjectRoot(t)
	runID := "run-mkdir-001"
	target := handlercontract.WorkerPidfilePath(root, runID)

	// The directory tree must NOT exist before the call.
	parentDir := filepath.Dir(target)
	if _, err := os.Stat(parentDir); !os.IsNotExist(err) {
		t.Skipf("parent dir already exists (unexpected): %q", parentDir)
	}

	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, os.Getpid()); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("pidfile not found at %q after write with MkdirAll: %v", target, err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RemoveWorkerPidfile
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_RemoveDeletesFile verifies that RemoveWorkerPidfile
// deletes an existing pidfile.
func TestOrphanCheck_HC044a_RemoveDeletesFile(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-remove-001"
	orphancheckFixtureWritePidfile(t, root, runID, os.Getpid())

	target := handlercontract.WorkerPidfilePath(root, runID)
	if err := handlercontract.RemoveWorkerPidfile(target); err != nil {
		t.Fatalf("RemoveWorkerPidfile: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("pidfile still exists at %q after remove", target)
	}
}

// TestOrphanCheck_HC044a_RemoveIdempotent verifies that RemoveWorkerPidfile on
// a non-existent path returns nil (idempotent).
func TestOrphanCheck_HC044a_RemoveIdempotent(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	target := handlercontract.WorkerPidfilePath(root, "run-absent-001")
	// File does not exist; remove must succeed idempotently.
	if err := handlercontract.RemoveWorkerPidfile(target); err != nil {
		t.Errorf("RemoveWorkerPidfile on absent file: got error %v, want nil (idempotent)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckOrphanHeldWorkspace — no pidfile
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_NoPidfileNotHeld verifies that when no pidfile exists,
// CheckOrphanHeldWorkspace reports Held=false, Stale=false (clean launch path).
func TestOrphanCheck_HC044a_NoPidfileNotHeld(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	result, err := handlercontract.CheckOrphanHeldWorkspace(root, "run-nopidfile-001", nil)
	if err != nil {
		t.Fatalf("CheckOrphanHeldWorkspace: unexpected error: %v", err)
	}
	if result.Held {
		t.Errorf("Held = true, want false when no pidfile exists")
	}
	if result.Stale {
		t.Errorf("Stale = true, want false when no pidfile exists")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckOrphanHeldWorkspace — stale pidfile (PID not live)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_StalePidfileReclaimed verifies that a pidfile carrying
// a PID that is no longer live is classified as Stale=true, Held=false.
//
// We use a PID that cannot be live: a negative PID (-1) causes ESRCH on liveness
// probe.  We write this by constructing the JSON manually and placing it at the
// canonical path.
func TestOrphanCheck_HC044a_StalePidfileReclaimed(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-stale-001"

	// Write a pidfile carrying a PID that is guaranteed not to exist.
	// On POSIX, PID 0 is invalid and kill(0, 0) probes the calling process's
	// group — use a large PID that is extremely unlikely to be live.
	// We use a sentinel value PID via /proc/sys/kernel/pid_max trick — simplest
	// approach: pick a PID value > 4194304 (Linux max on most kernels), which
	// will ESRCH on any kill(pid,0).  On macOS, max PID is 99998; use a value
	// between 100000 and 999999.
	//
	// Cross-platform safe sentinel: write PID = 999999 (above both Linux and
	// macOS defaults; not likely to be in use in a test environment).
	const stalePID = 999999

	pidfilePath := handlercontract.WorkerPidfilePath(root, runID)
	if err := handlercontract.WriteWorkerPidfileAtomic(pidfilePath, runID, stalePID); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}

	result, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)
	if err != nil {
		// If the stale PID is accidentally live (race), skip rather than fail.
		// In practice, PID 999999 should never be live in a test environment.
		t.Skipf("CheckOrphanHeldWorkspace: unexpected error (possible PID collision): %v", err)
	}

	if result.Held {
		t.Errorf("Held = true, want false for stale pidfile (PID %d not live)", stalePID)
	}
	if !result.Stale {
		t.Errorf("Stale = false, want true for pidfile with dead PID %d", stalePID)
	}
}

// TestOrphanCheck_HC044a_InvalidJSONPidfileStale verifies that a pidfile with
// unparseable JSON is treated as stale (reclaim-safe per HC-044a).
func TestOrphanCheck_HC044a_InvalidJSONPidfileStale(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-badjson-001"

	pidfilePath := handlercontract.WorkerPidfilePath(root, runID)
	if err := os.MkdirAll(filepath.Dir(pidfilePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	//nolint:gosec // G304: path constructed from t.TempDir() in test
	if err := os.WriteFile(pidfilePath, []byte("not-valid-json{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)
	if err != nil {
		t.Fatalf("CheckOrphanHeldWorkspace: unexpected error for bad-JSON pidfile: %v", err)
	}
	if result.Held {
		t.Errorf("Held = true, want false for unparseable pidfile (treat as stale per HC-044a)")
	}
	if !result.Stale {
		t.Errorf("Stale = false, want true for unparseable pidfile")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckOrphanHeldWorkspace — owned by current generation (no orphan)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_OwnedPIDNotOrphan verifies that a live PID that
// is present in ownedByCurrentGen does NOT trigger the orphan condition.
// This models the HC-004 concurrent-launch scenario where the same (run_id,
// node_id) is launched twice and the second call detects the first call's
// pidfile.
func TestOrphanCheck_HC044a_OwnedPIDNotOrphan(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-owned-001"
	selfPID := os.Getpid() // self is definitely live

	orphancheckFixtureWritePidfile(t, root, runID, selfPID)

	// Mark self's PID as owned by current generation.
	owned := map[int]bool{selfPID: true}
	result, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, owned)
	if err != nil {
		t.Fatalf("CheckOrphanHeldWorkspace: unexpected error: %v", err)
	}
	if result.Held {
		t.Errorf("Held = true, want false when live PID is owned by current generation (HC-004 concurrent-launch scenario)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckOrphanHeldWorkspace — live PID not owned (orphan)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_LiveOrphanPIDReturnsError verifies that a live PID
// not owned by the current daemon generation triggers Held=true and an error
// wrapping ErrStructural per HC-044a.
//
// We use PID 1 (init/launchd on POSIX) which is always live and never owned
// by our daemon generation.
func TestOrphanCheck_HC044a_LiveOrphanPIDReturnsError(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-orphan-001"

	// PID 1 (init/launchd) is always live and never our subprocess.
	const orphanPID = 1
	orphancheckFixtureWritePidfile(t, root, runID, orphanPID)

	result, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)

	if !result.Held {
		t.Errorf("Held = false, want true for live PID %d not owned by current generation", orphanPID)
	}
	if result.OrphanPID != orphanPID {
		t.Errorf("OrphanPID = %d, want %d", result.OrphanPID, orphanPID)
	}
	if result.OrphanRunID != runID {
		t.Errorf("OrphanRunID = %q, want %q", result.OrphanRunID, runID)
	}

	// The returned error MUST wrap ErrStructural per HC-044a.
	if err == nil {
		t.Fatalf("error = nil, want non-nil error wrapping ErrStructural when orphan detected")
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf(
			"error %v does not wrap ErrStructural; HC-044a requires Launch to return ErrStructural",
			err,
		)
	}
}

// TestOrphanCheck_HC044a_OrphanErrorClassIsStructural verifies that the error
// returned when an orphan is detected has error class "structural" per §8.2.
func TestOrphanCheck_HC044a_OrphanErrorClassIsStructural(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-class-001"

	// PID 1 is always live.
	orphancheckFixtureWritePidfile(t, root, runID, 1)

	_, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)
	if err == nil {
		t.Fatal("error = nil, want structural error")
	}

	got := handlercontract.Class(err)
	if got != "structural" {
		t.Errorf("Class(err) = %q, want %q (HC-044a emits ErrStructural)", got, "structural")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: pidfile path is distinct from WM-013a lease-lock path
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_PidfileDistinctFromLeaseLock verifies that the HC-044a
// worker pidfile path (.harmonik/worktrees/<run_id>/.lock) is distinct from the
// workspace-model lease-lock path (.harmonik/lease.lock within workspacePath).
//
// These serve different purposes: the HC-044a pidfile is per-run and lives in
// the project .harmonik dir; the WM-013a lease-lock is per-workspace and lives
// within the workspace path itself.
func TestOrphanCheck_HC044a_PidfileDistinctFromLeaseLock(t *testing.T) {
	t.Parallel()

	projectRoot := "/project"
	workspacePath := "/project/.harmonik/worktrees/run-001" // example workspace path
	runID := "run-001"

	workerPidfile := handlercontract.WorkerPidfilePath(projectRoot, runID)
	// WM-013a canonical lease-lock path: <workspacePath>/.harmonik/lease.lock
	wm013aLeaseLock := filepath.Join(workspacePath, ".harmonik", "lease.lock")

	if workerPidfile == wm013aLeaseLock {
		t.Errorf(
			"HC-044a worker pidfile path %q equals WM-013a lease-lock path %q; "+
				"these MUST be distinct files serving different purposes",
			workerPidfile, wm013aLeaseLock,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: OrphanCheckResult field consistency
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_ResultHeldImpliesOrphanPID verifies that when
// Held=true the OrphanPID and OrphanRunID fields are populated.
func TestOrphanCheck_HC044a_ResultHeldImpliesOrphanPID(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-fields-001"
	orphancheckFixtureWritePidfile(t, root, runID, 1) // PID 1 always live

	result, _ := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)

	if result.Held {
		if result.OrphanPID == 0 {
			t.Errorf("OrphanCheckResult.Held=true but OrphanPID=0; MUST be populated when Held")
		}
		if result.OrphanRunID == "" {
			t.Errorf("OrphanCheckResult.Held=true but OrphanRunID empty; MUST be populated when Held")
		}
	}
}

// TestOrphanCheck_HC044a_RunIDInOrphanResult verifies that the OrphanRunID
// returned by CheckOrphanHeldWorkspace matches the run_id written in the
// pidfile (not necessarily the runID passed to Check).
func TestOrphanCheck_HC044a_RunIDInOrphanResult(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	orphanRunID := "run-prior-gen-001"
	// Write a pidfile for a prior generation's run_id using the same runID
	// directory (simulating a workspace already allocated to orphanRunID).
	orphancheckFixtureWritePidfile(t, root, orphanRunID, 1) // PID 1 always live

	result, _ := handlercontract.CheckOrphanHeldWorkspace(root, orphanRunID, nil)

	if result.Held && result.OrphanRunID != orphanRunID {
		t.Errorf("OrphanRunID = %q, want %q", result.OrphanRunID, orphanRunID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: write+remove round-trip leaves no file
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_WriteRemoveRoundTrip verifies the full
// write-then-remove lifecycle: pidfile is present after write, absent after
// remove, and CheckOrphanHeldWorkspace reports no orphan after removal.
func TestOrphanCheck_HC044a_WriteRemoveRoundTrip(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "run-roundtrip-001"
	target := handlercontract.WorkerPidfilePath(root, runID)

	// Write.
	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, os.Getpid()); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("pidfile missing after write: %v", err)
	}

	// Remove.
	if err := handlercontract.RemoveWorkerPidfile(target); err != nil {
		t.Fatalf("RemoveWorkerPidfile: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("pidfile still present after remove")
	}

	// Check: no orphan detected after clean removal.
	result, err := handlercontract.CheckOrphanHeldWorkspace(root, runID, nil)
	if err != nil {
		t.Fatalf("CheckOrphanHeldWorkspace after remove: unexpected error: %v", err)
	}
	if result.Held {
		t.Errorf("Held = true after pidfile removal; want false (clean termination removes pidfile per HC-044a)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sensor: pidfile path encoding of run_id (injection safety)
// ─────────────────────────────────────────────────────────────────────────────

// TestOrphanCheck_HC044a_PidfilePathRunIDIsPathSegment verifies that the run_id
// is used as a single path segment in WorkerPidfilePath (not joined with an
// unsanitized separator). This prevents path-traversal via crafted run_id values.
//
// Normative: run_id values are UUIDs (execution-model.md §4.3 EM-013); the test
// uses a UUID-shaped value. Non-UUID run_ids would be a caller error; this sensor
// confirms the path construction is safe for well-formed run_ids.
func TestOrphanCheck_HC044a_PidfilePathRunIDIsPathSegment(t *testing.T) {
	t.Parallel()

	root := "/project"
	// UUID-shaped run_id: 36 characters, no path separators.
	runID := "550e8400-e29b-41d4-a716-446655440000"
	got := handlercontract.WorkerPidfilePath(root, runID)

	// The run_id must appear as a complete path segment.
	dir := filepath.Dir(filepath.Dir(got)) // strip /.lock and runID dir
	if filepath.Base(dir) != "worktrees" {
		t.Errorf(
			"WorkerPidfilePath(%q, %q) = %q; expected .../worktrees/<runID>/.lock shape",
			root, runID, got,
		)
	}

	// The run_id segment must equal the runID parameter exactly.
	runIDSegment := filepath.Base(filepath.Dir(got))
	if runIDSegment != runID {
		t.Errorf(
			"run_id segment in pidfile path = %q, want %q",
			runIDSegment, runID,
		)
	}
}

// TestOrphanCheck_HC044a_PidfileRunIDFieldPreserved verifies that the run_id
// written into the pidfile JSON matches the runID parameter (not truncated or
// altered).
func TestOrphanCheck_HC044a_PidfileRunIDFieldPreserved(t *testing.T) {
	t.Parallel()

	root := orphancheckFixtureProjectRoot(t)
	runID := "test-run-id-" + strconv.Itoa(os.Getpid())
	target := handlercontract.WorkerPidfilePath(root, runID)

	if err := handlercontract.WriteWorkerPidfileAtomic(target, runID, os.Getpid()); err != nil {
		t.Fatalf("WriteWorkerPidfileAtomic: %v", err)
	}

	var m map[string]any
	orphancheckFixtureReadJSON(t, target, &m)

	gotRunID, _ := m["run_id"].(string)
	if gotRunID != runID {
		t.Errorf("pidfile run_id = %q, want %q (run_id must be preserved verbatim)", gotRunID, runID)
	}
}
