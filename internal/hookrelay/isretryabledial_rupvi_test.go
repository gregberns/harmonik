package hookrelay_test

// isretryabledial_rupvi_test.go — hk-rupvi: isRetryableDialErr must disambiguate
// ECONNREFUSED for the unix transport by stat, so a non-socket misconfiguration
// is FATAL (not retried for the whole startup window) on EVERY platform. Dialing
// a regular file as a unix socket returns ENOTSOCK on darwin (already fatal) but
// ECONNREFUSED on linux (was retried → #31 Tier2 red). These unit tests feed a
// synthetic ECONNREFUSED so the linux fatal-path is proven on darwin too.

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

	// A path that EXISTS and is NOT a socket (regular file). unix + ECONNREFUSED
	// here is a fatal misconfiguration — must NOT be retryable (the hk-rupvi bug:
	// on linux the real dial returns exactly this errno).
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

	// (1) ENOENT — socket not created yet — retryable regardless of transport.
	if !hookrelay.ExportedIsRetryableDialErr("unix", "/no/such/socket", syscall.ENOENT) {
		t.Error("ENOENT (socket not created yet) must stay retryable")
	}

	// (2) unix + ECONNREFUSED on an ABSENT path — stat fails, so stay retryable
	// (conservative; a genuine not-yet-created socket).
	if !hookrelay.ExportedIsRetryableDialErr("unix", filepath.Join(t.TempDir(), "absent.sock"), syscall.ECONNREFUSED) {
		t.Error("unix + ECONNREFUSED on an absent path must stay retryable")
	}

	// (3) unix + ECONNREFUSED on a REAL socket file (present but treated as
	// not-listening) — the genuine cold-boot race — must stay retryable. Keep the
	// listener open so the socket file exists AND is a socket. Use a SHORT /tmp
	// dir: a t.TempDir() path can exceed the 104-byte sun_path limit and fail bind.
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
	defer func() { _ = ln.Close() }() //nolint:errcheck // test cleanup, unactionable
	if fi, statErr := os.Stat(sockPath); statErr != nil || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("fixture: %q is not a socket (stat err=%v)", sockPath, statErr)
	}
	if !hookrelay.ExportedIsRetryableDialErr("unix", sockPath, syscall.ECONNREFUSED) {
		t.Error("unix + ECONNREFUSED on a REAL socket file must stay retryable (socket-not-listening race)")
	}

	// (4) tcp + ECONNREFUSED — no filesystem path to stat — stays retryable
	// (listener still starting). TCP transport is unchanged by the fix.
	if !hookrelay.ExportedIsRetryableDialErr("tcp", "127.0.0.1:0", syscall.ECONNREFUSED) {
		t.Error("tcp + ECONNREFUSED must stay retryable (listener starting)")
	}
}

func TestIsRetryableDialErr_OtherErrorsFatal_rupvi(t *testing.T) {
	t.Parallel()

	// A non-ECONNREFUSED / non-ENOENT error is fatal — including the darwin
	// non-socket errno (ENOTSOCK), which the prior code already treated as fatal.
	if hookrelay.ExportedIsRetryableDialErr("unix", "/anything", syscall.ENOTSOCK) {
		t.Error("ENOTSOCK must be fatal (not retryable)")
	}
	if hookrelay.ExportedIsRetryableDialErr("unix", "/anything", syscall.EPERM) {
		t.Error("EPERM must be fatal (not retryable)")
	}
}
