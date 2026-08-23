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
	"github.com/gregberns/harmonik/internal/lifecycle"
)

func socketFixtureSockPathUnder(root string) string {
	return filepath.Join(root, ".harmonik", "daemon.sock")
}

func socketFixtureTempSockPath(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if lifecycle.ValidateSocketPathLength(socketFixtureSockPathUnder(root)) != nil {
		dir, err := os.MkdirTemp("/tmp", "sk-")
		if err != nil {
			t.Fatalf("socketFixtureTempSockPath: MkdirTemp /tmp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) }) //nolint:errcheck // cleanup error unactionable
		root = dir
	}

	sockPath := socketFixtureSockPathUnder(root)
	if lenErr := lifecycle.ValidateSocketPathLength(sockPath); lenErr != nil {
		t.Fatalf("socketFixtureTempSockPath: no bindable socket path for this test: %v", lenErr)
	}

	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("socketFixtureTempSockPath: MkdirAll .harmonik: %v", err)
	}
	return sockPath
}

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

func socketFixtureDial(t *testing.T, sockPath string) net.Conn {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("socketFixtureDial: DialContext %q: %v", sockPath, err)
	}
	return conn
}

func socketFixtureSendRecv(t *testing.T, conn net.Conn, req daemon.SocketRequest) daemon.SocketResponse {
	t.Helper()

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("socketFixtureSendRecv: marshal request: %v", err)
	}
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("socketFixtureSendRecv: write: %v", err)
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // cleanup error unactionable
	}

	var resp daemon.SocketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("socketFixtureSendRecv: decode response: %v", err)
	}
	return resp
}

func socketFixtureStartListener(t *testing.T, sockPath string, h daemon.RequestHandler, hr ...daemon.HookRelayHandler) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()

	var hookRelay daemon.HookRelayHandler
	if len(hr) > 0 {
		hookRelay = hr[0]
	}

	ctx, cancel := context.WithCancel(t.Context())
	ch := make(chan error, 1)
	go func() {
		ch <- daemon.RunSocketListener(ctx, sockPath, h, hookRelay)
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

func socketFixtureWaitReady(t *testing.T, sockPath string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
		if err == nil {
			_ = conn.Close()
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

func socketFixtureWaitMode(t *testing.T, sockPath string, wantMode os.FileMode, budget time.Duration) os.FileMode {
	t.Helper()
	deadline := time.Now().Add(budget)
	var got os.FileMode
	for time.Now().Before(deadline) {
		info, err := os.Stat(sockPath)
		if err == nil {
			got = info.Mode().Perm()
			if got == wantMode {
				return got
			}
		}
		time.Sleep(time.Millisecond)
	}
	return got
}

// TestRunSocketListener_BindsAndSetsMode verifies that RunSocketListener
// creates a socket at sockPath and sets its permissions to 0600.
func TestRunSocketListener_BindsAndSetsMode(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	const wantMode = os.FileMode(0o600)
	got := socketFixtureWaitMode(t, sockPath, wantMode, 500*time.Millisecond)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("socket path %q is not a Unix domain socket (mode=%v)", sockPath, info.Mode())
	}
	if got != wantMode {
		t.Errorf("socket mode = %04o, want %04o", got, wantMode)
	}
}

// TestRunSocketListener_StaleRemoval verifies that RunSocketListener removes
// a pre-existing stale file at sockPath before binding.
func TestRunSocketListener_StaleRemoval(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)

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

func socketFixtureCreateStaleSocket(t *testing.T, sockPath string) func() {
	t.Helper()

	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: sockPath, Net: "unix"})
	if err != nil {
		t.Fatalf("socketFixtureCreateStaleSocket: listen %q: %v", sockPath, err)
	}
	l.SetUnlinkOnClose(false)
	if err := l.Close(); err != nil {
		t.Fatalf("socketFixtureCreateStaleSocket: close listener: %v", err)
	}

	return func() {
		_ = os.Remove(sockPath) //nolint:errcheck // cleanup; already-removed is fine
	}
}

// TestRunSocketListener_StaleSockFileNoListener verifies Gap-2 of the recovery
// audit: when daemon.sock exists on disk but no process is listening (e.g. after
// a SIGKILL), RunSocketListener must detect the stale socket, remove it, and
// successfully bind so that the daemon starts instead of failing with EADDRINUSE.
//
// The test creates a Unix domain socket inode with no listener behind it, then starts RunSocketListener and asserts that the socket is
// re-created and is accepting connections.
//
// Bead ref: hk-63omj (recovery: stale daemon.sock after SIGKILL → EADDRINUSE).
func TestRunSocketListener_StaleSockFileNoListener(t *testing.T) {
	t.Parallel()

	sockPath := socketFixtureTempSockPath(t)

	cleanupStale := socketFixtureCreateStaleSocket(t, sockPath)
	defer cleanupStale()

	if _, statErr := os.Stat(sockPath); statErr != nil {
		t.Fatalf("StaleSockFileNoListener: stale socket file missing before daemon start: %v", statErr)
	}

	h := &stubHandler{}
	cancel, done := socketFixtureStartListener(t, sockPath, h)
	defer cancel()

	select {
	case startErr := <-done:
		t.Fatalf("StaleSockFileNoListener: RunSocketListener returned early with error: %v", startErr)
	default:
	}

	socketFixtureWaitReady(t, sockPath)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("StaleSockFileNoListener: Stat socket after rebind: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("StaleSockFileNoListener: %q is not a Unix domain socket after rebind (mode=%v)", sockPath, info.Mode())
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
	defer func() { _ = conn.Close() }()

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
	defer func() { _ = conn.Close() }()

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
	defer func() { _ = conn.Close() }()

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
	h := &stubHandler{claimNextErr: fmt.Errorf("brcli: no ready beads")}
	socketFixtureStartListener(t, sockPath, h)
	socketFixtureWaitReady(t, sockPath)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()

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
	if err := <-done; err != nil {
		t.Errorf("CancelStopsListener: RunSocketListener returned non-nil after cancel: %v", err)
	}
}

func hookRelayFixtureEnvBytes(t *testing.T, env map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("hookRelayFixtureEnvBytes: marshal: %v", err)
	}
	return append(data, '\n')
}

func hookRelayFixtureSendAndReadAck(t *testing.T, conn net.Conn, envBytes []byte) map[string]string {
	t.Helper()
	if _, err := conn.Write(envBytes); err != nil {
		t.Fatalf("hookRelayFixtureSendAndReadAck: write: %v", err)
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck // cleanup error unactionable
	}
	var ack map[string]string
	if err := json.NewDecoder(conn).Decode(&ack); err != nil {
		t.Fatalf("hookRelayFixtureSendAndReadAck: decode ack: %v", err)
	}
	return ack
}

// TestSocketListener_HookRelayHandler is the bead hk-gql20.21 acceptance test.
// It starts the socket listener with a real hookSessionStore, sends a
// hook-relay envelope, and asserts the envelope is accepted (not bad_envelope)
// and the store has recorded the outcome.
//
// Bead ref: hk-gql20.21.
func TestSocketListener_HookRelayHandler(t *testing.T) {
	t.Parallel()

	const runID = "run-wire-hr-01"
	const sessionID = "claude-sess-wire-hr-01"

	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)

	sockPath := socketFixtureTempSockPath(t)
	h := &stubHandler{}

	cancel, _ := socketFixtureStartListener(t, sockPath, h, store)
	defer cancel()
	socketFixtureWaitReady(t, sockPath)

	payload, err := json.Marshal(map[string]string{"kind": "WORK_COMPLETE", "summary": "wire test"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	envMap := map[string]interface{}{
		"type":               "outcome_emitted",
		"run_id":             runID,
		"claude_session_id":  sessionID,
		"handler_session_id": "handler-sess-wire-01",
		"emitted_at_ns":      int64(42000),
		"payload":            json.RawMessage(payload),
	}
	envBytes := hookRelayFixtureEnvBytes(t, envMap)

	conn := socketFixtureDial(t, sockPath)
	defer func() { _ = conn.Close() }()

	ack := hookRelayFixtureSendAndReadAck(t, conn, envBytes)

	if ack["status"] != "ok" {
		t.Errorf("hook-relay ACK status = %q (reason=%q), want %q",
			ack["status"], ack["reason"], "ok")
	}

	got := daemon.ExportedHookLatestOutcome(store, runID, sessionID)
	if got == nil {
		t.Fatal("LatestOutcome after hook-relay dispatch: nil, want non-nil")
	}
	var gotMap map[string]string
	if err := json.Unmarshal(*got, &gotMap); err != nil {
		t.Fatalf("LatestOutcome unmarshal: %v", err)
	}
	if gotMap["summary"] != "wire test" {
		t.Errorf("LatestOutcome summary = %q, want %q", gotMap["summary"], "wire test")
	}
}

// The fixture must hand back a bindable socket path even when the calling
// test's own name is long. t.TempDir() embeds that name, truncated to 64
// characters, and then appends a random suffix that is sometimes 9 digits and
// sometimes 10. Only one of those two draws used to cross the line, so a single
// run proves nothing. Each subtest draws a fresh random, so the loop covers
// both.
//
// This test's name is deliberately past the 64-character truncation point. That
// is the case that broke (hk-m3jai).
func TestSocketFixture_TempSockPath_HandsBackABindablePathForALongTestName(t *testing.T) {
	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			sockPath := socketFixtureTempSockPath(t)
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", sockPath)
			if err != nil {
				t.Fatalf("the fixture returned a %d-byte socket path the kernel refuses: %v", len(sockPath), err)
			}
			_ = ln.Close()
		})
	}
}
