package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

const sessionBootstrapReceipt = `{"schema_version":1,"binding":{"queue_id":"0197d100-0000-7000-8000-000000000001","queue_name":"main","group_index":0,"item_index":1,"bead_id":"hk-bootstrap","run_id":"0197d100-0000-7000-8000-000000000002","claim_transition_id":"0197d100-0000-7000-8000-000000000003"},"session_name":"harmonik-project-run","window_name":"run-window"}`

func TestSessionBootstrapAcknowledgesBeforeStartingHandler(t *testing.T) {
	server, client := net.Pipe()
	requestSeen := make(chan daemon.SocketRequest, 1)
	go func() {
		defer server.Close()
		var request daemon.SocketRequest
		if err := json.NewDecoder(server).Decode(&request); err != nil {
			return
		}
		requestSeen <- request
		if _, err := server.Write([]byte(`{"ok":true,"result":{"acknowledged":true}}`)); err != nil {
			return
		}
	}()

	execCalls := 0
	var gotArgv, gotEnv []string
	code := runSessionBootstrap(
		[]string{"/handler", "--flag"},
		func(name string) string {
			switch name {
			case sessionStartReceiptEnv:
				return sessionBootstrapReceipt
			case "HARMONIK_DAEMON_SOCKET":
				return "/coordinator.sock"
			default:
				return ""
			}
		},
		func() []string { return []string{"KEEP=value", sessionStartReceiptEnv + "=" + sessionBootstrapReceipt} },
		func(string) (string, error) { return "/resolved/handler", nil },
		func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "unix" || address != "/coordinator.sock" {
				t.Fatalf("dial = %s %s; want unix coordinator socket", network, address)
			}
			return client, nil
		},
		func(path string, argv, env []string) error {
			execCalls++
			if path != "/resolved/handler" {
				t.Fatalf("exec path = %q; want resolved executable", path)
			}
			gotArgv, gotEnv = slices.Clone(argv), slices.Clone(env)
			return nil
		},
		&strings.Builder{},
	)
	if code != 0 || execCalls != 1 {
		t.Fatalf("code = %d, exec calls = %d; want 0, 1", code, execCalls)
	}
	request := <-requestSeen
	if request.Op != "session-start-ack" || len(request.Payload) == 0 {
		t.Fatalf("request = %+v; want exact session acknowledgement", request)
	}
	if !slices.Equal(gotArgv, []string{"/resolved/handler", "--flag"}) {
		t.Fatalf("argv = %v", gotArgv)
	}
	if !slices.Equal(gotEnv, []string{"KEEP=value"}) {
		t.Fatalf("child env = %v; receipt must not reach the handler", gotEnv)
	}
}

func TestResolveSessionBootstrapExecutableUsesEffectiveAccess(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "handler")
	if err := os.WriteFile(regular, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(regular, 0o700); err != nil { //nolint:gosec // the fixture must be executable
		t.Fatal(err)
	}
	resolved, err := resolveSessionBootstrapExecutable(regular)
	if err != nil || resolved != regular {
		t.Fatalf("resolved = %q, %v; want executable path", resolved, err)
	}
	for _, tc := range []struct {
		name string
		path string
		mode os.FileMode
	}{
		{name: "non executable", path: regular, mode: 0o600},
		{name: "owner cannot execute", path: regular, mode: 0o001},
		{name: "directory", path: root, mode: 0o700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.path == regular {
				if err := os.Chmod(tc.path, tc.mode); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := os.Chmod(tc.path, 0o700); err != nil { //nolint:gosec // restore executable test fixture
						t.Errorf("restore executable mode: %v", err)
					}
				}()
			}
			if got, err := resolveSessionBootstrapExecutable(tc.path); err == nil {
				t.Fatalf("resolved = %q; want effective-access refusal", got)
			}
		})
	}
}

func TestSessionBootstrapRefusalDoesNotStartHandler(t *testing.T) {
	server, client := net.Pipe()
	go func() {
		defer server.Close()
		var request daemon.SocketRequest
		if err := json.NewDecoder(server).Decode(&request); err != nil {
			return
		}
		if _, err := server.Write([]byte(`{"ok":false,"error":"receipt conflict"}`)); err != nil {
			return
		}
	}()
	execCalls := 0
	var stderr strings.Builder
	code := runSessionBootstrap(
		[]string{"/handler"},
		func(name string) string {
			if name == sessionStartReceiptEnv {
				return sessionBootstrapReceipt
			}
			return "/coordinator.sock"
		},
		func() []string { return nil },
		func(path string) (string, error) { return path, nil },
		func(context.Context, string, string) (net.Conn, error) { return client, nil },
		func(string, []string, []string) error { execCalls++; return nil },
		&stderr,
	)
	if code != 1 || execCalls != 0 || !strings.Contains(stderr.String(), "receipt conflict") {
		t.Fatalf("code=%d exec=%d stderr=%q", code, execCalls, stderr.String())
	}
}

func TestSessionBootstrapRequiresExplicitAcknowledgedResult(t *testing.T) {
	server, client := net.Pipe()
	go func() {
		defer server.Close()
		var request daemon.SocketRequest
		if err := json.NewDecoder(server).Decode(&request); err != nil {
			return
		}
		if _, err := server.Write([]byte(`{"ok":true,"result":{"acknowledged":false}}`)); err != nil {
			return
		}
	}()
	execCalls := 0
	code := runSessionBootstrap(
		[]string{"/handler"},
		func(name string) string {
			if name == sessionStartReceiptEnv {
				return sessionBootstrapReceipt
			}
			return "/coordinator.sock"
		},
		func() []string { return nil },
		func(path string) (string, error) { return path, nil },
		func(context.Context, string, string) (net.Conn, error) { return client, nil },
		func(string, []string, []string) error { execCalls++; return nil },
		&strings.Builder{},
	)
	if code != 1 || execCalls != 0 {
		t.Fatalf("code=%d exec=%d; an ok envelope without acknowledged=true must stop", code, execCalls)
	}
}

func TestSessionBootstrapRejectsInvalidReceiptBeforeDial(t *testing.T) {
	dialCalls := 0
	code := runSessionBootstrap(
		[]string{"/handler"},
		func(name string) string {
			if name == sessionStartReceiptEnv {
				return `{"schema_version":1}`
			}
			return "/coordinator.sock"
		},
		func() []string { return nil },
		func(path string) (string, error) { return path, nil },
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("must not dial")
		},
		func(string, []string, []string) error { t.Fatal("handler started"); return nil },
		&strings.Builder{},
	)
	if code != 1 || dialCalls != 0 {
		t.Fatalf("code=%d dial calls=%d; want 1, 0", code, dialCalls)
	}
}

func TestSessionBootstrapDialTargetSupportsRemoteTunnel(t *testing.T) {
	network, address, err := sessionBootstrapDialTarget("tcp://127.0.0.1:51234")
	if err != nil || network != "tcp" || address != "127.0.0.1:51234" {
		t.Fatalf("dial target = %q %q %v", network, address, err)
	}
}

func TestSessionBootstrapRejectsUnresolvedHandlerBeforeAcknowledgement(t *testing.T) {
	for _, handlerPath := range []string{"claude", "/missing/harmonik-handler"} {
		t.Run(strings.ReplaceAll(handlerPath, "/", "_"), func(t *testing.T) {
			dialCalls := 0
			code := runSessionBootstrap(
				[]string{handlerPath},
				func(name string) string {
					if name == sessionStartReceiptEnv {
						return sessionBootstrapReceipt
					}
					return "/coordinator.sock"
				},
				func() []string { return nil },
				resolveSessionBootstrapExecutable,
				func(context.Context, string, string) (net.Conn, error) {
					dialCalls++
					return nil, errors.New("must not dial")
				},
				func(string, []string, []string) error { t.Fatal("handler started"); return nil },
				&strings.Builder{},
			)
			if code != 1 || dialCalls != 0 {
				t.Fatalf("code=%d dial calls=%d; handler must resolve before acknowledgement", code, dialCalls)
			}
		})
	}
}

func TestSessionBootstrapRejectsNonStrictCoordinatorResponse(t *testing.T) {
	for _, response := range []string{
		`{"ok":true,"result":{"acknowledged":true},"extra":1}`,
		`{"ok":true,"result":{"acknowledged":true,"extra":1}}`,
		`{"ok":true,"result":{"acknowledged":true}} {}`,
	} {
		t.Run(response, func(t *testing.T) {
			server, client := net.Pipe()
			go serveSessionBootstrapResponse(server, response)
			execCalls := 0
			code := runSessionBootstrap(
				[]string{"/handler"}, sessionBootstrapGetenv, func() []string { return nil },
				func(path string) (string, error) { return path, nil },
				func(context.Context, string, string) (net.Conn, error) { return client, nil },
				func(string, []string, []string) error { execCalls++; return nil },
				&strings.Builder{},
			)
			if code != 1 || execCalls != 0 {
				t.Fatalf("code=%d exec=%d; non-strict response must stop", code, execCalls)
			}
		})
	}
}

type sessionBootstrapCloseErrorConn struct {
	net.Conn
}

type sessionBootstrapFailWriter struct{}

func (sessionBootstrapFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected diagnostic write failure")
}

func (c sessionBootstrapCloseErrorConn) Close() error {
	if err := c.Conn.Close(); err != nil {
		return err
	}
	return errors.New("injected close failure")
}

func TestSessionBootstrapDiagnosticWriteFailureDoesNotUndoAcknowledgement(t *testing.T) {
	server, client := net.Pipe()
	go serveSessionBootstrapResponse(server, `{"ok":true,"result":{"acknowledged":true}}`)
	execCalls := 0
	code := runSessionBootstrap(
		[]string{"/handler"}, sessionBootstrapGetenv, func() []string { return nil },
		func(path string) (string, error) { return path, nil },
		func(context.Context, string, string) (net.Conn, error) {
			return sessionBootstrapCloseErrorConn{Conn: client}, nil
		},
		func(string, []string, []string) error { execCalls++; return nil },
		sessionBootstrapFailWriter{},
	)
	if code != 0 || execCalls != 1 {
		t.Fatalf("code=%d exec=%d; diagnostic failure after acknowledgement must not suppress handler", code, execCalls)
	}
}

func TestSessionBootstrapConnectionCloseFailureIsDiagnosticAfterAcknowledgement(t *testing.T) {
	server, client := net.Pipe()
	go serveSessionBootstrapResponse(server, `{"ok":true,"result":{"acknowledged":true}}`)
	execCalls := 0
	var stderr strings.Builder
	code := runSessionBootstrap(
		[]string{"/handler"}, sessionBootstrapGetenv, func() []string { return nil },
		func(path string) (string, error) { return path, nil },
		func(context.Context, string, string) (net.Conn, error) {
			return sessionBootstrapCloseErrorConn{Conn: client}, nil
		},
		func(string, []string, []string) error { execCalls++; return nil },
		&stderr,
	)
	if code != 0 || execCalls != 1 || !strings.Contains(stderr.String(), "injected close failure") {
		t.Fatalf("code=%d exec=%d stderr=%q; post-response close failure must be reported without suppressing the handler", code, execCalls, stderr.String())
	}
}

func sessionBootstrapGetenv(name string) string {
	if name == sessionStartReceiptEnv {
		return sessionBootstrapReceipt
	}
	return "/coordinator.sock"
}

func serveSessionBootstrapResponse(conn net.Conn, response string) {
	defer conn.Close()
	var request daemon.SocketRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		return
	}
	if _, err := conn.Write([]byte(response)); err != nil {
		return
	}
}
