package daemon

// pi_ratelimit_carveout_test.go — the carve-out that keeps a free-tier Pi rate
// limit off the paid fleet, tested through the thing that has to feed it.
//
// When an agent reports it is rate limited, the daemon's backstop snaps the
// global concurrency ceiling to one. That is right for a paid harness, where a
// 429 means the whole account is throttled, and wrong for a free-tier Pi run,
// where it means only that one provider said no. So the backstop asks the run's
// handle which harness the run resolved to and skips the global snap for Pi.
//
// The two tests already covering that decision set the agent type on the handle
// themselves. Both stayed green for three days while NOTHING in production set
// it: the launch site that used to was deleted, GetAgentType returned the empty
// type for every run, the carve-out matched nothing, and every free-tier Pi 429
// throttled the paid Claude fleet. A test that supplies the input cannot see the
// producer go away — that is the same shape as a durable store with no writer.
//
// So these two drive the real launch and let it set the type, then hand the
// backstop a real rate-limit event for that run. Delete the SetAgentType call in
// runAgentLaunch and the first one fails.
//
// Helper prefix: piCarve.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runloop"
)

// piCarveLaunchThenRateLimit runs one launch of agentType through the real
// runAgentLaunch, then delivers a status=active rate-limit event for that run to
// a real backstop wired to the same registry. It returns the global concurrency
// ceiling afterwards, which started at four.
//
// The launch's substrate refuses the spawn, so the launch fails. That is fine
// and deliberate: the agent type is resolved from the built spec before any
// agent starts, which is exactly when a rate limit can first arrive.
func piCarveLaunchThenRateLimit(t *testing.T, agentType core.AgentType) int {
	t.Helper()

	in := alaunchInput(t, &alaunchSpySubstrate{}, false, []string{"PATH=/usr/bin"})
	in.Artifacts = shared.LaunchArtifacts{ResolvedAgentType: agentType}

	// The registry the launch reaches, and the same one the backstop reads. A run
	// with no handle is a different case (the backstop falls through to the tuner
	// by design), so the handle has to be there and has to start with no agent
	// type on it — the run path is what puts one there.
	registry := NewRunRegistry()
	registry.Register(in.RunID, &RunHandle{BeadID: core.BeadID("hk-pi-carve-out")})
	in.Handles.RunRegistry = daemonRunRegistry{reg: registry}

	if handle, ok := registry.Get(in.RunID); !ok || handle.GetAgentType() != core.AgentType("") {
		t.Fatalf("the handle already carries agent type %q before the launch.\n"+
			"This fixture only means something if the LAUNCH is what sets it.",
			handle.GetAgentType())
	}

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o750); err != nil {
		t.Fatalf("piCarve: make the transcript dir the tuner reads: %v", err)
	}
	controller := NewConcurrencyController(4)
	backstop := &bandwidthTunerBackstop{}
	backstop.SetTuner(NewBandwidthTuner(controller, 4, 1_000_000, home))
	backstop.SetRunRegistry(registry)

	retryAfter := 60
	payload, marshalErr := json.Marshal(core.AgentRateLimitStatusPayload{
		RunID:             in.RunID,
		SessionID:         "pi-carve-out-session",
		Status:            core.AgentRateLimitStatusActive,
		RetryAfterSeconds: &retryAfter,
		ChangedAt:         time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	})
	if marshalErr != nil {
		t.Fatalf("piCarve: build the rate-limit payload: %v", marshalErr)
	}
	if err := backstop.handle(context.Background(), core.Event{Payload: payload}); err != nil {
		t.Fatalf("piCarve: deliver the rate-limit event: %v", err)
	}
	return controller.Get()
}

// TestPiRateLimit_AFreeTierPiRunDoesNotThrottleTheFleet is the claim, driven end
// to end.
//
// A Pi run reports it is rate limited. The paid fleet must keep its concurrency,
// because a free-tier provider saying no says nothing about the paid account.
// The backstop can only know the run is a Pi run if the launch recorded it, so
// this fails whenever that recording stops happening — which is what a hand-set
// agent type on the handle cannot detect.
func TestPiRateLimit_AFreeTierPiRunDoesNotThrottleTheFleet(t *testing.T) {
	t.Parallel()

	if got := piCarveLaunchThenRateLimit(t, core.AgentTypePi); got != 4 {
		t.Errorf("global concurrency after a Pi rate limit = %d, want 4 (unchanged).\n"+
			"The carve-out reads the agent type off the run's handle, and the launch is what "+
			"puts it there. With nothing setting it the handle reports the empty type, the "+
			"carve-out matches no run at all, and every free-tier Pi 429 throttles the paid "+
			"Claude fleet — which looks like general slowness rather than a bug.", got)
	}
}

// TestPiRateLimit_APaidRunStillThrottlesTheFleet is the control, and without it
// the test above passes for any backstop that ignores every rate limit.
//
// Same launch, same registry, same event: only the resolved harness differs. A
// Claude run's 429 is the whole account saying no, so the ceiling must snap.
func TestPiRateLimit_APaidRunStillThrottlesTheFleet(t *testing.T) {
	t.Parallel()

	if got := piCarveLaunchThenRateLimit(t, core.AgentTypeClaudeCode); got != 1 {
		t.Errorf("global concurrency after a Claude rate limit = %d, want 1.\n"+
			"A paid run's rate limit must still reach the tuner. A backstop that skipped every "+
			"run would satisfy the Pi test above while protecting nothing.", got)
	}
}

// TestPiRateLimit_TheLaunchIsWhatRecordsTheHarness names the missing link on its
// own, so a failure says which half broke.
//
// The two tests above go red together for two very different reasons: the launch
// stopped recording the harness, or the backstop stopped reading it. This one
// fails only for the first.
func TestPiRateLimit_TheLaunchIsWhatRecordsTheHarness(t *testing.T) {
	t.Parallel()

	in := alaunchInput(t, &alaunchSpySubstrate{}, false, []string{"PATH=/usr/bin"})
	in.Artifacts = shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypePi}

	registry := NewRunRegistry()
	registry.Register(in.RunID, &RunHandle{BeadID: core.BeadID("hk-pi-carve-out")})
	in.Handles.RunRegistry = daemonRunRegistry{reg: registry}

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	handle, ok := registry.Get(in.RunID)
	if !ok {
		t.Fatal("the run's handle is gone from the registry, so nothing below can be measured")
	}
	if got := handle.GetAgentType(); got != core.AgentTypePi {
		t.Errorf("the run's handle carries agent type %q after the launch, want %q.\n"+
			"Every reader of this field — the Pi rate-limit carve-out above all — is answered "+
			"with the empty type until the launch records it.", got, core.AgentTypePi)
	}
}

// piCarveHandlePortIsSatisfied keeps the compile-time link between the port the
// launch writes through and the handle the backstop reads.
var _ runloop.RunHandlePort = (*RunHandle)(nil)
