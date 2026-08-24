package runmerge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// The fixture helpers the fork-death tests share: one signal stub, one way to
// run git, one way to write a file, one run ID.
//
// There used to be two of each, one per test file, in this one package. Two
// copies of a stub is the part that hurt: a repair made to one never reaches the
// tests that use the other, and nothing reports the gap.
//
// Bead: hk-7neu1.

// gitStubAnyArgs, given to stopGitOnceOn as subArg, matches the subcommand
// whatever words follow it.
const gitStubAnyArgs = "*"

// gitStubNonFlagArg, given to stopGitOnceOn as subArg, matches only when the
// word after the subcommand is not a flag. It is how `git rebase <branch>` is
// told apart from `git rebase --abort`.
const gitStubNonFlagArg = ""

// stopGitOnceOn puts a git on PATH that stops ITSELF with a signal on the first
// call that matches, and hands every other call to the real git.
//
// sub is the git subcommand, matched against the first word. subArg picks which
// calls of that subcommand count: gitStubAnyArgs for all of them,
// gitStubNonFlagArg for the ones whose next word is not a flag, or an exact word
// to match that word alone.
//
// when is "before", where the child dies with git never having run, or "after",
// where the real git runs first and the signal lands on a command that already
// did its work.
//
// The stub stops the FIRST matching call only. A stub that stopped every call
// would prove nothing: the second run would be stopped too, and the test would
// read the same whether the fix works or not.
//
// It returns the path of the file that counts the matching calls. Build the
// repository BEFORE calling this.
func stopGitOnceOn(t *testing.T, sub, subArg, when string) string {
	t.Helper()
	// Look the real git up BEFORE the PATH swap, or the stub finds itself.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("find the real git: %v", err)
	}

	var body string
	switch when {
	case "before":
		body = "kill -SEGV $$\n"
	case "after":
		body = "\"" + realGit + "\" \"$@\"\n    kill -SEGV $$\n"
	default:
		t.Fatalf("unknown stub mode %q", when)
	}

	// The match test is built in Go rather than in the shell, so each rule reads
	// as itself instead of as a quoted pattern that has to escape a glob.
	var match string
	switch subArg {
	case gitStubAnyArgs:
		match = "if [ \"$1\" = \"" + sub + "\" ]; then matched=1; fi\n"
	case gitStubNonFlagArg:
		match = "if [ \"$1\" = \"" + sub + "\" ]; then\n" +
			"  case \"$2\" in\n" +
			"    -*) ;;\n" +
			"    *) matched=1 ;;\n" +
			"  esac\n" +
			"fi\n"
	default:
		match = "if [ \"$1\" = \"" + sub + "\" ] && [ \"$2\" = \"" + subArg + "\" ]; then matched=1; fi\n"
	}

	countPath := filepath.Join(t.TempDir(), "calls")
	script := "#!/bin/sh\n" +
		"matched=0\n" +
		match +
		"if [ \"$matched\" = 1 ]; then\n" +
		"  n=$(cat " + countPath + " 2>/dev/null || echo 0)\n" +
		"  n=$((n+1))\n" +
		"  echo $n > " + countPath + "\n" +
		"  if [ \"$n\" -le 1 ]; then\n" +
		"    " + body +
		"  fi\n" +
		"fi\n" +
		"exec \"" + realGit + "\" \"$@\"\n"

	binDir := t.TempDir()
	//nolint:gosec // G306: a fake git has to be executable to stand in for one
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700); err != nil {
		t.Fatalf("write the fake git: %v", err)
	}
	// The stub goes in FRONT of the real PATH rather than replacing it: it is a
	// shell script and it needs the ordinary commands to still be there.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return countPath
}

// stubCalls reads how many matching git calls the stub saw.
func stubCalls(t *testing.T, countPath string) int {
	t.Helper()
	raw, err := os.ReadFile(countPath) //nolint:gosec // G304: the path comes from the test itself
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read the call count: %v", err)
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil {
		t.Fatalf("the call count %q is not a number: %v", raw, convErr)
	}
	return n
}

// gitInDir runs git in dir and returns its output, failing the test on error.
func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// writeFixtureFile writes name under dir. name may name a subdirectory, which is
// made first.
func writeFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("make the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// fixtureRunID is the run these tests merge. The value is fixed so a failure
// message names the same run every time it is read.
func fixtureRunID(t *testing.T) core.RunID {
	t.Helper()
	return core.RunID(uuid.MustParse("0190a000-0000-7000-8000-0000000007a1"))
}
