package core

// PathGlob is a typed alias for a workspace-relative glob string referenced
// in a PermissionSchema (specs/control-points.md §6.2 RECORD PermissionSchema
// fields writable_paths and readable_paths).
//
// A PathGlob identifies a set of workspace paths via glob syntax. The value
// "**" matches all paths (the default for readable_paths per §6.2). An empty
// PathGlob is invalid; Valid() returns false for the zero value.
type PathGlob string

// Valid reports whether p is a non-empty workspace-relative glob.
//
// Rules per specs/control-points.md §6.2:
//   - PathGlob must be non-empty.
func (p PathGlob) Valid() bool {
	return p != ""
}
