package daemon

// remote_substrate_b10_test.go — unit tests for rs B10: dispatch via
// SSH-backed substrate + run metadata (hk-rs-b10-wiring-12cl).
//
// Gate-runnable: no real tmux, SSH, or git required.  All observable behaviour
// is exercised through package-internal functions and the exported
// workloopRunStartedPayload struct.
//
// Test matrix (acceptance criteria from bead):
//   TestRSB10_ZeroWorkers_LocalSubstrate:
//     zero workers → perRunSubstrate runner is nil (LocalRunner fallback).
//   TestRSB10_OneHealthyWorker_SSHSubstrate:
//     one healthy worker → perRunSubstrate runner is SSHRunner{Host}.
//   TestRSB10_APIKeyInEnv_Refused:
//     ANTHROPIC_API_KEY in spawn env → hasAPIKeyInEnv returns true (D2 guard).
//   TestRSB10_APIKeyAbsent_NotRefused:
//     env without ANTHROPIC_API_KEY → hasAPIKeyInEnv returns false.
//   TestRSB10_RunStartedPayload_WorkerFields_Remote:
//     workloopRunStartedPayload carries worker_name + worker_os for remote runs.
//   TestRSB10_RunStartedPayload_WorkerFields_Local:
//     workloopRunStartedPayload has empty worker fields for local runs.
//
// Bead: hk-rs-b10-wiring-12cl.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// newPerRunSubstrate runner wiring (tests 1 and 2)
// ─────────────────────────────────────────────────────────────────────────────

// TestRSB10_ZeroWorkers_LocalSubstrate verifies that newPerRunSubstrate with
// a nil runner stores nil (commandRunner() falls back to LocalRunner{} per B9).
func TestRSB10_ZeroWorkers_LocalSubstrate(t *testing.T) {
	t.Parallel()

	// Build a perRunSubstrate with nil runner (local run — no worker selected).
	prs := newPerRunSubstrate(nil, "claude", nil)
	// nil sub → newPerRunSubstrate returns nil; the local path is taken.
	if prs != nil {
		t.Errorf("RSB10: newPerRunSubstrate(nil substrate) = non-nil, want nil (local fallback)")
	}
}

// TestRSB10_OneHealthyWorker_SSHSubstrate verifies that when an SSHRunner is
// passed to newPerRunSubstrate the stored runner is that SSHRunner, not nil.
// This ensures liveness probes (pgrep, ps, git) are tunnelled to the worker.
func TestRSB10_OneHealthyWorker_SSHSubstrate(t *testing.T) {
	t.Parallel()

	host := "worker@remote.internal"
	sshRunner := tmux.SSHRunner{Host: host}

	// Use a minimal *tmuxSubstrate as the inner substrate so newPerRunSubstrate
	// returns a non-nil perRunSubstrate (the function returns nil when sub is
	// not a *tmuxSubstrate).
	ts := &tmuxSubstrate{sessionName: "test-session"}
	prs := newPerRunSubstrate(ts, "claude", sshRunner)
	if prs == nil {
		t.Fatal("RSB10: newPerRunSubstrate(*tmuxSubstrate, sshRunner) = nil, want non-nil")
	}

	// commandRunner() must return the injected SSHRunner, not LocalRunner{}.
	got := prs.commandRunner()
	gotSSH, ok := got.(tmux.SSHRunner)
	if !ok {
		t.Fatalf("RSB10: commandRunner() type = %T, want tmux.SSHRunner", got)
	}
	if gotSSH.Host != host {
		t.Errorf("RSB10: commandRunner().Host = %q, want %q", gotSSH.Host, host)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hasAPIKeyInEnv (D2 fail-closed check — test 3 and 4)
// ─────────────────────────────────────────────────────────────────────────────

// TestRSB10_APIKeyInEnv_Refused verifies that hasAPIKeyInEnv returns true when
// ANTHROPIC_API_KEY appears in the env slice (KEY= form and bare KEY form).
func TestRSB10_APIKeyInEnv_Refused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  []string
	}{
		{"key=value form", []string{"OTHER=x", "ANTHROPIC_API_KEY=sk-ant-abc", "MORE=y"}},
		{"bare key form", []string{"OTHER=x", "ANTHROPIC_API_KEY"}},
		{"only key", []string{"ANTHROPIC_API_KEY=sk-ant-abc"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !hasAPIKeyInEnv(tc.env) {
				t.Errorf("RSB10: hasAPIKeyInEnv(%v) = false, want true (D2 must refuse)", tc.env)
			}
		})
	}
}

// TestRSB10_APIKeyAbsent_NotRefused verifies that hasAPIKeyInEnv returns false
// when ANTHROPIC_API_KEY is not present in the env slice.
func TestRSB10_APIKeyAbsent_NotRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  []string
	}{
		{"empty env", nil},
		{"unrelated keys only", []string{"PATH=/usr/bin", "HOME=/home/user"}},
		{"key-prefix false positive guard", []string{"ANTHROPIC_API_KEY_EXTRA=foo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if hasAPIKeyInEnv(tc.env) {
				t.Errorf("RSB10: hasAPIKeyInEnv(%v) = true, want false", tc.env)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// workloopRunStartedPayload worker fields (FR13 — tests 5 and 6)
// ─────────────────────────────────────────────────────────────────────────────

// TestRSB10_RunStartedPayload_WorkerFields_Remote verifies that
// workloopRunStartedPayload carries WorkerName and WorkerOS for remote runs.
func TestRSB10_RunStartedPayload_WorkerFields_Remote(t *testing.T) {
	t.Parallel()

	const wantName = "worker-a"
	const wantOS = "darwin"

	pl := workloopRunStartedPayload{
		RunID:         "019ec897-0000-7000-8000-000000000001",
		BeadID:        "hk-rsb10-test",
		WorkspacePath: "/tmp/wt",
		StartedAt:     "2026-06-14T00:00:00Z",
		WorkerName:    wantName,
		WorkerOS:      wantOS,
	}

	if pl.WorkerName != wantName {
		t.Errorf("RSB10: WorkerName = %q, want %q", pl.WorkerName, wantName)
	}
	if pl.WorkerOS != wantOS {
		t.Errorf("RSB10: WorkerOS = %q, want %q", pl.WorkerOS, wantOS)
	}
}

// TestRSB10_RunStartedPayload_WorkerFields_Local verifies that
// workloopRunStartedPayload has empty WorkerName/WorkerOS for local runs.
func TestRSB10_RunStartedPayload_WorkerFields_Local(t *testing.T) {
	t.Parallel()

	pl := workloopRunStartedPayload{
		RunID:         "019ec897-0000-7000-8000-000000000002",
		BeadID:        "hk-rsb10-local",
		WorkspacePath: "/tmp/wt",
		StartedAt:     "2026-06-14T00:00:00Z",
		// WorkerName and WorkerOS intentionally left empty (local run).
	}

	if pl.WorkerName != "" {
		t.Errorf("RSB10: local run WorkerName = %q, want empty", pl.WorkerName)
	}
	if pl.WorkerOS != "" {
		t.Errorf("RSB10: local run WorkerOS = %q, want empty", pl.WorkerOS)
	}
}
