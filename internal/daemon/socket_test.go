package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// socketFixtureTempSockPath creates a temporary .harmonik directory and
// returns a socket path that fits within the 104-byte macOS sun_path limit.
//
// macOS enforces a 104-character limit on Unix domain socket paths
// (sun_path in sockaddr_un). This helper mirrors the strategy used in
// lifecycle/testfixture_test.go (plFixtureTempProjectDir) to keep tests
// portable.
func socketFixtureTempSockPath(t *testing.T) string {
	t.Helper()

	const sunPathMax = 104 // sockaddr_un.sun_path limit on macOS
	const sockFile = "daemon.sock"

	candidate := t.TempDir()
	harmonikDir := filepath.Join(candidate, ".harmonik")
	sockCandidate := filepath.Join(harmonikDir, sockFile)

	var root string
	if len(sockCandidate) <= sunPathMax {
		root = candidate
	} else {
		dir, err := os.MkdirTemp("/tmp", "sk-")
		if err != nil {
			t.Fatalf("socketFixtureTempSockPath: MkdirTemp /tmp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		root = dir
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(root, ".harmonik"), 0o755); err != nil {
		t.Fatalf("socketFixtureTempSockPath: MkdirAll .harmonik: %v", err)
	}
	return filepath.Join(root, ".harmonik", sockFile)
}

// stubHandler is a minimal RequestHandler that records calls for test
// assertions. It returns configurable results for EmitOutcome and ClaimNext.
type stubHandler struct {
	emitOutcomeCalled bool
	emitOutcomeReq    daemon.OutcomeRequest

	claimNextCalled bool
	claimNextRole   string

	emitOutcomeResult json.RawMessage
	emitOutcomeErr    error

	claimNextResult json.RawMessage
	claimNextErr    error
}

func (s *stubHandler) EmitOutcome(_ context.Context, req daemon.OutcomeRequest) (json.RawMessage, error) {
	s.emitOutcomeCalled = true
	s.emitOutcomeReq = req
	return s.emitOutcomeResult, s.emitOutcomeErr
}

func (s *stubHandler) ClaimNext(_ context.Context, role string) (json.RawMessage, error) {
	s.claimNextCalled = true
	s.claimNextRole = role
	return s.claimNextResult, s.claimNextErr
}

// socketFixtureDial connects to a Unix socket at sockPath using the
// context-aware Dialer (lint: net.Dial is forbidden).
func socketFixtureDial(t *testing.T, sockPath string) net.Conn {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("socketFixtureDial: DialContext %q: %v", sockPath, err)
	}
	return conn
}

// socketFixtureSendRecv writes req as JSON to conn and reads a JSON
// SocketResponse back. The caller is responsible for closing conn.
func socketFixtureSendRecv(t *testing.T, conn net.Conn, req daemon.SocketRequest) daemon.SocketResponse {
	t.Helper()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("socketFixtureSendRecv: marshal request: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("socketFixtureSendRecv: write: %v", err)
	}
	// Half-close the write side so the server's json.Decoder can detect EOF.
	//nolint:errorlint // *net.UnixConn specific; type assertion is intentional
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // cleanup error unactionable
	}

	var resp daemon.SocketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("socketFixtureSendRecv: decode response: %v", err)
	}
	return resp
}

// socketFixtureStartListener starts RunSocketListener in a goroutine and
// returns a cancel function and a result channel. The channel carries exactly
// one value (the RunSocketListener return error). The cancel func and channel
// are independent: calling cancel() causes the listener to stop; the channel
// value can be read exactly once.
//
// A t.Cleanup is registered that cancels the context. If the caller has
// already drained the channel, the cleanup skips the drain (non-blocking
// receive) to avoid a deadlock.
func socketFixtureStartListener(t *testing.T, sockPath string, h daemon.RequestHandler) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.RunSocketListener(ctx, sockPath, h)
	}()
	t.Cleanup(func() {
		cancel()
		// Non-blocking drain: the test may have already read from ch.
		select {
		case <-ch:
		default:
		}
	})
	return cancel, ch
}

// socketFixtureWaitReady polls until the socket at sockPath is accepting
// connections (i.e., a dial succeeds). This is more reliable than polling for
// file existence because it confirms that both Listen and Accept are running.
func socketFixtureWaitReady(t *testing.T, sockPath string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
		if err == nil {
			_ = conn.Close() //nolint:errcheck // probe conn; cleanup error unactionable
			return
		}
		runtime.Gosched()
		select {
		case <-t.Context().Done():
			t.Fatalf("socketFixtureWaitReady: context cancelled before socket ready at %q", sockPath)
			return
		default:
		}
	}
	t.Fatalf("socketFixtureWaitReady: socket at %q not ready within 5s", sockPath)
}

// TestRunSocketListener_BindsAndSetsMode verifies that RunSocketListener
// creates a socket at sockPath and sets its permissions to 0600.
func TestRunSocketListener_BindsAndSetsMode(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("socket path %q is not a Unix domain socket (mode=%v)", sockPath, info.Mode())
	}
	const wantMode = os.FileMode(0o600)
	if got := info.Mode().Perm(); got != wantMode {
		t.Errorf("socket mode = %04o, want %04o", got, wantMode)
	}
}

// TestRunSocketListener_StaleRemoval verifies that RunSocketListener removes
// a pre-existing stale file at sockPath before binding.
func TestRunSocketListener_StaleRemoval(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)

	// Lay down a stale file (simulates a crashed daemon's leftover socket).
	if err := os.WriteFile(sockPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile stale socket: %v", err)
	}

	h := &stubHandler{}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat socket after stale removal: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("socket path %q is not a Unix domain socket after stale removal (mode=%v)", sockPath, info.Mode())
	}
}

// TestRunSocketListener_EmitOutcome verifies that an "emit-outcome" request
// is routed to RequestHandler.EmitOutcome and the response shape is correct.
func TestRunSocketListener_EmitOutcome(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	stubResult := json.RawMessage(`{"acked":true}`)
	h := &stubHandler{emitOutcomeResult: stubResult}

	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := daemon.SocketRequest{
		Op:      "emit-outcome",
		RunID:   "run-abc",
		BeadID:  "hk-0001",
		Outcome: json.RawMessage(`{"exit_code":0}`),
	}
	resp := socketFixtureSendRecv(t, conn, req)

	if !resp.Ok {
		t.Fatalf("emit-outcome: response.ok = false, error = %q", resp.Error)
	}
	if !h.emitOutcomeCalled {
		t.Fatal("emit-outcome: handler.EmitOutcome was not called")
	}
	if h.emitOutcomeReq.RunID != "run-abc" {
		t.Errorf("emit-outcome: handler received RunID = %q, want %q", h.emitOutcomeReq.RunID, "run-abc")
	}
	if h.emitOutcomeReq.BeadID != "hk-0001" {
		t.Errorf("emit-outcome: handler received BeadID = %q, want %q", h.emitOutcomeReq.BeadID, "hk-0001")
	}
	if string(resp.Result) != string(stubResult) {
		t.Errorf("emit-outcome: response.result = %s, want %s", resp.Result, stubResult)
	}
}

// TestRunSocketListener_ClaimNext verifies that a "claim-next" request is
// routed to RequestHandler.ClaimNext and the response shape is correct.
func TestRunSocketListener_ClaimNext(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	stubResult := json.RawMessage(`{"bead_id":"hk-9999","title":"do the thing"}`)
	h := &stubHandler{claimNextResult: stubResult}

	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := daemon.SocketRequest{
		Op:   "claim-next",
		Role: "implementer",
	}
	resp := socketFixtureSendRecv(t, conn, req)

	if !resp.Ok {
		t.Fatalf("claim-next: response.ok = false, error = %q", resp.Error)
	}
	if !h.claimNextCalled {
		t.Fatal("claim-next: handler.ClaimNext was not called")
	}
	if h.claimNextRole != "implementer" {
		t.Errorf("claim-next: handler received role = %q, want %q", h.claimNextRole, "implementer")
	}
	if string(resp.Result) != string(stubResult) {
		t.Errorf("claim-next: response.result = %s, want %s", resp.Result, stubResult)
	}
}

// TestRunSocketListener_UnknownOp verifies that an unrecognised "op" value
// produces ok=false with a descriptive error.
func TestRunSocketListener_UnknownOp(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := daemon.SocketRequest{Op: "not-a-real-op"}
	resp := socketFixtureSendRecv(t, conn, req)

	if resp.Ok {
		t.Fatal("unknown-op: response.ok = true, want false")
	}
	if resp.Error == "" {
		t.Fatal("unknown-op: response.error is empty, want descriptive message")
	}
}

// TestRunSocketListener_HandlerError verifies that a handler error is
// propagated to the caller as ok=false with the error message.
func TestRunSocketListener_HandlerError(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	//nolint:goerr113 // test sentinel error; inline construction is intentional
	h := &stubHandler{claimNextErr: fmt.Errorf("brcli: no ready beads")}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	req := daemon.SocketRequest{Op: "claim-next", Role: "implementer"}
	resp := socketFixtureSendRecv(t, conn, req)

	if resp.Ok {
		t.Fatal("handler-error: response.ok = true, want false")
	}
	if resp.Error == "" {
		t.Fatal("handler-error: response.error is empty, want error message from handler")
	}
}

// TestRunSocketListener_CancelStopsListener verifies that cancelling the
// context causes RunSocketListener to return nil (clean exit).
func TestRunSocketListener_CancelStopsListener(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}
	cancel, done := socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	cancel()
	// The channel is buffered (cap 1); block until the goroutine exits.
	if err := <-done; err != nil {
		t.Errorf("CancelStopsListener: RunSocketListener returned non-nil after cancel: %v", err)
	}
}
