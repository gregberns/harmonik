package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

type alaunchSpySubstrate struct {
	mu     sync.Mutex
	spawns int
}

var _ handler.Substrate = (*alaunchSpySubstrate)(nil)

func (s *alaunchSpySubstrate) SpawnWindow(context.Context, handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.mu.Lock()
	s.spawns++
	s.mu.Unlock()
	return nil, errors.New("alaunch spy substrate: spawn refused")
}

func (s *alaunchSpySubstrate) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spawns
}

type alaunchDiscardEmitter struct{}

var _ handlercontract.EventEmitter = alaunchDiscardEmitter{}

func (alaunchDiscardEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (alaunchDiscardEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

func alaunchInput(t *testing.T, spy handler.Substrate, remote bool, specEnv []string) agentLaunchInput {
	t.Helper()
	wt := t.TempDir()
	return agentLaunchInput{
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
		},
		RunID:     z8ekRunID(t),
		LogPrefix: "daemon: agentlaunch behavioral test",
		Spec: handler.LaunchSpec{
			Binary:  "/bin/true",
			WorkDir: wt,
			Env:     specEnv,
		},
		Artifacts:     shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeClaudeCode},
		WorktreePath:  wt,
		BaseSubstrate: spy,
		Remote:        remote,
	}
}

// TestRunAgentLaunch_RemoteRunWithLiveCredentialsIsRefused is the behavioral pin
// on the D2 fail-closed guard: a remote run whose FINAL spawn environment
// carries a live ANTHROPIC_API_KEY must be REFUSED, and the refusal must be
// visible in the result the caller reads.
//
// The `Fail` assertion is the load-bearing one — it is what fails when
// refuseLaunch stops recording the refusal, which is the exact mutation the AST
// sensor could not see.
func TestRunAgentLaunch_RemoteRunWithLiveCredentialsIsRefused(t *testing.T) {
	t.Parallel()

	spy := &alaunchSpySubstrate{}
	in := alaunchInput(t, spy, true, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-live-not-a-real-key",
	})

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	if res.Fail != agentLaunchPrelaunchFailed {
		t.Errorf("Fail = %v, want agentLaunchPrelaunchFailed (%v).\n"+
			"The D2 refusal was not RECORDED, so the caller cannot tell this remote run — which carried live Anthropic credentials — from one that launched and completed. Fail is the only channel the refusal travels on.",
			res.Fail, agentLaunchPrelaunchFailed)
	}
	if res.FailErr == nil {
		t.Error("FailErr = nil, want the D2 refusal reason; the caller has no reason to report to the operator")
	} else if !strings.Contains(res.FailErr.Error(), string(d2APIKeyRefusal)) {
		t.Errorf("FailErr = %q, want it to carry %q", res.FailErr, string(d2APIKeyRefusal))
	}

	if got := spy.count(); got != 0 {
		t.Errorf("substrate SpawnWindow calls = %d, want 0 — the launch was attempted despite the refusal", got)
	}
	if res.Session != nil {
		t.Errorf("Session = %#v, want nil", res.Session)
	}
	if !res.LaunchedAt.IsZero() {
		t.Error("LaunchedAt is set, so the launch machinery ran past the guard")
	}
	if res.Dispatch.Phase != runexec.DispatchPhase("") {
		t.Errorf("Dispatch.Phase = %q, want the zero phase — the dispatch segment ran", res.Dispatch.Phase)
	}
}

// TestRunAgentLaunch_LocalRunWithCredentialsReachesTheSubstrate is the control
// for the test above, and behavioral coverage of the agentLaunchErrored
// boundary in its own right.
//
// Without it, "SpawnWindow calls = 0" above proves nothing: a runAgentLaunch
// that could never reach a substrate under these inputs would satisfy it
// vacuously. The SAME input with Remote flipped to false must reach the
// substrate — the guard's whole scope is the remote/local distinction — and a
// substrate that refuses the spawn must surface as agentLaunchErrored carrying
// the launch error, not as a silent success.
func TestRunAgentLaunch_LocalRunWithCredentialsReachesTheSubstrate(t *testing.T) {
	t.Parallel()

	spy := &alaunchSpySubstrate{}
	in := alaunchInput(t, spy, false, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-ant-live-not-a-real-key",
	})

	res := runAgentLaunch(context.Background(), in)
	res.Cleanup()

	if got := spy.count(); got != 1 {
		t.Fatalf("substrate SpawnWindow calls = %d, want 1.\n"+
			"The local launch never reached the substrate, which makes the remote test's zero-spawn assertion vacuous — fix this fixture before trusting that one.", got)
	}
	if res.Fail != agentLaunchErrored {
		t.Errorf("Fail = %v, want agentLaunchErrored (%v) — a substrate that refused the spawn must not read as a launched run", res.Fail, agentLaunchErrored)
	}
	if res.FailErr == nil {
		t.Error("FailErr = nil, want the raw launch error the caller wraps in its own phrasing")
	}
	if res.Session != nil {
		t.Errorf("Session = %#v, want nil after a failed launch", res.Session)
	}
}

// TestNewAgentLaunchLogf_PrefixIsNotPartOfTheFormatString pins the prefix as a
// VALUE. The prefix is caller-supplied — `daemon: dot: bead <id> node <id>` —
// and a bead id or an operator-authored graph node id may contain a '%'.
// Splicing it into the format string made that '%' a verb, consuming or
// mis-rendering the real arguments and garbling every line for the run.
func TestNewAgentLaunchLogf_PrefixIsNotPartOfTheFormatString(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logf := newAgentLaunchLogf(&buf, "daemon: dot: node 100%-done %s %d")
	logf("ForAgent(%s): %v (skipping ready-wait)", "claude_code", errors.New("no adapter"))

	got := buf.String()
	want := "daemon: dot: node 100%-done %s %d: ForAgent(claude_code): no adapter (skipping ready-wait)\n"
	if got != want {
		t.Errorf("logf output\n got: %q\nwant: %q", got, want)
	}
	if strings.Contains(got, "%!") {
		t.Errorf("output carries a fmt verb error (%%!…): %q — the prefix is being read as a format string", got)
	}
}

// TestNewAgentLaunchLogf_NoArgs covers the zero-argument call shape
// (refuseLaunch's `logf("%s (refusing launch)", reason)` has args; several
// call sites do not) so the append-based argument prepend is pinned at len 0 too.
func TestNewAgentLaunchLogf_NoArgs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	var w io.Writer = &buf
	newAgentLaunchLogf(w, "50%")("watcher.Done() reap timed out after Kill — continuing")

	want := "50%: watcher.Done() reap timed out after Kill — continuing\n"
	if got := buf.String(); got != want {
		t.Errorf("logf output\n got: %q\nwant: %q", got, want)
	}
}
