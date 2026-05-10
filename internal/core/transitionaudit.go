// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import "fmt"

// AuditViolationKind is the closed enum of integrity-violation conditions that
// a post-hoc audit tool MUST detect for every commit reachable from every
// active or archived task branch, per execution-model.md §4.4.EM-020a.
//
// The five conditions are labelled (a)–(e) in the spec and map directly to the
// five constants below. An audit tool that detects any of these conditions MUST
// route the flagged commit to reconciliation per RC-010.
type AuditViolationKind string

const (
	// AuditViolationKindNoSiblingFile corresponds to condition (a) in EM-020a:
	// a commit carries Harmonik-Run-ID and Harmonik-Transition-ID trailers but
	// no sibling file exists at
	// .harmonik/transitions/<run_id>/<transition_id>.json in the commit's tree.
	//
	// Detection: parse trailers via git interpret-trailers --parse, then check
	// git show <commit>:.harmonik/transitions/<run_id>/<transition_id>.json.
	AuditViolationKindNoSiblingFile AuditViolationKind = "no-sibling-file"

	// AuditViolationKindOrphanSiblingFile corresponds to condition (b) in
	// EM-020a: a sibling file exists under .harmonik/transitions/ on a commit
	// but does not match any trailer pair on that commit.
	//
	// Detection: enumerate .harmonik/transitions/<run_id>/*.json in the
	// commit's tree and verify each path is matched by a Harmonik-Run-ID /
	// Harmonik-Transition-ID trailer pair on the same commit.
	AuditViolationKindOrphanSiblingFile AuditViolationKind = "orphan-sibling-file"

	// AuditViolationKindDuplicateTransitionID corresponds to condition (c) in
	// EM-020a: within a single run's sub-directory
	// (.harmonik/transitions/<run_id>/), the same transition_id appears on
	// more than one commit across the run's task-branch history.
	//
	// Detection: for each run_id seen in trailers, collect all transition_ids
	// emitted under that run across all task-branch commits and flag any
	// transition_id that appears on more than one commit.
	AuditViolationKindDuplicateTransitionID AuditViolationKind = "duplicate-transition-id"

	// AuditViolationKindSchemaVersionMismatch corresponds to condition (d) in
	// EM-020a: the sibling file's schema_version field disagrees with the
	// commit's Harmonik-Schema-Version trailer value.
	//
	// Detection: parse the sibling file's schema_version and compare with the
	// integer value of the Harmonik-Schema-Version trailer; any disagreement
	// is a violation. This mirrors the pre-write check in
	// ValidateTransitionSchemaVersion.
	AuditViolationKindSchemaVersionMismatch AuditViolationKind = "schema-version-mismatch"

	// AuditViolationKindRunIDPathMismatch corresponds to condition (e) in
	// EM-020a: the sibling file's path <run_id> component disagrees with the
	// commit's Harmonik-Run-ID trailer.
	//
	// Detection: extract the run_id segment from the sibling file path
	// (.harmonik/transitions/<run_id>/<transition_id>.json) and compare with
	// the Harmonik-Run-ID trailer; any disagreement is a violation (e.g.
	// caused by an incorrect cherry-pick or manual path manipulation).
	AuditViolationKindRunIDPathMismatch AuditViolationKind = "run-id-path-mismatch"
)

// Valid reports whether k is one of the five declared AuditViolationKind
// constants. The enum is harmonik-owned and closed; unknown values are never
// valid.
func (k AuditViolationKind) Valid() bool {
	switch k {
	case AuditViolationKindNoSiblingFile,
		AuditViolationKindOrphanSiblingFile,
		AuditViolationKindDuplicateTransitionID,
		AuditViolationKindSchemaVersionMismatch,
		AuditViolationKindRunIDPathMismatch:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so AuditViolationKind
// serialises correctly in JSON.
// It rejects any value that is not one of the five declared constants.
func (k AuditViolationKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("auditvioationkind: unknown value %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the five declared constants.
func (k *AuditViolationKind) UnmarshalText(text []byte) error {
	v := AuditViolationKind(text)
	if !v.Valid() {
		return fmt.Errorf(
			"auditviolationkind: unknown value %q;"+
				" must be one of no-sibling-file, orphan-sibling-file,"+
				" duplicate-transition-id, schema-version-mismatch, run-id-path-mismatch",
			string(text),
		)
	}
	*k = v
	return nil
}

// AuditViolation is a single integrity-violation record produced by the
// post-hoc audit tool described in execution-model.md §4.4.EM-020a.
//
// Each AuditViolation identifies one flagged commit by its SHA, the
// violation's kind (one of the five EM-020a conditions), and carries the
// run_id and transition_id extracted from the commit's trailers (or from the
// sibling file path for condition (b) violations). The Description field
// provides a human-readable explanation suitable for routing to reconciliation
// per RC-010.
//
// Flagged commits MUST be routed to reconciliation per
// [reconciliation/spec.md §4.3 RC-010]. An AuditViolation is the carrier
// record that communicates the violation to the reconciliation subsystem.
//
// # Mapping to EM-020a violation conditions
//
//   - AuditViolationKindNoSiblingFile        → condition (a)
//   - AuditViolationKindOrphanSiblingFile     → condition (b)
//   - AuditViolationKindDuplicateTransitionID → condition (c)
//   - AuditViolationKindSchemaVersionMismatch → condition (d)
//   - AuditViolationKindRunIDPathMismatch     → condition (e)
type AuditViolation struct {
	// Kind is the EM-020a violation condition that was detected.
	// Must be a declared AuditViolationKind constant (Kind.Valid() == true).
	Kind AuditViolationKind

	// CommitSHA is the full git object SHA of the offending commit.
	// Must not be empty.
	CommitSHA string

	// RunID is the run_id extracted from the commit's Harmonik-Run-ID trailer,
	// or from the sibling file path for condition (b) violations.
	// May be the zero value when the trailer is entirely absent (condition (a)
	// with a missing trailer pair rather than a missing sibling file).
	RunID RunID

	// TransitionID is the transition_id extracted from the commit's
	// Harmonik-Transition-ID trailer, or from the sibling file path for
	// condition (b) violations.
	// May be the zero value when the trailer is entirely absent.
	TransitionID TransitionID

	// Description is a human-readable explanation of the specific violation,
	// including relevant values (e.g. expected vs. actual schema_version for
	// condition (d), or the orphaned path for condition (b)).
	// Must not be empty.
	Description string
}

// Valid reports whether av carries valid values for all required fields.
//
// Rules:
//   - Kind is a declared AuditViolationKind constant
//   - CommitSHA is non-empty
//   - Description is non-empty
//
// RunID and TransitionID are permitted to be zero for condition (a) violations
// where the trailer pair itself is absent; no UUID constraint is enforced here.
func (av AuditViolation) Valid() bool {
	if !av.Kind.Valid() {
		return false
	}
	if av.CommitSHA == "" {
		return false
	}
	if av.Description == "" {
		return false
	}
	return true
}
