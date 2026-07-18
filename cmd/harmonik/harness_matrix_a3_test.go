package main

// harness_matrix_a3_test.go — tests for SH-030 matrix expansion wired into the
// harness runner's suite-load phase (harnessDiscoverScenarios). Finding A3.
//
// Before A3, harnessDiscoverScenarios returned raw scenarios and the runner
// ranged them without ever calling ScenarioFile.ExpandMatrix — a `matrix:` YAML
// with `{{.param}}` ran ONCE unsubstituted. These tests exercise the wired
// expansion path: a matrix scenario now expands into one concrete, substituted
// ScenarioFile per cartesian-product cell (SH-030), with synthetic per-cell
// names participating in suite-wide uniqueness (SH-005).
//
// Spec refs: specs/scenario-harness.md §4.10 SH-030, §4.1 SH-005, §4.2 SH-007.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a3WriteMatrixScenario writes a scenario YAML with a two-parameter matrix and
// a `{{.param}}`-templated description at scenarios/<file>.
func a3WriteMatrixScenario(t *testing.T, root, file, name string) {
	t.Helper()
	yaml := "name: " + name + "\n" +
		"description: run for env={{.env}} mode={{.mode}}\n" +
		"workflow_path: test.dot\n" +
		"timeout_secs: 30\n" +
		"cadence_tag: smoke\n" +
		"matrix:\n" +
		"  env: [dev, prod]\n" +
		"  mode: [fast]\n"
	p := filepath.Join(root, "scenarios", file)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestHarnessDiscover_MatrixExpandedIntoCells_SH030 verifies that a matrix
// scenario is expanded by the runner's suite-load into one ScenarioFile per
// cell, with synthetic byte-lex names and substituted `{{.param}}` fields.
func TestHarnessDiscover_MatrixExpandedIntoCells_SH030(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a3WriteMatrixScenario(t, root, "m.yaml", "base")

	got, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected suite-load errors: %v", errs)
	}
	// 2 (env) × 1 (mode) = 2 cells.
	if len(got) != 2 {
		t.Fatalf("want 2 expanded cells, got %d: %v", len(got), namesOf(got))
	}

	// Synthetic names: keys in byte-lex order → base[env=dev,mode=fast], etc.
	wantNames := []string{"base[env=dev,mode=fast]", "base[env=prod,mode=fast]"}
	for i, w := range wantNames {
		if got[i].Name != w {
			t.Errorf("cell %d: got name %q, want %q", i, got[i].Name, w)
		}
	}

	// Matrix must be cleared and `{{.param}}` substituted in the description.
	for _, c := range got {
		if c.Matrix != nil {
			t.Errorf("cell %q: Matrix not cleared after expansion", c.Name)
		}
		if strings.Contains(c.Description, "{{") {
			t.Errorf("cell %q: description still has unsubstituted template: %q", c.Name, c.Description)
		}
	}
	// Verify the actual substituted values landed per cell.
	if !strings.Contains(got[0].Description, "env=dev") || !strings.Contains(got[0].Description, "mode=fast") {
		t.Errorf("cell 0 description not substituted: %q", got[0].Description)
	}
	if !strings.Contains(got[1].Description, "env=prod") {
		t.Errorf("cell 1 description not substituted: %q", got[1].Description)
	}
}

// TestHarnessDiscover_MatrixNameCollision_SH005 verifies that a synthetic
// matrix-cell name colliding with a standalone scenario is caught as a
// suite-load failure (SH-005 uniqueness spans expanded cells).
func TestHarnessDiscover_MatrixNameCollision_SH005(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Two files share the base name "base" and the same matrix, so their
	// synthetic per-cell names (e.g. base[env=dev,mode=fast]) collide across
	// files — a suite-wide SH-005 uniqueness violation caught at suite-load.
	a3WriteMatrixScenario(t, root, "m1.yaml", "base")
	a3WriteMatrixScenario(t, root, "m2.yaml", "base")

	_, errs := harnessDiscoverScenarios(root, nil, "all", false, nil)
	if len(errs) == 0 {
		t.Fatal("want duplicate-name suite-load error for matrix-cell/standalone collision; got none")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "duplicate") {
			found = true
		}
	}
	if !found {
		t.Errorf("no duplicate-name error in: %v", errs)
	}
}
