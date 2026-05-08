// Tests for ResolveLaunchPath (HC-042) and ResolveTwinSearchPath (SH-009).
//
// This file provides requirement-traceable test coverage for the
// handler-launch path-resolution discipline.  Every ResolveLaunchPath test
// name contains "HC042" and every ResolveTwinSearchPath test name contains
// "SH009" so that CI can grep for coverage of the specific requirements.
package handler

import (
	"errors"
	"os/exec"
	"testing"
)

// TestResolveLaunchPath_HC042_RepoRelativeNonSystem verifies that a non-system
// handler with a relative binaryRef is joined to repoRoot without consulting
// PATH, per HC-042: "all other handlers MUST NOT use $PATH."
func TestResolveLaunchPath_HC042_RepoRelativeNonSystem(t *testing.T) {
	t.Parallel()

	got, err := ResolveLaunchPath("/repo", "bin/handler", false)
	if err != nil {
		t.Fatalf("ResolveLaunchPath: unexpected error: %v", err)
	}
	want := "/repo/bin/handler"
	if got != want {
		t.Errorf("ResolveLaunchPath = %q, want %q", got, want)
	}
}

// TestResolveLaunchPath_HC042_SystemBareNameUsesPATH verifies that a system
// handler with a bare name (no path separator) resolves via exec.LookPath,
// per HC-042: "a handler whose agent_type declaration carries system_handler=true
// MAY resolve via $PATH."
//
// The test uses "sh" as the bare name because it is universally present on
// Unix-like systems.  Replace with any other known-present binary if needed.
func TestResolveLaunchPath_HC042_SystemBareNameUsesPATH(t *testing.T) {
	t.Parallel()

	// "sh" is present on all Unix platforms; use it as a proxy for any
	// operator-installed system handler (e.g. the Claude Code CLI).
	const binaryRef = "sh"

	expected, err := exec.LookPath(binaryRef)
	if err != nil {
		t.Skipf("skipping: %q not found via PATH: %v", binaryRef, err)
	}

	got, gotErr := ResolveLaunchPath("/repo", binaryRef, true)
	if gotErr != nil {
		t.Fatalf("ResolveLaunchPath: unexpected error: %v", gotErr)
	}
	if got != expected {
		t.Errorf("ResolveLaunchPath = %q, want exec.LookPath result %q", got, expected)
	}
}

// TestResolveLaunchPath_HC042_EmptyBinaryRefIsError verifies that an empty
// binaryRef returns ErrLaunchPathMissing regardless of the systemHandler flag,
// per HC-042: "MUST fail launch if the configured absolute path is absent."
func TestResolveLaunchPath_HC042_EmptyBinaryRefIsError(t *testing.T) {
	t.Parallel()

	for _, systemHandler := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-system", true: "system"}[systemHandler], func(t *testing.T) {
			t.Parallel()
			_, err := ResolveLaunchPath("/repo", "", systemHandler)
			if !errors.Is(err, ErrLaunchPathMissing) {
				t.Errorf("ResolveLaunchPath with empty binaryRef: got %v, want ErrLaunchPathMissing", err)
			}
		})
	}
}

// TestResolveLaunchPath_HC042_NonSystemBareNameDoesNotUsePATH verifies that a
// non-system handler with a bare name (no separator) is joined to repoRoot
// rather than resolved via PATH, per HC-042: "all other handlers MUST NOT use
// $PATH."
//
// Even though "claude" (or any bare name) could be found on PATH, the
// function must NOT consult PATH and MUST return a repo-rooted path.
func TestResolveLaunchPath_HC042_NonSystemBareNameDoesNotUsePATH(t *testing.T) {
	t.Parallel()

	got, err := ResolveLaunchPath("/repo", "claude", false)
	if err != nil {
		t.Fatalf("ResolveLaunchPath: unexpected error: %v", err)
	}
	// Spell out the expected path rather than using filepath.Join with a
	// hardcoded absolute prefix, to satisfy gocritic's filepathJoin rule.
	const repoRoot = "/repo"
	want := repoRoot + "/claude"
	if got != want {
		t.Errorf("ResolveLaunchPath = %q, want %q (no PATH lookup for non-system handlers)", got, want)
	}
}

// TestResolveLaunchPath_HC042_NonSystemAbsolutePathIsError verifies that a
// non-system handler with an already-absolute binaryRef is rejected with
// ErrLaunchPathMissing, per HC-042: the path MUST be resolved from the
// repo-relative prefix — an absolute direct path bypasses that contract.
func TestResolveLaunchPath_HC042_NonSystemAbsolutePathIsError(t *testing.T) {
	t.Parallel()

	_, err := ResolveLaunchPath("/repo", "/usr/local/bin/handler", false)
	if !errors.Is(err, ErrLaunchPathMissing) {
		t.Errorf("ResolveLaunchPath with absolute binaryRef for non-system handler: got %v, want ErrLaunchPathMissing", err)
	}
}

// TestResolveLaunchPath_HC042_SystemAbsolutePathReturnedAsIs verifies that a
// system handler with an already-absolute binaryRef is returned unchanged,
// per HC-042: the operator has pinned an explicit location for the system
// handler binary (e.g., a non-standard Claude Code CLI install path).
func TestResolveLaunchPath_HC042_SystemAbsolutePathReturnedAsIs(t *testing.T) {
	t.Parallel()

	const abs = "/usr/local/bin/claude"
	got, err := ResolveLaunchPath("/repo", abs, true)
	if err != nil {
		t.Fatalf("ResolveLaunchPath: unexpected error: %v", err)
	}
	if got != abs {
		t.Errorf("ResolveLaunchPath = %q, want %q (absolute path for system handler returned as-is)", got, abs)
	}
}

// TestResolveLaunchPath_HC042_SystemRelativePathWithSeparator verifies that a
// system handler with a relative binaryRef containing a path separator is
// resolved against repoRoot, treating it as a repo-relative path.  This
// covers the branch where the operator pins a relative (not bare-name) path
// for a system handler.
func TestResolveLaunchPath_HC042_SystemRelativePathWithSeparator(t *testing.T) {
	t.Parallel()

	got, err := ResolveLaunchPath("/repo", "vendor/claude/claude", true)
	if err != nil {
		t.Fatalf("ResolveLaunchPath: unexpected error: %v", err)
	}
	const repoRoot = "/repo"
	want := repoRoot + "/vendor/claude/claude"
	if got != want {
		t.Errorf("ResolveLaunchPath = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// ResolveTwinSearchPath — scenario-harness.md SH-009
// ---------------------------------------------------------------------------

// TestResolveTwinSearchPath_SH009_CLIOverrideWins verifies that a non-empty
// cliOverride is returned as-is, taking precedence over both the environment
// variable and the in-tree default per SH-009 precedence tier (i).
//
// Note: t.Setenv requires no t.Parallel — env mutation is not goroutine-safe.
func TestResolveTwinSearchPath_SH009_CLIOverrideWins(t *testing.T) {
	t.Setenv(EnvTwinSearchPath, "/env/twins")

	const cliPath = "/cli/twins"
	got := ResolveTwinSearchPath(cliPath, "/repo")
	if got != cliPath {
		t.Errorf("ResolveTwinSearchPath = %q, want CLI override %q", got, cliPath)
	}
}

// TestResolveTwinSearchPath_SH009_EnvVarOverrideWins verifies that when
// cliOverride is empty, the HARMONIK_TWIN_SEARCH_PATH env var is returned per
// SH-009 precedence tier (ii).
func TestResolveTwinSearchPath_SH009_EnvVarOverrideWins(t *testing.T) {
	const envPath = "/env/twins"
	t.Setenv(EnvTwinSearchPath, envPath)

	got := ResolveTwinSearchPath("", "/repo")
	if got != envPath {
		t.Errorf("ResolveTwinSearchPath = %q, want env-var override %q", got, envPath)
	}
}

// TestResolveTwinSearchPath_SH009_InTreeDefaultApplied verifies that when
// both cliOverride and the env var are empty, the function returns
// <repoRoot>/twins/ per SH-009 precedence tier (iii).
func TestResolveTwinSearchPath_SH009_InTreeDefaultApplied(t *testing.T) {
	t.Setenv(EnvTwinSearchPath, "") // ensure env is absent

	const repoRoot = "/repo"
	got := ResolveTwinSearchPath("", repoRoot)
	want := repoRoot + "/" + DefaultTwinDirName
	if got != want {
		t.Errorf("ResolveTwinSearchPath = %q, want in-tree default %q", got, want)
	}
}

// TestResolveTwinSearchPath_SH009_EmptyEnvFallsThrough verifies that an
// explicitly-set-but-empty env var is treated as unset, falling through to the
// in-tree default per SH-009 ("non-empty" requirement).
func TestResolveTwinSearchPath_SH009_EmptyEnvFallsThrough(t *testing.T) {
	t.Setenv(EnvTwinSearchPath, "") // empty string must not win

	const repoRoot = "/repo"
	got := ResolveTwinSearchPath("", repoRoot)
	want := repoRoot + "/" + DefaultTwinDirName
	if got != want {
		t.Errorf("ResolveTwinSearchPath with empty env: got %q, want in-tree default %q", got, want)
	}
}
