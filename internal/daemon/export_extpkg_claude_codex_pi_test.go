package daemon

// export_extpkg_claude_codex_pi_test.go — thin re-exports of the already-extracted
// claude / codex / pi harness packages (RT19.15a split of export_test.go). These
// are RETAINED shims for STAYING daemon_test files that assert daemon-composition
// claims across harnesses. package daemon test file; see export_test.go header for
// the seam rationale. Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

// ExportedClaudeRunCtx is the launch DTO for tests in package daemon_test.
//
// It used to be a hand-maintained mirror struct plus a field-by-field
// translation in every builder below. P2 unit E1b-prep moved the real type out
// of internal/daemon into internal/harness/shared (where it belongs — it is the
// universal launch DTO, fed to codex and pi as well as claude), so the mirror
// collapsed into a plain alias and the translations became passthroughs.
//
// Bead ref: hk-gql20.13, hk-xo03m.
type ExportedClaudeRunCtx = shared.LaunchCtx

// ExportedClaudeRunArtifacts is the post-launch artifact bundle for tests in
// package daemon_test. Alias of the real type for the same reason as
// ExportedClaudeRunCtx above.
//
// Bead ref: hk-gql20.13.
type ExportedClaudeRunArtifacts = shared.LaunchArtifacts

// ExportedBuildClaudeLaunchSpec exposes claude.BuildLaunchSpec for tests in
// package daemon_test. Since E1b-prep the exported and internal DTOs are the
// same type, so this is a plain passthrough.
//
// RETAINED after P2 E1b: the builder itself moved to internal/harness/claude,
// but nine STAYING daemon test files still call this shim to assert
// daemon-composition claims (routed-vs-direct spec parity, harness pinning,
// review-loop resume, CHB-024 settings shadowing, the DOT prompt path). Those
// are claims about the daemon's wiring, not about the claude unit, so they do
// not move with it.
//
// Bead ref: hk-gql20.13.
func ExportedBuildClaudeLaunchSpec(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	return claude.BuildLaunchSpec(ctx, rc)
}

// ExportedCodexRunCtx is the exported shape of the codex per-launch run context.
//
// Bead refs: hk-rgxwd (T7), hk-tu48u (T11 billing-guard fields), hk-heh3t (model guard).
type ExportedCodexRunCtx = codex.RunCtx

// ExportedBuildCodexLaunchSpec exposes codex.BuildLaunchSpec for tests in
// package daemon_test.
//
// Bead ref: hk-rgxwd.
var ExportedBuildCodexLaunchSpec = codex.BuildLaunchSpec

// ExportedNewClaudeHarness re-exports claude.NewHarness for tests in package
// daemon_test.
//
// RETAINED after P2 E1b for the same reason as ExportedNewCodexHarness below:
// regression_golden_no_selection_hkhwwlk_test.go asserts, from package
// daemon_test, that the claude harness is what a no-selection bead resolves to
// and that its Completion() mode discriminates from codex's — a
// daemon-composition claim, not a claude-unit claim.
//
// Bead ref: hk-3kyh3.
var ExportedNewClaudeHarness = claude.NewHarness

// ExportedPiHarnessFields returns the provider, model, apiKeyEnv and apiKeyFile
// of a pi.Harness for test assertions on the config→harness seam (hk-f8u5j;
// apiKeyFile added by hk-xmfoi). P2 unit E1c moved pi.Harness out of this
// package, so the fields are read through the harness accessors instead of
// directly.
func ExportedPiHarnessFields(h *pi.Harness) (provider, model, apiKeyEnv, apiKeyFile string) {
	return h.Provider(), h.Model(), h.APIKeyEnv(), h.APIKeyFile()
}

// ExportedNewCodexHarness re-exports codex.NewHarness for tests in package
// daemon_test.
//
// Bead ref: hk-m57va.
var ExportedNewCodexHarness = codex.NewHarness

// ExportedNewPiHarness re-exports pi.NewHarness for tests in package
// daemon_test. Three STAYING daemon tests construct a pi harness directly
// (sandboxgate_hkr4p0l_test.go, hk_lfrub_dot_node_model_leak_test.go,
// hk_pkugu_pi_model_leak_test.go), so this shim survives P2 unit E1c.
//
// Bead ref: hk-4rmj1 (PI-010/012/013).
var ExportedNewPiHarness = pi.NewHarness

// ExportedPiRunCtx is the exported shape of the pi per-launch run context.
// P2 unit E1c moved it to internal/harness/pi; this is now an alias, kept
// because two cross-harness parity tables in package daemon_test
// (crossharness_empty_model_test.go, crossharness_seedprompt_test.go) assert
// about codex AND pi in one table each and therefore cannot move.
//
// Bead ref: hk-1c16h.
type ExportedPiRunCtx = pi.RunCtx

// ExportedBuildPiLaunchSpec exposes pi.BuildLaunchSpec for tests in package
// daemon_test.
//
// Bead ref: hk-1c16h.
var ExportedBuildPiLaunchSpec = pi.BuildLaunchSpec
