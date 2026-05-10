package workspace

import (
	"encoding/json"
	"fmt"
	"os"
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

// WriteSessionMetadataSidecarAtomic writes sidecar to the canonical path target
// using the atomic-write discipline mandated by workspace-model.md §4.7.WM-026:
//
//  1. Validate sidecar (s.Valid()).
//  2. Write JSON to a sibling temp file (harmonik.meta.json.tmp-<pid>).
//  3. fsync the temp file so data is durable before rename.
//  4. rename(2) the temp file to target (POSIX rename is atomic within one fs).
//  5. fsync the parent directory to durably record the rename.
//
// The caller MUST call WriteSessionMetadataSidecarAtomic BEFORE the handler
// subprocess is launched and BEFORE workspace_leased is emitted (WM-016's
// 4-step ordering gate). The sidecar MUST be durable before either event.
//
// Use SessionMetadataSidecarPath to construct target from workspacePath and sessionID.
//
// Step 5 (parent-dir fsync) is best-effort on macOS/APFS per spec but MUST be
// attempted for spec compliance.
//
// Returns an error if s.Valid() fails, or if any I/O step fails.
func WriteSessionMetadataSidecarAtomic(target string, s *SessionMetadataSidecar) error {
	if err := s.Valid(); err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: invalid sidecar: %w", err)
	}

	content, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: marshal: %w", err)
	}

	dir := filepath.Dir(target)
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments + session_id; not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Write: %w", err)
	}

	// Step 3: fsync temp file before rename.
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Sync (pre-rename): %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Close (pre-rename): %w", err)
	}

	// Step 4: atomic rename.
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Rename %q → %q: %w", tmpPath, target, err)
	}

	// Step 5: parent-dir fsync — best-effort on macOS/APFS per spec.
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per WM-026 / WM-013a precedent
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("workspace: WriteSessionMetadataSidecarAtomic: Close dir fd: %w", err)
	}

	return nil
}

// ReadSessionMetadataSidecar reads and parses the sidecar file at target.
//
// Returns (nil, nil) when target does not exist — the caller interprets
// absence as "no sidecar yet" per WM-026. Returns an error for I/O or parse
// failures other than os.IsNotExist.
func ReadSessionMetadataSidecar(target string) (*SessionMetadataSidecar, error) {
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments + session_id; not user input
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // caller interprets nil as "no sidecar" per WM-026
		}
		return nil, fmt.Errorf("workspace: ReadSessionMetadataSidecar: ReadFile %q: %w", target, err)
	}

	var s SessionMetadataSidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("workspace: ReadSessionMetadataSidecar: Unmarshal %q: %w", target, err)
	}
	if err := s.Valid(); err != nil {
		return nil, fmt.Errorf("workspace: ReadSessionMetadataSidecar: parsed sidecar at %q is not valid: %w", target, err)
	}
	return &s, nil
}
