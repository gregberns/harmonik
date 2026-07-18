package handlercontract_test

// watcher_w4_correlation_test.go — Wave-4 regression: watcher-synthesized
// agent_failed events MUST carry session_id and run_id so self-defect
// terminals are attributable and auto-recoverable by the reconciler.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// w4corrPublisher captures full payloads (the shared fixture publisher drops them).
type w4corrPublisher struct {
	mu       sync.Mutex
	payloads map[string][]byte // eventType -> last payload
}

func (p *w4corrPublisher) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.payloads == nil {
		p.payloads = map[string][]byte{}
	}
	p.payloads[string(eventType)] = append([]byte(nil), payload...)
	return nil
}

func (p *w4corrPublisher) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return p.Emit(ctx, eventType, payload)
}

func (p *w4corrPublisher) payloadFor(eventType string) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.payloads[eventType]
}

type w4corrDeadLetter struct{}

func (w4corrDeadLetter) Append(core.EventType, []byte, string) error { return nil }

// TestWatcher_SynthesizedAgentFailed_CarriesSessionAndRunID drives the watcher
// into a self-defect terminal (malformed NDJSON) and asserts the synthesized
// agent_failed payload carries the envelope correlation fields.
func TestWatcher_SynthesizedAgentFailed_CarriesSessionAndRunID(t *testing.T) {
	t.Parallel()

	const sessID = "w4-corr-session"
	const runID = "w4-corr-run"

	pub := &w4corrPublisher{}
	m := hclifecycle.New(sessID, runID)
	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID(sessID),
		ProgressStream: strings.NewReader("{not valid json}\n"),
		Publisher:      pub,
		DeadLetter:     w4corrDeadLetter{},
		Machine:        m,
	})

	select {
	case <-w.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not finish")
	}

	raw := pub.payloadFor(string(handlercontract.ProgressMsgTypeAgentFailed))
	if raw == nil {
		t.Fatal("no agent_failed payload published")
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal agent_failed payload: %v", err)
	}
	if got["session_id"] != sessID {
		t.Errorf("session_id = %q, want %q (payload: %s)", got["session_id"], sessID, raw)
	}
	if got["run_id"] != runID {
		t.Errorf("run_id = %q, want %q (payload: %s)", got["run_id"], runID, raw)
	}
	if got["sub_reason"] == "" {
		t.Errorf("sub_reason missing (payload: %s)", raw)
	}
}
