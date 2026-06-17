//go:build integration

package keeper_test

// cycle_twin_e2e_integration_test.go — bead hk-sav, Part B.
//
// The TRUE send-keys / bracketed-paste end-to-end test for the session keeper's
// clear→restart ("context clean") cycle. Every existing cycle test FAKES the
// loop:
//
//   - cycle_test.go uses a spy InjectFn that merely records "/clear" as a string
//     and flips the gauge SID on a fixed call count — nothing reacts.
//   - cycle_scenario_reactive_*_test.go (Part A) closes the *causal* loop with an
//     in-process reactiveSession fake, but injection is still a plain Go function
//     call — no tmux, no real script, no real subprocess.
//
// This file closes the LAST gap: it runs the faithful session twin
// (cmd/harmonik-twin-session, also hk-sav Part A) in a REAL tmux pane, emits
// statusLine JSON through the REAL scripts/keeper-statusline.sh →
// <project>/.harmonik/keeper/<agent>.ctx pipeline, touches the REAL
// <agent>.idle marker via scripts/keeper-stop-hook.sh, and drives the REAL
// keeper.Cycler with the REAL keeper.InjectText (tmux load-buffer →
// paste-buffer → send-keys Enter). The only fakes left are the wall-clock and
// the LLM itself; the file/stdin/tmux contracts the keeper depends on are all
// real.
//
// What is REAL here (vs. Part A's in-process fakes):
//   - keeper.Cycler.MaybeRun — the production gate + 7-step cycle.
//   - keeper.InjectText — real tmux paste-buffer + send-keys into a real pane.
//   - keeper.ReadCtxFile — reads the .ctx the real bash script wrote.
//   - keeper.CrispIdle / keeper.HoldingDispatch / keeper.IsManaged — real
//     marker-file gates against the twin's real .idle / .dispatching / .managed.
//   - scripts/keeper-statusline.sh + scripts/keeper-stop-hook.sh — the real
//     pipeline, invoked by the twin on every emit.
//
// No injection adaptation: this test uses the production keeper.InjectText
// verbatim as its InjectFn. The production cycle.go emits the /session-handoff
// directive as a MULTI-LINE string (nonce on a later line), and keeper.InjectText
// delivers it via tmux paste-buffer (bracketed paste). The twin parses that
// real multi-line shape natively (hk-fan: it arms on the "/session-handoff"
// trigger and scans the following lines of the same paste for the
// <!-- KEEPER:<nonce> --> marker, modeling a real Claude REPL ingesting the
// whole bracketed paste as one prompt — see internal/daemon/pasteinject.go:112-114).
// The earlier twFlattenInjectFn workaround (which collapsed the directive's
// embedded newlines to spaces before injection) is therefore GONE — every
// command travels through the REAL, unmodified tmux send-keys path.
//
// # Safety contract (load-bearing — a live daemon/keeper/crew fleet runs here)
//
// This test creates and destroys ONLY its own uniquely-named throwaway tmux
// sessions. Session names use the prefix "hksav-twin-" (which no harmonik
// machinery ever produces) plus two rand/v2 suffixes, and every teardown kills
// THAT session BY EXACT NAME via `tmux kill-session -t <name>`. There is NO
// kill-server, NO glob/pattern kill, NO list-and-kill. It can never touch
// harmonik-daemon, hk-daemon-supervise, harmonik-<hash>-default, *-flywheel,
// crew panes, harmonik-pi/main/kerf, or any other pre-existing session. If tmux
// is not on PATH the whole test t.Skip()s.
//
// Teardown discipline (the hk-dju "directory not empty" class): the twin's
// emitter goroutine writes into the temp project dir on every tick. Cleanup
// kills the session AND BLOCKS until the pane is gone (twKillAndWait) BEFORE the
// test body returns, so nothing writes into t.TempDir() during its removal.
//
// Helper prefix: tw (twin). Bead: hk-sav.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// twRequireTmux skips the calling test when tmux is not installed. The real
// send-keys E2E is meaningless without it.
func twRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tw: tmux not found on PATH; skipping real-tmux twin E2E test")
	}
}

// twRepoRoot returns the repository root, two directories up from the
// internal/keeper test working directory. Used to locate cmd/harmonik-twin-session
// and the scripts/ directory.
func twRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("tw: getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// twBuildTwin compiles cmd/harmonik-twin-session into dir and returns the binary
// path. Building once per test keeps the test hermetic against an out-of-date
// installed binary.
func twBuildTwin(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "harmonik-twin-session")
	root := twRepoRoot(t)
	out, err := exec.Command("go", "build", "-o", bin, filepath.Join(root, "cmd", "harmonik-twin-session")).CombinedOutput() //nolint:gosec // G204: test-local build of a repo binary
	if err != nil {
		t.Fatalf("tw: build twin: %v\n%s", err, out)
	}
	return bin
}

// twScripts returns the absolute paths to the real keeper statusLine and stop
// (idle) hooks under scripts/. It fails the test if either is missing.
func twScripts(t *testing.T) (statusline, idleHook string) {
	t.Helper()
	root := twRepoRoot(t)
	statusline = filepath.Join(root, "scripts", "keeper-statusline.sh")
	idleHook = filepath.Join(root, "scripts", "keeper-stop-hook.sh")
	for _, p := range []string{statusline, idleHook} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("tw: required script missing: %s: %v", p, err)
		}
	}
	return statusline, idleHook
}

// twUniqueSessionName returns a throwaway tmux session name guaranteed not to
// collide with any real harmonik/captain/crew session. The "hksav-twin-" prefix
// is never produced by harmonik machinery; two rand/v2 suffixes make it unique.
func twUniqueSessionName() string {
	return fmt.Sprintf("hksav-twin-%d-%d", rand.Int64(), rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance
}

// twPaneAlive reports whether the named tmux session still exists, using the
// exact-match "=" anchor so a prefix collision can never report a false live.
func twPaneAlive(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", "="+name).Run() //nolint:gosec // G204: name is a test-local generated session name
	return err == nil
}

// twKillAndWait kills the named session BY EXACT NAME and BLOCKS until tmux no
// longer reports it live (or a short timeout elapses). This is the load-bearing
// teardown discipline: the twin's emitter goroutine writes into the temp project
// dir every tick, so the pane MUST be fully gone before t.TempDir() is removed,
// or cleanup races the writer ("directory not empty", the hk-dju class).
func twKillAndWait(t *testing.T, name string) {
	t.Helper()
	// kill-session by EXACT name only. An already-dead session is a no-op.
	_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run() //nolint:gosec,errcheck // G204: test-local name; best-effort
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !twPaneAlive(name) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("tw: WARNING session %q still alive after kill+5s wait", name)
}

// twTwinSpec configures a twin pane.
type twTwinSpec struct {
	project     string
	agent       string
	twin        string
	statusline  string
	idleHook    string
	model       string // default "claude-opus-4-8 [1m]" if empty
	window      int64  // 0 omits context_window_size (the [1m] quirk)
	growth      int64
	startTokens int64
	emitEvery   time.Duration
	// extraEnv is appended to the pane's environment (e.g.
	// HARMONIK_KEEPER_WINDOW_SIZE=...) so the real statusline script's env
	// fallbacks can be exercised.
	extraEnv []string
	// emitNA passes the twin's --emit-na flag: every statusLine carries a
	// non-numeric used_percentage ("NA") so the real script SKIPS the .ctx write
	// (models the post-/clear NA statusLine). Downstream gauge-liveness beads
	// reuse this single definition.
	emitNA bool
	// suppressAfter passes the twin's --suppress-statusline-after flag: statusLine
	// emits stop after this much elapsed time so the gauge .ctx goes stale while
	// the session stays alive (idle hook + growth keep running). 0 = never.
	// Downstream gauge-liveness / force-restart beads reuse this single definition.
	suppressAfter time.Duration
	// resumeStatuslineOnClear passes the twin's --resume-statusline-on-clear flag:
	// a /clear lifts an active suppressAfter so the post-clear session resumes
	// emitting (the gauge re-appears with the rotated session_id). This is what
	// lets the operator real-env cycle present a STALE gauge before /clear yet a
	// FRESH, rebound gauge after — the headline hk-nlio scenario. Single
	// definition; downstream force-restart/live-recovery beads reuse it.
	resumeStatuslineOnClear bool
}

// twStartTwin launches the twin binary as the foreground process of a new,
// uniquely-named, detached tmux session and registers a blocking teardown. The
// twin reads injected commands from its stdin (the pane), so the real keeper
// InjectText (paste-buffer + send-keys) reaches it. Returns the session name,
// which doubles as the keeper's TmuxTarget.
func twStartTwin(t *testing.T, spec twTwinSpec) string {
	t.Helper()
	if spec.model == "" {
		spec.model = "claude-opus-4-8 [1m]"
	}
	sess := twUniqueSessionName()

	// Build the twin command line. All paths are test-local temp/repo paths.
	cmd := fmt.Sprintf(
		"exec %s --project %s --agent %s --statusline %s --idle-hook %s --model %q --window %d --growth %d --start-tokens %d --emit-interval %s",
		spec.twin, spec.project, spec.agent, spec.statusline, spec.idleHook,
		spec.model, spec.window, spec.growth, spec.startTokens, spec.emitEvery,
	)
	// Optional gauge-liveness knobs (off by default; the happy-path E2E above
	// passes neither). Single definition — downstream beads set the spec fields.
	if spec.emitNA {
		cmd += " --emit-na"
	}
	if spec.suppressAfter > 0 {
		cmd += fmt.Sprintf(" --suppress-statusline-after %s", spec.suppressAfter)
	}
	if spec.resumeStatuslineOnClear {
		cmd += " --resume-statusline-on-clear"
	}

	args := []string{"new-session", "-d", "-s", sess}
	// Inject extra env via tmux's -e flag (tmux 3.2+) so the statusline script
	// sees it. Each entry is KEY=VALUE.
	for _, e := range spec.extraEnv {
		args = append(args, "-e", e)
	}
	args = append(args, "sh", "-c", cmd)

	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: test-local generated args
		t.Fatalf("tw: tmux new-session %q: %v\n%s", sess, err, out)
	}
	// Blocking teardown — kill by EXACT name and wait for the pane to die BEFORE
	// the test returns and t.TempDir() is removed.
	t.Cleanup(func() { twKillAndWait(t, sess) })
	return sess
}

// twWaitForCtxTokens polls the REAL .ctx (written by the real bash script) until
// the absolute token count reaches atLeast, or fails after timeout. Returns the
// observed CtxFile. This is how the test waits for the twin's emitter to grow
// context over the keeper's act threshold without faking the gauge.
func twWaitForCtxTokens(t *testing.T, project, agent string, atLeast int64, timeout time.Duration) *keeper.CtxFile {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *keeper.CtxFile
	for time.Now().Before(deadline) {
		cf, _, err := keeper.ReadCtxFile(project, agent)
		if err == nil {
			last = cf
			if cf.Tokens >= atLeast {
				return cf
			}
		}
		time.Sleep(75 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("tw: .ctx never reached %d tokens within %s (last: tokens=%d window=%d sid=%q)",
			atLeast, timeout, last.Tokens, last.WindowSize, last.SessionID)
	}
	t.Fatalf("tw: .ctx file never appeared for agent %q within %s", agent, timeout)
	return nil
}

// twWatchForReset polls the .ctx until stop closes, recording the MINIMUM token
// count observed on a session_id that differs from prevSID (i.e. the rotated,
// post-/clear session). It sends that minimum on resetCh exactly once when stop
// closes (or -1 if no rotated-session reading was ever seen). This witnesses the
// /clear token RESET directly, immune to the twin emitter's subsequent regrowth.
func twWatchForReset(project, agent, prevSID string, stop <-chan struct{}, resetCh chan<- int64) {
	minTokens := int64(-1)
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			resetCh <- minTokens
			return
		case <-ticker.C:
			cf, _, err := keeper.ReadCtxFile(project, agent)
			if err != nil || cf.SessionID == "" || cf.SessionID == prevSID {
				continue
			}
			if minTokens < 0 || cf.Tokens < minTokens {
				minTokens = cf.Tokens
			}
		}
	}
}

// TestIntegration_TwinClearRestartCycle_E2E drives a FULL clear→restart cycle
// end-to-end against the faithful session twin in a real tmux pane, through the
// real statusLine pipeline and the real keeper.Cycler with real tmux injection.
//
// Flow:
//  1. Build the twin; start it in a uniquely-named tmux pane emitting [1m]
//     statusLine JSON (window=1M so the gauge has an absolute-token view) and
//     growing tokens past the keeper act threshold.
//  2. Mark the agent .managed and wait (via the REAL .ctx) for tokens to cross
//     the act threshold. Confirm CrispIdle is true (the twin touched .idle).
//  3. Run keeper.Cycler.MaybeRun with REAL gates + REAL InjectText.
//
// Asserts the "context clean" cycle happened for real:
//   - the nonce <!-- KEEPER:<cycleID> --> landed in the twin's HANDOFF file on
//     /session-handoff (the safety precondition for /clear).
//   - a NEW, valid UUIDv4 session_id was minted in the .ctx on /clear (prev→new),
//     and tokens dropped from the pre-clear high-water mark.
//   - .idle exists (await-input boundary touched by the real stop hook).
//   - session_keeper_cycle_complete emitted (prev==seed SID, new==rotated SID);
//     NO session_keeper_cycle_aborted.
func TestIntegration_TwinClearRestartCycle_E2E(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twe2e%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	// Opt the agent in (.managed) so the REAL IsManaged gate passes.
	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw: WriteManagedSessionID: %v", err)
	}

	// window=1M so the gauge sees an absolute-token window; growth pushes tokens
	// over the act threshold (default 300k, capped by 0.85*1M) within a couple
	// of seconds at 50k/200ms.
	sess := twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      1_000_000,
		growth:      50_000,
		startTokens: 50_000,
		emitEvery:   200 * time.Millisecond,
	})

	// Wait (via the REAL .ctx) for tokens to cross the act threshold.
	seed := twWaitForCtxTokens(t, project, agent, 300_000, 8*time.Second)
	seedSID := seed.SessionID
	if seedSID == "" {
		t.Fatal("tw: seed .ctx has empty session_id")
	}
	if seed.WindowSize != 1_000_000 {
		t.Fatalf("tw: seed .ctx window_size = %d; want 1000000", seed.WindowSize)
	}
	// The twin fired the real stop hook on each emit → .idle exists → CrispIdle.
	if !keeper.CrispIdle(project, agent) {
		t.Fatal("tw: CrispIdle false at seed — the twin's .idle marker did not register a crisp boundary")
	}
	if keeper.HoldingDispatch(project, agent) {
		t.Fatal("tw: HoldingDispatch true unexpectedly (no .dispatching marker was written)")
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		AgentName:  agent,
		ProjectDir: project,
		TmuxTarget: sess,
		// Generous real-time budgets: real tmux send-keys + script emits are slow.
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
		// REAL, UNMODIFIED keeper.InjectText — the production InjectFn default,
		// performing the real tmux load-buffer → paste-buffer → send-keys Enter
		// sequence with the verbatim MULTI-LINE /session-handoff directive (no
		// flatten). The twin now parses the multi-line directive natively
		// (hk-fan), so the E2E exercises the maximally-faithful path. Everything
		// else (ReadGaugeFn, ReadHandoff, CrispIdleFn, HoldingDispatchFn,
		// IsManagedFn, SetManagedSessionFn, HandoffFilePath, TruncateHandoffFn,
		// AppendHandoffFn, SetTmuxEnvFn, WriteJournalFn) uses the production
		// defaults.
		InjectFn: keeper.InjectText,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Watch the gauge concurrently with the cycle so we capture the post-/clear
	// token RESET directly. The twin's emitter keeps growing tokens after /clear
	// resets them to start-tokens, so a single post-cycle read would race the
	// regrowth; this watcher records the minimum tokens seen on the NEW (rotated)
	// session, which is the reset value the /clear caused. Stops when stopWatch
	// closes (after MaybeRun returns).
	stopWatch := make(chan struct{})
	resetCh := make(chan int64, 1)
	go twWatchForReset(project, agent, seedSID, stopWatch, resetCh)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		close(stopWatch)
		t.Fatalf("tw: MaybeRun: %v", err)
	}
	close(stopWatch)
	minPostClearTokens := <-resetCh

	// (a) The nonce landed in the twin's HANDOFF file (handoff confirmed — the
	// safety precondition for /clear).
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")
	hb, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("tw: read HANDOFF: %v", err)
	}
	if !strings.Contains(string(hb), "<!-- KEEPER:") {
		t.Fatalf("tw: HANDOFF file missing keeper nonce; got:\n%s", hb)
	}

	// (b) A NEW, valid UUIDv4 session_id was minted on /clear (prev→new) and
	// tokens dropped below the pre-clear high-water mark.
	final, _, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read final .ctx: %v", err)
	}
	if final.SessionID == seedSID {
		t.Fatalf("tw: session_id did not rotate on /clear (still %q) — the clear→restart did not happen", seedSID)
	}
	if !twIsValidUUIDv4(final.SessionID) {
		t.Fatalf("tw: rotated session_id %q is not a valid UUIDv4 (keeper rejects v7)", final.SessionID)
	}
	// The /clear reset tokens to start-tokens on the NEW session. The concurrent
	// watcher captured the minimum tokens observed on that rotated session — the
	// reset value — before the emitter grew it again. That minimum must be BELOW
	// the pre-clear high-water mark (proving context was dropped by /clear), and
	// at/near the twin's start-tokens reset point (50k; allow headroom for a few
	// emit ticks that may have grown it before the watcher first sampled).
	if minPostClearTokens < 0 {
		t.Errorf("tw: never observed a reading on the rotated session — cannot confirm the token reset")
	} else {
		if minPostClearTokens >= seed.Tokens {
			t.Errorf("tw: tokens did not drop after /clear: min-on-new-session=%d >= seed-high-water=%d", minPostClearTokens, seed.Tokens)
		}
		if minPostClearTokens > 200_000 {
			t.Errorf("tw: post-/clear token reset too high: min-on-new-session=%d; want near start-tokens (50k)", minPostClearTokens)
		}
	}

	// (c) .idle exists (await-input boundary touched by the real stop hook).
	idlePath := filepath.Join(project, ".harmonik", "keeper", agent+".idle")
	if _, err := os.Stat(idlePath); err != nil {
		t.Errorf("tw: .idle marker missing after cycle: %v", err)
	}

	// (d) cycle_complete emitted with prev==seed and new==rotated; NO aborted.
	complete := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(complete) != 1 {
		t.Fatalf("tw: want 1 cycle_complete; got %d (events imply the cycle did not finish cleanly)", len(complete))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Errorf("tw: want 0 cycle_aborted on the happy path; got %d", n)
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(complete[0].Payload, &cp); err != nil {
		t.Fatalf("tw: unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != seedSID {
		t.Errorf("tw: cycle_complete.prev_session_id = %q; want %q (seed SID)", cp.PrevSessionID, seedSID)
	}
	if cp.NewSessionID != final.SessionID {
		t.Errorf("tw: cycle_complete.new_session_id = %q; want %q (the rotated .ctx SID)", cp.NewSessionID, final.SessionID)
	}

	// (e) handoff_started emitted exactly once (cycle was auditable).
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("tw: want 1 handoff_started; got %d", n)
	}
}

// TestIntegration_TwinE2E_OperatorRealEnv is the KEEPER-REDESIGN HEADLINE
// VALIDATION GATE (bead hk-nlio). It drives the FULL clear→restart cycle against
// the faithful session twin in a real tmux pane, REAL statusLine pipeline, and
// REAL keeper.InjectText — but it first reconstructs the operator's actual
// failure environment, the one the redesign was built to survive:
//
//	(1) HIGH CONTEXT — tokens past the act threshold on a [1m] (1M-window) model,
//	    so the gauge reports an absolute-token view well above Act 300k.
//	(2) STALE GAUGE while the pane is ALIVE — the statusLine emit is suppressed
//	    (the .ctx FREEZES) while the idle hook keeps firing (the .idle marker
//	    advances). This is the "gauge stale on a live agent" condition that used
//	    to blind the keeper (no_gauge:stale / gauge-NA stall).
//	(3) IDLE / REMOTE-CONTROL CLIENT ATTACHED — the operator drives via the
//	    iOS / `claude --remote-control` channel, so a tmux client is attached but
//	    its #{client_activity} is frozen at attach time. The keeper MUST NOT
//	    treat that as a live typist and false-suppress the cycle (hk-0t5s).
//
// It then asserts the full cycle COMPLETES and REBINDS IDENTITY:
//
//	handoff → confirm-nonce → /clear → session-id FLIP (a fresh, valid UUIDv4,
//	NOT v7) → /session-resume, with .managed rebound to the rotated session_id.
//
// Plus the standing invariants that the redesign locks in:
//   - no-auto-clear: EXACTLY ONE /clear is injected, and only AFTER the handoff
//     nonce confirmed — the old identity-disambiguation heuristic loop is dead.
//   - SetManagedSession is called EXACTLY ONCE, with the rotated session_id.
//   - the cycle is NOT suppressed by the idle/remote-control client (0
//     operator_attached events; the cycle completes).
//
// ACCEPTANCE: RED on a pre-redesign main (identity-binding / gauge-liveness /
// operator-attached / live-pane beads absent); GREEN once they all land. This is
// the deterministic "it works" gate. The LIVE-SOAK with a real attached client
// over a >5-min idle window is the INDEPENDENT-verifier step recorded in the
// epic acceptance procedure — NOT this deterministic test.
func TestIntegration_TwinE2E_OperatorRealEnv(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twe2eop%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	// Opt the agent in (.managed) so the REAL IsManaged gate passes; start with an
	// empty binding (the watcher would latch the first gauge tick in production).
	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw: WriteManagedSessionID: %v", err)
	}

	// (1) HIGH CONTEXT on a 1M window: start low and grow past Act 300k, then
	// (2) FREEZE the gauge after suppressAfter while keeping the pane alive, and
	// resume emitting on /clear so the post-clear rotated SID becomes observable
	// (the operator's stale-gauge-then-recover path).
	const emitEvery = 150 * time.Millisecond
	sess := twStartTwin(t, twTwinSpec{
		project:                 project,
		agent:                   agent,
		twin:                    twin,
		statusline:              statusline,
		idleHook:                idleHook,
		model:                   "claude-opus-4-8 [1m]",
		window:                  1_000_000,
		growth:                  60_000,
		startTokens:             50_000, // post-/clear reset value — must be well below Act.
		emitEvery:               emitEvery,
		suppressAfter:           2500 * time.Millisecond,
		resumeStatuslineOnClear: true,
	})

	// Wait (via the REAL .ctx) for tokens to cross the act threshold while the
	// gauge is still fresh — this is the seed reading the keeper acts on.
	seed := twWaitForCtxTokens(t, project, agent, 300_000, 8*time.Second)
	seedSID := seed.SessionID
	if seedSID == "" {
		t.Fatal("tw: seed .ctx has empty session_id")
	}
	if seed.WindowSize != 1_000_000 {
		t.Fatalf("tw: seed .ctx window_size = %d; want 1000000 (the [1m] absolute-token view)", seed.WindowSize)
	}
	if !keeper.CrispIdle(project, agent) {
		t.Fatal("tw: CrispIdle false at seed — the twin's .idle marker did not register a crisp boundary")
	}

	// (2) Prove the gauge is now STALE while the pane is ALIVE: wait past the
	// suppression deadline, then confirm the .ctx modTime is FROZEN across a
	// sampling window while the .idle marker keeps ADVANCING.
	time.Sleep(2500*time.Millisecond + 4*emitEvery)
	_, ctxMod1, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read .ctx (stale sample 1): %v", err)
	}
	idle1, err := os.Stat(twIdlePath(project, agent))
	if err != nil {
		t.Fatalf("tw: stat .idle (sample 1): %v", err)
	}
	time.Sleep(6 * emitEvery)
	_, ctxMod2, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read .ctx (stale sample 2): %v", err)
	}
	idle2, err := os.Stat(twIdlePath(project, agent))
	if err != nil {
		t.Fatalf("tw: stat .idle (sample 2): %v", err)
	}
	if !ctxMod2.Equal(ctxMod1) {
		t.Fatalf("tw: gauge .ctx advanced (%s → %s); the operator-real-env precondition is a STALE gauge", ctxMod1, ctxMod2)
	}
	if !idle2.ModTime().After(idle1.ModTime()) {
		t.Fatalf("tw: .idle did not advance (%s → %s); the pane must stay ALIVE under a stale gauge", idle1.ModTime(), idle2.ModTime())
	}
	if !keeper.CrispIdle(project, agent) {
		t.Fatal("tw: CrispIdle false under the stale gauge — a live pane must still present a crisp boundary")
	}

	// (3) IDLE / REMOTE-CONTROL CLIENT: feed a STALE #{client_activity} line to the
	// PRODUCTION distinction logic with the PRODUCTION window. operatorActiveSince
	// must read NOT-active (the client is attached but idle), so the cycle proceeds
	// rather than false-suppressing (hk-0t5s). Exercising the real parser keeps the
	// gate faithful; the live attached-client soak is the human-verifier step.
	operatorAttached := func(_ string) bool {
		staleClient := fmt.Sprintf("%d\n", time.Now().Add(-10*time.Minute).Unix())
		return keeper.OperatorActiveSinceForTest(staleClient, time.Now(), keeper.OperatorActiveWindowForTest)
	}
	if operatorAttached(sess) {
		t.Fatal("tw: idle/remote-control client mis-read as ACTIVE — it would false-suppress the cycle")
	}

	// Recording wrappers around the PRODUCTION fns so the no-auto-clear and
	// SetManagedSession-called-once invariants are observable while still driving
	// the REAL tmux injection + REAL .managed write.
	var injects []string
	recInject := func(ctx context.Context, target, text string) error {
		injects = append(injects, text)
		return keeper.InjectText(ctx, target, text)
	}
	var setManagedSIDs []string
	recSetManaged := func(projectDir, agentName, sessionID string) error {
		setManagedSIDs = append(setManagedSIDs, sessionID)
		return keeper.WriteManagedSessionID(projectDir, agentName, sessionID)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.CyclerConfig{
		AgentName:           agent,
		ProjectDir:          project,
		TmuxTarget:          sess,
		HandoffTimeout:      10 * time.Second,
		ClearSettle:         6 * time.Second,
		PollInterval:        150 * time.Millisecond,
		InjectFn:            recInject,
		SetManagedSessionFn: recSetManaged,
		OperatorAttachedFn:  operatorAttached,
	}
	cycler := keeper.NewCycler(cfg, em)

	// Watch for the post-/clear token RESET on the rotated session (immune to the
	// resumed emitter's regrowth), exactly as the happy-path E2E does.
	stopWatch := make(chan struct{})
	resetCh := make(chan int64, 1)
	go twWatchForReset(project, agent, seedSID, stopWatch, resetCh)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		close(stopWatch)
		t.Fatalf("tw: MaybeRun: %v", err)
	}
	close(stopWatch)
	minPostClearTokens := <-resetCh

	// (a) Nonce landed in the HANDOFF file (handoff confirmed — the /clear precondition).
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")
	hb, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("tw: read HANDOFF: %v", err)
	}
	if !strings.Contains(string(hb), "<!-- KEEPER:") {
		t.Fatalf("tw: HANDOFF file missing keeper nonce; got:\n%s", hb)
	}

	// (b) SESSION-ID FLIP: a new, valid UUIDv4 (keeper rejects v7) replaced the seed.
	final, _, err := keeper.ReadCtxFile(project, agent)
	if err != nil {
		t.Fatalf("tw: read final .ctx: %v", err)
	}
	if final.SessionID == seedSID {
		t.Fatalf("tw: session_id did not flip on /clear (still %q) — the cycle did not happen", seedSID)
	}
	if !twIsValidUUIDv4(final.SessionID) {
		t.Fatalf("tw: rotated session_id %q is not a valid UUIDv4", final.SessionID)
	}

	// (c) Tokens dropped after /clear (context was actually shed).
	if minPostClearTokens < 0 {
		t.Errorf("tw: never observed a reading on the rotated session — cannot confirm the token reset")
	} else if minPostClearTokens >= seed.Tokens {
		t.Errorf("tw: tokens did not drop after /clear: min-on-new-session=%d >= seed=%d", minPostClearTokens, seed.Tokens)
	}

	// (d) IDENTITY REBOUND: .managed now holds the rotated session_id, and IsManaged
	// stays true (the opt-in marker is preserved across the cycle).
	if !keeper.IsManaged(project, agent) {
		t.Error("tw: agent no longer .managed after the cycle — the opt-in marker must be preserved")
	}
	boundSID, err := keeper.ReadManagedSessionID(project, agent)
	if err != nil {
		t.Fatalf("tw: ReadManagedSessionID: %v", err)
	}
	if boundSID != final.SessionID {
		t.Errorf("tw: .managed rebound to %q; want the rotated SID %q", boundSID, final.SessionID)
	}

	// (e) cycle_complete with prev==seed and new==rotated; NO aborted.
	complete := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(complete) != 1 {
		t.Fatalf("tw: want 1 cycle_complete; got %d", len(complete))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)); n != 0 {
		t.Errorf("tw: want 0 cycle_aborted; got %d", n)
	}
	var cp core.SessionKeeperCycleCompletePayload
	if err := json.Unmarshal(complete[0].Payload, &cp); err != nil {
		t.Fatalf("tw: unmarshal cycle_complete: %v", err)
	}
	if cp.PrevSessionID != seedSID {
		t.Errorf("tw: cycle_complete.prev_session_id = %q; want %q", cp.PrevSessionID, seedSID)
	}
	if cp.NewSessionID != final.SessionID {
		t.Errorf("tw: cycle_complete.new_session_id = %q; want %q", cp.NewSessionID, final.SessionID)
	}

	// (f) NO operator-attached suppression (the idle/remote-control client was
	// correctly read as not-a-live-typist).
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached)); n != 0 {
		t.Errorf("tw: want 0 operator_attached events for an IDLE/remote-control client; got %d", n)
	}

	// (g) NO-AUTO-CLEAR invariant: EXACTLY ONE /clear, injected only AFTER the
	// handoff and before the resume — the deterministic 7-step cycle, with the old
	// heuristic auto-clear loop dead.
	handoffIdx, clearIdx, resumeIdx := -1, -1, -1
	clears := 0
	for i, text := range injects {
		switch {
		case strings.Contains(text, "/session-handoff"):
			if handoffIdx < 0 {
				handoffIdx = i
			}
		case strings.Contains(text, "/session-resume"):
			resumeIdx = i
		case strings.Contains(text, "/clear"):
			clears++
			clearIdx = i
		}
	}
	if clears != 1 {
		t.Errorf("tw: want exactly 1 /clear injection (no-auto-clear); got %d (%v)", clears, injects)
	}
	if !(handoffIdx >= 0 && handoffIdx < clearIdx && clearIdx < resumeIdx) {
		t.Errorf("tw: inject order must be handoff(%d) < clear(%d) < resume(%d): %v", handoffIdx, clearIdx, resumeIdx, injects)
	}

	// (h) SetManagedSession called EXACTLY ONCE, with the rotated SID.
	if len(setManagedSIDs) != 1 {
		t.Fatalf("tw: want SetManagedSession called exactly once; got %d (%v)", len(setManagedSIDs), setManagedSIDs)
	}
	if setManagedSIDs[0] != final.SessionID {
		t.Errorf("tw: SetManagedSession called with %q; want the rotated SID %q", setManagedSIDs[0], final.SessionID)
	}

	// (i) handoff_started emitted exactly once (the cycle was auditable).
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("tw: want 1 handoff_started; got %d", n)
	}
}

// TestIntegration_TwinE2E_DefaultsPin asserts the STANDING band-PIN invariant in
// the same suite as the headline gate (hk-nlio): the resolved CyclerConfig
// defaults are the operator-decided Act 300k / 0.85, Warn 270k / 0.70, and
// Force +40k → 340k / 0.95. NO band retune — widening the band is an operator
// HARD-NO. A value drifting here fails the gate. Refs: hk-lhu2, hk-bpkv,
// codename:keeper-redesign.
func TestIntegration_TwinE2E_DefaultsPin(t *testing.T) {
	d := keeper.ResolveCyclerDefaultsForTest()
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"ActAbsTokens", float64(d.ActAbsTokens), 300_000},
		{"ActPctCeil", d.ActPctCeil, 0.85},
		{"WarnAbsTokens", float64(d.WarnAbsTokens), 270_000},
		{"WarnPctCeil", d.WarnPctCeil, 0.70},
		{"ForceActAbsTokens", float64(d.ForceActAbsTokens), 340_000},
		{"ForceActPctCeil", d.ForceActPctCeil, 0.95},
		{"ActPct", d.ActPct, 90},
		{"WarnPct", d.WarnPct, 80},
		{"ForceActPct", d.ForceActPct, 95},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v (operator-pinned band — NO retune)", c.name, c.got, c.want)
		}
	}

	// Operator idle/remote-control-vs-live-typist distinction (the guard the
	// headline relies on to NOT false-suppress). Exercises the production parser.
	now := time.Now()
	w := keeper.OperatorActiveWindowForTest
	live := fmt.Sprintf("%d\n", now.Unix())                      // keystroke just now → live typist.
	idle := fmt.Sprintf("%d\n", now.Add(-10*time.Minute).Unix()) // frozen at attach → remote-control.
	if !keeper.OperatorActiveSinceForTest(live, now, w) {
		t.Error("tw: a just-now keystroke must read as an ACTIVE live typist (suppress)")
	}
	if keeper.OperatorActiveSinceForTest(idle, now, w) {
		t.Error("tw: an idle/remote-control client (stale activity) must read as NOT active (proceed)")
	}
	if keeper.OperatorActiveSinceForTest("", now, w) {
		t.Error("tw: no attached client must read as NOT active")
	}
}

// TestIntegration_TwinE2E_GaugeStateTransitions is the gauge-state-transition
// table standing invariant (hk-nlio). It drives the REAL keeper.Cycler.MaybeRun
// across representative gauge states and asserts the fire/no-fire decision,
// pinning the effective band BEHAVIORALLY: Act 300k absolute on a 1M window,
// Act 0.85*window on a 200k window, and Force 340k bypassing CrispIdle. A fired
// cycle emits handoff_started (here it then aborts on the missing nonce, since
// there is no real pane — fire/no-fire is all this table cares about).
func TestIntegration_TwinE2E_GaugeStateTransitions(t *testing.T) {
	cases := []struct {
		name      string
		window    int64
		tokens    int64
		crispIdle bool
		wantFired bool
	}{
		{"1m-below-act", 1_000_000, 299_999, true, false},
		{"1m-at-act", 1_000_000, 300_000, true, true},
		{"1m-below-warn", 1_000_000, 269_999, true, false},
		{"200k-below-act-ceil", 200_000, 169_999, true, false}, // 0.85*200k = 170k
		{"200k-at-act-ceil", 200_000, 170_000, true, true},
		{"1m-act-but-not-crisp", 1_000_000, 300_000, false, false},   // act but below force, not idle → no fire
		{"1m-force-bypasses-crisp", 1_000_000, 340_000, false, true}, // force bypasses CrispIdle
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			em := &keeper.RecordingEmitter{}
			cfg := keeper.CyclerConfig{
				AgentName:           "twe2egst",
				ProjectDir:          t.TempDir(),
				TmuxTarget:          "", // no real pane: a fired cycle aborts on the missing nonce.
				HandoffTimeout:      200 * time.Millisecond,
				PollInterval:        30 * time.Millisecond,
				ClearSettle:         30 * time.Millisecond,
				IsManagedFn:         func(_, _ string) bool { return true },
				CrispIdleFn:         func(_, _ string) bool { return c.crispIdle },
				HoldingDispatchFn:   func(_, _ string) bool { return false },
				HandoffFilePath:     func(_, _ string) string { return filepath.Join(t.TempDir(), "HANDOFF.md") },
				ReadHandoff:         func(_ string) (string, error) { return "", nil }, // never confirms → abort.
				TruncateHandoffFn:   func(_ string) error { return nil },
				WriteJournalFn:      func(_ string, _ *keeper.CycleJournal) error { return nil },
				SetManagedSessionFn: func(_, _, _ string) error { return nil },
				InjectFn:            func(_ context.Context, _, _ string) error { return nil },
			}
			cycler := keeper.NewCycler(cfg, em)
			cf := &keeper.CtxFile{
				Tokens:     c.tokens,
				WindowSize: c.window,
				SessionID:  fmt.Sprintf("sess-gst-%s", c.name),
			}
			if err := cycler.MaybeRun(context.Background(), cf); err != nil {
				t.Fatalf("MaybeRun: %v", err)
			}
			fired := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)) > 0
			if fired != c.wantFired {
				t.Errorf("fired=%v; want %v (tokens=%d window=%d crisp=%v)", fired, c.wantFired, c.tokens, c.window, c.crispIdle)
			}
		})
	}
}

// twIsValidUUIDv4 checks the canonical 8-4-4-4-12 layout with the version nibble
// '4' at index 14 and the RFC-4122 variant (8/9/a/b) at index 19. Mirrors the
// twin's own newUUIDv4 contract (keeper rejects UUIDv7).
func twIsValidUUIDv4(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	if s[14] != '4' {
		return false
	}
	switch s[19] {
	case '8', '9', 'a', 'b', 'A', 'B':
		return true
	}
	return false
}
