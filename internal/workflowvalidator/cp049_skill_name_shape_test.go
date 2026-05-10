package workflowvalidator

// cp049_skill_name_shape_test.go — CP-049 ingest-time syntactic validity tests.
//
// Per CP-049: every declared required_skill name MUST match the skill-name
// shape (lowercase-hyphenated identifier, optionally @<version>); syntactic
// violations fail workflow-ingest with ErrDeterministic.
//
// Spec ref: specs/control-points.md §4.11.CP-049.
// Bead: hk-a8bg.51.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// cp049FixtureNodeWithSkill returns a valid agentic DOT string where the
// given node has the specified required_skills attribute value.
func cp049FixtureNodeWithSkill(skillValue string) string {
	return `digraph workflow {
    graph [
        workflow_id       = "018f1e2a-0000-7000-8000-000000000001"
        name              = "cp049-fixture"
        version           = "0.1.0"
        start_node_id     = "agent_node"
        terminal_node_ids = "done_node"
    ]

    agent_node [
        type               = "agentic"
        handler_ref        = "handlers/my-handler"
        idempotency_class  = "non-idempotent"
        "llm-freedom"      = "unbounded"
        "io-determinism"   = "nondeterministic"
        "replay-safety"    = "unsafe"
        idempotency        = "non-idempotent"
        mode               = "cognition"
        required_skills    = "` + skillValue + `"
    ]

    done_node [
        type               = "non-agentic"
        idempotency_class  = "idempotent"
        "llm-freedom"      = "none"
        "io-determinism"   = "deterministic"
        "replay-safety"    = "safe"
        idempotency        = "idempotent"
        mode               = "mechanism"
    ]

    agent_node -> done_node [ordering_key = "a"]
}`
}

// cp049FixtureRegistryWithSkill returns a mapRegistry pre-populated with the
// handler_ref required by the agentic DOT fixture and the given skill.
func cp049FixtureRegistryWithSkill(skill string) *mapRegistry {
	reg := newMapRegistry()
	reg.handlers["handlers/my-handler"] = true
	reg.skills[skill] = true
	return reg
}

// TestCP049_SkillNameShape_ValidName_PassesWithRegisteredSkill verifies that a
// syntactically-valid skill name that is registered passes validation.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestCP049_SkillNameShape_ValidName_PassesWithRegisteredSkill(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithSkill("beads-cli")
	v := preRunValidatorFixtureWithRegistry(t, reg)

	err := v.Validate(cp049FixtureNodeWithSkill("beads-cli"))
	if err != nil {
		t.Errorf("CP-049: valid skill name with registered skill: got err %v, want nil", err)
	}
}

// TestCP049_SkillNameShape_ValidNameWithVersion_Passes verifies that a
// syntactically-valid skill name with @<version> suffix passes shape check.
//
// Spec ref: control-points.md §4.11.CP-049 — "optionally suffixed with @<version>."
func TestCP049_SkillNameShape_ValidNameWithVersion_Passes(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithSkill("beads-cli@1.0.0")
	v := preRunValidatorFixtureWithRegistry(t, reg)

	err := v.Validate(cp049FixtureNodeWithSkill("beads-cli@1.0.0"))
	if err != nil {
		t.Errorf("CP-049: valid skill name with @version, registered: got err %v, want nil", err)
	}
}

// cp049FixtureRegistryWithHandler returns a mapRegistry pre-populated only
// with the handler_ref required by the agentic DOT fixture (no skills).
func cp049FixtureRegistryWithHandler() *mapRegistry {
	reg := newMapRegistry()
	reg.handlers["handlers/my-handler"] = true
	return reg
}

// TestCP049_SkillNameShape_BadShapeUppercase_FailsWithBadShapeCode verifies
// that a skill name with uppercase letters is rejected with codeSkillNameBadShape.
//
// Spec ref: control-points.md §4.11.CP-049 — "syntactic violations fail
// workflow-ingest."
func TestCP049_SkillNameShape_BadShapeUppercase_FailsWithBadShapeCode(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithHandler()
	v := preRunValidatorFixtureWithRegistry(t, reg)

	err := v.Validate(cp049FixtureNodeWithSkill("BadSkill"))
	if err == nil {
		t.Fatal("CP-049: uppercase skill name: expected non-nil error, got nil")
	}
	if !hasCode(err, codeSkillNameBadShape) {
		t.Errorf("CP-049: uppercase skill name: error does not carry %q code; got %v", codeSkillNameBadShape, err)
	}
}

// TestCP049_SkillNameShape_StartsWithDigit_FailsWithBadShapeCode verifies
// that a skill name starting with a digit is rejected.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestCP049_SkillNameShape_StartsWithDigit_FailsWithBadShapeCode(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithHandler()
	v := preRunValidatorFixtureWithRegistry(t, reg)

	err := v.Validate(cp049FixtureNodeWithSkill("1invalid"))
	if err == nil {
		t.Fatal("CP-049: digit-starting skill name: expected non-nil error, got nil")
	}
	if !hasCode(err, codeSkillNameBadShape) {
		t.Errorf("CP-049: digit-starting skill name: error code want %q, got %v", codeSkillNameBadShape, err)
	}
}

// TestCP049_SkillNameShape_HasSpace_FailsWithBadShapeCode verifies that a
// skill name with a space is rejected at the skill level.
// Note: "has space" parses as two tokens; "has" and "space" are both valid-shape
// but unregistered, producing codeSkillUnresolved for each.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestCP049_SkillNameShape_HasSpace_ProducesError(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithHandler()
	v := preRunValidatorFixtureWithRegistry(t, reg)

	// "has space" parses as space-separated: ["has", "space"] — both valid shape
	// but not registered → codeSkillUnresolved.
	err := v.Validate(cp049FixtureNodeWithSkill("has space"))
	if err == nil {
		t.Error("CP-049: space-in-name (parsed as two tokens): expected error, got nil")
	}
}

// TestCP049_SkillNameShape_BadShapeNoRegistryLookup verifies that a
// syntactically-invalid skill name (uppercase) does NOT trigger a registry
// lookup — the shape check stops processing for that name.
//
// Spec ref: control-points.md §4.11.CP-049 — shape check precedes registry
// resolution.
func TestCP049_SkillNameShape_BadShapeNoRegistryLookup(t *testing.T) {
	t.Parallel()

	// Use a counting registry to verify HasSkill was NOT called for bad-shape names.
	cr := &countingRegistry{}
	cr.skills = map[string]bool{"BadSkill": true} // registered but should not be looked up
	v := New(nil, cr)

	err := v.Validate(cp049FixtureNodeWithSkill("BadSkill"))
	if err == nil {
		t.Fatal("CP-049: bad-shape name: expected error, got nil")
	}
	if cr.skillLookups > 0 {
		t.Errorf("CP-049: bad-shape name must not trigger registry lookup; got %d lookups", cr.skillLookups)
	}
}

// TestCP049_SkillNameShape_ValidShapeNotRegistered_FailsWithUnresolved verifies
// that a syntactically-valid but unregistered skill name produces
// codeSkillUnresolved (NOT codeSkillNameBadShape).
//
// Spec ref: control-points.md §4.11.CP-049 — covering partition: syntactic
// (this spec) vs. launch-time resolution (handler-contract).
func TestCP049_SkillNameShape_ValidShapeNotRegistered_FailsWithUnresolved(t *testing.T) {
	t.Parallel()

	reg := cp049FixtureRegistryWithHandler() // handler registered; skill not registered
	v := preRunValidatorFixtureWithRegistry(t, reg)

	err := v.Validate(cp049FixtureNodeWithSkill("valid-skill"))
	if err == nil {
		t.Fatal("CP-049: valid shape, not registered: expected non-nil error, got nil")
	}
	if !hasCode(err, codeSkillUnresolved) {
		t.Errorf("CP-049: valid shape, not registered: want code %q, got %v", codeSkillUnresolved, err)
	}
	if hasCode(err, codeSkillNameBadShape) {
		t.Errorf("CP-049: valid shape should not produce %q error", codeSkillNameBadShape)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// countingRegistry — ReferenceRegistry that counts HasSkill lookups
// ─────────────────────────────────────────────────────────────────────────────

type countingRegistry struct {
	skills       map[string]bool
	skillLookups int
}

func (r *countingRegistry) HasHandler(_ string) bool                        { return false }
func (r *countingRegistry) HasPolicy(_ core.PolicyRef) bool                 { return false }
func (r *countingRegistry) HasGate(_ core.GateRef) bool                     { return false }
func (r *countingRegistry) HasFreedomProfile(_ core.FreedomProfileRef) bool { return false }
func (r *countingRegistry) HasBudget(_ core.BudgetRef) bool                 { return false }
func (r *countingRegistry) HasSkill(name string) bool {
	r.skillLookups++
	return r.skills[name]
}
