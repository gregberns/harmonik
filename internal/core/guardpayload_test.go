package core

import (
	"encoding/json"
	"testing"
)

func TestGuardPayloadValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		p     GuardPayload
		valid bool
	}{
		{
			name:  "nil applies_to_node is valid (all nodes)",
			p:     GuardPayload{AppliesToNode: nil},
			valid: true,
		},
		{
			name:  "non-empty node_id is valid",
			p:     GuardPayload{AppliesToNode: ptr("node-001")},
			valid: true,
		},
		{
			name:  "arbitrary non-empty string is valid",
			p:     GuardPayload{AppliesToNode: ptr("n")},
			valid: true,
		},
		{
			name:  "empty string pointer is invalid",
			p:     GuardPayload{AppliesToNode: ptr("")},
			valid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.p.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestGuardPayloadJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  GuardPayload
	}{
		{
			name:  "absent field decodes to nil",
			input: `{}`,
			want:  GuardPayload{AppliesToNode: nil},
		},
		{
			name:  "null field decodes to nil",
			input: `{"applies_to_node":null}`,
			want:  GuardPayload{AppliesToNode: nil},
		},
		{
			name:  "node_id present",
			input: `{"applies_to_node":"node-001"}`,
			want:  GuardPayload{AppliesToNode: ptr("node-001")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got GuardPayload
			if err := json.Unmarshal([]byte(tc.input), &got); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			switch {
			case tc.want.AppliesToNode == nil && got.AppliesToNode == nil:
				// both nil — OK
			case tc.want.AppliesToNode == nil && got.AppliesToNode != nil:
				t.Errorf("AppliesToNode: want nil, got %q", *got.AppliesToNode)
			case tc.want.AppliesToNode != nil && got.AppliesToNode == nil:
				t.Errorf("AppliesToNode: want %q, got nil", *tc.want.AppliesToNode)
			default:
				if *got.AppliesToNode != *tc.want.AppliesToNode {
					t.Errorf("AppliesToNode: got %q, want %q", *got.AppliesToNode, *tc.want.AppliesToNode)
				}
			}

			if !got.Valid() {
				t.Errorf("decoded value failed Valid(): %+v", got)
			}
		})
	}
}

func TestGuardPayloadJSONMarshal(t *testing.T) {
	t.Parallel()

	t.Run("nil omitted from output", func(t *testing.T) {
		t.Parallel()

		p := GuardPayload{AppliesToNode: nil}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if string(data) != `{}` {
			t.Errorf("got %q, want {}", string(data))
		}
	})

	t.Run("non-nil node_id present in output", func(t *testing.T) {
		t.Parallel()

		p := GuardPayload{AppliesToNode: ptr("node-001")}
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		want := `{"applies_to_node":"node-001"}`
		if string(data) != want {
			t.Errorf("got %q, want %q", string(data), want)
		}
	})
}
