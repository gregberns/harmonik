package workflow

// params_graph.go — post-parse, per-attribute template-param substitution
// (WG-045 / WG-046 security close).
//
// The pre-parse raw-source substitution that this replaces was a context-blind
// string splice: a param value carrying shell metacharacters landed inside a
// node tool_command and was executed verbatim by /bin/sh -c (command injection),
// and a value carrying DOT syntax (a `"` or `]`) could break out of an attribute
// and alter the parsed graph (DOT-structure injection).
//
// substituteGraphParams runs AFTER dot.Parse, walking the typed graph and
// substituting each attribute with context-appropriate handling:
//   - node tool_command  → the value is POSIX shell-quoted (ShellQuote) so it
//     becomes one inert shell word; this is the load-bearing command-injection
//     close, and it covers BOTH the local /bin/sh -c sink and the remote
//     /bin/sh -lc sink because the quoting happens before the value ever reaches
//     dispatchDotToolNode.
//   - every other attribute → substituted VERBATIM (no quoting), preserving the
//     existing free-text behavior of goal/prompt/role/label/etc.
//
// Because substitution happens on the already-parsed graph, a value can no longer
// alter graph shape regardless of its bytes — DOT-structure injection is closed by
// construction. Author shell syntax inside a tool_command (`&&`, `>`, `$(...)`,
// `[ "$C" = LOW ]`) is author text and is NEVER quoted; only the substituted value
// bytes are.
//
// Ordering invariant (WG-046, reversed from the original):
//   read source → parse(template, tokens intact) → substitute(per-attribute) →
//   validate → dispatch.
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-045 — param values UNTRUSTED; tool_command
//     values shell-quoted; other attributes verbatim.
//   - specs/workflow-graph.md §4 WG-046 — post-parse per-attribute ordering.
//
// Tags: mechanism, normative

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ErrQuotedToolCommandToken is returned when a template token appears inside a
// single- or double-quoted span within a node's tool_command. The shell-quoting
// close (WG-045) neutralises a token ONLY when the author writes it UNQUOTED — the
// daemon supplies the quoting. If an author wraps the token in their own quotes
// (e.g. tool_command="echo '__SID__'" or "echo \"__SID__\""), the substituted,
// already-single-quoted value concatenates OUT of the author's quotes (or is
// re-expanded inside double quotes), reopening command injection for a value such
// as $(touch x). WG-045 makes "tokens in tool_command MUST be unquoted" normative;
// this lint makes that rule fail-loud at load instead of silently trusting it.
type ErrQuotedToolCommandToken struct {
	// NodeID is the node whose tool_command carries the quoted token.
	NodeID string
	// Token is the offending token name (without the __ delimiters).
	Token string
}

func (e *ErrQuotedToolCommandToken) Error() string {
	return fmt.Sprintf("node %q: template token __%s__ is inside a quoted span in tool_command; "+
		"tokens in tool_command MUST be unquoted — the daemon shell-quotes the value for you "+
		"(security: workflow-graph.md WG-045)", e.NodeID, e.Token)
}

func findQuotedToolCommandToken(cmd string) string {
	inside := quotedSpanMask(cmd)
	for _, span := range templateTokenRe.FindAllStringIndex(cmd, -1) {
		start, end := span[0], span[1]
		if start < len(inside) && inside[start] {
			return cmd[start+2 : end-2] // strip __ delimiters
		}
	}
	return ""
}

func quotedSpanMask(cmd string) []bool {
	inside := make([]bool, len(cmd))
	inSingle, inDouble := false, false
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false // closing quote; not "inside"
			} else {
				inside[i] = true
			}
		case inDouble:
			switch {
			case c == '\\' && i+1 < len(cmd):
				inside[i] = true
				i++
				inside[i] = true // the escaped char is still inside the span
			case c == '"':
				inDouble = false // closing quote
			default:
				inside[i] = true
			}
		default: // unquoted
			switch {
			case c == '\'':
				inSingle = true
			case c == '"':
				inDouble = true
			case c == '\\' && i+1 < len(cmd):
				i++ // skip the escaped char (still unquoted)
			}
		}
	}
	return inside
}

func replaceTokens(val string, params map[string]string, quote bool) string {
	if !templateTokenRe.MatchString(val) {
		return val
	}
	return templateTokenRe.ReplaceAllStringFunc(val, func(match string) string {
		key := match[2 : len(match)-2] // strip the __ delimiters
		if v, ok := params[key]; ok {
			if quote {
				return ShellQuote(v)
			}
			return v
		}
		return match // leave unresolved tokens for the residual check
	})
}

func scanResidualTokens(sources []string) []string {
	seen := make(map[string]struct{})
	var tokens []string
	for _, s := range sources {
		for _, tok := range templateTokenRe.FindAllString(s, -1) {
			key := tok[2 : len(tok)-2]
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				tokens = append(tokens, key)
			}
		}
	}
	return tokens
}

func substituteGraphParams(g *dot.Graph, params map[string]string) error {
	if err := core.ValidateTemplateParams(params); err != nil {
		return err
	}
	if g == nil {
		return nil
	}

	for _, n := range g.Nodes {
		if n.ToolCommand == "" {
			continue
		}
		if tok := findQuotedToolCommandToken(n.ToolCommand); tok != "" {
			return &ErrQuotedToolCommandToken{NodeID: n.ID, Token: tok}
		}
	}

	var verbatim []*string
	quoted := make([]*string, 0, len(g.Nodes)) // exactly one tool_command per node
	var attrMaps []map[string]string

	verbatim = append(verbatim,
		&g.Name, &g.SchemaVersion, &g.Version, &g.StartNodeID,
		&g.WorkflowClass, &g.NoProgressGuard, &g.Goal,
	)
	g.WorkflowID = core.WorkflowID(replaceTokens(g.WorkflowID.String(), params, false))
	for i := range g.TerminalNodeIDs {
		verbatim = append(verbatim, &g.TerminalNodeIDs[i])
	}
	for i := range g.ContextKeys {
		verbatim = append(verbatim, &g.ContextKeys[i])
	}
	if g.UnknownAttrs != nil {
		attrMaps = append(attrMaps, g.UnknownAttrs)
	}

	for _, n := range g.Nodes {
		verbatim = append(verbatim,
			&n.ID, &n.RawType, &n.AgentType, &n.HandlerRef, &n.GateRef,
			&n.SubWorkflowRef, &n.WorkflowVersion, &n.InputMapping, &n.IdempotencyClass,
			&n.Role, &n.Prompt, &n.Model, &n.Effort, &n.AxisTags, &n.HookRef,
			&n.GuardRef, &n.BudgetRef, &n.SkillsRef, &n.FreedomProfileRef,
			&n.Timeout, &n.Harness, &n.AgentRuntime, &n.ReviewerHarness,
		)
		quoted = append(quoted, &n.ToolCommand) // shell sink — quote substituted values
		if n.UnknownAttrs != nil {
			attrMaps = append(attrMaps, n.UnknownAttrs)
		}
	}

	for _, e := range g.Edges {
		verbatim = append(verbatim,
			&e.FromNodeID, &e.ToNodeID, &e.ConditionRaw, &e.PreferredLabel,
			&e.Weight, &e.OrderingKey,
		)
		if e.Condition != nil {
			for i := range e.Condition.Clauses {
				verbatim = append(verbatim, &e.Condition.Clauses[i].RHS)
			}
		}
		if e.UnknownAttrs != nil {
			attrMaps = append(attrMaps, e.UnknownAttrs)
		}
	}

	for _, p := range verbatim {
		*p = replaceTokens(*p, params, false)
	}
	for _, p := range quoted {
		*p = replaceTokens(*p, params, true)
	}
	for _, m := range attrMaps {
		for k, v := range m {
			m[k] = replaceTokens(v, params, false)
		}
	}

	residualSources := make([]string, 0, len(verbatim)+len(quoted))
	for _, p := range verbatim {
		residualSources = append(residualSources, *p)
	}
	for _, p := range quoted {
		residualSources = append(residualSources, *p)
	}
	residualSources = append(residualSources, g.WorkflowID.String())
	for _, m := range attrMaps {
		for _, v := range m {
			residualSources = append(residualSources, v)
		}
	}
	if toks := scanResidualTokens(residualSources); len(toks) > 0 {
		return &ErrResidualToken{Tokens: toks}
	}
	return nil
}
