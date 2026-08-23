package brcli_test

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

type breakageFixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func breakageFixtureListHarmonikPackages(t *testing.T) []breakageFixtureGoListPackage {
	t.Helper()
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

func breakageFixtureChangedSchemaJSON(id string) string {
	return `{"beads":[{"id":"` + id + `","title":"t","description":"d","status":"open","issue_type":"task","dependencies":[],"dependents":[]}]}`
}

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

	handlerCarveout := map[string]string{
		handlerPkg: "exec.LookPath for system-handler path resolution (HC-042); no br invocation",
		toolPkg:    "exec.Command(\"go\", ...) for module import inspection (build tool, not daemon)",
	}

	pkgs := breakageFixtureListHarmonikPackages(t)

	var violations []string
	for _, pkg := range pkgs {
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

		if pkg.ImportPath == adapterPkg {
			continue
		}

		if _, ok := handlerCarveout[pkg.ImportPath]; ok {
			continue
		}

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
// shape, the resulting error surfaces through the adapter boundary as
// BrSchemaMismatch — not through any scattered call site.
//
// This is the BI-026 behavioral test: the adapter absorbs the breakage (returns
// a typed error to callers) rather than the breakage propagating as raw
// output into other packages.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026; §4.8a BI-025b
// (parse failures of structured output MUST classify as BrSchemaMismatch per BI-025b).
func TestBreakageSchemaChangeSurfacedThroughAdapter(t *testing.T) {
	id := core.BeadID("hk-872.99")
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

	if errors.Is(showErr, brcli.ErrBeadNotFound) {
		t.Errorf("schema-change error incorrectly wrapped ErrBeadNotFound; got: %v", showErr)
	}
	if !strings.Contains(showErr.Error(), "brcli.") {
		t.Errorf("schema-change error does not originate from brcli adapter; got: %v", showErr)
	}
	if !errors.Is(showErr, brcli.BrSchemaMismatch) {
		t.Errorf("schema-change error does not wrap BrSchemaMismatch per BI-025b; got: %v", showErr)
	}
}

// TestBreakageSchemaChangeListSurfacedThroughAdapter verifies that a breaking
// schema change in the `br list` surface (used by ListInFlightBeads) also
// surfaces through the adapter boundary as BrSchemaMismatch.
//
// Spec ref: specs/beads-integration.md §4.8 BI-026; §4.5 BI-013; §4.8a BI-025b.
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

	if !strings.Contains(listErr.Error(), "brcli.") {
		t.Errorf("list schema-change error does not originate from brcli adapter; got: %v", listErr)
	}
	if !errors.Is(listErr, brcli.BrSchemaMismatch) {
		t.Errorf("list schema-change error does not wrap BrSchemaMismatch per BI-025b; got: %v", listErr)
	}
}
