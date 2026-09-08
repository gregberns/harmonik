package herdrwire_test

// Fault matrix (KH-2 self-check item 1): every fault below must fail
// closed — a typed error, never a silent wrong-success and never a hang.
// Each test's name states the claim it defends; run one at a time with
// -run to watch it fail on purpose against a stub client before trusting
// it (PRINCIPLES §7).

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper/panehost/herdrwire"
	"github.com/gregberns/harmonik/internal/keeper/panehost/herdrwire/herdrfake"
)

// closeConn is used from fake-server handler goroutines, which can outlive
// the test that started them — it must never call back into *testing.T
// (t.Logf/t.Fatal from a goroutine after the test returns panics the run),
// so failures go through the same slog path as logIfErr.
func closeConn(conn net.Conn) {
	logIfErr(conn.Close())
}

func writeAll(conn net.Conn, b []byte) error {
	_, err := conn.Write(b)
	return err
}

// FaultConnectionRefused: no server listening at all — the socket path
// exists in no process's accept queue.
func TestFaultConnectionRefused(t *testing.T) {
	dir := t.TempDir()
	deadSock := filepath.Join(dir, "nobody-home.sock")
	// A path that was never listen()'d on gives ENOENT/ECONNREFUSED
	// depending on platform; either way dial must fail, never hang.
	c := herdrwire.NewClient(deadSock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.Ping(ctx)
	if err == nil {
		t.Fatal("Ping against a socket with no listener: want error, got nil")
	}
	var dialErr *herdrwire.DialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("Ping error = %#v (%T), want *DialError", err, err)
	}
}

// FaultConnectionRefusedAfterListenerCloses: exercise the ECONNREFUSED path
// specifically (as opposed to ENOENT for a path never listened on) by
// listening then immediately closing, which removes the socket file too.
func TestFaultConnectionRefusedAfterListenerCloses(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) { closeConn(conn) })
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	sockPath := srv.Path
	if err := srv.Close(); err != nil {
		t.Fatalf("close fake server: %v", err)
	}

	c := herdrwire.NewClient(sockPath)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = c.Ping(ctx)
	if err == nil {
		t.Fatal("Ping against a closed socket: want error, got nil")
	}
	var dialErr *herdrwire.DialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("Ping error = %#v (%T), want *DialError", err, err)
	}
}

// FaultMalformedJSONLine: the server writes bytes that are not valid JSON
// at all. The client must surface a typed decode error, not panic and not
// return a zero-value success.
func TestFaultMalformedJSONLine(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) {
		defer closeConn(conn)
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			return
		}
		logIfErr(writeAll(conn, []byte("{this is not json}\n")))
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = c.PaneClose(ctx, "w1:p1")
	if err == nil {
		t.Fatal("PaneClose against a malformed response line: want error, got nil")
	}
	var frameErr *herdrwire.FrameError
	if !errors.As(err, &frameErr) {
		t.Fatalf("PaneClose error = %#v (%T), want *FrameError", err, err)
	}
}

// FaultErrorResponseBody: herdr answers with a well-formed
// {"id","error":{...}} body. The client must surface it as a typed
// *WireError carrying the code/message, not swallow it as success.
func TestFaultErrorResponseBody(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.close": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteError(conn, req.ID, "pane_not_found", "pane w9:p9 not found"))
		},
	})
	err := c.PaneClose(ctxT(t), "w9:p9")
	if err == nil {
		t.Fatal("PaneClose against an error response: want error, got nil")
	}
	var wireErr *herdrwire.WireError
	if !errors.As(err, &wireErr) || wireErr.Code != "pane_not_found" {
		t.Fatalf("PaneClose error = %#v, want *WireError{Code: pane_not_found}", err)
	}
}

// FaultStaleInvalidPaneID: the realistic instance of FaultErrorResponseBody
// — a pane id that WAS valid a moment ago (e.g. across a respawn) is now
// gone. Every P0 pane-scoped call must fail closed on it, not silently
// operate on nothing.
func TestFaultStaleInvalidPaneID(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.read": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteError(conn, req.ID, "pane_not_found", "pane w3:p9 not found"))
		},
		"pane.process_info": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteError(conn, req.ID, "pane_not_found", "pane w3:p9 not found"))
		},
	})
	ctx := ctxT(t)

	if _, err := c.PaneRead(ctx, herdrwire.PaneReadParams{PaneID: "w3:p9", Source: herdrwire.ReadSourceRecent}); err == nil {
		t.Fatal("PaneRead with a stale pane id: want error, got nil")
	}
	if _, err := c.PaneProcessInfo(ctx, "w3:p9"); err == nil {
		t.Fatal("PaneProcessInfo with a stale pane id: want error, got nil")
	}
}

// FaultProtocolMismatch: the server reports a protocol number this package
// does not pin. The client must refuse EVERY call against that server —
// including the very first one — rather than proceed against an unverified
// wire shape.
func TestFaultProtocolMismatch(t *testing.T) {
	srv, err := herdrfake.NewMethodServer(herdrwire.ProtocolVersion+1, map[string]herdrfake.MethodHandler{
		"pane.close": func(req herdrfake.Request, conn net.Conn) {
			// Must never be reached: the protocol check must short-circuit
			// before this handler runs.
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "ok"}))
		},
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx := ctxT(t)

	err = c.PaneClose(ctx, "w1:p1")
	if err == nil {
		t.Fatal("PaneClose against a protocol mismatch: want error, got nil")
	}
	var mismatch *herdrwire.ProtocolMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("PaneClose error = %#v (%T), want *ProtocolMismatchError", err, err)
	}
	if mismatch.Got != herdrwire.ProtocolVersion+1 || mismatch.Want != herdrwire.ProtocolVersion {
		t.Fatalf("ProtocolMismatchError = %+v", mismatch)
	}

	// A second call against the same still-mismatched server must ALSO
	// fail closed — the failed check must not be cached as success.
	err = c.CheckProtocol(ctx)
	if !errors.As(err, &mismatch) {
		t.Fatalf("second CheckProtocol error = %#v (%T), want *ProtocolMismatchError", err, err)
	}
}

// FaultMidStreamCloseDuringSubscribe: the server acks the subscription,
// delivers nothing more, then closes the connection outright. Next must
// return a clear error, never block forever and never fabricate an Event.
func TestFaultMidStreamCloseDuringSubscribe(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) {
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			closeConn(conn)
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			closeConn(conn)
			return
		}
		logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "subscription_started"}))
		closeConn(conn) // mid-stream close: no events ever follow
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sub, err := c.Subscribe(ctx, herdrwire.PaneCreatedSpec())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			t.Logf("close subscription: %v", closeErr)
		}
	}()

	_, err = sub.Next(ctx)
	if err == nil {
		t.Fatal("Next after a mid-stream close: want error, got nil")
	}
}

// FaultSlowResponsePastDeadline: the server accepts and never answers.
// A caller-supplied deadline must fire the call, not hang past it.
func TestFaultSlowResponsePastDeadline(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	srv, err := herdrfake.Start(func(conn net.Conn) {
		defer closeConn(conn)
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			return
		}
		<-block // never answer the real call within the test's deadline
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = c.PaneClose(ctx, "w1:p1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("PaneClose past its deadline: want error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("PaneClose took %s, want it to fail near the 300ms deadline", elapsed)
	}
}

// FaultTruncatedFrame: the server writes a valid-looking but incomplete
// JSON object and closes before the newline — this must decode-fail, not
// be treated as an empty/zero success.
func TestFaultTruncatedFrame(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) {
		defer closeConn(conn)
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			return
		}
		logIfErr(writeAll(conn, []byte(`{"id":"1","result":{"type":"ok"`))) // no closing braces, no newline
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = c.PaneClose(ctx, "w1:p1")
	if err == nil {
		t.Fatal("PaneClose against a truncated frame: want error, got nil")
	}
}

// FaultSubscribeAgentStatusChangedMissingPaneID (iter-2 review): the live
// protocol-22 server refuses a pane.agent_status_changed subscription with
// no pane_id (invalid_request, "missing field `pane_id`"). Subscribe must
// fail closed on this before ever dialing — asserted here without a fake
// server at all, since the check is local.
func TestFaultSubscribeAgentStatusChangedMissingPaneID(t *testing.T) {
	c := herdrwire.NewClient(filepath.Join(t.TempDir(), "unreachable.sock"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := c.Subscribe(ctx, herdrwire.PaneAgentStatusChangedSpec(""))
	if err == nil {
		t.Fatal("Subscribe(PaneAgentStatusChangedSpec(\"\")): want error, got nil")
	}
	var dialErr *herdrwire.DialError
	if errors.As(err, &dialErr) {
		t.Fatalf("Subscribe error = %#v, want a local validation error (no dial attempted against the unreachable socket)", err)
	}
}

// FaultSubscribeAgentStatusChangedServerRejects: even if the local
// pre-dial check were bypassed, the server's own invalid_request rejection
// must still surface as a typed error, not a usable *Subscription.
func TestFaultSubscribeAgentStatusChangedServerRejects(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) {
		defer closeConn(conn)
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			return
		}
		logIfErr(herdrfake.WriteError(conn, req.ID, "invalid_request", "missing field `pane_id`"))
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sub, err := c.Subscribe(ctx, herdrwire.PaneAgentStatusChangedSpec("w1:p1"))
	if err == nil {
		if closeErr := sub.Close(); closeErr != nil {
			t.Logf("close subscription: %v", closeErr)
		}
		t.Fatal("Subscribe against a server that rejects the request: want error, got a live subscription")
	}
	var wireErr *herdrwire.WireError
	if !errors.As(err, &wireErr) || wireErr.Code != "invalid_request" {
		t.Fatalf("Subscribe error = %#v, want *WireError{Code: invalid_request}", err)
	}
}
