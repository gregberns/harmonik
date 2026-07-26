package daemon

// export_pasteinject_timeouts_test.go — pasteInjectQuitOnCommit timeout seams.
//
// Split out of export_test.go (RT19.6, P2 E5 export_test.go split) so the
// contiguous hk-trjef "pasteInjectQuitOnCommit timeout-recovery" section (the
// watchdog / timeout / kill-delay knobs, the pasteInjectQuitOnCommit delivery
// wrappers, and the bead-guard probes that close the section) lives in one topic
// file. Same package (daemon), so every daemon_test caller resolves
// daemon.ExportedX byte-identically after the move.
//
// HAZARD: several timing knobs are re-exported BY POINTER
// (ExportedBriefDeliveredTimeout = &briefDeliveredTimeout, &noChangeKillDelay,
// &postQuitKillGrace) so tests can mutate the production var. The pointer form
// is preserved exactly — never converted to a value alias, which would silently
// sever the mutation.
//
// Bead: hk-ecrxy.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ExportedBriefDeliveredTimeout is a pointer to the package-level
// briefDeliveredTimeout var.  Tests set *ExportedBriefDeliveredTimeout to a
// short duration to exercise the timeout path without waiting 2 minutes.
//
// Bead: hk-930o3.
var (
	ExportedBriefDeliveredTimeout = &briefDeliveredTimeout
	// ExportedCommitPollTimeout is a pointer to the package-level commitPollTimeout
	// var.  Tests set *ExportedCommitPollTimeout to a short duration to avoid
	// waiting 10 min for the timeout path.
	//
	// Bead: hk-trjef.
	ExportedCommitPollTimeout = &commitPollTimeout
	// ExportedNoChangeKillDelay is a pointer to the package-level noChangeKillDelay
	// var.  Tests set *ExportedNoChangeKillDelay to a short duration to avoid
	// waiting 30 s for the kill path.
	//
	// Bead: hk-trjef.
	ExportedNoChangeKillDelay = &noChangeKillDelay
	// ExportedPostQuitKillGrace is a pointer to the package-level postQuitKillGrace
	// var.  Tests set *ExportedPostQuitKillGrace to a short duration to exercise the
	// post-commit /quit watchdog without waiting 60 s of wall time.
	//
	// Bead: hk-5s7tg.
	ExportedPostQuitKillGrace = &postQuitKillGrace
	// ExportedResumeSubmitRetries and ExportedResumeSubmitRetryDelay are pointers to
	// the package-level implementer-resume submit-retry tunables.  Tests set the
	// delay to a short duration so the bounded submit retry on the resume paste path
	// (the hk-ip33d fix) runs without burning real wall time.
	//
	// Bead: hk-ip33d.
	ExportedResumeSubmitRetries = &resumeSubmitRetries
)

// ExportedResumeSubmitRetryDelay / ExportedSetResumeSubmitRetryDelay read and
// write the package-level resumeSubmitRetryDelay (now an atomic.Int64 of
// nanoseconds) so a parallel test's shrink does not race a production read
// (hk-ip33d).
func ExportedResumeSubmitRetryDelay() time.Duration { return resumeSubmitRetryDelayDur() }

func ExportedSetResumeSubmitRetryDelay(d time.Duration) {
	resumeSubmitRetryDelayNs.Store(int64(d))
}

// ExportedCommitPollInterval is a pointer to the package-level commitPollInterval
// var.  Tests set *ExportedCommitPollInterval to a short duration to keep
// polling tight during timeout tests.
//
// Bead: hk-trjef.
var ExportedCommitPollInterval = &commitPollInterval

// ExportedSessionKiller is the exported alias for the sessionKiller interface so
// tests can implement it without naming the unexported type.
//
// Bead: hk-trjef.
type ExportedSessionKiller = sessionKiller

// ExportedPasteInjectQuitOnCommit exposes pasteInjectQuitOnCommit for tests.
//
// eventCh may be nil; when nil the heartbeat-staleness check is skipped and
// only the wall-clock commitPollTimeout acts as the kill trigger.
//
// This wrapper passes a nil bus and a zero runID (no implementer_budget_exceeded
// emission); use ExportedPasteInjectQuitOnCommitWithBus when the test needs to
// observe the hk-9vp51 diagnostic.
//
// Beads: hk-trjef, hk-930o3, hk-7srrd.
func ExportedPasteInjectQuitOnCommit(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
) {
	pasteInjectQuitOnCommit(ctx, substrate.SystemClock{}, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, nil, core.RunID{})
}

// ExportedPasteInjectQuitOnCommitWithBus is like ExportedPasteInjectQuitOnCommit
// but threads a bus and runID so tests can observe the hk-9vp51
// implementer_budget_exceeded diagnostic emitted on a commit-budget kill.
//
// Bead: hk-9vp51.
func ExportedPasteInjectQuitOnCommitWithBus(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	wtPath string,
	initialSHA string,
	noChangeTimeoutCh chan<- struct{},
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) {
	pasteInjectQuitOnCommit(ctx, substrate.SystemClock{}, qs, killer, wtPath, initialSHA, noChangeTimeoutCh, briefDelivered, eventCh, bus, runID)
}

// ExportedCommitHardCeiling is a pointer to the package-level commitHardCeiling
// var.  Tests set *ExportedCommitHardCeiling to a short duration to exercise the
// absolute-backstop kill path quickly (hk-9vp51).
var (
	ExportedCommitHardCeiling = &commitHardCeiling
	// ExportedHeartbeatStalenessThreshold is a pointer to the package-level
	// heartbeatStalenessThreshold var.  Tests set *ExportedHeartbeatStalenessThreshold
	// to a short duration to exercise the heartbeat-stale kill path quickly.
	//
	// Bead: hk-7srrd.
	ExportedHeartbeatStalenessThreshold = &heartbeatStalenessThreshold
	// ExportedLaunchHeartbeatTimeout is a pointer to the package-level
	// launchHeartbeatTimeout var.  Tests set *ExportedLaunchHeartbeatTimeout to a
	// short duration to exercise the launch-verification kill path quickly.
	//
	// Bead: hk-3gq0b.
	ExportedLaunchHeartbeatTimeout = &launchHeartbeatTimeout
	// ExportedLaunchSuppressionCeiling is a pointer to the package-level
	// launchSuppressionCeiling var. Tests set *ExportedLaunchSuppressionCeiling to a
	// short duration to prove the launch-verification suppression terminates even
	// when the pane reports an active child process forever (hk-jgxqc).
	//
	// Bead: hk-jgxqc.
	ExportedLaunchSuppressionCeiling = &launchSuppressionCeiling
)

// quitSenderExported is the exported alias for quitSender so the exported
// wrapper can accept it.
type quitSenderExported = quitSender

// ExportedBeadAlreadySubsumedInMain exposes the main-history Refs-trailer probe
// for tests. The implementation left internal/daemon for
// shared.MainHistoryHasRefsTrailer (P2 E5 RT19b); this shim keeps the existing
// daemon_test callers compiling unchanged.
//
// Bead: hk-trjef.
func ExportedBeadAlreadySubsumedInMain(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	return shared.MainHistoryHasRefsTrailer(ctx, projectDir, beadID)
}

// ExportedBeadExplicitlyReopened exposes beadExplicitlyReopened for tests.
//
// Bead: hk-wcv.
func ExportedBeadExplicitlyReopened(ctx context.Context, auditLogger func(context.Context, core.BeadID) ([]brcli.AuditEvent, error), beadID core.BeadID) bool {
	return beadExplicitlyReopened(ctx, auditLogger, beadID)
}

// ExportedAutoCloseStaleBlockersOnClaimFailure exposes
// autoCloseStaleBlockersOnClaimFailure for unit tests via WorkLoopDepsParams.
//
// Bead: hk-rnsjs.
func ExportedAutoCloseStaleBlockersOnClaimFailure(ctx context.Context, p WorkLoopDepsParams, beadID core.BeadID) {
	autoCloseStaleBlockersOnClaimFailure(ctx, ExportedWorkLoopDeps(p), beadID)
}
