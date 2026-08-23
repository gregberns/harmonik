package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The tests in this file are the defects an adversarial pass found in the
// first version of this tool. Each one is a way the tool could damage
// something outside what it was asked to change, and each was silent.

// TestScopeRefusesARootOutsideTheRepository is the worst of them. Exclusion
// was by directory NAME, never by containment, so "-root .." walked every
// sibling checkout and rewrote files in all of them.
func TestScopeRefusesARootOutsideTheRepository(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	victim := filepath.Join(tmp, "victim")
	for _, d := range []string{filepath.Join(repo, "internal"), victim} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(repo, "internal", "a.go"), "package p\n")
	write(t, filepath.Join(victim, "victim.go"), "package v\n\n// MUST NOT BE TOUCHED\n")

	for _, root := range []string{"..", "../victim", "../../"} {
		_, err := findFiles(repo, []string{root}, nil, nil)
		if err == nil {
			t.Fatalf("-root %q was accepted", root)
		}
		if !strings.Contains(err.Error(), "outside the repository") {
			t.Fatalf("-root %q: error = %v, want it to name the escape", root, err)
		}
	}
	if _, err := findFiles(repo, []string{filepath.Join(tmp, "victim")}, nil, nil); err == nil {
		t.Fatal("an absolute root was accepted")
	}
}

// TestScopeRefusesASymlinkedFileOutsideTheRepository closes the other route
// out: the walk stays inside the tree, but the file it finds does not.
func TestScopeRefusesASymlinkedFileOutsideTheRepository(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on windows")
	}
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "internal"), 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(tmp, "outside.go")
	write(t, outside, "package v\n\n// MUST NOT BE TOUCHED\n")
	if err := os.Symlink(outside, filepath.Join(repo, "internal", "link.go")); err != nil {
		t.Fatal(err)
	}
	_, err := findFiles(repo, []string{"internal"}, nil, nil)
	if err == nil {
		t.Fatal("a symlink out of the repository was accepted")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want it to name the symlink", err)
	}
}

// TestApplyRefusesADirtyScope is the abort story. The only copy of the
// originals a run holds is process memory, so the undo has to be git's. That
// is only true when git already holds an identical copy of every file in
// scope — which is also what stops the tool cutting its own untracked source.
func TestApplyRefusesADirtyScope(t *testing.T) {
	useRepoFormatters(t)
	repo := tempModule(t, map[string]string{
		"internal/p/a.go": "package p\n\nfunc f() {\n\t// prose\n\tg()\n}\n\nfunc g() {}\n",
	})
	write(t, filepath.Join(repo, "internal", "p", "b.go"), "package p\n\nfunc h() {\n\t// prose\n\tg()\n}\n")
	before := readFile(t, filepath.Join(repo, "internal", "p", "a.go"))

	out, code := runTool(repo, "apply", "-mode", "max", "-root", "internal", "-test=false")
	if code == 0 {
		t.Fatalf("apply ran against a dirty scope\n%s", out)
	}
	mustContain(t, out, []string{"untracked or already modified", "internal/p/b.go", "-allow-dirty"})
	if readFile(t, filepath.Join(repo, "internal", "p", "a.go")) != before {
		t.Fatal("a.go was rewritten before the precondition fired")
	}
	// -allow-dirty is the escape hatch, and it works.
	if out, code := runTool(repo, "apply", "-mode", "max", "-root", "internal",
		"-test=false", "-allow-dirty"); code != 0 {
		t.Fatalf("apply -allow-dirty exited %d\n%s", code, out)
	}
	if strings.Contains(readFile(t, filepath.Join(repo, "internal", "p", "a.go")), "prose") {
		t.Fatal("-allow-dirty did not cut anything")
	}
}

// TestApplyRestoresWhenAWriteFails: a mid-run I/O error used to return without
// restoring, leaving a half-cut tree that was never formatted and never
// checked.
func TestApplyRestoresWhenAWriteFails(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}
	body := "package p\n\nfunc f%s() {\n\t// prose that max mode cuts\n\tg%s()\n}\n\nfunc g%s() {}\n"
	files := map[string]string{}
	for _, n := range []string{"a", "b", "z"} {
		files["internal/p/"+n+".go"] = strings.ReplaceAll(body, "%s", n)
	}
	repo := tempModule(t, files)
	originals := map[string]string{}
	for rel := range files {
		originals[rel] = readFile(t, filepath.Join(repo, rel))
	}
	if err := os.Chmod(filepath.Join(repo, "internal", "p", "z.go"), 0o400); err != nil {
		t.Fatal(err)
	}

	out, code := runTool(repo, "apply", "-mode", "max", "-root", "internal", "-test=false")
	if code == 0 {
		t.Fatalf("apply succeeded despite an unwritable file\n%s", out)
	}
	mustContain(t, out, []string{"permission denied", "restoring"})
	for rel, want := range originals {
		if got := readFile(t, filepath.Join(repo, rel)); got != want {
			t.Fatalf("%s was left half-cut:\n%s", rel, got)
		}
	}
}

// TestTruncateNeverEmptiesADocComment: "-keep 1" against a block-comment doc
// produced "/* */", which parses, satisfies no reader, and takes the doc
// comment revive requires away without any check noticing.
func TestTruncateNeverEmptiesADocComment(t *testing.T) {
	t.Parallel()
	const src = "package p\n\n/*\nStar does a thing.\n\nline3 *\n*/\nfunc Star() {}\n"
	fb, err := classifyFile("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	decide(fb, Selection{Allowed: map[Attach]bool{AttachDocExported: true}})
	var doc *Block
	for _, b := range fb.Blocks {
		if b.Cuttable {
			doc = b
		}
	}
	if doc == nil {
		t.Fatal("the exported doc was not selected")
	}
	if _, ok, err := truncateSplice([]byte(src), doc, 1); err != nil || ok {
		t.Fatalf("truncateSplice(keep=1) = ok %v, err %v; want it refused", ok, err)
	}
	s, ok, err := truncateSplice([]byte(src), doc, 2)
	if err != nil || !ok {
		t.Fatalf("truncateSplice(keep=2) = ok %v, err %v", ok, err)
	}
	if !strings.Contains(s.Text, "Star does a thing.") {
		t.Fatalf("keep=2 lost the doc text: %q", s.Text)
	}
}

// runTool runs one verb against a scratch repository and returns everything it
// printed on either stream, plus the exit code.
func runTool(repo string, args ...string) (output string, code int) {
	var out bytes.Buffer
	code = run(append([]string{args[0], "-repo", repo}, args[1:]...), &out, &out)
	return out.String(), code
}

func write(t *testing.T, abs, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
