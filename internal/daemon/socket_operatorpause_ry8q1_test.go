package daemon_test

// socket_operatorpause_ry8q1_test.go — socket routing tests for operator-pause/resume (hk-ry8q1).
//
// Acceptance criteria:
//   - operator-pause op dispatched to OperatorControlHandler; returns Ok=true
//     and controller is paused.
//   - operator-resume op dispatched to OperatorControlHandler; returns Ok=true
//     and controller is no longer paused.
//   - operator-pause with nil OperatorControlHandler returns Ok=false with an
//     error message (graceful degradation).
//   - operator-resume with nil OperatorControlHandler returns Ok=false.
//
// Bead ref: hk-ry8q1.

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// socketOpFixtureStartListenerFull starts RunSocketListenerFull in a goroutine
// with the supplied OperatorControlHandler. Returns the socket path,
// cancel func, and done channel.
func socketOpFixtureStartListenerFull(t *testing.T, oh daemon.OperatorControlHandler) (sockPath string, cancel context.CancelFunc) {
	t.Helper()

	// macOS enforces a 104-char limit on Unix-domain socket paths
	// (sockaddr_un.sun_path). t.TempDir() yields a ~123-char
	// /var/folders/... path that silently overflows the limit, so
	// RunSocketListenerFull never binds and socketFixtureWaitReady times
	// out at 5s. Use the shared short-path helper instead (Refs: hk-p258q).
	sockPath = socketFixtureTempSockPath(t)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		_ = daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, nil, oh, nil)
	}()
	t.Cleanup(func() { cancel() })

	socketFixtureWaitReady(t, sockPath)
	return sockPath, cancel
}

// socketOpSend writes {"op": opName} to sockPath and returns the SocketResponse.
func socketOpSend(t *testing.T, sockPath, opName string) daemon.SocketResponse {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		t.Fatalf("socketOpSend: dial %q: %v", sockPath, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck

	payload, _ := json.Marshal(map[string]string{"op": opName})
	if _, writeErr := conn.Write(payload); writeErr != nil {
		t.Fatalf("socketOpSend: write: %v", writeErr)
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite() //nolint:errcheck
	}

	var resp daemon.SocketResponse
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		t.Fatalf("socketOpSend: decode response: %v", decErr)
	}
	return resp
}

// ---------------------------------------------------------------------------
// TestSocketRouting_OperatorPause_PausesController
// ---------------------------------------------------------------------------

// TestSocketRouting_OperatorPause_PausesController verifies that an
// "operator-pause" socket op is dispatched to the OperatorControlHandler,
// the daemon returns Ok=true, and IsPaused() is set.
func TestSocketRouting_OperatorPause_PausesController(t *testing.T) {
	t.Parallel()

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)

	sockPath, _ := socketOpFixtureStartListenerFull(t, ctrl)

	resp := socketOpSend(t, sockPath, "operator-pause")
	if !resp.Ok {
		t.Fatalf("operator-pause: expected Ok=true; got error=%q", resp.Error)
	}

	if !ctrl.IsPaused() {
		t.Fatal("expected controller IsPaused=true after operator-pause socket op")
	}

	// Emitted pausing + paused events.
	pauseEvts := collectEventsByType(col, "operator_pause_status")
	if len(pauseEvts) != 2 {
		t.Fatalf("expected 2 operator_pause_status events; got %d", len(pauseEvts))
	}
}

// ---------------------------------------------------------------------------
// TestSocketRouting_OperatorResume_ResumesController
// ---------------------------------------------------------------------------

// TestSocketRouting_OperatorResume_ResumesController verifies that after a
// pause, an "operator-resume" socket op clears the paused state.
func TestSocketRouting_OperatorResume_ResumesController(t *testing.T) {
	t.Parallel()

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)

	sockPath, _ := socketOpFixtureStartListenerFull(t, ctrl)

	// Pause first.
	if resp := socketOpSend(t, sockPath, "operator-pause"); !resp.Ok {
		t.Fatalf("operator-pause: %q", resp.Error)
	}

	// Now resume.
	resp := socketOpSend(t, sockPath, "operator-resume")
	if !resp.Ok {
		t.Fatalf("operator-resume: expected Ok=true; got error=%q", resp.Error)
	}

	if ctrl.IsPaused() {
		t.Fatal("expected controller IsPaused=false after operator-resume socket op")
	}

	resumeEvts := collectEventsByType(col, "operator_resuming")
	if len(resumeEvts) != 1 {
		t.Fatalf("expected 1 operator_resuming event; got %d", len(resumeEvts))
	}
}

// ---------------------------------------------------------------------------
// TestSocketRouting_OperatorPause_NilHandler_ReturnsError
// ---------------------------------------------------------------------------

// TestSocketRouting_OperatorPause_NilHandler_ReturnsError verifies that when
// no OperatorControlHandler is registered, operator-pause returns Ok=false.
func TestSocketRouting_OperatorPause_NilHandler_ReturnsError(t *testing.T) {
	t.Parallel()

	// nil OperatorControlHandler — operator-pause/resume must return errors.
	sockPath, _ := socketOpFixtureStartListenerFull(t, nil)

	resp := socketOpSend(t, sockPath, "operator-pause")
	if resp.Ok {
		t.Fatal("expected Ok=false with nil OperatorControlHandler; got Ok=true")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty Error with nil handler")
	}
}

// ---------------------------------------------------------------------------
// TestSocketRouting_OperatorResume_NilHandler_ReturnsError
// ---------------------------------------------------------------------------

func TestSocketRouting_OperatorResume_NilHandler_ReturnsError(t *testing.T) {
	t.Parallel()

	sockPath, _ := socketOpFixtureStartListenerFull(t, nil)

	resp := socketOpSend(t, sockPath, "operator-resume")
	if resp.Ok {
		t.Fatal("expected Ok=false with nil OperatorControlHandler; got Ok=true")
	}
}
