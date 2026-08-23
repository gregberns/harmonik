//go:build integration

package keeper_test

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

func twReadRawCtxSID(project, agent string) string {
	path := filepath.Join(project, ".harmonik", "keeper", agent+".ctx")
	raw, err := os.ReadFile(path) //nolint:gosec // G304: test-local temp path
	if err != nil {
		return ""
	}
	var cf keeper.CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		return ""
	}
	return cf.SessionID
}

func twRequireTmux(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tw: tmux not found on PATH; skipping real-tmux twin E2E test")
	}
}

func twRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("tw: getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

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

func twUniqueSessionName() string {
	return fmt.Sprintf("hksav-twin-%d-%d", rand.Int64(), rand.Int64()) //nolint:gosec // G404: test-local session-name uniqueness, no security relevance
}

func twPaneAlive(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", "="+name).Run() //nolint:gosec // G204: name is a test-local generated session name
	return err == nil
}

func twKillAndWait(t *testing.T, name string) {
	t.Helper()
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
	// sessionName, when non-empty, is used as the tmux session name instead of
	// a random twUniqueSessionName(). Needed by tests that exercise the REAL
	// (unstubbed) keeper.ResolveTmuxTarget, which derives the canonical
	// "harmonik-<hash12(project)>-<agent>" name from projectDir+agentName —
	// a random name can never be found by that derivation (hk-9cqtm twin-loop
	// proof). Pass keeper.HarmonikSessionName(project, agent) here to make the
	// twin's real session discoverable by the real resolver.
	sessionName string
}

func twStartTwin(t *testing.T, spec twTwinSpec) string {
	t.Helper()
	if spec.model == "" {
		spec.model = "claude-opus-4-8 [1m]"
	}
	sess := spec.sessionName
	if sess == "" {
		sess = twUniqueSessionName()
	}

	cmd := fmt.Sprintf(
		"exec %s --project %s --agent %s --statusline %s --idle-hook %s --model %q --window %d --growth %d --start-tokens %d --emit-interval %s",
		spec.twin, spec.project, spec.agent, spec.statusline, spec.idleHook,
		spec.model, spec.window, spec.growth, spec.startTokens, spec.emitEvery,
	)
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
	for _, e := range spec.extraEnv {
		args = append(args, "-e", e)
	}
	args = append(args, "sh", "-c", cmd)

	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil { //nolint:gosec // G204: test-local generated args
		t.Fatalf("tw: tmux new-session %q: %v\n%s", sess, err, out)
	}
	t.Cleanup(func() { twKillAndWait(t, sess) })
	return sess
}

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

	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw: WriteManagedSessionID: %v", err)
	}

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

	seed := twWaitForCtxTokens(t, project, agent, 300_000, 8*time.Second)
	seedSID := seed.SessionID
	if seedSID == "" {
		t.Fatal("tw: seed .ctx has empty session_id")
	}
	if seed.WindowSize != 1_000_000 {
		t.Fatalf("tw: seed .ctx window_size = %d; want 1000000", seed.WindowSize)
	}
	if !keeper.CrispIdle(project, agent) {
		t.Fatal("tw: CrispIdle false at seed — the twin's .idle marker did not register a crisp boundary")
	}
	if keeper.HoldingDispatch(project, agent) {
		t.Fatal("tw: HoldingDispatch true unexpectedly (no .dispatching marker was written)")
	}

	em := &keeper.RecordingEmitter{}
	cfgOverrides := testCycleOverrides{Inject:

	// Generous real-time budgets: real tmux send-keys + script emits are slow.

	// REAL, UNMODIFIED keeper.InjectText — the production InjectFn default,
	// performing the real tmux load-buffer → paste-buffer → send-keys Enter
	// sequence with the verbatim MULTI-LINE /session-handoff directive (no
	// flatten). The twin now parses the multi-line directive natively
	// (hk-fan), so the E2E exercises the maximally-faithful path. Everything
	// else (ReadGaugeFn, ReadHandoff, IdleProbe, DispatchProbe,
	// ManagedProbe, managed-session port, HandoffFilePath, TruncateHandoffFn,
	// defaults.
	keeper.InjectText}
	cfg := keeper.CyclerConfig{
		AgentName:  agent,
		ProjectDir: project,
		TmuxTarget: sess,

		HandoffTimeout: 10 * time.Second,
		ClearSettle:    5 * time.Second,
		PollInterval:   150 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	stopWatch := make(chan struct{})
	resetCh := make(chan int64, 1)
	go twWatchForReset(project, agent, seedSID, stopWatch, resetCh)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		close(stopWatch)
		t.Fatalf("tw: MaybeRun: %v", err)
	}
	close(stopWatch)
	minPostClearTokens := <-resetCh

	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")
	hb, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("tw: read HANDOFF: %v", err)
	}
	if !strings.Contains(string(hb), "<!-- KEEPER:") {
		t.Fatalf("tw: HANDOFF file missing keeper nonce; got:\n%s", hb)
	}

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

	idlePath := filepath.Join(project, ".harmonik", "keeper", agent+".idle")
	if _, err := os.Stat(idlePath); err != nil {
		t.Errorf("tw: .idle marker missing after cycle: %v", err)
	}

	complete := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(complete) != 1 {
		t.Fatalf("tw: want 1 cycle_complete; got %d (events imply the cycle did not finish cleanly)", len(complete))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Errorf("tw: want 0 cycle_parked on the happy path; got %d", n)
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
//	    so the gauge reports an absolute-token view well above Act 215k.
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
//	NOT v7) → agent brief (T8/I1), with .managed rebound to the rotated session_id.
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

	if err := keeper.WriteManagedSessionID(project, agent, ""); err != nil {
		t.Fatalf("tw: WriteManagedSessionID: %v", err)
	}

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

	operatorAttached := func(_ string) bool {
		staleClient := fmt.Sprintf("%d\n", time.Now().Add(-10*time.Minute).Unix())
		return keeper.OperatorActiveSinceForTest(staleClient, time.Now(), keeper.OperatorActiveWindowForTest)
	}
	if operatorAttached(sess) {
		t.Fatal("tw: idle/remote-control client mis-read as ACTIVE — it would false-suppress the cycle")
	}

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
	cfgOverrides := testCycleOverrides{Inject: recInject}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     project,
		TmuxTarget:     sess,
		HandoffTimeout: 10 * time.Second,
		ClearSettle:    6 * time.Second,
		PollInterval:   150 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Operator = testOperatorProbe(operatorAttached)
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			return recSetManaged(project, agent, sid)
		}}
	})

	stopWatch := make(chan struct{})
	resetCh := make(chan int64, 1)
	go twWatchForReset(project, agent, seedSID, stopWatch, resetCh)

	if err := cycler.MaybeRun(context.Background(), seed); err != nil {
		close(stopWatch)
		t.Fatalf("tw: MaybeRun: %v", err)
	}
	close(stopWatch)
	minPostClearTokens := <-resetCh

	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")
	hb, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("tw: read HANDOFF: %v", err)
	}
	if !strings.Contains(string(hb), "<!-- KEEPER:") {
		t.Fatalf("tw: HANDOFF file missing keeper nonce; got:\n%s", hb)
	}

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

	if minPostClearTokens < 0 {
		t.Errorf("tw: never observed a reading on the rotated session — cannot confirm the token reset")
	} else if minPostClearTokens >= seed.Tokens {
		t.Errorf("tw: tokens did not drop after /clear: min-on-new-session=%d >= seed=%d", minPostClearTokens, seed.Tokens)
	}

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

	complete := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)
	if len(complete) != 1 {
		t.Fatalf("tw: want 1 cycle_complete; got %d", len(complete))
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleParked)); n != 0 {
		t.Errorf("tw: want 0 cycle_parked; got %d", n)
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

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperOperatorAttached)); n != 0 {
		t.Errorf("tw: want 0 operator_attached events for an IDLE/remote-control client; got %d", n)
	}

	handoffIdx, clearIdx, briefIdx := -1, -1, -1
	clears := 0
	for i, text := range injects {
		switch {
		case strings.Contains(text, "/session-handoff"):
			if handoffIdx < 0 {
				handoffIdx = i
			}
		case strings.Contains(text, "agent brief"):
			briefIdx = i
		case strings.Contains(text, "/clear"):
			clears++
			clearIdx = i
		}
	}
	if clears != 1 {
		t.Errorf("tw: want exactly 1 /clear injection (no-auto-clear); got %d (%v)", clears, injects)
	}
	if !(handoffIdx >= 0 && handoffIdx < clearIdx && clearIdx < briefIdx) {
		t.Errorf("tw: inject order must be handoff(%d) < clear(%d) < brief(%d): %v", handoffIdx, clearIdx, briefIdx, injects)
	}

	if len(setManagedSIDs) != 1 {
		t.Fatalf("tw: want SetManagedSession called exactly once; got %d (%v)", len(setManagedSIDs), setManagedSIDs)
	}
	if setManagedSIDs[0] != final.SessionID {
		t.Errorf("tw: SetManagedSession called with %q; want the rotated SID %q", setManagedSIDs[0], final.SessionID)
	}

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHandoffStarted)); n != 1 {
		t.Errorf("tw: want 1 handoff_started; got %d", n)
	}
}

// TestIntegration_TwinE2E_DefaultsPin asserts the STANDING band-PIN invariant in
// the same suite as the headline gate (hk-nlio): the resolved CyclerConfig
// defaults are the operator-decided Act 215k / 0.85, Warn 200k / 0.70, and
// Force +25k → 240k / 0.95. NO band retune — widening the band is an operator
// HARD-NO. The TA1 earlier-restart band (hk-8hr1, operator-authorized
// 2026-06-17) is the SOURCE OF TRUTH in thresholds.go; these expectations track
// it. A value drifting here fails the gate. Refs: hk-8hr1, hk-lhu2, hk-bpkv,
// codename:keeper-redesign.
func TestIntegration_TwinE2E_DefaultsPin(t *testing.T) {
	d := keeper.ResolveCyclerDefaultsForTest()
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"ActAbsTokens", float64(d.ActAbsTokens), 200_000},
		{"ActPctCeil", d.ActPctCeil, 0.85},
		{"WarnAbsTokens", float64(d.WarnAbsTokens), 170_000},
		{"WarnPctCeil", d.WarnPctCeil, 0.70},
		{"ForceActAbsTokens", float64(d.ForceActAbsTokens), 220_000},
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
// pinning the effective band BEHAVIORALLY: Act 215k absolute on a 1M window,
// Act 0.85*window on a 200k window, and Force 240k bypassing CrispIdle. The TA1
// earlier-restart band (hk-8hr1) is the source of truth in thresholds.go. A
// fired cycle emits handoff_started (here it then aborts on the missing nonce,
// since there is no real pane — fire/no-fire is all this table cares about).
func TestIntegration_TwinE2E_GaugeStateTransitions(t *testing.T) {
	cases := []struct {
		name      string
		window    int64
		tokens    int64
		crispIdle bool
		wantFired bool
	}{
		{"1m-below-act", 1_000_000, 199_999, true, false},
		{"1m-at-act", 1_000_000, 200_000, true, true},
		{"1m-below-warn", 1_000_000, 169_999, true, false},
		{"200k-below-act-ceil", 200_000, 169_999, true, false}, // 0.85*200k = 170k
		{"200k-at-act-ceil", 200_000, 170_000, true, true},
		{"1m-act-but-not-crisp", 1_000_000, 200_000, false, false},   // act but below force, not idle → no fire
		{"1m-force-bypasses-crisp", 1_000_000, 220_000, false, true}, // force bypasses CrispIdle
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			em := &keeper.RecordingEmitter{}
			cfgOverrides := testCycleOverrides{HandoffPath:

			// no real pane: a fired cycle aborts on the missing nonce.

			func(_, _ string) string { return filepath.Join(t.TempDir(), "HANDOFF.md") }, HandoffRead: func(_ string) (string, error) { return "", nil }, HandoffScrub: // never confirms → abort.
			func(_ string) error { return nil }, JournalWrite:                                         func(_ string, _ *keeper.CycleJournal) error { return nil }, Inject: func(_ context.Context, _, _ string) error { return nil }}
			cfg := keeper.CyclerConfig{
				AgentName:      "twe2egst",
				ProjectDir:     t.TempDir(),
				TmuxTarget:     "",
				HandoffTimeout: 200 * time.Millisecond,
				PollInterval:   30 * time.Millisecond,
				ClearSettle:    30 * time.Millisecond,
			}
			cycler := mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
				deps.Idle = testIdleProbe(c.crispIdle)
			})
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

// TestIntegration_TwinWatcher_ExternalClearReResolve proves the watcher's
// foreign-guard re-resolve path (hk-1tn2) end-to-end against the faithful
// session twin in a real tmux pane. It drives an EXTERNAL /clear — one NOT
// caused by the keeper's own cycle — which rotates the twin's session_id.
// The test then asserts the watcher re-resolves .managed to the new id without
// emitting any foreign_session event.
//
// Scenario:
//  1. A prior watcher latched SID-A into .managed.
//  2. The operator issues /clear directly into the twin pane (no keeper cycle);
//     the twin mints SID-B and the gauge reflects it.
//  3. The SessionStart hook (simulated by writing .sid=SID-B) records SID-B as
//     the authoritative live identity.
//  4. The watcher's next tick sees: managed=SID-A, gauge=SID-B (from .sid),
//     .sid=SID-B == gauge → re-adopt, updating .managed to SID-B.
//
// RED on a pre-fix main that treats every SID mismatch as a true concurrent
// foreign session; GREEN once the re-resolve path (hk-1tn2) lands.
func TestIntegration_TwinWatcher_ExternalClearReResolve(t *testing.T) {
	twRequireTmux(t)

	project := t.TempDir()
	agent := fmt.Sprintf("twextclr%d", rand.Int64()) //nolint:gosec // G404: test-local agent-name uniqueness
	twin := twBuildTwin(t, project)
	statusline, idleHook := twScripts(t)

	const startTokens int64 = 50_000

	sess := twStartTwin(t, twTwinSpec{
		project:     project,
		agent:       agent,
		twin:        twin,
		statusline:  statusline,
		idleHook:    idleHook,
		model:       "claude-opus-4-8 [1m]",
		window:      1_000_000,
		growth:      50_000,
		startTokens: startTokens,
		emitEvery:   200 * time.Millisecond,
	})

	seed := twWaitForCtxTokens(t, project, agent, startTokens+1, 5*time.Second)
	seedSID := seed.SessionID
	if seedSID == "" {
		t.Fatal("tw: seed .ctx has empty session_id")
	}

	if err := keeper.WriteManagedSessionID(project, agent, seedSID); err != nil {
		t.Fatalf("tw: WriteManagedSessionID(seedSID): %v", err)
	}

	if err := keeper.InjectText(context.Background(), sess, "/clear"); err != nil {
		t.Fatalf("tw: inject /clear into twin pane: %v", err)
	}

	var newSID string
	sidDeadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(sidDeadline) {
		if raw := twReadRawCtxSID(project, agent); raw != "" && raw != seedSID {
			newSID = raw
			break
		}
		time.Sleep(75 * time.Millisecond)
	}
	if newSID == "" {
		t.Fatalf("tw: gauge never reflected the rotated session_id after external /clear (seed %q unchanged within timeout)", seedSID)
	}
	if !twIsValidUUIDv4(newSID) {
		t.Fatalf("tw: rotated session_id %q is not a valid UUIDv4", newSID)
	}

	writeSidFile(t, project, agent, newSID)

	em := &keeper.RecordingEmitter{}
	adoptedCh := make(chan string, 1)

	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   project,
		TmuxTarget:   "", // no injection needed; warn is a side-effect only
		PollInterval: 100 * time.Millisecond,
		Staleness:    30 * time.Second,
		IdleQuiesce:  1 * time.Millisecond,
		WarnPct:      80.0,
		WriteManagedSessionFn: func(projectDir, agentName, sid string) error {
			select {
			case adoptedCh <- sid:
			default:
			}
			return keeper.WriteManagedSessionID(projectDir, agentName, sid)
		},
	}

	watchCtx, watchCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer watchCancel()

	w := keeper.NewWatcher(cfg, em)
	go func() { _ = w.Run(watchCtx) }() //nolint:errcheck // context cancel is expected

	var gotSID string
	select {
	case gotSID = <-adoptedCh:
	case <-watchCtx.Done():
		t.Fatalf("tw: watcher did not re-resolve .managed within the run window "+
			"(re-resolve path did not fire; seed=%q new=%q)", seedSID, newSID)
	}
	watchCancel()

	if gotSID != newSID {
		t.Errorf("tw: watcher re-adopted %q; want the rotated SID %q", gotSID, newSID)
	}

	for _, ev := range em.EventsOfType(core.EventTypeSessionKeeperNoGauge) {
		var payload core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload.Reason == "foreign_session" {
			t.Errorf("tw: watcher emitted no_gauge(foreign_session); the re-resolve path must adopt same-agent rotations cleanly (seed=%q new=%q)", seedSID, newSID)
		}
	}
}

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
