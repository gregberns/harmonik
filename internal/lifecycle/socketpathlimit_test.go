package lifecycle

import (
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

	max := sunPathMax()
	// Build a path well beyond the limit; TestValidateSocketPathLength_ExactlyAtLimit
	// covers the true boundary (len == max vs len == max-1).
	long := "/" + strings.Repeat("a", max) + "/.harmonik/daemon.sock"
	err := ValidateSocketPathLength(long)
	if err == nil {
		t.Fatalf("ValidateSocketPathLength(%d-byte path) = nil, want error (max=%d)", len(long), max)
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

	max := sunPathMax()

	atLimit := strings.Repeat("a", max)
	if err := ValidateSocketPathLength(atLimit); err == nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = nil, want error (max=%d, no room for NUL)", len(atLimit), max)
	}

	underLimit := strings.Repeat("a", max-1)
	if err := ValidateSocketPathLength(underLimit); err != nil {
		t.Errorf("ValidateSocketPathLength(len=%d) = %v, want nil (max=%d)", len(underLimit), err, max)
	}
}
