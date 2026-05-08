package handler_test

// BI-004 handler-gate enforcement test.
//
// Spec ref: specs/beads-integration.md §4.2 BI-004.
//
// TestBi004HandlerGateNoBrcliImport verifies that no package in the handler
// subsystem (internal/handler and internal/handlercontract) imports the brcli
// adapter. The handler must not provision br outside the Beads-CLI skill path
// per handler-contract.md §4.11.
//
// The depguard rule handler-brcli-ban in .golangci.yml is the primary
// enforcement surface; this test is the secondary structural guard that catches
// violations at `go test` time even in CI environments without golangci-lint.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// bi004FixtureGoListPackage is the subset of "go list -json" output needed
// for the BI-004 handler-gate enforcement test.
type bi004FixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

// bi004FixtureListHarmonikPackages runs "go list -json ./..." from the module
// root and returns the parsed package list. The test helper fails the test on
// any exec or parse error.
func bi004FixtureListHarmonikPackages(t *testing.T) []bi004FixtureGoListPackage {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bi004FixtureListHarmonikPackages: go list: %v", err)
	}

	var pkgs []bi004FixtureGoListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg bi004FixtureGoListPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			t.Fatalf("bi004FixtureListHarmonikPackages: json decode: %v", decErr)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

// TestBi004HandlerGateNoBrcliImport verifies that no package within the
// handler subsystem (internal/handler and internal/handlercontract) imports
// internal/brcli.
//
// The handler subsystem is the skill-injection seam between the daemon and
// agent subprocesses. Agents must receive br access exclusively via the
// Beads-CLI skill injected through the LaunchSpec skill-injection mechanism
// (handler-contract.md §4.11). Importing brcli directly would bypass that
// discipline, letting a handler provision br access outside the skill path.
//
// The depguard rule handler-brcli-ban in .golangci.yml is the primary
// enforcement surface. This test is the belt-and-suspenders structural guard
// that runs at `go test` time.
//
// Spec ref: specs/beads-integration.md §4.2 BI-004.
func TestBi004HandlerGateNoBrcliImport(t *testing.T) {
	const (
		handlerPrefix         = "github.com/gregberns/harmonik/internal/handler"
		handlercontractPrefix = "github.com/gregberns/harmonik/internal/handlercontract"
		brcliPkg              = "github.com/gregberns/harmonik/internal/brcli"
		selfPrefix            = "github.com/gregberns/harmonik"
	)

	pkgs := bi004FixtureListHarmonikPackages(t)

	var violations []string
	for _, pkg := range pkgs {
		// Only inspect harmonik packages.
		if !strings.HasPrefix(pkg.ImportPath, selfPrefix) {
			continue
		}

		// Only inspect handler and handlercontract packages.
		inHandler := pkg.ImportPath == handlerPrefix ||
			strings.HasPrefix(pkg.ImportPath, handlerPrefix+"/")
		inHandlerContract := pkg.ImportPath == handlercontractPrefix ||
			strings.HasPrefix(pkg.ImportPath, handlercontractPrefix+"/")
		if !inHandler && !inHandlerContract {
			continue
		}

		for _, imp := range pkg.Imports {
			if imp == brcliPkg || strings.HasPrefix(imp, brcliPkg+"/") {
				violations = append(violations, pkg.ImportPath+" imports "+imp)
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-004 violation: handler subsystem package(s) imported internal/brcli — "+
				"agents MUST access br only via the Beads-CLI skill (handler-contract.md §4.11); "+
				"handler MUST NOT provision br outside the skill path (specs/beads-integration.md §4.2):\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}
