package scenario

import (
	"encoding/json"
	"reflect"
	"testing"
)

// gitSeedOpFixtureCommit returns a minimally valid commit GitSeedOp.
func gitSeedOpFixtureCommit(t *testing.T) GitSeedOp {
	t.Helper()
	return GitSeedOp{
		Op:   GitSeedOpCommit,
		Args: map[string]string{"message": "initial commit"},
	}
}

// gitSeedOpFixtureBranch returns a minimally valid branch GitSeedOp.
func gitSeedOpFixtureBranch(t *testing.T) GitSeedOp {
	t.Helper()
	return GitSeedOp{
		Op:   GitSeedOpBranch,
		Args: map[string]string{"name": "feature-branch"},
	}
}

// gitSeedOpFixtureTag returns a minimally valid tag GitSeedOp.
func gitSeedOpFixtureTag(t *testing.T) GitSeedOp {
	t.Helper()
	return GitSeedOp{
		Op:   GitSeedOpTag,
		Args: map[string]string{"name": "v1.0.0"},
	}
}

// gitSeedOpFixtureCheckout returns a minimally valid checkout GitSeedOp.
func gitSeedOpFixtureCheckout(t *testing.T) GitSeedOp {
	t.Helper()
	return GitSeedOp{
		Op:   GitSeedOpCheckout,
		Args: map[string]string{"ref": "main"},
	}
}

func TestGitSeedOpKindValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input GitSeedOpKind
		want  bool
	}{
		{name: "commit", input: GitSeedOpCommit, want: true},
		{name: "branch", input: GitSeedOpBranch, want: true},
		{name: "tag", input: GitSeedOpTag, want: true},
		{name: "checkout", input: GitSeedOpCheckout, want: true},
		{name: "unknown empty", input: GitSeedOpKind(""), want: false},
		{name: "unknown arbitrary", input: GitSeedOpKind("rebase"), want: false},
		{name: "unknown uppercase", input: GitSeedOpKind("COMMIT"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("GitSeedOpKind(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestGitSeedOpKindMarshalText(t *testing.T) {
	t.Parallel()

	validKinds := []GitSeedOpKind{
		GitSeedOpCommit,
		GitSeedOpBranch,
		GitSeedOpTag,
		GitSeedOpCheckout,
	}

	// Round-trip each valid value through MarshalText → UnmarshalText.
	for _, k := range validKinds {
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			text, err := k.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q) error: %v", string(k), err)
			}
			if string(text) != string(k) {
				t.Errorf("MarshalText(%q) = %q, want %q", string(k), string(text), string(k))
			}

			var got GitSeedOpKind
			if err := got.UnmarshalText(text); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", string(text), err)
			}
			if got != k {
				t.Errorf("round-trip mismatch: got %q, want %q", string(got), string(k))
			}
		})
	}

	// MarshalText on an unknown value must return an error.
	t.Run("unknown rejects marshal", func(t *testing.T) {
		t.Parallel()
		unknown := GitSeedOpKind("unknown-op")
		if _, err := unknown.MarshalText(); err == nil {
			t.Errorf("MarshalText on unknown value %q should return error", string(unknown))
		}
	})

	// UnmarshalText on an unknown value must return an error.
	t.Run("unknown rejects unmarshal", func(t *testing.T) {
		t.Parallel()
		var k GitSeedOpKind
		if err := k.UnmarshalText([]byte("unknown-op")); err == nil {
			t.Errorf("UnmarshalText on unknown value should return error")
		}
	})
}

func TestGitSeedOpValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input GitSeedOp
		want  bool
	}{
		{
			name:  "valid commit: message only",
			input: gitSeedOpFixtureCommit(t),
			want:  true,
		},
		{
			name: "valid commit: with optional parent",
			input: GitSeedOp{
				Op:   GitSeedOpCommit,
				Args: map[string]string{"message": "second commit", "parent": "abc123"},
			},
			want: true,
		},
		{
			name: "valid commit: with optional ref",
			input: GitSeedOp{
				Op:   GitSeedOpCommit,
				Args: map[string]string{"message": "on branch", "ref": "refs/heads/feature"},
			},
			want: true,
		},
		{
			name: "valid commit: with all optional keys",
			input: GitSeedOp{
				Op:   GitSeedOpCommit,
				Args: map[string]string{"message": "full", "parent": "abc123", "ref": "refs/heads/main"},
			},
			want: true,
		},
		{
			name: "invalid commit: missing message",
			input: GitSeedOp{
				Op:   GitSeedOpCommit,
				Args: map[string]string{"parent": "abc123"},
			},
			want: false,
		},
		{
			name:  "valid branch: name only",
			input: gitSeedOpFixtureBranch(t),
			want:  true,
		},
		{
			name: "valid branch: with optional from",
			input: GitSeedOp{
				Op:   GitSeedOpBranch,
				Args: map[string]string{"name": "feat", "from": "refs/heads/main"},
			},
			want: true,
		},
		{
			name: "invalid branch: missing name",
			input: GitSeedOp{
				Op:   GitSeedOpBranch,
				Args: map[string]string{"from": "HEAD"},
			},
			want: false,
		},
		{
			name:  "valid tag: name only",
			input: gitSeedOpFixtureTag(t),
			want:  true,
		},
		{
			name: "valid tag: with optional target",
			input: GitSeedOp{
				Op:   GitSeedOpTag,
				Args: map[string]string{"name": "v2.0.0", "target": "refs/heads/release"},
			},
			want: true,
		},
		{
			name: "invalid tag: missing name",
			input: GitSeedOp{
				Op:   GitSeedOpTag,
				Args: map[string]string{"target": "HEAD"},
			},
			want: false,
		},
		{
			name:  "valid checkout: ref only",
			input: gitSeedOpFixtureCheckout(t),
			want:  true,
		},
		{
			name: "invalid checkout: missing ref",
			input: GitSeedOp{
				Op:   GitSeedOpCheckout,
				Args: map[string]string{"other": "value"},
			},
			want: false,
		},
		{
			name:  "invalid: unknown op",
			input: GitSeedOp{Op: GitSeedOpKind("merge"), Args: map[string]string{"into": "main"}},
			want:  false,
		},
		{
			name:  "invalid: zero value",
			input: GitSeedOp{},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("GitSeedOp{Op:%q}.Valid() = %v, want %v", string(tc.input.Op), got, tc.want)
			}
		})
	}
}

func TestGitSeedOpJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input GitSeedOp
	}{
		{name: "commit minimal", input: gitSeedOpFixtureCommit(t)},
		{name: "branch minimal", input: gitSeedOpFixtureBranch(t)},
		{name: "tag minimal", input: gitSeedOpFixtureTag(t)},
		{name: "checkout minimal", input: gitSeedOpFixtureCheckout(t)},
		{
			name: "commit with optional keys",
			input: GitSeedOp{
				Op:   GitSeedOpCommit,
				Args: map[string]string{"message": "bump", "parent": "deadbeef", "ref": "refs/heads/main"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got GitSeedOp
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

// TestGitSeedOpRequiredKeys asserts that gitSeedOpRequiredKeys matches
// specs/scenario-harness.md §6.3 exactly — one entry per op, correct key names.
func TestGitSeedOpRequiredKeys(t *testing.T) {
	t.Parallel()

	// Expected is the canonical table from specs/scenario-harness.md §6.3.
	expected := map[GitSeedOpKind][]string{
		GitSeedOpCommit:   {"message"},
		GitSeedOpBranch:   {"name"},
		GitSeedOpTag:      {"name"},
		GitSeedOpCheckout: {"ref"},
	}

	if len(gitSeedOpRequiredKeys) != len(expected) {
		t.Errorf("gitSeedOpRequiredKeys has %d entries, want %d", len(gitSeedOpRequiredKeys), len(expected))
	}

	for op, wantKeys := range expected {
		gotKeys, ok := gitSeedOpRequiredKeys[op]
		if !ok {
			t.Errorf("gitSeedOpRequiredKeys missing entry for op %q", string(op))
			continue
		}
		if !reflect.DeepEqual(gotKeys, wantKeys) {
			t.Errorf("gitSeedOpRequiredKeys[%q] = %v, want %v", string(op), gotKeys, wantKeys)
		}
	}

	// Verify no extra ops are present beyond the four declared in §6.3.
	for op := range gitSeedOpRequiredKeys {
		if _, ok := expected[op]; !ok {
			t.Errorf("gitSeedOpRequiredKeys has unexpected entry for op %q", string(op))
		}
	}
}
