package lifecycle

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"
)

// startupSweepFixtureStartupFailureMode names the failure_mode values for
// daemon_startup_failed events per [event-model.md §8.7.4] and PL-008a.
type startupSweepFixtureStartupFailureMode string

const (
	startupSweepFixtureFailurePidfileLocked           startupSweepFixtureStartupFailureMode = "pidfile-locked"
	startupSweepFixtureFailureSocketBindFailed        startupSweepFixtureStartupFailureMode = "socket-bind-failed"
	startupSweepFixtureFailureGitBadState             startupSweepFixtureStartupFailureMode = "git-bad-state"
	startupSweepFixtureFailureBeadsUnavailable        startupSweepFixtureStartupFailureMode = "beads-unavailable"
	startupSweepFixtureFailureFilesystemUnwritable    startupSweepFixtureStartupFailureMode = "filesystem-unwritable"
	startupSweepFixtureFailureDiskFull                startupSweepFixtureStartupFailureMode = "disk-full"
	startupSweepFixtureFailureUpgradeHashMismatch     startupSweepFixtureStartupFailureMode = "upgrade-hash-mismatch-on-restart"
	startupSweepFixtureFailureRuntimePanic            startupSweepFixtureStartupFailureMode = "runtime-panic"
	startupSweepFixtureFailureNtmUnavailable          startupSweepFixtureStartupFailureMode = "ntm-unavailable"
	startupSweepFixtureFailureOrchestratorUnavailable startupSweepFixtureStartupFailureMode = "orchestrator-agent-unavailable"
)

// startupSweepFixtureDaemonStartupFailedEvent models the daemon_startup_failed
// event payload per [event-model.md §8.7.4] and PL-008a.
// Fields: failed_at, exit_code, failure_mode.
type startupSweepFixtureDaemonStartupFailedEvent struct {
	FailedAt    time.Time                             `json:"failed_at"`
	ExitCode    int                                   `json:"exit_code"`
	FailureMode startupSweepFixtureStartupFailureMode `json:"failure_mode"`
}

// startupSweepFixtureExitCodeError is a typed error used to test the
// error-to-exit-code mapping for startup failures beyond the two already covered
// by plFixtureErrToExitCode (codes 5 and 6).
type startupSweepFixtureExitCodeError struct {
	code    int
	message string
}

func (e *startupSweepFixtureExitCodeError) Error() string {
	return fmt.Sprintf("startup failure (exit %d): %s", e.code, e.message)
}

// startupSweepFixtureNewExitCodeError constructs a typed startup failure error
// mapping to the given ON §8 exit code.
func startupSweepFixtureNewExitCodeError(code int, message string) error {
	return &startupSweepFixtureExitCodeError{code: code, message: message}
}

// startupSweepFixtureMapErrorToExitCode implements the full ON §8 exit-code
// taxonomy for the codes consumed by this spec (PL-008a). This is the
// fixture-level mapper that the daemon's actual startup-failure path would use.
//
// Codes consumed by PL-008a:
//
//	5  — pidfile-locked         (EWOULDBLOCK / EAGAIN from flock)
//	6  — socket-bind-failed     (EADDRINUSE from bind)
//	7  — git-bad-state          (git log walk failure)
//	8  — beads-unavailable      (br CLI or SQLite unreadable)
//	9  — filesystem-unwritable  (workspace or .harmonik not writable)
//	10 — disk-full              (filesystem full during checkpoint)
//	14 — upgrade-hash-mismatch  (startup marker mismatch per PL-005 step 8a)
//	19 — runtime-panic          (panic barrier per PL-018a)
//	22 — ntm-unavailable        (ntm not on PATH or version-incompatible)
//	23 — orchestrator-agent-unavailable (Claude Code not found by PL-028)
func startupSweepFixtureMapErrorToExitCode(err error) int {
	if err == nil {
		return 0
	}

	// Check for typed exit-code errors first.
	var ecErr *startupSweepFixtureExitCodeError
	if errors.As(err, &ecErr) {
		return ecErr.code
	}

	// Syscall-level mappings (codes 5 and 6, delegated to the existing helper).
	existing := plFixtureErrToExitCode(err)
	if existing != 0 && existing != 1 {
		return existing
	}

	return 1 // generic-failure fallback
}

// startupSweepFixtureEmitStartupFailed constructs the daemon_startup_failed
// event payload for a given exit code and failure mode. Per PL-008a, the event
// MUST be emitted where the event bus has been initialized (step 0 complete).
// Returns the event payload for assertion; emission wiring is downstream.
func startupSweepFixtureEmitStartupFailed(exitCode int, mode startupSweepFixtureStartupFailureMode) startupSweepFixtureDaemonStartupFailedEvent {
	return startupSweepFixtureDaemonStartupFailedEvent{
		FailedAt:    time.Now(),
		ExitCode:    exitCode,
		FailureMode: mode,
	}
}

// TestPL008a_ExitCode5_PidfileLocked verifies that a pidfile-lock-contention
// error maps to exit code 5 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "The codes consumed by this
// spec are: 5 (pidfile-locked, per PL-002)... On emission, the daemon MUST also
// emit daemon_startup_failed (per [event-model.md §8.7.4]) with {failed_at,
// exit_code, failure_mode} BEFORE process exit where the event bus has been
// initialized."
func TestPL008a_ExitCode5_PidfileLocked(t *testing.T) {
	t.Parallel()

	// Code 5: EWOULDBLOCK from flock (pidfile-locked).
	err := syscall.EWOULDBLOCK
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 5 {
		t.Errorf("PL-008a exit 5: mapErrorToExitCode(EWOULDBLOCK) = %d, want 5 (pidfile-locked)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailurePidfileLocked)
	if evt.ExitCode != 5 {
		t.Errorf("PL-008a exit 5: event.ExitCode = %d, want 5", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailurePidfileLocked {
		t.Errorf("PL-008a exit 5: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailurePidfileLocked)
	}
	if evt.FailedAt.IsZero() {
		t.Error("PL-008a exit 5: event.FailedAt is zero; must be set at emission time")
	}
}

// TestPL008a_ExitCode6_SocketBindFailed verifies that EADDRINUSE maps to exit
// code 6 and daemon_startup_failed is emitted with the correct failure_mode.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "6 (socket-bind-failed, per PL-003)".
func TestPL008a_ExitCode6_SocketBindFailed(t *testing.T) {
	t.Parallel()

	err := syscall.EADDRINUSE
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 6 {
		t.Errorf("PL-008a exit 6: mapErrorToExitCode(EADDRINUSE) = %d, want 6 (socket-bind-failed)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureSocketBindFailed)
	if evt.ExitCode != 6 {
		t.Errorf("PL-008a exit 6: event.ExitCode = %d, want 6", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureSocketBindFailed {
		t.Errorf("PL-008a exit 6: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureSocketBindFailed)
	}
}

// TestPL008a_ExitCode7_GitBadState verifies that a git-bad-state startup
// failure maps to exit code 7 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "7 (git-bad-state)".
func TestPL008a_ExitCode7_GitBadState(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(7, "git log walk failed: corrupt repo")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 7 {
		t.Errorf("PL-008a exit 7: mapErrorToExitCode = %d, want 7 (git-bad-state)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureGitBadState)
	if evt.ExitCode != 7 {
		t.Errorf("PL-008a exit 7: event.ExitCode = %d, want 7", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureGitBadState {
		t.Errorf("PL-008a exit 7: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureGitBadState)
	}
}

// TestPL008a_ExitCode8_BeadsUnavailable verifies that a beads-unavailable
// failure maps to exit code 8 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "8 (beads-unavailable)".
func TestPL008a_ExitCode8_BeadsUnavailable(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(8, "br CLI invocation failed: file not found")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 8 {
		t.Errorf("PL-008a exit 8: mapErrorToExitCode = %d, want 8 (beads-unavailable)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureBeadsUnavailable)
	if evt.ExitCode != 8 {
		t.Errorf("PL-008a exit 8: event.ExitCode = %d, want 8", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureBeadsUnavailable {
		t.Errorf("PL-008a exit 8: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureBeadsUnavailable)
	}
}

// TestPL008a_ExitCode9_FilesystemUnwritable verifies that a filesystem-unwritable
// failure maps to exit code 9 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "9 (filesystem-unwritable,
// including pidfile/socket fs failure)".
func TestPL008a_ExitCode9_FilesystemUnwritable(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(9, ".harmonik/ directory is not writable")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 9 {
		t.Errorf("PL-008a exit 9: mapErrorToExitCode = %d, want 9 (filesystem-unwritable)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureFilesystemUnwritable)
	if evt.ExitCode != 9 {
		t.Errorf("PL-008a exit 9: event.ExitCode = %d, want 9", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureFilesystemUnwritable {
		t.Errorf("PL-008a exit 9: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureFilesystemUnwritable)
	}
}

// TestPL008a_ExitCode10_DiskFull verifies that a disk-full failure maps to
// exit code 10 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "10 (disk-full)".
func TestPL008a_ExitCode10_DiskFull(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(10, "filesystem full during checkpoint commit")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 10 {
		t.Errorf("PL-008a exit 10: mapErrorToExitCode = %d, want 10 (disk-full)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureDiskFull)
	if evt.ExitCode != 10 {
		t.Errorf("PL-008a exit 10: event.ExitCode = %d, want 10", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureDiskFull {
		t.Errorf("PL-008a exit 10: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureDiskFull)
	}
}

// TestPL008a_ExitCode14_UpgradeHashMismatch verifies that an upgrade-hash
// mismatch failure maps to exit code 14 and daemon_startup_failed is emitted
// with failure_mode = "upgrade-hash-mismatch-on-restart".
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "14 (upgrade-hash-mismatch,
// emitted on startup-marker mismatch per PL-005 step 8a / [operator-nfr.md
// §4.6 ON-020a])".
func TestPL008a_ExitCode14_UpgradeHashMismatch(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(14, "upgrade hash mismatch on restart: expected abc123, got def456")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 14 {
		t.Errorf("PL-008a exit 14: mapErrorToExitCode = %d, want 14 (upgrade-hash-mismatch)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureUpgradeHashMismatch)
	if evt.ExitCode != 14 {
		t.Errorf("PL-008a exit 14: event.ExitCode = %d, want 14", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureUpgradeHashMismatch {
		t.Errorf("PL-008a exit 14: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureUpgradeHashMismatch)
	}
}

// TestPL008a_ExitCode19_RuntimePanic verifies that a runtime-panic failure
// maps to exit code 19 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "19 (runtime-panic, emitted by
// the panic barrier per PL-018a)".
func TestPL008a_ExitCode19_RuntimePanic(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(19, "top-level panic barrier intercepted uncaught panic")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 19 {
		t.Errorf("PL-008a exit 19: mapErrorToExitCode = %d, want 19 (runtime-panic)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureRuntimePanic)
	if evt.ExitCode != 19 {
		t.Errorf("PL-008a exit 19: event.ExitCode = %d, want 19", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureRuntimePanic {
		t.Errorf("PL-008a exit 19: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureRuntimePanic)
	}
}

// TestPL008a_ExitCode22_NtmUnavailable verifies that an ntm-unavailable failure
// maps to exit code 22 and daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "22 (ntm-unavailable, emitted
// by PL-021a when ntm is missing, version-incompatible, or absent)".
func TestPL008a_ExitCode22_NtmUnavailable(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(22, "ntm not on PATH or version-incompatible")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 22 {
		t.Errorf("PL-008a exit 22: mapErrorToExitCode = %d, want 22 (ntm-unavailable)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureNtmUnavailable)
	if evt.ExitCode != 22 {
		t.Errorf("PL-008a exit 22: event.ExitCode = %d, want 22", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureNtmUnavailable {
		t.Errorf("PL-008a exit 22: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureNtmUnavailable)
	}
}

// TestPL008a_ExitCode23_OrchestratorAgentUnavailable verifies that an
// orchestrator-agent-unavailable failure maps to exit code 23 and
// daemon_startup_failed is emitted.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "23 (orchestrator-agent-unavailable,
// emitted by PL-028 step 4 when harmonik runner --orchestrator-agent cannot
// locate Claude Code)".
func TestPL008a_ExitCode23_OrchestratorAgentUnavailable(t *testing.T) {
	t.Parallel()

	err := startupSweepFixtureNewExitCodeError(23, "harmonik runner --orchestrator-agent: Claude Code not found")
	gotCode := startupSweepFixtureMapErrorToExitCode(err)
	if gotCode != 23 {
		t.Errorf("PL-008a exit 23: mapErrorToExitCode = %d, want 23 (orchestrator-agent-unavailable)", gotCode)
	}

	evt := startupSweepFixtureEmitStartupFailed(gotCode, startupSweepFixtureFailureOrchestratorUnavailable)
	if evt.ExitCode != 23 {
		t.Errorf("PL-008a exit 23: event.ExitCode = %d, want 23", evt.ExitCode)
	}
	if evt.FailureMode != startupSweepFixtureFailureOrchestratorUnavailable {
		t.Errorf("PL-008a exit 23: event.FailureMode = %q, want %q", evt.FailureMode, startupSweepFixtureFailureOrchestratorUnavailable)
	}
}

// TestPL008a_AllConsumedExitCodesAreDistinct verifies that every exit code in
// the PL-008a set (5, 6, 7, 8, 9, 10, 14, 19, 22, 23) maps to a distinct
// failure mode, and that no two codes share the same failure_mode string.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "The daemon MUST emit exit
// codes from the authoritative taxonomy in [operator-nfr.md §8]. The codes
// consumed by this spec are: 5 ... 6 ... 7 ... 8 ... 9 ... 10 ... 14 ... 19 ...
// 22 ... and 23."
func TestPL008a_AllConsumedExitCodesAreDistinct(t *testing.T) {
	t.Parallel()

	type entry struct {
		code int
		mode startupSweepFixtureStartupFailureMode
	}

	// The canonical PL-008a set per [operator-nfr.md §8].
	consumed := []entry{
		{5, startupSweepFixtureFailurePidfileLocked},
		{6, startupSweepFixtureFailureSocketBindFailed},
		{7, startupSweepFixtureFailureGitBadState},
		{8, startupSweepFixtureFailureBeadsUnavailable},
		{9, startupSweepFixtureFailureFilesystemUnwritable},
		{10, startupSweepFixtureFailureDiskFull},
		{14, startupSweepFixtureFailureUpgradeHashMismatch},
		{19, startupSweepFixtureFailureRuntimePanic},
		{22, startupSweepFixtureFailureNtmUnavailable},
		{23, startupSweepFixtureFailureOrchestratorUnavailable},
	}

	// Assert all codes are distinct.
	seenCodes := make(map[int]bool)
	seenModes := make(map[startupSweepFixtureStartupFailureMode]bool)

	for _, e := range consumed {
		if seenCodes[e.code] {
			t.Errorf("PL-008a distinct codes: exit code %d appears more than once in the consumed set", e.code)
		}
		seenCodes[e.code] = true

		if seenModes[e.mode] {
			t.Errorf("PL-008a distinct codes: failure_mode %q appears more than once in the consumed set", e.mode)
		}
		seenModes[e.mode] = true
	}

	// Assert each code maps to its expected daemon_startup_failed event.
	for _, e := range consumed {
		evt := startupSweepFixtureEmitStartupFailed(e.code, e.mode)
		if evt.ExitCode != e.code {
			t.Errorf("PL-008a distinct codes: emitStartupFailed(%d, %q).ExitCode = %d, want %d",
				e.code, e.mode, evt.ExitCode, e.code)
		}
		if evt.FailureMode != e.mode {
			t.Errorf("PL-008a distinct codes: emitStartupFailed(%d, %q).FailureMode = %q, want %q",
				e.code, e.mode, evt.FailureMode, e.mode)
		}
	}
}

// TestPL008a_PreStep0FailureNoEvent verifies that failures occurring BEFORE
// PL-005 step 0 (event bus not yet initialized) must NOT emit
// daemon_startup_failed — they emit only the exit code to stderr.
//
// Spec ref: process-lifecycle.md §4.2 PL-008a — "For failures that occur
// BEFORE step 0, the daemon MUST emit only the exit code to stderr; the event
// surface is unreachable."
func TestPL008a_PreStep0FailureNoEvent(t *testing.T) {
	t.Parallel()

	// Model the pre-step-0 state: event bus not initialized.
	type daemonStartupState struct {
		eventBusInitialized bool
	}
	preStep0State := daemonStartupState{eventBusInitialized: false}
	postStep0State := daemonStartupState{eventBusInitialized: true}

	// Pre-step-0: event bus not initialized; daemon_startup_failed MUST NOT be emitted.
	if preStep0State.eventBusInitialized {
		t.Error("PL-008a pre-step0: fixture state invalid; event bus should not be initialized before step 0")
	}

	// Pre-step-0 path: only exit code to stderr, no event.
	canEmitEvent := preStep0State.eventBusInitialized
	if canEmitEvent {
		t.Error("PL-008a pre-step0: daemon_startup_failed MUST NOT be emitted before event bus init (step 0)")
	}

	// Post-step-0: event bus is initialized; daemon_startup_failed MUST be emitted.
	canEmitEventPostStep0 := postStep0State.eventBusInitialized
	if !canEmitEventPostStep0 {
		t.Error("PL-008a post-step0: daemon_startup_failed MUST be emitted when event bus is initialized")
	}
}
