package core

// ToolName is a typed alias for a tool name string referenced in a
// PermissionSchema (specs/control-points.md §6.2 RECORD PermissionSchema
// field allowed_tools).
//
// A ToolName identifies a single tool that a Role may invoke. An empty
// ToolName is invalid; Valid() returns false for the zero value.
type ToolName string

// Valid reports whether t is a non-empty tool name.
//
// Rules per specs/control-points.md §6.2:
//   - ToolName must be non-empty.
func (t ToolName) Valid() bool {
	return t != ""
}
