package workflowvalidator

// mechanism_tag_sensor_test.go — EM-039 sensor: PreRunValidator is mechanism-tagged.
//
// Spec ref: specs/execution-model.md §4.9 EM-039.
//
// EM-039 states: "Every validator check MUST be mechanism-tagged; delegation to
// cognition is forbidden. Semantic judgments (is this policy expression 'good'?
// is this node name 'descriptive'?) belong in reviewer nodes, not the validator."
//
// This file asserts that the PreRunValidator type declaration in validator.go
// carries a "Tags: mechanism" line in its godoc comment. The test uses go/parser
// with ParseComments mode so that a future contributor removing or altering the
// tag causes an immediate test failure.
//
// Helper prefix: mechanismTagFixture (per implementer-protocol.md §Helper-prefix discipline).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// mechanismTagFixtureValidatorPath returns the absolute path to validator.go
// by resolving relative to this test file's source location via runtime.Caller.
// The test file lives at:
//
//	internal/workflowvalidator/mechanism_tag_sensor_test.go
//
// so validator.go is in the same directory.
func mechanismTagFixtureValidatorPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("mechanismTagFixtureValidatorPath: runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "validator.go")
}

// mechanismTagFixtureTagsLine matches a "Tags: mechanism" line in a doc comment.
// The line MUST contain the word "mechanism" as a standalone token per EM-039.
var mechanismTagFixtureTagsLine = regexp.MustCompile(`\bTags:.*\bmechanism\b`)

// mechanismTagFixtureFindTypeDoc parses the named Go source file and returns
// the godoc comment text for the named type declaration, or ("", false) if the
// type is not found.
func mechanismTagFixtureFindTypeDoc(t *testing.T, srcPath, typeName string) (string, bool) {
	t.Helper()

	fset := token.NewFileSet()
	//nolint:gosec // G304: path derived from runtime.Caller + same-package dir; not user input.
	f, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("mechanismTagFixtureFindTypeDoc: parser.ParseFile(%q): %v", srcPath, err)
	}

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		// Only consider top-level type declarations.
		if genDecl.Doc == nil {
			continue
		}
		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if ts.Name.Name == typeName {
				return genDecl.Doc.Text(), true
			}
		}
	}
	return "", false
}

// TestEM039_PreRunValidatorIsMechanismTagged is the EM-039 sensor test.
//
// It parses internal/workflowvalidator/validator.go via go/parser and asserts
// that the PreRunValidator type declaration carries a "Tags: mechanism" line in
// its godoc comment. The check encodes EM-039's prohibition on delegating any
// validator check to cognition; removing or altering the tag causes this test
// to fail.
//
// Spec ref: specs/execution-model.md §4.9 EM-039.
func TestEM039_PreRunValidatorIsMechanismTagged(t *testing.T) {
	t.Parallel()

	validatorPath := mechanismTagFixtureValidatorPath(t)
	docText, found := mechanismTagFixtureFindTypeDoc(t, validatorPath, "PreRunValidator")
	if !found {
		t.Fatalf(
			"EM-039 sensor: PreRunValidator type declaration not found in %s; "+
				"the type must exist and carry a godoc comment with 'Tags: mechanism'",
			validatorPath,
		)
	}

	// Check each line of the doc comment for a "Tags: mechanism" entry.
	for _, line := range strings.Split(docText, "\n") {
		if mechanismTagFixtureTagsLine.MatchString(line) {
			t.Logf(
				"EM-039 sensor PASS: PreRunValidator godoc carries %q "+
					"(specs/execution-model.md §4.9 EM-039)",
				strings.TrimSpace(line),
			)
			return
		}
	}

	t.Errorf(
		"EM-039 sensor FAIL: PreRunValidator godoc in %s does not carry "+
			"a 'Tags: mechanism' line; EM-039 requires every validator check "+
			"to be mechanism-tagged (specs/execution-model.md §4.9 EM-039).\n"+
			"Godoc text:\n%s",
		validatorPath,
		docText,
	)
}
