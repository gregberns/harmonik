package daemon

// export_sandbox_session_test.go — test-seam exports for internal/daemon sandbox
// gating and session-context capture (RT19.19 split of export_test.go): the
// sandboxgate.go and sessioncontext_chb023.go seams. package daemon test file;
// see export_test.go header for the seam rationale. Bead: hk-ecrxy.

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// ExportedResolveGateAgentType exposes resolveGateAgentType for tests in package
// daemon_test. See sandboxgate.go for semantics (hk-r4p0l).
func ExportedResolveGateAgentType(implHarness handlercontract.Harness, fromArtifacts core.AgentType) core.AgentType {
	return resolveGateAgentType(implHarness, fromArtifacts)
}

// ExportedSandboxSpawnForRun exposes sandboxSpawnForRun for tests in package
// daemon_test, returning whether a SrtSpawnConfig would be attached (non-nil)
// for the given config + resolved agent type. See sandboxgate.go (hk-r4p0l).
func ExportedSandboxSpawnForRun(cfg projectconfig.SandboxConfig, agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig {
	return sandboxSpawnForRun(cfg, agentType, in)
}

// ExportedSandboxWrapExecArgv exposes sandboxWrapExecArgv for tests in package
// daemon_test — the EXEC-path srt argv-wrap applied to a SessionIDCaptured
// (pi) run's LaunchSpec (spec.Substrate==nil). Returns (binary, args) unchanged
// when spawn is nil (strict no-op). See sandboxgate.go (hk-r4p0l part 2).
func ExportedSandboxWrapExecArgv(spawn *SrtSpawnConfig, binary string, args []string) (wrappedBinary string, wrappedArgs []string, err error) {
	return sandboxWrapExecArgv(spawn, binary, args)
}

// ExportedVerifySandboxEngaged exposes verifySandboxEngaged for tests in
// package daemon_test — the production-path srt sandbox-engagement proof
// (hk-5wdon, follow-up to hk-tch4t). See sandboxgate.go for semantics.
func ExportedVerifySandboxEngaged(ctx context.Context, spawn *SrtSpawnConfig, canaryPath string, logf func(format string, args ...any)) error {
	return verifySandboxEngaged(ctx, spawn, canaryPath, logf)
}

// ExportedSrtEngagementCanaryPath exposes srtEngagementCanaryPath for tests in
// package daemon_test. See sandboxgate.go (hk-5wdon).
func ExportedSrtEngagementCanaryPath(projectDir, runID string) string {
	return srtEngagementCanaryPath(projectDir, runID)
}

// ExportedPersistClaudeSessionID exposes persistClaudeSessionID for tests.
//
// Bead ref: hk-w5vra.6.
func ExportedPersistClaudeSessionID(ctx context.Context, wtPath string, runID core.RunID, sessionID string) (commitSHA string, skipped bool, err error) {
	res, err := persistClaudeSessionID(ctx, wtPath, runID, sessionID)
	return res.CommitSHA, res.Skipped, err
}

// ExportedNewSessionIDInterceptor exposes newSessionIDInterceptor for tests.
//
// Bead ref: hk-w5vra.6.
func ExportedNewSessionIDInterceptor(r io.Reader, cb func(string)) io.Reader {
	return newSessionIDInterceptor(r, cb)
}

// ExportedSrtSpawnConfig is a type alias for SrtSpawnConfig so tests in
// package daemon_test can reference the type without importing internal symbols.
//
// Bead: hk-rlxgx.
type ExportedSrtSpawnConfig = SrtSpawnConfig
