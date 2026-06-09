package core_test

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestEpicCompletedPayloadValid(t *testing.T) {
	tests := []struct {
		name  string
		p     core.EpicCompletedPayload
		valid bool
	}{
		{
			name:  "all fields set",
			p:     core.EpicCompletedPayload{EpicID: "hk-aaa", LastChildBeadID: "hk-bbb", ClosedAt: "2026-06-01T00:00:00Z"},
			valid: true,
		},
		{
			name:  "missing EpicID",
			p:     core.EpicCompletedPayload{EpicID: "", LastChildBeadID: "hk-bbb", ClosedAt: "2026-06-01T00:00:00Z"},
			valid: false,
		},
		{
			name:  "missing LastChildBeadID",
			p:     core.EpicCompletedPayload{EpicID: "hk-aaa", LastChildBeadID: "", ClosedAt: "2026-06-01T00:00:00Z"},
			valid: false,
		},
		{
			name:  "missing ClosedAt",
			p:     core.EpicCompletedPayload{EpicID: "hk-aaa", LastChildBeadID: "hk-bbb", ClosedAt: ""},
			valid: false,
		},
		{
			name:  "zero value",
			p:     core.EpicCompletedPayload{},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v; want %v", got, tc.valid)
			}
		})
	}
}

// TestEpicCompletedRegistryRoundTrip verifies that epic_completed is registered
// in the global event registry and that its payload survives a marshal/unmarshal
// round-trip via Event.DecodePayload.
func TestEpicCompletedRegistryRoundTrip(t *testing.T) {
	original := core.EpicCompletedPayload{
		EpicID:          "hk-epic",
		LastChildBeadID: "hk-child",
		ClosedAt:        "2026-06-01T12:00:00Z",
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	evt := core.Event{
		Type:    string(core.EventTypeEpicCompleted),
		Payload: b,
	}

	decoded, err := evt.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	got, ok := decoded.(*core.EpicCompletedPayload)
	if !ok {
		t.Fatalf("expected *EpicCompletedPayload, got %T", decoded)
	}
	if got.EpicID != original.EpicID {
		t.Errorf("EpicID = %q; want %q", got.EpicID, original.EpicID)
	}
	if got.LastChildBeadID != original.LastChildBeadID {
		t.Errorf("LastChildBeadID = %q; want %q", got.LastChildBeadID, original.LastChildBeadID)
	}
	if got.ClosedAt != original.ClosedAt {
		t.Errorf("ClosedAt = %q; want %q", got.ClosedAt, original.ClosedAt)
	}
}
