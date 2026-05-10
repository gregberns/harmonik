package lifecycle

import (
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// TestPL012_BuildImmediateShutdownPayload_Valid verifies that
// BuildImmediateShutdownPayload returns a well-formed payload.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "emit daemon_shutdown{mode=immediate}."
func TestPL012_BuildImmediateShutdownPayload_Valid(t *testing.T) {
	t.Parallel()

	payload, err := BuildImmediateShutdownPayload()
	if err != nil {
		t.Fatalf("PL-012 BuildImmediateShutdownPayload: unexpected error: %v", err)
	}

	if !payload.Valid() {
		t.Errorf("PL-012 BuildImmediateShutdownPayload: payload.Valid() = false; payload = %+v", payload)
	}
}

// TestPL012_BuildImmediateShutdownPayload_ModeIsImmediate verifies that the
// Mode field is set to core.ShutdownModeImmediate.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "The mode is immediate for
// PL-012 (for the interceptable stop --immediate path)."
func TestPL012_BuildImmediateShutdownPayload_ModeIsImmediate(t *testing.T) {
	t.Parallel()

	payload, err := BuildImmediateShutdownPayload()
	if err != nil {
		t.Fatalf("PL-012 mode-immediate: unexpected error: %v", err)
	}

	if payload.Mode != core.ShutdownModeImmediate {
		t.Errorf("PL-012 mode-immediate: Mode = %q, want %q", payload.Mode, core.ShutdownModeImmediate)
	}
}

// TestPL012_BuildImmediateShutdownPayload_ShutdownAtFormat verifies that
// ShutdownAt is a non-empty RFC 3339 timestamp.
//
// Spec ref: process-lifecycle.md §4.4 PL-011a — "shutdown_at is the
// wall-clock time at emission (RFC 3339 with ms)."
func TestPL012_BuildImmediateShutdownPayload_ShutdownAtFormat(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	payload, err := BuildImmediateShutdownPayload()
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("PL-012 shutdown-at: unexpected error: %v", err)
	}

	if payload.ShutdownAt == "" {
		t.Fatal("PL-012 shutdown-at: ShutdownAt is empty")
	}

	parsed, parseErr := time.Parse(time.RFC3339Nano, payload.ShutdownAt)
	if parseErr != nil {
		t.Fatalf("PL-012 shutdown-at: parse RFC3339 %q: %v", payload.ShutdownAt, parseErr)
	}

	if parsed.Before(before) || parsed.After(after) {
		t.Errorf("PL-012 shutdown-at: parsed time %v not in [%v, %v]", parsed, before, after)
	}
}

// TestPL012_BuildImmediateShutdownPayload_MonotonicCompanionPositive verifies
// that ShutdownAtNsSinceBoot is positive.
//
// Spec ref: operator-nfr.md §4.8 ON-033 — monotonic-companion required.
func TestPL012_BuildImmediateShutdownPayload_MonotonicCompanionPositive(t *testing.T) {
	t.Parallel()

	payload, err := BuildImmediateShutdownPayload()
	if err != nil {
		t.Fatalf("PL-012 monotonic: unexpected error: %v", err)
	}

	if payload.ShutdownAtNsSinceBoot == 0 {
		t.Error("PL-012 monotonic: ShutdownAtNsSinceBoot = 0, want > 0")
	}
}

// TestPL012_BuildImmediateShutdownPayload_ShutdownAtIsUTC verifies that
// ShutdownAt is emitted in UTC.
func TestPL012_BuildImmediateShutdownPayload_ShutdownAtIsUTC(t *testing.T) {
	t.Parallel()

	payload, err := BuildImmediateShutdownPayload()
	if err != nil {
		t.Fatalf("PL-012 UTC: unexpected error: %v", err)
	}

	if !strings.HasSuffix(payload.ShutdownAt, "Z") && !strings.HasSuffix(payload.ShutdownAt, "+00:00") {
		t.Errorf("PL-012 UTC: ShutdownAt %q is not UTC (must end with Z or +00:00)", payload.ShutdownAt)
	}
}

// TestPL012_KillSubprocesses_EmptyList verifies that KillSubprocesses returns
// nil for an empty PID list (no subprocesses to kill).
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — "In-flight agent subprocesses
// are killed" (zero in-flight processes is a valid case).
func TestPL012_KillSubprocesses_EmptyList(t *testing.T) {
	t.Parallel()

	if err := KillSubprocesses(nil); err != nil {
		t.Errorf("PL-012 kill-empty: KillSubprocesses(nil): %v", err)
	}
	if err := KillSubprocesses([]int{}); err != nil {
		t.Errorf("PL-012 kill-empty: KillSubprocesses([]int{}): %v", err)
	}
}

// TestPL012_KillSubprocesses_ESRCH_IsNotError verifies that ESRCH (process
// already exited) is treated as a non-error by KillSubprocesses. This covers
// the race where the subprocess exits between the drain-skip and the kill.
//
// Spec ref: process-lifecycle.md §4.4 PL-012 — subprocess kill step; the
// subprocess may have already exited before SIGKILL is sent.
func TestPL012_KillSubprocesses_ESRCH_IsNotError(t *testing.T) {
	t.Parallel()

	// PID 999999 is expected to not exist on most test hosts.
	const deadPID = 999989
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-012 ESRCH: PID %d is live on this host; skipping", deadPID)
	}

	// KillSubprocesses against a dead PID must return nil (ESRCH is tolerated).
	if err := KillSubprocesses([]int{deadPID}); err != nil {
		t.Errorf("PL-012 ESRCH: KillSubprocesses([dead pid]): %v; want nil (ESRCH tolerated)", err)
	}
}
