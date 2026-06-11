package daemon_test

// postreadyhang_hka2okh_test.go — unit tests for waitPostAgentReadyProgress
// and the exported hang-detection seams (hk-a2okh).
//
// Helper prefix: postReadyHang* (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// ExportedWaitPostAgentReadyProgress unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPostReadyHang_nilOnFirstEvent verifies that waitPostAgentReadyProgress
// returns nil when the first event arrives before the timeout.
func TestPostReadyHang_nilOnFirstEvent(t *testing.T) {
	t.Parallel()
	ch := make(chan core.EventEnvelope, 1)
	ch <- core.EventEnvelope{Type: "agent_heartbeat"}
	ctx := context.Background()
	err := daemon.ExportedWaitPostAgentReadyProgress(ctx, ch, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// TestPostReadyHang_errOnTimeout verifies that waitPostAgentReadyProgress
// returns ErrPostAgentReadyHang when no event arrives within the timeout.
func TestPostReadyHang_errOnTimeout(t *testing.T) {
	t.Parallel()
	ch := make(chan core.EventEnvelope) // never receives
	ctx := context.Background()
	err := daemon.ExportedWaitPostAgentReadyProgress(ctx, ch, 50*time.Millisecond)
	if err != daemon.ExportedErrPostAgentReadyHang {
		t.Fatalf("want ErrPostAgentReadyHang, got %v", err)
	}
}

// TestPostReadyHang_errOnContextCancel verifies that waitPostAgentReadyProgress
// returns ctx.Err() when the context is cancelled before an event arrives.
func TestPostReadyHang_errOnContextCancel(t *testing.T) {
	t.Parallel()
	ch := make(chan core.EventEnvelope) // never receives
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	err := daemon.ExportedWaitPostAgentReadyProgress(ctx, ch, 5*time.Second)
	if err == nil || err == daemon.ExportedErrPostAgentReadyHang {
		t.Fatalf("want ctx.Err(), got %v", err)
	}
}

// TestPostReadyHang_errOnClosedChannel verifies that waitPostAgentReadyProgress
// returns ErrPostAgentReadyHang when the event channel is closed with no events.
func TestPostReadyHang_errOnClosedChannel(t *testing.T) {
	t.Parallel()
	ch := make(chan core.EventEnvelope)
	close(ch)
	ctx := context.Background()
	err := daemon.ExportedWaitPostAgentReadyProgress(ctx, ch, 5*time.Second)
	if err != daemon.ExportedErrPostAgentReadyHang {
		t.Fatalf("want ErrPostAgentReadyHang on closed channel, got %v", err)
	}
}

// TestPostReadyHang_zeroTimeoutUsesDefault verifies that a zero timeout
// substitutes defaultPostAgentReadyHangTimeout rather than returning instantly.
func TestPostReadyHang_zeroTimeoutUsesDefault(t *testing.T) {
	t.Parallel()
	// Temporarily shorten the default so the test does not take 7 minutes.
	orig := *daemon.ExportedDefaultPostAgentReadyHangTimeout
	*daemon.ExportedDefaultPostAgentReadyHangTimeout = 50 * time.Millisecond
	defer func() { *daemon.ExportedDefaultPostAgentReadyHangTimeout = orig }()

	ch := make(chan core.EventEnvelope) // never receives
	ctx := context.Background()
	start := time.Now()
	err := daemon.ExportedWaitPostAgentReadyProgress(ctx, ch, 0 /* zero → default */)
	elapsed := time.Since(start)
	if err != daemon.ExportedErrPostAgentReadyHang {
		t.Fatalf("want ErrPostAgentReadyHang, got %v", err)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("returned too fast (%v): zero timeout must use default, not be instant", elapsed)
	}
}
