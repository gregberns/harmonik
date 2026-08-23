package handler_test

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func launchFixtureHandler(t *testing.T) (handler.Handler, *handlercontract.CollectingEmitter) {
	t.Helper()
	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	reg := handlercontract.NewAdapterRegistry()
	h := handler.NewHandler(pub, dl, reg)
	return h, pub
}

// TestHandler_Launch_ReturnHandles verifies that Launch returns a non-nil Session
// and non-nil Watcher when the child exits cleanly.
func TestHandler_Launch_ReturnHandles(t *testing.T) {
	t.Parallel()

	h, _ := launchFixtureHandler(t)

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
