// Package core holds shared types that cross subsystem boundaries.
package core

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// TrailerValueType describes the semantic type of a checkpoint-commit trailer value.
// Used by trailer-lint to validate the shape of each trailer's value.
type TrailerValueType int

const (
	// TrailerTypeUUID indicates the value must be a UUID string
	// (typically UUIDv7 per event-model.md §4.1 EV-002).
	TrailerTypeUUID TrailerValueType = iota

	// TrailerTypeInteger indicates the value must be a decimal integer string.
	TrailerTypeInteger

	// TrailerTypeString indicates the value is an opaque string with no further
	// structural constraint enforced at the registry level.
	TrailerTypeString

	// TrailerTypeEnum indicates the value must be one of the strings in EnumValues.
	TrailerTypeEnum
)

// TrailerRequirement classifies whether a trailer is required on every checkpoint
// commit, conditionally required (present in specific circumstances only), or a
// known extension owned by another subsystem.
type TrailerRequirement int

const (
	// TrailerRequired means the trailer MUST appear on every checkpoint commit.
	// Absence is a lint violation.
	TrailerRequired TrailerRequirement = iota

	// TrailerConditional means the trailer MUST appear when a specific condition
	// holds (documented in TrailerSpec.Description) and MUST be absent otherwise.
	// Presence without the triggering condition is equally a lint violation.
	TrailerConditional

	// TrailerKnownExtension means the trailer is not required but is a recognised
	// extension owned by a non-execution-model subsystem. Trailer-lint MUST NOT
	// flag it as an unknown trailer; value validation is the owner's responsibility.
	TrailerKnownExtension
)

// TrailerSpec describes a single entry in the checkpoint-commit trailer registry.
// The registry is authoritative for trailer-lint: any trailer key NOT present in
// the registry is a lint violation.
//
// See execution-model.md §6.2 for the normative trailer contract; EM-017 lists
// the four unconditionally required trailers, with Bead-ID as the EM-017
// conditional trailer.
type TrailerSpec struct {
	// Key is the canonical trailer name as it appears in the git commit message,
	// e.g. "Harmonik-Run-ID".
	Key string

	// Type is the semantic type of the trailer value.
	Type TrailerValueType

	// Requirement classifies when this trailer must be present.
	Requirement TrailerRequirement

	// OwnerSpec is the spec file (without extension) that owns the semantics of
	// this trailer, e.g. "execution-model" or "reconciliation".
	OwnerSpec string

	// EnumValues lists the permitted values when Type == TrailerTypeEnum.
	// Nil or empty for all other types.
	EnumValues []string

	// Description is a short human-readable note (one line) summarising the
	// trailer's purpose and, for conditional trailers, the triggering condition.
	Description string
}

var trailerRegistry = []TrailerSpec{
	{
		Key:         "Harmonik-Bead-ID",
		Type:        TrailerTypeString,
		Requirement: TrailerConditional,
		OwnerSpec:   "execution-model",
		Description: "Bead identifier; MUST be present when the run is tied to a bead per EM-014, absent otherwise.",
	},
	{
		Key:         "Harmonik-Run-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the run; present on every checkpoint commit (EM-013 join key across git, Beads, and JSONL).",
	},
	{
		Key:         "Harmonik-Schema-Version",
		Type:        TrailerTypeInteger,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "Integer version of the transition-record sibling file schema; must match sibling file's schema_version field per EM-018; N-1 readable per EM-022.",
	},
	{
		Key:         "Harmonik-State-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the run state after the transition; present on every checkpoint commit.",
	},
	{
		Key:         "Harmonik-Target-Run-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerConditional,
		OwnerSpec:   "reconciliation",
		Description: "UUIDv7 of the run being reconciled; MUST be present when Harmonik-Workflow-Class=reconciliation, absent otherwise.",
	},
	{
		Key:         "Harmonik-Transition-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the specific transition recorded by this commit; present on every checkpoint commit.",
	},
	{
		Key:         "Harmonik-Workflow-Class",
		Type:        TrailerTypeEnum,
		Requirement: TrailerConditional,
		OwnerSpec:   "reconciliation",
		EnumValues:  []string{"reconciliation"},
		Description: "Workflow class; MUST be present on reconciliation-workflow checkpoint commits. Enum values: {reconciliation}.",
	},
	{
		Key:         "Harmonik-Verdict-Executed",
		Type:        TrailerTypeEnum,
		Requirement: TrailerKnownExtension,
		OwnerSpec:   "reconciliation",
		EnumValues:  []string{"true"},
		Description: "Reconciliation verdict execution marker; value is the fixed literal \"true\" (RC-023); presence marks execution (marker-only semantics per schemas.md §6.4).",
	},
}

var trailerIndex map[string]TrailerSpec

func init() {
	trailerIndex = make(map[string]TrailerSpec, len(trailerRegistry))
	for _, spec := range trailerRegistry {
		trailerIndex[spec.Key] = spec
	}
}

// LookupTrailer returns the TrailerSpec for the given trailer key and true if the
// key is in the registry, or a zero TrailerSpec and false if it is not.
//
// The lookup is exact-match (case-sensitive) on the canonical trailer key.
func LookupTrailer(key string) (TrailerSpec, bool) {
	spec, ok := trailerIndex[key]
	return spec, ok
}

// IsKnownTrailer reports whether the given trailer key is present in the registry.
// A trailer that is not known is a lint violation per execution-model §6.2.
func IsKnownTrailer(key string) bool {
	_, ok := trailerIndex[key]
	return ok
}

// RegistryEntries returns the full registry as a slice in stable declared order.
// Callers that need deterministic iteration (e.g. lint reporting, documentation
// generation) MUST use this function rather than ranging over an internal map.
//
// The returned slice is a copy; callers MUST NOT modify it.
func RegistryEntries() []TrailerSpec {
	out := make([]TrailerSpec, len(trailerRegistry))
	copy(out, trailerRegistry)
	return out
}

// ValidateTrailerValue enforces the trailer-value contract declared by spec at runtime.
//
// Rules per trailer type:
//   - TrailerTypeUUID: value must parse as a UUID and be version 7
//     (UUIDv7 per EM-013; execution-model.md §4.4 EM-013).
//   - TrailerTypeInteger: value must parse as a decimal integer string
//     (strconv.ParseInt base 10).
//   - TrailerTypeEnum: value must be present in spec.EnumValues (exact match).
//   - TrailerTypeString: any non-empty string is accepted.
//
// An empty value is always an error regardless of type, because an absent
// trailer must be represented by omission, not by an empty-string value.
//
// This helper unblocks the trailer-lint layer and allows the registry's
// EnumValues=["true"] declaration on Harmonik-Verdict-Executed to enforce the
// RC-023 malformed-value rule (schemas.md §6.4).
//
// Spec ref: execution-model.md §6.2; reconciliation/schemas.md §6.4; RC-023.
func ValidateTrailerValue(spec TrailerSpec, value string) error {
	if value == "" {
		return fmt.Errorf("trailer %q: value must not be empty", spec.Key)
	}
	switch spec.Type {
	case TrailerTypeUUID:
		u, err := uuid.Parse(value)
		if err != nil {
			return fmt.Errorf("trailer %q: value %q is not a valid UUID: %w", spec.Key, value, err)
		}
		if u.Version() != 7 {
			return fmt.Errorf("trailer %q: value %q is UUID version %d, want version 7 (UUIDv7 per EM-013)", spec.Key, value, u.Version())
		}
		return nil
	case TrailerTypeInteger:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("trailer %q: value %q is not a decimal integer: %w", spec.Key, value, err)
		}
		return nil
	case TrailerTypeEnum:
		for _, permitted := range spec.EnumValues {
			if value == permitted {
				return nil
			}
		}
		return fmt.Errorf("trailer %q: value %q not in permitted values [%s]",
			spec.Key, value, strings.Join(spec.EnumValues, ", "))
	case TrailerTypeString:
		return nil
	default:
		return fmt.Errorf("trailer %q: unknown TrailerValueType %d", spec.Key, spec.Type)
	}
}
