package daemon_test

// dot_reviewer_verdict_malformed_retry_hkvv10r_test.go — regression test for
// hk-vv10r.
//
// # The bug
//
// dispatchDotAgenticNode's finalize verdict read (dot_cascade.go, isReviewer
// branch) called workspace.ReadReviewVerdictVia directly. That function only
// retries a transient ErrMalformed on its REMOTE (SSH cat) branch; the
// local/nil-runner branch falls straight through to the bare, no-retry
// ReadReviewVerdict (by design — NFR7, so pollers like the quit-watchdog gate
// keep a fast absent/malformed return). But the DOT finalize read is NOT a
// poller: it runs exactly once, right after the reviewer node has already
// exited — the same shape as reviewloop.go's finalize read, which uses
// ReadReviewVerdictLocalRetry precisely so a review.json observed mid-flush
// doesn't false-fail the run. The DOT path never got that fix.
//
// # The fix (hk-vv10r)
//
// dispatchDotAgenticNode now routes its finalize verdict reads through
// readDotReviewVerdictRetry, which retries on ErrMalformed on BOTH the local
// and remote branch.
//
// # Scenario
//
// The reviewer node writes a TRUNCATED (invalid JSON) review.json, then
// backgrounds a delayed rewrite of a valid APPROVE verdict before exiting.
// dispatchDotAgenticNode's read races the truncated file first — before the
// fix this returns ErrMalformed immediately and the run hard-fails; after the
// fix the retry loop observes the later, valid write and the run completes.
//
// Bead: hk-vv10r.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// vv10rWriteDOT materialises a minimal review-loop DOT graph identical in
// shape to bqf1qReviewLoopDOT, to a temp file, and loads it.
func vv10rWriteDOT(t *testing.T) *dot.Graph {
	t.Helper()
	src := `digraph "hk-vv10r-malformed-retry" {
    schema_version="1"; version="1.0"; workflow_id="hk-vv10r-malformed-retry";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start         [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement     [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review        [type="agentic", agent_type="reviewer",    handler_ref="claude-reviewer",    idempotency_class="idempotent"];
    close         [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("vv10rWriteDOT: WriteFile: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("vv10rWriteDOT: LoadDotWorkflow: %v", err)
	}
	return g
}

// vv10rMalformedThenValidScript — implementer commits on iter 1, then the
// reviewer writes a truncated review.json and backgrounds a delayed rewrite
// with a valid APPROVE verdict before exiting.
func vv10rMalformedThenValidScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/vv10r_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter 1: commit real work -> HEAD advances past parentSHA.
    printf 'vv10r work' > "$WS/vv10r.txt"
    git -C "$WS" add "vv10r.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
        commit -m "vv10r impl iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer: write a truncated (invalid JSON) review.json synchronously,
    # then background a delayed rewrite with a valid APPROVE verdict. The
    # daemon's finalize read must observe the truncated file first and retry
    # until the valid write lands.
    mkdir -p "$WS/.harmonik"
    printf '{"schema_versio' > "$WS/.harmonik/review.json"
    ( sleep 5; printf '%%s' '%s' > "$WS/.harmonik/review.json" ) >/dev/null 2>&1 &
    disown 2>/dev/null || true
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, approve)
	scriptPath := filepath.Join(t.TempDir(), "vv10r_handler.sh")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("vv10rMalformedThenValidScript: WriteFile: %v", err)
	}
	return scriptPath
}

// TestDotReviewerVerdict_MalformedThenValid_LocalRetry_hkvv10r reproduces
// hk-vv10r: a local DOT run whose reviewer verdict read observes a truncated
// review.json mid-flush must retry rather than hard-failing the run.
func TestDotReviewerVerdict_MalformedThenValid_LocalRetry_hkvv10r(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := vv10rMalformedThenValidScript(t, wtPath)
	graph := vv10rWriteDOT(t)

	collector := &stubEventCollector{}
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           &stubBeadLedger{},
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeDot,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("dot-vv10r-malformed-then-valid"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("malformed-then-valid: result=%+v events=%v", result, events)

	if !result.Success {
		t.Fatalf("hk-vv10r: expected success=true (retry recovers the valid verdict written after the truncated read); summary=%q", result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("hk-vv10r: expected needs_attention=false; summary=%q", result.Summary)
	}
	rlAssertEventPresent(t, events, string(core.EventTypeReviewerVerdict))
}
