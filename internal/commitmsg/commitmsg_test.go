package commitmsg

import (
	"sort"
	"strings"
	"testing"
)

// A case states a message, the options a caller would have resolved for it, and
// the EXACT set of rules that must fire. Naming the set and not just "it
// failed" is what keeps a case honest: a message refused for the wrong reason
// is a green test that proves nothing, and several cases below exist only
// because one missing field used to produce three complaints about fields that
// were fine.
type validateCase struct {
	name string
	msg  string
	opts Options
	want []Check // empty means the message must be accepted
	// text, when set, must appear in the rendered refusal. Used where the
	// WORDING is the thing under test, not just which rule fired.
	text string
}

const approveTrailers = "\n\n" +
	"Reviewed-By: agent-reviewer\n" +
	`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}`

// notReviewedMsg is an honest no-reviewer commit with the Reviewed-By value
// under test.
func notReviewedMsg(value string) string {
	return "fix(gate): stop a stranded item from reading as drained\n\n" +
		"Reviewed-By: " + value + "\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`
}

// payloadMsg puts an adversarial verdict payload on an otherwise ordinary
// commit, so the payload is the only thing that can decide the outcome.
func payloadMsg(payload string) string {
	return "fix(gate): an adversarial verdict payload\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		"Review-Verdict: " + payload
}

// hashFabrication hides a fabricated approval behind a comment marker. Under
// every cleanup mode but `strip` git STORES that line, so the audit grep counts
// it and this validator has to see it too.
func hashFabrication(marker string) string {
	return "fix(gate): a fabricated approval hidden behind a comment line\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		marker + " Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`
}

// githubMergeSubject is a real `refs/pull/N/merge` subject, copied off a public
// repository rather than guessed at.
const githubMergeSubject = "Merge 95129b3e317a4c990df6b55268a17bd172addab1 into 62e3e719a483d9e2b9be2b7dba7e2bb4a7f91c8c"

// ─── the honest form, and the bar an approval clears ────────────────────────

var honestFormCases = []validateCase{{
	name: "the honest no-reviewer form is accepted",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reachable from this session."}`,
}, {
	name: "the honest form needs neither a reviewer name nor a flags key",
	msg: "docs(specs): record the gap the guard left open\n\n" +
		"Reviewed-By: none\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "notes": "Documents only. No reviewer was reached."}`,
}, {
	name: "a well-formed APPROVE is accepted",
	msg:  "feat(s04): add the claude-twin handler adapter" + approveTrailers,
}, {
	name: "a CLEAN verdict from the config reviewer is accepted",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: agent-config-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"CLEAN","flags":[],"notes":"No drift.","proposed_diff":""}`,
}, {
	name: "an APPROVE naming a reviewer this repo does not have is refused",
	msg: "feat(queue): chain the claim through\n\n" +
		"Reviewed-By: implement_claim_chain\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks right."}`,
	want: []Check{CheckApprovalIdentity},
	text: "must name a reviewer skill this repo has",
}, {
	name: "a CLEAN naming a reviewer this repo does not have is refused",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: decompose_queue_specs\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"CLEAN","flags":[],"notes":"No drift."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	name: "a qualifier after a real reviewer name is refused",
	msg: "fix(daemon): honor run_id in the worktree path\n\n" +
		"Reviewed-By: agent-reviewer (codex harness, fresh context)\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	name: "a qualifier cannot smuggle in a reviewer this repo does not have",
	msg: "fix(daemon): honor run_id in the worktree path\n\n" +
		"Reviewed-By: agent-reviewer (Kierkegaard)\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	// The obvious answer is wrong and the case says so: the self-authorship
	// probe is a word match on `self`, and `myself` does not match it, because
	// the letter before `self` is a letter. What refuses this is the exact-name
	// rule. Assert the rule and not only the refusal, or the case reads as
	// evidence for a check that never ran.
	name: "an author claiming their own approval in a qualifier is refused by the NAME rule",
	msg: "fix(daemon): honor run_id in the worktree path\n\n" +
		"Reviewed-By: agent-reviewer (myself)\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	name: "a bare reviewer name is the accepted form",
	msg: "fix(daemon): honor run_id in the worktree path\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked."}`,
}, {
	name: "a reviewer name is matched case-folded",
	msg: "fix(daemon): honor run_id in the worktree path\n\n" +
		"Reviewed-By: AGENT-REVIEWER\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Checked."}`,
}, {
	name: "a self-authored APPROVE is refused",
	msg: "refactor(keeper): fold the two threshold paths together\n\n" +
		"Reviewed-By: self\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Read it twice."}`,
	want: []Check{CheckSelfAuthored},
	text: "must not be self-authored",
}, {
	name: "an APPROVE with an empty Reviewed-By value is refused",
	msg: "feat(cli): add the promote subcommand\n\n" +
		"Reviewed-By:\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"A real review."}`,
	want: []Check{CheckApprovalIdentity},
	text: "needs a 'Reviewed-By:' value naming the reviewer that ran",
}, {
	name: "an APPROVE with no flags key is refused",
	msg: "feat(cli): add the promote subcommand\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","notes":"All checks pass."}`,
	want: []Check{CheckApprovalFlags},
	text: "must carry the 'flags' key",
}, {
	name: "a tab inside notes cannot hide a missing flags key",
	msg: "feat(queue): chain the claim through\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","notes":"one\tarray\tok\tarray\ttwo"}`,
	want: []Check{CheckApprovalFlags},
}, {
	name: "a BLOCK verdict is never committed",
	msg: "feat(s04): add the adapter\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"BLOCK","flags":["spec-divergence"],"notes":"Return a typed alias."}`,
	want: []Check{CheckBlockNeverCommitted},
	text: "BLOCK verdict must not be committed",
}, {
	name: "an unknown verdict word is refused",
	msg: "feat(s04): add the adapter\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"LGTM","flags":[],"notes":"Fine."}`,
	want: []Check{CheckVerdictValue},
	text: "unknown verdict value 'LGTM'",
}}

// ─── the verdicts that are NOT an approval ──────────────────────────────────
//
// These land, and every rule that makes one more expensive to write pushes the
// author back toward APPROVE. The only thing checked on this arm is that the
// author is not the reviewer.

var nonApprovingCases = []validateCase{{
	name: "REQUEST_CHANGES may name a reviewer outside the known set",
	msg: "fix(workspace): widen the verdict read retry\n\n" +
		"Reviewed-By: Codex independent review\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers it."}`,
}, {
	name: "REQUEST_CHANGES naming the real reviewer is truthful and accepted",
	msg: "fix(workspace): widen the verdict read retry\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers it."}`,
}, {
	name: "a non-approving verdict needs no flags key",
	msg: "fix(workspace): widen the verdict read retry\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","notes":"No test covers it."}`,
}, {
	name: "DRIFT_MINOR naming the real config reviewer is accepted",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: agent-config-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MINOR","flags":["skill-registry-drift"],"notes":"One line differs.","proposed_diff":"-a\n+b"}`,
}, {
	name: "DRIFT_MAJOR naming the real config reviewer is accepted",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: agent-config-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["enforced-config-drift"],"notes":"They disagree.","proposed_diff":"-a\n+b"}`,
}, {
	// The other side of the APPROVE|CLEAN boundary. The same name refused under
	// CLEAN must pass here: a reviewer who declined has not been minted as an
	// approver. This reddens the moment the identity rule widens onto this arm.
	name: "DRIFT_MAJOR may name a reviewer outside the known set",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: decompose_queue_specs\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["enforced-config-drift"],"notes":"They disagree.","proposed_diff":"-a\n+b"}`,
}, {
	name: "a self-authored REQUEST_CHANGES is refused",
	msg: "fix(workspace): widen the verdict read retry\n\n" +
		"Reviewed-By: self\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"No test covers it."}`,
	want: []Check{CheckSelfAuthored},
}, {
	name: "a self-review dressed as a harness name is refused on a DRIFT verdict",
	msg: "chore(agents): sync the skill registry\n\n" +
		"Reviewed-By: Codex self-review\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"DRIFT_MAJOR","flags":["skill-registry-drift"],"notes":"They disagree."}`,
	want: []Check{CheckSelfAuthored},
}, {
	// The control for the probe's narrowness. `myself` is not the word `self`,
	// and on this arm nothing else catches it — which is exactly why the
	// exact-name rule exists on the approval arm and not here.
	name: "myself is not the word self and passes the probe on a non-approving verdict",
	msg: "fix(workspace): widen the verdict read retry\n\n" +
		"Reviewed-By: reviewed by myself and a colleague\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"REQUEST_CHANGES","notes":"No test covers it."}`,
}}

// ─── NOT_REVIEWED may not name a reviewer ───────────────────────────────────
//
// The cost of a contradiction here is not the contradiction. It is that
// `git log --grep 'Reviewed-By: agent-reviewer'` counts the commit as reviewed
// work, so the whole audit stops meaning anything. The test to apply to any
// change in this block is not "is this value a reviewer name" — it is "would
// somebody grepping the history count this line".

var notReviewedCases = []validateCase{{
	name: "a NOT_REVIEWED verdict naming the real reviewer is a contradiction",
	msg:  notReviewedMsg("agent-reviewer"),
	want: []Check{CheckNoReviewerNamed},
	text: "must not name a reviewer this repo has",
}, {
	name: "a NOT_REVIEWED verdict naming the reviewer with a note after it is refused",
	msg:  notReviewedMsg("agent-reviewer (unreachable this session)"),
	want: []Check{CheckNoReviewerNamed},
}, {
	name: "a NOT_REVIEWED verdict naming the config reviewer is refused too",
	msg:  notReviewedMsg("agent-config-reviewer"),
	want: []Check{CheckNoReviewerNamed},
}, {
	name: "the refusal quotes the author's own Reviewed-By line back",
	msg: "fix(gate): a second trailer names a reviewer the verdict denies\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
	want: []Check{CheckNoReviewerNamed},
	text: "  Reviewed-By: none — no reviewer was reached for this commit",
}, {
	name: "a duplicate Reviewed-By line cannot smuggle the reviewer in before the honest form",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
	want: []Check{CheckNoReviewerNamed},
}, {
	// The control for the choice of fix. The contradiction check re-derives its
	// own haystack instead of widening the shared capture; widen the shared one
	// and this APPROVE holds two lines, fails the exact-name rule, and is
	// refused for quoting the rule in its own prose.
	name: "an APPROVE whose body quotes the trailer key in prose is accepted",
	msg: "docs(gate): explain what the trailer rule asks for\n\n" +
		"The rule says a landed commit carries a Reviewed-By: line naming the reviewer\n" +
		"that ran, and a Review-Verdict: line quoting what it said.\n\n" +
		"Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Documentation only."}`,
}}

// ─── a trailer is a line that starts at column zero ─────────────────────────
//
// These pin the DEFINITION, and they are the cases that used to go the other
// way. The contradiction rule searched the whole message unanchored, because
// the audit it answered to was spelled `git log --grep 'Reviewed-By: <name>'`
// — a substring search that counts an indented line, a commented-out line and
// a sentence of prose. Both sides are anchored now
// (`--grep '^Reviewed-By: <name>'`), so a line this package accepts is a line
// that grep walks past, and the relaxation costs no fabrication.
//
// Every case below carries an HONEST NOT_REVIEWED verdict and must pass. If a
// future change reintroduces the unanchored scan, all six go red at once.

var anchoredTrailerCases = []validateCase{{
	// The case the old rule got wrong, and the reason the definition changed:
	// a docs commit teaching the trailer format was refused for its own
	// example while recording, truthfully, that no reviewer was reached.
	name: "a docs commit may quote the trailer format as an indented example",
	msg: "docs(gate): show an implementer what the trailer looks like\n\n" +
		"Write the two lines like this, at the start of their own line:\n\n" +
		"    Reviewed-By: agent-reviewer\n" +
		`    Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"..."}` + "\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "Documents only. No reviewer was reached."}`,
}, {
	name: "one space of indentation is enough to make a line prose",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		"  Reviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
}, {
	name: "a tab-indented mention is prose too",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		"\tReviewed-By: agent-reviewer\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
}, {
	name: "an indented mention before the honest trailer is prose in that order too",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"  Reviewed-By: agent-reviewer\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
}, {
	name: "prose that puts the reviewer name after the key mid-sentence is prose",
	msg: "fix(gate): stop a stranded item from reading as drained\n\n" +
		"I tried to get Reviewed-By: agent-reviewer onto this commit and could not.\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
}, {
	// The old rule folded every Reviewed-By-bearing line into one haystack and
	// padded the front, so a name sitting at index 0 was refused. The bash
	// suite that pinned it said outright that this shape answers no audit and
	// was kept for positional consistency alone.
	name: "a reviewer name in front of the key is prose, not a trailer",
	msg: "fix(scope): a perfectly ordinary subject\n\n" +
		"Body text here.\n\n" +
		"agent-reviewer Reviewed-By: none\n" +
		"Reviewed-By: none — no reviewer was reached\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": ["no-reviewer-reached"], "notes": "No reviewer was reached."}`,
}, {
	// THE CONTROL, and the whole block is worthless without it. A real trailer
	// line still claims a review, and NOT_REVIEWED still contradicts it.
	name: "a real trailer line naming the reviewer is still refused under NOT_REVIEWED",
	msg:  notReviewedMsg("agent-reviewer"),
	want: []Check{CheckNoReviewerNamed},
}, {
	// The second control. A fence does not move the column, so an example left
	// at column zero inside one IS a trailer, IS counted by the anchored audit,
	// and IS refused. Indent it and it becomes prose. Say so, or a reader
	// concludes that "inside a fence" is the exemption.
	name: "a fenced example left at column zero is still a trailer and is refused",
	msg: "docs(gate): show the trailer format without indenting it\n\n" +
		"```\n" +
		"Reviewed-By: agent-reviewer\n" +
		"```\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "Documents only."}`,
	want: []Check{CheckNoReviewerNamed},
}}

// The decoration set. Each of these exited 0 against an equality comparison,
// and each puts a real reviewer name inside a Reviewed-By line.
var decoratedReviewerNames = []string{
	"agent-reviewer.",
	"agent-reviewer,",
	",agent-reviewer",
	"agent-reviewer — never ran",
	"agent-reviewer was not reachable",
	"agent-reviewer [unreachable]",
	"agent-reviewer (codex) (unreachable)",
	"(agent-reviewer)",
	"AGENT-REVIEWER",
	"none (agent-reviewer down)",
	"not agent-reviewer",
	"x-agent-reviewer",
}

// The suffix set. Every one of these answers
// `git log --grep 'Reviewed-By: agent-reviewer'`, which is why no boundary
// character is required AFTER the name. The last answers the SECOND reviewer's
// audit, which is how the rule says it is per-name and not per-this-one-name.
var suffixedReviewerNames = []string{
	"agent-reviewer2",
	"agent-reviewers",
	"agent-reviewerx",
	"agent-reviewer0",
	"agent-config-reviewer9",
}

// The honest spellings. Recording that no reviewer was reached is REQUIRED, so
// a false refusal here does the exact damage the rule exists to prevent: it
// makes the true sentence the expensive one to write.
var honestReviewerValues = []string{
	"none — no reviewer was reached for this commit",
	"none",
	"nobody — session ending",
	"no reviewer was reached",
	"nobody",
	"n/a",
	"(none)",
	"NONE",
	"unreachable",
	"none reached — this session had no sub-agents to review with",
	"nobody — the session was ending and had no sub-agents",
}

// The prefix set. A prefix does not answer the audit grep, so refusing one
// would be a false fail. These are what stops the suffix rule from being
// spelled as "match the name anywhere".
var prefixedReviewerNames = []string{
	"xxagent-reviewer",
	"notagent-reviewer",
	"9agent-reviewer",
	"superagent-reviewer",
}

// ─── the trailer has to be there, and the bypasses that say it need not ─────

var bypassCases = []validateCase{{
	name: "a commit with no trailers at all is refused for both of them",
	msg:  "fix(gate): land something without saying who read it",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
	text: "missing required trailer 'Reviewed-By:'",
}, {
	name: "a Reviewed-By with no Review-Verdict is refused for the verdict alone",
	msg:  "fix(gate): land something\n\nReviewed-By: agent-reviewer",
	want: []Check{CheckReviewVerdictPresent},
}, {
	name: "the Trivial: true bypass skips the trailer rules",
	msg:  "docs(readme): fix a typo in the boot order\n\nTrivial: true",
}, {
	name: "Trivial: true is exact and anchored, so a near miss does not bypass",
	msg:  "docs(readme): fix a typo in the boot order\n\nTrivial: true please",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "a fixup! commit needs no trailers",
	msg:  "fixup! feat(gate): the merge decision refuses a box that cannot answer",
}, {
	name: "a squash! commit needs no trailers",
	msg:  "squash! feat(gate): the merge decision refuses a box that cannot answer",
}}

// ─── merges ─────────────────────────────────────────────────────────────────
//
// A merge LANDS, so it answers for its content like anything else. The one
// exemption is the commit nobody writes: GitHub computes `refs/pull/N/merge`
// for a pull_request event and `actions/checkout` checks it out, so it is the
// HEAD this gate reads in CI. Held to the trailer rules it fails for a trailer
// no command can add, which refuses every pull request whose base is not
// already an ancestor of its head.

var mergeCases = []validateCase{{
	name: "an ordinary merge with no trailers is refused like any other commit",
	msg:  "Merge branch 'work/bravo-lane' into work/alpha-integration-merge",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "an ordinary merge cannot carry an approval from a reviewer this repo lacks",
	msg: "Merge branch 'work/evil' into main\n\n" +
		"Reviewed-By: nobody-ran-this\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Fabricated."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	name: "a merge that declares itself trivial is accepted",
	msg:  "Merge branch 'work/bravo-lane' into work/alpha-integration-merge\n\nTrivial: true",
}, {
	name: "a merge carrying the honest no-reviewer form is accepted",
	msg: "Merge branch 'work/bravo-lane' into work/alpha-integration-merge\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "A lane merge."}`,
}, {
	name: "the merge commit GitHub builds for a pull request is accepted",
	msg:  githubMergeSubject,
}, {
	name: "a hash-pair merge subject with a body is not GitHub's and is refused",
	msg:  githubMergeSubject + "\n\nSneaking a change in under a subject that reads like the merge ref.",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	// The control the exemption's width turns on. Widen it to "a merge subject
	// with hashes in it" and this goes green, at which point anyone wanting no
	// reviewer writes a subject in this shape by hand.
	name: "a hash-pair subject carrying a fabricated approval is still refused",
	msg: githubMergeSubject + "\n\n" +
		"Reviewed-By: nobody-ran-this\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Fabricated."}`,
	want: []Check{CheckApprovalIdentity},
}, {
	name: "abbreviated hashes are not the shape GitHub writes and are refused",
	msg:  "Merge 95129b3e3 into 62e3e719a",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "upper-case hex is not the shape GitHub writes and is refused",
	msg:  "Merge 95129B3E317A4C990DF6B55268A17BD172ADDAB1 into 62e3e719a483d9e2b9be2b7dba7e2bb4a7f91c8c",
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "trailing blank lines do not stop the synthetic merge exemption",
	msg:  githubMergeSubject + "\n\n\n",
}}

// ─── comment lines, and the cleanup mode that decides whether they count ────
//
// WHERE THE MODE STILL BITES, NOW THAT A TRAILER IS A COLUMN-ZERO LINE. It no
// longer decides whether a commented-out `Reviewed-By:` is a claim — that line
// is prose under every mode, because the key is not at column zero. What the
// mode still decides is which line is the SUBJECT, and the subject is what
// says whether a commit is a `fixup!` or the merge GitHub writes. Under
// `strip` a leading comment line is removed and the next line is the subject;
// under every other mode the comment line IS the subject, git stores it, and
// the exemption is gone with it.
//
// Getting that backwards is the fail-open these cases exist to stop: strip a
// line git keeps and the validator reads a subject the commit does not have.

// commentedFixup puts a comment line in front of a scratch `fixup!` subject.
func commentedFixup(marker string) string {
	return marker + " scratch commit, do not keep\n" +
		"fixup! feat(gate): the merge decision refuses a box that cannot answer"
}

var cleanupCases = []validateCase{{
	name: "under strip the comment git would remove is removed, and the fixup is exempt",
	msg:  commentedFixup("#"),
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: "#"},
}, {
	name: "under the default mode the comment line is the subject and the fixup exemption is gone",
	msg:  commentedFixup("#"),
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "verbatim keeps comment lines",
	msg:  commentedFixup("#"),
	opts: Options{Cleanup: CleanupVerbatim, CommentPrefix: "#"},
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "whitespace keeps comment lines",
	msg:  commentedFixup("#"),
	opts: Options{Cleanup: CleanupWhitespace, CommentPrefix: "#"},
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "the configured marker is the one that gets stripped",
	msg:  commentedFixup(";"),
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: ";"},
}, {
	// The direction that matters more. With `;` configured a `#` line is
	// ORDINARY TEXT that git stores, and a hard-coded `#` would drop it.
	name: "with another marker configured a # line is content git keeps",
	msg:  commentedFixup("#"),
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: ";"},
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "a multi-character marker is honoured",
	msg:  commentedFixup("//"),
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: "//"},
}, {
	// git's `auto` picks a marker that begins NO line of the message, so under
	// it no line of the author's text is a comment. An empty prefix strips
	// nothing.
	name: "an empty comment prefix strips nothing even under strip",
	msg:  commentedFixup("#"),
	opts: Options{Cleanup: CleanupStrip},
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	// A comment is a line that BEGINS with the marker, with no leading
	// whitespace allowed — git's own test.
	name: "an indented marker is not a comment line",
	msg:  "  # scratch commit, do not keep\nfixup! feat(gate): a scratch change",
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: "#"},
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	// The same rule on the other subject that carries an exemption: GitHub's
	// merge is one line and nothing else, so a comment line git keeps takes
	// the exemption away.
	name: "a comment line git keeps costs the synthetic merge its exemption",
	msg:  "# a line somebody added by hand\n" + githubMergeSubject,
	want: []Check{CheckReviewedByPresent, CheckReviewVerdictPresent},
}, {
	name: "under strip the same comment line leaves the synthetic merge exempt",
	msg:  "# a line somebody added by hand\n" + githubMergeSubject,
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: "#"},
}, {
	// A commented-out trailer is not a trailer under ANY mode, because the key
	// is not at column zero. Under `strip` git deletes the line; under the rest
	// git stores it and the anchored audit walks past it. The two agree, which
	// is the point of anchoring both.
	name: "a commented-out Reviewed-By is prose under the default mode",
	msg:  hashFabrication("#"),
}, {
	name: "a commented-out Reviewed-By is prose under strip as well",
	msg:  hashFabrication("#"),
	opts: Options{Cleanup: CleanupStrip, CommentPrefix: "#"},
}, {
	// The control, and it is not decoration: without it a validator that
	// refused every `#` line would keep the cases above green while making a
	// comment in a commit body an error nobody asked for.
	name: "an ordinary # line in the body is content, not an error",
	msg: "fix(gate): keep a comment in the body harmless\n\n" +
		"# A note to the next reader. Nothing about a reviewer here.\n\n" +
		"Reviewed-By: none — no reviewer was reached for this commit\n" +
		`Review-Verdict: {"schema_version": 1, "verdict": "NOT_REVIEWED", "flags": [], "notes": "No reviewer was reached."}`,
}}

// ─── the reviewer list is supplied, and an empty one is not a free pass ─────

var reviewerListCases = []validateCase{{
	// An empty list compared with `==` matches nothing, which refuses every
	// approval; an empty list read as "no opinion" accepts every approval.
	// Neither is right, so it falls back to the names this repo has.
	name: "with no reviewer list supplied a real reviewer name is still accepted",
	msg:  "feat(s04): add the claude-twin handler adapter" + approveTrailers,
	opts: Options{KnownReviewers: nil},
}, {
	name: "with no reviewer list supplied a bogus reviewer name is still refused",
	msg: "feat(queue): chain the claim through\n\n" +
		"Reviewed-By: implement_claim_chain\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Looks right."}`,
	opts: Options{KnownReviewers: nil},
	want: []Check{CheckApprovalIdentity},
}, {
	// The caller decides which names are real, by reading what git TRACKS. A
	// name the caller did not hand over is not a reviewer here, however much it
	// looks like one — that is what stops an untracked `mkdir` beside the real
	// skills from minting a trusted name.
	name: "a reviewer-shaped name the caller did not supply is refused",
	msg: "fix(gate): a subject that is otherwise fine\n\n" +
		"Reviewed-By: forged-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"minted"}`,
	opts: Options{KnownReviewers: []string{"agent-reviewer"}},
	want: []Check{CheckApprovalIdentity},
}, {
	name: "a glob is not a reviewer name",
	msg: "fix(gate): a subject that is otherwise fine\n\n" +
		"Reviewed-By: *reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"minted"}`,
	opts: Options{KnownReviewers: []string{"agent-reviewer"}},
	want: []Check{CheckApprovalIdentity},
}, {
	name: "the caller's list is what a name is matched against",
	msg: "fix(gate): a subject that is otherwise fine\n\n" +
		"Reviewed-By: forged-reviewer\n" +
		`Review-Verdict: {"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"a real review"}`,
	opts: Options{KnownReviewers: []string{"agent-reviewer", "forged-reviewer"}},
}, {
	name: "the contradiction rule reads the caller's list too",
	msg:  notReviewedMsg("forged-reviewer"),
	opts: Options{KnownReviewers: []string{"forged-reviewer"}},
	want: []Check{CheckNoReviewerNamed},
}}

func allCases() []validateCase {
	out := make([]validateCase, 0, 160)
	out = append(out, honestFormCases...)
	out = append(out, nonApprovingCases...)
	out = append(out, notReviewedCases...)
	out = append(out, anchoredTrailerCases...)
	out = append(out, bypassCases...)
	out = append(out, mergeCases...)
	out = append(out, cleanupCases...)
	out = append(out, reviewerListCases...)
	out = append(out, verdictPayloadCases()...)

	for _, value := range decoratedReviewerNames {
		out = append(out, validateCase{
			name: "a NOT_REVIEWED verdict is refused for naming the reviewer as " + value,
			msg:  notReviewedMsg(value),
			want: []Check{CheckNoReviewerNamed},
		})
	}
	for _, value := range suffixedReviewerNames {
		out = append(out, validateCase{
			name: "a NOT_REVIEWED verdict is refused for naming the reviewer as " + value,
			msg:  notReviewedMsg(value),
			want: []Check{CheckNoReviewerNamed},
		})
	}
	for _, value := range honestReviewerValues {
		out = append(out, validateCase{
			name: "the honest form " + value + " is accepted",
			msg:  notReviewedMsg(value),
		})
	}
	for _, value := range prefixedReviewerNames {
		out = append(out, validateCase{
			name: "a name the audit does not count, " + value + ", is accepted",
			msg:  notReviewedMsg(value),
		})
	}
	return out
}

func TestValidate(t *testing.T) {
	t.Parallel()
	for _, tc := range allCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Validate(tc.msg, tc.opts)
			assertChecks(t, got, tc.want)
			if tc.text != "" && !strings.Contains(Render(got), tc.text) {
				t.Errorf("refusal did not say %q\n%s", tc.text, Render(got))
			}
		})
	}
}

func assertChecks(t *testing.T, got []Problem, want []Check) {
	t.Helper()
	gotChecks := make([]string, 0, len(got))
	for _, p := range got {
		gotChecks = append(gotChecks, p.Check.String())
		if len(p.Lines) == 0 {
			t.Errorf("problem %v carries no lines to print", p.Check)
		}
	}
	wantChecks := make([]string, 0, len(want))
	for _, c := range want {
		wantChecks = append(wantChecks, c.String())
	}
	sort.Strings(gotChecks)
	sort.Strings(wantChecks)
	if strings.Join(gotChecks, ",") != strings.Join(wantChecks, ",") {
		t.Errorf("checks fired = [%s]; want [%s]\n%s",
			strings.Join(gotChecks, ","), strings.Join(wantChecks, ","), Render(got))
	}
}

func TestRenderNumbersEveryLineAndSaysNothingWhenClean(t *testing.T) {
	t.Parallel()
	if got := Render(nil); got != "" {
		t.Errorf("Render(nil) = %q; want the empty string", got)
	}
	got := Render(Validate("feat(x): a thing", Options{}))
	want := "validate-commit-msg: validation failed:\n" +
		"  [1] missing required trailer 'Reviewed-By:' on a non-trivial commit.\n" +
		"  [2]   Add 'Trivial: true' trailer to bypass for typos/whitespace fixes.\n" +
		"  [3] missing required trailer 'Review-Verdict:' on a non-trivial commit.\n" +
		"  [4]   Add 'Trivial: true' trailer to bypass for typos/whitespace fixes.\n"
	if got != want {
		t.Errorf("Render mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// The subject rules are GONE, and this pins that. Conventional Commits, the
// 72-character ceiling and the trailing period policed FORM rather than
// honesty; the shell validator still refuses all four of these and this package
// deliberately does not.
func TestSubjectShapeIsNotThisPackagesBusiness(t *testing.T) {
	t.Parallel()
	subjects := []string{
		`Revert "feat: add thing"`,
		"fix(cmd/harmonik): route the path through the adapter",
		"fix(run_loop): stop reading the wall clock",
		"wibble(gate): a type nobody declared",
		"fix(gate): a subject that ends with a period.",
		"fix(gate): " + strings.Repeat("x", 200),
		"no conventional shape at all",
	}
	for _, subject := range subjects {
		t.Run(subject[:min(len(subject), 40)], func(t *testing.T) {
			t.Parallel()
			if got := Validate(subject+approveTrailers, Options{}); len(got) != 0 {
				t.Errorf("subject %q was refused: %s", subject, Render(got))
			}
		})
	}
}
