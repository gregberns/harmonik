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
	SchemaVersion     int                 `json:"schema_version"`
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
		SchemaVersion:     tr.SchemaVersion,
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("MarshalTransitionRecord: %w", err)
	}
	return data, nil
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
