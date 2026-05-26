package core

// EffectiveSkillSet computes the CP-050 effective skill set for an agent
// launched into a node: the set-union of (a) the node's declared
// required_skills and (b) the assigned role's default_skills per
// specs/control-points.md §4.11.CP-050.
//
// Ordering: node-declared skills appear first (preserving their declaration
// order); role defaults that are not already present are appended after.
//
// Deduplication is name-exact (string equality). Neither slice is mutated.
// nil inputs are treated as empty.
//
// Consumption of the returned set (resolution against skill_search_paths[],
// provisioning into the agent process shape, skills_provisioned emission, and
// fail-launch on resolution failure) is owned by handler-contract.md §4.11;
// this function owns only the union step.
//
// Spec: specs/control-points.md §4.11.CP-050.
// Bead: hk-a8bg.52.
func EffectiveSkillSet(nodeRequiredSkills []string, roleDefaultSkills []SkillName) []string {
	seen := make(map[string]struct{}, len(nodeRequiredSkills)+len(roleDefaultSkills))
	result := make([]string, 0, len(nodeRequiredSkills)+len(roleDefaultSkills))

	for _, s := range nodeRequiredSkills {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	for _, s := range roleDefaultSkills {
		name := string(s)
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}
