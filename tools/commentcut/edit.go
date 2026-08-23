package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Splice replaces the byte range [Start,End) of a file with Text. It is the
// only shape of edit this tool makes: the original file bytes are spliced, and
// the AST is never re-printed. go/printer decides comment placement from
// heuristics and re-emits the whole file, which is exactly the fragile thing
// this tool exists to avoid.
type Splice struct {
	Start int
	End   int
	Text  string
}

// applySplices returns src with every splice applied. Splices must not overlap.
func applySplices(src []byte, ss []Splice) ([]byte, error) {
	sorted := append([]Splice(nil), ss...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	out := make([]byte, 0, len(src))
	prev := 0
	for _, s := range sorted {
		if s.Start < prev {
			return nil, fmt.Errorf("overlapping edits at byte %d", s.Start)
		}
		if s.Start < 0 || s.End > len(src) || s.Start > s.End {
			return nil, fmt.Errorf("edit range [%d,%d) outside file of %d bytes", s.Start, s.End, len(src))
		}
		out = append(out, src[prev:s.Start]...)
		out = append(out, s.Text...)
		prev = s.End
	}
	out = append(out, src[prev:]...)
	return out, nil
}

// deleteSplices turns cuttable blocks into whole-line deletions.
func deleteSplices(fb *FileBlocks) []Splice {
	ss := make([]Splice, 0, len(fb.Blocks))
	for _, b := range fb.Blocks {
		if !b.Cuttable {
			continue
		}
		ss = append(ss, Splice{Start: b.startOff, End: b.endOff})
	}
	return ss
}

// truncateSplice shortens one comment block to its first keep physical lines.
// It returns ok=false when the block is already short enough or when nothing
// would survive.
//
// The hard case is a block comment. Cutting its physical lines can leave the
// "/*" open, and a tool that drops the closing "*/" produces a file that no
// longer parses. This function tracks the comment state across the kept lines
// and re-emits a closing delimiter when the cut lands inside a block.
func truncateSplice(src []byte, b *Block, keep int) (Splice, bool, error) {
	if keep < 1 {
		return Splice{}, false, fmt.Errorf("keep must be at least 1, got %d", keep)
	}
	if b.Trailing || b.endOff == 0 {
		return Splice{}, false, nil
	}
	if b.Lines <= keep {
		return Splice{}, false, nil
	}
	raw := string(src[b.startOff:b.endOff])
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) <= keep {
		return Splice{}, false, nil
	}
	kept := lines[:keep]

	// Drop trailing lines that carry no words: an empty "//" tail reads as a
	// dangling paragraph break.
	for len(kept) > 1 && isEmptyCommentLine(kept[len(kept)-1]) {
		kept = kept[:len(kept)-1]
	}
	if len(kept) == 0 {
		return Splice{}, false, nil
	}
	if inBlockComment(strings.Join(kept, "\n")) {
		kept[len(kept)-1] = closeBlock(kept[len(kept)-1])
	}
	// Refuse a result that says nothing. "-keep 1" against a block comment
	// whose first line is a bare "/*" produces "/* */", which still parses,
	// still satisfies nothing, and takes a required doc comment away from
	// revive without any check noticing.
	if !hasWord(strings.Join(kept, "\n")) {
		return Splice{}, false, nil
	}
	text := strings.Join(kept, "\n") + "\n"
	if text == raw {
		return Splice{}, false, nil
	}
	return Splice{Start: b.startOff, End: b.endOff, Text: text}, true, nil
}

// hasWord reports whether s carries a letter or a digit anywhere outside its
// comment delimiters.
func hasWord(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func isEmptyCommentLine(l string) bool {
	t := strings.TrimSpace(l)
	return t == "//" || t == "" || t == "*" || t == "/*"
}

// inBlockComment reports whether s ends with an open "/*" that no "*/" closed.
// s is comment text only, so the only states are "outside", "in a line comment
// until end of line" and "in a block comment".
func inBlockComment(s string) bool {
	const (
		outside = iota
		inLine
		inBlock
	)
	state := outside
	for i := 0; i < len(s); i++ {
		switch state {
		case outside:
			if s[i] == '/' && i+1 < len(s) {
				switch s[i+1] {
				case '/':
					state = inLine
					i++
				case '*':
					state = inBlock
					i++
				}
			}
		case inLine:
			if s[i] == '\n' {
				state = outside
			}
		case inBlock:
			if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
				state = outside
				i++
			}
		}
	}
	return state == inBlock
}

// closeBlock appends a closing delimiter to a line that ends inside a block
// comment, with a separating space so a trailing "*" cannot fuse into "**/".
func closeBlock(line string) string {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == "" {
		return line + "*/"
	}
	return trimmed + " */"
}
