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
// WHY THIS IS A STATIC TEST — AND WHAT IT CANNOT SEE. This sensor asserts the
// guard's SHAPE: that the harness-agnostic remote predicate and the built spec
// environment reach d2RemoteAPIKeyRefusal, and that the refusal is reported and
// returned before the launch. Shape is all it asserts. It cannot see whether
// reporting the refusal does anything — gut refuseLaunch's body and every shape
// fact here is still true while a refused remote run becomes indistinguishable
// from one that launched and completed. That is not hypothetical; it was
// demonstrated, and the suite stayed green.
//
// So this sensor is HALF the gate, not the gate. The other half is
// agentlaunch_behavior_test.go, which calls runAgentLaunch and asserts on the
// RESULT. The older claim that there was "no cheap behavioural route to the
// branch" was true of beadRunOne, whose remote arm needed a live worker, an ssh
// runner and a reverse tunnel; it stopped being true when the guard moved to
// runAgentLaunch, which is reachable with neither tmux nor a worker. If the
// guard moves again, move BOTH halves — a shape assertion alone has already
// been shown to protect nothing.
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
//
// INDIRECT LAUNCH (2026-07-28). The launch is no longer a statement in
// beadRunOne's own body: it moved inside the Launch closure of a
// runloop.DispatchSegment composite literal, which a later statement runs. The
// earlier form of this checker demanded the launch statement sit IMMEDIATELY
// after the guard, so it went red the moment the launch moved — while the
// security property still held. A false red on a security gate is how a gate
// stops being read, and then it protects nothing. The rule is now DOMINANCE
// rather than adjacency, and it is shape-agnostic — it holds for a direct launch
// and for a launch reached through a closure:
//
//	the guard is a top-level statement of beadRunOne at index G;
//	the sole launch is lexically inside the top-level statement at index L;
//	G < L, and beyond the guard the identifier spec appears only as the
//	launched value — no assignment, no alias, no read.
//
// Defining the launch closure after the guard is what makes G < L sufficient: a
// closure that does not exist yet cannot already have been invoked, so
// dominating the DEFINITION dominates every invocation. A launch closure built
// BEFORE the guard is refused — not because it is provably unsafe, but because
// this checker can no longer prove it safe, and a credential gate fails closed.
//
// THE GUARD MOVED (2026-07-29, launch-path collapse). beadRunOne no longer
// launches anything: the three hand-written launch paths (single-mode, the DOT
// agentic node, the cognition gate) collapsed into runAgentLaunch
// (agentlaunch.go), which is now the package's ONLY handler.Launch call site.
// The guard moved with it, and this sensor is re-anchored there. The property is
// unchanged and its REACH is strictly larger: one guard now dominates all three
// launches instead of one of three — the DOT node and the cognition gate
// previously had NO credential guard at all, which is the defect the collapse
// eliminated by construction.
//
// Two spellings changed with the move and the shape predicates accept both:
//
//   - the remote predicate. beadRunOne spelled it `rbc != nil`; runAgentLaunch
//     receives it as `in.Remote`. Both are accepted as a WHOLE, unmodified
//     predicate — a bare identifier or field selector, never a composite. That
//     is what keeps `in.Remote && false` and `in.Remote && isClaude` refused,
//     exactly as `rbc != nil && false` always was.
//   - the refusal report. beadRunOne called failRun(reason, reason) because it
//     owned the run's terminal spine; runAgentLaunch calls refuseLaunch(reason)
//     because after the collapse the CALLER owns what a refusal means. Either
//     is accepted; the strict part — report, then return, with nothing in
//     between — is unchanged.
func TestM4C7_D2Chokepoint_IsHarnessAgnostic(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootForConformance(), "internal", "daemon", "agentlaunch.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse agentlaunch.go: %v", err)
	}

	if !hasValidD2Wiring(file, "runAgentLaunch") {
		t.Fatal("runAgentLaunch must call d2RemoteAPIKeyRefusal(in.Remote, spec.Env), report the refusal then return, in a top-level statement that DOMINATES its unique launch (direct, or inside a DispatchSegment closure defined after the guard); beyond that guard the identifier spec may appear ONLY as the launched value — a post-guard read is refused along with a write, so if you added a harmless-looking spec.<Field> read after the guard, move it above the guard")
	}
}

// d2GuardFixture is the canonical guard as beadRunOne spells it, including the
// `return false` demanded by its named `succeeded bool` result.
const d2GuardFixture = `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return false }`

// d2SegmentFixture is the INDIRECT launch shape: the launch lives in a closure
// hanging off a runloop.DispatchSegment literal, which a separate statement runs.
// This is the shape beadRunOne actually has since the segment extraction.
const d2SegmentFixture = `implSeg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; implDispatch := implSeg.Run(ctx)`

// d2CollapsedGuardFixture is the canonical guard as runAgentLaunch spells it
// after the launch-path collapse: the remote predicate arrives on the input
// struct, the refusal is reported through refuseLaunch (the caller decides what
// it means), and the function returns its result value.
const d2CollapsedGuardFixture = `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }`

func TestM4C7_D2Wiring_RejectsAdversarialMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		want       bool
		ownsLaunch bool
		// fn is the function the fixture body is wrapped in and the name the
		// checker is anchored to. Empty means beadRunOne — the pre-collapse
		// spelling, kept verbatim so every adversarial case below still exercises
		// the shape it was written to catch.
		fn string
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

		// ── INDIRECT SHAPE: the launch lives in a DispatchSegment closure ──────
		// The cases above all assume the launch is a statement of beadRunOne. Since
		// the segment extraction it is not, so every bypass has an indirect twin;
		// without these, teaching the checker to follow the closure would MOVE the
		// unsoundness rather than fix it.
		{name: "indirect canonical", body: d2GuardFixture + `; ` + d2SegmentFixture, want: true, ownsLaunch: true},
		// The guard refuses and returns, but the launch it was meant to prevent is
		// built and run on the refusal path itself — a guard that returns whose
		// segment still launches.
		{name: "indirect launch inside the refusal branch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); seg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; seg.Run(ctx); failRun(reason, reason); return false }`, ownsLaunch: true},
		// The launch closure exists before the guard runs, so the guard cannot be
		// shown to dominate every invocation of it. Fail closed.
		{name: "indirect segment constructed before the guard", body: d2SegmentFixture + `; ` + d2GuardFixture, ownsLaunch: true},
		// A second launch closure ahead of the guard. Refused by the one-launch
		// rule rather than by the dominance comparison — which is the point: two
		// launch sites make dominance a question this checker will not answer, so
		// it fails closed instead of picking one.
		{name: "indirect second launch closure ahead of the guard", body: `preSeg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; ` + d2GuardFixture + `; ` + d2SegmentFixture + `; preDispatch := preSeg.Run(ctx)`, ownsLaunch: true},
		// The guard demoted into the closure returns from the CLOSURE, not from
		// beadRunOne, so the run is never failed and the refusal is invisible.
		{name: "indirect guard demoted into the launch closure", body: `implSeg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return nil, nil }; sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; implDispatch := implSeg.Run(ctx)`, ownsLaunch: true},
		// Refusing a run but reporting it as succeeded is its own defect, so the
		// only accepted refusal returns are `return` and `return false`.
		{name: "indirect refusal returns success", body: `if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return true }; ` + d2SegmentFixture, ownsLaunch: true},

		// ── POST-GUARD TAMPERING ──────────────────────────────────────────────
		// Adjacency used to make these impossible by leaving nowhere to put them.
		// Dominance opens the gap, so each shape gets its own case. Only the first
		// is an assignment to spec.Env; the rest leak through an alias, a callee or
		// a receiver, and a checker that looked for assignments would pass them all.
		{name: "post-guard environment append", body: d2GuardFixture + `; spec.Env = append(spec.Env, "ANTHROPIC_API_KEY=late"); ` + d2SegmentFixture, ownsLaunch: true},
		// Hidden one level deeper: the closure re-adds the key just before spawning,
		// where a checker that only walked top-level statements would never look.
		{name: "post-guard environment append inside the launch closure", body: d2GuardFixture + `; implSeg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { spec.Env = append(spec.Env, "ANTHROPIC_API_KEY=late"); sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; implDispatch := implSeg.Run(ctx)`, ownsLaunch: true},
		// A slice alias shares the backing array, so writing through it mutates the
		// spawn env with no assignment to spec anywhere.
		{name: "post-guard environment alias write", body: d2GuardFixture + `; envAlias := spec.Env; envAlias[0] = "ANTHROPIC_API_KEY=leak"; ` + d2SegmentFixture, ownsLaunch: true},
		// The mutation moves into a callee, out of this function's text entirely.
		{name: "post-guard mutation via helper call", body: d2GuardFixture + `; injectWorkerEnv(spec); ` + d2SegmentFixture, ownsLaunch: true},
		// Same, through a method on the spec itself.
		{name: "post-guard mutation via method call", body: d2GuardFixture + `; spec.AddEnv("ANTHROPIC_API_KEY=leak"); ` + d2SegmentFixture, ownsLaunch: true},
		// And through a pointer to the env field.
		{name: "post-guard mutation via pointer alias", body: d2GuardFixture + `; envPtr := &spec.Env; *envPtr = append(*envPtr, "ANTHROPIC_API_KEY=leak"); ` + d2SegmentFixture, ownsLaunch: true},
		// Reading spec after the guard is refused too. It is very likely harmless,
		// but distinguishing a read from a write is exactly the reasoning that let
		// the shapes above through, so the rule stays blunt and this case pins it.
		{name: "post-guard read of spec is refused too", body: d2GuardFixture + `; logf("binary=%s", spec.Binary); ` + d2SegmentFixture, ownsLaunch: true},

		// KNOWN GAP, pinned deliberately as want:true. An alias taken BEFORE the
		// guard and written after it leaks through the shared backing array without
		// naming spec anywhere the checker looks, so the gate stays green. This case
		// exists so the limit is a recorded fact rather than a discovery: if someone
		// later adds alias tracking, this case fails and gets flipped to want:false —
		// which is the notification. The wider defence is that the launch spec is
		// built once and not passed around; see hasValidD2Wiring's KNOWN GAP note.
		{name: "known gap: pre-guard alias written after the guard", body: `preAlias := spec.Env; ` + d2GuardFixture + `; preAlias[0] = "ANTHROPIC_API_KEY=leak"; ` + d2SegmentFixture, want: true, ownsLaunch: true},

		// ── POST-COLLAPSE SHAPE (runAgentLaunch) ──────────────────────────────
		// The guard moved into runAgentLaunch with the launch, and spells its two
		// variable parts differently: `in.Remote` for the remote predicate and
		// refuseLaunch(reason) for the report, because the caller now owns what a
		// refusal means. These twins prove the loosened predicates did not loosen
		// the property: every bypass the old shape refuses, the new shape refuses.
		{name: "collapsed canonical", fn: "runAgentLaunch", body: d2CollapsedGuardFixture + `; ` + d2SegmentFixture, want: true, ownsLaunch: true},
		{name: "collapsed disabled remote", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote && false, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed agent narrowed", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote && isClaude, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed or remote", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote || local, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed hardcoded remote false", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(false, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed wrong environment", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, other.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed report only, no return", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason) }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed log instead of report", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); log(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed intervening side effect", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); log(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed refusal returns success", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return true }; ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed segment constructed before the guard", fn: "runAgentLaunch", body: d2SegmentFixture + `; ` + d2CollapsedGuardFixture, ownsLaunch: true},
		{name: "collapsed post-guard environment append", fn: "runAgentLaunch", body: d2CollapsedGuardFixture + `; spec.Env = append(spec.Env, "ANTHROPIC_API_KEY=late"); ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed post-guard read of spec is refused too", fn: "runAgentLaunch", body: d2CollapsedGuardFixture + `; logf("binary=%s", spec.Binary); ` + d2SegmentFixture, ownsLaunch: true},
		{name: "collapsed guard demoted into the launch closure", fn: "runAgentLaunch", body: `implSeg := &runloop.DispatchSegment{ Launch: func(lctx context.Context) (<-chan struct{}, error) { if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return nil, nil }; sess, watcher, launchErr = runH.Launch(lctx, spec); return nil, nil } }; implDispatch := implSeg.Run(ctx)`, ownsLaunch: true},
		// The remote predicate reads a Remote field off a DECOY struct instead of
		// the launch input. It reads as correct and disarms the gate — the same
		// substitution "wrong environment" refuses on the other argument, so the
		// selector base is pinned to `in` and this is refused too.
		{name: "collapsed remote predicate off a decoy struct", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(decoy.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return res }; ` + d2SegmentFixture, ownsLaunch: true},
		// A refusal that returns a named bool result which may well be true is the
		// same defect as `return true`, so the accepted return identifiers are a
		// closed set rather than "anything but true".
		{name: "collapsed refusal returns a named success result", fn: "runAgentLaunch", body: `if refusal, refused := d2RemoteAPIKeyRefusal(in.Remote, spec.Env); refused { reason := string(refusal); refuseLaunch(reason); return succeeded }; ` + d2SegmentFixture, ownsLaunch: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fn := tc.fn
			if fn == "" {
				fn = "beadRunOne"
			}
			body := tc.body
			if !tc.ownsLaunch {
				body += `; sess, watcher, err := runH.Launch(ctx, spec)`
			}
			src := "package fixture\nfunc " + fn + "() { " + body + " }\n"
			if tc.name == "decoy outside beadRunOne" {
				src += `func decoy() { if refusal, refused := d2RemoteAPIKeyRefusal(rbc != nil, spec.Env); refused { reason := string(refusal); failRun(reason, reason); return } }`
			}
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", src, 0)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got := hasValidD2Wiring(file, fn); got != tc.want {
				t.Errorf("hasValidD2Wiring() = %v, want %v", got, tc.want)
			}
		})
	}
}

// hasValidD2Wiring reports whether beadRunOne's D2 credential guard dominates its
// launch. See TestM4C7_D2Chokepoint_IsHarnessAgnostic's doc comment for why this is
// structural and why the rule is dominance rather than adjacency.
//
// Four conditions, all necessary:
//
//	(a) exactly one beadRunOne, holding exactly one d2RemoteAPIKeyRefusal call and
//	    exactly one launch — a second decision call is a bypass, a second launch is
//	    a path the guard may not cover, and both are refused rather than reasoned about;
//	(b) the sole decision call is the init of a well-formed guard (isValidD2If) that
//	    is a TOP-LEVEL statement of beadRunOne, so returning from it leaves the function;
//	(c) that guard's statement index is strictly less than the index of the top-level
//	    statement lexically containing the launch — whether the launch is that
//	    statement itself or sits inside a closure it defines;
//	(d) after the guard, the identifier spec is mentioned exactly once — as the
//	    launched value — so the bytes the guard inspected are the bytes spawned.
//
// (d) is deliberately blunt. The adjacency rule this checker replaced made
// post-guard tampering structurally impossible by leaving no room for it; nothing
// weaker than "do not touch spec at all after the guard" recovers that. A rule
// that hunted for assignments specifically would miss the aliasing shapes —
// `env := spec.Env; env[0] = …`, `inject(spec)`, `spec.AddEnv(…)`,
// `p := &spec.Env; *p = append(*p, …)` — every one of which leaks through a
// backing array or a receiver without an assignment to spec in sight.
//
// KNOWN GAP, stated rather than papered over: (d) sees only the identifier spec,
// so anything that binds a handle to it BEFORE the guard escapes entirely. The
// simplest form needs no closure and no call at all —
//
//	preAlias := spec.Env                          // before the guard
//	<guard>
//	preAlias[0] = "ANTHROPIC_API_KEY=leak"        // leaks through the backing array
//
// and the same is true of a helper or closure that captured spec earlier and is
// invoked later. Catching these needs alias tracking, which a conformance test is
// the wrong place for. The gap is pinned by the "known gap" case in
// TestM4C7_D2Wiring_RejectsAdversarialMutations, so it stays a documented limit
// rather than becoming a surprise. The decision function itself is covered
// behaviourally by TestM4C7_D2RemoteAPIKeyRefusal and
// TestM4C7_BillingFailClosed_AllRemoteHarnesses; what is asserted here is only
// the wiring.
func hasValidD2Wiring(file *ast.File, fnName string) bool {
	var beadRunOne *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == fnName {
			if beadRunOne != nil {
				return false
			}
			beadRunOne = fn
		}
	}
	if beadRunOne == nil || beadRunOne.Body == nil {
		return false
	}

	// (a) One decision, one launch — anywhere in the function, closures included.
	decisionCalls, launchCalls := 0, 0
	var launch *ast.CallExpr
	ast.Inspect(beadRunOne.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isIdent(call.Fun, "d2RemoteAPIKeyRefusal") {
			decisionCalls++
		}
		if isRunLaunchCall(call) {
			launchCalls++
			launch = call
		}
		return true
	})
	if decisionCalls != 1 || launchCalls != 1 {
		return false
	}

	// (b) The guard is a well-formed, top-level statement.
	var guard *ast.IfStmt
	guardIndex := -1
	for i, stmt := range beadRunOne.Body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || !isValidD2If(ifStmt) {
			continue
		}
		if guardIndex >= 0 {
			return false
		}
		guard, guardIndex = ifStmt, i
	}
	if guardIndex < 0 {
		return false
	}

	// (c) The guard dominates the launch. A launch inside the guard's own body
	// resolves to launchIndex == guardIndex and is refused by the same comparison.
	launchIndex := topLevelStmtIndexContaining(beadRunOne.Body.List, launch)
	if launchIndex < 0 || guardIndex >= launchIndex {
		return false
	}

	// (d) The inspected environment is the launched environment.
	return specUntouchedAfter(beadRunOne.Body, guard.End(), unparenExpr(launch.Args[1]))
}

// specUntouchedAfter reports whether every mention of the identifier spec beyond
// after is the launched value itself. Anything else — an assignment, an alias, a
// helper call, a method call, taking its address — is refused without trying to
// decide whether that particular shape happens to be harmless.
func specUntouchedAfter(body *ast.BlockStmt, after token.Pos, launchArg ast.Expr) bool {
	clean := true
	ast.Inspect(body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != "spec" || ident.Pos() <= after {
			return true
		}
		if ident.Pos() != launchArg.Pos() {
			clean = false
		}
		return true
	})
	return clean
}

// topLevelStmtIndexContaining returns the index of the top-level statement that
// lexically contains target, or -1. This is what makes the checker indifferent to
// whether the launch is a direct statement or lives in a closure the statement builds.
func topLevelStmtIndexContaining(list []ast.Stmt, target ast.Node) int {
	for i, stmt := range list {
		found := false
		ast.Inspect(stmt, func(node ast.Node) bool {
			if node == target {
				found = true
			}
			return !found
		})
		if found {
			return i
		}
	}
	return -1
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
			if call, ok := exprStmt.X.(*ast.CallExpr); ok && isRefusalReportWithReason(call, reason) {
				failIndex = i
			}
		}
		if ret, ok := stmt.(*ast.ReturnStmt); ok && isRefusalReturn(ret) {
			returnIndex = i
		}
	}
	return failIndex >= 0 && returnIndex == failIndex+1
}

// isRefusalReturn accepts exactly the three spellings that abandon the run: a
// bare `return`, `return false`, and `return res`. beadRunOne has a named
// `succeeded bool` result so its guard spells it `return false`; runAgentLaunch
// returns its populated result value so its guard spells it `return res`.
//
// The allow-list is a closed set of names rather than "any identifier that is
// not true". A blanket ident rule would accept `return succeeded` — an
// identifier that may well BE true — which is the same "refused run reported as
// successful" defect `return true` is refused for. Naming the two accepted
// identifiers costs nothing and leaves no judgment to a future reader.
func isRefusalReturn(ret *ast.ReturnStmt) bool {
	switch len(ret.Results) {
	case 0:
		return true
	case 1:
		ident, ok := unparenExpr(ret.Results[0]).(*ast.Ident)
		return ok && (ident.Name == "false" || ident.Name == "res")
	default:
		return false
	}
}

// isRunLaunchCall matches `runH.Launch(<ctx>, spec)`. The context argument is any
// identifier: inside the DispatchSegment closure the segment supplies its own
// launch context (`lctx`), and pinning the name would re-create the brittleness
// this checker exists to avoid. What is load-bearing is that the launched value is
// `spec` — the same object the guard inspected.
func isRunLaunchCall(call *ast.CallExpr) bool {
	if len(call.Args) != 2 || !isIdent(call.Args[1], "spec") {
		return false
	}
	if _, ok := unparenExpr(call.Args[0]).(*ast.Ident); !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && isIdent(selector.X, "runH") && selector.Sel.Name == "Launch"
}

func isD2DecisionCall(call *ast.CallExpr) bool {
	if !isIdent(call.Fun, "d2RemoteAPIKeyRefusal") || len(call.Args) != 2 {
		return false
	}
	if !isWholeRemotePredicate(unparenExpr(call.Args[0])) {
		return false
	}
	env, ok := unparenExpr(call.Args[1]).(*ast.SelectorExpr)
	return ok && isIdent(env.X, "spec") && env.Sel.Name == "Env"
}

// isWholeRemotePredicate accepts the remote predicate only in a form that cannot
// have been narrowed, widened or switched off:
//
//	rbc != nil   — beadRunOne's pre-collapse spelling
//	in.Remote    — runAgentLaunch's, arriving on the input struct
//
// Anything composite is refused. That is the whole point: `rbc != nil && false`,
// `in.Remote && isClaude` and `in.Remote || local` are all BinaryExprs, so
// loosening the accepted spelling to include a field selector did NOT loosen the
// rule — a bare selector is as unmodifiable as `rbc != nil` was. A bare boolean
// literal is refused for the same reason.
//
// The BASE of the selector is pinned to `in`, exactly as the environment
// argument is pinned to `spec`. Accepting any `<ident>.Remote` would let a decoy
// struct with an always-false Remote field disarm the gate while reading as
// correct — the same substitution the "wrong environment" case already refuses
// on the other argument.
func isWholeRemotePredicate(expr ast.Expr) bool {
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		return isIdent(sel.X, "in") && sel.Sel.Name == "Remote"
	}
	remote, ok := expr.(*ast.BinaryExpr)
	return ok && remote.Op == token.NEQ && isIdent(remote.X, "rbc") && isNil(remote.Y)
}

// isRefusalReportWithReason matches the call that reports the refusal, in either
// spelling: beadRunOne's failRun(reason, reason) — it owned the run's terminal
// spine — or runAgentLaunch's refuseLaunch(reason), which records the refusal on
// the result for the caller to act on. Every argument must be the reason bound
// from the refusal, so a report that invents its own text is refused.
func isRefusalReportWithReason(call *ast.CallExpr, reason *ast.Ident) bool {
	var want int
	switch {
	case isIdent(call.Fun, "failRun"):
		want = 2
	case isIdent(call.Fun, "refuseLaunch"):
		want = 1
	default:
		return false
	}
	if len(call.Args) != want || reason.Obj == nil {
		return false
	}
	for _, arg := range call.Args {
		ident, ok := unparenExpr(arg).(*ast.Ident)
		if !ok || ident.Obj != reason.Obj {
			return false
		}
	}
	return true
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
