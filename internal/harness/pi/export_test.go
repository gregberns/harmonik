package pi

// export_test.go — the pi test seams, relocated verbatim from
// internal/daemon/export_test.go by P2 unit E1c
// (plans/2026-07-21-p2-extraction/E1c-pi.md §4 step 9).
//
// A Go export_test.go seam is visible only inside its OWN package's test
// binary, so the eight pi test files that moved with the implementation could
// not keep calling daemon.ExportedPi*. Every shim below is a byte-for-byte copy
// of the daemon-side original with the qualifier dropped; the daemon-side
// copies are deleted in the same commit because their only consumers moved
// here. The two that daemon KEEPS (ExportedPiHarnessFields, ExportedNewPiHarness)
// are the ones whose consumers stayed behind.
//
// ExportedWorktreeHEADHasRefsTrailer forwards to internal/harness/shared there
// as it does here — same as internal/harness/codex/export_test.go.

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Refs:<bead> trailer fallback seams (hk-mazln PI-030/031)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedPiRefsOutcome mirrors the internal piRefsOutcome enum for tests.
type ExportedPiRefsOutcome = piRefsOutcome

// Exported piRefsOutcome constants for EnsureRefsTrailer assertions.
const (
	ExportedPiRefsAlreadyPresent = piRefsAlreadyPresent
	ExportedPiRefsAmended        = piRefsAmended
	ExportedPiRefsCommitted      = piRefsCommitted
	ExportedPiRefsNoChange       = piRefsNoChange
)

// ExportedWorktreeHEADHasRefsTrailer exposes shared.WorktreeHEADHasRefsTrailer
// (VERIFY). The nil argument is the tmux.CommandRunner — nil means bare local
// exec (NFR7), which is what these tests exercise.
//
// Bead ref: hk-bpxci.
func ExportedWorktreeHEADHasRefsTrailer(ctx context.Context, wtPath string, beadID core.BeadID) (bool, error) {
	return shared.WorktreeHEADHasRefsTrailer(ctx, nil, wtPath, beadID)
}

// ExportedEnsurePiRefsTrailer exposes EnsureRefsTrailer (VERIFY + deterministic
// commit-after-exit FALLBACK) for tests. Passes nil runner (local path) so tests
// exercise the byte-identical local-substrate behaviour (NFR7).
//
// Bead ref: hk-mazln.
func ExportedEnsurePiRefsTrailer(ctx context.Context, wtPath, parentSHA string, beadID core.BeadID) (ExportedPiRefsOutcome, error) {
	return EnsureRefsTrailer(ctx, nil, wtPath, parentSHA, beadID)
}

// ExportedEnsurePiRefsTrailerViaRunner exposes EnsureRefsTrailer with a
// caller-supplied runner so tests can exercise the runner-routed remote path
// (PI-031/PI-100). Use tmux.RecordingRunner with nil CmdFunc to run real git
// commands while recording every call.
//
// Bead ref: hk-ypxwl (PI-100).
func ExportedEnsurePiRefsTrailerViaRunner(ctx context.Context, runner tmuxPkg.CommandRunner, wtPath, parentSHA string, beadID core.BeadID) (ExportedPiRefsOutcome, error) {
	return EnsureRefsTrailer(ctx, runner, wtPath, parentSHA, beadID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Harness + NDJSON parser seams (hk-4rmj1 PI-010/012/013, hk-mkcwg PI-014)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewPiHarness re-exports NewHarness for tests in package pi_test.
//
// Bead ref: hk-4rmj1 (PI-010/012/013).
var ExportedNewPiHarness = NewHarness

// ExportedNewPiSessionIDInterceptor exposes newPiSessionIDInterceptor for
// tests in package pi_test.
//
// Bead ref: hk-4rmj1 (PI-012); hk-mkcwg (PI-014 agentEndCb param added).
func ExportedNewPiSessionIDInterceptor(inner io.Reader, sessionIDCb func(string), agentEndCb func()) io.Reader {
	return newPiSessionIDInterceptor(inner, sessionIDCb, agentEndCb)
}

// ExportedParsePiNDJSONEvent exposes parsePiNDJSONEvent for tests in package
// pi_test. Returns (kind, rawType, sessionID, usage, err).
//
// Bead ref: hk-4rmj1 (PI-012); hk-eval-prog-pi-tokens-sr316 (WS1d — usage added).
func ExportedParsePiNDJSONEvent(line []byte) (kind piEventKind, rawType, sessionID string, usage ExportedPiTokenUsage, err error) {
	ev, err := parsePiNDJSONEvent(line)
	return ev.Kind, ev.RawType, ev.SessionID, ExportedPiTokenUsage{InputTokens: ev.Usage.InputTokens, OutputTokens: ev.Usage.OutputTokens}, err
}

// ExportedPiTokenUsage is the exported shape of piTokenUsage for tests.
//
// Bead ref: hk-eval-prog-pi-tokens-sr316 (WS1d).
type ExportedPiTokenUsage struct {
	InputTokens  int64
	OutputTokens int64
}

// ExportedPiRunArtifacts is a type alias for piRunArtifacts so tests in package
// pi_test can inspect accumulated usage without exporting the internal type.
//
// Bead ref: hk-eval-prog-pi-tokens-sr316 (WS1d).
type ExportedPiRunArtifacts = piRunArtifacts

// ExportedCapturePiUsage exposes capturePiUsage for tests in package pi_test.
//
// Bead ref: hk-eval-prog-pi-tokens-sr316 (WS1d).
func ExportedCapturePiUsage(arts *piRunArtifacts, line []byte) bool {
	ev, err := parsePiNDJSONEvent(line)
	if err != nil {
		return false
	}
	return capturePiUsage(arts, ev)
}

// ExportedPiEventKindSession re-exports piEventKindSession for tests.
const ExportedPiEventKindSession = piEventKindSession

// ExportedPiEventKindAgentEnd re-exports piEventKindAgentEnd for tests.
const ExportedPiEventKindAgentEnd = piEventKindAgentEnd

// ExportedPiEventKindOther re-exports piEventKindOther for tests.
const ExportedPiEventKindOther = piEventKindOther

// ExportedPiEventKindMessageStart re-exports piEventKindMessageStart for tests.
const ExportedPiEventKindMessageStart = piEventKindMessageStart

// ExportedPiEventKindMessageEnd re-exports piEventKindMessageEnd for tests.
const ExportedPiEventKindMessageEnd = piEventKindMessageEnd

// ─────────────────────────────────────────────────────────────────────────────
// BuildLaunchSpec test seams (hk-1c16h PI-015/020/021)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedPiRunCtx is the exported shape of the pi per-launch run context.
//
// Bead ref: hk-1c16h.
type ExportedPiRunCtx = RunCtx

// ExportedBuildPiLaunchSpec exposes BuildLaunchSpec for tests in package
// pi_test.
//
// Bead ref: hk-1c16h.
var ExportedBuildPiLaunchSpec = BuildLaunchSpec

// ExportedBuildPiModelsJSON exposes buildPiModelsJSON for tests in package
// pi_test. Allows direct verification of the models.json content generated
// for locally-hosted OpenAI-compatible endpoints (hk-z13jz).
func ExportedBuildPiModelsJSON(provider, baseURL, api, apiKeyFile, apiKeyEnv, model string) ([]byte, error) {
	return buildPiModelsJSON(provider, baseURL, api, apiKeyFile, apiKeyEnv, model)
}

// ExportedRunCtxForPi builds a minimal handlercontract.RunCtx suitable for
// calling Harness.LaunchSpec in production-path tests (hk-z13jz).
// workspacePath is the run worktree; beadID is the bead correlation id.
// PriorSessionID is nil (initial turn). BaseEnv is PATH=/usr/bin.
func ExportedRunCtxForPi(workspacePath, beadID string) handlercontract.RunCtx {
	return handlercontract.RunCtx{
		WorkspacePath: workspacePath,
		BeadID:        beadID,
		BaseEnv:       []string{"PATH=/usr/bin"},
	}
}

// ExportedBuildPiEnv exposes buildPiEnv for tests in package pi_test.
// Allows direct verification of the allowlist-strip semantics (PI-021) and
// the file-first key resolution (PI-050/hk-xmfoi).
// Pass empty string for apiKeyFile when api_key_file is not configured.
//
// Bead ref: hk-1c16h.
func ExportedBuildPiEnv(baseEnv []string, apiKeyFile, apiKeyEnv string) []string {
	return buildPiEnv(baseEnv, apiKeyFile, apiKeyEnv)
}

// ExportedResolvePiAPIKeyValue exposes resolvePiAPIKeyValue for tests in
// package pi_test. Allows tests to verify the shared key-resolution helper
// applies file-first precedence (PI-050) and falls back to the env var (PI-021).
// Pass empty string for apiKeyFile when api_key_file is not configured.
//
// Bead ref: hk-1c16h.
func ExportedResolvePiAPIKeyValue(apiKeyFile, apiKeyEnv string) string {
	return resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)
}

// ─────────────────────────────────────────────────────────────────────────────
// Pi billing guard test seams (hk-l1bkp PI-040/042/043)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedRunPiBillingGuard exposes runPiBillingGuard for tests in package
// pi_test. piHome is forwarded as-is so tests can supply a fake home dir and
// exercise the PI-042 on-disk deny path without touching the real ~/.pi.
// Pass "" for apiKeyFile when api_key_file is not configured (uses env var only).
// Pass "" or piDefaultHome() for piHome for production-equivalent behavior.
//
// Bead ref: hk-l1bkp.
func ExportedRunPiBillingGuard(bus handlercontract.EventEmitter, beadID, apiKeyFile, apiKeyEnv, piHome string) error {
	return runPiBillingGuard(context.Background(), bus, core.RunID{}, beadID, apiKeyFile, apiKeyEnv, piHome)
}

// ExportedPiAuthIndicatesPersistentCredential exposes
// piAuthIndicatesPersistentCredential for tests in package pi_test. Allows
// direct testing of the PI-042 on-disk credential check with a controlled piHome.
//
// Bead ref: hk-l1bkp.
func ExportedPiAuthIndicatesPersistentCredential(piHome string) (bool, error) {
	return piAuthIndicatesPersistentCredential(piHome)
}
