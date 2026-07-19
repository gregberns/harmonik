package daemon_test

// codex_empty_model_hkd170r_test.go — gated routing regression for codex
// empty-model resolution (hk-d170r; hk-heh3t retired).
//
// # What this proves
//
// A bead labelled harness:codex with no model: label resolves to an empty model
// string (codex has no tier-3 default in defaultModelEntries). The real routed
// launch path through routedLaunchSpecBuilder → buildCodexRoutedLaunchSpec →
// CodexHarness.LaunchSpec → buildCodexLaunchSpec MUST launch codex WITHOUT a
// --model flag, so codex resolves its model from $CODEX_HOME/config.toml — the
// account default.
//
// # Why the old fail-loud assertion was inverted (hk-heh3t retired)
//
// hk-heh3t once made this path fail loud: on codex 0.139.0 an omitted --model hung
// on stdin for ~30 min ("Reading additional input from stdin..."), reaped only by
// the never-spawned timeout. Two facts retired that guard:
//   1. specs/harness-contract.md HN-022 mandates codex run the ChatGPT-subscription
//      path by default. On that path EVERY explicitly-named model is rejected with
//      HTTP 400 ("not supported when using Codex with a ChatGPT account", verified
//      2026-07-16) — so a required --model makes codex structurally un-runnable.
//   2. The omitted---model hang no longer reproduces on the pinned codex (0.142.5
//      verified: a --model-less `codex exec` completes in seconds and edits the
//      tree). The never-spawned reaper remains the backstop for any future regress.
// So the correct routed behavior for an unpinned codex bead is: launch with no
// --model (account default), not fail loud.
//
// # Why this level matters
//
// - codexlaunchspec_test.go (TestBuildCodexLaunchSpec_EmptyModelInitialTurn) tests
//   the lowest-level function in isolation.
// - codexharness_test.go (TestCodexHarness_LaunchSpec_EmptyModelAccountDefault) tests
//   the harness adapter layer.
// - THIS FILE tests the ROUTING layer: the full production path from a bead label
//   through resolveHarness + HarnessRegistry + buildCodexRoutedLaunchSpec. If any
//   intermediate layer injects or drops the model, these lower tests pass but the
//   routing path silently regresses.
//
// Bead refs: hk-d170r (gated regression), hk-heh3t (retired guard).
// Helper prefix: hkd170rGated.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hkd170rRecordingEmitter records every emitted (eventType, payload) pair. It is
// the recording handlercontract.EventEmitter used by
// TestHkd170rGated_CodexEmptyModel_ModelSelectedTieThrough (GAP-1) to prove the
// model_selected event and the built argv agree on the empty codex model.
type hkd170rRecordingEmitter struct {
	mu     sync.Mutex
	events []hkd170rRecordedEvent
}

type hkd170rRecordedEvent struct {
	typ     core.EventType
	payload []byte
}

func (e *hkd170rRecordingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.record(eventType, payload)
	return nil
}

func (e *hkd170rRecordingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	e.record(eventType, payload)
	return nil
}

func (e *hkd170rRecordingEmitter) record(eventType core.EventType, payload []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	e.events = append(e.events, hkd170rRecordedEvent{typ: eventType, payload: cp})
}

// modelSelectedPayloads returns the decoded payloads of every model_selected
// event recorded, in order.
func (e *hkd170rRecordingEmitter) modelSelectedPayloads(t *testing.T) []core.ModelSelectedPayload {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]core.ModelSelectedPayload, 0, len(e.events))
	for _, ev := range e.events {
		if ev.typ != core.EventTypeModelSelected {
			continue
		}
		var pl core.ModelSelectedPayload
		if err := json.Unmarshal(ev.payload, &pl); err != nil {
			t.Fatalf("hkd170r: decode model_selected payload: %v\nraw: %s", err, ev.payload)
		}
		out = append(out, pl)
	}
	return out
}

// compile-time assertion that the recorder satisfies the emitter interface.
var _ handlercontract.EventEmitter = (*hkd170rRecordingEmitter)(nil)

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

// TestHkd170rGated_CodexEmptyModelAccountDefault is the gated routing-layer
// regression: a bead with harness:codex but no model: label resolves to an empty
// model, and the full routed launch path MUST build a spec that launches codex
// WITHOUT a --model flag (account default), not fail loud (hk-heh3t retired).
func TestHkd170rGated_CodexEmptyModelAccountDefault(t *testing.T) {
	// NOT parallel: the routed path now proceeds into the codex billing guard
	// (the old empty-model guard used to return first), which materializes+reads
	// $CODEX_HOME/config.toml. Point HOME at a throwaway dir so resolveCodexHome
	// ("") lands in <tmp>/.codex — hermetic, and no write/read race against the
	// operator's real ~/.codex shared with other codex tests. t.Setenv forbids
	// t.Parallel().
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	bus := eventbus.NewBusImpl()

	// Bead carries harness:codex but NO model: label — the unpinned-codex config.
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
		core.AgentType(""),       // queue default
		core.AgentType(""),       // node default
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
		t.Fatalf("codex model resolution = %q; want empty (no tier-3 default → unpinned → account default)", sealedModel)
	}

	// Real routed launch path — this is what beadRunOne calls in production.
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""),       // queue default
		core.AgentType(""),       // node default
		core.AgentTypeClaudeCode, // global default
		bus,
	)

	ws := t.TempDir()
	spec, _, err := build(ctx, hkd170rGatedRunCtx(t, ws))
	if err != nil {
		t.Fatalf("routed launch with harness:codex + empty model: want account-default launch, got error: %v", err)
	}

	// The initial-turn argv MUST omit --model so codex uses its config-default.
	for _, arg := range spec.Args {
		if arg == "--model" {
			t.Errorf("routed empty-model argv must omit --model (account default); got %v", spec.Args)
			return
		}
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

	// GAP-3: the routing layer must carry the resolved model VALUE all the way
	// into argv — not merely reach the codex harness. Mirrors the empty-model
	// routing assertion (which proves --model is ABSENT); this proves the
	// non-empty model is PRESENT with its value at the routed layer.
	codexLaunchSpecAssertArgContainsValue(t, spec.Args, "--model", "o4-mini")
}

// TestHkd170rGated_CodexEmptyModel_ModelSelectedTieThrough is the GAP-1 tie-through:
// for the SAME empty-model harness:codex bead the gated regression uses, the
// model_selected event the routing layer publishes and the argv the routing layer
// builds must AGREE that the codex model is empty. A recording emitter captures the
// event; the same routed build(ctx, rc) call yields the argv. Without this, the
// event could advertise model="" while argv silently carried a --model (or vice
// versa) and each single-layer test would still pass.
func TestHkd170rGated_CodexEmptyModel_ModelSelectedTieThrough(t *testing.T) {
	// NOT parallel: the routed path proceeds into the codex billing guard, which
	// materializes+reads $CODEX_HOME/config.toml. Point HOME at a throwaway dir so
	// resolveCodexHome("") lands in <tmp>/.codex — hermetic. t.Setenv forbids
	// t.Parallel().
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	rec := &hkd170rRecordingEmitter{}

	// Same bead shape as the gated regression: harness:codex, NO model: label.
	bead := core.BeadRecord{
		BeadID: "hk-d170r-regression-bead",
		Title:  "codex empty-model regression bead",
		Labels: []string{"harness:codex"},
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

	// Production model resolution through the SAME recording emitter (empty).
	sealedModel, _ := daemon.ExportedResolveModelPreference(
		ctx, bead.Labels, agentType, daemon.ProjectConfig{}, rec, string(bead.BeadID),
	)
	if sealedModel != "" {
		t.Fatalf("codex model resolution = %q; want empty", sealedModel)
	}

	// Routed launch path through the SAME recording emitter — this is what
	// publishes model_selected.
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""),
		core.AgentType(""),
		core.AgentTypeClaudeCode,
		rec,
	)

	ws := t.TempDir()
	rc := hkd170rGatedRunCtx(t, ws)
	// Feed the RESOLVED model through instead of relying on the fixture's zero
	// value: this makes the resolution→rc.Model→argv seam actually exercised
	// (sealedModel is "" here), rather than asserted and then discarded.
	rc.Model = sealedModel

	spec, _, err := build(ctx, rc)
	if err != nil {
		t.Fatalf("routed empty-model launch: unexpected error: %v", err)
	}

	// (1) Exactly one model_selected event, harness=codex, model empty.
	sels := rec.modelSelectedPayloads(t)
	if len(sels) != 1 {
		t.Fatalf("model_selected event count = %d; want exactly 1 (payloads: %+v)", len(sels), sels)
	}
	if sels[0].Harness != "codex" {
		t.Errorf("model_selected.Harness = %q; want %q", sels[0].Harness, "codex")
	}
	if sels[0].Model != "" {
		t.Errorf("model_selected.Model = %q; want empty (codex not harmonik-controlled)", sels[0].Model)
	}

	// NOTE: no separate "leaked concrete model" loop here — the Model != "" check
	// above already fails on ANY non-empty value (sonnet / claude-opus-4-8 /
	// o4-mini / ornith included), so an enumerated list would be unreachable and
	// would read as coverage that does not exist.

	// (2) The SAME routed build produced argv with no --model — the event and
	// the argv agree the codex model is empty.
	for _, arg := range spec.Args {
		if arg == "--model" {
			t.Errorf("routed empty-model argv must omit --model; got %v", spec.Args)
			return
		}
	}
}
