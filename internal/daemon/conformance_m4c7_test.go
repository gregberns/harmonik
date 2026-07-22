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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
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
		rc := claudeRunCtx{
			runID:           z8ekRunID(t),
			beadID:          "hk-m4c7-codex-local",
			workspacePath:   wt,
			phase:           "implementer-initial",
			iterationCount:  1,
			beadTitle:       "codex local NFR7",
			beadDescription: "local body",
			model:           "o4-mini",
			runner:          nil, // LOCAL run — no worker
		}
		// hk-b7rt7: temp CODEX_HOME — the routed path runs the billing guard, which
		// MkdirAlls + WriteFiles <CODEX_HOME>/config.toml.
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, NewCodexHarness("", t.TempDir()), core.AgentTypeCodex)
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
		rc := claudeRunCtx{
			runID:           z8ekRunID(t),
			beadID:          "hk-m4c7-pi-local",
			workspacePath:   wt,
			phase:           "implementer-initial",
			iterationCount:  1,
			beadTitle:       "pi local NFR7",
			beadDescription: "local body",
			handlerBinary:   "pi",
			provider:        "openrouter",
			model:           "openrouter/qwen/qwen3-coder",
			apiKeyEnv:       "OPENROUTER_API_KEY",
			baseURL:         "http://dgx.local:8080/v1",
			api:             "openai",
			runner:          nil, // LOCAL run — no worker
		}
		h := NewPiHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
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
		rc := claudeRunCtx{
			runID:           z8ekRunID(t),
			beadID:          "hk-m4c7-codex-remote",
			workspacePath:   wt,
			phase:           "implementer-initial",
			iterationCount:  1,
			beadTitle:       "codex remote D2",
			beadDescription: "remote body",
			model:           "o4-mini",
			baseEnv:         []string{"PATH=/usr/bin"},
			runner:          newNoOpRecorderZ8ek(), // REMOTE run (worker selected)
		}
		// hk-b7rt7: temp CODEX_HOME — the routed path runs the billing guard, which
		// MkdirAlls + WriteFiles <CODEX_HOME>/config.toml.
		spec, _, err := buildCodexRoutedLaunchSpec(ctx, rc, NewCodexHarness("", t.TempDir()), core.AgentTypeCodex)
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
		rc := claudeRunCtx{
			runID:           z8ekRunID(t),
			beadID:          "hk-m4c7-pi-remote",
			workspacePath:   wt,
			phase:           "implementer-initial",
			iterationCount:  1,
			beadTitle:       "pi remote D2",
			beadDescription: "remote body",
			handlerBinary:   "pi",
			provider:        "openrouter",
			model:           "openrouter/qwen/qwen3-coder",
			apiKeyEnv:       "OPENROUTER_API_KEY",
			baseURL:         "http://dgx.local:8080/v1",
			api:             "openai",
			baseEnv:         []string{"PATH=/usr/bin"},
			runner:          newNoOpRecorderZ8ek(), // REMOTE run (worker selected)
		}
		h := NewPiHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
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

// TestM4C7_D2Chokepoint_IsHarnessAgnostic is a static guard proving the D2
// fail-closed check in workloop.go is applied to whatever the specBuilder produced,
// guarded ONLY by the remote predicate (rbc != nil) — never conditioned on the
// agent type. A regression that made the guard Claude-only (e.g. `if rbc != nil &&
// isClaude && ...`) would slip a Codex/Pi remote key past it; this test trips first.
func TestM4C7_D2Chokepoint_IsHarnessAgnostic(t *testing.T) {
	t.Parallel()
	src := readRepoFile(t, "internal", "daemon", "workloop.go")
	idx := strings.Index(src, "hasAPIKeyInEnv(spec.Env)")
	if idx < 0 {
		t.Fatal("D2 chokepoint hasAPIKeyInEnv(spec.Env) not found in workloop.go — the fail-closed guard was removed or renamed")
	}
	// Grab the ~200 chars preceding the call (the guard condition).
	start := idx - 200
	if start < 0 {
		start = 0
	}
	guard := src[start:idx]
	if !strings.Contains(guard, "rbc != nil") {
		t.Errorf("D2 guard is not gated on the remote predicate rbc != nil; context:\n%s", guard)
	}
	// The guard must NOT be narrowed to a single agent type.
	for _, narrow := range []string{"isClaude", "AgentTypeClaude", "== core.AgentTypeClaude"} {
		if strings.Contains(guard, narrow) {
			t.Errorf("D2 guard appears narrowed to Claude (%q) — it must gate ALL harnesses; context:\n%s", narrow, guard)
		}
	}
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

	// (b) The reverse-tunnel seam must exist.
	rtSrc := readRepoFile(t, "internal", "daemon", "reversetunnel.go")
	for _, sym := range []string{"reverseTunnelRunner", "buildReverseTunnelArgs"} {
		if !strings.Contains(rtSrc, sym) {
			t.Errorf("seam deleted: reverse-tunnel symbol %q missing from reversetunnel.go", sym)
		}
	}

	// (c) The runner-threading through the shared harness builder must survive.
	hrSrc := readRepoFile(t, "internal", "daemon", "harnessregistry.go")
	if !strings.Contains(hrSrc, "Runner: rc.runner") {
		t.Error("seam deleted: harnessregistry.go no longer threads the per-run runner (Runner: rc.runner)")
	}

	// (d) …Via(runner) helpers — the CommandRunner-aware file I/O seam. M4 landed
	// with a large family of them; a floor guards against a wholesale collapse to
	// bare os.* (which would silently break remote runs).
	viaFloor := 12
	if n := countViaHelpers(t); n < viaFloor {
		t.Errorf("seam eroded: only %d …Via helper decls across internal/daemon+internal/workspace; want >= %d (DEC-A cleanup is DEFERRED)", n, viaFloor)
	}

	// (e) The remote/local dual-path branch (rbc != nil) must NOT be collapsed.
	// DEC-A cleanup is DEFERRED (decision 5): the dual path stays. A floor on the
	// remote-predicate branch count trips if someone deletes the local fall-through.
	rbcFloor := 8
	wlSrc := readRepoFile(t, "internal", "daemon", "workloop.go")
	if n := strings.Count(wlSrc, "rbc != nil"); n < rbcFloor {
		t.Errorf("dual-path collapsed: only %d `rbc != nil` branches in workloop.go; want >= %d (DEC-A deferred — the local/remote branch must survive)", n, rbcFloor)
	}
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
			_ = f.Close() //nolint:errcheck // scan-only read; close error unactionable
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
