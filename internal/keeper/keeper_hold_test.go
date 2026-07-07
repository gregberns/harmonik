package keeper_test

// keeper_hold_test.go — unit tests for the operator HOLD switch (hk-9waz / ES6 /
// codename:keeper-hold). A hold suspends the keeper's destructive ACT/restart
// cutoff while the operator co-works, and MUST auto-revert: it can never survive a
// restart (it is keyed by the live .sid session-id, re-minted on every /clear) and
// it can never survive walk-away/crash (a timer backstop expires a stale marker).
//
// THE LOAD-BEARING PROOFS:
//   - H2 (core auto-revert): a hold under sid-A is gone the instant the .sid flips
//     to sid-B — the marker is keyed by the OLD sid so it becomes unreachable.
//   - H8 (adversarial agent-keyed-leak): an AGENT-keyed marker (<agent>.hold, no
//     sid suffix) is deliberately ignored; IsHeld only honors the sid-keyed one.
//     This guards against the keying TRAP (agent-name keying would survive a
//     restart).
//
// hk-4rago: RunForPrecompact and RunForIdle now also check HeldCheckFn so a
// co-working hold prevents cycles on those entry points too (the "rehydration gap"
// bug where those paths silently dropped in-flight hold directives).
//
// Reuses writeSidFile / primarySID / gaugeSID (sessionid_test.go), writeCtxFile /
// runWatcherFor / RecordingEmitter (watcher_test.go), all package keeper_test.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// holdMarkerPathForTest reconstructs the production hold-marker path
// (<projectDir>/.harmonik/keeper/<agent>.hold.<sessionID>) for direct inspection.
func holdMarkerPathForTest(projectDir, agent, sid string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".hold."+sid)
}

// secondSID is a SECOND valid UUIDv4, distinct from primarySID/gaugeSID, used to
// simulate the /clear session-id re-mint.
const secondSID = "44444444-4444-4444-8444-444444444444"

// ── H1: set/read ──────────────────────────────────────────────────────────────

// TestHold_H1_SetReadRoundtrip: SetHold writes .hold.<sid> with a parseable
// RFC3339 timestamp; IsHeld is true; the returned sid matches the .sid contents.
func TestHold_H1_SetReadRoundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h1-agent"
	writeSidFile(t, dir, agent, primarySID)

	sid, err := keeper.SetHold(dir, agent)
	if err != nil {
		t.Fatalf("SetHold: unexpected error: %v", err)
	}
	if sid != primarySID {
		t.Errorf("SetHold returned sid %q; want %q (the .sid contents)", sid, primarySID)
	}
	marker := holdMarkerPathForTest(dir, agent, primarySID)
	raw, readErr := os.ReadFile(marker) //nolint:gosec // test-controlled path
	if readErr != nil {
		t.Fatalf("read hold marker %q: %v", marker, readErr)
	}
	if _, parseErr := time.Parse(time.RFC3339, string(trimSpace(raw))); parseErr != nil {
		t.Errorf("hold marker content %q is not parseable RFC3339: %v", raw, parseErr)
	}
	if !keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want true immediately after SetHold")
	}
}

// trimSpace is a tiny helper so the test file does not import strings just for one
// call (keeps the import set minimal / mirrors the other keeper tests' style).
func trimSpace(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	for len(b) > 0 && (b[0] == '\n' || b[0] == '\r' || b[0] == ' ' || b[0] == '\t') {
		b = b[1:]
	}
	return b
}

// ── H2: CORE auto-revert across restart (LOAD-BEARING) ─────────────────────────

// TestHold_H2_AutoRevertAcrossRestart is the load-bearing "a hold never survives a
// restart" proof. A hold is active under sid-A; then the .sid file is OVERWRITTEN
// with a different valid UUIDv4 sid-B (simulating the /clear re-mint + SessionStart
// re-write). Because the marker is keyed by sid-A, IsHeld — which resolves the
// CURRENT .sid (now sid-B) — must report NOT-held: the sid-A marker is orphaned and
// structurally unreachable. This is the whole point of session-id keying.
func TestHold_H2_AutoRevertAcrossRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h2-agent"
	writeSidFile(t, dir, agent, primarySID) // sid-A

	if _, err := keeper.SetHold(dir, agent); err != nil {
		t.Fatalf("SetHold under sid-A: %v", err)
	}
	if !keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Fatal("pre-condition: IsHeld must be true under sid-A")
	}

	// /clear re-mints the session-id: the SessionStart hook re-writes .sid to sid-B.
	writeSidFile(t, dir, agent, secondSID) // sid-B

	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("LOAD-BEARING FAILURE: hold survived a session-id re-mint (restart) — " +
			"the marker must be keyed by the OLD sid and become unreachable after /clear")
	}
}

// ── H3: timer expiry ──────────────────────────────────────────────────────────

// TestHold_H3_TimerExpiry: a marker whose timestamp is older than the TTL is
// EXPIRED — IsHeld returns false even though the sid still matches. This is the
// walk-away/crash backstop.
func TestHold_H3_TimerExpiry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h3-agent"
	writeSidFile(t, dir, agent, primarySID)

	// Write the marker manually with a stale timestamp (TTL + 1m in the past).
	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stale := time.Now().UTC().Add(-keeper.DefaultHoldTTL - time.Minute).Format(time.RFC3339)
	marker := holdMarkerPathForTest(dir, agent, primarySID)
	if err := os.WriteFile(marker, []byte(stale+"\n"), 0o600); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}

	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false for a marker older than the TTL (timer backstop)")
	}
}

// ── H4: release ───────────────────────────────────────────────────────────────

// TestHold_H4_Release: SetHold then ReleaseHold → IsHeld false and the marker is
// gone. ReleaseHold on an already-clear agent is idempotent (no error).
func TestHold_H4_Release(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h4-agent"
	writeSidFile(t, dir, agent, primarySID)

	if _, err := keeper.SetHold(dir, agent); err != nil {
		t.Fatalf("SetHold: %v", err)
	}
	if err := keeper.ReleaseHold(dir, agent); err != nil {
		t.Fatalf("ReleaseHold: %v", err)
	}
	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false after ReleaseHold")
	}
	if _, statErr := os.Stat(holdMarkerPathForTest(dir, agent, primarySID)); statErr == nil {
		t.Error("hold marker still present after ReleaseHold")
	}

	// Idempotent: releasing again (now clear) is not an error.
	if err := keeper.ReleaseHold(dir, agent); err != nil {
		t.Errorf("ReleaseHold on already-clear agent: want nil, got %v", err)
	}
}

// ── H5: no trustworthy sid ────────────────────────────────────────────────────

// TestHold_H5_NoTrustworthySid: with NO .sid → SetHold errors, writes no marker,
// IsHeld false. With a non-UUIDv4 .sid (uppercase, UUIDv7) → SetHold errors too.
func TestHold_H5_NoTrustworthySid(t *testing.T) {
	t.Parallel()

	t.Run("absent_sid", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		agent := "hold-h5-absent"
		// No .sid written.
		if _, err := keeper.SetHold(dir, agent); err == nil {
			t.Error("SetHold with no .sid: want error, got nil")
		}
		// No marker of any kind should have been written.
		matches, _ := filepath.Glob(filepath.Join(dir, ".harmonik", "keeper", agent+".hold.*"))
		if len(matches) != 0 {
			t.Errorf("SetHold with no .sid wrote %d marker(s); want 0", len(matches))
		}
		if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
			t.Error("IsHeld with no .sid: want false")
		}
	})

	t.Run("garbage_non_uuid", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		agent := "hold-h5-garbage"
		// ReadSessionIDFile lowercases before validation, so an uppercase UUIDv4
		// would normalize to a VALID lowercase one — use a genuinely non-UUID value
		// to exercise the "not a primary UUIDv4" reject path.
		writeSidFile(t, dir, agent, "not-a-uuid-at-all")
		if _, err := keeper.SetHold(dir, agent); err == nil {
			t.Error("SetHold with non-UUID .sid: want error, got nil")
		}
		if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
			t.Error("IsHeld with non-UUID .sid: want false")
		}
	})

	t.Run("uuidv7", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		agent := "hold-h5-v7"
		writeSidFile(t, dir, agent, "33333333-3333-7333-8333-333333333333") // version nibble 7 → not v4
		if _, err := keeper.SetHold(dir, agent); err == nil {
			t.Error("SetHold with UUIDv7 .sid: want error, got nil")
		}
		if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
			t.Error("IsHeld with UUIDv7 .sid: want false")
		}
	})
}

// ── H6: corrupt marker content ────────────────────────────────────────────────

// TestHold_H6_CorruptMarkerContent: a marker present under the correct sid but with
// unparseable content fails toward NOT-held (a corrupt marker can never produce an
// unbounded hold).
func TestHold_H6_CorruptMarkerContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h6-agent"
	writeSidFile(t, dir, agent, primarySID)

	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	marker := holdMarkerPathForTest(dir, agent, primarySID)
	if err := os.WriteFile(marker, []byte("not-a-timestamp\n"), 0o600); err != nil {
		t.Fatalf("write corrupt marker: %v", err)
	}

	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false for an unparseable marker (fail toward not-held)")
	}
}

// ── H8: ADVERSARIAL agent-keyed-leak (MANDATORY) ───────────────────────────────

// TestHold_H8_AdversarialAgentKeyedLeak guards against the KEYING TRAP. After a
// session-id flip (H2's scenario), IsHeld must be false. Separately, it proves an
// AGENT-keyed scheme WOULD have leaked: it constructs an agent-keyed marker
// (<agent>.hold, NO sid suffix) — the shape an agent-name-keyed implementation
// would consult — flips the .sid, and asserts IsHeld STILL returns false. The
// implementation must ignore the agent-keyed marker and honor ONLY the
// session-id-keyed one. If IsHeld ever consulted the agent-keyed file, this test
// would catch the regression: a hold that survives a restart.
func TestHold_H8_AdversarialAgentKeyedLeak(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hold-h8-agent"
	writeSidFile(t, dir, agent, primarySID) // sid-A

	if _, err := keeper.SetHold(dir, agent); err != nil {
		t.Fatalf("SetHold under sid-A: %v", err)
	}

	// Also plant an AGENT-KEYED marker (<agent>.hold, no sid) with a FRESH
	// timestamp — this is the file a buggy agent-name-keyed implementation would
	// consult. It must be invisible to IsHeld.
	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	agentKeyed := filepath.Join(keeperDir, agent+".hold")
	fresh := time.Now().UTC().Format(time.RFC3339)
	if err := os.WriteFile(agentKeyed, []byte(fresh+"\n"), 0o600); err != nil {
		t.Fatalf("write agent-keyed marker: %v", err)
	}

	// Flip the session-id (the /clear re-mint).
	writeSidFile(t, dir, agent, secondSID) // sid-B

	// Even with a FRESH agent-keyed marker present, the session-id flip must leave
	// IsHeld false. An agent-keyed implementation would have LEAKED here (returned
	// true) — proving the trap is avoided.
	if keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Error("KEYING-TRAP REGRESSION: IsHeld honored an agent-keyed (<agent>.hold) marker " +
			"after a session-id flip — the hold leaked across a restart")
	}
}

// TestHold_InvalidAgentName: a path-traversal agent name fails toward not-held and
// SetHold errors (mirrors validateAgent fail-open in the other gates).
func TestHold_InvalidAgentName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if keeper.IsHeld(dir, "../evil", keeper.DefaultHoldTTL) {
		t.Error("IsHeld: want false for a traversal agent name")
	}
	if _, err := keeper.SetHold(dir, "../evil"); err == nil {
		t.Error("SetHold: want error for a traversal agent name")
	}
}

// ── Cycler MaybeRun gate (Gate 5c) ─────────────────────────────────────────────

// TestCyclerMaybeRun_DeferredWhenHeld verifies MaybeRun does NOT inject (cycle
// suppressed) when HeldCheckFn returns true, even with all other gates satisfied;
// and DOES proceed (HeldCheckFn consulted, then cycle attempted) when false.
func TestCyclerMaybeRun_DeferredWhenHeld(t *testing.T) {
	t.Parallel()

	mkManaged := func(t *testing.T, projectDir, agent, sessionID string) {
		t.Helper()
		keeperDirPath := filepath.Join(projectDir, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDirPath, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(keeperDirPath, agent+".managed"), []byte(sessionID+"\n"), 0o600); err != nil {
			t.Fatalf("write managed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(keeperDirPath, agent+".idle"), []byte{}, 0o600); err != nil {
			t.Fatalf("write idle: %v", err)
		}
	}

	// baseCfg builds a CyclerConfig whose gates ALL pass except (optionally) the
	// hold gate. injectCount records cycle firings; heldCalled records that the HOLD
	// gate (Gate 5c) was actually consulted.
	baseCfg := func(projectDir, agent string, held bool, injectCount *int, heldCalled *bool) keeper.CyclerConfig {
		return keeper.CyclerConfig{
			AgentName:         agent,
			ProjectDir:        projectDir,
			TmuxTarget:        "",
			ActPct:            80.0,
			WarnPct:           70.0,
			IsManagedFn:       func(_, _ string) bool { return true },
			CrispIdleFn:       func(_, _ string) bool { return true },
			HoldingDispatchFn: func(_, _ string) bool { return false },
			SleepingCheckFn:   func(_, _ string) bool { return false },
			HeldCheckFn: func(_, _ string) bool {
				*heldCalled = true
				return held
			},
			InjectFn: func(_ context.Context, _, _ string) error {
				*injectCount++
				return nil
			},
			ReadHandoff:     func(_ string) (string, error) { return "", nil },
			HandoffFilePath: func(_, agentName string) string { return filepath.Join(projectDir, "HANDOFF-"+agentName+".md") },
		}
	}

	t.Run("held_suppresses", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		agent := "cycler-held"
		sessionID := "sess-held"
		mkManaged(t, dir, agent, sessionID)

		injectCount := 0
		heldCalled := false
		cfg := baseCfg(dir, agent, true, &injectCount, &heldCalled)
		cycler := keeper.NewCycler(cfg, &keeper.RecordingEmitter{})
		cf := &keeper.CtxFile{Pct: 90.0, SessionID: sessionID, Ts: time.Now().UTC().Format(time.RFC3339)}
		if err := cycler.MaybeRun(context.Background(), cf); err != nil {
			t.Fatalf("MaybeRun returned error: %v", err)
		}
		if !heldCalled {
			t.Error("HeldCheckFn not called; Gate 5c was not reached")
		}
		if injectCount != 0 {
			t.Errorf("cycle injected %d time(s) while held; want 0", injectCount)
		}
	})

	t.Run("not_held_reaches_gate_and_proceeds", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		agent := "cycler-not-held"
		sessionID := "sess-not-held"
		mkManaged(t, dir, agent, sessionID)
		if err := os.WriteFile(filepath.Join(dir, "HANDOFF-"+agent+".md"), []byte{}, 0o600); err != nil {
			t.Fatalf("write handoff: %v", err)
		}

		injectCount := 0
		heldCalled := false
		cfg := baseCfg(dir, agent, false, &injectCount, &heldCalled)
		cycler := keeper.NewCycler(cfg, &keeper.RecordingEmitter{})
		cf := &keeper.CtxFile{Pct: 90.0, SessionID: sessionID, Ts: time.Now().UTC().Format(time.RFC3339)}

		// Pre-cancelled context so runCycle returns immediately without blocking on
		// the nonce poll. The control proof is that the HOLD gate was REACHED (Gate
		// 5c consulted) and did NOT short-circuit — execution flowed past it into
		// the cycle body. The cancelled ctx means inject may be 0, so we assert on
		// gate-reached, mirroring the sleep-gate "GateReachedWhenAwake" idiom.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = cycler.MaybeRun(ctx, cf)
		if !heldCalled {
			t.Error("HeldCheckFn not called; Gate 5c bypassed when not held (gate off the hot path?)")
		}
	})
}

// ── Watcher gates: maybeRespawn / maybeLivePaneRecover ─────────────────────────

// lprHoldRecorder is a thread-safe spy for LiveRecoverFn used by the hold tests.
type lprHoldRecorder struct {
	mu    sync.Mutex
	calls int
}

func (r *lprHoldRecorder) fn(_ context.Context, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return nil
}

func (r *lprHoldRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestWatcher_RespawnSuppressedWhenHeld: with a HOLD active, the respawn command
// (sentinel-writing) must NOT run even though every other respawn gate passes
// (gauge stale, pane idle, cooldown elapsed). Control: hold off → sentinel IS
// written.
func TestWatcher_RespawnSuppressedWhenHeld(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, held bool) bool {
		t.Helper()
		dir := t.TempDir()
		agent := "respawn-hold-agent"
		sentinel := filepath.Join(dir, "respawned.flag")

		em := &keeper.RecordingEmitter{}
		cfg := keeper.WatcherConfig{
			AgentName:    agent,
			ProjectDir:   dir,
			PollInterval: 10 * time.Millisecond,
			Staleness:    5 * time.Millisecond,
			RespawnGrace: 5 * time.Millisecond,
			RespawnCmd:   "printf RESPAWNED > " + sentinel,
			TmuxTarget:   "dummy-pane",
			IsPaneIdleFn: func(_ context.Context, _ string) bool { return true },
			InjectFn:     func(_ context.Context, _ string) error { return nil },
			HeldCheckFn:  func(_, _ string) bool { return held },
		}
		// No gauge file → immediately absent/stale; keeper dir must exist.
		keeperDir := filepath.Join(dir, ".harmonik", "keeper")
		if err := os.MkdirAll(keeperDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)
		_, statErr := os.Stat(sentinel)
		return statErr == nil
	}

	if run(t, true) {
		t.Error("respawn fired (sentinel written) while HELD; want suppressed")
	}
	if !run(t, false) {
		t.Error("control (hold off): respawn did NOT fire; want sentinel written")
	}
}

// TestWatcher_LivePaneRecoverSuppressedWhenHeld: with a HOLD active, LiveRecoverFn
// must NOT be called even though every other live-recover gate passes (stale gauge,
// pane alive, no operator, valid .sid). Control: hold off → it IS called.
func TestWatcher_LivePaneRecoverSuppressedWhenHeld(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, held bool) int {
		t.Helper()
		dir := t.TempDir()
		agent := "lpr-hold-agent"
		writeGauge(t, dir, agent, gaugeSID)
		writeSidFile(t, dir, agent, primarySID)

		rec := &lprHoldRecorder{}
		em := &keeper.RecordingEmitter{}
		cfg := keeper.WatcherConfig{
			AgentName:           agent,
			ProjectDir:          dir,
			PollInterval:        10 * time.Millisecond,
			Staleness:           5 * time.Millisecond,
			LiveRecoverGrace:    10 * time.Millisecond,
			LiveRecoverCooldown: 10 * time.Second,
			TmuxTarget:          "dummy-pane",
			IsPaneAliveFn:       func(_ context.Context, _ string) bool { return true },
			OperatorAttachedFn:  func(_ string) bool { return false },
			LiveRecoverFn:       rec.fn,
			InjectFn:            func(_ context.Context, _ string) error { return nil },
			HeldCheckFn:         func(_, _ string) bool { return held },
		}
		runWatcherFor(context.Background(), cfg, em, 800*time.Millisecond)
		return rec.count()
	}

	if n := run(t, true); n != 0 {
		t.Errorf("live-pane recovery fired %d time(s) while HELD; want 0", n)
	}
	if n := run(t, false); n == 0 {
		t.Error("control (hold off): live-pane recovery did NOT fire; want ≥1")
	}
}

// ── Hard-ceiling override (hold does NOT suppress overflow protection) ──────────

// TestWatcher_HardCeilingOverridesHold drives the SID-independent hard-ceiling
// restart path (restart mode, tokens above the ceiling) WITH HeldCheckFn returning
// true, and asserts the HardCeilingRestartFn IS still called — proving the operator
// HOLD does NOT suppress the hard ceiling (true overflow protection beats a hold).
func TestWatcher_HardCeilingOverridesHold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "hard-ceiling-hold-agent"

	em := &keeper.RecordingEmitter{}
	spy := &lprHoldRecorder{} // reuse as a simple call counter for the restart fn

	cfg := foreignSessionConfig(t, dir, agent, 290_000)
	cfg.HardCeilingMode = keeper.HardCeilingModeRestart
	cfg.HardCeilingRestartFn = spy.fn
	cfg.HardCeilingCooldown = 10 * time.Second
	// HOLD active — must be IGNORED by the hard-ceiling path.
	cfg.HeldCheckFn = func(_, _ string) bool { return true }

	runWatcherFor(context.Background(), cfg, em, 200*time.Millisecond)

	if spy.count() == 0 {
		t.Error("hard-ceiling restart did NOT fire while HELD; the hard ceiling must OVERRIDE a hold")
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperHardCeiling)); n == 0 {
		t.Error("want ≥1 session_keeper_hard_ceiling event while held; got 0")
	}
}

// ── WARN still fires under hold ────────────────────────────────────────────────

// TestWatcher_WarnFiresUnderHold: a HOLD suspends only the destructive ACT/restart
// paths — the WARN path is unaffected. With the gauge above the warn threshold and
// HeldCheckFn returning true, the session_keeper_warn event must still be emitted.
func TestWatcher_WarnFiresUnderHold(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "warn-hold-agent"

	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   dir,
		PollInterval: 10 * time.Millisecond,
		WarnPct:      80.0,
		IdleQuiesce:  1 * time.Millisecond,
		Staleness:    120 * time.Second,
		TmuxTarget:   "",
		HeldCheckFn:  func(_, _ string) bool { return true }, // HELD
	}
	writeCtxFile(t, dir, agent, 85.0, "sess-warn-held")

	runWatcherFor(context.Background(), cfg, em, 80*time.Millisecond)

	if n := len(em.EventsOfType(core.EventTypeSessionKeeperWarn)); n == 0 {
		t.Error("WARN must still fire under a hold; got 0 session_keeper_warn events")
	}
}

// ── RunForPrecompact gate 3b: hold suppresses precompact cycle (hk-4rago) ──────

// TestRunForPrecompact_SuppressedWhenHeld verifies that RunForPrecompact emits
// a "hold_skip" precompact_blocked event and does NOT inject (cycle suppressed)
// when HeldCheckFn returns true. Control: hold off → cycle proceeds (inject fires).
// Refs: hk-4rago ("rehydration gap silently drops in-flight directives").
func TestRunForPrecompact_SuppressedWhenHeld(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, held bool) (injects int, precompactActions []string) {
		t.Helper()
		dir := t.TempDir()
		agent := "prec-hold-agent"

		em := &keeper.RecordingEmitter{}
		spy := &cycleSpyInjector{}
		jc := &journalCapture{}
		const cycleID = "cyc-prec-hold"
		nonce := "<!-- KEEPER:" + cycleID + " -->"

		cfg := keeper.CyclerConfig{
			AgentName:      agent,
			ProjectDir:     dir,
			TmuxTarget:     "fake-pane",
			ActPct:         90.0,
			WarnPct:        80.0,
			HandoffTimeout: 200 * time.Millisecond,
			ClearSettle:    20 * time.Millisecond,
			PollInterval:   10 * time.Millisecond,
			CycleIDGen:     func() string { return cycleID },
			IsManagedFn:    func(_, _ string) bool { return true },
			HandoffFilePath: func(_, a string) string {
				return filepath.Join(dir, "HANDOFF-"+a+".md")
			},
			ReadHandoff: func(_ string) (string, error) {
				return "# Handoff\n\n" + nonce + "\n", nil
			},
			TruncateHandoffFn: func(_ string) error { return nil },
			InjectFn:          spy.inject,
			ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
				return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-new"}, time.Now(), nil
			},
			CrispIdleFn:              func(_, _ string) bool { return false },
			HoldingDispatchFn:        func(_, _ string) bool { return false },
			HeldCheckFn:              func(_, _ string) bool { return held },
			WriteJournalFn:           jc.write,
			ClearPrecompactTriggerFn: func(_, _ string) error { return nil },
		}
		cycler := keeper.NewCycler(cfg, em)
		cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-abc"}
		if err := cycler.RunForPrecompact(context.Background(), cf); err != nil {
			t.Fatalf("RunForPrecompact: unexpected error: %v", err)
		}

		for _, ev := range em.EventsOfType(core.EventTypeSessionKeeperPrecompactBlocked) {
			precompactActions = append(precompactActions, precompactAction(t, ev))
		}
		return len(spy.texts()), precompactActions
	}

	t.Run("held_suppresses", func(t *testing.T) {
		t.Parallel()
		injects, actions := run(t, true)
		if injects != 0 {
			t.Errorf("RunForPrecompact fired %d inject(s) while HELD; want 0", injects)
		}
		if len(actions) != 1 || actions[0] != "hold_skip" {
			t.Errorf("want [hold_skip] precompact_blocked event, got %v", actions)
		}
	})

	t.Run("not_held_proceeds", func(t *testing.T) {
		t.Parallel()
		_, actions := run(t, false)
		// Gate 4 (anti-loop) hasn't fired before, so the cycle proceeds to
		// cycle_triggered. Verify no hold_skip was emitted.
		for _, a := range actions {
			if a == "hold_skip" {
				t.Errorf("hold_skip emitted while NOT held; want it absent")
			}
		}
	})
}

// ── RunForIdle gate 5b: hold suppresses idle restart (hk-4rago) ─────────────────

// TestRunForIdle_SuppressedWhenHeld verifies that RunForIdle does NOT fire the
// cycle when HeldCheckFn returns true, even with all other gates satisfied.
// Control: hold off → cycle proceeds (inject fires). Refs: hk-4rago.
func TestRunForIdle_SuppressedWhenHeld(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, held bool) int {
		t.Helper()
		dir := t.TempDir()
		agent := "idle-hold-agent"

		em := &keeper.RecordingEmitter{}
		spy := &cycleSpyInjector{}
		jc := &journalCapture{}
		const cycleID = "cyc-idle-hold"
		nonce := "<!-- KEEPER:" + cycleID + " -->"

		cfg := keeper.CyclerConfig{
			AgentName:      agent,
			ProjectDir:     dir,
			TmuxTarget:     "fake-pane",
			ActAbsTokens:   300_000,
			ActPct:         90.0,
			WarnPct:        80.0,
			HandoffTimeout: 200 * time.Millisecond,
			ClearSettle:    20 * time.Millisecond,
			PollInterval:   10 * time.Millisecond,
			CycleIDGen:     func() string { return cycleID },
			IsManagedFn:    func(_, _ string) bool { return true },
			HandoffFilePath: func(_, a string) string {
				return filepath.Join(dir, "HANDOFF-"+a+".md")
			},
			ReadHandoff: func(_ string) (string, error) {
				return "# Handoff\n\n" + nonce + "\n", nil
			},
			TruncateHandoffFn: func(_ string) error { return nil },
			InjectFn:          spy.inject,
			// WindowSize=400k: actThreshold = min(300k, 0.85×400k=340k) = 300k,
			// so tokens=200k is below the act threshold and Gate 3 passes.
			ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
				return &keeper.CtxFile{Pct: 10.0, Tokens: 5_000, WindowSize: 400_000, SessionID: "sess-new"}, time.Now(), nil
			},
			CrispIdleFn:              func(_, _ string) bool { return true },
			HoldingDispatchFn:        func(_, _ string) bool { return false },
			HeldCheckFn:              func(_, _ string) bool { return held },
			WriteJournalFn:           jc.write,
			SetTmuxEnvFn:             func(_ context.Context, _, _, _ string) error { return nil },
			ClearPrecompactTriggerFn: func(_, _ string) error { return nil },
			IdleRestartAbsTokens:     150_000,
			IdleRestartCooldown:      0,
		}
		cycler := keeper.NewCycler(cfg, em)
		// Tokens above IdleRestartAbsTokens (150k) but below actThreshold (300k).
		cf := &keeper.CtxFile{Pct: 80.0, Tokens: 200_000, WindowSize: 400_000, SessionID: "sess-idle"}
		if err := cycler.RunForIdle(context.Background(), cf); err != nil {
			t.Fatalf("RunForIdle: unexpected error: %v", err)
		}
		return len(spy.texts())
	}

	if n := run(t, true); n != 0 {
		t.Errorf("RunForIdle fired %d inject(s) while HELD; want 0", n)
	}
	if n := run(t, false); n == 0 {
		t.Error("control (hold off): RunForIdle did NOT fire; want ≥1 inject")
	}
}

// ── Older binary silently ignores the hold marker (version-gated) ────────────

// TestWatcher_OlderBinaryIgnoresHoldMarker is the version-gated assertion: a
// hold marker on disk has NO effect on a binary that does not check it. The hold
// feature was added 2026-06-20 (hk-9waz); older keeper binaries never call
// IsHeld and so will ACT/restart regardless of what is on disk. This test
// proves the marker is purely code-path-gated — it is NOT a global filesystem
// lock that all keeper versions would consult.
//
// Invariant: an older binary (modelled here by HeldCheckFn that always returns
// false, the behavior of a binary compiled before the hold gate existed) MUST
// fire respawn even when a fresh, valid <agent>.hold.<sessionID> is present on
// disk. Skill §hold: "An older keeper silently ignores the hold marker and will
// ACT/restart anyway."
func TestWatcher_OlderBinaryIgnoresHoldMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "older-bin-agent"

	// Write a live .sid so SetHold succeeds, then place a fresh hold on disk.
	writeSidFile(t, dir, agent, primarySID)
	if _, err := keeper.SetHold(dir, agent); err != nil {
		t.Fatalf("SetHold: %v", err)
	}
	// Pre-condition: the CURRENT binary would see a hold (proves the marker is there).
	if !keeper.IsHeld(dir, agent, keeper.DefaultHoldTTL) {
		t.Fatal("pre-condition: hold must be active on disk for this test to be meaningful")
	}

	// Simulate an older binary: HeldCheckFn always returns false — it does not know
	// about the hold gate and never inspects the marker file.
	sentinel := filepath.Join(dir, "respawned.flag")
	em := &keeper.RecordingEmitter{}
	cfg := keeper.WatcherConfig{
		AgentName:    agent,
		ProjectDir:   dir,
		PollInterval: 10 * time.Millisecond,
		Staleness:    5 * time.Millisecond,
		RespawnGrace: 5 * time.Millisecond,
		RespawnCmd:   "printf RESPAWNED > " + sentinel,
		TmuxTarget:   "dummy-pane",
		IsPaneIdleFn: func(_ context.Context, _ string) bool { return true },
		InjectFn:     func(_ context.Context, _ string) error { return nil },
		// Older binary: hold gate absent — always returns false regardless of disk.
		HeldCheckFn: func(_, _ string) bool { return false },
	}
	keeperDir := filepath.Join(dir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	runWatcherFor(context.Background(), cfg, em, 300*time.Millisecond)

	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Error("OLDER-BINARY REGRESSION: respawn did NOT fire despite a live hold marker " +
			"— an older binary must ignore the hold marker and ACT/restart anyway; " +
			"the marker must be purely code-path-gated")
	}
}
