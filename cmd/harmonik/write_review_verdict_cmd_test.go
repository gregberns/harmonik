package main

// write_review_verdict_cmd_test.go — behavior tests for the
// `harmonik write-review-verdict` subcommand (hk-9w79a) and shared
// output-silencing helper for the verdict/gate command cluster.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/workspace"
)

// vgSilenceStd redirects os.Stdout and os.Stderr to /dev/null for the duration
// of the test, restoring them on cleanup. The verdict/gate run* functions print
// usage banners and diagnostics straight to the process streams; silencing keeps
// the test log readable. NOTE: it mutates process globals, so callers must NOT
// use t.Parallel().
func vgSilenceStd(t *testing.T) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open /dev/null: %v", err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devnull, devnull
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = devnull.Close()
	})
}

// readReviewJSON reads and decodes ${projectDir}/.harmonik/review.json.
func readReviewJSON(t *testing.T, projectDir string) workspace.ReviewVerdict {
	t.Helper()
	raw, err := os.ReadFile(workspace.ReviewVerdictPath(projectDir))
	if err != nil {
		t.Fatalf("read review.json: %v", err)
	}
	var v workspace.ReviewVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode review.json: %v", err)
	}
	return v
}

func TestWriteReviewVerdict_ApproveHappyPath(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}

	code := runWriteReviewVerdictSubcommand([]string{
		"--verdict", "APPROVE",
		"--notes", "All checks pass.",
		"--project", dir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	v := readReviewJSON(t, dir)
	if v.SchemaVersion != workspace.ReviewVerdictSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", v.SchemaVersion, workspace.ReviewVerdictSchemaVersion)
	}
	if v.Verdict != workspace.ReviewVerdictApprove {
		t.Errorf("Verdict = %q, want %q", v.Verdict, workspace.ReviewVerdictApprove)
	}
	if v.Notes != "All checks pass." {
		t.Errorf("Notes = %q", v.Notes)
	}
	if len(v.Flags) != 0 {
		t.Errorf("Flags = %v, want empty", v.Flags)
	}
}

func TestWriteReviewVerdict_FlagsParsedTrimmedAndCompacted(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Equals-form flags, with surrounding whitespace and an empty element that
	// must be dropped.
	code := runWriteReviewVerdictSubcommand([]string{
		"--verdict=REQUEST_CHANGES",
		"--flags= missing-tests , , spec-drift ",
		"--notes=No unit tests.",
		"--project=" + dir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	v := readReviewJSON(t, dir)
	if v.Verdict != workspace.ReviewVerdictRequestChanges {
		t.Errorf("Verdict = %q, want REQUEST_CHANGES", v.Verdict)
	}
	want := []string{"missing-tests", "spec-drift"}
	if len(v.Flags) != len(want) {
		t.Fatalf("Flags = %v, want %v", v.Flags, want)
	}
	for i := range want {
		if v.Flags[i] != want[i] {
			t.Errorf("Flags[%d] = %q, want %q", i, v.Flags[i], want[i])
		}
	}
}

func TestWriteReviewVerdict_InvalidVerdict(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	code := runWriteReviewVerdictSubcommand([]string{
		"--verdict", "MAYBE",
		"--notes", "x",
		"--project", dir,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for invalid verdict", code)
	}
}

func TestWriteReviewVerdict_MissingVerdict(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	code := runWriteReviewVerdictSubcommand([]string{
		"--notes", "x",
		"--project", dir,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing verdict", code)
	}
}

func TestWriteReviewVerdict_MissingNotes(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	code := runWriteReviewVerdictSubcommand([]string{
		"--verdict", "BLOCK",
		"--project", dir,
	})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for missing notes", code)
	}
}

func TestWriteReviewVerdict_UnknownFlag(t *testing.T) {
	vgSilenceStd(t)
	code := runWriteReviewVerdictSubcommand([]string{"--bogus"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for unknown flag", code)
	}
}

func TestWriteReviewVerdict_ExtraPositional(t *testing.T) {
	vgSilenceStd(t)
	code := runWriteReviewVerdictSubcommand([]string{"extra-arg"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for extra positional", code)
	}
}

func TestWriteReviewVerdict_Help(t *testing.T) {
	vgSilenceStd(t)
	for _, arg := range []string{"--help", "-h"} {
		if code := runWriteReviewVerdictSubcommand([]string{arg}); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", arg, code)
		}
	}
}

func TestWriteReviewVerdict_BlockValid(t *testing.T) {
	vgSilenceStd(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}
	code := runWriteReviewVerdictSubcommand([]string{
		"--verdict", "BLOCK",
		"--notes", "Critical regression.",
		"--project", dir,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := readReviewJSON(t, dir).Verdict; got != workspace.ReviewVerdictBlock {
		t.Errorf("Verdict = %q, want BLOCK", got)
	}
}
