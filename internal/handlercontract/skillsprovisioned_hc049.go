package handlercontract

// skillsprovisioned_hc049.go — SkillsProvisionedMsg wire struct for HC-049.
//
// Spec refs: specs/handler-contract.md §4.11.HC-049 and §6.5 (progress-stream
// wire protocol). Bead hk-8i31.58.
//
// HC-049 requires that after successful skill provisioning and BEFORE
// agent_ready, the handler MUST emit a skills_provisioned progress-stream
// message carrying the set of installed skills.  The on-wire payload schema
// is co-owned by event-model.md §8.3.8 (§6.3 field list: run_id, session_id,
// skills[] where each skill has name, source_path, version?).
//
// Ordering invariant (HC-INV-004, handler-contract.md §5):
//
//	handler_capabilities → session_log_location → skills_provisioned → agent_ready → …
//
// The handler MUST NOT emit agent_ready before skills_provisioned.

// SkillProvisionedEntry is one entry in the SkillsProvisionedMsg.Skills slice.
// Each entry describes a single installed skill.
//
// # Wire fields (event-model.md §8.3.8)
//
//   - name        — the skill name as declared in LaunchSpec.required_skills
//   - source_path — the resolved filesystem path from which the skill was installed
//   - version     — optional skill package version; omitted when unavailable
type SkillProvisionedEntry struct {
	// Name is the skill name as declared in LaunchSpec.required_skills.
	// Required (non-empty).
	Name string `json:"name"`

	// SourcePath is the resolved filesystem path from which the skill was
	// installed. Required (non-empty). Declared as the resolved path per
	// LaunchSpec.skill_search_paths.
	SourcePath string `json:"source_path"`

	// Version is the optional skill package version. Omitted (omitempty)
	// when the skill has no detectable version. Nil when unavailable.
	Version *string `json:"version,omitempty"`
}

// SkillsProvisionedMsg is the on-wire NDJSON message the handler subprocess
// MUST emit after completing skill provisioning and BEFORE agent_ready,
// per specs/handler-contract.md §4.11.HC-049.
//
// This message is emitted exactly once per session, after the handler has
// ensured every skill in LaunchSpec.required_skills is available. It MUST
// precede the agent_ready message; the daemon watcher translates it into the
// skills_provisioned bus event (event-model.md §8.3.8).
//
// An empty skills slice is valid when LaunchSpec.required_skills is empty
// (no skills were required or provisioned).
//
// # Wire fields (event-model.md §8.3.8; handler-contract.md §4.11.HC-049)
//
//   - type       — always ProgressMsgTypeSkillsProvisioned ("skills_provisioned")
//   - run_id     — the run in whose context provisioning occurred
//   - session_id — the session identifier assigned by the handler
//   - skills     — the installed skill entries (non-nil; may be empty slice)
type SkillsProvisionedMsg struct {
	// Type is always ProgressMsgTypeSkillsProvisioned; used by the watcher
	// as the dispatch key.
	Type string `json:"type"`

	// RunID is the run in whose context provisioning occurred.
	// Required (non-empty).
	RunID string `json:"run_id"`

	// SessionID is the handler-assigned session identifier.
	// Required (non-empty).
	SessionID string `json:"session_id"`

	// Skills is the list of installed skill entries. Required (non-nil).
	// May be an empty slice when no skills were required or provisioned.
	Skills []SkillProvisionedEntry `json:"skills"`
}
