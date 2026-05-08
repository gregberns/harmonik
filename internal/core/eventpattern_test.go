package core

import (
	"encoding/json"
	"testing"
)

// eventPatternFixtureTypes returns an EventPattern in explicit mode with the given types.
func eventPatternFixtureTypes(types ...string) EventPattern {
	m := make(map[string]struct{}, len(types))
	for _, t := range types {
		m[t] = struct{}{}
	}
	return EventPattern{Wildcard: false, Types: m}
}

// eventPatternFixtureWildcard returns an EventPattern in wildcard mode.
func eventPatternFixtureWildcard() EventPattern {
	return EventPattern{Wildcard: true, Types: map[string]struct{}{}}
}

func TestEventPatternValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern EventPattern
		wantErr bool
	}{
		{
			// Wildcard=true with empty types: valid per §6.1 invariant.
			name:    "wildcard true empty types",
			pattern: eventPatternFixtureWildcard(),
			wantErr: false,
		},
		{
			// Wildcard=false with non-empty types: valid per §6.1 invariant.
			name:    "wildcard false non-empty types",
			pattern: eventPatternFixtureTypes("run_started", "run_completed"),
			wantErr: false,
		},
		{
			// Wildcard=true with non-empty types: invalid per §6.1 "empty when wildcard=true".
			name: "wildcard true non-empty types",
			pattern: EventPattern{
				Wildcard: true,
				Types:    map[string]struct{}{"run_started": {}},
			},
			wantErr: true,
		},
		{
			// Wildcard=false with empty types: invalid — explicit mode requires at least one type.
			name:    "wildcard false empty types",
			pattern: EventPattern{Wildcard: false, Types: map[string]struct{}{}},
			wantErr: true,
		},
		{
			// Wildcard=false with nil types map is equivalent to empty — invalid.
			name:    "wildcard false nil types",
			pattern: EventPattern{Wildcard: false, Types: nil},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.pattern.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("Validate() = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestEventPatternMatchesType(t *testing.T) {
	t.Parallel()

	t.Run("wildcard matches any type", func(t *testing.T) {
		t.Parallel()
		p := eventPatternFixtureWildcard()
		for _, typ := range []string{"run_started", "run_completed", "unknown_future_type", ""} {
			if !p.MatchesType(typ) {
				t.Errorf("wildcard pattern: MatchesType(%q) = false, want true", typ)
			}
		}
	})

	t.Run("explicit matches listed types only", func(t *testing.T) {
		t.Parallel()
		p := eventPatternFixtureTypes("run_started", "run_completed")
		if !p.MatchesType("run_started") {
			t.Error("MatchesType(run_started) = false, want true")
		}
		if !p.MatchesType("run_completed") {
			t.Error("MatchesType(run_completed) = false, want true")
		}
		if p.MatchesType("run_failed") {
			t.Error("MatchesType(run_failed) = true, want false")
		}
		if p.MatchesType("") {
			t.Error("MatchesType(\"\") = true, want false")
		}
	})
}

func TestEventPatternMarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("wildcard round-trips through JSON", func(t *testing.T) {
		t.Parallel()
		orig := eventPatternFixtureWildcard()
		if err := orig.Validate(); err != nil {
			t.Fatalf("fixture invalid: %v", err)
		}
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		var got EventPattern
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("UnmarshalJSON: %v", err)
		}
		if got.Wildcard != orig.Wildcard {
			t.Errorf("Wildcard: got %v, want %v", got.Wildcard, orig.Wildcard)
		}
		if len(got.Types) != 0 {
			t.Errorf("Types: got %v, want empty", got.Types)
		}
	})

	t.Run("explicit types round-trip through JSON", func(t *testing.T) {
		t.Parallel()
		orig := eventPatternFixtureTypes("run_started", "agent_ready")
		if err := orig.Validate(); err != nil {
			t.Fatalf("fixture invalid: %v", err)
		}
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("MarshalJSON: %v", err)
		}
		var got EventPattern
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("UnmarshalJSON: %v", err)
		}
		if got.Wildcard {
			t.Errorf("Wildcard: got true, want false")
		}
		if len(got.Types) != 2 {
			t.Errorf("Types len: got %d, want 2", len(got.Types))
		}
		for typ := range orig.Types {
			if _, ok := got.Types[typ]; !ok {
				t.Errorf("Types: missing %q after round-trip", typ)
			}
		}
	})

	t.Run("unmarshal deduplicates duplicate type strings", func(t *testing.T) {
		t.Parallel()
		raw := `{"wildcard":false,"types":["run_started","run_started","run_completed"]}`
		var p EventPattern
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("UnmarshalJSON: %v", err)
		}
		if len(p.Types) != 2 {
			t.Errorf("Types len after dedup: got %d, want 2", len(p.Types))
		}
	})

	t.Run("unmarshal rejects malformed JSON", func(t *testing.T) {
		t.Parallel()
		if err := json.Unmarshal([]byte(`not-json`), &EventPattern{}); err == nil {
			t.Error("UnmarshalJSON: expected error for malformed input, got nil")
		}
	})
}
