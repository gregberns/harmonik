// Package evalexpreval implements a tokenizer and recursive-descent evaluator
// for arithmetic expressions with +, -, *, /, parentheses, and standard
// operator precedence.
package evalexpreval

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"
)

// ErrDivisionByZero is returned when the evaluated expression divides by zero.
var ErrDivisionByZero = errors.New("division by zero")

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNum
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokLParen
	tokRParen
)

type token struct {
	kind tokenKind
	num  float64
}

type lexer struct {
	src []rune
	pos int
}

func (l *lexer) skipWS() {
	for l.pos < len(l.src) && unicode.IsSpace(l.src[l.pos]) {
		l.pos++
	}
}

func (l *lexer) next() (token, error) {
	l.skipWS()
	if l.pos >= len(l.src) {
		return token{kind: tokEOF}, nil
	}
	ch := l.src[l.pos]
	switch ch {
	case '+':
		l.pos++
		return token{kind: tokPlus}, nil
	case '-':
		l.pos++
		return token{kind: tokMinus}, nil
	case '*':
		l.pos++
		return token{kind: tokStar}, nil
	case '/':
		l.pos++
		return token{kind: tokSlash}, nil
	case '(':
		l.pos++
		return token{kind: tokLParen}, nil
	case ')':
		l.pos++
		return token{kind: tokRParen}, nil
	}
	if unicode.IsDigit(ch) || ch == '.' {
		start := l.pos
		for l.pos < len(l.src) && (unicode.IsDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
			l.pos++
		}
		v, err := strconv.ParseFloat(string(l.src[start:l.pos]), 64)
		if err != nil {
			return token{}, fmt.Errorf("invalid number: %s", string(l.src[start:l.pos]))
		}
		return token{kind: tokNum, num: v}, nil
	}
	return token{}, fmt.Errorf("unexpected character %q at position %d", ch, l.pos)
}

type parser struct {
	lex *lexer
	cur token
	err error
}

func newParser(s string) *parser {
	p := &parser{lex: &lexer{src: []rune(s)}}
	p.advance()
	return p
}

func (p *parser) advance() {
	if p.err != nil {
		return
	}
	p.cur, p.err = p.lex.next()
}

// expr → term (('+' | '-') term)*
func (p *parser) expr() float64 {
	v := p.term()
	for p.err == nil && (p.cur.kind == tokPlus || p.cur.kind == tokMinus) {
		op := p.cur.kind
		p.advance()
		r := p.term()
		if op == tokPlus {
			v += r
		} else {
			v -= r
		}
	}
	return v
}

// term → factor (('*' | '/') factor)*
func (p *parser) term() float64 {
	v := p.factor()
	for p.err == nil && (p.cur.kind == tokStar || p.cur.kind == tokSlash) {
		op := p.cur.kind
		p.advance()
		r := p.factor()
		if p.err != nil {
			break
		}
		if op == tokStar {
			v *= r
		} else {
			if r == 0 {
				p.err = ErrDivisionByZero
				break
			}
			v /= r
		}
	}
	return v
}

// factor → ('-' factor) | ('(' expr ')') | number
func (p *parser) factor() float64 {
	if p.err != nil {
		return 0
	}
	switch p.cur.kind {
	case tokMinus:
		p.advance()
		return -p.factor()
	case tokLParen:
		p.advance()
		v := p.expr()
		if p.err != nil {
			return 0
		}
		if p.cur.kind != tokRParen {
			p.err = fmt.Errorf("missing ')'")
			return 0
		}
		p.advance()
		return v
	case tokNum:
		v := p.cur.num
		p.advance()
		return v
	default:
		p.err = fmt.Errorf("unexpected token at position %d", p.lex.pos)
		return 0
	}
}

// Eval parses and evaluates the arithmetic expression s.
// Returns ErrDivisionByZero for division by zero, or a descriptive error for
// malformed input (unexpected characters, unbalanced parentheses, etc.).
func Eval(s string) (float64, error) {
	p := newParser(s)
	if p.err != nil {
		return 0, p.err
	}
	if p.cur.kind == tokEOF {
		return 0, fmt.Errorf("empty expression")
	}
	v := p.expr()
	if p.err != nil {
		return 0, p.err
	}
	if p.cur.kind != tokEOF {
		return 0, fmt.Errorf("unexpected input after expression")
	}
	return v, nil
}
