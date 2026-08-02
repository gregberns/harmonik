package daemon

import (
	"context"
	"testing"
)

func TestDispatchGatesPort_HeldEventDedupCanResetForNewEpoch(t *testing.T) {
	emitter := &recordingEmitter{}
	gates := newDispatchGatesPort(emitter, nil, nil, nil)

	emitHeldEvent(context.Background(), gates, "hk-held", 3)
	emitHeldEvent(context.Background(), gates, "hk-held", 3)
	if got := len(emitter.types); got != 1 {
		t.Fatalf("events after duplicate key = %d, want 1", got)
	}

	clear(gates.heldEventDedup)
	emitHeldEvent(context.Background(), gates, "hk-held", 3)
	if got := len(emitter.types); got != 2 {
		t.Fatalf("events after dedup reset = %d, want 2", got)
	}
}
