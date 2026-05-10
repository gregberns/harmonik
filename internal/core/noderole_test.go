package core

import (
	"encoding/json"
	"testing"
)

// idemDefaultFixtureIdempotentRoles returns the set of NodeRole values that
// must map to IdempotencyClassIdempotent per EM-010.
// Helper prefix: idemDefaultFixture (bead hk-b3f.10).
func idemDefaultFixtureIdempotentRoles() []NodeRole {
	return []NodeRole{
		NodeRoleReviewer,
		NodeRoleResearcher,
		NodeRoleLint,
		NodeRoleTest,
		NodeRoleTypecheck,
		NodeRoleAnalysis,
	}
}

// idemDefaultFixtureNonIdempotentRoles returns the set of NodeRole values that
// must map to IdempotencyClassNonIdempotent per EM-010.
func idemDefaultFixtureNonIdempotentRoles() []NodeRole {
	return []NodeRole{
		NodeRoleBuilder,
		NodeRoleMerge,
	}
}

func TestNodeRoleValid(t *testing.T) {
	t.Parallel()

	valid := []NodeRole{
		NodeRoleReviewer,
		NodeRoleResearcher,
		NodeRoleLint,
		NodeRoleTest,
		NodeRoleTypecheck,
		NodeRoleAnalysis,
		NodeRoleBuilder,
		NodeRoleMerge,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	invalid := []NodeRole{
		"",
		"Reviewer",
		"BUILDER",
		"planner",
		"scheduler",
		"unknown",
		"linter",
		"tester",
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}

func TestNodeRoleMarshalText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		role NodeRole
		want string
	}{
		{NodeRoleReviewer, "reviewer"},
		{NodeRoleResearcher, "researcher"},
		{NodeRoleLint, "lint"},
		{NodeRoleTest, "test"},
		{NodeRoleTypecheck, "typecheck"},
		{NodeRoleAnalysis, "analysis"},
		{NodeRoleBuilder, "builder"},
		{NodeRoleMerge, "merge"},
	}

	for _, tc := range cases {
		got, err := tc.role.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%q) error: %v", tc.role, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("MarshalText(%q) = %q, want %q", tc.role, string(got), tc.want)
		}
	}

	if _, err := NodeRole("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid NodeRole")
	}
}

func TestNodeRoleUnmarshalText(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Role NodeRole `json:"role"`
	}

	tests := []struct {
		name    string
		input   string
		want    NodeRole
		wantErr bool
	}{
		{name: "reviewer", input: `{"role":"reviewer"}`, want: NodeRoleReviewer},
		{name: "researcher", input: `{"role":"researcher"}`, want: NodeRoleResearcher},
		{name: "lint", input: `{"role":"lint"}`, want: NodeRoleLint},
		{name: "test", input: `{"role":"test"}`, want: NodeRoleTest},
		{name: "typecheck", input: `{"role":"typecheck"}`, want: NodeRoleTypecheck},
		{name: "analysis", input: `{"role":"analysis"}`, want: NodeRoleAnalysis},
		{name: "builder", input: `{"role":"builder"}`, want: NodeRoleBuilder},
		{name: "merge", input: `{"role":"merge"}`, want: NodeRoleMerge},
		{name: "invalid uppercase", input: `{"role":"Reviewer"}`, wantErr: true},
		{name: "invalid empty", input: `{"role":""}`, wantErr: true},
		{name: "invalid unknown", input: `{"role":"planner"}`, wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var w wrapper
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
			if w.Role != tc.want {
				t.Errorf("got %q, want %q", w.Role, tc.want)
			}
		})
	}
}

// TestDefaultIdempotencyClassForNodeRole_IdempotentRoles verifies that
// reviewer, researcher, lint, test, typecheck, and analysis roles default to
// IdempotencyClassIdempotent per EM-010.
func TestDefaultIdempotencyClassForNodeRole_IdempotentRoles(t *testing.T) {
	t.Parallel()

	for _, role := range idemDefaultFixtureIdempotentRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			got, ok := DefaultIdempotencyClassForNodeRole(role)
			if !ok {
				t.Errorf("DefaultIdempotencyClassForNodeRole(%q): ok=false, want true", role)
				return
			}
			if got != IdempotencyClassIdempotent {
				t.Errorf("DefaultIdempotencyClassForNodeRole(%q) = %q, want %q",
					role, got, IdempotencyClassIdempotent)
			}
		})
	}
}

// TestDefaultIdempotencyClassForNodeRole_NonIdempotentRoles verifies that
// builder and merge roles default to IdempotencyClassNonIdempotent per EM-010.
func TestDefaultIdempotencyClassForNodeRole_NonIdempotentRoles(t *testing.T) {
	t.Parallel()

	for _, role := range idemDefaultFixtureNonIdempotentRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			got, ok := DefaultIdempotencyClassForNodeRole(role)
			if !ok {
				t.Errorf("DefaultIdempotencyClassForNodeRole(%q): ok=false, want true", role)
				return
			}
			if got != IdempotencyClassNonIdempotent {
				t.Errorf("DefaultIdempotencyClassForNodeRole(%q) = %q, want %q",
					role, got, IdempotencyClassNonIdempotent)
			}
		})
	}
}

// TestDefaultIdempotencyClassForNodeRole_UnknownRole verifies that unknown
// roles return ok=false and no class is assumed.
func TestDefaultIdempotencyClassForNodeRole_UnknownRole(t *testing.T) {
	t.Parallel()

	unknowns := []NodeRole{
		"",
		"planner",
		"verifier",
		"scheduler",
		"governor",
		"unknown-role",
	}

	for _, role := range unknowns {
		role := role
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()
			_, ok := DefaultIdempotencyClassForNodeRole(role)
			if ok {
				t.Errorf("DefaultIdempotencyClassForNodeRole(%q): ok=true for unknown role, want false", role)
			}
		})
	}
}
