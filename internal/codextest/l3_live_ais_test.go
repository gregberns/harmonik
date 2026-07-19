package codextest_test

// L3 live tier for the INPUT driver (T9). The codex structured driver rides the
// SAME app-server wire protocol as the output reactor, whose live pre-deploy E2E
// gate is TestL3_HappyPathLive (l3_live_hkoe86p_test.go): a real codex subprocess
// completing one budget-capped turn. This file adds the input-driver-facing
// precondition canary — env-gated on CODEX_LIVE=1, it asserts the driver's launch
// dependency (a resolvable codex binary) is present, so `make test-codex-live`
// fails fast with a clear message before the heavier happy-path turn runs. It is
// SKIPPED by default (zero-token).

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
