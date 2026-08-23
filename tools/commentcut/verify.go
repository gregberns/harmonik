package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"strings"
)

// tokenStream returns the file's tokens with comments excluded, one per line of
// the returned slice. Positions are deliberately absent: deleting a comment
// moves every later token, and the claim under test is that the token SEQUENCE
// did not change.
func tokenStream(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f := fset.AddFile("in.go", -1, len(src))
	var errs scanner.ErrorList
	var s scanner.Scanner
	s.Init(f, src, func(pos token.Position, msg string) { errs.Add(pos, msg) }, 0)
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		out = append(out, tok.String()+"\x00"+lit)
	}
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	return out, nil
}

// blankComments returns src with every comment byte replaced by a space,
// newlines kept so line structure survives.
func blankComments(src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	f := fset.AddFile("in.go", -1, len(src))
	var errs scanner.ErrorList
	var s scanner.Scanner
	s.Init(f, src, func(pos token.Position, msg string) { errs.Add(pos, msg) }, scanner.ScanComments)
	out := append([]byte(nil), src...)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		off := f.Offset(pos)
		for i := off; i < off+len(lit) && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	return out, nil
}

// codeLine is one line of code with its comments blanked out: the code text
// itself, and the column the line's own trailing comment starts at, or -1.
type codeLine struct {
	text string
	// col is the byte offset of the first comment on the line, or -1 when the
	// line has none.
	col int
}

// codeLines returns the file's code with every comment blanked and every line
// that is then whitespace-only dropped.
//
// The comment column is carried alongside because check (b) has one blind spot
// that the syntactic hazard rule cannot see either. gofmt aligns the trailing
// comments of consecutive lines into one tabwriter run, and a floating comment
// between two such lines separates the runs. Delete it and the runs merge, so
// gofmt re-pads a CODE line:
//
//	x := 1 // short   ->   x := 1                       // short
//
// The token stream is identical and the code text is identical, so with the
// column dropped the tool reported the file as clean and "verify -baseline"
// agreed. Comparing the column catches it and the run reverts the file.
//
// The column is compared only when BOTH sides have a comment. A trailing
// comment that was deleted outright takes its own padding with it, and that is
// not a code change.
func codeLines(src []byte) ([]codeLine, error) {
	blanked, err := blankComments(src)
	if err != nil {
		return nil, err
	}
	cols, err := commentColumns(src)
	if err != nil {
		return nil, err
	}
	split := strings.Split(string(blanked), "\n")
	out := make([]codeLine, 0, len(split))
	for i, l := range split {
		l = strings.TrimRight(l, "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		col := -1
		if c, ok := cols[i+1]; ok {
			col = c
		}
		out = append(out, codeLine{text: strings.TrimRight(l, " \t"), col: col})
	}
	return out, nil
}

// commentColumns maps a 1-based line number to the column of the first comment
// that starts on it, for lines that carry code before the comment. A line whose
// comment starts at column 0 has no code in front of it and no alignment to
// disturb.
func commentColumns(src []byte) (map[int]int, error) {
	fset := token.NewFileSet()
	f := fset.AddFile("in.go", -1, len(src))
	var errs scanner.ErrorList
	var s scanner.Scanner
	s.Init(f, src, func(pos token.Position, msg string) { errs.Add(pos, msg) }, scanner.ScanComments)
	out := map[int]int{}
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		p := f.Position(pos)
		if p.Column <= 1 {
			continue
		}
		if _, seen := out[p.Line]; !seen {
			out[p.Line] = p.Column
		}
	}
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	return out, nil
}

// CheckResult says whether one file passed the two static checks and, when it
// did not, which one and where.
type CheckResult struct {
	OK     bool
	Reason string
}

// checkFile runs the two static gates on one file: identical token stream, and
// an empty list of changed code lines.
func checkFile(before, after []byte) CheckResult {
	tb, err := tokenStream(before)
	if err != nil {
		return CheckResult{Reason: "original does not scan: " + err.Error()}
	}
	ta, err := tokenStream(after)
	if err != nil {
		return CheckResult{Reason: "result does not scan: " + err.Error()}
	}
	if d := firstDiff(tb, ta); d >= 0 {
		return CheckResult{Reason: fmt.Sprintf("token stream changed at token %d: %s -> %s",
			d, atOr(tb, d), atOr(ta, d))}
	}
	cb, err := codeLines(before)
	if err != nil {
		return CheckResult{Reason: "original does not scan: " + err.Error()}
	}
	ca, err := codeLines(after)
	if err != nil {
		return CheckResult{Reason: "result does not scan: " + err.Error()}
	}
	if d, why := firstCodeDiff(cb, ca); d >= 0 {
		return CheckResult{Reason: fmt.Sprintf("code line %d %s: %q -> %q",
			d+1, why, lineAt(cb, d), lineAt(ca, d))}
	}
	return CheckResult{OK: true}
}

// firstCodeDiff returns the index of the first code line that changed and the
// name of what changed about it, or -1.
func firstCodeDiff(a, b []codeLine) (index int, what string) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i].text != b[i].text {
			return i, "changed"
		}
		if a[i].col >= 0 && b[i].col >= 0 && a[i].col != b[i].col {
			return i, fmt.Sprintf("had its trailing comment re-padded from column %d to %d",
				a[i].col, b[i].col)
		}
	}
	if len(a) != len(b) {
		return n, "changed"
	}
	return -1, ""
}

func lineAt(ls []codeLine, i int) string {
	if i < 0 || i >= len(ls) {
		return "<end of file>"
	}
	return ls[i].text
}

func firstDiff(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

func atOr(ss []string, i int) string {
	if i < 0 || i >= len(ss) {
		return "<end of file>"
	}
	return strings.ReplaceAll(ss[i], "\x00", " ")
}
