package handler_test

// handler_test.go — tests for Handler.Launch (MVH_ROADMAP row #7, bead hk-zxpj2).
//
// Helper prefix: launchFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-zxpj2).
//
// Tests drive a tiny sh -c child that emits valid NDJSON progress-stream lines
// and asserts:
//   - Launch returns a non-nil Session and non-nil Watcher.
//   - The Watcher receives at least one known-type event.
//   - Session.Wait returns after the child exits.
//
// Event collection and dead-letter stubs are provided by
// handlercontract.CollectingEmitter and handlercontract.NoopWatcherDeadLetter so
// that this file does not need to import internal/core (EV-002b boundary).

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// launchFixtureHandler constructs a Handler with fresh fixture dependencies.
// Uses handlercontract.CollectingEmitter and handlercontract.NoopWatcherDeadLetter
// so the test file has no direct import of internal/core (EV-002b).
func launchFixtureHandler(t *testing.T) (handler.Handler, *handlercontract.CollectingEmitter) {
	t.Helper()
	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl)
	return h, pub
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHandler_Launch_ReturnHandles verifies that Launch returns a non-nil Session
// and non-nil Watcher when the child exits cleanly.
func TestHandler_Launch_ReturnHandles(t *testing.T) {
	t.Parallel()

	h, _ := launchFixtureHandler(t)

	// Child emits one valid NDJSON agent_ready line and exits immediately.
	spec := handler.LaunchSpec{
		Binary:  "sh",
		Args:    []string{"-c", `printf '{"type":"agent_ready"}\n'`},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "test",
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if sess == nil {
		t.Fatal("Launch: returned nil Session")
	}
	if watcher == nil {
		t.Fatal("Launch: returned nil Watcher")
	}

	// Wait for the watcher to finish (process exits → stdout EOF → watcher done).
	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}
}

// TestHandler_Launch_WatcherReceivesEvent verifies that the Watcher receives at
// least one known-type progress-stream event emitted by the child.
func TestHandler_Launch_WatcherReceivesEvent(t *testing.T) {
	t.Parallel()

	h, pub := launchFixtureHandler(t)

	// Child emits agent_ready followed by agent_completed, then exits.
	spec := handler.LaunchSpec{
		Binary:  "sh",
		Args:    []string{"-c", `printf '{"type":"agent_ready"}\n{"type":"agent_completed"}\n'`},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "test",
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Wait for watcher to drain all output.
	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	types := pub.EventTypes()
	if len(types) == 0 {
		t.Fatal("publisher received no events; expected at least one from the child's NDJSON output")
	}

	// Verify at least one expected event type is present.
	found := false
	for _, et := range types {
		if et == "agent_ready" || et == "agent_completed" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("publisher event types %v do not include agent_ready or agent_completed", types)
	}
}

// TestHandler_Launch_WatcherCleanExitAfterSessionWait verifies that the Watcher
// exits cleanly (Err() == nil) after the child emits output and exits.
func TestHandler_Launch_WatcherCleanExitAfterSessionWait(t *testing.T) {
	t.Parallel()

	h, _ := launchFixtureHandler(t)

	spec := handler.LaunchSpec{
		Binary:  "sh",
		Args:    []string{"-c", `printf '{"type":"agent_heartbeat"}\n'`},
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "test",
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}

	if watcherErr := watcher.Err(); watcherErr != nil {
		t.Errorf("watcher.Err(): expected nil (clean exit), got %v", watcherErr)
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}
}

// TestHandler_Launch_MissingBinary verifies that Launch returns a non-nil error
// when the binary path does not exist.
func TestHandler_Launch_MissingBinary(t *testing.T) {
	t.Parallel()

	h, _ := launchFixtureHandler(t)

	spec := handler.LaunchSpec{
		Binary:  "/nonexistent/binary/path",
		Args:    nil,
		Env:     []string{},
		WorkDir: t.TempDir(),
		Role:    "test",
	}

	_, _, err := h.Launch(t.Context(), spec)
	if err == nil {
		t.Fatal("Launch: expected error for missing binary, got nil")
	}
}
