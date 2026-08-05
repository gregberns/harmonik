package run_test

// durable_state_needs_a_writer_test.go — the shape of the defect, stated once,
// so the next one is caught by the same test.
//
// A durable store is a package that puts state on disk for a LATER process to
// read. The state only means anything if both halves are wired: somebody writes
// it while the fact is true, and somebody reads it after a restart. Delete the
// write and nothing breaks loudly. The readers keep compiling, keep running and
// keep returning the empty set, and every one of them silently switches to the
// branch it takes when there is no state — which is usually the branch that
// says "nothing was in flight", and is usually wrong.
//
// That is invisible to ordinary tests, because a test that wants to exercise a
// reader writes a record itself. The store's unit tests stay green, the
// readers' tests stay green, and the only thing that changed is that production
// stopped producing.
//
// So this test asks a question no other test in the tree asks: does anything
// OUTSIDE a test call the writer at all?
//
// Helper prefix: durable.

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

// durableStore names a package that owns state on disk and splits its surface
// into the calls that put state there and the calls that act on state somebody
// else put there.
//
// Add a row when a package starts owning durable state. The rule is the same
// for every row and does not need restating per store.
type durableStore struct {
	// importPath is the package as it is imported. Import aliases are resolved,
	// so a caller spelling it `runpkg` is found.
	importPath string
	// producers put state on disk.
	producers []string
	// consumers read, list or clear state that a producer put there.
	consumers []string
}

// durableStores is the set this test guards.
var durableStores = []durableStore{
	{
		// The run registry: the only durable statement that a bead is being
		// worked on right now. The daemon decides session adoption, orphan
		// reconciliation, dead-process reaping and the stranded-bead reset from
		// it.
		importPath: "github.com/gregberns/harmonik/internal/run",
		producers:  []string{"Write"},
		consumers:  []string{"Load", "List", "Remove"},
	},
}

// durableCallSites counts, per function name, the calls to functions of
// importPath found in the module. includeTests selects whether _test.go files
// are scanned. It returns the counts and, per function, a sorted sample of the
// files the calls are in.
//
// The scan is syntactic. It resolves the import alias each file gives the
// package and then looks for a selector on that name. It therefore sees a call
// written out, and does not see one reached only through a function value, an
// interface or reflection. It also counts a call that no live path reaches. The
// consequence is one-sided and that is the useful direction: a zero here means
// the name is written nowhere in production, which is a fact no reachability
// argument can talk its way out of.
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
			// Nested agent worktrees and scratch checkouts live UNDER the repo
			// root on this machine, so the walk reached their copies of the tree
			// as well as the real one. Those copies hold whatever a partial write
			// or a crashed tool left behind, and an unparseable placeholder file
			// in one of them failed this sensor against a clean tree. The same
			// skip was added to the event-parity sensor at 35c9b9e6 for the same
			// reason.
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
			// A file this test cannot parse is a file it cannot vouch for, and
			// silently skipping it would let the defect hide in it.
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

// durableImportName returns the name importPath is bound to in file, and
// whether the file imports it at all. An aliased import returns the alias; a
// plain one returns the last path segment, which is what Go binds.
func durableImportName(file *ast.File, importPath string) (string, bool) {
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || value != importPath {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				// A blank import calls nothing; a dot import writes no selector
				// and this scan cannot see through it. Report it rather than
				// pass silently, because it is a hole in the check.
				return "", false
			}
			return spec.Name.Name, true
		}
		return importPath[strings.LastIndex(importPath, "/")+1:], true
	}
	return "", false
}

// durableModuleRoot returns the repository root, two levels above this file.
//
// It confirms go.mod is there. A wrong root walks a directory with no Go files
// in it, finds no call to anything, and the count that comes back is zero for a
// reason that has nothing to do with the code under test.
func durableModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("durable: cannot locate this source file, so the scan has nothing to walk")
	}
	// thisFile = <root>/internal/run/durable_state_needs_a_writer_test.go
	root := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr != nil {
		t.Fatalf("durable: %s holds no go.mod, so it is not the module root: %v\n"+
			"A scan of the wrong directory reports zero calls to everything, which reads "+
			"exactly like the defect this test looks for.", root, statErr)
	}
	return root
}

// durableTotal sums the counts for a set of function names.
func durableTotal(counts map[string]int, names []string) int {
	total := 0
	for _, n := range names {
		total += counts[n]
	}
	return total
}

// durableNamesWithCalls lists the names in the set that were actually called,
// with the files they were called from.
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
