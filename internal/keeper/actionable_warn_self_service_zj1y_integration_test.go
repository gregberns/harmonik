//go:build integration

package keeper

// actionable_warn_self_service_zj1y_integration_test.go — LIVE/integration
// definition-of-done test for hk-zj1y: prove, DETERMINISTICALLY (not via organic
// context fill), the three keeper behaviors that compose the
// "actionable warn → self-service restart → identity re-tie" flow:
//
//   R3 (hk-vs4u) — Actionable warn names the VERBATIM self-service command. A LOW
//     configured warn threshold (set in .harmonik/config.yaml's keeper block, NOT
//     a code default) drives a warn crossing on the very first tick. The text the
//     production warn-inject path SELECTS (the exact selectWarnText call the
//     watcher loop makes at watcher.go ~1286 on the nil-InjectFn branch) contains
//     the verbatim `harmonik keeper restart-now --agent <agentname>` command. We
//     ALSO run the real Watcher.Run loop end-to-end and assert a real
//     session_keeper_warn event fires at the LOW configured threshold (proving the
//     config value — not the compiled 200000 default — armed the warn).
//
//   hk-1ryc — Exactly ONE /clear (no double-restart). After the agent's own
//     `harmonik keeper restart-now` synchronously injects /clear and the gauge
//     drops below the act threshold, the Cycler's Gate-3 (belowActThreshold)
//     suppresses any SECOND auto-cycle on the next tick — using the IN-PROCESS
//     gauge signal, NO marker file. We drive one auto-cycle (one /clear), then a
//     dropped gauge, and assert still exactly one /clear and exactly one
//     cycle_complete.
//
//   R4 (hk-1tn2 re-resolve) — Identity re-tie after a post-/clear new-session-id
//     mint. With .managed bound to the OLD SID and the gauge + .sid both endorsing
//     a NEW (rotated) UUIDv4 SID, the Watcher re-adopts the new SID (calls
//     WriteManagedSessionFn with it) WITHOUT emitting any no_gauge(foreign_session)
//     event — i.e. no sustained (>1-tick) foreign blindness.
//
// ─────────────────────────────────────────────────────────────────────────────
// HONESTY — what is proven MECHANICALLY here vs. what only LIVE fleet use validates
// ─────────────────────────────────────────────────────────────────────────────
// PROVEN MECHANICALLY by this test (no fakes for the parts under test):
//   - The LOW config warn threshold flows config.yaml → LoadProjectConfig →
//     ResolveKeeperConfig → buildKeeperConfigs is exercised by the SIBLING
//     cmd/harmonik config_e2e test; HERE the low warn is applied directly to the
//     real WatcherConfig and the real Watcher.Run loop fires a real
//     session_keeper_warn at it (criterion 1).
//   - The SELECTED warn text (the production selectWarnText path) carries the
//     verbatim restart-now command for THIS agent name (criterion 2).
//   - Gate-3 gauge-drop suppression: the real Cycler fires exactly ONE /clear and
//     ONE cycle_complete across a high→drop gauge transition (criterion 3).
//   - The real Watcher re-resolve path adopts a rotated UUIDv4 SID with NO
//     foreign_session emission (criterion 4).
//
// ONLY LIVE FLEET USE VALIDATES (NOT mechanizable here, documented not overclaimed):
//   - A human/agent actually TYPING /session-handoff and /session-resume into a
//     real Claude REPL pane in response to the injected actionable warn, and that
//     pane's REAL context dropping after the real /clear. This test simulates the
//     post-/clear gauge drop and the post-/clear new-SID mint by writing the gauge
//     and .sid directly (the same seam the SessionStart hook fills live); it does
//     NOT drive a real Claude agent reading the warn and self-handing-off. The
//     causal "agent reads warn, runs the command, pane clears" loop is exercised
//     only on the live fleet (and partially by the real-tmux twin in
//     cycle_twin_e2e_integration_test.go, which this test does NOT duplicate).
//
// A throwaway tmux pane IS spawned with a before/after leak guard (criterion 5);
// the watcher/cycler logic under test is gauge-file driven, so the pane proves a
// real pane lifecycle without leaking rather than carrying the assertions.
//
// Placement: package keeper (internal) so the SELECTED-text assertion can call the
// unexported selectWarnText — the exact function the watcher's nil-InjectFn warn
// branch invokes. Helper names are zj1y-prefixed to avoid collision with the
// package-keeper helpers in actionable_warn_hkvs4u_test.go (restartNowStem,
// ctxWith, primarySID) which this file REUSES.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// zj1yLowWarnTokens is the LOW configured warn threshold the test drives the warn
// from — deliberately far below the compiled 200000 default so a warn crossing at
// this level can ONLY be the configured value, never the default. The act/force
// thresholds stay at their normal high values so the warn fires WITHOUT tripping a
// cycle (warn-text selection is the thing under test in criterion 2).
const zj1yLowWarnTokens int64 = 60_000

// zj1yWriteGauge writes <agent>.ctx with the given tokens + session_id and a
// WindowSize so the absolute-token gates are active (Tokens>0 && WindowSize>0).
// Distinct name from the package-keeper_test writeGauge helper.
func zj1yWriteGauge(t *testing.T, projectDir, agent string, tokens int64, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("zj1y: mkdir keeper dir: %v", err)
	}
	cf := CtxFile{
		Pct:        float64(tokens) / 200_000.0 * 100.0,
		Tokens:     tokens,
		WindowSize: 200_000,
		SessionID:  sid,
		Ts:         time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(cf)
	if err != nil {
		t.Fatalf("zj1y: marshal ctx: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".ctx"), append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("zj1y: write ctx: %v", err)
	}
}

// zj1yWriteSid writes <agent>.sid directly, modeling the SessionStart hook. Once a
// valid UUIDv4 .sid is present, ReadCtxFile overrides the gauge's raw session_id
// with it — the watcher's re-resolve gate then confirms .sid == gauge and re-adopts.
func zj1yWriteSid(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("zj1y: mkdir keeper dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("zj1y: write sid: %v", err)
	}
}

// zj1yInjectSpy records every text injected by the Cycler. Thread-safe so the
// watcher/cycler can call it from any goroutine.
type zj1yInjectSpy struct {
	mu   sync.Mutex
	sent []string
}

func (s *zj1yInjectSpy) inject(_ context.Context, _, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, text)
	return nil
}

func (s *zj1yInjectSpy) clears() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, tx := range s.sent {
		if strings.TrimSpace(tx) == "/clear" {
			n++
		}
	}
	return n
}

func (s *zj1yInjectSpy) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.sent))
	copy(out, s.sent)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Criterion 1 + 2: deterministic LOW-warn trigger + actionable warn names the
// VERBATIM self-service command.
// ─────────────────────────────────────────────────────────────────────────────

func TestZJ1Y_ActionableWarn_LowConfigWarn_NamesVerbatimRestartNowCommand(t *testing.T) {
	projectDir := t.TempDir()
	const agent = "captain"

	// Spawn a throwaway tmux pane (criterion 5 leak guard). The warn path under
	// test is gauge-driven; the pane proves a real lifecycle without leaking. We do
	// NOT route the warn injection into this pane: the live Watcher loop below runs
	// with TmuxTarget EMPTY (warn emits, no keystroke injection) and we assert the
	// SELECTED text separately via selectWarnText — the exact call the watcher makes.
	tmuxTarget := zj1ySpawnThrowawayTmuxPane(t)
	_ = tmuxTarget // spawned + leak-guarded; not used for keystroke injection here

	// Guard: the LOW config warn must differ from the compiled default, else the
	// "config armed the warn, not the default" claim is trivial.
	if zj1yLowWarnTokens >= DefaultWarnAbsTokens {
		t.Fatalf("zj1y: low warn %d must be far below the %d default to be a real config-driven trigger",
			zj1yLowWarnTokens, DefaultWarnAbsTokens)
	}

	// ── Criterion 2: the SELECTED warn text (production selectWarnText) carries the
	// verbatim restart-now command for THIS agent. This is the exact function the
	// watcher's nil-InjectFn warn branch calls (watcher.go ~1286). The actionable
	// form requires SelfServiceEnabled + a primary UUIDv4 SID + CrispIdle +
	// detached. We assert it contains the templated, agent-specific command.
	cfg := WatcherConfig{
		AgentName:          agent,
		SelfServiceEnabled: true,
		WarnAbsTokens:      zj1yLowWarnTokens,
	}
	wantCmd := "harmonik keeper restart-now --agent " + agent
	selected := cfg.selectWarnText(ctxWith(primarySID, 70_000), true /*crispIdle*/, false /*operatorAttached*/)
	if !strings.Contains(selected, restartNowStem) {
		t.Fatalf("zj1y: selected warn text must carry the restart-now stem; got: %s", selected)
	}
	if !strings.Contains(selected, wantCmd) {
		t.Fatalf("zj1y: selected warn text must name the VERBATIM agent-specific command %q; got: %s",
			wantCmd, selected)
	}

	// ── Criterion 1: drive the LOW configured warn end-to-end through the real
	// Watcher.Run loop and assert a real session_keeper_warn fires AT the low
	// threshold. The gauge sits at 70000 — above the LOW 60000 config warn but FAR
	// below the 200000 default — so a warn here can ONLY be the configured value.
	// TmuxTarget empty: warn emits without injecting keystrokes into a real pane.
	zj1yWriteGauge(t, projectDir, agent, 70_000, primarySID)
	zj1yWriteSid(t, projectDir, agent, primarySID)

	runCfg := WatcherConfig{
		AgentName:     agent,
		ProjectDir:    projectDir,
		PollInterval:  5 * time.Millisecond,
		IdleQuiesce:   1 * time.Millisecond,
		Staleness:     120 * time.Second,
		WarnAbsTokens: zj1yLowWarnTokens, // ← the LOW configured threshold under test
		// WarnPct is the pct<WarnPct NECESSARY-condition gate shared byte-for-byte
		// with CyclerConfig.belowWarnThreshold (hk-lbo9w/F45): it exists to stop a
		// default 200k abs threshold from firing prematurely on a huge (e.g. 1M)
		// context window, and is NOT derived from WarnAbsTokens — config.yaml has no
		// warn_pct knob (only warn_pct_ceil, a different field). A LOW abs config
		// must be paired with a correspondingly LOW WarnPct for the abs threshold to
		// actually govern, exactly as an operator configuring a low context budget
		// would set both. 30 comfortably sits below the gauge's 35% (70000/200000)
		// pct so the LOW abs value — not the 80-default pct gate — decides the fire.
		WarnPct: 30,
		// act/force stay high (defaults) so the warn fires WITHOUT a cycle.
		TmuxTarget:         "", // warn emits; no keystroke injection into the throwaway pane
		SelfServiceEnabled: true,
	}
	em := &RecordingEmitter{}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	w := NewWatcher(runCfg, em)
	_ = w.Run(ctx) //nolint:errcheck // DeadlineExceeded expected

	warns := em.EventsOfType(core.EventTypeSessionKeeperWarn)
	if len(warns) == 0 {
		t.Fatalf("zj1y: want >=1 session_keeper_warn at the LOW configured warn %d (gauge 70000, well below the %d default); got 0",
			zj1yLowWarnTokens, DefaultWarnAbsTokens)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Criterion 3: exactly ONE /clear — Gate-3 gauge-drop suppression (hk-1ryc).
// ─────────────────────────────────────────────────────────────────────────────

func TestZJ1Y_SelfServiceRestart_GaugeDrop_ExactlyOneClear(t *testing.T) {
	const (
		agent   = "captain"
		cycleID = "cyc-zj1y-one-clear"
		sid     = "33333333-4444-4333-8444-000000000003"
	)

	em := &RecordingEmitter{}
	spy := &zj1yInjectSpy{}
	nonce := "<!-- KEEPER:" + cycleID + " -->"

	cfg := CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "zj1y-fake-pane",
		ActAbsTokens:   200_000,
		WarnAbsTokens:  170_000,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    20 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-zj1y-" + a + ".md"
		},
		ReadHandoff:       func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil },
		TruncateHandoffFn: func(_ string) error { return nil },
		InjectFn:          spy.inject,
		ReadGaugeFn: func(_, _ string) (*CtxFile, time.Time, error) {
			return &CtxFile{Tokens: 40_000, WindowSize: 200_000, Pct: 20, SessionID: sid}, time.Now(), nil
		},
		CrispIdleFn:        func(_, _ string) bool { return true },
		HoldingDispatchFn:  func(_, _ string) bool { return false },
		WriteJournalFn:     func(_ string, _ *CycleJournal) error { return nil },
		SetTmuxEnvFn:       func(_ context.Context, _, _, _ string) error { return nil },
		OperatorAttachedFn: func(_ string) bool { return false },
		// Stop-hook .idle marker reads fresh so model-done lands on the first
		// AwaitModelDone poll (SK-014) — without it the cycle waits the full
		// ModelDoneTimeout (60s) fail-open before clearing, making this test
		// needlessly slow. Model-done speed does not affect the /clear count; it
		// only removes the 60s wait.
		IdleMarkerModTimeFn: func(_, _ string) (time.Time, bool) { return time.Now(), true },
	}
	cycler := NewCycler(cfg, em)
	ctx := context.Background()

	// Tick 1: gauge ABOVE the act threshold, agent has not yet self-restarted —
	// the keeper auto-cycle fires here (one /clear). This is the cycle the actionable
	// warn nudged the agent to do; here we let it fire to establish "one cycle ran".
	high := &CtxFile{Tokens: 210_000, WindowSize: 200_000, Pct: 95, SessionID: sid}
	if err := cycler.MaybeRun(ctx, high); err != nil {
		t.Fatalf("zj1y: MaybeRun(high): %v", err)
	}
	if got := spy.clears(); got != 1 {
		t.Fatalf("zj1y: setup expected exactly 1 /clear from the first cycle; got %d (%v)", got, spy.snapshot())
	}

	// The agent's own `harmonik keeper restart-now` synchronously injects /clear and
	// drops the gauge below the act threshold. Tick 2 reads that dropped gauge —
	// SAME session_id, low context. Gate-3 (belowActThreshold) must suppress a
	// SECOND cycle: NO new /clear, NO cross-process marker involved.
	low := &CtxFile{Tokens: 40_000, WindowSize: 200_000, Pct: 20, SessionID: sid}
	if err := cycler.MaybeRun(ctx, low); err != nil {
		t.Fatalf("zj1y: MaybeRun(low): %v", err)
	}
	if got := spy.clears(); got != 1 {
		t.Fatalf("zj1y: no-double-restart violated: want still exactly 1 /clear after the gauge dropped; got %d (%v)", got, spy.snapshot())
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Fatalf("zj1y: want exactly 1 cycle_complete (no double restart); got %d", len(evts))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Criterion 4: SID adoption / identity re-tie — no sustained foreign_session (R4).
// ─────────────────────────────────────────────────────────────────────────────

func TestZJ1Y_PostClearNewSID_Adopted_NoForeignSession(t *testing.T) {
	projectDir := t.TempDir()
	const agent = "captain"
	const oldSID = "11111111-2222-4333-8444-aaaaaaaaaaaa" // valid UUIDv4 (prior session)
	const newSID = "55555555-6666-4333-8444-bbbbbbbbbbbb" // valid UUIDv4 (post-/clear mint)

	// State the watcher starts in (the post-/clear new-SID-mint moment):
	//   .managed = oldSID  (stale latch from before /clear)
	//   gauge    = newSID  (rotated by the self-service /clear)
	//   .sid     = newSID  (SessionStart hook endorses the new identity as primary)
	zj1yWriteGauge(t, projectDir, agent, 70_000, newSID)
	zj1yWriteSid(t, projectDir, agent, newSID)
	if err := WriteManagedSessionID(projectDir, agent, oldSID); err != nil {
		t.Fatalf("zj1y: seed .managed=oldSID: %v", err)
	}

	em := &RecordingEmitter{}
	adoptedCh := make(chan string, 4)

	cfg := WatcherConfig{
		AgentName:    agent,
		ProjectDir:   projectDir,
		TmuxTarget:   "", // no injection needed; re-resolve is a gauge/.sid side-effect
		PollInterval: 10 * time.Millisecond,
		Staleness:    30 * time.Second,
		IdleQuiesce:  1 * time.Millisecond,
		// Keep warn high so warn machinery does not interfere with the re-resolve assertion.
		WarnAbsTokens: 200_000,
		WriteManagedSessionFn: func(projectDir, agentName, sid string) error {
			select {
			case adoptedCh <- sid:
			default:
			}
			return WriteManagedSessionID(projectDir, agentName, sid)
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	w := NewWatcher(cfg, em)
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }() //nolint:errcheck // cancel expected

	var gotSID string
	select {
	case gotSID = <-adoptedCh:
	case <-ctx.Done():
		t.Fatalf("zj1y: watcher never re-resolved .managed within the run window (old=%q new=%q)", oldSID, newSID)
	}
	// Stop the watcher and WAIT for the loop goroutine to fully exit before reading
	// em / before t.TempDir() teardown — otherwise a late tick can write the gauge
	// file (or the .managed temp-rename) concurrently with cleanup (a benign but
	// noisy race). Draining done makes the assertions and teardown deterministic.
	cancel()
	<-done

	// (a) the adopted SID is the rotated/new one, not the stale old one.
	if gotSID != newSID {
		t.Errorf("zj1y: watcher adopted %q; want the rotated new SID %q (NOT the stale old %q)", gotSID, newSID, oldSID)
	}

	// (b) NO sustained foreign_session: the re-resolve path must recognize the
	// mismatch as "same agent, new session after /clear" (endorsed by .sid) and
	// re-adopt cleanly. Even ONE foreign_session emit means the gate rejected a
	// valid same-agent rotation as a concurrent intruder (>1-tick blindness).
	for _, ev := range em.EventsOfType(core.EventTypeSessionKeeperNoGauge) {
		var payload core.SessionKeeperNoGaugePayload
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			continue
		}
		if payload.Reason == "foreign_session" {
			t.Errorf("zj1y: watcher emitted no_gauge(foreign_session); the re-resolve path must adopt same-agent rotations cleanly (old=%q new=%q)", oldSID, newSID)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Leak-guarded throwaway tmux pane (criterion 5). zj1y-prefixed so it never
// collides with the cmd/harmonik spawnThrowawayTmuxPane (different package).
// Snapshots tmux ls before/after and kills in cleanup, asserting no leak.
// ─────────────────────────────────────────────────────────────────────────────

func zj1ySpawnThrowawayTmuxPane(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Logf("zj1y: tmux not available (%v); continuing without a live pane (logic under test is gauge-driven)", err)
		return ""
	}

	session := "hk-zj1y-e2e-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	before := zj1yTmuxSnapshot()

	cmd := exec.Command("tmux", "new-session", "-d", "-s", session, "bash")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("zj1y: tmux new-session failed (%v: %s); continuing without a live pane", err, bytes.TrimSpace(out))
		return ""
	}

	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run() //nolint:errcheck
		after := zj1yTmuxSnapshot()
		if _, stillThere := after[session]; stillThere {
			t.Errorf("zj1y LEAK: tmux session %q survived cleanup", session)
		}
		for s := range after {
			if _, existedBefore := before[s]; !existedBefore && s != session {
				if strings.HasPrefix(s, "hk-zj1y-e2e-") {
					t.Errorf("zj1y LEAK: unexpected new tmux session %q after test", s)
				}
			}
		}
	})

	return session + ":0"
}

func zj1yTmuxSnapshot() map[string]struct{} {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	set := map[string]struct{}{}
	if err != nil {
		return set
	}
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		set[string(line)] = struct{}{}
	}
	return set
}
