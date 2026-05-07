package core

import "testing"

func TestWorkspaceRef_NominalTyping(t *testing.T) {
	ref := WorkspaceRef("workspace://project/input")
	if string(ref) != "workspace://project/input" {
		t.Errorf("string(WorkspaceRef) = %q, want %q", string(ref), "workspace://project/input")
	}
}

func TestWorkspaceRef_ZeroValue(t *testing.T) {
	var ref WorkspaceRef
	if ref != "" {
		t.Errorf("zero WorkspaceRef = %q, want empty string", ref)
	}
}

func TestWorkspaceRef_Equality(t *testing.T) {
	a := WorkspaceRef("workspace://project/alpha")
	b := WorkspaceRef("workspace://project/alpha")
	c := WorkspaceRef("workspace://project/beta")

	if a != b {
		t.Errorf("equal WorkspaceRefs should be ==: %q != %q", a, b)
	}

	if a == c {
		t.Errorf("different WorkspaceRefs should not be ==: %q == %q", a, c)
	}
}
