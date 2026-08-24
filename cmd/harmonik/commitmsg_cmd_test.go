package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/commitmsg"
)

// The rules themselves are tested in internal/commitmsg, over 163 subtests, and
// nothing here re-tests them. What this file tests is the seam: the two git
// inputs it resolves, the exit codes it returns, and the stderr shape
// scripts/commit-msg-gate.sh reads.

const cleanMessage = `docs(commitmsg): a message that claims nothing it cannot back up

Body text, so this is not a trivial commit.

Reviewed-By: none — no reviewer was reached for this commit
Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "self-test fixture"}
`

// gitIn runs one git command against a t.TempDir repository.
//
// Every git call in this file goes through here, so the one #nosec sits in one
// place rather than at each of the six call sites.
//
// #nosec G204 -- the executable is the literal "git", root is a t.TempDir path
// and every argument is a constant written in this file. Nothing reaches a
// shell and nothing comes from outside the test binary.
func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// scratchRepo builds a git repository with the reviewer skills this project
// ships, one of them TRACKED and one of them not.
//
// The untracked one is the point. A reviewer identity has to be one git tracks
// at HEAD, or an ordinary `mkdir` beside the real skills mints a name the
// validator trusts, and the whole approval rule is then satisfiable by anyone
// who can write to the checkout.
func scratchRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "commitmsg seam test"},
		{"config", "user.email", "seam@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitIn(t, root, args...)
	}

	for _, name := range []string{"agent-reviewer", "untracked-reviewer"} {
		dir := filepath.Join(root, ".claude", "skills", name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name+"\n"), 0o600); err != nil {
			t.Fatalf("write SKILL.md for %s: %v", name, err)
		}
	}

	gitIn(t, root, "add", ".claude/skills/agent-reviewer/SKILL.md")
	gitIn(t, root, "commit", "-q", "-m", "seed")
	return root
}

func writeMessage(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "commit-msg")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write commit message: %v", err)
	}
	return path
}

func runCommitMsg(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runCommitMsgSubcommand(t.Context(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// TestCommitMsgValidateReproducesTheGateContract pins what
// scripts/commit-msg-gate.sh depends on: exit 0 on a clean message, exit 1 with
// every problem numbered on stderr, and nothing on stdout either way. The gate
// reads the exit status and prints the stderr under the offending commit, so a
// problem written to stdout would vanish from its report.
func TestCommitMsgValidateReproducesTheGateContract(t *testing.T) {
	t.Parallel()
	repo := scratchRepo(t)

	t.Run("clean message exits 0 and says nothing", func(t *testing.T) {
		t.Parallel()
		code, stdout, stderr := runCommitMsg(t, "validate", writeMessage(t, cleanMessage), "--repo", repo)
		if code != 0 {
			t.Errorf("exit = %d, want 0; stderr:\n%s", code, stderr)
		}
		if stdout != "" || stderr != "" {
			t.Errorf("a clean message wrote output; stdout=%q stderr=%q", stdout, stderr)
		}
	})

	t.Run("a fabricated approval exits 1 and numbers every problem", func(t *testing.T) {
		t.Parallel()
		msg := writeMessage(t, `feat(x): a change approved by nobody

Reviewed-By: reviewer-that-does-not-exist
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "approved"}
`)
		code, stdout, stderr := runCommitMsg(t, "validate", msg, "--repo", repo)
		if code != 1 {
			t.Fatalf("exit = %d, want 1; stderr:\n%s", code, stderr)
		}
		if stdout != "" {
			t.Errorf("a refusal wrote to stdout, where the gate does not read it: %q", stdout)
		}
		if !strings.HasPrefix(stderr, "validate-commit-msg: validation failed:\n") {
			t.Errorf("stderr does not open with the line the gate's report indents:\n%s", stderr)
		}
		for _, want := range []string{"  [1] ", "must name a reviewer skill this repo has"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
	})
}

// TestCommitMsgReviewerSetComesFromWhatGitTracks is the reason this command
// exists at all: internal/commitmsg cannot answer which reviewers a repository
// has, and answering it wrong in either direction breaks a real rule.
func TestCommitMsgReviewerSetComesFromWhatGitTracks(t *testing.T) {
	t.Parallel()
	repo := scratchRepo(t)

	got := knownReviewers(t.Context(), repo)
	if len(got) != 1 || got[0] != "agent-reviewer" {
		t.Fatalf("knownReviewers = %v, want exactly [agent-reviewer]: the untracked directory beside it must not mint a name", got)
	}

	// An APPROVE naming the tracked skill lands; one naming the untracked
	// directory does not. Same repository, same command, one `git add` apart.
	for _, tc := range []struct {
		name     string
		reviewer string
		wantExit int
	}{
		{"tracked reviewer", "agent-reviewer", 0},
		{"untracked directory", "untracked-reviewer", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := writeMessage(t, "feat(x): a change\n\nReviewed-By: "+tc.reviewer+
				"\nReview-Verdict: {\"schema_version\": 1, \"verdict\": \"APPROVE\", \"flags\": [], \"notes\": \"ok\"}\n")
			code, _, stderr := runCommitMsg(t, "validate", msg, "--repo", repo)
			if code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", code, tc.wantExit, stderr)
			}
		})
	}
}

// TestCommitMsgReviewerSetFailsClosed covers the answer for a directory git
// cannot read. Returning nothing is not "no opinion": internal/commitmsg reads
// an empty set as its own fallback, so an unreadable checkout refuses a
// fabricated approval instead of waving it through.
func TestCommitMsgReviewerSetFailsClosed(t *testing.T) {
	t.Parallel()

	if got := knownReviewers(t.Context(), t.TempDir()); got != nil {
		t.Fatalf("knownReviewers on a directory that is no repository = %v, want nil", got)
	}
	msg := writeMessage(t, `feat(x): a change approved by nobody

Reviewed-By: reviewer-that-does-not-exist
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "approved"}
`)
	code, _, stderr := runCommitMsg(t, "validate", msg, "--repo", t.TempDir())
	if code != 1 {
		t.Errorf("exit = %d, want 1: an unreadable skills directory must not be a free pass; stderr:\n%s", code, stderr)
	}
}

// TestCommitMsgCleanupModeResolutionOrder pins the precedence the shell
// validator had and the gate depends on. The gate pins COMMIT_MSG_CLEANUP to
// `verbatim` because it reads messages git has ALREADY STORED, and a
// `commit.cleanup` in a config file describes the NEXT commit rather than the
// one being judged.
//
// Not parallel: it sets an environment variable.
func TestCommitMsgCleanupModeResolutionOrder(t *testing.T) {
	repo := scratchRepo(t)

	t.Run("unset falls to the git commit -F default", func(t *testing.T) {
		t.Setenv("COMMIT_MSG_CLEANUP", "")
		mode, source := resolveCleanupMode(t.Context(), repo)
		if mode != commitmsg.CleanupWhitespace {
			t.Errorf("mode = %q, want %q", mode, commitmsg.CleanupWhitespace)
		}
		if !strings.Contains(source, "git commit -F") {
			t.Errorf("source = %q, want it to name the git commit -F default", source)
		}
	})

	t.Run("git config is read when the environment says nothing", func(t *testing.T) {
		t.Setenv("COMMIT_MSG_CLEANUP", "")
		gitIn(t, repo, "config", "commit.cleanup", "strip")
		mode, source := resolveCleanupMode(t.Context(), repo)
		if mode != commitmsg.CleanupStrip {
			t.Errorf("mode = %q, want %q", mode, commitmsg.CleanupStrip)
		}
		if source != "git config commit.cleanup" {
			t.Errorf("source = %q, want %q", source, "git config commit.cleanup")
		}

		// And the environment wins over it. This is the pin the gate relies on.
		t.Setenv("COMMIT_MSG_CLEANUP", "verbatim")
		if mode, source := resolveCleanupMode(t.Context(), repo); mode != commitmsg.CleanupVerbatim || source != "COMMIT_MSG_CLEANUP" {
			t.Errorf("with COMMIT_MSG_CLEANUP set: mode = %q source = %q, want verbatim from COMMIT_MSG_CLEANUP", mode, source)
		}

		gitIn(t, repo, "config", "--unset", "commit.cleanup")
	})

	t.Run("default means whitespace", func(t *testing.T) {
		t.Setenv("COMMIT_MSG_CLEANUP", "default")
		if mode, _ := resolveCleanupMode(t.Context(), repo); mode != commitmsg.CleanupWhitespace {
			t.Errorf("mode = %q, want %q: git's `default` means whitespace for every non-editor caller", mode, commitmsg.CleanupWhitespace)
		}
	})
}

// TestCommitMsgCommentPrefixFollowsGit pins the marker resolution. A hard-coded
// `#` is wrong in BOTH directions against a repo that sets either config key: it
// drops lines git stores, and it keeps lines git drops.
func TestCommitMsgCommentPrefixFollowsGit(t *testing.T) {
	t.Parallel()
	repo := scratchRepo(t)

	if got := resolveCommentPrefix(t.Context(), repo); got != "#" {
		t.Errorf("with nothing configured, prefix = %q, want %q", got, "#")
	}

	for _, tc := range []struct {
		key, value, want string
	}{
		{"core.commentChar", ";", ";"},
		// commentString wins over commentChar: it is the newer key and git
		// prefers it.
		{"core.commentString", "//", "//"},
		// `auto` asks git to pick a marker that begins NO line of the message,
		// so under it nothing in the author's text is a comment.
		{"core.commentString", "auto", ""},
	} {
		gitIn(t, repo, "config", tc.key, tc.value)
		if got := resolveCommentPrefix(t.Context(), repo); got != tc.want {
			t.Errorf("with %s=%q, prefix = %q, want %q", tc.key, tc.value, got, tc.want)
		}
	}
}

// TestCommitMsgArgumentErrors covers every way the command refuses to run. Each
// returns 1 rather than a distinct code, because the gate branches on
// pass-or-fail and a second failure code would be a distinction nothing reads.
func TestCommitMsgArgumentErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no verb", nil, "missing verb"},
		{"unknown verb", []string{"lint"}, "unrecognised verb"},
		{"no file", []string{"validate"}, "no commit-message file provided"},
		{"missing file", []string{"validate", "/nonexistent/commit-msg"}, "no commit-message file provided"},
		{"unknown flag", []string{"validate", "--strict"}, "unknown flag"},
		{"extra argument", []string{"validate", "a", "b"}, "unexpected extra argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, _, stderr := runCommitMsg(t, tc.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr missing %q:\n%s", tc.want, stderr)
			}
		})
	}
}

// TestCommitMsgHelpNamesTheSurvivingRules guards against the help text drifting
// back into describing the subject rules this migration deleted. An operator
// who reads a rule here and cannot make the gate enforce it has been sent on a
// hunt for a check that does not exist.
func TestCommitMsgHelpNamesTheSurvivingRules(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"--help"}, {"validate", "--help"}} {
		code, stdout, stderr := runCommitMsg(t, args...)
		if code != 0 {
			t.Errorf("%v: exit = %d, want 0", args, code)
		}
		if stderr != "" {
			t.Errorf("%v: help wrote to stderr: %q", args, stderr)
		}
		for _, want := range []string{
			"harmonik commit-msg validate <commit-msg-file>",
			"NOT_REVIEWED",
			"Trivial: true",
			"It does NOT check the shape of the subject.",
		} {
			if !strings.Contains(stdout, want) {
				t.Errorf("%v: help missing %q", args, want)
			}
		}
	}
}

// TestCommitMsgExplainNamesItsSources covers COMMIT_MSG_EXPLAIN, which is how a
// person finds out WHY the validator read their message the way it did. A
// resolution that cannot be inspected gets guessed at instead.
//
// Not parallel: it sets an environment variable.
func TestCommitMsgExplainNamesItsSources(t *testing.T) {
	repo := scratchRepo(t)
	t.Setenv("COMMIT_MSG_EXPLAIN", "1")
	t.Setenv("COMMIT_MSG_CLEANUP", "verbatim")

	code, _, stderr := runCommitMsg(t, "validate", writeMessage(t, cleanMessage), "--repo", repo)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"cleanup mode 'verbatim'", "COMMIT_MSG_CLEANUP", "comment marker '#'"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the explain line does not say %q:\n%s", want, stderr)
		}
	}
}

// TestCommitMsgRepoFlagPinsTheReviewerSet is the flag the gate passes, and the
// reason it passes it. Without --repo the command reads the WORKING DIRECTORY's
// repository, which on every scratch path is the repository under test rather
// than the one that ships the reviewer skills.
func TestCommitMsgRepoFlagPinsTheReviewerSet(t *testing.T) {
	t.Parallel()

	withSkills := scratchRepo(t)
	msg := writeMessage(t, `feat(x): a change a real reviewer approved

Reviewed-By: agent-reviewer
Review-Verdict: {"schema_version": 1, "verdict": "APPROVE", "flags": [], "notes": "ok"}
`)

	if code, _, stderr := runCommitMsg(t, "validate", msg, "--repo", withSkills); code != 0 {
		t.Errorf("pinned at the repository that ships agent-reviewer: exit = %d, want 0; stderr:\n%s", code, stderr)
	}
	// A directory with no reviewer skills falls back to DefaultReviewers, which
	// also holds agent-reviewer — so the fallback keeps the honest case working
	// rather than refusing every approval in a scratch checkout.
	if code, _, stderr := runCommitMsg(t, "validate", msg, "--repo", t.TempDir()); code != 0 {
		t.Errorf("pinned at a directory with no skills: exit = %d, want 0 through the fallback set; stderr:\n%s", code, stderr)
	}
	// The equals spelling has to work too; the gate could use either.
	if code, _, _ := runCommitMsg(t, "validate", msg, "--repo="+withSkills); code != 0 {
		t.Errorf("--repo=DIR: exit = %d, want 0", code)
	}
}
