package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

func agentOverrideFixtureBasic(t *testing.T) AgentOverride {
	t.Helper()
	return AgentOverride{Binary: "my-twin"}
}

func agentOverrideFixtureWithArgs(t *testing.T) AgentOverride {
	t.Helper()
	return AgentOverride{
		Binary: "/absolute/path/to/twin",
		Args:   []string{"--dry-run", "--verbose"},
	}
}

func TestAgentOverrideValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AgentOverride
		want  bool
	}{
		{
			name:  "valid: non-empty binary, no args",
			input: agentOverrideFixtureBasic(t),
			want:  true,
		},
		{
			name:  "valid: non-empty binary with args",
			input: agentOverrideFixtureWithArgs(t),
			want:  true,
		},
		{
			name:  "valid: non-empty binary, nil args",
			input: AgentOverride{Binary: "twin-name", Args: nil},
			want:  true,
		},
		{
			name:  "valid: non-empty binary, empty args slice",
			input: AgentOverride{Binary: "twin-name", Args: []string{}},
			want:  true,
		},
		{
			name:  "invalid: empty binary",
			input: AgentOverride{Binary: ""},
			want:  false,
		},
		{
			name:  "invalid: zero value",
			input: AgentOverride{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("AgentOverride{Binary:%q}.Valid() = %v, want %v", tc.input.Binary, got, tc.want)
			}
		})
	}
}

func TestAgentOverrideJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input AgentOverride
	}{
		{
			name:  "binary only",
			input: agentOverrideFixtureBasic(t),
		},
		{
			name:  "binary with args",
			input: agentOverrideFixtureWithArgs(t),
		},
		{
			name:  "binary with single arg",
			input: AgentOverride{Binary: "my-twin", Args: []string{"--flag"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got AgentOverride
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestAgentOverrideOmitEmptyArgs(t *testing.T) {
	t.Parallel()

	a := AgentOverride{Binary: "twin-bin"}
	data, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	if _, ok := raw["args"]; ok {
		t.Errorf("marshaled JSON contains 'args' key when Args is nil; got %s", data)
	}
	if _, ok := raw["binary"]; !ok {
		t.Errorf("marshaled JSON missing 'binary' key; got %s", data)
	}
}

func TestAgentOverrideAppendSemantics(t *testing.T) {
	t.Parallel()

	productionDefaults := []string{"--config", "/etc/harmonik.yaml"}

	mergeArgs := func(defaults []string, override AgentOverride) []string {
		result := make([]string, len(defaults), len(defaults)+len(override.Args))
		copy(result, defaults)
		result = append(result, override.Args...)
		return result
	}

	override := agentOverrideFixtureWithArgs(t)
	merged := mergeArgs(productionDefaults, override)
	want := []string{"--config", "/etc/harmonik.yaml", "--dry-run", "--verbose"}
	if !reflect.DeepEqual(merged, want) {
		t.Errorf("append merge mismatch:\n  got:  %v\n  want: %v", merged, want)
	}

	nilOverride := agentOverrideFixtureBasic(t) // Args is nil
	mergedNil := mergeArgs(productionDefaults, nilOverride)
	if !reflect.DeepEqual(mergedNil, productionDefaults) {
		t.Errorf("nil-args merge should equal productionDefaults:\n  got:  %v\n  want: %v", mergedNil, productionDefaults)
	}

	emptyOverride := AgentOverride{Binary: "twin", Args: []string{}}
	mergedEmpty := mergeArgs(productionDefaults, emptyOverride)
	if !reflect.DeepEqual(mergedEmpty, productionDefaults) {
		t.Errorf("empty-args merge should equal productionDefaults:\n  got:  %v\n  want: %v", mergedEmpty, productionDefaults)
	}
}
