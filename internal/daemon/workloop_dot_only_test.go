package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestBeadRunOneExecutesOnlyResolvedDOTGraphs prevents restoration of the
// retired imperative single-workflow tail. Legacy inputs select no-review DOT
// in run planning. beadRunOne must only drive the resolved graph.
func TestBeadRunOneExecutesOnlyResolvedDOTGraphs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootForConformance(), "internal", "daemon", "workloop.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse workloop.go: %v", err)
	}

	var runOne *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "beadRunOne" {
			runOne = fn
			break
		}
	}
	if runOne == nil {
		t.Fatal("beadRunOne is missing")
	}

	var droveDOT, imperativeLaunch, singleModeBranch bool
	ast.Inspect(runOne.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			if name, ok := value.Fun.(*ast.Ident); ok {
				if name.Name == "driveDotWorkflow" {
					droveDOT = true
				}
				if name.Name == "runAgentLaunch" {
					imperativeLaunch = true
				}
			}
		case *ast.SelectorExpr:
			if pkg, ok := value.X.(*ast.Ident); ok && pkg.Name == "core" && value.Sel.Name == "WorkflowModeSingle" {
				singleModeBranch = true
			}
		}
		return true
	})

	if !droveDOT {
		t.Fatal("beadRunOne must drive the resolved DOT graph")
	}
	if imperativeLaunch {
		t.Fatal("beadRunOne calls runAgentLaunch: the retired imperative single-workflow tail returned")
	}
	if singleModeBranch {
		t.Fatal("beadRunOne selects WorkflowModeSingle: legacy inputs must resolve to no-review DOT before execution")
	}
}
