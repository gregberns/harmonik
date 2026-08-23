//go:build integration

package keeper

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

const zj1yLowWarnTokens int64 = 60_000

func zj1yWriteGauge(t *testing.T, projectDir, agent string, tokens int64, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
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

func zj1yWriteSid(t *testing.T, projectDir, agent, sid string) {
	t.Helper()
	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("zj1y: mkdir keeper dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(sid+"\n"), 0o600); err != nil {
		t.Fatalf("zj1y: write sid: %v", err)
	}
}

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

func TestZJ1Y_ActionableWarn_LowConfigWarn_NamesVerbatimRestartNowCommand(t *testing.T) {
	projectDir := t.TempDir()
	const agent = "captain"

	tmuxTarget := zj1ySpawnThrowawayTmuxPane(t)
	_ = tmuxTarget // spawned + leak-guarded; not used for keystroke injection here

	if zj1yLowWarnTokens >= DefaultWarnAbsTokens {
		t.Fatalf("zj1y: low warn %d must be far below the %d default to be a real config-driven trigger",
			zj1yLowWarnTokens, DefaultWarnAbsTokens)
	}

	cfg := WatcherConfig{
		AgentName:          agent,
		SelfServiceEnabled: true,
		WarnAbsTokens:      zj1yLowWarnTokens,
	}
	wantCmd := "harmonik keeper restart-now --agent " + agent
	selected := cfg.selectWarnText(&CtxFile{SessionID: primarySID, Tokens: 70_000, WindowSize: 200_000, Pct: 85}, true /*crispIdle*/, false /*operatorAttached*/)
	if !strings.Contains(selected, restartNowStem) {
		t.Fatalf("zj1y: selected warn text must carry the restart-now stem; got: %s", selected)
	}
	if !strings.Contains(selected, wantCmd) {
		t.Fatalf("zj1y: selected warn text must name the VERBATIM agent-specific command %q; got: %s",
			wantCmd, selected)
	}

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

func TestZJ1Y_SelfServiceRestart_GaugeDrop_ExactlyOneClear(t *testing.T) {
	const (
		agent   = "captain"
		cycleID = "cyc-zj1y-one-clear"
		sid     = "33333333-4444-4333-8444-000000000003"
	)

	em := &RecordingEmitter{}
	spy := &zj1yInjectSpy{}
	nonce := "<!-- KEEPER:" + cycleID + " -->"

	overrides := configTestOverrides{
		cycleIDs: func() string { return cycleID },
		path:     func(_, a string) string { return "/tmp/HANDOFF-zj1y-" + a + ".md" },
		read:     func(_ string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil },
		scrub:    func(_ string) error { return nil },
		inject:   spy.inject,
		gauge: func(_, _ string) (*CtxFile, time.Time, error) {
			return &CtxFile{Tokens: 40_000, WindowSize: 200_000, Pct: 20, SessionID: sid}, time.Now(), nil
		},
		journal: func(_ string, _ *CycleJournal) error { return nil },
	}
	cfg := CyclerConfig{
		AgentName:      agent,
		ProjectDir:     t.TempDir(),
		TmuxTarget:     "zj1y-fake-pane",
		ActAbsTokens:   200_000,
		WarnAbsTokens:  170_000,
		HandoffTimeout: 200 * time.Millisecond,
		ClearSettle:    20 * time.Millisecond,
		PollInterval:   5 * time.Millisecond,
	}
	cycler := mustNewCyclerWithConfigOverrides(cfg, em, overrides)
	ctx := context.Background()

	high := &CtxFile{Tokens: 210_000, WindowSize: 200_000, Pct: 95, SessionID: sid}
	if err := cycler.MaybeRun(ctx, high); err != nil {
		t.Fatalf("zj1y: MaybeRun(high): %v", err)
	}
	if got := spy.clears(); got != 1 {
		t.Fatalf("zj1y: setup expected exactly 1 /clear from the first cycle; got %d (%v)", got, spy.snapshot())
	}

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

func TestZJ1Y_PostClearNewSID_Adopted_NoForeignSession(t *testing.T) {
	projectDir := t.TempDir()
	const agent = "captain"
	const oldSID = "11111111-2222-4333-8444-aaaaaaaaaaaa" // valid UUIDv4 (prior session)
	const newSID = "55555555-6666-4333-8444-bbbbbbbbbbbb" // valid UUIDv4 (post-/clear mint)

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
	cancel()
	<-done

	if gotSID != newSID {
		t.Errorf("zj1y: watcher adopted %q; want the rotated new SID %q (NOT the stale old %q)", gotSID, newSID, oldSID)
	}

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
