package daemon

// export_pasteinject_test.go — paste-inject delivery test-seam exports.
//
// Split out of export_test.go (RT19.5, P2 E5 export_test.go split) so the
// pasteinject.go delivery shims (the pasteInjectReviewer / pasteInjectImplementerInitial
// / pasteInjectQuitOnReviewFile kick-off path, its pasteInjecter / enterSender /
// paneCapturer stub interfaces, the splash-dismiss and seed-paste verify knobs,
// and the hk-sah87 diff-scaled reviewer-budget seams) live in one topic file.
// Same package (daemon), so every daemon_test caller resolves daemon.ExportedX
// byte-identically after the move.
//
// Several knobs are re-exported BY POINTER (var Exported... = &pkgVar) so tests
// can mutate the production var; the pointer form is preserved exactly.
//
// Bead: hk-ecrxy.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ExportedReviewFileTimeout is a pointer to the package-level reviewFileTimeout
// var.  Tests set *ExportedReviewFileTimeout to a short duration to exercise
// the timeout path without waiting 10 minutes.
//
// Bead: hk-jimbc.
var (
	ExportedReviewFileTimeout = &reviewFileTimeout
	// ExportedReviewFilePollInterval is a pointer to the package-level
	// reviewFilePollInterval var.  Tests set *ExportedReviewFilePollInterval to a
	// short duration to keep polling tight during unit tests.
	//
	// Bead: hk-jimbc.
	ExportedReviewFilePollInterval = &reviewFilePollInterval
)

// PasteInjecterExported is an exported alias for the unexported pasteInjecter
// interface so tests in package daemon_test can supply a structural stub as the
// inj re-seed target of ExportedPasteInjectQuitOnReviewFile (hk-7rgqs).
type (
	PasteInjecterExported = pasteInjecter
	// EnterSenderExported is an exported alias for the unexported enterSender
	// interface so tests can assert on / drive the splash-dismiss + submit Enter
	// path (hk-7rgqs).
	EnterSenderExported = enterSender
)

// ExportedReviewerReseedGrace is a pointer to the package-level
// reviewerReseedGrace var.  Tests set *ExportedReviewerReseedGrace to a short
// duration to exercise the hk-7rgqs one-shot reviewer re-seed path without
// waiting the production 75s.
//
// Bead: hk-7rgqs.
var (
	ExportedReviewerReseedGrace = &reviewerReseedGrace
	// ExportedImplementerReseedGrace is a pointer to the package-level
	// implementerReseedGrace var.  Tests set *ExportedImplementerReseedGrace to a
	// short duration to exercise the hk-76n5g one-shot reseed-Enter path in
	// pasteInjectQuitOnCommit without waiting the production 75 s.
	//
	// Bead: hk-76n5g.
	ExportedImplementerReseedGrace = &implementerReseedGrace
)

// ExportedSplashDismissDelay / ExportedSetSplashDismissDelay read and write the
// package-level splashDismissDelay (now an atomic.Int64 of nanoseconds).  Tests
// set it to a short duration so the splash-dismiss wait inside the paste-inject
// helpers does not slow unit tests; atomic access keeps a parallel test's write
// from racing a production read.
//
// Bead: hk-7rgqs.
func ExportedSplashDismissDelay() time.Duration { return splashDismissDelayDur() }

func ExportedSetSplashDismissDelay(d time.Duration) {
	splashDismissDelayNs.Store(int64(d))
}

// PaneCapturerExported is an exported alias for the unexported paneCapturer
// interface so tests can supply a stub that drives the seed-paste
// land-verification + re-paste loop (hk-zexsj).
type PaneCapturerExported = paneCapturer

// ExportedPasteVerifyAttempts, ExportedPasteVerifyBackoff and
// ExportedPasteVerifyScrollback are pointers to the package-level seed-paste
// verify knobs so tests can bound the attempts and shrink the backoff without
// waiting real wall time (hk-zexsj).
var (
	ExportedPasteVerifyAttempts   = &pasteVerifyAttempts
	ExportedPasteVerifyScrollback = &pasteVerifyScrollback
)

// ExportedPasteVerifyBackoff / ExportedSetPasteVerifyBackoff read and write the
// package-level pasteVerifyBackoff (now an atomic.Int64 of nanoseconds) so a
// parallel test's shrink does not race a production read (hk-zexsj).
func ExportedPasteVerifyBackoff() time.Duration { return pasteVerifyBackoffDur() }

func ExportedSetPasteVerifyBackoff(d time.Duration) {
	pasteVerifyBackoffNs.Store(int64(d))
}

// ExportedPasteInjectReviewer exposes pasteInjectReviewer for unit tests that
// assert the reviewer kick-off delivery (splash-dismiss → paste → bounded submit
// Enter) directly (hk-7rgqs).
func ExportedPasteInjectReviewer(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectReviewer(ctx, substrate.SystemClock{}, inj, claudeSessID, wtPath, nil)
}

// ExportedPasteInjectImplementerInitial exposes pasteInjectImplementerInitial for
// unit tests that assert the implementer-initial robust-submit hardening
// (hk-7rgqs).
func ExportedPasteInjectImplementerInitial(ctx context.Context, inj pasteInjecter, claudeSessID, wtPath string) string {
	return pasteInjectImplementerInitial(ctx, substrate.SystemClock{}, inj, claudeSessID, wtPath, nil)
}

// ExportedPasteInjectQuitOnReviewFile exposes pasteInjectQuitOnReviewFile for
// tests in package daemon_test.
//
// hk-7rgqs: now takes inj (pasteInjecter) + claudeSessID so the one-shot re-seed
// path is exercisable; pass nil inj to disable re-seed (the pre-hk-7rgqs
// behaviour).
//
// hk-60t8: now takes eventCh (heartbeat channel; nil = disabled) and
// overrideCeiling (0 = use reviewFileHardCeiling).
//
// Bead: hk-jimbc, hk-7rgqs, hk-60t8.
func ExportedPasteInjectQuitOnReviewFile(
	ctx context.Context,
	qs quitSenderExported,
	killer sessionKiller,
	inj pasteInjecter,
	claudeSessID string,
	wtPath string,
	briefDelivered <-chan struct{},
	eventCh <-chan core.EventEnvelope,
	overrideCeiling time.Duration,
) {
	pasteInjectQuitOnReviewFile(ctx, substrate.SystemClock{}, qs, killer, inj, claudeSessID, wtPath, briefDelivered, eventCh, overrideCeiling)
}

// ExportedReviewFileHardCeiling is a pointer to the package-level
// reviewFileHardCeiling var (the absolute upper bound on the reviewer-verdict
// wait, regardless of diff size).
//
// Bead: hk-sah87.
var (
	ExportedReviewFileHardCeiling = &reviewFileHardCeiling
	// ExportedReviewFilePerKLineBudget is a pointer to the package-level
	// reviewFilePerKLineBudget var (extra wait per 1000 changed lines).
	//
	// Bead: hk-sah87.
	ExportedReviewFilePerKLineBudget = &reviewFilePerKLineBudget
	// ExportedReviewerHeartbeatActiveGrace is a pointer to the package-level
	// reviewerHeartbeatActiveGrace var.  Tests set *ExportedReviewerHeartbeatActiveGrace
	// to a short duration to exercise the heartbeat-based extension path without
	// waiting 10 minutes.
	//
	// Bead: hk-60t8.
	ExportedReviewerHeartbeatActiveGrace = &reviewerHeartbeatActiveGrace
)

// ExportedReviewBudgetForDiff exposes reviewBudgetForDiff for unit tests.
//
// Bead: hk-sah87.
func ExportedReviewBudgetForDiff(changedLines int, base, perKLine, ceiling time.Duration) time.Duration {
	return reviewBudgetForDiff(changedLines, base, perKLine, ceiling)
}

// ExportedSumNumstatLines exposes sumNumstatLines for unit tests.
//
// Bead: hk-sah87.
func ExportedSumNumstatLines(numstat string) (int, bool) {
	return sumNumstatLines(numstat)
}

// ExportedReviewerBudgetSentinelName re-exports reviewerBudgetSentinelName so
// tests can assert the marker file basename.
//
// Bead: hk-sah87.
const ExportedReviewerBudgetSentinelName = reviewerBudgetSentinelName

// ExportedReadReviewerBudgetSentinelFields reads the reviewer budget-kill marker
// at wtPath and returns its fields (present is false when the marker is absent).
// Exposed so tests in package daemon_test can assert the marker contents without
// access to the unexported reviewerBudgetSentinel struct.
//
// Bead: hk-sah87.
//
//nolint:gocritic // hk-sah87: the flat result tuple is the point of this seam — it lets daemon_test assert marker fields without access to the unexported reviewerBudgetSentinel struct; returning a struct would just re-export it.
func ExportedReadReviewerBudgetSentinelFields(wtPath string) (present bool, reason string, budgetMS, elapsedMS int64, changedLines int, err error) {
	s, rErr := ReadReviewerBudgetSentinel(wtPath)
	if rErr != nil {
		return false, "", 0, 0, 0, rErr
	}
	if s == nil {
		return false, "", 0, 0, 0, nil
	}
	return true, s.Reason, s.BudgetMS, s.ElapsedMS, s.ChangedLines, nil
}
