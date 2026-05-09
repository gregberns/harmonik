package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

// fixtureSetupFixtureEmpty returns a zero-value FixtureSetup (all nil fields).
// A zero-value is valid per the spec: all fields are optional (|None).
func fixtureSetupFixtureEmpty(t *testing.T) FixtureSetup {
	t.Helper()
	return FixtureSetup{}
}

// fixtureSetupFixtureFull returns a fully-populated FixtureSetup with all
// three fields set to non-nil, non-empty values using valid sibling types.
func fixtureSetupFixtureFull(t *testing.T) FixtureSetup {
	t.Helper()
	return FixtureSetup{
		GitSeed: []GitSeedOp{
			gitSeedOpFixtureCommit(t),
			gitSeedOpFixtureBranch(t),
		},
		Files: map[string]FileSeed{
			"README.md":  fileSeedFixtureUTF8(t),
			"bin/script": fileSeedFixtureBase64(t),
		},
		SkillSearchPaths: []string{"/opt/skills", "/usr/local/skills"},
	}
}

// fixtureSetupFixtureGitOnly returns a FixtureSetup with only GitSeed set.
func fixtureSetupFixtureGitOnly(t *testing.T) FixtureSetup {
	t.Helper()
	return FixtureSetup{
		GitSeed: []GitSeedOp{gitSeedOpFixtureCommit(t)},
	}
}

// fixtureSetupFixtureFilesOnly returns a FixtureSetup with only Files set.
func fixtureSetupFixtureFilesOnly(t *testing.T) FixtureSetup {
	t.Helper()
	return FixtureSetup{
		Files: map[string]FileSeed{
			"config.yaml": fileSeedFixtureDefaults(t),
		},
	}
}

// fixtureSetupFixtureSkillPathsOnly returns a FixtureSetup with only
// SkillSearchPaths set.
func fixtureSetupFixtureSkillPathsOnly(t *testing.T) FixtureSetup {
	t.Helper()
	return FixtureSetup{
		SkillSearchPaths: []string{"/skills"},
	}
}

func TestFixtureSetupValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input FixtureSetup
		want  bool
	}{
		{
			name:  "zero value (all nil) is valid",
			input: fixtureSetupFixtureEmpty(t),
			want:  true,
		},
		{
			name:  "fully populated with valid fields is valid",
			input: fixtureSetupFixtureFull(t),
			want:  true,
		},
		{
			name:  "git_seed only is valid",
			input: fixtureSetupFixtureGitOnly(t),
			want:  true,
		},
		{
			name:  "files only is valid",
			input: fixtureSetupFixtureFilesOnly(t),
			want:  true,
		},
		{
			name:  "skill_search_paths only is valid",
			input: fixtureSetupFixtureSkillPathsOnly(t),
			want:  true,
		},
		{
			name: "empty (non-nil) slices and map are valid",
			input: FixtureSetup{
				GitSeed:          []GitSeedOp{},
				Files:            map[string]FileSeed{},
				SkillSearchPaths: []string{},
			},
			want: true,
		},
		{
			name: "invalid GitSeedOp element propagates invalid",
			input: FixtureSetup{
				GitSeed: []GitSeedOp{
					{Op: GitSeedOpKind("bad-op"), Args: map[string]string{}},
				},
			},
			want: false,
		},
		{
			name: "invalid FileSeed value propagates invalid",
			input: FixtureSetup{
				Files: map[string]FileSeed{
					"file.txt": {
						Encoding: FileSeedEncoding("hex"), // unknown encoding
						Contents: "data",
					},
				},
			},
			want: false,
		},
		{
			name: "valid GitSeedOp mixed with invalid propagates invalid",
			input: FixtureSetup{
				GitSeed: []GitSeedOp{
					gitSeedOpFixtureCommit(t),
					{Op: GitSeedOpKind("invalid"), Args: map[string]string{}},
				},
			},
			want: false,
		},
		{
			name: "valid FileSeed mixed with invalid propagates invalid",
			input: FixtureSetup{
				Files: map[string]FileSeed{
					"good.txt": fileSeedFixtureUTF8(t),
					"bad.txt": {
						Encoding: FileSeedEncoding("hex"),
						Contents: "nope",
					},
				},
			},
			want: false,
		},
		{
			name: "GitSeedOp with missing required key is invalid",
			input: FixtureSetup{
				GitSeed: []GitSeedOp{
					// commit requires "message" key
					{Op: GitSeedOpCommit, Args: map[string]string{"parent": "abc"}},
				},
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("FixtureSetup.Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFixtureSetupJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input FixtureSetup
	}{
		{name: "zero value", input: fixtureSetupFixtureEmpty(t)},
		{name: "git_seed only", input: fixtureSetupFixtureGitOnly(t)},
		{name: "files only", input: fixtureSetupFixtureFilesOnly(t)},
		{name: "skill_search_paths only", input: fixtureSetupFixtureSkillPathsOnly(t)},
		{name: "fully populated", input: fixtureSetupFixtureFull(t)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got FixtureSetup
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestFixtureSetupOmitEmptyFields(t *testing.T) {
	t.Parallel()

	// A zero-value FixtureSetup must marshal with no keys present (all omitempty).
	f := FixtureSetup{}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	for _, key := range []string{"git_seed", "files", "skill_search_paths"} {
		if _, ok := raw[key]; ok {
			t.Errorf("marshaled zero-value JSON contains %q key; got %s", key, data)
		}
	}
}

func TestFixtureSetupNilVsEmpty(t *testing.T) {
	t.Parallel()

	// nil and non-nil-empty behave the same for Valid() — both are valid.
	nilSetup := FixtureSetup{}
	emptySetup := FixtureSetup{
		GitSeed:          []GitSeedOp{},
		Files:            map[string]FileSeed{},
		SkillSearchPaths: []string{},
	}

	if !nilSetup.Valid() {
		t.Error("nil-field FixtureSetup.Valid() = false, want true")
	}
	if !emptySetup.Valid() {
		t.Error("empty-field FixtureSetup.Valid() = false, want true")
	}

	// But nil and non-nil-empty are distinguishable via reflect (Go idiom
	// for the caller that needs None vs. empty discrimination).
	if reflect.DeepEqual(nilSetup, emptySetup) {
		t.Error("nil-field and empty-field FixtureSetup are DeepEqual; Go nil-vs-empty semantics require them to differ")
	}
}
