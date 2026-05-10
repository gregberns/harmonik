package handlercontract

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// LaunchSpec is the record the daemon delivers to every Handler.Launch call
// per specs/handler-contract.md §6.1 (HC-006).
//
// The daemon MUST populate all required fields. Optional fields (BeadID,
// SnapshotToken) are nil when absent. The record is serialised to JSON and
// delivered to the handler subprocess via stdin (default) or a file-path
// argument when the payload exceeds 1 MiB (HC-005).
//
// Schema evolution follows the N-1 readability contract per
// [operator-nfr.md §4.5]: adding an optional field is non-breaking;
// removing or renaming a field is breaking and requires a migration release.
// SchemaVersion is incremented on every normative schema change.
//
// Current SchemaVersion: 1 (initial).
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

	// SnapshotToken is the opaque token for reconciliation-investigator handlers,
	// binding the agent's reads to a captured (git_head_hash, beads_audit_entry_id)
	// per [reconciliation/spec.md §9.4b] (bootstrap phase).
	// Optional: present only for investigator handlers; nil otherwise.
	SnapshotToken *core.SnapshotToken `json:"snapshot_token,omitempty"`

	// SchemaVersion is the integer schema version of this LaunchSpec record.
	// N-1 readable per [operator-nfr.md §4.5].
	// Required; must be positive; current value is 1.
	SchemaVersion int `json:"schema_version"`
}

// LaunchSpecSchemaVersion is the current schema version of LaunchSpec.
// Increment this constant on every normative schema change.
const LaunchSpecSchemaVersion = 1

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
	if s.SchemaVersion <= 0 {
		return fmt.Errorf("handlercontract: LaunchSpec.SchemaVersion must be positive, got %d", s.SchemaVersion)
	}
	return nil
}
