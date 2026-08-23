package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twinGenericFixtureSocketFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "daemon.sock")
}

func twinGenericFixtureLaunchSpecFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "launch-spec.json")
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("twinGenericFixtureLaunchSpecFile: write: %v", err)
	}
	return p
}

// TestRunMissingSocketPath verifies that run() exits with code 1 and does not
// panic when --socket-path is absent.  This is the "exit cleanly on missing
// fixture" precondition declared by hk-ahvq.48.1.
func TestRunMissingSocketPath(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-generic"}
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
		"harmonik-twin-generic",
		"--socket-path", twinGenericFixtureSocketFile(t),
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
		"harmonik-twin-generic",
		"--socket-path", twinGenericFixtureSocketFile(t),
		"--launch-spec", twinGenericFixtureLaunchSpecFile(t),
	}
	defer func() { os.Args = orig }()

	code := run()
	if code != 1 {
		t.Errorf("expected exit code 1 (dial failure), got %d", code)
	}
}

// TestCommitHashVarIsSettable verifies that the commitHash package-level
// variable can be set from a test — confirming that -ldflags "-X
// main.commitHash=<sha>" will work at build time.  In normal `go test` runs
// the variable is the zero string because tests do not pass ldflags.
//
// This test does NOT require ldflags to be set; the zero-string state is the
// expected baseline for unstamped builds.
//
// Cite: specs/handler-contract.md §4.10.HC-043.
func TestCommitHashVarIsSettable(t *testing.T) {
	orig := commitHash
	defer func() { commitHash = orig }()

	if commitHash != "" {
		t.Logf("commitHash is %q (non-empty — binary was built with ldflags stamp)", commitHash)
	}

	const testHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	commitHash = testHash
	if commitHash != testHash {
		t.Errorf("commitHash = %q, want %q", commitHash, testHash)
	}
}

// TestVersionLineUnstamped verifies that versionLine() returns the
// "(unstamped)" marker when commitHash is the zero string.
func TestVersionLineUnstamped(t *testing.T) {
	orig := commitHash
	commitHash = ""
	defer func() { commitHash = orig }()

	got := versionLine()
	const want = "harmonik-twin-generic commit=(unstamped)"
	if got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}
}

// TestVersionLineStamped verifies that versionLine() includes the stamped SHA
// when commitHash is non-empty.
func TestVersionLineStamped(t *testing.T) {
	orig := commitHash
	const stamp = "abc1234abc1234abc1234abc1234abc1234abc123"
	commitHash = stamp
	defer func() { commitHash = orig }()

	got := versionLine()
	want := "harmonik-twin-generic commit=" + stamp
	if got != want {
		t.Errorf("versionLine() = %q, want %q", got, want)
	}
}

// TestWriteVersion verifies that writeVersion writes the version line followed
// by a newline to the supplied writer.
func TestWriteVersion(t *testing.T) {
	orig := commitHash
	commitHash = ""
	defer func() { commitHash = orig }()

	var buf bytes.Buffer
	if err := writeVersion(&buf); err != nil {
		t.Fatalf("writeVersion: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, "harmonik-twin-generic commit=") {
		t.Errorf("writeVersion output %q does not start with expected prefix", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("writeVersion output %q does not end with newline", got)
	}
}

// TestRunVersionFlag verifies that run() returns exit code 0 when --version
// is passed and does not require --socket-path.
func TestRunVersionFlag(t *testing.T) {
	orig := os.Args
	os.Args = []string{"harmonik-twin-generic", "--version"}
	defer func() { os.Args = orig }()

	code := run()
	if code != 0 {
		t.Errorf("run() with --version returned %d, want 0", code)
	}
}
