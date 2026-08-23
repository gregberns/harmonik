package main

import (
	"go/format"
	"strings"
	"testing"
)

// TestHazardRuleFlagsEveryFormatterHazard states the rule as cases. Each source
// is a whole file; the marker names the comment under test.
func TestHazardRuleFlagsEveryFormatterHazard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		marker string
		want   string // "" means the rule must clear the comment
	}{
		{
			name: "comment splits an aligned composite literal",
			src: `package p

type T struct {
	A          int
	LongerName int
}

func f() {
	_ = T{
		A: 1,
		// splitter
		LongerName: 2,
	}
}
`,
			marker: "splitter",
			want:   "hazard:alignment-group-merge",
		},
		{
			name: "a blank line above the comment already separates the runs",
			src: `package p

type T struct {
	A          int
	LongerName int
}

func f() {
	_ = T{
		A: 1,

		// splitter
		LongerName: 2,
	}
}
`,
			marker: "splitter",
			want:   "",
		},
		{
			name: "comment splits an aligned struct field list",
			src: `package p

type S struct {
	A int
	// splitter
	LongerName string
}
`,
			marker: "splitter",
			want:   "hazard:alignment-group-merge",
		},
		{
			name: "comment splits a parenthesised const block",
			src: `package p

const (
	A = 1
	// splitter
	LongerName = 2
)
`,
			marker: "splitter",
			want:   "hazard:alignment-group-merge",
		},
		{
			name: "comment inside one element forces the wrap",
			src: `package p

type T struct{ Inject string }

func f() {
	_ = T{Inject: // nolint is not here
	// joiner
	"text"}
}
`,
			marker: "joiner",
			want:   "hazard:one-line-join",
		},
		{
			name: "comment is the sole content of a composite literal",
			src: `package p

func f() {
	_ = map[string]int{
		// sole
	}
}
`,
			marker: "sole",
			want:   "hazard:sole-content-collapse",
		},
		{
			name: "comment is the sole content of a struct body",
			src: `package p

type S struct {
	// sole
}
`,
			marker: "sole",
			want:   "hazard:sole-content-collapse",
		},
		{
			name: "comment is the sole content of a grouped var declaration",
			src: `package p

var (
// sole
)
`,
			marker: "sole",
			want:   "hazard:sole-content-collapse",
		},
		{
			name: "an interface method list is not column aligned",
			src: `package p

type I interface {
	A()
	// splitter
	LongerName()
}
`,
			marker: "splitter",
			want:   "",
		},
		{
			name: "a statement list in a function body is not aligned",
			src: `package p

func f() {
	a := 1
	// splitter
	longerName := 2
	_, _ = a, longerName
}
`,
			marker: "splitter",
			want:   "",
		},
		{
			name: "a comment at the head of a container has nothing to merge",
			src: `package p

type S struct {
	// head
	A          int
	LongerName string
}
`,
			marker: "head",
			want:   "",
		},
		{
			name: "a comment at the tail of a container has nothing to merge",
			src: `package p

type S struct {
	A          int
	LongerName string
	// tail
}
`,
			marker: "tail",
			want:   "",
		},
		{
			name: "a trailing comment inside an aligned container is refused",
			src: `package p

type S struct {
	A          int // trailing
	LongerName string
}
`,
			marker: "trailing",
			want:   "hazard:trailing-in-aligned-container",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := blockForMarker(t, tc.src, tc.marker, "max")
			got := ""
			for _, r := range b.Reasons {
				if strings.HasPrefix(r, "hazard:") {
					got = r
					break
				}
			}
			if got != tc.want {
				t.Fatalf("hazard reason = %q, want %q (all reasons %v)", got, tc.want, b.Reasons)
			}
		})
	}
}

// TestAlignmentHazardIsRealNotTheoretical breaks it on purpose and watches. It
// deletes a comment the hazard rule refuses, formats the result, and proves the
// code bytes moved. Without this the rule could be blocking nothing and every
// other test would still pass.
func TestAlignmentHazardIsRealNotTheoretical(t *testing.T) {
	t.Parallel()
	const src = `package p

type S struct {
	A int
	// splitter
	LongerName string
}
`
	b := blockForMarker(t, src, "splitter", "max")
	if b.Cuttable {
		t.Fatal("the hazard rule cleared a comment that reformats the code; the rule is not working")
	}

	forced, err := applySplices([]byte(src), []Splice{{Start: b.startOff, End: b.endOff}})
	if err != nil {
		t.Fatalf("applySplices: %v", err)
	}
	formatted, err := format.Source(forced)
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	res := checkFile([]byte(src), formatted)
	if res.OK {
		t.Fatalf("deleting the comment left the code bytes alone; the fixture no longer reproduces "+
			"the hazard and this test defends nothing:\n%s", formatted)
	}
	if !strings.Contains(res.Reason, "code line") {
		t.Fatalf("expected a changed code line, got: %s", res.Reason)
	}
	if !strings.Contains(string(formatted), "A          int") {
		t.Fatalf("expected the key column to be re-padded, got:\n%s", formatted)
	}
}

// TestCuttingASafeCommentLeavesCodeAlone is the positive half of the pair
// above: the same machinery, on a comment the rule clears, must change nothing.
func TestCuttingASafeCommentLeavesCodeAlone(t *testing.T) {
	t.Parallel()
	const src = `package p

func f() {
	a := 1
	// splitter explains the next line.
	longerName := 2
	_, _ = a, longerName
}
`
	b := blockForMarker(t, src, "splitter", "safe")
	if !b.Cuttable {
		t.Fatalf("expected the comment to be cuttable, reasons: %v", b.Reasons)
	}
	cut, err := applySplices([]byte(src), []Splice{{Start: b.startOff, End: b.endOff}})
	if err != nil {
		t.Fatalf("applySplices: %v", err)
	}
	formatted, err := format.Source(cut)
	if err != nil {
		t.Fatalf("format.Source: %v", err)
	}
	if res := checkFile([]byte(src), formatted); !res.OK {
		t.Fatalf("safe cut changed code: %s", res.Reason)
	}
	if strings.Contains(string(formatted), "splitter") {
		t.Fatalf("comment survived the cut:\n%s", formatted)
	}
}
