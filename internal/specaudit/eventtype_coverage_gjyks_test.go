package specaudit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	_ "github.com/gregberns/harmonik/internal/daemon"
	_ "github.com/gregberns/harmonik/internal/workers"
	_ "github.com/gregberns/harmonik/internal/workflow"
)

// TestEveryDeclaredEventTypeHasRuntimeContracts compares declarations with the
// production registries. It does not keep a second hand-written event list.
func TestEveryDeclaredEventTypeHasRuntimeContracts(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not return the test file")
	}
	registered := core.AllPayloadSchemaVersions()
	declared := make(map[core.EventType]string)
	internalDir := filepath.Dir(filepath.Dir(thisFile))
	err := filepath.WalkDir(internalDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		collectDeclaredEventTypes(t, path, parsed, declared)
		return nil
	})
	if err != nil {
		t.Fatalf("scan event declarations: %v", err)
	}

	for eventType, symbol := range declared {
		if _, ok := registered[eventType]; !ok {
			t.Errorf("%s (%q) has no production payload registration", symbol, eventType)
		}
		if _, ok := core.LookupPayloadCompatEntry(eventType); !ok {
			t.Errorf("%s (%q) has no payload compatibility entry", symbol, eventType)
		}
	}
	if len(declared) == 0 {
		t.Fatal("event declaration scan found no EventType constants")
	}
}

func collectDeclaredEventTypes(t *testing.T, path string, parsed *ast.File, declared map[core.EventType]string) {
	t.Helper()
	for _, decl := range parsed.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range value.Names {
				if i >= len(value.Values) {
					continue
				}
				literal, ok := eventTypeLiteral(parsed.Name.Name == "core", value.Type, value.Values[i])
				if !ok {
					continue
				}
				valueText, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					t.Fatalf("parse %s %s: %v", path, name.Name, unquoteErr)
				}
				declared[core.EventType(valueText)] = path + " " + name.Name
			}
		}
	}
}

func eventTypeLiteral(corePackage bool, typeExpr, valueExpr ast.Expr) (*ast.BasicLit, bool) {
	typed := false
	switch expr := typeExpr.(type) {
	case *ast.Ident:
		typed = corePackage && expr.Name == "EventType"
	case *ast.SelectorExpr:
		owner, ok := expr.X.(*ast.Ident)
		typed = ok && owner.Name == "core" && expr.Sel.Name == "EventType"
	}
	if typed {
		literal, ok := valueExpr.(*ast.BasicLit)
		return literal, ok && literal.Kind == token.STRING
	}
	call, ok := valueExpr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "EventType" {
		return nil, false
	}
	owner, ok := selector.X.(*ast.Ident)
	if !ok || owner.Name != "core" {
		return nil, false
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	return literal, ok && literal.Kind == token.STRING
}
