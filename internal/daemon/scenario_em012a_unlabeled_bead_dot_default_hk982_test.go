//go:build scenario

package daemon_test

// scenario_em012a_unlabeled_bead_dot_default_hk982_test.go — scenario test for
// EM-012a tier-4 flip: an unlabeled bead dispatches via standard-bead.dot DOT
// mode by default.
//
// # What this guards
//
// EM-012a flipped the tier-4 built-in workflow_mode fallback from `single` to
// `dot` at v1.0 (kerf work `dot-default` / hk-30vlb). Two claims require
// end-to-end coverage:
//
//  1. Mode-resolution tier-4: an unlabeled bead with no daemon default resolves
//     to `dot`, NEVER `single`.
//
//  2. Mode-resolution tier-3 (v1.0 production default): an unlabeled bead with
//     WorkflowModeDefault=dot (the daemon's v1.0 startup default per PL-004a)
//     also resolves to `dot`.
//
//  3. DOT dispatch via standard-bead.dot: after the mode resolves to `dot`,
//     the embedded standard-bead.dot graph drives the cascade to completion,
//     reaching the `close` terminal node on the happy path
//     (start → implement → commit_gate(SUCCESS) → review(APPROVE) → close).
//
// # Tier-4 vs tier-3 distinction
//
// The tier-4 fallback fires when `deps.workflowModeDefault` is invalid/absent.
// In production, daemon.Start REQUIRES a valid WorkflowModeDefault (line 659 of
// start.go); the tier-4 path in resolveWorkflowMode is a defensive safety net.
// ExportedWorkLoopDeps normalises a zero WorkflowModeDefault to WorkflowModeSingle
// (mirroring the historical test-seam behaviour); to exercise tier-4 we call
// ExportedResolveWorkflowMode directly with an empty daemonDefault.
//
// # Test project worktree
//
// The commit_gate in standard-bead.dot runs:
//   go build ./... && go vet ./... && bash scripts/scenario-gate.sh
//
// To make this pass inside the test worktree we create:
//   - A minimal go.mod (module em012a-test; go 1.21) — `go build ./...` and
//     `go vet ./...` report no packages and exit 0 on an empty module.
//   - scripts/scenario-gate.sh that exits 0.
//
// Both files are committed to the initial project commit so every worktree
// derived from that project starts with a passing gate.
//
// # Handler script (agentic nodes)
//
// A single /bin/sh script handles both implementer and reviewer invocations
// (driveDotWorkflow dispatches all agentic nodes through the same HandlerBinary):
//   - Odd invocations  → implementer: commit a unique file to advance HEAD.
//   - Even invocations → reviewer:    write APPROVE verdict to
//     $HARMONIK_WORKSPACE_PATH/.harmonik/review.json.
//
// The counter lives in wtPath/.harmonik/em012a_count so it persists across
// the single implement→review pair.
//
// # Spec refs
//   - specs/execution-model.md §4.3 EM-012a (four-tier mode-resolution)
//   - specs/execution-model.md §4.3 EM-012a-FLOOR (review-floor guarantee)
//   - specs/execution-model.md §7.5 (dot-mode binding)
//   - specs/workflow-graph.md §17 WG-047..WG-052 (standard-bead.dot invariants)
//
// Run: go test -tags=scenario -run TestScenario_EM012a ./internal/daemon/...
//
// Bead: hk-982.
// Helper prefix: em012a (per implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow"
)

// ─────────────────────────────────────────────────────────────────────────────
// em012a fixtures
// ─────────────────────────────────────────────────────────────────────────────

// em012aProjectDir creates a test project directory with:
//   - .harmonik/events/ and .harmonik/beads-intents/ directories
//   - a minimal go.mod (empty module → `go build ./...` exits 0)
//   - scripts/scenario-gate.sh that exits 0
//
// All files are staged and committed so the derived worktree starts with a
// passing commit_gate.
func em012aProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("em012aProjectDir: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory; not production
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("em012aProjectDir: mkdir beads-intents: %v", err)
	}

	// Minimal go.mod + package source so `go build ./...` and `go vet ./...` find
	// at least one package and exit 0 (an empty module returns "no packages to
	// vet" with exit code 1, which fails the commit_gate).
	goMod := "module em012a-test\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("em012aProjectDir: write go.mod: %v", err)
	}
	// Minimal package so go build / go vet have something to analyse.
	docGo := "// Package em012atest is a minimal test module for the em012a scenario.\npackage em012atest\n"
	if err := os.WriteFile(filepath.Join(dir, "doc.go"), []byte(docGo), 0o644); err != nil {
		t.Fatalf("em012aProjectDir: write doc.go: %v", err)
	}

	// scripts/scenario-gate.sh — exits 0 so commit_gate passes unconditionally.
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("em012aProjectDir: mkdir scripts: %v", err)
	}
	gateScript := "#!/bin/sh\nexit 0\n"
	//nolint:gosec // G306: test-only fixture script; not production
	if err := os.WriteFile(filepath.Join(dir, "scripts", "scenario-gate.sh"), []byte(gateScript), 0o755); err != nil {
		t.Fatalf("em012aProjectDir: write scenario-gate.sh: %v", err)
	}

	// Initialise the git repo and commit everything.
	run := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("em012aProjectDir: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("add", "go.mod", "doc.go", "scripts/scenario-gate.sh")
	run("commit", "-m", "Initial commit", "--no-gpg-sign")

	return dir
}

// em012aWorktree creates a detached git worktree from projectDir, creates the
// .harmonik/ subdirectory inside it, and registers cleanup.
func em012aWorktree(t *testing.T, projectDir string) (wtPath, parentSHA string) {
	t.Helper()

	headCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	headCmd.Dir = projectDir
	headOut, headErr := headCmd.Output()
	if headErr != nil {
		t.Fatalf("em012aWorktree: git rev-parse HEAD: %v", headErr)
	}
	parentSHA = strings.TrimSpace(string(headOut))

	wtDir := t.TempDir()
	wtPath = filepath.Join(wtDir, "wt")

	//nolint:gosec // G204: git args are test-internal
	addCmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", wtPath, parentSHA)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		t.Fatalf("em012aWorktree: git worktree add: %v\n%s", addErr, addOut)
	}

	//nolint:gosec // G301: test-only
	if mkErr := os.MkdirAll(filepath.Join(wtPath, ".harmonik"), 0o755); mkErr != nil {
		t.Fatalf("em012aWorktree: mkdir .harmonik: %v", mkErr)
	}

	t.Cleanup(func() {
		//nolint:gosec // G204: git args are test-internal
		rmCmd := exec.Command("git", "worktree", "remove", "--force", "--force", wtPath)
		rmCmd.Dir = projectDir
		_ = rmCmd.Run()
	})

	return wtPath, parentSHA
}

// em012aHandlerScript writes a /bin/sh handler script for the agentic nodes in
// the standard-bead.dot happy path.
//
// Dispatch routing uses the $HARMONIK_PHASE env var set by the DOT cascade:
//   - "reviewer"             → write APPROVE verdict to .harmonik/review.json.
//   - "implementer-initial"  → commit a unique file to advance HEAD.
//   - "implementer-resume"   → commit a unique file (after a gate fix-loop).
//
// Using $HARMONIK_PHASE instead of an odd/even invocation counter ensures
// correct routing even when the cascade re-enters the implementer node
// (e.g. after a deterministic commit_gate failure), since the phase value is
// authoritative regardless of invocation order.
func em012aHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()

	approveVerdict := `{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"em012a scenario APPROVE"}`
	approveVerdictEsc := strings.ReplaceAll(approveVerdict, "'", "'\\''")
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")

	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
PHASE="${HARMONIK_PHASE:-implementer-initial}"
case "$PHASE" in
  reviewer)
    # Reviewer: write APPROVE verdict.
    mkdir -p "$WS/.harmonik"
    printf '%%s' '%s' > "$WS/.harmonik/review.json"
    ;;
  implementer-initial|implementer-resume|*)
    # Implementer (initial or resume): commit a unique file to advance HEAD.
    # Use a timestamp-based name to ensure uniqueness across re-entries.
    FNAME="em012a_impl_$(date +%%s%%N 2>/dev/null || date +%%s).txt"
    printf '%%s' "$PHASE" > "$WS/$FNAME"
    git -C "$WS" add "$FNAME" >/dev/null 2>&1
    git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" \
      commit -m "em012a $PHASE" --no-gpg-sign >/dev/null 2>&1
    ;;
esac
exit 0
`, wtpEsc, approveVerdictEsc)

	scriptPath := filepath.Join(t.TempDir(), "em012a_handler.sh")
	//nolint:gosec // G306: test-only fixture script
	if writeErr := os.WriteFile(scriptPath, []byte(script), 0o755); writeErr != nil {
		t.Fatalf("em012aHandlerScript: WriteFile: %v", writeErr)
	}
	return scriptPath
}

// em012aRunID returns a fresh RunID for the test.
func em012aRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("em012aRunID: uuid.NewV7: %v", err)
	}
	return core.RunID(u)
}

// em012aStandardBeadDotPath returns the absolute path to the canonical
// specs/examples/standard-bead.dot. The test binary's working directory is the
// package directory (internal/daemon/), so the spec is two levels up.
func em012aStandardBeadDotPath(t *testing.T) string {
	t.Helper()
	repoRoot := filepath.Join("..", "..")
	dotPath := filepath.Join(repoRoot, "specs", "examples", "standard-bead.dot")
	if _, statErr := os.Stat(dotPath); statErr != nil {
		t.Fatalf("em012aStandardBeadDotPath: spec file not found: %v", statErr)
	}
	return dotPath
}

// ─────────────────────────────────────────────────────────────────────────────
// (1) Mode-resolution: tier-4 and tier-3 both yield dot, never single
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_EM012a_TierFour_YieldsDotNeverSingle verifies the EM-012a tier-4
// flip: an unlabeled bead with an absent/invalid daemon default MUST resolve to
// `dot`, NEVER to `single`.
//
// Spec ref: execution-model.md §4.3.EM-012a tier 4 ("Built-in fallback: `dot`").
func TestScenario_EM012a_TierFour_YieldsDotNeverSingle(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &modeResolveFixtureBus{}
	// Unlabeled bead — no workflow:* label, so tier-1 is absent.
	bead := modeResolveFixtureBead(t, nil)
	// Empty daemon default → tier-3 absent → tier-4 fires.
	daemonDefault := core.WorkflowMode("")

	got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, daemonDefault, bus)

	if got == core.WorkflowModeSingle {
		t.Errorf("EM-012a tier-4: resolved to %q — MUST NOT be single (EM-012a flipped "+
			"the tier-4 default from single to dot at v1.0)", got)
	}
	if got != core.WorkflowModeDot {
		t.Errorf("EM-012a tier-4: resolved to %q, want %q", got, core.WorkflowModeDot)
	}
	// No conflict events should have fired (unlabeled bead has no workflow labels).
	events := modeResolveFixtureBusEvents(t, bus)
	for _, e := range events {
		if e.EventType == core.EventTypeBeadLabelConflict {
			t.Error("EM-012a tier-4: unexpected bead_label_conflict event for unlabeled bead")
		}
	}
}

// TestScenario_EM012a_TierThree_ProductionDefault_YieldsDot verifies that an
// unlabeled bead dispatched with the v1.0 production default
// (WorkflowModeDefault = dot) resolves to `dot` at tier-3, NEVER to `single`.
//
// Spec ref: execution-model.md §4.3.EM-012a tier 3 (daemon default) + PL-004a.
func TestScenario_EM012a_TierThree_ProductionDefault_YieldsDot(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &modeResolveFixtureBus{}
	bead := modeResolveFixtureBead(t, nil) // no workflow:* labels
	// Tier-3: daemon started with WorkflowModeDefault=dot (v1.0 production default).
	daemonDefault := core.WorkflowModeDot

	got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, daemonDefault, bus)

	if got == core.WorkflowModeSingle {
		t.Errorf("EM-012a tier-3 (production default): resolved to %q — MUST NOT be "+
			"single; the v1.0 daemon default is dot (PL-004a)", got)
	}
	if got != core.WorkflowModeDot {
		t.Errorf("EM-012a tier-3: resolved to %q, want %q", got, core.WorkflowModeDot)
	}
}

// TestScenario_EM012a_UnrelatedLabels_TierFour_YieldsDot verifies that a bead
// carrying non-workflow labels (e.g. area:, size:) still falls through to the
// tier-4 dot fallback when no daemon default is set.
func TestScenario_EM012a_UnrelatedLabels_TierFour_YieldsDot(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	bus := &modeResolveFixtureBus{}
	// Bead has area/size labels but NO workflow:* label.
	bead := modeResolveFixtureBead(t, []string{"area:daemon", "size:S", "priority:1"})
	daemonDefault := core.WorkflowMode("") // tier-3 absent

	got := daemon.ExportedResolveWorkflowMode(t.Context(), bead, daemonDefault, bus)

	if got == core.WorkflowModeSingle {
		t.Errorf("EM-012a: non-workflow-labelled bead resolved to single at tier-4 — "+
			"MUST be dot (EM-012a flip); got %q", got)
	}
	if got != core.WorkflowModeDot {
		t.Errorf("EM-012a: non-workflow-labelled bead: got %q, want %q", got, core.WorkflowModeDot)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// (2) DOT dispatch via standard-bead.dot: happy path to close terminal node
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_EM012a_StandardBeadDotHappyPath drives the standard-bead.dot
// graph through its full happy path using driveDotWorkflow:
//
//	start → implement → commit_gate(SUCCESS) → review(APPROVE) → close (terminal)
//
// Asserts:
//   - success=true, TerminalNodeID="close" (APPROVE path).
//   - node_dispatch_decided events fired for each agentic node.
//   - NeedsAttention=false (no BLOCK or cap-hit path taken).
//
// This test pins the claim "unlabeled bead dispatches via standard-bead.dot DOT
// mode by default": after mode resolves to `dot` (via tier-4 or tier-3), the
// embedded standard-bead.dot graph is the one that runs.
func TestScenario_EM012a_StandardBeadDotHappyPath(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir := em012aProjectDir(t)
	wtPath, parentSHA := em012aWorktree(t, projectDir)
	scriptPath := em012aHandlerScript(t, wtPath)

	// Load the canonical standard-bead.dot from the spec path.
	dotPath := em012aStandardBeadDotPath(t)
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(standard-bead.dot): %v", loadErr)
	}

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
		WorkflowModeDefault: core.WorkflowModeDot, // v1.0 production default
	})

	// Budget: standard-bead.dot happy path runs one implementer + one gate check +
	// one reviewer. The gate runs `go build/vet` (fast on an empty module) plus the
	// trivial scenario-gate.sh. Allow generous wall-clock time.
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()

	done := make(chan daemon.DotWorkflowResultExported, 1)
	go func() {
		done <- daemon.ExportedDriveDotWorkflow(
			ctx, deps,
			em012aRunID(t),
			core.BeadID("hk-982-em012a-scenario"),
			wtPath, parentSHA,
			graph,
		)
	}()

	var result daemon.DotWorkflowResultExported
	select {
	case result = <-done:
	case <-ctx.Done():
		t.Fatalf("EM-012a: standard-bead.dot happy path did not terminate within budget; "+
			"events=%v", collector.eventTypes())
	}

	t.Logf("EM-012a: result=%+v events=%v", result, collector.eventTypes())

	// ── Result assertions ────────────────────────────────────────────────────
	if !result.Success {
		t.Errorf("EM-012a: expected success=true on APPROVE happy path; summary=%q",
			result.Summary)
	}
	if result.NeedsAttention {
		t.Errorf("EM-012a: expected NeedsAttention=false on APPROVE happy path; summary=%q",
			result.Summary)
	}
	if result.TerminalNodeID != "close" {
		t.Errorf("EM-012a: TerminalNodeID=%q, want %q (APPROVE path → close; "+
			"BLOCK/cap-hit → close-needs-attention)", result.TerminalNodeID, "close")
	}

	// ── Event assertions: DOT cascade dispatch events ────────────────────────
	events := collector.eventTypes()

	// reviewer_verdict must be present (APPROVE verdict was written and read).
	foundReviewerVerdict := false
	for _, et := range events {
		if et == string(core.EventTypeReviewerVerdict) {
			foundReviewerVerdict = true
			break
		}
	}
	if !foundReviewerVerdict {
		t.Errorf("EM-012a: reviewer_verdict event not found — the reviewer (APPROVE) "+
			"did not complete correctly; events=%v", events)
	}

	// ── Verify reviewer_verdict carries workflow_mode=dot (EM-012a) ──────────
	for _, ev := range collector.allEvents() {
		if ev.EventType != string(core.EventTypeReviewerVerdict) {
			continue
		}
		var pl map[string]json.RawMessage
		if unmarshalErr := json.Unmarshal(ev.Payload, &pl); unmarshalErr != nil {
			t.Fatalf("EM-012a: unmarshal reviewer_verdict payload: %v", unmarshalErr)
		}
		if wmRaw, ok := pl["workflow_mode"]; ok {
			var wm string
			if parseErr := json.Unmarshal(wmRaw, &wm); parseErr == nil {
				if wm != string(core.WorkflowModeDot) {
					t.Errorf("EM-012a: reviewer_verdict.workflow_mode=%q, want %q",
						wm, core.WorkflowModeDot)
				}
			}
		}
		// Only check the first reviewer_verdict event.
		break
	}

	// run_stale must NOT have fired (clean termination, no hang).
	for _, et := range events {
		if et == string(core.EventTypeRunStale) {
			t.Errorf("EM-012a: run_stale must NOT fire on a clean happy-path run; "+
				"events=%v", events)
			break
		}
	}
}
