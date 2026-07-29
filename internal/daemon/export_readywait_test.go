package daemon

// export_readywait_test.go — agent-ready / post-ready-hang / socket-grace seams.
//
// Split out of export_test.go (RT19.4, P2 E5 export_test.go split) so the
// ready-wait shims (agent-ready timeout knobs now homed in internal/runlaunch,
// waitsocketgrace.go stop-hook grace seams) live in one topic file. Same
// package (daemon), so every
// daemon_test caller resolves daemon.ExportedX byte-identically after the move.
//
// Two of the originally-catalogued agent-ready shims were deleted by RT14 (the
// agentready.go removal); only the surviving 11 seams are relocated here.
//
// Bead: hk-ecrxy.

import (
	"context"
	"time"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ExportedSetAgentReadyKillReapTimeout overrides the package-level
// runlaunch.KillReapTimeout for tests. Returns a restore function; pass it to
// t.Cleanup. NOT safe for use with t.Parallel() — modifies a package global.
//
// Bead ref: hk-4hso5.
func ExportedSetAgentReadyKillReapTimeout(d time.Duration) func() {
	orig := runlaunch.KillReapTimeout
	runlaunch.KillReapTimeout = d
	return func() { runlaunch.KillReapTimeout = orig }
}

// ExportedErrAgentReadyTimeout exposes runlaunch.ErrAgentReadyTimeout for tests.
//
// Bead ref: hk-gql20.18.
var ExportedErrAgentReadyTimeout = runlaunch.ErrAgentReadyTimeout

// ExportedDefaultAgentReadyTimeout exposes runlaunch.DefaultAgentReadyTimeout
// (HC-056, internal/runlaunch/deadlines.go) so the WS3-Claude-C timing
// property/fuzz harness can PIN the
// real threshold constant — if the production default drifts, the pin assertion
// fails, surfacing that the harness's scaled band no longer models the real one.
var (
	ExportedDefaultAgentReadyTimeout = runlaunch.DefaultAgentReadyTimeout
	// ExportedEmitAgentReadyTimeout exposes runlaunch.EmitAgentReadyTimeout (hk-5cox8) so the
	// WS3-Claude-C harness drives the REAL anomaly emitter (inv-3) rather than a
	// fabricated stand-in.
	ExportedEmitAgentReadyTimeout = runlaunch.EmitAgentReadyTimeout
)

// ExitInfoExported is the exported shape of exitInfo for tests in package
// daemon_test.
//
// Bead ref: hk-gql20.22.
type ExitInfoExported struct {
	ExitCode   int
	WaitErr    error
	StderrTail []byte
}

// ExportedStopHookGrace exposes the stopHookGrace constant so tests can assert
// that the fast-path returns well within the grace window.
//
// Bead: hk-3jmke.
const ExportedStopHookGrace = runloop.StopHookGrace

// ExportedWaitWithSocketGrace exposes waitWithSocketGrace for tests in package
// daemon_test.
//
// Bead ref: hk-gql20.22.
func ExportedWaitWithSocketGrace(
	ctx context.Context,
	store *hookSessionStore,
	watcher *handlercontract.Watcher,
	sess handler.Session,
	runID, claudeSessID string,
) (*handler.ExportedOutcomeEmittedPayload, ExitInfoExported) {
	outcome, ei := runloop.WaitWithSocketGrace(ctx, substrate.SystemClock{}, store, watcher, sess, runID, claudeSessID)
	return outcome, ExitInfoExported{ExitCode: ei.ExitCode, WaitErr: ei.WaitErr, StderrTail: ei.StderrTail}
}
