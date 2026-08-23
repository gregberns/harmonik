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

func brcliFixtureMockBinary(t *testing.T, stdout, stderr string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' %q\nprintf '%%s' %q >&2\nexit %d\n", stdout, stderr, exitCode)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("brcliFixtureMockBinary: write mock: %v", err)
	}
	return path
}

func brcliFixtureEchoArgsBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := "#!/bin/sh\nprintf '%s' \"$*\"\nexit 0\n"
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("brcliFixtureEchoArgsBinary: write mock: %v", err)
	}
	return path
}

func brcliFixtureSleepBinary(t *testing.T, seconds int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nsleep %d\nexit 0\n", seconds)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
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

	time.Sleep(100 * time.Millisecond)
	cancel()

	returnTimeout := 30 * time.Second
	if dl, ok := t.Deadline(); ok {
		if budget := time.Until(dl) - 2*time.Second; budget > 0 && budget < returnTimeout {
			returnTimeout = budget
		}
	}
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
	case <-time.After(returnTimeout):
		t.Fatal("Run did not return promptly after context cancellation")
	}
}

func brcliFixtureEchoArgsToFileBinary(t *testing.T, argsFile string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$*\" > %q\nexit 0\n", argsFile)
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("brcliFixtureEchoArgsToFileBinary: write mock: %v", err)
	}
	return path
}

func brcliFixturePWDBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "br")
	script := "#!/bin/sh\npwd\nexit 0\n"
	//nolint:gosec // G306: mock binary fixture; permissive mode required for executability
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("brcliFixturePWDBinary: write mock: %v", err)
	}
	return path
}

// TestNewForProjectSetsCmdDir verifies that Run sets cmd.Dir to the
// workingDir supplied to NewForProject, so that `br` runs in the project
// directory rather than the daemon's process CWD. Regression guard for
// hk-o1sln.
func TestNewForProjectSetsCmdDir(t *testing.T) {
	path := brcliFixturePWDBinary(t)
	workingDir := t.TempDir()

	adapter, err := brcli.NewForProject(path, workingDir)
	if err != nil {
		t.Fatalf("NewForProject: %v", err)
	}

	result, runErr := adapter.Run(context.Background())
	if runErr != nil {
		t.Fatalf("Run: unexpected error: %v", runErr)
	}
	got := strings.TrimRight(string(result.Stdout), "\n")
	wantReal, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(workingDir): %v", err)
	}
	gotReal, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(got): %v", err)
	}
	if gotReal != wantReal {
		t.Errorf("cmd.Dir not set: subprocess pwd = %q; want %q", gotReal, wantReal)
	}
}

// TestNewForProjectRejectsEmptyWorkingDir verifies that NewForProject
// returns an error when workingDir is empty (hk-o1sln).
func TestNewForProjectRejectsEmptyWorkingDir(t *testing.T) {
	adapter, err := brcli.NewForProject("/path/to/br", "")
	if err == nil {
		t.Fatal("expected error for empty workingDir, got nil")
	}
	if adapter != nil {
		t.Fatal("expected nil Adapter on error, got non-nil")
	}
}

// TestNewForProjectRejectsEmptyBrPath verifies that NewForProject
// returns an error when brPath is empty (hk-o1sln).
func TestNewForProjectRejectsEmptyBrPath(t *testing.T) {
	adapter, err := brcli.NewForProject("", "/some/dir")
	if err == nil {
		t.Fatal("expected error for empty brPath, got nil")
	}
	if adapter != nil {
		t.Fatal("expected nil Adapter on error, got non-nil")
	}
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
