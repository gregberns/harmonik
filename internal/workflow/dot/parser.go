package dot

// parser.go — DOT parser producing a typed AST per WG-031/WG-032/WG-033.
//
// Design decision (documented per bead body requirement):
//   The pre-existing parser lives in internal/workflowvalidator/dotparser.go and
//   produces a flat rawGraph (map[string]string attributes, no typed AST).  This
//   new package (internal/workflow/dot/) introduces a proper typed AST and a
//   WG-031-aware parser that classifies attributes as strict or permissive at
//   parse time.  The old parser is NOT removed here — that migration is owned by
//   the validator bead (hk-0a60l, T-IMPL-002) which will wire this package's output
//   into the existing PreRunValidator or replace it.  Shipping parallel parsers
//   temporarily is intentional and safe: the new parser is not yet called by any
//   production path.
//
// Architecture:
//   1. tokenize()  — low-level character scanner producing a []token with line numbers.
//   2. dotParser   — recursive-descent consumer: tokens → rawDoc.
//   3. buildGraph  — converts rawDoc → typed *Graph, applying WG-031 policy.
//
// Spec refs:
//   - specs/workflow-graph.md §4 WG-001/WG-002 — node types and attribute catalog.
//   - specs/workflow-graph.md §5 WG-009        — edge field set.
//   - specs/workflow-graph.md §6 WG-013..016   — edge-condition dialect.
//   - specs/workflow-graph.md §10 WG-031/032   — mixed strict/permissive policy.
//   - specs/workflow-graph.md §11 WG-033/035   — schema_version / version.
//   - specs/workflow-graph.md §9 WG-027        — start_node / terminal_node_ids.
//
// Tags: mechanism, normative

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gregberns/harmonik/internal/core"
)

// Parse parses src into a typed *Graph applying the WG-031 mixed strict/permissive policy.
//
// Returns (graph, nil) when no strict errors occur.  graph.Warnings may be non-nil
// for permissive-position unknowns per WG-031/032.
//
// Returns (nil, err) when one or more strict errors are detected.  err is a
// *ParseError (single error) or ParseErrors (multiple).
//
// filename is used in error messages only; pass "" or the real path.
//
// Tags: mechanism
func Parse(src, filename string) (*Graph, error) {
	tokens, scanErr := tokenize(src)
	if scanErr != nil {
		return nil, &ParseError{Line: scanErr.line, Message: scanErr.msg}
	}

	p := &dotParser{tokens: tokens, filename: filename}
	raw, parseErr := p.parse()
	if parseErr != nil {
		return nil, parseErr
	}

	return buildGraph(raw)
}

// ── token types ───────────────────────────────────────────────────────────────

type tokenKind int

const (
	tokIdent  tokenKind = iota // unquoted identifier or keyword
	tokString                  // double-quoted string (value already unescaped)
	tokArrow                   // ->
	tokLBrack                  // [
	tokRBrack                  // ]
	tokLBrace                  // {
	tokRBrace                  // }
	tokEq                      // =
	tokSemi                    // ;
	tokComma                   // ,
)

type token struct {
	kind  tokenKind
	value string
	line  int // 1-based
}

// scanError is the internal scanner error type.
type scanError struct {
	line int
	msg  string
}

// ── tokenizer ─────────────────────────────────────────────────────────────────

// tokenize converts src into a flat token slice with 1-based line numbers.
func tokenize(src string) ([]token, *scanError) {
	var tokens []token
	i, line := 0, 1
	n := len(src)

	for i < n {
		ch := src[i]

		if ch == '\n' {
			line++
			i++
			continue
		}
		if unicode.IsSpace(rune(ch)) {
			i++
			continue
		}

		// Comments.
		if ch == '/' && i+1 < n {
			switch src[i+1] {
			case '/':
				i += 2
				for i < n && src[i] != '\n' {
					i++
				}
				continue
			case '*':
				i += 2
				for i+1 < n {
					if src[i] == '\n' {
						line++
					}
					if src[i] == '*' && src[i+1] == '/' {
						i += 2
						goto nextTok
					}
					i++
				}
				return nil, &scanError{line: line, msg: "unterminated block comment"}
			}
		}

		// Arrow.
		if ch == '-' && i+1 < n && src[i+1] == '>' {
			tokens = append(tokens, token{kind: tokArrow, value: "->", line: line})
			i += 2
			continue
		}

		// Single-character symbols.
		switch ch {
		case '[':
			tokens = append(tokens, token{kind: tokLBrack, value: "[", line: line})
			i++
			continue
		case ']':
			tokens = append(tokens, token{kind: tokRBrack, value: "]", line: line})
			i++
			continue
		case '{':
			tokens = append(tokens, token{kind: tokLBrace, value: "{", line: line})
			i++
			continue
		case '}':
			tokens = append(tokens, token{kind: tokRBrace, value: "}", line: line})
			i++
			continue
		case '=':
			tokens = append(tokens, token{kind: tokEq, value: "=", line: line})
			i++
			continue
		case ';':
			tokens = append(tokens, token{kind: tokSemi, value: ";", line: line})
			i++
			continue
		case ',':
			tokens = append(tokens, token{kind: tokComma, value: ",", line: line})
			i++
			continue
		}

		// Double-quoted string.
		if ch == '"' {
			startLine := line
			i++
			var b strings.Builder
			for i < n {
				c := src[i]
				if c == '\n' {
					line++
				}
				if c == '"' {
					i++
					tokens = append(tokens, token{kind: tokString, value: b.String(), line: startLine})
					goto nextTok
				}
				if c == '\\' && i+1 < n {
					i++
					switch src[i] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case '"':
						b.WriteByte('"')
					case '\\':
						b.WriteByte('\\')
					default:
						b.WriteByte(src[i])
					}
					i++
					continue
				}
				b.WriteByte(c)
				i++
			}
			return nil, &scanError{line: startLine, msg: "unterminated string literal"}
		}

		// Unquoted identifier.
		if isIDStartChar(rune(ch)) || unicode.IsDigit(rune(ch)) {
			start := i
			startLine := line
			for i < n && isIDChar(rune(src[i])) {
				i++
			}
			tokens = append(tokens, token{kind: tokIdent, value: src[start:i], line: startLine})
			continue
		}

		return nil, &scanError{line: line, msg: fmt.Sprintf("unexpected character %q", string(ch))}

	nextTok:
	}
	return tokens, nil
}

func isIDStartChar(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isIDChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) ||
		r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@'
}

// ── intermediate raw document ─────────────────────────────────────────────────

type rawAttrPair struct {
	key  string
	val  string
	line int
}

type rawNode struct {
	id    string
	line  int
	attrs []rawAttrPair
}

type rawEdge struct {
	from  string
	to    string
	line  int
	attrs []rawAttrPair
}

type rawDoc struct {
	name       string
	graphAttrs []rawAttrPair
	nodes      []*rawNode
	edges      []*rawEdge
}

// ── recursive-descent parser ──────────────────────────────────────────────────

type dotParser struct {
	tokens   []token
	pos      int
	filename string
}

func (p *dotParser) peek() (token, bool) {
	if p.pos >= len(p.tokens) {
		return token{}, false
	}
	return p.tokens[p.pos], true
}

func (p *dotParser) consume() (token, bool) {
	if p.pos >= len(p.tokens) {
		return token{}, false
	}
	t := p.tokens[p.pos]
	p.pos++
	return t, true
}

func (p *dotParser) currentLine() int {
	if p.pos > 0 && p.pos-1 < len(p.tokens) {
		return p.tokens[p.pos-1].line
	}
	if len(p.tokens) > 0 {
		return p.tokens[len(p.tokens)-1].line
	}
	return 1
}

func (p *dotParser) expectIdent(what string) (token, error) {
	t, ok := p.consume()
	if !ok {
		return token{}, &ParseError{Line: p.currentLine(), Message: fmt.Sprintf("expected %s, got EOF", what)}
	}
	if t.kind != tokIdent && t.kind != tokString {
		return token{}, &ParseError{Line: t.line, Message: fmt.Sprintf("expected %s, got %q", what, t.value)}
	}
	return t, nil
}

func (p *dotParser) expectKind(k tokenKind, sym string) (token, error) {
	t, ok := p.consume()
	if !ok {
		return token{}, &ParseError{Line: p.currentLine(), Message: fmt.Sprintf("expected %q, got EOF", sym)}
	}
	if t.kind != k {
		return token{}, &ParseError{Line: t.line, Message: fmt.Sprintf("expected %q, got %q", sym, t.value)}
	}
	return t, nil
}

func (p *dotParser) consumeOptSemi() {
	if t, ok := p.peek(); ok && t.kind == tokSemi {
		_, _ = p.consume()
	}
}

// parse is the top-level entry point: parse a full digraph.
func (p *dotParser) parse() (*rawDoc, error) {
	// Consume optional "strict".
	if t, ok := p.peek(); ok && t.kind == tokIdent && t.value == "strict" {
		_, _ = p.consume()
	}

	// Expect "digraph".
	kw, err := p.expectIdent("keyword \"digraph\"")
	if err != nil {
		return nil, err
	}
	if kw.value != "digraph" {
		return nil, &ParseError{Line: kw.line, Message: fmt.Sprintf("expected \"digraph\", got %q", kw.value)}
	}

	// Optional graph name: anything that isn't "{".
	name := ""
	if t, ok := p.peek(); ok && t.kind != tokLBrace {
		if t.kind == tokIdent || t.kind == tokString {
			nameTok, _ := p.consume()
			name = nameTok.value
		}
	}

	if _, err := p.expectKind(tokLBrace, "{"); err != nil {
		return nil, err
	}

	doc := &rawDoc{name: name}
	if err := p.parseBody(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// parseBody parses the content inside the outer braces.
func (p *dotParser) parseBody(doc *rawDoc) error {
	for {
		t, ok := p.peek()
		if !ok {
			return &ParseError{Line: p.currentLine(), Message: "unexpected EOF inside digraph body"}
		}
		if t.kind == tokRBrace {
			_, _ = p.consume()
			return nil
		}
		if t.kind == tokSemi {
			_, _ = p.consume()
			continue
		}

		// "graph [ ... ]" attribute block (DOT convention for graph-level attrs).
		if t.kind == tokIdent && t.value == "graph" {
			_, _ = p.consume()
			if pk, ok2 := p.peek(); ok2 && pk.kind == tokLBrack {
				attrs, attrErr := p.parseAttrList()
				if attrErr != nil {
					return attrErr
				}
				doc.graphAttrs = append(doc.graphAttrs, attrs...)
				p.consumeOptSemi()
				continue
			}
			// "graph" used as a node/edge ID (unusual but valid DOT).
			if err := p.parseStmt(doc, "graph", t.line); err != nil {
				return err
			}
			p.consumeOptSemi()
			continue
		}

		// Node/edge statement.
		if t.kind != tokIdent && t.kind != tokString {
			return &ParseError{Line: t.line, Message: fmt.Sprintf("unexpected token %q in digraph body", t.value)}
		}
		_, _ = p.consume()

		// Bare graph-level attribute: id = value ; (no brackets, no arrow).
		// This is the DOT idiom for writing graph attrs directly in the body:
		//   schema_version="1";
		//   start_node="foo";
		// Detect by peeking for '='.
		if pk, ok2 := p.peek(); ok2 && pk.kind == tokEq {
			_, _ = p.consume() // consume '='
			valTok, valErr := p.expectIdent("graph attribute value")
			if valErr != nil {
				return valErr
			}
			doc.graphAttrs = append(doc.graphAttrs, rawAttrPair{
				key:  t.value,
				val:  valTok.value,
				line: t.line,
			})
			p.consumeOptSemi()
			continue
		}

		if err := p.parseStmt(doc, t.value, t.line); err != nil {
			return err
		}
		p.consumeOptSemi()
	}
}

// parseStmt handles one node or edge statement given the already-consumed LHS id.
func (p *dotParser) parseStmt(doc *rawDoc, id string, idLine int) error {
	// Edge: id -> target [attrs]
	if pk, ok := p.peek(); ok && pk.kind == tokArrow {
		_, _ = p.consume()
		toTok, err := p.expectIdent("edge target node ID")
		if err != nil {
			return err
		}
		var attrs []rawAttrPair
		if pk2, ok2 := p.peek(); ok2 && pk2.kind == tokLBrack {
			attrs, err = p.parseAttrList()
			if err != nil {
				return err
			}
		}
		doc.edges = append(doc.edges, &rawEdge{from: id, to: toTok.value, line: idLine, attrs: attrs})
		return nil
	}
	// Node: id [attrs]
	var attrs []rawAttrPair
	if pk, ok := p.peek(); ok && pk.kind == tokLBrack {
		var err error
		attrs, err = p.parseAttrList()
		if err != nil {
			return err
		}
	}
	doc.nodes = append(doc.nodes, &rawNode{id: id, line: idLine, attrs: attrs})
	return nil
}

// parseAttrList parses a [ key=value; ... ] attribute list.
func (p *dotParser) parseAttrList() ([]rawAttrPair, error) {
	if _, err := p.expectKind(tokLBrack, "["); err != nil {
		return nil, err
	}
	var pairs []rawAttrPair
	for {
		t, ok := p.peek()
		if !ok {
			return nil, &ParseError{Line: p.currentLine(), Message: "unexpected EOF inside attribute list"}
		}
		if t.kind == tokRBrack {
			_, _ = p.consume()
			return pairs, nil
		}
		if t.kind == tokSemi || t.kind == tokComma {
			_, _ = p.consume()
			continue
		}
		keyTok, err := p.expectIdent("attribute key")
		if err != nil {
			return nil, err
		}
		if _, err := p.expectKind(tokEq, "="); err != nil {
			return nil, err
		}
		valTok, err := p.expectIdent("attribute value")
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, rawAttrPair{key: keyTok.value, val: valTok.value, line: keyTok.line})
	}
}

// ── WG-031 attribute classification ──────────────────────────────────────────

// reservedGraphAttrs is the set of reserved graph-level attribute names per WG-031.
// Unknown names at the graph level are permissive (warning + retained) per WG-031/032.
var reservedGraphAttrs = map[string]bool{
	"schema_version":    true,
	"version":           true,
	"start_node":        true,
	"start_node_id":     true, // alternate spelling accepted for compat
	"terminal_node_ids": true,
	"context_keys":      true,
	"workflow_id":       true,
	"workflow_class":    true,
}

// ── graph builder (rawDoc → *Graph) ──────────────────────────────────────────

// buildGraph converts a rawDoc into a typed *Graph, applying WG-031 policy.
func buildGraph(doc *rawDoc) (*Graph, error) {
	g := &Graph{
		Name:         doc.name,
		UnknownAttrs: make(map[string]string),
	}
	var strictErrs ParseErrors
	var warnings []ParseWarning

	// Graph-level attributes.
	for _, pair := range doc.graphAttrs {
		switch pair.key {
		case "schema_version":
			g.SchemaVersion = pair.val
		case "version":
			g.Version = pair.val
		case "start_node", "start_node_id":
			// Accept both DOT attribute name ("start_node") and the Go-field name
			// spelling ("start_node_id").  The spec glossary distinguishes DOT attr
			// ("start_node") from parsed record field ("start_node_id"); both are
			// used in existing fixtures.
			if g.StartNodeID == "" {
				g.StartNodeID = pair.val
			}
		case "terminal_node_ids":
			g.TerminalNodeIDs = splitIDs(pair.val)
		case "context_keys":
			g.ContextKeys = splitIDs(pair.val)
		case "workflow_id":
			// Informational; retained in UnknownAttrs for round-trip.
			g.UnknownAttrs["workflow_id"] = pair.val
		case "workflow_class":
			g.WorkflowClass = pair.val
		default:
			// Non-reserved graph-level attribute: permissive per WG-031/032.
			g.UnknownAttrs[pair.key] = pair.val
			warnings = append(warnings, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("graph-level: unknown permissive attribute %q=%q (WG-031)", pair.key, pair.val),
			})
		}
	}

	// Nodes.
	for _, rn := range doc.nodes {
		node, nodeErrs, nodeWarns := buildNode(rn)
		strictErrs = append(strictErrs, nodeErrs...)
		warnings = append(warnings, nodeWarns...)
		g.Nodes = append(g.Nodes, node)
	}

	// Edges.
	for _, re := range doc.edges {
		edge, edgeErrs, edgeWarns := buildEdge(re)
		strictErrs = append(strictErrs, edgeErrs...)
		warnings = append(warnings, edgeWarns...)
		g.Edges = append(g.Edges, edge)
	}

	g.Warnings = warnings

	if len(strictErrs) > 0 {
		return nil, strictErrs
	}
	return g, nil
}

// buildNode converts a rawNode to a typed *Node applying WG-031 node-level policy.
func buildNode(rn *rawNode) (*Node, []*ParseError, []ParseWarning) {
	node := &Node{
		ID:           rn.id,
		UnknownAttrs: make(map[string]string),
	}
	var errs []*ParseError
	var warns []ParseWarning

	for _, pair := range rn.attrs {
		switch pair.key {
		case "type":
			node.RawType = pair.val
			nt := core.NodeType(pair.val)
			// WG-001 specifies a CLOSED FOUR-MEMBER enum: {agentic, non-agentic, gate, sub-workflow}.
			// control-point was removed from the v1.0 vocabulary (see §16.2 and WG-001).
			// core.NodeType.Valid() still accepts control-point (pre-Phase-3 code); we
			// enforce the spec's 4-type enum here at the parser layer.
			if !isValidWG001NodeType(nt) {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: type %q is not one of {agentic, non-agentic, gate, sub-workflow} (WG-001)",
						rn.id, pair.val),
				})
			} else {
				node.Type = nt
			}
		case "agent_type":
			node.AgentType = pair.val
		case "handler_ref":
			node.HandlerRef = pair.val
		case "gate_ref":
			node.GateRef = pair.val
		case "sub_workflow_ref":
			node.SubWorkflowRef = pair.val
		case "workflow_version":
			node.WorkflowVersion = pair.val
		case "input_mapping":
			node.InputMapping = pair.val
		case "idempotency_class":
			node.IdempotencyClass = pair.val
		case "axis_tags":
			node.AxisTags = pair.val
		case "hook_ref":
			node.HookRef = pair.val
		case "guard_ref":
			node.GuardRef = pair.val
		case "budget_ref":
			node.BudgetRef = pair.val
		case "skills_ref":
			node.SkillsRef = pair.val
		case "freedom_profile_ref":
			node.FreedomProfileRef = pair.val
		case "policy_ref":
			// Reserved-and-rejected per CP-056 / WG-031.
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"policy_ref\" is reserved-and-rejected (CP-056 / WG-031); use skills_ref or freedom_profile_ref",
					rn.id),
			})
		case "schema_version":
			// Per WG-033: schema_version is graph-level only; on a node it is a strict error.
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"schema_version\" is reserved for graph-level use only (WG-033)",
					rn.id),
			})
		default:
			// Non-reserved node attribute: permissive per WG-031/032.
			node.UnknownAttrs[pair.key] = pair.val
			warns = append(warns, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("node %q: unknown permissive attribute %q=%q (WG-031/032)", rn.id, pair.key, pair.val),
			})
		}
	}
	return node, errs, warns
}

// buildEdge converts a rawEdge to a typed *Edge applying WG-031 edge-level policy.
func buildEdge(re *rawEdge) (*Edge, []*ParseError, []ParseWarning) {
	edge := &Edge{
		FromNodeID:   re.from,
		ToNodeID:     re.to,
		UnknownAttrs: make(map[string]string),
	}
	var errs []*ParseError
	var warns []ParseWarning

	for _, pair := range re.attrs {
		switch pair.key {
		case "condition":
			edge.ConditionRaw = pair.val
			cond, condErr := parseCondition(pair.val, pair.line)
			if condErr != nil {
				errs = append(errs, condErr)
			} else {
				edge.Condition = cond
			}
		case "preferred_label":
			edge.PreferredLabel = pair.val
		case "weight":
			edge.Weight = pair.val
		case "ordering_key":
			edge.OrderingKey = pair.val
		case "traversal_cap":
			// Reserved per EM-043; not directly a Graph field but retained for
			// round-trip and validator layer consumption.
			edge.UnknownAttrs[pair.key] = pair.val
		case "schema_version":
			// Strict error: schema_version on an edge per WG-033.
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"edge %s->%s: attribute \"schema_version\" is reserved for graph-level use only (WG-033)",
					re.from, re.to),
			})
		default:
			// Non-reserved edge attribute: permissive per WG-031/032.
			edge.UnknownAttrs[pair.key] = pair.val
			warns = append(warns, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("edge %s->%s: unknown permissive attribute %q=%q (WG-031/032)", re.from, re.to, pair.key, pair.val),
			})
		}
	}
	return edge, errs, warns
}

// ── edge-condition mini-language (WG-013..WG-015) ────────────────────────────

// lhsWhitelist is the allowed LHS values per WG-014.
// context.<key> is handled separately via HasPrefix check.
var lhsWhitelist = map[string]bool{
	"outcome.status":          true,
	"outcome.preferred_label": true,
	"outcome.failure_class":   true,
	"outcome.kind":            true,
}

// closedStatusValues is the OutcomeStatus closed enum per EM-005.
var closedStatusValues = map[string]bool{
	"SUCCESS":         true,
	"FAIL":            true,
	"RETRY":           true,
	"PARTIAL_SUCCESS": true,
}

// closedFailureClassValues is the FailureClass closed enum per §7 WG-017.
var closedFailureClassValues = map[string]bool{
	"transient":        true,
	"structural":       true,
	"deterministic":    true,
	"canceled":         true,
	"budget_exhausted": true,
	"compilation_loop": true,
}

// closedKindValues is the OutcomeKind closed enum per EM-005a.
var closedKindValues = map[string]bool{
	"default":                true,
	"handler_outcome":        true,
	"gate_decision":          true,
	"reconciliation_verdict": true,
}

// parseCondition parses an edge condition per the §6 WG-013 grammar.
// Returns (*Condition, nil) on success or (nil, *ParseError) on any violation
// (strict per WG-025).
func parseCondition(raw string, line int) (*Condition, *ParseError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Split on "&&" to get individual equality clauses.
	parts := strings.Split(raw, "&&")
	cond := &Condition{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, &ParseError{Line: line,
				Message: fmt.Sprintf("condition: empty clause in %q", raw)}
		}
		eq, err := parseEquality(part, line, raw)
		if err != nil {
			return nil, err
		}
		cond.Clauses = append(cond.Clauses, eq)
	}
	return cond, nil
}

// parseEquality parses a single "lhs op rhs" equality per WG-013.
func parseEquality(s string, line int, fullCond string) (Equality, *ParseError) {
	s = strings.TrimSpace(s)

	// Find the operator. Check != before == so that "!=" is not split as "!" + "=".
	var op string
	var opIdx int
	if i := strings.Index(s, "!="); i >= 0 {
		op = "!="
		opIdx = i
	} else if i := strings.Index(s, "=="); i >= 0 {
		op = "=="
		opIdx = i
	} else {
		return Equality{}, &ParseError{Line: line,
			Message: fmt.Sprintf("condition: clause %q has no == or != operator (full condition: %q)", s, fullCond)}
	}

	lhs := strings.TrimSpace(s[:opIdx])
	rhs := strings.TrimSpace(s[opIdx+len(op):])

	// Validate LHS against WG-014 whitelist.
	if !lhsWhitelist[lhs] && !strings.HasPrefix(lhs, "context.") {
		return Equality{}, &ParseError{Line: line,
			Message: fmt.Sprintf(
				"condition: LHS %q is not in the WG-014 whitelist "+
					"(allowed: outcome.status, outcome.preferred_label, outcome.failure_class, outcome.kind, context.<key>)",
				lhs)}
	}

	// Validate / normalise RHS per WG-015.
	normRHS, rhsErr := validateRHS(rhs, lhs, line, fullCond)
	if rhsErr != nil {
		return Equality{}, rhsErr
	}

	return Equality{LHS: lhs, Op: op, RHS: normRHS}, nil
}

// validateRHS validates and normalises the RHS literal per WG-015.
// Single-quoted strings are stripped of their quotes; integers and enum
// members are returned verbatim.
func validateRHS(rhs, lhs string, line int, fullCond string) (string, *ParseError) {
	// Single-quoted string literal.
	if strings.HasPrefix(rhs, "'") && strings.HasSuffix(rhs, "'") && len(rhs) >= 2 {
		return rhs[1 : len(rhs)-1], nil
	}
	// Double-quoted string (already unescaped by tokenizer).
	if strings.HasPrefix(rhs, "\"") && strings.HasSuffix(rhs, "\"") && len(rhs) >= 2 {
		return rhs[1 : len(rhs)-1], nil
	}
	// Non-negative integer.
	if isNonNegInt(rhs) {
		return rhs, nil
	}
	// Closed-enum identifier: validate membership per WG-015.
	switch lhs {
	case "outcome.status":
		if !closedStatusValues[rhs] {
			return "", &ParseError{Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.status is not a valid status value "+
						"(must be one of SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS) in condition %q",
					rhs, fullCond)}
		}
	case "outcome.failure_class":
		if !closedFailureClassValues[rhs] {
			return "", &ParseError{Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.failure_class is not a valid failure class value in condition %q",
					rhs, fullCond)}
		}
	case "outcome.kind":
		if !closedKindValues[rhs] {
			return "", &ParseError{Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.kind is not a valid OutcomeKind value in condition %q",
					rhs, fullCond)}
		}
	default:
		// outcome.preferred_label and context.<key>: any bare identifier is valid.
	}
	return rhs, nil
}

func isNonNegInt(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isValidWG001NodeType reports whether nt is one of the four WG-001 node type
// members: {agentic, non-agentic, gate, sub-workflow}.
func isValidWG001NodeType(nt core.NodeType) bool {
	switch nt {
	case core.NodeTypeAgentic, core.NodeTypeNonAgentic, core.NodeTypeGate, core.NodeTypeSubWorkflow:
		return true
	default:
		return false
	}
}

// splitIDs splits a comma-and-space-separated ID list.
func splitIDs(s string) []string {
	s = strings.ReplaceAll(s, ",", " ")
	parts := strings.Fields(s)
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}
