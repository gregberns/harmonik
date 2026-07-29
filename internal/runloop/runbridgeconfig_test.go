package runloop

// runbridgeconfig_test.go — pins the per-mode merge-retry budget.
//
// This exists because the budget was nearly lost twice over.
//
// MaxMergeAttempts=3 and the retryable-reason classification belonged to the
// review-loop mode alone. When that mode was retired (execution-model.md
// §4.3.EM-015d) the budget was deliberately ported to dot rather than dropped:
// the reasons runmerge.IsRetryableReason classifies — rebase_conflict,
// non_ff_merge, merge_fmt_failed — are artifacts of CONCURRENT merge-to-main,
// not properties of a workflow shape, and they get MORE likely as concurrency
// rises. Letting the budget lapse to 1 as a side effect of a deletion would
// have made the surviving default mode strictly less robust at the merge step
// than the mode it replaced.
//
// The scenario fixture that used to exercise this (RunConcurrentMerge) is
// currently pinned to single mode, where MaxMergeAttempts is 1 by design — see
// hk-gpwmj. So that fixture does NOT cover the budget today. This unit test is
// deliberately independent of it: the invariant is guarded here, at the level
// where it is decided, and it stays guarded regardless of how that fixture is
// eventually re-homed.
//
// Helper prefix: rbc (per implementer-protocol.md §Helper-prefix discipline).

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestRunBridgeConfig_DotCarriesTheMergeRetryBudget pins the ported behaviour:
// dot gets the initial attempt plus the two pre-RT9 retries (hk-f9xzs).
func TestRunBridgeConfig_DotCarriesTheMergeRetryBudget(t *testing.T) {
	t.Parallel()

	cfg := runBridgeConfig(core.WorkflowModeDot)
	if cfg.MaxMergeAttempts != 3 {
		t.Errorf("dot MaxMergeAttempts = %d, want 3 (initial + 2 retries, hk-f9xzs); "+
			"a transient rebase_conflict / non_ff_merge race now fails the run on first contact",
			cfg.MaxMergeAttempts)
	}
}

// TestRunBridgeConfig_SingleKeepsOneAttempt pins that single was NOT swept along
// with the port. Its no-retry behaviour is deliberate (A1 §3) and out of scope
// of the review-loop retirement.
func TestRunBridgeConfig_SingleKeepsOneAttempt(t *testing.T) {
	t.Parallel()

	cfg := runBridgeConfig(core.WorkflowModeSingle)
	if cfg.MaxMergeAttempts != 1 {
		t.Errorf("single MaxMergeAttempts = %d, want 1; single's no-retry merge is deliberate (A1 §3) "+
			"and the review-loop retirement was not licence to change it", cfg.MaxMergeAttempts)
	}
}

// TestRunBridgeConfig_ModeIsCarriedAndOutcomeEmitted pins the two fields every
// mode shares, so a future mode added to the switch cannot silently arrive
// without them.
func TestRunBridgeConfig_ModeIsCarriedAndOutcomeEmitted(t *testing.T) {
	t.Parallel()

	for _, mode := range []core.WorkflowMode{core.WorkflowModeDot, core.WorkflowModeSingle} {
		cfg := runBridgeConfig(mode)
		if cfg.Mode != string(mode) {
			t.Errorf("mode %q: cfg.Mode = %q, want %q", mode, cfg.Mode, string(mode))
		}
		if !cfg.EmitOutcome {
			t.Errorf("mode %q: EmitOutcome is false; the run would terminate without an outcome_emitted event", mode)
		}
		if cfg.MaxMergeAttempts < 1 {
			t.Errorf("mode %q: MaxMergeAttempts = %d, want >= 1; zero attempts never merges at all",
				mode, cfg.MaxMergeAttempts)
		}
	}
}

// TestRunBridgeConfig_DotCarriesItsOwnCloseTransient pins that dot's
// br-unavailable summary is dot's own, not the retired review-loop string. The
// summary is operator-facing text on the close-transient path; inheriting
// "(review-loop APPROVE)" would name a mode that no longer exists.
func TestRunBridgeConfig_DotCarriesItsOwnCloseTransient(t *testing.T) {
	t.Parallel()

	cfg := runBridgeConfig(core.WorkflowModeDot)
	const want = "close-transient-merged (dot success)"
	if cfg.BrUnavailableSummary != want {
		t.Errorf("dot BrUnavailableSummary = %q, want %q", cfg.BrUnavailableSummary, want)
	}
}
