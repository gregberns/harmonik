package codextest_test

// L2 integration tests — reactor + faked comms/queue/beads (codex-app-server T5, hk-oe86p)
//
// L2 exercises the full chain: corpus → twin → reactor → HarmonikBridgeSink.
// The HarmonikBridgeSink is an Effector implementation that records what the
// real harmonik-side effector would write to comms, queue, and beads — all
// faked (in-memory, no I/O).
//
// CODEX_LIVE=0 (default): runs against captured corpus only.

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexreactor"
)

// ─── HarmonikBridgeSink ──────────────────────────────────────────────────────

// HarmonikBridgeSink is a test-only Effector that records what the real
// harmonik integration layer would write to comms, queue, and beads.
//
// In production, an Effector of this shape would:
//   - On complete_turn  → advance the queue item to "done", close the bead
//   - On emit_output    → publish the delta to the harmonik comms stream
//   - On emit_error     → publish an error event, halt the turn
//   - On notify_status  → update a cached thread-status field
//   - On notify_token_usage → feed the billing guard / token ledger
//
// The sink fakes all three services (comms, queue, beads) in-memory so L2
// tests assert WHAT the bridge emits without real I/O.
type HarmonikBridgeSink struct {
	mu sync.Mutex

	// CommsMessages records deltas published to the comms stream.
	CommsMessages []BridgeCommsMessage

	// QueueCompletions records complete_turn calls (queue item done).
	QueueCompletions []BridgeQueueCompletion

	// BeadTransitions records bead-status updates.
	BeadTransitions []BridgeBeadTransition

	// ErrorEvents records error signals.
	ErrorEvents []BridgeErrorEvent

	// StatusUpdates records thread-status notifications.
	StatusUpdates []BridgeStatusUpdate

	// TokenUsageUpdates records token-usage notifications.
	TokenUsageUpdates []BridgeTokenUsageUpdate
}

// BridgeCommsMessage is a delta published to the harmonik comms stream.
type BridgeCommsMessage struct {
	ThreadID string
	TurnID   string
	ItemID   string
	Delta    string
}

// BridgeQueueCompletion is a queue-item completion (turn done).
type BridgeQueueCompletion struct {
	ThreadID string
	TurnID   string
	Status   string
}

// BridgeBeadTransition is a bead-status update (e.g. in_progress → closed).
type BridgeBeadTransition struct {
	ThreadID string
	TurnID   string
	Status   string
}

// BridgeErrorEvent is an error signal from the reactor.
type BridgeErrorEvent struct {
	Message string
}

// BridgeStatusUpdate is a thread-status notification.
type BridgeStatusUpdate struct {
	ThreadID string
	Status   string
}

// BridgeTokenUsageUpdate is a token-usage notification.
type BridgeTokenUsageUpdate struct {
	ThreadID      string
	TurnID        string
	TotalTokens   int64
	ContextWindow int64
}

// Execute implements codexreactor.Effector.
// Routes each action type to the appropriate fake harmonik service sink.
func (s *HarmonikBridgeSink) Execute(_ context.Context, a codexreactor.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch a.Type {
	case codexreactor.ActionTypeEmitOutput:
		// comms: publish delta to the run's output stream
		s.CommsMessages = append(s.CommsMessages, BridgeCommsMessage{
			ThreadID: a.ThreadID,
			TurnID:   a.TurnID,
			ItemID:   a.ItemID,
			Delta:    a.Delta,
		})

	case codexreactor.ActionTypeCompleteTurn:
		// queue: mark queue item done
		s.QueueCompletions = append(s.QueueCompletions, BridgeQueueCompletion{
			ThreadID: a.ThreadID,
			TurnID:   a.TurnID,
			Status:   a.Status,
		})
		// beads: update bead status
		s.BeadTransitions = append(s.BeadTransitions, BridgeBeadTransition{
			ThreadID: a.ThreadID,
			TurnID:   a.TurnID,
			Status:   a.Status,
		})

	case codexreactor.ActionTypeEmitError:
		// comms + queue: signal error, halt the turn
		s.ErrorEvents = append(s.ErrorEvents, BridgeErrorEvent{Message: a.Message})

	case codexreactor.ActionTypeNotifyStatus:
		// update cached thread-status field
		s.StatusUpdates = append(s.StatusUpdates, BridgeStatusUpdate{
			ThreadID: a.ThreadID,
			Status:   a.Status,
		})

	case codexreactor.ActionTypeNotifyTokenUsage:
		// billing guard / token ledger
		s.TokenUsageUpdates = append(s.TokenUsageUpdates, BridgeTokenUsageUpdate{
			ThreadID:      a.ThreadID,
			TurnID:        a.TurnID,
			TotalTokens:   a.TotalTokens,
			ContextWindow: a.ContextWindow,
		})
	}
	return nil
}

// ─── L2 integration tests ────────────────────────────────────────────────────

// TestL2_ReactorHappyPathWithBridgeSink runs the full corpus through:
//
//	corpus → twin → reactor → HarmonikBridgeSink (faked comms/queue/beads)
//
// and asserts that each fake harmonik service received exactly the calls
// that a real Effector would make in a happy-path turn.
func TestL2_ReactorHappyPathWithBridgeSink(t *testing.T) {
	t.Parallel()

	const (
		threadID = "019f5489-8dde-7ed2-81c3-5848fe26f1ac"
		turnID   = "019f5489-8e9f-7d62-b86c-6020273ed855"
		itemID   = "msg_0bb2d88f02914c01016a5314e2fb5c819ab15adab033e890c5"
	)

	f, err := os.Open(l1CorpusPath())
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	twin := codexdigitaltwin.New(f, codexdigitaltwin.FaultConfig{})
	sink := &HarmonikBridgeSink{}
	r := codexreactor.New()
	if err := r.Run(context.Background(), twin, sink); err != nil {
		t.Fatalf("reactor.Run: %v", err)
	}

	// comms: one output delta ("ok")
	if len(sink.CommsMessages) != 1 {
		t.Errorf("comms: want 1 message, got %d: %v", len(sink.CommsMessages), sink.CommsMessages)
	} else {
		m := sink.CommsMessages[0]
		if m.ThreadID != threadID || m.TurnID != turnID || m.ItemID != itemID || m.Delta != "ok" {
			t.Errorf("comms message mismatch: %+v", m)
		}
	}

	// queue: one completion (turn done)
	if len(sink.QueueCompletions) != 1 {
		t.Errorf("queue: want 1 completion, got %d: %v", len(sink.QueueCompletions), sink.QueueCompletions)
	} else {
		c := sink.QueueCompletions[0]
		if c.ThreadID != threadID || c.TurnID != turnID || c.Status != "completed" {
			t.Errorf("queue completion mismatch: %+v", c)
		}
	}

	// beads: one transition (turn done)
	if len(sink.BeadTransitions) != 1 {
		t.Errorf("beads: want 1 transition, got %d: %v", len(sink.BeadTransitions), sink.BeadTransitions)
	} else {
		b := sink.BeadTransitions[0]
		if b.ThreadID != threadID || b.TurnID != turnID || b.Status != "completed" {
			t.Errorf("bead transition mismatch: %+v", b)
		}
	}

	// status updates: active then idle
	if len(sink.StatusUpdates) != 2 {
		t.Errorf("status updates: want 2, got %d: %v", len(sink.StatusUpdates), sink.StatusUpdates)
	} else {
		if sink.StatusUpdates[0].Status != "active" {
			t.Errorf("status[0]: want active, got %q", sink.StatusUpdates[0].Status)
		}
		if sink.StatusUpdates[1].Status != "idle" {
			t.Errorf("status[1]: want idle, got %q", sink.StatusUpdates[1].Status)
		}
	}

	// token usage: one update
	if len(sink.TokenUsageUpdates) != 1 {
		t.Errorf("token usage: want 1, got %d", len(sink.TokenUsageUpdates))
	} else {
		tu := sink.TokenUsageUpdates[0]
		if tu.TotalTokens != 15825 || tu.ContextWindow != 258400 {
			t.Errorf("token usage mismatch: total=%d window=%d", tu.TotalTokens, tu.ContextWindow)
		}
	}

	// no errors
	if len(sink.ErrorEvents) != 0 {
		t.Errorf("unexpected error events: %v", sink.ErrorEvents)
	}
}

// TestL2_ReactorDisconnectWithBridgeSink verifies that a mid-turn disconnect
// routes an error to the comms error sink instead of a queue completion.
func TestL2_ReactorDisconnectWithBridgeSink(t *testing.T) {
	t.Parallel()

	f, err := os.Open(l1CorpusPath())
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	defer f.Close()

	// FaultDropAfter ev2 (turn/started) — disconnect while turn in-flight.
	fault := codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 2}
	twin := codexdigitaltwin.New(f, fault)
	sink := &HarmonikBridgeSink{}
	r := codexreactor.New()
	if err := r.Run(context.Background(), twin, sink); err != nil {
		t.Fatalf("reactor.Run: %v", err)
	}

	// Expect an error event (disconnect during turn)
	if len(sink.ErrorEvents) == 0 {
		t.Error("L2 disconnect: expected at least one error event, got none")
	}
	// No queue completion on error path
	if len(sink.QueueCompletions) != 0 {
		t.Errorf("L2 disconnect: unexpected queue completions: %v", sink.QueueCompletions)
	}
}
