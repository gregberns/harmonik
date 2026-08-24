// Package commitmsg decides whether a commit message may land, on one question
// only: does the review it claims hold up.
//
// The rule it enforces is the one an audit depends on. `git log --grep
// 'Reviewed-By: agent-reviewer'` is how anybody asks this history which commits
// a reviewer read, and that grep believes whatever the message says. So an
// APPROVE has to name a reviewer this repo actually ships, a NOT_REVIEWED may
// not name one at all, and neither may be authored by the person the review is
// about. The honest sentence — no reviewer was reached — stays the cheapest
// thing to write, because a gate that makes the true statement expensive
// teaches authors to write the false one.
//
// It replaces the trailer half of scripts/validate-commit-msg.sh. The subject
// half — Conventional Commits, the 72-character ceiling, the trailing period —
// is deliberately absent: that half policed FORM rather than honesty, and it is
// not ported.
//
// WHAT COUNTS AS A TRAILER, AND THE AUDIT THAT AGREES WITH IT. A trailer is a
// line that starts at column zero with the key. Nothing else is one: not a line
// with a space in front of it, not one behind a comment marker, not a sentence
// that happens to put a reviewer name after the key. Those are prose, they
// claim nothing, and this package leaves them alone — which is what lets an
// author write about the rules, or quote the trailer format as an example,
// without the gate reading the example as a claim.
//
// The audit that asks this history which commits a reviewer read has to be
// spelled the same way, or the two disagree and one of them is wrong. It is
// ANCHORED:
//
//	git log --grep='^Reviewed-By: agent-reviewer'
//	git log --grep='^Reviewed-By: agent-config-reviewer'
//
// Drop the `^` and the grep counts every indented, commented-out and quoted
// mention in the history, none of which is a claim. The anchor is the whole
// agreement: a line this package refuses is a line that grep counts, and a line
// it accepts is a line that grep walks past.
//
// [Validate] is pure. It reads no file, runs no git command, and reaches no
// clock, so every rule below is decided by a table test. The two things it
// cannot work out for itself arrive in [Options]: which reviewer names this
// repository has (git has to answer that, by reading what is tracked at HEAD),
// and the cleanup mode git will apply to the message (which decides whether a
// comment line is content). Resolving both is the caller's job.
//
// The policy is owned by docs/foundation/project-level/build-practices.md.
package commitmsg
