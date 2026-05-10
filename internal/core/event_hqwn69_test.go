// Package core — JSON struct-tag conformance sensor for core.Event (hk-hqwn.69).
//
// event-model.md §6.2 mandates snake_case wire keys for the event envelope:
// event_id, schema_version, type, timestamp_wall, timestamp_mono_nsec, run_id,
// state_id, source_subsystem, trace_context, payload. Without explicit
// json:"..." struct tags, json.Marshal produces PascalCase keys (EventID,
// SchemaVersion, ...) which violate §6.2.
//
// This file adds explicit json struct tags to core.Event and core.TraceContext
// and verifies via reflect that each field carries the correct tag so that
// direct json.Marshal(event) is spec-conformant.
//
// Requirement-traceable bead: hk-hqwn.69.
package core

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// jsonTagFixtureFieldCase describes one Event struct field's expected JSON tag.
type jsonTagFixtureFieldCase struct {
	goName      string // Go field identifier
	wantJSONKey string // expected json key (the part before any comma)
	omitEmpty   bool   // whether ,omitempty is expected
}

// hqwn69EventJSONTagCases lists every field of core.Event with its expected
// json tag per event-model.md §6.2.
var hqwn69EventJSONTagCases = []jsonTagFixtureFieldCase{
	{goName: "EventID", wantJSONKey: "event_id", omitEmpty: false},
	{goName: "SchemaVersion", wantJSONKey: "schema_version", omitEmpty: false},
	{goName: "Type", wantJSONKey: "type", omitEmpty: false},
	{goName: "TimestampWall", wantJSONKey: "timestamp_wall", omitEmpty: false},
	{goName: "TimestampMonoNsec", wantJSONKey: "timestamp_mono_nsec", omitEmpty: true},
	{goName: "RunID", wantJSONKey: "run_id", omitEmpty: true},
	{goName: "StateID", wantJSONKey: "state_id", omitEmpty: true},
	{goName: "SourceSubsystem", wantJSONKey: "source_subsystem", omitEmpty: false},
	{goName: "TraceContext", wantJSONKey: "trace_context", omitEmpty: true},
	{goName: "Payload", wantJSONKey: "payload", omitEmpty: false},
}

// hqwn69TraceContextJSONTagCases lists every field of core.TraceContext with
// its expected json tag per event-model.md §6.1.
var hqwn69TraceContextJSONTagCases = []jsonTagFixtureFieldCase{
	{goName: "TraceID", wantJSONKey: "trace_id", omitEmpty: true},
	{goName: "ParentEventID", wantJSONKey: "parent_event_id", omitEmpty: true},
	{goName: "RootEventID", wantJSONKey: "root_event_id", omitEmpty: true},
}

// hqwn69CheckJSONTags is a shared helper that verifies JSON struct tags on the
// given reflect.Type against the provided field-case table.
func hqwn69CheckJSONTags(t *testing.T, structType reflect.Type, cases []jsonTagFixtureFieldCase) {
	t.Helper()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.goName, func(t *testing.T) {
			t.Parallel()

			sf, ok := structType.FieldByName(tc.goName)
			if !ok {
				t.Errorf("hk-hqwn.69: struct %s is missing field %q; json tag cannot be verified",
					structType.Name(), tc.goName)
				return
			}

			tag := sf.Tag.Get("json")
			if tag == "" {
				t.Errorf("hk-hqwn.69: %s.%s has no json struct tag; "+
					"event-model.md §6.2 requires snake_case wire key %q",
					structType.Name(), tc.goName, tc.wantJSONKey)
				return
			}

			// Split tag into key and options (e.g., "event_id,omitempty").
			parts := strings.SplitN(tag, ",", 2)
			key := parts[0]
			opts := ""
			if len(parts) == 2 {
				opts = parts[1]
			}

			if key != tc.wantJSONKey {
				t.Errorf("hk-hqwn.69: %s.%s json key = %q, want %q (§6.2 snake_case wire key)",
					structType.Name(), tc.goName, key, tc.wantJSONKey)
			}

			hasOmitEmpty := strings.Contains(opts, "omitempty")
			if tc.omitEmpty && !hasOmitEmpty {
				t.Errorf("hk-hqwn.69: %s.%s json tag is missing ,omitempty; "+
					"optional field MUST use omitempty so absent value is omitted from wire form",
					structType.Name(), tc.goName)
			}
			if !tc.omitEmpty && hasOmitEmpty {
				t.Errorf("hk-hqwn.69: %s.%s json tag has unexpected ,omitempty; "+
					"required field MUST NOT use omitempty (zero value must appear on wire)",
					structType.Name(), tc.goName)
			}
		})
	}
}

// TestHqwn69_EventJSONTags verifies that every field of core.Event carries an
// explicit json struct tag with the snake_case wire key mandated by
// event-model.md §6.2.
//
// Without tags, json.Marshal produces PascalCase keys (EventID, SchemaVersion,
// ...) that violate §6.2 and would break any consumer reading JSONL written
// directly from core.Event.
//
// Spec ref: event-model.md §6.2 — on-disk JSONL format wire keys.
// Bead ref: hk-hqwn.69.
func TestHqwn69_EventJSONTags(t *testing.T) {
	t.Parallel()
	hqwn69CheckJSONTags(t, reflect.TypeOf(Event{}), hqwn69EventJSONTagCases)
}

// TestHqwn69_TraceContextJSONTags verifies that every field of core.TraceContext
// carries an explicit json struct tag with the snake_case wire key mandated by
// event-model.md §6.1 (trace_context sub-object).
//
// Spec ref: event-model.md §6.1 RECORD TraceContext; §6.2 wire form.
// Bead ref: hk-hqwn.69.
func TestHqwn69_TraceContextJSONTags(t *testing.T) {
	t.Parallel()
	hqwn69CheckJSONTags(t, reflect.TypeOf(TraceContext{}), hqwn69TraceContextJSONTagCases)
}

// TestHqwn69_EventMarshalProducesSnakeCaseKeys is an integration-level sensor
// that marshals a minimal Event to JSON and asserts the output contains the
// required snake_case wire keys. This catches any custom MarshalJSON that might
// override the struct tags.
//
// Spec ref: event-model.md §6.2 — "event_id", "schema_version", "type",
// "timestamp_wall", "source_subsystem", "payload" are required wire keys.
// Bead ref: hk-hqwn.69.
func TestHqwn69_EventMarshalProducesSnakeCaseKeys(t *testing.T) {
	t.Parallel()

	eventID := EventID(uuid.Must(uuid.NewV7()))
	e := Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            "run_started",
		TimestampWall:   time.Date(2026, 4, 24, 14, 22, 11, 0, time.UTC),
		SourceSubsystem: "github.com/gregberns/harmonik/internal/orchestrator",
		Payload:         json.RawMessage(`{}`),
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("hk-hqwn.69: json.Marshal(Event) failed: %v", err)
	}
	wire := string(b)

	requiredKeys := []string{
		`"event_id"`,
		`"schema_version"`,
		`"type"`,
		`"timestamp_wall"`,
		`"source_subsystem"`,
		`"payload"`,
	}
	for _, key := range requiredKeys {
		if !strings.Contains(wire, key) {
			t.Errorf("hk-hqwn.69: json.Marshal(Event) output missing wire key %s; "+
				"event-model.md §6.2 requires snake_case keys.\nFull wire: %s", key, wire)
		}
	}

	// Verify PascalCase keys are NOT present (the old tag-free form would produce these).
	forbiddenKeys := []string{
		`"EventID"`,
		`"SchemaVersion"`,
		`"TimestampWall"`,
		`"SourceSubsystem"`,
	}
	for _, key := range forbiddenKeys {
		if strings.Contains(wire, key) {
			t.Errorf("hk-hqwn.69: json.Marshal(Event) output contains PascalCase key %s; "+
				"json struct tags must produce snake_case keys per §6.2.\nFull wire: %s", key, wire)
		}
	}
}

// TestHqwn69_EventOmitEmptyFieldsAbsentWhenNil verifies that optional fields
// (run_id, state_id, timestamp_mono_nsec, trace_context) are absent from the
// marshaled JSON when nil. This confirms the ,omitempty tags work correctly
// and do not pollute the wire form with null values for absent optionals.
//
// Spec ref: event-model.md §6.2 — optional fields are omitted when absent.
// Bead ref: hk-hqwn.69.
func TestHqwn69_EventOmitEmptyFieldsAbsentWhenNil(t *testing.T) {
	t.Parallel()

	e := Event{
		EventID:         EventID(uuid.Must(uuid.NewV7())),
		SchemaVersion:   1,
		Type:            "run_started",
		TimestampWall:   time.Date(2026, 4, 24, 14, 22, 11, 0, time.UTC),
		SourceSubsystem: "github.com/gregberns/harmonik/internal/orchestrator",
		Payload:         json.RawMessage(`{}`),
		// All optional fields left nil.
	}

	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("hk-hqwn.69: json.Marshal(Event) with nil optionals failed: %v", err)
	}
	wire := string(b)

	absentKeys := []string{
		`"run_id"`,
		`"state_id"`,
		`"timestamp_mono_nsec"`,
		`"trace_context"`,
	}
	for _, key := range absentKeys {
		if strings.Contains(wire, key) {
			t.Errorf("hk-hqwn.69: optional field %s present in wire output when nil; "+
				",omitempty must suppress absent optional fields per §6.2.\nFull wire: %s", key, wire)
		}
	}
}
