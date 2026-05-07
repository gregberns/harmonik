package core

import "testing"

func TestWorkflowVersion_NominalTyping(t *testing.T) {
	v := WorkflowVersion("1.0.0")
	if string(v) != "1.0.0" {
		t.Errorf("string(WorkflowVersion) = %q, want %q", string(v), "1.0.0")
	}
}

func TestWorkflowVersion_ZeroValue(t *testing.T) {
	var v WorkflowVersion
	if v != "" {
		t.Errorf("zero WorkflowVersion = %q, want empty string", v)
	}
}

func TestWorkflowVersion_Equality(t *testing.T) {
	a := WorkflowVersion("1.2.3")
	b := WorkflowVersion("1.2.3")
	c := WorkflowVersion("2.0.0")

	if a != b {
		t.Errorf("equal WorkflowVersions should be ==: %q != %q", a, b)
	}

	if a == c {
		t.Errorf("different WorkflowVersions should not be ==: %q == %q", a, c)
	}
}
