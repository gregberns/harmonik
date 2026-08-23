package daemon_test

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

const hk5rmtcThreadID = "th_hk5rmtc_captured_0001"

const hk5rmtcTrackingID = "019fe8d2-ced3-7465-9c9b-417b8d57be61"

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

const hk5rmtcRequestChangesJSON = `{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["fixture"],` +
	`"notes":"fixture reviewer asks for one more pass."}`

const hk5rmtcApproveJSON = `{"schema_version":1,"verdict":"APPROVE","flags":[],` +
	`"notes":"fixture reviewer approves the second pass."}`

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

type hk5rmtcLaunch struct {
	phase string
	prior *string
}

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

	for i, l := range got {
		if l.prior != nil && *l.prior == hk5rmtcTrackingID {
			t.Errorf("launch[%d] (phase %s) resumes the minted TRACKING id %q; codex has no rollout for it and answers -32600 (hk-codex-resume-wrong-threadid-5rmtc)",
				i, l.phase, hk5rmtcTrackingID)
		}
	}

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

func hk5rmtcDeref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
