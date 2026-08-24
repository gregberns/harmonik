package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gregberns/harmonik/internal/commitmsg"
)

// This file is the I/O half of the commit-message validator. The rules live in
// internal/commitmsg, which is pure — it reads no file and runs no git command,
// so every rule it enforces is decided by a table test. What is left over is
// the two things a rule cannot work out for itself, and both are git state:
// which reviewer names this repository HAS, and the cleanup mode git will
// apply to the message. Resolving those is this file's whole job.
//
// It replaces scripts/validate-commit-msg.sh. The subject half of that script —
// Conventional Commits, the 72-character ceiling, the trailing period — is not
// ported: it policed FORM rather than honesty. The refusal wording, the exit
// codes and the `validate-commit-msg:` prefix are kept, because
// scripts/commit-msg-gate.sh reads them and an operator greps for them.

func commitMsgUsage(w io.Writer) {
	say(w, "%s", `harmonik commit-msg — check what a commit message CLAIMS about its review

USAGE
  harmonik commit-msg validate <commit-msg-file> [--repo DIR]

VERBS
  validate  Read a commit-message file and refuse it if the review it claims
            does not hold up.

ARGUMENTS
  <commit-msg-file>  The message to read. This is a file on disk, not a commit:
                     pass the file you are about to hand 'git commit -F', or a
                     file you wrote with 'git log -1 --format=%B'.

FLAGS
  --repo DIR  The repository whose reviewer set and git config decide the two
              inputs below (default: the working directory's repository).

WHAT IT CHECKS
  One question: does the review this message claims hold up. A non-trivial
  commit carries 'Reviewed-By:' and 'Review-Verdict:'. The verdict is ONE
  schema-v1 JSON object with schema_version 1, a non-empty 'notes' and a known
  verdict word. An APPROVE or CLEAN names a reviewer this repository ships and
  nothing else, and carries 'flags'. A NOT_REVIEWED names none. No verdict is
  self-authored, and a BLOCK is never committed.

  'Trivial: true' on its own line stands the whole check aside, and so do a
  'fixup!' or 'squash!' subject and the one-line merge commit GitHub builds at
  refs/pull/N/merge.

  It does NOT check the shape of the subject. There is no type set, no length
  ceiling and no trailing-period rule; that half policed form rather than
  honesty and is gone.

  The honest no-reviewer form is one line each, and it passes:
    Reviewed-By: none — no reviewer was reached for this commit
    Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "no reviewer was reached for this commit"}

THE TWO GIT INPUTS
  Reviewer set   Every .claude/skills/*reviewer* directory that git TRACKS at
                 HEAD. An untracked directory beside the real ones cannot mint
                 a name this command trusts. An unreadable skills directory
                 falls back to the two names this repo has rather than to
                 "anything goes".
  Cleanup mode   COMMIT_MSG_CLEANUP, then 'git config commit.cleanup', then the
                 default for 'git commit -F'. Only 'strip' removes comment
                 lines; under every other mode a '# Reviewed-By:' line is
                 CONTENT that the audit grep counts. The comment marker is
                 'core.commentString', else 'core.commentChar', else '#'.

  COMMIT_MSG_EXPLAIN=1 prints which source each one came from, on stderr.

EXIT CODES
  0  The message may land.
  1  At least one problem, each numbered on its own line on stderr; or the
     message file is missing, or an argument is wrong.

EXAMPLES
  harmonik commit-msg validate "${TMPDIR:-/tmp}/commit-msg.txt"
  git log -1 --format=%B > /tmp/m && COMMIT_MSG_CLEANUP=verbatim harmonik commit-msg validate /tmp/m
`)
}

func runCommitMsgSubcommand(ctx context.Context, subArgs []string, stdout, stderr io.Writer) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}
	switch verb {
	case "--help", "-h":
		commitMsgUsage(stdout)
		return 0
	case "validate":
	case "":
		say(stderr, "harmonik commit-msg: missing verb; the only verb is 'validate'\n")
		return 1
	default:
		say(stderr, "harmonik commit-msg: unrecognised verb %q; the only verb is 'validate'\n", verb)
		return 1
	}
	return runCommitMsgValidate(ctx, subArgs[1:], stdout, stderr)
}

// missingMessageFile is the wording scripts/commit-msg-gate.sh and every
// operator who has run this by hand already know. It is kept from the shell
// validator, numbered the same way, so a caller that greps for it still finds
// it.
const missingMessageFile = "validate-commit-msg [1]: no commit-message file provided or file not found"

func runCommitMsgValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	msgPath := ""
	repoFlag := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			commitMsgUsage(stdout)
			return 0
		case arg == "--repo" && i+1 < len(args):
			i++
			repoFlag = args[i]
		case strings.HasPrefix(arg, "--repo="):
			repoFlag = strings.TrimPrefix(arg, "--repo=")
		case strings.HasPrefix(arg, "-"):
			say(stderr, "harmonik commit-msg validate: unknown flag %q\n", arg)
			return 1
		case msgPath == "":
			msgPath = arg
		default:
			say(stderr, "harmonik commit-msg validate: unexpected extra argument %q\n", arg)
			return 1
		}
	}

	if msgPath == "" {
		say(stderr, "%s\n", missingMessageFile)
		return 1
	}
	raw, err := os.ReadFile(msgPath)
	if err != nil {
		say(stderr, "%s\n", missingMessageFile)
		return 1
	}

	if repoFlag == "" {
		repoFlag = "."
	}
	opts, explain := resolveCommitMsgOptions(ctx, repoFlag)
	if os.Getenv("COMMIT_MSG_EXPLAIN") != "" {
		say(stderr, "%s\n", explain)
	}

	problems := commitmsg.Validate(string(raw), opts)
	if len(problems) == 0 {
		return 0
	}
	say(stderr, "%s", commitmsg.Render(problems))
	return 1
}

// resolveCommitMsgOptions reads the two inputs [commitmsg.Validate] cannot work
// out for itself, and returns the one-line account of where each came from.
//
// Nothing here can fail the command. Every probe below is a question git may
// decline to answer — the directory may not be a repository at all, which is
// the case a scratch checkout takes — and each one has a defined answer for
// "git said nothing". That is deliberate: a validator that refuses to run
// because it could not read a config file teaches the caller to skip it.
func resolveCommitMsgOptions(ctx context.Context, dir string) (opts commitmsg.Options, explain string) {
	cleanup, cleanupSource := resolveCleanupMode(ctx, dir)
	prefix := resolveCommentPrefix(ctx, dir)

	shown := prefix
	if shown == "" {
		shown = "<none>"
	}
	explain = fmt.Sprintf(
		"validate-commit-msg: cleanup mode '%s' from %s; comment marker '%s'",
		cleanup, cleanupSource, shown)

	return commitmsg.Options{
		KnownReviewers: knownReviewers(ctx, dir),
		Cleanup:        cleanup,
		CommentPrefix:  prefix,
	}, explain
}

func resolveCleanupMode(ctx context.Context, dir string) (mode commitmsg.CleanupMode, source string) {
	raw := os.Getenv("COMMIT_MSG_CLEANUP")
	source = "COMMIT_MSG_CLEANUP"
	if raw == "" {
		raw = gitConfig(ctx, dir, "commit.cleanup")
		source = "git config commit.cleanup"
	}
	// `default` is git's word for "strip if an editor was used, whitespace
	// otherwise". Every caller here is a non-editor one, so it means whitespace.
	if raw == "" || raw == "default" {
		return commitmsg.CleanupWhitespace, "the default for git commit -F, which this repo mandates"
	}
	return commitmsg.CleanupMode(raw), source
}

// resolveCommentPrefix returns the marker git treats as beginning a comment.
// A hard-coded `#` is wrong in BOTH directions against a repo that sets either
// config key: it drops lines git stores, and it keeps lines git drops.
func resolveCommentPrefix(ctx context.Context, dir string) string {
	prefix := gitConfig(ctx, dir, "core.commentString")
	if prefix == "" {
		prefix = gitConfig(ctx, dir, "core.commentChar")
	}
	if prefix == "" {
		return "#"
	}
	// `auto` asks git to pick a marker that begins NO line of the message, so
	// under it no line of the author's own text is a comment and stripping any
	// would delete text git stores. An empty marker strips nothing.
	if prefix == "auto" {
		return ""
	}
	return prefix
}

// reviewerName is the shape a reviewer skill directory has to have. Without it
// a directory named with glob metacharacters walks into the pathspec below, and
// a git pathspec GLOBS.
var reviewerName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// knownReviewers names the reviewer identities an APPROVE or CLEAN may claim.
//
// Read off the repository rather than hard-coded, so adding a reviewer skill is
// enough and this list cannot rot behind it. It reads HEAD and not the index:
// `git add` alone must not mint a trusted reviewer, and neither must an
// untracked `mkdir` beside the real skills.
//
// An empty answer is returned as nil, which [commitmsg.Options] documents as
// "use the fallback set". That is the fail-CLOSED direction and it is the
// point: an unreadable skills directory must not turn into a free pass for
// every APPROVE, and it must not refuse every APPROVE either.
func knownReviewers(ctx context.Context, dir string) []string {
	root := gitTopLevel(ctx, dir)
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, ".claude", "skills", "*reviewer*"))
	if err != nil {
		return nil
	}

	found := make([]string, 0, len(matches))
	for _, match := range matches {
		info, statErr := os.Stat(match)
		if statErr != nil || !info.IsDir() {
			continue
		}
		name := filepath.Base(match)
		if !reviewerName.MatchString(name) {
			continue
		}
		if !gitTracksAtHead(ctx, root, ".claude/skills/"+name+"/SKILL.md") {
			continue
		}
		found = append(found, name)
	}
	if len(found) == 0 {
		// nil, not an empty slice: commitmsg.Options documents an empty
		// KnownReviewers as "use the fallback set", and a caller that tested
		// `== nil` would read a zero-length slice as an answer.
		return nil
	}
	return found
}

func gitTopLevel(ctx context.Context, dir string) string {
	return gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
}

func gitConfig(ctx context.Context, dir, key string) string {
	return gitOutput(ctx, dir, "config", "--get", key)
}

func gitTracksAtHead(ctx context.Context, root, path string) bool {
	// #nosec G204 -- the executable is the literal "git"; root is the caller's
	// repository path and path is built from a directory name this file has
	// already held to reviewerName, so neither reaches a shell.
	cmd := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-e", "HEAD:"+path)
	return cmd.Run() == nil
}

// gitOutput runs one read-only git command and returns its trimmed stdout, or
// the empty string when git could not answer. An error is not distinguished
// from an empty answer on purpose — every caller treats both the same way, and
// giving them separate paths would only invent a difference the callers do not
// have.
func gitOutput(ctx context.Context, dir string, args ...string) string {
	// #nosec G204 -- the executable is the literal "git" and every args element
	// is a constant written in this file. Only dir comes from outside, and it
	// is one argument to git, not a command line.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
