package daemon_test

// codex_empty_model_hkd170r_test.go — gated harness regression for the codex
// empty-model hang (hk-d170r, hk-heh3t).
//
// # What this proves
//
// A bead labelled harness:codex with no model: label resolves to an empty model
// string (codex has no tier-3 default in defaultModelEntries). The real routed
// launch path through routedLaunchSpecBuilder → buildCodexRoutedLaunchSpec →
// CodexHarness.LaunchSpec → buildCodexLaunchSpec MUST return a descriptive error
// immediately, not block on stdin for ~30 minutes.
//
// Without the hk-heh3t fix, buildCodexLaunchSpec launched `codex exec` without
// --model, which caused codex 0.139.0 to print "Reading additional input from
// stdin..." and block forever. The daemon's never-spawned reaper cancelled the
// run after 30 minutes with "context cancelled during node implement".
//
// # Why this level matters
//
// - codexlaunchspec_test.go (TestBuildCodexLaunchSpec_EmptyModelInitialTurn) tests
//   the lowest-level function in isolation.
// - codexharness_test.go (TestCodexHarness_LaunchSpec_EmptyModelErrors) tests the
//   harness adapter layer.
// - THIS FILE tests the ROUTING layer: the full production path from a bead label
//   through resolveHarness + HarnessRegistry + buildCodexRoutedLaunchSpec. If any
//   intermediate layer strips or ignores the model before the check, these lower
//   tests pass but the routing path silently regresses.
//
// Bead refs: hk-d170r (gated regression), hk-heh3t (original fix).
// Helper prefix: hkd170rGated.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hkd170rGatedRunCtx builds a minimal ExportedClaudeRunCtx for a codex dispatch.
// model is intentionally left at its zero value ("") to reproduce the bug.
func hkd170rGatedRunCtx(t *testing.T, ws string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hkd170rGatedRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "hk-d170r-regression-bead",
		WorkspacePath: ws,
		DaemonSocket:  "/tmp/harmonik-hk-d170r-regression.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "codex",
		// Model deliberately empty — this is the bug condition.
		BaseEnv: []string{},
	}
}

// TestHkd170rGated_CodexEmptyModelFailsLoud is the gated routing-layer regression:
// a bead with harness:codex but no model: label resolves to an empty model, and the
// full routed launch path MUST return a descriptive error immediately (not hang).
func TestHkd170rGated_CodexEmptyModelFailsLoud(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	// Bead carries harness:codex but NO model: label — the exact incident config.
	bead := core.BeadRecord{
		BeadID: "hk-d170r-regression-bead",
		Title:  "codex empty-model regression bead",
		Labels: []string{"harness:codex"},
	}

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	// Production claim-time seam: resolve harness (must be codex from bead label).
	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead,
		core.AgentType(""), // queue default
		core.AgentType(""), // node default
		core.AgentTypeClaudeCode, // global default
	)
	if agentType != core.AgentTypeCodex {
		t.Fatalf("resolved agentType = %q; want codex (harness:codex label must win tier 1)", agentType)
	}

	// Production model resolution: codex has no tier-3 default → empty.
	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "" {
		t.Fatalf("codex model resolution = %q; want empty (no tier-3 default → should require explicit model: label)", sealedModel)
	}

	// Real routed launch path — this is what beadRunOne calls in production.
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), // queue default
		core.AgentType(""), // node default
		core.AgentTypeClaudeCode, // global default
		bus,
	)

	ws := t.TempDir()
	_, _, err = build(ctx, hkd170rGatedRunCtx(t, ws))
	if err == nil {
		t.Fatal("routed launch with harness:codex + empty model: want immediate error, got nil — " +
			"codex will block on stdin for ~30 minutes (hk-heh3t regression)")
	}

	// Error must mention model so operators know how to fix it.
	if !strings.Contains(err.Error(), "model") {
		t.Errorf("error does not mention 'model'; operators need a fix hint: %v", err)
	}
}

// TestHkd170rGated_CodexWithModelSucceeds is the positive counterpart: the same
// routing path with an explicit model: label resolves a non-empty model and the
// launch spec is built (no stdin-block error). Proves the fix is not over-broad.
func TestHkd170rGated_CodexWithModelSucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	// Bead carries harness:codex AND model:o4-mini — correct operator config.
	bead := core.BeadRecord{
		BeadID: "hk-d170r-regression-bead-with-model",
		Title:  "codex with-model regression bead",
		Labels: []string{"harness:codex", "model:o4-mini"},
	}

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	agentType := daemon.ExportedResolveHarnessAgentTypeQuiet(
		bead,
		core.AgentType(""),
		core.AgentType(""),
		core.AgentTypeClaudeCode,
	)
	if agentType != core.AgentTypeCodex {
		t.Fatalf("resolved agentType = %q; want codex", agentType)
	}

	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "o4-mini" {
		t.Fatalf("model resolution with model:o4-mini label = %q; want o4-mini", sealedModel)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""),
		core.AgentType(""),
		core.AgentTypeClaudeCode,
		bus,
	)

	ws := t.TempDir()
	rc := hkd170rGatedRunCtx(t, ws)
	rc.Model = sealedModel // supply the resolved model

	spec, _, err := build(ctx, rc)
	if err != nil {
		t.Fatalf("routed launch with model:o4-mini: unexpected error: %v", err)
	}

	// Spec must carry codex binary (not claude).
	if spec.Binary == "claude" {
		t.Errorf("spec.Binary = claude; want codex — routing went to wrong harness")
	}
}
