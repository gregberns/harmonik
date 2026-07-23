package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// verifyRepoFixture is a throwaway repo with two linear commits.
type verifyRepoFixture struct {
	dir   string
	first string
	head  string
}

// gitFixtureRun runs a git command in dir and fails the test on error.
// Identity/signing are forced off so the fixture is independent of the
// developer's global git config.
//
// Argv is assembled on cmd.Args rather than passed variadically so that the
// call site holds no spread slice (which gosec G204 reports).
func gitFixtureRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git")
	cmd.Args = append(cmd.Args,
		"-C", dir,
		"-c", "user.name=harmonik-test",
		"-c", "user.email=harmonik-test@example.invalid",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	)
	cmd.Args = append(cmd.Args, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitFixtureCommit writes a file and commits it, returning the new SHA.
func gitFixtureCommit(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	gitFixtureRun(t, dir, "add", name)
	gitFixtureRun(t, dir, "commit", "-m", "add "+name)
	return gitFixtureRun(t, dir, "rev-parse", "HEAD")
}

// newVerifyRepoFixture builds a throwaway repo with two linear commits.
func newVerifyRepoFixture(t *testing.T) verifyRepoFixture {
	t.Helper()
	dir := t.TempDir()
	gitFixtureRun(t, dir, "init")
	first := gitFixtureCommit(t, dir, "a.txt", "one\n")
	head := gitFixtureCommit(t, dir, "b.txt", "two\n")
	return verifyRepoFixture{dir: dir, first: first, head: head}
}

// TestReadBinaryStamp_NoBuildInfo verifies that a file which is not a Go
// binary is reported as errNoBuildInfo — the honest "cannot establish
// provenance" answer — rather than being silently treated as unstamped.
//
// Bead ref: hk-9hvr0.
func TestReadBinaryStamp_NoBuildInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-go-binary")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := readBinaryStamp(path)
	if !errors.Is(err, errNoBuildInfo) {
		t.Fatalf("readBinaryStamp(non-Go file) error = %v; want errNoBuildInfo", err)
	}
}

// TestReadBinaryStamp_MissingFile verifies a missing path surfaces as a
// filesystem error (an operator mistake), not as a provenance verdict.
func TestReadBinaryStamp_MissingFile(t *testing.T) {
	_, err := readBinaryStamp(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readBinaryStamp(missing) error = %v; want fs.ErrNotExist", err)
	}
	if errors.Is(err, errNoBuildInfo) {
		t.Error("a missing file must not be reported as no-build-info")
	}
}

// TestClassifyStamp_NoVCSStamp verifies a binary built with vcs stamping
// disabled yields the no-vcs-stamp status and the indeterminate exit code —
// never a false "missing".
func TestClassifyStamp_NoVCSStamp(t *testing.T) {
	res, err := classifyStamp(t.Context(), binaryStamp{}, t.TempDir(), "deadbeef")
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusNoVCSStamp {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusNoVCSStamp)
	}
	if res.ExitCode != verifyExitIndeterminate {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitIndeterminate)
	}
}

// TestClassifyStamp_UnknownRevision verifies that a revision absent from the
// local repository (shallow clone, or a rev rebased away) is reported as
// unknown-revision rather than as "missing".
func TestClassifyStamp_UnknownRevision(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	stamp := binaryStamp{Revision: "0000000000000000000000000000000000000000"}
	res, err := classifyStamp(t.Context(), stamp, repo.dir, repo.first)
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusUnknownRevision {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusUnknownRevision)
	}
	if res.ExitCode != verifyExitIndeterminate {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitIndeterminate)
	}
}

// TestClassifyStamp_Contains verifies the positive gate: an ancestor commit
// plus a clean tree is the only combination that exits 0.
func TestClassifyStamp_Contains(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	res, err := classifyStamp(t.Context(), binaryStamp{Revision: repo.head}, repo.dir, repo.first)
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusContains {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusContains)
	}
	if res.ExitCode != verifyExitContains {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitContains)
	}
}

// TestClassifyStamp_ContainsDirty verifies vcs.modified=true downgrades a
// containment hit to "necessary but not sufficient" with its own exit code,
// so a dirty build can never pass a swap gate silently.
func TestClassifyStamp_ContainsDirty(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	stamp := binaryStamp{Revision: repo.head, Modified: true}
	res, err := classifyStamp(t.Context(), stamp, repo.dir, repo.first)
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusContainsDirty {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusContainsDirty)
	}
	if res.ExitCode != verifyExitDirty {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitDirty)
	}
}

// TestClassifyStamp_Missing verifies a commit on a divergent branch is
// correctly reported as NOT contained.
func TestClassifyStamp_Missing(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	gitFixtureRun(t, repo.dir, "checkout", "-b", "side", repo.first)
	sideSHA := gitFixtureCommit(t, repo.dir, "c.txt", "three\n")

	res, err := classifyStamp(t.Context(), binaryStamp{Revision: repo.head}, repo.dir, sideSHA)
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusMissing {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusMissing)
	}
	if res.ExitCode != verifyExitMissing {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitMissing)
	}
}

// TestClassifyStamp_RevisionOnly verifies that omitting a target commit
// yields the informational revision report, including the dirty flag.
func TestClassifyStamp_RevisionOnly(t *testing.T) {
	stamp := binaryStamp{Revision: "abc123", Modified: true}
	res, err := classifyStamp(t.Context(), stamp, t.TempDir(), "")
	if err != nil {
		t.Fatalf("classifyStamp: %v", err)
	}
	if res.Status != verifyStatusRevision {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusRevision)
	}
	if !strings.Contains(res.Detail, "DIRTY") {
		t.Errorf("detail = %q; want it to name the dirty tree", res.Detail)
	}
}

// TestClassifyStamp_TargetNotInRepo verifies that a target commit the repo
// does not have is an operator error (returned error -> exit 2), not a
// verdict about the binary.
func TestClassifyStamp_TargetNotInRepo(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	stamp := binaryStamp{Revision: repo.head}
	if _, err := classifyStamp(t.Context(), stamp, repo.dir, "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("classifyStamp with an unknown target commit: want error, got nil")
	}
}

// TestRunVersionInspect_NoBuildInfoJSON exercises the whole command path on a
// file with no Go build info and asserts the JSON contract and exit code.
func TestRunVersionInspect_NoBuildInfoJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plain.txt")
	if err := os.WriteFile(path, []byte("not a binary"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runVersionInspect([]string{"--binary", path, "--contains", "deadbeef", "--json"}, &stdout, &stderr)
	if code != verifyExitIndeterminate {
		t.Fatalf("exit = %d; want %d (stderr: %s)", code, verifyExitIndeterminate, stderr.String())
	}
	var got verifyResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout.String(), err)
	}
	if got.Status != verifyStatusNoBuildInfo {
		t.Errorf("status = %q; want %q", got.Status, verifyStatusNoBuildInfo)
	}
	if got.ExitCode != verifyExitIndeterminate {
		t.Errorf("json exit_code = %d; want %d", got.ExitCode, verifyExitIndeterminate)
	}
}

// TestRunVersionInspect_MissingBinary verifies an unreadable path is an
// operator error (exit 2 with a stderr diagnostic), not a provenance verdict.
func TestRunVersionInspect_MissingBinary(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runVersionInspect([]string{"--binary", filepath.Join(t.TempDir(), "nope")}, &stdout, &stderr)
	if code != verifyExitUsage {
		t.Fatalf("exit = %d; want %d", code, verifyExitUsage)
	}
	if stderr.Len() == 0 {
		t.Error("want a stderr diagnostic for an unreadable binary")
	}
}

// TestRunVersionInspect_UnknownFlag verifies flag errors exit 2 and print usage.
func TestRunVersionInspect_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runVersionInspect([]string{"--bogus"}, &stdout, &stderr); code != verifyExitUsage {
		t.Fatalf("exit = %d; want %d", code, verifyExitUsage)
	}
	if !strings.Contains(stderr.String(), "USAGE") {
		t.Errorf("stderr = %q; want usage text", stderr.String())
	}
}

// TestRunVersionInspect_HelpMentionsStringsPitfall verifies the help text
// carries the warning that motivated this command: `strings | grep` gives
// false positives, which is what caused the hk-9hvr0 false close.
func TestRunVersionInspect_HelpMentionsStringsPitfall(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runVersionInspect([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d; want 0", code)
	}
	if !strings.Contains(stdout.String(), "strings") {
		t.Error("help text must warn that `strings | grep` is unreliable")
	}
}

// TestParseVersionVerifyFlags covers both --flag value and --flag=value forms
// and the missing-value error.
func TestParseVersionVerifyFlags(t *testing.T) {
	f, err := parseVersionVerifyFlags([]string{"--binary=/bin/x", "--contains", "abc", "--repo=/tmp", "--json"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.binary != "/bin/x" || f.contains != "abc" || f.repo != "/tmp" || !f.asJSON {
		t.Errorf("parsed = %+v; want binary=/bin/x contains=abc repo=/tmp json=true", f)
	}
	if _, err := parseVersionVerifyFlags([]string{"--contains"}); err == nil {
		t.Error("want an error for a value-less --contains")
	}
}

// TestRenderVerifyResult_HumanFormat verifies the key/value rendering carries
// the revision, the dirty flag and the status.
func TestRenderVerifyResult_HumanFormat(t *testing.T) {
	out, err := renderVerifyResult(verifyResult{
		Binary:   "/bin/harmonik",
		Revision: "abc123",
		Modified: true,
		Contains: "def456",
		Status:   verifyStatusContainsDirty,
		Detail:   "detail here",
	}, false)
	if err != nil {
		t.Fatalf("renderVerifyResult: %v", err)
	}
	for _, want := range []string{"/bin/harmonik", "abc123", "vcs.modified=true", "def456", verifyStatusContainsDirty} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q missing %q", out, want)
		}
	}
}

// TestGitIsAncestor_ErrorOnBadRepo verifies git failures are surfaced as
// errors rather than silently collapsing to "not an ancestor".
func TestGitIsAncestor_ErrorOnBadRepo(t *testing.T) {
	if _, err := gitIsAncestor(t.Context(), t.TempDir(), "abc", "def"); err == nil {
		t.Error("want an error when the directory is not a git repository")
	}
}

// TestCommitSpec verifies the peel suffix, which is what makes an existence
// check reject a name that resolves to a non-commit or not at all.
func TestCommitSpec(t *testing.T) {
	if got := commitSpec("abc"); got != "abc^{commit}" {
		t.Errorf("commitSpec = %q; want %q", got, "abc^{commit}")
	}
}

// TestGitObjectExists_AbsentIsNotAnError verifies an absent object is
// (false, nil) — a verdict input, not an environment failure.
func TestGitObjectExists_AbsentIsNotAnError(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	ok, err := gitObjectExists(context.Background(), repo.dir, commitSpec("0000000000000000000000000000000000000000"))
	if err != nil {
		t.Fatalf("gitObjectExists: %v", err)
	}
	if ok {
		t.Error("absent object reported as present")
	}
}
