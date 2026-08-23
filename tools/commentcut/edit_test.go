package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestTruncateKeepsBlockCommentsWhole is the regression the prior prototype
// failed. It cut the physical lines of a /* */ comment, dropped the closing
// delimiter, produced a file that no longer parsed, and then skipped the file
// without saying so. Only three files in this tree carry a multi-line block
// comment, so the defect looked clean.
func TestTruncateKeepsBlockCommentsWhole(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		src        string
		marker     string
		keep       int
		wantLines  []string
		wantNoCut  bool
		mustParse  bool
		mustAbsent string
	}{
		{
			name: "multi-line block comment closes at the cut",
			src: `package p

/*
Line one of the block.
Line two of the block.
Line three of the block.
*/
func f() {}
`,
			marker:     "Line",
			keep:       3,
			wantLines:  []string{"/*", "Line one of the block.", "Line two of the block. */"},
			mustParse:  true,
			mustAbsent: "Line three",
		},
		{
			name: "cutting right after the opener still closes",
			src: `package p

/*
Line one of the block.
Line two of the block.
*/
func f() {}
`,
			marker:    "Line",
			keep:      2,
			wantLines: []string{"/*", "Line one of the block. */"},
			mustParse: true,
		},
		{
			name: "a kept line ending in a star does not fuse into a double close",
			src: `package p

/*
 * one
 * two
 * three
 */
func f() {}
`,
			marker:    "one",
			keep:      2,
			wantLines: []string{"/*", " * one */"},
			mustParse: true,
		},
		{
			name: "a block comment that already fits is left alone",
			src: `package p

/* short */
func f() {}
`,
			marker:    "short",
			keep:      2,
			wantNoCut: true,
		},
		{
			name: "a line comment group keeps its first lines",
			src: `package p

// Exported does the first thing.
//
// It also does a second thing, described at length.
// And a third.
func Exported() {}
`,
			marker:     "Exported does",
			keep:       2,
			wantLines:  []string{"// Exported does the first thing."},
			mustParse:  true,
			mustAbsent: "second thing",
		},
		{
			name: "keep=1 keeps only the summary line",
			src: `package p

// Exported does the first thing.
// More detail here.
func Exported() {}
`,
			marker:    "Exported does",
			keep:      1,
			wantLines: []string{"// Exported does the first thing."},
			mustParse: true,
		},
		{
			name: "a group mixing a line comment and a block comment stays valid",
			src: `package p

// Exported does the first thing.
/*
 detail one
 detail two
*/
func Exported() {}
`,
			marker:    "Exported does",
			keep:      3,
			wantLines: []string{"// Exported does the first thing.", "/*", " detail one */"},
			mustParse: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fb, err := classifyFile("x.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("classifyFile: %v", err)
			}
			var b *Block
			for _, cand := range fb.Blocks {
				if strings.Contains(cand.Text, tc.marker) {
					b = cand
					break
				}
			}
			if b == nil {
				t.Fatalf("no block containing %q", tc.marker)
			}
			s, ok, err := truncateSplice([]byte(tc.src), b, tc.keep)
			if err != nil {
				t.Fatalf("truncateSplice: %v", err)
			}
			if tc.wantNoCut {
				if ok {
					t.Fatalf("expected no truncation, got %q", s.Text)
				}
				return
			}
			if !ok {
				t.Fatal("expected a truncation, got none")
			}
			got := strings.Split(strings.TrimSuffix(s.Text, "\n"), "\n")
			if len(got) != len(tc.wantLines) {
				t.Fatalf("got %d lines %q, want %d lines %q", len(got), got, len(tc.wantLines), tc.wantLines)
			}
			for i := range got {
				if got[i] != tc.wantLines[i] {
					t.Errorf("line %d = %q, want %q", i+1, got[i], tc.wantLines[i])
				}
			}
			out, err := applySplices([]byte(tc.src), []Splice{s})
			if err != nil {
				t.Fatalf("applySplices: %v", err)
			}
			if tc.mustParse {
				fset := token.NewFileSet()
				if _, err := parser.ParseFile(fset, "x.go", out, parser.ParseComments); err != nil {
					t.Fatalf("truncated file does not parse: %v\n%s", err, out)
				}
			}
			if tc.mustAbsent != "" && strings.Contains(string(out), tc.mustAbsent) {
				t.Fatalf("expected %q to be gone:\n%s", tc.mustAbsent, out)
			}
			if res := checkFile([]byte(tc.src), out); !res.OK {
				t.Fatalf("truncation changed code: %s", res.Reason)
			}
		})
	}
}

func TestInBlockCommentTracksState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"// plain", false},
		{"/* closed */", false},
		{"/* open", true},
		{"/* open\nstill open", true},
		{"/* one */ /* two", true},
		{"/* one */\n// two", false},
		{"// a line comment mentioning /* does not open one", false},
		{"/*\n * one\n */", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := inBlockComment(tc.in); got != tc.want {
				t.Fatalf("inBlockComment(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestTruncateRefusesKeepBelowOne(t *testing.T) {
	t.Parallel()
	if _, _, err := truncateSplice([]byte("package p\n"), &Block{Lines: 5}, 0); err == nil {
		t.Fatal("expected an error for keep=0")
	}
}

func TestApplySplicesRefusesOverlap(t *testing.T) {
	t.Parallel()
	src := []byte("0123456789")
	if _, err := applySplices(src, []Splice{{Start: 0, End: 5}, {Start: 3, End: 7}}); err == nil {
		t.Fatal("expected an overlap error")
	}
	if _, err := applySplices(src, []Splice{{Start: 0, End: 20}}); err == nil {
		t.Fatal("expected an out-of-range error")
	}
}
