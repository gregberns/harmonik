package daemon_test

// dot_codex_resume_threadid_hk5rmtc_test.go — regression test for
// hk-codex-resume-wrong-threadid-5rmtc.
//
// # The bug
//
// The graph cascade resumed a codex session with an identifier codex never
// issued, and codex correctly refused it:
//
//	implementer_resumed        {"iteration_count":2,"claude_session_id":"019fe8d2-…"}
//	implementer_phase_complete {"exit_code":1,"stderr_tail_head":
//	  "Error: thread/resume: thread/resume failed: no rollout found for thread id
//	   019fe8d2-… (code -32600)"}
//	run_failed
//
// That identifier is the TRACKING uuid buildCodexRoutedLaunchSpec mints for its
// own bookkeeping. The launch path DID wire a session-id interceptor that
// captures the real codex thread_id, but it wrote the captured value into a
// buffered channel with no reader ("buffered; no site reads it back"), and the
// cascade carried the tracking uuid forward instead. Any codex bead that failed
// a gate once took this path and died in about 3.5 seconds — and a bead with no
// workflow label gets the project default graph, which has a commit gate, so a
// second pass is normal.
//
// # What this test pins
//
// Through the real cascade, on a two-pass codex run: the second implementer
// launch is handed the thread id the harness reported, never the tracking uuid.
//
// Helper prefix: hk5rmtc (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/harness/shared"
)

// hk5rmtcThreadID is the identifier the fake codex process announces on stdout —
// the ONLY value codex would accept on a resume.
const hk5rmtcThreadID = "th_hk5rmtc_captured_0001"

// hk5rmtcTrackingID stands in for the uuid buildCodexRoutedLaunchSpec mints for
// its own bookkeeping. codex has no rollout for it. It is the literal value the
// live run died on.
const hk5rmtcTrackingID = "019fe8d2-ced3-7465-9c9b-417b8d57be61"

// hk5rmtcGraph is the reviewer topology with a REQUEST_CHANGES back-edge — the
// shape that produces a second implementer pass, which is the shape that failed.
const hk5rmtcGraph = `digraph "hk5rmtc-back-edge" {
    schema_version="1";
    version="1.0";
    workflow_id="hk5rmtc-back-edge";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", role="entry"];
    implement [type="agentic", agent_type="implementer", handler_ref="codex-implementer", idempotency_class="non-idempotent", role="produce the change"];
    review [type="agentic", agent_type="reviewer", handler_ref="codex-reviewer", idempotency_class="idempotent", role="render a verdict"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", role="success terminal"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent", role="failure terminal"];

    start -> implement;
    implement -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention";
}
`

// hk5rmtcRequestChangesJSON and hk5rmtcApproveJSON are the two verdicts the
// fixture reviewer writes, in that order.
const hk5rmtcRequestChangesJSON = `{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["fixture"],` +
	`"notes":"fixture reviewer asks for one more pass."}`

const hk5rmtcApproveJSON = `{"schema_version":1,"verdict":"APPROVE","flags":[],` +
	`"notes":"fixture reviewer approves the second pass."}`

// hk5rmtcHandler writes the fake codex process both agentic nodes run. It
// announces a thread id on stdout the way `codex exec --json` does, then plays
// the four-invocation sequence: implement, REQUEST_CHANGES, implement again,
// APPROVE.
//
// The invocation counter lives OUTSIDE the run worktree so the implementer's
// commits cannot disturb it.
func hk5rmtcHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	counter := filepath.Join(t.TempDir(), "hk5rmtc_count")
	body := "" +
		"printf '{\"type\":\"thread.started\",\"thread_id\":\"" + hk5rmtcThreadID + "\"}\\n'\n" +
		"CNT_FILE='" + counter + "'\n" +
		"if [ ! -f \"$CNT_FILE\" ]; then printf '0' > \"$CNT_FILE\"; fi\n" +
		"CNT=$(cat \"$CNT_FILE\"); CNT=$((CNT + 1)); printf '%d' \"$CNT\" > \"$CNT_FILE\"\n" +
		"case \"$CNT\" in\n" +
		"  2)\n" +
		"    mkdir -p .harmonik\n" +
		"    printf '%s' '" + hk5rmtcRequestChangesJSON + "' > .harmonik/review.json\n" +
		"    exit 0 ;;\n" +
		"  4)\n" +
		"    mkdir -p .harmonik\n" +
		"    printf '%s' '" + hk5rmtcApproveJSON + "' > .harmonik/review.json\n" +
		"    exit 0 ;;\n" +
		"esac\n" +
		"printf 'hk5rmtc pass %s' \"$CNT\" > hk5rmtc_pass_$CNT.txt\n" +
		dotFixtureCommitLines(bead) +
		"exit 0\n"
	return dotFixtureHandlerScript(t, "hk5rmtc-codex.sh", body)
}

// hk5rmtcLaunch records one launch: the phase and the prior-session id the
// cascade handed the spec builder.
type hk5rmtcLaunch struct {
	phase string
	prior *string
}

// hk5rmtcRecorder is the launch-spec port. It has the same SHAPE as the
// production buildCodexRoutedLaunchSpec — resolved agent type codex plus a
// harmonik-minted TRACKING session id codex itself never sees — while running a
// shell script instead of the real codex binary.
type hk5rmtcRecorder struct {
	script string

	mu       sync.Mutex
	launches []hk5rmtcLaunch
}

func (r *hk5rmtcRecorder) build(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	r.mu.Lock()
	var prior *string
	if rc.PriorClaudeSessID != nil {
		v := *rc.PriorClaudeSessID
		prior = &v
	}
	r.launches = append(r.launches, hk5rmtcLaunch{phase: string(rc.Phase), prior: prior})
	r.mu.Unlock()

	spec := handler.LaunchSpec{
		Binary:  "/bin/sh",
		Args:    []string{r.script},
		WorkDir: rc.WorkspacePath,
		Role:    string(rc.Phase),
	}
	arts := shared.LaunchArtifacts{
		ClaudeSessionID:   hk5rmtcTrackingID,
		HandlerSessionID:  hk5rmtcTrackingID,
		ResolvedAgentType: core.AgentTypeCodex,
	}
	return spec, arts, nil
}

func (r *hk5rmtcRecorder) snapshot() []hk5rmtcLaunch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hk5rmtcLaunch(nil), r.launches...)
}

// TestDotCodexBackEdgeResumesCapturedThreadID_hk5rmtc drives the real graph
// cascade over a two-pass codex run and asserts the second implementer launch
// targets the thread id the harness reported.
//
// Before the fix that launch carried the minted tracking uuid, and the live run
// died with "no rollout found for thread id … (code -32600)".
func TestDotCodexBackEdgeResumesCapturedThreadID_hk5rmtc(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk5rmtc-codex-resume")

	// The codex harness is what makes SessionIDPolicy() == SessionIDCaptured, so
	// the launch path forces the exec path and wires the thread-id interceptor.
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	script := hk5rmtcHandler(t, beadID)
	rec := &hk5rmtcRecorder{script: script}

	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		WorkflowMode:      core.WorkflowModeDot,
		Graph:             hk5rmtcGraph,
		HandlerScript:     script,
		HookOutcome:       dotFixtureWorkComplete,
		HarnessRegistry:   reg,
		LaunchSpecBuilder: rec.build,
	})

	got := rec.snapshot()
	for i, l := range got {
		t.Logf("launch[%d] phase=%s prior=%s", i, l.phase, hk5rmtcDeref(l.prior))
	}

	// No launch may ever be told to resume the tracking uuid: that is the exact
	// value codex rejected live.
	for i, l := range got {
		if l.prior != nil && *l.prior == hk5rmtcTrackingID {
			t.Errorf("launch[%d] (phase %s) resumes the minted TRACKING id %q; codex has no rollout for it and answers -32600 (hk-codex-resume-wrong-threadid-5rmtc)",
				i, l.phase, hk5rmtcTrackingID)
		}
	}

	// The back-edge implementer-resume must target the captured thread id.
	var resumes int
	for i, l := range got {
		if !strings.Contains(l.phase, "resume") {
			continue
		}
		resumes++
		if l.prior == nil {
			t.Errorf("launch[%d] is an implementer-resume with no prior session id; want the captured thread id %q", i, hk5rmtcThreadID)
			continue
		}
		if *l.prior != hk5rmtcThreadID {
			t.Errorf("launch[%d] implementer-resume prior session id = %q; want the captured thread id %q",
				i, *l.prior, hk5rmtcThreadID)
		}
	}
	if resumes == 0 {
		t.Fatalf("no implementer-resume launch happened, so the back-edge never ran; launches=%d events=%v",
			len(got), res.Bus.eventTypes())
	}
}

// hk5rmtcDeref renders a prior-session pointer for test logs.
func hk5rmtcDeref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
