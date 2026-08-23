package main

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"
)

// Mode is a named set of attachment kinds the tool is allowed to delete.
type Mode struct {
	Name    string
	Summary string
	Classes []Attach
}

// modes are ordered from least to most volume. safe is the default.
//
// Nothing in any mode touches a package doc, a doc comment on an exported
// symbol, a struct field doc or trailing comment, an interface member doc, a
// directive group, or a marker group. Those are preserved by policy, not by
// accident, and there is no flag that releases them.
var modes = []Mode{
	{
		Name: "safe",
		Summary: "floating comment blocks inside function bodies only, " +
			"subject to the formatter-hazard rule",
		Classes: []Attach{AttachFuncBodyFloating},
	},
	{
		Name: "aggressive",
		Summary: "every floating (unattached) comment block: function bodies, " +
			"file level, pre-package header, composite literals, struct type " +
			"bodies, interface bodies, grouped const/var/type declarations and " +
			"parameter lists. All doc comments survive.",
		Classes: []Attach{
			AttachFuncBodyFloating,
			AttachFileFloating,
			AttachFileHeader,
			AttachComplitFloating,
			AttachStructFloating,
			AttachIfaceFloating,
			AttachGenBlockFloating,
			AttachParamListFloating,
		},
	},
	{
		Name: "max",
		Summary: "aggressive plus doc comments on UNEXPORTED symbols. " +
			"Exported-symbol docs, package docs and struct field docs still " +
			"survive.",
		Classes: []Attach{
			AttachFuncBodyFloating,
			AttachFileFloating,
			AttachFileHeader,
			AttachComplitFloating,
			AttachStructFloating,
			AttachIfaceFloating,
			AttachGenBlockFloating,
			AttachParamListFloating,
			AttachDocUnexported,
		},
	},
}

func lookupMode(name string) (Mode, error) {
	for _, m := range modes {
		if m.Name == name {
			return m, nil
		}
	}
	names := make([]string, 0, len(modes))
	for _, m := range modes {
		names = append(names, m.Name)
	}
	return Mode{}, fmt.Errorf("unknown mode %q (have %s)", name, strings.Join(names, ", "))
}

func (m Mode) Describe() string {
	cs := make([]string, 0, len(m.Classes))
	for _, c := range m.Classes {
		cs = append(cs, string(c))
	}
	sort.Strings(cs)
	return fmt.Sprintf("mode %s: %s\n  classes cut: %s", m.Name, m.Summary, strings.Join(cs, ", "))
}

// Selection is the policy applied to one pass: which attachment kinds are in
// play, and whether the formatter-hazard rule gates them.
//
// Deletion needs the hazard rule. Truncation does not: it keeps at least the
// first comment line, so the group still separates the alignment runs on either
// side and still occupies the container it collapses when empty.
type Selection struct {
	Allowed map[Attach]bool
	Hazard  bool
}

// SelectionFor builds the deletion policy for a mode.
func SelectionFor(m Mode) Selection {
	allowed := map[Attach]bool{}
	for _, c := range m.Classes {
		allowed[c] = true
	}
	return Selection{Allowed: allowed, Hazard: true}
}

// decide fills Class, Cuttable and Reasons for every block in the file. A block
// is cuttable only when its attachment is selected, it carries no directive and
// no marker, it is a standalone line range, and the hazard rule clears it.
func decide(fb *FileBlocks, sel Selection) {
	allowed := sel.Allowed
	for _, b := range fb.Blocks {
		b.Reasons = nil
		if !allowed[b.Attach] {
			b.Reasons = append(b.Reasons, "class-not-in-mode:"+string(b.Attach))
		}
		if r := directiveReasons(b.group); len(r) > 0 {
			b.Reasons = append(b.Reasons, r...)
		}
		b.Reasons = append(b.Reasons, markerReasons(b.Text)...)
		if b.Trailing {
			b.Reasons = append(b.Reasons, "trailing-comment")
		} else if b.endOff == 0 {
			b.Reasons = append(b.Reasons, "code-follows-on-last-line")
		}
		if sel.Hazard {
			b.Reasons = append(b.Reasons, hazardReasons(b)...)
		}

		b.Cuttable = len(b.Reasons) == 0
		b.Class = "cut"
		if !b.Cuttable {
			b.Class = "preserve"
			for _, r := range b.Reasons {
				if strings.HasPrefix(r, "hazard:") {
					b.Class = "hazard"
					break
				}
			}
		}
	}
}

func directiveReasons(g *ast.CommentGroup) []string {
	for _, c := range g.List {
		if isDirective(c.Text) {
			return []string{"directive-in-group"}
		}
	}
	return nil
}

// hazardReasons applies the formatter-avoidance rule. Every clause exists
// because deleting the comment makes gofumpt rewrite code bytes with an
// unchanged token stream.
//
// The clauses, in the order they are tested:
//
//  1. Alignment-group merge. A standalone comment inside a multi-line aligned
//     container separates two tabwriter alignment runs. Delete it, the runs
//     merge, and the merged run re-pads to the widest key across both. A blank
//     line on either side is already a separator and survives the deletion, so
//     the clause does not fire there.
//  2. One-line-eligible join. A comment inside a single element of such a
//     container is the only thing forcing the wrap. Delete it and go/printer
//     collapses the literal onto one line.
//  3. Empty-body collapse. A comment that is the sole content of a struct,
//     interface or composite literal body makes gofumpt collapse the braces —
//     and inside a parenthesised var/const it deletes the whole declaration.
//  4. Trailing comment in an aligned container. Stripping an end-of-line
//     comment removes that line's last tabwriter cell, which re-pads its
//     neighbours. Deliberately blunt: no mode cuts trailing comments anyway.
//
// Interface method lists are not column-aligned, so only clause 3 applies to
// them.
func hazardReasons(b *Block) []string {
	if b.container == ContainerNone {
		return nil
	}
	if b.containerEmpty {
		return []string{"hazard:sole-content-collapse"}
	}
	if b.container == ContainerIface {
		return nil
	}
	if b.Trailing {
		return []string{"hazard:trailing-in-aligned-container"}
	}
	var out []string
	if b.splitsAlignmentRun && !b.blankBefore && !b.blankAfter {
		out = append(out, "hazard:alignment-group-merge")
	}
	if b.insideOneEntry {
		out = append(out, "hazard:one-line-join")
	}
	return out
}
