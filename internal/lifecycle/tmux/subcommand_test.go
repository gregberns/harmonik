package tmux

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test fixtures (tmuxStart prefix per bead hk-gql20.10)
// ──────────────────────────────────────────────────────────────────────────────

// tmuxStartFixtureEnv returns a minimal env slice that does NOT set $TMUX, but
// does have the fake binDir prepended to PATH so RunTmuxStart can locate tmux.
func tmuxStartFixtureEnv(t *testing.T, binDir string) []string {
	t.Helper()
	orig := os.Getenv("PATH")
	newPath := binDir + string(os.PathListSeparator) + orig
	return []string{"PATH=" + newPath}
}

// tmuxStartFixtureTmuxEnv returns an env slice with $TMUX set to simulate the
// caller already being inside a tmux session.
func tmuxStartFixtureTmuxEnv(t *testing.T, tmuxVal string) []string {
	t.Helper()
	return []string{
		"TMUX=" + tmuxVal,
		"PATH=" + os.Getenv("PATH"),
	}
}

// tmuxStartFixtureProjectDir creates a temporary directory representing the
// project root and returns its absolute path.
func tmuxStartFixtureProjectDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// tmuxStartFixtureFakeTmuxScript writes a fake tmux script to binDir that
// handles the subcommands needed by RunTmuxStart tests.
//
// The script behaviour:
//   - `tmux -V`           → prints "tmux 3.4", exit 0
//   - `tmux new-session`  → exits with newSessionExit; prints newSessionOut to stdout
//   - any other invocation → exits 0 silently
func tmuxStartFixtureFakeTmuxScript(
	t *testing.T,
	binDir string,
	newSessionExit int,
	newSessionOut string,
) {
	t.Helper()

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("case \"$1\" in\n")
	sb.WriteString("  '-V') printf 'tmux 3.4\\n'; exit 0 ;;\n")
	if newSessionOut != "" {
		fmt.Fprintf(&sb, "  'new-session') printf '%%s\\n' %q; exit %d ;;\n", newSessionOut, newSessionExit)
	} else {
		fmt.Fprintf(&sb, "  'new-session') exit %d ;;\n", newSessionExit)
	}
	sb.WriteString("  *) exit 0 ;;\n")
	sb.WriteString("esac\n")

	scriptPath := filepath.Join(binDir, "tmux")
	//nolint:gosec // G306: executable test script, 0755 is intentional
	if err := os.WriteFile(scriptPath, []byte(sb.String()), 0o755); err != nil {
		t.Fatalf("tmuxStartFixtureFakeTmuxScript: WriteFile: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// RunTmuxStart tests
// ──────────────────────────────────────────────────────────────────────────────

// TestRunTmuxStart_TMUXAlreadySet verifies that RunTmuxStart exits 0 with a
// friendly message when $TMUX is already set (operator is inside tmux).
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement step i.
func TestRunTmuxStart_TMUXAlreadySet(t *testing.T) {
	env := tmuxStartFixtureTmuxEnv(t, "/tmp/tmux-12345,0,0")

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		"",
		"",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 0 {
		t.Errorf("RunTmuxStart $TMUX set: exit code = %d, want 0", code)
	}
	if len(execCalled) > 0 {
		t.Errorf("RunTmuxStart $TMUX set: exec was called unexpectedly: %v", execCalled)
	}
	if !strings.Contains(stdout.String(), "already inside") {
		t.Errorf("RunTmuxStart $TMUX set: stdout %q does not contain 'already inside'", stdout.String())
	}
}

// TestRunTmuxStart_CreateAndExec verifies the happy path: $TMUX unset, session
// created, exec called with tmux attach-session.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement steps ii–iv.
// NOTE: uses t.Setenv — not parallel.
func TestRunTmuxStart_CreateAndExec(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	tmuxStartFixtureFakeTmuxScript(t, binDir, 0, "")
	osAdapterFixtureWithFakeTmux(t, binDir) // sets PATH via t.Setenv for OSAdapter calls

	projectDir := tmuxStartFixtureProjectDir(t)
	env := tmuxStartFixtureEnv(t, binDir)

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		projectDir,
		"",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 0 {
		t.Errorf("RunTmuxStart create+exec: exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if len(execCalled) == 0 {
		t.Fatal("RunTmuxStart create+exec: exec was not called")
	}
	// argv[0] is the binary path, argv[1] should be "tmux", argv[2] "attach-session".
	if len(execCalled) < 4 {
		t.Fatalf("RunTmuxStart create+exec: exec argv too short: %v", execCalled)
	}
	if execCalled[1] != "tmux" {
		t.Errorf("RunTmuxStart create+exec: exec argv[1] = %q, want \"tmux\"", execCalled[1])
	}
	if execCalled[2] != "attach-session" {
		t.Errorf("RunTmuxStart create+exec: exec argv[2] = %q, want \"attach-session\"", execCalled[2])
	}
	// Session name must follow -t flag.
	if execCalled[3] != "-t" {
		t.Errorf("RunTmuxStart create+exec: exec argv[3] = %q, want \"-t\"", execCalled[3])
	}
	// Session name must start with harmonik- prefix.
	if len(execCalled) < 5 || !strings.HasPrefix(execCalled[4], "harmonik-") {
		t.Errorf("RunTmuxStart create+exec: session name missing harmonik- prefix: %v", execCalled)
	}
}

// TestRunTmuxStart_TmuxProbeFails verifies that RunTmuxStart exits 22 when the
// tmux probe step fails (binary missing).
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement §5 exit code 22.
// NOTE: uses t.Setenv — not parallel.
func TestRunTmuxStart_TmuxProbeFails(t *testing.T) {
	// Override PATH with an empty dir so OSAdapter.ProbeTmux cannot find tmux.
	emptyBinDir := osAdapterFixtureBinDir(t)
	t.Setenv("PATH", emptyBinDir)

	projectDir := tmuxStartFixtureProjectDir(t)
	env := tmuxStartFixtureEnv(t, emptyBinDir)

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		projectDir,
		"",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 22 {
		t.Errorf("RunTmuxStart probe-fail: exit code = %d, want 22 (stderr: %s)", code, stderr.String())
	}
	if len(execCalled) > 0 {
		t.Errorf("RunTmuxStart probe-fail: exec called unexpectedly: %v", execCalled)
	}
}

// TestRunTmuxStart_SessionNameBadPrefix verifies that RunTmuxStart exits 24
// when --session-name is supplied but lacks the required harmonik- prefix.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement step ii.
// NOTE: prefix validation runs before the tmux probe so no PATH override needed.
func TestRunTmuxStart_SessionNameBadPrefix(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	tmuxStartFixtureFakeTmuxScript(t, binDir, 0, "")

	projectDir := tmuxStartFixtureProjectDir(t)
	env := tmuxStartFixtureEnv(t, binDir) // PATH not critical here; probe never reached

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		projectDir,
		"bad-prefix-session",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 24 {
		t.Errorf("RunTmuxStart bad-prefix: exit code = %d, want 24 (stderr: %s)", code, stderr.String())
	}
	if len(execCalled) > 0 {
		t.Errorf("RunTmuxStart bad-prefix: exec called unexpectedly: %v", execCalled)
	}
}

// TestRunTmuxStart_EnsureSessionFails verifies that RunTmuxStart exits 24 when
// the tmux new-session step fails with an unexpected error.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 refinement §5 exit code 24.
// NOTE: uses t.Setenv — not parallel.
func TestRunTmuxStart_EnsureSessionFails(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	// Fake tmux: -V returns 3.4, new-session fails with unexpected error (exit 2).
	tmuxStartFixtureFakeTmuxScript(t, binDir, 2, "unexpected internal error")
	osAdapterFixtureWithFakeTmux(t, binDir) // sets PATH via t.Setenv for OSAdapter calls

	projectDir := tmuxStartFixtureProjectDir(t)
	env := tmuxStartFixtureEnv(t, binDir)

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		projectDir,
		"",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 24 {
		t.Errorf("RunTmuxStart ensure-fail: exit code = %d, want 24 (stderr: %s)", code, stderr.String())
	}
	if len(execCalled) > 0 {
		t.Errorf("RunTmuxStart ensure-fail: exec called unexpectedly: %v", execCalled)
	}
}

// TestRunTmuxStart_DefaultSessionName verifies that the computed default session
// name follows the harmonik-<hash12>-default pattern.
//
// NOTE: uses t.Setenv — not parallel.
func TestRunTmuxStart_DefaultSessionName(t *testing.T) {
	binDir := osAdapterFixtureBinDir(t)
	tmuxStartFixtureFakeTmuxScript(t, binDir, 0, "")
	osAdapterFixtureWithFakeTmux(t, binDir) // sets PATH via t.Setenv for OSAdapter calls

	projectDir := tmuxStartFixtureProjectDir(t)
	env := tmuxStartFixtureEnv(t, binDir)

	var stdout, stderr bytes.Buffer
	var execCalled []string

	code := RunTmuxStart(
		projectDir,
		"",
		&stdout,
		&stderr,
		SkipExecRecorder(&execCalled),
		env,
	)

	if code != 0 {
		t.Fatalf("RunTmuxStart default-name: exit code = %d (stderr: %s)", code, stderr.String())
	}
	if len(execCalled) < 5 {
		t.Fatalf("RunTmuxStart default-name: exec argv too short: %v", execCalled)
	}
	sessionName := execCalled[4]
	if !strings.HasPrefix(sessionName, "harmonik-") {
		t.Errorf("RunTmuxStart default-name: session %q missing harmonik- prefix", sessionName)
	}
	if !strings.HasSuffix(sessionName, "-default") {
		t.Errorf("RunTmuxStart default-name: session %q missing -default suffix", sessionName)
	}
	// Hash part must be exactly 12 hex chars.
	parts := strings.SplitN(sessionName, "-", 3)
	if len(parts) != 3 {
		t.Fatalf("RunTmuxStart default-name: cannot split session name %q into 3 parts", sessionName)
	}
	hash := parts[1]
	if len(hash) != 12 {
		t.Errorf("RunTmuxStart default-name: hash part %q has len %d, want 12", hash, len(hash))
	}
}
