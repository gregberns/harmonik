package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const usage = `commentcut — cut comment volume from Go sources without changing a code byte

usage:
  commentcut list     [flags]              classify every comment block as JSON
  commentcut apply    [flags]              delete the cuttable blocks
  commentcut truncate [flags]              shorten over-long blocks to their first N lines
  commentcut verify   --baseline DIR       check a modified tree against a pristine copy
  commentcut modes                         print every mode and the classes it cuts

Scope defaults to internal/ cmd/ tools/ test/. testdata/ and evaltasks/ are
always excluded, and a root that resolves outside the repository is refused.
Run "commentcut modes" for the class set of each mode.

apply and truncate refuse to start when a file in scope is untracked or
already modified, so that "git restore --source=HEAD --worktree -- '*.go'"
is always a complete undo. -allow-dirty lifts that, and gives up the undo.
`

// exit codes: 0 success, 1 a check failed or a file was declined unexpectedly,
// 2 usage or environment error.
const (
	exitOK      = 0
	exitFailed  = 1
	exitBadArgs = 2
)

var errBadArgs = errors.New("bad arguments")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdoutW, stderrW io.Writer) int {
	stdout, stderr := printer{stdoutW}, printer{stderrW}
	if len(args) == 0 {
		stderr.print(usage)
		return exitBadArgs
	}
	verb, rest := args[0], args[1:]
	var err error
	switch verb {
	case "list":
		err = cmdList(rest, stdout, stderr)
	case "apply":
		err = cmdApply(rest, stdout, stderr)
	case "truncate":
		err = cmdTruncate(rest, stdout, stderr)
	case "verify":
		err = cmdVerify(rest, stdout, stderr)
	case "modes":
		for _, m := range modes {
			stdout.printf("%s\n", m.Describe())
		}
		return exitOK
	case "-h", "--help", "help":
		stdout.print(usage)
		return exitOK
	default:
		stderr.printf("unknown verb %q\n\n%s", verb, usage)
		return exitBadArgs
	}
	if err != nil {
		stderr.printf("commentcut %s: %v\n", verb, err)
		if errors.Is(err, errBadArgs) {
			return exitBadArgs
		}
		return exitFailed
	}
	return exitOK
}

// printer writes progress and diagnostics. A failed write to a console is not
// a condition this tool can act on, and threading that error through every
// report site would bury the reports it exists to make.
type printer struct{ w io.Writer }

func (p printer) printf(format string, args ...any) {
	fmt.Fprintf(p.w, format, args...) //nolint:errcheck // a failed write to the console is not actionable here
}

func (p printer) print(s string) {
	io.WriteString(p.w, s) //nolint:errcheck,gosec // a failed write to the console is not actionable here
}

// commonFlags are the scope and write flags the verbs share.
type commonFlags struct {
	repo       string
	roots      stringList
	filter     stringList
	exclude    stringList
	mode       string
	dryRun     bool
	tests      bool
	quiet      bool
	allowDirty bool
}

// bindScope binds the flags every verb has: which files are in play.
func (c *commonFlags) bindScope(fset *flag.FlagSet) {
	fset.StringVar(&c.repo, "repo", "", "repository root (default: git rev-parse --show-toplevel)")
	fset.Var(&c.roots, "root", "scope root, repeatable (default: internal, cmd, tools, test)")
	fset.Var(&c.filter, "path", "keep only files whose path contains this substring, repeatable")
	fset.Var(&c.exclude, "exclude", "drop files whose path contains this substring, repeatable")
}

// bindWrite binds the flags of a verb that changes files.
func (c *commonFlags) bindWrite(fset *flag.FlagSet) {
	c.bindScope(fset)
	fset.BoolVar(&c.dryRun, "dry-run", false, "report what would change and write nothing")
	fset.BoolVar(&c.tests, "test", true, "run go test on every affected package and revert if it fails")
	fset.BoolVar(&c.quiet, "quiet", false, "suppress the summary line")
	fset.BoolVar(&c.allowDirty, "allow-dirty", false,
		"proceed even when a file in scope is untracked or already modified (the undo is then not `git restore`)")
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func repoRoot(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locating repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func moduleName(repo string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("reading module path: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// readSource reads one file in scope.
func readSource(path string) ([]byte, error) {
	return os.ReadFile(path) // #nosec G304 -- the path comes from the scope walk, which is confined to the repo roots
}

// ---------------------------------------------------------------- list

func cmdList(args []string, stdout, stderr printer) error {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	fset.SetOutput(stderr.w)
	var c commonFlags
	c.bindScope(fset)
	fset.StringVar(&c.mode, "mode", "safe", "class set to score against: safe, aggressive, max")
	noText := fset.Bool("no-text", false, "omit the comment text from each record")
	only := fset.String("only", "", "emit only records whose class is this (cut, preserve, hazard)")
	if err := fset.Parse(args); err != nil {
		return errBadArgs
	}
	repo, err := repoRoot(c.repo)
	if err != nil {
		return err
	}
	m, err := lookupMode(c.mode)
	if err != nil {
		return fmt.Errorf("%w: %w", errBadArgs, err)
	}
	files, err := findFiles(repo, c.roots, c.filter, c.exclude)
	if err != nil {
		return err
	}
	var out []*Block
	summary := map[string]int{}
	lines := map[string]int{}
	declined := 0
	sel := SelectionFor(m)
	for _, rel := range files {
		src, err := readSource(filepath.Join(repo, rel))
		if err != nil {
			return err
		}
		fb, err := classifyFile(rel, src)
		if err != nil {
			stderr.printf("declined %s: parse error: %v\n", rel, err)
			declined++
			continue
		}
		decide(fb, sel)
		for _, b := range fb.Blocks {
			summary[string(b.Attach)+"/"+b.Class]++
			lines[b.Class] += b.Lines
			if *only != "" && b.Class != *only {
				continue
			}
			if *noText {
				b.Text = ""
			}
			out = append(out, b)
		}
	}
	enc := json.NewEncoder(stdout.w)
	enc.SetIndent("", "  ")
	payload := struct {
		Mode    string         `json:"mode"`
		Classes []Attach       `json:"classes_cut"`
		Files   int            `json:"files"`
		Counts  map[string]int `json:"counts_by_attach_and_class"`
		Lines   map[string]int `json:"lines_by_class"`
		Blocks  []*Block       `json:"blocks"`
	}{m.Name, m.Classes, len(files), summary, lines, out}
	if err := enc.Encode(payload); err != nil {
		return err
	}
	if declined > 0 {
		return fmt.Errorf("%d file(s) declined", declined)
	}
	return nil
}

// ---------------------------------------------------------------- apply

func cmdApply(args []string, stdout, stderr printer) error {
	fset := flag.NewFlagSet("apply", flag.ContinueOnError)
	fset.SetOutput(stderr.w)
	var c commonFlags
	c.bindWrite(fset)
	fset.StringVar(&c.mode, "mode", "safe", "class set to cut: safe, aggressive, max")
	if err := fset.Parse(args); err != nil {
		return errBadArgs
	}
	m, err := lookupMode(c.mode)
	if err != nil {
		return fmt.Errorf("%w: %w", errBadArgs, err)
	}
	stderr.printf("%s\n", m.Describe())
	return mutate(c, stdout, stderr, SelectionFor(m), func(_ []byte, fb *FileBlocks) ([]Splice, error) {
		return deleteSplices(fb), nil
	})
}

// ---------------------------------------------------------------- truncate

// truncateDefaultClasses are the classes truncation targets by default: the two
// kinds of doc comment the lint rules require to EXIST but do not read past the
// first line. Struct field docs are excluded on purpose — three tests in
// internal/core walk the "//" block above a field and require phrases that live
// well past line two.
var truncateDefaultClasses = []Attach{AttachDocExported, AttachPkgDoc}

func cmdTruncate(args []string, stdout, stderr printer) error {
	fset := flag.NewFlagSet("truncate", flag.ContinueOnError)
	fset.SetOutput(stderr.w)
	var c commonFlags
	c.bindWrite(fset)
	keep := fset.Int("keep", 2, "number of leading comment lines to keep")
	classes := fset.String("classes", joinAttach(truncateDefaultClasses),
		"comma-separated attachment kinds to truncate")
	if err := fset.Parse(args); err != nil {
		return errBadArgs
	}
	if *keep < 1 {
		return fmt.Errorf("%w: -keep must be at least 1", errBadArgs)
	}
	allowed := map[Attach]bool{}
	for _, name := range strings.Split(*classes, ",") {
		if name = strings.TrimSpace(name); name != "" {
			allowed[Attach(name)] = true
		}
	}
	if len(allowed) == 0 {
		return fmt.Errorf("%w: -classes selected nothing", errBadArgs)
	}
	stderr.printf("truncate: keeping the first %d line(s) of blocks in: %s\n",
		*keep, strings.Join(sortedKeys(allowed), ", "))
	sel := Selection{Allowed: allowed, Hazard: false}
	return mutate(c, stdout, stderr, sel, func(src []byte, fb *FileBlocks) ([]Splice, error) {
		ss := make([]Splice, 0, len(fb.Blocks))
		for _, b := range fb.Blocks {
			if !b.Cuttable {
				continue
			}
			s, ok, err := truncateSplice(src, b, *keep)
			if err != nil {
				return nil, err
			}
			if ok {
				ss = append(ss, s)
			}
		}
		return ss, nil
	})
}

func joinAttach(as []Attach) string {
	ss := make([]string, 0, len(as))
	for _, a := range as {
		ss = append(ss, string(a))
	}
	return strings.Join(ss, ",")
}

func sortedKeys(m map[Attach]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------- verify

func cmdVerify(args []string, stdout, stderr printer) error {
	fset := flag.NewFlagSet("verify", flag.ContinueOnError)
	fset.SetOutput(stderr.w)
	var c commonFlags
	c.bindScope(fset)
	fset.BoolVar(&c.tests, "test", true, "run go test on every affected package")
	baseline := fset.String("baseline", "", "directory holding pristine copies at the same relative paths")
	if err := fset.Parse(args); err != nil {
		return errBadArgs
	}
	if *baseline == "" {
		return fmt.Errorf("%w: -baseline is required", errBadArgs)
	}
	repo, err := repoRoot(c.repo)
	if err != nil {
		return err
	}
	files, err := findFiles(repo, c.roots, c.filter, c.exclude)
	if err != nil {
		return err
	}
	changed := make([]string, 0, len(files))
	bad := 0
	for _, rel := range files {
		before, err := readSource(filepath.Join(*baseline, rel))
		if err != nil {
			stderr.printf("declined %s: no baseline copy: %v\n", rel, err)
			bad++
			continue
		}
		after, err := readSource(filepath.Join(repo, rel))
		if err != nil {
			return err
		}
		if bytes.Equal(before, after) {
			continue
		}
		changed = append(changed, rel)
		if res := checkFile(before, after); !res.OK {
			stderr.printf("FAIL %s: %s\n", rel, res.Reason)
			bad++
		}
	}
	stdout.printf("verify: %d file(s) changed, %d failed the static checks\n", len(changed), bad)
	if bad > 0 {
		return fmt.Errorf("%d file(s) failed check (a) token stream or (b) code bytes", bad)
	}
	if c.tests && len(changed) > 0 {
		return runTests(repo, packagesOf(changed), stdout, stderr)
	}
	return nil
}

// ---------------------------------------------------------------- engine

type spliceFn func(src []byte, fb *FileBlocks) ([]Splice, error)

// pending is one file the run has rewritten, with what it looked like before.
type pending struct {
	rel  string
	src  []byte
	mode fs.FileMode
}

// tally is what one pass changed.
type tally struct {
	blocks   int
	lines    int
	declined int
}

// mutate is the whole write path. Every file it touches goes through: splice
// the original bytes, format with the repo's own formatters, check the token
// stream and the code lines against the original, and restore the original on
// any failure. Nothing is accepted on the strength of "it still compiles".
func mutate(c commonFlags, stdout, stderr printer, sel Selection, mk spliceFn) error {
	repo, err := repoRoot(c.repo)
	if err != nil {
		return err
	}
	files, err := findFiles(repo, c.roots, c.filter, c.exclude)
	if err != nil {
		return err
	}
	if !c.dryRun && !c.allowDirty {
		if err := requireCleanScope(repo, files); err != nil {
			return err
		}
	}
	touched, sum, err := rewriteAll(repo, files, sel, mk, c.dryRun, stderr)
	if err != nil {
		if len(touched) > 0 {
			stderr.printf("%v\nrestoring the %d file(s) already rewritten\n", err, len(touched))
			restoreAll(repo, touched, stderr)
		}
		return err
	}
	if c.dryRun {
		stdout.printf("dry run: %d file(s), %d block(s), %d comment line(s) would change\n",
			len(touched), sum.blocks, sum.lines)
		if sum.declined > 0 {
			return fmt.Errorf("%d file(s) declined", sum.declined)
		}
		return nil
	}
	return settle(c, repo, touched, sum, stdout, stderr)
}

// settle is everything that happens after the bytes are on disk: format, check
// (a) and (b) with a revert for each file that fails, report, then check (c).
func settle(c commonFlags, repo string, touched []pending, sum tally, stdout, stderr printer) error {
	if err := formatTouched(repo, touched); err != nil {
		restoreAll(repo, touched, stderr)
		return err
	}
	kept, reverted, err := keepOnlyVerified(repo, touched, stderr)
	if err != nil {
		return err
	}
	if !c.quiet {
		stdout.printf("%d file(s) changed, %d block(s), %d comment line(s), %d reverted, %d declined\n",
			len(kept), sum.blocks, sum.lines, reverted, sum.declined)
	}
	if c.tests && len(kept) > 0 {
		if err := runTests(repo, packagesOf(relPaths(kept)), stdout, stderr); err != nil {
			stderr.print("tests failed; restoring every file this run changed\n")
			restoreAll(repo, kept, stderr)
			return err
		}
	}
	if reverted > 0 || sum.declined > 0 {
		return fmt.Errorf("%d file(s) reverted, %d declined", reverted, sum.declined)
	}
	return nil
}

// rewriteAll classifies every file, builds its edits and writes the result.
// atomicWrite replaces path with content by writing a sibling temp file and
// renaming it over the target. os.WriteFile opens with O_TRUNC and then writes,
// so a process killed in that window leaves the file at zero bytes; rename is
// atomic on POSIX, so the file is either the old bytes or the new ones. The
// close error is handled before the rename, not deferred past it.
func atomicWrite(path string, content []byte, mode os.FileMode) error {
	// rename needs write permission on the DIRECTORY, not on the target, so an
	// atomic write would silently replace a read-only file that os.WriteFile
	// refuses. Probe the target first and keep the old refusal.
	probe, err := os.OpenFile(path, os.O_WRONLY, 0) // #nosec G304 -- the path comes from the scope walk, which is confined to the repo roots
	switch {
	case err == nil:
		if err := probe.Close(); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return err
	}

	tmp := fmt.Sprintf("%s.commentcut-%d.tmp", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) // #nosec G304 -- sibling temp of a scope-walk path
	if err != nil {
		return err
	}
	// The temp is created fresh, so its mode is `mode` masked by the umask.
	// os.WriteFile left an existing file's mode alone; chmod restores that.
	if cerr := f.Chmod(mode); cerr != nil {
		return errors.Join(cerr, f.Close(), os.Remove(tmp))
	}
	if _, werr := f.Write(content); werr != nil {
		return errors.Join(werr, f.Close(), os.Remove(tmp))
	}
	if cerr := f.Close(); cerr != nil {
		return errors.Join(cerr, os.Remove(tmp))
	}
	if rerr := os.Rename(tmp, path); rerr != nil {
		return errors.Join(rerr, os.Remove(tmp))
	}
	return nil
}

func rewriteAll(repo string, files []string, sel Selection, mk spliceFn, dryRun bool, stderr printer) ([]pending, tally, error) {
	var sum tally
	touched := make([]pending, 0, len(files))
	for _, rel := range files {
		abs := filepath.Join(repo, rel)
		info, err := os.Stat(abs)
		if err != nil {
			return touched, sum, err
		}
		src, err := readSource(abs)
		if err != nil {
			return touched, sum, err
		}
		next, splices, err := rewriteOne(rel, src, sel, mk)
		if err != nil {
			stderr.printf("declined %s: %v\n", rel, err)
			sum.declined++
			continue
		}
		if len(splices) == 0 {
			continue
		}
		sum.blocks += len(splices)
		sum.lines += linesRemoved(src, splices)
		touched = append(touched, pending{rel: rel, src: src, mode: info.Mode().Perm()})
		if dryRun {
			continue
		}
		if err := atomicWrite(abs, next, info.Mode().Perm()); err != nil {
			return touched, sum, err
		}
	}
	return touched, sum, nil
}

func rewriteOne(rel string, src []byte, sel Selection, mk spliceFn) ([]byte, []Splice, error) {
	fb, err := classifyFile(rel, src)
	if err != nil {
		return nil, nil, fmt.Errorf("parse error: %w", err)
	}
	decide(fb, sel)
	splices, err := mk(src, fb)
	if err != nil || len(splices) == 0 {
		return nil, nil, err
	}
	next, err := applySplices(src, splices)
	if err != nil {
		return nil, nil, err
	}
	return next, splices, nil
}

func formatTouched(repo string, touched []pending) error {
	if len(touched) == 0 {
		return nil
	}
	module, err := moduleName(repo)
	if err != nil {
		return err
	}
	fmtr, err := NewFormatter(repo, module)
	if err != nil {
		return err
	}
	if err := fmtr.Run(relPaths(touched)); err != nil {
		return fmt.Errorf("formatting: %w", err)
	}
	return nil
}

// keepOnlyVerified runs checks (a) and (b) on every rewritten file and restores
// the ones that fail.
func keepOnlyVerified(repo string, touched []pending, stderr printer) ([]pending, int, error) {
	kept := make([]pending, 0, len(touched))
	reverted := 0
	for _, p := range touched {
		abs := filepath.Join(repo, p.rel)
		after, err := readSource(abs)
		if err != nil {
			return nil, reverted, err
		}
		res := checkFile(p.src, after)
		if res.OK {
			kept = append(kept, p)
			continue
		}
		stderr.printf("reverted %s: %s\n", p.rel, res.Reason)
		if err := atomicWrite(abs, p.src, p.mode); err != nil {
			return nil, reverted, err
		}
		reverted++
	}
	return kept, reverted, nil
}

func relPaths(ps []pending) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.rel)
	}
	return out
}

// linesRemoved counts the physical lines a splice set takes out: the lines it
// spans minus the lines it puts back. Counting the blocks' own line totals
// instead would be right for a deletion and wrong for a truncation, which keeps
// some of them.
func linesRemoved(src []byte, ss []Splice) int {
	n := 0
	for _, s := range ss {
		n += bytes.Count(src[s.Start:s.End], []byte("\n")) - strings.Count(s.Text, "\n")
	}
	return n
}

func restoreAll(repo string, touched []pending, stderr printer) {
	for _, p := range touched {
		if err := atomicWrite(filepath.Join(repo, p.rel), p.src, p.mode); err != nil {
			stderr.printf("restore %s: %v\n", p.rel, err)
		}
	}
}

// runTests is check (c). It compiles and runs the tests of every package the
// run touched. A package with no test files is a compile check and nothing
// more, which is the honest description of what it proves.
func runTests(repo string, pkgs []string, stdout, stderr printer) error {
	stdout.printf("running go test over %d package(s)\n", len(pkgs))
	start := time.Now()
	args := append([]string{"test", "-count=1"}, pkgs...)
	cmd := exec.Command("go", args...) // #nosec G204 -- the arguments are package directories this run computed
	cmd.Dir = repo
	cmd.Stdout = stdout.w
	cmd.Stderr = stderr.w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go test failed after %s: %w", time.Since(start).Round(time.Second), err)
	}
	stdout.printf("go test passed in %s\n", time.Since(start).Round(time.Second))
	return nil
}

// requireCleanScope refuses to start when any file the run may rewrite is
// untracked or already modified.
//
// This is the abort story, and it is a precondition rather than a rollback
// because the only copy of the originals a run holds is process memory. A
// SIGINT, a closed terminal or an OOM leaves a half-cut tree and nothing that
// says which files were done. Requiring that git holds an identical copy of
// every file in scope makes one command a complete undo:
//
//	git restore --source=HEAD --worktree -- '*.go'
//
// It also stops the tool cutting its own untracked source, where that undo
// would not reach.
func requireCleanScope(repo string, files []string) error {
	inScope := make(map[string]bool, len(files))
	for _, f := range files {
		inScope[filepath.ToSlash(f)] = true
	}
	cmd := exec.Command("git", "status", "--porcelain", "-z", "--untracked-files=all", "--", ".")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("reading git status: %w", err)
	}
	var dirty []string
	for _, rec := range strings.Split(string(out), "\x00") {
		if len(rec) < 4 {
			continue
		}
		path := filepath.ToSlash(strings.TrimSpace(rec[3:]))
		if inScope[path] {
			dirty = append(dirty, rec[:2]+" "+path)
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	sort.Strings(dirty)
	shown := dirty
	if len(shown) > 10 {
		shown = shown[:10]
	}
	return fmt.Errorf("%d file(s) in scope are untracked or already modified, so "+
		"`git restore --source=HEAD --worktree` would not undo this run:\n  %s\n"+
		"Commit or stash them, exclude them with -exclude, or pass -allow-dirty and "+
		"make your own backup", len(dirty), strings.Join(shown, "\n  "))
}
