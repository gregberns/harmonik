// Package agentmanifest: check.go — full filesystem validation (harmonik agent check).
// Spec: .kerf/works/agent-manifest/SPEC.md §3 (C-C checks).
package agentmanifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
//     or is the reserved terminal "operator" (C-C parent-intent check).
//
// agentsDir is the absolute path to .harmonik/agents/.
// repoRoot is the project root used to resolve path-bearing context refs.
//
// Returns nil when the folder is well-formed; one or more Defects otherwise.
func Check(agentsDir, typeName, repoRoot string) []Defect {
	tf, err := Load(agentsDir, typeName)
	if err != nil {
		return []Defect{{Field: "load", Message: err.Error()}}
	}

	var defects []Defect

	// C-C parent-intent check (SPEC §3): must name an existing type with a
	// readable soul.md, or the reserved terminal "operator".
	pi := tf.Manifest.Identity.ParentIntent
	if pi != "operator" {
		parentSoul := filepath.Join(agentsDir, pi, soulFile)
		if _, statErr := os.Stat(parentSoul); statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				defects = append(defects, Defect{
					Field:   "identity.parent_intent",
					Message: fmt.Sprintf("parent type %q has no soul.md (type folder does not exist or is missing soul.md)", pi),
				})
			} else {
				defects = append(defects, Defect{
					Field:   "identity.parent_intent",
					Message: fmt.Sprintf("cannot stat parent type %q soul.md: %v", pi, statErr),
				})
			}
		}
	}

	// Ref resolution check (SPEC §6): each context[].ref must resolve.
	for i, c := range tf.Manifest.Context {
		field := fmt.Sprintf("context[%d].ref", i)
		if strings.Contains(c.Ref, "/") {
			// Path-bearing ref: taken literally, relative to repoRoot.
			absPath := filepath.Join(repoRoot, c.Ref)
			if _, statErr := os.Stat(absPath); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					defects = append(defects, Defect{
						Field:   field,
						Message: fmt.Sprintf("path-bearing ref %q does not exist (resolved to %q)", c.Ref, absPath),
					})
				} else {
					defects = append(defects, Defect{
						Field:   field,
						Message: fmt.Sprintf("cannot stat path-bearing ref %q: %v", c.Ref, statErr),
					})
				}
			}
		} else {
			// Bare ref: _skills/ first, then type folder.
			if _, refErr := ResolveRef(agentsDir, typeName, c.Ref); refErr != nil {
				defects = append(defects, Defect{
					Field:   field,
					Message: fmt.Sprintf("bare ref %q not found in _skills/ or type folder %q", c.Ref, typeName),
				})
			}
		}
	}

	return defects
}
