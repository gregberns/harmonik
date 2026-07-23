package daemon

// conformance_m4c7_test.go — remote-substrate M4-C7 (T8): the CONTINUOUS
// conformance gate that GATES every M4 merge.
//
// It proves three invariants hold across ALL of M4 (Claude/tmux, Codex/codexdriver,
// and Pi), consolidating the per-harness coverage that T1–T7 landed rather than
// inventing a parallel abstraction:
//
//  1. NFR7 — zero/disabled workers ⇒ byte-identical LOCAL operation.  Each of the
//     three harness paths spawns LOCALLY (nil / LocalRunner) when no worker is
//     selected, so the spawned argv/env/cwd are byte-identical to the pre-M4 local
//     path.
//       - Claude/tmux : perRunSubstrate.commandRunner() falls through to
//         tmux.LocalRunner{} when the per-run runner is nil (T1 seam).
//       - Codex        : buildCodexRoutedLaunchSpec(AgentTypeCodex) yields a nil
//         LaunchSpec.Runner ⇒ handler.Launch takes the byte-identical
//         exec.CommandContext local path (T5 fall-through).
//       - Pi           : buildCodexRoutedLaunchSpec(AgentTypePi) yields a nil
//         LaunchSpec.Runner ⇒ same exec.CommandContext local path (T6 fall-through).
//     (The composition-root Codex router's own zero/disabled-worker NFR7 proof lives
//     next to it in cmd/harmonik/substrate_select_router_hkm4c3_test.go — that path
//     is not importable from this package.)
//
//  2. Seam-survival (structural / grep) — the remote seam is NOT deleted and no
//     runner!=nil / rbc!=nil dual-path branch was removed (DEC-A cleanup DEFERRED,
//     decision 5).  A floor-based static audit fails if any load-bearing seam symbol
//     drops below its expected count.
//
//  3. Billing fail-closed (D2) on ALL THREE remote harness paths — ANTHROPIC_API_KEY
//     is NEVER forwarded to a remote spawn.  The D2 chokepoint (hasAPIKeyInEnv on
//     spec.Env, guarded only by rbc!=nil in workloop.go) is harness-agnostic: it
//     gates whatever the specBuilder produced, Claude OR Codex OR Pi.  We assert the
//     Codex and Pi remote specs introduce no key AND that the shared chokepoint would
//     refuse them identically if one leaked in.  (Claude's equivalent is
//     remote_substrate_b10_test.go's TestRSB10_APIKeyInEnv_Refused; re-asserted here
//     so the gate reads as one suite.)
//
// Gate-runnable: no real tmux, SSH, git, or network required.  All routing is
// exercised through package-internal builders with a RecordingRunner / nil runner
// standing in for the worker's SSHRunner (same idiom as the sibling M4 tests).
//
// Bead: T8 / M4-C7 (codename:remote-substrate).

import (
	"bufio"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// (1) NFR7 — zero/disabled workers ⇒ byte-identical LOCAL for each harness
// ─────────────────────────────────────────────────────────────────────────────

// TestM4C7_NFR7_LocalByteIdentical_AllHarnesses proves the zero/disabled-worker
// local path for all three M4 harnesses in one place: no worker selected ⇒ the
// run is spawned LOCALLY (LocalRunner / nil Runner), never over ssh.
func TestM4C7_NFR7_LocalByteIdentical_AllHarnesses(t *testing.T) {
	ctx := context.Background()

	// ── Claude/tmux ──────────────────────────────────────────────────────────
	// A per-run substrate built with a nil runner (no worker) must fall through
	// to tmux.LocalRunner{} — the byte-identical box-A-local command path.
	t.Run("claude_tmux_nil_runner_is_LocalRunner", func(t *testing.T) {
		ts := &tmuxSubstrate{sessionName: "m4c7-local"}
		prs := newPerRunSubstrate(ts, "claude", nil) // nil runner == no worker selected
		if prs == nil {
			t.Fatal("newPerRunSubstrate(*tmuxSubstrate, nil) = nil; want non-nil")
		}
		got := prs.commandRunner()
		if _, isLocal := got.(tmux.LocalRunner); !isLocal {
			t.Fatalf("NFR7: claude local commandRunner() = %T; want tmux.LocalRunner (byte-identical local)", got)
		}
	})

	// ── Codex/codexdriver ────────────────────────────────────────────────────
	// A nil rc.runner (no worker) must produce a LaunchSpec with a nil Runner, so
	// handler.Launch's exec path uses exec.CommandContext locally (NFR7).
	t.Run("codex_nil_runner_localspawn", func(t *testing.T) {
		wt := t.TempDir()
		if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
			t.Fatalf("mkdir .harmonik: %v", err)
		}
		rc := shared.LaunchCtx{
			RunID:           z8ekRunID(t),
			BeadID:          "hk-m4c7-codex-local",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "codex local NFR7",
			BeadDescription: "local body",
			Model:           "o4-mini",
			Runner:          nil, // LOCAL run — no worker
		}
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, codex.NewHarness("", ""), core.AgentTypeCodex)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (local codex): %v", err)
		}
		if spec.Runner != nil {
			t.Errorf("NFR7: codex local LaunchSpec.Runner = %#v; want nil (byte-identical exec.CommandContext local path)", spec.Runner)
		}
	})

	// ── Pi ───────────────────────────────────────────────────────────────────
	// Same nil-runner fall-through for the Pi harness (T6).
	t.Run("pi_nil_runner_localspawn", func(t *testing.T) {
		t.Setenv("OPENROUTER_API_KEY", "sk-test-m4c7")
		wt := t.TempDir()
		if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
			t.Fatalf("mkdir .harmonik: %v", err)
		}
		rc := shared.LaunchCtx{
			RunID:           z8ekRunID(t),
			BeadID:          "hk-m4c7-pi-local",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "pi local NFR7",
			BeadDescription: "local body",
			HandlerBinary:   "pi",
			Provider:        "openrouter",
			Model:           "openrouter/qwen/qwen3-coder",
			APIKeyEnv:       "OPENROUTER_API_KEY",
			BaseURL:         "http://dgx.local:8080/v1",
			API:             "openai",
			Runner:          nil, // LOCAL run — no worker
		}
		h := pi.NewHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (local pi): %v", err)
		}
		if spec.Runner != nil {
			t.Errorf("NFR7: pi local LaunchSpec.Runner = %#v; want nil (byte-identical exec.CommandContext local path)", spec.Runner)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// (3) Billing fail-closed (D2) — ANTHROPIC_API_KEY never on a remote spawn env,
//     enforced identically for Claude / Codex / Pi.
// ─────────────────────────────────────────────────────────────────────────────

// TestM4C7_BillingFailClosed_AllRemoteHarnesses proves the D2 chokepoint is
// harness-agnostic: the same hasAPIKeyInEnv guard that refuses a Claude remote run
// with ANTHROPIC_API_KEY in its spawn env would refuse a Codex or Pi remote run just
// the same, and neither Codex nor Pi introduces the key into its spawn env.
func TestM4C7_BillingFailClosed_AllRemoteHarnesses(t *testing.T) {
	ctx := context.Background()

	// ── Claude (re-assert the shared chokepoint; full matrix in b10 test) ─────
	t.Run("claude_key_refused_absent_ok", func(t *testing.T) {
		if !hasAPIKeyInEnv([]string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=sk-ant-x"}) {
			t.Error("D2: claude spawn env with ANTHROPIC_API_KEY not refused")
		}
		if hasAPIKeyInEnv([]string{"PATH=/usr/bin", "HOME=/root"}) {
			t.Error("D2: clean claude spawn env falsely refused")
		}
	})

	// codexRemoteSpecEnv / piRemoteSpecEnv build a WORKER-selected (remote) spec
	// and return its spawn env. The per-run runner is a RecordingRunner standing
	// in for the worker's SSHRunner (rc.runner != nil == remote).
	codexRemoteSpecEnv := func(t *testing.T) []string {
		t.Helper()
		wt := t.TempDir()
		if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
			t.Fatalf("mkdir .harmonik: %v", err)
		}
		rc := shared.LaunchCtx{
			RunID:           z8ekRunID(t),
			BeadID:          "hk-m4c7-codex-remote",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "codex remote D2",
			BeadDescription: "remote body",
			Model:           "o4-mini",
			BaseEnv:         []string{"PATH=/usr/bin"},
			Runner:          newNoOpRecorderZ8ek(), // REMOTE run (worker selected)
		}
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, codex.NewHarness("", ""), core.AgentTypeCodex)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (remote codex): %v", err)
		}
		if spec.Runner == nil {
			t.Fatal("precondition: remote codex spec must carry a non-nil Runner")
		}
		return spec.Env
	}

	piRemoteSpecEnv := func(t *testing.T) []string {
		t.Helper()
		t.Setenv("OPENROUTER_API_KEY", "sk-test-m4c7")
		wt := t.TempDir()
		if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
			t.Fatalf("mkdir .harmonik: %v", err)
		}
		rc := shared.LaunchCtx{
			RunID:           z8ekRunID(t),
			BeadID:          "hk-m4c7-pi-remote",
			WorkspacePath:   wt,
			Phase:           "implementer-initial",
			IterationCount:  1,
			BeadTitle:       "pi remote D2",
			BeadDescription: "remote body",
			HandlerBinary:   "pi",
			Provider:        "openrouter",
			Model:           "openrouter/qwen/qwen3-coder",
			APIKeyEnv:       "OPENROUTER_API_KEY",
			BaseURL:         "http://dgx.local:8080/v1",
			API:             "openai",
			BaseEnv:         []string{"PATH=/usr/bin"},
			Runner:          newNoOpRecorderZ8ek(), // REMOTE run (worker selected)
		}
		h := pi.NewHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, core.AgentTypePi)
		if err != nil {
			t.Fatalf("buildCodexRoutedLaunchSpec (remote pi): %v", err)
		}
		if spec.Runner == nil {
			t.Fatal("precondition: remote pi spec must carry a non-nil Runner")
		}
		return spec.Env
	}

	// Codex/Pi remote spawn env must NOT introduce ANTHROPIC_API_KEY on its own.
	t.Run("codex_remote_env_carries_no_anthropic_key", func(t *testing.T) {
		if hasAPIKeyInEnv(codexRemoteSpecEnv(t)) {
			t.Error("D2: codex remote spawn env carries ANTHROPIC_API_KEY (must never be forwarded to a worker)")
		}
	})
	t.Run("pi_remote_env_carries_no_anthropic_key", func(t *testing.T) {
		if hasAPIKeyInEnv(piRemoteSpecEnv(t)) {
			t.Error("D2: pi remote spawn env carries ANTHROPIC_API_KEY (must never be forwarded to a worker)")
		}
	})

	// The shared D2 chokepoint operates on the Codex/Pi spec's OWN env slice exactly
	// as it does for Claude: if a leaked ANTHROPIC_API_KEY ever reached the spawn env
	// of any harness, workloop's hasAPIKeyInEnv(spec.Env) refuses the remote run. We
	// append the key to the actual codex/pi spec.Env slice — the identical argument
	// the workloop guard receives — and assert the chokepoint catches it.
	t.Run("codex_spec_env_with_leaked_key_is_caught", func(t *testing.T) {
		env := append(codexRemoteSpecEnv(t), "ANTHROPIC_API_KEY=sk-ant-leak")
		if !hasAPIKeyInEnv(env) {
			t.Error("D2: a leaked ANTHROPIC_API_KEY in the codex remote spec.Env was NOT caught by the shared chokepoint")
		}
	})
	t.Run("pi_spec_env_with_leaked_key_is_caught", func(t *testing.T) {
		env := append(piRemoteSpecEnv(t), "ANTHROPIC_API_KEY=sk-ant-leak")
		if !hasAPIKeyInEnv(env) {
			t.Error("D2: a leaked ANTHROPIC_API_KEY in the pi remote spec.Env was NOT caught by the shared chokepoint")
		}
	})
}

func TestM4C7_D2RemoteAPIKeyRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		remote bool
		env    []string
		refuse bool
	}{
		{name: "remote live key", remote: true, env: []string{"ANTHROPIC_API_KEY=secret"}, refuse: true},
		{name: "remote inherited key", remote: true, env: []string{"ANTHROPIC_API_KEY"}, refuse: true},
		{name: "local live key", env: []string{"ANTHROPIC_API_KEY=secret"}},
		{name: "remote empty override", remote: true, env: []string{"ANTHROPIC_API_KEY="}},
		{name: "remote clean environment", remote: true, env: []string{"PATH=/usr/bin"}},
		{name: "remote empty environment", remote: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refusal, refused := d2RemoteAPIKeyRefusal(tc.remote, tc.env)
			if refused != tc.refuse {
				t.Fatalf("refused = %v, want %v", refused, tc.refuse)
			}
			if refused && refusal != d2APIKeyRefusal {
				t.Errorf("refusal = %q, want %q", refusal, d2APIKeyRefusal)
			}
			if !refused && refusal != "" {
				t.Errorf("non-refusal returned reason %q", refusal)
			}
		})
	}
}

// TestM4C7_D2Chokepoint_IsHarnessAgnostic proves the D2 fail-closed check is
// applied to whatever the specBuilder produced. The behavioral decision is made
// by d2RemoteAPIKeyRefusal; this sensor proves beadRunOne passes the harness-
// agnostic remote predicate and built spec environment to it, then fails the run
// and returns before launch.
//
// WHY THIS IS A STATIC TEST, DELIBERATELY. The guard lives inside beadRunOne
// (workloop.go), a ~2,200-line function whose remote arm needs a live worker, an
// ssh runner and a reverse tunnel to reach. There is no cheap behavioural route to
// the branch, and the credential-leak class is severe enough to warrant a
// structural assertion rather than no assertion. The predicate itself is covered
// behaviorally above; what is asserted here is only its composition-root wiring.
//
// It parses the AST rather than grepping source text. The previous version took a
// 200-character window before the call site and string-matched inside it. That was
// not merely brittle, it was UNSOUND — it passes on code where the guard has been
// completely un-gated, as long as an unrelated `rbc != nil` happens to sit in the
// preceding window. Demonstrated:
//
//	if rbc != nil { setupTunnel() }
//	log("some intervening work here")
//	if hasAPIKeyInEnv(spec.Env) { return }   // <- NOT gated on remote any more
//
// The 200-char window contains "rbc != nil" (from the FIRST if), so the old
// assertion returned true and the suite stayed green while the credential guard
// was disarmed. The AST form anchors the call to beadRunOne, checks identifier
// bindings, and requires the guarded body to pass the typed reason to failRun
// before returning.
func TestM4C7_D2Chokepoint_IsHarnessAgnostic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootForConformance(), "internal", "daemon", "workloop.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse workloop.go: %v", err)
	}

	if !hasValidD2Wiring(file) {
		t.Fatal("beadRunOne must call d2RemoteAPIKeyRefusal(rbc != nil, spec.Env), failRun then return on refusal, immediately before its unique launch")
	}
}

func TestM4C7_D2Wiring_RejectsAdversarialMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		want       bool
		ownsLaunch bool
	}{
		{name: "canonical", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }`, want: true},
		{name: "decoy outside beadRunOne", body: `return`},
		{name: "valid decoy plus bypass", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }; _ = d2RemoteAPIKeyRefusal(false, spec.Env)`},
		{name: "disabled remote", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil && false, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }`},
		{name: "agent narrowed", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil && isClaude, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }`},
		{name: "or remote", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil || local, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }`},
		{name: "body log only", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); log(reason); return }`},
		{name: "intervening side effect", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); log(reason); return }`},
		{name: "nested launch before fail", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); if retry { sess0, watcher0, err0 := runH.Launch(ctx, spec) }; failRun(reason, reason); return }`},
		{name: "no return", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason) }`},
		{name: "shadow refusal", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { refusal := d2Refusal("other"); reason := string(refusal); failRun(reason, reason); return }`},
		{name: "wrong environment", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, other.Env); refused { reason := string(refusal); failRun(reason, reason); return }`},
		{name: "environment mutation after guard", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }; spec.Env = append(spec.Env, "ANTHROPIC_API_KEY=late")`},
		{name: "guard after launch", body: `sess, watcher, err := runH.Launch(ctx, spec); if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }`, ownsLaunch: true},
		{name: "duplicate launch ambiguity", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return }; sess, watcher, err := runH.Launch(ctx, spec); sess2, watcher2, err2 := runH.Launch(ctx, spec)`, ownsLaunch: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			if !tc.ownsLaunch {
				body += `; sess, watcher, err := runH.Launch(ctx, spec)`
			}
			src := "package fixture\nfunc beadRunOne() { " + body + " }\n"
			if tc.name == "decoy outside beadRunOne" {
				src += `func decoy() { if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return } }`
			}
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", src, 0)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got := hasValidD2Wiring(file); got != tc.want {
				t.Errorf("hasValidD2Wiring() = %v, want %v", got, tc.want)
			}
		})
	}
}

func hasValidD2Wiring(file *ast.File) bool {
	var beadRunOne *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "beadRunOne" {
			if beadRunOne != nil {
				return false
			}
			beadRunOne = fn
		}
	}
	if beadRunOne == nil || beadRunOne.Body == nil {
		return false
	}

	valid := 0
	var guardPos token.Pos
	var launchPos token.Pos
	var directLaunchPos token.Pos
	guardIndex, launchIndex := -1, -1
	decisionCalls := 0
	ast.Inspect(beadRunOne.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && isIdent(call.Fun, "d2RemoteAPIKeyRefusal") {
			decisionCalls++
		}
		if ok && isRunLaunchCall(call) {
			if launchPos != token.NoPos {
				launchPos = -1 // more than one launch is always ambiguous
			} else {
				launchPos = call.Pos()
			}
		}
		return true
	})
	for i, stmt := range beadRunOne.Body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if ok && isValidD2If(ifStmt) {
			valid++
			guardPos = ifStmt.Pos()
			guardIndex = i
		}
		if assign, ok := stmt.(*ast.AssignStmt); ok && len(assign.Rhs) == 1 {
			if call, ok := assign.Rhs[0].(*ast.CallExpr); ok && isRunLaunchCall(call) {
				directLaunchPos = call.Pos()
				launchIndex = i
			}
		}
	}
	return valid == 1 && decisionCalls == 1 && launchPos > token.NoPos &&
		directLaunchPos == launchPos && guardPos < launchPos && launchIndex == guardIndex+1
}

func isValidD2If(ifStmt *ast.IfStmt) bool {
	init, ok := ifStmt.Init.(*ast.AssignStmt)
	if !ok || init.Tok != token.DEFINE || len(init.Lhs) != 2 || len(init.Rhs) != 1 {
		return false
	}
	refusal, refusalOK := init.Lhs[0].(*ast.Ident)
	refused, refusedOK := init.Lhs[1].(*ast.Ident)
	call, callOK := init.Rhs[0].(*ast.CallExpr)
	if !refusalOK || !refusedOK || !callOK || !isD2DecisionCall(call) {
		return false
	}
	cond, ok := unparenExpr(ifStmt.Cond).(*ast.Ident)
	if !ok || cond.Obj == nil || cond.Obj != refused.Obj {
		return false
	}

	var reason *ast.Ident
	failIndex, returnIndex := -1, -1
	for i, stmt := range ifStmt.Body.List {
		if assign, ok := stmt.(*ast.AssignStmt); ok && assign.Tok == token.DEFINE && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
			id, idOK := assign.Lhs[0].(*ast.Ident)
			conversion, convOK := assign.Rhs[0].(*ast.CallExpr)
			if idOK && convOK && len(conversion.Args) == 1 && isIdent(conversion.Fun, "string") {
				arg, argOK := unparenExpr(conversion.Args[0]).(*ast.Ident)
				if argOK && arg.Obj != nil && arg.Obj == refusal.Obj {
					reason = id
				}
			}
		}
		if exprStmt, ok := stmt.(*ast.ExprStmt); ok && reason != nil {
			if call, ok := exprStmt.X.(*ast.CallExpr); ok && isFailRunWithReason(call, reason) {
				failIndex = i
			}
		}
		if ret, ok := stmt.(*ast.ReturnStmt); ok && len(ret.Results) == 0 {
			returnIndex = i
		}
	}
	return failIndex >= 0 && returnIndex == failIndex+1
}

func isRunLaunchCall(call *ast.CallExpr) bool {
	if len(call.Args) != 2 || !isIdent(call.Args[0], "ctx") || !isIdent(call.Args[1], "spec") {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(selector.X, "runH") && selector.Sel.Name == "Launch"
}

func isD2DecisionCall(call *ast.CallExpr) bool {
	if !isIdent(call.Fun, "d2RemoteAPIKeyRefusal") || len(call.Args) != 2 {
		return false
	}
	remote, ok := unparenExpr(call.Args[0]).(*ast.BinaryExpr)
	if !ok || remote.Op != token.NEQ || !isIdent(remote.X, "rbc") || !isNil(remote.Y) {
		return false
	}
	env, ok := unparenExpr(call.Args[1]).(*ast.SelectorExpr)
	return ok && isIdent(env.X, "spec") && env.Sel.Name == "Env"
}

func isFailRunWithReason(call *ast.CallExpr, reason *ast.Ident) bool {
	if !isIdent(call.Fun, "failRun") || len(call.Args) != 2 {
		return false
	}
	left, leftOK := unparenExpr(call.Args[0]).(*ast.Ident)
	right, rightOK := unparenExpr(call.Args[1]).(*ast.Ident)
	return leftOK && rightOK && reason.Obj != nil && left.Obj == reason.Obj && right.Obj == reason.Obj
}

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := unparenExpr(expr).(*ast.Ident)
	return ok && ident.Name == name
}

func isNil(expr ast.Expr) bool {
	return isIdent(expr, "nil")
}

// ─────────────────────────────────────────────────────────────────────────────
// (2) Seam-survival — the remote seam is NOT deleted; no dual-path branch removed.
// ─────────────────────────────────────────────────────────────────────────────

// TestM4C7_SeamSurvival_StructuralFloors is the DEC-A-deferred (decision 5) guard:
// M4 wires new harnesses onto the EXISTING remote seam and deletes nothing. This
// static audit fails if any load-bearing seam symbol drops below its expected floor.
func TestM4C7_SeamSurvival_StructuralFloors(t *testing.T) {
	t.Parallel()

	// (a) The CommandRunner seam + its two implementations must exist verbatim.
	runnerSrc := readRepoFile(t, "internal", "lifecycle", "tmux", "runner.go")
	for _, decl := range []string{
		"type CommandRunner interface",
		"type LocalRunner struct",
		"type SSHRunner struct",
	} {
		if !strings.Contains(runnerSrc, decl) {
			t.Errorf("seam deleted: %q missing from internal/lifecycle/tmux/runner.go", decl)
		}
	}

	// (b) The reverse-tunnel seam must exist. It left internal/daemon in P2 unit
	// E4a and now lives in internal/transport/tunnel; the seam itself is unchanged.
	rtSrc := readRepoFile(t, "internal", "transport", "tunnel", "tunnel.go")
	for _, sym := range []string{"ReverseTunnelRunner", "BuildArgs"} {
		if !strings.Contains(rtSrc, sym) {
			t.Errorf("seam deleted: reverse-tunnel symbol %q missing from internal/transport/tunnel/tunnel.go", sym)
		}
	}

	// (c) The runner-threading through the shared harness builder must survive.
	hrSrc := readRepoFile(t, "internal", "daemon", "harnessregistry.go")
	if !strings.Contains(hrSrc, "Runner: rc.Runner") {
		t.Error("seam deleted: harnessregistry.go no longer threads the per-run runner (Runner: rc.Runner)")
	}

	// (d) …Via(runner) helpers — the CommandRunner-aware file I/O seam. M4 landed
	// with a large family of them; a floor guards against a wholesale collapse to
	// bare os.* (which would silently break remote runs).
	viaFloor := 12
	if n := countViaHelpers(t); n < viaFloor {
		t.Errorf("seam eroded: only %d …Via helper decls across internal/daemon+internal/workspace; want >= %d (DEC-A cleanup is DEFERRED)", n, viaFloor)
	}

	// (e) REMOVED 2026-07-22 — the `strings.Count(workloop.go, "rbc != nil") >= 8` floor.
	//
	// It asserted a magic number of occurrences of a string in a source file, as a
	// proxy for "the remote/local dual path has not been collapsed". That is not a
	// test of behaviour, and it failed on three counts:
	//
	//   1. It could not detect the thing it claimed to. Deleting the local
	//      fall-through entirely while leaving eight `rbc != nil` predicates
	//      elsewhere passes. Conversely a legitimate refactor that consolidates
	//      predicates fails. The signal is uncorrelated with the invariant.
	//   2. The threshold was arbitrary and slack — the floor was 8 against an
	//      actual count of 20, so it only tripped after a change had already
	//      removed 60% of the branch sites.
	//   3. It obstructed exactly the refactoring P2 exists to do: any extraction
	//      touching the remote path trips it for reasons unrelated to correctness.
	//
	// The invariant it was reaching for — that the local and remote paths BOTH still
	// work — is covered behaviourally, and those tests fail for the right reasons:
	//
	//   - TestSingleModeWorkloopThreadsRunnerIntoSubstrate_hkfxy9  (substrate_runner_parity_hkfxy9_test.go)
	//         local run threads its runner into the substrate
	//   - TestReviewLoopReviewerSubstrateRunnerIsNil_hkfxy9        (substrate_runner_parity_hkfxy9_test.go)
	//         a local reviewer run carries a nil runner
	//   - TestScenario_RemoteSubstrate_Localhost_E2E               (scenario_remote_substrate_localhost_test.go)
	//         the remote path end-to-end over a localhost worker
	//   - TestScenario_RemoteSubstrate_NoWorker_RunStartedWorkerNameEmpty
	//         no worker available => the run falls through to local
	//
	// The D2 credential guard, which is the one genuinely security-critical
	// structural property here, keeps a static test — but an AST-based one. See
	// TestM4C7_D2Chokepoint_IsHarnessAgnostic above for why that one stays static.
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

// repoRootForConformance returns the repository root (the directory two levels
// above this test file: internal/daemon/ → repo root).
func repoRootForConformance() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = .../internal/daemon/conformance_m4c7_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// readRepoFile reads a source file addressed by path segments relative to the repo
// root, failing the test if it cannot be read.
func readRepoFile(t *testing.T, segments ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{repoRootForConformance()}, segments...)...)
	data, err := os.ReadFile(path) //nolint:gosec // G304: repo-relative source path, test-only
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// countViaHelpers counts distinct `func …Via(` declarations across the daemon and
// workspace packages — the CommandRunner-aware file-I/O seam.
func countViaHelpers(t *testing.T) int {
	t.Helper()
	seen := map[string]bool{}
	for _, dir := range []string{
		filepath.Join(repoRootForConformance(), "internal", "daemon"),
		filepath.Join(repoRootForConformance(), "internal", "workspace"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			f, err := os.Open(filepath.Join(dir, name)) //nolint:gosec // G304: repo source, test-only
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if !strings.HasPrefix(line, "func ") {
					continue
				}
				fn := extractViaFuncName(line)
				if fn != "" {
					seen[fn] = true
				}
			}
			_ = f.Close()
		}
	}
	return len(seen)
}

// extractViaFuncName returns the function name from a `func …` declaration line if
// it ends in "Via" (optionally with a receiver), else "".
func extractViaFuncName(line string) string {
	s := strings.TrimPrefix(line, "func ")
	if strings.HasPrefix(s, "(") { // method receiver
		end := strings.Index(s, ")")
		if end < 0 {
			return ""
		}
		s = strings.TrimSpace(s[end+1:])
	}
	name := s
	for i, c := range s {
		if c == '(' || c == '[' || c == ' ' {
			name = s[:i]
			break
		}
	}
	if strings.HasSuffix(name, "Via") && name != "Via" {
		return name
	}
	return ""
}
