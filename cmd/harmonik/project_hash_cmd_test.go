package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// project_hash_cmd_test.go — unit tests for `harmonik project-hash` (PL-031).
//
// Spec ref: specs/process-lifecycle.md §4.2 PL-031.
// Bead ref: hk-dmw.

// captureProjectHash calls runProjectHashSubcommand and captures its stdout.
// It redirects os.Stdout to a pipe so the function's fmt.Println is captured.
func captureProjectHashOutput(t *testing.T, args []string) (stdout string, exitCode int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	exitCode = runProjectHashSubcommand(args)

	w.Close()
	os.Stdout = old

	var buf [64]byte
	n, _ := r.Read(buf[:])
	return string(buf[:n]), exitCode
}

func TestRunProjectHashSubcommand_DefaultDir(t *testing.T) {
	// Default (no --project): uses current working directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	realWD, err := filepath.EvalSymlinks(wd)
	if err != nil {
		realWD = wd
	}
	want := lifecycle.ComputeProjectHash(realWD).String()

	out, code := captureProjectHashOutput(t, nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := strings.TrimSuffix(out, "\n")
	if got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}
	if len(got) != 12 {
		t.Errorf("hash length = %d, want 12", len(got))
	}
}

func TestRunProjectHashSubcommand_ExplicitDir(t *testing.T) {
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}
	want := lifecycle.ComputeProjectHash(realDir).String()

	out, code := captureProjectHashOutput(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := strings.TrimSuffix(out, "\n")
	if got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}
}

func TestRunProjectHashSubcommand_ExplicitDirEquals(t *testing.T) {
	// --project=DIR form (equals-separated).
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}
	want := lifecycle.ComputeProjectHash(realDir).String()

	out, code := captureProjectHashOutput(t, []string{"--project=" + dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := strings.TrimSuffix(out, "\n")
	if got != want {
		t.Errorf("hash = %q, want %q", got, want)
	}
}

func TestRunProjectHashSubcommand_NonexistentDir(t *testing.T) {
	// Error path: nonexistent directory → exit non-zero, no stdout.
	out, code := captureProjectHashOutput(t, []string{"--project", "/nonexistent-harmonik-test-dir-12345"})
	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for nonexistent directory")
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty on error", out)
	}
}

func TestRunProjectHashSubcommand_OutputFormat(t *testing.T) {
	// Output must be exactly 12 lowercase hex chars + newline.
	dir := t.TempDir()
	out, code := captureProjectHashOutput(t, []string{"--project", dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output %q does not end with newline", out)
	}
	hash := strings.TrimSuffix(out, "\n")
	if len(hash) != 12 {
		t.Errorf("hash length = %d, want 12", len(hash))
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash %q contains non-lowercase-hex char %q", hash, c)
		}
	}
}

func TestRunProjectHashSubcommand_HelpExitsZero(t *testing.T) {
	// --help exits 0. Capture stdout to suppress output in test log.
	old := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	code := runProjectHashSubcommand([]string{"--help"})
	w.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for --help", code)
	}
}
