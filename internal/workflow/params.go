package workflow

// params.go — shared template-parameter primitives (WG-045).
//
// Token grammar: __[A-Z][A-Z0-9_]*__ (uppercase letter followed by zero or more
// uppercase letters, digits, or underscores, enclosed in double-underscores).
// After substitution any residual __TOKEN__ is a launch-time error naming the
// offending token.
//
// SECURITY / ORDERING (WG-046): production loaders substitute per-attribute AFTER
// parse — read → parse(template) → substitute(per-attribute, context-aware) →
// validate → dispatch, implemented by substituteGraphParams (params_graph.go),
// which shell-quotes tool_command values. Raw-source pre-parse substitution was a
// command- and DOT-structure-injection vector and MUST NOT be reintroduced on any
// load path that feeds a shell sink. This file holds the shared token regexp and
// residual-token error type consumed by substituteGraphParams.
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-045 — template-param trust boundary (untrusted).
//   - specs/workflow-graph.md §4 WG-046 — post-parse per-attribute ordering.
//
// Bead ref: hk-55zv2 (T5).
// Tags: mechanism

import (
	"fmt"
	"regexp"
	"strings"
)

// templateTokenRe matches __[A-Z][A-Z0-9_]*__ per WG-045.
var templateTokenRe = regexp.MustCompile(`__[A-Z][A-Z0-9_]*__`)

// ErrResidualToken is returned when one or more template tokens remain in the
// source after substitution (WG-045 launch-time error).
type ErrResidualToken struct {
	// Tokens is the deduplicated list of unresolved token names (without the __ delimiters).
	Tokens []string
}

func (e *ErrResidualToken) Error() string {
	return fmt.Sprintf("workflow: unresolved template token(s): %s (pass --param KEY=VALUE for each)",
		strings.Join(e.Tokens, ", "))
}
