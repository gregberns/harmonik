package specaudit_test

// Durable-state writer parity sensor.
//
// # The claim this test defends
//
// Every function in this module that persists durable state is reached by
// production code. A writer that only tests reach does not run, so its readers
// see an empty directory or an absent file forever.
//
// # Why this class hides
//
// For most durable state in this tree, "absent" and "empty" are legitimate
// answers. `.harmonik/runs/` with no files means "no run is in progress".
// `.harmonik/reconciliation-locks/` with no files means "no reconciliation is
// running". So when the producer disappears, every reader keeps taking its
// empty-set branch and reports a clean result. No error is returned. No test
// goes red. The system says all-clear because it is measuring nothing.
//
// The live example this sensor was built from: internal/run.Write recorded
// each in-flight bead-run to `.harmonik/runs/`. Its production call site was
// deleted on 2026-08-02 as collateral of an unrelated cleanup. Five reader
// sites remain, all of which now discover zero surviving sessions after every
// daemon restart, which is exactly what a healthy idle fleet looks like.
// Nothing failed (hk-sat32).
//
// # How the sensor works
//
//  1. Parse every non-test .go file under internal/, cmd/ and tools/.
//  2. Mark a function as a durable writer when it calls an os filesystem
//     mutation directly, or calls a package-level function that is one.
//     The rule runs to a fixpoint, so a writer behind a helper is still a
//     writer.
//  3. Walk forward from root context — method bodies, top-level declarations,
//     main and init — following every identifier mention. A mention is a call
//     or a plain use as a value, so a function passed as a callback counts.
//  4. Report every writer the walk does not reach.
//
// Step 3 is reachability, not reference counting, and the difference matters.
// Counting references was the first design, and it missed internal/crew.Write
// when both of its production call sites were deleted, because the dead
// UpdateSessionID still mentioned it. One dead function vouching for another
// is exactly how this class survives. So a mention only carries weight when
// the function making it is itself reached.
//
// A consequence: the sensor reports every writer in a dead subtree, not only
// its root. ExecuteVerdict and the helpers only it calls all appear. Deleting
// the root clears the whole group in one pass.
//
// # Known limits, stated plainly
//
//   - Methods are not nodes, and their bodies are ROOT context. Resolving a
//     method call needs type information, and matching on the method name
//     alone would count every Close in the tree. So an unwired method writer
//     is a false negative, and worse, a DEAD method still vouches for every
//     function it names. That is the same hole reachability closed for plain
//     functions, left open for methods. Closing it needs go/types.
//   - A function reached only through an interface or a func value is counted
//     as used, because a plain mention of its name counts. That direction is
//     deliberate. This sensor should not cry wolf.
//   - Two dead functions that name each other hide each other.
//   - The reverse defect, a writer with no reader, is not detected. Nothing
//     in the syntax separates "nobody reads this file" from "the reader is a
//     shell script or an LLM prompt", and several readers are exactly that.
//   - The parser ignores build tags, so it sees every platform variant of a
//     name at once. addFile folds them into one node and keeps the union, so
//     a name counts as a writer when any variant writes.
//
// # The allowlist
//
// knownUnwiredWriters below records the instances that already exist. The
// sensor fails on anything new. It ALSO fails when an allowlist entry stops
// being true, either because the writer is now reached or because it is gone.
// So the list can only shrink without a deliberate edit.
//
// Each entry carries the consequence in one line: what silently does not
// happen while it stays there. Those lines are prose and nothing checks them.
// Only the KEY is mechanically verified. Read a consequence as a claim some
// human made once, and confirm it before you act on it. Two of the first
// draft's lines were wrong.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/gregberns/harmonik"

// osMutations are the stdlib calls that change durable state. A function that
// makes one of these is a writer.
var osMutations = map[string]bool{
	"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true,
	"Rename": true, "Remove": true, "RemoveAll": true,
	"Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
	"Symlink": true, "Link": true, "Truncate": true,
}

// testInfraDirs are packages that exist to serve *_test.go files. Their
// writers are called from tests by design, so the parity rule does not apply.
// internal/testhelpers carries the same carve-out in .golangci.yml.
var testInfraDirs = map[string]bool{
	"internal/testhelpers": true,
}

// knownUnwiredWriters maps "<pkgdir>.<FuncName>" to the consequence of it
// being unreached. Most entries are defects that are already in the tree. A
// few are deliberate, and their line says so. Remove an entry when the writer
// is wired up or deleted. The sensor fails if a stale entry is left behind.
var knownUnwiredWriters = map[string]string{
	// ---- Protections that cannot protect. Absence reads as safe. ----
	"internal/workspace.WriteLeaseLockAtomic":       "no worktree ever takes a lease, so every worktree is classified NoLock and the 7-day mtime heuristic in RemoveAgedNoLockWorktrees can force-remove a live run's worktree",
	"internal/lifecycle.AcquireReconciliationLock":  "no reconciliation takes a lock, so SweepStaleReconciliationLocks reports zero stale locks forever and nothing serializes two reconciliations of the same run",
	"internal/daemon.ExecuteVerdict":                "no reconciliation verdict is ever applied or committed, so the WIP capture under .harmonik/reconciliation/ never happens and an absent capture reads as 'there was no work to preserve'",
	"internal/lifecycle.WriteVerdictAttemptAtomic":  "no verdict retry is ever counted, so the Cat-3b re-execution cap reads zero attempts forever and cannot stop a loop",
	"internal/lifecycle.CheckBranchTipMonotonicity": "the branch-tip rewind sensor never runs and never persists a tip, so a force-push or reset under an in-flight run is not detected",
	"internal/dashboard.Write":                      "nothing can refresh .harmonik/context/dashboard.json, and an operator who adds a dashboard block to config.yaml arms a gate that reads ErrNotFound as maximally stale, which blocks every captain-curated queue with no way to satisfy it",
	"internal/run.Write":                            "no in-flight bead-run is recorded, so after a daemon restart every reader of .harmonik/runs/ finds an empty set and adopts nothing — indistinguishable from an idle fleet (hk-sat32)",
	"internal/workspace.EnsureGitignoreHygiene":     "the worktree .gitignore entries that keep .harmonik/review.json out of a commit are never installed, though init_cmd.go's blanket .harmonik/.gitignore currently covers for it",
	"internal/workspace.CreateReviewerWorktree":     "no reviewer ever gets an isolated worktree, so reviewer and implementer share one checkout",
	"internal/lifecycle.WritePersistedTip":          "no run's branch tip is ever persisted, so ReadPersistedTip returns the empty string forever and reads it as 'first observation, not a violation'",
	"internal/workspace.WriteWIPCapture":            "an implementer's uncommitted work is never captured before a reopen-bead verdict, and the absent capture directory reads as 'there was no work to preserve'",

	// ---- Reached only by an unwired root above. Clearing the root clears these. ----
	"internal/daemon.commitVerdictEmitted":                "the evidence commit for a reconciliation verdict, reached only from ExecuteVerdict",
	"internal/lifecycle.removeTempFile":                   "temp-file cleanup for the verdict attempt counter, reached only from WriteVerdictAttemptAtomic",
	"internal/workspace.writeFileIfNonEmpty":              "writes one WIP capture part, and is reached only from WriteWIPCapture",
	"internal/workspace.reassertGitignoreWorkingTree":     "reached only from EnsureGitignoreHygiene",
	"internal/workspace.resetSquashProbe":                 "reached only from DetectSquashMergeConflict",
	"internal/workspace.WriteInterruptStateChangedMarker": "reached only from SetInterruptStateToNone, so the WM-040 state-change marker is never written",

	// ---- Features that quietly do nothing. ----
	"cmd/harmonik/supervise.WriteLoopStatusAtomic":         "harmonik supervise status never reports loop status or pause reason, so a budget-exhausted loop looks healthy",
	"internal/lifecycle.RemoveStalePidfile":                "the documented stale-pidfile recovery never runs, and AcquirePidfile's flock takeover covers for it",
	"internal/crew.UpdateSessionID":                        "a crew's recorded session id is frozen at spawn, so after a keeper restart the registry points at a dead session",
	"internal/workspace.ArchiveVerdict":                    "review.iter-N.json is never written, while every implementer-resume brief tells the agent to read it",
	"internal/workspace.CreateSessionLogDir":               "the per-session log directory is never created, so the WM-016 must-pre-exist ordering gate never runs",
	"internal/workspace.WriteSessionMetadataSidecarAtomic": "harmonik.meta.json is never written, so merge-time implementer identification has no input",
	"internal/workspace.WriteLeaseReleasedMarker":          "the workspace-local events log is never written, so the crash-recovery ordering its comment mandates has no participants",
	"internal/workspace.SetInterruptStateToNone":           "the WM-040 interrupt state is never cleared",
	"internal/workspace.DetectSquashMergeConflict":         "the squash-merge conflict probe never runs",
	"internal/lifecycle.GCRetiredIntents":                  "superseded by GCRetiredIntentsWithRedrive, which is the wired one",
	// All seven RunSocketListener arities are dead. The daemon serves its
	// socket through Serve with a SocketHandlers struct, called from
	// (*bootState).startSocketListener. The arity ladder is what Serve
	// replaced, and nothing outside socket*.go names any rung of it.
	"internal/daemon.RunSocketListener":              "superseded by Serve plus SocketHandlers, and no production code calls any RunSocketListener arity",
	"internal/daemon.RunSocketListenerWithSubscribe": "superseded by Serve plus SocketHandlers",
	"internal/daemon.RunSocketListenerFull":          "superseded by Serve plus SocketHandlers",
	"internal/daemon.RunSocketListenerWithCrew":      "superseded by Serve plus SocketHandlers",
	"internal/daemon.RunSocketListenerWithSleepWake": "superseded by Serve plus SocketHandlers",
	"internal/daemon.RunSocketListenerWithState":     "superseded by Serve plus SocketHandlers",
	"internal/daemon.RunSocketListenerWithDashboard": "superseded by Serve plus SocketHandlers",
	"internal/core.OpenDeadLetterSink":               "no dead-letter sink is ever opened, so a dropped handler message is recorded nowhere",
	"internal/daemon.stagedBeadGeneratorEval":        "the staged bead generator never evaluates, so no follow-up bead is ever staged",
	"internal/handlercontract.OpenDeadLetterSink":    "a pass-through to core.OpenDeadLetterSink that nothing calls",
	"internal/scenario.NewFixtureRoot":               "scenario fixture helper, called from tests only, by design",
	"internal/watch.NewLedger":                       "internal/watch has no non-test importer at all, so the whole watch ledger is unreached",
}

func TestEveryDurableStateWriterIsReachedByProductionCode(t *testing.T) {
	root := repoRoot(t)
	idx := indexModule(t, root)

	unwired := idx.unwiredWriters()
	seen := make(map[string]bool, len(unwired))

	for _, w := range unwired {
		seen[w.key] = true
		if _, known := knownUnwiredWriters[w.key]; known {
			continue
		}
		t.Errorf(`durable-state writer with no production caller: %s
  declared at %s
  It writes durable state and nothing outside *_test.go calls or mentions it.
  Its readers cannot tell "the producer is gone" from "there is nothing to report".
  Wire it to a production path, delete it, or add it to knownUnwiredWriters
  with one line saying what silently does not happen while it stays unwired.`,
			w.key, w.pos)
	}

	for key := range knownUnwiredWriters {
		if seen[key] {
			continue
		}
		if idx.writerExists(key) {
			t.Errorf(`stale allowlist entry: %s is now reached by production code.
  Delete it from knownUnwiredWriters. The list is a debt register, not a graveyard.`, key)
			continue
		}
		t.Errorf(`stale allowlist entry: %s no longer exists, or no longer writes durable state.
  Delete it from knownUnwiredWriters.`, key)
	}
}

// ---------------------------------------------------------------------------
// module index
// ---------------------------------------------------------------------------

// funcNode is one package-level function. Methods are not nodes. Their bodies
// are root context. See "Known limits" above.
type funcNode struct {
	key     string // "<pkgdir>.<Name>", the form used in knownUnwiredWriters
	qual    string // "<importpath>.<Name>", the form references resolve to
	pkgDir  string
	pkgPath string
	name    string
	pos     string
	refs    map[string]bool // qualified keys this body mentions
	direct  bool            // calls an os mutation itself
	writer  bool            // calls an os mutation, directly or through a helper
	isRoot  bool            // main or init: entered without a caller
	skipped bool            // test infrastructure by directory or by signature
}

type moduleIndex struct {
	nodes    map[string]*funcNode // keyed by qual
	order    []string             // qual keys, insertion order, for stable output
	rootRefs map[string]bool      // qualified keys mentioned outside any func body
	live     map[string]bool
}

// unwiredWriters returns every durable writer that no production path reaches.
func (m *moduleIndex) unwiredWriters() []*funcNode {
	var out []*funcNode
	for _, q := range m.order {
		n := m.nodes[q]
		if !n.writer || n.skipped || m.live[q] {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// writerExists reports whether key still names a durable writer this sensor
// would consider. It is how a stale allowlist entry is told apart from one
// that is now wired up.
func (m *moduleIndex) writerExists(key string) bool {
	for _, q := range m.order {
		if n := m.nodes[q]; n.key == key && n.writer && !n.skipped {
			return true
		}
	}
	return false
}

type parsedFile struct {
	syntax *ast.File
	pkgDir string
}

func indexModule(t *testing.T, root string) *moduleIndex {
	t.Helper()
	fset := token.NewFileSet()
	idx := &moduleIndex{
		nodes:    map[string]*funcNode{},
		rootRefs: map[string]bool{},
	}

	// Pass one parses. It also records each directory's declared package name,
	// because an import alias defaults to the package NAME and not to the last
	// path segment. internal/daemon/router declares package socketrouter. If
	// this pass is skipped, every reference into such a package resolves to a
	// key no node has, and the sensor reports its writers as dead.
	var files []parsedFile
	pkgNames := map[string]string{}
	for _, top := range []string{"internal", "cmd", "tools"} {
		topDir := filepath.Join(root, top)
		if _, err := os.Stat(topDir); err != nil {
			continue
		}
		err := filepath.WalkDir(topDir, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				// assets/ holds embedded skill text. testdata/ holds fixtures
				// that are deliberately outside the build.
				if d.Name() == "testdata" || d.Name() == "assets" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			rel, relErr := filepath.Rel(root, filepath.Dir(p))
			if relErr != nil {
				return relErr
			}
			pkgDir := filepath.ToSlash(rel)
			pkgNames[pkgDir] = f.Name.Name
			files = append(files, parsedFile{syntax: f, pkgDir: pkgDir})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", topDir, err)
		}
	}
	for _, pf := range files {
		idx.addFile(fset, pf.syntax, pf.pkgDir, pkgNames)
	}
	if len(idx.nodes) == 0 {
		t.Fatal("indexed zero functions — the sensor is measuring nothing")
	}
	idx.propagateWriters()
	idx.computeLive()
	return idx
}

func (m *moduleIndex) addFile(fset *token.FileSet, f *ast.File, pkgDir string, pkgNames map[string]string) {
	imports := fileImports(f, pkgNames)
	pkgPath := modulePath + "/" + pkgDir
	skipDir := testInfraDirs[pkgDir]

	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || fd.Recv != nil {
			// Methods and non-function declarations are root context: their
			// mentions keep a function alive without needing a caller.
			m.collectRefs(d, imports, pkgPath, m.rootRefs)
			continue
		}
		n := &funcNode{
			key:     pkgDir + "." + fd.Name.Name,
			qual:    pkgPath + "." + fd.Name.Name,
			pkgDir:  pkgDir,
			pkgPath: pkgPath,
			name:    fd.Name.Name,
			pos:     fset.Position(fd.Name.Pos()).String(),
			refs:    map[string]bool{},
			direct:  callsOSMutation(fd.Body, imports),
			isRoot:  fd.Name.Name == "main" || fd.Name.Name == "init",
			skipped: skipDir || takesTestingTB(fd.Type),
		}
		m.collectRefs(fd, imports, pkgPath, n.refs)
		// One name can be declared more than once across a package's files.
		// Two files may each declare init, and a build-tag split declares one
		// name per platform. The parser ignores build tags, so it sees every
		// variant. Fold them into one node and keep the union, so a variant
		// that writes still marks the name a writer.
		if prev, dup := m.nodes[n.qual]; dup {
			for r := range n.refs {
				prev.refs[r] = true
			}
			// Point "declared at" to a variant that actually writes, so the
			// report does not send a reader to a stub on the other platform.
			if n.direct && !prev.direct {
				prev.pos = n.pos
			}
			prev.direct = prev.direct || n.direct
			prev.skipped = prev.skipped && n.skipped
			continue
		}
		m.nodes[n.qual] = n
		m.order = append(m.order, n.qual)
	}
}

// propagateWriters runs "calls a writer, is a writer" to a fixpoint, so a
// function that persists state through a helper still counts as a writer.
func (m *moduleIndex) propagateWriters() {
	for _, q := range m.order {
		m.nodes[q].writer = m.nodes[q].direct
	}
	for changed := true; changed; {
		changed = false
		for _, q := range m.order {
			n := m.nodes[q]
			if n.writer {
				continue
			}
			for r := range n.refs {
				if callee, ok := m.nodes[r]; ok && callee.writer {
					n.writer = true
					changed = true
					break
				}
			}
		}
	}
}

// computeLive walks forward from root context. A function is live when root
// context mentions it, or when a live function mentions it. Test
// infrastructure is never a root, so a writer only tests reach stays dead —
// which is the whole point of the sensor.
func (m *moduleIndex) computeLive() {
	m.live = map[string]bool{}
	var queue []string
	push := func(q string) {
		if n, ok := m.nodes[q]; ok && !m.live[q] && !n.skipped {
			m.live[q] = true
			queue = append(queue, q)
		}
	}
	for r := range m.rootRefs {
		push(r)
	}
	for _, q := range m.order {
		if m.nodes[q].isRoot {
			push(q)
		}
	}
	for len(queue) > 0 {
		q := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		for r := range m.nodes[q].refs {
			push(r)
		}
	}
}

// ---------------------------------------------------------------------------
// syntax helpers
// ---------------------------------------------------------------------------

// fileImports maps each import's local name to its path. Without an explicit
// alias the local name is the imported package's declared NAME, which need not
// match the last path segment, so pkgNames supplies the real one for packages
// inside this module.
func fileImports(f *ast.File, pkgNames map[string]string) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, im := range f.Imports {
		ip, err := strconv.Unquote(im.Path.Value)
		if err != nil {
			continue
		}
		var alias string
		switch {
		case im.Name != nil:
			alias = im.Name.Name
		default:
			alias = ip[strings.LastIndex(ip, "/")+1:]
			if name, ok := pkgNames[strings.TrimPrefix(ip, modulePath+"/")]; ok {
				alias = name
			}
		}
		out[alias] = ip
	}
	return out
}

func takesTestingTB(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	found := false
	ast.Inspect(ft.Params, func(n ast.Node) bool {
		se, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, isIdent := se.X.(*ast.Ident); isIdent && x.Name == "testing" {
			found = true
		}
		return true
	})
	return found
}

func callsOSMutation(b *ast.BlockStmt, imports map[string]string) bool {
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		if found {
			return false
		}
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		se, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := se.X.(*ast.Ident)
		if !ok {
			return true
		}
		if imports[x.Name] == "os" && osMutations[se.Sel.Name] {
			found = true
		}
		return true
	})
	return found
}

// collectRefs records every identifier mention under n into out, resolved to
// the qualified "<importpath>.<Name>" form. A function's own declared name is
// not a mention of itself. A mention counts whether it is a call or a plain
// use as a value, so a function passed as a callback is not reported dead.
func (m *moduleIndex) collectRefs(n ast.Node, imports map[string]string, selfPkg string, out map[string]bool) {
	var walk func(ast.Node)
	walk = func(cur ast.Node) {
		if cur == nil {
			return
		}
		switch t := cur.(type) {
		case *ast.FuncDecl:
			if t.Recv != nil {
				walk(t.Recv)
			}
			walk(t.Type)
			if t.Body != nil {
				walk(t.Body)
			}
			return // skip t.Name: a declaration is not a use
		case *ast.SelectorExpr:
			if x, isIdent := t.X.(*ast.Ident); isIdent {
				if ip, isImport := imports[x.Name]; isImport {
					out[ip+"."+t.Sel.Name] = true
					return
				}
			}
			walk(t.X)
			return
		case *ast.Ident:
			out[selfPkg+"."+t.Name] = true
			return
		}
		ast.Inspect(cur, func(c ast.Node) bool {
			if c == nil || c == cur {
				return true
			}
			switch c.(type) {
			case *ast.FuncDecl, *ast.SelectorExpr, *ast.Ident:
				walk(c)
				return false
			}
			return true
		})
	}
	walk(n)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		//nolint:gosec // G304: dir starts at the test's own working directory and only walks up.
		data, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		if readErr == nil && strings.Contains(string(data), "module "+modulePath) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod declaring module %s above %s", modulePath, dir)
		}
		dir = parent
	}
}
