package daemon_test

// pilaunchspec_pi042_via_spec_hk6g5iu_test.go — PI-042 deny path testable
// through ExportedBuildPiLaunchSpec via injectable piHome (hk-6g5iu).
//
// RED (before fix): ExportedPiRunCtx has no PiHome field — compilation failure.
// GREEN (after fix): piRunCtx gains piHome; build passes; billing guard denies
// the launch when auth.json carries an api_key.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestBuildPiLaunchSpec_PI042_DeniesViaInjectableHome verifies that the PI-042
// deny path (persistent on-disk credential) is exercisable end-to-end through
// ExportedBuildPiLaunchSpec when a controlled piHome is supplied.
//
// Fragility closed: previously the billing guard in buildPiLaunchSpec hardcoded
// piDefaultHome(), making this path unreachable in tests — the denial had to be
// tested only via ExportedRunPiBillingGuard, leaving the buildPiLaunchSpec seam
// untested. Bead: hk-6g5iu.
func TestBuildPiLaunchSpec_PI042_DeniesViaInjectableHome(t *testing.T) {
	const key = "TEST_HK6G5IU_PI042_VIA_SPEC"
	t.Setenv(key, "sk-or-test-key-not-empty")

	piHome := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(piHome, "auth.json"),
		[]byte(`{"api_key":"sk-or-persisted-credential"}`),
		0o600,
	); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}

	rc := daemon.ExportedPiRunCtx{
		WorkspacePath: t.TempDir(),
		BeadID:        "hk-pi042-via-spec-sentinel",
		Provider:      "openrouter",
		Model:         "openrouter/qwen/qwen3-coder",
		APIKeyEnv:     key,
		BaseEnv:       []string{"PATH=/usr/bin"},
		PiHome:        piHome, // injectable: triggers PI-042 denial
	}

	if _, err := daemon.ExportedBuildPiLaunchSpec(rc); err == nil {
		t.Fatal("expected PI-042 billing guard denial when auth.json carries api_key; got nil error")
	}
}
