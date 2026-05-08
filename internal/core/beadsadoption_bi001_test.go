// Package core — BI-001 tests for Beads fork-adoption discipline.
//
// Per beads-integration.md §4.1 BI-001: harmonik MUST adopt the SQLite-backed
// Beads fork (github.com/Dicklesworthstone/beads_rust) as its task-ledger
// dependency; it MUST NOT adopt the Dolt-backed variant; and it MUST NOT fork
// or modify the Beads codebase — Beads is consumed as an external binary only.
//
// These tests enforce that contract at test time by inspecting go.mod, the
// repository directory tree, and the spec text.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot resolves the repository root by walking up from this test file.
// It looks for a go.mod file, mirroring the pattern used by other discipline
// sensors in this package (e.g., beadid_test.go).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repository root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (go.mod) not found — walked to filesystem root")
		}
		dir = parent
	}
}

// TestBI001_GoModHasNoBeadsLibraryDep asserts that go.mod does not list any
// Beads package as a Go library dependency. Beads is a binary dependency only;
// importing it as a Go module would violate BI-001.
func TestBI001_GoModHasNoBeadsLibraryDep(t *testing.T) {
	root := repoRoot(t)
	goMod := filepath.Join(root, "go.mod")

	data, err := os.ReadFile(goMod) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", goMod, err)
	}
	content := string(data)

	forbidden := []string{
		"Dicklesworthstone/beads",
		"dolt",
	}
	for _, pkg := range forbidden {
		if strings.Contains(strings.ToLower(content), strings.ToLower(pkg)) {
			t.Errorf(
				"BI-001 violation: go.mod must not contain a Go library dependency on Beads; "+
					"found prohibited import path fragment %q in go.mod.\n"+
					"Beads is an external binary; it must not appear under `require`.\n"+
					"See beads-integration.md §4.1 BI-001.",
				pkg,
			)
		}
	}
}

// TestBI001_NoForkedBeadsSource asserts that none of the forbidden source
// directories (vendor/beads*, internal/beads*, pkg/beads*) exist in the
// repository. Their presence would indicate that Beads source has been forked
// into harmonik, which is prohibited by BI-001.
func TestBI001_NoForkedBeadsSource(t *testing.T) {
	root := repoRoot(t)

	forbidden := []string{
		filepath.Join(root, "vendor", "beads"),
		filepath.Join(root, "internal", "beads"),
		filepath.Join(root, "pkg", "beads"),
	}

	// Also catch any directory under vendor/Dicklesworthstone or vendor/dolt.
	vendorBeads := []string{
		filepath.Join(root, "vendor", "Dicklesworthstone"),
		filepath.Join(root, "vendor", "dolt"),
	}
	forbidden = append(forbidden, vendorBeads...)

	for _, dir := range forbidden {
		if _, err := os.Stat(dir); err == nil {
			t.Errorf(
				"BI-001 violation: forbidden directory %q exists in the repository.\n"+
					"Beads source must not be forked into harmonik; Beads is consumed "+
					"as an external binary only.\n"+
					"See beads-integration.md §4.1 BI-001.",
				dir,
			)
		}
	}
}

// TestBI001_SpecContainsSQLiteAndDoltDeclaration asserts that the spec text at
// §4.1 BI-001 references both "SQLite" (the adopted fork) and "Dolt" (the
// rejected variant). This guards against accidental removal of the normative
// declaration from the spec.
func TestBI001_SpecContainsSQLiteAndDoltDeclaration(t *testing.T) {
	root := repoRoot(t)
	specFile := filepath.Join(root, "specs", "beads-integration.md")

	data, err := os.ReadFile(specFile) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", specFile, err)
	}
	content := string(data)

	required := []string{"SQLite", "Dolt"}
	for _, phrase := range required {
		if !strings.Contains(content, phrase) {
			t.Errorf(
				"BI-001 spec declaration missing: %q not found in %q.\n"+
					"The spec at §4.1 BI-001 must declare both the adopted SQLite fork "+
					"and the rejected Dolt variant.\n"+
					"See beads-integration.md §4.1 BI-001.",
				phrase,
				specFile,
			)
		}
	}
}
