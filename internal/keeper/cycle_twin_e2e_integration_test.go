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
