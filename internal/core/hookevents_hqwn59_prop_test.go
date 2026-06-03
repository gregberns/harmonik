package core

// hookevents_hqwn59_prop_test.go — property tests for the Valid() methods
// declared in hookevents_hqwn59.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// HookFiredPayload
// ============================================================

func TestProp_HookFiredPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFiredPayload{
			HookName:             HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID:    EventID(drawNonNilUUID(rt, "triggering_event_id")),
			SideEffectDescriptor: validSideEffect(),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed HookFiredPayload")
		}
	})
}

func TestProp_HookFiredPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := HookFiredPayload{
			RunID:                &nilID,
			HookName:             HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID:    EventID(drawNonNilUUID(rt, "triggering_event_id")),
			SideEffectDescriptor: validSideEffect(),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_HookFiredPayload_EmptyHookNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFiredPayload{
			HookName:             "",
			TriggeringEventID:    EventID(drawNonNilUUID(rt, "triggering_event_id")),
			SideEffectDescriptor: validSideEffect(),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty HookName")
		}
	})
}

func TestProp_HookFiredPayload_NilTriggeringEventIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFiredPayload{
			HookName:             HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID:    EventID(uuid.Nil),
			SideEffectDescriptor: validSideEffect(),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil TriggeringEventID")
		}
	})
}

func TestProp_HookFiredPayload_InvalidSideEffectRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Zero SideEffect (empty Kind, empty Target) is always invalid.
		p := HookFiredPayload{
			HookName:             HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID:    EventID(drawNonNilUUID(rt, "triggering_event_id")),
			SideEffectDescriptor: SideEffect{}, // zero value: Kind="", Target=""
		}
		if p.Valid() {
			rt.Error("Valid() should be false with zero-value SideEffectDescriptor")
		}
	})
}

// ============================================================
// HookFailedPayload
// ============================================================

func TestProp_HookFailedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFailedPayload{
			HookName:          HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID: EventID(drawNonNilUUID(rt, "triggering_event_id")),
			ErrorCategory:     rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:            drawNonEmptyString(rt, "reason"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed HookFailedPayload")
		}
	})
}

func TestProp_HookFailedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := HookFailedPayload{
			RunID:             &nilID,
			HookName:          HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID: EventID(drawNonNilUUID(rt, "triggering_event_id")),
			ErrorCategory:     rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:            drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_HookFailedPayload_EmptyHookNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFailedPayload{
			HookName:          "",
			TriggeringEventID: EventID(drawNonNilUUID(rt, "triggering_event_id")),
			ErrorCategory:     rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:            drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty HookName")
		}
	})
}

func TestProp_HookFailedPayload_NilTriggeringEventIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFailedPayload{
			HookName:          HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID: EventID(uuid.Nil),
			ErrorCategory:     rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:            drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil TriggeringEventID")
		}
	})
}

func TestProp_HookFailedPayload_InvalidErrorCategoryRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allErrorCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "err_cat_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := HookFailedPayload{
			HookName:          HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID: EventID(drawNonNilUUID(rt, "triggering_event_id")),
			ErrorCategory:     ErrorCategory(s),
			Reason:            drawNonEmptyString(rt, "reason"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown error category %q", s)
		}
	})
}

func TestProp_HookFailedPayload_EmptyReasonRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookFailedPayload{
			HookName:          HookName(drawNonEmptyString(rt, "hook_name")),
			TriggeringEventID: EventID(drawNonNilUUID(rt, "triggering_event_id")),
			ErrorCategory:     rapid.SampledFrom(allErrorCategories).Draw(rt, "error_category"),
			Reason:            "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty Reason")
		}
	})
}

// ============================================================
// HookVerdictPersistedPayload
// ============================================================

func TestProp_HookVerdictPersistedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			HookInvocationID: drawNonEmptyString(rt, "invocation_id"),
			HookName:         HookName(drawNonEmptyString(rt, "hook_name")),
			VerdictPath:      drawNonEmptyString(rt, "verdict_path"),
			CommitHash:       drawNonEmptyString(rt, "commit_hash"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed HookVerdictPersistedPayload")
		}
	})
}

func TestProp_HookVerdictPersistedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(uuid.Nil),
			HookInvocationID: drawNonEmptyString(rt, "invocation_id"),
			HookName:         HookName(drawNonEmptyString(rt, "hook_name")),
			VerdictPath:      drawNonEmptyString(rt, "verdict_path"),
			CommitHash:       drawNonEmptyString(rt, "commit_hash"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil RunID")
		}
	})
}

func TestProp_HookVerdictPersistedPayload_EmptyHookInvocationIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			HookInvocationID: "",
			HookName:         HookName(drawNonEmptyString(rt, "hook_name")),
			VerdictPath:      drawNonEmptyString(rt, "verdict_path"),
			CommitHash:       drawNonEmptyString(rt, "commit_hash"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty HookInvocationID")
		}
	})
}

func TestProp_HookVerdictPersistedPayload_EmptyHookNameRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			HookInvocationID: drawNonEmptyString(rt, "invocation_id"),
			HookName:         "",
			VerdictPath:      drawNonEmptyString(rt, "verdict_path"),
			CommitHash:       drawNonEmptyString(rt, "commit_hash"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty HookName")
		}
	})
}

func TestProp_HookVerdictPersistedPayload_EmptyVerdictPathRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			HookInvocationID: drawNonEmptyString(rt, "invocation_id"),
			HookName:         HookName(drawNonEmptyString(rt, "hook_name")),
			VerdictPath:      "",
			CommitHash:       drawNonEmptyString(rt, "commit_hash"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty VerdictPath")
		}
	})
}

func TestProp_HookVerdictPersistedPayload_EmptyCommitHashRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := HookVerdictPersistedPayload{
			RunID:            RunID(drawNonNilUUID(rt, "run_id")),
			HookInvocationID: drawNonEmptyString(rt, "invocation_id"),
			HookName:         HookName(drawNonEmptyString(rt, "hook_name")),
			VerdictPath:      drawNonEmptyString(rt, "verdict_path"),
			CommitHash:       "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty CommitHash")
		}
	})
}
