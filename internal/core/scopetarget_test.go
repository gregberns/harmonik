package core

import (
	"encoding/json"
	"testing"
)

// scopeTargetFixtureWrapper wraps a ScopeTarget for JSON round-trip tests.
type scopeTargetFixtureWrapper struct {
	Target ScopeTarget `json:"scope_target"`
}

// --- constructor / Valid tests ---

func TestScopeTargetWildcard(t *testing.T) {
	t.Parallel()

	st := ScopeTargetWildcard()
	if !st.Valid() {
		t.Fatal("ScopeTargetWildcard() produced invalid ScopeTarget")
	}
	if st.Kind != ScopeTargetKindWildcard {
		t.Errorf("kind = %q, want %q", st.Kind, ScopeTargetKindWildcard)
	}
	if st.PredicateType != "" {
		t.Errorf("PredicateType = %q, want empty", st.PredicateType)
	}
	if len(st.IDs) != 0 {
		t.Errorf("IDs = %v, want nil/empty", st.IDs)
	}
}

func TestScopeTargetPredicate(t *testing.T) {
	t.Parallel()

	st, err := ScopeTargetPredicate("llm_node")
	if err != nil {
		t.Fatalf("ScopeTargetPredicate: unexpected error: %v", err)
	}
	if !st.Valid() {
		t.Fatal("ScopeTargetPredicate produced invalid ScopeTarget")
	}
	if st.Kind != ScopeTargetKindPredicate {
		t.Errorf("kind = %q, want %q", st.Kind, ScopeTargetKindPredicate)
	}
	if st.PredicateType != "llm_node" {
		t.Errorf("PredicateType = %q, want %q", st.PredicateType, "llm_node")
	}
	if len(st.IDs) != 0 {
		t.Errorf("IDs = %v, want nil/empty", st.IDs)
	}

	_, err = ScopeTargetPredicate("")
	if err == nil {
		t.Error("ScopeTargetPredicate(\"\") should return error")
	}
}

func TestScopeTargetList(t *testing.T) {
	t.Parallel()

	st, err := ScopeTargetList([]string{"role-a", "role-b", "role-c"})
	if err != nil {
		t.Fatalf("ScopeTargetList: unexpected error: %v", err)
	}
	if !st.Valid() {
		t.Fatal("ScopeTargetList produced invalid ScopeTarget")
	}
	if st.Kind != ScopeTargetKindList {
		t.Errorf("kind = %q, want %q", st.Kind, ScopeTargetKindList)
	}
	if len(st.IDs) != 3 {
		t.Errorf("IDs length = %d, want 3", len(st.IDs))
	}

	// reject empty list
	_, err = ScopeTargetList([]string{})
	if err == nil {
		t.Error("ScopeTargetList([]) should return error")
	}
	_, err = ScopeTargetList(nil)
	if err == nil {
		t.Error("ScopeTargetList(nil) should return error")
	}

	// reject list with empty element
	_, err = ScopeTargetList([]string{"role-a", "", "role-c"})
	if err == nil {
		t.Error("ScopeTargetList with empty element should return error")
	}
}

func TestScopeTargetSingleton(t *testing.T) {
	t.Parallel()

	st, err := ScopeTargetSingleton("reviewer")
	if err != nil {
		t.Fatalf("ScopeTargetSingleton: unexpected error: %v", err)
	}
	if !st.Valid() {
		t.Fatal("ScopeTargetSingleton produced invalid ScopeTarget")
	}
	if st.Kind != ScopeTargetKindSingleton {
		t.Errorf("kind = %q, want %q", st.Kind, ScopeTargetKindSingleton)
	}
	if len(st.IDs) != 1 || st.IDs[0] != "reviewer" {
		t.Errorf("IDs = %v, want [\"reviewer\"]", st.IDs)
	}

	_, err = ScopeTargetSingleton("")
	if err == nil {
		t.Error("ScopeTargetSingleton(\"\") should return error")
	}
}

func TestScopeTargetValidRejects(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		st   ScopeTarget
	}{
		{
			name: "zero value (empty kind)",
			st:   ScopeTarget{},
		},
		{
			name: "wildcard with PredicateType",
			st:   ScopeTarget{Kind: ScopeTargetKindWildcard, PredicateType: "foo"},
		},
		{
			name: "wildcard with IDs",
			st:   ScopeTarget{Kind: ScopeTargetKindWildcard, IDs: []string{"a"}},
		},
		{
			name: "predicate with empty PredicateType",
			st:   ScopeTarget{Kind: ScopeTargetKindPredicate, PredicateType: ""},
		},
		{
			name: "predicate with IDs",
			st:   ScopeTarget{Kind: ScopeTargetKindPredicate, PredicateType: "t", IDs: []string{"a"}},
		},
		{
			name: "list with no IDs",
			st:   ScopeTarget{Kind: ScopeTargetKindList},
		},
		{
			name: "list with empty element",
			st:   ScopeTarget{Kind: ScopeTargetKindList, IDs: []string{"a", ""}},
		},
		{
			name: "list with PredicateType",
			st:   ScopeTarget{Kind: ScopeTargetKindList, PredicateType: "t", IDs: []string{"a"}},
		},
		{
			name: "singleton with no IDs",
			st:   ScopeTarget{Kind: ScopeTargetKindSingleton},
		},
		{
			name: "singleton with two IDs",
			st:   ScopeTarget{Kind: ScopeTargetKindSingleton, IDs: []string{"a", "b"}},
		},
		{
			name: "singleton with empty id",
			st:   ScopeTarget{Kind: ScopeTargetKindSingleton, IDs: []string{""}},
		},
		{
			name: "unknown kind",
			st:   ScopeTarget{Kind: "bogus"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.st.Valid() {
				t.Errorf("expected invalid, but Valid() returned true")
			}
		})
	}
}

// --- marshal tests ---

func TestScopeTargetMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		st   ScopeTarget
		want string
	}{
		{
			name: "wildcard",
			st:   ScopeTargetWildcard(),
			want: `"*"`,
		},
		{
			name: "predicate llm_node",
			st:   mustScopeTargetPredicate(t, "llm_node"),
			want: `"node_type:llm_node"`,
		},
		{
			name: "list two ids",
			st:   mustScopeTargetList(t, []string{"role-a", "role-b"}),
			want: `["role-a","role-b"]`,
		},
		{
			name: "list single id",
			st:   mustScopeTargetList(t, []string{"only-role"}),
			want: `["only-role"]`,
		},
		{
			name: "singleton reviewer",
			st:   mustScopeTargetSingleton(t, "reviewer"),
			want: `"reviewer"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tc.st)
			if err != nil {
				t.Fatalf("MarshalJSON error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %s, want %s", string(got), tc.want)
			}
		})
	}

	t.Run("invalid rejects", func(t *testing.T) {
		t.Parallel()
		_, err := json.Marshal(ScopeTarget{})
		if err == nil {
			t.Error("expected error marshaling zero-value ScopeTarget")
		}
	})
}

// --- unmarshal tests ---

func TestScopeTargetUnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    ScopeTarget
		wantErr bool
	}{
		// wildcard
		{
			name:  "wildcard bare string",
			input: `{"scope_target":"*"}`,
			want:  ScopeTargetWildcard(),
		},
		// predicate
		{
			name:  "predicate llm_node",
			input: `{"scope_target":"node_type:llm_node"}`,
			want:  mustScopeTargetPredicate(t, "llm_node"),
		},
		{
			name:  "predicate worker",
			input: `{"scope_target":"node_type:worker"}`,
			want:  mustScopeTargetPredicate(t, "worker"),
		},
		// list
		{
			name:  "list two elements",
			input: `{"scope_target":["role-a","role-b"]}`,
			want:  mustScopeTargetList(t, []string{"role-a", "role-b"}),
		},
		{
			name:  "list single element",
			input: `{"scope_target":["only-role"]}`,
			want:  mustScopeTargetList(t, []string{"only-role"}),
		},
		{
			name:  "list three elements",
			input: `{"scope_target":["id-1","id-2","id-3"]}`,
			want:  mustScopeTargetList(t, []string{"id-1", "id-2", "id-3"}),
		},
		// singleton
		{
			name:  "singleton reviewer",
			input: `{"scope_target":"reviewer"}`,
			want:  mustScopeTargetSingleton(t, "reviewer"),
		},
		{
			name:  "singleton run-id like string",
			input: `{"scope_target":"run-abc123"}`,
			want:  mustScopeTargetSingleton(t, "run-abc123"),
		},
		// error cases
		{
			name:    "empty string rejected",
			input:   `{"scope_target":""}`,
			wantErr: true,
		},
		{
			name:    "empty list rejected",
			input:   `{"scope_target":[]}`,
			wantErr: true,
		},
		{
			name:    "list with empty element rejected",
			input:   `{"scope_target":["a",""]}`,
			wantErr: true,
		},
		{
			name:    "numeric value rejected",
			input:   `{"scope_target":42}`,
			wantErr: true,
		},
		{
			name:    "null rejected",
			input:   `{"scope_target":null}`,
			wantErr: true,
		},
		{
			name:    "object rejected",
			input:   `{"scope_target":{"kind":"wildcard"}}`,
			wantErr: true,
		},
		{
			name:    "predicate with empty type rejected",
			input:   `{"scope_target":"node_type:"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w scopeTargetFixtureWrapper
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
			if w.Target.Kind != tc.want.Kind {
				t.Errorf("Kind: got %q, want %q", w.Target.Kind, tc.want.Kind)
			}
			if w.Target.PredicateType != tc.want.PredicateType {
				t.Errorf("PredicateType: got %q, want %q", w.Target.PredicateType, tc.want.PredicateType)
			}
			if len(w.Target.IDs) != len(tc.want.IDs) {
				t.Errorf("IDs length: got %d, want %d", len(w.Target.IDs), len(tc.want.IDs))
				return
			}
			for i := range tc.want.IDs {
				if w.Target.IDs[i] != tc.want.IDs[i] {
					t.Errorf("IDs[%d]: got %q, want %q", i, w.Target.IDs[i], tc.want.IDs[i])
				}
			}
		})
	}
}

// --- round-trip tests ---

func TestScopeTargetRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []ScopeTarget{
		ScopeTargetWildcard(),
		mustScopeTargetPredicate(t, "llm_node"),
		mustScopeTargetPredicate(t, "gate_node"),
		mustScopeTargetList(t, []string{"role-a", "role-b"}),
		mustScopeTargetList(t, []string{"only"}),
		mustScopeTargetSingleton(t, "reviewer"),
		mustScopeTargetSingleton(t, "run-abc123"),
	}

	for _, orig := range cases {
		data, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("MarshalJSON(%v) error: %v", orig.Kind, err)
		}
		var roundTripped ScopeTarget
		if err := json.Unmarshal(data, &roundTripped); err != nil {
			t.Fatalf("UnmarshalJSON(%s) error: %v", string(data), err)
		}
		if !roundTripped.Valid() {
			t.Errorf("round-tripped ScopeTarget invalid: %v", roundTripped)
		}
		if orig.Kind != roundTripped.Kind {
			t.Errorf("Kind mismatch: orig %q, got %q", orig.Kind, roundTripped.Kind)
		}
		if orig.PredicateType != roundTripped.PredicateType {
			t.Errorf("PredicateType mismatch: orig %q, got %q", orig.PredicateType, roundTripped.PredicateType)
		}
		if len(orig.IDs) != len(roundTripped.IDs) {
			t.Errorf("IDs length mismatch: orig %d, got %d", len(orig.IDs), len(roundTripped.IDs))
			continue
		}
		for i := range orig.IDs {
			if orig.IDs[i] != roundTripped.IDs[i] {
				t.Errorf("IDs[%d] mismatch: orig %q, got %q", i, orig.IDs[i], roundTripped.IDs[i])
			}
		}
	}
}

// --- test helpers ---

func mustScopeTargetPredicate(t *testing.T, nodeType string) ScopeTarget {
	t.Helper()
	st, err := ScopeTargetPredicate(nodeType)
	if err != nil {
		t.Fatalf("ScopeTargetPredicate(%q): %v", nodeType, err)
	}
	return st
}

func mustScopeTargetList(t *testing.T, ids []string) ScopeTarget {
	t.Helper()
	st, err := ScopeTargetList(ids)
	if err != nil {
		t.Fatalf("ScopeTargetList(%v): %v", ids, err)
	}
	return st
}

func mustScopeTargetSingleton(t *testing.T, id string) ScopeTarget {
	t.Helper()
	st, err := ScopeTargetSingleton(id)
	if err != nil {
		t.Fatalf("ScopeTargetSingleton(%q): %v", id, err)
	}
	return st
}
