package core

import (
	"encoding/json"
	"errors"
	"fmt"
)

// RoleStatus discriminates mvh-required roles (CP-029 — concrete permission
// defaults) from declared-but-deferred roles (CP-030 — empty shells pending
// implementation).
//
// Spec: specs/control-points.md §6.2 RECORD Role field status.
type RoleStatus string

const (
	// RoleStatusMVHRequired indicates the role is fully specified and MUST carry
	// a concrete PermissionSchema per CP-029.
	RoleStatusMVHRequired RoleStatus = "mvh-required"

	// RoleStatusDeclaredButDeferred indicates the role is declared as an empty
	// shell, with permission schema left blank pending implementation per CP-030.
	RoleStatusDeclaredButDeferred RoleStatus = "declared-but-deferred"
)

// ErrInvalidRoleStatus is returned by RoleStatus.Valid and Role.Validate when
// the status value is not one of the two normative values.
var ErrInvalidRoleStatus = errors.New("invalid role status: must be mvh-required or declared-but-deferred")

// Valid reports whether s is one of the two normative RoleStatus values.
//
// Rules per specs/control-points.md §6.2:
//   - Only "mvh-required" and "declared-but-deferred" are valid.
func (s RoleStatus) Valid() bool {
	return s == RoleStatusMVHRequired || s == RoleStatusDeclaredButDeferred
}

// UnmarshalJSON implements json.Unmarshaler so that any JSON string value is
// validated against the normative enum before being accepted.
func (s *RoleStatus) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("RoleStatus: %w", err)
	}
	candidate := RoleStatus(raw)
	if !candidate.Valid() {
		return fmt.Errorf("%w: got %q", ErrInvalidRoleStatus, raw)
	}
	*s = candidate
	return nil
}

// Role is the in-memory representation of the §6.2 RECORD Role, combining the
// role name, its permission schema, and its lifecycle status.
//
// # Status semantics
//
// A role with status mvh-required MUST carry a fully-populated PermissionSchema
// (CP-029 concrete defaults). A role with status declared-but-deferred carries
// an empty PermissionSchema shell (CP-030 empty shells); enforcement of
// "non-empty" constraints lives in the policy validator, not at the type level.
//
// # Construction
//
// Use NewRole to construct a Role with the PermissionSchema default applied
// (ReadablePaths = ["**"] per §6.2). Struct-literal construction is allowed but
// callers MUST set PermissionSchema.ReadablePaths explicitly.
//
// Spec: specs/control-points.md §6.2 RECORD Role.
type Role struct {
	// Name is the role name per architecture.md §4.8.
	// Spec: specs/control-points.md §6.2 RECORD Role field name.
	Name RoleName `json:"name"`

	// PermissionSchema carries the tool and path allowances for this role.
	// Spec: specs/control-points.md §6.2 RECORD Role field permission_schema.
	PermissionSchema PermissionSchema `json:"permission_schema"`

	// Status discriminates mvh-required (CP-029) from declared-but-deferred
	// (CP-030).
	// Spec: specs/control-points.md §6.2 RECORD Role field status.
	Status RoleStatus `json:"status"`
}

// NewRole returns a Role with the given name and status, with the
// PermissionSchema default applied (ReadablePaths = ["**"] per §6.2).
//
// The caller is responsible for populating the returned Role's PermissionSchema
// fields beyond the ReadablePaths default.
func NewRole(name RoleName, status RoleStatus) Role {
	return Role{
		Name:             name,
		PermissionSchema: NewPermissionSchema(),
		Status:           status,
	}
}

// Validate reports the first invariant violation on r, or nil when r is valid.
//
// Rules enforced:
//   - Name must be non-empty (RoleName.Valid).
//   - Status must be one of the two normative values (RoleStatus.Valid).
//
// PermissionSchema content validation (e.g. CP-031 beads-cli requirement for
// mvh-required roles) is the responsibility of the policy validator, not this
// method.
func (r Role) Validate() error {
	if !r.Name.Valid() {
		return errors.New("Role.Name must be non-empty")
	}
	if !r.Status.Valid() {
		return fmt.Errorf("%w: got %q", ErrInvalidRoleStatus, r.Status)
	}
	return nil
}
