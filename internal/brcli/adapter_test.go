package brcli_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
)

// brcliFixtureMockBinary writes a shell script that prints stdout/stderr and
// exits with the given code. The returned path is valid for the duration of
// the test (t.TempDir is used for cleanup).
//
// The binary is written with mode 0755 for executability; the gosec G306
// finding is suppressed because this is a test fixture, not production data.
func brcliFixtureMockBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nprintf '%%s' %q >&2\nexit %d\n", stdout, stderr, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureMockBinary: write mock: %v", err)
	}
	return path
}

// brcliFixtureEchoArgsBinary writes a shell script that prints all received
// arguments to stdout (space-separated) and exits 0. Used to verify that
// higher-level adapter methods forward the expected flags to `br`.
func brcliFixtureEchoArgsBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// "$*" expands all positional parameters space-separated.
	script := "#!/bin/sh\nprintf '%s' \"$*\"\nexit 0\n"
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureEchoArgsBinary: write mock: %v", err)
	}
	return path
}

// brcliFixtureSleepBinary writes a shell script that sleeps for the given
// number of seconds then exits 0. Used for context-cancellation tests.
func brcliFixtureSleepBinary(t *testing.T, seconds int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nsleep %d\nexit 0\n", seconds)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureSleepBinary: write mock: %v", err)
	}
	return path
}

func TestNewRejectsEmptyPath(t *testing.T) {
	adapter, err := brcli.New("")
	if err == nil {
		t.Fatal("expected error for empty brPath, got nil")
	}
	if adapter != nil {
		t.Fatal("expected nil Adapter on error, got non-nil")
	}
}

func TestNewAcceptsValidPath(t *testing.T) {
	adapter, err := brcli.New("/path/to/br")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil Adapter, got nil")
	}
}

func TestRunCapturesStdout(t *testing.T) {
	path := brcliFixtureMockBinary(t, "hello stdout", "", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if string(result.Stdout) != "hello stdout" {
		t.Errorf("Stdout = %q; want %q", string(result.Stdout), "hello stdout")
	}
}

func TestRunCapturesStderr(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "hello stderr", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if string(result.Stderr) != "hello stderr" {
		t.Errorf("Stderr = %q; want %q", string(result.Stderr), "hello stderr")
	}
}

func TestRunReportsExitCode(t *testing.T) {
	path := brcliFixtureMockBinary(t, "", "", 1)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: unexpected error for non-zero exit: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode = %d; want 1", result.ExitCode)
	}
}

func TestRunReportsExitCodeZero(t *testing.T) {
	path := brcliFixtureMockBinary(t, "ok", "", 0)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := adapter.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d; want 0", result.ExitCode)
	}
}

func TestRunReturnsErrorOnMissingBinary(t *testing.T) {
	adapter, err := brcli.New("/nonexistent/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestRunPropagatesContextCancellation(t *testing.T) {
	path := brcliFixtureSleepBinary(t, 5)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, runErr := adapter.Run(ctx)
		done <- runErr
	}()

	// Cancel after a short delay to let the subprocess start.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}

// brcliFixtureEchoArgsToFileBinary writes a shell script that records all
// received arguments (space-separated) to argsFile and exits 0. Used to spy
// on the exact argument list forwarded to the mock binary by higher-level
// adapter methods, without going through the methods' JSON-parsing layer.
func brcliFixtureEchoArgsToFileBinary(t *testing.T, argsFile string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	// Write all positional args to argsFile; print nothing to stdout so that
	// higher-level callers (ShowBead) receive empty output and produce a
	// parse error — which is expected and asserted in the test.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\nexit 0\n", argsFile)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("brcliFixtureEchoArgsToFileBinary: write mock: %v", err)
	}
	return path
}

// TestRunFormatJSONAppendsFlag verifies that the BI-025b JSON-mode discipline
// is wired end-to-end: commands routed through runFormatJSON (ShowBead,
// ListDependencies) receive --format json as the last two arguments to `br`.
//
// The test calls ShowBead with a spy binary that records its argument list to a
// temp file. ShowBead returns a BrSchemaMismatch parse error (expected, because
// the spy writes nothing to stdout). The test then reads the temp file and
// asserts "--format json" was present in the forwarded args. A regression that
// removes runFormatJSON routing from ShowBead would omit "--format json" and
// cause the args-file assertion to fail.
//
// Spec ref: specs/beads-integration.md §4.8a BI-025b.
func TestRunFormatJSONAppendsFlag(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "spy-args.txt")
	path := brcliFixtureEchoArgsToFileBinary(t, argsFile)
	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// ShowBead internally calls runFormatJSON, which must append --format json.
	// The spy binary writes empty stdout, so ShowBead will return a
	// BrSchemaMismatch error — that is expected and asserted below.
	_, showErr := adapter.ShowBead(context.Background(), "hk-test")
	if showErr == nil {
		t.Fatal("expected parse error from ShowBead with spy binary, got nil")
	}
	if !errors.Is(showErr, brcli.BrSchemaMismatch) {
		t.Errorf("expected BrSchemaMismatch parse error; got %v", showErr)
	}

	// Read the spy file and confirm --format json was forwarded.
	//nolint:gosec // G304: argsFile path is constructed from t.TempDir() — test-controlled
	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil {
		t.Fatalf("reading spy args file: %v", readErr)
	}
	got := string(raw)
	if !strings.Contains(got, "--format json") {
		t.Errorf("ShowBead did not forward --format json to br; spy args: %q", got)
	}
}
