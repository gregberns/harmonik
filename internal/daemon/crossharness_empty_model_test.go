package daemon_test

// crossharness_empty_model_test.go — cross-harness empty-model asymmetry lock
// (GAP-4).
//
// # Why this asymmetry exists (and MUST be preserved)
//
// An empty model resolves to DIFFERENT behavior per harness, and that difference
// is deliberate — not an inconsistency to be "fixed" by making both branches
// match:
//
//   - Codex: an empty model is VALID. buildCodexLaunchSpec omits the --model flag
//     so codex resolves the model from $CODEX_HOME/config.toml — the account
//     default. This is the ONLY working configuration on the HN-022-mandated
//     ChatGPT-subscription auth path, where every explicitly-named model 400s
//     ("not supported when using Codex with a ChatGPT account"). Codex has an
//     account-default fallback, so no model on the command line is runnable.
//
//   - Pi: an empty model is a HARD ERROR. The pi argv is
//     `--provider <p> --model <p/id>` with NO account-default fallback — an empty
//     model is structurally un-runnable for pi, so buildPiLaunchSpec fails loud
//     (pointing at the missing harnesses.pi.model config) rather than launching a
//     broken invocation.
//
// If a future refactor made codex fail-loud on empty (re-adding the retired
// hk-heh3t guard) OR made pi tolerate empty, this test breaks — which is the
// intent: the two branches must never silently converge.
//
// Bead refs: hk-d170r (codex empty→account-default), hk-heh3t (retired codex guard).

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

	// ── Codex half: empty model → no error, argv omits --model. ──────────────
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

	// ── Pi half: empty model with a provider → hard error naming the config. ──
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
