// Package core — named requirement-traceable sensors for the event-envelope field set.
//
// This file provides reflect-based sensors that assert the [Event] struct carries every
// field mandated by event-model.md §4.1 EV-001. The sensors are intentionally structural:
// they will FAIL if a future contributor renames, removes, or changes the type of any
// EV-001 envelope field on [Event].
//
// Spec reference: event-model.md §4.1 EV-001 — "Every event MUST carry the common
// envelope fields".
package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// ev001Fields maps each EV-001 wire-form name to its expected Go-identifier and
// expected reflect.Type.  Optional/scoped fields (run_id, state_id, trace_context,
// timestamp_mono_nsec) are pointer types in the struct because EV-001 says "when
// scoped"; the sensor records this with a pointer type.
//
// Spec reference: event-model.md §4.1 EV-001.
var ev001Fields = []struct {
	specName string       // wire / spec name (snake_case, per EV-001)
	goName   string       // Go struct field identifier
	wantType reflect.Type // expected reflect.Type of the field
}{
	{
		specName: "event_id",
		goName:   "EventID",
		wantType: reflect.TypeOf(EventID{}),
	},
	{
		specName: "schema_version",
		goName:   "SchemaVersion",
		wantType: reflect.TypeOf(int(0)),
	},
	{
		specName: "type",
		goName:   "Type",
		// EventType enum (hk-hqwn.59) is not yet defined; the field uses string
		// until that enum lands — non-breaking hoist per event.go comment.
		wantType: reflect.TypeOf(""),
	},
	{
		specName: "timestamp_wall",
		goName:   "TimestampWall",
		wantType: reflect.TypeOf(time.Time{}),
	},
	{
		specName: "timestamp_mono_nsec",
		goName:   "TimestampMonoNsec",
		// Optional per EV-001 / EV-003: present as *int64 (nil when absent).
		wantType: reflect.TypeOf((*int64)(nil)),
	},
	{
		specName: "source_subsystem",
		goName:   "SourceSubsystem",
		wantType: reflect.TypeOf(""),
	},
	{
		specName: "trace_context",
		goName:   "TraceContext",
		// EV-001 "for cross-subsystem correlation"; optional, pointer to avoid
		// zero-value ambiguity.
		wantType: reflect.TypeOf((*TraceContext)(nil)),
	},
	{
		specName: "run_id",
		goName:   "RunID",
		// EV-001 "when scoped to a run"; optional, pointer.
		wantType: reflect.TypeOf((*RunID)(nil)),
	},
	{
		specName: "state_id",
		goName:   "StateID",
		// EV-001 "when scoped to a state"; optional, pointer.
		wantType: reflect.TypeOf((*StateID)(nil)),
	},
	{
		specName: "payload",
		goName:   "Payload",
		// json.RawMessage is []byte; required (non-nil) per Event.Valid().
		wantType: reflect.TypeOf(json.RawMessage(nil)),
	},
}

// TestEventEV001_EnvelopeFieldsPresent is a reflect-based sensor that asserts the
// [Event] struct carries each of the ten EV-001 envelope fields with the correct Go
// identifier name and exact reflect.Type.
//
// The test iterates ev001Fields and for each entry:
//  1. looks up the named field on Event via reflect — FAIL if absent (field removed or renamed).
//  2. asserts the field's reflect.Type matches wantType — FAIL if type changed.
//
// Spec reference: event-model.md §4.1 EV-001.
func TestEventEV001_EnvelopeFieldsPresent(t *testing.T) {
	t.Parallel()

	eventType := reflect.TypeOf(Event{})

	for _, tc := range ev001Fields {
		t.Run(tc.goName, func(t *testing.T) {
			t.Parallel()

			sf, ok := eventType.FieldByName(tc.goName)
			if !ok {
				t.Errorf(
					"EV-001: Event struct is missing field %q (spec name %q); "+
						"this field is required by event-model.md §4.1 EV-001",
					tc.goName, tc.specName,
				)
				return
			}

			if sf.Type != tc.wantType {
				t.Errorf(
					"EV-001: Event.%s (spec %q) has type %v, want %v; "+
						"event-model.md §4.1 EV-001 requires this envelope field",
					tc.goName, tc.specName, sf.Type, tc.wantType,
				)
			}
		})
	}
}

// TestEventEV001_FieldNamesMatchSpec verifies that each EV-001 spec wire-form name
// maps to an identically-named (CamelCase) field on [Event].  The table below is the
// authoritative spec-name → Go-identifier mapping; any discrepancy between the table
// and the [Event] declaration is a conformance failure.
//
// Spec reference: event-model.md §4.1 EV-001.
func TestEventEV001_FieldNamesMatchSpec(t *testing.T) {
	t.Parallel()

	eventType := reflect.TypeOf(Event{})

	for _, tc := range ev001Fields {
		t.Run(tc.specName, func(t *testing.T) {
			t.Parallel()

			if _, ok := eventType.FieldByName(tc.goName); !ok {
				t.Errorf(
					"EV-001 spec field %q maps to Go identifier %q but that field "+
						"does not exist on Event; event-model.md §4.1 EV-001 requires it",
					tc.specName, tc.goName,
				)
			}
		})
	}
}
