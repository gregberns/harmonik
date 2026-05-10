package workflowvalidator

// dotparser.go — minimal DOT parser for the workflow-validator (EM-038).
//
// Parses only the strict subset of DOT used by harmonik workflow definitions:
//
//	digraph <name> {
//	    graph [ <key> = <value> ... ]
//	    <node_id> [ <key> = <value> ... ]
//	    <from> -> <to>
//	    <from> -> <to> [ <key> = <value> ... ]
//	}
//
// The parser is intentionally limited: it does not implement the full DOT
// language. Features not listed above produce a parse error; this satisfies
// the DOT-parseability check in EM-038 without pulling in an external library.
//
// Spec ref: specs/execution-model.md §4.9.EM-038.
// Tags: mechanism (EM-039 — no semantic judgment delegated to cognition).

import (
	"fmt"
	"strings"
	"unicode"
)

// rawGraph is the intermediate parse result before validation.
type rawGraph struct {
	// graphAttrs holds attributes from the graph [ ... ] block.
	graphAttrs map[string]string
	// nodes maps node ID → attribute map.
	nodes map[string]map[string]string
	// nodeOrder preserves declaration order (for deterministic iteration in tests).
	nodeOrder []string
	// edges holds raw edge records.
	edges []rawEdge
}

type rawEdge struct {
	from  string
	to    string
	attrs map[string]string
}

// parseDOT parses a harmonik workflow DOT document and returns the raw graph.
// Returns an error if the document is not parseable per the supported subset.
//
// Tags: mechanism.
func parseDOT(src string) (*rawGraph, error) {
	p := &dotParser{src: src, pos: 0}
	return p.parseDigraph()
}

// dotParser is a simple hand-written recursive-descent parser.
type dotParser struct {
	src string
	pos int
}

func (p *dotParser) parseDigraph() (*rawGraph, error) {
	p.skipWS()
	if err := p.expect("digraph"); err != nil {
		return nil, err
	}
	p.skipWS()
	// Consume the optional graph name (identifier or string) up to '{'.
	_ = p.consumeUntil('{')
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	g := &rawGraph{
		graphAttrs: make(map[string]string),
		nodes:      make(map[string]map[string]string),
	}
	if err := p.parseBody(g); err != nil {
		return nil, err
	}
	p.skipWS()
	if p.pos < len(p.src) && strings.TrimSpace(p.src[p.pos:]) != "}" {
		if err := p.expect("}"); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// parseBody parses the content between the outer braces of a digraph.
func (p *dotParser) parseBody(g *rawGraph) error {
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			return fmt.Errorf("dotparser: unexpected EOF inside digraph body")
		}
		// Closing brace ends the body.
		if p.src[p.pos] == '}' {
			p.pos++ // consume '}'
			return nil
		}
		// Comments: // or /* */
		if p.pos+1 < len(p.src) && p.src[p.pos] == '/' {
			if err := p.skipComment(); err != nil {
				return err
			}
			continue
		}
		// Peek: is it a "graph" keyword?
		if strings.HasPrefix(p.src[p.pos:], "graph") {
			rest := p.src[p.pos+5:]
			if len(rest) == 0 || !isIDChar(rune(rest[0])) {
				p.pos += 5
				p.skipWS()
				attrs, err := p.parseAttrList()
				if err != nil {
					return fmt.Errorf("dotparser: graph attrs: %w", err)
				}
				for k, v := range attrs {
					g.graphAttrs[k] = v
				}
				p.skipWS()
				p.consumeOptionalSemicolon()
				continue
			}
		}
		// Read an identifier (node name or 'graph').
		id, err := p.readIdentifier()
		if err != nil {
			return fmt.Errorf("dotparser: expected identifier: %w", err)
		}
		p.skipWS()
		// Edge: id -> ...
		if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] == '>' {
			p.pos += 2
			p.skipWS()
			toID, err2 := p.readIdentifier()
			if err2 != nil {
				return fmt.Errorf("dotparser: edge target: %w", err2)
			}
			p.skipWS()
			var edgeAttrs map[string]string
			if p.pos < len(p.src) && p.src[p.pos] == '[' {
				edgeAttrs, err2 = p.parseAttrList()
				if err2 != nil {
					return fmt.Errorf("dotparser: edge attrs: %w", err2)
				}
			}
			if edgeAttrs == nil {
				edgeAttrs = make(map[string]string)
			}
			g.edges = append(g.edges, rawEdge{from: id, to: toID, attrs: edgeAttrs})
			p.skipWS()
			p.consumeOptionalSemicolon()
			continue
		}
		// Node: id [ ... ]
		if p.pos < len(p.src) && p.src[p.pos] == '[' {
			attrs, err2 := p.parseAttrList()
			if err2 != nil {
				return fmt.Errorf("dotparser: node %q attrs: %w", id, err2)
			}
			if _, seen := g.nodes[id]; !seen {
				g.nodeOrder = append(g.nodeOrder, id)
			}
			g.nodes[id] = attrs
			p.skipWS()
			p.consumeOptionalSemicolon()
			continue
		}
		// Bare identifier with no attrs and no edge — treat as node with empty attrs.
		if _, seen := g.nodes[id]; !seen {
			g.nodeOrder = append(g.nodeOrder, id)
		}
		g.nodes[id] = make(map[string]string)
		p.consumeOptionalSemicolon()
	}
}

// parseAttrList parses a [ key = value ; key = value ] attribute list.
// The leading '[' must be the next non-whitespace character.
func (p *dotParser) parseAttrList() (map[string]string, error) {
	p.skipWS()
	if p.pos >= len(p.src) || p.src[p.pos] != '[' {
		return nil, fmt.Errorf("dotparser: expected '[', got %q", p.src[p.pos:p.pos+1])
	}
	p.pos++ // consume '['
	attrs := make(map[string]string)
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("dotparser: unexpected EOF inside attribute list")
		}
		if p.src[p.pos] == ']' {
			p.pos++ // consume ']'
			return attrs, nil
		}
		// Optional comma or semicolon separators between attribute pairs.
		if p.src[p.pos] == ',' || p.src[p.pos] == ';' {
			p.pos++
			continue
		}
		// Read key.
		key, err := p.readAttrKey()
		if err != nil {
			return nil, fmt.Errorf("dotparser: attr key: %w", err)
		}
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != '=' {
			return nil, fmt.Errorf("dotparser: expected '=' after key %q", key)
		}
		p.pos++ // consume '='
		p.skipWS()
		// Read value.
		val, err := p.readAttrValue()
		if err != nil {
			return nil, fmt.Errorf("dotparser: attr value for key %q: %w", key, err)
		}
		attrs[key] = val
	}
}

// readAttrKey reads an attribute key, which may be a quoted string (e.g. "llm-freedom")
// or an unquoted identifier.
func (p *dotParser) readAttrKey() (string, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("dotparser: unexpected EOF reading attr key")
	}
	if p.src[p.pos] == '"' {
		return p.readQuotedString()
	}
	return p.readIdentifier()
}

// readAttrValue reads an attribute value: quoted string or unquoted identifier/number.
func (p *dotParser) readAttrValue() (string, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("dotparser: unexpected EOF reading attr value")
	}
	if p.src[p.pos] == '"' {
		return p.readQuotedString()
	}
	return p.readIdentifier()
}

// readQuotedString reads a double-quoted string, handling \" escapes.
func (p *dotParser) readQuotedString() (string, error) {
	if p.pos >= len(p.src) || p.src[p.pos] != '"' {
		return "", fmt.Errorf("dotparser: expected '\"'")
	}
	p.pos++ // consume opening '"'
	var b strings.Builder
	for {
		if p.pos >= len(p.src) {
			return "", fmt.Errorf("dotparser: unterminated quoted string")
		}
		ch := p.src[p.pos]
		if ch == '"' {
			p.pos++ // consume closing '"'
			return b.String(), nil
		}
		if ch == '\\' && p.pos+1 < len(p.src) {
			p.pos++
			b.WriteByte(p.src[p.pos])
			p.pos++
			continue
		}
		b.WriteByte(ch)
		p.pos++
	}
}

// readIdentifier reads an unquoted identifier: letters, digits, underscores, hyphens, dots.
// DOT identifiers may not start with a digit when interpreted as node IDs, but the
// grammar allows numerals; we accept any non-whitespace run that forms a valid token.
func (p *dotParser) readIdentifier() (string, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("dotparser: unexpected EOF reading identifier")
	}
	start := p.pos
	for p.pos < len(p.src) && isIDChar(rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("dotparser: unexpected character %q at position %d", string(p.src[p.pos]), p.pos)
	}
	return p.src[start:p.pos], nil
}

// skipWS advances past whitespace and newlines.
func (p *dotParser) skipWS() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

// skipComment skips a // line comment or a /* block */ comment.
func (p *dotParser) skipComment() error {
	if p.pos+1 >= len(p.src) {
		return fmt.Errorf("dotparser: unexpected '/' at end of input")
	}
	if p.src[p.pos+1] == '/' {
		// Line comment: skip to end of line.
		p.pos += 2
		for p.pos < len(p.src) && p.src[p.pos] != '\n' {
			p.pos++
		}
		return nil
	}
	if p.src[p.pos+1] == '*' {
		// Block comment: skip to */.
		p.pos += 2
		for p.pos+1 < len(p.src) {
			if p.src[p.pos] == '*' && p.src[p.pos+1] == '/' {
				p.pos += 2
				return nil
			}
			p.pos++
		}
		return fmt.Errorf("dotparser: unterminated block comment")
	}
	return fmt.Errorf("dotparser: unexpected '/' at position %d", p.pos)
}

// consumeOptionalSemicolon consumes a trailing semicolon if present.
func (p *dotParser) consumeOptionalSemicolon() {
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ';' {
		p.pos++
	}
}

// consumeUntil returns everything up to (not including) the next occurrence of
// delim and advances pos to that delimiter.
func (p *dotParser) consumeUntil(delim byte) string {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != delim {
		p.pos++
	}
	return p.src[start:p.pos]
}

// expect consumes the given literal string (case-sensitive) or returns an error.
func (p *dotParser) expect(lit string) error {
	p.skipWS()
	if !strings.HasPrefix(p.src[p.pos:], lit) {
		snippet := p.src[p.pos:]
		if len(snippet) > 20 {
			snippet = snippet[:20]
		}
		return fmt.Errorf("dotparser: expected %q, got %q at position %d", lit, snippet, p.pos)
	}
	p.pos += len(lit)
	return nil
}

// isIDChar reports whether r is valid inside a DOT identifier.
// Accepts letters, digits, underscore, hyphen, dot, slash, colon, @.
// This is a superset of the strict DOT grammar to accommodate workflow IDs and refs.
func isIDChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) ||
		r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == '@'
}
