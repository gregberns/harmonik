package codex

// export_test.go — the codex test seams, relocated verbatim from
// internal/daemon/export_test.go by P2 unit E1a-1
// (plans/2026-07-21-p2-extraction/E1a-codex-harness.md §4 step 13).
//
// Go's export_test.go seams are visible ONLY inside their own package's test
// binary, so the daemon's copy could not serve the codex tests once those tests
// became package codex_test. Every seam below is byte-equivalent to the daemon
// original apart from the identifier renames the move required, which is why the
// seven relocated external test files are an import-path edit rather than a
// rewrite.
//
// Four seams did NOT come along, because their consumers stayed in the daemon:
// ExportedCodexRunCtx / ExportedBuildCodexLaunchSpec / ExportedNewCodexHarness
// still exist in internal/daemon/export_test.go as thin aliases onto this
// package's now-exported RunCtx / BuildLaunchSpec / NewHarness, and
// ExportedWorktreeHEADHasRefsTrailer forwards to internal/harness/shared there
// as it does here.

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

// ─────────────────────────────────────────────────────────────────────────────
// BuildLaunchSpec test seams (hk-rgxwd C2/T7)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedCodexRunCtx is the exported shape of the per-launch run context.
//
// It used to be a hand-maintained mirror of the daemon-private codexRunCtx. P2
// E1a-1 exported RunCtx itself (§3b: the daemon's own export_test.go needs a
// type it can alias), so the mirror collapses to an alias and the field names
// the tests already use are unchanged.
//
// Bead refs: hk-rgxwd (T7), hk-tu48u (T11 billing-guard fields), hk-heh3t (model guard).
type ExportedCodexRunCtx = RunCtx

// ExportedBuildCodexLaunchSpec exposes BuildLaunchSpec for tests in package
// codex_test.
//
// Bead ref: hk-rgxwd.
var ExportedBuildCodexLaunchSpec = BuildLaunchSpec

// ─────────────────────────────────────────────────────────────────────────────
// codex billing guard test seams (hk-tu48u C3/T11)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedMaterializeForcedLoginMethod exposes materializeForcedLoginMethod for
// tests in package codex_test.
//
// Bead ref: hk-tu48u.
func ExportedMaterializeForcedLoginMethod(codexHome string) error {
	return materializeForcedLoginMethod(codexHome)
}

// ExportedAssertChatGPTPlan exposes the fail-closed assertChatGPTPlan for tests
// in package codex_test.
//
// Bead ref: hk-tu48u.
func ExportedAssertChatGPTPlan(codexHome string) error {
	return assertChatGPTPlan(codexHome)
}

// ExportedRunCodexBillingGuard exposes runCodexBillingGuard (materialize + assert
// + emit) for tests in package codex_test.
//
// Bead ref: hk-tu48u.
func ExportedRunCodexBillingGuard(bus handlercontract.EventEmitter, beadID, codexHome string) error {
	return runCodexBillingGuard(context.Background(), bus, core.RunID{}, beadID, codexHome)
}

// ExportedForcedLoginMethodValue is the value the guard materializes / asserts.
// Bead ref: hk-tu48u.
const ExportedForcedLoginMethodValue = forcedLoginMethodValue

// ─────────────────────────────────────────────────────────────────────────────
// Harness + codex JSONL parser test seams (hk-m57va C2/T8)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewCodexHarness re-exports NewHarness for tests in package codex_test.
//
// Bead ref: hk-m57va.
var ExportedNewCodexHarness = NewHarness

// ExportedCodexEventKind mirrors the internal codexEventKind enum for tests.
type ExportedCodexEventKind = codexEventKind

// Exported codexEventKind constants for table-driven parser tests.
const (
	ExportedCodexEventKindOther         = EventKindOther
	ExportedCodexEventKindThreadStarted = EventKindThreadStarted
	ExportedCodexEventKindTurnStarted   = EventKindTurnStarted
	ExportedCodexEventKindTurnCompleted = EventKindTurnCompleted
	ExportedCodexEventKindTurnFailed    = EventKindTurnFailed
)

// ExportedCodexEvent is the exported projection of the parsed codexEvent for
// test assertions.
type ExportedCodexEvent struct {
	Kind         ExportedCodexEventKind
	RawType      string
	ThreadID     string
	TurnID       string
	ErrorMessage string
	InputTokens  int
	OutputTokens int
}

// ExportedParseCodexJSONLEvent exposes parseCodexJSONLEvent for tests, returning
// the exported event projection.
//
// Bead ref: hk-m57va.
func ExportedParseCodexJSONLEvent(line []byte) (ExportedCodexEvent, error) {
	ev, err := parseCodexJSONLEvent(line)
	if err != nil {
		return ExportedCodexEvent{}, err
	}
	return ExportedCodexEvent{
		Kind:         ev.Kind,
		RawType:      ev.RawType,
		ThreadID:     ev.ThreadID,
		TurnID:       ev.TurnID,
		ErrorMessage: ev.ErrorMessage,
		InputTokens:  ev.Usage.InputTokens,
		OutputTokens: ev.Usage.OutputTokens,
	}, nil
}

// ExportedCodexRunArtifacts is the exported projection of codexRunArtifacts for
// thread-id-capture tests.
type ExportedCodexRunArtifacts struct {
	CapturedThreadID   string
	TurnCompleted      bool
	TurnFailed         bool
	TurnFailureMessage string
	InputTokens        int
	OutputTokens       int
}

// ExportedCaptureCodexThreadStream folds an ordered slice of raw JSONL lines
// through parseCodexJSONLEvent + captureCodexThreadID and returns the resulting
// run artifacts. Malformed lines are surfaced as an error (the production stream
// reader skips them, but tests assert exact behaviour). This exercises the
// thread-id capture-into-run-state requirement of T8.
//
// Bead ref: hk-m57va.
func ExportedCaptureCodexThreadStream(lines [][]byte) (ExportedCodexRunArtifacts, error) {
	var arts codexRunArtifacts
	for _, line := range lines {
		ev, err := parseCodexJSONLEvent(line)
		if err != nil {
			return ExportedCodexRunArtifacts{}, err
		}
		captureCodexThreadID(&arts, ev)
	}
	return ExportedCodexRunArtifacts{
		CapturedThreadID:   arts.capturedThreadID,
		TurnCompleted:      arts.turnCompleted,
		TurnFailed:         arts.turnFailed,
		TurnFailureMessage: arts.turnFailureMessage,
		InputTokens:        arts.inputTokens,
		OutputTokens:       arts.outputTokens,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// codex Refs:<bead> trailer guarantee test seams (hk-bpxci C2/T9)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedCodexRefsOutcome mirrors the shared.RefsOutcome enum for tests. The
// enum moved to internal/harness/shared in P2 unit E1a-0; the seam name is kept
// so the existing test files compile unchanged.
type ExportedCodexRefsOutcome = shared.RefsOutcome

// Exported shared.RefsOutcome constants for EnsureRefsTrailer assertions.
const (
	ExportedCodexRefsAlreadyPresent = shared.RefsAlreadyPresent
	ExportedCodexRefsAmended        = shared.RefsAmended
	ExportedCodexRefsCommitted      = shared.RefsCommitted
	ExportedCodexRefsNoChange       = shared.RefsNoChange
)

// ExportedCodexNoWorkDurationFloorDefault exposes the default no-work duration
// floor so tests can assert against the shipped value rather than restating it.
//
// Bead ref: hk-368i4.
const ExportedCodexNoWorkDurationFloorDefault = codexNoWorkDurationFloorDefault

// ExportedCodexNoWorkSuspected exposes NoWorkSuspected — the hk-368i4
// detector pairing a shared.RefsNoChange outcome with a sub-floor phase duration.
//
// Bead ref: hk-368i4.
func ExportedCodexNoWorkSuspected(outcome ExportedCodexRefsOutcome, phaseDuration, floorOverride time.Duration) bool {
	return NoWorkSuspected(outcome, phaseDuration, floorOverride)
}

// ExportedCodexNoWorkFloor exposes NoWorkFloor (override resolution).
//
// Bead ref: hk-368i4.
func ExportedCodexNoWorkFloor(override time.Duration) time.Duration {
	return NoWorkFloor(override)
}

// ExportedWorktreeHEADHasRefsTrailer exposes shared.WorktreeHEADHasRefsTrailer
// (VERIFY). The nil argument is the tmux.CommandRunner — nil means bare local
// exec (NFR7), which is what these tests exercise.
//
// Bead ref: hk-bpxci.
func ExportedWorktreeHEADHasRefsTrailer(ctx context.Context, wtPath string, beadID core.BeadID) (bool, error) {
	return shared.WorktreeHEADHasRefsTrailer(ctx, nil, wtPath, beadID)
}

// ExportedEnsureCodexRefsTrailer exposes EnsureRefsTrailer (VERIFY +
// deterministic commit-after-exit FALLBACK).
//
// Bead ref: hk-bpxci.
func ExportedEnsureCodexRefsTrailer(ctx context.Context, wtPath, parentSHA string, beadID core.BeadID) (ExportedCodexRefsOutcome, error) {
	return EnsureRefsTrailer(ctx, nil, wtPath, parentSHA, beadID)
}

// ExportedCodexSeedPromptInstruction returns the codex seed prompt for beadID so
// tests can assert the INSTRUCT part (the prompt tells codex to commit with the
// Refs: trailer).
//
// Bead ref: hk-bpxci.
func ExportedCodexSeedPromptInstruction(beadID core.BeadID) string {
	return fmt.Sprintf(codexSeedPromptTemplate, string(beadID))
}

// ─────────────────────────────────────────────────────────────────────────────
// codex thread_id interceptor test seams (hk-mzgh)
// ─────────────────────────────────────────────────────────────────────────────

// ExportedNewCodexThreadIDInterceptor exposes newCodexThreadIDInterceptor for
// tests in package codex_test. It returns the concrete type so tests can call
// TokenUsage() after draining the stream.
//
// Bead ref: hk-mzgh.
func ExportedNewCodexThreadIDInterceptor(inner io.Reader, cb func(string)) *codexThreadIDInterceptor {
	return newCodexThreadIDInterceptor(inner, cb)
}
