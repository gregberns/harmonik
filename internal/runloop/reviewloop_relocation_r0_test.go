package runloop_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestR0ReviewLoopDaemonPrivateDependencyCensus is the L8 relocation ratchet.
// It counts package-level daemon identifiers and private selector members
// referenced by reviewloop.go but declared in another non-test daemon file.
// Moving a dependency behind an owner API may shrink the map; adding or
// increasing a dependency fails.
//
// The go/types census is the reproducible relocation probe: every identifier
// and selection is resolved to its declaring object, so same-named members and
// package-qualified selectors cannot create false dependencies.
func TestR0ReviewLoopDaemonPrivateDependencyCensus(t *testing.T) {
	t.Parallel()

	got := daemonPrivateDependencyCensus(t)
	// The research LIFT L7 snapshot reported 18 symbols / 29 sites. The first
	// syntax-only R0 probe found 19 / 32 in the live tree. Resolving declaration
	// identity removed its false-positive Config sites and found selected members
	// it had omitted, yielding the authoritative 26 symbols / 48 sites below.
	// This remains a ceiling: owner-API extraction should shrink it.
	baseline := map[string]int{
		".BudgetMS":                     3,
		".ChangedLines":                 3,
		".CommitSHA":                    3,
		".ElapsedMS":                    2,
		".Reason":                       3,
		".Skipped":                      2,
		".workerSessionCwd":             1,
		".workerSessionName":            1,
		"ErrSpawnCapTimeout":            4,
		"ErrTmuxNewWindowTimeout":       4,
		"ReadReviewerBudgetSentinelVia": 1,
		"defaultNewWindowTimeout":       1,
		"defaultSpawnAcquireTimeout":    1,
		"emitClaudeSessionIDPersisted":  2,
		"newDaemonHeartbeatEmitter":     1,
		"newPerRunSubstrate":            2,
		"newSessionIDInterceptor":       1,
		"pasteInjectOnLaunch":           2,
		"pasteInjectQuitOnCommit":       1,
		"pasteInjectQuitOnReviewFile":   1,
		"pasteInjecter":                 1,
		"persistClaudeSessionID":        2,
		"quitSender":                    2,
		"routedLaunchSpecBuilder":       1,
		"sendVersionSelectedACK":        1,
		"substrateSpawnStats":           2,
	}
	var regressions []string
	for name, count := range got {
		ceiling, known := baseline[name]
		if !known {
			regressions = append(regressions, name+" (new)")
			continue
		}
		if count > ceiling {
			regressions = append(regressions, name+" (site count increased)")
		}
	}
	if len(regressions) > 0 {
		sort.Strings(regressions)
		t.Fatalf("daemon-private dependency census regressed: %v\ncurrent (%d symbols, %d sites): %#v\nR0 ceiling (%d symbols, %d sites): %#v",
			regressions, len(got), censusSites(got), got,
			len(baseline), censusSites(baseline), baseline)
	}
}

func daemonPrivateDependencyCensus(t *testing.T) map[string]int {
	t.Helper()
	const daemonDir = "../daemon"
	daemonAbs, err := filepath.Abs(daemonDir)
	if err != nil {
		t.Fatalf("resolve daemon package: %v", err)
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-export", "-json", "./internal/daemon")
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list daemon dependency exports: %v", err)
	}
	type listedPackage struct {
		Dir        string
		ImportPath string
		Export     string
		GoFiles    []string
	}
	exports := make(map[string]string)
	var daemonPackage listedPackage
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	for {
		var pkg listedPackage
		if decodeErr := decoder.Decode(&pkg); errors.Is(decodeErr, io.EOF) {
			break
		} else if decodeErr != nil {
			t.Fatalf("decode go list output: %v", decodeErr)
		}
		if pkg.Export != "" {
			exports[pkg.ImportPath] = pkg.Export
		}
		if samePath(pkg.Dir, daemonAbs) {
			daemonPackage = pkg
		}
	}
	if daemonPackage.ImportPath == "" {
		t.Fatalf("go list did not return daemon package at %s", daemonAbs)
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(daemonPackage.GoFiles))
	var reviewFile *ast.File
	reviewPath := filepath.Join(daemonAbs, "reviewloop.go")
	for _, name := range daemonPackage.GoFiles {
		path := filepath.Join(daemonAbs, name)
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		files = append(files, file)
		if samePath(path, reviewPath) {
			reviewFile = file
		}
	}
	if reviewFile == nil {
		t.Fatalf("daemon package did not include %s", reviewPath)
	}

	imp := importer.ForCompiler(fset, "gc", func(importPath string) (io.ReadCloser, error) {
		exportPath := exports[importPath]
		if exportPath == "" {
			return nil, fmt.Errorf("no export data for %s", importPath)
		}
		return os.Open(exportPath)
	})
	info := &types.Info{
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	config := types.Config{Importer: imp}
	if _, checkErr := config.Check(daemonPackage.ImportPath, fset, files, info); checkErr != nil {
		t.Fatalf("type-check daemon package: %v", checkErr)
	}

	selectorNames := make(map[token.Pos]struct{})
	census := make(map[string]int)
	ast.Inspect(reviewFile, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok {
			selectorNames[sel.Sel.Pos()] = struct{}{}
			selection := info.Selections[sel]
			if selection != nil && objectDeclaredOutsideReviewloop(
				fset, selection.Obj(), daemonPackage.ImportPath, reviewPath,
			) {
				census["."+selection.Obj().Name()]++
			}
		}
		return true
	})

	ast.Inspect(reviewFile, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if _, selector := selectorNames[ident.Pos()]; selector {
			return true
		}
		if objectDeclaredOutsideReviewloop(
			fset, info.Uses[ident], daemonPackage.ImportPath, reviewPath,
		) {
			census[ident.Name]++
		}
		return true
	})
	return census
}

func objectDeclaredOutsideReviewloop(
	fset *token.FileSet,
	obj types.Object,
	daemonImportPath string,
	reviewPath string,
) bool {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != daemonImportPath || !obj.Pos().IsValid() {
		return false
	}
	return !samePath(fset.PositionFor(obj.Pos(), false).Filename, reviewPath)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil &&
		filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func censusSites(census map[string]int) int {
	total := 0
	for _, count := range census {
		total += count
	}
	return total
}
