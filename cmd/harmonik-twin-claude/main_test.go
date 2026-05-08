package main

import (
	"os"
	"path/filepath"
	"testing"
)

// twinClaudeFixture helpers follow the per-bead prefix discipline declared in
// implementer-protocol.md §Helper-prefix discipline. Prefix: twinClaudeFixture.

// twinClaudeFixtureSocketFile creates a temp directory and returns a
// non-existent socket path inside it (the socket is not actually created here;
// the twin binary only dials, the daemon creates it).
func twinClaudeFixtureSocketFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "daemon.sock")
}

// twinClaudeFixtureLaunchSpecFile writes a minimal JSON file to t.TempDir and
// returns its path.  The content is intentionally minimal: LaunchSpec parsing
// is deferred to hk-ahvq.48.2.
func twinClaudeFixtureLaunchSpecFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "launch-spec.json")
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("twinClaudeFixtureLaunchSpecFile: write: %v", err)
	}
	return p
}

// TestRunMissingSocketPath verifies that run() exits with code 1 and does not
// panic when --socket-path is absent.  This is the "exit cleanly on missing
// fixture" precondition declared by hk-ahvq.48.1.
func TestRunMissingSocketPath(t *testing.T) {
	// Override os.Args so flag.Parse sees no arguments.
	orig := os.Args
	os.Args = []string{"harmonik-twin-claude"}
	defer func() { os.Args = orig }()

	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// TestRunLaunchSpecFileMissing verifies that run() exits with code 1 when
// --launch-spec points to a non-existent file.
func TestRunLaunchSpecFileMissing(t *testing.T) {
	orig := os.Args
	os.Args = []string{
		"harmonik-twin-claude",
		"--socket-path", twinClaudeFixtureSocketFile(t),
		"--launch-spec", filepath.Join(t.TempDir(), "does-not-exist.json"),
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

// TestRunLaunchSpecFilePresent verifies that run() proceeds past the
// launch-spec precondition check when the file exists.  The binary will still
// fail with code 1 because no daemon socket is listening, confirming the
// dial-error path.
func TestRunLaunchSpecFilePresent(t *testing.T) {
	orig := os.Args
	os.Args = []string{
		"harmonik-twin-claude",
		"--socket-path", twinClaudeFixtureSocketFile(t),
		"--launch-spec", twinClaudeFixtureLaunchSpecFile(t),
	}
	defer func() { os.Args = orig }()

	// No daemon is listening; expect dial failure → exit code 1.
	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 (dial failure), got %d", code)
	}
}
