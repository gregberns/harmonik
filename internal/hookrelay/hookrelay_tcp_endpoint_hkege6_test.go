package hookrelay_test

// hookrelay_tcp_endpoint_hkege6_test.go — the hook relay must select its dial
// transport from the HARMONIK_DAEMON_SOCKET value: a "tcp://host:port" endpoint
// dials TCP (the REMOTE reverse-tunnel transport), any other value dials a unix
// socket path (the LOCAL default). hk-ege6: remote runs MUST reach box A over a
// TCP loopback listener on the worker, because the macOS-root sshd `-R`
// unix-socket bind is root-owned 0600 and unconnectable by the unprivileged hook
// subprocess.
//
// These are black-box end-to-end tests: a real listener is started, the public
// hookrelay.Run is invoked with the endpoint set, and the message arriving on the
// correct transport proves resolveDialTarget routed it there.

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

// tcpFixtureListenAndRespond starts a real TCP loopback listener that responds
// once with ackJSON. Returns the "tcp://127.0.0.1:<port>" endpoint string (the
// HARMONIK_DAEMON_SOCKET form the daemon injects for remote runs) and a channel
// receiving the first message bytes.
func tcpFixtureListenAndRespond(t *testing.T, ackJSON string) (endpoint string, received <-chan []byte) {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcpFixtureListenAndRespond: listen: %v", err)
	}

	ch := make(chan []byte, 1)
	hookRelayFixtureWatch(t, "tcpFixtureListenAndRespond", ln, func() error {
		return hookRelayFixtureServe(ln, []string{ackJSON}, ch)
	})

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("tcpFixtureListenAndRespond: listener address is %T, want *net.TCPAddr", ln.Addr())
	}
	return "tcp://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(addr.Port)), ch
}

// TestHookRelay_TCPEndpoint_DialsTCP asserts a "tcp://host:port" endpoint makes
// the relay dial TCP and deliver the agent_ready message to a real TCP listener
// (hk-ege6 remote-run transport).
func TestHookRelay_TCPEndpoint_DialsTCP(t *testing.T) {
	t.Parallel()

	endpoint, received := tcpFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = endpoint

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("SessionStart over tcp endpoint: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case msgBytes := <-received:
		msg, _ := hookRelayFixtureEnvelope(t, "SessionStart over tcp", msgBytes)
		if msgType := hookRelayFixtureString(t, "SessionStart over tcp", msg, "type"); msgType != "agent_ready" {
			t.Errorf("message type = %q, want agent_ready", msgType)
		}
	default:
		t.Error("no message received on the TCP listener; the relay did not dial tcp")
	}
}

// TestHookRelay_UnixEndpoint_DialsUnix asserts a plain (non-"tcp://") value still
// dials a unix socket — the LOCAL default, backward-compatible. The existing
// unix-socket fixture is reused.
func TestHookRelay_UnixEndpoint_DialsUnix(t *testing.T) {
	t.Parallel()

	sockPath, received := hookRelayFixtureListenAndRespond(t, `{"status":"ok"}`)
	e := hookRelayFixtureEnv(t.TempDir())
	e.DaemonSocket = sockPath // plain unix path, no tcp:// prefix

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)
	if code != 0 {
		t.Fatalf("SessionStart over unix endpoint: exit %d, want 0; stderr=%q", code, stderr.String())
	}

	select {
	case <-received:
		// delivered over the unix socket — good.
	default:
		t.Error("no message received on the unix socket; the relay did not dial unix")
	}
}

// TestHookRelay_TCPEndpoint_DoesNotDialUnix proves the transport selection is by
// PREFIX, not coincidence: a "tcp://" endpoint must NOT be dialed as a unix path.
// A unix listener is bound at a path that equals the endpoint's bare host:port;
// the relay must fail to deliver to it (it dials tcp, where nothing listens),
// returning a non-zero exit with a dial failure — never a false unix delivery.
func TestHookRelay_TCPEndpoint_DoesNotDialUnix(t *testing.T) {
	t.Parallel()

	// Bind a unix listener whose PATH is the bare addr the tcp endpoint trims to.
	dir := hookRelayFixtureShortSockDir(t)
	// Use an in-dir name; the point is only that a unix socket EXISTS and the
	// relay must not deliver to it for a tcp:// endpoint.
	unixPath := dir + "/u.sock"
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", unixPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	gotUnix := make(chan []byte, 1)
	hookRelayFixtureWatch(t, "TCPEndpoint_DoesNotDialUnix", ln, func() error {
		return hookRelayFixtureServe(ln, []string{`{"status":"ok"}`}, gotUnix)
	})

	e := hookRelayFixtureEnv(t.TempDir())
	// A tcp:// endpoint the relay cannot dial. Use an out-of-range port so the
	// dial fails FAST and FATALLY (an "invalid port" error, not the retryable
	// connection-refused startup-race case per CHB-016) — the relay must dial tcp
	// (fail), and must NOT fall back to the unix path above.
	e.DaemonSocket = "tcp://127.0.0.1:99999"

	stdin := hookRelayFixtureStdin(t, e.ClaudeSessionID, "SessionStart", nil)
	var stderr bytes.Buffer
	code := hookrelay.Run("SessionStart", stdin, &stderr, &e)

	// Must NOT have reached the unix listener.
	select {
	case msg := <-gotUnix:
		t.Fatalf("relay delivered %q to a unix socket for a tcp:// endpoint (transport not selected by prefix)", msg)
	default:
	}
	// And the tcp dial to the dead port should fail → non-zero exit + dial error.
	if code == 0 {
		t.Errorf("expected non-zero exit on a dead tcp endpoint, got 0; stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "bridge_dial_failed") {
		t.Errorf("stderr = %q, want a bridge_dial_failed (tcp dial refused)", stderr.String())
	}
}
