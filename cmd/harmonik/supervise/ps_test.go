package supervisecmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func TestBuildPsResult_PrintsCanonicalSignaturesAndSessions(t *testing.T) {
	dir := t.TempDir()

	result, err := buildPsResult(dir)
	if err != nil {
		t.Fatalf("buildPsResult: %v", err)
	}

	if result.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", result.SchemaVersion)
	}
	if result.ProjectHash != projectHashForRealDir(result.ProjectDir) {
		t.Fatalf("ProjectHash = %q, want hash of %q", result.ProjectHash, result.ProjectDir)
	}

	processes := map[string]ProcessSignature{}
	for _, sig := range result.ProcessSignatures {
		processes[sig.Name] = sig
	}
	for _, name := range []string{"supervisor-shim", "daemon", "keeper-fallback", "supervise-fallback"} {
		sig, ok := processes[name]
		if !ok {
			t.Fatalf("missing process signature %q in %#v", name, result.ProcessSignatures)
		}
		if !strings.Contains(sig.Pattern, result.ProjectDir) {
			t.Errorf("%s pattern %q does not include canonical project dir %q", name, sig.Pattern, result.ProjectDir)
		}
		if !strings.HasPrefix(sig.Command, "pgrep -af ") {
			t.Errorf("%s command = %q, want pgrep -af command", name, sig.Command)
		}
	}

	sessions := map[string]TmuxSessionTarget{}
	for _, sess := range result.TmuxSessions {
		sessions[sess.Name] = sess
	}
	wantSessions := map[string]string{
		"flywheel":               FlywheelSessionName(result.ProjectDir),
		"auto-revive-supervisor": ltmux.SupervisorSessionName(result.ProjectDir),
		"daemon-default":         ltmux.DefaultSessionName(result.ProjectDir),
		"keeper-fallback":        "hk-" + result.ProjectHash + "-keeper",
		"supervise-fallback":     "hk-" + result.ProjectHash + "-daemon-supervise",
	}
	for name, want := range wantSessions {
		got, ok := sessions[name]
		if !ok {
			t.Fatalf("missing tmux session %q in %#v", name, result.TmuxSessions)
		}
		if got.Session != want {
			t.Errorf("%s session = %q, want %q", name, got.Session, want)
		}
		if !strings.Contains(got.Command, want) {
			t.Errorf("%s command = %q, want it to contain session %q", name, got.Command, want)
		}
	}
}

func TestRunPs_JSON(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer

	code := RunPs([]string{"--project", dir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("RunPs exit = %d, stderr = %s", code, stderr.String())
	}

	var result PsResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal JSON output %q: %v", stdout.String(), err)
	}
	if result.ProjectDir == "" || result.ProjectHash == "" {
		t.Fatalf("expected project_dir and project_hash in JSON, got %#v", result)
	}
	if len(result.ProcessSignatures) == 0 || len(result.TmuxSessions) == 0 {
		t.Fatalf("expected signatures and sessions in JSON, got %#v", result)
	}
}
