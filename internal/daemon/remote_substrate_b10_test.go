package daemon

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestRSB10_ZeroWorkers_LocalSubstrate verifies that newPerRunSubstrate with
// a nil runner stores nil (commandRunner() falls back to LocalRunner{} per B9).
func TestRSB10_ZeroWorkers_LocalSubstrate(t *testing.T) {
	t.Parallel()

	prs := newPerRunSubstrate(nil, "claude", nil)
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

	ts := &tmuxSubstrate{sessionName: "test-session"}
	prs := newPerRunSubstrate(ts, "claude", sshRunner)
	if prs == nil {
		t.Fatal("RSB10: newPerRunSubstrate(*tmuxSubstrate, sshRunner) = nil, want non-nil")
	}

	got := prs.commandRunner()
	gotSSH, ok := got.(tmux.SSHRunner)
	if !ok {
		t.Fatalf("RSB10: commandRunner() type = %T, want tmux.SSHRunner", got)
	}
	if gotSSH.Host != host {
		t.Errorf("RSB10: commandRunner().Host = %q, want %q", gotSSH.Host, host)
	}
}

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

// TestRSB10_RunStartedPayload_WorkerFields_Remote verifies that
// RunStartedPayload carries WorkerName and WorkerOS for remote runs.
func TestRSB10_RunStartedPayload_WorkerFields_Remote(t *testing.T) {
	t.Parallel()

	wantName := "worker-a"
	wantOS := "darwin"

	pl := core.RunStartedPayload{
		WorkerName: &wantName,
		WorkerOS:   &wantOS,
	}

	if pl.WorkerName == nil || *pl.WorkerName != wantName {
		t.Errorf("RSB10: WorkerName = %v, want %q", pl.WorkerName, wantName)
	}
	if pl.WorkerOS == nil || *pl.WorkerOS != wantOS {
		t.Errorf("RSB10: WorkerOS = %v, want %q", pl.WorkerOS, wantOS)
	}
}

// TestRSB10_RunStartedPayload_WorkerFields_Local verifies that
// RunStartedPayload has explicit null WorkerName/WorkerOS for local runs.
func TestRSB10_RunStartedPayload_WorkerFields_Local(t *testing.T) {
	t.Parallel()

	pl := core.RunStartedPayload{}

	if pl.WorkerName != nil {
		t.Errorf("RSB10: local run WorkerName = %v, want null", pl.WorkerName)
	}
	if pl.WorkerOS != nil {
		t.Errorf("RSB10: local run WorkerOS = %v, want null", pl.WorkerOS)
	}
}
