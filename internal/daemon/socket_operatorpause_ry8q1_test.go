package daemon_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func socketOpFixtureStartListenerFull(t *testing.T, oh daemon.OperatorControlHandler) (sockPath string, cancel context.CancelFunc) {
	t.Helper()

	sockPath = socketFixtureTempSockPath(t)

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		_ = daemon.RunSocketListenerFull(ctx, sockPath, nil, nil, nil, oh, nil)
	}()
	t.Cleanup(func() { cancel() })

	socketFixtureWaitReady(t, sockPath)
	return sockPath, cancel
}

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

	pauseEvts := collectEventsByType(col, "operator_pause_status")
	if len(pauseEvts) != 2 {
		t.Fatalf("expected 2 operator_pause_status events; got %d", len(pauseEvts))
	}
}

// TestSocketRouting_OperatorResume_ResumesController verifies that after a
// pause, an "operator-resume" socket op clears the paused state.
func TestSocketRouting_OperatorResume_ResumesController(t *testing.T) {
	t.Parallel()

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)

	sockPath, _ := socketOpFixtureStartListenerFull(t, ctrl)

	if resp := socketOpSend(t, sockPath, "operator-pause"); !resp.Ok {
		t.Fatalf("operator-pause: %q", resp.Error)
	}

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

// TestSocketRouting_OperatorPause_NilHandler_ReturnsError verifies that when
// no OperatorControlHandler is registered, operator-pause returns Ok=false.
func TestSocketRouting_OperatorPause_NilHandler_ReturnsError(t *testing.T) {
	t.Parallel()

	sockPath, _ := socketOpFixtureStartListenerFull(t, nil)

	resp := socketOpSend(t, sockPath, "operator-pause")
	if resp.Ok {
		t.Fatal("expected Ok=false with nil OperatorControlHandler; got Ok=true")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty Error with nil handler")
	}
}

func TestSocketRouting_OperatorResume_NilHandler_ReturnsError(t *testing.T) {
	t.Parallel()

	sockPath, _ := socketOpFixtureStartListenerFull(t, nil)

	resp := socketOpSend(t, sockPath, "operator-resume")
	if resp.Ok {
		t.Fatal("expected Ok=false with nil OperatorControlHandler; got Ok=true")
	}
}
