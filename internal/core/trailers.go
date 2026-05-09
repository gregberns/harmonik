// Package core holds shared types that cross subsystem boundaries.
package core

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

// trailerRegistry is the ordered, authoritative list of all known checkpoint-commit
// trailers. Declared order is the canonical iteration order for RegistryEntries().
//
// The seven primary registry rows are defined by execution-model §6.2.
// Harmonik-Verdict-Executed is a known extension owned by the reconciliation spec;
// its inclusion here ensures trailer-lint never rejects it as unknown.
//
// Role of trailers (EM-017): trailers are a cheap index for git-log scanning.
// They are NOT the authoritative source of truth for transition data; authoritative
// fields live in the transition-record sibling file per §4.4.EM-018
// (`.harmonik/transitions/<run_id>/<transition_id>.json`). Readers that need
// complete field data MUST retrieve the sibling file, not parse trailers.
var trailerRegistry = []TrailerSpec{
	{
		// execution-model.md §6.2; EM-017 (required trailer)
		Key:         "Harmonik-Bead-ID",
		Type:        TrailerTypeString,
		Requirement: TrailerConditional,
		OwnerSpec:   "execution-model",
		Description: "Bead identifier; MUST be present when the run is tied to a bead per EM-014, absent otherwise.",
	},
	{
		// execution-model.md §6.2; EM-017 (required trailer)
		Key:         "Harmonik-Run-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the run; present on every checkpoint commit.",
	},
	{
		// execution-model.md §6.2; EM-017 (required trailer)
		Key:         "Harmonik-Schema-Version",
		Type:        TrailerTypeInteger,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "Integer version of the transition-record sibling file schema; must match sibling file's schema_version field per EM-018.",
	},
	{
		// execution-model.md §6.2; EM-017 (required trailer)
		Key:         "Harmonik-State-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the run state after the transition; present on every checkpoint commit.",
	},
	{
		// execution-model.md §6.2; conditional, RC-owned
		// Payload semantics live in reconciliation per EM v0.3.3 §6.2 informative
		// ownership annotation (discipline v0.7 §2.11(d.1)).
		Key:         "Harmonik-Target-Run-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerConditional,
		OwnerSpec:   "reconciliation",
		Description: "UUIDv7 of the run being reconciled; MUST be present when Harmonik-Workflow-Class=reconciliation, absent otherwise.",
	},
	{
		// execution-model.md §6.2; EM-017 (required trailer)
		Key:         "Harmonik-Transition-ID",
		Type:        TrailerTypeUUID,
		Requirement: TrailerRequired,
		OwnerSpec:   "execution-model",
		Description: "UUIDv7 identifying the specific transition recorded by this commit; present on every checkpoint commit.",
	},
	{
		// execution-model.md §6.2; conditional, RC-owned (see §6.2 informative annotation)
		// Payload semantics live in reconciliation per EM v0.3.3 §6.2 informative
		// ownership annotation (discipline v0.7 §2.11(d.1)).
		Key:         "Harmonik-Workflow-Class",
		Type:        TrailerTypeEnum,
		Requirement: TrailerConditional,
		OwnerSpec:   "reconciliation",
		EnumValues:  []string{"reconciliation"},
		Description: "Workflow class; MUST be present on reconciliation-workflow checkpoint commits. Enum values at MVH: {reconciliation}.",
	},
	{
		// Known extension owned by the reconciliation spec.
		// Type is TrailerTypeString: no canonical value structure is defined by the
		// registry; the reconciliation spec governs value semantics.
		// Included so trailer-lint does not flag it as an unknown trailer.
		Key:         "Harmonik-Verdict-Executed",
		Type:        TrailerTypeString,
		Requirement: TrailerKnownExtension,
		OwnerSpec:   "reconciliation",
		Description: "Reconciliation verdict execution marker; present on verdict commits per the reconciliation spec.",
	},
}

// trailerIndex is a map built at init time for O(1) lookup.
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
