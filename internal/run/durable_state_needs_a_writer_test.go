package run_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type durableStore struct {
	// importPath is the package as it is imported. Import aliases are resolved,
	// so a caller spelling it `runpkg` is found.
	importPath string
	// producers put state on disk.
	producers []string
	// consumers read, list or clear state that a producer put there.
	consumers []string
}

var durableStores = []durableStore{
	{
		importPath: "github.com/gregberns/harmonik/internal/run",
		producers:  []string{"Write"},
		consumers:  []string{"Load", "List", "Remove"},
	},
}

func durableCallSites(t *testing.T, importPath string, includeTests bool) (counts map[string]int, callerFiles map[string][]string) {
	t.Helper()

	counts = make(map[string]int)
	files := make(map[string]map[string]struct{})

	root := durableModuleRoot(t)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return filepath.SkipDir
			case ".claire", ".claude", "worktrees":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("durable: parse %s: %v", path, parseErr)
			return nil
		}

		local, imported := durableImportName(file, importPath)
		if !imported {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != local {
				return true
			}
			counts[sel.Sel.Name]++
			if files[sel.Sel.Name] == nil {
				files[sel.Sel.Name] = make(map[string]struct{})
			}
			files[sel.Sel.Name][rel] = struct{}{}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("durable: walk the module: %v", walkErr)
	}

	callerFiles = make(map[string][]string, len(files))
	for name, set := range files {
		list := make([]string, 0, len(set))
		for f := range set {
			list = append(list, f)
		}
		sort.Strings(list)
		callerFiles[name] = list
	}
	return counts, callerFiles
}

func durableImportName(file *ast.File, importPath string) (string, bool) {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value != importPath {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				return "", false
			}
			return spec.Name.Name, true
		}
		return importPath[strings.LastIndex(importPath, "/")+1:], true
	}
	return "", false
}

func durableModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("durable: cannot locate this source file, so the scan has nothing to walk")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("durable: %s holds no go.mod, so it is not the module root: %v\n"+
			"A scan of the wrong directory reports zero calls to everything, which reads "+
			"exactly like the defect this test looks for.", root, statErr)
	}
	return root
}

func durableTotal(counts map[string]int, names []string) int {
	total := 0
	for _, n := range names {
		total += counts[n]
	}
	return total
}

func durableNamesWithCalls(counts map[string]int, samples map[string][]string, names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if counts[n] == 0 {
			continue
		}
		out = append(out, n+" ("+strings.Join(samples[n], ", ")+")")
	}
	sort.Strings(out)
	return out
}

// TestDurableStateWithReadersHasAProductionWriter is the property, not the
// case.
//
// For every durable store: if production reads it, production must also write
// it. A store that is only ever read is a store whose readers all take their
// empty-set branch for ever, and no other test in this tree can see that,
// because a test that exercises a reader supplies its own record.
//
// The test proves its own scanner before it accuses anyone. For each store it
// first re-runs the same scan with _test.go files included. If the writer turns
// up there, the scanner recognises the name and can find calls to it, so a zero
// in the production scan is a fact about production and not about the scan. If
// the writer turns up in NEITHER, the test says so as a separate failure,
// because a scanner that finds nothing anywhere is the more likely explanation
// and must not be reported as a defect in the code.
func TestDurableStateWithReadersHasAProductionWriter(t *testing.T) {
	t.Parallel()

	for _, store := range durableStores {
		t.Run(store.importPath, func(t *testing.T) {
			t.Parallel()

			prod, prodFiles := durableCallSites(t, store.importPath, false)
			all, allFiles := durableCallSites(t, store.importPath, true)

			readers := durableTotal(prod, store.consumers)
			writers := durableTotal(prod, store.producers)
			writersAnywhere := durableTotal(all, store.producers)

			if readers == 0 && writers == 0 {
				t.Fatalf("the scan found no call to %s at all, in production or anywhere else.\n"+
					"Either the package is genuinely unused — in which case delete it and this row — "+
					"or this scan is broken and every result below is worthless.", store.importPath)
			}
			if writersAnywhere == 0 {
				t.Fatalf("the scan found no call to %v anywhere in the module, tests included.\n"+
					"That is far more likely to be the scan failing to recognise the name than every "+
					"caller vanishing, so it is reported as a broken check rather than as a defect.",
					store.producers)
			}

			if readers > 0 && writers == 0 {
				t.Errorf("%s is read by production and written by nothing.\n"+
					"Readers in production: %s\n"+
					"Writers, tests included: %s\n"+
					"The store is empty on every boot, so every one of those readers takes its "+
					"empty-set branch for ever. Nothing fails loudly, and the tests that cover the "+
					"readers stay green because each one writes its own record first. Either restore "+
					"the production write, or delete the readers and the package with them.",
					store.importPath,
					strings.Join(durableNamesWithCalls(prod, prodFiles, store.consumers), "; "),
					strings.Join(durableNamesWithCalls(all, allFiles, store.producers), "; "))
			}
		})
	}
}
