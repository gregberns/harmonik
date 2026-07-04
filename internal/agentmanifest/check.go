// Package agentmanifest: check.go — full filesystem validation (harmonik agent check).
// Spec: .kerf/works/agent-manifest/SPEC.md §3 (C-C checks).
package agentmanifest

// Defect is a single validation failure found during Check.
type Defect struct {
	Field   string // field or aspect that failed (e.g. "context[1].ref")
	Message string // human-readable description
}

func (d Defect) String() string {
	return d.Field + ": " + d.Message
}

// Check performs full filesystem-level validation of the named type folder.
//
// It runs Load (schema + file presence) then additionally verifies:
//   - each context[].ref resolves under the ref resolution rule (SPEC §6):
//     bare refs checked against _skills/ then the type folder;
//     path-bearing refs checked relative to repoRoot.
//   - identity.parent_intent names an existing type with a readable soul.md,
//     or is the reserved terminal "operator".
//
// agentsDir is the absolute path to .harmonik/agents/.
// repoRoot is the project root used to resolve path-bearing context refs.
//
// Returns nil when the folder is well-formed; one or more Defects otherwise.
func Check(agentsDir, typeName, repoRoot string) []Defect {
	panic("not implemented")
}
