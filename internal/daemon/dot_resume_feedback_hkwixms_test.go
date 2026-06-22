package daemon_test

// dot_resume_feedback_hkwixms_test.go — regression tests for hk-wixms.
//
// # The bug
//
// The DOT cascade's implement RE-ENTRY back-edges in dot_cascade.go —
// both reviewer→implement (REQUEST_CHANGES) and commit_gate→implement (no
// commit / deterministic gate FAIL) — resumed the implementer session
// (claude --resume <prior-session-id>) but handed it NO actionable instruction:
//
//   - The reviewer-feedback file (reviewer-feedback.iter-<N-1>.md) was never
//     written in dot_cascade.go — WriteReviewerFeedback was called ONLY in
//     reviewloop.go (the builtin review loop). So pasteInjectImplementerResume
//     saw feedbackExists=false and degraded to a bare "read agent-task.md and
//     begin." message.
//   - The resumed implementer (in its iter-1 session that already produced
//     satisfying work) then had nothing concrete to do → sat active-but-idle
//     until the budget watchdog killed it → no commit → no_progress →
//     re-dispatch → thrash.
//
// # The fix (hk-wixms)
//
// dot_cascade.go now mirrors reviewloop.go's feedback delivery on the
// implementer-resume back-edges (iterationCount >= 2), distinguishing the two
// re-entry causes:
//
//   - reviewer→implement (REQUEST_CHANGES): WriteReviewerFeedback carrying the
//     prior reviewer's verdict, flags, and notes verbatim.
//   - commit_gate→implement (no commit / gate FAIL, no review verdict yet):
//     WriteReviewerFeedback carrying an explicit "your previous pass produced
//     NO commit — you MUST commit your changes" nudge.
//
// It also emits implementer_resumed (workflow_mode="dot") before the resume
// dispatch, mirroring the builtin path.
//
// # Spec refs
//   - specs/execution-model.md EM-015d-RFD (reviewer-feedback delivery file)
//   - specs/event-model.md §8.1a.1 (implementer_resumed; prior_verdict_summary)
//
// Bead: hk-wixms.

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
	"github.com/gregberns/harmonik/internal/workspace"
)

// wixmsReviewLoopDOT is the standard implement→review topology with the
// REQUEST_CHANGES back-edge to implement (cap 3).
func wixmsReviewLoopDOT() string {
	return `digraph "hk-wixms-review-loop" {
    schema_version="1"; version="1.0"; workflow_id="hk-wixms-review-loop";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`
}

// wixmsGateDOT is start → implement → gate, where the gate passes only once a
// marker file exists in the worktree (created by the iter-2 implementer). The
// gate FAILs deterministically until then, driving the commit_gate→implement
// no-commit back-edge.
func wixmsGateDOT(wtPath string) string {
	marker := filepath.Join(wtPath, ".harmonik", "wixms_gate_ok")
	return fmt.Sprintf(`digraph "hk-wixms-gate" {
    schema_version="1"; version="1.0"; workflow_id="hk-wixms-gate";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="test -f %s", timeout="30"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> gate;
    gate -> close [condition="outcome.status == 'SUCCESS'"];
    gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="3"];
    gate -> "close-needs-attention";
}
`, marker)
}

func wixmsWriteDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("write DOT: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("LoadDotWorkflow: %v", err)
	}
	return g
}

func wixmsWriteScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("wixmsWriteScript(%s): %v", name, err)
	}
	return p
}

// wixmsRequestChangesNotes is the distinctive marker carried in the iter-1
// reviewer's REQUEST_CHANGES notes; the test asserts it reaches the
// reviewer-feedback.iter-1.md file the resumed implementer reads.
const wixmsRequestChangesNotes = "WIXMS-RC-MARKER: rename the helper and add the missing nil-guard"

// ── Case A: reviewer REQUEST_CHANGES back-edge delivers reviewer feedback ─────

func wixmsRCScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(fmt.Sprintf(
		`{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["wixms-flag"],"notes":"%s"}`,
		wixmsRequestChangesNotes), "'", "'\\''")
	approve := strings.ReplaceAll(
		`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"addressed"}`, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
FEEDBACK_FILE="$WTP/.harmonik/reviewer-feedback.iter-1.md"
CNT_FILE="$WTP/.harmonik/wixms_rc_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter-1 implementer: commit (HEAD advances).
    printf 'v1' > "$WS/wixms_rc.txt"
    git -C "$WS" add "wixms_rc.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "wixms rc iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # iter-1 reviewer: REQUEST_CHANGES with the distinctive marker notes.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # iter-2 implementer-resume: record whether the feedback file is present and
    # whether it carries the reviewer marker, then commit a DIFFERENT file so
    # HEAD advances (avoids no_progress masking the assertion).
    if [ -f "$FEEDBACK_FILE" ] && grep -q 'WIXMS-RC-MARKER' "$FEEDBACK_FILE"; then
      printf 'present' > "$WTP/.harmonik/wixms_rc_status.txt"
    else
      printf 'absent' > "$WTP/.harmonik/wixms_rc_status.txt"
    fi
    printf 'v2' > "$WS/wixms_rc2.txt"
    git -C "$WS" add "wixms_rc2.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "wixms rc iter2" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter-2 reviewer: APPROVE.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
esac
exit 0
`, wtpEsc, rc, approve)
	return wixmsWriteScript(t, "wixms_rc.sh", script)
}

// TestDotResume_ReviewerFeedbackDelivered_hkwixms proves that on a
// reviewer→implement REQUEST_CHANGES back-edge the DOT cascade writes
// reviewer-feedback.iter-1.md carrying the reviewer's notes, so the resumed
// implementer receives an actionable instruction (pre-fix: the file was absent
// and the message degraded to "read agent-task.md and begin").
func TestDotResume_ReviewerFeedbackDelivered_hkwixms(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := wixmsRCScript(t, wtPath)
	graph := wixmsWriteDOT(t, wixmsReviewLoopDOT())

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
		core.BeadID("dot-wixms-rc"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("case-A: result=%+v events=%v", result, events)

	// The feedback file must exist and carry the reviewer's marker notes.
	feedbackPath := workspace.ReviewerFeedbackPath(wtPath, 1)
	content, err := os.ReadFile(feedbackPath)
	if err != nil {
		t.Fatalf("hk-wixms: reviewer-feedback.iter-1.md absent after REQUEST_CHANGES back-edge: %v", err)
	}
	if !strings.Contains(string(content), "WIXMS-RC-MARKER") {
		t.Errorf("hk-wixms: reviewer-feedback.iter-1.md does not carry the reviewer notes; content=%q", string(content))
	}

	// The iter-2 implementer must have SEEN the feedback file (sentinel).
	statusPath := filepath.Join(wtPath, ".harmonik", "wixms_rc_status.txt")
	if status, sErr := os.ReadFile(statusPath); sErr != nil {
		t.Errorf("hk-wixms: feedback-status sentinel not found: %v", sErr)
	} else if string(status) != "present" {
		t.Errorf("hk-wixms: feedback file was %q at iter-2 implementer launch; want \"present\"", string(status))
	}

	// implementer_resumed (workflow_mode=dot) must have been emitted.
	rlAssertEventPresent(t, events, string(core.EventTypeImplementerResumed))

	if !result.Success {
		t.Errorf("hk-wixms case-A: expected success=true (RC→iter2→APPROVE→close); summary=%q", result.Summary)
	}
}

// ── Case B: commit_gate→implement no-commit bounce delivers a commit nudge ────

func wixmsGateScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
FEEDBACK_FILE="$WTP/.harmonik/reviewer-feedback.iter-1.md"
CNT_FILE="$WTP/.harmonik/wixms_gate_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # iter-1 implementer: commit (HEAD advances) but do NOT create the gate
    # marker, so the gate FAILs deterministically and bounces back to implement.
    printf 'v1' > "$WS/wixms_gate.txt"
    git -C "$WS" add "wixms_gate.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "wixms gate iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  *)
    # iter-2 implementer-resume (commit_gate bounce): record whether the
    # commit-nudge feedback file is present and carries the NO-commit nudge, then
    # commit a new file AND create the gate marker so the gate passes next.
    if [ -f "$FEEDBACK_FILE" ] && grep -q 'NO commit' "$FEEDBACK_FILE"; then
      printf 'present' > "$WTP/.harmonik/wixms_gate_status.txt"
    else
      printf 'absent' > "$WTP/.harmonik/wixms_gate_status.txt"
    fi
    printf 'v2' > "$WS/wixms_gate2.txt"
    git -C "$WS" add "wixms_gate2.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "wixms gate iter2" --no-gpg-sign >/dev/null 2>&1
    : > "$WTP/.harmonik/wixms_gate_ok"
    ;;
esac
exit 0
`, wtpEsc)
	return wixmsWriteScript(t, "wixms_gate.sh", script)
}

// TestDotResume_CommitNudgeDelivered_hkwixms proves that on a
// commit_gate→implement no-commit bounce (deterministic gate FAIL, no review
// verdict yet) the DOT cascade writes reviewer-feedback.iter-1.md carrying an
// explicit NO-commit nudge, so the resumed implementer is told to commit (pre-fix:
// the file was absent and the resume degraded to "read agent-task.md and begin").
func TestDotResume_CommitNudgeDelivered_hkwixms(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := wixmsGateScript(t, wtPath)
	graph := wixmsWriteDOT(t, wixmsGateDOT(wtPath))

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
		core.BeadID("dot-wixms-gate"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("case-B: result=%+v events=%v", result, events)

	// The feedback file must exist and carry the NO-commit nudge.
	feedbackPath := workspace.ReviewerFeedbackPath(wtPath, 1)
	content, err := os.ReadFile(feedbackPath)
	if err != nil {
		t.Fatalf("hk-wixms: reviewer-feedback.iter-1.md absent after commit_gate bounce: %v", err)
	}
	if !strings.Contains(string(content), "NO commit") {
		t.Errorf("hk-wixms: commit-nudge feedback file does not carry the NO-commit nudge; content=%q", string(content))
	}

	// The iter-2 implementer must have SEEN the nudge (sentinel).
	statusPath := filepath.Join(wtPath, ".harmonik", "wixms_gate_status.txt")
	if status, sErr := os.ReadFile(statusPath); sErr != nil {
		t.Errorf("hk-wixms: gate-nudge sentinel not found: %v", sErr)
	} else if string(status) != "present" {
		t.Errorf("hk-wixms: commit-nudge file was %q at iter-2 implementer launch; want \"present\"", string(status))
	}

	// implementer_resumed (workflow_mode=dot) must have been emitted.
	rlAssertEventPresent(t, events, string(core.EventTypeImplementerResumed))
}

// ── Case C: mixed reviewer+gate trace — discrimination keys on the inbound edge ─
//
// review[RC]→implement[commits]→commit_gate[FAIL]→implement. Pre-(prevNodeID-fix)
// this delivered the STALE reviewer notes on the gate-bounce re-entry because
// priorVerdict still read REQUEST_CHANGES. With the prevNodeID discrimination the
// FIRST re-entry (from review) gets the reviewer feedback (iter-1 file) and the
// SECOND re-entry (from commit_gate) gets the commit nudge (iter-2 file).

func wixmsMixedDOT(wtPath string) string {
	gateOK := filepath.Join(wtPath, ".harmonik", "wixms_mixed_gate_ok")
	return fmt.Sprintf(`digraph "hk-wixms-mixed" {
    schema_version="1"; version="1.0"; workflow_id="hk-wixms-mixed";
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    gate [type="non-agentic", handler_ref="shell", idempotency_class="idempotent", tool_command="test -f %s", timeout="30"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> gate;
    gate -> review [condition="outcome.status == 'SUCCESS'"];
    gate -> implement [condition="outcome.status == 'FAIL' && outcome.failure_class == 'deterministic'", traversal_cap="3"];
    gate -> "close-needs-attention";
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="3"];
    review -> "close-needs-attention";
}
`, gateOK)
}

func wixmsMixedScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(fmt.Sprintf(
		`{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["wixms-flag"],"notes":"%s"}`,
		wixmsRequestChangesNotes), "'", "'\\''")
	approve := strings.ReplaceAll(
		`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"addressed"}`, "'", "'\\''")
	// Handler invocation counter (CNT) — the gate is a non-agentic shell node and
	// does NOT call this handler, so CNT advances only on implementer/reviewer
	// dispatches. Walk:
	//   CNT=1 impl:   commit + create gate_ok     -> gate SUCCESS -> review
	//   CNT=2 review: RC                           -> implement (inbound edge: review)
	//   CNT=3 impl:   commit, REMOVE gate_ok       -> gate FAIL  -> implement (inbound edge: gate)
	//   CNT=4 impl:   commit + create gate_ok      -> gate SUCCESS -> review
	//   CNT=5 review: APPROVE                       -> close
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
GATE_OK="$WTP/.harmonik/wixms_mixed_gate_ok"
FB1="$WTP/.harmonik/reviewer-feedback.iter-1.md"
FB2="$WTP/.harmonik/reviewer-feedback.iter-2.md"
CNT_FILE="$WTP/.harmonik/wixms_mixed_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    printf 'v1' > "$WS/wixms_mixed_1.txt"
    git -C "$WS" add "wixms_mixed_1.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "mixed iter1" --no-gpg-sign >/dev/null 2>&1
    : > "$GATE_OK"
    ;;
  2)
    # reviewer: REQUEST_CHANGES.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # implement re-entry FROM REVIEW (RC): the iter-1 feedback file must carry
    # the reviewer marker. Commit (addresses RC) but REMOVE gate_ok so the gate
    # FAILs and bounces back to implement.
    if [ -f "$FB1" ] && grep -q 'WIXMS-RC-MARKER' "$FB1"; then
      printf 'rc' > "$WTP/.harmonik/wixms_mixed_fb1.txt"
    else
      printf 'missing' > "$WTP/.harmonik/wixms_mixed_fb1.txt"
    fi
    printf 'v2' > "$WS/wixms_mixed_2.txt"
    git -C "$WS" add "wixms_mixed_2.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "mixed iter2" --no-gpg-sign >/dev/null 2>&1
    rm -f "$GATE_OK"
    ;;
  4)
    # implement re-entry FROM COMMIT_GATE (FAIL): the iter-2 feedback file must
    # carry the NO-commit nudge (NOT the stale reviewer notes). Commit + recreate
    # gate_ok so the gate passes and we reach the APPROVE reviewer.
    if [ -f "$FB2" ] && grep -q 'NO commit' "$FB2"; then
      printf 'nudge' > "$WTP/.harmonik/wixms_mixed_fb2.txt"
    elif [ -f "$FB2" ] && grep -q 'WIXMS-RC-MARKER' "$FB2"; then
      printf 'stale-rc' > "$WTP/.harmonik/wixms_mixed_fb2.txt"
    else
      printf 'missing' > "$WTP/.harmonik/wixms_mixed_fb2.txt"
    fi
    printf 'v3' > "$WS/wixms_mixed_3.txt"
    git -C "$WS" add "wixms_mixed_3.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "mixed iter3" --no-gpg-sign >/dev/null 2>&1
    : > "$GATE_OK"
    ;;
  *)
    # reviewer: APPROVE.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
esac
exit 0
`, wtpEsc, rc, approve)
	return wixmsWriteScript(t, "wixms_mixed.sh", script)
}

// TestDotResume_MixedReviewerGate_EdgeSourceDiscriminates_hkwixms proves the
// implementer-resume message keys on the INBOUND EDGE SOURCE: a review[RC] bounce
// gets the reviewer verdict, and a subsequent commit_gate[FAIL] bounce in the
// SAME run gets the commit nudge — not the stale REQUEST_CHANGES notes.
func TestDotResume_MixedReviewerGate_EdgeSourceDiscriminates_hkwixms(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := wixmsMixedScript(t, wtPath)
	graph := wixmsWriteDOT(t, wixmsMixedDOT(wtPath))

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

	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("dot-wixms-mixed"), wtPath, parentSHA, graph)

	t.Logf("case-C: result=%+v events=%v", result, collector.eventTypes())

	// iter-1 feedback (from review RC) must carry the reviewer notes.
	if fb1, err := os.ReadFile(filepath.Join(wtPath, ".harmonik", "wixms_mixed_fb1.txt")); err != nil {
		t.Errorf("hk-wixms case-C: review→implement sentinel (fb1) not found: %v", err)
	} else if string(fb1) != "rc" {
		t.Errorf("hk-wixms case-C: review→implement re-entry saw %q feedback; want reviewer notes (\"rc\")", string(fb1))
	}

	// iter-2 feedback (from commit_gate FAIL) must carry the NO-commit nudge,
	// NOT the stale reviewer notes — this is the prevNodeID discrimination.
	if fb2, err := os.ReadFile(filepath.Join(wtPath, ".harmonik", "wixms_mixed_fb2.txt")); err != nil {
		t.Errorf("hk-wixms case-C: iter-3 implementer sentinel (fb2) not found: %v", err)
	} else if string(fb2) == "stale-rc" {
		t.Errorf("hk-wixms case-C REGRESSION: commit_gate→implement re-entry delivered STALE reviewer notes instead of the commit nudge")
	} else if string(fb2) != "nudge" {
		t.Errorf("hk-wixms case-C: commit_gate→implement re-entry saw %q feedback; want commit nudge (\"nudge\")", string(fb2))
	}
}
