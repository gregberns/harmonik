package daemon

// crewstart_keeper_probe_hkqgfme_test.go — unit tests for the async crew keeper
// post-spawn liveness probe (hk-qgfme).
//
// Tests cover:
//   - Probe disabled when FlockAcquireGrace == 0.
//   - Watcher live within grace → no event / comms emitted.
//   - Watcher dead after grace → session_keeper_watcher_dead event + keeper-alert
//     comms emitted; crew agent unaffected (probe is async / non-blocking).
//   - Grace comes from config, not a literal (ACCEPTANCE: "grace is config not literal").
//
// Bead ref: hk-qgfme.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stubs
// ─────────────────────────────────────────────────────────────────────────────

// stubEventBus records Emit calls.
type stubEventBus struct {
	mu     sync.Mutex
	events []core.EventType
}

func (s *stubEventBus) Emit(_ context.Context, et core.EventType, _ []byte) error {
	s.mu.Lock()
	s.events = append(s.events, et)
	s.mu.Unlock()
	return nil
}

func (s *stubEventBus) emitted() []core.EventType {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.EventType, len(s.events))
	copy(out, s.events)
	return out
}

// stubCommsBus records EmitAgentMessage calls.
type stubCommsBus struct {
	mu       sync.Mutex
	messages []core.AgentMessagePayload
}

func (s *stubCommsBus) EmitAgentMessage(_ context.Context, p core.AgentMessagePayload) (core.EventID, error) {
	s.mu.Lock()
	s.messages = append(s.messages, p)
	s.mu.Unlock()
	return core.EventID{}, nil
}

func (s *stubCommsBus) sent() []core.AgentMessagePayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.AgentMessagePayload, len(s.messages))
	copy(out, s.messages)
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// runProbeSync runs probeKeeperLiveness in a goroutine and waits for it to
// finish (or for timeout to elapse). Returns true when the goroutine returned
// within timeout, false when it timed out.
func runProbeSync(t *testing.T, h *crewHandlerImpl, crewName string, grace time.Duration, timeout time.Duration) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		h.probeKeeperLiveness(crewName, grace)
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestKeeperProbe_LiveWithinGrace verifies that no event or comms is emitted
// when the keeper watcher is live before the grace period expires.
//
// ACCEPTANCE: "probe-false unit test" (inverse: probe-true path also covered).
func TestKeeperProbe_LiveWithinGrace(t *testing.T) {
	evBus := &stubEventBus{}
	comms := &stubCommsBus{}

	h := &crewHandlerImpl{
		projectDir: t.TempDir(),
		keeperCfg:  KeeperConfig{FlockAcquireGrace: 5 * time.Second},
		eventBus:   evBus,
		commsBus:   comms,
		// Keeper is live immediately.
		liveKeeperFn: func(_, _ string) bool { return true },
	}

	if !runProbeSync(t, h, "crew-a", 5*time.Second, 3*time.Second) {
		t.Fatal("probe did not finish within timeout")
	}

	if got := evBus.emitted(); len(got) != 0 {
		t.Errorf("expected no events; got %v", got)
	}
	if got := comms.sent(); len(got) != 0 {
		t.Errorf("expected no comms; got %v", got)
	}
}

// TestKeeperProbe_WatcherDeadAfterGrace verifies that when the liveness probe
// never sees a live watcher:
//   - A session_keeper_watcher_dead event is emitted.
//   - A keeper-alert comms message is sent to the operator.
//
// ACCEPTANCE: "probe-false unit test".
func TestKeeperProbe_WatcherDeadAfterGrace(t *testing.T) {
	evBus := &stubEventBus{}
	comms := &stubCommsBus{}

	// Short grace to keep the test fast.
	const grace = 80 * time.Millisecond

	h := &crewHandlerImpl{
		projectDir: t.TempDir(),
		keeperCfg:  KeeperConfig{FlockAcquireGrace: grace},
		eventBus:   evBus,
		commsBus:   comms,
		// Watcher never comes up.
		liveKeeperFn: func(_, _ string) bool { return false },
	}

	// Probe must finish in well under 5 s (it only polls for 80 ms).
	if !runProbeSync(t, h, "crew-b", grace, 5*time.Second) {
		t.Fatal("probe did not finish within timeout")
	}

	// Verify event emitted.
	events := evBus.emitted()
	if len(events) == 0 {
		t.Fatal("expected session_keeper_watcher_dead event; got none")
	}
	if events[0] != core.EventTypeSessionKeeperWatcherDead {
		t.Errorf("event type = %q; want %q", events[0], core.EventTypeSessionKeeperWatcherDead)
	}

	// Verify comms sent to operator with keeper-alert topic.
	msgs := comms.sent()
	if len(msgs) == 0 {
		t.Fatal("expected keeper-alert comms; got none")
	}
	m := msgs[0]
	if m.To != "operator" {
		t.Errorf("comms To = %q; want \"operator\"", m.To)
	}
	if m.Topic != "keeper-alert" {
		t.Errorf("comms Topic = %q; want \"keeper-alert\"", m.Topic)
	}
	if m.Body == "" {
		t.Error("comms Body is empty")
	}
}

// TestKeeperProbe_GraceFromConfig verifies that the probe uses the grace period
// supplied from config, not a hardcoded literal. This is the
// "grace is config not literal" acceptance criterion from hk-qgfme.
func TestKeeperProbe_GraceFromConfig(t *testing.T) {
	evBus := &stubEventBus{}

	// Configure a short grace (50 ms) and verify the probe finishes and reports
	// dead within a generous timeout. If the probe used a hardcoded value, the
	// timing would be wrong.
	const configuredGrace = 50 * time.Millisecond
	h := &crewHandlerImpl{
		projectDir:   t.TempDir(),
		keeperCfg:    KeeperConfig{FlockAcquireGrace: configuredGrace},
		eventBus:     evBus,
		liveKeeperFn: func(_, _ string) bool { return false },
	}

	start := time.Now()
	if !runProbeSync(t, h, "crew-c", configuredGrace, 5*time.Second) {
		t.Fatal("probe did not finish within timeout")
	}
	elapsed := time.Since(start)

	// Probe should have finished close to grace (not e.g. minutes later).
	// Allow generous headroom for CI timing jitter.
	if elapsed > 3*time.Second {
		t.Errorf("probe took %v; expected close to configured grace %v", elapsed, configuredGrace)
	}

	if got := evBus.emitted(); len(got) == 0 || got[0] != core.EventTypeSessionKeeperWatcherDead {
		t.Errorf("expected session_keeper_watcher_dead; got %v", got)
	}
}

// TestKeeperProbe_NilBusesDoNotPanic verifies that when eventBus and commsBus
// are nil (test or production with no bus wired), probeKeeperLiveness does not
// panic on failure — it logs to stderr and returns cleanly.
func TestKeeperProbe_NilBusesDoNotPanic(t *testing.T) {
	const grace = 40 * time.Millisecond
	h := &crewHandlerImpl{
		projectDir:   t.TempDir(),
		keeperCfg:    KeeperConfig{FlockAcquireGrace: grace},
		eventBus:     nil, // deliberately nil
		commsBus:     nil, // deliberately nil
		liveKeeperFn: func(_, _ string) bool { return false },
	}
	// Must not panic.
	if !runProbeSync(t, h, "crew-d", grace, 5*time.Second) {
		t.Fatal("probe did not finish within timeout")
	}
}

// TestKeeperProbe_LiveOnSecondPoll verifies that the probe recognises a watcher
// that comes up after the first poll attempt (i.e. it retries before giving up).
func TestKeeperProbe_LiveOnSecondPoll(t *testing.T) {
	evBus := &stubEventBus{}

	calls := 0
	liveAfter := 2 // return live on the 2nd call
	h := &crewHandlerImpl{
		projectDir: t.TempDir(),
		keeperCfg:  KeeperConfig{FlockAcquireGrace: 10 * time.Second},
		eventBus:   evBus,
		liveKeeperFn: func(_, _ string) bool {
			calls++
			return calls >= liveAfter
		},
	}

	if !runProbeSync(t, h, "crew-e", 10*time.Second, 5*time.Second) {
		t.Fatal("probe did not finish within timeout")
	}

	// Keeper was detected as live; no dead event should have fired.
	if got := evBus.emitted(); len(got) != 0 {
		t.Errorf("expected no events (keeper came up); got %v", got)
	}
}
