// Package core holds shared types that cross subsystem boundaries.
// No imports from any internal subsystem are permitted (see internal/core depguard rule).
package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// transitionWireCommitRange is the JSON wire shape for CommitRange.
type transitionWireCommitRange struct {
	FirstCommitSHA string `json:"first_commit_sha"`
	LastCommitSHA  string `json:"last_commit_sha"`
}

// transitionWireState is the JSON wire shape for State.
// Field names follow the snake_case convention of execution-model.md §6.1 RECORD State.
type transitionWireState struct {
	StateID           StateID                   `json:"state_id"`
	RunID             RunID                     `json:"run_id"`
	NodeID            NodeID                    `json:"node_id"`
	EnteredAt         time.Time                 `json:"entered_at"`
	TransitionHistory transitionWireCommitRange `json:"transition_history"`
}

// transitionWireRemoteEndpoint is the JSON wire shape for RemoteEndpoint
// (execution-model.md §6.1 RECORD RemoteEndpoint).
type transitionWireRemoteEndpoint struct {
	WorkerName string `json:"worker_name"`
	Host       string `json:"host"`
	RepoPath   string `json:"repo_path"`
}

// transitionWireReleaseClaim is the JSON wire shape for ReleaseClaim
// (execution-model.md §6.1 RECORD ReleaseClaim).
//
// remote_endpoint is omitted for local work rather than written as null, so a
// local claim and a remote claim differ by the presence of the key. §6.1
// declares the field `RemoteEndpoint | None` and EM-031b treats an
// incompletely-recorded endpoint as unusable, so absence is the clearer signal.
type transitionWireReleaseClaim struct {
	DispatchHeadSHA string                        `json:"dispatch_head_sha"`
	MergeTargetRef  string                        `json:"merge_target_ref"`
	MergeTargetSHA  string                        `json:"merge_target_sha"`
	RemoteEndpoint  *transitionWireRemoteEndpoint `json:"remote_endpoint,omitempty"`
}

// transitionWire is the JSON wire shape for a Transition sibling file.
// Field names follow the snake_case convention of execution-model.md §6.1 RECORD Transition.
// schema_version is included per §4.4.EM-018 and MUST match the commit's
// Harmonik-Schema-Version trailer. The field is the N-1-readable sentinel per
// §4.4.EM-022: readers MUST accept the immediately prior schema version (N-1);
// breaking changes (rename or removal of fields) require a migration release
// and MUST increment schema_version.
type transitionWire struct {
	TransitionID      TransitionID        `json:"transition_id"`
	RunID             RunID               `json:"run_id"`
	FromState         transitionWireState `json:"from_state"`
	ToState           transitionWireState `json:"to_state"`
	ActorRole         ActorRole           `json:"actor_role"`
	CandidateActions  []ActionDescriptor  `json:"candidate_actions"`
	ChosenAction      ActionDescriptor    `json:"chosen_action"`
	PolicyVersion     PolicyVersion       `json:"policy_version"`
	Evidence          map[string]any      `json:"evidence"`
	VerifierMetrics   map[string]any      `json:"verifier_metrics"`
	Confidence        *float64            `json:"confidence"`
	OutcomeStatus     OutcomeStatus       `json:"outcome_status"`
	TransitionKind    TransitionKind      `json:"transition_kind"`
	RollbackToStateID *StateID            `json:"rollback_to_state_id"`
	// ReleaseClaim is omitted entirely on an ordinary transition. §6.1 declares
	// the claim "absent on all other transitions", so the key must not appear
	// as a null on records that carry no claim.
	ReleaseClaim  *transitionWireReleaseClaim `json:"release_claim,omitempty"`
	SchemaVersion int                         `json:"schema_version"`
}

// releaseClaimToWire converts a ReleaseClaim to its wire representation.
// It returns nil for a nil claim, which the omitempty tag then drops.
func releaseClaimToWire(c *ReleaseClaim) *transitionWireReleaseClaim {
	if c == nil {
		return nil
	}
	wire := &transitionWireReleaseClaim{
		DispatchHeadSHA: c.DispatchHeadSHA,
		MergeTargetRef:  c.MergeTargetRef,
		MergeTargetSHA:  c.MergeTargetSHA,
	}
	if c.RemoteEndpoint != nil {
		wire.RemoteEndpoint = &transitionWireRemoteEndpoint{
			WorkerName: c.RemoteEndpoint.WorkerName,
			Host:       c.RemoteEndpoint.Host,
			RepoPath:   c.RemoteEndpoint.RepoPath,
		}
	}
	return wire
}

// releaseClaimFromWire converts a wire release claim back to its typed form.
func releaseClaimFromWire(w *transitionWireReleaseClaim) *ReleaseClaim {
	if w == nil {
		return nil
	}
	claim := &ReleaseClaim{
		DispatchHeadSHA: w.DispatchHeadSHA,
		MergeTargetRef:  w.MergeTargetRef,
		MergeTargetSHA:  w.MergeTargetSHA,
	}
	if w.RemoteEndpoint != nil {
		claim.RemoteEndpoint = &RemoteEndpoint{
			WorkerName: w.RemoteEndpoint.WorkerName,
			Host:       w.RemoteEndpoint.Host,
			RepoPath:   w.RemoteEndpoint.RepoPath,
		}
	}
	return claim
}

// stateToWire converts a State to its wire representation.
func stateToWire(s State) transitionWireState {
	return transitionWireState{
		StateID:   s.StateID,
		RunID:     s.RunID,
		NodeID:    s.NodeID,
		EnteredAt: s.EnteredAt,
		TransitionHistory: transitionWireCommitRange{
			FirstCommitSHA: s.TransitionHistory.FirstCommitSHA,
			LastCommitSHA:  s.TransitionHistory.LastCommitSHA,
		},
	}
}

// MarshalTransitionRecord serialises a Transition to the typed JSON bytes that
// must be stored at the canonical sibling-file path within a checkpoint commit's
// tree (execution-model.md §4.4.EM-018).
//
// The returned bytes are a single JSON object whose top-level field names use
// snake_case as declared in the §6.1 RECORD Transition schema. The
// schema_version field in the output equals tr.SchemaVersion and MUST equal the
// commit's Harmonik-Schema-Version trailer per §4.4.EM-018; callers MUST call
// ValidateTransitionSchemaVersion before writing the commit to enforce this.
// The schema_version value is the N-1-readable version sentinel per §4.4.EM-022.
//
// MarshalTransitionRecord does not validate the Transition; callers SHOULD
// ensure tr.Valid() == true before marshaling.
func MarshalTransitionRecord(tr Transition) ([]byte, error) {
	wire := transitionWire{
		TransitionID:      tr.TransitionID,
		RunID:             tr.RunID,
		FromState:         stateToWire(tr.FromState),
		ToState:           stateToWire(tr.ToState),
		ActorRole:         tr.ActorRole,
		CandidateActions:  tr.CandidateActions,
		ChosenAction:      tr.ChosenAction,
		PolicyVersion:     tr.PolicyVersion,
		Evidence:          tr.Evidence,
		VerifierMetrics:   tr.VerifierMetrics,
		Confidence:        tr.Confidence,
		OutcomeStatus:     tr.OutcomeStatus,
		TransitionKind:    tr.TransitionKind,
		RollbackToStateID: tr.RollbackToStateID,
		ReleaseClaim:      releaseClaimToWire(tr.ReleaseClaim),
		SchemaVersion:     tr.SchemaVersion,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("MarshalTransitionRecord: %w", err)
	}
	return data, nil
}

// wireToState converts a wire state back to its typed form.
func wireToState(w transitionWireState) State {
	return State{
		StateID:   w.StateID,
		RunID:     w.RunID,
		NodeID:    w.NodeID,
		EnteredAt: w.EnteredAt,
		TransitionHistory: CommitRange{
			FirstCommitSHA: w.TransitionHistory.FirstCommitSHA,
			LastCommitSHA:  w.TransitionHistory.LastCommitSHA,
		},
	}
}

// UnmarshalTransitionRecord decodes the typed JSON bytes of a transition-record
// sibling file back into a Transition (execution-model.md §4.4.EM-018,
// §4.4.EM-019).
//
// EM-019 requires the record to be retrievable from the checkpoint commit alone,
// with no cross-commit index. This function is the decode half of that contract:
// give it the bytes of
// .harmonik/transitions/<run_id>/<transition_id>.json and it returns the record
// the daemon wrote.
//
// UnmarshalTransitionRecord does NOT validate the decoded record. A caller that
// depends on the record's shape MUST check Valid() on the result. Recovery paths
// under §4.7.EM-031b treat a record that fails Valid() as a corrupt claim and
// take the safe branch.
//
// The decoder is strict about JSON syntax and lenient about unknown fields, per
// the EM-022 N-1 readability contract: a reader at schema version N-1 MUST parse
// a record written at version N and treat added fields as unknown but non-fatal.
func UnmarshalTransitionRecord(data []byte) (Transition, error) {
	var wire transitionWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return Transition{}, fmt.Errorf("UnmarshalTransitionRecord: %w", err)
	}
	return Transition{
		TransitionID:      wire.TransitionID,
		RunID:             wire.RunID,
		FromState:         wireToState(wire.FromState),
		ToState:           wireToState(wire.ToState),
		ActorRole:         wire.ActorRole,
		CandidateActions:  wire.CandidateActions,
		ChosenAction:      wire.ChosenAction,
		PolicyVersion:     wire.PolicyVersion,
		Evidence:          Evidence(wire.Evidence),
		VerifierMetrics:   VerifierMetrics(wire.VerifierMetrics),
		Confidence:        wire.Confidence,
		OutcomeStatus:     wire.OutcomeStatus,
		TransitionKind:    wire.TransitionKind,
		RollbackToStateID: wire.RollbackToStateID,
		ReleaseClaim:      releaseClaimFromWire(wire.ReleaseClaim),
		SchemaVersion:     wire.SchemaVersion,
	}, nil
}

// ValidateTransitionSchemaVersion checks that tr.SchemaVersion equals
// commitSchemaVersion (execution-model.md §4.4.EM-018, §4.4.EM-022).
//
// The sibling file's schema_version field MUST match the commit's
// Harmonik-Schema-Version trailer value (EM-018). Both values are the N-1-readable
// version sentinel per EM-022: readers MUST accept the immediately prior schema
// version (N-1). A mismatch between the sibling file and the trailer is an integrity
// violation that prevents the checkpoint commit from being assembled.
//
// Returns nil when the versions agree, or an error with both values when they
// disagree.
func ValidateTransitionSchemaVersion(tr Transition, commitSchemaVersion int) error {
	if tr.SchemaVersion != commitSchemaVersion {
		return fmt.Errorf(
			"ValidateTransitionSchemaVersion: schema_version mismatch: "+
				"transition.SchemaVersion=%d, commitSchemaVersion=%d",
			tr.SchemaVersion, commitSchemaVersion,
		)
	}
	return nil
}
