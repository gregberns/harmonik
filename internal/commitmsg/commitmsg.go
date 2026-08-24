package commitmsg

import (
	"regexp"
	"strings"
)

// DefaultReviewers is the reviewer set to use when a caller supplies none.
//
// It is a fallback, not a default policy: the caller is expected to read the
// real list off the repository. But an empty list cannot be treated as "no
// opinion" — that turns an unreadable skills directory into a free pass for
// every APPROVE — and it cannot be compared against either, because an empty
// list matches nothing and would refuse every APPROVE. Neither is right, so an
// empty list falls back to the two names this repository has.
var DefaultReviewers = []string{"agent-reviewer", "agent-config-reviewer"}

// CleanupMode is git's `commit.cleanup` setting, which decides whether a
// comment line is part of the message git will store.
//
// Only [CleanupStrip] removes comment lines. Under every other mode a line
// beginning with the comment marker is CONTENT: git stores it, and a fabricated
// `# Reviewed-By: agent-reviewer` hidden behind one is counted by the audit
// grep, so this package has to read it too.
type CleanupMode string

// The cleanup modes this package tells apart. Anything else — including git's
// own `default`, and the empty string — behaves like [CleanupWhitespace],
// because the only distinction that matters here is whether comment lines are
// dropped.
const (
	CleanupWhitespace CleanupMode = "whitespace"
	CleanupVerbatim   CleanupMode = "verbatim"
	CleanupStrip      CleanupMode = "strip"
)

// Options carries the two things [Validate] cannot work out from the message.
//
// Both are git state. Reading them is I/O and belongs to the caller; deciding
// what they mean is a rule and belongs here.
type Options struct {
	// KnownReviewers names the reviewer identities an APPROVE or CLEAN may
	// claim, and the names a NOT_REVIEWED may not. The caller derives them
	// from what git TRACKS — an untracked directory beside the real reviewer
	// skills must not mint a name this validator trusts. Empty means
	// [DefaultReviewers].
	KnownReviewers []string

	// Cleanup is the mode git will apply to this message. The zero value
	// behaves like [CleanupWhitespace], which is what `git commit -F` gets.
	Cleanup CleanupMode

	// CommentPrefix is the marker git treats as beginning a comment line —
	// `core.commentString`, else `core.commentChar`, else `#`. It is read only
	// under [CleanupStrip]. Empty means nothing is a comment, which is what
	// git's `auto` guarantees: `auto` picks a marker that begins NO line of the
	// message, so under it no line of the author's text is a comment and
	// stripping any would delete text git stores.
	CommentPrefix string
}

// Check names the rule that raised a [Problem]. Tests assert the rule that
// fired rather than matching prose, so rewording a message cannot silently
// move a case onto a different rule.
type Check int

// The rules this package enforces.
const (
	CheckReviewedByPresent Check = iota + 1
	CheckReviewVerdictPresent
	CheckVerdictJSON
	CheckSchemaVersion
	CheckNotes
	CheckVerdictValue
	CheckApprovalIdentity
	CheckApprovalFlags
	CheckSelfAuthored
	CheckNoReviewerNamed
	CheckBlockNeverCommitted
)

// Problem is one refusal. Lines carries the refusal as it is meant to be read:
// Lines[0] states what is wrong and the rest say what to write instead. Every
// line is numbered separately by [Render], which is how the shell validator
// this replaces printed them.
type Problem struct {
	Check Check
	Lines []string
}

// Render writes the problems as the validator's stderr. It returns the empty
// string when there are none, so a caller can test the output rather than
// remembering to test the slice.
func Render(problems []Problem) string {
	if len(problems) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("validate-commit-msg: validation failed:\n")
	n := 0
	for _, p := range problems {
		for _, line := range p.Lines {
			n++
			b.WriteString("  [")
			b.WriteString(itoa(n))
			b.WriteString("] ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// syntheticMergeSubject is the subject GitHub's merge machinery writes for
// `refs/pull/N/merge`: the word Merge, two full 40-character object names, and
// ` into ` between them. It is deliberately exact. `git merge` writes
// `Merge branch '<name>'` and never a bare pair of hashes, so a real lane merge
// does not match and still answers for its content.
var syntheticMergeSubject = regexp.MustCompile(`^Merge [0-9a-f]{40} into [0-9a-f]{40}$`)

// Validate reports every review-trailer problem in msg. It is pure: same
// message and same options, same answer, on any machine.
//
// An empty result means the message may land. Problems come back in the order
// a reader should work through them, and all of them come back — a validator
// that stops at the first one makes the author run it once per mistake.
func Validate(msg string, opts Options) []Problem {
	stripped := strippedMessage(msg, opts)
	subject := subjectOf(stripped)

	if isExempt(stripped, subject) {
		return nil
	}
	return trailerProblems(stripped, opts)
}

// isExempt reports the three ways a commit carries no reviewer claim to check.
func isExempt(stripped, subject string) bool {
	// `Trivial: true`, anchored and exact, is the author saying this is a typo
	// or a whitespace fix.
	for _, line := range strings.Split(stripped, "\n") {
		if line == "Trivial: true" {
			return true
		}
	}

	// A `fixup!` / `squash!` commit is scratch. `git rebase --autosquash` folds
	// it into another commit and it never lands under this subject, so there is
	// no landed change for a reviewer to be quoted about.
	if strings.HasPrefix(subject, "fixup! ") || strings.HasPrefix(subject, "squash! ") {
		return true
	}

	return isSyntheticPullRequestMerge(stripped, subject)
}

// isSyntheticPullRequestMerge reports the ONE merge nobody writes.
//
// On a `pull_request` event GitHub computes `refs/pull/N/merge` — the PR head
// merged into the base — and `actions/checkout` checks THAT commit out, so it
// is the HEAD this gate reads in CI. GitHub's merge machinery writes its
// message: one line, no body, belonging to no branch, seen by no author, and
// amendable by no command. Held to the trailer rules it fails for a trailer
// that cannot be added, which refuses every pull request whose base is not
// already an ancestor of its head, whatever the code says.
//
// The exemption is the exact shape and no wider. An ordinary
// `Merge branch 'work/x' into main` lands and answers for its content; so does
// a hash-pair subject that carries a body, because the body is the thing
// GitHub's merge never has and a hand-written smuggle always does.
func isSyntheticPullRequestMerge(stripped, subject string) bool {
	if !syntheticMergeSubject.MatchString(subject) {
		return false
	}
	for _, line := range strings.Split(stripped, "\n") {
		if line == subject {
			continue
		}
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}

// strippedMessage returns the message the way GIT WILL STORE IT.
func strippedMessage(msg string, opts Options) string {
	lines := strings.Split(msg, "\n")
	if opts.Cleanup == CleanupStrip && opts.CommentPrefix != "" {
		kept := make([]string, 0, len(lines))
		for _, line := range lines {
			// Git's own test: a line is a comment when it BEGINS with the
			// marker, with no leading whitespace allowed. A prefix test, not a
			// pattern match — the marker is whatever git config says, and `;`,
			// `|`, `$` and `//` all mean something else to a regexp engine.
			if strings.HasPrefix(line, opts.CommentPrefix) {
				continue
			}
			kept = append(kept, line)
		}
		lines = kept
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// subjectOf returns the first line holding anything but spaces and tabs.
func subjectOf(stripped string) string {
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Trim(line, " \t") != "" {
			return line
		}
	}
	return ""
}

// trailerLines returns the lines that ARE trailers with the named key.
//
// A TRAILER IS A LINE THAT STARTS AT COLUMN ZERO. That one definition decides
// every rule below, and it is the whole reason this package can tell a claim
// from a mention. One leading space, a comment marker, or a sentence in front
// of the key, and the line is prose: it makes no claim, no reader counts it,
// and refusing it would only teach authors to stop writing about the rules.
func trailerLines(stripped, key string) []string {
	var out []string
	for _, line := range strings.Split(stripped, "\n") {
		if strings.HasPrefix(line, key) {
			out = append(out, line)
		}
	}
	return out
}

// trailerProblems runs the trailer rules over a commit that is not exempt.
func trailerProblems(stripped string, opts Options) []Problem {
	var problems []Problem

	reviewedByLines := trailerLines(stripped, "Reviewed-By:")
	verdictLines := trailerLines(stripped, "Review-Verdict:")

	// Joined, because that is what the approval rules are written against:
	// two `Reviewed-By:` lines can never equal one reviewer name, which is how
	// a duplicate trailer is refused without a rule of its own.
	reviewedBy := strings.Join(reviewedByLines, "\n")
	verdictLine := strings.Join(verdictLines, "\n")

	if len(reviewedByLines) == 0 {
		problems = append(problems, Problem{Check: CheckReviewedByPresent, Lines: []string{
			"missing required trailer 'Reviewed-By:' on a non-trivial commit.",
			"  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes.",
		}})
	}
	if len(verdictLines) == 0 {
		problems = append(problems, Problem{Check: CheckReviewVerdictPresent, Lines: []string{
			"missing required trailer 'Review-Verdict:' on a non-trivial commit.",
			"  Add 'Trivial: true' trailer to bypass for typos/whitespace fixes.",
		}})
		return problems
	}

	return append(problems, verdictProblems(reviewedByLines, reviewedBy, verdictLine, opts)...)
}

// reviewers returns the names an approval may claim.
func reviewers(opts Options) []string {
	if len(opts.KnownReviewers) == 0 {
		return DefaultReviewers
	}
	return opts.KnownReviewers
}

// reviewedByValue is the `Reviewed-By:` text with the key and the surrounding
// space removed.
func reviewedByValue(reviewedBy string) string {
	return strings.TrimSpace(strings.TrimPrefix(reviewedBy, "Reviewed-By:"))
}

// selfToken finds the word `self` in a value, case-folded, with a non-letter on
// each side. It is a word match and not proof: it catches the author who says
// plainly that the approval is their own. `myself` does NOT match it — the
// letter before `self` is a letter — and that gap is closed by the exact-name
// rule on an approval, not by widening this one.
var selfToken = regexp.MustCompile(`(?im)(^|[^a-zA-Z])self([^a-zA-Z]|$)`)

// notSelfAuthored appends the refusal when a verdict claiming a reviewer names
// the author. It reports whether the value passed.
func notSelfAuthored(problems []Problem, verdict, reviewedBy string) ([]Problem, bool) {
	value := reviewedByValue(reviewedBy)
	if !selfToken.MatchString(value) {
		return problems, true
	}
	return append(problems, Problem{Check: CheckSelfAuthored, Lines: []string{
		"the " + verdict + " verdict must not be self-authored (Reviewed-By: " + value + ").",
		"  The rule is: quote the reviewer's verdict, never author your own.",
		`  If no reviewer was reached, record that instead: "verdict": "NOT_REVIEWED".`,
	}}), false
}

// approvalIdentity is the extra bar an APPROVE or CLEAN clears: the whole
// `Reviewed-By:` value must be EXACTLY the name of a reviewer this repository
// has. That proves the name is a real reviewer. It cannot prove one ran.
//
// A qualifier after the name is refused. What the qualifier bought was a span
// of free text that no rule read, sitting on the one line the approval rules
// are written about, so the rules could be answered and defeated in the same
// breath. The context has somewhere better to go: the verdict's own `notes`.
func approvalIdentity(problems []Problem, verdict, reviewedBy string, opts Options) []Problem {
	value := reviewedByValue(reviewedBy)
	if value == "" {
		return append(problems, Problem{Check: CheckApprovalIdentity, Lines: []string{
			"the " + verdict + " verdict needs a 'Reviewed-By:' value naming the reviewer that ran.",
		}})
	}

	problems, ok := notSelfAuthored(problems, verdict, reviewedBy)
	if !ok {
		return problems
	}

	known := reviewers(opts)
	for _, name := range known {
		if strings.EqualFold(value, name) {
			return problems
		}
	}

	return append(problems, Problem{Check: CheckApprovalIdentity, Lines: []string{
		"the " + verdict + " verdict must name a reviewer skill this repo has; got '" + value + "'.",
		"  Known reviewers: " + strings.Join(known, " ") + " ",
		"  Write the name alone. A qualifier after it is no longer accepted:",
		`  put which harness ran, or when, in the verdict's own "notes" field.`,
		`  If no reviewer was reached, record that instead: "verdict": "NOT_REVIEWED".`,
	}})
}

// noReviewerNamed is the bar a NOT_REVIEWED verdict clears. The verdict says
// no reviewer was reached, so no `Reviewed-By:` TRAILER may name one.
//
// IT READS TRAILER LINES ONLY, and that is the rule, not an optimisation. This
// check used to search the whole message unanchored, with a leading pad in
// front of the folded haystack so that a name at index 0 still had a boundary
// character before it. Both existed to mimic an audit spelled
// `git log --grep 'Reviewed-By: agent-reviewer'` — an UNANCHORED substring
// search, which counts an indented line, a commented-out line, and a sentence
// of prose that happens to put the name after the key. So did this rule, and
// the cost was real: a docs commit that quoted the trailer format as an
// example, while honestly recording that no reviewer was reached, was refused
// for the example.
//
// The audit is anchored now — `git log --grep '^Reviewed-By: agent-reviewer'`
// — and this rule is anchored with it. Fixing the definition on both sides is
// what makes the relaxation safe: an indented or commented-out line claims
// nothing to the validator AND is counted by nothing, so the two agree. See
// the package doc for the audit form.
//
// Inside a trailer's value the boundary stays asymmetric, because the audit
// still is. `agent-reviewer2` answers `^Reviewed-By: agent-reviewer` and is
// refused; `xxagent-reviewer` does not answer it and is accepted, because
// refusing a name that makes no claim is a false fail on the one sentence this
// repo asks an author to write.
func noReviewerNamed(problems []Problem, reviewedByLines []string, opts Options) []Problem {
	known := reviewers(opts)
	matched := ""

	for _, line := range reviewedByLines {
		// A leading space, so a name at the START of the value has a boundary
		// character before it. Here that pad is honest rather than a mimicry
		// hack: `Reviewed-By: ` really does sit in front of the value, and a
		// name at the start of the value is exactly what the anchored audit
		// counts.
		value := " " + strings.ToLower(reviewedByValue(line))
		for _, name := range known {
			if hasBoundedName(value, strings.ToLower(name)) {
				matched = name
				break
			}
		}
		if matched != "" {
			break
		}
	}
	if matched == "" {
		return problems
	}

	lines := []string{
		"the NOT_REVIEWED verdict must not name a reviewer this repo has; found '" + matched + "'.",
	}
	// Quote the author's own lines back, exactly as they wrote them. The
	// refusal used to print a flattened haystack, which is a sentence that is
	// in no message anywhere and that an author cannot search their file for.
	for _, line := range reviewedByLines {
		lines = append(lines, "  "+line)
	}
	lines = append(lines,
		"  NOT_REVIEWED says no reviewer was reached. Naming one contradicts it,",
		"  and an audit that greps the history for a reviewer name counts it as reviewed.",
		"  Write the honest form instead, on ONE Reviewed-By line:",
		"    Reviewed-By: none — no reviewer was reached for this commit",
		"  If the reviewer DID run and declined, the verdict is REQUEST_CHANGES.",
	)
	return append(problems, Problem{Check: CheckNoReviewerNamed, Lines: lines})
}

// hasBoundedName reports whether name occurs in hay preceded by a character
// that is not a lowercase letter or a digit. Nothing is required after it.
func hasBoundedName(hay, name string) bool {
	if name == "" {
		return false
	}
	for at := 0; ; {
		i := strings.Index(hay[at:], name)
		if i < 0 {
			return false
		}
		i += at
		if i > 0 && !isLowerAlnum(hay[i-1]) {
			return true
		}
		at = i + 1
	}
}

func isLowerAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
