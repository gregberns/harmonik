package workspace

import (
	"fmt"
	"path/filepath"

	"github.com/gregberns/harmonik/internal/core"
)

// SessionMetadataSidecar is the record written to each session's sidecar file
// per workspace-model.md §6.1 and §4.7.WM-026.
//
// The sidecar is written at:
//
//	${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json
//
// and MUST be written BEFORE the handler launches, using the atomic write
// discipline (temp+fsync+rename+parent-fsync) per WM-026.
//
// It is the authoritative join key for CASS indexing AND the authoritative
// source for agent_type at merge-time implementer identification per WM-022.
//
// Schema evolution: N-1 readable per §6.4 (add optional = non-breaking).
// Current SchemaVersion: 1 (initial).
type SessionMetadataSidecar struct {
	// RunID is the UUID of the run that owns this session.
	// Required per [execution-model.md §4.3 Run].
	RunID core.RunID `json:"run_id"`

	// NodeID is the node identifier within the workflow graph.
	// Inside expanded sub-workflows, the value is namespaced as
	// "<parent_node_id>/<sub_node_id>" per WM-026.
	// Required; non-empty.
	NodeID core.NodeID `json:"node_id"`

	// AgentType is the agent-type conformance-class identifier per
	// [architecture.md §4.7 AR-024]. The workspace manager reads this field
	// to identify whether a session is agentic at merge-time per WM-022.
	// Required; non-empty.
	AgentType core.AgentType `json:"agent_type"`

	// WorkflowID is the UUID of the workflow that contains the run.
	// Required per [execution-model.md §4.1 Workflow].
	WorkflowID core.WorkflowID `json:"workflow_id"`

	// BeadID is the opaque bead correlation identifier.
	// Optional: present when the run is bead-tied per
	// [beads-integration.md §4 BI-020]; nil otherwise.
	BeadID *core.BeadID `json:"bead_id,omitempty"`

	// LaunchedAt is the RFC 3339 wall-clock timestamp at which the session
	// was launched (handler subprocess spawned). Required; non-empty.
	LaunchedAt string `json:"launched_at"`

	// SchemaVersion is the integer schema version of this sidecar record.
	// N-1 readable per workspace-model.md §6.4.
	// Required; must be positive; current value is 1.
	SchemaVersion int `json:"schema_version"`
}

// SessionMetadataSidecarSchemaVersion is the current schema version of
// SessionMetadataSidecar. Increment this constant on every normative schema change.
const SessionMetadataSidecarSchemaVersion = 1

// Valid reports whether s is a well-formed SessionMetadataSidecar ready to be
// written to disk. It checks all required fields for non-zero / non-empty values.
// It does NOT validate that the run is live or that the sidecar path exists.
//
// Returns a non-nil error describing the first invalid field found.
func (s SessionMetadataSidecar) Valid() error {
	if s.RunID == (core.RunID{}) {
		return fmt.Errorf("workspace: SessionMetadataSidecar.RunID must be non-zero")
	}
	if s.NodeID == "" {
		return fmt.Errorf("workspace: SessionMetadataSidecar.NodeID must be non-empty")
	}
	if s.AgentType == "" {
		return fmt.Errorf("workspace: SessionMetadataSidecar.AgentType must be non-empty")
	}
	if s.WorkflowID == (core.WorkflowID{}) {
		return fmt.Errorf("workspace: SessionMetadataSidecar.WorkflowID must be non-zero")
	}
	if s.LaunchedAt == "" {
		return fmt.Errorf("workspace: SessionMetadataSidecar.LaunchedAt must be non-empty")
	}
	if s.SchemaVersion <= 0 {
		return fmt.Errorf("workspace: SessionMetadataSidecar.SchemaVersion must be positive, got %d", s.SchemaVersion)
	}
	return nil
}

// SessionMetadataSidecarPath returns the canonical path for a session's sidecar
// file per workspace-model.md §6.2 S06:
//
//	${workspace_path}/.harmonik/sessions/<session_id>/harmonik.meta.json
//
// The caller MUST pass the absolute worktree path (workspace_path) and the
// session_id as a non-empty string. Path construction is deterministic.
func SessionMetadataSidecarPath(workspacePath, sessionID string) string {
	return filepath.Join(workspacePath, ".harmonik", "sessions", sessionID, "harmonik.meta.json")
}
