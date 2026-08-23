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
	"regexp"
	"strconv"
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

type scanError struct {
	line int
	msg  string
}

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

		if ch == '-' && i+1 < n && src[i+1] == '>' {
			tokens = append(tokens, token{kind: tokArrow, value: "->", line: line})
			i += 2
			continue
		}

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

func (p *dotParser) expectKind(k tokenKind, sym string) error {
	t, ok := p.consume()
	if !ok {
		return &ParseError{Line: p.currentLine(), Message: fmt.Sprintf("expected %q, got EOF", sym)}
	}
	if t.kind != k {
		return &ParseError{Line: t.line, Message: fmt.Sprintf("expected %q, got %q", sym, t.value)}
	}
	return nil
}

func (p *dotParser) consumeOptSemi() {
	if t, ok := p.peek(); ok && t.kind == tokSemi {
		_, _ = p.consume()
	}
}

func (p *dotParser) parse() (*rawDoc, error) {
	if t, ok := p.peek(); ok && t.kind == tokIdent && t.value == "strict" {
		_, _ = p.consume()
	}

	kw, err := p.expectIdent("keyword \"digraph\"")
	if err != nil {
		return nil, err
	}
	if kw.value != "digraph" {
		return nil, &ParseError{Line: kw.line, Message: fmt.Sprintf("expected \"digraph\", got %q", kw.value)}
	}

	name := ""
	if t, ok := p.peek(); ok && t.kind != tokLBrace {
		if t.kind == tokIdent || t.kind == tokString {
			nameTok, _ := p.consume()
			name = nameTok.value
		}
	}

	if err := p.expectKind(tokLBrace, "{"); err != nil {
		return nil, err
	}

	doc := &rawDoc{name: name}
	if err := p.parseBody(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

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
			if err := p.parseStmt(doc, "graph", t.line); err != nil {
				return err
			}
			p.consumeOptSemi()
			continue
		}

		if t.kind != tokIdent && t.kind != tokString {
			return &ParseError{Line: t.line, Message: fmt.Sprintf("unexpected token %q in digraph body", t.value)}
		}
		_, _ = p.consume()

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

func (p *dotParser) parseStmt(doc *rawDoc, id string, idLine int) error {
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

func (p *dotParser) parseAttrList() ([]rawAttrPair, error) {
	if err := p.expectKind(tokLBrack, "["); err != nil {
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
		if err := p.expectKind(tokEq, "="); err != nil {
			return nil, err
		}
		valTok, err := p.expectIdent("attribute value")
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, rawAttrPair{key: keyTok.value, val: valTok.value, line: keyTok.line})
	}
}

var nodeModelRegex = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)

var workflowIDTemplatePattern = regexp.MustCompile(`^__[A-Z][A-Z0-9_]*__$`)

const nodeModelMaxLen = 128

var validNodeEffortLevels = map[string]bool{
	"low": true, "medium": true, "high": true, "xhigh": true, "max": true,
}

func buildGraph(doc *rawDoc) (*Graph, error) {
	g := &Graph{
		Name:         doc.name,
		UnknownAttrs: make(map[string]string),
	}
	var strictErrs ParseErrors
	var warnings []ParseWarning
	workflowIDSeen := false

	for _, pair := range doc.graphAttrs {
		switch pair.key {
		case "schema_version":
			g.SchemaVersion = pair.val
		case "version":
			g.Version = pair.val
		case "start_node", "start_node_id":
			if g.StartNodeID == "" {
				g.StartNodeID = pair.val
			}
		case "terminal_node_ids":
			g.TerminalNodeIDs = splitIDs(pair.val)
		case "context_keys":
			g.ContextKeys = splitIDs(pair.val)
		case "workflow_id":
			if workflowIDSeen {
				strictErrs = append(strictErrs, &ParseError{
					Line:    pair.line,
					Message: "graph-level workflow_id must appear exactly once (WG-055)",
				})
				continue
			}
			workflowIDSeen = true
			if workflowIDTemplatePattern.MatchString(pair.val) {
				g.WorkflowID = core.WorkflowID(pair.val)
				continue
			}
			workflowID, err := core.NewWorkflowID(pair.val)
			if err != nil {
				strictErrs = append(strictErrs, &ParseError{
					Line:    pair.line,
					Message: fmt.Sprintf("graph-level workflow_id %q: %v (WG-055)", pair.val, err),
				})
				continue
			}
			g.WorkflowID = workflowID
		case "workflow_class":
			g.WorkflowClass = pair.val
		case "goal":
			g.Goal = pair.val
		case "no_progress_guard":
			if err := validateNoProgressGuard(pair.val); err != nil {
				strictErrs = append(strictErrs, &ParseError{
					Line:    pair.line,
					Message: fmt.Sprintf("graph-level: no_progress_guard %q: %v", pair.val, err),
				})
			} else {
				g.NoProgressGuard = pair.val
			}
		default:
			g.UnknownAttrs[pair.key] = pair.val
			warnings = append(warnings, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("graph-level: unknown permissive attribute %q=%q (WG-031)", pair.key, pair.val),
			})
		}
	}
	if !workflowIDSeen {
		strictErrs = append(strictErrs, &ParseError{
			Line:    0,
			Message: "workflow must declare a graph-level workflow_id attribute (WG-055)",
		})
	}

	for _, rn := range doc.nodes {
		node, nodeErrs, nodeWarns := buildNode(rn)
		strictErrs = append(strictErrs, nodeErrs...)
		warnings = append(warnings, nodeWarns...)
		g.Nodes = append(g.Nodes, node)
	}

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

func buildNode(rn *rawNode) (*Node, []*ParseError, []ParseWarning) {
	node := &Node{
		ID:           rn.id,
		Line:         rn.line,
		UnknownAttrs: make(map[string]string),
	}
	var errs []*ParseError
	var warns []ParseWarning

	for _, pair := range rn.attrs {
		switch pair.key {
		case "type":
			node.RawType = pair.val
			nt := core.NodeType(pair.val)
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
		case "role":
			node.Role = pair.val
		case "prompt":
			node.Prompt = pair.val
		case "model":
			if pair.val == "" || !nodeModelRegex.MatchString(pair.val) || len(pair.val) > nodeModelMaxLen {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: model %q must match ^[A-Za-z0-9._:/-]+$ and be <=128 chars (WG-042 §I.5)",
						rn.id, pair.val),
				})
			} else {
				node.Model = pair.val
			}
		case "effort":
			if !validNodeEffortLevels[pair.val] {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: effort %q is not in {low,medium,high,xhigh,max} (WG-042 §I.5)",
						rn.id, pair.val),
				})
			} else {
				node.Effort = pair.val
			}
		case "non_committing":
			switch pair.val {
			case "true":
				node.NonCommitting = true
			case "false":
				node.NonCommitting = false
			default:
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: non_committing %q must be \"true\" or \"false\" (WG-041 §I.4)",
						rn.id, pair.val),
				})
			}
		case "auto_status":
			switch pair.val {
			case "true":
				node.AutoStatus = true
			case "false":
				node.AutoStatus = false
			default:
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: auto_status %q must be \"true\" or \"false\" (WG-053); "+
							"non-boolean policy values are reserved for a future step",
						rn.id, pair.val),
				})
			}
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
		case "tool_command":
			node.ToolCommand = pair.val
		case "timeout":
			n, err := strconv.Atoi(pair.val)
			if err != nil || n <= 0 {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: timeout %q must be a positive integer (WG-024)",
						rn.id, pair.val),
				})
			} else {
				node.Timeout = pair.val
			}
		case "harness":
			if !core.AgentType(pair.val).Valid() {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: harness %q must be a valid agent_type matching %s (AR-025, codex-harness C4/T5)",
						rn.id, pair.val, core.AgentTypeRegexPattern),
				})
			} else {
				node.Harness = pair.val
			}
		case "agent_runtime":
			if !core.AgentType(pair.val).Valid() {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: agent_runtime %q must be a valid agent_type matching %s (AR-025, codex-harness C4/T5)",
						rn.id, pair.val, core.AgentTypeRegexPattern),
				})
			} else {
				node.AgentRuntime = pair.val
			}
		case "reviewer_harness":
			if !core.AgentType(pair.val).Valid() {
				errs = append(errs, &ParseError{
					Line: pair.line,
					Message: fmt.Sprintf(
						"node %q: reviewer_harness %q must be a valid agent_type matching %s (AR-025, codex-harness C4/T5)",
						rn.id, pair.val, core.AgentTypeRegexPattern),
				})
			} else {
				node.ReviewerHarness = pair.val
			}
		case "policy_ref":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"policy_ref\" is reserved-and-rejected (CP-056 / WG-031); use gate_ref, skills_ref, or freedom_profile_ref instead (CP-055)",
					rn.id),
			})
		case "schema_version":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"schema_version\" is reserved for graph-level use only (WG-033)",
					rn.id),
			})
		case "workflow_id":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"workflow_id\" is reserved for graph-level use only (WG-055)",
					rn.id),
			})
		case "goal":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"node %q: attribute \"goal\" is reserved for graph-level use only (WG-044)",
					rn.id),
			})
		default:
			node.UnknownAttrs[pair.key] = pair.val
			warns = append(warns, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("node %q: unknown permissive attribute %q=%q (WG-031/032)", rn.id, pair.key, pair.val),
			})
		}
	}
	if node.Prompt != "" && node.Type != "" && node.Type != core.NodeTypeAgentic {
		warns = append(warns, ParseWarning{
			Line: node.Line,
			Message: fmt.Sprintf(
				"node %q: attribute \"prompt\" is agentic-only; on type %q it is retained but ignored at v1 (WG-040 §I.3)",
				rn.id, node.RawType),
		})
	}
	if node.NonCommitting && node.Type != "" && node.Type != core.NodeTypeAgentic {
		warns = append(warns, ParseWarning{
			Line: node.Line,
			Message: fmt.Sprintf(
				"node %q: attribute \"non_committing\" is agentic-only; on type %q it is retained but ignored at v1 (WG-041 §I.4)",
				rn.id, node.RawType),
		})
	}
	if node.AutoStatus && node.Type != "" && node.Type != core.NodeTypeAgentic {
		warns = append(warns, ParseWarning{
			Line: node.Line,
			Message: fmt.Sprintf(
				"node %q: attribute \"auto_status\" is agentic-only; on type %q it is retained but ignored at v1 (WG-053)",
				rn.id, node.RawType),
		})
	}
	if node.Type != "" && node.Type != core.NodeTypeAgentic {
		if node.Model != "" {
			errs = append(errs, &ParseError{
				Line: node.Line,
				Message: fmt.Sprintf(
					"node %q: attribute \"model\" is agentic-only; reserved-out-of-position on type %q (WG-042 §I.5 / WG-031)",
					rn.id, node.RawType),
			})
			node.Model = ""
		}
		if node.Effort != "" {
			errs = append(errs, &ParseError{
				Line: node.Line,
				Message: fmt.Sprintf(
					"node %q: attribute \"effort\" is agentic-only; reserved-out-of-position on type %q (WG-042 §I.5 / WG-031)",
					rn.id, node.RawType),
			})
			node.Effort = ""
		}
	}
	if node.Harness != "" && node.AgentRuntime != "" && node.Harness != node.AgentRuntime {
		errs = append(errs, &ParseError{
			Line: node.Line,
			Message: fmt.Sprintf(
				"node %q: harness %q and agent_runtime %q conflict; they are alias spellings of the same per-node harness override (codex-harness C4/T5)",
				rn.id, node.Harness, node.AgentRuntime),
		})
	}
	if node.Harness == "" && node.AgentRuntime != "" {
		node.Harness = node.AgentRuntime
	}
	return node, errs, warns
}

func buildEdge(re *rawEdge) (*Edge, []*ParseError, []ParseWarning) {
	edge := &Edge{
		FromNodeID:   re.from,
		ToNodeID:     re.to,
		Line:         re.line,
		UnknownAttrs: make(map[string]string),
	}
	var errs []*ParseError
	var warns []ParseWarning

	for _, pair := range re.attrs {
		switch pair.key {
		case "workflow_id":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"edge %q -> %q: attribute \"workflow_id\" is reserved for graph-level use only (WG-055)",
					re.from, re.to),
			})
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
			edge.UnknownAttrs[pair.key] = pair.val
		case "schema_version":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"edge %s->%s: attribute \"schema_version\" is reserved for graph-level use only (WG-033)",
					re.from, re.to),
			})
		case "goal":
			errs = append(errs, &ParseError{
				Line: pair.line,
				Message: fmt.Sprintf(
					"edge %s->%s: attribute \"goal\" is reserved for graph-level use only (WG-044)",
					re.from, re.to),
			})
		default:
			edge.UnknownAttrs[pair.key] = pair.val
			warns = append(warns, ParseWarning{
				Line:    pair.line,
				Message: fmt.Sprintf("edge %s->%s: unknown permissive attribute %q=%q (WG-031/032)", re.from, re.to, pair.key, pair.val),
			})
		}
	}
	return edge, errs, warns
}

var lhsWhitelist = map[string]bool{
	lhsOutcomeStatus:         true,
	lhsOutcomePreferredLabel: true,
	lhsOutcomeFailureClass:   true,
	lhsOutcomeKind:           true,
}

var closedStatusValues = map[string]bool{
	"SUCCESS":         true,
	"FAIL":            true,
	"RETRY":           true,
	"PARTIAL_SUCCESS": true,
}

var closedFailureClassValues = map[string]bool{
	"transient":        true,
	"structural":       true,
	"deterministic":    true,
	"canceled":         true,
	"budget_exhausted": true,
	"compilation_loop": true,
}

var closedKindValues = map[string]bool{
	"default":                true,
	"handler_outcome":        true,
	"gate_decision":          true,
	"reconciliation_verdict": true,
}

func parseCondition(raw string, line int) (*Condition, *ParseError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, "&&")
	cond := &Condition{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, &ParseError{
				Line:    line,
				Message: fmt.Sprintf("condition: empty clause in %q", raw),
			}
		}
		eq, err := parseEquality(part, line, raw)
		if err != nil {
			return nil, err
		}
		cond.Clauses = append(cond.Clauses, eq)
	}
	return cond, nil
}

func parseEquality(s string, line int, fullCond string) (Equality, *ParseError) {
	s = strings.TrimSpace(s)

	var op string
	var opIdx int
	if i := strings.Index(s, "!="); i >= 0 {
		op = "!="
		opIdx = i
	} else if i := strings.Index(s, "=="); i >= 0 {
		op = "=="
		opIdx = i
	} else {
		return Equality{}, &ParseError{
			Line:    line,
			Message: fmt.Sprintf("condition: clause %q has no == or != operator (full condition: %q)", s, fullCond),
		}
	}

	lhs := strings.TrimSpace(s[:opIdx])
	rhs := strings.TrimSpace(s[opIdx+len(op):])

	if !lhsWhitelist[lhs] && !strings.HasPrefix(lhs, lhsContextPrefix) {
		return Equality{}, &ParseError{
			Line: line,
			Message: fmt.Sprintf(
				"condition: LHS %q is not in the WG-014 whitelist "+
					"(allowed: outcome.status, outcome.preferred_label, outcome.failure_class, outcome.kind, context.<key>)",
				lhs),
		}
	}

	normRHS, rhsErr := validateRHS(rhs, lhs, line, fullCond)
	if rhsErr != nil {
		return Equality{}, rhsErr
	}

	return Equality{LHS: lhs, Op: op, RHS: normRHS}, nil
}

func validateRHS(rhs, lhs string, line int, fullCond string) (string, *ParseError) {
	if strings.HasPrefix(rhs, "'") && strings.HasSuffix(rhs, "'") && len(rhs) >= 2 {
		return rhs[1 : len(rhs)-1], nil
	}
	if strings.HasPrefix(rhs, "\"") && strings.HasSuffix(rhs, "\"") && len(rhs) >= 2 {
		return rhs[1 : len(rhs)-1], nil
	}
	if isNonNegInt(rhs) {
		return rhs, nil
	}
	switch lhs {
	case lhsOutcomeStatus:
		if !closedStatusValues[rhs] {
			return "", &ParseError{
				Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.status is not a valid status value "+
						"(must be one of SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS) in condition %q",
					rhs, fullCond),
			}
		}
	case lhsOutcomeFailureClass:
		if !closedFailureClassValues[rhs] {
			return "", &ParseError{
				Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.failure_class is not a valid failure class value in condition %q",
					rhs, fullCond),
			}
		}
	case lhsOutcomeKind:
		if !closedKindValues[rhs] {
			return "", &ParseError{
				Line: line,
				Message: fmt.Sprintf(
					"condition: RHS %q for outcome.kind is not a valid OutcomeKind value in condition %q",
					rhs, fullCond),
			}
		}
	default:
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

func isValidWG001NodeType(nt core.NodeType) bool {
	switch nt {
	case core.NodeTypeAgentic, core.NodeTypeNonAgentic, core.NodeTypeGate, core.NodeTypeSubWorkflow:
		return true
	default:
		return false
	}
}

func validateNoProgressGuard(val string) error {
	switch val {
	case "", "strict", "off":
		return nil
	}
	if strings.HasPrefix(val, "capped:") {
		n, err := strconv.Atoi(strings.TrimPrefix(val, "capped:"))
		if err != nil || n < 1 {
			return fmt.Errorf("capped:N requires N to be a positive integer; got %q", strings.TrimPrefix(val, "capped:"))
		}
		return nil
	}
	return fmt.Errorf("must be one of \"\", \"strict\", \"off\", or \"capped:N\" (N >= 1); got %q", val)
}

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
