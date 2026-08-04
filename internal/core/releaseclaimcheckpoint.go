// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ReleaseClaimHeadRef is the revision the writer checks before it writes.
//
// The writer commits onto the branch that is checked out, so the current HEAD
// is the only pre-image that can already hold the record.
const ReleaseClaimHeadRef = "HEAD"

// ErrTransitionRecordAbsent reports that a transition record is not in the tree
// of the requested commit.
//
// A ReleaseClaimStore implementation MUST return an error that wraps this
// sentinel when the path is missing, and MUST NOT return empty bytes with a nil
// error. Callers separate "no record" from "the read failed" with errors.Is,
// and an empty-bytes-and-nil result collapses the two.
var ErrTransitionRecordAbsent = errors.New("core: transition record absent at commit")

// ErrReleaseClaimMissing reports that a transition offered to the release-claim
// writer carries no claim (EM-031b).
var ErrReleaseClaimMissing = errors.New("core: transition carries no release claim (EM-031b)")

// ErrReleaseClaimInvalid reports that a release claim is present but incomplete,
// or that its transition record does not satisfy Transition.Valid.
//
// EM-031b treats a corrupt claim exactly like a missing one: retain the branch
// and route to reconciliation.
var ErrReleaseClaimInvalid = errors.New("core: release claim is invalid (EM-031b)")

// ErrReleaseClaimInconsistent reports that a stored record disagrees with the
// run or transition it was read for (EM-031b "inconsistent with its
// checkpoint").
var ErrReleaseClaimInconsistent = errors.New("core: release claim inconsistent with its checkpoint (EM-031b)")

// ErrReleaseClaimImmutable reports that a record already exists at the target
// path and the writer refused to write over it.
//
// The claim's immutability is structural — a later transition owns a different
// transition_id and therefore a different path (EM-018) — and this error is the
// enforced guard behind that structure. It fires when a caller retries a write
// under a transition_id that already committed a record.
var ErrReleaseClaimImmutable = errors.New("core: release claim already committed; a claim is never rewritten (EM-031b)")

// ReleaseClaimStore is the narrow git seam the release-claim checkpoint needs.
//
// internal/core is a leaf package with no subsystem imports and no process
// execution, so it declares the seam and the daemon supplies the git. The
// interface has two methods because the checkpoint needs exactly two effects:
// read the record that may already be there, and commit the record that must be
// there before any release step runs.
//
// An implementation operates on ONE worktree, fixed when it is constructed. For
// a remote run that worktree is on the worker, so the implementation carries
// whatever runner reaches it.
type ReleaseClaimStore interface {
	// ReadTransitionRecord returns the bytes of relPath as of commitish.
	//
	// It MUST return an error wrapping ErrTransitionRecordAbsent when relPath is
	// not in that commit's tree. Any other failure (the commit does not resolve,
	// the command fails, the transport drops) MUST return a different error, so
	// that a broken read is never mistaken for a missing claim.
	ReadTransitionRecord(ctx context.Context, commitish, relPath string) ([]byte, error)

	// CommitTransitionRecord writes data at relPath in the worktree, stages that
	// one path, and commits it with the given message. It returns the SHA of the
	// new commit.
	//
	// The commit MUST include relPath and MUST NOT include unrelated worktree
	// changes: EM-016 makes the commit the atomicity boundary, and a commit that
	// sweeps in other files makes the claim's durability depend on work the
	// claim does not describe.
	CommitTransitionRecord(ctx context.Context, relPath string, data []byte, message string) (string, error)
}

// ReleaseClaimCheckpointRequest is the input to the release-claim checkpoint.
type ReleaseClaimCheckpointRequest struct {
	// Transition is the final pre-release transition record. Its ReleaseClaim
	// field MUST be non-nil and MUST be valid.
	Transition Transition

	// BeadID ties the checkpoint to a bead per EM-014, and becomes the
	// Harmonik-Bead-ID trailer. It is nil for a run with no bead.
	BeadID *BeadID
}

// ReleaseOperation is a release step guarded by the claim: synchronize, merge,
// close, or reopen.
//
// It receives the durable checkpoint and the claim that checkpoint carries. It
// MUST use the claim's recorded merge target and recorded endpoint rather than
// choosing either again, per EM-031b.
type ReleaseOperation func(ctx context.Context, cp Checkpoint, claim ReleaseClaim) error

// WriteReleaseClaimCheckpoint persists the final pre-release checkpoint and
// returns it (execution-model.md §4.7 EM-031b).
//
// The sequence is:
//
//  1. Reject a transition that carries no claim, or whose claim or record is
//     invalid. Nothing is written.
//  2. Refuse to write when a record already exists at the transition's path in
//     ReleaseClaimHeadRef. This is the immutability guard.
//  3. Marshal the record and commit it with the EM-017 trailers.
//
// The returned Checkpoint is durable when the error is nil: the commit has
// landed and the claim is readable with ReadReleaseClaim. EM-031b forbids
// beginning any release operation before that point, so a caller that merges,
// closes, or reopens should route through ReleaseAfterClaim rather than
// sequencing the two calls by hand.
func WriteReleaseClaimCheckpoint(
	ctx context.Context,
	store ReleaseClaimStore,
	req ReleaseClaimCheckpointRequest,
) (Checkpoint, error) {
	if store == nil {
		return Checkpoint{}, errors.New("WriteReleaseClaimCheckpoint: store is nil")
	}
	tr := req.Transition
	if tr.ReleaseClaim == nil {
		return Checkpoint{}, fmt.Errorf("WriteReleaseClaimCheckpoint: %w", ErrReleaseClaimMissing)
	}
	// Transition.Valid covers the claim, so one check answers both questions.
	// The two messages exist because the caller needs to know which half is
	// wrong, and the record is immutable once committed.
	if !tr.Valid() {
		if !tr.ReleaseClaim.Valid() {
			return Checkpoint{}, fmt.Errorf(
				"WriteReleaseClaimCheckpoint: release claim is incomplete: %w", ErrReleaseClaimInvalid)
		}
		return Checkpoint{}, fmt.Errorf(
			"WriteReleaseClaimCheckpoint: transition record fails Valid(): %w", ErrReleaseClaimInvalid)
	}
	if req.BeadID != nil && *req.BeadID == "" {
		return Checkpoint{}, errors.New("WriteReleaseClaimCheckpoint: BeadID is non-nil but empty")
	}

	relPath := TransitionRecordPath(tr.RunID, tr.TransitionID)

	// EM-031b immutability guard. A record already at this path means the claim
	// was written before; writing again would replace it.
	switch _, err := store.ReadTransitionRecord(ctx, ReleaseClaimHeadRef, relPath); {
	case err == nil:
		return Checkpoint{}, fmt.Errorf("WriteReleaseClaimCheckpoint: %s: %w", relPath, ErrReleaseClaimImmutable)
	case errors.Is(err, ErrTransitionRecordAbsent):
		// The expected case: nothing is there yet.
	default:
		return Checkpoint{}, fmt.Errorf("WriteReleaseClaimCheckpoint: read %s: %w", relPath, err)
	}

	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("WriteReleaseClaimCheckpoint: %w", err)
	}

	sha, err := store.CommitTransitionRecord(ctx, relPath, data, releaseClaimCommitMessage(tr, req.BeadID))
	if err != nil {
		return Checkpoint{}, fmt.Errorf("WriteReleaseClaimCheckpoint: commit %s: %w", relPath, err)
	}
	if sha == "" {
		return Checkpoint{}, errors.New("WriteReleaseClaimCheckpoint: store returned an empty commit SHA")
	}

	cp := Checkpoint{
		CommitHash:           sha,
		RunID:                tr.RunID,
		StateID:              tr.ToState.StateID,
		TransitionID:         tr.TransitionID,
		BeadID:               req.BeadID,
		SchemaVersion:        tr.SchemaVersion,
		TransitionRecordPath: relPath,
	}
	if !cp.Valid() {
		return Checkpoint{}, errors.New("WriteReleaseClaimCheckpoint: assembled checkpoint fails Valid()")
	}
	return cp, nil
}

// ReleaseAfterClaim writes the release-claim checkpoint and only then runs the
// release step (execution-model.md §4.7 EM-031b: "A release operation MUST NOT
// begin until this checkpoint is durable").
//
// The ordering is structural rather than checked. When the claim write fails,
// release is never called and the error names the write. When the write
// succeeds, release receives the durable checkpoint and the recorded claim, so
// it uses the recorded merge target and the recorded endpoint instead of
// choosing either again.
//
// The returned Checkpoint is the durable claim checkpoint. It is returned even
// when release fails, because the branch and its claim survive a failed release
// and recovery reads them.
func ReleaseAfterClaim(
	ctx context.Context,
	store ReleaseClaimStore,
	req ReleaseClaimCheckpointRequest,
	release ReleaseOperation,
) (Checkpoint, error) {
	if release == nil {
		return Checkpoint{}, errors.New("ReleaseAfterClaim: release operation is nil")
	}
	cp, err := WriteReleaseClaimCheckpoint(ctx, store, req)
	if err != nil {
		return Checkpoint{}, err
	}
	if err := release(ctx, cp, *req.Transition.ReleaseClaim); err != nil {
		return cp, fmt.Errorf("ReleaseAfterClaim: release after claim %s: %w", cp.CommitHash, err)
	}
	return cp, nil
}

// ReadReleaseClaim reads a release claim back out of git
// (execution-model.md §4.7 EM-031b).
//
// It reads the transition record at the canonical path for runID and
// transitionID as of commitish, decodes it, and checks that the record names
// the same run and transition it was asked for.
//
// The three failure modes EM-031b groups together each get their own sentinel,
// so a caller can log which one it hit:
//
//   - the record is not in the commit — ErrTransitionRecordAbsent
//   - the record does not decode, or carries no claim, or the claim is
//     incomplete — ErrReleaseClaimInvalid
//   - the record names a different run or transition —
//     ErrReleaseClaimInconsistent
//
// All three lead to the same behavior: retain the branch and route to
// reconciliation, with no merge, close, reopen, or redispatch. Use
// IsReleaseClaimUnusable to test for that set.
//
// The caller MUST NOT fill a missing field from the JSONL event log, a
// daemon-local registry, or process memory. EM-031b forbids all three as
// release-state sources.
func ReadReleaseClaim(
	ctx context.Context,
	store ReleaseClaimStore,
	commitish string,
	runID RunID,
	transitionID TransitionID,
) (ReleaseClaim, error) {
	if store == nil {
		return ReleaseClaim{}, errors.New("ReadReleaseClaim: store is nil")
	}
	relPath := TransitionRecordPath(runID, transitionID)
	data, err := store.ReadTransitionRecord(ctx, commitish, relPath)
	if err != nil {
		return ReleaseClaim{}, fmt.Errorf("ReadReleaseClaim: read %s at %s: %w", relPath, commitish, err)
	}

	tr, err := UnmarshalTransitionRecord(data)
	if err != nil {
		return ReleaseClaim{}, fmt.Errorf("ReadReleaseClaim: decode %s: %w: %w", relPath, ErrReleaseClaimInvalid, err)
	}
	if tr.RunID != runID || tr.TransitionID != transitionID {
		return ReleaseClaim{}, fmt.Errorf(
			"ReadReleaseClaim: record at %s names run %s transition %s: %w",
			relPath, tr.RunID, tr.TransitionID, ErrReleaseClaimInconsistent)
	}
	if tr.ReleaseClaim == nil {
		return ReleaseClaim{}, fmt.Errorf("ReadReleaseClaim: %s: %w", relPath, ErrReleaseClaimInvalid)
	}
	if !tr.ReleaseClaim.Valid() {
		return ReleaseClaim{}, fmt.Errorf("ReadReleaseClaim: %s: %w", relPath, ErrReleaseClaimInvalid)
	}
	return *tr.ReleaseClaim, nil
}

// IsReleaseClaimUnusable reports whether err is one of the three conditions
// EM-031b routes to reconciliation: the claim is absent, invalid, or
// inconsistent with its checkpoint.
//
// EM-031b gives all three the same result — retain the run branch, reconcile,
// and take no merge, close, reopen, or redispatch — so the caller needs one
// test, not three.
func IsReleaseClaimUnusable(err error) bool {
	return errors.Is(err, ErrTransitionRecordAbsent) ||
		errors.Is(err, ErrReleaseClaimInvalid) ||
		errors.Is(err, ErrReleaseClaimInconsistent) ||
		errors.Is(err, ErrReleaseClaimMissing)
}

// releaseClaimCommitMessage builds the checkpoint commit message.
//
// The trailers are the EM-017 required set plus the EM-017 conditional
// Harmonik-Bead-ID. They appear in the declared order of the §6.2 trailer
// registry, so trailer-lint reads them in the order it lists them.
func releaseClaimCommitMessage(tr Transition, beadID *BeadID) string {
	var b strings.Builder
	b.WriteString("harmonik: release claim for run ")
	b.WriteString(tr.RunID.String())
	b.WriteString("\n\n")
	b.WriteString("Record the release inputs before any synchronize, merge, close or\n")
	b.WriteString("reopen step. Restart recovery reads this claim from git per EM-031b.\n\n")
	if beadID != nil {
		b.WriteString("Harmonik-Bead-ID: ")
		b.WriteString(string(*beadID))
		b.WriteString("\n")
	}
	b.WriteString("Harmonik-Run-ID: ")
	b.WriteString(tr.RunID.String())
	b.WriteString("\n")
	b.WriteString("Harmonik-Schema-Version: ")
	fmt.Fprintf(&b, "%d", tr.SchemaVersion)
	b.WriteString("\n")
	b.WriteString("Harmonik-State-ID: ")
	b.WriteString(tr.ToState.StateID.String())
	b.WriteString("\n")
	b.WriteString("Harmonik-Transition-ID: ")
	b.WriteString(tr.TransitionID.String())
	b.WriteString("\n")
	return b.String()
}
