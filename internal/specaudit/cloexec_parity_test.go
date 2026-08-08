// Every descriptor born from a raw syscall must name its close-on-exec flag.
//
// # WHY THIS SENSOR EXISTS, AND WHY THE OBVIOUS TEST DOES NOT WORK
//
// A descriptor opened through the os or net packages is close-on-exec for free:
// Go's runtime ORs O_CLOEXEC into every open it makes (src/os/file_unix.go,
// unconditional). A descriptor opened through a raw syscall gets nothing — the
// flag is set only if the call site names it.
//
// That asymmetry is the whole bug class, and it is invisible at the call site.
// Both spellings look equally careful. Only one of them leaks.
//
// A per-descriptor unit test cannot catch this, and internal/lifecycle
// TestPidfileAcquire_OCloexec is the proof: it asserts FD_CLOEXEC on the pidfile
// descriptor, that descriptor is opened with os.OpenFile, and so the assertion
// is satisfied by the standard library no matter what this codebase does.
// Deleting the explicit syscall.O_CLOEXEC from AcquirePidfile leaves it green
// (mutation-proved 2026-08-07). It guards Go's behaviour, not ours. Any test of
// that shape is unfailable for the same reason, so writing more of them adds
// green, not safety.
//
// The check that would have caught the real defect is this one: read the tree as
// source text and find the raw calls. On 2026-08-07 a raw syscall.Open on a FIFO
// write end in internal/daemon was inherited by a neighbouring test's forked
// child; the pipe's reader never saw EOF and the test hung 58 seconds against a
// one-second bound. That call site was one grep away and nothing was grepping.
//
// SCOPE — this sensor reads _test.go files too, deliberately. The 58-second
// failure was in a test file. A leaked descriptor wedges a suite whichever kind
// of file opened it, and test files get less review, not more.
//
// WHAT THIS SENSOR CANNOT SEE. It matches on syntax, so it finds a call written
// as syscall.Open and misses the same call reached through a local helper or a
// function value. It checks that the flag is NAMED, not that the descriptor ends
// up with the bit set — a call that names the flag and then clears it passes.
// It reads only this module. Those are the known holes; it is a floor, not a
// proof.
package specaudit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// cloexecWaiver marks a raw call site that cannot name a close-on-exec flag.
//
// Some creators have no flag argument to name: syscall.Pipe, Dup, Accept and
// Creat take none, and on darwin SOCK_CLOEXEC does not exist at all, so
// syscall.Socket cannot be fixed by adding an argument. For those the correct
// repair is an fcntl(F_SETFD, FD_CLOEXEC) immediately after creation, which this
// sensor cannot verify from syntax.
//
// Rather than exempt those creators silently — which would leave the sensor
// quiet about the exact sites most likely to leak — each one must carry this
// comment and a reason. The exemption then survives as a decision someone wrote
// down, not as a gap in a matcher.
const cloexecWaiver = "//cloexec:waived"

// cloexecFlagged lists the raw creators that accept a flags argument, with the
// index of that argument and the constant that must appear inside it.
var cloexecFlagged = map[string]struct {
	argIndex int
	want     string
}{
	"Open":    {argIndex: 1, want: "O_CLOEXEC"},
	"Openat":  {argIndex: 2, want: "O_CLOEXEC"},
	"Socket":  {argIndex: 1, want: "SOCK_CLOEXEC"},
	"Accept4": {argIndex: 1, want: "SOCK_CLOEXEC"},
	"Pipe2":   {argIndex: 1, want: "O_CLOEXEC"},
	"Dup3":    {argIndex: 2, want: "O_CLOEXEC"},
}

// cloexecUnflagged lists the raw creators with no flags argument at all. These
// can only ever be waived, never satisfied by naming a constant.
var cloexecUnflagged = map[string]bool{
	"Pipe":   true,
	"Dup":    true,
	"Dup2":   true,
	"Accept": true,
	"Creat":  true,
}

// cloexecRawPackages are the import names whose calls bypass the runtime's
// automatic O_CLOEXEC. os and net are absent on purpose: they set it for free.
var cloexecRawPackages = map[string]bool{
	"syscall": true,
	"unix":    true,
}

type cloexecFinding struct {
	pos    string
	call   string
	reason string
}

func TestRawDescriptorCreatorsNameCloexec(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	fset := token.NewFileSet()

	var findings []cloexecFinding
	scanned := 0

	for _, top := range []string{"internal", "cmd", "tools", "test"} {
		topDir := filepath.Join(root, top)
		if _, err := os.Stat(topDir); err != nil {
			continue
		}
		err := filepath.WalkDir(topDir, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				// testdata/ holds fixtures deliberately outside the build;
				// assets/ holds embedded skill text, not compiled Go.
				if d.Name() == "testdata" || d.Name() == "assets" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
			if perr != nil {
				return perr
			}
			scanned++
			findings = append(findings, cloexecScanFile(fset, f, root)...)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", topDir, err)
		}
	}

	// A sensor that parsed nothing reports clean for the wrong reason. This
	// codebase is thousands of files; a handful means the walk broke.
	if scanned < 100 {
		t.Fatalf("scanned only %d Go files — the walk is measuring nothing, not passing", scanned)
	}

	if len(findings) > 0 {
		sort.Slice(findings, func(i, j int) bool { return findings[i].pos < findings[j].pos })
		var b strings.Builder
		b.WriteString("raw descriptor creators that do not name a close-on-exec flag:\n\n")
		for _, f := range findings {
			b.WriteString("  " + f.pos + ": " + f.call + "\n      " + f.reason + "\n")
		}
		b.WriteString("\nA descriptor from os.* or net.* is close-on-exec for free. A descriptor\n")
		b.WriteString("from a raw syscall is not, and leaks into every forked child until the\n")
		b.WriteString("process exits — which reads as a hang in an unrelated test, not as a leak.\n")
		b.WriteString("Fix by naming the flag, by switching to the os/net equivalent, or — where\n")
		b.WriteString("no flag argument exists — by setting FD_CLOEXEC with fcntl and marking the\n")
		b.WriteString("site " + cloexecWaiver + " with the reason.\n")
		t.Error(b.String())
	}
}

// cloexecScanFile returns one finding per offending call site in f.
func cloexecScanFile(fset *token.FileSet, f *ast.File, root string) []cloexecFinding {
	waived := cloexecWaivedLines(fset, f)

	var out []cloexecFinding
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || !cloexecRawPackages[pkg.Name] {
			return true
		}

		name := sel.Sel.Name
		spec, isFlagged := cloexecFlagged[name]
		if !isFlagged && !cloexecUnflagged[name] {
			return true
		}

		posn := fset.Position(call.Pos())
		if waived[posn.Line] {
			return true
		}

		rel, err := filepath.Rel(root, posn.Filename)
		if err != nil {
			rel = posn.Filename
		}
		pos := filepath.ToSlash(rel) + ":" + strconv.Itoa(posn.Line)
		full := pkg.Name + "." + name

		if !isFlagged {
			out = append(out, cloexecFinding{
				pos:    pos,
				call:   full,
				reason: "takes no flags argument, so it can never set close-on-exec at creation",
			})
			return true
		}
		if spec.argIndex >= len(call.Args) {
			out = append(out, cloexecFinding{
				pos:    pos,
				call:   full,
				reason: "has no argument at index " + strconv.Itoa(spec.argIndex) + " to carry " + spec.want,
			})
			return true
		}
		if !cloexecArgNames(call.Args[spec.argIndex], spec.want) {
			out = append(out, cloexecFinding{
				pos:    pos,
				call:   full,
				reason: "flags argument does not name " + spec.want,
			})
		}
		return true
	})
	return out
}

// cloexecWaivedLines returns the source lines carrying a waiver comment. A
// waiver covers its own line and the line after it, so it can sit trailing on
// the call or on the line above it.
func cloexecWaivedLines(fset *token.FileSet, f *ast.File) map[int]bool {
	waived := map[int]bool{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			if !strings.Contains(c.Text, cloexecWaiver) {
				continue
			}
			line := fset.Position(c.Pos()).Line
			waived[line] = true
			waived[line+1] = true
		}
	}
	return waived
}

// cloexecArgNames reports whether the flags expression mentions want anywhere
// inside it. The argument is normally an OR of constants, so a subtree walk is
// what is needed rather than a match on the top node.
func cloexecArgNames(arg ast.Expr, want string) bool {
	found := false
	ast.Inspect(arg, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if v.Name == want {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == want {
				found = true
			}
		}
		return !found
	})
	return found
}
