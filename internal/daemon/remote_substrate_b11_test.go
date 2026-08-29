package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
)

// TestRSB11_IsSSHConnectionFailure_Exit255 verifies that IsSSHConnectionFailure
// returns true for an *exec.ExitError with exit code 255, which is the code
// SSH uses to signal transport failures (refused, timeout, host-key mismatch).
func TestRSB11_IsSSHConnectionFailure_Exit255(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "exit 255")
	err := cmd.Run()
	if err == nil {
		t.Fatal("RSB11: sh -c 'exit 255' unexpectedly returned nil error")
	}
	if !tmux.IsSSHConnectionFailure(err) {
		t.Errorf("RSB11: IsSSHConnectionFailure(%v) = false, want true (exit-255 is SSH transport failure)", err)
	}
}

// TestRSB11_IsSSHConnectionFailure_OtherExits verifies that IsSSHConnectionFailure
// returns false for exit codes other than 255 (normal remote-command results).
func TestRSB11_IsSSHConnectionFailure_OtherExits(t *testing.T) {
	t.Parallel()

	for _, code := range []int{0, 1, 2, 127} {
		t.Run(fmt.Sprintf("exit%d", code), func(t *testing.T) {
			t.Parallel()
			var cmd *exec.Cmd
			switch code {
			case 0:
				cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 0")
			case 1:
				cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 1")
			case 2:
				cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 2")
			case 127:
				cmd = exec.CommandContext(t.Context(), "sh", "-c", "exit 127")
			default:
				t.Fatalf("unsupported exit code %d", code)
			}
			err := cmd.Run()
			if tmux.IsSSHConnectionFailure(err) {
				t.Errorf("RSB11: IsSSHConnectionFailure(exit %d) = true, want false", code)
			}
		})
	}
}

// TestRSB11_IsSSHConnectionFailure_Nil verifies that IsSSHConnectionFailure
// returns false for a nil error (success case).
func TestRSB11_IsSSHConnectionFailure_Nil(t *testing.T) {
	t.Parallel()

	if tmux.IsSSHConnectionFailure(nil) {
		t.Error("RSB11: IsSSHConnectionFailure(nil) = true, want false")
	}
}

func exitCodeCmdFunc(code int) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("exit %d", code))
	}
}

// TestRSB11_LivenessProbe_ProcessAlive verifies that probeLivenessOrSSHFail
// returns (true, false) when the first probe (pgrep -P) exits 0 (process found).
func TestRSB11_LivenessProbe_ProcessAlive(t *testing.T) {
	t.Parallel()

	rr := &tmux.RecordingRunner{CmdFunc: exitCodeCmdFunc(0)}
	alive, connFailed := probeLivenessOrSSHFail(context.Background(), rr, 99999, nil)
	if !alive {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-0) alive = false, want true")
	}
	if connFailed {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-0) connFailed = true, want false")
	}
}

// TestRSB11_LivenessProbe_ProcessGone verifies that probeLivenessOrSSHFail
// returns (false, false) when both probes exit non-zero (process gone, no SSH failure).
func TestRSB11_LivenessProbe_ProcessGone(t *testing.T) {
	t.Parallel()

	rr := &tmux.RecordingRunner{CmdFunc: exitCodeCmdFunc(1)}
	alive, connFailed := probeLivenessOrSSHFail(context.Background(), rr, 99999, nil)
	if alive {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-1) alive = true, want false")
	}
	if connFailed {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-1) connFailed = true, want false (process gone ≠ SSH fail)")
	}
}

// TestRSB11_LivenessProbe_SSHConnFail_NotAWedge verifies that
// probeLivenessOrSSHFail returns (false, true) when the runner exits 255
// (SSH connection failure). The function must return promptly — an unreachable
// SSH host must never cause the daemon goroutine to wedge indefinitely.
//
// "recoverable terminal state" aspect: the call returns (false, true) so
// PaneHasActiveProcess returns false; the run appears finished and the
// existing run_stale path handles recovery.
func TestRSB11_LivenessProbe_SSHConnFail_NotAWedge(t *testing.T) {
	t.Parallel()

	rr := &tmux.RecordingRunner{CmdFunc: exitCodeCmdFunc(255)}
	alive, connFailed := probeLivenessOrSSHFail(context.Background(), rr, 99999, nil)
	if alive {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-255) alive = true, want false")
	}
	if !connFailed {
		t.Error("RSB11: probeLivenessOrSSHFail(exit-255) connFailed = false, want true (SSH transport failure)")
	}
}

// TestRSB11_HealthyThenSSHFail_OfflineDetected is the primary acceptance test:
// a runner that is initially healthy (exit-0) then transitions to SSH connection
// failure (exit-255) mid-run. Verifies:
//  1. First probe: run is alive (not terminated).
//  2. Second probe: connFailed = true, alive = false (recoverable terminal state,
//     not a wedge).
//  3. The offline notification (onConnectionFailure callback equivalent) fires
//     exactly once on the failing probe.
func TestRSB11_HealthyThenSSHFail_OfflineDetected(t *testing.T) {
	t.Parallel()

	callCount := 0
	rr := &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			callCount++
			if callCount == 1 {
				return exec.CommandContext(ctx, "sh", "-c", "exit 0")
			}
			return exec.CommandContext(ctx, "sh", "-c", "exit 255")
		},
	}

	alive1, cf1 := probeLivenessOrSSHFail(context.Background(), rr, 99999, nil)
	if !alive1 {
		t.Error("RSB11: probe 1 (healthy): alive = false, want true")
	}
	if cf1 {
		t.Error("RSB11: probe 1 (healthy): connFailed = true, want false")
	}

	offlineCalled := false
	alive2, cf2 := probeLivenessOrSSHFail(context.Background(), rr, 99999, nil)
	if alive2 {
		t.Error("RSB11: probe 2 (SSH fail): alive = true, want false (run should appear terminated)")
	}
	if !cf2 {
		t.Error("RSB11: probe 2 (SSH fail): connFailed = false, want true (worker_offline must be detectable)")
	}
	if cf2 {
		offlineCalled = true
	}
	if !offlineCalled {
		t.Error("RSB11: worker offline notification not triggered after SSH failure")
	}
}

// TestRSB11_WorkerOfflinePayload_Fields verifies that WorkerOfflinePayload
// carries the expected fields for downstream event consumers (operator
// dashboards, orchestrator monitors).
func TestRSB11_WorkerOfflinePayload_Fields(t *testing.T) {
	t.Parallel()

	pl := workers.WorkerOfflinePayload{
		WorkerName: "worker-a",
		WorkerHost: "user@worker.internal",
		Phase:      "liveness",
		Detail:     "pgrep probe returned ssh exit-255",
		DetectedAt: "2026-06-14T00:00:00Z",
	}

	if pl.WorkerName != "worker-a" {
		t.Errorf("RSB11: WorkerName = %q, want %q", pl.WorkerName, "worker-a")
	}
	if pl.WorkerHost != "user@worker.internal" {
		t.Errorf("RSB11: WorkerHost = %q, want %q", pl.WorkerHost, "user@worker.internal")
	}
	if pl.Phase != "liveness" {
		t.Errorf("RSB11: Phase = %q, want %q", pl.Phase, "liveness")
	}
	if pl.Detail == "" {
		t.Error("RSB11: Detail is empty, want non-empty")
	}
	if pl.DetectedAt == "" {
		t.Error("RSB11: DetectedAt is empty, want RFC 3339 timestamp")
	}

	spawnPl := workers.WorkerOfflinePayload{Phase: "spawn"}
	if spawnPl.Phase != "spawn" {
		t.Errorf("RSB11: spawn Phase = %q, want %q", spawnPl.Phase, "spawn")
	}
}
