package handlercontract_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

type seamFixtureGoListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Imports    []string `json:"Imports"`
}

func seamFixtureListHarmonikPackages(t *testing.T) []seamFixtureGoListPackage {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "list", "-json", "./...")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("seamFixtureListHarmonikPackages: go list: %v", err)
	}

	var pkgs []seamFixtureGoListPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var pkg seamFixtureGoListPackage
		if decErr := dec.Decode(&pkg); decErr != nil {
			t.Fatalf("seamFixtureListHarmonikPackages: json decode: %v", decErr)
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

func seamFixtureIsExecutionShape(importPath, modulePrefix string) bool {
	handlerPkg := modulePrefix + "/internal/handler"
	return importPath == handlerPkg ||
		strings.HasPrefix(importPath, handlerPkg+"/")
}

func seamFixtureIsDaemonSide(importPath, modulePrefix string) bool {
	if !strings.HasPrefix(importPath, modulePrefix+"/") {
		return false
	}
	if seamFixtureIsExecutionShape(importPath, modulePrefix) {
		return false
	}
	if importPath == modulePrefix+"/internal/testhelpers" ||
		strings.HasPrefix(importPath, modulePrefix+"/internal/testhelpers/") {
		return false
	}
	return true
}

// TestSeam_HC051_DaemonDoesNotImportExecutionShape is the HC-051 seam sensor.
//
// It resolves the full module package graph via "go list -json ./..." and
// asserts that no daemon-side package (any harmonik package that is not itself
// an execution-shape package) imports the execution-shape package
// (internal/handler).
//
// A failure here means a daemon package has coupled itself to the ntm
// execution shape, violating HC-051 and the modularity seam defined in
// specs/handler-contract.md §4.12.  Fix by moving any execution-shape concern
// behind the Handler or Adapter interface surface (§6.1) so the daemon depends
// only on the contract, not on the implementation.
func TestSeam_HC051_DaemonDoesNotImportExecutionShape(t *testing.T) {
	t.Parallel()

	const modulePrefix = "github.com/gregberns/harmonik"

	pkgs := seamFixtureListHarmonikPackages(t)

	var violations []string
	for _, pkg := range pkgs {
		if !seamFixtureIsDaemonSide(pkg.ImportPath, modulePrefix) {
			continue
		}
		for _, imp := range pkg.Imports {
			if seamFixtureIsExecutionShape(imp, modulePrefix) {
				violations = append(violations, pkg.ImportPath+" imports "+imp)
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf(
			"HC-051 violation: daemon-side package(s) import the execution-shape package "+
				"(internal/handler) — the handler contract (specs/handler-contract.md §4.12) "+
				"is the seam; daemon routing MUST depend only on the Handler/Adapter/Session "+
				"interfaces, not on ntm-specific adapter types:\n  %s",
			strings.Join(violations, "\n  "),
		)
	}
}

// TestSeam_HC051_SensorHasCoverage verifies that the seam sensor scanned at
// least one daemon-side package.  A zero count would mean the sensor is
// silently not operating (e.g., module path changed without updating this
// file).
func TestSeam_HC051_SensorHasCoverage(t *testing.T) {
	t.Parallel()

	const modulePrefix = "github.com/gregberns/harmonik"

	pkgs := seamFixtureListHarmonikPackages(t)

	var count int
	for _, pkg := range pkgs {
		if seamFixtureIsDaemonSide(pkg.ImportPath, modulePrefix) {
			count++
		}
	}

	if count == 0 {
		t.Error(
			"HC-051 sensor has no coverage: zero daemon-side packages found; " +
				"check that the module prefix matches go.mod and that internal/ packages exist",
		)
	}
}

type seamFixtureNoHandlerImportStub struct{}

func (seamFixtureNoHandlerImportStub) DetectReady(_ core.EventEnvelope) bool { return false }
func (seamFixtureNoHandlerImportStub) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (seamFixtureNoHandlerImportStub) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (seamFixtureNoHandlerImportStub) RotateAccount(_ context.Context) error { return nil }
func (seamFixtureNoHandlerImportStub) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

var _ handlercontract.Adapter = seamFixtureNoHandlerImportStub{}

// TestSeam_HC051_AdapterIsSubstitutable is the changeable-adapter test
// required by specs/handler-contract.md §10.2.HC-051.
//
// The test compiles only if seamFixtureNoHandlerImportStub satisfies the
// Adapter interface, and this file imports no execution-shape package
// (internal/handler).  Together these two facts demonstrate that the Adapter
// surface is fully satisfiable from a context that is blind to ntm-specific
// types — i.e., swapping adapters does not require altering the daemon side.
//
// The check is intentionally compile-time: a runtime assertion would add no
// signal beyond what the type system already guarantees.
func TestSeam_HC051_AdapterIsSubstitutable(t *testing.T) {
	t.Parallel()

	var a handlercontract.Adapter = seamFixtureNoHandlerImportStub{}
	if a.DetectReady(core.EventEnvelope{}) {
		t.Error("seamFixtureNoHandlerImportStub.DetectReady = true; want false")
	}
	if err := a.CleanExitSequence(t.Context(), nil); err != nil {
		t.Errorf("seamFixtureNoHandlerImportStub.CleanExitSequence: %v", err)
	}
}
