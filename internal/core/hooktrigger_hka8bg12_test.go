package core

// hooktrigger_hka8bg12_test.go — HookTriggerSet and CP-013 baseline trigger tests.
//
// Covers specs/control-points.md §4.3.CP-013:
//
//   - NewBaselineHookTriggerSet returns a set containing all 8 MVH-baseline triggers.
//   - Contains returns true for declared triggers, false for unrecognized ones.
//   - AddTrigger registers additional triggers; idempotent on re-add.
//   - AddTrigger rejects empty trigger names.
//   - S02Registrar.AddHookTrigger wires through to the trigger set.
//   - constructHook rejects a Hook whose TriggerEvent is not in the declared set.
//   - constructHook accepts a Hook whose TriggerEvent is in the baseline set.
//   - S02Registrar.RegisterFromDocument rejects a Hook with an unrecognized trigger.
//   - S02Registrar.RegisterFromDocument accepts a Hook with a subsystem-added trigger.
//
// Refs: hk-a8bg.12

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// NewBaselineHookTriggerSet
// ---------------------------------------------------------------------------

// TestBaselineHookTriggerSet_ContainsAllEight verifies that NewBaselineHookTriggerSet
// returns a set containing all 8 MVH-baseline trigger names declared in CP-013.
func TestBaselineHookTriggerSet_ContainsAllEight(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()

	baseline := []string{
		"on_agent_started",
		"on_agent_output",
		"on_agent_completed",
		"on_timeout",
		"on_review_required",
		"on_transition_attempted",
		"on_checkpoint_written",
		"on_checkpoint_failed",
	}

	for _, trigger := range baseline {
		if !ts.Contains(trigger) {
			t.Errorf("NewBaselineHookTriggerSet().Contains(%q) = false, want true", trigger)
		}
	}
}

// TestBaselineHookTriggerSet_AllReturnsEightEntries verifies that All() returns
// exactly 8 entries after construction from NewBaselineHookTriggerSet.
func TestBaselineHookTriggerSet_AllReturnsEightEntries(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	all := ts.All()
	if len(all) != 8 {
		t.Errorf("NewBaselineHookTriggerSet().All() len = %d, want 8", len(all))
	}
}

// TestBaselineHookTriggerSet_AllReturnsSorted verifies that All() returns
// trigger names in ascending order.
func TestBaselineHookTriggerSet_AllReturnsSorted(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	all := ts.All()
	for i := 1; i < len(all); i++ {
		if all[i] <= all[i-1] {
			t.Errorf("All() not sorted: all[%d]=%q <= all[%d]=%q", i, all[i], i-1, all[i-1])
		}
	}
}

// ---------------------------------------------------------------------------
// Contains
// ---------------------------------------------------------------------------

// TestHookTriggerSet_ContainsUnrecognized verifies that Contains returns false
// for a trigger name not in the set.
func TestHookTriggerSet_ContainsUnrecognized(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	if ts.Contains("on_not_a_real_trigger") {
		t.Error("Contains(\"on_not_a_real_trigger\") = true, want false")
	}
}

// TestHookTriggerSet_ContainsEmpty verifies that Contains returns false for
// the empty string (not a valid trigger name).
func TestHookTriggerSet_ContainsEmpty(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	if ts.Contains("") {
		t.Error("Contains(\"\") = true, want false")
	}
}

// ---------------------------------------------------------------------------
// AddTrigger
// ---------------------------------------------------------------------------

// TestHookTriggerSet_AddTrigger_ExtendsBeyondBaseline verifies that AddTrigger
// makes a new trigger name recognizable via Contains.
func TestHookTriggerSet_AddTrigger_ExtendsBeyondBaseline(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	trigger := "on_subsystem_custom_event"

	if ts.Contains(trigger) {
		t.Fatalf("pre-condition: %q should not be in baseline set", trigger)
	}

	if err := ts.AddTrigger(trigger); err != nil {
		t.Fatalf("AddTrigger(%q): unexpected error: %v", trigger, err)
	}

	if !ts.Contains(trigger) {
		t.Errorf("after AddTrigger(%q), Contains = false, want true", trigger)
	}
}

// TestHookTriggerSet_AddTrigger_Idempotent verifies that adding a trigger that
// is already declared is a no-op (no error, set size unchanged).
func TestHookTriggerSet_AddTrigger_Idempotent(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	trigger := "on_agent_started" // already in baseline

	before := len(ts.All())
	if err := ts.AddTrigger(trigger); err != nil {
		t.Fatalf("AddTrigger(%q) on existing trigger: unexpected error: %v", trigger, err)
	}
	after := len(ts.All())

	if after != before {
		t.Errorf("AddTrigger on existing trigger changed set size: before=%d after=%d", before, after)
	}
}

// TestHookTriggerSet_AddTrigger_EmptyNameErrors verifies that AddTrigger
// returns an error when the trigger name is empty.
func TestHookTriggerSet_AddTrigger_EmptyNameErrors(t *testing.T) {
	t.Parallel()

	ts := NewBaselineHookTriggerSet()
	if err := ts.AddTrigger(""); err == nil {
		t.Error("AddTrigger(\"\") = nil, want error")
	}
}

// ---------------------------------------------------------------------------
// S02Registrar.AddHookTrigger
// ---------------------------------------------------------------------------

// TestS02Registrar_AddHookTrigger_ExtendsTriggerSet verifies that
// AddHookTrigger on an S02Registrar makes a non-baseline trigger accepted
// during RegisterFromDocument.
func TestS02Registrar_AddHookTrigger_ExtendsTriggerSet(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	if err := s.AddHookTrigger("on_custom_subsystem_event"); err != nil {
		t.Fatalf("AddHookTrigger: %v", err)
	}

	doc := PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Hooks: []PolicyHook{
			{
				Name:           "custom-hook",
				TriggerEvent:   "on_custom_subsystem_event",
				SideEffectKind: "emit-event",
				Evaluator:      PolicyEvaluatorBlock{Mode: "mechanism", Expression: `true`},
			},
		},
	}

	if err := s.RegisterFromDocument(doc); err != nil {
		t.Errorf("RegisterFromDocument with subsystem-added trigger: unexpected error: %v", err)
	}
}

// TestS02Registrar_AddHookTrigger_EmptyNameErrors verifies that AddHookTrigger
// surfaces the error from the underlying trigger set when name is empty.
func TestS02Registrar_AddHookTrigger_EmptyNameErrors(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	if err := s.AddHookTrigger(""); err == nil {
		t.Error("AddHookTrigger(\"\") = nil, want error")
	}
}

// ---------------------------------------------------------------------------
// CP-013: constructHook trigger validation via S02Registrar.RegisterFromDocument
// ---------------------------------------------------------------------------

// TestCP013_UnrecognizedTriggerFailsRegistration verifies that RegisterFromDocument
// returns an error wrapping ErrConstructControlPoint when a Hook's TriggerEvent
// is not in the declared lifecycle set (CP-013).
func TestCP013_UnrecognizedTriggerFailsRegistration(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Hooks: []PolicyHook{
			{
				Name:           "bad-hook",
				TriggerEvent:   "on_not_a_declared_trigger",
				SideEffectKind: "emit-event",
				Evaluator:      PolicyEvaluatorBlock{Mode: "mechanism", Expression: `true`},
			},
		},
	}

	err := s.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("RegisterFromDocument with unrecognized trigger: expected error, got nil")
	}
	if !errors.Is(err, ErrConstructControlPoint) {
		t.Errorf("RegisterFromDocument with unrecognized trigger: got %v, want error wrapping ErrConstructControlPoint", err)
	}
}

// TestCP013_BaselineTriggerAccepted verifies that each of the 8 MVH-baseline
// trigger names is accepted by RegisterFromDocument without error.
func TestCP013_BaselineTriggerAccepted(t *testing.T) {
	t.Parallel()

	baseline := []string{
		"on_agent_started",
		"on_agent_output",
		"on_agent_completed",
		"on_timeout",
		"on_review_required",
		"on_transition_attempted",
		"on_checkpoint_written",
		"on_checkpoint_failed",
	}

	for i, trigger := range baseline {
		trigger := trigger
		i := i
		t.Run(trigger, func(t *testing.T) {
			t.Parallel()

			s := NewS02Registrar()
			doc := PolicyDocument{
				Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
				Hooks: []PolicyHook{
					{
						// unique name per sub-test to avoid CP-044 divergent-body issues
						Name:           "baseline-hook-" + string(rune('a'+i)),
						TriggerEvent:   trigger,
						SideEffectKind: "emit-event",
						Evaluator:      PolicyEvaluatorBlock{Mode: "mechanism", Expression: `true`},
					},
				},
			}

			if err := s.RegisterFromDocument(doc); err != nil {
				t.Errorf("RegisterFromDocument(trigger=%q): unexpected error: %v", trigger, err)
			}
		})
	}
}

// TestCP013_EmptyTriggerFailsRegistration verifies that a Hook with an empty
// TriggerEvent is rejected (empty string is not a declared trigger per CP-013,
// and HookPayload.Valid also requires TriggerEvent to be non-empty).
func TestCP013_EmptyTriggerFailsRegistration(t *testing.T) {
	t.Parallel()

	s := NewS02Registrar()
	doc := PolicyDocument{
		Metadata: PolicyDocumentMeta{Name: "p", Version: "1", Author: "a", SchemaVersion: 1},
		Hooks: []PolicyHook{
			{
				Name:           "empty-trigger-hook",
				TriggerEvent:   "",
				SideEffectKind: "emit-event",
				Evaluator:      PolicyEvaluatorBlock{Mode: "mechanism", Expression: `true`},
			},
		},
	}

	err := s.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("RegisterFromDocument with empty TriggerEvent: expected error, got nil")
	}
}
