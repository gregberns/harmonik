package scenario

// sh015_fixture_teardown_test.go — contract tests for the SH-015 fixture
// teardown phase.
//
// Per specs/scenario-harness.md §4.4 SH-015: the harness MUST execute fixture
// teardown on every terminal scenario path (pass, fail, timeout, error).
// Teardown is run-to-completion best-effort: a failure in any sub-step MUST
// NOT halt the remaining sub-steps; all errors are accumulated and reported.
// Sub-steps in order:
//
//	(a) terminate still-live handler subprocesses honoring HC-018 bounds;
//	(b) release worktree leases per WM-013b;
//	(c) close the event-log file (fsync then close);
//	(d) stop per-scenario daemon via daemon stop RPC of PL-003a;
//	(e) record workspace_snapshot_path per SH-015a.
//
// Teardown is idempotent.
//
// Helper prefix: sh015Teardown (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.4 SH-015.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/workspace"
)

// sh015TeardownTempDir creates a temporary directory, registered for cleanup.
func sh015TeardownTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-sh015-")
	if err != nil {
		t.Fatalf("sh015TeardownTempDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// sh015TeardownWriteLeaseLock creates a minimal lease-lock file at the
// canonical path under workspacePath so sub-step (b) has a file to remove.
func sh015TeardownWriteLeaseLock(t *testing.T, workspacePath string) string {
	t.Helper()
	lockPath := workspace.LeaseLockPath(workspacePath)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("sh015TeardownWriteLeaseLock: MkdirAll: %v", err)
	}
	//nolint:gosec // G306: 0644 is appropriate for test fixture files
	if err := os.WriteFile(lockPath, []byte(`{"run_id":"00000000-0000-0000-0000-000000000001","pid":1,"created_at":"2026-01-01T00:00:00Z","ttl_sec":3600}`+"\n"), 0o644); err != nil {
		t.Fatalf("sh015TeardownWriteLeaseLock: WriteFile: %v", err)
	}
	return lockPath
}

// sh015TeardownWriteEventLog creates a minimal event-log JSONL file so
// sub-step (c) has a file to fsync+close.
func sh015TeardownWriteEventLog(t *testing.T, eventLogPath string) {
	t.Helper()
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(eventLogPath), 0o755); err != nil {
		t.Fatalf("sh015TeardownWriteEventLog: MkdirAll: %v", err)
	}
	//nolint:gosec // G306: 0644 is appropriate for test fixture files
	if err := os.WriteFile(eventLogPath, []byte(`{"type":"daemon_started"}`+"\n"), 0o644); err != nil {
		t.Fatalf("sh015TeardownWriteEventLog: WriteFile: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step (a): HandlerCancels
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_SubStepA_CallsHandlerCancels verifies that
// TeardownFixture calls each HandlerCancel function in params, satisfying
// sub-step (a)'s obligation to terminate still-live handler subprocesses.
func TestSH015_TeardownFixture_SubStepA_CallsHandlerCancels(t *testing.T) {
	t.Parallel()

	var called [2]bool
	params := TeardownParams{
		ScenarioName: "sh015-cancel-test",
		HandlerCancels: []func(ctx context.Context) error{
			func(_ context.Context) error { called[0] = true; return nil },
			func(_ context.Context) error { called[1] = true; return nil },
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", teardownErr)
	}
	for i, c := range called {
		if !c {
			t.Errorf("SH-015(a): HandlerCancels[%d] was not called", i)
		}
	}
}

// TestSH015_TeardownFixture_SubStepA_NilEntrySkipped verifies that a nil
// entry in HandlerCancels is skipped without error.
func TestSH015_TeardownFixture_SubStepA_NilEntrySkipped(t *testing.T) {
	t.Parallel()

	called := false
	params := TeardownParams{
		ScenarioName: "sh015-nil-cancel",
		HandlerCancels: []func(ctx context.Context) error{
			nil,
			func(_ context.Context) error { called = true; return nil },
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", teardownErr)
	}
	if !called {
		t.Error("SH-015(a): non-nil HandlerCancels[1] was not called after skipping nil entry")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step (b): worktree lease release
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_SubStepB_ReleasesLeaseLock verifies that
// TeardownFixture removes the lease-lock file at the canonical path under
// WorkspacePath, implementing WM-013b's lease release contract.
func TestSH015_TeardownFixture_SubStepB_ReleasesLeaseLock(t *testing.T) {
	t.Parallel()

	workspacePath := sh015TeardownTempDir(t)
	lockPath := sh015TeardownWriteLeaseLock(t, workspacePath)

	params := TeardownParams{
		ScenarioName:  "sh015-lease-release",
		WorkspacePath: workspacePath,
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", teardownErr)
	}

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("SH-015(b): lease-lock file %q still exists after teardown; want removed", lockPath)
	}
}

// TestSH015_TeardownFixture_SubStepB_IdempotentOnMissingLeaseLock verifies
// that TeardownFixture succeeds when no lease-lock file exists (already
// released or never written), satisfying the idempotency invariant.
func TestSH015_TeardownFixture_SubStepB_IdempotentOnMissingLeaseLock(t *testing.T) {
	t.Parallel()

	workspacePath := sh015TeardownTempDir(t)
	params := TeardownParams{
		ScenarioName:  "sh015-idempotent-lease",
		WorkspacePath: workspacePath,
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(b): TeardownFixture with no lease-lock file returned error: %v; want nil (idempotent)", err)
	}
}

// TestSH015_TeardownFixture_SubStepB_EmptyWorkspacePathSkipped verifies that
// an empty WorkspacePath causes sub-step (b) to be skipped without error.
func TestSH015_TeardownFixture_SubStepB_EmptyWorkspacePathSkipped(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName:  "sh015-empty-workspace",
		WorkspacePath: "", // sub-step (b) must be skipped
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(b): TeardownFixture with empty WorkspacePath returned error: %v; want nil", err)
	}
}

// TestSH015_TeardownFixture_SubStepB_TwiceIsNoop verifies that calling
// TeardownFixture twice with the same WorkspacePath is a no-op on the second
// call, satisfying the SH-015 idempotency invariant.
func TestSH015_TeardownFixture_SubStepB_TwiceIsNoop(t *testing.T) {
	t.Parallel()

	workspacePath := sh015TeardownTempDir(t)
	sh015TeardownWriteLeaseLock(t, workspacePath)

	params := TeardownParams{
		ScenarioName:  "sh015-idempotent-twice",
		WorkspacePath: workspacePath,
	}

	if _, err := TeardownFixture(t.Context(), params); err != nil {
		t.Fatalf("TeardownFixture first call: unexpected error: %v", err)
	}
	if _, err := TeardownFixture(t.Context(), params); err != nil {
		t.Errorf("SH-015 idempotency: TeardownFixture second call returned error: %v; want nil (idempotent)", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step (c): event-log fsync+close
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_SubStepC_FsyncsAndClosesEventLog verifies that
// TeardownFixture opens the event-log file, fsyncs it to durable storage, and
// closes the file descriptor without error.
func TestSH015_TeardownFixture_SubStepC_FsyncsAndClosesEventLog(t *testing.T) {
	t.Parallel()

	dir := sh015TeardownTempDir(t)
	eventLogPath := filepath.Join(dir, "events.jsonl")
	sh015TeardownWriteEventLog(t, eventLogPath)

	params := TeardownParams{
		ScenarioName: "sh015-fsync-close",
		EventLogPath: eventLogPath,
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(c): TeardownFixture with existing event-log returned error: %v; want nil", err)
	}
}

// TestSH015_TeardownFixture_SubStepC_IdempotentOnMissingEventLog verifies
// that TeardownFixture returns no error when the event-log file does not exist
// (daemon never wrote events, or prior teardown already handled it).
func TestSH015_TeardownFixture_SubStepC_IdempotentOnMissingEventLog(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-no-eventlog",
		EventLogPath: "/nonexistent-sh015-test/events.jsonl",
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(c): TeardownFixture with missing event-log returned error: %v; want nil (idempotent)", err)
	}
}

// TestSH015_TeardownFixture_SubStepC_EmptyPathSkipped verifies that an empty
// EventLogPath causes sub-step (c) to be skipped without error.
func TestSH015_TeardownFixture_SubStepC_EmptyPathSkipped(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-empty-eventlog",
		EventLogPath: "", // sub-step (c) must be skipped
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(c): TeardownFixture with empty EventLogPath returned error: %v; want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step (d): daemon stop
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_SubStepD_CallsStopDaemon verifies that
// TeardownFixture calls the StopDaemon function, satisfying sub-step (d)'s
// daemon stop RPC obligation.
func TestSH015_TeardownFixture_SubStepD_CallsStopDaemon(t *testing.T) {
	t.Parallel()

	stopCalled := false
	params := TeardownParams{
		ScenarioName: "sh015-stop-daemon",
		StopDaemon: func(_ context.Context) error {
			stopCalled = true
			return nil
		},
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", err)
	}
	if !stopCalled {
		t.Error("SH-015(d): StopDaemon was not called")
	}
}

// TestSH015_TeardownFixture_SubStepD_NilStopDaemonIsNoop verifies that a nil
// StopDaemon causes sub-step (d) to be skipped without error (daemon already
// exited via CancelOnQueueExit or was never started).
func TestSH015_TeardownFixture_SubStepD_NilStopDaemonIsNoop(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-nil-stop",
		StopDaemon:   nil,
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Errorf("SH-015(d): TeardownFixture with nil StopDaemon returned error: %v; want nil", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step (e): workspace_snapshot_path recording
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_SubStepE_SnapshotPathPopulated verifies that
// TeardownResult.WorkspaceSnapshotPath equals WorkspaceSnapshotPath(scenarioName)
// on all paths (success and failure), satisfying sub-step (e)'s recording
// obligation per SH-015a.
func TestSH015_TeardownFixture_SubStepE_SnapshotPathPopulated(t *testing.T) {
	t.Parallel()

	const scenarioName = "my-scenario"
	params := TeardownParams{ScenarioName: scenarioName}

	result, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", err)
	}

	want := WorkspaceSnapshotPath(scenarioName)
	if result.WorkspaceSnapshotPath != want {
		t.Errorf("SH-015(e): WorkspaceSnapshotPath = %q; want %q", result.WorkspaceSnapshotPath, want)
	}
}

// TestSH015_TeardownFixture_SubStepE_SnapshotPathPopulatedOnFailure verifies
// that WorkspaceSnapshotPath is populated even when a sub-step fails, so the
// caller can record the snapshot path regardless of teardown errors.
func TestSH015_TeardownFixture_SubStepE_SnapshotPathPopulatedOnFailure(t *testing.T) {
	t.Parallel()

	const scenarioName = "sh015-fail-scenario"
	params := TeardownParams{
		ScenarioName: scenarioName,
		StopDaemon: func(_ context.Context) error {
			return fmt.Errorf("simulated stop failure")
		},
	}

	result, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}

	want := WorkspaceSnapshotPath(scenarioName)
	if result.WorkspaceSnapshotPath != want {
		t.Errorf("SH-015(e): WorkspaceSnapshotPath on failure = %q; want %q",
			result.WorkspaceSnapshotPath, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Best-effort: run-to-completion past sub-step failures
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_BestEffort_ContinuesPastSubStepError verifies
// that a failure in one sub-step does not halt the remaining sub-steps. The
// test injects an error into StopDaemon (sub-step d) and verifies that the
// HandlerCancels (sub-step a) still executes.
func TestSH015_TeardownFixture_BestEffort_ContinuesPastSubStepError(t *testing.T) {
	t.Parallel()

	cancelCalled := false
	params := TeardownParams{
		ScenarioName: "sh015-best-effort",
		HandlerCancels: []func(ctx context.Context) error{
			func(_ context.Context) error { cancelCalled = true; return nil },
		},
		StopDaemon: func(_ context.Context) error {
			return fmt.Errorf("simulated daemon stop failure")
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}
	if !cancelCalled {
		t.Error("SH-015 best-effort: HandlerCancels[0] was not called despite StopDaemon failure; " +
			"teardown MUST continue past sub-step errors")
	}
}

// TestSH015_TeardownFixture_BestEffort_AccumulatesAllErrors verifies that
// errors from multiple sub-steps are accumulated in TeardownError.Errs so
// the caller gets a full picture of what failed.
func TestSH015_TeardownFixture_BestEffort_AccumulatesAllErrors(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-accumulate",
		HandlerCancels: []func(ctx context.Context) error{
			func(_ context.Context) error { return fmt.Errorf("handler 0 failed") },
			func(_ context.Context) error { return fmt.Errorf("handler 1 failed") },
		},
		StopDaemon: func(_ context.Context) error {
			return fmt.Errorf("daemon stop failed")
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}

	// Three sub-steps failed: handler-cancel[0], handler-cancel[1], stop-daemon.
	const wantErrCount = 3
	if len(teardownErr.Errs) != wantErrCount {
		t.Errorf("TeardownError.Errs len = %d; want %d\nerrs: %v",
			len(teardownErr.Errs), wantErrCount, teardownErr.Errs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TeardownError taxonomy
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownError_FailureClassIsCleanupFailed verifies that
// TeardownError.FailureClass() returns FailureClassCleanupFailed, tying the
// error type to §8.8 of the failure taxonomy.
func TestSH015_TeardownError_FailureClassIsCleanupFailed(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-cleanup-failed",
		StopDaemon: func(_ context.Context) error {
			return fmt.Errorf("simulated failure")
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}

	got := teardownErr.FailureClass()
	if got != FailureClassCleanupFailed {
		t.Errorf("TeardownError.FailureClass() = %q; want %q",
			got, FailureClassCleanupFailed)
	}
	if !got.Valid() {
		t.Errorf("TeardownError.FailureClass() = %q is not a valid FailureClass", got)
	}
}

// TestSH015_TeardownError_ErrorStringContainsCleanupFailed verifies that the
// TeardownError.Error() string contains "cleanup-failed" for operator diagnosis.
func TestSH015_TeardownError_ErrorStringContainsCleanupFailed(t *testing.T) {
	t.Parallel()

	params := TeardownParams{
		ScenarioName: "sh015-error-string",
		StopDaemon: func(_ context.Context) error {
			return fmt.Errorf("injected error")
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}

	msg := teardownErr.Error()
	const want = "cleanup-failed"
	if !containsSubstring(msg, want) {
		t.Errorf("TeardownError.Error() = %q; want it to contain %q", msg, want)
	}
}

// TestSH015_TeardownError_UnwrapReturnsAllErrs verifies that
// TeardownError.Unwrap() returns the accumulated sub-step errors so that
// errors.Is and errors.As can traverse the full error chain.
func TestSH015_TeardownError_UnwrapReturnsAllErrs(t *testing.T) {
	t.Parallel()

	sentinel := fmt.Errorf("sentinel-err")
	params := TeardownParams{
		ScenarioName: "sh015-unwrap",
		HandlerCancels: []func(ctx context.Context) error{
			func(_ context.Context) error { return sentinel },
		},
	}

	_, teardownErr := TeardownFixture(t.Context(), params)
	if teardownErr == nil {
		t.Fatal("TeardownFixture: expected TeardownError, got nil")
	}

	if !errors.Is(teardownErr, sentinel) {
		t.Errorf("errors.Is(teardownErr, sentinel) = false; want true (Unwrap must expose sentinel)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-step ordering: (a) before (b) before (c) before (d)
// ─────────────────────────────────────────────────────────────────────────────

// TestSH015_TeardownFixture_OrderABeforeD verifies that HandlerCancels (a) are
// called before StopDaemon (d) by recording the call sequence.
func TestSH015_TeardownFixture_OrderABeforeD(t *testing.T) {
	t.Parallel()

	var seq []string
	params := TeardownParams{
		ScenarioName: "sh015-order",
		HandlerCancels: []func(ctx context.Context) error{
			func(_ context.Context) error { seq = append(seq, "a"); return nil },
		},
		StopDaemon: func(_ context.Context) error { seq = append(seq, "d"); return nil },
	}

	_, err := TeardownFixture(t.Context(), params)
	if err != nil {
		t.Fatalf("TeardownFixture: unexpected error: %v", err)
	}

	if len(seq) != 2 || seq[0] != "a" || seq[1] != "d" {
		t.Errorf("SH-015 sub-step order: got %v; want [a d]", seq)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// containsSubstring is a simple case-sensitive substring check used to avoid
// importing strings in the test file.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
