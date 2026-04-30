# v2 Revisions — Draft 1 → Draft 2 (post-review)

> What changed between the initial v2 drafts (`session-handoff.md`, `session-resume.md`) and the post-review revisions (`session-handoff-revised.md`, `session-resume-revised.md`), with which review surfaced each change.

## Length

| File | Draft 1 lines | Draft 2 lines | v1 (current skills) for comparison |
|---|---|---|---|
| session-handoff | 24 | 17 | 104 |
| session-resume | 18 | 15 | 102 |

## Changes accepted, with attribution

### From R1 (skeptic-of-fix)

**Highest-leverage change: collapsed the six bold-prefixed bullets into prose.** R1's argument: *"a smaller schema is still a schema… named slots get filled regardless of 'skip if empty' disclaimers."* This single change addresses Causes 2 (descriptive-becomes-policy), 3 (back-brief bleed), 4 (schema-fill), and 7 (output-style-matches-input) simultaneously. Six imperatives became one prose sentence with semicolon-separated parts.

**Dropped literal `if X happens, then do Y` template.** R1: the literal template invites future agents to read each clause as a directive. Replaced with prose phrasing *"anything that should change the next agent's plan if a specific thing happens"* — keeps the contingency idea, drops the slot-naming.

**Dropped "blocking question, if there is one" framing in resume.** R1: question-framing on turn 1 still primes stop-and-ask shape. Replaced with *"any question the previous agent flagged that's actually blocking, asked so the user can answer without digging"* — same content, less ceremony.

### From R2 (adversarial completeness)

**Added branch + date to the trial-flag line.** R2 named two concrete failures: stale handoffs being read as current; cross-branch handoffs landing in the wrong tree. Resolution: extend the first-line marker to `<!-- PP-TRIAL:v2 YYYY-MM-DD <branch-name> -->`. Costs one line. Resume now checks both.

**Dropped explicit root-file list in session-resume.** R2: the explicit list (`CLAUDE.md, AGENT_INDEX.md, STATUS.md, TASKS.md`) pulls the agent toward repo-root files even in nested-doc projects where track-local equivalents take precedence. Replaced with *"Follow whatever reading order the project's CLAUDE.md describes if there is one."* Trusts the project's own orientation file.

**Did NOT add explicit fan-out-state instruction.** R2 flagged a real failure case (next session relaunches all 5 sub-agents because "in flight" hid mid-fan-out state). Decision: trust the agent to handle this naturally under "where things stand." Adding an explicit fan-out clause inflates the prompt; if next iteration shows the failure, add then. Recording as deferred.

### From R4 (plain-language)

**"First line is X (greppable)" → plainer.** Kept the substance, dropped the parenthetical-jargon framing.

**"git log is authoritative" → "git log already has the decisions, and project instructions already cover scope."** R4 flagged "authoritative" as the legalistic word in the file. Plainer wording adopted.

**"Post a short message" → "Before starting work, say back briefly:"** R4 flagged "post a short message" as procedural-sounding. Plainer adopted.

**"Anything stale or contradicting the repo" → "Anything in the handoff that looks stale or doesn't match the repo."** R4: the original was terse-to-cryptic. Expanded slightly for readability.

**Bold-label-period bullets dropped.** R1 and R4 both flagged the bold-prefix form-like shape. Removed entirely along with the bullet-to-prose collapse.

### From R3 (self-application output)

R3 didn't surface new issues — but the actual handoff it produced (17 lines, accurate jargon translation in the open question, three named files to read first, no padding) is positive evidence that the v2 prompt produces the kind of output we want. The R3 output is on disk at `reviews/r3-self-application-output.md`; worth reading as proof-of-shape.

## Changes considered and rejected

- **Reverting to bullet-list shape after the prose collapse.** Tempting because prose is harder to scan than bullets. Rejected because R1's diagnosis is structurally correct: bullets train the agent to fill them. The first-cut v2 already had "skip what doesn't apply" disclaimers; they didn't prevent R3's self-application output from using bold-label-period formatting downstream. Prose breaks the pattern more cleanly.

- **Adding a "fan-out state" sub-clause** (R2's #3). Concrete failure case is real but specific. Adding the clause adds a line of prompt; the case may not actually fire often. Deferred to next iteration if observed.

- **Adding a "watcher" status value alongside green/blocked/broken.** I-PASS has stable/watcher/unstable; we picked green/blocked/broken. The watcher tier (worth keeping an eye on, not yet blocking) is real but adds a third value to a tag that benefits from being binary-ish. Deferred; revisit if we see "I almost flagged this but didn't fit any category" reports.

## Summary diff

The Draft 1 → Draft 2 edit is mostly **structural compression** (bullets → prose) plus **wording polish** (R4) plus **two real additions** (R2: date + branch on first line; resume's branch sanity check). The substance of v2 — no token list, no decide-vs-ask, no out-of-scope, prose intent, named outward translation, judgment disposition after back-brief — is unchanged.
