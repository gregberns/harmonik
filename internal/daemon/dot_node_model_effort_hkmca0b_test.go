package daemon_test

// dot_node_model_effort_hkmca0b_test.go — conformance gate: per-node
// model/effort override on agentic DOT node vs. run-level default for
// sibling node (workflow-graph.md §4 WG-042, EM-012b-NODE).
//
// # What this file proves
//
// A single test run that exercises TWO agentic nodes from the same graph
// independently and asserts that:
//
//  1. A node carrying model="claude-opus-4-8" and effort="high" causes the
//     dispatcher to use those values (tier-0 override), NOT the run-level pair.
//
//  2. A sibling node carrying no model= or effort= attrs causes the dispatcher
//     to fall back to the run-level resolved pair unchanged.
//
// Both nodes share the same run-level defaults (claude-sonnet-4-6 / medium),
// making the contrast explicit: same run, divergent per-node dispatch.
//
// Observable terminal condition: captured via the claudelaunchspec test seam
// (ExportedCaptureModelEffortBuilder + ExportedDriveDotWorkflowWithModelEffort).
// The bead description permits this seam as an alternative to events.jsonl
// (see bead hk-mca0b).
//
// Bead ref: hk-mca0b. Spec: WG-042, EM-012b-NODE.

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// runLevelDefaults holds the run-level model/effort pair shared by both
// sub-tests. Using concrete model IDs (not symbolic names like "opus") so the
// assertions are unambiguous.
const (
	hkmca0bRunModel  = "claude-sonnet-4-6"
	hkmca0bRunEffort = "medium"
)

// TestDotWorkNodeAndSiblingReviewNodeModelEffortDiverge is the hk-mca0b
// conformance gate. It runs two dispatch sub-tests under a single test
// function, each exercising one node from what would be a work→review→close
// graph, and asserts the divergence contract:
//
//   - work node (model=claude-opus-4-8, effort=high) → dispatched with opus/high
//   - review node (no attrs)                         → dispatched with run-level sonnet/medium
func TestDotWorkNodeAndSiblingReviewNodeModelEffortDiverge(t *testing.T) {
	t.Parallel()

	t.Run("work_node_overrides_run_level", func(t *testing.T) {
		t.Parallel()

		const (
			wantModel = "claude-opus-4-8"
			wantEffort = "high"
		)

		projectDir := t.TempDir()
		gitInit := func(args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = projectDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		gitInit("init", "--initial-branch=main")
		gitInit("config", "user.email", "test@harmonik.local")
		gitInit("config", "user.name", "Harmonik Test")
		gitInit("commit", "--allow-empty", "-m", "Initial commit")

		wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

		// work node: carries model= and effort= attrs — the per-node override path.
		graph := &dot.Graph{
			StartNodeID:     "work",
			TerminalNodeIDs: []string{"close"},
			Nodes: []*dot.Node{
				{
					ID:               "work",
					Type:             core.NodeTypeAgentic,
					AgentType:        "implementer",
					HandlerRef:       "claude-implementer",
					IdempotencyClass: "non-idempotent",
					Model:            wantModel,
					Effort:           wantEffort,
				},
			},
			UnknownAttrs: map[string]string{},
		}

		captured := make(chan daemon.ModelEffortPair, 1)
		lsb := daemon.ExportedCaptureModelEffortBuilder(captured)

		runID := implReadyFixtureRunID(t)
		adapterReg := NewSealedAdapterRegistryForTest(t)

		deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
			BrAdapter:           &stubBeadLedger{},
			Bus:                 &stubEventCollector{},
			ProjectDir:          projectDir,
			HandlerBinary:       "/bin/sh",
			HandlerArgs:         []string{"-c", "exit 0"},
			IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
			WorkflowModeDefault: core.WorkflowModeDot,
			AdapterRegistry2:    adapterReg,
			HookStore:           daemon.ExportedNewHookSessionStore(),
			LaunchSpecBuilder:   lsb,
		})

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		result := daemon.ExportedDriveDotWorkflowWithModelEffort(
			ctx, deps, runID, "hk-mca0b-work-001",
			"work node override test", "bead body",
			wtPath, parentSHA,
			graph,
			hkmca0bRunModel, hkmca0bRunEffort,
		)

		// Stub short-circuits dispatch → NeedsAttention is expected.
		if !result.NeedsAttention {
			t.Errorf("expected NeedsAttention=true (capture stub short-circuited), got false; summary=%q", result.Summary)
		}

		var pair daemon.ModelEffortPair
		select {
		case pair = <-captured:
		default:
			t.Fatal("launchSpecBuilder was never called — model/effort not captured for work node")
		}

		if pair.Model != wantModel {
			t.Errorf("work node: model = %q; want %q (per-node override must supersede run-level %q)",
				pair.Model, wantModel, hkmca0bRunModel)
		}
		if pair.Effort != wantEffort {
			t.Errorf("work node: effort = %q; want %q (per-node override must supersede run-level %q)",
				pair.Effort, wantEffort, hkmca0bRunEffort)
		}
	})

	t.Run("review_node_inherits_run_level_default", func(t *testing.T) {
		t.Parallel()

		projectDir := t.TempDir()
		gitInit := func(args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = projectDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		gitInit("init", "--initial-branch=main")
		gitInit("config", "user.email", "test@harmonik.local")
		gitInit("config", "user.name", "Harmonik Test")
		gitInit("commit", "--allow-empty", "-m", "Initial commit")

		wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)

		// review node: carries NO model= or effort= attrs — inherits run-level pair.
		graph := &dot.Graph{
			StartNodeID:     "review",
			TerminalNodeIDs: []string{"close"},
			Nodes: []*dot.Node{
				{
					ID:               "review",
					Type:             core.NodeTypeAgentic,
					AgentType:        "reviewer",
					HandlerRef:       "claude-reviewer",
					IdempotencyClass: "non-idempotent",
					// Model and Effort deliberately absent — must inherit run-level pair.
				},
			},
			UnknownAttrs: map[string]string{},
		}

		captured := make(chan daemon.ModelEffortPair, 1)
		lsb := daemon.ExportedCaptureModelEffortBuilder(captured)

		runID := implReadyFixtureRunID(t)
		adapterReg := NewSealedAdapterRegistryForTest(t)

		deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
			BrAdapter:           &stubBeadLedger{},
			Bus:                 &stubEventCollector{},
			ProjectDir:          projectDir,
			HandlerBinary:       "/bin/sh",
			HandlerArgs:         []string{"-c", "exit 0"},
			IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
			WorkflowModeDefault: core.WorkflowModeDot,
			AdapterRegistry2:    adapterReg,
			HookStore:           daemon.ExportedNewHookSessionStore(),
			LaunchSpecBuilder:   lsb,
		})

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		daemon.ExportedDriveDotWorkflowWithModelEffort(
			ctx, deps, runID, "hk-mca0b-review-001",
			"review node default test", "bead body",
			wtPath, parentSHA,
			graph,
			hkmca0bRunModel, hkmca0bRunEffort,
		)

		var pair daemon.ModelEffortPair
		select {
		case pair = <-captured:
		default:
			t.Fatal("launchSpecBuilder was never called — model/effort not captured for review node")
		}

		// Review node carries no model= attr → MUST use run-level model, NOT opus.
		if pair.Model != hkmca0bRunModel {
			t.Errorf("review node: model = %q; want run-level default %q (no per-node override)",
				pair.Model, hkmca0bRunModel)
		}
		if pair.Effort != hkmca0bRunEffort {
			t.Errorf("review node: effort = %q; want run-level default %q (no per-node override)",
				pair.Effort, hkmca0bRunEffort)
		}

		// Cross-check: the review node must NOT have received the work node's opus/high.
		if pair.Model == "claude-opus-4-8" {
			t.Errorf("review node: model = %q (opus) but review node carries no model= attr; "+
				"run-level default must not be shadowed by a different node's override", pair.Model)
		}
	})
}
