package core

import "fmt"

// SchemaChangeKind classifies a schema evolution change according to the
// breaking-change table in event-model.md §6.4 (EV-030).
//
// Additive changes (AddOptionalField, WidenType, AddEnumVariant) are
// non-breaking; all others are breaking and require a migration release
// scheduled at an operator pause per operator-nfr.md §4.3.
//
// Spec ref: event-model.md §4.7 EV-030; §6.4 breaking-change table.
// Bead ref: hk-hqwn.39.
type SchemaChangeKind string

// SchemaChangeKind values — the nine rows of the §6.4 breaking-change table.
const (
	// SchemaChangeAddOptionalField is a new optional field added to a payload
	// or envelope. Non-breaking: older readers accept and ignore unknown fields.
	SchemaChangeAddOptionalField SchemaChangeKind = "add_optional_field"

	// SchemaChangeAddRequiredField is a new required field added to a payload
	// or envelope. Breaking: older readers fail closed with a typed error on the
	// missing field.
	SchemaChangeAddRequiredField SchemaChangeKind = "add_required_field"

	// SchemaChangeRenameField is a rename of an existing field. Breaking:
	// older readers lose the value at the old name and see an unknown field at
	// the new name. Requires a migration release; no on-the-fly rewrite.
	SchemaChangeRenameField SchemaChangeKind = "rename_field"

	// SchemaChangeRemoveField is the removal of an existing field. Breaking:
	// older readers that depend on the field lose the value. Requires a
	// migration release.
	SchemaChangeRemoveField SchemaChangeKind = "remove_field"

	// SchemaChangeWidenType is a type widening (e.g., int32 → int64). Non-breaking:
	// older readers accept widened values within their type range.
	SchemaChangeWidenType SchemaChangeKind = "widen_type"

	// SchemaChangeNarrowType is a type narrowing (e.g., string → enum). Breaking:
	// older readers may reject values outside the narrowed domain. Requires a
	// migration release.
	SchemaChangeNarrowType SchemaChangeKind = "narrow_type"

	// SchemaChangeAddEnumVariant is a new enum variant added without a required-
	// semantics shift. Non-breaking: older readers see the new variant as
	// "unknown" and MUST treat unknown variants as non-fatal.
	SchemaChangeAddEnumVariant SchemaChangeKind = "add_enum_variant"

	// SchemaChangeRemoveEnumVariant is the removal of an existing enum variant.
	// Breaking: producers that still emit the old variant break older readers
	// expecting it. Requires a migration release.
	SchemaChangeRemoveEnumVariant SchemaChangeKind = "remove_enum_variant"

	// SchemaChangeTightenValidation is the addition of a stricter validation
	// rule (e.g., imposing a required length bound on a formerly unconstrained
	// field). Breaking: values accepted by older writers may be rejected by
	// newer readers. Requires a migration release.
	SchemaChangeTightenValidation SchemaChangeKind = "tighten_validation"
)

// Valid reports whether k is one of the nine declared SchemaChangeKind constants.
func (k SchemaChangeKind) Valid() bool {
	switch k {
	case SchemaChangeAddOptionalField,
		SchemaChangeAddRequiredField,
		SchemaChangeRenameField,
		SchemaChangeRemoveField,
		SchemaChangeWidenType,
		SchemaChangeNarrowType,
		SchemaChangeAddEnumVariant,
		SchemaChangeRemoveEnumVariant,
		SchemaChangeTightenValidation:
		return true
	default:
		return false
	}
}

// IsBreaking reports whether k is a breaking schema change per event-model.md
// §6.4 (EV-030). Breaking changes require a migration release scheduled at an
// operator pause per operator-nfr.md §4.3.
//
// Non-breaking kinds (return false):
//   - SchemaChangeAddOptionalField
//   - SchemaChangeWidenType
//   - SchemaChangeAddEnumVariant
//
// Breaking kinds (return true):
//   - SchemaChangeAddRequiredField
//   - SchemaChangeRenameField
//   - SchemaChangeRemoveField
//   - SchemaChangeNarrowType
//   - SchemaChangeRemoveEnumVariant
//   - SchemaChangeTightenValidation
func (k SchemaChangeKind) IsBreaking() bool {
	switch k {
	case SchemaChangeAddOptionalField,
		SchemaChangeWidenType,
		SchemaChangeAddEnumVariant:
		return false
	case SchemaChangeAddRequiredField,
		SchemaChangeRenameField,
		SchemaChangeRemoveField,
		SchemaChangeNarrowType,
		SchemaChangeRemoveEnumVariant,
		SchemaChangeTightenValidation:
		return true
	default:
		// Unknown kind — conservative: treat as breaking to prevent silent
		// compatibility violations on future additions.
		return true
	}
}

// ReaderObligation returns the reader obligation string from the §6.4 table
// for this change kind.
func (k SchemaChangeKind) ReaderObligation() string {
	switch k {
	case SchemaChangeAddOptionalField:
		return "Accept; ignore unknown fields on older readers."
	case SchemaChangeAddRequiredField:
		return "Older readers fail closed with typed error on missing field."
	case SchemaChangeRenameField:
		return "Migration release; no on-the-fly rewrite."
	case SchemaChangeRemoveField:
		return "Migration release."
	case SchemaChangeWidenType:
		return "Accept widened values."
	case SchemaChangeNarrowType:
		return "Migration release."
	case SchemaChangeAddEnumVariant:
		return "Older readers see the new variant as unknown; handlers MUST treat unknown variants as non-fatal."
	case SchemaChangeRemoveEnumVariant:
		return "Migration release."
	case SchemaChangeTightenValidation:
		return "Migration release."
	default:
		return ""
	}
}

// String implements fmt.Stringer.
func (k SchemaChangeKind) String() string {
	return string(k)
}

// allSchemaChangeKinds is the exhaustive set of nine §6.4 change-kind rows.
// Tests that validate coverage of the breaking-change table use this slice.
var allSchemaChangeKinds = []SchemaChangeKind{
	SchemaChangeAddOptionalField,
	SchemaChangeAddRequiredField,
	SchemaChangeRenameField,
	SchemaChangeRemoveField,
	SchemaChangeWidenType,
	SchemaChangeNarrowType,
	SchemaChangeAddEnumVariant,
	SchemaChangeRemoveEnumVariant,
	SchemaChangeTightenValidation,
}

// ErrUnknownSchemaChangeKind is returned by ParseSchemaChangeKind when the
// input string does not match any declared SchemaChangeKind constant.
var ErrUnknownSchemaChangeKind = fmt.Errorf("core: unknown SchemaChangeKind")

// ParseSchemaChangeKind parses s into a SchemaChangeKind. Returns
// ErrUnknownSchemaChangeKind when s does not match any declared constant.
func ParseSchemaChangeKind(s string) (SchemaChangeKind, error) {
	k := SchemaChangeKind(s)
	if !k.Valid() {
		return "", fmt.Errorf("%w: %q", ErrUnknownSchemaChangeKind, s)
	}
	return k, nil
}
