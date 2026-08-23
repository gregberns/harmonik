package daemon

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
