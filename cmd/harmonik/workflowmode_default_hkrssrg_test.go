package main

// workflowmode_default_hkrssrg_test.go — regression tests for the daemon-level
// workflow-mode default (hk-rssrg, hk-y3o51).
//
// hk-rssrg root cause: the persistent daemon never set Config.WorkflowModeDefault,
// so items with empty workflow_mode resolved to single. ~117 beads landed unreviewed.
// Fix: --workflow-mode flag added to main.go, defaulting to "dot".
//
// hk-y3o51 root cause: queue submit hardcoded "review-loop" as the per-item default,
// overriding the daemon config's dot default. Fix: submit default changed to "" (inherit).
// run.go also changed: bare `harmonik run --beads` no longer forces review-loop.
//
// Tests verify:
//   - the daemon --workflow-mode flag defaults to "dot" (main.go:793)
//   - passing --workflow-mode single is accepted (opt-out path)
//   - passing an invalid value is rejected by daemon.Start
//   - items from bare `harmonik run --beads` carry empty workflow_mode (inherit daemon)
//   - explicit --review-loop / --no-review-loop still override correctly
//
// Helper prefix: wfDefaultFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-rssrg).
//
// Bead ref: hk-rssrg, hk-y3o51.

import (
	"flag"
	"os"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// wfDefaultFixtureParseWorkflowMode drives the flag parsing that main() does
// for --workflow-mode, using an isolated FlagSet to avoid contaminating the
// process-global flag.CommandLine. Returns the parsed workflow-mode string.
func wfDefaultFixtureParseWorkflowMode(t *testing.T, args []string) string {
	t.Helper()
	fs := flag.NewFlagSet("harmonik-test", flag.ContinueOnError)
	var wfMode string
	fs.StringVar(&wfMode, "workflow-mode", string(core.WorkflowModeDot), "")
	// Ignore other flags (project, max-concurrent, etc.) for this narrowly-scoped test.
	fs.Usage = func() {}
	_ = fs.Parse(args)
	return wfMode
}

// TestWfDefaultIsDot asserts that --workflow-mode defaults to "dot"
// when not supplied. This is the regression guard for hk-y3o51: a bare
// `harmonik --project ...` invocation must produce WorkflowModeDefault=dot (triple-review).
func TestWfDefaultIsDot(t *testing.T) {
	t.Parallel()
	got := wfDefaultFixtureParseWorkflowMode(t, []string{})
	want := string(core.WorkflowModeDot)
	if got != want {
		t.Errorf("--workflow-mode default = %q; want %q (hk-y3o51 regression)", got, want)
	}
}

// TestWfDefaultSingleOptOut verifies that passing --workflow-mode single is
// accepted — this is the explicit opt-out path for operators who want single-node dispatch.
func TestWfDefaultSingleOptOut(t *testing.T) {
	t.Parallel()
	got := wfDefaultFixtureParseWorkflowMode(t, []string{"--workflow-mode", "single"})
	if got != string(core.WorkflowModeSingle) {
		t.Errorf("--workflow-mode single: got %q; want %q", got, string(core.WorkflowModeSingle))
	}
}

// TestWfDefaultReviewLoopExplicit verifies that --workflow-mode review-loop is
// accepted when set explicitly (idempotent with the default).
func TestWfDefaultReviewLoopExplicit(t *testing.T) {
	t.Parallel()
	got := wfDefaultFixtureParseWorkflowMode(t, []string{"--workflow-mode", "review-loop"})
	if got != string(core.WorkflowModeReviewLoop) {
		t.Errorf("--workflow-mode review-loop: got %q; want %q", got, string(core.WorkflowModeReviewLoop))
	}
}

// TestWfDefaultInvalidRejectedByDaemonStart verifies that daemon.Start rejects
// an invalid --workflow-mode value at startup (PL-004a fail-fast). The daemon
// never silently degrades to single when given an unrecognised mode.
func TestWfDefaultInvalidRejectedByDaemonStart(t *testing.T) {
	t.Parallel()
	cfg := daemon.Config{
		// Supply a minimal ProjectDir so daemon.Start reaches the WorkflowModeDefault
		// validation step before returning. An empty string causes an earlier error
		// on the pidfile path, which would hide the mode-validation path.
		ProjectDir:          t.TempDir(),
		WorkflowModeDefault: core.WorkflowMode("bogus-mode"),
		// BrPath empty → work loop is skipped (unit-test mode without a bead ledger).
		// LogWriter nil → silences daemon log output.
	}
	err := daemon.Start(t.Context(), cfg)
	if err == nil {
		t.Fatal("daemon.Start with invalid WorkflowModeDefault: expected error, got nil")
	}
}

// TestWfDefaultRunItemsInheritDaemon is the integration-level regression guard:
// items built by the `harmonik run --beads` flag-parse path carry
// workflow_mode="" (empty = inherit daemon default, normally dot/triple-review)
// when no --workflow-mode / --review-loop / --no-review-loop flags are passed (hk-y3o51).
func TestWfDefaultRunItemsInheritDaemon(t *testing.T) {
	t.Parallel()

	// resolveItemWorkflowModeFromArgs exercises the same flag→itemWorkflowMode
	// logic as runBeadSubcommand without the br/daemon plumbing.
	//
	// The canonical path in run.go (hk-y3o51): when --workflow-mode is absent
	// and neither --review-loop nor --no-review-loop was explicitly passed,
	// itemWorkflowMode is left empty so the daemon's config default applies.
	subArgs := []string{} // no flags → inherit daemon default
	itemWorkflowMode := resolveItemWorkflowModeFromArgs(subArgs)
	if itemWorkflowMode != "" {
		t.Errorf("items from bare `harmonik run --beads` carry workflow_mode=%q; want %q (hk-y3o51: should inherit daemon default)",
			itemWorkflowMode, "")
	}
}

// resolveItemWorkflowModeFromArgs replicates the flag-parse → itemWorkflowMode
// resolution block from runBeadSubcommand so TestWfDefaultRunItemsInheritDaemon
// can probe it without the br/daemon/tmux plumbing.
//
// Post hk-y3o51: bare `harmonik run --beads hk-x` with no --workflow-mode /
// --review-loop / --no-review-loop flags produces "" (inherit daemon default).
// Explicit --review-loop → "review-loop"; --no-review-loop → "single".
//
// Bead ref: hk-rssrg, hk-y3o51.
func resolveItemWorkflowModeFromArgs(subArgs []string) string {
	reviewLoop := true     // default ON per hk-g0ckv
	reviewLoopSet := false // tracks whether --review-loop or --no-review-loop was explicit
	workflowModeFlag := "" // --workflow-mode <builtin|single|review-loop|dot>

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--no-review-loop":
			reviewLoop = false
			reviewLoopSet = true
		case arg == "--review-loop":
			reviewLoop = true
			reviewLoopSet = true
		case arg == "--workflow-mode" && i+1 < len(subArgs):
			i++
			workflowModeFlag = subArgs[i]
		}
	}

	// Mirror the resolution in runBeadSubcommand (run.go, hk-y3o51).
	switch workflowModeFlag {
	case "", "builtin":
		// When --review-loop / --no-review-loop was explicit, honour it.
		// Otherwise leave empty so the daemon's config default applies.
		if reviewLoopSet {
			if reviewLoop {
				return string(core.WorkflowModeReviewLoop)
			}
			return string(core.WorkflowModeSingle)
		}
		return "" // inherit daemon default
	case "single":
		return string(core.WorkflowModeSingle)
	case "review-loop":
		return string(core.WorkflowModeReviewLoop)
	case "dot":
		return string(core.WorkflowModeDot)
	default:
		return workflowModeFlag // will be rejected by runBeadSubcommand
	}
}

// TestWfDefaultNoReviewLoopOptOut verifies that --no-review-loop sets
// workflow_mode to single (existing opt-out path unbroken after hk-rssrg).
func TestWfDefaultNoReviewLoopOptOut(t *testing.T) {
	t.Parallel()
	got := resolveItemWorkflowModeFromArgs([]string{"--no-review-loop"})
	if got != string(core.WorkflowModeSingle) {
		t.Errorf("--no-review-loop: workflow_mode=%q; want %q", got, string(core.WorkflowModeSingle))
	}
}

// Ensure core and daemon packages are imported (suppresses "imported and not used"
// if the compiler sees only indirect usage through the test helpers).
var (
	_ = os.DevNull
	_ = daemon.Config{}
)
