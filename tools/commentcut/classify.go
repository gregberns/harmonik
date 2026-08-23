package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// Attach names where a comment group sits in the syntax tree. The set is
// closed: classifyFile assigns exactly one of these to every group.
type Attach string

const (
	AttachPkgDoc            Attach = "pkg-doc"
	AttachFileHeader        Attach = "file-header"
	AttachDocExported       Attach = "doc-exported"
	AttachDocUnexported     Attach = "doc-unexported"
	AttachStructFieldDoc    Attach = "struct-field-doc"
	AttachStructFieldTrail  Attach = "struct-field-trailing"
	AttachIfaceMemberDoc    Attach = "iface-member-doc"
	AttachIfaceMemberTrail  Attach = "iface-member-trailing"
	AttachSpecTrail         Attach = "spec-trailing"
	AttachFileFloating      Attach = "file-floating"
	AttachFuncBodyFloating  Attach = "func-body-floating"
	AttachComplitFloating   Attach = "complit-floating"
	AttachStructFloating    Attach = "structtype-floating"
	AttachIfaceFloating     Attach = "iface-floating"
	AttachGenBlockFloating  Attach = "genblock-floating"
	AttachParamListFloating Attach = "paramlist-floating"
	AttachTrailingOther     Attach = "trailing-other"
)

// Container names the aligned syntactic container a group sits inside, or
// ContainerNone. The formatter hazard rule keys off this.
type Container string

const (
	ContainerNone     Container = ""
	ContainerComplit  Container = "composite-literal"
	ContainerStruct   Container = "struct-type"
	ContainerIface    Container = "interface-type"
	ContainerGenBlock Container = "grouped-decl"
)

// Block is one comment group plus everything a caller needs to decide whether
// to delete it and how.
type Block struct {
	File      string   `json:"file"`
	StartLine int      `json:"start_line"`
	EndLine   int      `json:"end_line"`
	Lines     int      `json:"lines"`
	Attach    Attach   `json:"attach"`
	Class     string   `json:"class"`
	Cuttable  bool     `json:"cuttable"`
	Reasons   []string `json:"reasons"`
	Trailing  bool     `json:"trailing"`
	Text      string   `json:"text,omitempty"`

	// startOff and endOff bound the physical lines the group occupies:
	// startOff is the first byte of the group's first line, endOff the first
	// byte after the group's last line. Deleting [startOff,endOff) removes
	// whole lines and leaves no blank behind. Both are zero for a trailing
	// group, which is never deleted.
	startOff int
	endOff   int
	// commentOff and commentEnd bound the comment text itself.
	commentOff int
	commentEnd int

	container      Container
	containerEmpty bool
	blankBefore    bool
	blankAfter     bool
	// splitsAlignmentRun is true when an entry of the enclosing container ends
	// on an earlier line and another entry begins on a later line, so the group
	// sits between two alignment runs.
	splitsAlignmentRun bool
	// insideOneEntry is true when the group sits strictly within the line span
	// of a single entry of the enclosing container.
	insideOneEntry bool
	group          *ast.CommentGroup
}

// FileBlocks is the classification of one file.
type FileBlocks struct {
	Path   string
	Src    []byte
	Blocks []*Block
}

// directiveWordColon is Go's own directive shape: "//" then a lowercase word,
// then a colon, then a non-space. It is matched against the RAW comment text
// with the slashes still attached, on purpose. cmd/harmonik/asset_manifest.go
// carries the prose line "// //go:embed assets directive in
// init_skill_assets.go"; untrimmed it correctly fails, trimmed it would falsely
// match and the tool would preserve prose while a real directive elsewhere
// looked the same.
var directiveWordColon = regexp.MustCompile(`^//[a-z0-9]+:[^ \t]`)

// spacedDirectives are directive forms that a word-colon test cannot see,
// because a space, a plus or a capital letter sits where the shape expects a
// lowercase word and a colon. Each one changes what the compiler or a linter
// does, so each one is preserved with its whole group.
//
//   - "#nosec" — every one of the 67 sites here is "// #nosec G304 -- reason".
//     Deleting them took gosec from 192 findings to 256 and broke the lint gate.
//   - "+build" — the legacy build constraint. Go still honours a lone one, and
//     no check in this tool can see a build-constraint change: the token stream
//     and the code bytes are both per-file and both identical after the cut,
//     while the file silently joins or leaves the build. There are no live
//     instances in this tree; the rule is here because the failure is silent.
//   - "sys" / "sysnb" — mksyscall prototypes, tab-separated rather than
//     colon-separated.
//   - "nolint" with a leading space, which nolintlint still reads.
//   - "SPDX-…:" — a licence identifier tools read, and capitalised, so the
//     lowercase word-colon shape misses it.
var spacedDirectives = regexp.MustCompile(
	`^//[ \t]*(#nosec\b|\+build\b|sys(nb)?\b|nolint\b|(?i:SPDX-[A-Za-z-]+:))`)

// isDirective reports whether one comment is a compiler or linter directive.
// The whole group containing one is preserved, not just this line: a //nolint
// explanation and a //go:embed target both live on neighbouring lines, and
// preserving the group removes a family of edge cases for the price of 236
// prose lines tree-wide.
//
// The test is against the RAW comment with its slashes attached and is a
// PREFIX test, never a substring one. cmd/harmonik/asset_manifest.go carries
// the prose line "// //go:embed assets directive in init_skill_assets.go";
// a substring test would preserve prose everywhere that phrase appears.
func isDirective(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "//line "),
		strings.HasPrefix(raw, "//extern "),
		strings.HasPrefix(raw, "//export "),
		strings.HasPrefix(raw, "//nolint"):
		return true
	}
	return directiveWordColon.MatchString(raw) || spacedDirectives.MatchString(raw)
}

// tagsMechanism is the regexp from CP-051's own sensor test, copied verbatim.
var tagsMechanism = regexp.MustCompile(`\bTags:.*\bmechanism\b`)

// markerPatterns are text a test or a human reads out of a comment. A group
// carrying one is preserved whole.
var markerPatterns = []struct {
	name  string
	match func(string) bool
}{
	// internal/specaudit/cloexec_parity_test.go walks every comment group under
	// the four roots with strings.Contains, so a waiver may be a trailing
	// comment and may carry leading text.
	{"cloexec-waiver", func(s string) bool { return strings.Contains(s, "//cloexec:waived") }},
	// internal/handlercontract/cp051_skill_mechanism_tagged_test.go requires a
	// line matching \bTags:.*\bmechanism\b in the doc comments of two EXPORTED
	// types, and it reads them through GenDecl.Doc. Both are therefore already
	// protected as doc-exported in every mode. A bare Contains(s, "Tags:") test
	// went far wider than the assertion: it pinned 4,292 lines, 47 blocks of
	// which were floating banner essays that no mode would otherwise keep and
	// no test reads. The marker is now the assertion's own shape, so it covers
	// the tagged docs and nothing else.
	{"tags-mechanism", tagsMechanism.MatchString},
	{"deprecated", func(s string) bool { return strings.Contains(s, "Deprecated:") }},
	{"code-generated", func(s string) bool { return strings.Contains(s, "Code generated") }},
	{"todo", func(s string) bool {
		return strings.Contains(s, "TODO") || strings.Contains(s, "FIXME") ||
			strings.Contains(s, "XXX") || strings.Contains(s, "HACK")
	}},
}

func markerReasons(text string) []string {
	var out []string
	for _, m := range markerPatterns {
		if m.match(text) {
			out = append(out, "marker:"+m.name)
		}
	}
	return out
}

// classifyFile parses src and returns one Block per comment group. It never
// decides whether to cut; policy lives in decide.
func classifyFile(path string, src []byte) (*FileBlocks, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	tf := fset.File(f.Package)
	lines := lineIndex(src)

	docOf := docAttachments(f)
	chains := enclosingChains(f, f.Comments)

	fb := &FileBlocks{Path: path, Src: src}
	for i, g := range f.Comments {
		b := &Block{
			File:       path,
			StartLine:  tf.Position(g.Pos()).Line,
			EndLine:    tf.Position(g.End()).Line,
			Text:       rawText(src, tf.Offset(g.Pos()), tf.Offset(g.End())),
			commentOff: tf.Offset(g.Pos()),
			commentEnd: tf.Offset(g.End()),
			group:      g,
		}
		b.Lines = b.EndLine - b.StartLine + 1
		b.Trailing = hasCodeBefore(src, lines, b.StartLine, b.commentOff)

		c := containerFacts(chains[i], tf, b.StartLine, b.EndLine)
		b.container, b.containerEmpty = c.kind, c.empty
		b.splitsAlignmentRun, b.insideOneEntry = c.splits, c.insideEntry
		b.blankBefore = isBlankLine(src, lines, b.StartLine-1)
		b.blankAfter = isBlankLine(src, lines, b.EndLine+1)

		if !b.Trailing && !hasCodeAfter(src, lines, b.EndLine, b.commentEnd) {
			b.startOff = lines[b.StartLine-1]
			b.endOff = lineEndOffset(src, lines, b.EndLine)
		}

		b.Attach = attachOf(b, docOf[g], chains[i], f, tf)
		fb.Blocks = append(fb.Blocks, b)
	}
	return fb, nil
}

// docKind records that a group is the Doc or Comment field of a declaration.
type docKind struct {
	kind     Attach
	exported bool
	known    bool
}

func docAttachments(f *ast.File) map[*ast.CommentGroup]docKind {
	out := map[*ast.CommentGroup]docKind{}
	set := func(g *ast.CommentGroup, k Attach, exported bool) {
		if g != nil {
			out[g] = docKind{kind: k, exported: exported, known: true}
		}
	}
	if f.Doc != nil {
		set(f.Doc, AttachPkgDoc, true)
	}
	declDoc := func(g *ast.CommentGroup, exported bool) {
		if exported {
			set(g, AttachDocExported, true)
		} else {
			set(g, AttachDocUnexported, false)
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			declDoc(d.Doc, isExportedFunc(d))
		case *ast.GenDecl:
			declDoc(d.Doc, genDeclExported(d))
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					declDoc(s.Doc, s.Name.IsExported())
					set(s.Comment, AttachSpecTrail, false)
				case *ast.ValueSpec:
					declDoc(s.Doc, anyExported(s.Names))
					set(s.Comment, AttachSpecTrail, false)
				case *ast.ImportSpec:
					declDoc(s.Doc, false)
					set(s.Comment, AttachSpecTrail, false)
				}
			}
		}
	}
	// Field lists carry their own doc and trailing comments. Which kind they
	// are depends on the enclosing type, so walk for it. Parameter and result
	// lists are absent on purpose: go/parser never fills Doc or Comment on a
	// parameter Field, so a comment in a signature is always floating.
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructType:
			fieldList(out, n.Fields, AttachStructFieldDoc, AttachStructFieldTrail)
		case *ast.InterfaceType:
			fieldList(out, n.Methods, AttachIfaceMemberDoc, AttachIfaceMemberTrail)
		}
		return true
	})
	return out
}

func fieldList(out map[*ast.CommentGroup]docKind, fl *ast.FieldList, doc, trail Attach) {
	if fl == nil {
		return
	}
	for _, fld := range fl.List {
		if fld.Doc != nil {
			if _, seen := out[fld.Doc]; !seen {
				out[fld.Doc] = docKind{kind: doc, known: true}
			}
		}
		if fld.Comment != nil {
			if _, seen := out[fld.Comment]; !seen {
				out[fld.Comment] = docKind{kind: trail, known: true}
			}
		}
	}
}

func isExportedFunc(d *ast.FuncDecl) bool { return d.Name.IsExported() }

func genDeclExported(d *ast.GenDecl) bool {
	for _, s := range d.Specs {
		switch s := s.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() {
				return true
			}
		case *ast.ValueSpec:
			if anyExported(s.Names) {
				return true
			}
		}
	}
	return false
}

func anyExported(names []*ast.Ident) bool {
	for _, n := range names {
		if n.IsExported() {
			return true
		}
	}
	return false
}

func attachOf(b *Block, dk docKind, chain []ast.Node, f *ast.File, tf *token.File) Attach {
	if dk.known {
		return dk.kind
	}
	if b.commentOff < tf.Offset(f.Package) {
		return AttachFileHeader
	}
	if b.Trailing {
		return AttachTrailingOther
	}
	for _, n := range chain { // innermost first
		switch n := n.(type) {
		case *ast.CompositeLit:
			return AttachComplitFloating
		case *ast.StructType:
			return AttachStructFloating
		case *ast.InterfaceType:
			return AttachIfaceFloating
		case *ast.FuncType:
			return AttachParamListFloating
		case *ast.BlockStmt:
			return AttachFuncBodyFloating
		case *ast.GenDecl:
			if n.Lparen.IsValid() {
				return AttachGenBlockFloating
			}
		}
	}
	return AttachFileFloating
}

// containerInfo is what the hazard rule needs to know about the aligned
// container around one comment group.
type containerInfo struct {
	kind        Container
	empty       bool
	splits      bool
	insideEntry bool
}

// containerFacts finds the innermost aligned container around a group, walking
// the enclosing chain from the inside out, and measures the positional facts
// the hazard rule needs. A FuncType parameter list is not column-aligned by
// gofmt and is deliberately not a container here.
func containerFacts(chain []ast.Node, tf *token.File, startLine, endLine int) containerInfo {
	for _, n := range chain {
		switch n := n.(type) {
		case *ast.CompositeLit:
			return entryFacts(ContainerComplit, exprNodes(n.Elts), tf, startLine, endLine)
		case *ast.StructType:
			return entryFacts(ContainerStruct, fieldNodes(n.Fields), tf, startLine, endLine)
		case *ast.InterfaceType:
			return entryFacts(ContainerIface, fieldNodes(n.Methods), tf, startLine, endLine)
		case *ast.FuncType:
			return containerInfo{kind: ContainerNone}
		case *ast.BlockStmt:
			return containerInfo{kind: ContainerNone}
		case *ast.GenDecl:
			if n.Lparen.IsValid() {
				return entryFacts(ContainerGenBlock, specNodes(n.Specs), tf, startLine, endLine)
			}
		}
	}
	return containerInfo{kind: ContainerNone}
}

func entryFacts(kind Container, entries []ast.Node, tf *token.File, startLine, endLine int) containerInfo {
	f := containerInfo{kind: kind, empty: len(entries) == 0}
	var before, after bool
	for _, e := range entries {
		lo := tf.Position(e.Pos()).Line
		hi := tf.Position(e.End()).Line
		if hi < startLine {
			before = true
		}
		if lo > endLine {
			after = true
		}
		if lo <= startLine && endLine <= hi {
			f.insideEntry = true
		}
	}
	f.splits = before && after
	return f
}

func exprNodes(xs []ast.Expr) []ast.Node {
	out := make([]ast.Node, 0, len(xs))
	for _, x := range xs {
		out = append(out, x)
	}
	return out
}

func fieldNodes(fl *ast.FieldList) []ast.Node {
	if fl == nil {
		return nil
	}
	out := make([]ast.Node, 0, len(fl.List))
	for _, f := range fl.List {
		out = append(out, f)
	}
	return out
}

func specNodes(ss []ast.Spec) []ast.Node {
	out := make([]ast.Node, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// enclosingChains returns, for each group, the enclosing AST nodes ordered
// innermost first. One walk serves every group in the file.
func enclosingChains(f *ast.File, groups []*ast.CommentGroup) [][]ast.Node {
	out := make([][]ast.Node, len(groups))
	if len(groups) == 0 {
		return out
	}
	starts := make([]token.Pos, len(groups))
	for i, g := range groups {
		starts[i] = g.Pos()
	}
	v := &chainWalker{groups: groups, starts: starts, out: out}
	ast.Walk(v, f)
	for i := range out {
		reverse(out[i])
	}
	return out
}

type chainWalker struct {
	groups []*ast.CommentGroup
	starts []token.Pos
	stack  []ast.Node
	out    [][]ast.Node
}

func (w *chainWalker) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		w.stack = w.stack[:len(w.stack)-1]
		return nil
	}
	w.stack = append(w.stack, n)
	lo := sort.Search(len(w.starts), func(i int) bool { return w.starts[i] >= n.Pos() })
	for i := lo; i < len(w.groups) && w.starts[i] < n.End(); i++ {
		if w.groups[i].End() <= n.End() {
			w.out[i] = append(w.out[i][:0], w.stack...)
			w.out[i] = append([]ast.Node(nil), w.out[i]...)
		}
	}
	return w
}

func reverse(ns []ast.Node) {
	for i, j := 0, len(ns)-1; i < j; i, j = i+1, j-1 {
		ns[i], ns[j] = ns[j], ns[i]
	}
}

// lineIndex returns the byte offset at which each 1-based line starts.
func lineIndex(src []byte) []int {
	idx := []int{0}
	for i, c := range src {
		if c == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

func lineText(src []byte, lines []int, line int) string {
	if line < 1 || line > len(lines) {
		return ""
	}
	start := lines[line-1]
	end := len(src)
	if line < len(lines) {
		end = lines[line] - 1
	}
	if end > len(src) {
		end = len(src)
	}
	return string(src[start:end])
}

func lineEndOffset(src []byte, lines []int, line int) int {
	if line < len(lines) {
		return lines[line]
	}
	return len(src)
}

func isBlankLine(src []byte, lines []int, line int) bool {
	if line < 1 || line > len(lines) {
		return false
	}
	return strings.TrimSpace(lineText(src, lines, line)) == ""
}

func hasCodeBefore(src []byte, lines []int, line, off int) bool {
	return strings.TrimSpace(string(src[lines[line-1]:off])) != ""
}

func hasCodeAfter(src []byte, lines []int, line, off int) bool {
	end := lineEndOffset(src, lines, line)
	if end > len(src) {
		end = len(src)
	}
	if off > end {
		return false
	}
	return strings.TrimSpace(string(src[off:end])) != ""
}

func rawText(src []byte, from, to int) string {
	if from < 0 || to > len(src) || from > to {
		return ""
	}
	return string(src[from:to])
}
