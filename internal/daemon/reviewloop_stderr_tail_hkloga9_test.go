package daemon_test

// reviewloop_stderr_tail_hkloga9_test.go — scenario test for hk-loga9.
//
// # Bug context
//
// hk-ajhqw added stderr-tail surfacing for the single-workflow path
// (workloop.go:1428-1441) when the implementer subprocess crashes
// without producing NDJSON output. The review-loop path
// (reviewloop.go ~ implementer wait site) discarded the exit info via
// `_ = implEI`, so when the implementer claude crashed silently in
// review-loop mode (the default at the time of hk-g0ckv; dot is the default since hk-30vlb), the daemon had
// zero diagnostic context to surface.
//
// On 2026-05-21 dispatches for hk-xegej / hk-yejfj / hk-g0ckv first try
// all hit this — implementer never reached the bridge handshake,
// synthetic-claude-session fallback fired, and no stderr appeared
// anywhere.
//
// # Expected behaviour
//
// When the implementer phase exits non-zero on iteration 1 without
// advancing the worktree HEAD, the review loop's no_commit_during_implementer
// failure path MUST include the implementer's stderr tail in
// reviewLoopResult.Summary so the noChange-timeout fail-reason carries
// the crash diagnostic.
//
// # What this test asserts (BEFORE fix it FAILS)
//
//  1. result.Success == false
//  2. result.Summary contains the literal stderr content emitted by the
//     handler (e.g. "boom: handler crashed in reasoning phase").
//  3. result.Summary contains an exit-code marker (`exit=`).
//
// Pre-fix (commit 0173179): summary is the static string
// "no_commit_during_implementer: HEAD did not advance past parent ..."
// with no stderr — assertion 2 fails.
//
// Helper prefix: `loga9` (per implementer-protocol §Helper-prefix discipline).
//
// Bead: hk-loga9. Refs: hk-ajhqw (workloop equivalent), hk-yozgd
// (diagnostic gap that motivated this).

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
)

const loga9StderrMarker = "boom-loga9: handler crashed in reasoning phase before any commit"

// loga9HandlerScriptStderrCrash writes a handler that:
//   - On the first invocation (implementer): writes a distinctive
//     diagnostic line to stderr and exits with a non-zero code WITHOUT
//     committing anything to the worktree. HEAD stays at parentSHA.
//   - On any subsequent invocation: emits an APPROVE verdict. This branch
//     MUST NOT fire because the no-commit guard (hk-9c1v4) short-circuits
//     before reviewer dispatch.
func loga9HandlerScriptStderrCrash(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
WTP='%s'
CNT_FILE="$WTP/.harmonik/loga9_count"
mkdir -p "$WTP/.harmonik"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ "$CNT" -eq 1 ]; then
  # Implementer: emit stderr diagnostic, exit non-zero without committing.
  printf '%%s\n' '%s' 1>&2
  exit 42
fi
# Defensive: reviewer should never run; emit APPROVE to surface regression.
printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"loga9 reviewer must NOT have run"}' > "$WTP/.harmonik/review.json"
exit 0
`, wtpEsc, loga9StderrMarker)
	scriptPath := filepath.Join(t.TempDir(), "loga9_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("loga9HandlerScriptStderrCrash: WriteFile: %v", err)
	}
	return scriptPath
}

// TestReviewLoop_ImplementerStderrTail_SurfacedOnNoCommit_Hkloga9 is the
// load-bearing scenario test for hk-loga9. It MUST fail on parent commit
// 0173179 (stderr tail dropped) and pass after the fix (stderr tail
// propagated into the failure summary).
//
// Bead: hk-loga9.
func TestReviewLoop_ImplementerStderrTail_SurfacedOnNoCommit_Hkloga9(t *testing.T) {
	t.Parallel()

	projectDir := rlFixtureProjectDir(t)
	rlFixtureGitRepo(t, projectDir)
	wtPath, parentSHA := rlFixtureWorktree(t, projectDir)

	scriptPath := loga9HandlerScriptStderrCrash(t, wtPath)

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{scriptPath},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	result := daemon.ExportedRunReviewLoop(
		ctx, deps,
		rlFixtureRunID(t),
		core.BeadID("loga9-stderr-tail-001"),
		wtPath, parentSHA,
	)

	// Assertion 1: result MUST be a failure.
	if result.Success {
		t.Errorf("hk-loga9 FAIL: result.Success = true; want false (no commit + stderr crash). summary=%q",
			result.Summary)
	}

	// Assertion 2 (load-bearing): summary MUST carry the stderr-tail content.
	// Pre-fix this assertion fails because the summary is the static
	// "no_commit_during_implementer: HEAD did not advance past parent ..." string.
	if !strings.Contains(result.Summary, loga9StderrMarker) {
		t.Errorf("hk-loga9 FAIL: result.Summary missing implementer stderr tail.\n"+
			"  want substring: %q\n"+
			"  got summary:    %q",
			loga9StderrMarker, result.Summary)
	}

	// Assertion 3: summary should carry an exit-code marker (hk-loga9 fix
	// also surfaces the non-zero exit so we can distinguish crashes from
	// self-quits).
	if !strings.Contains(result.Summary, "exit=") {
		t.Errorf("hk-loga9 FAIL: result.Summary missing exit code marker. summary=%q",
			result.Summary)
	}
}
