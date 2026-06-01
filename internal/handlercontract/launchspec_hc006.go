package handlercontract

import (
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// ReviewLoopPhase identifies which phase of a review-loop dispatch this
// LaunchSpec represents, per specs/handler-contract.md §6.1 HC-006.
//
// Valid values are the three declared constants below. The field is present
// in LaunchSpec only when workflow_mode = review-loop.
//
// TODO(hk-7om2q.6): migrate to core.ReviewLoopPhase once the typed wrapper is
// added to internal/core per the typed-alias-deferral pattern.
type ReviewLoopPhase string

const (
	// ReviewLoopPhaseImplementerInitial is the first implementer launch in a
	// review-loop cycle. No prior Claude session exists.
	ReviewLoopPhaseImplementerInitial ReviewLoopPhase = "implementer-initial"

	// ReviewLoopPhaseImplementerResume is a subsequent implementer launch
	// resuming a prior Claude Code session (claude --resume <id>).
	ReviewLoopPhaseImplementerResume ReviewLoopPhase = "implementer-resume"

	// ReviewLoopPhaseReviewer is the reviewer launch within a review-loop
	// cycle. Each reviewer launch is a fresh Claude session.
	ReviewLoopPhaseReviewer ReviewLoopPhase = "reviewer"
)

// Valid reports whether p is one of the declared ReviewLoopPhase constants.
func (p ReviewLoopPhase) Valid() bool {
	switch p {
	case ReviewLoopPhaseImplementerInitial,
		ReviewLoopPhaseImplementerResume,
		ReviewLoopPhaseReviewer:
		return true
	default:
		return false
	}
}

// LaunchSpec is the record the daemon delivers to every Handler.Launch call
// per specs/handler-contract.md §6.1 (HC-006).
//
// The daemon MUST populate all required fields. Optional fields (BeadID,
// SnapshotToken, WorkflowMode, Phase, IterationCount, ClaudeSessionID) are nil
// when absent. The record is serialised to JSON and delivered to the handler
// subprocess via stdin (default) or a file-path argument when the payload
// exceeds 1 MiB (HC-005).
//
// Schema evolution follows the N-1 readability contract per
// [operator-nfr.md §4.5]: adding an optional field is non-breaking;
// removing or renaming a field is breaking and requires a migration release.
// SchemaVersion is incremented on every normative schema change.
//
// Current SchemaVersion: 2 (added WorkflowMode, Phase, IterationCount,
// ClaudeSessionID per HC-006).
type LaunchSpec struct {
	// RunID is the UUID of the run that owns this session.
	// Required per [execution-model.md §4.3 Run].
	RunID core.RunID `json:"run_id"`

	// WorkflowID is the UUID of the workflow that contains the run.
	// Required per [execution-model.md §4.1 Workflow].
	WorkflowID core.WorkflowID `json:"workflow_id"`

	// NodeID is the node identifier within the workflow graph.
	// Required; non-empty opaque string per [execution-model.md §4.1].
	NodeID core.NodeID `json:"node_id"`

	// AgentType is the lowercase-hyphenated agent-type identifier.
	// Required per [architecture.md §6.1 Agent type identifier].
	AgentType core.AgentType `json:"agent_type"`

	// WorkspacePath is the absolute filesystem path to the run's worktree.
	// Required per [workspace-model.md §4.1].
	WorkspacePath string `json:"workspace_path"`

	// RequiredSkills is the ordered list of resolved skill names that must
	// be provisioned before agent_ready per HC-046 / HC-047.
	// Required (may be empty slice; nil is treated as empty).
	RequiredSkills []string `json:"required_skills"`

	// SkillSearchPaths is the ordered list of absolute directory paths
	// searched for skill packages. Resolution takes the first match per HC-047.
	// Required (may be empty slice; nil is treated as empty).
	SkillSearchPaths []string `json:"skill_search_paths"`

	// Timeout is the wall-clock budget in seconds for the run's work.
	// Required; must be positive. Zero is forbidden (HC-006).
	Timeout int `json:"timeout"`

	// ProvisioningTimeout is the skill-provisioning deadline in seconds,
	// distinct from Timeout. Default is 60 per HC-048a.
	// Required; must be positive.
	ProvisioningTimeout int `json:"provisioning_timeout"`

	// Budget names the Budget registered in the policy-layer registry
	// per [control-points.md §4.5 CP-022].
	// Required.
	Budget core.BudgetRef `json:"budget"`

	// FreedomProfileRef names the freedom profile governing this run
	// per [control-points.md §6.7].
	// Required; non-empty string.
	FreedomProfileRef string `json:"freedom_profile_ref"`

	// BeadID is the opaque bead correlation identifier.
	// Optional: present when the run is bead-tied per [execution-model.md §4.3];
	// nil otherwise.
	BeadID *string `json:"bead_id,omitempty"`

	// SnapshotToken is the JSON-serialized core.SnapshotToken for
	// reconciliation-investigator handlers, binding the agent's reads to a
	// captured (git_head_hash, beads_audit_entry_id) per RC-015 and
	// [specs/handler-contract.md §6.1 HC-006].
	//
	// Declared String|None in HC-006: the wire value is a JSON string whose
	// contents are the JSON-encoded SnapshotToken record
	// ({git_head_hash, beads_audit_entry_id, captured_at_timestamp}).
	// Use MarshalSnapshotToken to encode and ParseSnapshotToken to decode.
	//
	// Optional: present only for investigator handlers; nil otherwise.
	SnapshotToken *string `json:"snapshot_token,omitempty"`

	// WorkflowMode is the dispatch shape resolved for this run per
	// specs/handler-contract.md §6.1 HC-006 and execution-model.md §4.3.EM-012.
	// Optional: present iff the daemon resolved a non-default mode; omitted for
	// single-handler runs. Observational only — handlers MUST NOT branch on this
	// field; it is supplied for logging and skill-loading hints per §4.1.HC-003a.
	//
	// TODO(hk-7om2q.6): replace *string with *core.WorkflowMode once
	// internal/core exports the typed wrapper (T-WM-001 closed; pending merge).
	WorkflowMode *string `json:"workflow_mode,omitempty"`

	// Phase identifies which phase of a multi-phase dispatch this LaunchSpec
	// represents, per specs/handler-contract.md §6.1 HC-006.
	// Optional: present iff the run is in a multi-phase mode (e.g., review-loop).
	// For review-loop the domain is {implementer-initial, implementer-resume, reviewer}.
	// Must be present iff IterationCount is present (co-presence rule).
	Phase *ReviewLoopPhase `json:"phase,omitempty"`

	// IterationCount is the 1-based iteration index within a multi-phase mode
	// that iterates, per specs/handler-contract.md §6.1 HC-006.
	// Optional: present iff Phase is present. For review-loop: 1..3 per
	// [operator-nfr.md §4.1 ON-004].
	// Must be present iff Phase is present (co-presence rule).
	IterationCount *int `json:"iteration_count,omitempty"`

	// ClaudeSessionID carries the Claude Code session identifier for
	// `claude --resume <id>`, per specs/handler-contract.md §6.1 HC-006.
	// Optional: present iff Phase = implementer-resume. The reviewer phase and
	// implementer-initial phase MUST omit this field (no prior session to resume).
	// Distinct from harmonik's own SessionID per §6.1.
	ClaudeSessionID *string `json:"claude_session_id,omitempty"`

	// SchemaVersion is the integer schema version of this LaunchSpec record.
	// N-1 readable per [operator-nfr.md §4.5].
	// Required; must be positive; current value is 2.
	SchemaVersion int `json:"schema_version"`
}

// LaunchSpecSchemaVersion is the current schema version of LaunchSpec.
// Increment this constant on every normative schema change.
const LaunchSpecSchemaVersion = 2

// Valid reports whether s is a well-formed LaunchSpec ready to be delivered
// to a handler subprocess. It checks all required fields for non-zero values;
// it does NOT validate that the IDs correspond to live workflow state.
//
// Returns a non-nil error describing the first invalid field found.
func (s LaunchSpec) Valid() error {
	if s.RunID == (core.RunID{}) {
		return fmt.Errorf("handlercontract: LaunchSpec.RunID must be non-zero")
	}
	if s.WorkflowID == (core.WorkflowID{}) {
		return fmt.Errorf("handlercontract: LaunchSpec.WorkflowID must be non-zero")
	}
	if s.NodeID == "" {
		return fmt.Errorf("handlercontract: LaunchSpec.NodeID must be non-empty")
	}
	if s.AgentType == "" {
		return fmt.Errorf("handlercontract: LaunchSpec.AgentType must be non-empty")
	}
	if s.WorkspacePath == "" {
		return fmt.Errorf("handlercontract: LaunchSpec.WorkspacePath must be non-empty")
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("handlercontract: LaunchSpec.Timeout must be positive, got %d", s.Timeout)
	}
	if s.ProvisioningTimeout <= 0 {
		return fmt.Errorf("handlercontract: LaunchSpec.ProvisioningTimeout must be positive, got %d", s.ProvisioningTimeout)
	}
	if !s.Budget.Valid() {
		return fmt.Errorf("handlercontract: LaunchSpec.Budget must be non-empty")
	}
	if s.FreedomProfileRef == "" {
		return fmt.Errorf("handlercontract: LaunchSpec.FreedomProfileRef must be non-empty")
	}
	// Co-presence rule: Phase and IterationCount must either both be present
	// or both be absent per specs/handler-contract.md §6.1 HC-006.
	if (s.Phase == nil) != (s.IterationCount == nil) {
		return fmt.Errorf(
			"handlercontract: LaunchSpec.Phase and IterationCount must both be present or both absent; got phase=%v iteration_count=%v",
			s.Phase, s.IterationCount,
		)
	}
	// Phase value must be a declared ReviewLoopPhase constant when present.
	if s.Phase != nil && !s.Phase.Valid() {
		return fmt.Errorf(
			"handlercontract: LaunchSpec.Phase %q is not a valid ReviewLoopPhase",
			*s.Phase,
		)
	}
	// IterationCount must be positive when present.
	if s.IterationCount != nil && *s.IterationCount <= 0 {
		return fmt.Errorf(
			"handlercontract: LaunchSpec.IterationCount must be positive when present, got %d",
			*s.IterationCount,
		)
	}
	// ClaudeSessionID must be present iff Phase = implementer-resume.
	if s.ClaudeSessionID != nil {
		if s.Phase == nil || *s.Phase != ReviewLoopPhaseImplementerResume {
			return fmt.Errorf(
				"handlercontract: LaunchSpec.ClaudeSessionID must only be set when Phase = implementer-resume; got phase=%v",
				s.Phase,
			)
		}
	}
	if s.Phase != nil && *s.Phase == ReviewLoopPhaseImplementerResume && s.ClaudeSessionID == nil {
		return fmt.Errorf(
			"handlercontract: LaunchSpec.ClaudeSessionID must be present when Phase = implementer-resume",
		)
	}
	if s.SchemaVersion <= 0 {
		return fmt.Errorf("handlercontract: LaunchSpec.SchemaVersion must be positive, got %d", s.SchemaVersion)
	}
	return nil
}

// MarshalSnapshotToken JSON-encodes tok and returns the resulting string,
// suitable for assignment to LaunchSpec.SnapshotToken.
//
// Per RC-015 and specs/handler-contract.md §6.1 HC-006, the snapshot_token
// wire field is String|None: a JSON string whose contents are the JSON-encoded
// SnapshotToken record. The caller MUST verify tok.Valid() before encoding.
//
// MarshalSnapshotToken returns an error only when json.Marshal fails, which
// cannot happen for a plain struct with no custom marshal logic.
func MarshalSnapshotToken(tok core.SnapshotToken) (string, error) {
	b, err := json.Marshal(tok)
	if err != nil {
		return "", fmt.Errorf("handlercontract: MarshalSnapshotToken: %w", err)
	}
	return string(b), nil
}

// ParseSnapshotToken decodes a LaunchSpec.SnapshotToken string (JSON-encoded
// SnapshotToken record) back into a core.SnapshotToken.
//
// Per RC-015 and specs/handler-contract.md §6.1 HC-006, the snapshot_token
// wire field is String|None carrying a JSON-encoded SnapshotToken record. The
// investigator subprocess calls this function to recover the structured token
// from the LaunchSpec it received.
//
// Returns an error if s is not valid JSON or the decoded token fails
// core.SnapshotToken.Valid().
func ParseSnapshotToken(s string) (core.SnapshotToken, error) {
	var tok core.SnapshotToken
	if err := json.Unmarshal([]byte(s), &tok); err != nil {
		return core.SnapshotToken{}, fmt.Errorf("handlercontract: ParseSnapshotToken: %w", err)
	}
	if !tok.Valid() {
		return core.SnapshotToken{}, fmt.Errorf(
			"handlercontract: ParseSnapshotToken: decoded token fails Valid() (missing required fields)",
		)
	}
	return tok, nil
}
