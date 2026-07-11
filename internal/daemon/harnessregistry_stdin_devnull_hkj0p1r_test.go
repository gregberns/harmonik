package daemon_test

// harnessregistry_stdin_devnull_hkj0p1r_test.go — StdinDevNull routed-path
// coverage (hk-j0p1r).
//
// The 2071963c fix taught handler.Launch to close stdin at startup when
// LaunchSpec.StdinDevNull is true, so argv-driven ProcessExit harnesses (pi,
// codex) see EOF on fd0 instead of blocking forever. That fix was INERT for the
// routed path: buildPiLaunchSpec / buildCodexLaunchSpec set StdinDevNull:true on
// their handler.LaunchSpec, but the value was DROPPED crossing the
// handler.LaunchSpec → handlercontract.SpawnSpec → handler.LaunchSpec conversion
// chain (SpawnSpec had no StdinDevNull field, and buildCodexRoutedLaunchSpec
// rebuilt the LaunchSpec without copying it). It therefore arrived false at
// handler.Launch and the /dev/null close never fired.
//
// These tests exercise the ROUTED path end-to-end (routedLaunchSpecBuilder →
// resolveHarness → CodexHarness/PiHarness.LaunchSpec → buildCodexRoutedLaunchSpec)
// and assert StdinDevNull survives as true on the assembled handler.LaunchSpec —
// the gap the 2071963c handler-only test cannot catch.

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// TestRoutedLaunchSpec_Codex_StdinDevNullThreaded verifies that a codex-routed
// launch (harness:codex + explicit model) yields a handler.LaunchSpec with
// StdinDevNull == true. Without the SpawnSpec.StdinDevNull field +
// buildCodexRoutedLaunchSpec copy, this arrives false and codex blocks ~30min on
// pane PTY stdin.
func TestRoutedLaunchSpec_Codex_StdinDevNullThreaded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	// harness:codex routes to CodexHarness; model:o4-mini clears the empty-model
	// stdin-block guard (hk-d170r) so the spec actually builds.
	bead := core.BeadRecord{
		BeadID: "hk-j0p1r-codex-stdin-devnull",
		Title:  "codex stdin-devnull routed regression bead",
		Labels: []string{"harness:codex", "model:o4-mini"},
	}

	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, core.AgentTypeCodex, daemon.ProjectConfig{}, bus, string(bead.BeadID),
	)
	if sealedModel != "o4-mini" {
		t.Fatalf("model resolution with model:o4-mini label = %q; want o4-mini", sealedModel)
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""),       // queue default
		core.AgentType(""),       // node default
		core.AgentTypeClaudeCode, // global default
		bus,
	)

	rc := hkd170rGatedRunCtx(t, t.TempDir())
	rc.Model = sealedModel

	spec, _, err := build(ctx, rc)
	if err != nil {
		t.Fatalf("routed codex launch: unexpected error: %v", err)
	}
	if spec.Binary == "claude" {
		t.Fatalf("spec.Binary = claude; routing went to the wrong harness")
	}
	if !spec.StdinDevNull {
		t.Error("routed codex LaunchSpec.StdinDevNull = false; want true — " +
			"the /dev/null stdin flag was dropped in the SpawnSpec conversion chain (hk-j0p1r); " +
			"codex will block ~30min on pane PTY stdin (hk-rpr6)")
	}
}

// TestRoutedLaunchSpec_Pi_StdinDevNullThreaded is the pi-symmetric assertion:
// a pi-routed launch yields a handler.LaunchSpec with StdinDevNull == true.
func TestRoutedLaunchSpec_Pi_StdinDevNullThreaded(t *testing.T) {
	// No t.Parallel: t.Setenv (below) is incompatible with parallel tests.

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	// A fully-configured pi harness so buildPiLaunchSpec does not fail on empty
	// provider/model/apiKeyEnv. The API key env must be present for the PI-040
	// billing guard (fail-closed) to let the launch spec build.
	t.Setenv("OPENROUTER_API_KEY", "sk-test-hk-j0p1r")
	piCfg := daemon.PiHarnessConfig{
		Provider:  "openrouter",
		Model:     "openrouter/qwen/qwen3-coder",
		APIKeyEnv: "OPENROUTER_API_KEY",
	}
	reg, err := daemon.ExportedNewHarnessRegistryWithPi(piCfg)
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistryWithPi: %v", err)
	}

	bead := core.BeadRecord{
		BeadID: "hk-j0p1r-pi-stdin-devnull",
		Title:  "pi stdin-devnull routed regression bead",
		Labels: []string{"harness:pi"},
	}

	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""),       // queue default
		core.AgentType(""),       // node default
		core.AgentTypeClaudeCode, // global default
		bus,
	)

	rc := hkd170rGatedRunCtx(t, t.TempDir())
	rc.BeadID = string(bead.BeadID)
	rc.HandlerBinary = "pi"

	spec, _, err := build(ctx, rc)
	if err != nil {
		t.Fatalf("routed pi launch: unexpected error: %v", err)
	}
	if spec.Binary == "claude" {
		t.Fatalf("spec.Binary = claude; routing went to the wrong harness")
	}
	if !spec.StdinDevNull {
		t.Error("routed pi LaunchSpec.StdinDevNull = false; want true — " +
			"the /dev/null stdin flag was dropped in the SpawnSpec conversion chain (hk-j0p1r); " +
			"pi will hang on pane PTY stdin (PI-020)")
	}
}
