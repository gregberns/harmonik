package lifecycle

import (
	"net"
	"os"
	"strings"
	"testing"
)

// TestValidateSocketPathLength_Fits verifies a normal-length project path
// (well under the platform sun_path limit) passes with no error.
//
// Bead ref: hk-ta6dg.
func TestValidateSocketPathLength_Fits(t *testing.T) {
	t.Parallel()

	short := "/tmp/hk-ta6dg/.harmonik/daemon.sock"
	if err := ValidateSocketPathLength(short); err != nil {
		t.Fatalf("ValidateSocketPathLength(%q) = %v, want nil (len=%d, max=%d)", short, err, len(short), sunPathMax())
	}
}

// TestValidateSocketPathLength_TooLong verifies a deeply-nested projectDir
// whose resulting daemon.sock path is at or beyond the platform sun_path
// limit fails loud with a descriptive error, rather than silently degrading
// (PL-003's existing non-fatal-bind-error path — see daemon.go — otherwise
// swallows exactly this failure).
//
// Bead ref: hk-ta6dg.
func TestValidateSocketPathLength_TooLong(t *testing.T) {
	t.Parallel()

	limit := sunPathMax()
	// Build a path well beyond the limit; TestValidateSocketPathLength_ExactlyAtLimit
	// covers the true boundary (len == limit vs len == limit-1).
	long := "/" + strings.Repeat("a", limit) + "/.harmonik/daemon.sock"
	err := ValidateSocketPathLength(long)
	if err == nil {
		t.Fatalf("ValidateSocketPathLength(%d-byte path) = nil, want error (max=%d)", len(long), limit)
	}
	if !strings.Contains(err.Error(), "sun_path") {
		t.Errorf("error %q does not mention sun_path", err.Error())
	}
}

// TestValidateSocketPathLength_ExactlyAtLimit verifies the boundary: a path
// whose length equals sunPathMax leaves no room for the NUL terminator and
// must be rejected, while max-1 (with room for the NUL) must pass.
//
// Bead ref: hk-ta6dg.
func TestValidateSocketPathLength_ExactlyAtLimit(t *testing.T) {
	t.Parallel()

	limit := sunPathMax()

	atLimit := strings.Repeat("a", limit)
	if err := ValidateSocketPathLength(atLimit); err == nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = nil, want error (max=%d, no room for NUL)", len(atLimit), limit)
	}

	underLimit := strings.Repeat("a", limit-1)
	if err := ValidateSocketPathLength(underLimit); err != nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = %v, want nil (max=%d)", len(underLimit), err, limit)
	}
}

// TestValidateSocketPathLength_AgreesWithTheKernel binds real Unix sockets on
// both sides of the boundary and checks the operating system draws the line in
// the same place.
//
// The three tests above compare the validator with its own constant, so they
// stay green even if the constant is wrong. This one asks the kernel instead. A
// path of exactly sunPathMax bytes must fail to bind, because one byte of
// sun_path holds the NUL terminator. One byte shorter must succeed.
//
// This boundary is worth pinning to the platform. Two hand-rolled copies of the
// rule in internal/daemon admitted the at-limit case, and three daemon tests
// then failed about five runs in six for a reason that had nothing to do with
// the code under test (hk-m3jai).
func TestValidateSocketPathLength_AgreesWithTheKernel(t *testing.T) {
	t.Parallel()

	limit := sunPathMax()
	root, err := os.MkdirTemp("/tmp", "sp-")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) //nolint:errcheck // cleanup error unactionable

	// pathOfLength pads a filename under root so the whole path is exactly n bytes.
	pathOfLength := func(n int) string {
		prefix := root + "/"
		if len(prefix) >= n {
			t.Fatalf("temp root %q is already %d bytes, so a %d-byte path cannot be built", root, len(prefix), n)
		}
		return prefix + strings.Repeat("s", n-len(prefix))
	}

	lc := &net.ListenConfig{}

	atLimit := pathOfLength(limit)
	if ln, bindErr := lc.Listen(t.Context(), "unix", atLimit); bindErr == nil {
		_ = ln.Close()
		t.Errorf("the kernel bound a %d-byte socket path, so the sun_path limit here is not %d", len(atLimit), limit)
	}
	if lenErr := ValidateSocketPathLength(atLimit); lenErr == nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = nil, but the kernel refuses that path", len(atLimit))
	}

	underLimit := pathOfLength(limit - 1)
	ln, bindErr := lc.Listen(t.Context(), "unix", underLimit)
	if bindErr != nil {
		t.Errorf("the kernel refused a %d-byte socket path: %v; the validator calls %d bytes usable", len(underLimit), bindErr, limit-1)
	} else {
		_ = ln.Close()
	}
	if lenErr := ValidateSocketPathLength(underLimit); lenErr != nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = %v, but the kernel accepts that path", len(underLimit), lenErr)
	}
}
