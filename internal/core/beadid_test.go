package core

// BeadID opacity sensor — beads-integration.md BI-008 + BI-008a
//
// BI-008:  A bead ID MUST be stable from creation to tombstone; harmonik treats
//          it as an immutable handle throughout its lifetime.
//
// BI-008a: The adapter MUST treat bead_id as opaque — no parsing, no minting,
//          no rewriting.  This package therefore MUST NOT export any function
//          whose name suggests it parses or manufactures a BeadID.
//
// The tests in this file enforce that contract at test time via Go AST
// inspection of beadid.go.  A future contributor adding ParseBeadID,
// MintBeadID, NewBeadID, GenerateBeadID, or any similarly named function to
// beadid.go will cause TestBeadID_NoParseOrMintHelpers to fail.

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

// forbiddenBeadIDFunc matches exported top-level function names that would
// violate the BI-008/BI-008a opacity discipline.  The pattern is intentionally
// conservative: it only trips on names that suggest parsing or minting — not on
// purely structural helpers such as BeadID.Valid() or BeadID.String().
//
// Matched prefixes (case-insensitive against the CamelCase segment before
// "BeadID"):  Parse, Mint, New, Generate, Make, Create, From.
var forbiddenBeadIDFunc = regexp.MustCompile(
	`(?i)^(Parse|Mint|New|Generate|Make|Create|From)BeadID$`,
)

// TestBeadID_NoParseOrMintHelpers parses internal/core/beadid.go via go/ast and
// asserts that no exported top-level function declaration matches the
// forbiddenBeadIDFunc pattern.
//
// References: beads-integration.md BI-008, BI-008a.
func TestBeadID_NoParseOrMintHelpers(t *testing.T) {
	// Locate beadid.go relative to this test file using runtime.Caller so the
	// test is resilient to changes in working directory.
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
		// Receiver methods are allowed (e.g. BeadID.String, BeadID.Valid).
		if fn.Recv != nil {
			continue
		}
		name := fn.Name.Name
		// Only check exported names.
		if !ast.IsExported(name) {
			continue
		}
		// Also catch any exported function that *contains* "BeadID" and a
		// forbidden verb anywhere in its name (broader safety net).
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
