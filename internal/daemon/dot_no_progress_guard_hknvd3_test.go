package daemon_test

// dot_no_progress_guard_hknvd3_test.go — unit tests for the configurable
// no-progress guard (hk-nvd3, PR#3 follow-up #1).
//
// # What is tested
//
// The no_progress_guard graph-level DOT attribute lets non-code workflows (doc,
// design, vendor) cap or disable the no-progress guard that fires at iter ≥ 2
// when HEAD has not advanced.  Three modes:
//
//   - "strict" / "" (default): fires on the FIRST no-progress detection (iter ≥ 2
//     + HEAD unchanged). This is today's exact behavior and must remain unchanged.
//   - "capped:N": allows up to N consecutive no-progress iterations; fires only
//     after the (N+1)th consecutive occurrence.
//   - "off": never fires; the run continues until the graph reaches a terminal node
//     or the absolute visit bound.
//
// # Completion exemptions are orthogonal
//
// The APPROVE-and-done (hk-8ps7q) and advisory-RC-and-green (hk-w2ow) completion
// exemptions are applied BEFORE the guard knob and bypass it regardless of mode.
// Those are tested separately in their own regression suites.
//
// # Acceptance gate (DONE MEANS per hk-nvd3 bead)
//
//   (1) A workflow-level knob controls whether/how strictly the guard fires.
//   (2) Default ("" / "strict") preserves today's strict behavior — no silent
//       loosening — verified by TestNoProgressGuard_Strict_DefaultFiresImmediately.
//   (3) A capped workflow does NOT fail where strict would, until the cap is
//       exceeded — verified by TestNoProgressGuard_Capped_AllowsUpToCap and
//       TestNoProgressGuard_Capped_FiresAfterCapExceeded.
//   (4) An "off" workflow never fires the guard — verified by
//       TestNoProgressGuard_Off_NeverFires.
//
// Bead: hk-nvd3.

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

// ── DOT fixture helpers ───────────────────────────────────────────────────────

// nvd3WriteDOT materialises a DOT graph string to a temp file and loads it.
func nvd3WriteDOT(t *testing.T, src string) *dot.Graph {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "g.dot")
	//nolint:gosec // G306: test-only fixture
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatalf("nvd3WriteDOT: write: %v", err)
	}
	g, err := workflow.LoadDotWorkflow(p)
	if err != nil {
		t.Fatalf("nvd3WriteDOT: LoadDotWorkflow: %v", err)
	}
	return g
}

// nvd3WriteScript writes a /bin/sh fixture script to a temp dir and returns its path.
func nvd3WriteScript(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatalf("nvd3WriteScript(%s): %v", name, err)
	}
	return p
}

// nvd3ReviewLoopDOT returns the standard implement→review topology with the
// given no_progress_guard value embedded as a graph-level attribute.
// An empty guardVal omits the attribute entirely (equivalent to "strict").
func nvd3ReviewLoopDOT(guardVal string) string {
	guardAttr := ""
	if guardVal != "" {
		guardAttr = fmt.Sprintf(` no_progress_guard=%q;`, guardVal)
	}
	return fmt.Sprintf(`digraph "hk-nvd3-review-loop" {
    schema_version="1"; version="1.0"; workflow_id="hk-nvd3-test";%s
    start_node="start"; terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="claude-implementer", idempotency_class="non-idempotent"];
    review [type="agentic", agent_type="reviewer", handler_ref="claude-reviewer", idempotency_class="idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> review;
    review -> close [condition="outcome.preferred_label == 'APPROVE'"];
    review -> implement [condition="outcome.preferred_label == 'REQUEST_CHANGES'", traversal_cap="5"];
    review -> "close-needs-attention" [condition="outcome.preferred_label == 'BLOCK'"];
    review -> "close-needs-attention";
}
`, guardAttr)
}


// ── scripts ───────────────────────────────────────────────────────────────────

// nvd3StrictScript: iter-1 commits then reviewer gives REQUEST_CHANGES; iter-2
// makes NO commit (HEAD unchanged, genuinely stuck). The strict guard must fire.
func nvd3StrictScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/nvd3_strict_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    printf 'strict-work' > "$WS/nvd3_strict.txt"
    git -C "$WS" add "nvd3_strict.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "nvd3 strict iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Iter-2 implementer: NO commit -> genuinely stuck.
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc)
	return nvd3WriteScript(t, "nvd3_strict.sh", script)
}

// nvd3CappedScript: iter-1 commits; reviewer gives REQUEST_CHANGES; iter-2
// makes NO commit (no-progress 1); reviewer again REQUEST_CHANGES; iter-3 makes
// NO commit (no-progress 2). With cap=1 the guard should fire at count=2
// (the second no-progress iteration). With cap=2 it should NOT fire here.
// The script is parameterised by maxIters so tests can stop early via exit 1
// on unexpected calls.
func nvd3CappedScript(t *testing.T, wtPath string, finalVerdict string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	fv := strings.ReplaceAll(rlFixtureVerdictJSON(finalVerdict), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/nvd3_capped_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter-1: commit real work.
    printf 'capped-work' > "$WS/nvd3_capped.txt"
    git -C "$WS" add "nvd3_capped.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "nvd3 capped iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter-1: REQUEST_CHANGES -> back to implementer.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter-2: NO commit -> first no-progress.
    ;;
  4)
    # Reviewer iter-2: REQUEST_CHANGES -> back to implementer.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  5)
    # Implementer iter-3: NO commit -> second no-progress.
    ;;
  6)
    # Reviewer iter-3: emit final verdict.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc, rc, fv)
	return nvd3WriteScript(t, "nvd3_capped.sh", script)
}

// nvd3OffScript: iter-1 commits; reviewer gives REQUEST_CHANGES; iter-2 makes
// NO commit; reviewer APPROVES.  With guard="off" the run should complete
// (the APPROVE at iter-2 produces the approved-and-done exemption).
func nvd3OffScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	rc := strings.ReplaceAll(rlFixtureVerdictJSON("REQUEST_CHANGES"), "'", "'\\''")
	approve := strings.ReplaceAll(rlFixtureVerdictJSON("APPROVE"), "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/nvd3_off_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE"); CNT=$((CNT + 1)); printf '%%d' "$CNT" > "$CNT_FILE"
case "$CNT" in
  1)
    # Implementer iter-1: commit real work.
    printf 'off-work' > "$WS/nvd3_off.txt"
    git -C "$WS" add "nvd3_off.txt" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "nvd3 off iter1" --no-gpg-sign >/dev/null 2>&1
    ;;
  2)
    # Reviewer iter-1: REQUEST_CHANGES -> back to implementer.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  3)
    # Implementer iter-2: NO commit (no-progress situation).
    # With guard="off" this must NOT fire the guard.
    ;;
  4)
    # Reviewer iter-2: APPROVE -> should reach close terminal.
    mkdir -p "$WS/.harmonik"; printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  *)
    exit 1 ;;
esac
exit 0
`, wtpEsc, rc, approve)
	return nvd3WriteScript(t, "nvd3_off.sh", script)
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestNoProgressGuard_Strict_DefaultFiresImmediately verifies that a workflow
// with no no_progress_guard attribute (empty = default = strict) fires the guard
// on the first no-progress iteration (iter ≥ 2, HEAD unchanged).  This is the
// negative-guard invariant from hk-togxq preserved by hk-nvd3.
func TestNoProgressGuard_Strict_DefaultFiresImmediately(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := nvd3StrictScript(t, wtPath)
	graph := nvd3WriteDOT(t, nvd3ReviewLoopDOT("")) // empty = strict

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
		core.BeadID("nvd3-strict"), wtPath, parentSHA, graph)

	t.Logf("strict: result=%+v events=%v", result, collector.eventTypes())

	if result.Success {
		t.Errorf("strict: expected success=false (guard must fire immediately at first no-progress); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("strict: expected needs_attention=true; summary=%q", result.Summary)
	}
	if !strings.Contains(result.Summary, "no-progress") && !strings.Contains(result.Summary, "fix-up stalled") {
		t.Errorf("strict: expected summary to contain no-progress or fix-up stalled; got %q", result.Summary)
	}
}

// TestNoProgressGuard_Strict_ExplicitFiresImmediately verifies that an explicit
// no_progress_guard="strict" attribute fires on the first no-progress iteration,
// identically to the default.
func TestNoProgressGuard_Strict_ExplicitFiresImmediately(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := nvd3StrictScript(t, wtPath)
	graph := nvd3WriteDOT(t, nvd3ReviewLoopDOT("strict"))

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
		core.BeadID("nvd3-strict-explicit"), wtPath, parentSHA, graph)

	t.Logf("strict-explicit: result=%+v events=%v", result, collector.eventTypes())

	if result.Success {
		t.Errorf("strict-explicit: expected success=false; summary=%q", result.Summary)
	}
}

// TestNoProgressGuard_Capped_AllowsUpToCap verifies that a workflow with
// no_progress_guard="capped:2" does NOT fail at the first no-progress iteration
// (which would be iteration count 1 of 2 allowed); the run continues.  The
// subsequent APPROVE (after the second no-progress iteration is within cap)
// causes the run to complete successfully.
func TestNoProgressGuard_Capped_AllowsUpToCap(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	// cap=2: allows 2 consecutive no-progress iterations; fires on the 3rd.
	// Our script has iter-1 commit + RC, iter-2 no-commit (1st no-progress allowed),
	// iter-3 no-commit (2nd no-progress allowed), then reviewer APPROVEs at iter-3.
	// The APPROVE at iter-3 also triggers the approved-and-done exemption, so the
	// run completes successfully via that path — proving the guard did NOT fire
	// prematurely at no-progress count 1.
	scriptPath := nvd3CappedScript(t, wtPath, "APPROVE")
	graph := nvd3WriteDOT(t, nvd3ReviewLoopDOT("capped:2"))

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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("nvd3-capped-within"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("capped-within-cap: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("capped-within-cap: expected success=true (guard must not fire within cap=2); summary=%q", result.Summary)
	}
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) || et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("capped-within-cap: guard fired (%s) but cap=2 allows 2 consecutive no-progress iterations; events=%v", et, events)
		}
	}
}

// TestNoProgressGuard_Capped_FiresAfterCapExceeded verifies that a workflow
// with no_progress_guard="capped:1" DOES fail after the second consecutive
// no-progress iteration (cap=1 allows only 1; the 2nd exceeds it).
func TestNoProgressGuard_Capped_FiresAfterCapExceeded(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	// cap=1: allows 1 consecutive no-progress iteration; fires on the 2nd.
	// Our script: iter-1 commit + RC, iter-2 no-commit (1st = allowed), iter-3
	// no-commit (2nd = exceeds cap → must fire).  Final verdict is REQUEST_CHANGES
	// to ensure no advisory-RC+green exemption applies (no gate node here).
	scriptPath := nvd3CappedScript(t, wtPath, "REQUEST_CHANGES")
	graph := nvd3WriteDOT(t, nvd3ReviewLoopDOT("capped:1"))

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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	result := daemon.ExportedDriveDotWorkflow(ctx, deps, rlFixtureRunID(t),
		core.BeadID("nvd3-capped-exceeded"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("capped-exceeded: result=%+v events=%v", result, events)

	if result.Success {
		t.Errorf("capped-exceeded: expected success=false (cap=1 exceeded at 2nd no-progress iteration); summary=%q", result.Summary)
	}
	if !result.NeedsAttention {
		t.Errorf("capped-exceeded: expected needs_attention=true; summary=%q", result.Summary)
	}
	// Guard must have fired (as review_fixup_stalled, since prior verdict was REQUEST_CHANGES).
	found := false
	for _, et := range events {
		if et == string(core.EventTypeReviewFixupStalled) || et == string(core.EventTypeNoProgressDetected) {
			found = true
		}
	}
	if !found {
		t.Errorf("capped-exceeded: expected review_fixup_stalled or no_progress_detected event; got events=%v", events)
	}
}

// TestNoProgressGuard_Off_NeverFires verifies that a workflow with
// no_progress_guard="off" does NOT fail when HEAD has not advanced; the run
// continues until another exit path (here: APPROVE after the no-progress round)
// produces a success terminal.
func TestNoProgressGuard_Off_NeverFires(t *testing.T) {
	t.Parallel()
	projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
	scriptPath := nvd3OffScript(t, wtPath)
	graph := nvd3WriteDOT(t, nvd3ReviewLoopDOT("off"))

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
		core.BeadID("nvd3-off"), wtPath, parentSHA, graph)

	events := collector.eventTypes()
	t.Logf("off: result=%+v events=%v", result, events)

	if !result.Success {
		t.Errorf("off: expected success=true (guard=off must not fire; run reaches APPROVE→close); summary=%q", result.Summary)
	}
	for _, et := range events {
		if et == string(core.EventTypeNoProgressDetected) || et == string(core.EventTypeReviewFixupStalled) {
			t.Errorf("off: guard event %s fired but no_progress_guard=\"off\" must suppress it; events=%v", et, events)
		}
	}
}
