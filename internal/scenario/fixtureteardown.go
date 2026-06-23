package scenario

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gregberns/harmonik/internal/workspace"
)

// TeardownParams carries the per-scenario state needed for SH-015 fixture
// teardown. All fields are optional — absent fields cause the corresponding
// sub-step to be skipped, which is always safe (idempotent by absence).
type TeardownParams struct {
	// ScenarioName is the scenario identifier used to compute
	// WorkspaceSnapshotPath (sub-step e, SH-015a). Must match the name used
	// at fixture-setup time so WorkspaceSnapshotPath returns the correct
	// fixture-root-relative path.
	ScenarioName string

	// WorkspacePath is the per-scenario isolated workspace directory (SH-013).
	// Sub-step (b) releases the lease-lock file at
	// WorkspacePath/.harmonik/lease.lock per WM-013b. If empty, sub-step (b)
	// is skipped.
	WorkspacePath string

	// EventLogPath is the absolute path to the per-scenario event-log JSONL
	// file (SH-014). Sub-step (c) opens this file (if it exists), calls Sync
	// to flush pending writes to durable storage, then closes the file
	// descriptor. If empty or the file does not exist, sub-step (c) is a no-op.
	EventLogPath string

	// StopDaemon stops the per-scenario daemon, implementing sub-step (d)'s
	// "daemon stop RPC" obligation of SH-015 and PL-003a.
	//
	// In the in-process harness context, the per-scenario daemon is run by
	// DriveOrchestration inside the same process, controlled by a
	// CancelOnQueueExit cancel function. Callers wrap that cancel as:
	//
	//   StopDaemon: func(ctx context.Context) error { cancelRun(); return nil }
	//
	// If nil, sub-step (d) is a no-op (daemon already exited via
	// CancelOnQueueExit or was never started — both are idempotent outcomes).
	//
	// Spec ref: specs/scenario-harness.md §4.4 SH-015(d),
	//            specs/process-lifecycle.md §4.2 PL-003a.
	StopDaemon func(ctx context.Context) error

	// HandlerCancels are the per-handler subprocess cancel functions for
	// sub-step (a). The harness calls each to terminate still-live handler
	// subprocesses spawned by the orchestration drive, honouring the
	// HC-018 cancellation bounds. ctx is forwarded to each function so that
	// the scenario-level deadline bounds the total cancel wall-clock.
	//
	// In the in-process harness (Substrate=nil, exec.CommandContext), handler
	// processes are children of the daemon goroutine and are typically
	// terminated before DriveOrchestration returns. May be nil or empty; nil
	// entries are skipped. Errors are accumulated but do not halt the remaining
	// cancel calls or sub-steps.
	HandlerCancels []func(ctx context.Context) error
}

// TeardownResult records the observable outputs of a successful or partial
// TeardownFixture call. It is populated on both success and failure paths so
// that the caller can set ScenarioResult fields regardless of sub-step errors.
type TeardownResult struct {
	// WorkspaceSnapshotPath is the fixture-root-relative path to the
	// post-teardown worktree directory per SH-015a and WorkspaceSnapshotPath.
	// Populated on all paths, including failure, because sub-step (e) is a
	// pure recording obligation that does not touch the filesystem.
	WorkspaceSnapshotPath string
}

// TeardownError accumulates all sub-step errors from a best-effort teardown
// pass. Its presence signals failure_class=cleanup-failed per §8.8.
//
// Per §8.0 precedence, cleanup-failed (precedence rank 8, lowest) never
// overwrites a prior fail/timeout/pass verdict; it is appended to error_detail.
type TeardownError struct {
	// Errs lists the sub-step errors in execution order. Each error is
	// prefixed with the sub-step label ("(a) handler-cancel[N]", "(b)", etc.)
	// for operator diagnosis.
	Errs []error
}

// Error implements the error interface.
func (e *TeardownError) Error() string {
	msgs := make([]string, len(e.Errs))
	for i, err := range e.Errs {
		msgs[i] = err.Error()
	}
	return fmt.Sprintf("cleanup-failed: %d sub-step error(s): %s",
		len(e.Errs), strings.Join(msgs, "; "))
}

// Unwrap returns all accumulated sub-step errors so that errors.Is and
// errors.As can traverse the full error tree.
func (e *TeardownError) Unwrap() []error { return e.Errs }

// FailureClass returns FailureClassCleanupFailed, tying TeardownError to the
// §8.8 failure taxonomy.
func (e *TeardownError) FailureClass() FailureClass {
	return FailureClassCleanupFailed
}

// TeardownFixture executes the five ordered sub-steps of the fixture teardown
// phase declared in specs/scenario-harness.md §4.4 SH-015:
//
//	(a) Terminate any still-live handler subprocesses (HandlerCancels).
//	(b) Release the per-scenario worktree lease per WM-013b.
//	(c) Fsync then close the event-log file.
//	(d) Stop the per-scenario daemon per PL-003a (StopDaemon).
//	(e) Record the workspace_snapshot_path (pure: no filesystem mutation).
//
// Teardown is run-to-completion best-effort: a failure in any sub-step is
// accumulated into the returned *TeardownError but MUST NOT halt the remaining
// sub-steps. All five sub-steps are always attempted.
//
// Teardown is idempotent: calling TeardownFixture twice with the same params
// is a no-op on already-completed sub-steps:
//   - sub-step (a): cancel functions are idempotent (context cancels, etc.)
//   - sub-step (b): ReleaseLeaseLock treats ENOENT as success
//   - sub-step (c): ENOENT on EventLogPath is a no-op
//   - sub-step (d): calling a nil-guarded stop function a second time is a no-op
//   - sub-step (e): pure formula; always returns the same snapshot path
//
// ctx is forwarded to StopDaemon and HandlerCancels so that the scenario-level
// deadline bounds their wall-clock. Sub-steps (b) and (c) are fast local I/O
// operations that do not need per-operation context checking.
//
// Returns (result, nil) on full success, or (result, *TeardownError) when any
// sub-step failed. result.WorkspaceSnapshotPath is populated on all paths.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-015.
// Tags: mechanism
func TeardownFixture(ctx context.Context, params TeardownParams) (TeardownResult, *TeardownError) {
	var errs []error
	accumulate := func(label string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("sub-step %s: %w", label, err))
		}
	}

	// Sub-step (a): terminate still-live handler subprocesses per HC-018.
	// Each HandlerCancel is called independently; errors accumulate without
	// halting the remaining cancel calls.
	for i, cancel := range params.HandlerCancels {
		if cancel != nil {
			accumulate(fmt.Sprintf("(a) handler-cancel[%d]", i), cancel(ctx))
		}
	}

	// Sub-step (b): release the per-scenario worktree lease per WM-013b.
	// workspace.ReleaseLeaseLock treats ENOENT as success (idempotent release:
	// a second release call against an already-released workspace succeeds).
	if params.WorkspacePath != "" {
		lockPath := workspace.LeaseLockPath(params.WorkspacePath)
		accumulate("(b) lease-release", workspace.ReleaseLeaseLock(lockPath))
	}

	// Sub-step (c): fsync then close the event-log file.
	// ENOENT is not an error — the daemon may not have created the file (scenario
	// failed before the daemon started writing events) or a prior teardown pass
	// already handled it. Both cases are idempotent by absence.
	if params.EventLogPath != "" {
		accumulate("(c) event-log-fsync-close", fsyncCloseEventLog(params.EventLogPath))
	}

	// Sub-step (d): stop the per-scenario daemon via the daemon stop RPC per
	// PL-003a. In the in-process harness, StopDaemon wraps the CancelOnQueueExit
	// cancel function from DriveOrchestration. ctx is forwarded to bound the
	// graceful drain per [operator-nfr.md §4.7].
	if params.StopDaemon != nil {
		accumulate("(d) stop-daemon", params.StopDaemon(ctx))
	}

	// Sub-step (e): record the workspace_snapshot_path per SH-015a.
	// This is a recording obligation (pure path formula), NOT a termination
	// action. The worktree files and refs are unmodified by sub-steps (a)-(d);
	// any merge-back to integration occurred during orchestration (WM-019),
	// BEFORE teardown.
	result := TeardownResult{
		WorkspaceSnapshotPath: WorkspaceSnapshotPath(params.ScenarioName),
	}

	if len(errs) == 0 {
		return result, nil
	}
	return result, &TeardownError{Errs: errs}
}

// fsyncCloseEventLog opens the event-log file at path, calls Sync to flush
// pending writes to durable storage, then closes the file descriptor.
//
// ENOENT is treated as a no-op (idempotent: the file was not created because
// the scenario failed before the daemon started, or it was handled by a prior
// teardown pass).
//
// Spec ref: specs/scenario-harness.md §4.4 SH-015(c).
func fsyncCloseEventLog(path string) error {
	//nolint:gosec // G304: path is derived from fixture root + known relative constants, not user input
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // idempotent: file not created or already gone
		}
		return fmt.Errorf("open %q: %w", path, err)
	}
	if syncErr := f.Sync(); syncErr != nil {
		_ = f.Close()
		return fmt.Errorf("sync %q: %w", path, syncErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("close %q: %w", path, closeErr)
	}
	return nil
}
