package keeper

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// TestWatcherRun_JoinedJobs characterizes the jobs that Watcher.Run joins at
// its present seams. It is deliberately structural. The later decision about
// which job should leave the watcher must first update this inventory and its
// behavioral coverage; this test must not become an extraction plan.
func TestWatcherRun_JoinedJobs(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate characterization test source")
	}

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filepath.Join(filepath.Dir(thisFile), "watcher.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse watcher.go: %v", err)
	}

	calls := watcherRunCallNames(t, file)
	for job, wantCalls := range map[string][]string{
		"boot gauge check":              {"gaugeUnavailable", "maybeEmitNoGauge"},
		"warn text reload":              {"seedConfigMtime", "maybeReloadWarnMessages"},
		"poll and cancellation":         {"NewTicker", "Done"},
		"cycle suppression":             {"InCycle"},
		"decision reaping":              {"maybeReapOrphanedDecisions"},
		"dashboard staleness nag":       {"maybeNagDashboardStale"},
		"gauge read and no-gauge state": {"ReadCtxFile", "maybeEmitNoGauge"},
		"live-pane heartbeat":           {"maybeHeartbeat"},
		"stale-gauge recovery":          {"maybeRespawn", "maybeLivePaneRecover"},
		"session binding":               {"ReadManagedSessionFn", "ReadSidFn", "WriteManagedSessionFn"},
		"hard ceiling":                  {"maybeHandleHardCeiling"},
		"foreign-session backstop":      {"emitBlind"},
		"normal restart cycle":          {"MaybeRun"},
		"precompact restart cycle":      {"HasPrecompactTrigger", "RunForPrecompact"},
		"idle restart cycle":            {"RunForIdle"},
		"warn and self hint":            {"emitWarn", "SelfHintInjectFn"},
		"leader warn delivery":          {"maybeDeliverLeaderWarn"},
	} {
		t.Run(job, func(t *testing.T) {
			for _, wantCall := range wantCalls {
				if !calls[wantCall] {
					t.Errorf("Watcher.Run no longer directly joins %q through %s", job, wantCall)
				}
			}
		})
	}
}

func watcherRunCallNames(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()

	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "Run" || funcDecl.Recv == nil {
			continue
		}

		calls := make(map[string]bool)
		ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				calls[fun.Name] = true
			case *ast.SelectorExpr:
				calls[fun.Sel.Name] = true
			}
			return true
		})
		return calls
	}

	t.Fatal("Watcher.Run declaration not found")
	return nil
}
