package hookrelay_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gregberns/harmonik/internal/hookrelay"
)

func TestIsRetryableDialErr_UnixNonSocketIsFatal_rupvi(t *testing.T) {
	t.Parallel()

	regular := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(regular, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if hookrelay.ExportedIsRetryableDialErr("unix", regular, syscall.ECONNREFUSED) {
		t.Error("unix + ECONNREFUSED on a REGULAR FILE must be FATAL (return false), not retried — hk-rupvi")
	}
}

func TestIsRetryableDialErr_RetryableCases_rupvi(t *testing.T) {
	t.Parallel()

	if !hookrelay.ExportedIsRetryableDialErr("unix", "/no/such/socket", syscall.ENOENT) {
		t.Error("ENOENT (socket not created yet) must stay retryable")
	}

	if !hookrelay.ExportedIsRetryableDialErr("unix", filepath.Join(t.TempDir(), "absent.sock"), syscall.ECONNREFUSED) {
		t.Error("unix + ECONNREFUSED on an absent path must stay retryable")
	}

	sockDir, err := os.MkdirTemp("/tmp", "rupvi-sock-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(sockDir) }() //nolint:errcheck // test cleanup, unactionable
	sockPath := filepath.Join(sockDir, "d.sock")
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if fi, statErr := os.Stat(sockPath); statErr != nil || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("fixture: %q is not a socket (stat err=%v)", sockPath, statErr)
	}
	if !hookrelay.ExportedIsRetryableDialErr("unix", sockPath, syscall.ECONNREFUSED) {
		t.Error("unix + ECONNREFUSED on a REAL socket file must stay retryable (socket-not-listening race)")
	}

	if !hookrelay.ExportedIsRetryableDialErr("tcp", "127.0.0.1:0", syscall.ECONNREFUSED) {
		t.Error("tcp + ECONNREFUSED must stay retryable (listener starting)")
	}
}

func TestIsRetryableDialErr_OtherErrorsFatal_rupvi(t *testing.T) {
	t.Parallel()

	if hookrelay.ExportedIsRetryableDialErr("unix", "/anything", syscall.ENOTSOCK) {
		t.Error("ENOTSOCK must be fatal (not retryable)")
	}
	if hookrelay.ExportedIsRetryableDialErr("unix", "/anything", syscall.EPERM) {
		t.Error("EPERM must be fatal (not retryable)")
	}
}
