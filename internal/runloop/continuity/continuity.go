// Package continuity coordinates the review-loop implementer continuity
// checkpoint and its optional version-selected handshake.
//
// The package owns ordering and validation only. Committed-state inspection,
// git checkpoint creation, remote execution, and protocol writes are effects
// supplied through narrow ports by a composition root.
package continuity

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// IdentityPolicy describes when a harness obtains its authoritative continuity
// identity.
type IdentityPolicy string

const (
	// IdentityMinted means the caller obtains the authoritative identity before
	// first work and checkpoints it before permitting that work.
	IdentityMinted IdentityPolicy = "minted"

	// IdentityCaptured means the harness reveals its authoritative native
	// identity after initial launch. The service is called only after capture
	// and checkpoints it before any later resume or Retask.
	IdentityCaptured IdentityPolicy = "captured"
)

// Valid reports whether p is a declared identity policy.
func (p IdentityPolicy) Valid() bool {
	switch p {
	case IdentityMinted, IdentityCaptured:
		return true
	default:
		return false
	}
}

// VersionSelection describes whether the selected protocol requires a
// version_selected control message and, when it does, the exact negotiated
// version that message must carry.
type VersionSelection struct {
	Applicable      bool
	SelectedVersion int
}

// NoVersionSelection returns the plan for a protocol without a
// version_selected control message.
func NoVersionSelection() VersionSelection {
	return VersionSelection{}
}

// SendVersionSelection returns the plan for a protocol whose
// version_selected control message must carry selectedVersion.
func SendVersionSelection(selectedVersion int) VersionSelection {
	return VersionSelection{
		Applicable:      true,
		SelectedVersion: selectedVersion,
	}
}

// Valid reports whether the plan has exactly one valid shape.
func (s VersionSelection) Valid() bool {
	if s.Applicable {
		return s.SelectedVersion > 0
	}
	return s.SelectedVersion == 0
}

// LocationKind identifies where the authoritative run workspace exists.
type LocationKind string

const (
	LocationLocal  LocationKind = "local"
	LocationRemote LocationKind = "remote"
)

// Valid reports whether k is a declared location kind.
func (k LocationKind) Valid() bool {
	switch k {
	case LocationLocal, LocationRemote:
		return true
	default:
		return false
	}
}

// Location names the exact workspace in which committed state must be
// inspected and checkpointed.
//
// WorkerName is empty for a local workspace and required for a remote
// workspace. WorkspacePath is interpreted at that location; it is never
// silently replaced with a box-A path by this package or its ports.
type Location struct {
	Kind          LocationKind
	WorkspacePath string
	WorkerName    string
}

// Valid reports whether l has one unambiguous local or remote shape.
func (l Location) Valid() bool {
	if !l.Kind.Valid() || l.WorkspacePath == "" {
		return false
	}
	switch l.Kind {
	case LocationLocal:
		return l.WorkerName == ""
	case LocationRemote:
		return l.WorkerName != ""
	default:
		return false
	}
}

// CheckpointMetadata carries the run-scoped identifiers required to construct
// the EM-023a context-checkpoint transition and its commit trailers.
//
// Context checkpoints use StateID as both from_state_id and to_state_id. An
// empty BeadID means the run is not bead-tied and the optional bead trailer is
// omitted.
type CheckpointMetadata struct {
	RunID         core.RunID
	StateID       core.StateID
	TransitionID  core.TransitionID
	SchemaVersion int
	BeadID        core.BeadID
}

// Valid reports whether every required checkpoint identifier is UUIDv7 and the
// schema version is positive.
func (m CheckpointMetadata) Valid() bool {
	return uuidVersion(m.RunID) == 7 &&
		uuidVersion(m.StateID) == 7 &&
		m.TransitionID.IsUUIDv7() &&
		m.SchemaVersion > 0
}

// Request is the complete input to one continuity checkpoint transaction.
//
// For IdentityMinted, ExpectedIdentity is the concrete launch-artifact identity
// and ObservedIdentity is the identity reported by handler capabilities; both
// are required and must match before any checkpoint effect. For
// IdentityCaptured, ExpectedIdentity must remain empty (no substitute identity
// exists before launch) and ObservedIdentity is the native identity captured
// from the already-running harness.
type Request struct {
	Policy           IdentityPolicy
	ExpectedIdentity string
	ObservedIdentity string
	Location         Location
	Checkpoint       CheckpointMetadata
	VersionSelection VersionSelection
}

// Valid reports whether r can be executed without consulting an effectful
// dependency.
func (r Request) Valid() bool {
	if !r.Policy.Valid() ||
		!r.Location.Valid() ||
		!r.Checkpoint.Valid() ||
		!r.VersionSelection.Valid() {
		return false
	}
	switch r.Policy {
	case IdentityMinted:
		return r.ExpectedIdentity != "" &&
			r.ObservedIdentity != "" &&
			r.ExpectedIdentity == r.ObservedIdentity
	case IdentityCaptured:
		return r.ExpectedIdentity == "" && r.ObservedIdentity != ""
	default:
		return false
	}
}

// CheckpointRequest is the value passed to committed-state persistence.
type CheckpointRequest struct {
	Policy                IdentityPolicy
	AuthoritativeIdentity string
	Location              Location
	Metadata              CheckpointMetadata
}

// Valid reports whether r identifies one authoritative committed-state
// transaction target.
func (r CheckpointRequest) Valid() bool {
	return r.Policy.Valid() &&
		r.AuthoritativeIdentity != "" &&
		r.Location.Valid() &&
		r.Metadata.Valid()
}

// Checkpoint is the authoritative result of committed-state inspection and,
// when needed, an EM-023a context-checkpoint transition.
//
// CheckpointSHA is also the exact initial no-work-product comparison baseline.
// On identical committed reuse it must be the SHA that introduced the mapping,
// not an opportunistic current HEAD.
type Checkpoint struct {
	AuthoritativeIdentity  string
	CheckpointSHA          string
	ReusedCommittedMapping bool
}

// ValidFor reports whether c is a usable authoritative result for request.
func (c Checkpoint) ValidFor(request CheckpointRequest) bool {
	return request.Valid() &&
		c.AuthoritativeIdentity == request.AuthoritativeIdentity &&
		c.CheckpointSHA != ""
}

// CheckpointPort owns the committed-state transaction.
//
// EnsureCommitted must inspect committed Run.context state only. It must reuse
// an identical committed identity and return the exact introducing SHA, reject
// a conflicting committed identity, or persist a new mapping through an
// EM-023a context-checkpoint transition. A working-tree-only mapping is not
// durable and must not be treated as reuse.
type CheckpointPort interface {
	EnsureCommitted(ctx context.Context, request CheckpointRequest) (Checkpoint, error)
}

// IdentityConflictError is the typed failure returned by a CheckpointPort when
// committed Run.context already maps the run to a different authoritative
// continuity identity. The adapter must not overwrite that mapping.
type IdentityConflictError struct {
	CommittedIdentity string
	RequestedIdentity string
	CheckpointSHA     string
}

// Error implements error.
func (e *IdentityConflictError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf(
		"continuity: committed identity %q at checkpoint %q conflicts with requested identity %q",
		e.CommittedIdentity,
		e.CheckpointSHA,
		e.RequestedIdentity,
	)
}

// VersionSelectedPort sends and finalizes the selected-version control message.
// A nil error means the peer accepted the exact version supplied.
type VersionSelectedPort interface {
	SendVersionSelected(ctx context.Context, selectedVersion int) error
}

// VersionSelectedDisposition reports the successful handshake result.
type VersionSelectedDisposition string

const (
	VersionSelectedNotApplicable VersionSelectedDisposition = "not_applicable"
	VersionSelectedSent          VersionSelectedDisposition = "sent"
)

// Valid reports whether d is a declared successful disposition.
func (d VersionSelectedDisposition) Valid() bool {
	switch d {
	case VersionSelectedNotApplicable, VersionSelectedSent:
		return true
	default:
		return false
	}
}

// Result is returned only after every applicable transaction step succeeds.
type Result struct {
	Checkpoint                 Checkpoint
	VersionSelectedDisposition VersionSelectedDisposition
}

// Valid reports whether r is a complete successful transaction result.
func (r Result) Valid() bool {
	if r.Checkpoint.AuthoritativeIdentity == "" || r.Checkpoint.CheckpointSHA == "" {
		return false
	}
	return r.VersionSelectedDisposition.Valid()
}

// FailureStage identifies the transaction boundary that failed.
type FailureStage string

const (
	FailureValidation       FailureStage = "validation"
	FailureCheckpoint       FailureStage = "checkpoint"
	FailureCheckpointResult FailureStage = "checkpoint-result"
	FailureVersionSelected  FailureStage = "version-selected"
)

// Error is a typed transaction failure.
//
// CommittedCheckpoint is non-nil only when checkpointing succeeded but the
// later version-selected write/finalization failed. This preserves the
// irreversible durable fact for recovery without presenting a successful
// Result.
type Error struct {
	Stage               FailureStage
	CommittedCheckpoint *Checkpoint
	Cause               error
}

// Error implements error.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return fmt.Sprintf("continuity: %s failed", e.Stage)
	}
	return fmt.Sprintf("continuity: %s failed: %v", e.Stage, e.Cause)
}

// Unwrap returns the underlying port or validation error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Service synchronously coordinates checkpoint-before-version-selected
// ordering. It starts no goroutines and performs no effects outside its ports.
type Service struct {
	Checkpoints     CheckpointPort
	VersionSelected VersionSelectedPort
}

// Execute runs one continuity transaction.
//
// Every validation that can be performed locally happens before Checkpoints is
// called. A version-selected message is never attempted unless a valid
// checkpoint result has already returned. When applicable, the exact
// Request.VersionSelection.SelectedVersion is forwarded unchanged.
func (s Service) Execute(ctx context.Context, request Request) (Result, error) {
	if err := s.validate(ctx, request); err != nil {
		return Result{}, &Error{Stage: FailureValidation, Cause: err}
	}

	checkpointRequest := CheckpointRequest{
		Policy:                request.Policy,
		AuthoritativeIdentity: request.ObservedIdentity,
		Location:              request.Location,
		Metadata:              request.Checkpoint,
	}
	checkpoint, err := s.Checkpoints.EnsureCommitted(ctx, checkpointRequest)
	if err != nil {
		return Result{}, &Error{Stage: FailureCheckpoint, Cause: err}
	}
	if !checkpoint.ValidFor(checkpointRequest) {
		return Result{}, &Error{
			Stage: FailureCheckpointResult,
			Cause: fmt.Errorf(
				"invalid checkpoint result: identity=%q checkpoint_sha=%q for requested identity %q",
				checkpoint.AuthoritativeIdentity,
				checkpoint.CheckpointSHA,
				request.ObservedIdentity,
			),
		}
	}

	if !request.VersionSelection.Applicable {
		return Result{
			Checkpoint:                 checkpoint,
			VersionSelectedDisposition: VersionSelectedNotApplicable,
		}, nil
	}

	if err := s.VersionSelected.SendVersionSelected(
		ctx,
		request.VersionSelection.SelectedVersion,
	); err != nil {
		committed := checkpoint
		return Result{}, &Error{
			Stage:               FailureVersionSelected,
			CommittedCheckpoint: &committed,
			Cause:               err,
		}
	}

	return Result{
		Checkpoint:                 checkpoint,
		VersionSelectedDisposition: VersionSelectedSent,
	}, nil
}

func (s Service) validate(ctx context.Context, request Request) error {
	if ctx == nil {
		return fmt.Errorf("context must not be nil")
	}
	if !request.Policy.Valid() {
		return fmt.Errorf("identity policy %q is not valid", request.Policy)
	}
	switch request.Policy {
	case IdentityMinted:
		if request.ExpectedIdentity == "" {
			return fmt.Errorf("minted expected identity must not be empty")
		}
		if request.ObservedIdentity == "" {
			return fmt.Errorf("minted observed identity must not be empty")
		}
		if request.ExpectedIdentity != request.ObservedIdentity {
			return fmt.Errorf(
				"minted observed identity %q does not match expected launch identity %q",
				request.ObservedIdentity,
				request.ExpectedIdentity,
			)
		}
	case IdentityCaptured:
		if request.ExpectedIdentity != "" {
			return fmt.Errorf(
				"captured policy must not carry a pre-launch expected identity %q",
				request.ExpectedIdentity,
			)
		}
		if request.ObservedIdentity == "" {
			return fmt.Errorf("captured observed identity must not be empty")
		}
	}
	if !request.Location.Valid() {
		return fmt.Errorf(
			"continuity location is invalid: kind=%q workspace_path=%q worker_name=%q",
			request.Location.Kind,
			request.Location.WorkspacePath,
			request.Location.WorkerName,
		)
	}
	if !request.Checkpoint.Valid() {
		return fmt.Errorf("checkpoint metadata is invalid")
	}
	if !request.VersionSelection.Valid() {
		return fmt.Errorf(
			"version selection is invalid: applicable=%t selected_version=%d",
			request.VersionSelection.Applicable,
			request.VersionSelection.SelectedVersion,
		)
	}
	if s.Checkpoints == nil {
		return fmt.Errorf("checkpoint port must not be nil")
	}
	if request.VersionSelection.Applicable && s.VersionSelected == nil {
		return fmt.Errorf("version-selected port must not be nil when selection is applicable")
	}
	return nil
}

func uuidVersion[T ~[16]byte](id T) byte {
	return id[6] >> 4
}
