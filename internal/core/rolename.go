package core

// RoleName is a typed alias for a role name string referenced in a
// PermissionSchema (specs/control-points.md §6.2 RECORD PermissionSchema
// field invocable_by).
//
// A RoleName identifies an agent role permitted to spawn the role that carries
// the PermissionSchema. An empty RoleName is invalid; Valid() returns false for
// the zero value.
//
// RoleName is also the type used for Role.name per §6.2 RECORD Role field
// name; that substitution occurs when the Role record is implemented.
type RoleName string

// Valid reports whether r is a non-empty role name.
//
// Rules per specs/control-points.md §6.2:
//   - RoleName must be non-empty.
func (r RoleName) Valid() bool {
	return r != ""
}
