package brcli_test

// BI-026 enforcement tests — Harmonik absorbs breakage rather than forking Beads.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026.
//
// Tests in this file assert the structural and behavioral properties of the
// breakage-absorption policy:
//
//  1. TestBreakageAdapterIsSoleExecImporter — release-engineering test: verifies
//     that internal/brcli is the ONLY harmonik package that imports os/exec for
//     the purpose of spawning `br` subprocesses. Any other harmonik package
//     importing os/exec for br invocations is a structural violation of BI-025.
//
//  2. TestBreakageSchemaChangeSurfacedThroughAdapter — mock-Beads test: simulates
//     a backwards-incompatible Beads schema change (unexpected JSON shape returned
//     by `br`) and verifies that the error surfaces through the adapter boundary,
//     not through a scattered call site.

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// breakageFixtureGoListPackage is the subset of "go list -json" output relevant
// to the sole-importer enforcement test. Only ImportPath and Imports are needed.
type breakageFixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

// breakageFixtureListHarmonikPackages runs "go list -json ./..." from the module
// root and returns the parsed package list. The test helper fails the test on
// any exec or parse error.
func breakageFixtureListHarmonikPackages(t *testing.T) []breakageFixtureGoListPackage {
	t.Helper()
	//nolint:gosec // G204: "go" is resolved from PATH; args are static strings, not user input.
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("breakageFixtureListHarmonikPackages: go list: %v", err)
	}

	var pkgs []breakageFixtureGoListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg breakageFixtureGoListPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			t.Fatalf("breakageFixtureListHarmonikPackages: json decode: %v", decErr)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// breakageFixtureChangedSchemaJSON returns `br show` JSON output that simulates a
// backwards-incompatible Beads schema change: the top-level response is no longer
// an array of objects but a single object (a breaking reshape). The adapter's JSON
// parser will reject this and return an error.
func breakageFixtureChangedSchemaJSON(id string) string {
	// Simulate Beads v-next renaming the array wrapper to a map with a "beads"
	// key — a breaking schema change. The adapter expects a JSON array at the
	// top level; this shape is not parseable as []brShowItem.
	return `{"beads":[{"id":"` + id + `","title":"t","description":"d","status":"open","issue_type":"task","dependencies":[],"dependents":[]}]}`
}

// breakageFixtureListResponseChangedSchemaJSON simulates a breaking Beads schema
// change for the `br list` surface: the envelope object is gone and br now
// returns a bare JSON array of bead summaries. The adapter expects a JSON object
// at the top level ({issues: [...]}); a bare array is a type mismatch that
// causes json.Unmarshal to fail.
func breakageFixtureListResponseChangedSchemaJSON() string {
	return `[{"id":"hk-1","title":"T","status":"open","issue_type":"task"}]`
}

// TestBreakageAdapterIsSoleExecImporter verifies that internal/brcli is the
// ONLY harmonik package that imports os/exec.
//
// This is the BI-025 / BI-026 structural invariant: all `br` subprocess
// invocations MUST route through the adapter. No other harmonik package may
// bypass the adapter by importing os/exec to call br directly.
//
// Note: internal/handler imports os/exec for exec.LookPath (system-handler
// path resolution per HC-042), which is NOT a br subprocess invocation. The
// test accounts for this carve-out explicitly.
//
// Spec ref: specs/beads-integration.md §4.8 BI-025, BI-026; §10.2
// (release-engineering tests verify adapter is sole importer).
func TestBreakageAdapterIsSoleExecImporter(t *testing.T) {
	const (
		adapterPkg = "github.com/gregberns/harmonik/internal/brcli"
		handlerPkg = "github.com/gregberns/harmonik/internal/handler"
		toolPkg    = "github.com/gregberns/harmonik/tools/forbid-import"
		execImport = "os/exec"
		selfPrefix = "github.com/gregberns/harmonik"
	)

	// handlerCarveout is the set of harmonik packages that legitimately import
	// os/exec for purposes OTHER than invoking `br`. Each entry here must have
	// a documented justification.
	//
	// internal/handler — uses exec.LookPath for system-handler path resolution
	//   per HC-042. This is NOT a br subprocess invocation.
	// tools/forbid-import — uses exec.Command("go", ...) for module inspection.
	//   This is a build tool, not daemon code.
	handlerCarveout := map[string]string{
		handlerPkg: "exec.LookPath for system-handler path resolution (HC-042); no br invocation",
		toolPkg:    "exec.Command(\"go\", ...) for module import inspection (build tool, not daemon)",
	}

	pkgs := breakageFixtureListHarmonikPackages(t)

	var violations []string
	for _, pkg := range pkgs {
		// Only inspect harmonik packages.
		if !strings.HasPrefix(pkg.ImportPath, selfPrefix) {
			continue
		}

		importsExec := false
		for _, imp := range pkg.Imports {
			if imp == execImport {
				importsExec = true
				break
			}
		}
		if !importsExec {
			continue
		}

		// The adapter package itself is expected to import os/exec.
		if pkg.ImportPath == adapterPkg {
			continue
		}

		// Check the known carve-out list.
		if _, ok := handlerCarveout[pkg.ImportPath]; ok {
			continue
		}

		// Any other harmonik package importing os/exec is a BI-025 / BI-026 violation.
		violations = append(violations, pkg.ImportPath)
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-025/BI-026 violation: harmonik packages outside internal/brcli imported os/exec — "+
				"all br subprocess invocations MUST route through the adapter (specs/beads-integration.md §4.8):\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}

// TestBreakageSchemaChangeSurfacedThroughAdapter verifies that when a
// backwards-incompatible Beads schema change breaks the `br show` JSON output
// shape, the resulting error surfaces through the adapter boundary with a
// parse-failure error — not through any scattered call site.
//
// This is the BI-026 behavioral test: the adapter absorbs the breakage (returns
// a typed error to callers) rather than the breakage propagating as raw
// output into other packages.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026; §4.8a BI-025b
// (parse failures of structured output MUST classify as BrSchemaMismatch —
// tracked in hk-872.28; until that bead lands, parse failure returns a plain
// wrapped error from brcli.ShowBead).
func TestBreakageSchemaChangeSurfacedThroughAdapter(t *testing.T) {
	id := core.BeadID("hk-872.99")
	// Simulate a backwards-incompatible Beads schema change: br show now returns
	// a wrapped object instead of a bare array.
	changedJSON := breakageFixtureChangedSchemaJSON(string(id))
	path := brcliFixtureMockBinary(t, changedJSON, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, showErr := adapter.ShowBead(context.Background(), id)
	if showErr == nil {
		t.Fatal("TestBreakageSchemaChangeSurfacedThroughAdapter: expected error for changed schema, got nil")
	}

	// The error MUST originate from within brcli (adapter boundary).
	// It must NOT be nil (which would mean the changed schema was silently accepted).
	// It must NOT be a sentinel from another package (which would indicate the
	// raw output leaked past the adapter boundary).
	//
	// The exact error type here is a plain wrapped JSON parse error from brcli.ShowBead
	// until hk-872.28 lands BrSchemaMismatch. We verify:
	//   (a) error is non-nil — the adapter detected the breakage
	//   (b) error does NOT wrap ErrBeadNotFound (wrong sentinel — this is a schema issue)
	//   (c) error string contains "brcli." — the error originates inside the adapter package
	if errors.Is(showErr, brcli.ErrBeadNotFound) {
		t.Errorf("schema-change error incorrectly wrapped ErrBeadNotFound; got: %v", showErr)
	}
	if !strings.Contains(showErr.Error(), "brcli.") {
		t.Errorf("schema-change error does not originate from brcli adapter; got: %v", showErr)
	}
}

// TestBreakageSchemaChangeListSurfacedThroughAdapter verifies that a breaking
// schema change in the `br list` surface (used by ListInFlightBeads) also
// surfaces through the adapter boundary.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026; §4.5 BI-013.
func TestBreakageSchemaChangeListSurfacedThroughAdapter(t *testing.T) {
	changedJSON := breakageFixtureListResponseChangedSchemaJSON()
	path := brcliFixtureMockBinary(t, changedJSON, "", 0)

	adapter, err := brcli.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, listErr := adapter.ListInFlightBeads(context.Background())
	if listErr == nil {
		t.Fatal("TestBreakageSchemaChangeListSurfacedThroughAdapter: expected error for changed schema, got nil")
	}

	// Verify the error originates from the adapter boundary.
	if !strings.Contains(listErr.Error(), "brcli.") {
		t.Errorf("list schema-change error does not originate from brcli adapter; got: %v", listErr)
	}
}
