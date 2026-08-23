package codextest_test

import (
	"os"
	"testing"
)

// TestL3AIS_DriverLaunchPrecondition asserts the codex binary the structured
// input driver launches is resolvable. Gated on CODEX_LIVE=1.
func TestL3AIS_DriverLaunchPrecondition(t *testing.T) {
	skipUnlessLive(t)
	bin := codexBinaryPath(t)
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("codex binary %q not usable for the structured input driver: %v", bin, err)
	}
	t.Logf("L3 input-driver precondition: codex binary resolved at %s", bin)
}
