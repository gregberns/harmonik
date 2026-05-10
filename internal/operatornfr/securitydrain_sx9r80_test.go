package operatornfr_test

// securityDrainFixture — spec-level harness for hk-sx9r.80.
//
// Covers: ON-022 (secrets injected at launch, never logged), ON-023 (compile-time
// schema check), ON-024 (sandbox invariant), ON-025 (egress policy), ON-026
// (prompt-injection handler-owned), ON-027 (8-step drain ordering), ON-027a
// (drain step atomicity + crash-recovery), ON-028 (stop --immediate skips 2-3),
// ON-029 (drain timeout configurable), ON-INV-003 (secrets never in durable sinks).
//
// These are spec-artifact existence and structural-constraint tests. Runtime
// enforcement lives in the subsystem implementations; this file is the §10.2
// sensor that verifies the obligation catalog exists and is internally consistent.
//
// Spec ref: specs/operator-nfr.md §4.7, §5 ON-INV-003, §10.2.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// securityDrainFixtureDrainStep models one step in the ON-027 8-step drain
// sequence. Fields capture the step number, its canonical name, and whether
// stop --immediate may skip it (per ON-028).
//
// Spec ref: operator-nfr.md §4.7 ON-027 — eight ordered drain steps.
type securityDrainFixtureDrainStep struct {
	Number          int    // 1-based step index (step 3a is encoded as 30 for ordering)
	Name            string // canonical step name as described in §4.7 ON-027
	SkipOnImmediate bool   // true if ON-028 permits skipping this step
}

// securityDrainFixtureDrainSequence is the authoritative fixture representation
// of the ON-027 eight-step drain sequence.
//
// The step order is normative per §4.7 ON-027. Steps 2 and 3 are marked
// SkipOnImmediate per ON-028. Step 3a (intent-log drain) is encoded as step
// number 30 so it sorts between 3 and 4 in integer ordering.
//
// Spec ref: operator-nfr.md §4.7 ON-027.
// Numbers use a 10x scale so step 3a (35) sorts between step 3 (30) and step 4
// (40) under normal integer comparison. The logical step labels are 1-7 plus
// 3a; the encoded Numbers preserve the canonical ordering invariant.
var securityDrainFixtureDrainSequence = []securityDrainFixtureDrainStep{
	{10, "orchestrator-stops-pulling", false},                  // logical step 1
	{20, "in-flight-runs-reach-checkpoint-then-suspend", true}, // logical step 2
	{30, "agent-runners-wait-for-handler-subprocesses", true},  // logical step 3
	{35, "br-cli-intent-log-drain", false},                     // logical step 3a
	{40, "event-bus-flushes-pending-events-fsync", false},      // logical step 4
	{50, "memory-layer-flushes-indexing", false},               // logical step 5
	{60, "workspace-manager-unlocks-leased-workspaces", false}, // logical step 6
	{70, "orchestrator-exits-or-enters-paused", false},         // logical step 7
}

// securityDrainFixtureDurableSink models one durable sink that must be free of
// unredacted secrets per ON-INV-003.
//
// Spec ref: operator-nfr.md §5 ON-INV-003.
type securityDrainFixtureDurableSink struct {
	Name    string // human-readable sink name
	SpecRef string // normative spec section owning the sink
}

// securityDrainFixtureDurableSinks enumerates every durable sink declared by
// the event-model and workspace-model specs that ON-INV-003 requires to be
// secrets-free.
//
// Spec ref: operator-nfr.md §5 ON-INV-003 — "For every event-model-declared
// sink (event log per [event-model.md §4.4], dead-letter log per
// [event-model.md §4.3], session log per [workspace-model.md §4.7])".
var securityDrainFixtureDurableSinks = []securityDrainFixtureDurableSink{
	{"event-log", "event-model.md §4.4"},
	{"dead-letter-log", "event-model.md §4.3"},
	{"session-log", "workspace-model.md §4.7"},
}

// securityDrainFixtureExitCodeOnDrainTimeout is the §8 exit code that must be
// emitted when any drain step exceeds its timeout bound, per ON-027 step 7.
//
// Spec ref: operator-nfr.md §4.7 ON-027; §8 code 11 drain-timeout-escalated.
const securityDrainFixtureExitCodeOnDrainTimeout = 11

// securityDrainFixtureExitCodeOnDrainError is the §8 exit code for a
// non-recoverable per-step drain error, per §8 code 21.
//
// Spec ref: operator-nfr.md §8 — code 21 drain-step-errored.
const securityDrainFixtureExitCodeOnDrainError = 21

// TestON027_DrainSequenceHasEightSteps verifies the fixture drain sequence
// encodes exactly 8 logical steps (steps 1, 2, 3, 3a, 4, 5, 6, 7).
//
// Spec ref: operator-nfr.md §4.7 ON-027 — "the daemon MUST execute the
// shutdown/drain sequence … (1) … (2) … (3) … (3a) … (4) … (5) … (6) … (7)."
func TestON027_DrainSequenceHasEightSteps(t *testing.T) {
	t.Parallel()

	const wantSteps = 8
	if len(securityDrainFixtureDrainSequence) != wantSteps {
		t.Errorf("ON-027: drain sequence has %d entries, want %d (steps 1, 2, 3, 3a, 4, 5, 6, 7)",
			len(securityDrainFixtureDrainSequence), wantSteps)
	}
}

// TestON027_DrainSequenceStepsAreOrdered verifies that the step Numbers in the
// fixture are monotonically non-decreasing (step 3a = 30 is between 3 and 4).
//
// Spec ref: operator-nfr.md §4.7 ON-027 — "each step completing before the
// next begins."
func TestON027_DrainSequenceStepsAreOrdered(t *testing.T) {
	t.Parallel()

	for i := 1; i < len(securityDrainFixtureDrainSequence); i++ {
		prev := securityDrainFixtureDrainSequence[i-1]
		curr := securityDrainFixtureDrainSequence[i]
		if curr.Number <= prev.Number {
			t.Errorf("ON-027: drain step %d (%q) follows step %d (%q) but has non-increasing Number; steps must be strictly ordered",
				curr.Number, curr.Name, prev.Number, prev.Name)
		}
	}
}

// TestON028_OnlyStepsTwo_ThreeAreSkippable verifies that exactly steps 2 and 3
// are marked SkipOnImmediate=true, and all others are false.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "MUST skip steps 2 and 3 of
// §4.7.ON-027."
func TestON028_OnlyStepsTwo_ThreeAreSkippable(t *testing.T) {
	t.Parallel()

	for _, step := range securityDrainFixtureDrainSequence {
		step := step
		t.Run(step.Name, func(t *testing.T) {
			t.Parallel()

			// Step 2 is encoded as 20, step 3 as 30 in the 10x scale.
			wantSkip := step.Number == 20 || step.Number == 30
			if step.SkipOnImmediate != wantSkip {
				t.Errorf("ON-028: drain step %d (%q) SkipOnImmediate=%v, want %v; only steps 2 and 3 MUST be skipped on stop --immediate",
					step.Number, step.Name, step.SkipOnImmediate, wantSkip)
			}
		})
	}
}

// TestON027_DrainSequenceStepsHaveNames verifies that every drain step has a
// non-empty Name.
//
// Spec ref: operator-nfr.md §4.7 ON-027 — named steps as artifact.
func TestON027_DrainSequenceStepsHaveNames(t *testing.T) {
	t.Parallel()

	for _, step := range securityDrainFixtureDrainSequence {
		step := step
		t.Run(step.Name, func(t *testing.T) {
			t.Parallel()

			if step.Name == "" {
				t.Errorf("ON-027: drain step %d has empty Name; every step MUST be named", step.Number)
			}
		})
	}
}

// TestON027_DrainTimeoutEscalationExitCodeExistsInTaxonomy verifies that the
// drain-timeout exit code (§8 code 11) is present in the §8 taxonomy.
//
// Spec ref: operator-nfr.md §4.7 ON-027 step 7 — "exit code for
// 'drain-timeout-escalated' per §8."
func TestON027_DrainTimeoutEscalationExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeFixtureLookup(securityDrainFixtureExitCodeOnDrainTimeout)
	if !ok {
		t.Errorf("ON-027: drain-timeout exit code %d not in §8 taxonomy", securityDrainFixtureExitCodeOnDrainTimeout)
		return
	}
	if e.Category != "drain-timeout-escalated" {
		t.Errorf("ON-027: §8 code %d category = %q, want %q",
			securityDrainFixtureExitCodeOnDrainTimeout, e.Category, "drain-timeout-escalated")
	}
}

// TestON027_DrainStepErrorExitCodeExistsInTaxonomy verifies that the
// drain-step-errored exit code (§8 code 21) is present in the §8 taxonomy.
//
// Spec ref: operator-nfr.md §8 — code 21 drain-step-errored.
func TestON027_DrainStepErrorExitCodeExistsInTaxonomy(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeFixtureLookup(securityDrainFixtureExitCodeOnDrainError)
	if !ok {
		t.Errorf("ON-027: drain-step-error exit code %d not in §8 taxonomy", securityDrainFixtureExitCodeOnDrainError)
		return
	}
	if e.Category != "drain-step-errored" {
		t.Errorf("ON-027: §8 code %d category = %q, want %q",
			securityDrainFixtureExitCodeOnDrainError, e.Category, "drain-step-errored")
	}
}

// TestON022_SpecSectionExists verifies that ON-022 (secrets injected at handler
// launch and never logged) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.7 ON-022.
func TestON022_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-022") {
		t.Error("ON-022: specs/operator-nfr.md does not contain 'ON-022'")
	}
	if !strings.Contains(content, "Secrets are injected at handler launch") {
		t.Error("ON-022: specs/operator-nfr.md missing 'Secrets are injected at handler launch' heading text")
	}
}

// TestON023_SpecSectionExists verifies that ON-023 (compile-time payload-schema
// check) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.7 ON-023.
func TestON023_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-023") {
		t.Error("ON-023: specs/operator-nfr.md does not contain 'ON-023'")
	}
	if !strings.Contains(content, "compile-time") {
		t.Error("ON-023: specs/operator-nfr.md missing 'compile-time' in ON-023 context")
	}
}

// TestON024_SpecSectionExists verifies that ON-024 (command-execution sandbox
// invariant) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.7 ON-024.
func TestON024_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-024") {
		t.Error("ON-024: specs/operator-nfr.md does not contain 'ON-024'")
	}
	if !strings.Contains(content, "sandbox") {
		t.Error("ON-024: specs/operator-nfr.md missing 'sandbox' keyword in ON-024 context")
	}
}

// TestON025_SpecSectionExists verifies that ON-025 (network egress and
// skill-injection policy enforcement) exists.
//
// Spec ref: operator-nfr.md §4.7 ON-025.
func TestON025_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-025") {
		t.Error("ON-025: specs/operator-nfr.md does not contain 'ON-025'")
	}
}

// TestON026_SpecSectionExists verifies that ON-026 (prompt-injection defense
// is handler-owned) exists.
//
// Spec ref: operator-nfr.md §4.7 ON-026.
func TestON026_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-026") {
		t.Error("ON-026: specs/operator-nfr.md does not contain 'ON-026'")
	}
	if !strings.Contains(content, "Prompt-injection") {
		t.Error("ON-026: specs/operator-nfr.md missing 'Prompt-injection' heading text")
	}
}

// TestON027_SpecSectionExists verifies that ON-027 (8-step drain ordering)
// exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.7 ON-027.
func TestON027_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-027") {
		t.Error("ON-027: specs/operator-nfr.md does not contain 'ON-027'")
	}
	if !strings.Contains(content, "Graceful-shutdown ordering") {
		t.Error("ON-027: specs/operator-nfr.md missing 'Graceful-shutdown ordering' heading text")
	}
}

// TestON027a_SpecSectionExists verifies that ON-027a (drain step atomicity and
// crash-recovery) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §4.7 ON-027a.
func TestON027a_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-027a") {
		t.Error("ON-027a: specs/operator-nfr.md does not contain 'ON-027a'")
	}
	if !strings.Contains(content, "atomicity") {
		t.Error("ON-027a: specs/operator-nfr.md missing 'atomicity' in ON-027a context")
	}
}

// TestON028_SpecSectionExists verifies that ON-028 (stop --immediate skips
// drain steps 2-3) exists.
//
// Spec ref: operator-nfr.md §4.7 ON-028.
func TestON028_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-028") {
		t.Error("ON-028: specs/operator-nfr.md does not contain 'ON-028'")
	}
}

// TestON029_SpecSectionExists verifies that ON-029 (drain timeout
// operator-configurable) exists.
//
// Spec ref: operator-nfr.md §4.7 ON-029.
func TestON029_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-029") {
		t.Error("ON-029: specs/operator-nfr.md does not contain 'ON-029'")
	}
	if !strings.Contains(content, "configurable") {
		t.Error("ON-029: specs/operator-nfr.md missing 'configurable' in ON-029 context")
	}
}

// TestONINV003_SpecSectionExists verifies that ON-INV-003 (secrets never in
// durable sinks unredacted) exists in specs/operator-nfr.md.
//
// Spec ref: operator-nfr.md §5 ON-INV-003.
func TestONINV003_SpecSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "ON-INV-003") {
		t.Error("ON-INV-003: specs/operator-nfr.md does not contain 'ON-INV-003'")
	}
	if !strings.Contains(content, "Secrets never appear in durable sinks") {
		t.Error("ON-INV-003: specs/operator-nfr.md missing 'Secrets never appear in durable sinks' heading text")
	}
}

// TestONINV003_DurableSinksCoverEventModelDeclarations verifies that the
// ON-INV-003 durable-sinks fixture covers all three event-model-declared sinks.
//
// Spec ref: operator-nfr.md §5 ON-INV-003 — "For every event-model-declared
// sink (event log per [event-model.md §4.4], dead-letter log per
// [event-model.md §4.3], session log per [workspace-model.md §4.7])."
func TestONINV003_DurableSinksCoverEventModelDeclarations(t *testing.T) {
	t.Parallel()

	const wantSinks = 3
	if len(securityDrainFixtureDurableSinks) < wantSinks {
		t.Errorf("ON-INV-003: durable-sinks fixture has %d entries, want at least %d (event log, dead-letter log, session log)",
			len(securityDrainFixtureDurableSinks), wantSinks)
	}

	required := map[string]bool{
		"event-log":       false,
		"dead-letter-log": false,
		"session-log":     false,
	}
	for _, sink := range securityDrainFixtureDurableSinks {
		required[sink.Name] = true
	}
	for name, found := range required {
		if !found {
			t.Errorf("ON-INV-003: durable-sink %q is missing from the fixture; it is declared in the spec and must be covered", name)
		}
	}
}

// TestONINV003_DurableSinksHaveSpecRefs verifies that every durable sink in
// the fixture has a non-empty SpecRef.
//
// Spec ref: operator-nfr.md §5 ON-INV-003.
func TestONINV003_DurableSinksHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, sink := range securityDrainFixtureDurableSinks {
		sink := sink
		t.Run(sink.Name, func(t *testing.T) {
			t.Parallel()

			if sink.SpecRef == "" {
				t.Errorf("ON-INV-003: durable sink %q has empty SpecRef; each sink MUST cite its owning spec section", sink.Name)
			}
		})
	}
}

// TestON022_RedactorFailClosedSensorMustHaveTwoParts verifies that the
// ON-INV-003 sensor description in the spec names both required parts:
// (a) compile-time schema linter and (b) regression test harness.
//
// Spec ref: operator-nfr.md §5 ON-INV-003 Sensor — "Two-part sensor: (a)
// compile-time schema linter … (b) regression test harness."
func TestON022_RedactorFailClosedSensorMustHaveTwoParts(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	// Verify the sensor description calls out both parts.
	if !strings.Contains(content, "Two-part sensor") {
		t.Error("ON-INV-003: specs/operator-nfr.md missing 'Two-part sensor' in ON-INV-003 Sensor description")
	}
	if !strings.Contains(content, "compile-time schema linter") {
		t.Error("ON-INV-003: specs/operator-nfr.md missing 'compile-time schema linter' in ON-INV-003 Sensor description (part a)")
	}
	if !strings.Contains(content, "regression test harness") {
		t.Error("ON-INV-003: specs/operator-nfr.md missing 'regression test harness' in ON-INV-003 Sensor description (part b)")
	}
}

// TestON027_StopCommandCoversCode11 verifies that the §8 taxonomy has an entry
// for code 11 (drain-timeout-escalated) that ON-027 step 7 depends on.
// The per-command assignment is verified independently in commandcodes_test.go.
//
// Spec ref: operator-nfr.md §4.7 ON-027 step 7 — drain-timeout-escalated per §8.
func TestON027_StopCommandCoversCode11(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeFixtureLookup(securityDrainFixtureExitCodeOnDrainTimeout)
	if !ok {
		t.Errorf("ON-027: §8 taxonomy missing code %d (drain-timeout-escalated); step 7 of ON-027 depends on this code", securityDrainFixtureExitCodeOnDrainTimeout)
		return
	}
	if e.Category != "drain-timeout-escalated" {
		t.Errorf("ON-027: §8 code %d category = %q, want %q", securityDrainFixtureExitCodeOnDrainTimeout, e.Category, "drain-timeout-escalated")
	}
}

// TestON027_DrainStepErrorExitCode21InTaxonomy verifies that the §8 taxonomy
// has code 21 (drain-step-errored) required by ON-027 per-step error paths.
//
// Spec ref: operator-nfr.md §8 — code 21 drain-step-errored.
func TestON027_DrainStepErrorExitCode21InTaxonomy(t *testing.T) {
	t.Parallel()

	e, ok := exitCodeFixtureLookup(securityDrainFixtureExitCodeOnDrainError)
	if !ok {
		t.Errorf("ON-027: §8 taxonomy missing code %d (drain-step-errored)", securityDrainFixtureExitCodeOnDrainError)
		return
	}
	if e.Category != "drain-step-errored" {
		t.Errorf("ON-027: §8 code %d category = %q, want %q", securityDrainFixtureExitCodeOnDrainError, e.Category, "drain-step-errored")
	}
}

// TestON027a_CrashRecoveryRequiresIdempotentSteps verifies that the spec
// documents idempotency and next-uncompleted-step resume for ON-027a.
//
// Spec ref: operator-nfr.md §4.7 ON-027a — "resumption MUST be idempotent on
// completed steps (each step's effect is replay-safe)."
func TestON027a_CrashRecoveryRequiresIdempotentSteps(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "idempotent") {
		t.Error("ON-027a: specs/operator-nfr.md missing 'idempotent' in ON-027a crash-recovery context")
	}
	if !strings.Contains(content, "next-uncompleted step") {
		t.Error("ON-027a: specs/operator-nfr.md missing 'next-uncompleted step' in ON-027a crash-recovery description")
	}
}

// securityDrainFixtureReadSpec reads specs/operator-nfr.md and returns its
// contents. Fatals on read error.
func securityDrainFixtureReadSpec(t *testing.T, root string) []byte {
	t.Helper()

	specPath := filepath.Join(root, "specs", "operator-nfr.md")
	//nolint:gosec // G304: specPath derived from runtime.Caller source path, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("securityDrainFixtureReadSpec: cannot read %s: %v", specPath, err)
	}
	return data
}
