package continuity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionPackageRemainsEffectFree(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	allowedImports := map[string]bool{
		"context": true,
		"fmt":     true,
		"github.com/gregberns/harmonik/internal/core": true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		for _, imported := range file.Imports {
			importPath := strings.Trim(imported.Path.Value, `"`)
			if !allowedImports[importPath] {
				t.Errorf("%s imports effectful or unapproved dependency %q", name, importPath)
			}
		}
		for _, declaration := range file.Decls {
			generic, isGeneric := declaration.(*ast.GenDecl)
			if isGeneric {
				switch generic.Tok {
				case token.VAR:
					t.Errorf("%s declares package-level mutable state", name)
				default:
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.GoStmt:
				t.Errorf("%s contains a goroutine", name)
			case *ast.DeferStmt:
				t.Errorf("%s contains deferred lifecycle work", name)
			}
			return true
		})
	}
}
