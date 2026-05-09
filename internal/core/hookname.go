package core

// HookName is a typed alias for a Hook name string referenced in a
// PermissionSchema (specs/control-points.md §6.2 RECORD PermissionSchema
// field allowed_hooks).
//
// A HookName identifies a Hook ControlPoint that may modify a Role's behavior.
// An empty HookName is invalid; Valid() returns false for the zero value.
type HookName string

// Valid reports whether h is a non-empty hook name.
//
// Rules per specs/control-points.md §6.2:
//   - HookName must be non-empty.
func (h HookName) Valid() bool {
	return h != ""
}
