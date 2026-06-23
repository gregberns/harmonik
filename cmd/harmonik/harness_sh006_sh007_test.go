package main

// harness_sh006_sh007_test.go — contract tests for SH-006 (suite-load phase)
// and SH-007 (byte-lexicographic execution order).
//
// SH-006: harness MUST execute scenario discovery, file parse, schema
// validation, and uniqueness check in a discrete suite-load phase before
// launching any orchestration. A suite-load failure (duplicate name, parse
// error, schema error, wrong extension) MUST abort the suite.
//
// SH-007: within a single suite invocation, scenarios MUST execute in
// byte-lexicographic order of their name field (locale-independent UTF-8 byte
// comparison; NOT file-path order).
//
// Helper prefix: sh006sh007 (per implementer-protocol.md §Helper-prefix discipline).
// Spec ref: specs/scenario-harness.md §4.1 SH-002, §4.2 SH-006, §4.2 SH-007,
//           §4.1 SH-005 (name uniqueness).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/scenario"
)

// sh006sh007MinimalYAML returns a minimal valid scenario YAML that will pass
// ParseScenarioFile. The name and cadence are caller-supplied.
func sh006sh007MinimalYAML(name, cadence string) string {
	return "name: " + name + "\n" +
		"description: test scenario\n" +
		"workflow_path: test.dot\n" +
		"timeout_secs: 30\n" +
		"cadence_tag: " + cadence + "\n"
}

// sh006sh007WriteScenario writes a minimal scenario YAML at the given path,
// creating parent directories as needed.
func sh006sh007WriteScenario(t *testing.T, path, name, cadence string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("sh006sh007WriteScenario: mkdir %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(sh006sh007MinimalYAML(name, cadence)), 0o600); err != nil {
		t.Fatalf("sh006sh007WriteScenario: write %q: %v", path, err)
	}
}

// sh006sh007TempRoot creates a temporary directory for the test and registers
// cleanup with t.Cleanup.
func sh006sh007TempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	return root
}

// TestHarnessDiscoverScenarios_RecursiveYAMLOnlyDiscovery verifies SH-006:
// the suite-load phase discovers .yaml files in arbitrary subdirectories of
// scenarios/ without requiring a flat directory layout.
func TestHarnessDiscoverScenarios_RecursiveYAMLOnlyDiscovery(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	// Place .yaml files at various depths.
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "top.yaml"), "top-scenario", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "sub", "nested.yaml"), "nested-scenario", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "a", "b", "deep.yaml"), "deep-scenario", "smoke")

	got, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 scenarios, got %d: %v", len(got), namesOf(got))
	}
}

// TestHarnessDiscoverScenarios_ByteLexNameOrder_SH007 verifies SH-007:
// scenarios are returned in byte-lexicographic order of their NAME field,
// not in file-path order. The test places files so that path order and name
// order differ.
func TestHarnessDiscoverScenarios_ByteLexNameOrder_SH007(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	// File path order would be: z-dir/... before z-dir/... — but name order
	// must be alpha < beta < zeta regardless of directory prefix.
	//
	// Deliberately pick directory names so that path-sort differs from name-sort:
	//   scenarios/z-dir/file1.yaml → name "alpha"
	//   scenarios/a-dir/file2.yaml → name "beta"
	//   scenarios/m-dir/file3.yaml → name "zeta"
	//
	// File-path order: a-dir/file2 < m-dir/file3 < z-dir/file1 → beta, zeta, alpha
	// Name order:      alpha < beta < zeta → alpha, beta, zeta
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "z-dir", "file1.yaml"), "alpha", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "a-dir", "file2.yaml"), "beta", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "m-dir", "file3.yaml"), "zeta", "smoke")

	got, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 scenarios, got %d", len(got))
	}

	want := []string{"alpha", "beta", "zeta"}
	for i, sf := range got {
		if sf.Name != want[i] {
			t.Errorf("position %d: got name %q, want %q (name order must be byte-lex per SH-007, not file-path order)", i, sf.Name, want[i])
		}
	}
}

// TestHarnessDiscoverScenarios_DuplicateNameDetection_SH005 verifies SH-005 /
// SH-006: duplicate scenario names detected at suite-load abort the suite and
// the error message includes the conflicting file paths.
func TestHarnessDiscoverScenarios_DuplicateNameDetection_SH005(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	pathA := filepath.Join(scenariosDir, "smoke", "first.yaml")
	pathB := filepath.Join(scenariosDir, "regression", "second.yaml")
	sh006sh007WriteScenario(t, pathA, "duplicate-name", "smoke")
	sh006sh007WriteScenario(t, pathB, "duplicate-name", "regression")

	_, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) == 0 {
		t.Fatal("want duplicate-name error, got none")
	}

	// At least one error must mention "duplicate" and include both file names.
	found := false
	for _, e := range errs {
		msg := e.Error()
		if strings.Contains(msg, "duplicate") &&
			strings.Contains(msg, "first.yaml") &&
			strings.Contains(msg, "second.yaml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no error mentions 'duplicate' with both conflicting paths; errors: %v", errs)
	}
}

// TestHarnessDiscoverScenarios_YmlExtensionRejected_SH002 verifies SH-002:
// files with .yml extension found during discovery are rejected as
// scenario-load-failure and cause the suite load to abort.
func TestHarnessDiscoverScenarios_YmlExtensionRejected_SH002(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	// Write one valid .yaml and one invalid .yml.
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "valid.yaml"), "valid-scenario", "smoke")
	ymlPath := filepath.Join(scenariosDir, "bad-extension.yml")
	if err := os.MkdirAll(filepath.Dir(ymlPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ymlPath, []byte(sh006sh007MinimalYAML("bad-ext", "smoke")), 0o600); err != nil {
		t.Fatal(err)
	}

	_, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) == 0 {
		t.Fatal("want scenario-load-failure for .yml extension, got no errors")
	}

	found := false
	for _, e := range errs {
		msg := e.Error()
		if strings.Contains(msg, "scenario-load-failure") && strings.Contains(msg, ".yml") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no error mentions scenario-load-failure + .yml; errors: %v", errs)
	}
}

// TestHarnessDiscoverScenarios_NonYAMLExtensionsSkipped verifies that files
// with truly alien extensions (.dot, .md, .go) in the scenarios/ tree do NOT
// cause suite-load failures — they are silently skipped. This is required
// because scenarios/_workflows/ contains .dot workflow files.
func TestHarnessDiscoverScenarios_NonYAMLExtensionsSkipped(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	// One valid scenario plus non-YAML files of various types.
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "valid.yaml"), "valid-scenario", "smoke")
	for _, name := range []string{
		filepath.Join("_workflows", "flow.dot"),
		"README.md",
		"notes.txt",
	} {
		p := filepath.Join(scenariosDir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("not a scenario"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) != 0 {
		t.Fatalf("want no errors for non-yaml files, got: %v", errs)
	}
	if len(got) != 1 || got[0].Name != "valid-scenario" {
		t.Errorf("want [valid-scenario], got %v", namesOf(got))
	}
}

// TestHarnessDiscoverScenarios_EmptyDirNoError verifies that an empty
// scenarios/ directory returns no scenarios and no errors.
func TestHarnessDiscoverScenarios_EmptyDirNoError(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "scenarios"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) != 0 {
		t.Fatalf("want no errors for empty dir, got: %v", errs)
	}
	if len(got) != 0 {
		t.Errorf("want empty slice, got %d scenarios", len(got))
	}
}

// TestHarnessDiscoverScenarios_CadenceFilterApplied verifies that cadence
// filtering is applied after suite-load and does not affect suite-wide
// duplicate detection (both cadence-matched and unmatched scenarios are checked
// for name uniqueness).
func TestHarnessDiscoverScenarios_CadenceFilterApplied(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "s.yaml"), "smoke-only", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "r.yaml"), "regression-only", "regression")

	got, errs := harnessDiscoverScenarios(root, nil, "smoke", false, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 1 || got[0].Name != "smoke-only" {
		t.Errorf("cadence=smoke: want [smoke-only], got %v", namesOf(got))
	}
}

// TestHarnessDiscoverScenarios_DuplicateAcrossCadences_SH005 verifies that
// duplicate-name detection is suite-wide (across all cadences), not
// filter-scoped. A name that collides between a smoke and a regression
// scenario MUST be rejected even when --cadence smoke is in effect.
func TestHarnessDiscoverScenarios_DuplicateAcrossCadences_SH005(t *testing.T) {
	t.Parallel()

	root := sh006sh007TempRoot(t)
	scenariosDir := filepath.Join(root, "scenarios")

	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "s.yaml"), "shared-name", "smoke")
	sh006sh007WriteScenario(t, filepath.Join(scenariosDir, "r.yaml"), "shared-name", "regression")

	_, errs := harnessDiscoverScenarios(root, nil, "smoke", false, nil)
	if len(errs) == 0 {
		t.Fatal("want duplicate-name error even when one scenario is filtered by cadence; got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "duplicate") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("error slice does not contain a duplicate-name error: %v", errs)
	}
}

// namesOf extracts the Name field from a slice of ScenarioFile for concise
// test failure messages.
func namesOf(sfs []scenario.ScenarioFile) []string {
	names := make([]string, len(sfs))
	for i, sf := range sfs {
		names[i] = sf.Name
	}
	return names
}
