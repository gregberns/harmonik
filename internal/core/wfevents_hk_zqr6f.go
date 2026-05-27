package core

// wfevents_hk_zqr6f.go — event-bus payload for the skills_resolved event type.
//
// Emitted at workflow-ingest time when a node's skills_ref attribute resolves
// to a skill_sets[] entry in the run's policy YAML per control-points.md §4.13
// CP-057. One event per node that carries a non-empty skills_ref.
//
// Spec ref: specs/control-points.md §4.13 CP-057.
// Bead ref: hk-zqr6f.

// SkillsResolvedPayload is the typed event payload for the skills_resolved event.
//
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
// Durability class: O (ordinary — observability of ingest-time resolution; the
// authoritative resolved skill set is held in the graph's parsed AST).
//
// Emitted once per node whose skills_ref attribute successfully resolves to a
// skill_sets[] entry per CP-057 during workflow-ingest. A missing or unresolvable
// skills_ref is an ingest error (structural failure) and does NOT produce this event.
//
// # Payload fields
//
//   - node_id    — the workflow graph node ID whose skills_ref was resolved
//   - skills_ref — the skills_ref attribute value (the skill-set name)
//   - skills     — the resolved skills list from the matching skill_sets[] entry
type SkillsResolvedPayload struct {
	// NodeID is the workflow graph node ID whose skills_ref was resolved.
	// Required (non-empty).
	NodeID string `json:"node_id"`

	// SkillsRef is the skills_ref attribute value on the node — the name of the
	// skill_sets[] entry that was looked up. Required (non-empty).
	SkillsRef string `json:"skills_ref"`

	// Skills is the resolved skills list from the matching skill_sets[] entry.
	// May be empty if the named skill set declares no skills.
	Skills []string `json:"skills"`
}

// Valid reports whether p is a well-formed SkillsResolvedPayload.
func (p SkillsResolvedPayload) Valid() bool {
	return p.NodeID != "" && p.SkillsRef != ""
}
