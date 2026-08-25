package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	"github.com/gregberns/harmonik/internal/eventbus"
)

type hfatalLedger struct {
	mu     sync.Mutex
	ready  []core.BeadID
	closed []core.BeadID
	opened []core.BeadID
}

func (l *hfatalLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ready) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := l.ready[0]
	l.ready = l.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (l *hfatalLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *hfatalLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *hfatalLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = append(l.closed, beadID)
	return nil
}

func (l *hfatalLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = append(l.opened, beadID)
	return nil
}

// AddReady appends a bead to the ready queue (safe to call concurrently).
func (l *hfatalLedger) AddReady(id core.BeadID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ready = append(l.ready, id)
}

func (l *hfatalLedger) reopenedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.opened))
	copy(out, l.opened)
	return out
}

func (l *hfatalLedger) closedIDs() []core.BeadID {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]core.BeadID, len(l.closed))
	copy(out, l.closed)
	return out
}

func hfatalFixtureTwinPath() string {
	path, _ := scenariotest.CheckoutBinaryPath("harmonik-twin-claude")
	return path
}

func hfatalFixtureMakeRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hfatalFixtureMakeRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

func hfatalFixtureEmitRateLimitActive(t *testing.T, policy *daemon.HandlerPausePolicyGoroutine) {
	t.Helper()
	runID := hfatalFixtureMakeRunID(t)
	payload := core.AgentRateLimitStatusPayload{
		RunID:     runID,
		SessionID: core.SessionID("hfatal-synth-sess-" + runID.String()),
		Status:    core.AgentRateLimitStatusActive,
		ChangedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("hfatalFixtureEmitRateLimitActive: marshal: %v", err)
	}
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hfatalFixtureEmitRateLimitActive: uuid.NewV7 for evID: %v", err)
	}
	evt := core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            core.EventTypeAgentRateLimitStatus,
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payloadJSON),
	}
	if err := daemon.ExportedPolicyHandleRateLimitStatus(policy, context.Background(), evt); err != nil {
		t.Fatalf("hfatalFixtureEmitRateLimitActive: ExportedPolicyHandleRateLimitStatus: %v", err)
	}
}

func hfatalFixturePollReopen(t *testing.T, ledger *hfatalLedger, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		if len(ledger.reopenedIDs()) > 0 {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("hfatalFixturePollReopen: timed out after %s waiting for bead reopen", deadline)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func hfatalFixturePollIsPaused(t *testing.T, ctrl *daemon.HandlerPauseController, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		if ctrl.IsPaused(core.AgentTypeClaudeCode) {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("hfatalFixturePollIsPaused: timed out after %s; IsPaused() never became true", deadline)
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func hfatalFixturePollHeldEvent(t *testing.T, bus *stubEventCollector, deadline time.Duration) {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		for _, e := range bus.allEvents() {
			if e.EventType == string(core.EventTypeQueueItemHeldForHandlerPause) {
				return
			}
		}
		select {
		case <-timer.C:
			t.Fatalf("hfatalFixturePollHeldEvent: timed out after %s waiting for queue_item_held_for_handler_pause", deadline)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// TestScenario_WorkLoop_HandlerFatalTripsGate boots ExportedRunWorkLoop with
// a one-bead stub ledger and a twin in --scenario handler-fatal mode, then
// asserts the full handler-fatal → pause-trip → queue-held path:
//
//	(a) bead-1 reaches agent_failed (ReopenBead is called on the ledger).
//	(b) HandlerPauseController.IsPaused() is true after the policy is tripped
//	    by two synthetic rate-limit active events.
//	(c) bead-2 (added to the ledger only after pause is confirmed) is held with
//	    queue_item_held_for_handler_pause rather than dispatched.
//
// Bead ref: hk-qxtbq.
func TestScenario_WorkLoop_HandlerFatalTripsGate(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	twinBin := hfatalFixtureTwinPath()
	if _, err := os.Stat(twinBin); err != nil {
		t.Skipf("harmonik-twin-claude not found at %s; build with: go build -o ./harmonik-twin-claude ./cmd/harmonik-twin-claude", twinBin)
	}

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	ctrlBus := eventbus.NewBusImpl()
	if err := ctrlBus.Seal(); err != nil {
		t.Fatalf("ctrlBus.Seal: %v", err)
	}
	ctrl := daemon.NewHandlerPauseController(ctrlBus, nil)

	reg := runregistry.NewRunRegistry()

	policy := daemon.ExportedNewHandlerPausePolicyGoroutine(daemon.ExportedHandlerPausePolicyConfig{
		AgentType:  core.AgentTypeClaudeCode,
		Controller: ctrl,
		Registry:   reg,
	})

	const (
		bead1 = core.BeadID("hk-qxtbq-bead1-fatal")
		bead2 = core.BeadID("hk-qxtbq-bead2-held")
	)

	ledger := &hfatalLedger{ready: []core.BeadID{bead1}}

	bus := &stubEventCollector{}

	p := daemon.TestRuntimeParams{
		BrAdapter:              ledger,
		Bus:                    bus,
		ProjectDir:             projectDir,
		HandlerBinary:          twinBin,
		HandlerArgs:            []string{"--scenario", "handler-fatal"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		RunRegistry:            reg,
		AdapterRegistry2:       NewSealedAdapterRegistryForTest(t),
		HandlerPauseController: ctrl,
	}
	deps := daemon.ExportedTestRuntime(p)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	hfatalFixturePollReopen(t, ledger, 30*time.Second)

	reopened := ledger.reopenedIDs()
	if len(reopened) == 0 || reopened[0] != bead1 {
		t.Fatalf("expected bead-1 (%q) to be reopened; got %v", bead1, reopened)
	}
	if len(ledger.closedIDs()) > 0 {
		t.Fatalf("bead-1 was closed; expected reopen on agent_failed")
	}

	hfatalFixtureEmitRateLimitActive(t, policy)
	hfatalFixtureEmitRateLimitActive(t, policy)

	hfatalFixturePollIsPaused(t, ctrl, 5*time.Second)

	ledger.AddReady(bead2)

	hfatalFixturePollHeldEvent(t, bus, 15*time.Second)

	var foundBead2Held bool
	for _, e := range bus.allEvents() {
		if e.EventType != string(core.EventTypeQueueItemHeldForHandlerPause) {
			continue
		}
		var pl core.QueueItemHeldForHandlerPausePayload
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatalf("unmarshal QueueItemHeldForHandlerPausePayload: %v", err)
		}
		if !pl.Valid() {
			t.Fatalf("QueueItemHeldForHandlerPausePayload.Valid() = false: %+v", pl)
		}
		if core.BeadID(pl.BeadID) == bead2 {
			foundBead2Held = true
		}
	}
	if !foundBead2Held {
		t.Fatalf("no queue_item_held_for_handler_pause event found for bead-2 (%q)", bead2)
	}

	for _, id := range ledger.closedIDs() {
		if id == bead2 {
			t.Fatalf("bead-2 was dispatched (closed) while handler was paused; expected hold")
		}
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(daemon.ExportedDaemonExitHangBudget):
		t.Fatalf("work loop did not exit within %s after context cancellation", daemon.ExportedDaemonExitHangBudget)
	}
}
