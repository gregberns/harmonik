package main

import (
	"strings"
	"testing"
)

// TestDirectiveRecognitionMatchesGoCompiler pins the one detail that a
// substring matcher gets wrong: a comment that TALKS about a directive is
// prose, and a comment that IS one is a directive. cmd/harmonik/asset_manifest.go
// carries the prose form in the live tree.
func TestDirectiveRecognitionMatchesGoCompiler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"go:build", "//go:build scenario", true},
		{"go:build with expression", "//go:build integration && darwin", true},
		{"go:embed", "//go:embed assets", true},
		{"go:generate", "//go:generate stringer -type=Kind", true},
		{"nolint with explanation", "//nolint:gosec // path comes from a validated config", true},
		{"nolint bare", "//nolint", true},
		{"dirmode allow", "//dirmode:allow the site owns its own permission bits", true},
		{"cloexec waived", "//cloexec:waived the fd is handed to a child on purpose", true},
		{"lint ignore", "//lint:ignore SA1019 the replacement is not in this Go version", true},
		{"line directive", "//line foo.go:12", true},
		{"export", "//export CFunc", true},
		{"extern", "//extern gccgoSymbol", true},
		{"nosec with a space", "// #nosec G304 -- the path is validated above", true},
		{"nosec no space", "//#nosec G204", true},
		// The forms a word-colon test cannot see. None occurs in this tree
		// today; each is here because getting it wrong is silent.
		{"legacy build constraint", "// +build scenario", true},
		{"legacy build constraint, no space", "//+build linux,!cgo", true},
		{"mksyscall prototype", "//sys\treadlen(fd int) (n int, err error)", true},
		{"mksyscall nonblocking prototype", "//sysnb Gettimeofday(tv *Timeval) (err error)", true},
		{"nolint with a leading space", "// nolint:gosec // space form", true},
		{"SPDX identifier", "// SPDX-License-Identifier: Apache-2.0", true},

		// The traps. Every one of these is prose.
		{"prose that names go:embed", "// //go:embed assets directive in init_skill_assets.go", false},
		{"prose that names go:build", "// The scenario tier is selected with //go:build scenario.", false},
		{"prose that names nolint", "// Delete the //nolint here once the upstream fix lands.", false},
		{"prose that names a build tag mid-line", "// Files carry a // +build line only for Go 1.16.", false},

		{"sentence with a colon", "// Note: this is prose.", false},
		{"uppercase word colon", "// Tags: mechanism", false},
		{"word colon then a space", "//todo: something", false},
		{"plain prose", "// A comment.", false},
		{"block comment", "/* not a directive */", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isDirective(tc.raw); got != tc.want {
				t.Fatalf("isDirective(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

// attachSrc carries one instance of every attachment kind, each tagged with a
// marker word the test looks for, so a change in classification names itself.
const attachSrc = `// Package p is the package doc.
//
// It has a second paragraph.
package p

import (
	"fmt" // spec-trailing here
)

// floatingAtFileLevel is not attached to anything.

// Exported is an exported function doc.
func Exported() {}

// unexported is an unexported function doc.
func unexported() {
	// funcBodyFloating explains the next statement.
	fmt.Println("x")
	x := 1 // trailingOther
	_ = x
	_ = []int{
		1,
		// complitFloating
		2,
	}
}

// S is an exported type doc.
type S struct {
	// FieldDoc documents the field.
	Field int
	Other int // fieldTrailing

	// structFloating sits between fields with a blank line above.

	Third int
}

// I is an exported interface.
type I interface {
	// Method documents the interface member.
	Method()

	// ifaceFloating sits between members.

	Other()
}

// paramDoc lives on a parameter.
func withParams(
	// paramFieldDoc documents the parameter.
	a int,
	// paramListFloating floats in the list.

	b int,
) {
	_, _ = a, b
}

const (
	// A is exported and documented.
	A = 1

	// genBlockFloating floats in the grouped declaration.

	b = 2
)
`

func TestClassifyAssignsOneAttachmentPerMarker(t *testing.T) {
	t.Parallel()
	fb, err := classifyFile("attach.go", []byte(attachSrc))
	if err != nil {
		t.Fatalf("classifyFile: %v", err)
	}
	byMarker := map[string]Attach{}
	for _, b := range fb.Blocks {
		byMarker[firstWord(b.Text)] = b.Attach
	}
	want := map[string]Attach{
		"Package":             AttachPkgDoc,
		"spec-trailing":       AttachSpecTrail,
		"floatingAtFileLevel": AttachFileFloating,
		"Exported":            AttachDocExported,
		"unexported":          AttachDocUnexported,
		"funcBodyFloating":    AttachFuncBodyFloating,
		"trailingOther":       AttachTrailingOther,
		"complitFloating":     AttachComplitFloating,
		"S":                   AttachDocExported,
		"FieldDoc":            AttachStructFieldDoc,
		"fieldTrailing":       AttachStructFieldTrail,
		"structFloating":      AttachStructFloating,
		"I":                   AttachDocExported,
		"Method":              AttachIfaceMemberDoc,
		"ifaceFloating":       AttachIfaceFloating,
		"paramDoc":            AttachDocUnexported,
		// go/parser never fills Field.Doc in a signature, so a comment above a
		// parameter is floating, not a doc comment.
		"paramFieldDoc":     AttachParamListFloating,
		"paramListFloating": AttachParamListFloating,
		"A":                 AttachDocExported,
		"genBlockFloating":  AttachGenBlockFloating,
	}
	for marker, wantAttach := range want {
		got, ok := byMarker[marker]
		if !ok {
			t.Errorf("no comment block found for marker %q", marker)
			continue
		}
		if got != wantAttach {
			t.Errorf("marker %q: attach = %s, want %s", marker, got, wantAttach)
		}
	}
}

func firstWord(text string) string {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "//")
	t = strings.TrimPrefix(t, "/*")
	fields := strings.Fields(t)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// TestPolicyPreservesEveryProtectedClass is the preservation spec restated as
// assertions. Each case is a whole file so the classifier sees real context.
func TestPolicyPreservesEveryProtectedClass(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		src        string
		marker     string
		mode       string
		wantCut    bool
		wantReason string
	}{
		{
			name:       "package doc survives",
			src:        "// Package p does a thing.\npackage p\n",
			marker:     "Package",
			mode:       "max",
			wantReason: "class-not-in-mode:pkg-doc",
		},
		{
			name:       "exported symbol doc survives",
			src:        "package p\n\n// Exported does a thing.\nfunc Exported() {}\n",
			marker:     "Exported",
			mode:       "max",
			wantReason: "class-not-in-mode:doc-exported",
		},
		{
			name:       "struct field doc survives",
			src:        "package p\n\ntype S struct {\n\t// EV-003 says the field MUST NOT be compared across processes.\n\tF int\n}\n",
			marker:     "EV-003",
			mode:       "max",
			wantReason: "class-not-in-mode:struct-field-doc",
		},
		{
			name:       "trailing comment in an alignment group survives",
			src:        "package p\n\nconst (\n\tA    = 1 // trailingOne\n\tLong = 2 // trailingTwo\n)\n",
			marker:     "trailingOne",
			mode:       "max",
			wantReason: "trailing-comment",
		},
		{
			name:       "cloexec waiver survives even as a trailing comment",
			src:        "package p\n\nfunc f() {\n\tg() //cloexec:waived the fd is handed to a child\n}\n\nfunc g() {}\n",
			marker:     "the",
			mode:       "max",
			wantReason: "directive-in-group",
		},
		{
			name:       "a group carrying Tags: mechanism survives",
			src:        "package p\n\nfunc f() {\n\t// A note.\n\t//\n\t// Tags: mechanism\n\tg()\n}\n\nfunc g() {}\n",
			marker:     "A",
			mode:       "max",
			wantReason: "marker:tags-mechanism",
		},
		{
			// The marker is CP-051's own regexp. A bare "Tags:" that no test
			// reads used to pin 4,292 lines, 47 blocks of them banner essays.
			name:    "a floating block with a bare Tags: line is cut",
			src:     "package p\n\nfunc f() {\n\t// A note.\n\t//\n\t// Tags: queue, dispatch\n\tg()\n}\n\nfunc g() {}\n",
			marker:  "A",
			mode:    "max",
			wantCut: true,
		},
		{
			name:       "a nosec group survives",
			src:        "package p\n\nfunc f() {\n\t// #nosec G304 -- the path is validated above\n\tg()\n}\n\nfunc g() {}\n",
			marker:     "#nosec",
			mode:       "max",
			wantReason: "directive-in-group",
		},
		{
			name:       "a go:build group survives whole",
			src:        "//go:build scenario\n\npackage p\n",
			marker:     "scenario",
			mode:       "max",
			wantReason: "directive-in-group",
		},
		{
			name:       "prose above a directive in the same group survives too",
			src:        "package p\n\nfunc f() {\n\t// This explains why.\n\t//nolint:gosec // the path is validated above\n\tg()\n}\n\nfunc g() {}\n",
			marker:     "This",
			mode:       "max",
			wantReason: "directive-in-group",
		},
		{
			name:    "a function-body floating block is cut in safe mode",
			src:     "package p\n\nfunc f() {\n\t// plainProse explains the next line.\n\tg()\n}\n\nfunc g() {}\n",
			marker:  "plainProse",
			mode:    "safe",
			wantCut: true,
		},
		{
			name:       "a file-level floating block is not cut in safe mode",
			src:        "package p\n\n// fileProse floats.\n\nfunc f() {}\n",
			marker:     "fileProse",
			mode:       "safe",
			wantReason: "class-not-in-mode:file-floating",
		},
		{
			name:    "a file-level floating block is cut in aggressive mode",
			src:     "package p\n\n// fileProse floats.\n\nfunc f() {}\n",
			marker:  "fileProse",
			mode:    "aggressive",
			wantCut: true,
		},
		{
			name:       "an unexported doc is not cut in aggressive mode",
			src:        "package p\n\n// unexported does a thing.\nfunc unexported() {}\n",
			marker:     "unexported",
			mode:       "aggressive",
			wantReason: "class-not-in-mode:doc-unexported",
		},
		{
			name:    "an unexported doc is cut in max mode",
			src:     "package p\n\n// unexported does a thing.\nfunc unexported() {}\n",
			marker:  "unexported",
			mode:    "max",
			wantCut: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := blockForMarker(t, tc.src, tc.marker, tc.mode)
			if b.Cuttable != tc.wantCut {
				t.Fatalf("cuttable = %v, want %v (reasons %v)", b.Cuttable, tc.wantCut, b.Reasons)
			}
			if tc.wantReason != "" && !hasReason(b, tc.wantReason) {
				t.Fatalf("reasons = %v, want one of them to be %q", b.Reasons, tc.wantReason)
			}
		})
	}
}

func blockForMarker(t *testing.T, src, marker, mode string) *Block {
	t.Helper()
	fb, err := classifyFile("x.go", []byte(src))
	if err != nil {
		t.Fatalf("classifyFile: %v\n%s", err, src)
	}
	m, err := lookupMode(mode)
	if err != nil {
		t.Fatalf("lookupMode: %v", err)
	}
	decide(fb, SelectionFor(m))
	for _, b := range fb.Blocks {
		if strings.Contains(b.Text, marker) {
			return b
		}
	}
	t.Fatalf("no comment block containing %q in:\n%s", marker, src)
	return nil
}

func hasReason(b *Block, want string) bool {
	for _, r := range b.Reasons {
		if r == want {
			return true
		}
	}
	return false
}
