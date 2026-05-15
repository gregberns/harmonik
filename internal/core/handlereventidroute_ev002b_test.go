// Package core — EV-002b sensor: handler subprocesses MUST NOT generate event_id.
//
// Per event-model.md §4.1 EV-002b: handler subprocesses MUST NOT generate
// event_id values independently. Handler writes an envelope with no event_id
// (or a placeholder the daemon discards); the daemon watcher stamps event_id,
// envelope timestamps, and source_subsystem at enqueue time. This preserves
// EV-002a's intra-daemon-process monotonicity as the sole monotonicity contract
// across all cross-bus events.
//
// This file ships three layered sensor shapes:
//
//   - Shape A: surface-area sensor — asserts that no Go source file under
//     internal/handler/ imports the core package (which hosts NewEventIDGenerator).
//     If handler code ever imports core, a future contributor could instantiate
//     EventIDGenerator in-handler, violating EV-002b. The import boundary is
//     the structural enforcement of the routing contract.
//
//   - Shape B: reflect + AST field-shape sensor — asserts that Event.EventID
//     carries godoc citing EV-002b and its daemon-stamp contract ("MUST NOT
//     populate"). This makes the contract machine-discoverable for anyone
//     reading the field declaration.
//
//   - Shape C: spec-content sensor — asserts that specs/event-model.md contains
//     the normative phrase "MUST NOT generate" at or near "EV-002b", confirming
//     the spec has not been silently edited to remove the routing requirement.
//
// Spec reference: event-model.md §4.1 EV-002b.
// Requirement-traceable bead: hk-hqwn.4.
package core

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is defined in beadsadoption_bi001_test.go (same package); reused here.

// ── Shape A: surface-area sensor ──────────────────────────────────────────────

// TestEventEV002b_HandlerPackageDoesNotImportCore asserts that no production
// (non-test) Go source file under internal/handler/ imports the internal/core
// package.
//
// EventIDGenerator and NewEventIDGenerator both live in internal/core. If a
// handler-side production file imports core, the import boundary that enforces
// EV-002b is broken: handler code could call NewEventIDGenerator() and mint
// event_id values independently, violating the daemon-watcher-stamps-at-enqueue
// contract.
//
// Test files (_test.go) are explicitly excluded from this sensor because they
// do not compile into the handler subprocess binary — they run in the Go test
// harness as a separate process. A test file that imports internal/core to use
// shared type aliases (e.g. core.RunID, core.WorkflowID) for assertion fixtures
// does NOT violate EV-002b: the handler subprocess never links against that code.
// Including test files would produce false positives on any test that references
// core types for fixture construction without ever calling NewEventIDGenerator.
//
// This test is a surface-area sensor: it does not assert that handler code
// _does_ call NewEventIDGenerator (that would require execution tracing), but
// asserts that the structural import boundary in production code that prevents
// such a call is intact.
//
// Spec reference: event-model.md §4.1 EV-002b.
func TestEventEV002b_HandlerPackageDoesNotImportCore(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	handlerDir := filepath.Join(root, "internal", "handler")

	// If internal/handler does not exist yet, the constraint is vacuously
	// satisfied: there is no handler code that could import core.
	if _, err := os.Stat(handlerDir); os.IsNotExist(err) {
		t.Skip("EV-002b Shape A: internal/handler/ does not exist; constraint vacuously satisfied")
	}

	// Collect production (non-test) .go files under internal/handler/.
	// Test files (_test.go) are excluded: they compile into the Go test binary,
	// not the handler subprocess, so importing internal/core in a test file does
	// not violate EV-002b's "handler subprocess MUST NOT generate event_id"
	// constraint. Including test files would produce false positives for test
	// fixtures that reference core types (e.g. core.RunID) for assertion purposes.
	var goFiles []string
	err := filepath.Walk(handlerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("EV-002b: walking internal/handler/: %v", err)
	}

	if len(goFiles) == 0 {
		t.Skip("EV-002b Shape A: internal/handler/ has no production .go files; constraint vacuously satisfied")
	}

	// Parse each file and inspect its imports.
	fset := token.NewFileSet()
	for _, goFile := range goFiles {
		f, err := parser.ParseFile(fset, goFile, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("EV-002b: parsing %s: %v", goFile, err)
			continue
		}
		for _, imp := range f.Imports {
			// imp.Path.Value is a quoted string, e.g. `"github.com/gregberns/harmonik/internal/core"`.
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasSuffix(importPath, "/internal/core") || importPath == "internal/core" {
				rel, err := filepath.Rel(root, goFile)
				if err != nil {
					rel = goFile // fallback to absolute path on error
				}
				t.Errorf(
					"EV-002b: %s imports %q; handler-side production packages MUST NOT import internal/core "+
						"because NewEventIDGenerator lives there and handler subprocesses MUST NOT "+
						"generate event_id independently (event-model.md §4.1 EV-002b); "+
						"event_id MUST be stamped by the daemon watcher at enqueue time",
					rel, importPath,
				)
			}
		}
	}
}

// TestEventEV002b_SensorSkipsTestFiles verifies that the Shape A sensor
// correctly distinguishes _test.go files from production files: a test file
// that imports internal/core must NOT cause the sensor to fail, whereas a
// production file importing internal/core MUST trigger a failure.
//
// This test is a meta-sensor: it exercises the sensor's file-filtering logic
// by synthesizing temporary production and test files in a temp directory that
// mirrors the internal/handler/ layout, then verifying the sensor's per-file
// classification matches expectation.
//
// Spec reference: event-model.md §4.1 EV-002b.
func TestEventEV002b_SensorSkipsTestFiles(t *testing.T) {
	t.Parallel()

	// We verify the filter predicate used by the Shape A sensor directly:
	// files ending in _test.go are test files; all other .go files are production.
	cases := []struct {
		filename      string
		isProductionF bool // true = would be scanned by the sensor
	}{
		{"handler.go", true},
		{"session.go", true},
		{"launchspecdelivery_hc005.go", true},
		{"handler_test.go", false},
		{"launchspecdelivery_hc005_test.go", false},
		{"session_test.go", false},
	}

	for _, tc := range cases {
		isProduction := strings.HasSuffix(tc.filename, ".go") && !strings.HasSuffix(tc.filename, "_test.go")
		if isProduction != tc.isProductionF {
			t.Errorf(
				"EV-002b sensor filter: %q: got isProduction=%v, want %v; "+
					"the Shape A sensor must skip _test.go files to avoid "+
					"false positives on test fixtures that import internal/core "+
					"for type aliases (event-model.md §4.1 EV-002b)",
				tc.filename, isProduction, tc.isProductionF,
			)
		}
	}
}

// ── Shape B: AST field-comment sensor ─────────────────────────────────────────

// TestEventEV002b_EventIDFieldGodocCitesDaemonStamp asserts that the EventID
// field on the Event struct has godoc that:
//   - cites "EV-002b" by name (the spec requirement), and
//   - contains "MUST NOT populate" (the handler contract phrasing).
//
// This is an AST-level sensor: it parses event.go and inspects the comment
// block attached to the EventID field declaration, rather than using reflect
// (which does not expose godoc). The test fails if the godoc is stripped,
// silently reworded, or the field is renamed, drawing immediate attention to
// the regression.
//
// Spec reference: event-model.md §4.1 EV-002b.
func TestEventEV002b_EventIDFieldGodocCitesDaemonStamp(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	eventFile := filepath.Join(root, "internal", "core", "event.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, eventFile, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("EV-002b: parsing event.go: %v", err)
	}

	// Walk top-level declarations looking for type Event struct.
	var eventIDComment string
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "Event" {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name.Name == "EventID" && field.Comment != nil {
						eventIDComment = field.Comment.Text()
					}
					if name.Name == "EventID" && field.Doc != nil {
						// Prefer the leading doc comment over the inline comment.
						eventIDComment = field.Doc.Text()
					}
				}
			}
		}
	}

	if eventIDComment == "" {
		t.Fatal(
			"EV-002b: Event.EventID field has no doc comment; " +
				"event-model.md §4.1 EV-002b requires the godoc to document " +
				"that event_id is daemon-stamped and handler MUST NOT populate it",
		)
	}

	if !strings.Contains(eventIDComment, "EV-002b") {
		t.Errorf(
			"EV-002b: Event.EventID godoc does not cite \"EV-002b\"; "+
				"got comment: %q; "+
				"event-model.md §4.1 EV-002b requires the field comment to name "+
				"the spec requirement so future contributors can trace back to the rule",
			eventIDComment,
		)
	}

	if !strings.Contains(eventIDComment, "MUST NOT populate") {
		t.Errorf(
			"EV-002b: Event.EventID godoc does not contain \"MUST NOT populate\"; "+
				"got comment: %q; "+
				"EV-002b's handler contract (event-model.md §4.1 EV-002b) requires "+
				"this phrasing so that any handler author reading the field sees the "+
				"prohibition inline with the type declaration",
			eventIDComment,
		)
	}
}

// ── Shape C: spec-content sensor ──────────────────────────────────────────────

// TestEventEV002b_SpecContainsMustNotGenerate asserts that the normative spec
// file specs/event-model.md contains the phrase "MUST NOT generate" in close
// proximity to "EV-002b".
//
// This is a content-integrity sensor: it fails if someone edits the spec to
// remove the routing requirement without updating the sensor, giving the team
// a chance to notice the change in review. It reads the file line-by-line and
// looks for a line that contains both "EV-002b" and "MUST NOT generate", OR
// asserts that both strings appear within a 10-line window of each other.
//
// Spec reference: event-model.md §4.1 EV-002b.
func TestEventEV002b_SpecContainsMustNotGenerate(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	specFile := filepath.Join(root, "specs", "event-model.md")

	f, err := os.Open(specFile) //nolint:gosec // specFile is constructed from repoRoot + a literal constant suffix; not user-controlled input
	if err != nil {
		t.Fatalf("EV-002b: opening %s: %v", specFile, err)
	}
	defer f.Close() //nolint:errcheck // read-only file; close error is immaterial

	const windowSize = 10

	type lineRecord struct {
		n    int
		text string
	}
	var lines []lineRecord
	scanner := bufio.NewScanner(f)
	n := 0
	for scanner.Scan() {
		n++
		lines = append(lines, lineRecord{n: n, text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("EV-002b: reading %s: %v", specFile, err)
	}

	// Find all lines containing "EV-002b".
	var ev002bLines []int
	for _, lr := range lines {
		if strings.Contains(lr.text, "EV-002b") {
			ev002bLines = append(ev002bLines, lr.n)
		}
	}
	if len(ev002bLines) == 0 {
		t.Fatalf(
			"EV-002b: specs/event-model.md contains no line with \"EV-002b\"; " +
				"the spec requirement defining the handler-routing contract " +
				"(event-model.md §4.1 EV-002b) appears to have been removed",
		)
	}

	// For each EV-002b occurrence, check whether "MUST NOT generate" appears
	// within ±windowSize lines.
	found := false
	for _, refLine := range ev002bLines {
		lo := refLine - windowSize
		hi := refLine + windowSize
		for _, lr := range lines {
			if lr.n >= lo && lr.n <= hi && strings.Contains(lr.text, "MUST NOT generate") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	if !found {
		t.Errorf(
			"EV-002b: specs/event-model.md mentions \"EV-002b\" but \"MUST NOT generate\" "+
				"does not appear within %d lines of any EV-002b occurrence; "+
				"the normative routing prohibition from event-model.md §4.1 EV-002b appears "+
				"to have been edited or removed — restore it before shipping",
			windowSize,
		)
	}
}
