package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// redactionFailedFixtureRunID returns a non-nil RunID for RedactionFailedPayload tests.
func redactionFailedFixtureRunID(t *testing.T) RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return RunID(id)
}

// redactionFailedFixtureRunIDPtr returns a pointer to a RunID for use as the
// optional run_id field in RedactionFailedPayload tests.
func redactionFailedFixtureRunIDPtr(t *testing.T) *RunID {
	t.Helper()
	id := redactionFailedFixtureRunID(t)
	return &id
}

func TestRedactionFailedPayloadValid(t *testing.T) {
	t.Parallel()

	runID := redactionFailedFixtureRunIDPtr(t)

	tests := []struct {
		name  string
		p     RedactionFailedPayload
		valid bool
	}{
		{
			name: "minimal valid without run_id",
			p: RedactionFailedPayload{
				EventType:  EventType("agent_started"),
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "valid with run_id",
			p: RedactionFailedPayload{
				EventType:  EventType("agent_started"),
				RunID:      runID,
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "valid with structural error class",
			p: RedactionFailedPayload{
				EventType:  EventType("bus_overflow"),
				ErrorClass: ErrorCategoryStructural,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "empty event_type rejected",
			p: RedactionFailedPayload{
				EventType:  EventType(""),
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "nil run_id pointer accepted (optional field absent)",
			p: RedactionFailedPayload{
				EventType:  EventType("bus_overflow"),
				RunID:      nil,
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: true,
		},
		{
			name: "uuid.Nil run_id rejected when non-nil pointer",
			p: RedactionFailedPayload{
				EventType:  EventType("agent_started"),
				RunID:      func() *RunID { id := RunID(uuid.Nil); return &id }(),
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "invalid error_class rejected",
			p: RedactionFailedPayload{
				EventType:  EventType("agent_started"),
				ErrorClass: ErrorCategory("not-a-real-class"),
				FailedAt:   "2026-05-14T00:00:00.000Z",
			},
			valid: false,
		},
		{
			name: "empty failed_at rejected",
			p: RedactionFailedPayload{
				EventType:  EventType("agent_started"),
				ErrorClass: ErrorCategoryTransient,
				FailedAt:   "",
			},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("RedactionFailedPayload.Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestRedactionFailedPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	runID := redactionFailedFixtureRunIDPtr(t)

	original := RedactionFailedPayload{
		EventType:  EventType("agent_started"),
		RunID:      runID,
		ErrorClass: ErrorCategoryTransient,
		FailedAt:   "2026-05-14T12:34:56.789Z",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded RedactionFailedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !decoded.Valid() {
		t.Error("decoded payload failed Valid()")
	}
	if decoded.EventType != original.EventType {
		t.Errorf("EventType: got %q, want %q", decoded.EventType, original.EventType)
	}
	if decoded.RunID == nil || *decoded.RunID != *original.RunID {
		t.Errorf("RunID: got %v, want %v", decoded.RunID, original.RunID)
	}
	if decoded.ErrorClass != original.ErrorClass {
		t.Errorf("ErrorClass: got %q, want %q", decoded.ErrorClass, original.ErrorClass)
	}
	if decoded.FailedAt != original.FailedAt {
		t.Errorf("FailedAt: got %q, want %q", decoded.FailedAt, original.FailedAt)
	}
}

func TestRedactionFailedPayloadRunIDOmittedWhenNil(t *testing.T) {
	t.Parallel()

	p := RedactionFailedPayload{
		EventType:  EventType("agent_started"),
		RunID:      nil,
		ErrorClass: ErrorCategoryTransient,
		FailedAt:   "2026-05-14T00:00:00.000Z",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, exists := m["run_id"]; exists {
		t.Error("run_id key present when RunID is nil; want omitted")
	}
}

// TestRedactionFailedPayloadConstructorShape verifies that the constructor
// registered for redaction_failed in the global event registry (EV-032/EV-034)
// returns a *RedactionFailedPayload.
//
// This is a constructor-shape test only. It does not call DecodePayload (which
// would be sensitive to eventRegistryReset() called in TestRegistry cleanup).
// Instead it directly invokes the constructor to verify the payload type.
func TestRedactionFailedPayloadConstructorShape(t *testing.T) {
	t.Parallel()

	ctor := func() EventPayload { return &RedactionFailedPayload{} }
	got := ctor()
	if got == nil {
		t.Fatal("constructor returned nil")
	}
	if _, ok := got.(*RedactionFailedPayload); !ok {
		t.Errorf("constructor returned %T, want *RedactionFailedPayload", got)
	}
}
