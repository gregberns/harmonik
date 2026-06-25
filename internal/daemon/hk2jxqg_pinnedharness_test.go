package daemon_test

// hk2jxqg_pinnedharness_test.go — tests for pinnedHarnessLaunchSpecBuilder
// (hk-2jxqg: DOT reviewer-harness footgun fix).
//
// Regression: routedLaunchSpecBuilder calls resolveHarness, which lets a
// tier-1 bead label (harness:codex) override the node-level pin
// (reviewer_harness=claude-code), routing the reviewer to codex → no verdict →
// run_failed.
//
// Fix: pinnedHarnessLaunchSpecBuilder bypasses resolveHarness entirely; the
// caller-supplied agentType wins unconditionally.
//
// Tests:
//  1. pinnedHarnessLaunchSpecBuilder with bead(harness:codex) + agentType=claude-code
//     emits harness_selected with tier=3 and agent_type=claude-code (not codex).
//  2. The returned LaunchSpec Binary matches buildClaudeLaunchSpec (i.e. the
//     claude harness was used, not codex).
//  3. Contrast: routedLaunchSpecBuilder on the same bead resolves to codex
//     (demonstrating the pre-fix behaviour that pinnedHarnessLaunchSpecBuilder
//     overrides).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (prefixed hk2jxqg to avoid collision with other test files)
// ─────────────────────────────────────────────────────────────────────────────

func hk2jxqgWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("hk2jxqgWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

func hk2jxqgRunCtx(t *testing.T, workspacePath string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("hk2jxqgRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:         core.RunID(runUID),
		BeadID:        "test-bead-pinned-harness-hk-2jxqg",
		WorkspacePath: workspacePath,
		DaemonSocket:  "/tmp/harmonik-test-pinned-harness.sock",
		WorkflowMode:  core.WorkflowModeSingle,
		HandlerBinary: "claude",
		BaseEnv:       []string{"HARMONIK_PROJECT_HASH=deadbeef2jxqg"},
	}
}

// hk2jxqgCaptureBus is a minimal in-process event collector for this test.
type hk2jxqgCaptureBus struct {
	inner handlercontract.EventEmitter
	mu    ctxMu
	evts  []hk2jxqgEvent
}

type hk2jxqgEvent struct {
	EventType core.EventType
	Payload   []byte
}

// ctxMu is a trivial mutex alias to avoid collision with harnessResolveFixtureBus.
type ctxMu struct{}

func newHk2jxqgBus(t *testing.T) *hk2jxqgCaptureBus {
	t.Helper()
	return &hk2jxqgCaptureBus{inner: eventbus.NewBusImpl()}
}

func (b *hk2jxqgCaptureBus) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.evts = append(b.evts, hk2jxqgEvent{EventType: et, Payload: payload})
	return nil
}

func (b *hk2jxqgCaptureBus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

var _ handlercontract.EventEmitter = (*hk2jxqgCaptureBus)(nil)

// hk2jxqgBeadWithCodexLabel builds a BeadRecord that carries harness:codex.
func hk2jxqgBeadWithCodexLabel() core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID("test-bead-pinned-harness-hk-2jxqg"),
		Title:         "fixture bead with harness:codex label",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        []string{"harness:codex"},
		AuditTrailRef: "test-bead-pinned-harness-hk-2jxqg",
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. harness_selected event carries the pinned type (tier 3, claude-code)
//    even when the bead has harness:codex.
// ─────────────────────────────────────────────────────────────────────────────

// TestPinnedHarnessLaunchSpecBuilder_HarnessSelectedEvent verifies that
// pinnedHarnessLaunchSpecBuilder emits harness_selected with tier=3 and
// agent_type=claude-code when agentType=claude-code is pinned, even though the
// bead carries a harness:codex label (hk-2jxqg footgun fix).
func TestPinnedHarnessLaunchSpecBuilder_HarnessSelectedEvent(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	bead := hk2jxqgBeadWithCodexLabel()
	bus := newHk2jxqgBus(t)

	ws := hk2jxqgWorkspace(t)
	rc := hk2jxqgRunCtx(t, ws)

	build := daemon.ExportedPinnedHarnessLaunchSpecBuilder(
		reg,
		bead,
		core.AgentTypeClaudeCode, // pinned: must win over harness:codex label
		bus,
	)

	// Call the builder. It emits harness_selected before building the spec.
	// Ignore the spec/err — we care about the event.
	_, _, _ = build(context.Background(), rc)

	// Find harness_selected event.
	var selectedPL *core.HarnessSelectedPayload
	for _, e := range bus.evts {
		if e.EventType == core.EventTypeHarnessSelected {
			var pl core.HarnessSelectedPayload
			if err := json.Unmarshal(e.Payload, &pl); err != nil {
				t.Fatalf("harness_selected payload unmarshal: %v", err)
			}
			selectedPL = &pl
			break
		}
	}

	if selectedPL == nil {
		t.Fatal("pinnedHarnessLaunchSpecBuilder: no harness_selected event emitted")
	}
	if !selectedPL.Valid() {
		t.Errorf("pinnedHarnessLaunchSpecBuilder: HarnessSelectedPayload.Valid() = false; payload = %+v", *selectedPL)
	}
	if got, want := core.AgentType(selectedPL.AgentType), core.AgentTypeClaudeCode; got != want {
		t.Errorf("pinnedHarnessLaunchSpecBuilder: harness_selected agent_type = %q; want %q (bead label harness:codex must be ignored)", got, want)
	}
	if selectedPL.Tier != 3 {
		t.Errorf("pinnedHarnessLaunchSpecBuilder: harness_selected tier = %d; want 3", selectedPL.Tier)
	}
	if selectedPL.BeadID != string(bead.BeadID) {
		t.Errorf("pinnedHarnessLaunchSpecBuilder: harness_selected bead_id = %q; want %q", selectedPL.BeadID, bead.BeadID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. LaunchSpec Binary matches buildClaudeLaunchSpec (claude harness used, not codex)
// ─────────────────────────────────────────────────────────────────────────────

// TestPinnedHarnessLaunchSpecBuilder_ClaudeBinaryUsed verifies that
// pinnedHarnessLaunchSpecBuilder with agentType=claude-code produces a LaunchSpec
// whose Binary matches a direct buildClaudeLaunchSpec call, confirming the
// claude harness was selected rather than the codex harness the bead label
// would have triggered (hk-2jxqg).
func TestPinnedHarnessLaunchSpecBuilder_ClaudeBinaryUsed(t *testing.T) {
	// Reference: direct buildClaudeLaunchSpec on a fresh workspace.
	wsRef := hk2jxqgWorkspace(t)
	rcRef := hk2jxqgRunCtx(t, wsRef)
	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcRef)
	if err != nil {
		t.Fatalf("reference ExportedBuildClaudeLaunchSpec: %v", err)
	}

	reg, regErr := daemon.ExportedNewHarnessRegistry()
	if regErr != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", regErr)
	}

	bead := hk2jxqgBeadWithCodexLabel() // harness:codex label — must be ignored
	bus := newHk2jxqgBus(t)

	wsPinned := hk2jxqgWorkspace(t)
	rcPinned := hk2jxqgRunCtx(t, wsPinned)

	build := daemon.ExportedPinnedHarnessLaunchSpecBuilder(
		reg,
		bead,
		core.AgentTypeClaudeCode,
		bus,
	)
	pinnedSpec, _, err := build(context.Background(), rcPinned)
	if err != nil {
		t.Fatalf("pinnedHarnessLaunchSpecBuilder: %v", err)
	}

	// The pinned builder must produce the claude binary, not "codex".
	if pinnedSpec.Binary != refSpec.Binary {
		t.Errorf("pinnedHarnessLaunchSpecBuilder Binary = %q; want %q (should use claude, not codex)", pinnedSpec.Binary, refSpec.Binary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Contrast: routedLaunchSpecBuilder resolves to codex when bead has harness:codex
//    (demonstrates the pre-fix behaviour that pinnedHarnessLaunchSpecBuilder overrides)
// ─────────────────────────────────────────────────────────────────────────────

// TestPinnedHarnessLaunchSpecBuilder_RoutedWouldResolveCodex confirms that the
// same bead (harness:codex) fed to routedLaunchSpecBuilder with nodeDefault=claude-code
// resolves to codex via the tier-1 bead label — the footgun that hk-2jxqg fixed.
// This test documents the pre-fix routing so the difference is explicit.
func TestPinnedHarnessLaunchSpecBuilder_RoutedWouldResolveCodex(t *testing.T) {
	bus := newHk2jxqgBus(t)
	bead := hk2jxqgBeadWithCodexLabel()

	// resolveHarness with the bead label present: tier-1 wins over node default.
	got := daemon.ExportedResolveHarness(
		context.Background(),
		bead,
		core.AgentType(""),       // no queue default
		core.AgentTypeClaudeCode, // node default = claude-code (the intended pin)
		core.AgentType(""),       // no global default
		bus,
	)

	// The tier-1 bead label (harness:codex) wins over the node default (claude-code).
	// This is the pre-fix footgun: routedLaunchSpecBuilder would route to codex.
	if got != core.AgentTypeCodex {
		t.Errorf("routedLaunchSpecBuilder precedence: resolveHarness = %q; want %q (tier-1 bead label wins over node default)", got, core.AgentTypeCodex)
	}
}
