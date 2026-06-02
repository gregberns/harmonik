package daemon_test

// socket_queue_test.go — additive tests asserting that the four queue
// control-surface ops are registered on the daemon socket (hk-nomxl).
//
// These tests verify that:
//   - "queue-submit", "queue-append", "queue-status", "queue-dry-run" are
//     handled by RunSocketListener when a QueueHandler is registered.
//   - Responses carry the expected ErrorCode when no QueueHandler is wired
//     (nil QueueHandler path → -32099 per handleQueueOp).
//   - "enqueue" is NOT in the registered set (per bead body acceptance criterion).
//
// Spec ref: specs/process-lifecycle.md §4.4 PL-003a.
// Bead ref: hk-nomxl.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// Concrete QueueHandler stub for socket tests
// ---------------------------------------------------------------------------

// socketQueueFixtureStub satisfies daemon.QueueHandler with canned responses.
// It uses context.Context parameters matching the interface definition.
type socketQueueFixtureStub struct{}

func (s *socketQueueFixtureStub) HandleQueueSubmit(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"queue_id":"00000000-0000-0000-0000-000000000001","status":"active","group_count":1}`), nil
}

func (s *socketQueueFixtureStub) HandleQueueAppend(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"appended_count":1,"new_tail_indices":[0]}`), nil
}

func (s *socketQueueFixtureStub) HandleQueueStatus(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"queue":null}`), nil
}

func (s *socketQueueFixtureStub) HandleQueueDryRun(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"resolved_queue":{},"ledger_dep_notices":[],"parallelism_narrowed":false}`), nil
}

func (s *socketQueueFixtureStub) HandleQueueList(_ context.Context) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"queues":[]}`), nil
}

func (s *socketQueueFixtureStub) HandleQueueSetConcurrency(_ context.Context, _ json.RawMessage) (json.RawMessage, *queue.RPCError) {
	return json.RawMessage(`{"old_n":4,"new_n":6}`), nil
}

// ---------------------------------------------------------------------------
// Fixture helper: start listener with optional QueueHandler
// ---------------------------------------------------------------------------

// socketQueueFixtureStartListenerWithQH starts RunSocketListener with a
// QueueHandler registered. Returns cancel func and done channel like
// socketFixtureStartListener. Passing nil for qh omits the queue handler.
func socketQueueFixtureStartListenerWithQH(t *testing.T, sockPath string, h daemon.RequestHandler, qh daemon.QueueHandler) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() {
		if qh != nil {
			ch <- daemon.RunSocketListener(ctx, sockPath, h, nil, qh)
		} else {
			ch <- daemon.RunSocketListener(ctx, sockPath, h, nil)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-ch:
		default:
		}
	})
	return cancel, ch
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSocketListener_QueueMethodsRegistered verifies that all four queue ops
// are routed to the QueueHandler when one is registered and return Ok=true.
func TestSocketListener_QueueMethodsRegistered(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}
	qh := &socketQueueFixtureStub{}

	socketQueueFixtureStartListenerWithQH(t, sockPath, h, qh)
	socketFixtureWaitReady(t, sockPath)

	queueOps := []string{"queue-submit", "queue-append", "queue-status", "queue-dry-run", "queue-list", "queue-set-concurrency"}
	for _, op := range queueOps {
		op := op
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			conn := socketFixtureDial(t, sockPath)
			defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

			resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: op})
			// The stub returns Ok=true; a non-ok response means the op is not
			// registered (routed to default "unknown op" case).
			if !resp.Ok {
				t.Errorf("op %q: response.ok = false, error = %q (method not registered or routed incorrectly)",
					op, resp.Error)
			}
		})
	}
}

// TestSocketListener_EnqueueNotRegistered verifies that "enqueue" is NOT in
// the registered op set (per bead body acceptance criterion).
func TestSocketListener_EnqueueNotRegistered(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}

	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: "enqueue"})
	if resp.Ok {
		t.Fatal("enqueue: response.ok = true, want false (enqueue must not be registered per bead body)")
	}
}

// TestSocketListener_QueueMethodsNilHandler verifies that queue ops return a
// structured error (ok=false, error_code=-32099) when no QueueHandler is wired.
func TestSocketListener_QueueMethodsNilHandler(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}

	// No QueueHandler registered.
	socketQueueFixtureStartListenerWithQH(t, sockPath, h, nil)
	socketFixtureWaitReady(t, sockPath)

	queueOps := []string{"queue-submit", "queue-append", "queue-status", "queue-dry-run", "queue-list", "queue-set-concurrency"}
	for _, op := range queueOps {
		op := op
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			conn := socketFixtureDial(t, sockPath)
			defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

			resp := socketFixtureSendRecv(t, conn, daemon.SocketRequest{Op: op})
			if resp.Ok {
				t.Errorf("op %q: response.ok = true, want false (nil QueueHandler)", op)
			}
			if resp.ErrorCode != -32099 {
				t.Errorf("op %q: ErrorCode = %d, want -32099 (nil QueueHandler path)", op, resp.ErrorCode)
			}
		})
	}
}
