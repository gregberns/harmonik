package brcli_test

// BI-002 adapter-gate enforcement test.
//
// Spec ref: specs/beads-integration.md §4.2 BI-002.
//
// TestAdapterGateNoDatabaseSQLOutsideBrcli verifies that no harmonik package
// outside internal/brcli imports database/sql or a known SQLite driver. This
// mirrors the BI-025/BI-026 enforcement in breakage_test.go (which bans os/exec
// outside the adapter) but targets the SQL-layer ban required by BI-002.
//
// The depguard rule in .golangci.yml (beads-direct-access-ban) is the primary
// enforcement surface; this test is the secondary structural guard that catches
// violations at `go test` time even in CI environments without golangci-lint.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// adapterGateFixtureGoListPackage is the subset of "go list -json" output
// needed for the adapter-gate enforcement test.
type adapterGateFixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

// adapterGateFixtureListHarmonikPackages runs "go list -json ./..." from the
// module root and returns the parsed package list. The test helper fails the
// test on any exec or parse error.
func adapterGateFixtureListHarmonikPackages(t *testing.T) []adapterGateFixtureGoListPackage {
	t.Helper()
	//nolint:gosec // G204: "go" is resolved from PATH; args are static strings, not user input.
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("adapterGateFixtureListHarmonikPackages: go list: %v", err)
	}

	var pkgs []adapterGateFixtureGoListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg adapterGateFixtureGoListPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			t.Fatalf("adapterGateFixtureListHarmonikPackages: json decode: %v", decErr)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// adapterGateForbiddenImports is the set of import paths whose presence
// outside internal/brcli constitutes a BI-002 violation. database/sql covers
// both direct SQL access and any SQLite driver that wraps database/sql.
// The four named SQLite drivers cover direct usage without the stdlib interface.
var adapterGateForbiddenImports = []string{
	"database/sql",
	"github.com/mattn/go-sqlite3",
	"modernc.org/sqlite",
	"crawshaw.io/sqlite",
	"zombiezen.com/go/sqlite",
}

// TestAdapterGateNoDatabaseSQLOutsideBrcli verifies that no harmonik package
// outside internal/brcli imports database/sql or a known SQLite driver.
//
// The depguard rule beads-direct-access-ban in .golangci.yml is the primary
// enforcement surface. This test is the belt-and-suspenders structural guard
// that runs at `go test` time.
//
// Exempt packages:
//   - internal/brcli — the sole adapter; owns all br CLI interactions
//   - internal/testhelpers — fixture infrastructure (may set up SQLite state for
//     integration tests that drive the adapter; none currently exist)
//
// Spec ref: specs/beads-integration.md §4.2 BI-002.
func TestAdapterGateNoDatabaseSQLOutsideBrcli(t *testing.T) {
	const (
		adapterPkg     = "github.com/gregberns/harmonik/internal/brcli"
		testhelpersPkg = "github.com/gregberns/harmonik/internal/testhelpers"
		selfPrefix     = "github.com/gregberns/harmonik"
	)

	pkgs := adapterGateFixtureListHarmonikPackages(t)

	var violations []string
	for _, pkg := range pkgs {
		// Only inspect harmonik packages.
		if !strings.HasPrefix(pkg.ImportPath, selfPrefix) {
			continue
		}

		// Adapter and test-helper packages are exempt.
		if pkg.ImportPath == adapterPkg || strings.HasPrefix(pkg.ImportPath, adapterPkg+"/") {
			continue
		}
		if pkg.ImportPath == testhelpersPkg || strings.HasPrefix(pkg.ImportPath, testhelpersPkg+"/") {
			continue
		}

		for _, imp := range pkg.Imports {
			for _, forbidden := range adapterGateForbiddenImports {
				if imp == forbidden || strings.HasPrefix(imp, forbidden+"/") {
					violations = append(violations,
						pkg.ImportPath+" imports "+imp,
					)
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-002 violation: harmonik packages outside internal/brcli imported a forbidden Beads/SQL import — "+
				"all Beads access MUST route through the adapter (specs/beads-integration.md §4.2):\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}
