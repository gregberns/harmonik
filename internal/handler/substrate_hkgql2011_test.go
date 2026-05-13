package handler_test

// substrate_hkgql2011_test.go — unit tests for the Substrate seam (hk-gql20.11).
//
// Helper prefix: substrateFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-gql20.11).
//
// Tests verify:
//  1. nil Substrate preserves the current exec.CommandContext path.
//  2. non-nil Substrate routes to SpawnWindow (mock substrate).
//  3. When SubstrateSession.Stdout() returns nil, LaunchViaSubstrate returns a
//     nil watcher and a valid Session.
//  4. When SubstrateSession.Stdout() returns a non-nil io.Reader, a Watcher is
//     returned and wired to the progress stream.
//  5. SpawnWindow error propagates as a non-nil error from Launch.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// substrateFixtureHandler constructs a Handler with fresh fixture dependencies.
func substrateFixtureHandler(t *testing.T) (handler.Handler, *handlercontract.CollectingEmitter) {
	t.Helper()
	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	reg := handlercontract.NewAdapterRegistry()
	h := handler.NewHandler(pub, dl, reg)
	return h, pub
}

// fakeSubstrate is a test double for handler.Substrate.
type fakeSubstrate struct {
	// spawnCalled is incremented each time SpawnWindow is called.
	spawnCalled atomic.Int32

	// spawnErr is returned by SpawnWindow when non-nil.
	spawnErr error

	// sess is the SubstrateSession returned on successful spawn.
	sess handler.SubstrateSession
}

// SpawnWindow records the call and returns the configured session or error.
func (f *fakeSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnCalled.Add(1)
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	return f.sess, nil
}

// Compile-time assertion: fakeSubstrate implements handler.Substrate.
var _ handler.Substrate = (*fakeSubstrate)(nil)

// fakeSubstrateSession is a test double for handler.SubstrateSession.
type fakeSubstrateSession struct {
	// stdout, when non-nil, is returned by Stdout(). When nil, simulates a
	// tmux-hosted session with no stdout pipe.
	stdout io.Reader

	// outcome is returned by Outcome().
	outcome handler.Outcome

	// waitBlock, when non-nil, blocks Wait until closed.
	waitBlock chan struct{}
}

func (s *fakeSubstrateSession) Kill(_ context.Context) error { return nil }
func (s *fakeSubstrateSession) Wait(_ context.Context) error {
	if s.waitBlock != nil {
		<-s.waitBlock
	}
	return nil
}
func (s *fakeSubstrateSession) Outcome() handler.Outcome { return s.outcome }
func (s *fakeSubstrateSession) PID() int                 { return 42 }
func (s *fakeSubstrateSession) Stdout() io.Reader        { return s.stdout }

// Compile-time assertion: fakeSubstrateSession implements handler.SubstrateSession.
var _ handler.SubstrateSession = (*fakeSubstrateSession)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestHandler_Launch_NilSubstrate_PreservesExecPath verifies that when
// LaunchSpec.Substrate is nil, Handler.Launch uses the exec.CommandContext path
// (backward compatible behavior).
func TestHandler_Launch_NilSubstrate_PreservesExecPath(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	spec := handler.LaunchSpec{
		Binary:    "sh",
		Args:      []string{"-c", `printf '{"type":"agent_ready"}\n'`},
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Role:      "test",
		Substrate: nil, // explicitly nil — exec path
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch with nil Substrate: %v", err)
	}
	if sess == nil {
		t.Fatal("Launch: returned nil Session")
	}
	if watcher == nil {
		t.Fatal("Launch: returned nil Watcher (expected non-nil for exec path)")
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

// TestHandler_Launch_NonNilSubstrate_CallsSpawnWindow verifies that when
// LaunchSpec.Substrate is non-nil, Handler.Launch calls Substrate.SpawnWindow
// instead of exec.CommandContext.
func TestHandler_Launch_NonNilSubstrate_CallsSpawnWindow(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	done := make(chan struct{})
	fakeSess := &fakeSubstrateSession{
		stdout:    nil, // tmux-hosted: no stdout pipe
		waitBlock: done,
	}
	fake := &fakeSubstrate{sess: fakeSess}

	spec := handler.LaunchSpec{
		Binary:    "claude",
		Args:      []string{"--session-id", "test-session"},
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Role:      "test",
		Substrate: fake,
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch with non-nil Substrate: %v", err)
	}
	if sess == nil {
		t.Fatal("Launch: returned nil Session")
	}
	// When SubstrateSession.Stdout() returns nil, watcher MUST be nil.
	if watcher != nil {
		t.Errorf("Launch: expected nil Watcher for substrate-hosted session with nil Stdout(), got non-nil")
	}
	// Verify SpawnWindow was called exactly once.
	if n := fake.spawnCalled.Load(); n != 1 {
		t.Errorf("SpawnWindow call count: got %d, want 1", n)
	}

	// Unblock the fake Wait.
	close(done)
	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}
}

// TestHandler_Launch_SubstrateNilStdout_WatcherIsNil verifies that when the
// SubstrateSession returns nil from Stdout(), Launch returns a nil Watcher
// (bridge wire is the daemon socket, not stdout).
func TestHandler_Launch_SubstrateNilStdout_WatcherIsNil(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	fakeSess := &fakeSubstrateSession{stdout: nil}
	fake := &fakeSubstrate{sess: fakeSess}

	spec := handler.LaunchSpec{
		Binary:    "claude",
		Args:      nil,
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Substrate: fake,
	}

	_, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if watcher != nil {
		t.Errorf("Launch: watcher should be nil when Substrate.Stdout() returns nil, got non-nil")
	}
}

// TestHandler_Launch_SubstrateNonNilStdout_WatcherIsWired verifies that when
// the SubstrateSession returns a non-nil Stdout(), Launch wires a Watcher to it.
func TestHandler_Launch_SubstrateNonNilStdout_WatcherIsWired(t *testing.T) {
	t.Parallel()

	h, pub := substrateFixtureHandler(t)

	// Provide an io.Reader that emits one event line and then EOF.
	ndjson := `{"type":"agent_ready"}` + "\n"
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, ndjson)
		_ = pw.Close()
	}()

	fakeSess := &fakeSubstrateSession{stdout: pr}
	fake := &fakeSubstrate{sess: fakeSess}

	spec := handler.LaunchSpec{
		Binary:    "claude",
		Args:      nil,
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Substrate: fake,
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if watcher == nil {
		t.Fatal("Launch: expected non-nil Watcher when SubstrateSession.Stdout() is non-nil")
	}

	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}

	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	// Verify the watcher received the agent_ready event.
	types := pub.EventTypes()
	found := false
	for _, et := range types {
		if et == "agent_ready" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("publisher event types %v do not include agent_ready", types)
	}
}

// TestHandler_Launch_SubstrateSpawnError_ReturnsError verifies that a
// SpawnWindow error is propagated as a non-nil error from Launch.
func TestHandler_Launch_SubstrateSpawnError_ReturnsError(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	sentinelErr := errors.New("substrate: window collision")
	fake := &fakeSubstrate{spawnErr: sentinelErr}

	spec := handler.LaunchSpec{
		Binary:    "claude",
		Args:      nil,
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Substrate: fake,
	}

	_, _, err := h.Launch(t.Context(), spec)
	if err == nil {
		t.Fatal("Launch: expected error when SpawnWindow returns error, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("Launch: expected errors.Is(err, sentinelErr) == true; got %v", err)
	}
}

// TestHandler_Launch_SubstrateSession_KillAndWait verifies that Kill and Wait
// on a substrate-backed Session delegate to the underlying SubstrateSession.
func TestHandler_Launch_SubstrateSession_KillAndWait(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	waitDone := make(chan struct{})
	fakeSess := &fakeSubstrateSession{
		stdout:    nil,
		waitBlock: waitDone,
	}
	fake := &fakeSubstrate{sess: fakeSess}

	spec := handler.LaunchSpec{
		Binary:    "claude",
		Args:      nil,
		Env:       []string{},
		WorkDir:   t.TempDir(),
		Substrate: fake,
	}

	sess, _, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Kill should complete without error.
	if err := sess.Kill(t.Context()); err != nil {
		t.Errorf("Session.Kill: %v", err)
	}

	// Unblock Wait; it should return now.
	close(waitDone)

	waitCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := sess.Wait(waitCtx); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}
}

// TestSubstrateSpawn_StdoutWrapperApplied verifies that LaunchSpec.StdoutWrapper
// is applied to the Substrate session's Stdout() when non-nil.
func TestSubstrateSpawn_StdoutWrapperApplied(t *testing.T) {
	t.Parallel()

	h, _ := substrateFixtureHandler(t)

	ndjson := `{"type":"agent_ready"}` + "\n"
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, ndjson)
		_ = pw.Close()
	}()

	wrapperCalled := false
	wrapper := func(r io.Reader) io.Reader {
		wrapperCalled = true
		// Return a reader that reads from r and buffers everything (passthrough).
		return io.MultiReader(r, bytes.NewReader(nil))
	}

	fakeSess := &fakeSubstrateSession{stdout: pr}
	fake := &fakeSubstrate{sess: fakeSess}

	spec := handler.LaunchSpec{
		Binary:        "claude",
		Args:          nil,
		Env:           []string{},
		WorkDir:       t.TempDir(),
		Substrate:     fake,
		StdoutWrapper: wrapper,
	}

	_, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if watcher == nil {
		t.Fatal("Launch: expected non-nil Watcher")
	}

	select {
	case <-watcher.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watcher.Done() did not close within timeout")
	}

	if !wrapperCalled {
		t.Error("StdoutWrapper was not called for substrate-backed session with non-nil Stdout()")
	}
}
