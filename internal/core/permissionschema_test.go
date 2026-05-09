package core

import (
	"encoding/json"
	"testing"
)

// permissionSchemaFixture returns a fully-populated PermissionSchema with all
// fields set to non-zero values, suitable for structural tests (hk-a8bg.79).
func permissionSchemaFixture(t *testing.T) PermissionSchema {
	t.Helper()

	tier := "standard"
	return PermissionSchema{
		AllowedTools:  []string{"bash", "read"},
		WritablePaths: []string{"output/**"},
		ReadablePaths: []string{"**"},
		ModelTier:     &tier,
		DefaultSkills: []string{"beads-cli"},
		AllowedHooks:  []string{"pre-node-entry"},
		InvocableBy:   []string{"orchestrator"},
	}
}

// TestNewPermissionSchema_DefaultReadablePaths verifies that NewPermissionSchema
// sets ReadablePaths to ["**"] per specs/control-points.md §6.2.
func TestNewPermissionSchema_DefaultReadablePaths(t *testing.T) {
	t.Parallel()

	ps := NewPermissionSchema()
	if len(ps.ReadablePaths) != 1 || ps.ReadablePaths[0] != "**" {
		t.Errorf("NewPermissionSchema().ReadablePaths = %v, want [\"**\"]", ps.ReadablePaths)
	}
}

// TestNewPermissionSchema_ListsNonNil verifies that all list fields returned by
// NewPermissionSchema are non-nil (empty but appendable).
func TestNewPermissionSchema_ListsNonNil(t *testing.T) {
	t.Parallel()

	ps := NewPermissionSchema()
	if ps.AllowedTools == nil {
		t.Error("NewPermissionSchema().AllowedTools is nil, want non-nil empty slice")
	}
	if ps.WritablePaths == nil {
		t.Error("NewPermissionSchema().WritablePaths is nil, want non-nil empty slice")
	}
	if ps.DefaultSkills == nil {
		t.Error("NewPermissionSchema().DefaultSkills is nil, want non-nil empty slice")
	}
	if ps.AllowedHooks == nil {
		t.Error("NewPermissionSchema().AllowedHooks is nil, want non-nil empty slice")
	}
	if ps.InvocableBy == nil {
		t.Error("NewPermissionSchema().InvocableBy is nil, want non-nil empty slice")
	}
}

// TestNewPermissionSchema_ModelTierNil verifies that NewPermissionSchema leaves
// ModelTier nil (no tier constraint by default).
func TestNewPermissionSchema_ModelTierNil(t *testing.T) {
	t.Parallel()

	ps := NewPermissionSchema()
	if ps.ModelTier != nil {
		t.Errorf("NewPermissionSchema().ModelTier = %v, want nil", ps.ModelTier)
	}
}

// TestPermissionSchema_JSONRoundTrip verifies that a fully-populated
// PermissionSchema survives a JSON marshal/unmarshal round-trip with all fields
// intact (specs/control-points.md §6.3 wire shape).
func TestPermissionSchema_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	orig := permissionSchemaFixture(t)
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got PermissionSchema
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.ModelTier == nil || *got.ModelTier != *orig.ModelTier {
		t.Errorf("ModelTier round-trip: got %v, want %v", got.ModelTier, orig.ModelTier)
	}
	if len(got.AllowedTools) != len(orig.AllowedTools) {
		t.Errorf("AllowedTools length: got %d, want %d", len(got.AllowedTools), len(orig.AllowedTools))
	}
	if len(got.WritablePaths) != len(orig.WritablePaths) {
		t.Errorf("WritablePaths length: got %d, want %d", len(got.WritablePaths), len(orig.WritablePaths))
	}
	if len(got.ReadablePaths) != 1 || got.ReadablePaths[0] != "**" {
		t.Errorf("ReadablePaths: got %v, want [\"**\"]", got.ReadablePaths)
	}
	if len(got.DefaultSkills) != len(orig.DefaultSkills) || got.DefaultSkills[0] != "beads-cli" {
		t.Errorf("DefaultSkills: got %v, want %v", got.DefaultSkills, orig.DefaultSkills)
	}
	if len(got.AllowedHooks) != len(orig.AllowedHooks) {
		t.Errorf("AllowedHooks length: got %d, want %d", len(got.AllowedHooks), len(orig.AllowedHooks))
	}
	if len(got.InvocableBy) != len(orig.InvocableBy) {
		t.Errorf("InvocableBy length: got %d, want %d", len(got.InvocableBy), len(orig.InvocableBy))
	}
}

// TestPermissionSchema_JSONOmitsModelTierWhenNil verifies that when ModelTier
// is nil the JSON output omits the model_tier key entirely (omitempty).
func TestPermissionSchema_JSONOmitsModelTierWhenNil(t *testing.T) {
	t.Parallel()

	ps := NewPermissionSchema()
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["model_tier"]; ok {
		t.Error("model_tier key present in JSON when ModelTier is nil, want omitted")
	}
}

// TestPermissionSchema_JSONIncludesModelTierWhenSet verifies that when ModelTier
// is non-nil it appears in the JSON output.
func TestPermissionSchema_JSONIncludesModelTierWhenSet(t *testing.T) {
	t.Parallel()

	ps := permissionSchemaFixture(t)
	data, err := json.Marshal(ps)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal to map: %v", err)
	}
	if _, ok := m["model_tier"]; !ok {
		t.Error("model_tier key absent from JSON when ModelTier is set, want present")
	}
}

// TestPermissionSchema_ZeroValueReadablePathsIsNil confirms that a zero-value
// PermissionSchema (struct literal without NewPermissionSchema) has a nil
// ReadablePaths, documenting the need to use NewPermissionSchema for the default.
func TestPermissionSchema_ZeroValueReadablePathsIsNil(t *testing.T) {
	t.Parallel()

	var ps PermissionSchema
	if ps.ReadablePaths != nil {
		t.Errorf("zero-value PermissionSchema.ReadablePaths = %v, want nil (use NewPermissionSchema for default)", ps.ReadablePaths)
	}
}
