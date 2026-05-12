package daemon

// export_test.go — test-seam exports for internal/daemon.
//
// This file is compiled only when running tests (it lives in package daemon,
// not daemon_test). It exports otherwise-unexported symbols so that
// workloop_test.go (package daemon_test) can inject stub dependencies without
// modifying the production API surface.
//
// Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// WorkLoopDepsParams carries the parameters for ExportedWorkLoopDeps so callers
// can supply only the fields they care about; zero values use safe defaults.
type WorkLoopDepsParams struct {
	// BrAdapter is the stub bead ledger.  Required.
	BrAdapter beadLedger

	// Bus is the event collector.  Required.
	Bus handlercontract.EventEmitter

	// ProjectDir is the repo root.  Required.
	ProjectDir string

	// HandlerBinary is the binary to spawn.  Required.
	HandlerBinary string

	// HandlerArgs are extra args forwarded to the binary.  May be nil.
	HandlerArgs []string

	// IntentLogDir is the beads-intents directory path.  Required.
	IntentLogDir string
}

// ExportedWorkLoopDeps constructs a workLoopDeps from the supplied params and
// a real handler.Handler bound to the provided bus.  Use in tests to bypass
// newWorkLoopDeps (which requires a real br binary).
func ExportedWorkLoopDeps(p WorkLoopDepsParams) workLoopDeps {
	binary := p.HandlerBinary
	if binary == "" {
		binary = "claude"
	}

	// Build a LaunchSpec-mutating handler using a real handler.Handler.
	// To forward HandlerArgs to every Launch call we wrap the real handler.
	h := handler.NewHandler(p.Bus, handlercontract.NoopWatcherDeadLetter{})

	return workLoopDeps{
		brAdapter:     p.BrAdapter,
		bus:           p.Bus,
		h:             &argsInjectingHandler{inner: h, args: p.HandlerArgs},
		intentLogDir:  p.IntentLogDir,
		projectDir:    p.ProjectDir,
		handlerBinary: binary,
		handlerEnv:    nil,
		brTimeoutCfg:  brcli.TimeoutConfig{},
		tidGen:        core.NewTransitionIDGenerator(),
	}
}

// ExportedRunWorkLoop runs the work loop with the given deps until ctx is
// cancelled, mirroring runWorkLoop.
func ExportedRunWorkLoop(ctx context.Context, deps workLoopDeps) error {
	return runWorkLoop(ctx, deps)
}

// argsInjectingHandler wraps handler.Handler to append fixed args to every
// LaunchSpec before delegating to the inner handler.  This lets tests pass
// arguments like ["-c", "exit 0"] without modifying the production LaunchSpec
// construction in the work loop.
type argsInjectingHandler struct {
	inner handler.Handler
	args  []string
}

func (a *argsInjectingHandler) Launch(ctx context.Context, spec handler.LaunchSpec) (handler.Session, *handlercontract.Watcher, error) {
	if len(a.args) > 0 {
		spec.Args = append(spec.Args, a.args...)
	}
	return a.inner.Launch(ctx, spec)
}
