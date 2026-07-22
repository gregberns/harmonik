package daemon_test

// dot_gate_reviewer_harness_hk01vs0_test.go — the DOT cognition GATE never
// inherits a SessionIDCaptured harness (hk-01vs0).
//
// # The defect
//
// executeCognitionGate (dot_gate.go) built its launch spec from
// deps.launchSpecBuilder UNCONDITIONALLY while passing
// handlercontract.ReviewLoopPhaseReviewer. deps.launchSpecBuilder is
// routedLaunchSpecBuilder(reg, beadRecord, "", "", defaultHarness, bus) — the
// implementer's builder — so the gate silently inherited whatever harness the
// bead resolved to. Under codex that meant:
//
//   - codexlaunchspec.go emits ONLY an implementer seed prompt and never reads
//     rc.phase, so the gate evaluator was told to implement the bead and never
//     learned that .harmonik/gate-task.md (written moments earlier) exists, let
//     alone that it must write .harmonik/gate-verdict.json;
//   - codex never emits agent_ready, but executeCognitionGate blocks on
//     waitAgentReady, so every cognition gate died with
//     "cognition gate %q: agent_ready_timeout".
//
// This is the THIRD site of the "a reviewer silently inherits a harness that
// cannot review" class. hk-pkxju closed reviewloop.go and dot_cascade.go; the
// gate was explicitly out of scope there.
//
// # Trigger
//
// A TIER-1 per-bead `harness:codex` LABEL is enough — resolveHarness
// (harnessresolve.go) returns a single valid harness:<type> label immediately at
// tier 1, ahead of every other tier. The global codex default is only the
// tier-4 spelling of the same defect. Both are covered below; the LABEL case is
// the one that matters for the harness ramp.
//
// # What this file proves — on the PRODUCTION call path
//
// Every case drives executeCognitionGate itself (via
// daemon.ExportedExecuteCognitionGate), never reviewerDefaultHarness in
// isolation: that helper is hk-pkxju's and is already covered by
// reviewer_never_inherits_captured_hkpkxju_test.go, so a test that only
// exercised it would stay green with the dot_gate.go wiring deleted.
//
//   - tier-1 `harness:codex` bead label ⇒ the gate resolves claude-code, and the
//     inherited (implementer) builder is NOT consulted.
//   - tier-1 `harness:pi` bead label ⇒ same (pi is the other SessionIDCaptured
//     harness).
//   - tier-4 global codex default, no label ⇒ same.
//   - NEGATIVE / no-op: an all-claude run must be byte-identical to pre-hk-01vs0
//     behaviour — the gate keeps deps.launchSpecBuilder, which IS consulted, and
//     the selected harness is still claude-code. A regression that pinned the
//     gate unconditionally would fail here.
//   - The production wiring is pinned by source text so that deleting the
//     dot_gate.go call while keeping the helper cannot leave this file green.
//
// Helper prefix: vs0 (per implementer-protocol.md helper-prefix discipline).
//
// Tags: mechanism
//
// Bead ref: hk-01vs0. Builds on hk-pkxju, hk-iv748 [C5/T14] and hk-2jxqg.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (prefix vs0)
// ─────────────────────────────────────────────────────────────────────────────

// vs0Bus is a concurrency-safe capture bus. executeCognitionGate emits from the
// dispatching goroutine and from the per-run event tap, so the slice needs a
// lock even though the assertions run after the call returns.
type vs0Bus struct {
	mu   sync.Mutex
	evts []vs0Event
}

type vs0Event struct {
	EventType core.EventType
	Payload   []byte
}

func (b *vs0Bus) Emit(_ context.Context, et core.EventType, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evts = append(b.evts, vs0Event{EventType: et, Payload: payload})
	return nil
}

func (b *vs0Bus) EmitWithRunID(ctx context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(ctx, et, payload)
}

func (b *vs0Bus) snapshot() []vs0Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]vs0Event, len(b.evts))
	copy(out, b.evts)
	return out
}

var _ handlercontract.EventEmitter = (*vs0Bus)(nil)

// vs0HarnessSelected returns every harness_selected payload on the bus, in
// emission order. The gate emits exactly one per dispatch.
func vs0HarnessSelected(t *testing.T, bus *vs0Bus) []core.HarnessSelectedPayload {
	t.Helper()
	evts := bus.snapshot()
	out := make([]core.HarnessSelectedPayload, 0, len(evts))
	for _, e := range evts {
		if e.EventType != core.EventTypeHarnessSelected {
			continue
		}
		var pl core.HarnessSelectedPayload
		if err := json.Unmarshal(e.Payload, &pl); err != nil {
			t.Fatalf("vs0: harness_selected payload unmarshal: %v", err)
		}
		out = append(out, pl)
	}
	return out
}

// vs0Bead builds a BeadRecord carrying the supplied tier-1 labels.
func vs0Bead(id string, labels ...string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID(id),
		Title:         "hk-01vs0 cognition-gate harness fixture bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: id,
	}
}

// vs0GateControlPoint is a Cognition-tagged Gate ControlPoint — the shape
// dispatchDotGateNode routes into buildCognitionGateEval for.
func vs0GateControlPoint() core.ControlPoint {
	return core.ControlPoint{
		Name: "hk01vs0_cognition_gate",
		Kind: core.KindGate,
		Evaluator: core.Evaluator{
			Mode: core.ModeTagCognition,
			DelegationPath: &core.DelegationPath{
				Role:              "gate-evaluator",
				ModelClass:        "reviewer-tier-1",
				InputSchemaRef:    "hk01vs0-input",
				ResponseSchemaRef: "hk01vs0-response",
				PromptTemplateRef: "hk01vs0-prompt",
			},
		},
	}
}

// vs0HarnessRegistry mirrors the production newHarnessRegistry (claude + codex +
// pi, same SessionIDPolicy per harness — the only property the hk-01vs0
// correction reads) but points the codex and pi binaries at paths that cannot
// exist.
//
// That binary is load-bearing for SAFETY, not for the assertion: when this
// file's wiring is deliberately deleted to prove the tests can fail, the gate
// falls back to codex and handler.Launch would otherwise resolve the operator's
// REAL `codex` on PATH and start an agent against their account from a temp
// dir. A nonexistent binary makes that exec fail instantly instead.
func vs0HarnessRegistry(t *testing.T) *handlercontract.HarnessRegistry {
	t.Helper()
	missing := filepath.Join(t.TempDir(), "no-such-harness-binary-hk01vs0")
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, daemon.NewClaudeHarness()); err != nil {
		t.Fatalf("vs0HarnessRegistry: register claude: %v", err)
	}
	if err := reg.Register(core.AgentTypeCodex, daemon.NewCodexHarness(missing, "")); err != nil {
		t.Fatalf("vs0HarnessRegistry: register codex: %v", err)
	}
	if err := reg.Register(core.AgentTypePi, daemon.NewPiHarness(missing, "", "", "", "", "", "")); err != nil {
		t.Fatalf("vs0HarnessRegistry: register pi: %v", err)
	}
	return reg
}

// vs0GateNode is the DOT gate node that references the ControlPoint above.
//
// Harness is deliberately left EMPTY. node.Harness is read only by
// dispatchDotAgenticNode (dot_cascade.go); the gate path has never consulted it,
// so there is no gate-level operator pin for the hk-01vs0 correction to protect.
func vs0GateNode(gateRef string) *dot.Node {
	return &dot.Node{
		ID:      "hk01vs0_gate",
		Type:    core.NodeTypeGate,
		GateRef: gateRef,
	}
}

// vs0Worktree is a bare worktree directory. The gate's own writes
// (.harmonik/gate-task.md) and buildClaudeLaunchSpec's writes (.claude/settings.json,
// the isolated ~/.claude.json trust entry) are all plain file writes — no git
// repo is required to reach and pass the launch-spec build.
func vs0Worktree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("vs0Worktree: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// vs0Result is the outcome of one production cognition-gate dispatch.
type vs0Result struct {
	selected       []core.HarnessSelectedPayload
	inheritedCalls int
	// dispatchErr is executeCognitionGate's own error. The dispatch is EXPECTED
	// to fail (the fixture's agent binary cannot exist), so it is never asserted
	// on — but it is reported in every failure message. Without it, a fixture
	// transient that aborts BEFORE the launch-spec build (e.g. the gate-task.md
	// write failing on a saturated host) is indistinguishable from the real
	// regression, since both present as "no harness_selected event".
	dispatchErr error
}

// vs0RunGate drives the PRODUCTION executeCognitionGate with a deps set wired
// exactly as beadRunOne wires it: deps.launchSpecBuilder is the real
// routedLaunchSpecBuilder bound to this beadRecord and this global default, and
// deps.harnessRegistry is the production registry.
//
// The dispatch is expected to fail after the launch-spec build (handlerBinary is
// a path that does not exist, so handler.Launch cannot start a process). That is
// deliberate: it keeps the fixture free of tmux and of a real agent while still
// executing every line of dot_gate.go up to and including the harness decision.
func vs0RunGate(t *testing.T, bead core.BeadRecord, globalDefault core.AgentType) vs0Result {
	t.Helper()

	// buildClaudeLaunchSpec upserts worktree trust + theme into ~/.claude.json;
	// redirect it at a temp file so the operator's real config is untouched.
	// os.Setenv-based, hence no t.Parallel in the callers.
	rlIsolateClaudeConfig(t)

	reg := vs0HarnessRegistry(t)
	bus := &vs0Bus{}

	var mu sync.Mutex
	inheritedCalls := 0
	inherited := daemon.ExportedObservedRoutedLaunchSpecBuilder(
		reg, bead, globalDefault, bus,
		func() {
			mu.Lock()
			inheritedCalls++
			mu.Unlock()
		},
	)

	projectDir := t.TempDir()
	wtPath := vs0Worktree(t)

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:  &stubBeadLedger{},
		Bus:        bus,
		ProjectDir: projectDir,
		// A binary that cannot exist: handler.Launch fails at exec, AFTER the
		// launch-spec build has already made (and recorded) the harness decision.
		HandlerBinary:       filepath.Join(projectDir, "no-such-agent-binary-hk01vs0"),
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    handlercontract.NewAdapterRegistry(),
		AgentReadyTimeout:   100 * time.Millisecond,
		HookStore:           daemon.ExportedNewHookSessionStore(),
		HarnessRegistry:     reg,
		DefaultHarness:      globalDefault,
		LaunchSpecBuilder:   inherited,
	})

	cp := vs0GateControlPoint()
	node := vs0GateNode(cp.Name)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Error is expected (the launch cannot start); the harness decision is the
	// observable and it is already on the bus by then. Kept for failure messages.
	dispatchErr := daemon.ExportedExecuteCognitionGate(
		ctx, deps, implReadyFixtureRunID(t), cp, wtPath, node, bead.BeadID, bead,
	)

	mu.Lock()
	calls := inheritedCalls
	mu.Unlock()

	return vs0Result{
		selected:       vs0HarnessSelected(t, bus),
		inheritedCalls: calls,
		dispatchErr:    dispatchErr,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. The defect: a captured harness reaches the gate and must be corrected.
// ─────────────────────────────────────────────────────────────────────────────

// TestCognitionGateNeverInheritsCapturedHarness_hk01vs0 drives the production
// gate dispatch for each way a SessionIDCaptured harness reaches it, and asserts
// the gate lands on claude-code with the inherited implementer builder bypassed.
//
// Regression shape if this fails: the gate runs on codex/pi, is briefed with an
// IMPLEMENTER seed prompt that never mentions gate-task.md, emits no
// agent_ready, and the run dies with "cognition gate ...: agent_ready_timeout".
func TestCognitionGateNeverInheritsCapturedHarness_hk01vs0(t *testing.T) {
	// No t.Parallel: rlIsolateClaudeConfig uses os.Setenv (process-global).

	tests := []struct {
		name          string
		bead          core.BeadRecord
		globalDefault core.AgentType
	}{
		{
			// THE case that matters for the harness ramp: a per-bead tier-1 label.
			// resolveHarness returns it immediately at tier 1, ahead of everything.
			name:          "tier-1 harness:codex bead label",
			bead:          vs0Bead("hk01vs0-gate-label-codex", "harness:codex"),
			globalDefault: core.AgentType(""),
		},
		{
			name:          "tier-1 harness:pi bead label",
			bead:          vs0Bead("hk01vs0-gate-label-pi", "harness:pi"),
			globalDefault: core.AgentType(""),
		},
		{
			// tier-4 spelling of the same defect (the global default being retired).
			name:          "tier-4 global codex default, no label",
			bead:          vs0Bead("hk01vs0-gate-global-codex"),
			globalDefault: core.AgentTypeCodex,
		},
		{
			// Both at once: the label must not sneak back in past the correction
			// (the hk-2jxqg footgun — routedLaunchSpecBuilder would let it).
			name:          "tier-1 label AND tier-4 default both codex",
			bead:          vs0Bead("hk01vs0-gate-both-codex", "harness:codex"),
			globalDefault: core.AgentTypeCodex,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := vs0RunGate(t, tc.bead, tc.globalDefault)

			if len(got.selected) != 1 {
				t.Fatalf("cognition gate emitted %d harness_selected events; want exactly 1 "+
					"(payloads: %+v; dispatch error: %v) — zero events means the dispatch "+
					"never reached the launch-spec build, which is a fixture problem, not "+
					"the hk-01vs0 regression", len(got.selected), got.selected, got.dispatchErr)
			}
			pl := got.selected[0]
			if !pl.Valid() {
				t.Errorf("cognition gate harness_selected payload invalid: %+v", pl)
			}
			if gotType, want := core.AgentType(pl.AgentType), core.AgentTypeClaudeCode; gotType != want {
				t.Fatalf("cognition gate resolved harness = %q; want %q — a SessionIDCaptured "+
					"harness cannot review: it is briefed with the IMPLEMENTER seed prompt "+
					"(codexlaunchspec.go never reads rc.phase), never writes "+
					".harmonik/gate-verdict.json, and never emits agent_ready, so the gate "+
					"dies at \"cognition gate ...: agent_ready_timeout\" (hk-01vs0)",
					gotType, want)
			}
			if pl.Tier != 3 {
				t.Errorf("cognition gate harness_selected tier = %d; want 3 — the correction must "+
					"be pinned via pinnedHarnessLaunchSpecBuilder, not re-resolved through "+
					"routedLaunchSpecBuilder (which would let the tier-1 harness label win "+
					"again, hk-2jxqg)", pl.Tier)
			}
			if pl.BeadID != string(tc.bead.BeadID) {
				t.Errorf("cognition gate harness_selected bead_id = %q; want %q", pl.BeadID, tc.bead.BeadID)
			}
			if got.inheritedCalls != 0 {
				t.Fatalf("cognition gate consulted the INHERITED implementer launch-spec builder "+
					"%d time(s); want 0 — deps.launchSpecBuilder is bound to the bead's own "+
					"harness and is exactly what must be bypassed for a reviewer-class node "+
					"(hk-01vs0; dispatch error: %v)", got.inheritedCalls, got.dispatchErr)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. The negative: an all-claude run must be untouched.
// ─────────────────────────────────────────────────────────────────────────────

// TestCognitionGateAllClaudeUnchanged_hk01vs0 is the no-op guard. When the
// implementer harness is claude (SessionIDMinted) there is nothing to correct,
// so the gate must keep deps.launchSpecBuilder — the same object, actually
// invoked — and the selected harness must still be claude-code at the tier the
// inherited walk produced. A fix that pinned the gate unconditionally would
// change behaviour for every existing all-claude deployment; this fails then.
func TestCognitionGateAllClaudeUnchanged_hk01vs0(t *testing.T) {
	// No t.Parallel: rlIsolateClaudeConfig uses os.Setenv (process-global).

	tests := []struct {
		name          string
		bead          core.BeadRecord
		globalDefault core.AgentType
	}{
		{
			name:          "no harness label, built-in default",
			bead:          vs0Bead("hk01vs0-gate-allclaude-default"),
			globalDefault: core.AgentType(""),
		},
		{
			name:          "explicit tier-1 harness:claude-code label",
			bead:          vs0Bead("hk01vs0-gate-allclaude-label", "harness:claude-code"),
			globalDefault: core.AgentType(""),
		},
		{
			name:          "explicit tier-4 claude-code default",
			bead:          vs0Bead("hk01vs0-gate-allclaude-global"),
			globalDefault: core.AgentTypeClaudeCode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := vs0RunGate(t, tc.bead, tc.globalDefault)

			if got.inheritedCalls != 1 {
				t.Fatalf("all-claude cognition gate invoked the inherited launch-spec builder "+
					"%d time(s); want exactly 1 — an all-claude run must be byte-identical to "+
					"pre-hk-01vs0 behaviour, i.e. deps.launchSpecBuilder is used unchanged "+
					"(regression shape: the gate now pins unconditionally; dispatch error: %v)",
					got.inheritedCalls, got.dispatchErr)
			}
			if len(got.selected) != 1 {
				t.Fatalf("all-claude cognition gate emitted %d harness_selected events; want exactly 1 "+
					"(payloads: %+v; dispatch error: %v) — zero events means the dispatch "+
					"never reached the launch-spec build, which is a fixture problem, not "+
					"the hk-01vs0 regression", len(got.selected), got.selected, got.dispatchErr)
			}
			pl := got.selected[0]
			if gotType, want := core.AgentType(pl.AgentType), core.AgentTypeClaudeCode; gotType != want {
				t.Fatalf("all-claude cognition gate resolved harness = %q; want %q", gotType, want)
			}
			if pl.Tier == 3 {
				t.Errorf("all-claude cognition gate harness_selected tier = 3; want the tier the "+
					"INHERITED walk produced (1 or 4) — tier 3 means the gate pinned when it "+
					"had nothing to correct, which is not byte-identical (payload: %+v)", pl)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Production-wiring pin.
// ─────────────────────────────────────────────────────────────────────────────

// TestCognitionGateHarnessDispatchWiring_hk01vs0 pins the dot_gate.go call site
// and the beadRecord threading that feeds it. The behaviour tests above already
// drive the production function, so this is belt-and-braces for the specific
// failure mode this lane keeps hitting: a correction that survives only in a
// helper while the call site quietly reverts. Same idiom as
// TestReviewerNeverInheritsCapturedHarness_DispatchWiring_hkpkxju.
func TestCognitionGateHarnessDispatchWiring_hk01vs0(t *testing.T) {
	t.Parallel()

	tests := []struct {
		file string
		want []string
	}{
		{
			file: "dot_gate.go",
			want: []string{
				"gateInheritedHarness := dotReviewerInheritedHarnessOverride(",
				"specBuilder = pinnedHarnessLaunchSpecBuilder(",
				"gateInheritedHarness,",
			},
		},
		{
			// Without beadRecord reaching the gate, the tier-1 harness:<type> LABEL
			// is invisible to the correction and the label trigger returns.
			file: "dot_cascade.go",
			want: []string{"beadID, beadRecord, // hk-01vs0"},
		},
		{
			file: "sub_workflow_runner.go",
			want: []string{"r.beadID, r.beadRecord, // hk-01vs0"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			body, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			src := string(body)
			for _, want := range tc.want {
				if !strings.Contains(src, want) {
					t.Fatalf("hk-01vs0 regression: %s no longer contains %q — the cognition gate "+
						"is back to inheriting the implementer harness", tc.file, want)
				}
			}
		})
	}
}
