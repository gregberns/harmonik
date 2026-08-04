// Package core — requirement-traceable sensors for the release-claim checkpoint
// per execution-model.md §4.7 EM-031b.
//
// EM-031b makes four demands this file measures:
//
//  1. The claim is durable BEFORE any synchronize, merge, close or reopen step.
//  2. A local claim omits the endpoint.
//  3. A remote claim retains every endpoint field.
//  4. A later transition cannot rewrite a claim.
//
// The fake store below models a git worktree as an append-only history: each
// commit holds every path committed up to that point.
//
// The fake is deliberately STRICTER than git and stricter than the
// ReleaseClaimStore contract: it refuses to commit a path that already exists.
// Git does not give that for free. An old commit keeps its bytes, but the path
// at HEAD is replaceable — a new commit can carry different bytes at the same
// path, and the claim would then be rewritten. The refusal in the fake is a
// second net, not the thing under test. The writer's own pre-write check is
// what enforces immutability, and the tests below assert on the writer's
// sentinel error so that the fake's refusal cannot pass for the writer's.
// Whoever writes the real git adapter MUST NOT assume the refusal comes free.
package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// --- fake store ---

// rc31bStore is an in-memory ReleaseClaimStore. It records the order of every
// call so an ordering test can read it back.
type rc31bStore struct {
	// commits is the ordered history. Each entry maps a tree path to its bytes.
	commits []map[string][]byte
	// shas parallels commits.
	shas []string
	// calls records every effect in the order it happened.
	calls []string
	// messages records the commit message of every accepted commit.
	messages []string
	// commitErr, when non-nil, fails the next CommitTransitionRecord.
	commitErr error
	// emptySHA makes CommitTransitionRecord report success with no SHA,
	// modelling a store that reports a commit it did not make.
	emptySHA bool
	// readErr, when non-nil, fails every ReadTransitionRecord with a non-absent
	// error, modelling a broken git rather than a missing path.
	readErr error
}

func newRC31bStore() *rc31bStore {
	return &rc31bStore{}
}

// head returns the tree of the newest commit, or an empty tree.
func (s *rc31bStore) head() map[string][]byte {
	if len(s.commits) == 0 {
		return map[string][]byte{}
	}
	return s.commits[len(s.commits)-1]
}

// treeAt resolves a commitish to a tree. It accepts ReleaseClaimHeadRef and any
// SHA this store has handed out.
func (s *rc31bStore) treeAt(commitish string) (map[string][]byte, bool) {
	if commitish == ReleaseClaimHeadRef {
		return s.head(), true
	}
	for i, sha := range s.shas {
		if sha == commitish {
			return s.commits[i], true
		}
	}
	return nil, false
}

func (s *rc31bStore) ReadTransitionRecord(_ context.Context, commitish, relPath string) ([]byte, error) {
	s.calls = append(s.calls, "read "+relPath+"@"+commitish)
	if s.readErr != nil {
		return nil, s.readErr
	}
	tree, ok := s.treeAt(commitish)
	if !ok {
		return nil, fmt.Errorf("rc31bStore: unknown commitish %q", commitish)
	}
	data, ok := tree[relPath]
	if !ok {
		return nil, fmt.Errorf("rc31bStore: %s: %w", relPath, ErrTransitionRecordAbsent)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (s *rc31bStore) CommitTransitionRecord(_ context.Context, relPath string, data []byte, message string) (string, error) {
	s.calls = append(s.calls, "commit "+relPath)
	if s.commitErr != nil {
		return "", s.commitErr
	}
	if s.emptySHA {
		return "", nil
	}
	next := map[string][]byte{}
	for k, v := range s.head() {
		next[k] = v
	}
	// Model git's real guarantee: a path already in history is never replaced.
	// If the writer ever tries, the test sees it here rather than silently
	// accepting a rewritten claim.
	if _, exists := next[relPath]; exists {
		return "", fmt.Errorf("rc31bStore: refused to replace committed path %s", relPath)
	}
	stored := make([]byte, len(data))
	copy(stored, data)
	next[relPath] = stored
	s.commits = append(s.commits, next)
	sha := fmt.Sprintf("%040x", len(s.commits))
	s.shas = append(s.shas, sha)
	s.messages = append(s.messages, message)
	return sha, nil
}

var _ ReleaseClaimStore = (*rc31bStore)(nil)

// --- fixtures ---

// rc31bRequest builds a release-claim checkpoint request carrying claim.
func rc31bRequest(t *testing.T, claim ReleaseClaim) ReleaseClaimCheckpointRequest {
	t.Helper()
	tr := b3f77ValidTransition(t)
	c := claim
	tr.ReleaseClaim = &c
	bead := BeadID("hk-t5a01")
	return ReleaseClaimCheckpointRequest{Transition: tr, BeadID: &bead}
}

// --- Acceptance 1: the claim is durable before any release step ---

// TestReleaseAfterClaim_ClaimIsDurableBeforeRelease is the ordering sensor.
// EM-031b: "A release operation MUST NOT begin until this checkpoint is
// durable." The release step asserts, from inside itself, that the claim is
// already readable out of the committed checkpoint.
func TestReleaseAfterClaim_ClaimIsDurableBeforeRelease(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	var seenCallsAtRelease []string
	var readbackErr error
	var readback ReleaseClaim

	cp, err := ReleaseAfterClaim(context.Background(), store, req,
		func(ctx context.Context, cp Checkpoint, claim ReleaseClaim) error {
			seenCallsAtRelease = append([]string(nil), store.calls...)
			readback, readbackErr = ReadReleaseClaim(
				ctx, store, cp.CommitHash, req.Transition.RunID, req.Transition.TransitionID)
			return nil
		})
	if err != nil {
		t.Fatalf("ReleaseAfterClaim: %v", err)
	}

	// The commit must already have happened when the release step ran.
	sawCommit := false
	for _, c := range seenCallsAtRelease {
		if strings.HasPrefix(c, "commit ") {
			sawCommit = true
		}
	}
	if !sawCommit {
		t.Fatalf("release step ran before the claim commit; calls were %v", seenCallsAtRelease)
	}
	// And the claim must be readable from that commit, not merely written.
	if readbackErr != nil {
		t.Fatalf("claim was not readable from %s during the release step: %v", cp.CommitHash, readbackErr)
	}
	if readback != *req.Transition.ReleaseClaim {
		t.Errorf("claim read during release = %+v, want %+v", readback, *req.Transition.ReleaseClaim)
	}
}

// TestReleaseAfterClaim_ReleaseSkippedWhenClaimWriteFails proves the guard bites
// in the direction that matters. A failed claim write must stop the release, or
// the daemon would merge work whose release inputs were never recorded.
func TestReleaseAfterClaim_ReleaseSkippedWhenClaimWriteFails(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	store.commitErr = errors.New("disk full")
	req := rc31bRequest(t, rc31bLocalClaim())

	released := false
	_, err := ReleaseAfterClaim(context.Background(), store, req,
		func(context.Context, Checkpoint, ReleaseClaim) error {
			released = true
			return nil
		})
	if err == nil {
		t.Fatal("ReleaseAfterClaim: error = nil when the claim commit failed, want an error")
	}
	if released {
		t.Error("the release step ran even though the claim was never durable")
	}
}

// TestReleaseAfterClaim_ReleaseSkippedWhenClaimIsMissing proves a transition with
// no claim never reaches a release step.
func TestReleaseAfterClaim_ReleaseSkippedWhenClaimIsMissing(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := ReleaseClaimCheckpointRequest{Transition: b3f77ValidTransition(t)}

	released := false
	_, err := ReleaseAfterClaim(context.Background(), store, req,
		func(context.Context, Checkpoint, ReleaseClaim) error {
			released = true
			return nil
		})
	if !errors.Is(err, ErrReleaseClaimMissing) {
		t.Fatalf("error = %v, want ErrReleaseClaimMissing", err)
	}
	if released {
		t.Error("the release step ran for a transition carrying no claim")
	}
	if len(store.calls) != 0 {
		t.Errorf("the store was touched for a claimless transition: %v", store.calls)
	}
}

// TestReleaseAfterClaim_ReleaseReceivesTheRecordedClaim proves the release step
// is handed the recorded values. EM-031b requires recovery and release to use
// the recorded merge target and endpoint rather than choosing either again.
func TestReleaseAfterClaim_ReleaseReceivesTheRecordedClaim(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bRemoteClaim())

	var got ReleaseClaim
	if _, err := ReleaseAfterClaim(context.Background(), store, req,
		func(_ context.Context, _ Checkpoint, claim ReleaseClaim) error {
			got = claim
			return nil
		}); err != nil {
		t.Fatalf("ReleaseAfterClaim: %v", err)
	}
	want := *req.Transition.ReleaseClaim
	if got.MergeTargetRef != want.MergeTargetRef || got.MergeTargetSHA != want.MergeTargetSHA {
		t.Errorf("release received target (%s, %s), want (%s, %s)",
			got.MergeTargetRef, got.MergeTargetSHA, want.MergeTargetRef, want.MergeTargetSHA)
	}
	if got.RemoteEndpoint == nil || *got.RemoteEndpoint != *want.RemoteEndpoint {
		t.Errorf("release received endpoint %+v, want %+v", got.RemoteEndpoint, want.RemoteEndpoint)
	}
}

// TestReleaseAfterClaim_KeepsTheCheckpointWhenReleaseFails proves the claim
// survives a failed release. EM-031b keeps the branch and its claim retained
// until a release, reopen or reconciliation result is durable.
func TestReleaseAfterClaim_KeepsTheCheckpointWhenReleaseFails(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	cp, err := ReleaseAfterClaim(context.Background(), store, req,
		func(context.Context, Checkpoint, ReleaseClaim) error {
			return errors.New("merge conflict")
		})
	if err == nil {
		t.Fatal("ReleaseAfterClaim: error = nil when the release failed, want an error")
	}
	if cp.CommitHash == "" {
		t.Fatal("ReleaseAfterClaim returned no checkpoint after a failed release")
	}
	if _, readErr := ReadReleaseClaim(
		context.Background(), store, cp.CommitHash, req.Transition.RunID, req.Transition.TransitionID,
	); readErr != nil {
		t.Errorf("claim unreadable after a failed release: %v", readErr)
	}
}

// --- Acceptance 2 and 3: local omits the endpoint, remote keeps every field ---

// TestWriteReleaseClaimCheckpoint_LocalClaimOmitsEndpointOnDisk proves the
// omission survives the write, not just the marshal.
func TestWriteReleaseClaimCheckpoint_LocalClaimOmitsEndpointOnDisk(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	cp, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
	if err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	raw, err := store.ReadTransitionRecord(context.Background(), cp.CommitHash, cp.TransitionRecordPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(raw), "remote_endpoint") {
		t.Errorf("a local claim wrote a remote_endpoint key: %s", raw)
	}

	claim, err := ReadReleaseClaim(
		context.Background(), store, cp.CommitHash, req.Transition.RunID, req.Transition.TransitionID)
	if err != nil {
		t.Fatalf("ReadReleaseClaim: %v", err)
	}
	if !claim.IsLocal() {
		t.Errorf("read-back claim carries endpoint %+v, want none", claim.RemoteEndpoint)
	}
	if claim != *req.Transition.ReleaseClaim {
		t.Errorf("read-back claim = %+v, want %+v", claim, *req.Transition.ReleaseClaim)
	}
}

// TestWriteReleaseClaimCheckpoint_RemoteClaimRetainsEveryEndpointField proves
// the endpoint survives the write intact. Recovery uses these values to reach
// the one machine that holds the committed work.
func TestWriteReleaseClaimCheckpoint_RemoteClaimRetainsEveryEndpointField(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bRemoteClaim())

	cp, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
	if err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	claim, err := ReadReleaseClaim(
		context.Background(), store, cp.CommitHash, req.Transition.RunID, req.Transition.TransitionID)
	if err != nil {
		t.Fatalf("ReadReleaseClaim: %v", err)
	}
	want := req.Transition.ReleaseClaim.RemoteEndpoint
	if claim.RemoteEndpoint == nil {
		t.Fatal("read-back remote claim lost its endpoint")
	}
	if claim.RemoteEndpoint.WorkerName != want.WorkerName {
		t.Errorf("worker name = %q, want %q", claim.RemoteEndpoint.WorkerName, want.WorkerName)
	}
	if claim.RemoteEndpoint.Host != want.Host {
		t.Errorf("host = %q, want %q", claim.RemoteEndpoint.Host, want.Host)
	}
	if claim.RemoteEndpoint.RepoPath != want.RepoPath {
		t.Errorf("repo path = %q, want %q", claim.RemoteEndpoint.RepoPath, want.RepoPath)
	}
	if claim.DispatchHeadSHA != req.Transition.ReleaseClaim.DispatchHeadSHA {
		t.Errorf("dispatch head = %q, want %q",
			claim.DispatchHeadSHA, req.Transition.ReleaseClaim.DispatchHeadSHA)
	}
}

// --- Acceptance 4: a later transition cannot rewrite the claim ---

// TestWriteReleaseClaimCheckpoint_RefusesToRewriteAnExistingClaim proves the
// enforced immutability guard. A second write under the same transition id is
// refused, and the first claim's bytes are untouched.
func TestWriteReleaseClaimCheckpoint_RefusesToRewriteAnExistingClaim(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	first, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
	if err != nil {
		t.Fatalf("first WriteReleaseClaimCheckpoint: %v", err)
	}

	// A second attempt under the SAME transition id with a DIFFERENT target.
	rewrite := req
	changed := *req.Transition.ReleaseClaim
	changed.MergeTargetRef = "refs/heads/somewhere-else"
	changed.MergeTargetSHA = "9999999999999999999999999999999999999999"
	rewrite.Transition.ReleaseClaim = &changed

	if _, err := WriteReleaseClaimCheckpoint(context.Background(), store, rewrite); !errors.Is(err, ErrReleaseClaimImmutable) {
		t.Fatalf("second write error = %v, want ErrReleaseClaimImmutable", err)
	}
	if got := len(store.commits); got != 1 {
		t.Errorf("commit count = %d after a refused rewrite, want 1", got)
	}

	after, err := ReadReleaseClaim(
		context.Background(), store, ReleaseClaimHeadRef, req.Transition.RunID, req.Transition.TransitionID)
	if err != nil {
		t.Fatalf("ReadReleaseClaim after the refused rewrite: %v", err)
	}
	if after != *req.Transition.ReleaseClaim {
		t.Errorf("claim changed after a refused rewrite: got %+v, want %+v",
			after, *req.Transition.ReleaseClaim)
	}
	if _, err := ReadReleaseClaim(
		context.Background(), store, first.CommitHash, req.Transition.RunID, req.Transition.TransitionID,
	); err != nil {
		t.Errorf("original checkpoint no longer readable: %v", err)
	}
}

// TestWriteReleaseClaimCheckpoint_LaterTransitionCannotRewriteAPriorClaim is the
// acceptance condition stated in its own words: a LATER transition in the same
// run writes its own claim, and the earlier claim reads back unchanged from the
// new head.
func TestWriteReleaseClaimCheckpoint_LaterTransitionCannotRewriteAPriorClaim(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	first := rc31bRequest(t, rc31bLocalClaim())

	firstCP, err := WriteReleaseClaimCheckpoint(context.Background(), store, first)
	if err != nil {
		t.Fatalf("first WriteReleaseClaimCheckpoint: %v", err)
	}

	// A later transition in the SAME run: new transition id, new claim.
	second := first
	second.Transition.TransitionID = TransitionID(uuid.Must(uuid.NewV7()))
	second.Transition.ToState.StateID = StateID(uuid.Must(uuid.NewV7()))
	laterClaim := rc31bRemoteClaim()
	laterClaim.MergeTargetRef = "refs/heads/release"
	second.Transition.ReleaseClaim = &laterClaim

	secondCP, err := WriteReleaseClaimCheckpoint(context.Background(), store, second)
	if err != nil {
		t.Fatalf("second WriteReleaseClaimCheckpoint: %v", err)
	}
	if secondCP.TransitionRecordPath == firstCP.TransitionRecordPath {
		t.Fatal("the later transition reused the earlier record path; immutability is not structural")
	}

	// The earlier claim is unchanged when read from the LATER head.
	earlier, err := ReadReleaseClaim(
		context.Background(), store, secondCP.CommitHash, first.Transition.RunID, first.Transition.TransitionID)
	if err != nil {
		t.Fatalf("ReadReleaseClaim for the earlier transition at the later head: %v", err)
	}
	if earlier != *first.Transition.ReleaseClaim {
		t.Errorf("earlier claim = %+v after a later transition, want %+v",
			earlier, *first.Transition.ReleaseClaim)
	}
	if earlier.MergeTargetRef == laterClaim.MergeTargetRef {
		t.Error("the later transition's merge target leaked into the earlier claim")
	}
}

// --- writer rejections ---

// TestWriteReleaseClaimCheckpoint_RejectsAnIncompleteClaimWithoutWriting proves
// a partial claim never becomes durable. The commit is immutable, so a bad claim
// written once cannot be corrected.
func TestWriteReleaseClaimCheckpoint_RejectsAnIncompleteClaimWithoutWriting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mutas func(*ReleaseClaim)
	}{
		{"no dispatch head", func(c *ReleaseClaim) { c.DispatchHeadSHA = "" }},
		{"bare merge target ref", func(c *ReleaseClaim) { c.MergeTargetRef = "main" }},
		{"no merge target sha", func(c *ReleaseClaim) { c.MergeTargetSHA = "" }},
		{"endpoint missing its host", func(c *ReleaseClaim) {
			ep := rc31bRemoteEndpoint()
			ep.Host = ""
			c.RemoteEndpoint = &ep
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newRC31bStore()
			claim := rc31bLocalClaim()
			tc.mutas(&claim)
			req := rc31bRequest(t, claim)

			_, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
			if !errors.Is(err, ErrReleaseClaimInvalid) {
				t.Fatalf("error = %v, want ErrReleaseClaimInvalid", err)
			}
			if len(store.commits) != 0 {
				t.Errorf("an invalid claim was committed: %v", store.calls)
			}
		})
	}
}

// TestWriteReleaseClaimCheckpoint_ReadFailureIsNotTreatedAsAbsent proves a
// broken read never passes for "no claim yet". Treating it as absent would let
// the writer overwrite a claim it simply could not see.
func TestWriteReleaseClaimCheckpoint_ReadFailureIsNotTreatedAsAbsent(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	store.readErr = errors.New("ssh: connection reset")
	req := rc31bRequest(t, rc31bLocalClaim())

	if _, err := WriteReleaseClaimCheckpoint(context.Background(), store, req); err == nil {
		t.Fatal("error = nil when the pre-write read failed, want an error")
	}
	if len(store.commits) != 0 {
		t.Errorf("a claim was committed after a failed pre-write read: %v", store.calls)
	}
}

// TestWriteReleaseClaimCheckpoint_RejectsAnEmptyCommitSHA proves the writer does
// not hand back a checkpoint it cannot point at. A store that reports success
// with no SHA has not made the claim durable, and returning that checkpoint
// would let a release proceed against a commit that does not exist.
func TestWriteReleaseClaimCheckpoint_RejectsAnEmptyCommitSHA(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	store.emptySHA = true
	req := rc31bRequest(t, rc31bLocalClaim())

	released := false
	_, err := ReleaseAfterClaim(context.Background(), store, req,
		func(context.Context, Checkpoint, ReleaseClaim) error {
			released = true
			return nil
		})
	if err == nil {
		t.Fatal("error = nil when the store returned an empty commit SHA, want an error")
	}
	if released {
		t.Error("the release step ran against a checkpoint with no commit SHA")
	}
}

// TestWriteReleaseClaimCheckpoint_ChecksTheRecordPathBeforeCommitting proves the
// immutability guard runs first, not after the commit.
func TestWriteReleaseClaimCheckpoint_ChecksTheRecordPathBeforeCommitting(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	if _, err := WriteReleaseClaimCheckpoint(context.Background(), store, req); err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	want := []string{
		"read " + TransitionRecordPath(req.Transition.RunID, req.Transition.TransitionID) + "@" + ReleaseClaimHeadRef,
		"commit " + TransitionRecordPath(req.Transition.RunID, req.Transition.TransitionID),
	}
	if len(store.calls) != len(want) {
		t.Fatalf("store calls = %v, want %v", store.calls, want)
	}
	for i := range want {
		if store.calls[i] != want[i] {
			t.Errorf("store call %d = %q, want %q", i, store.calls[i], want[i])
		}
	}
}

// --- checkpoint shape and commit message ---

// TestWriteReleaseClaimCheckpoint_CheckpointFields proves the returned
// checkpoint names the run, state, transition and record path EM-016 and EM-018
// require, and that it satisfies Checkpoint.Valid.
func TestWriteReleaseClaimCheckpoint_CheckpointFields(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	cp, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
	if err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	if !cp.Valid() {
		t.Error("returned checkpoint fails Valid()")
	}
	if cp.RunID != req.Transition.RunID {
		t.Errorf("checkpoint run id = %s, want %s", cp.RunID, req.Transition.RunID)
	}
	if cp.TransitionID != req.Transition.TransitionID {
		t.Errorf("checkpoint transition id = %s, want %s", cp.TransitionID, req.Transition.TransitionID)
	}
	if cp.StateID != req.Transition.ToState.StateID {
		t.Errorf("checkpoint state id = %s, want the transition's to-state %s",
			cp.StateID, req.Transition.ToState.StateID)
	}
	if cp.BeadID == nil || *cp.BeadID != *req.BeadID {
		t.Errorf("checkpoint bead id = %v, want %s", cp.BeadID, *req.BeadID)
	}
	if cp.SchemaVersion != req.Transition.SchemaVersion {
		t.Errorf("checkpoint schema version = %d, want %d", cp.SchemaVersion, req.Transition.SchemaVersion)
	}
	want := TransitionRecordPath(req.Transition.RunID, req.Transition.TransitionID)
	if cp.TransitionRecordPath != want {
		t.Errorf("checkpoint record path = %q, want %q", cp.TransitionRecordPath, want)
	}
}

// TestWriteReleaseClaimCheckpoint_CommitMessageCarriesRequiredTrailers proves
// the checkpoint commit is findable by the EM-017 trailer index, and that every
// trailer it writes is in the §6.2 registry with a valid value.
func TestWriteReleaseClaimCheckpoint_CommitMessageCarriesRequiredTrailers(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())

	if _, err := WriteReleaseClaimCheckpoint(context.Background(), store, req); err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	if len(store.messages) != 1 {
		t.Fatalf("commit message count = %d, want 1", len(store.messages))
	}
	msg := store.messages[0]

	seen := map[string]string{}
	for _, line := range strings.Split(msg, "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || !strings.HasPrefix(key, "Harmonik-") {
			continue
		}
		seen[key] = value
	}
	for _, key := range []string{
		"Harmonik-Bead-ID", "Harmonik-Run-ID", "Harmonik-Schema-Version",
		"Harmonik-State-ID", "Harmonik-Transition-ID",
	} {
		value, present := seen[key]
		if !present {
			t.Errorf("commit message is missing trailer %s:\n%s", key, msg)
			continue
		}
		spec, known := LookupTrailer(key)
		if !known {
			t.Errorf("trailer %s is not in the §6.2 registry", key)
			continue
		}
		if err := ValidateTrailerValue(spec, value); err != nil {
			t.Errorf("trailer %s value %q: %v", key, value, err)
		}
	}
	if seen["Harmonik-Run-ID"] != req.Transition.RunID.String() {
		t.Errorf("Harmonik-Run-ID = %q, want %q", seen["Harmonik-Run-ID"], req.Transition.RunID.String())
	}
	if seen["Harmonik-Transition-ID"] != req.Transition.TransitionID.String() {
		t.Errorf("Harmonik-Transition-ID = %q, want %q",
			seen["Harmonik-Transition-ID"], req.Transition.TransitionID.String())
	}
}

// TestWriteReleaseClaimCheckpoint_OmitsBeadTrailerWhenTheRunHasNoBead proves the
// EM-017 conditional trailer stays conditional: present when the run is
// bead-tied per EM-014, absent otherwise.
func TestWriteReleaseClaimCheckpoint_OmitsBeadTrailerWhenTheRunHasNoBead(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	req := rc31bRequest(t, rc31bLocalClaim())
	req.BeadID = nil

	cp, err := WriteReleaseClaimCheckpoint(context.Background(), store, req)
	if err != nil {
		t.Fatalf("WriteReleaseClaimCheckpoint: %v", err)
	}
	if cp.BeadID != nil {
		t.Errorf("checkpoint bead id = %v for a run with no bead, want nil", cp.BeadID)
	}
	if strings.Contains(store.messages[0], "Harmonik-Bead-ID") {
		t.Errorf("commit message carries Harmonik-Bead-ID for a run with no bead:\n%s", store.messages[0])
	}
}

// --- reader failure modes ---

// TestReadReleaseClaim_AbsentRecord proves a missing record reports absence
// rather than an empty claim. EM-031b routes absence to reconciliation.
func TestReadReleaseClaim_AbsentRecord(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	runID := RunID(uuid.Must(uuid.NewV7()))
	tid := TransitionID(uuid.Must(uuid.NewV7()))

	_, err := ReadReleaseClaim(context.Background(), store, ReleaseClaimHeadRef, runID, tid)
	if !errors.Is(err, ErrTransitionRecordAbsent) {
		t.Fatalf("error = %v, want ErrTransitionRecordAbsent", err)
	}
	if !IsReleaseClaimUnusable(err) {
		t.Error("IsReleaseClaimUnusable = false for an absent record, want true")
	}
}

// TestReadReleaseClaim_TransitionWithNoClaim proves an ordinary checkpoint on
// the branch is not mistaken for a release claim.
func TestReadReleaseClaim_TransitionWithNoClaim(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	tr := b3f77ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	relPath := TransitionRecordPath(tr.RunID, tr.TransitionID)
	if _, err := store.CommitTransitionRecord(context.Background(), relPath, data, "ordinary checkpoint"); err != nil {
		t.Fatalf("CommitTransitionRecord: %v", err)
	}

	_, err = ReadReleaseClaim(context.Background(), store, ReleaseClaimHeadRef, tr.RunID, tr.TransitionID)
	if !errors.Is(err, ErrReleaseClaimInvalid) {
		t.Fatalf("error = %v, want ErrReleaseClaimInvalid", err)
	}
	if !IsReleaseClaimUnusable(err) {
		t.Error("IsReleaseClaimUnusable = false for a record with no claim, want true")
	}
}

// TestReadReleaseClaim_CorruptRecord proves unreadable bytes report an invalid
// claim rather than a zero one.
func TestReadReleaseClaim_CorruptRecord(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	runID := RunID(uuid.Must(uuid.NewV7()))
	tid := TransitionID(uuid.Must(uuid.NewV7()))
	relPath := TransitionRecordPath(runID, tid)
	if _, err := store.CommitTransitionRecord(
		context.Background(), relPath, []byte("{ truncated"), "corrupt"); err != nil {
		t.Fatalf("CommitTransitionRecord: %v", err)
	}

	_, err := ReadReleaseClaim(context.Background(), store, ReleaseClaimHeadRef, runID, tid)
	if !errors.Is(err, ErrReleaseClaimInvalid) {
		t.Fatalf("error = %v, want ErrReleaseClaimInvalid", err)
	}
	if !IsReleaseClaimUnusable(err) {
		t.Error("IsReleaseClaimUnusable = false for a corrupt record, want true")
	}
}

// TestReadReleaseClaim_InconsistentRecord proves a record whose own identity
// disagrees with the path it sits at is rejected. EM-031b names "inconsistent
// with its checkpoint" as its own failure.
func TestReadReleaseClaim_InconsistentRecord(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	tr := b3f77ValidTransition(t)
	claim := rc31bLocalClaim()
	tr.ReleaseClaim = &claim
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}

	// Store the record at a path belonging to a DIFFERENT transition.
	otherTID := TransitionID(uuid.Must(uuid.NewV7()))
	relPath := TransitionRecordPath(tr.RunID, otherTID)
	if _, err := store.CommitTransitionRecord(context.Background(), relPath, data, "misplaced"); err != nil {
		t.Fatalf("CommitTransitionRecord: %v", err)
	}

	_, err = ReadReleaseClaim(context.Background(), store, ReleaseClaimHeadRef, tr.RunID, otherTID)
	if !errors.Is(err, ErrReleaseClaimInconsistent) {
		t.Fatalf("error = %v, want ErrReleaseClaimInconsistent", err)
	}
	if !IsReleaseClaimUnusable(err) {
		t.Error("IsReleaseClaimUnusable = false for an inconsistent record, want true")
	}
}

// TestReadReleaseClaim_BrokenReadIsNotUnusableClaim proves a transport failure
// is NOT folded into the reconcile-and-retain set. A dropped ssh connection is a
// retryable read, not evidence that the claim is bad.
func TestReadReleaseClaim_BrokenReadIsNotUnusableClaim(t *testing.T) {
	t.Parallel()

	store := newRC31bStore()
	store.readErr = errors.New("ssh: connection reset")

	_, err := ReadReleaseClaim(context.Background(), store,
		ReleaseClaimHeadRef, RunID(uuid.Must(uuid.NewV7())), TransitionID(uuid.Must(uuid.NewV7())))
	if err == nil {
		t.Fatal("error = nil for a broken read, want an error")
	}
	if IsReleaseClaimUnusable(err) {
		t.Error("IsReleaseClaimUnusable = true for a transport failure, want false")
	}
}
