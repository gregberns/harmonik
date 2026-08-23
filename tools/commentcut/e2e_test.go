package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tempModule builds a throwaway Go module and returns its root. Files are
// repo-relative paths mapped to contents.
func tempModule(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	files["go.mod"] = "module example.com/tmpmod\n\ngo 1.25\n"
	for rel, body := range files {
		writeFile(t, repo, rel, body)
	}
	gitCommitAll(t, repo)
	return repo
}

// gitCommitAll makes the scratch module a git repository with a clean tree.
// apply and truncate require one: their undo is `git restore`, so they refuse
// to start against files git does not already hold an identical copy of.
func gitCommitAll(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "scratch"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...) // #nosec G204 -- every argument is a literal in the list above
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// useRepoFormatters points the tool at this repository's pinned gofumpt and
// gci. The gate is gofumpt, not gofmt, so a test that formatted with go/format
// would not be testing the thing that decides whether the tree is clean.
func useRepoFormatters(t *testing.T) {
	t.Helper()
	for env, name := range map[string]string{"GOFUMPT": "gofumpt", "GCI": "gci"} {
		abs, err := filepath.Abs(filepath.Join("..", "..", ".tools", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(abs); err != nil {
			if _, err := exec.LookPath(name); err != nil {
				t.Skipf("%s not built; run 'make tools'", name)
			}
			continue
		}
		t.Setenv(env, abs)
	}
}

const e2eSource = `// Package work does a thing.
//
// This paragraph is history that no lint rule reads.
package work

// #nosec G304 -- keep me
var keepNosec = 1

//go:generate stringer -type=Kind
type Kind int

// Exported is documented and must keep its doc.
func Exported() int {
	// This floating block explains the next line and is the safe-mode target.
	// It runs to two lines.
	x := computeIt()
	return x
}

// computeIt is unexported prose that only max mode removes.
func computeIt() int {
	cfg := Config{
		A: 1,
		// This comment splits an alignment run and must survive.
		LongerName: 2,
	}
	return cfg.A + cfg.LongerName
}

// Config holds two fields.
type Config struct {
	// A is the first field.
	A int
	// LongerName is the second.
	LongerName int // trailing note
}
`

const e2eTestSource = `package work

import "testing"

func TestExported(t *testing.T) {
	if Exported() != 3 {
		t.Fatal("wrong")
	}
}
`

func TestApplyCutsCommentsAndKeepsEveryCodeByte(t *testing.T) {
	useRepoFormatters(t)
	repo := tempModule(t, map[string]string{
		"internal/work/work.go":      e2eSource,
		"internal/work/work_test.go": e2eTestSource,
	})
	before := readFile(t, filepath.Join(repo, "internal", "work", "work.go"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", "-repo", repo, "-root", "internal", "-mode", "max"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("apply exited %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	after := readFile(t, filepath.Join(repo, "internal", "work", "work.go"))

	if res := checkFile([]byte(before), []byte(after)); !res.OK {
		t.Fatalf("apply changed code: %s\n---\n%s", res.Reason, after)
	}
	mustContain(t, after, []string{
		"// Package work does a thing.",
		"// #nosec G304 -- keep me",
		"//go:generate stringer -type=Kind",
		"// Exported is documented and must keep its doc.",
		"// This comment splits an alignment run and must survive.",
		"// A is the first field.",
		"// LongerName is the second.",
		"// trailing note",
	})
	mustNotContain(t, after, []string{
		"This floating block explains",
		"// computeIt is unexported prose",
	})
	// The class set printed by the mode must reach stderr, so a reader of the
	// run log can see what was in play.
	if !strings.Contains(stderr.String(), "classes cut:") {
		t.Fatalf("apply did not print its class set:\n%s", stderr.String())
	}
	// The formatter must have left the file clean.
	assertGofumptClean(t, repo, "internal/work/work.go")
}

func TestSafeModeTouchesOnlyFunctionBodyProse(t *testing.T) {
	useRepoFormatters(t)
	repo := tempModule(t, map[string]string{"internal/work/work.go": e2eSource})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"apply", "-repo", repo, "-root", "internal", "-test=false"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("apply exited %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	after := readFile(t, filepath.Join(repo, "internal", "work", "work.go"))
	mustNotContain(t, after, []string{"This floating block explains"})
	mustContain(t, after, []string{
		"// computeIt is unexported prose",
		"// This paragraph is history that no lint rule reads.",
	})
}

func TestApplyRevertsEverythingWhenTheAffectedTestsFail(t *testing.T) {
	useRepoFormatters(t)
	// This test asserts on the text of a comment the cut removes, which is
	// exactly the failure mode check (c) exists to catch.
	const guard = `package work

import (
	"os"
	"strings"
	"testing"
)

func TestSourceKeepsTheNote(t *testing.T) {
	b, err := os.ReadFile("work.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "load-bearing prose") {
		t.Fatal("the note is gone")
	}
}
`
	const src = `package work

func f() int {
	// load-bearing prose that a test reads out of the source
	return 1
}
`
	repo := tempModule(t, map[string]string{
		"internal/work/work.go":      src,
		"internal/work/work_test.go": guard,
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", "-repo", repo, "-root", "internal", "-mode", "max"}, &stdout, &stderr)
	if code == exitOK {
		t.Fatalf("apply succeeded although the affected package's tests fail\n%s\n%s",
			stdout.String(), stderr.String())
	}
	if got := readFile(t, filepath.Join(repo, "internal", "work", "work.go")); got != src {
		t.Fatalf("the file was not restored:\n%s", got)
	}
	if !strings.Contains(stderr.String(), "restoring every file this run changed") {
		t.Fatalf("apply did not say why it restored:\n%s", stderr.String())
	}
}

func TestTruncateShortensExportedDocsAndVerifyAgrees(t *testing.T) {
	useRepoFormatters(t)
	const src = `// Package work does a thing.
//
// History paragraph one.
// History paragraph two.
package work

// Exported does the first thing.
//
// It also carries three more paragraphs of history that no rule reads.
// Paragraph two.
// Paragraph three.
func Exported() {}

// S is a type.
//
// Tags: mechanism
//
// This group carries a marker a test reads, so it must not be truncated.
type S struct{}
`
	repo := tempModule(t, map[string]string{"internal/work/work.go": src})
	baseline := t.TempDir()
	writeFile(t, baseline, "internal/work/work.go", src)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"truncate", "-repo", repo, "-root", "internal", "-test=false"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("truncate exited %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	after := readFile(t, filepath.Join(repo, "internal", "work", "work.go"))
	mustContain(t, after, []string{
		"// Package work does a thing.",
		"// Exported does the first thing.",
		"// Tags: mechanism",
		"// This group carries a marker a test reads, so it must not be truncated.",
	})
	mustNotContain(t, after, []string{"History paragraph one.", "Paragraph three."})

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{
		"verify", "-repo", repo, "-root", "internal",
		"-baseline", baseline, "-test=false",
	}, &stdout, &stderr); code != exitOK {
		t.Fatalf("verify exited %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "1 file(s) changed, 0 failed") {
		t.Fatalf("verify summary: %s", stdout.String())
	}
}

func TestVerifyReportsACodeByteChange(t *testing.T) {
	repo := tempModule(t, map[string]string{
		"internal/work/work.go": "package work\n\ntype S struct {\n\tA          int\n\tLongerName string\n}\n",
	})
	baseline := t.TempDir()
	writeFile(t, baseline, "internal/work/work.go",
		"package work\n\ntype S struct {\n\tA int\n\t// c\n\tLongerName string\n}\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"verify", "-repo", repo, "-root", "internal",
		"-baseline", baseline, "-test=false",
	}, &stdout, &stderr)
	if code != exitFailed {
		t.Fatalf("verify exited %d, want %d\n%s\n%s", code, exitFailed, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "code line") {
		t.Fatalf("verify did not name the changed code line:\n%s", stderr.String())
	}
}

func TestDeclinedFilesAreReportedAndCounted(t *testing.T) {
	repo := tempModule(t, map[string]string{
		"internal/work/broken.go": "package work\n\nfunc f( {}\n",
	})
	var stdout, stderr bytes.Buffer
	code := run([]string{"apply", "-repo", repo, "-root", "internal", "-test=false"}, &stdout, &stderr)
	if code != exitFailed {
		t.Fatalf("apply exited %d, want %d", code, exitFailed)
	}
	if !strings.Contains(stderr.String(), "declined internal/work/broken.go: parse error") {
		t.Fatalf("the declined file was not reported with a reason:\n%s", stderr.String())
	}
}

func TestListEmitsJSONWithClassesAndText(t *testing.T) {
	repo := tempModule(t, map[string]string{"internal/work/work.go": e2eSource})
	var stdout, stderr bytes.Buffer
	if code := run([]string{"list", "-repo", repo, "-root", "internal", "-mode", "aggressive"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("list exited %d\n%s", code, stderr.String())
	}
	out := stdout.String()
	mustContain(t, out, []string{
		`"mode": "aggressive"`,
		`"attach": "doc-exported"`,
		`"attach": "func-body-floating"`,
		`"class": "cut"`,
		`"class": "preserve"`,
		`"class": "hazard"`,
		`"reasons"`,
		`"start_line"`,
		`"end_line"`,
		`"lines"`,
		`"text"`,
	})
}

func TestBadArgsExitTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitBadArgs {
		t.Fatalf("no verb exited %d, want %d", code, exitBadArgs)
	}
	if code := run([]string{"nope"}, &stdout, &stderr); code != exitBadArgs {
		t.Fatalf("unknown verb exited %d, want %d", code, exitBadArgs)
	}
	if code := run([]string{"apply", "-mode", "nope"}, &stdout, &stderr); code != exitBadArgs {
		t.Fatalf("unknown mode exited %d, want %d", code, exitBadArgs)
	}
	if code := run([]string{"verify"}, &stdout, &stderr); code != exitBadArgs {
		t.Fatalf("verify without a baseline exited %d, want %d", code, exitBadArgs)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- the path is a temporary directory this test made
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, got string, wants []string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("expected to find %q in:\n%s", w, got)
		}
	}
}

func mustNotContain(t *testing.T, got string, wants []string) {
	t.Helper()
	for _, w := range wants {
		if strings.Contains(got, w) {
			t.Errorf("expected %q to be gone from:\n%s", w, got)
		}
	}
}

func assertGofumptClean(t *testing.T, repo, rel string) {
	t.Helper()
	bin := os.Getenv("GOFUMPT")
	if bin == "" {
		bin = "gofumpt"
	}
	// #nosec G204 -- bin is this repository's own pinned gofumpt
	out, err := exec.Command(bin, "-l", filepath.Join(repo, rel)).CombinedOutput()
	if err != nil {
		t.Fatalf("gofumpt -l: %v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("gofumpt reports the file is not formatted: %s", out)
	}
}
