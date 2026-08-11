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

// daemonPattern returns the `daemon` process signature that `supervise ps`
// prints for dir, and the resolved project directory it printed alongside it.
// It fails the test when the signature is absent.
func daemonPattern(t *testing.T, dir string) (pattern, realDir string) {
	t.Helper()
	result, err := buildPsResult(dir)
	if err != nil {
		t.Fatalf("buildPsResult: %v", err)
	}
	for _, sig := range result.ProcessSignatures {
		if sig.Name == "daemon" {
			return sig.Pattern, result.ProjectDir
		}
	}
	t.Fatalf("no daemon process signature in %#v", result.ProcessSignatures)
	return "", ""
}

// TestPsDaemonPatternMatchesTheRevivalArgv holds the printed daemon signature
// against the argv the supervisor really spawns.
//
// The verb exists to hand an operator a signature that finds a running daemon.
// A pattern that matches nothing is worse than no pattern at all, because the
// operator reads an empty pgrep result as "no daemon" and starts a second one.
// The old test only asked that the pattern contain the project directory, so it
// stayed green when `harmonik --project DIR` stopped starting anything
// (hk-8fdbe).
//
// buildDaemonCmd is the one function that builds a daemon argv, so this test
// compares the signature against that argv and not against a copy of it. Change
// the spawn verb and this test goes red.
func TestPsDaemonPatternMatchesTheRevivalArgv(t *testing.T) {
	dir := t.TempDir()
	pattern, realDir := daemonPattern(t, dir)

	argv := buildDaemonCmd(realDir, 4)
	if argv == nil {
		t.Fatal("buildDaemonCmd returned nil; cannot resolve the executable")
	}
	// The test binary is not named harmonik. In the field the deployed binary
	// is, and the printed signature names it, so put the deployed name in
	// argv[0] and keep every later word that buildDaemonCmd produced.
	argv[0] = "/usr/local/bin/harmonik"
	cmdline := strings.Join(argv, " ")

	if !strings.Contains(cmdline, pattern) {
		t.Fatalf("`pgrep -f %q` would not find a live daemon.\n  pattern: %s\n  argv:    %s", pattern, pattern, cmdline)
	}

	// Positive evidence that the match is not free: the same pattern must
	// refuse an ordinary CLI call against the same project. Adjacency of the
	// verb and --project is what excludes it.
	cliCall := "/usr/local/bin/harmonik queue submit --project " + realDir + " --bead hk-1"
	if strings.Contains(cliCall, pattern) {
		t.Errorf("pattern %q also matches a plain CLI call %q; it would over-count daemons", pattern, cliCall)
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
