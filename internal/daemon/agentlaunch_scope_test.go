package daemon

// agentlaunch_scope_test.go — guards the sandbox-gate CONSOLIDATION.
//
// This file used to pin the opposite property. The launch-path collapse left one
// deliberate divergence: three call sites each passed a different sandbox scope
// (the cognition gate never sandboxed, the graph-node path sandboxed only
// harnesses that capture their own session id, single mode sandboxed
// everything). That divergence was resolved on 2026-07-29 by consolidating on
// the widest scope, so the test now guards the consolidation rather than the
// split.
//
// The invariant: there is exactly ONE sandbox gate, `sandboxSpawnForRun`, and
// every launch asks it. A per-site scope was a second gate stacked on the first,
// and a second gate can only ever subtract — it could silently un-sandbox a run
// whose harness the operator had explicitly listed in sandbox.harnesses. Putting
// one back is a production behaviour change, so it should fail here first.
//
// Three things are asserted, and they fail for different reasons. Each closes a
// hole the other two leave open, which is why none of them is redundant:
//
//   - No launch site re-gates. A launch input carrying a scope field means
//     per-site scoping came back.
//   - The gate call is unconditional. Wrapping it in an `if` inside
//     runAgentLaunch reintroduces the same second gate without touching any
//     call site, so the first assertion alone would not see it.
//   - The gate's answer is never overwritten. Neither assertion above catches a
//     second gate written as a post-hoc nil-out —
//     `sandboxSpawn := sandboxSpawnForRun(...)` followed by
//     `if !sessionIDCaptured { sandboxSpawn = nil }`. That keys off an existing
//     field, so no new name appears anywhere and the call stays unconditional.
//     It is the same second gate in a shape the first two tests read as clean.
//
// All three are source-level. A behavioural assertion would have to reach the real
// srt engagement probe, which shells out to the `srt` binary and is therefore a
// whole-system check rather than a package test — it is deferred deliberately,
// and tracked in NEXT_STEPS.md. What IS covered behaviourally elsewhere is the
// gate's own decision: sandboxgate_test.go exercises sandboxSpawnForRun against
// the backend, harness-list and remote-run predicates.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// launchSiteFiles are the files holding a runAgentLaunch call site. Kept
// explicit so a NEW launch site in a new file is a deliberate addition here
// rather than something the glob quietly absorbs.
var launchSiteFiles = []string{"workloop.go", "dot_cascade_core.go", "dot_gate.go"}

// TestAgentLaunch_NoCallSiteRegatesTheSandbox fails if any launch site starts
// scoping the sandbox for itself again.
func TestAgentLaunch_NoCallSiteRegatesTheSandbox(t *testing.T) {
	t.Parallel()

	for _, name := range launchSiteFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			file := parseDaemonFile(t, name)
			if got := agentLaunchScopeArgs(file); len(got) != 0 {
				t.Errorf("%s passes a per-site sandbox scope (%v) to runAgentLaunch, want none.\n"+
					"The sandbox scope was consolidated on 2026-07-29: sandboxSpawnForRun is the only gate, and sandbox.harnesses in config is the only switch. A per-site scope can only subtract from that, which silently un-sandboxes a harness the operator listed. If re-scoping is genuinely intended, make that decision explicitly and rewrite this test with it.",
					name, got)
			}
		})
	}
}

// TestAgentLaunch_SandboxGateIsAskedUnconditionally fails if the single
// sandboxSpawnForRun call inside runAgentLaunch is put back behind a condition.
//
// Without this, a second gate could be reintroduced inside runAgentLaunch and
// every call site would still look clean.
func TestAgentLaunch_SandboxGateIsAskedUnconditionally(t *testing.T) {
	t.Parallel()

	fn := findFuncDecl(t, parseDaemonFile(t, "agentlaunch.go"), "runAgentLaunch")

	var total, conditional int
	var stack []ast.Node
	ast.Inspect(fn, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if call, ok := node.(*ast.CallExpr); ok && isIdent(unparenExpr(call.Fun), "sandboxSpawnForRun") {
			total++
			for _, ancestor := range stack {
				switch ancestor.(type) {
				case *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.ForStmt, *ast.RangeStmt:
					conditional++
				}
			}
		}
		stack = append(stack, node)
		return true
	})

	if total != 1 {
		t.Fatalf("runAgentLaunch calls sandboxSpawnForRun %d times, want exactly 1 — one gate, asked once", total)
	}
	if conditional != 0 {
		t.Errorf("runAgentLaunch's sandboxSpawnForRun call sits inside a conditional, want it unconditional.\n" +
			"That is a second gate in front of the real one, and a second gate can only subtract: a run whose harness IS listed in sandbox.harnesses would silently launch unsandboxed. sandboxSpawnForRun already returns nil for every case that must not be sandboxed (non-srt backend, unlisted harness, remote run) — let it decide.")
	}
}

// TestAgentLaunch_SandboxGateAnswerIsNeverOverwritten fails if anything in
// runAgentLaunch assigns to sandboxSpawn after the gate has answered.
//
// This is the hole the other two tests leave. A `sandboxSpawn = nil` guarded by
// any condition is a second gate that adds no new identifier and leaves the gate
// call unconditional, so both of them stay green while a run the operator listed
// in sandbox.harnesses launches unsandboxed.
func TestAgentLaunch_SandboxGateAnswerIsNeverOverwritten(t *testing.T) {
	t.Parallel()

	fn := findFuncDecl(t, parseDaemonFile(t, "agentlaunch.go"), "runAgentLaunch")

	var declared, reassigned int
	ast.Inspect(fn, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if !isIdent(unparenExpr(lhs), "sandboxSpawn") {
				continue
			}
			if assign.Tok == token.DEFINE {
				declared++
			} else {
				reassigned++
			}
		}
		return true
	})

	if declared != 1 {
		t.Fatalf("runAgentLaunch declares sandboxSpawn %d times, want exactly 1 — the gate answers once", declared)
	}
	if reassigned != 0 {
		t.Errorf("runAgentLaunch assigns to sandboxSpawn %d time(s) after the gate answered, want 0.\n"+
			"Overwriting the gate's answer is a second gate that adds no new identifier and leaves the call unconditional, so the other tests in this file cannot see it. A second gate can only subtract: a run whose harness IS listed in sandbox.harnesses would silently launch unsandboxed. If a run must not be sandboxed, teach sandboxSpawnForRun — it is the one place that decides.", reassigned)
	}
}

func parseDaemonFile(t *testing.T, name string) *ast.File {
	t.Helper()

	path := filepath.Join(repoRootForConformance(), "internal", "daemon", name)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return file
}

func findFuncDecl(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Recv == nil {
			return fn
		}
	}
	t.Fatalf("no func %s in the parsed file — it was renamed or moved, and this test no longer guards anything", name)
	return nil
}

// agentLaunchScopeArgs returns the value passed as any sandbox-scope field in
// every agentLaunchInput composite literal in file. It matches on the field NAME
// rather than a type that no longer exists, so reintroducing per-site scoping
// under a fresh name is still caught. A non-identifier value is reported as
// "<dynamic>" so routing a scope through a variable cannot hide it.
func agentLaunchScopeArgs(file *ast.File) []string {
	var scopes []string
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || !isIdent(lit.Type, "agentLaunchInput") {
			return true
		}
		for _, elt := range lit.Elts {
			kv, kvOK := elt.(*ast.KeyValueExpr)
			if !kvOK {
				continue
			}
			key, keyOK := kv.Key.(*ast.Ident)
			if !keyOK || !strings.Contains(strings.ToLower(key.Name), "sandbox") {
				continue
			}
			if ident, identOK := unparenExpr(kv.Value).(*ast.Ident); identOK {
				scopes = append(scopes, key.Name+": "+ident.Name)
			} else {
				scopes = append(scopes, key.Name+": <dynamic>")
			}
		}
		return true
	})
	return scopes
}

// TestAgentLaunchScopeArgs_DetectsAReintroducedScope keeps the sensor above
// honest. Both assertions in this file pass trivially against source that has no
// scope field at all, which is exactly the state the tree is in — so without a
// positive case, "no scope found" could equally mean "the matcher is broken" and
// nobody would know until a real regression walked past it.
//
// The otherInput literal is not filler: without it a type filter that matched
// every composite literal would pass this test, and the sensor would report a
// scope field on any unrelated struct that happened to have one.
func TestAgentLaunchScopeArgs_DetectsAReintroducedScope(t *testing.T) {
	t.Parallel()

	const src = `package daemon
func f() {
	_ = agentLaunchInput{LogPrefix: "x", SandboxScope: sandboxScopeNone}
	_ = agentLaunchInput{SandboxPolicy: somePolicy()}
	_ = agentLaunchInput{LogPrefix: "no scope here"}
	_ = otherInput{SandboxScope: sandboxScopeAll}
}`
	file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}

	got := agentLaunchScopeArgs(file)
	want := []string{"SandboxScope: sandboxScopeNone", "SandboxPolicy: <dynamic>"}
	if len(got) != len(want) {
		t.Fatalf("agentLaunchScopeArgs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("agentLaunchScopeArgs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestLaunchSiteFilesExist guards the explicit file list against a rename.
func TestLaunchSiteFilesExist(t *testing.T) {
	t.Parallel()

	for _, name := range launchSiteFiles {
		path := filepath.Join(repoRootForConformance(), "internal", "daemon", name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("launch-site file %s is missing (%v) — update launchSiteFiles, or this test guards nothing", name, err)
		}
	}
}
