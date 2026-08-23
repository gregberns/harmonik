package daemon_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestCrossHarness_EmptyModelAsymmetry locks the deliberate codex-vs-pi split on
// an empty model: codex omits --model and succeeds (account default); pi fails
// loud pointing at harnesses.pi.model. See the file-level comment for rationale.
func TestCrossHarness_EmptyModelAsymmetry(t *testing.T) {
	t.Parallel()

	t.Run("codex empty model omits --model and succeeds", func(t *testing.T) {
		t.Parallel()
		rc := daemon.ExportedCodexRunCtx{
			WorkspacePath: "/tmp/wt-crossharness-codex-nomodel",
			BeadID:        "hk-crossharness-codex",
			// Model deliberately empty → account-default (no --model flag).
			SkipBillingGuard: true,
		}
		spec, err := daemon.ExportedBuildCodexLaunchSpec(rc)
		if err != nil {
			t.Fatalf("codex empty model must NOT error (account-default path); got: %v", err)
		}
		for _, arg := range spec.Args {
			if arg == "--model" {
				t.Errorf("codex empty-model argv must omit --model; got %v", spec.Args)
				return
			}
		}
	})

	t.Run("pi empty model with provider errors", func(t *testing.T) {
		t.Parallel()
		rc := daemon.ExportedPiRunCtx{
			WorkspacePath:    "/tmp/wt-crossharness-pi-nomodel",
			BeadID:           "hk-crossharness-pi",
			Provider:         "openrouter",
			Model:            "", // empty → un-runnable for pi (no account-default fallback)
			APIKeyEnv:        "OPENROUTER_API_KEY",
			SkipBillingGuard: true,
		}
		_, err := daemon.ExportedBuildPiLaunchSpec(rc)
		if err == nil {
			t.Fatal("pi empty model MUST error (no account-default fallback); got nil")
		}
		if !strings.Contains(err.Error(), "harnesses.pi.model") {
			t.Errorf("pi empty-model error must name the missing config 'harnesses.pi.model'; got: %v", err)
		}
	})
}
