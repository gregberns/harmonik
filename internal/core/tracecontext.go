package core

import "github.com/google/uuid"

// TraceContext carries optional cross-subsystem correlation identifiers for an
// event (event-model.md §6.1). All three fields are optional: the zero value
// (all nil) is valid and means "no trace information is available for this
// event."
//
// When causal linkage is known, producers SHOULD populate ParentEventID and
// RootEventID together. The partial-order contract (§6.1) uses ParentEventID
// to extend strict intra-process monotonicity across subsystem boundaries.
// RootEventID identifies the originating event of the causal chain.
//
// TraceContext is embedded in the Event envelope as a nullable field; the
// envelope is defined in §6.1 alongside this record.
type TraceContext struct {
	// TraceID is an optional external correlation identifier (e.g. an OpenTelemetry
	// trace-id or a request-id propagated from an upstream caller).
	// When non-nil, the string must be non-empty.
	TraceID *string

	// ParentEventID is the UUID of the event that causally preceded this one.
	// When non-nil, the value must not be uuid.Nil.
	// Producers SHOULD populate this field when causal linkage is known (see
	// partial-order contract in §6.1 and hidden assumption (4) in §7.x).
	ParentEventID *uuid.UUID

	// RootEventID is the UUID of the event that originated the causal chain.
	// When non-nil, the value must not be uuid.Nil.
	RootEventID *uuid.UUID
}

// Valid reports whether tc is structurally sound. The zero value is valid.
// Rules:
//   - If TraceID is non-nil, the string it points to must be non-empty.
//   - If ParentEventID is non-nil, it must not equal uuid.Nil.
//   - If RootEventID is non-nil, it must not equal uuid.Nil.
//
// Note: the spec advises producers to populate ParentEventID and RootEventID
// together when causal linkage is known, but this is a producer-side SHOULD,
// not a structural validity rule. Valid() does not enforce parent⇒root because
// the record stores whatever it receives; enforcement belongs in the producer,
// not in the record's validity check.
func (tc TraceContext) Valid() bool {
	if tc.TraceID != nil && *tc.TraceID == "" {
		return false
	}
	if tc.ParentEventID != nil && *tc.ParentEventID == uuid.Nil {
		return false
	}
	if tc.RootEventID != nil && *tc.RootEventID == uuid.Nil {
		return false
	}
	return true
}
