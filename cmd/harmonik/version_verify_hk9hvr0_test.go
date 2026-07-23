package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	if res.ExitCode != verifyExitOK {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitOK)
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

// TestRunVersionInspect_HelpMentionsStringsPitfall pins the CORRECTED rationale
// in the user-facing help.
//
// The original wording claimed `strings | grep` "false-positives ... the Go
// linker packs unrelated strings adjacent in the string blob". That was
// measured on this machine and is false for the case it cited: on 2026-07-22
// `strings -a … | grep -c harmonik-input` returned 1 on the pre-fix binary
// (which really declared `const inputBufferName = "harmonik-input"`) and 0 on
// the post-fix one — an ordinary TRUE positive. The defensible argument, pinned
// here, is that `strings`/`nm` probe an incidental artefact of one fix and so do
// not generalise, whereas vcs.revision answers "which revision is this binary"
// uniformly. This test fails if the retracted claim comes back.
func TestRunVersionInspect_HelpMentionsStringsPitfall(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runVersionInspect([]string{"--help"}, &stdout, &stderr); code != verifyExitOK {
		t.Fatalf("exit = %d; want %d", code, verifyExitOK)
	}
	help := stdout.String()
	for _, want := range []string{"strings", "go tool nm", "INCIDENTAL artefact", "vcs.revision"} {
		if !strings.Contains(help, want) {
			t.Errorf("help text is missing %q; it must explain why per-fix probes do not generalise", want)
		}
	}
	for _, retracted := range []string{"string blob", "packs unrelated strings", "false close"} {
		if strings.Contains(help, retracted) {
			t.Errorf("help text repeats the retracted claim %q (measured false; see this test's comment)", retracted)
		}
	}
}

// TestVersionArgsRouteToInspect pins how specs/release-pipeline.md §2.3 was
// honoured: §2.3 makes the `--version` output format normative, so only the
// POSITIONAL `version` subcommand may route to the provenance check. Routing
// `harmonik --version --json` there would emit JSON where the spec requires
// "harmonik v0.y.z (commit: <sha>)".
func TestVersionArgsRouteToInspect(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{"harmonik", "version"}, false},
		{[]string{"harmonik", "version", "--binary", "/bin/x"}, true},
		{[]string{"harmonik", "version", "--help"}, true},
		{[]string{"harmonik", "--version"}, false},
		{[]string{"harmonik", "--version", "--json"}, false},
		{[]string{"harmonik", "-version", "--binary", "/bin/x"}, false},
		{[]string{"harmonik"}, false},
	} {
		if got := versionArgsRouteToInspect(tc.argv); got != tc.want {
			t.Errorf("versionArgsRouteToInspect(%q) = %t; want %t", tc.argv, got, tc.want)
		}
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

// TestGitIsRepo distinguishes a working tree from a bare directory. A wrong
// answer here turns every containment question into an exit-2 usage error.
func TestGitIsRepo(t *testing.T) {
	repo := newVerifyRepoFixture(t)
	if !gitIsRepo(t.Context(), repo.dir) {
		t.Error("a git working tree was reported as not a repository")
	}
	if gitIsRepo(t.Context(), t.TempDir()) {
		t.Error("an empty directory was reported as a git repository")
	}
}

// TestContainmentVerdict_MissingFromDirtyBuild covers the !isAncestor &&
// Modified branch: the binary predates the commit AND was built dirty, so the
// detail must say the unrecorded local edits cannot be ruled in or out. Without
// that sentence an operator reads a flat "missing" and may conclude the fix is
// definitely absent when the build was never fully described by its revision.
func TestContainmentVerdict_MissingFromDirtyBuild(t *testing.T) {
	stamp := binaryStamp{Revision: "abc123", Modified: true}
	res := containmentVerdict(stamp, "def456", false, verifyResult{})
	if res.Status != verifyStatusMissing {
		t.Errorf("status = %q; want %q", res.Status, verifyStatusMissing)
	}
	if res.ExitCode != verifyExitMissing {
		t.Errorf("exit = %d; want %d", res.ExitCode, verifyExitMissing)
	}
	if !strings.Contains(res.Detail, "dirty") {
		t.Errorf("detail = %q; want it to say the tree was dirty at build time", res.Detail)
	}
}

// TestVerifyContract_LiteralExitCodesAndStatusTokens pins the PUBLISHED
// contract as literals.
//
// Every other test in this file compares a result against the same constant
// production uses, so renumbering verifyExitDirty to 5 or renaming
// verifyStatusContains to "ok" would leave the suite green while silently
// invalidating the tables in docs/daemon-redeploy.md, CLI-REFERENCE.md and
// `version --help`, plus every --json consumer and the step-2b swap gate.
func TestVerifyContract_LiteralExitCodesAndStatusTokens(t *testing.T) {
	for _, tc := range []struct{ name, got, want string }{
		{"verifyStatusRevision", verifyStatusRevision, "revision"},
		{"verifyStatusContains", verifyStatusContains, "contains"},
		{"verifyStatusMissing", verifyStatusMissing, "missing"},
		{"verifyStatusContainsDirty", verifyStatusContainsDirty, "contains-dirty"},
		{"verifyStatusNoBuildInfo", verifyStatusNoBuildInfo, "no-build-info"},
		{"verifyStatusNoVCSStamp", verifyStatusNoVCSStamp, "no-vcs-stamp"},
		{"verifyStatusUnknownRevision", verifyStatusUnknownRevision, "unknown-revision"},
		{"verifyStatusUsageError", verifyStatusUsageError, "usage-error"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q; want %q (published status token)", tc.name, tc.got, tc.want)
		}
	}
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"verifyExitOK", verifyExitOK, 0},
		{"verifyExitMissing", verifyExitMissing, 1},
		{"verifyExitUsage", verifyExitUsage, 2},
		{"verifyExitDirty", verifyExitDirty, 3},
		{"verifyExitIndeterminate", verifyExitIndeterminate, 4},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d; want %d (published exit code)", tc.name, tc.got, tc.want)
		}
	}
}

// verifyPublishedRows is the status/exit table as the docs and the help text
// publish it. It is the input to the parity test below, which is what keeps
// three unsynchronised copies of the table honest.
var verifyPublishedRows = []struct {
	status string
	exit   int
}{
	{verifyStatusContains, verifyExitOK},
	{verifyStatusRevision, verifyExitOK},
	{verifyStatusMissing, verifyExitMissing},
	{verifyStatusUsageError, verifyExitUsage},
	{verifyStatusContainsDirty, verifyExitDirty},
	{verifyStatusNoBuildInfo, verifyExitIndeterminate},
	{verifyStatusNoVCSStamp, verifyExitIndeterminate},
	{verifyStatusUnknownRevision, verifyExitIndeterminate},
}

// hasStatusExitRow reports whether text has a line pairing status with exit as
// adjacent fields. It tolerates both the help text's column layout and a
// markdown table row, so one check covers all three published surfaces.
func hasStatusExitRow(text, status string, exit int) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.ReplaceAll(line, "|", " "))
		for i := 0; i+1 < len(fields); i++ {
			if strings.Trim(fields[i], "`") == status && fields[i+1] == strconv.Itoa(exit) {
				return true
			}
		}
	}
	return false
}

// TestVersionStatusTableParity keeps the three copies of the status/exit table
// in sync: `version --help`, docs/daemon-redeploy.md and CLI-REFERENCE.md. A
// row added or renumbered in the code without updating a doc fails here rather
// than at 2am on a deploy.
func TestVersionStatusTableParity(t *testing.T) {
	surfaces := map[string]string{"version --help": versionVerifyUsage}
	for _, rel := range []string{
		filepath.Join("..", "..", "docs", "daemon-redeploy.md"),
		filepath.Join("..", "..", "CLI-REFERENCE.md"),
	} {
		body, err := os.ReadFile(rel) //nolint:gosec // G304: fixed repo-relative doc paths
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		surfaces[rel] = string(body)
	}
	for name, text := range surfaces {
		for _, row := range verifyPublishedRows {
			if !hasStatusExitRow(text, row.status, row.exit) {
				t.Errorf("%s does not publish %q -> exit %d", name, row.status, row.exit)
			}
		}
	}
}

// TestRenderVerifyResult_GateMarkerLine pins the exact human-output line the
// step-2b swap gate in docs/daemon-redeploy.md greps for
// (`grep -qx 'status:   contains'`). The gate needs a positive marker because
// any harmonik built before this command shipped ignores the flags, prints only
// its version line and exits 0. Changing this spacing breaks the gate open.
func TestRenderVerifyResult_GateMarkerLine(t *testing.T) {
	out, err := renderVerifyResult(verifyResult{
		Binary:   "/bin/harmonik",
		Revision: "abc123",
		Contains: "def456",
		Status:   verifyStatusContains,
		Detail:   "detail here",
	}, false)
	if err != nil {
		t.Fatalf("renderVerifyResult: %v", err)
	}
	const marker = "status:   contains"
	found := false
	for _, line := range strings.Split(out, "\n") {
		if line == marker {
			found = true
		}
	}
	if !found {
		t.Errorf("output %q has no line exactly equal to %q; the deploy gate greps for it", out, marker)
	}
}

// TestDaemonRedeployGateGrepsForMarker closes the other half of the loop that
// TestRenderVerifyResult_GateMarkerLine opens. That test pins what the code
// EMITS; this one pins what the runbook GREPS FOR. Without it, editing
// `status:   contains` in docs/daemon-redeploy.md silently breaks the gate open
// while the whole suite stays green — and a gate that reports success without
// checking anything is the exact defect the swap gate was written to close.
func TestDaemonRedeployGateGrepsForMarker(t *testing.T) {
	rel := filepath.Join("..", "..", "docs", "daemon-redeploy.md")
	body, err := os.ReadFile(rel) //nolint:gosec // G304: fixed repo-relative doc path
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	const gateGrep = `grep -qx 'status:   contains'`
	if !strings.Contains(string(body), gateGrep) {
		t.Errorf("%s no longer contains %q; the swap gate's positive marker must match "+
			"the line renderVerifyResult emits, byte for byte", rel, gateGrep)
	}
}

// TestRunVersionInspect_UsageErrorIsJSONOnStdout verifies that --json is
// honoured on the exit-2 paths too. A machine consumer that asked for JSON must
// not get prose on the second-most-likely outcome.
func TestRunVersionInspect_UsageErrorIsJSONOnStdout(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	for name, args := range map[string][]string{
		"unreadable binary": {"--binary", missing, "--json"},
		"unknown flag":      {"--bogus", "--json"},
	} {
		var stdout, stderr bytes.Buffer
		code := runVersionInspect(args, &stdout, &stderr)
		if code != verifyExitUsage {
			t.Errorf("%s: exit = %d; want %d", name, code, verifyExitUsage)
		}
		var got verifyResult
		if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
			t.Errorf("%s: stdout %q is not JSON: %v", name, stdout.String(), err)
			continue
		}
		if got.Status != verifyStatusUsageError {
			t.Errorf("%s: status = %q; want %q", name, got.Status, verifyStatusUsageError)
		}
		if got.ExitCode != verifyExitUsage {
			t.Errorf("%s: json exit_code = %d; want %d", name, got.ExitCode, verifyExitUsage)
		}
		if got.Detail == "" {
			t.Errorf("%s: json detail is empty", name)
		}
		if stderr.Len() != 0 {
			t.Errorf("%s: stderr = %q; want everything on stdout in --json mode", name, stderr.String())
		}
	}
}

// TestReadBinaryStamp_RealStampedBinary is the only test that exercises
// readBinaryStamp's core read path against a REAL Go binary, and so the only
// one that can catch a typo in the "vcs.revision" / "vcs.modified" / "vcs.time"
// setting keys. With a typo the command answers no-vcs-stamp (exit 4) forever
// while every other test in this file still passes — the whole feature would be
// dead. It builds a throwaway module inside a throwaway git repo and skips
// cleanly if the toolchain cannot build there.
func TestReadBinaryStamp_RealStampedBinary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain on PATH: %v", err)
	}
	src := t.TempDir()
	gitFixtureRun(t, src, "init")
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.invalid/stamped\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	gitFixtureRun(t, src, "add", "go.mod", "main.go")
	gitFixtureRun(t, src, "commit", "-m", "stamped fixture")
	head := gitFixtureRun(t, src, "rev-parse", "HEAD")

	out := filepath.Join(t.TempDir(), "stamped")
	build := func() error {
		// The program name is a literal and argv is assembled on cmd.Args, so
		// the call site holds no variable and no spread slice (gosec G204) —
		// matching gitFixtureRun above.
		cmd := exec.CommandContext(t.Context(), "go")
		cmd.Args = append(cmd.Args, "build", "-buildvcs=true", "-o", out, ".")
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOPROXY=off")
		if combined, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w\n%s", err, combined)
		}
		return nil
	}
	if err := build(); err != nil {
		t.Skipf("cannot build a stamped fixture binary here: %v", err)
	}

	stamp, err := readBinaryStamp(out)
	if err != nil {
		t.Fatalf("readBinaryStamp(stamped binary): %v", err)
	}
	if stamp.Revision != head {
		t.Errorf("Revision = %q; want %q — the vcs.revision setting key is not being read", stamp.Revision, head)
	}
	if stamp.Modified {
		t.Error("Modified = true for a binary built from a clean repo")
	}
	if stamp.Time == "" {
		t.Error("Time is empty — the vcs.time setting key is not being read")
	}

	// Dirty the tracked source and rebuild: vcs.modified must flip, which is
	// what makes contains-dirty (exit 3) reachable at all.
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n\nfunc main() { _ = 1 }\n"), 0o600); err != nil {
		t.Fatalf("dirty main.go: %v", err)
	}
	if err := build(); err != nil {
		t.Skipf("cannot rebuild the dirty fixture: %v", err)
	}
	dirty, err := readBinaryStamp(out)
	if err != nil {
		t.Fatalf("readBinaryStamp(dirty build): %v", err)
	}
	if !dirty.Modified {
		t.Error("Modified = false after building from a dirty tree — the vcs.modified setting key is not being read")
	}
}
