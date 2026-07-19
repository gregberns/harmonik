package daemon

// testopts_test.go — test-only constructor and functional options for daemon.Start.
//
// StartForTesting is the sanctioned entry point for tests that need to inject
// behaviour into daemon.Start without polluting the production Config surface.
// The former Config.TestOnlyBusObserver and Config.TestOnlyBrAdapterFactory
// fields have been moved here as functional options; production Config is now
// free of test-only branches.
//
// Design rationale:
//   - The seam belongs in a test-only constructor, not on production Config.
//   - kerf testing-strategy-uplift T3 originally proposed TestOnly* fields on
//     Config (the right intent — a daemon-level seam for tests), but the fields
//     belong here so Start has a single, test-free code path.
//   - startWithHooks (daemon.go) is unexported; only this file and daemon.go
//     can call it, keeping the hook surface invisible to daemon_test callers.
//
// Bead ref: hk-j192n.

import (
	"context"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/mergeq"
)

// WithWorktreeFactory returns a TestOption that replaces productionWorktreeFactory
// in beadRunOne with factory. Use this to inject a pre-committing factory that
// satisfies the no-commit guard (hk-mmh8f) without requiring the handler binary
// to make git commits.
//
// Bead ref: hk-bnm89.
func WithWorktreeFactory(factory func(ctx context.Context, projectDir, runID, headSHA string) (wtPath string, cleanup func(), err error)) TestOption {
	return func(h *daemonTestHooks) {
		h.worktreeFactory = factory
	}
}

// WithMergeQueue returns a TestOption that injects q as the merge exclusion
// domain (RSM-015). Pass an ALREADY-STARTED queue: runWorkLoop owns only a queue
// it creates itself, so it leaves an injected queue untouched — the test owns its
// lifecycle (Start it, and cancel its Start ctx on cleanup). Use this when a test
// needs to share/inspect the domain the commit-phase merge, the escape check, and
// the base-sync+worktree-add all serialise through.
//
// Bead ref: hk-bnm89, RSM-015.
func WithMergeQueue(q *mergeq.Queue) TestOption {
	return func(h *daemonTestHooks) {
		h.mergeQ = q
	}
}

// TestOption is a functional option for StartForTesting.
//
// Bead ref: hk-j192n.
type TestOption func(*daemonTestHooks)

// WithBusObserver returns a TestOption that installs fn as the bus observer
// hook.  fn is called immediately after all pre-Seal subscriptions have been
// registered and before bus.Seal() is called, mirroring the former
// Config.TestOnlyBusObserver behaviour.
//
// Bead ref: hk-37zy8, hk-j192n.
func WithBusObserver(fn func(bus eventbus.EventBus)) TestOption {
	return func(h *daemonTestHooks) {
		h.busObserver = fn
	}
}

// WithBrAdapterFactory returns a TestOption that replaces brcli.NewForProject
// at all three call sites in startWithHooks with factory.  factory is called
// with (brPath, projectDir) at each site, mirroring the former
// Config.TestOnlyBrAdapterFactory behaviour.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
// Bead ref: hk-th378, hk-j192n.
func WithBrAdapterFactory(factory func(brPath, projectDir string) (*brcli.Adapter, error)) TestOption {
	return func(h *daemonTestHooks) {
		h.brAdapterFactory = factory
	}
}

// WithSpendMeterObserver returns a TestOption that installs fn as the spend-meter
// observer hook.  fn is called with the DaemonSpendMeter immediately after it has
// been subscribed to the bus and before bus.Seal() is called.  Scenario tests use
// this to override the meter's caps (via ExportedSpendMeterSetMaxRunsPerDay /
// ExportedSpendMeterSetDailyCapBytes) so they can trip the meter with a small
// number of synthetic events.
//
// Bead ref: hk-c7lxc.
func WithSpendMeterObserver(fn func(*DaemonSpendMeter)) TestOption {
	return func(h *daemonTestHooks) {
		h.spendMeterObserver = fn
	}
}

// StartForTesting calls startWithHooks with the supplied Config and test hooks
// built from opts.  Use this instead of daemon.Start in tests that need to
// inject a bus observer or a stub br-adapter factory.
//
// Empty-cfg seam (hk-i0hor): when the caller leaves cfg.WorkflowModeDefault
// unset, StartForTesting defaults it to core.WorkflowModeReviewLoop. Production
// Start (PL-004a, hk-81n9r) fail-closes on an empty WorkflowModeDefault and
// returns before any pre-Seal subscription wiring — including the busObserver /
// spendMeterObserver hooks. Unit-test-mode callers (ProjectDir:"") that only
// exercise the bus-wiring path should not have to repeat
// `WorkflowModeDefault: core.WorkflowModeReviewLoop` boilerplate just to reach
// those hooks; the harness supplies the sensible default here so an otherwise
// empty cfg fires the observers. This does NOT relax production Start, which
// still validates WorkflowModeDefault itself.
//
// Example:
//
//	err := daemon.StartForTesting(ctx, cfg,
//	    daemon.WithBusObserver(func(bus eventbus.EventBus) { ... }),
//	)
//
// Bead ref: hk-j192n, hk-i0hor.
func StartForTesting(ctx context.Context, cfg Config, opts ...TestOption) error {
	if cfg.WorkflowModeDefault == "" {
		cfg.WorkflowModeDefault = core.WorkflowModeReviewLoop
	}
	var hooks daemonTestHooks
	for _, o := range opts {
		o(&hooks)
	}
	return startWithHooks(ctx, cfg, hooks)
}
