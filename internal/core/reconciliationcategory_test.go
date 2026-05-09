package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReconciliationCategoryValid(t *testing.T) {
	t.Parallel()

	valid := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("expected %q to be valid", c)
		}
	}

	invalid := []ReconciliationCategory{
		"",
		"Cat-0",
		"CAT-0",
		"cat0",   // missing hyphen
		"cat_0",  // underscore instead of hyphen
		"Cat-3a", // mixed case
		"CAT-3A", // uppercase
		"cat-3A", // uppercase suffix
		"cat-6A", // uppercase suffix
		"cat-6B", // uppercase suffix
		"cat-7",  // out of range
		"cat-3d", // non-existent sub-category
		"unknown",
		"category-0",
	}
	for _, c := range invalid {
		if c.Valid() {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestReconciliationCategoryMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cat  ReconciliationCategory
		want string
	}{
		{ReconciliationCategoryCat0, "cat-0"},
		{ReconciliationCategoryCat1, "cat-1"},
		{ReconciliationCategoryCat2, "cat-2"},
		{ReconciliationCategoryCat3, "cat-3"},
		{ReconciliationCategoryCat3a, "cat-3a"},
		{ReconciliationCategoryCat3b, "cat-3b"},
		{ReconciliationCategoryCat3c, "cat-3c"},
		{ReconciliationCategoryCat4, "cat-4"},
		{ReconciliationCategoryCat5, "cat-5"},
		{ReconciliationCategoryCat6a, "cat-6a"},
		{ReconciliationCategoryCat6b, "cat-6b"},
	}
	for _, tc := range tests {
		got, err := tc.cat.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) error: %v", tc.cat, err)
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.cat, string(got), tc.want)
		}
	}

	if _, err := ReconciliationCategory("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := ReconciliationCategory("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

func TestReconciliationCategoryUnmarshalText(t *testing.T) {
	t.Parallel()

	type reconciliationCategoryFixtureWrapper struct {
		Category ReconciliationCategory `json:"category"`
	}

	tests := []struct {
		name    string
		input   string
		want    ReconciliationCategory
		wantErr bool
	}{
		{
			name:  "cat-0",
			input: `{"category":"cat-0"}`,
			want:  ReconciliationCategoryCat0,
		},
		{
			name:  "cat-1",
			input: `{"category":"cat-1"}`,
			want:  ReconciliationCategoryCat1,
		},
		{
			name:  "cat-2",
			input: `{"category":"cat-2"}`,
			want:  ReconciliationCategoryCat2,
		},
		{
			name:  "cat-3",
			input: `{"category":"cat-3"}`,
			want:  ReconciliationCategoryCat3,
		},
		{
			name:  "cat-3a",
			input: `{"category":"cat-3a"}`,
			want:  ReconciliationCategoryCat3a,
		},
		{
			name:  "cat-3b",
			input: `{"category":"cat-3b"}`,
			want:  ReconciliationCategoryCat3b,
		},
		{
			name:  "cat-3c",
			input: `{"category":"cat-3c"}`,
			want:  ReconciliationCategoryCat3c,
		},
		{
			name:  "cat-4",
			input: `{"category":"cat-4"}`,
			want:  ReconciliationCategoryCat4,
		},
		{
			name:  "cat-5",
			input: `{"category":"cat-5"}`,
			want:  ReconciliationCategoryCat5,
		},
		{
			name:  "cat-6a",
			input: `{"category":"cat-6a"}`,
			want:  ReconciliationCategoryCat6a,
		},
		{
			name:  "cat-6b",
			input: `{"category":"cat-6b"}`,
			want:  ReconciliationCategoryCat6b,
		},
		{
			name:    "uppercase Cat-0 rejected",
			input:   `{"category":"Cat-0"}`,
			wantErr: true,
		},
		{
			name:    "uppercase CAT-3A rejected",
			input:   `{"category":"CAT-3A"}`,
			wantErr: true,
		},
		{
			name:    "mixed-case cat-3A rejected",
			input:   `{"category":"cat-3A"}`,
			wantErr: true,
		},
		{
			name:    "missing-hyphen cat0 rejected",
			input:   `{"category":"cat0"}`,
			wantErr: true,
		},
		{
			name:    "underscore cat_3 rejected",
			input:   `{"category":"cat_3"}`,
			wantErr: true,
		},
		{
			name:    "out-of-range cat-7 rejected",
			input:   `{"category":"cat-7"}`,
			wantErr: true,
		},
		{
			name:    "non-existent cat-3d rejected",
			input:   `{"category":"cat-3d"}`,
			wantErr: true,
		},
		{
			name:    "unknown value rejected",
			input:   `{"category":"unknown"}`,
			wantErr: true,
		},
		{
			name:    "empty string rejected",
			input:   `{"category":""}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w reconciliationCategoryFixtureWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.Category != tc.want {
				t.Errorf("got %q, want %q", string(w.Category), string(tc.want))
			}
		})
	}
}

func TestReconciliationCategoryRoundTrip(t *testing.T) {
	t.Parallel()

	type reconciliationCategoryFixtureWrapper struct {
		Category ReconciliationCategory `json:"category"`
	}

	values := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b,
	}

	for _, cat := range values {
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()

			in := reconciliationCategoryFixtureWrapper{Category: cat}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", cat, err)
			}
			var out reconciliationCategoryFixtureWrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("Unmarshal(%q): %v", string(data), err)
			}
			if out.Category != cat {
				t.Errorf("round-trip: got %q, want %q", out.Category, cat)
			}
		})
	}
}

func TestReconciliationCategoryUnmarshalTextErrorMessage(t *testing.T) {
	t.Parallel()

	// Error message for an unknown value must list all eleven declared values.
	var c ReconciliationCategory
	err := c.UnmarshalText([]byte("cat-99"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"cat-0", "cat-1", "cat-2", "cat-3", "cat-3a",
		"cat-3b", "cat-3c", "cat-4", "cat-5", "cat-6a", "cat-6b",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
