package core

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

var forbiddenBeadIDFunc = regexp.MustCompile(
	`(?i)^(Parse|Mint|New|Generate|Make|Create|From)BeadID$`,
)

// TestBeadID_NoParseOrMintHelpers parses internal/core/beadid.go via go/ast and
// asserts that no exported top-level function declaration matches the
// forbiddenBeadIDFunc pattern.
//
// References: beads-integration.md BI-008, BI-008a.
func TestBeadID_NoParseOrMintHelpers(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate beadid.go")
	}
	targetFile := filepath.Join(filepath.Dir(thisFile), "beadid.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, targetFile, nil, 0)
	if err != nil {
		t.Fatalf("go/parser.ParseFile(%q): %v", targetFile, err)
	}

	var violations []string

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		if !ast.IsExported(name) {
			continue
		}
		if strings.Contains(name, "BeadID") && forbiddenBeadIDFunc.MatchString(name) {
			violations = append(violations, name)
		}
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-008/BI-008a opacity violation: beadid.go must not export "+
				"parse/mint/generate helpers for BeadID; found: %v\n"+
				"See beads-integration.md BI-008 and BI-008a.",
			violations,
		)
	}
}

// TestBeadID_NominalTyping verifies that BeadID is a distinct named string type
// and that its identity round-trips through a plain string conversion.
func TestBeadID_NominalTyping(t *testing.T) {
	const raw = "bead-0001"
	id := BeadID(raw)
	if got := string(id); got != raw {
		t.Errorf("string round-trip: got %q, want %q", got, raw)
	}
}

// TestBeadID_Equality verifies that two BeadIDs with the same underlying value
// compare equal, and two with different values do not.
func TestBeadID_Equality(t *testing.T) {
	a := BeadID("bead-0002")
	b := BeadID("bead-0002")
	if a != b {
		t.Errorf("equal BeadIDs compare unequal: %v vs %v", a, b)
	}

	c := BeadID("bead-0003")
	if a == c {
		t.Errorf("distinct BeadIDs compare equal: %v vs %v", a, c)
	}
}
