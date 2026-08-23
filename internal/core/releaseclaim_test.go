// Package core — requirement-traceable sensors for the release-claim record
// per execution-model.md §4.7 EM-031b and §6.1 RECORD ReleaseClaim /
// RECORD RemoteEndpoint.
//
// EM-031b requires the final pre-release checkpoint to carry a claim naming the
// dispatch head, the resolved merge target, and — for a remote run only — the
// worker endpoint. These sensors cover the record itself: what a valid claim is,
// what the local and remote shapes are, and how the claim rides the transition
// wire format.
package core

import (
	"encoding/json"
	"testing"
)

func rc31bLocalClaim() ReleaseClaim {
	return ReleaseClaim{
		DispatchHeadSHA: "1111111111111111111111111111111111111111",
		MergeTargetRef:  "refs/heads/main",
		MergeTargetSHA:  "2222222222222222222222222222222222222222",
	}
}

func rc31bRemoteEndpoint() RemoteEndpoint {
	return RemoteEndpoint{
		WorkerName: "worker-alpha",
		Host:       "build-01.example.net",
		RepoPath:   "/srv/harmonik/checkout",
	}
}

func rc31bRemoteClaim() ReleaseClaim {
	ep := rc31bRemoteEndpoint()
	c := rc31bLocalClaim()
	c.RemoteEndpoint = &ep
	return c
}

func TestRemoteEndpointValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	if !rc31bRemoteEndpoint().Valid() {
		t.Error("Valid() = false for a fully-populated RemoteEndpoint, want true")
	}
}

// TestRemoteEndpointValid_EachFieldRequired proves every endpoint field is
// load-bearing. §6.1 declares all three, and EM-031b tells recovery to use the
// recorded endpoint rather than select a worker again — a half-recorded
// endpoint would send it back to selection.
func TestRemoteEndpointValid_EachFieldRequired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mutas func(*RemoteEndpoint)
	}{
		{"empty worker name", func(e *RemoteEndpoint) { e.WorkerName = "" }},
		{"empty host", func(e *RemoteEndpoint) { e.Host = "" }},
		{"empty repo path", func(e *RemoteEndpoint) { e.RepoPath = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep := rc31bRemoteEndpoint()
			tc.mutas(&ep)
			if ep.Valid() {
				t.Errorf("Valid() = true with %s, want false", tc.name)
			}
		})
	}
}

func TestReleaseClaimValid_LocalClaim(t *testing.T) {
	t.Parallel()

	if !rc31bLocalClaim().Valid() {
		t.Error("Valid() = false for a local claim, want true")
	}
}

func TestReleaseClaimValid_RemoteClaim(t *testing.T) {
	t.Parallel()

	if !rc31bRemoteClaim().Valid() {
		t.Error("Valid() = false for a remote claim, want true")
	}
}

// TestReleaseClaimValid_EachFieldRequired proves each of the three always-present
// claim fields is required (§6.1).
func TestReleaseClaimValid_EachFieldRequired(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		mutas func(*ReleaseClaim)
	}{
		{"empty dispatch head", func(c *ReleaseClaim) { c.DispatchHeadSHA = "" }},
		{"empty merge target ref", func(c *ReleaseClaim) { c.MergeTargetRef = "" }},
		{"empty merge target sha", func(c *ReleaseClaim) { c.MergeTargetSHA = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := rc31bLocalClaim()
			tc.mutas(&c)
			if c.Valid() {
				t.Errorf("Valid() = true with %s, want false", tc.name)
			}
		})
	}
}

// TestReleaseClaimValid_MergeTargetRefMustBeFullyQualified proves the claim
// rejects a bare branch name. §6.1 declares merge_target_ref fully qualified.
// A bare name goes through git's ref-search rules and can resolve to a tag or a
// remote tracking ref, so recovery could merge into something the release never
// chose.
func TestReleaseClaimValid_MergeTargetRefMustBeFullyQualified(t *testing.T) {
	t.Parallel()

	for _, bare := range []string{"main", "heads/main", "origin/main"} {
		c := rc31bLocalClaim()
		c.MergeTargetRef = bare
		if c.Valid() {
			t.Errorf("Valid() = true with bare merge target ref %q, want false", bare)
		}
	}
}

// TestReleaseClaimValid_PartialEndpointRejected proves a claim that names an
// endpoint must name a complete one.
func TestReleaseClaimValid_PartialEndpointRejected(t *testing.T) {
	t.Parallel()

	c := rc31bRemoteClaim()
	c.RemoteEndpoint.Host = ""
	if c.Valid() {
		t.Error("Valid() = true with an endpoint missing its host, want false")
	}
}

// TestReleaseClaimIsLocal separates the two shapes EM-031b distinguishes.
func TestReleaseClaimIsLocal(t *testing.T) {
	t.Parallel()

	if !rc31bLocalClaim().IsLocal() {
		t.Error("IsLocal() = false for a claim with no endpoint, want true")
	}
	if rc31bRemoteClaim().IsLocal() {
		t.Error("IsLocal() = true for a claim with an endpoint, want false")
	}
}

// TestTransitionValid_NilReleaseClaimIsNormal proves the field is optional.
// Only the final pre-release checkpoint carries a claim; every other transition
// carries none and must stay valid.
func TestTransitionValid_NilReleaseClaimIsNormal(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ReleaseClaim = nil
	if !tr.Valid() {
		t.Error("Valid() = false for a transition with no release claim, want true")
	}
}

// TestTransitionValid_InvalidReleaseClaimRejected proves an incomplete claim
// cannot ride a valid transition into a checkpoint commit. The commit is
// immutable, so an incomplete claim written once stays wrong forever.
func TestTransitionValid_InvalidReleaseClaimRejected(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	bad := rc31bLocalClaim()
	bad.MergeTargetSHA = ""
	tr.ReleaseClaim = &bad
	if tr.Valid() {
		t.Error("Valid() = true for a transition carrying an incomplete claim, want false")
	}
}

// TestMarshalTransitionRecord_ReleaseClaimAbsentWhenNil proves the key is
// omitted, not written as null. §6.1 declares the claim "absent on all other
// transitions".
func TestMarshalTransitionRecord_ReleaseClaimAbsentWhenNil(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	tr.ReleaseClaim = nil
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, present := m["release_claim"]; present {
		t.Errorf("release_claim key present on a transition with no claim: %s", data)
	}
}

// TestMarshalTransitionRecord_LocalClaimOmitsEndpoint is one half of the T5a
// acceptance: a local claim omits the endpoint.
func TestMarshalTransitionRecord_LocalClaimOmitsEndpoint(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	local := rc31bLocalClaim()
	tr.ReleaseClaim = &local

	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	claim, ok := m["release_claim"].(map[string]any)
	if !ok {
		t.Fatalf("release_claim missing or not an object: %s", data)
	}
	if _, present := claim["remote_endpoint"]; present {
		t.Errorf("remote_endpoint key present on a local claim: %s", data)
	}
	for key, want := range map[string]string{
		"dispatch_head_sha": local.DispatchHeadSHA,
		"merge_target_ref":  local.MergeTargetRef,
		"merge_target_sha":  local.MergeTargetSHA,
	} {
		if got := claim[key]; got != want {
			t.Errorf("release_claim.%s = %v, want %q", key, got, want)
		}
	}
}

// TestMarshalTransitionRecord_RemoteClaimRetainsEveryEndpointField is the other
// half of the T5a acceptance: a remote claim keeps all three endpoint fields.
func TestMarshalTransitionRecord_RemoteClaimRetainsEveryEndpointField(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	remote := rc31bRemoteClaim()
	tr.ReleaseClaim = &remote

	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	claim, ok := m["release_claim"].(map[string]any)
	if !ok {
		t.Fatalf("release_claim missing or not an object: %s", data)
	}
	ep, ok := claim["remote_endpoint"].(map[string]any)
	if !ok {
		t.Fatalf("remote_endpoint missing or not an object: %s", data)
	}
	for key, want := range map[string]string{
		"worker_name": remote.RemoteEndpoint.WorkerName,
		"host":        remote.RemoteEndpoint.Host,
		"repo_path":   remote.RemoteEndpoint.RepoPath,
	} {
		if got := ep[key]; got != want {
			t.Errorf("remote_endpoint.%s = %v, want %q", key, got, want)
		}
	}
}

// TestUnmarshalTransitionRecord_RoundTripsLocalClaim proves the reader returns
// a local claim with no endpoint.
func TestUnmarshalTransitionRecord_RoundTripsLocalClaim(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	local := rc31bLocalClaim()
	tr.ReleaseClaim = &local

	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	got, err := UnmarshalTransitionRecord(data)
	if err != nil {
		t.Fatalf("UnmarshalTransitionRecord: %v", err)
	}
	if got.ReleaseClaim == nil {
		t.Fatal("UnmarshalTransitionRecord: release claim is nil, want the local claim")
	}
	if !got.ReleaseClaim.IsLocal() {
		t.Errorf("decoded claim carries an endpoint %+v, want none", got.ReleaseClaim.RemoteEndpoint)
	}
	if *got.ReleaseClaim != local {
		t.Errorf("decoded claim = %+v, want %+v", *got.ReleaseClaim, local)
	}
}

// TestUnmarshalTransitionRecord_RoundTripsRemoteClaim proves the reader returns
// every endpoint field. Recovery uses these values instead of selecting a worker
// again, so a dropped field is a silent wrong-machine merge.
func TestUnmarshalTransitionRecord_RoundTripsRemoteClaim(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	remote := rc31bRemoteClaim()
	tr.ReleaseClaim = &remote

	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	got, err := UnmarshalTransitionRecord(data)
	if err != nil {
		t.Fatalf("UnmarshalTransitionRecord: %v", err)
	}
	if got.ReleaseClaim == nil || got.ReleaseClaim.RemoteEndpoint == nil {
		t.Fatalf("decoded claim lost its endpoint: %+v", got.ReleaseClaim)
	}
	if *got.ReleaseClaim.RemoteEndpoint != *remote.RemoteEndpoint {
		t.Errorf("decoded endpoint = %+v, want %+v", *got.ReleaseClaim.RemoteEndpoint, *remote.RemoteEndpoint)
	}
}

// TestUnmarshalTransitionRecord_RoundTripsOrdinaryTransition proves the reader
// covers the whole record, not only the claim, and that an ordinary transition
// decodes back to itself.
func TestUnmarshalTransitionRecord_RoundTripsOrdinaryTransition(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	got, err := UnmarshalTransitionRecord(data)
	if err != nil {
		t.Fatalf("UnmarshalTransitionRecord: %v", err)
	}
	if got.TransitionID != tr.TransitionID || got.RunID != tr.RunID {
		t.Errorf("decoded identity = (%s, %s), want (%s, %s)",
			got.TransitionID, got.RunID, tr.TransitionID, tr.RunID)
	}
	if got.ToState.StateID != tr.ToState.StateID || got.FromState.StateID != tr.FromState.StateID {
		t.Error("decoded states do not match the source transition")
	}
	if got.ActorRole != tr.ActorRole || got.ChosenAction != tr.ChosenAction {
		t.Error("decoded actor role or chosen action does not match the source transition")
	}
	if got.OutcomeStatus != tr.OutcomeStatus || got.TransitionKind != tr.TransitionKind {
		t.Error("decoded outcome status or transition kind does not match the source transition")
	}
	if got.SchemaVersion != tr.SchemaVersion {
		t.Errorf("decoded schema_version = %d, want %d", got.SchemaVersion, tr.SchemaVersion)
	}
	if !got.Valid() {
		t.Error("decoded transition fails Valid()")
	}
}

// TestUnmarshalTransitionRecord_UnknownFieldIsNonFatal proves the EM-022 N-1
// readability contract: a reader at the prior schema version parses a record
// written at the next one and treats added fields as unknown but non-fatal.
func TestUnmarshalTransitionRecord_UnknownFieldIsNonFatal(t *testing.T) {
	t.Parallel()

	tr := b3f77ValidTransition(t)
	data, err := MarshalTransitionRecord(tr)
	if err != nil {
		t.Fatalf("MarshalTransitionRecord: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	m["a_field_from_a_later_version"] = map[string]any{"nested": true}
	extended, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	got, err := UnmarshalTransitionRecord(extended)
	if err != nil {
		t.Fatalf("UnmarshalTransitionRecord with an unknown field: %v", err)
	}
	if got.TransitionID != tr.TransitionID {
		t.Error("decoded transition id does not match after an unknown field was added")
	}
}

// TestUnmarshalTransitionRecord_RejectsMalformedJSON proves corrupt bytes give
// an error rather than a zero record. EM-031b routes a corrupt claim to
// reconciliation, which needs the error to fire.
func TestUnmarshalTransitionRecord_RejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, err := UnmarshalTransitionRecord([]byte("{not json")); err == nil {
		t.Error("UnmarshalTransitionRecord: error = nil for malformed JSON, want an error")
	}
}
