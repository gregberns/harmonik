package core

// Edge-path table tests for five functions in internal/core:
//   - ParsePolicyDocument (malformed/edge YAML inputs)
//   - contains / isMaxNodesError (policyexprevaluator internal helpers)
//   - FreedomProfile.Valid (invalid budget refs, combinatorial table)
//   - SchemaChangeKind.IsBreaking (unknown kind conservative default)
//   - PathGlob.Valid (table-driven + property test)
//
// Part of hk-j3hrn (core coverage uplift: 80.7% → >85%).
// Refs: hk-6d6d1

import (
	"errors"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// ParsePolicyDocument — malformed / edge YAML inputs
// ---------------------------------------------------------------------------

// TestParsePolicyDocument_MalformedYAML verifies that ParsePolicyDocument
// returns a non-nil error on structurally invalid YAML.
func TestParsePolicyDocument_MalformedYAML(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []byte
	}{
		{
			name:  "tab_indented",
			input: []byte("metadata:\n\tname: bad"),
		},
		{
			name:  "unclosed_brace",
			input: []byte("metadata: {name: no-close"),
		},
		{
			name:  "trailing_colon_block",
			input: []byte("metadata:\n  name: ok\n  bad: : extra"),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParsePolicyDocument(tc.input)
			if err == nil {
				t.Errorf("ParsePolicyDocument(%q): expected error for malformed YAML, got nil", tc.input)
			}
		})
	}
}

// TestParsePolicyDocument_EmptyInput verifies that empty input succeeds at
// parse time (empty YAML is valid; section validation is a separate step).
func TestParsePolicyDocument_EmptyInput(t *testing.T) {
	t.Parallel()

	_, err := ParsePolicyDocument([]byte{})
	if err != nil {
		t.Errorf("ParsePolicyDocument(empty): unexpected error %v; empty YAML should parse without error", err)
	}
}

// TestParsePolicyDocument_NilInput verifies that nil input is handled
// gracefully (same as empty).
func TestParsePolicyDocument_NilInput(t *testing.T) {
	t.Parallel()

	_, err := ParsePolicyDocument(nil)
	if err != nil {
		t.Errorf("ParsePolicyDocument(nil): unexpected error %v", err)
	}
}

// TestParsePolicyDocument_TopLevelList verifies that a YAML document whose root
// is a list (not a mapping) returns an error — the first unmarshal into
// map[string]yaml.Node rejects a sequence.
func TestParsePolicyDocument_TopLevelList(t *testing.T) {
	t.Parallel()

	input := []byte("- item1\n- item2\n")
	_, err := ParsePolicyDocument(input)
	if err == nil {
		t.Error("ParsePolicyDocument(list root): expected error, got nil")
	}
}

// TestParsePolicyDocument_ScalarRoot verifies that a YAML document whose root
// is a plain scalar (not a mapping) returns an error — the first unmarshal
// into map[string]yaml.Node rejects a scalar value.
func TestParsePolicyDocument_ScalarRoot(t *testing.T) {
	t.Parallel()

	input := []byte("just a string")
	_, err := ParsePolicyDocument(input)
	if err == nil {
		t.Error("ParsePolicyDocument(scalar root): expected error, got nil")
	}
}

// TestParsePolicyDocument_AllSectionsPresent verifies that a document with all
// seven required sections records each as present and passes ValidateSections.
func TestParsePolicyDocument_AllSectionsPresent(t *testing.T) {
	t.Parallel()

	input := []byte(`
metadata:
  name: full-doc
  version: "1.0.0"
  author: tester
  schema_version: 1
roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(input)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateSections(); err != nil {
		t.Errorf("ValidateSections() = %v, want nil (all sections present)", err)
	}
}

// TestParsePolicyDocument_MissingSections verifies that each required section,
// when absent one at a time, causes ValidateSections to return
// ErrMissingPolicySection naming that section.
func TestParsePolicyDocument_MissingSections(t *testing.T) {
	t.Parallel()

	allSections := []string{
		"metadata", "roles", "freedom_profiles", "gates", "hooks", "guards", "budgets",
	}

	// Full valid YAML template; we will strip one key per sub-test.
	fullYAML := `metadata:
  name: section-test
  version: "1.0"
  author: tester
  schema_version: 1
roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`

	for _, section := range allSections {
		section := section
		t.Run("missing_"+section, func(t *testing.T) {
			t.Parallel()

			// Remove the line(s) starting with the section key.
			var kept []string
			for _, line := range strings.Split(fullYAML, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, section+":") {
					continue
				}
				kept = append(kept, line)
			}
			data := []byte(strings.Join(kept, "\n"))

			doc, err := ParsePolicyDocument(data)
			if err != nil {
				t.Fatalf("ParsePolicyDocument: %v", err)
			}
			secErr := doc.ValidateSections()
			if secErr == nil {
				t.Fatalf("ValidateSections() = nil, want ErrMissingPolicySection for missing %q", section)
			}
			if !errors.Is(secErr, ErrMissingPolicySection) {
				t.Errorf("ValidateSections error = %v, want wrapping ErrMissingPolicySection", secErr)
			}
			if !strings.Contains(secErr.Error(), section) {
				t.Errorf("ValidateSections error = %q, want %q in message", secErr.Error(), section)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// contains — internal helper (policyexprevaluator.go)
// ---------------------------------------------------------------------------

// TestContains_EdgePaths covers the edge branches of the contains helper.
func TestContains_EdgePaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		s      string
		substr string
		want   bool
	}{
		{"empty_substr_always_true", "anything", "", true},
		{"empty_both", "", "", true},
		{"substr_longer_than_s", "ab", "abc", false},
		{"exact_match", "hello", "hello", true},
		{"prefix_match", "hello world", "hello", true},
		{"suffix_match", "hello world", "world", true},
		{"mid_match", "abcde", "bcd", true},
		{"no_match", "abcde", "xyz", false},
		{"single_char_match", "a", "a", true},
		{"single_char_no_match", "a", "b", false},
		{"substr_equals_len_s", "abc", "abc", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := contains(tc.s, tc.substr)
			if got != tc.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tc.s, tc.substr, got, tc.want)
			}
		})
	}
}

// TestContains_Property verifies that contains agrees with strings.Contains for
// arbitrary inputs (property-based).
func TestContains_Property(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.String().Draw(rt, "s")
		sub := rapid.StringN(0, 20, -1).Draw(rt, "sub")

		got := contains(s, sub)
		want := strings.Contains(s, sub)
		if got != want {
			rt.Errorf("contains(%q, %q) = %v; strings.Contains = %v", s, sub, got, want)
		}
	})
}

// ---------------------------------------------------------------------------
// isMaxNodesError — internal helper (policyexprevaluator.go)
// ---------------------------------------------------------------------------

// TestIsMaxNodesError_EdgePaths tests nil and non-max-nodes errors.
func TestIsMaxNodesError_EdgePaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil_err", nil, false},
		{"unrelated_error", errors.New("some other error"), false},
		{"empty_message", errors.New(""), false},
		{"partial_match_prefix", errors.New("exceeds maximum"), false},
		{"exact_trigger_phrase", errors.New("exceeds maximum allowed nodes"), true},
		{"phrase_embedded_in_longer", errors.New("program (42 nodes) exceeds maximum allowed nodes (10)"), true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := isMaxNodesError(tc.err)
			if got != tc.want {
				t.Errorf("isMaxNodesError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FreedomProfile.Valid — invalid budget ref edge paths
// ---------------------------------------------------------------------------

// TestFreedomProfile_Valid_Table covers the branchy edge paths not exercised
// by the existing positive/negative tests in freedomprofile_test.go:
// specifically, non-nil but invalid (empty) BudgetRef pointers.
func TestFreedomProfile_Valid_Table(t *testing.T) {
	t.Parallel()

	emptyToken := BudgetRef("")
	emptyWall := BudgetRef("")
	validToken := BudgetRef("token-budget")
	validWall := BudgetRef("wall-clock-budget")

	cases := []struct {
		name  string
		fp    FreedomProfile
		valid bool
	}{
		{
			name:  "empty_name_invalid",
			fp:    FreedomProfile{Name: "", MaxIterations: 1},
			valid: false,
		},
		{
			name:  "zero_max_iterations_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 0},
			valid: false,
		},
		{
			name:  "negative_max_iterations_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: -5},
			valid: false,
		},
		{
			name:  "nil_token_budget_ref_valid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 1, TokenBudgetRef: nil},
			valid: true,
		},
		{
			name:  "empty_token_budget_ref_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 1, TokenBudgetRef: &emptyToken},
			valid: false,
		},
		{
			name:  "nil_wall_clock_budget_ref_valid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 1, WallClockBudgetRef: nil},
			valid: true,
		},
		{
			name:  "empty_wall_clock_budget_ref_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 1, WallClockBudgetRef: &emptyWall},
			valid: false,
		},
		{
			name:  "both_budget_refs_valid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 10, TokenBudgetRef: &validToken, WallClockBudgetRef: &validWall},
			valid: true,
		},
		{
			name:  "token_valid_wall_empty_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 10, TokenBudgetRef: &validToken, WallClockBudgetRef: &emptyWall},
			valid: false,
		},
		{
			name:  "token_empty_wall_valid_invalid",
			fp:    FreedomProfile{Name: "x", MaxIterations: 10, TokenBudgetRef: &emptyToken, WallClockBudgetRef: &validWall},
			valid: false,
		},
		{
			name:  "minimal_valid",
			fp:    FreedomProfile{Name: "minimal", MaxIterations: 1},
			valid: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.fp.Valid()
			if got != tc.valid {
				t.Errorf("FreedomProfile{Name:%q, MaxIterations:%d}.Valid() = %v, want %v",
					tc.fp.Name, tc.fp.MaxIterations, got, tc.valid)
			}
		})
	}
}

// TestProp_FreedomProfile_ValidRequiresNameAndPositiveIterations is a property
// test: for any non-empty name and positive MaxIterations (and nil budget refs),
// Valid() must return true.
func TestProp_FreedomProfile_ValidRequiresNameAndPositiveIterations(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringN(1, 64, -1).Draw(rt, "name")
		iters := rapid.IntRange(1, 1000).Draw(rt, "iters")

		fp := FreedomProfile{
			Name:          name,
			MaxIterations: iters,
		}
		if !fp.Valid() {
			rt.Errorf("FreedomProfile{Name:%q, MaxIterations:%d}.Valid() = false, want true", name, iters)
		}
	})
}

// TestProp_FreedomProfile_InvalidWhenEmptyName is a property test: any
// FreedomProfile with an empty name must be invalid regardless of other fields.
func TestProp_FreedomProfile_InvalidWhenEmptyName(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		iters := rapid.IntRange(1, 1000).Draw(rt, "iters")

		fp := FreedomProfile{
			Name:          "",
			MaxIterations: iters,
		}
		if fp.Valid() {
			rt.Errorf("FreedomProfile with empty name must be invalid; MaxIterations=%d", iters)
		}
	})
}

// ---------------------------------------------------------------------------
// SchemaChangeKind.IsBreaking — unknown kind conservative default
// ---------------------------------------------------------------------------

// TestSchemaChangeKind_IsBreaking_UnknownKindIsConservative verifies that any
// unknown SchemaChangeKind returns true from IsBreaking() — the conservative
// default documented in the source comment (prevents silent compat violations).
func TestSchemaChangeKind_IsBreaking_UnknownKindIsConservative(t *testing.T) {
	t.Parallel()

	unknown := []SchemaChangeKind{
		"",
		"unknown",
		"add_optional", // close but not exact
		"RENAME_FIELD", // wrong case
		"remove-field", // wrong separator
	}

	for _, k := range unknown {
		k := k
		t.Run(string(k)+"_is_breaking", func(t *testing.T) {
			t.Parallel()

			if !k.IsBreaking() {
				t.Errorf("SchemaChangeKind(%q).IsBreaking() = false, want true (conservative default for unknown kinds)", k)
			}
		})
	}
}

// TestSchemaChangeKind_IsBreaking_EmptyStringIsBreaking verifies the zero-value
// SchemaChangeKind (empty string) is treated as breaking.
func TestSchemaChangeKind_IsBreaking_EmptyStringIsBreaking(t *testing.T) {
	t.Parallel()

	var k SchemaChangeKind
	if !k.IsBreaking() {
		t.Error("zero-value SchemaChangeKind(\"\").IsBreaking() = false, want true (conservative)")
	}
}

// TestProp_SchemaChangeKind_UnknownAlwaysBreaking is a property test: any
// string that does not match a declared constant must be treated as breaking.
func TestProp_SchemaChangeKind_UnknownAlwaysBreaking(t *testing.T) {
	t.Parallel()

	known := map[SchemaChangeKind]bool{
		SchemaChangeAddOptionalField:  true,
		SchemaChangeAddRequiredField:  true,
		SchemaChangeRenameField:       true,
		SchemaChangeRemoveField:       true,
		SchemaChangeWidenType:         true,
		SchemaChangeNarrowType:        true,
		SchemaChangeAddEnumVariant:    true,
		SchemaChangeRemoveEnumVariant: true,
		SchemaChangeTightenValidation: true,
	}

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 64, -1).Draw(rt, "raw")
		k := SchemaChangeKind(raw)
		if known[k] {
			return // skip declared constants — they have known breaking status
		}
		if !k.IsBreaking() {
			rt.Errorf("unknown SchemaChangeKind(%q).IsBreaking() = false; unknown kinds must be conservative (true)", raw)
		}
	})
}

// ---------------------------------------------------------------------------
// PathGlob.Valid — table-driven tests + property test
// ---------------------------------------------------------------------------

// TestPathGlob_Valid_Table covers PathGlob.Valid() for empty and non-empty inputs.
func TestPathGlob_Valid_Table(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		glob  PathGlob
		valid bool
	}{
		{"empty_is_invalid", "", false},
		{"single_star", "*", true},
		{"double_star", "**", true},
		{"simple_path", "src/main.go", true},
		{"dir_glob", "internal/**", true},
		{"dot_slash", "./foo/bar", true},
		{"space_only", " ", true}, // non-empty; Valid only checks non-empty
		{"single_char", "a", true},
		{"wildcard_extension", "*.go", true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.glob.Valid()
			if got != tc.valid {
				t.Errorf("PathGlob(%q).Valid() = %v, want %v", tc.glob, got, tc.valid)
			}
		})
	}
}

// TestPathGlob_Valid_ZeroValue verifies the zero-value PathGlob is invalid.
func TestPathGlob_Valid_ZeroValue(t *testing.T) {
	t.Parallel()

	var p PathGlob
	if p.Valid() {
		t.Error("zero-value PathGlob should be invalid")
	}
}

// TestProp_PathGlob_NonEmptyIsValid is a property test: any non-empty PathGlob
// must return true from Valid() per the §6.2 rule (non-empty only).
func TestProp_PathGlob_NonEmptyIsValid(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 256, -1).Draw(rt, "raw")
		p := PathGlob(raw)
		if !p.Valid() {
			rt.Errorf("PathGlob(%q).Valid() = false for non-empty string; want true", raw)
		}
	})
}
