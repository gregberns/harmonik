package workspace

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitVersionFixtureFakeBinary writes a shell script at dir/git that prints the
// given version line and exits 0 (or exits 1 if exitCode is non-zero).
//
// The returned path is the directory containing the fake binary; callers MUST
// prepend it to PATH via t.Setenv so that exec.CommandContext picks it up.
//
// Prefixed gitVersionFixture per implementer-protocol helper-prefix discipline
// (bead hk-8mwo.2).
func gitVersionFixtureFakeBinary(t *testing.T, dir, versionLine string, exitCode int) string {
	t.Helper()

	script := "#!/bin/sh\n"
	if exitCode != 0 {
		script += "exit 1\n"
	} else {
		script += "echo '" + versionLine + "'\nexit 0\n"
	}

	binPath := filepath.Join(dir, "git")
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil { //nolint:gosec // G306: executable bit required for fake git binary
		t.Fatalf("gitVersionFixtureFakeBinary: WriteFile %q: %v", binPath, err)
	}
	return dir
}

// gitVersionFixturePrependPath prepends dir to PATH for the duration of the test.
//
// NOTE: t.Setenv is not compatible with t.Parallel. Tests that call this helper
// MUST NOT call t.Parallel.
func gitVersionFixturePrependPath(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
}

// TestParseGitVersion_WellFormedInputs exercises ParseGitVersion with the
// canonical output shapes produced by different git distributions.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002 — "parsing `git --version`".
// The parser MUST tolerate platform suffixes such as "2.34.1.windows.1".
func TestParseGitVersion_WellFormedInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
	}{
		{
			name:      "standard-three-field",
			input:     "git version 2.34.1",
			wantMajor: 2,
			wantMinor: 34,
		},
		{
			name:      "windows-platform-suffix",
			input:     "git version 2.34.1.windows.1",
			wantMajor: 2,
			wantMinor: 34,
		},
		{
			name:      "version-at-minimum-floor",
			input:     "git version 2.34.0",
			wantMajor: 2,
			wantMinor: 34,
		},
		{
			name:      "version-below-minimum",
			input:     "git version 2.33.9",
			wantMajor: 2,
			wantMinor: 33,
		},
		{
			name:      "version-above-minimum",
			input:     "git version 2.45.2",
			wantMajor: 2,
			wantMinor: 45,
		},
		{
			name:      "next-major-version",
			input:     "git version 3.0.0",
			wantMajor: 3,
			wantMinor: 0,
		},
		{
			name:      "trailing-newline",
			input:     "git version 2.39.5\n",
			wantMajor: 2,
			wantMinor: 39,
		},
		{
			name:      "extra-whitespace",
			input:     "  git version 2.40.1  ",
			wantMajor: 2,
			wantMinor: 40,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseGitVersion(tc.input)
			if err != nil {
				t.Fatalf("ParseGitVersion(%q): unexpected error: %v", tc.input, err)
			}
			if got.Major != tc.wantMajor || got.Minor != tc.wantMinor {
				t.Errorf("ParseGitVersion(%q) = %v; want %d.%d",
					tc.input, got, tc.wantMajor, tc.wantMinor)
			}
		})
	}
}

// TestParseGitVersion_MalformedInputs exercises ParseGitVersion with inputs
// that lack the expected "git version " prefix or numeric version fields.
func TestParseGitVersion_MalformedInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{name: "empty-string", input: ""},
		{name: "no-prefix", input: "2.34.1"},
		{name: "wrong-prefix", input: "version 2.34.1"},
		{name: "non-numeric-major", input: "git version abc.34.1"},
		{name: "non-numeric-minor", input: "git version 2.xyz.1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseGitVersion(tc.input)
			if err == nil {
				t.Errorf("ParseGitVersion(%q): expected error; got nil", tc.input)
			}
		})
	}
}

// TestWMENV002_DetectGitVersion_BelowMinimumReturnsErrGitVersionTooOld verifies
// that DetectGitVersion returns ErrGitVersionTooOld when the installed git is
// below the 2.34 floor required by WM-ENV-002.
//
// The three mechanical dependencies that motivate the 2.34 pin are:
//
//  1. WM-019: git merge --strategy=ort (default in git ≥ 2.34; explicit pin
//     prevents silent fallback to `recursive` on older installations).
//  2. for-each-ref trailers token: %(trailers:key=X,valueonly=true) requires
//     git 2.34's expanded trailers format token.
//  3. git worktree repair: stabilized in 2.34 (introduced in 2.30); used by
//     the daemon's worktree-repair recovery path.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002; §8 error class GitVersionTooOld.
//
// NOTE: t.Setenv is not compatible with t.Parallel — this test is sequential.
func TestWMENV002_DetectGitVersion_BelowMinimumReturnsErrGitVersionTooOld(t *testing.T) {
	// Versions strictly below 2.34 that must be rejected.
	oldVersions := []string{
		"git version 2.33.9",
		"git version 2.30.0",
		"git version 2.0.0",
		"git version 1.9.5",
	}

	for _, versionLine := range oldVersions {
		versionLine := versionLine
		t.Run(versionLine, func(t *testing.T) {
			dir := t.TempDir()
			gitVersionFixtureFakeBinary(t, dir, versionLine, 0)
			gitVersionFixturePrependPath(t, dir)

			_, err := DetectGitVersion(t.Context())
			if err == nil {
				t.Fatalf("DetectGitVersion: expected ErrGitVersionTooOld for %q; got nil", versionLine)
			}
			if !errors.Is(err, ErrGitVersionTooOld) {
				t.Errorf("DetectGitVersion: errors.Is(err, ErrGitVersionTooOld) = false; err = %v", err)
			}
		})
	}
}

// TestWMENV002_DetectGitVersion_AtOrAboveMinimumSucceeds verifies that
// DetectGitVersion returns no error for git versions at or above the 2.34 floor.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002.
//
// NOTE: t.Setenv is not compatible with t.Parallel — this test is sequential.
func TestWMENV002_DetectGitVersion_AtOrAboveMinimumSucceeds(t *testing.T) {
	goodVersions := []struct {
		versionLine string
		wantMajor   int
		wantMinor   int
	}{
		{"git version 2.34.0", 2, 34},
		{"git version 2.34.1", 2, 34},
		{"git version 2.34.1.windows.1", 2, 34},
		{"git version 2.45.2", 2, 45},
		{"git version 3.0.0", 3, 0},
	}

	for _, tc := range goodVersions {
		tc := tc
		t.Run(tc.versionLine, func(t *testing.T) {
			dir := t.TempDir()
			gitVersionFixtureFakeBinary(t, dir, tc.versionLine, 0)
			gitVersionFixturePrependPath(t, dir)

			got, err := DetectGitVersion(t.Context())
			if err != nil {
				t.Fatalf("DetectGitVersion(%q): unexpected error: %v", tc.versionLine, err)
			}
			if got.Major != tc.wantMajor || got.Minor != tc.wantMinor {
				t.Errorf("DetectGitVersion(%q) = %v; want %d.%d",
					tc.versionLine, got, tc.wantMajor, tc.wantMinor)
			}
		})
	}
}

// TestWMENV002_DetectGitVersion_ExecFailureReturnsError verifies that
// DetectGitVersion propagates an error when git itself fails (non-zero exit).
//
// NOTE: t.Setenv is not compatible with t.Parallel — this test is sequential.
func TestWMENV002_DetectGitVersion_ExecFailureReturnsError(t *testing.T) {
	dir := t.TempDir()
	gitVersionFixtureFakeBinary(t, dir, "", 1)
	gitVersionFixturePrependPath(t, dir)

	_, err := DetectGitVersion(t.Context())
	if err == nil {
		t.Fatal("DetectGitVersion: expected error when git exits non-zero; got nil")
	}
	// Must NOT be ErrGitVersionTooOld — the failure class is exec, not version.
	if errors.Is(err, ErrGitVersionTooOld) {
		t.Errorf("DetectGitVersion: got ErrGitVersionTooOld for exec failure; want a different error")
	}
}

// TestWMENV002_OrtMergeStrategyRationale documents the WM-019 motivation for
// the 2.34 pin: --strategy=ort is the default only from git 2.34 onward. This
// test is a pure contract test — it verifies that the minimum constants encode
// the correct pin, not that git is installed.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002 (i) and WM-019 squash merge.
func TestWMENV002_OrtMergeStrategyRationale(t *testing.T) {
	t.Parallel()

	// Any version below 2.34 must NOT meet minimum.
	below := GitVersion{Major: 2, Minor: 33}
	if below.meetsMinimum() {
		t.Errorf("GitVersion{2, 33}.meetsMinimum() = true; want false (--strategy=ort is not default below 2.34)")
	}

	// 2.34 exactly must meet minimum — ort is the default from this version.
	at := GitVersion{Major: 2, Minor: 34}
	if !at.meetsMinimum() {
		t.Errorf("GitVersion{2, 34}.meetsMinimum() = false; want true (ort becomes default at 2.34)")
	}
}

// TestWMENV002_ForEachRefTrailersRationale documents the for-each-ref trailers
// format token motivation for the 2.34 pin. This is a contract test verifying
// that git 2.33 is rejected and git 2.34 is accepted.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002 (ii): "git for-each-ref
// --format '%(trailers:key=X,valueonly=true)' requires git 2.34's expanded
// trailers format token".
func TestWMENV002_ForEachRefTrailersRationale(t *testing.T) {
	t.Parallel()

	// git 2.33: %(trailers:key=X,valueonly=true) not available — must be rejected.
	priorToTrailersExpansion := GitVersion{Major: 2, Minor: 33}
	if priorToTrailersExpansion.meetsMinimum() {
		t.Errorf("GitVersion{2, 33}: meetsMinimum() = true; want false (trailers format token absent before 2.34)")
	}

	// git 2.34: expanded trailers token available — must be accepted.
	atTrailersExpansion := GitVersion{Major: 2, Minor: 34}
	if !atTrailersExpansion.meetsMinimum() {
		t.Errorf("GitVersion{2, 34}: meetsMinimum() = false; want true (trailers format token available at 2.34)")
	}
}

// TestWMENV002_WorktreeRepairRationale documents the git worktree repair
// stabilization motivation for the 2.34 pin.
//
// Spec ref: workspace-model.md §4.a WM-ENV-002 (iii): "git worktree repair
// (introduced in 2.30 but stabilized in 2.34) is the supported recovery path".
func TestWMENV002_WorktreeRepairRationale(t *testing.T) {
	t.Parallel()

	// git 2.30–2.33: worktree repair introduced but not stabilized — must be rejected.
	introduced := GitVersion{Major: 2, Minor: 30}
	if introduced.meetsMinimum() {
		t.Errorf("GitVersion{2, 30}: meetsMinimum() = true; want false (worktree repair not stabilized until 2.34)")
	}

	// git 2.34: worktree repair stabilized — must be accepted.
	stabilized := GitVersion{Major: 2, Minor: 34}
	if !stabilized.meetsMinimum() {
		t.Errorf("GitVersion{2, 34}: meetsMinimum() = false; want true (worktree repair stabilized at 2.34)")
	}
}

// TestWMENV002_ErrGitVersionTooOld_ClassString verifies that the error class
// string for ErrGitVersionTooOld is "GitVersionTooOld" per §8.
func TestWMENV002_ErrGitVersionTooOld_ClassString(t *testing.T) {
	t.Parallel()

	if got := Class(ErrGitVersionTooOld); got != "GitVersionTooOld" {
		t.Errorf("Class(ErrGitVersionTooOld) = %q; want %q", got, "GitVersionTooOld")
	}

	// A wrapped ErrGitVersionTooOld must also classify correctly.
	wrapped := fmt.Errorf("outer: %w", ErrGitVersionTooOld)
	if got := Class(wrapped); got != "GitVersionTooOld" {
		t.Errorf("Class(wrapped ErrGitVersionTooOld) = %q; want %q", got, "GitVersionTooOld")
	}
}

// TestWMENV002_DetectGitVersion_LiveGit runs DetectGitVersion against the real
// system git (if available) and verifies it returns a parseable, meeting-minimum
// version. This test is skipped if git is not in PATH.
//
// Spec ref: workspace-model.md §10 (testing obligations) — "Git-version
// detection at startup tested with … current-git (starts cleanly)."
func TestWMENV002_DetectGitVersion_LiveGit(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH; skipping live git version test")
	}

	v, err := DetectGitVersion(t.Context())
	if err != nil {
		t.Fatalf("DetectGitVersion (live git): unexpected error: %v", err)
	}
	if v.Major == 0 && v.Minor == 0 {
		t.Errorf("DetectGitVersion (live git): returned zero version %v", v)
	}
	t.Logf("live git version: %v", v)
}
