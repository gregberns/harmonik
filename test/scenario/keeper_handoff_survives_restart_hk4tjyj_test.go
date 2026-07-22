//go:build scenario

// keeper_handoff_survives_restart_hk4tjyj_test.go — scenario-tier reproduction of
// the field failure in which the session keeper destroyed a crew's handoff one
// second before the rebooting session read it. Bead: hk-4tjyj.
//
// Required by docs/foundation/project-level/build-practices.md §"Bug fixes
// require a reproducing scenario test": hk-4tjyj is both labeled `bug` and filed
// from an observed dogfooding runtime failure (crew `chani`,
// .harmonik/events/events.jsonl — cycle 000003 completed at 20:11:41 with no new
// session id; cycle 000004 opened 0.97s later and truncated HANDOFF-chani.md to
// 0 bytes at 20:11:42; the rebooting session then read a zero-byte file and
// printed "(no handoff on record)").
//
// WHAT IS REAL HERE (this is the point — the unit tier already covers the pure
// logic with injected seams, and build-practices.md:120 exists precisely because
// unit tests plus reviewer agents passed several 2026-05-21 dogfood bugs that
// still failed live):
//
//   - The crew is the REAL harmonik-twin-session binary, a separate OS process,
//     parsing the REAL injected /session-handoff directive off its stdin and
//     writing a REAL handoff file.
//   - The gauge is the REAL pipeline: the twin pipes statusLine JSON through the
//     REAL scripts/keeper-statusline.sh, which writes .harmonik/keeper/<agent>.ctx,
//     and execs the REAL scripts/keeper-stop-hook.sh to touch the .idle marker.
//   - The keeper is the REAL keeper.Cycler with PRODUCTION effectors: production
//     ReadCtxFile, IsManaged, CrispIdle, HoldingDispatch, handoff path/read/
//     mod-time, and — the code under test — the production stale-nonce scrub.
//   - The reboot is the REAL `harmonik agent brief --wake keeper-restart`
//     binary, executed as a subprocess exactly as the keeper injects it, and the
//     assertion is on ITS stdout. Inspecting the file alone would be weaker: the
//     bug was only visible because the brief rendered nothing.
//
// SUBSTITUTED, and orthogonal to the defect: the pane transport (a pipe to the
// twin's stdin instead of tmux paste-buffer + send-keys) and the operator-attached
// / tmux-setenv probes, which shell out to tmux. The tmux transport itself is
// covered by internal/keeper/cycle_twin_e2e_integration_test.go (//go:build
// integration). Nothing on the causal path of this bug is faked.

package scenario

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestScenario_KeeperRestart_CrewHandoffSurvivesTheNextCycle_hk4tjyj is the
// end-to-end reproduction.
//
// A crew writes a handoff with real prose plus the current cycle's marker; the
// keeper confirms it and drives /clear + the reboot command; a SECOND cycle then
// opens on the rebooted session, which is when the keeper clears the now-stale
// marker. The handoff the crew wrote must still be on disk, and must still be
// what `harmonik agent brief` hands the rebooting session.
//
// Against the pre-fix effector (os.WriteFile(path, []byte{}, 0600)) the file is
// zero bytes by the time the brief runs and the brief prints
// "(no handoff on record)" — every content assertion below fails.
func TestScenario_KeeperRestart_CrewHandoffSurvivesTheNextCycle_hk4tjyj(t *testing.T) {
	t.Parallel()

	const agent = "chani"

	repoRoot := scenarioHk4tjyjModuleRoot(t)
	twinBin := scenarioHk4tjyjBuild(t, "github.com/gregberns/harmonik/cmd/harmonik-twin-session")
	harmonikBin := scenarioHk4tjyjBuild(t, "github.com/gregberns/harmonik/cmd/harmonik")

	project := t.TempDir()
	scenarioHk4tjyjSeedProject(t, project, agent)

	// The crew's handoff, authored BEFORE the first cycle. Every byte of this
	// must survive to the reboot.
	const crewHandoffBody = "# HANDOFF-chani\n\n" +
		"## Where I am\n\n" +
		"Mid-review on hk-abc; the daemon merged hk-def at 19:04.\n\n" +
		"## Decisions\n\n" +
		"LOCKED: the review gate stays on for hk-xyz. Do not reopen it.\n\n" +
		"## Next\n\n" +
		"1. Re-run the l0 keeper gate.\n" +
		"2. Mail the captain the drain report.\n"
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(handoffPath, []byte(crewHandoffBody), 0o600); err != nil {
		t.Fatalf("seed crew handoff: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// ── The crew: the real twin binary in its own process. Token growth crosses
	// the act threshold, and /clear resets it to --start-tokens so the keeper's
	// anti-loop re-arm (a below-warn reading on the new session id) is satisfied
	// naturally rather than by poking state.
	twin := exec.CommandContext(ctx, twinBin, //nolint:gosec // G204: test-built binary path
		"--project", project,
		"--agent", agent,
		"--statusline", filepath.Join(repoRoot, "scripts", "keeper-statusline.sh"),
		"--idle-hook", filepath.Join(repoRoot, "scripts", "keeper-stop-hook.sh"),
		"--emit-interval", "100ms",
		"--start-tokens", "20000",
		"--growth", "60000",
		"--window", "1000000",
	)
	twinStdin, err := twin.StdinPipe()
	if err != nil {
		t.Fatalf("twin stdin pipe: %v", err)
	}
	twin.Stdout = io.Discard
	twin.Stderr = io.Discard
	if err := twin.Start(); err != nil {
		t.Fatalf("start twin: %v", err)
	}
	t.Cleanup(func() {
		_ = twinStdin.Close()
		_ = twin.Process.Kill()
		_ = twin.Wait()
	})

	// The pane substitute: one injected command per line, which is exactly what
	// the twin's stdin REPL consumes.
	var injectMu sync.Mutex
	inject := func(_ context.Context, _ /*target*/, text string) error {
		injectMu.Lock()
		defer injectMu.Unlock()
		_, writeErr := io.WriteString(twinStdin, text+"\n")
		return writeErr
	}

	// Wait for the real statusLine pipeline to produce a gauge above the act
	// threshold before arming the keeper.
	scenarioHk4tjyjWaitForCtxAbove(t, ctx, project, agent, 215_000)

	em := &keeper.RecordingEmitter{}
	cycler := keeper.NewCycler(keeper.CyclerConfig{
		AgentName:  agent,
		ProjectDir: project,
		TmuxTarget: "scenario-pipe", // non-empty → the injection branches run
		// Generous, load-tolerant budgets: this suite runs on a saturated box.
		HandoffTimeout:       60 * time.Second,
		ClearSettle:          10 * time.Second,
		ClearConfirmBackstop: 45 * time.Second,
		PollInterval:         100 * time.Millisecond,
		ModelDoneTimeout:     30 * time.Second,
		InjectFn:             inject,
		// tmux-only probes, stubbed because there is no pane. Neither sits on the
		// handoff path.
		SetTmuxEnvFn:       func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn: func(_ string) bool { return false },
		// EVERYTHING else is left nil so applyDefaults binds the PRODUCTION
		// implementation — including TruncateHandoffFn, the code under test.
	}, em)

	// ── Drive the keeper the way the watcher does: read the real gauge, tick.
	tickCtx, stopTicking := context.WithCancel(ctx)
	var tickWG sync.WaitGroup
	// One defer, explicit order: cancel FIRST, then wait. Two separate defers
	// would run LIFO and wait on a ticker that was never told to stop.
	defer func() {
		stopTicking()
		tickWG.Wait()
	}()
	tickWG.Add(1)
	go func() {
		defer tickWG.Done()
		for tickCtx.Err() == nil {
			cf, _, readErr := keeper.ReadCtxFile(project, agent)
			if readErr == nil && cf != nil {
				_ = cycler.MaybeRun(tickCtx, cf)
			}
			select {
			case <-tickCtx.Done():
			case <-time.After(150 * time.Millisecond):
			}
		}
	}()

	// ── Cycle 1 must complete: the crew answered, /clear ran, the reboot command
	// was injected. This is the state the field failure started from.
	scenarioHk4tjyjWaitForEventCount(t, ctx, em, core.EventTypeSessionKeeperCycleComplete, 1,
		"cycle 1 never completed")

	afterCycle1, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read handoff after cycle 1: %v", err)
	}
	if !strings.Contains(string(afterCycle1), crewHandoffBody) {
		t.Fatalf("cycle 1 did not leave the crew's handoff intact; got:\n%q", afterCycle1)
	}
	if !strings.Contains(string(afterCycle1), "<!-- KEEPER:") {
		t.Fatalf("cycle 1 did not leave a keeper marker — the next cycle will not exercise "+
			"the stale-nonce scrub, so this scenario would prove nothing. got:\n%q", afterCycle1)
	}

	// ── Cycle 2 opens on the rebooted session. THIS is the moment the keeper
	// clears the now-stale marker, and the moment that destroyed the handoff in
	// the field — 0.97s after cycle 1 completed, seconds before the rebooting
	// session ran its brief.
	scenarioHk4tjyjWaitForEventCount(t, ctx, em, core.EventTypeSessionKeeperHandoffStarted, 2,
		"cycle 2 never opened (the stale-nonce scrub never ran)")

	// ── The reboot: the REAL command the keeper injects, as a real process.
	brief := scenarioHk4tjyjRunBrief(t, ctx, harmonikBin, project, agent)

	if strings.Contains(brief, "no handoff on record") {
		t.Errorf("the rebooting session was told there is NO HANDOFF ON RECORD — "+
			"the keeper destroyed the crew's handoff between writing and reading it (hk-4tjyj).\n"+
			"brief output:\n%s", brief)
	}
	for _, want := range []string{
		"LOCKED: the review gate stays on for hk-xyz",
		"Mid-review on hk-abc",
		"Re-run the l0 keeper gate",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the reboot brief does not carry the crew's handoff line %q (hk-4tjyj).\n"+
				"brief output:\n%s", want, brief)
		}
	}

	// The file itself must still hold the body, and must not have been left
	// empty — "" and "absent" are what made this bug so hard to diagnose.
	onDisk, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read handoff after cycle 2 opened: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatalf("HANDOFF-%s.md is ZERO BYTES after the next cycle opened — "+
			"the exact field failure (hk-4tjyj)", agent)
	}
	if !strings.Contains(string(onDisk), crewHandoffBody) {
		t.Errorf("the crew's handoff body was not preserved on disk.\nwant to contain:\n%q\ngot:\n%q",
			crewHandoffBody, onDisk)
	}
}

// ── helpers (prefix scenarioHk4tjyj — this file owns them) ───────────────────

// scenarioHk4tjyjModuleRoot resolves the repository root from `go env GOMOD`.
func scenarioHk4tjyjModuleRoot(t *testing.T) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain not on PATH: %v", err)
	}
	out, err := exec.Command(goTool, "env", "GOMOD").Output() //nolint:gosec // G204: goTool from LookPath
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		t.Fatalf("no go.mod found (GOMOD=%q)", gomod)
	}
	return filepath.Dir(gomod)
}

// scenarioHk4tjyjBuild compiles pkg into the test's temp dir and returns the path.
func scenarioHk4tjyjBuild(t *testing.T, pkg string) string {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain not on PATH: %v", err)
	}
	binPath := filepath.Join(t.TempDir(), filepath.Base(pkg))
	root := scenarioHk4tjyjModuleRoot(t)

	// Retried once. `go build` here reads the shared GOCACHE, and a concurrent
	// `go clean -cache` in another worktree makes it fail with
	// "could not import <stdlib pkg> (open .../go-build/...: no such file or
	// directory)" — an environment fault with nothing to do with the code under
	// test. One retry repopulates the cache. A second consecutive failure is
	// real and fails the test with the compiler output.
	var lastOut []byte
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		cmd := exec.Command(goTool, "build", "-o", binPath, pkg) //nolint:gosec // G204: goTool from LookPath, pkg is a compile-time constant
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, buildErr := cmd.CombinedOutput()
		if buildErr == nil {
			return binPath
		}
		lastOut, lastErr = out, buildErr
		t.Logf("build %s attempt %d failed (retrying): %v\n%s", pkg, attempt, buildErr, out)
	}
	t.Fatalf("build %s failed twice: %v\n%s", pkg, lastErr, lastOut)
	return ""
}

// scenarioHk4tjyjSeedProject lays down the minimum on-disk state the keeper and
// `agent brief` need: the .managed opt-in marker and an agent type folder.
func scenarioHk4tjyjSeedProject(t *testing.T, project, agent string) {
	t.Helper()

	keeperDir := filepath.Join(project, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("mkdir keeper dir: %v", err)
	}
	// .managed is the keeper's opt-in guard (production IsManaged). Empty body =
	// managed with no session binding yet.
	if err := os.WriteFile(filepath.Join(keeperDir, agent+".managed"), nil, 0o600); err != nil {
		t.Fatalf("write .managed: %v", err)
	}

	// `agent brief` resolves <project>/.harmonik/agents/<type>/. The agent name
	// doubles as the type so no crew registry is needed.
	typeDir := filepath.Join(project, ".harmonik", "agents", agent)
	if err := os.MkdirAll(typeDir, 0o755); err != nil {
		t.Fatalf("mkdir agents dir: %v", err)
	}
	manifest := "type: " + agent + "\n" +
		"cardinality: { min: 0, max: n }\n" +
		"harness: claude\n" +
		"identity:\n" +
		"  soul: soul.md\n" +
		"  parent_intent: operator\n" +
		"context: []\n" +
		"triggers: []\n" +
		"handoff:\n" +
		"  channel: private\n" +
		"keeper:\n" +
		"  thresholds: default\n" +
		"lifecycle:\n" +
		"  self_restart: true\n" +
		"markers:\n" +
		"  never_emits: []\n"
	for name, body := range map[string]string{
		"manifest.yaml": manifest,
		"soul.md":       "I am " + agent + " — a crew orchestrator.\n",
		"operating.md":  "## Loop\n1. Pick a bead.\n2. Dispatch it.\n",
	} {
		if err := os.WriteFile(filepath.Join(typeDir, name), []byte(body), 0o644); err != nil { //nolint:gosec // G306: test fixture
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// scenarioHk4tjyjWaitForCtxAbove blocks until the REAL statusLine pipeline has
// written a gauge at or above minTokens.
func scenarioHk4tjyjWaitForCtxAbove(t *testing.T, ctx context.Context, project, agent string, minTokens int64) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		cf, _, err := keeper.ReadCtxFile(project, agent)
		if err == nil && cf != nil && cf.Tokens >= minTokens {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("gauge never reached %d tokens — the real statusLine pipeline "+
				"(scripts/keeper-statusline.sh) did not produce a usable .ctx", minTokens)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// scenarioHk4tjyjWaitForEventCount blocks until at least n events of the given
// type have been emitted.
func scenarioHk4tjyjWaitForEventCount(
	t *testing.T, ctx context.Context, em *keeper.RecordingEmitter,
	evType core.EventType, n int, failMsg string,
) {
	t.Helper()
	deadline := time.Now().Add(150 * time.Second)
	for {
		if len(em.EventsOfType(evType)) >= n {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("%s (waited for %d× %s, saw %d)", failMsg, n, evType, len(em.EventsOfType(evType)))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// scenarioHk4tjyjRunBrief executes the REAL reboot command the keeper injects and
// returns its stdout.
func scenarioHk4tjyjRunBrief(t *testing.T, ctx context.Context, harmonikBin, project, agent string) string {
	t.Helper()
	cmd := exec.CommandContext(ctx, harmonikBin, //nolint:gosec // G204: test-built binary path
		"agent", "brief", "--wake", "keeper-restart", "--agent", agent, "--project", project)
	cmd.Env = append(os.Environ(), "HARMONIK_AGENT="+agent)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harmonik agent brief failed: %v\noutput:\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("harmonik agent brief produced no output")
	}
	return string(out)
}
