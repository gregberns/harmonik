package daemon

// fakes_launchbuilders_test.go — capture / minimal launch-spec test builders.
//
// Split out of export_test.go (RT19.10, P2 E5 export_test.go split). These are
// test fixtures, not shims — a minimal LaunchSpec builder plus the capture
// builders that record extra-context / node-prompt / runner / model-effort into
// a channel, and the ModelEffortPair carrier they emit. They are the real
// content of a future internal/runlooptest. Same package (daemon), so every
// daemon_test caller resolves daemon.ExportedX byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ExportedMinimalLaunchSpecBuilder returns a launchSpecBuilder stub that
// produces a handler.LaunchSpec with a no-op binary (/bin/true) and
// zero-value shared.LaunchArtifacts. Used in tests that inject a spy substrate
// and need handler.Launch to reach Substrate.SpawnWindow without the full
// Claude build infrastructure (e.g. hk-wnqos single-mode terminal-spawn test).
func ExportedMinimalLaunchSpecBuilder() func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, _ shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		return handler.LaunchSpec{Binary: "/bin/true"}, shared.LaunchArtifacts{}, nil
	}
}

// ExportedCaptureExtraContextBuilder returns a launchSpecBuilder stub that
// sends the extraContext from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node role= is injected into the agent brief (hk-m5lmo).
func ExportedCaptureExtraContextBuilder(ch chan<- string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.ExtraContext:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureNodePromptBuilder returns a launchSpecBuilder stub that
// sends the nodePrompt from the FIRST call into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert
// that node prompt= is threaded into shared.LaunchCtx (hk-sdnzj).
func ExportedCaptureNodePromptBuilder(ch chan<- string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.NodePrompt:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ExportedCaptureRunnerBuilder returns a launchSpecBuilder stub that sends the
// CommandRunner from the FIRST call's shared.LaunchCtx into ch (non-blocking), then
// returns an error to short-circuit the dispatch. Tests use this to assert that
// the review-loop and DOT launch paths thread the run's CommandRunner into the
// shared.LaunchCtx so the worktree-trust / settings / agent-task writes land on the
// WORKER for a REMOTE run (hk-3sus).
func ExportedCaptureRunnerBuilder(ch chan<- tmuxPkg.CommandRunner) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- rc.Runner:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}

// ModelEffortPair holds the model and effort values captured from a shared.LaunchCtx.
// Used by ExportedCaptureModelEffortBuilder tests (hk-q8nqr).
type ModelEffortPair struct {
	Model  string
	Effort string
}

// ExportedCaptureModelEffortBuilder returns a launchSpecBuilder stub that
// sends the (model, effort) pair from the FIRST call into ch (non-blocking),
// then returns an error to short-circuit the dispatch. Tests use this to
// assert that per-node model= / effort= overrides are threaded into
// shared.LaunchCtx (hk-q8nqr WG-042 §I.5 / EM-012b-NODE).
func ExportedCaptureModelEffortBuilder(ch chan<- ModelEffortPair) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		select {
		case ch <- ModelEffortPair{Model: rc.Model, Effort: rc.Effort}:
		default:
		}
		return handler.LaunchSpec{}, shared.LaunchArtifacts{}, fmt.Errorf("capture-only stub: stopping dispatch")
	}
}
