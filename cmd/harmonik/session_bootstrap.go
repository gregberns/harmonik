package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/dispatch"
)

const (
	sessionStartReceiptEnv  = "HARMONIK_SESSION_START_RECEIPT"
	sessionBootstrapTimeout = 25 * time.Second
)

type (
	sessionBootstrapDialFn              func(context.Context, string, string) (net.Conn, error)
	sessionBootstrapExecFn              func(string, []string, []string) error
	sessionBootstrapResolveExecutableFn func(string) (string, error)
)

func runSessionBootstrap(args []string, getenv func(string) string, environ func() []string, resolveExecutable sessionBootstrapResolveExecutableFn, dial sessionBootstrapDialFn, execHandler sessionBootstrapExecFn, stderr io.Writer) int {
	if len(args) == 0 {
		if _, err := fmt.Fprintln(stderr, "harmonik session-bootstrap: handler command is required"); err != nil {
			return 1
		}
		return 2
	}
	reportDiagnostic := func(err error) error {
		_, writeErr := fmt.Fprintf(stderr, "harmonik session-bootstrap: %v\n", err)
		return writeErr
	}
	if err := executeSessionBootstrap(args, getenv, environ, resolveExecutable, dial, execHandler, reportDiagnostic); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "harmonik session-bootstrap: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	return 0
}

func executeSessionBootstrap(args []string, getenv func(string) string, environ func() []string, resolveExecutable sessionBootstrapResolveExecutableFn, dial sessionBootstrapDialFn, execHandler sessionBootstrapExecFn, reportDiagnostic func(error) error) error {
	resolvedExecutable, err := resolveExecutable(args[0])
	if err != nil {
		return fmt.Errorf("resolve handler before acknowledgement: %w", err)
	}
	receiptJSON := getenv(sessionStartReceiptEnv)
	var receipt dispatch.SessionStartReceipt
	if err := json.Unmarshal([]byte(receiptJSON), &receipt); err != nil {
		return fmt.Errorf("invalid session receipt: %w", err)
	}
	canonicalReceipt, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("encode session receipt: %w", err)
	}
	cleanupErr, err := acknowledgeSessionBootstrap(getenv("HARMONIK_DAEMON_SOCKET"), canonicalReceipt, dial)
	if err != nil {
		return errors.Join(err, cleanupErr)
	}
	if cleanupErr != nil {
		reportCleanupDiagnostic(reportDiagnostic, cleanupErr)
	}
	childEnv := withoutEnvironmentName(environ(), sessionStartReceiptEnv)
	childArgv := slices.Clone(args)
	childArgv[0] = resolvedExecutable
	if err := execHandler(resolvedExecutable, childArgv, childEnv); err != nil {
		return fmt.Errorf("start handler: %w", err)
	}
	return nil
}

func reportCleanupDiagnostic(report func(error) error, cleanupErr error) {
	if err := report(cleanupErr); err != nil {
		return
	}
}

func acknowledgeSessionBootstrap(endpoint string, canonicalReceipt []byte, dial sessionBootstrapDialFn) (cleanupErr, resultErr error) {
	network, address, err := sessionBootstrapDialTarget(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionBootstrapTimeout)
	defer cancel()
	conn, err := dial(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("connect to coordinator: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			cleanupErr = fmt.Errorf("close coordinator connection: %w", err)
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(sessionBootstrapTimeout)); err != nil {
		return nil, fmt.Errorf("set coordinator deadline: %w", err)
	}
	request, err := json.Marshal(daemon.SocketRequest{Op: "session-start-ack", Payload: canonicalReceipt})
	if err != nil {
		return nil, fmt.Errorf("encode acknowledgement: %w", err)
	}
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("send acknowledgement: %w", err)
	}
	if closeWriter, ok := conn.(interface{ CloseWrite() error }); ok {
		if err := closeWriter.CloseWrite(); err != nil {
			return nil, fmt.Errorf("finish acknowledgement request: %w", err)
		}
	}
	var response sessionBootstrapResponse
	decoder := json.NewDecoder(bufio.NewReader(conn))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("read acknowledgement: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("acknowledgement must contain one JSON value")
	}
	if !response.OK || !response.Result.Acknowledged {
		if response.Error == "" {
			response.Error = "coordinator did not acknowledge the exact session"
		}
		return nil, errors.New(response.Error)
	}
	return nil, nil
}

type sessionBootstrapResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Acknowledged bool `json:"acknowledged"`
	} `json:"result"`
	Error string `json:"error,omitempty"`
}

func sessionBootstrapDialTarget(endpoint string) (network, address string, err error) {
	if endpoint == "" {
		return "", "", errors.New("HARMONIK_DAEMON_SOCKET is required")
	}
	if strings.HasPrefix(endpoint, "tcp://") {
		address := strings.TrimPrefix(endpoint, "tcp://")
		if address == "" {
			return "", "", errors.New("HARMONIK_DAEMON_SOCKET has an empty TCP address")
		}
		return "tcp", address, nil
	}
	return "unix", endpoint, nil
}

func withoutEnvironmentName(env []string, name string) []string {
	prefix := name + "="
	clean := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			clean = append(clean, entry)
		}
	}
	return clean
}

func sessionBootstrapDial(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func resolveSessionBootstrapExecutable(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("handler path must be absolute and resolved by the daemon")
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("handler path is not executable: %w", err)
	}
	return filepath.Clean(resolved), nil
}

func sessionBootstrapExec(path string, argv, env []string) error {
	return syscall.Exec(path, argv, env)
}
