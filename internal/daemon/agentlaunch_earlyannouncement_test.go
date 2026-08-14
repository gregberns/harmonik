package daemon

// agentlaunch_earlyannouncement_test.go — the launch WIRES the session slot in.
//
// agentlaunch_sessionslot_test.go pins sessionSlot on its own: an announcement
// that lands in an empty slot is held, and the kill fires the moment a session
// arrives. That is the type's behaviour, and it is true whether or not
// runAgentLaunch ever calls it. This file pins the other half — that the launch
// path actually routes the agent_end callback through the slot, so the kill
// survives the ordering the real world produces.
//
// The ordering is not a race here, it is a certainty. handler.Launch applies
// spec.StdoutWrapper INSIDE the call, before it hands the session back, so a
// harness whose interceptor announces on construction announces while the slot
// is still empty. The fake harness below does exactly that, synchronously, so
// the test reaches the early-announcement branch on every run with no sleeps
// and no timing luck.
//
// What goes wrong without the latch is a HANG, not a wrong value: the kill is
// dropped, the agent keeps running with its work already done, and in
// production a stall watchdog eventually reaps it — which IS recorded as a
// failure, so the run fails and the commit is discarded (hk-tyksz). The child
// here is a `sleep` that outlives any plausible test, so the assertion is that
// the launch comes back well inside its own deadline. It cannot, unless the
// kill landed.

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// earlyAnnounceHarness is the pi harness reduced to the three answers
// runAgentLaunch reads on this path:
//
//   - SessionIDCaptured, which is what makes the launch build an interceptor
//     and an agent_end callback at all (and, as a consequence, forces the exec
//     path with a real child process);
//   - CompletionProcessExit, the self-terminating mode pi and codex run in;
//   - an interceptor that fires agentEndCb before it returns the reader.
//
// Everything else is a stub. The launch never calls Seed, Retask, Teardown or
// DetectReady, and a stub that panics would be a truer statement of that — but
// it would also turn a future wiring change into a crash instead of a failed
// assertion, so they no-op.
type earlyAnnounceHarness struct {
	agentType core.AgentType

	// announcedBeforeLaunchReturned records that the callback ran while the
	// interceptor was being constructed, i.e. inside handler.Launch. It is the
	// witness that this test exercised the EARLY ordering and not the ordinary
	// one; without it a change to when handler.Launch applies the wrapper would
	// silently turn this test into a duplicate of the easy case.
	announcedBeforeLaunchReturned bool
}

var _ handlercontract.Harness = (*earlyAnnounceHarness)(nil)

func (h *earlyAnnounceHarness) AgentType() core.AgentType { return h.agentType }

func (h *earlyAnnounceHarness) LaunchSpec(handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	// runAgentLaunch is given a built spec; it never asks the harness for one.
	return handlercontract.SpawnSpec{}, nil
}

func (h *earlyAnnounceHarness) Seed(handlercontract.Session, handlercontract.RunCtx) error {
	return nil
}

func (h *earlyAnnounceHarness) Retask(handlercontract.Session, string, handlercontract.RunCtx) error {
	return nil
}

func (h *earlyAnnounceHarness) Teardown(handlercontract.Session) error { return nil }

func (h *earlyAnnounceHarness) DetectReady(handlercontract.EventEnvelope) bool { return false }

func (h *earlyAnnounceHarness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDCaptured
}

func (h *earlyAnnounceHarness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionProcessExit
}

// NewSessionIDInterceptor announces the end of the turn SYNCHRONOUSLY, before
// it returns the reader. handler.Launch calls this before it returns the
// session, so the announcement is guaranteed to reach an empty slot.
//
// A real pi interceptor announces when it PARSES {"type":"agent_end"} off the
// child's stdout. Announcing on construction is the same event moved to the
// earliest instant it can occur, which is the instant the bug lives at.
func (h *earlyAnnounceHarness) NewSessionIDInterceptor(inner io.Reader, _ func(string), agentEndCb func()) io.Reader {
	if agentEndCb != nil {
		h.announcedBeforeLaunchReturned = true
		agentEndCb()
	}
	return inner
}

// TestRunAgentLaunch_AnnouncementBeforeTheSessionExistsStillReapsTheChild is
// the wiring pin.
//
// The child is a plain `sleep`: nothing in this test ever ends it, and no
// watchdog is armed (no stall feed, no adapter, no ready handshake to time
// out). The only thing that can stop it is the agent_end kill, held in the
// session slot until the launch hands a session over. So a launch that returns
// inside its deadline is proof the held kill fired, and a launch that runs the
// deadline out is the dropped-kill defect.
func TestRunAgentLaunch_AnnouncementBeforeTheSessionExistsStillReapsTheChild(t *testing.T) {
	t.Parallel()

	const deadline = 20 * time.Second

	wt := t.TempDir()
	harness := &earlyAnnounceHarness{agentType: core.AgentTypeCodex}
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeCodex, harness); err != nil {
		t.Fatalf("register harness: %v", err)
	}

	spy := &alaunchSpySubstrate{}
	in := agentLaunchInput{
		Env: runloop.RunEnv{
			ProjectDir:              wt,
			AgentReadyTimeout:       time.Second,
			RemoteAgentReadyTimeout: time.Second,
		},
		Ports: runloop.RunPorts{
			Emitter: alaunchDiscardEmitter{},
			Clock:   substrate.SystemClock{},
		},
		Handles: runloop.SharedHandles{
			// Empty: no adapter for this agent type is a supported launch path —
			// the segment feeds a synthetic ready rather than waiting.
			AdapterRegistry: handlercontract.NewAdapterRegistry(),
			HarnessRegistry: reg,
			// The completion wait dereferences the store unconditionally, so a
			// nil one is not a supported input. This double answers "the agent
			// reported nothing" without standing up the socket relay and without
			// paying the stop-hook grace window.
			HookStore: &surviveGateHookStore{},
		},
		RunID:     z8ekRunID(t),
		LogPrefix: "daemon: early-announcement test",
		Spec: handler.LaunchSpec{
			Binary: "/bin/sh",
			// A child that will never end on its own, and a single process: no
			// grandchild inherits the stdout pipe, so the watcher observes the
			// exit the moment the kill lands.
			Args:    []string{"-c", "exec sleep 600"},
			WorkDir: wt,
			Env:     []string{"PATH=/usr/bin:/bin"},
		},
		Artifacts:    shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeCodex},
		WorktreePath: wt,
		// Never spawned on — SessionIDCaptured forces the exec path — and
		// asserted below, so a launch that quietly took the substrate path (where
		// StdoutWrapper is never applied and the whole announcement mechanism is
		// unreachable) cannot pass this test.
		BaseSubstrate: spy,
	}

	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	start := time.Now()
	res := runAgentLaunch(ctx, in)
	elapsed := time.Since(start)
	res.Cleanup()

	if !harness.announcedBeforeLaunchReturned {
		t.Fatal("the fake harness never built an interceptor, so no agent_end was announced — " +
			"this test exercised nothing. Check that SessionIDCaptured still selects the interceptor path.")
	}
	if got := spy.count(); got != 0 {
		t.Fatalf("substrate SpawnWindow calls = %d, want 0 — the launch took the substrate path, "+
			"where StdoutWrapper is never applied and the early announcement cannot happen at all", got)
	}

	if ctx.Err() != nil {
		t.Fatalf("runAgentLaunch ran its %v deadline out (elapsed %v).\n"+
			"An agent that announced agent_end before the launch handed the session back was never killed: "+
			"the announcement kill was dropped instead of being held in the session slot. "+
			"In production the agent then sits idle until a stall watchdog reaps it, and a watchdog kill is "+
			"recorded as a failure — so the run fails and the commit is thrown away (hk-tyksz).",
			deadline, elapsed)
	}

	if res.Fail != agentLaunchOK {
		t.Errorf("Fail = %v (FailErr %v), want agentLaunchOK (%v)", res.Fail, res.FailErr, agentLaunchOK)
	}
	if !res.Exit.AgentAnnouncedEnd {
		t.Error("Exit.AgentAnnouncedEnd = false, want true.\n" +
			"The agent said it had finished, so the kill the daemon then issued is the daemon's own act. " +
			"With this flag false the terminal classifier reads the signal death as a crash the agent suffered.")
	}
	if res.Session == nil {
		t.Error("Session = nil, want the launched session — the launch reports no session for a run that spawned one")
	}
}
