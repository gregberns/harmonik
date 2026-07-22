package daemon

// keeperrevive_hk220lv_test.go — unit tests for KeeperReviveWatcher, the
// daemon-hosted periodic sweep that detects a silently-dead crew keeper watcher
// (its exclusive flock is dropped by the kernel with no event, no mtime change)
// and re-arms the crew's keeper window.
//
// Tests cover:
//   - Live watcher across many ticks past Grace → zero re-arms, zero events
//     (the sweep must not churn healthy crews).
//   - Dead watcher: the FIRST scan only ARMS the grace clock; only a later scan
//     past Grace revives, emitting session_keeper_watcher_dead AND
//     session_keeper_watcher_revived exactly once each.
//   - Unmanaged agent (no .managed marker) → never revived, even dead past Grace.
//   - Confirmed-alive resets the attempt counter, so MaxAttempts is a
//     per-dead-episode budget, not a lifetime one.
//   - Permanently dead → re-arms capped at MaxAttempts and a keeper-alert comms
//     escalation fires exactly ONCE (not once per scan).
//   - The tmux session is derived from crew.Record.Handle by trimming the
//     ":agent" window suffix; an empty Handle yields no re-arm and no panic.
//   - An ABSENT config resolves to the compiled defaults with the sweep ENABLED
//     (a silently-disabled-by-default safety net is the bug this bead fixes).
//
// Helper prefix: kr
//
// Bead ref: hk-220lv.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (prefix: kr)
// ─────────────────────────────────────────────────────────────────────────────

// krReArm records one ReArmFn invocation.
type krReArm struct {
	crewName string
	session  string
}

// krHarness drives a KeeperReviveWatcher with every external dependency stubbed:
// an injected clock, an injected crew list, injected .managed / flock probes, and
// a recording re-arm seam. Reuses the package's existing stubEventBus /
// stubCommsBus (crewstart_keeper_probe_hkqgfme_test.go).
type krHarness struct {
	mu       sync.Mutex
	now      time.Time
	alive    bool
	managed  bool
	records  []crew.Record
	reArms   []krReArm
	reArmErr error

	events *stubEventBus
	comms  *stubCommsBus
	w      *KeeperReviveWatcher
}

// krNewHarness builds a harness whose crew registry contains records, with the
// keeper flock reading DEAD and the agent .managed by default.
func krNewHarness(t *testing.T, records []crew.Record, grace time.Duration, maxAttempts int) *krHarness {
	t.Helper()
	h := &krHarness{
		now:     time.Unix(1_700_000_000, 0).UTC(),
		alive:   false,
		managed: true,
		records: records,
		events:  &stubEventBus{},
		comms:   &stubCommsBus{},
	}
	h.w = NewKeeperReviveWatcher(KeeperReviveWatcherConfig{
		ProjectDir:  "/nonexistent/project",
		Grace:       grace,
		MaxAttempts: maxAttempts,
		ListCrews: func(string) ([]crew.Record, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			out := make([]crew.Record, len(h.records))
			copy(out, h.records)
			return out, nil
		},
		IsManagedFn: func(string, string) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.managed
		},
		LiveKeeperFn: func(string, string) bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.alive
		},
		ReArmFn: func(_ context.Context, crewName, session string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.reArms = append(h.reArms, krReArm{crewName: crewName, session: session})
			return h.reArmErr
		},
		Emit:  h.events,
		Comms: h.comms,
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.now
		},
	})
	return h
}

// krScan drives exactly one deterministic sweep.
func (h *krHarness) krScan() { h.w.scan(context.Background()) }

// krAdvance moves the injected clock forward.
func (h *krHarness) krAdvance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

// krSetAlive flips the injected flock-liveness probe.
func (h *krHarness) krSetAlive(alive bool) {
	h.mu.Lock()
	h.alive = alive
	h.mu.Unlock()
}

// krSetManaged flips the injected .managed opt-in probe.
func (h *krHarness) krSetManaged(managed bool) {
	h.mu.Lock()
	h.managed = managed
	h.mu.Unlock()
}

// krReArms returns a copy of the recorded re-arm calls.
func (h *krHarness) krReArms() []krReArm {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]krReArm, len(h.reArms))
	copy(out, h.reArms)
	return out
}

// krCountEvents counts emissions of a single event type.
func (h *krHarness) krCountEvents(et core.EventType) int {
	n := 0
	for _, got := range h.events.emitted() {
		if got == et {
			n++
		}
	}
	return n
}

// krCountComms counts comms messages on a topic.
func (h *krHarness) krCountComms(topic string) int {
	n := 0
	for _, m := range h.comms.sent() {
		if m.Topic == topic {
			n++
		}
	}
	return n
}

// krRecord builds a minimal crew registry record for the crew under test. Only
// the Handle varies across cases (it is what the tmux session is derived from),
// so the name is fixed here rather than threaded through every call site.
func krRecord(handle string) crew.Record {
	return crew.Record{SchemaVersion: 1, Name: krCrewName, Handle: handle}
}

// krCrewName is the single crew every case in this file registers.
const krCrewName = "kilo"

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestKeeperRevive_LiveWatcher_NeverRevives_hk220lv: a crew whose keeper flock is
// held stays untouched no matter how many ticks elapse past Grace.
func TestKeeperRevive_LiveWatcher_NeverRevives_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second
	h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, 3)
	h.krSetAlive(true)

	for i := 0; i < 10; i++ {
		h.krScan()
		h.krAdvance(grace * 2)
	}

	if got := len(h.krReArms()); got != 0 {
		t.Errorf("re-arms = %d; want 0 (regression: the sweep is churning a HEALTHY crew — "+
			"re-arming a live keeper spawns a second watcher that cannot take the flock)", got)
	}
	if got := len(h.events.emitted()); got != 0 {
		t.Errorf("events = %d; want 0 (regression: a live keeper is emitting watcher_dead/watcher_revived noise, "+
			"which trains the operator to ignore the alarm)", got)
	}
	if got := len(h.comms.sent()); got != 0 {
		t.Errorf("comms = %d; want 0 (regression: a healthy crew is paging the operator)", got)
	}
}

// TestKeeperRevive_DeadPastGrace_RevivesOnceWithBothEvents_hk220lv: the first
// scan that observes a dead watcher only ARMS the grace clock; a scan after Grace
// has elapsed revives exactly once and emits both the diagnosis and the
// remediation event.
func TestKeeperRevive_DeadPastGrace_RevivesOnceWithBothEvents_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second
	h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, 3)

	// Scan 1: dead, but this is the first observation — grace only ARMS.
	h.krScan()
	if got := len(h.krReArms()); got != 0 {
		t.Fatalf("re-arms after first scan = %d; want 0 (regression: reviving on the FIRST dead observation "+
			"kills a keeper that is merely mid-restart, since a restarting keeper drops and re-takes its flock)", got)
	}
	if got := len(h.events.emitted()); got != 0 {
		t.Fatalf("events after first scan = %d; want 0 (regression: the grace window is not being honoured)", got)
	}

	// Scan 2: still dead, now past Grace → revive.
	h.krAdvance(grace + time.Second)
	h.krScan()

	reArms := h.krReArms()
	if len(reArms) != 1 {
		t.Fatalf("re-arms after grace elapsed = %d; want 1 (regression: a genuinely dead keeper is NOT being "+
			"re-armed — this is the 43h-unmonitored-crew bug)", len(reArms))
	}
	if reArms[0].crewName != krCrewName {
		t.Errorf("re-armed crew = %q; want %q (regression: the sweep re-armed the wrong crew)", reArms[0].crewName, krCrewName)
	}
	if got := h.krCountEvents(core.EventTypeSessionKeeperWatcherDead); got != 1 {
		t.Errorf("session_keeper_watcher_dead count = %d; want 1 (regression: the diagnosis is not durable, "+
			"so a post-mortem cannot tell that the keeper died mid-life)", got)
	}
	if got := h.krCountEvents(core.EventTypeSessionKeeperWatcherRevived); got != 1 {
		t.Errorf("session_keeper_watcher_revived count = %d; want 1 (regression: the self-heal leaves no audit "+
			"trail, so a revive loop is invisible)", got)
	}
}

// TestKeeperRevive_UnmanagedAgent_NeverRevives_hk220lv: an agent without the
// .managed opt-in marker is deliberately keeper-less and must never be revived.
func TestKeeperRevive_UnmanagedAgent_NeverRevives_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second
	h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, 3)
	h.krSetManaged(false) // no .managed marker
	h.krSetAlive(false)   // and no live keeper — the revive-eligible shape but for the gate

	for i := 0; i < 5; i++ {
		h.krScan()
		h.krAdvance(grace * 2)
	}

	if got := len(h.krReArms()); got != 0 {
		t.Errorf("re-arms = %d; want 0 (regression: reviving an unmanaged agent resurrects a deliberately "+
			"keeper-less crew, fighting the operator's opt-out)", got)
	}
	if got := len(h.events.emitted()); got != 0 {
		t.Errorf("events = %d; want 0 (regression: an unmanaged agent is being reported as a dead watcher)", got)
	}
}

// TestKeeperRevive_ConfirmedAlive_ResetsAttemptCounter_hk220lv: with MaxAttempts=1,
// a dead→revive→alive→dead→revive sequence yields TWO re-arms, because observing a
// live flock resets the per-episode attempt budget.
func TestKeeperRevive_ConfirmedAlive_ResetsAttemptCounter_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second
	h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, 1)

	// Episode 1: dead → arm → revive.
	h.krScan()
	h.krAdvance(grace + time.Second)
	h.krScan()
	if got := len(h.krReArms()); got != 1 {
		t.Fatalf("re-arms after first episode = %d; want 1", got)
	}

	// Recovery: the re-armed keeper takes its flock.
	h.krSetAlive(true)
	h.krAdvance(grace)
	h.krScan()

	// Episode 2: it dies again later.
	h.krSetAlive(false)
	h.krAdvance(grace)
	h.krScan() // arms
	h.krAdvance(grace + time.Second)
	h.krScan() // revives

	if got := len(h.krReArms()); got != 2 {
		t.Errorf("re-arms across two episodes = %d; want 2 (regression: the attempt counter is NOT reset on a "+
			"confirmed-alive flock, so MaxAttempts becomes a LIFETIME budget and a long-lived crew permanently "+
			"loses its keeper after the cap)", got)
	}
}

// TestKeeperRevive_PermanentlyDead_CapsAttemptsAndAlertsOnce_hk220lv: a crew whose
// keeper never comes back is re-armed at most MaxAttempts times, and the operator
// escalation fires exactly once — not on every subsequent scan.
func TestKeeperRevive_PermanentlyDead_CapsAttemptsAndAlertsOnce_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second
	const maxAttempts = 2
	h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, maxAttempts)

	for i := 0; i < 12; i++ {
		h.krScan()
		h.krAdvance(grace + time.Second)
	}

	if got := len(h.krReArms()); got != maxAttempts {
		t.Errorf("re-arms = %d; want %d (regression: an unrevivable crew is being re-armed forever, spawning a "+
			"tmux window per scan)", got, maxAttempts)
	}
	if got := h.krCountComms("keeper-alert"); got != 1 {
		t.Errorf("keeper-alert comms = %d; want exactly 1 (regression: 0 means the give-up escalation never reaches "+
			"the operator and a permanently unmonitored crew stays silent; >1 means the escalation repeats every "+
			"scan and the operator learns to mute keeper-alert)", got)
	}
	if got := h.krCountEvents(core.EventTypeSessionKeeperWatcherRevived); got != maxAttempts {
		t.Errorf("session_keeper_watcher_revived count = %d; want %d (one per successful re-arm)", got, maxAttempts)
	}
}

// TestKeeperRevive_SessionFromHandle_TrimsAgentWindowSuffix_hk220lv: the tmux
// session passed to the re-arm seam is the crew registry Handle with its ":agent"
// window suffix trimmed; a record with no Handle is skipped rather than re-armed
// into an empty session.
func TestKeeperRevive_SessionFromHandle_TrimsAgentWindowSuffix_hk220lv(t *testing.T) {
	t.Parallel()

	grace := 90 * time.Second

	t.Run("handle_with_agent_suffix", func(t *testing.T) {
		t.Parallel()
		h := krNewHarness(t, []crew.Record{krRecord("harmonik-abc123-crew-kilo:agent")}, grace, 3)
		h.krScan()
		h.krAdvance(grace + time.Second)
		h.krScan()

		reArms := h.krReArms()
		if len(reArms) != 1 {
			t.Fatalf("re-arms = %d; want 1", len(reArms))
		}
		const wantSession = "harmonik-abc123-crew-kilo"
		if reArms[0].session != wantSession {
			t.Errorf("re-arm session = %q; want %q (regression: passing the full window handle as a SESSION name "+
				"makes tmux new-window target a session that does not exist, so the re-arm silently no-ops)",
				reArms[0].session, wantSession)
		}
	})

	t.Run("empty_handle", func(t *testing.T) {
		t.Parallel()
		h := krNewHarness(t, []crew.Record{krRecord("")}, grace, 3)
		h.krScan()
		h.krAdvance(grace + time.Second)
		h.krScan()
		h.krAdvance(grace + time.Second)
		h.krScan()

		if got := len(h.krReArms()); got != 0 {
			t.Errorf("re-arms = %d; want 0 (regression: a record with no tmux handle is being re-armed into an "+
				"EMPTY session name, which either errors every scan or creates a stray session)", got)
		}
		if got := h.krCountEvents(core.EventTypeSessionKeeperWatcherRevived); got != 0 {
			t.Errorf("session_keeper_watcher_revived count = %d; want 0 (regression: claiming a revive that never "+
				"happened)", got)
		}
		if got := h.krCountComms("keeper-alert"); got != 1 {
			t.Errorf("keeper-alert comms = %d; want 1 (an un-revivable record must escalate exactly once)", got)
		}
	})
}

// TestKeeperRevive_AbsentConfig_DefaultsOnAndEnabled_hk220lv: a zero-valued config
// (the production case — none of the keeper.timings.revive_* keys are set) resolves
// to the compiled defaults with the sweep ENABLED. A safety net that is silently
// disabled by default is the exact failure class this bead closes.
func TestKeeperRevive_AbsentConfig_DefaultsOnAndEnabled_hk220lv(t *testing.T) {
	t.Parallel()

	w := NewKeeperReviveWatcher(KeeperReviveWatcherConfig{ProjectDir: "/nonexistent/project"})

	if w.cfg.Disabled {
		t.Error("Disabled = true for an absent config; want false (regression: the keeper self-heal is " +
			"disabled-by-default, exactly like the flock_acquire_grace probe that failed to fire in production)")
	}
	if w.cfg.ScanInterval != keeperReviveDefaultScanInterval {
		t.Errorf("ScanInterval = %s; want %s (compiled default)", w.cfg.ScanInterval, keeperReviveDefaultScanInterval)
	}
	if w.cfg.Grace != keeperReviveDefaultGrace {
		t.Errorf("Grace = %s; want %s (compiled default)", w.cfg.Grace, keeperReviveDefaultGrace)
	}
	if w.cfg.MaxAttempts != keeperReviveDefaultMaxAttempts {
		t.Errorf("MaxAttempts = %d; want %d (compiled default)", w.cfg.MaxAttempts, keeperReviveDefaultMaxAttempts)
	}
	if w.cfg.ListCrews == nil || w.cfg.IsManagedFn == nil || w.cfg.LiveKeeperFn == nil {
		t.Error("nil production seam after construction; want crew.List / keeper.IsManaged / keeper.LiveKeeperPresent " +
			"(regression: the sweep would no-op in production while looking wired)")
	}
}
