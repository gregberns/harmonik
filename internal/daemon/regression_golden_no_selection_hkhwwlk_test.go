package daemon_test

// regression_golden_no_selection_hkhwwlk_test.go — Regression golden: no-selection
// bead runs byte-identical claude (argv golden + event-seq invariant); the N-1
// back-compat proof [C6/T16].
//
// This file proves that the addition of CodexHarness to newHarnessRegistry (T12,
// hk-xhawy) does NOT change the observable behaviour for beads dispatched without
// a harness: label (the "no-selection" case). A no-selection bead must still
// resolve to core.AgentTypeClaudeCode and produce an argv / env that is
// structurally byte-identical to a pre-codex buildClaudeLaunchSpec call.
//
// Tested invariants:
//  1. resolveHarness with a no-label bead → core.AgentTypeClaudeCode (tier-4 fallback,
//     not core.AgentTypeCodex) — even with both harnesses registered.
//  2. routedLaunchSpecBuilder (via production newHarnessRegistry, which now
//     registers BOTH harnesses) produces:
//     a. Binary = "claude"  (argv golden: the first field)
//     b. Args = normalised match of direct buildClaudeLaunchSpec (argv parity)
//     c. Env key set = normalised match of direct buildClaudeLaunchSpec (env parity)
//     d. HARMONIK_AGENT_TYPE = "claude-code" (not "codex")
//  3. ClaudeHarness.Completion() = CompletionEventStreamThenQuit (event-seq invariant:
//     the codex CompletionProcessExit bypass is NOT engaged for no-selection runs).
//
// Spec: codex-harness C6-migration-test-spec.md §Verification (AC6.1);
//       specs/harness-contract.md §2 N2 (CompletionMode).
//
// These tests do NOT call t.Parallel() because they invoke buildClaudeLaunchSpec
// which acquires the EnsureWorktreeTrust file lock under ~/.claude.json.
// Running them in parallel with the integration suite causes spurious
// "write-lock acquire timed out" failures.
//
// Helper prefix: regressionGoldenNoSelFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// Bead: hk-hwwlk [C6/T16]

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// regressionGoldenNoSelFixtureWorkspace creates a temp workspace with the
// minimal .claude/ directory required by MaterializeClaudeSettings.
func regressionGoldenNoSelFixtureWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatalf("regressionGoldenNoSelFixtureWorkspace: MkdirAll .claude: %v", err)
	}
	return dir
}

// regressionGoldenNoSelFixtureBead returns a BeadRecord with the given label set.
// Passing nil or an empty slice produces a bead with no harness: label.
func regressionGoldenNoSelFixtureBead(t *testing.T, labels []string) core.BeadRecord {
	t.Helper()
	return core.BeadRecord{
		BeadID:        "test-bead-no-sel-hk-hwwlk",
		Title:         "regression golden no-selection bead",
		BeadType:      "task",
		Status:        core.CoarseStatusOpen,
		Labels:        labels,
		AuditTrailRef: "test-bead-no-sel-hk-hwwlk",
	}
}

// regressionGoldenNoSelFixtureRunCtx builds an ExportedClaudeRunCtx for
// single-mode dispatch (the simplest no-selection scenario).
func regressionGoldenNoSelFixtureRunCtx(t *testing.T, workspacePath string) daemon.ExportedClaudeRunCtx {
	t.Helper()
	runUID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("regressionGoldenNoSelFixtureRunCtx: NewV7: %v", err)
	}
	return daemon.ExportedClaudeRunCtx{
		RunID:          core.RunID(runUID),
		BeadID:         "test-bead-no-sel-hk-hwwlk",
		WorkspacePath:  workspacePath,
		DaemonSocket:   "/tmp/harmonik-test-no-sel-hk-hwwlk.sock",
		WorkflowMode:   core.WorkflowModeSingle,
		Phase:          "",
		IterationCount: 0,
		HandlerBinary:  "claude",
		BaseEnv:        []string{"HARMONIK_PROJECT_HASH=deadbeef123456"},
	}
}

// regressionGoldenNoSelFixtureBus returns a real event bus for resolveHarness.
func regressionGoldenNoSelFixtureBus(t *testing.T) handlercontract.EventEmitter {
	t.Helper()
	return eventbus.NewBusImpl()
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. resolveHarness invariant: no-label bead → AgentTypeClaudeCode
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_Resolves verifies that a bead with no harness:
// label resolves to core.AgentTypeClaudeCode via the four-tier precedence walk,
// even after CodexHarness is registered in newHarnessRegistry (T12, hk-xhawy).
// This is the N-1 back-compat guarantee at tier-4 fallback.
func TestRegressionGoldenNoSelection_Resolves(t *testing.T) {
	bead := regressionGoldenNoSelFixtureBead(t, nil) // no labels
	got := daemon.ExportedResolveHarness(
		context.Background(), bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("no-selection resolveHarness = %q; want %q (N-1 back-compat)",
			got, core.AgentTypeClaudeCode)
	}
}

// TestRegressionGoldenNoSelection_ResolvesWithIrrelevantLabels verifies that bead
// labels unrelated to harness selection (codename:, priority:, type: etc.) do not
// affect the tier-4 fallback to claude-code.
func TestRegressionGoldenNoSelection_ResolvesWithIrrelevantLabels(t *testing.T) {
	bead := regressionGoldenNoSelFixtureBead(t, []string{
		"codename:my-work",
		"priority:high",
		"type:feature",
	})
	got := daemon.ExportedResolveHarness(
		context.Background(), bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)
	if got != core.AgentTypeClaudeCode {
		t.Errorf("non-harness labels: resolveHarness = %q; want %q (N-1 back-compat)",
			got, core.AgentTypeClaudeCode)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2a. argv golden: Binary must be "claude"
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_BinaryIsClaudeCode verifies that the routed
// builder (using the production newHarnessRegistry with BOTH harnesses) produces
// Binary = "claude" for a no-label bead.
func TestRegressionGoldenNoSelection_BinaryIsClaudeCode(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := regressionGoldenNoSelFixtureBead(t, nil)
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)

	ws := regressionGoldenNoSelFixtureWorkspace(t)
	rc := regressionGoldenNoSelFixtureRunCtx(t, ws)
	spec, _, err := build(context.Background(), rc)
	if err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	const wantBinary = "claude"
	if spec.Binary != wantBinary {
		t.Errorf("Binary = %q; want %q (argv golden: N-1 back-compat)", spec.Binary, wantBinary)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2b. argv golden: flag-shape invariants
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_ArgvGolden verifies the golden flag-shape for a
// no-label single-mode bead through the routed builder:
//   - --session-id flag is present with a non-empty value (CHB-008)
//   - --resume is absent (single-mode, not implementer-resume)
func TestRegressionGoldenNoSelection_ArgvGolden(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := regressionGoldenNoSelFixtureBead(t, nil)
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)

	ws := regressionGoldenNoSelFixtureWorkspace(t)
	rc := regressionGoldenNoSelFixtureRunCtx(t, ws)
	spec, _, err := build(context.Background(), rc)
	if err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	// Golden: --session-id must be present (single-mode, CHB-008).
	regressionGoldenNoSelAssertSessionIDFlag(t, spec.Args)

	// Golden: --resume must NOT be present (not implementer-resume).
	for _, a := range spec.Args {
		if a == "--resume" {
			t.Error("argv golden: --resume must not be present for single-mode no-selection run (CHB-008)")
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2c. env golden: HARMONIK_* key set and HARMONIK_AGENT_TYPE value
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_EnvKeysGolden verifies the routed builder emits
// the expected CHB-006 HARMONIK_* env keys for a no-label bead, and that
// HARMONIK_AGENT_TYPE = "claude-code" (not "codex").
func TestRegressionGoldenNoSelection_EnvKeysGolden(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := regressionGoldenNoSelFixtureBead(t, nil)
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)

	ws := regressionGoldenNoSelFixtureWorkspace(t)
	rc := regressionGoldenNoSelFixtureRunCtx(t, ws)
	spec, _, err := build(context.Background(), rc)
	if err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	// CHB-006: all HARMONIK_* env keys must be present.
	for _, key := range []string{
		"HARMONIK_RUN_ID",
		"HARMONIK_DAEMON_SOCKET",
		"HARMONIK_CLAUDE_SESSION_ID",
		"HARMONIK_HANDLER_SESSION_ID",
		"HARMONIK_AGENT_TYPE",
	} {
		regressionGoldenNoSelAssertEnvKey(t, spec.Env, key)
	}

	// Golden: HARMONIK_AGENT_TYPE must be "claude-code" — not "codex".
	regressionGoldenNoSelAssertEnvValue(t, spec.Env, "HARMONIK_AGENT_TYPE", string(core.AgentTypeClaudeCode))
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Spec parity: routed builder == direct buildClaudeLaunchSpec (byte-identical)
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_SpecParity is the core N-1 back-compat proof:
// the routed launchSpecBuilder (production newHarnessRegistry, no-label bead)
// produces a LaunchSpec that is structurally byte-identical to a direct
// buildClaudeLaunchSpec call. Binary, normalized-Args, and Env-key-set must match.
//
// Any regression here means the codex wiring has broken the N-1 guarantee.
// AC6.1 (C6-migration-test-spec.md §Verification).
func TestRegressionGoldenNoSelection_SpecParity(t *testing.T) {
	// Reference: direct buildClaudeLaunchSpec on a fresh workspace.
	wsRef := regressionGoldenNoSelFixtureWorkspace(t)
	rcRef := regressionGoldenNoSelFixtureRunCtx(t, wsRef)
	refSpec, _, err := daemon.ExportedBuildClaudeLaunchSpec(context.Background(), rcRef)
	if err != nil {
		t.Fatalf("reference buildClaudeLaunchSpec: %v", err)
	}

	// Routed: via newHarnessRegistry + resolveHarness (no-label → claude-code).
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}
	bead := regressionGoldenNoSelFixtureBead(t, nil) // no harness label
	build := daemon.ExportedRoutedLaunchSpecBuilder(
		reg, bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)
	wsRouted := regressionGoldenNoSelFixtureWorkspace(t)
	rcRouted := regressionGoldenNoSelFixtureRunCtx(t, wsRouted)
	routedSpec, _, err := build(context.Background(), rcRouted)
	if err != nil {
		t.Fatalf("routed launchSpecBuilder: %v", err)
	}

	// Binary parity (byte-identical).
	if routedSpec.Binary != refSpec.Binary {
		t.Errorf("Binary parity FAIL: routed = %q; want %q (N-1 back-compat)",
			routedSpec.Binary, refSpec.Binary)
	}

	// Args parity: session-id VALUES differ per-run (freshly minted UUIDs) but
	// the structural flag set must be byte-identical after normalisation.
	routedNorm := regressionGoldenNoSelNormalizeSessionID(routedSpec.Args)
	refNorm := regressionGoldenNoSelNormalizeSessionID(refSpec.Args)
	if !regressionGoldenNoSelEqualStrings(routedNorm, refNorm) {
		t.Errorf("Args parity FAIL (session-id normalised):\n routed = %v\n   want = %v",
			routedNorm, refNorm)
	}

	// Env key-set parity: values for run/session IDs differ per-run.
	routedKeys := regressionGoldenNoSelEnvKeys(routedSpec.Env)
	refKeys := regressionGoldenNoSelEnvKeys(refSpec.Env)
	if !regressionGoldenNoSelEqualKeySet(routedKeys, refKeys) {
		t.Errorf("Env key-set parity FAIL:\n routed = %v\n   want = %v",
			routedKeys, refKeys)
	}

	// WorkDir parity: each spec must point at its own workspace.
	if routedSpec.WorkDir != wsRouted {
		t.Errorf("routed WorkDir = %q; want %q", routedSpec.WorkDir, wsRouted)
	}
	if refSpec.WorkDir != wsRef {
		t.Errorf("ref WorkDir = %q; want %q", refSpec.WorkDir, wsRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Event-sequence invariant: completion mode is CompletionEventStreamThenQuit
// ─────────────────────────────────────────────────────────────────────────────

// TestRegressionGoldenNoSelection_EventSeqInvariant verifies the event-sequence
// golden for no-selection runs: the resolved harness is ClaudeHarness, and its
// Completion() = CompletionEventStreamThenQuit.
//
// The completion mode governs which code path the shared loop takes at the
// Completion() gate (dot_cascade.go):
//   - CompletionEventStreamThenQuit → /quit + kill-grace path → standard claude
//     event sequence (run_started, launch_initiated, agent_ready, run_completed).
//   - CompletionProcessExit (codex path) → bypasses pasteInjectQuitOnCommit →
//     relies on sess.Wait + commitHardCeiling alone.
//
// A no-selection run MUST use CompletionEventStreamThenQuit. The codex
// CompletionProcessExit bypass MUST NOT be engaged. This is the event-sequence
// back-compat golden (C6 AC6.1).
//
// Spec: specs/harness-contract.md §2 N2; codex-harness C5-workflow-integration-spec.md
// §Completion() gate.
func TestRegressionGoldenNoSelection_EventSeqInvariant(t *testing.T) {
	reg, err := daemon.ExportedNewHarnessRegistry()
	if err != nil {
		t.Fatalf("ExportedNewHarnessRegistry: %v", err)
	}

	// Resolve the harness for a no-label bead (all tiers absent → tier-4 fallback).
	bead := regressionGoldenNoSelFixtureBead(t, nil)
	resolvedType := daemon.ExportedResolveHarness(
		context.Background(), bead,
		core.AgentType(""), core.AgentType(""), core.AgentType(""),
		regressionGoldenNoSelFixtureBus(t),
	)
	if resolvedType != core.AgentTypeClaudeCode {
		t.Fatalf("resolveHarness = %q; want %q (test setup precondition)", resolvedType, core.AgentTypeClaudeCode)
	}

	h, err := reg.ForAgent(resolvedType)
	if err != nil {
		t.Fatalf("ForAgent(%q): %v", resolvedType, err)
	}

	// Event-sequence golden: Completion MUST be CompletionEventStreamThenQuit.
	if got := h.Completion(); got != handlercontract.CompletionEventStreamThenQuit {
		t.Errorf(
			"Completion() = %v; want CompletionEventStreamThenQuit\n"+
				"(event-seq golden: codex CompletionProcessExit bypass MUST NOT be engaged for no-selection run; AC6.1)",
			got,
		)
	}
}

// TestRegressionGoldenNoSelection_EventSeqCodexPathNotTaken verifies that the
// CodexHarness Completion() returns CompletionProcessExit, confirming that it is
// distinctly different from the claude path. This is the discriminant test: if
// someone accidentally changes the no-selection resolution to codex, the
// TestRegressionGoldenNoSelection_EventSeqInvariant test will fail because the
// two Completion() values are distinct.
func TestRegressionGoldenNoSelection_EventSeqCodexPathNotTaken(t *testing.T) {
	t.Parallel()

	codexHarness := daemon.ExportedNewCodexHarness("", "")
	if got := codexHarness.Completion(); got != handlercontract.CompletionProcessExit {
		t.Errorf("CodexHarness.Completion() = %v; want CompletionProcessExit (discriminant check)",
			got)
	}

	claudeHarness := daemon.ExportedNewClaudeHarness()
	if got := claudeHarness.Completion(); got == handlercontract.CompletionProcessExit {
		t.Error("ClaudeHarness.Completion() = CompletionProcessExit; must differ from CodexHarness (discriminant check)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// assertion helpers (prefix: regressionGoldenNoSel*)
// ─────────────────────────────────────────────────────────────────────────────

// regressionGoldenNoSelAssertSessionIDFlag verifies --session-id is present with
// a non-empty value in args (CHB-008: single/initial uses --session-id).
func regressionGoldenNoSelAssertSessionIDFlag(t *testing.T, args []string) {
	t.Helper()
	for i, a := range args {
		if a == "--session-id" {
			if i+1 >= len(args) || args[i+1] == "" {
				t.Error("--session-id present but session ID value is missing or empty")
			}
			return
		}
	}
	t.Errorf("--session-id not found in args %v; required for single-mode (CHB-008)", args)
}

// regressionGoldenNoSelAssertEnvKey verifies that env contains a "KEY=..." entry.
func regressionGoldenNoSelAssertEnvKey(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	t.Errorf("env missing key %q (CHB-006 golden)", key)
}

// regressionGoldenNoSelAssertEnvValue verifies env contains "KEY=VALUE" exactly.
func regressionGoldenNoSelAssertEnvValue(t *testing.T, env []string, key, wantVal string) {
	t.Helper()
	want := key + "=" + wantVal
	for _, e := range env {
		if e == want {
			return
		}
	}
	t.Errorf("env: %q not set to %q (golden: no-selection must report agent type = claude-code)", key, wantVal)
}

// regressionGoldenNoSelNormalizeSessionID returns a copy of args with the value
// following --session-id or --resume replaced by a placeholder, so per-run UUID
// differences do not defeat structural argv comparison.
func regressionGoldenNoSelNormalizeSessionID(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if (a == "--session-id" || a == "--resume") && i+1 < len(out) {
			out[i+1] = "<session-id>"
		}
	}
	return out
}

// regressionGoldenNoSelEnvKeys extracts the "KEY" portion from "KEY=VALUE" entries
// and returns a set for key-set comparison.
func regressionGoldenNoSelEnvKeys(env []string) map[string]bool {
	keys := make(map[string]bool, len(env))
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			keys[e[:i]] = true
		}
	}
	return keys
}

// regressionGoldenNoSelEqualStrings reports whether a and b are element-wise equal.
func regressionGoldenNoSelEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// regressionGoldenNoSelEqualKeySet reports whether two env key-sets are equal.
func regressionGoldenNoSelEqualKeySet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
