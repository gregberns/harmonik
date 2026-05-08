package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

// eventExpectationFixturePresent returns a minimally valid EventExpectation
// with kind=event_present, a non-empty type, and a non-empty description.
func eventExpectationFixturePresent(t *testing.T) EventExpectation {
	t.Helper()
	return EventExpectation{
		Kind:        EventExpectationKindPresent,
		Type:        "node_started",
		Description: "node_started event must appear",
	}
}

// eventExpectationFixtureAbsent returns a minimally valid EventExpectation
// with kind=event_absent, a non-empty type, and a non-empty description.
func eventExpectationFixtureAbsent(t *testing.T) EventExpectation {
	t.Helper()
	return EventExpectation{
		Kind:        EventExpectationKindAbsent,
		Type:        "run_failed",
		Description: "run_failed must not appear on happy path",
	}
}

// eventExpectationFixtureWithPayloadMatch returns a valid EventExpectation
// with PayloadMatch populated with a simple flat predicate.
func eventExpectationFixtureWithPayloadMatch(t *testing.T) EventExpectation {
	t.Helper()
	return EventExpectation{
		Kind:        EventExpectationKindPresent,
		Type:        "node_completed",
		PayloadMatch: map[string]any{
			"outcome.status": "SUCCESS",
		},
		Description: "node_completed with outcome.status=SUCCESS",
	}
}

func TestEventExpectationKindValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		kind  EventExpectationKind
		valid bool
	}{
		{
			name:  "event_present is valid",
			kind:  EventExpectationKindPresent,
			valid: true,
		},
		{
			name:  "event_absent is valid",
			kind:  EventExpectationKindAbsent,
			valid: true,
		},
		{
			name:  "empty string is invalid",
			kind:  EventExpectationKind(""),
			valid: false,
		},
		{
			name:  "unknown value is invalid",
			kind:  EventExpectationKind("event_maybe"),
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Validate kind via an otherwise-valid EventExpectation.
			e := EventExpectation{
				Kind:        tc.kind,
				Type:        "node_started",
				Description: "test",
			}
			if got := e.Valid(); got != tc.valid {
				t.Errorf("EventExpectation{Kind:%q}.Valid() = %v, want %v", tc.kind, got, tc.valid)
			}
		})
	}
}

func TestEventExpectationKindMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind EventExpectationKind
		want string
	}{
		{EventExpectationKindPresent, `"event_present"`},
		{EventExpectationKindAbsent, `"event_absent"`},
	}

	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tc.kind)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("json.Marshal(%q) = %s, want %s", tc.kind, data, tc.want)
			}
			var got EventExpectationKind
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}
			if got != tc.kind {
				t.Errorf("round-trip kind mismatch: got %q, want %q", got, tc.kind)
			}
		})
	}
}

func TestEventExpectationValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input EventExpectation
		want  bool
	}{
		{
			name:  "valid: present kind, type, description",
			input: eventExpectationFixturePresent(t),
			want:  true,
		},
		{
			name:  "valid: absent kind, type, description",
			input: eventExpectationFixtureAbsent(t),
			want:  true,
		},
		{
			name:  "valid: present with payload_match",
			input: eventExpectationFixtureWithPayloadMatch(t),
			want:  true,
		},
		{
			name: "valid: nil payload_match is permitted",
			input: EventExpectation{
				Kind:         EventExpectationKindPresent,
				Type:         "node_started",
				PayloadMatch: nil,
				Description:  "nil payload_match ok",
			},
			want: true,
		},
		{
			name: "valid: empty payload_match map is permitted",
			input: EventExpectation{
				Kind:         EventExpectationKindPresent,
				Type:         "node_started",
				PayloadMatch: map[string]any{},
				Description:  "empty payload_match ok",
			},
			want: true,
		},
		{
			name: "invalid: missing type",
			input: EventExpectation{
				Kind:        EventExpectationKindPresent,
				Type:        "",
				Description: "missing type",
			},
			want: false,
		},
		{
			name: "invalid: missing description",
			input: EventExpectation{
				Kind:        EventExpectationKindPresent,
				Type:        "node_started",
				Description: "",
			},
			want: false,
		},
		{
			name: "invalid: unknown kind",
			input: EventExpectation{
				Kind:        EventExpectationKind("unknown_kind"),
				Type:        "node_started",
				Description: "bad kind",
			},
			want: false,
		},
		{
			name: "invalid: empty kind",
			input: EventExpectation{
				Kind:        EventExpectationKind(""),
				Type:        "node_started",
				Description: "empty kind",
			},
			want: false,
		},
		{
			name:  "invalid: zero value",
			input: EventExpectation{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("EventExpectation.Valid() = %v, want %v (input: %+v)", got, tc.want, tc.input)
			}
		})
	}
}

func TestEventExpectationJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input EventExpectation
	}{
		{
			name:  "present without payload_match",
			input: eventExpectationFixturePresent(t),
		},
		{
			name:  "absent without payload_match",
			input: eventExpectationFixtureAbsent(t),
		},
		{
			name:  "present with payload_match",
			input: eventExpectationFixtureWithPayloadMatch(t),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got EventExpectation
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestEventExpectationOmitEmptyPayloadMatch(t *testing.T) {
	t.Parallel()

	// When PayloadMatch is nil, the marshaled JSON MUST NOT contain the
	// "payload_match" key (omitempty contract on the struct tag).
	e := eventExpectationFixturePresent(t) // PayloadMatch is nil
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	if _, ok := raw["payload_match"]; ok {
		t.Errorf("marshaled JSON contains 'payload_match' key when PayloadMatch is nil; got %s", data)
	}
	if _, ok := raw["kind"]; !ok {
		t.Errorf("marshaled JSON missing 'kind' key; got %s", data)
	}
	if _, ok := raw["type"]; !ok {
		t.Errorf("marshaled JSON missing 'type' key; got %s", data)
	}
	if _, ok := raw["description"]; !ok {
		t.Errorf("marshaled JSON missing 'description' key; got %s", data)
	}
}

func TestEventExpectationPayloadMatchHeterogeneous(t *testing.T) {
	t.Parallel()

	// PayloadMatch must survive a JSON round-trip with mixed value types:
	// string, number (float64 via encoding/json), bool, nested map, null.
	e := EventExpectation{
		Kind: EventExpectationKindPresent,
		Type: "node_completed",
		PayloadMatch: map[string]any{
			"outcome.status":   "SUCCESS",
			"retry_count":      float64(3),
			"skipped":          false,
			"metadata.source":  "integration",
			"metadata.version": float64(2),
		},
		Description: "heterogeneous payload_match round-trip",
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var got EventExpectation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}

	if !reflect.DeepEqual(e, got) {
		t.Errorf("heterogeneous payload_match round-trip mismatch:\n  in:  %+v\n  out: %+v", e, got)
	}

	// Spot-check individual values preserved as expected JSON types.
	if got.PayloadMatch["outcome.status"] != "SUCCESS" {
		t.Errorf("payload_match[outcome.status] = %v, want %q", got.PayloadMatch["outcome.status"], "SUCCESS")
	}
	if got.PayloadMatch["retry_count"] != float64(3) {
		t.Errorf("payload_match[retry_count] = %v (%T), want float64(3)", got.PayloadMatch["retry_count"], got.PayloadMatch["retry_count"])
	}
	if got.PayloadMatch["skipped"] != false {
		t.Errorf("payload_match[skipped] = %v, want false", got.PayloadMatch["skipped"])
	}
}
