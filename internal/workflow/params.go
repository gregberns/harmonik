package workflow

// params.go — raw-source template-parameter substitution primitive (WG-045).
//
// SubstituteTemplateParams applies one non-recursive substitution pass over a raw
// .dot source string.  Token grammar: __[A-Z][A-Z0-9_]*__ (uppercase letter
// followed by zero or more uppercase letters, digits, or underscores, enclosed in
// double-underscores).  After substitution any residual __TOKEN__ is a launch-time
// error naming the offending token.  Token-free source is returned byte-identical.
//
// SECURITY / ORDERING (WG-046, reversed): production loaders NO LONGER substitute
// over raw source before parse — that context-blind splice was the command- and
// DOT-structure-injection vector.  The new ordering is read → parse(template) →
// substitute(per-attribute, context-aware) → validate → dispatch, implemented by
// substituteGraphParams (params_graph.go), which shell-quotes tool_command values.
// SubstituteTemplateParams is retained as the lower-level raw-string primitive
// (exported API + its unit tests); it MUST NOT be reintroduced on a load path that
// feeds a shell sink.  The shared ErrResidualToken type below is used by both.
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

// ErrResidualToken is returned by SubstituteTemplateParams when one or more
// template tokens remain in the source after substitution (WG-045 launch-time error).
type ErrResidualToken struct {
	// Tokens is the deduplicated list of unresolved token names (without the __ delimiters).
	Tokens []string
}

func (e *ErrResidualToken) Error() string {
	return fmt.Sprintf("workflow: unresolved template token(s): %s (pass --param KEY=VALUE for each)",
		strings.Join(e.Tokens, ", "))
}

// SubstituteTemplateParams applies a single, non-recursive substitution pass over
// src, replacing every __KEY__ occurrence with params[KEY].  Keys in params that do
// not appear in src are silently ignored.
//
// Returns the substituted string and nil when all tokens are resolved.
// Returns ("", *ErrResidualToken) when any __TOKEN__ remains after substitution.
// Returns (src, nil) immediately when params is empty and src contains no tokens
// (byte-identical no-op path per WG-045).
func SubstituteTemplateParams(src string, params map[string]string) (string, error) {
	// Fast-path: no tokens in src → byte-identical no-op.
	if !templateTokenRe.MatchString(src) {
		return src, nil
	}

	if len(params) > 0 {
		// Build a replacement func: __KEY__ → params[KEY] when present; leave
		// unrecognised tokens intact so the residual check catches them.
		src = templateTokenRe.ReplaceAllStringFunc(src, func(match string) string {
			// Strip the __ delimiters to get the key.
			key := match[2 : len(match)-2]
			if val, ok := params[key]; ok {
				return val
			}
			return match // leave unresolved tokens as-is
		})
	}

	// Residual check: any remaining __TOKEN__ is a launch-time error.
	residual := templateTokenRe.FindAllString(src, -1)
	if len(residual) > 0 {
		// Deduplicate and strip delimiters for the error message.
		seen := make(map[string]struct{}, len(residual))
		var tokens []string
		for _, tok := range residual {
			key := tok[2 : len(tok)-2]
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				tokens = append(tokens, key)
			}
		}
		return "", &ErrResidualToken{Tokens: tokens}
	}

	return src, nil
}
