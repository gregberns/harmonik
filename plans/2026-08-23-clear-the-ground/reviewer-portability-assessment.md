# Is the reviewer any good, and can another project use it — findings

Assessment for [`tasks/reviewer-portability-review.md`](tasks/reviewer-portability-review.md)
(`hk-reviewer-portability-review-9fbjm`). Read-only: no code changed, nothing restructured — see
`## Limits` in the task file. Covers `.claude/skills/agent-reviewer/SKILL.md` (723 lines) and its
`.claude/agents/agent-reviewer.md` mirror (709 lines, frontmatter-only diff — confirmed byte-identical
in body as of `9c2424259`, the `reviewer-subagent-drift` fix this task depended on), plus the
`agent-config-reviewer` pair (424 / 407 lines, same mirroring discipline, also in sync).

## 1. General judgement vs. harmonik-specific

Read section by section against the nine Tier-1 checks. None of the nine sections is 100% one or the
other; the ratio varies a lot by section.

| § | Check | General core | Harmonik-specific |
|---|---|---|---|
| Citing code in a normative doc | Verify before you cite, re-verify after a correction, cite symbol not line, don't stretch one verified example | The whole judgement transfers as written | The evidence trail (`c1ad2629`, `dfb576fc`, named commit counts) is this repo's incident history, not a rule |
| 1. Spec alignment | "does the diff implement the spec, no more, no less" | Fully general | Assumes a `specs/*.md` normative-doc convention |
| 2. Idiom compliance | "review for idiom compliance against the pinned linter config, and the config wins over prose" — that meta-rule is general and well-stated | ~90% of the section's body: literal `.golangci.yml` settings, package paths (`internal/codexinput`, `internal/runloop`, `internal/runexec`), forbidigo marker names (`SC6-DRIVER-CLOCKPORT`, `C23-CLOCK-RATCHET`), the entire `Close()` sub-section's landed-code citations |
| 3. Test adequacy | "right tier, meaningful, lowest layer that would still fail" | General | The three mechanical checks (bead-named test files, `export_*_test.go` bans) are keyed to this repo's own naming convention and a documented 199,899-line incident |
| 4. Unwanted abstraction | "can the diff name what the abstraction buys" — the sharpest, most reusable check in the file | General, and it says so explicitly | Every worked example cites `PRINCIPLES.md` sections and `substrate.ClockPort` |
| 5. Bead/codename match | — | None | 100% harmonik: `Refs:` trailer, bead IDs, kerf codenames |
| 6. Production call-site wiring | "is this thing actually wired into the running binary, not just unit-tested" | General and valuable — this is a real, transferable review habit | Points at `internal/daemon/daemon.go` as *the* composition root |
| 7. Spec field-name conformance | "grep the diff for names the spec/bead demanded" | General mechanism | Its own worked example and root-cause bead (`hk-vh1jc`) are harmonik incidents |
| 8. Reproducing test for bug beads | "bug fixes need a repro test at the lowest failing layer" | General, matches a widely-held norm | Bead-label convention (`bug`), doc citation |
| 9. Deletion accounting | "for each deleted symbol, name every surviving reader and what it now sees" — the second sharpest check in the file | General and would catch real defects in any codebase | Grounded in a named harmonik incident (`hk-2h2fa`) but the *procedure* carries with no rewrite |

Outside the nine checks, three things are wholesale harmonik machinery, not judgement at all:

- **Output format / trailer contract** (`Reviewed-By:`, `Review-Verdict:`, the exact JSON shape,
  the "must be a git-tracked skill dir" identity rule). This is protocol, not review skill.
- **Flag vocabulary.** A closed tag list (`spec-divergence`, `bead-named-test`, `x-missing-wire-up`,
  ...) wired to this repo's own incidents and enforcement code (`internal/workspace/reviewverdict.go`,
  `internal/commitmsg`).
- **Liveness/currency section.** References `agent-config-reviewer`, `build-practices.md`,
  `quality-checks.md` — harmonik's own doc graph.

**`agent-config-reviewer` is not on this spectrum at all.** Every one of its five checks (CLAUDE.md
drift, `settings.json` drift, skill-registry drift, foundation-doc currency, `.golangci.yml` currency)
is defined in terms of *this repo's own* configuration files by name and path. There is no general
core to separate out — the skill's job is "does harmonik's agent configuration match itself," which is
inherently project-bound. A second project would not port this skill; it would write its own version
of the same idea (a periodic self-consistency check over its own config surface), reusing at most the
five-check *shape*.

## 2. Is the split clean enough to lift?

**No — not as a file split.** The two halves are interleaved sentence by sentence within most
sections, not separated into a general part and a harmonik part. Section 2 (idiom compliance) is the
clearest case: the opening two sentences ("review for Go idiom compliance against `.golangci.yml`" /
"the config wins and the prose is the bug") are the entire portable content of the section, and the
other ~190 lines are enforcement detail specific to this repo's linter config and package layout. A
second Go project with a *different* `.golangci.yml` cannot use those 190 lines at all — but it would
still want the two-sentence meta-rule, and there is no place in the file to cut that separates it.
Section 5 is the opposite extreme (100% harmonik, cleanly a single self-contained section — that part
*is* liftable, by deletion). Sections 4 and 9 sit in between: the check itself is general and
well-written, but every worked example is a harmonik citation, so lifting them means rewriting the
examples, not deleting a block.

So the honest description is: **one general document with harmonik load-bearing detail woven through
nearly every section, plus a couple of sections that are cleanly 100% harmonik (§5, output format,
flag vocabulary) and could be deleted wholesale.** A second project cannot "take the general half."
It would have to start from this file, keep the check *list* and the meta-rules (config-wins-over-prose,
name-what-it-buys, account for every deletion, verify-before-you-cite), and rewrite every worked
example, every idiom list, every doc citation, and the entire output-format/trailer section against its
own stack, its own linter, and its own commit-trailer convention (if it has one at all — most projects
don't gate commits on a machine-read verdict).

**Recommendation:** don't split this file now (the task's own limits agree — that's separate,
reviewed work). If portability is wanted later, the right target is not "extract the general half into
a shared file" but "write a second project's reviewer from scratch, using this one as a worked example
of the *shape* — nine-ish checks in this order, config-beats-prose, name-what-it-buys, account for
every deletion — and supplying that project's own idiom list, doc-citation targets, and (if any) commit
protocol." The check *order and judgement style* is the reusable artifact; the file itself is not.

## 3. Evidence it works

Confirmed from the skill body and the code it wires into, checked directly rather than taken on
the skill's word:

- **The verdict schema is not decorative — it drives daemon control flow.**
  `internal/workspace/reviewverdict.go` `ReadReviewVerdict` / `parseReviewVerdict` rejects any
  file missing `schema_version`, `verdict`, `flags`, or a non-empty `notes`, and rejects any
  `verdict` value outside `APPROVE` / `REQUEST_CHANGES` / `BLOCK`. `internal/daemon/dot_cascade_core.go`
  and `dot_cascade_helpers.go` branch on the actual verdict value — `ReviewVerdictApprove` vs
  `ReviewVerdictRequestChanges` vs `ReviewVerdictBlock` drive different daemon paths (commit
  acceptance, gate re-checks, and cascade continuation), not a boolean pass/fail collapsed from
  three strings. So the three-way distinction is doing real work, not ceremony for its own sake.
- **`hk-2h2fa`** — a reviewer approved a 1412-line deletion with the note "clean diff." The deletion
  orphaned two subsystems; four defects (an unwired run registry, a guard that cannot run, an
  agent that is never killed, a rate-limit carve-out that can never match) surfaced two days later.
  This is the reviewer being *wrong* in the way that matters most — approving something that broke
  production behavior silently. It directly produced Tier-1 check 9 (deletion accounting), which is
  now one of the two sharpest checks in the file (see §1).
- **`hk-vh1jc`** — a reviewer APPROVED a diff at iteration 2 that used the field name `SessID` despite
  both the iteration-1 BLOCK verdict and the bead enrichment naming `SessionID`. Also a genuine miss,
  not a near-miss; it produced Tier-1 check 7.
- **The retired Go-comment rule (`9e70fbe4b`, `cc2d7fc7e`, `9c2424259`, `ca35dabff`)** — the
  normative-doc citation rule (§Citing code in a normative doc) was originally applied to comments
  inside `.go` files. Measured cost, quoted in the skill itself: over 300 commits before
  2026-08-23, eight changed five or fewer lines of code and twenty-plus lines of comment, five of
  them re-editing the same two files — a correction, corrected, corrected again. The rule was
  retired for comments specifically (§Flag vocabulary now states "a finding whose only remedy is
  editing comment prose inside a `.go` file raises NO flag"). This is the reviewer causing harm
  through a rule that was well-intentioned but mis-scoped, and it is the one case the task already
  named.
- **Self-correcting pattern.** Both fixes above (deletion accounting, field-name conformance, the
  comment-loop retirement) show the same shape: reviewer produces a real miss → miss is diagnosed →
  a new mechanical check or scope exclusion is added and mirrored into both the skill and the
  sub-agent copy. That the file has grown by incident rather than by speculation is itself evidence
  the checks that exist are load-bearing, not invented for completeness.

**Verdict-rate sample.** Of the 100 most recent commits carrying a `Review-Verdict:` trailer: 46
`APPROVE`, 21 `REQUEST_CHANGES`, 31 `NOT_REVIEWED` (the author-written "no reviewer reached" case,
which is not a reviewer output at all — see §How the verdict lands in git), 2 `DRIFT_MINOR` (the
Tier-2 config reviewer, not agent-reviewer). `BLOCK` does not appear by construction — a BLOCK never
lands a commit. Restricting to commits where a reviewer actually ran (67 of the 100), the split is
46 `APPROVE` / 21 `REQUEST_CHANGES` — roughly **31% of reviewed commits got REQUEST_CHANGES**. That
is not a rubber-stamp rate; a reviewer that approved everything would show near-zero here. The other
finding worth naming on its own: **31% of recent commits report no reviewer was reached at all.**
That is a real gap in coverage — a third of commits are landing on the author's own judgment with an
honest `NOT_REVIEWED` record rather than a fabricated approval, which is the documented intent of
that trailer, but it means the answer to "does the reviewer catch things" only applies to two-thirds
of commits in the first place.

A deeper commit-by-commit audit (matching individual `REQUEST_CHANGES` verdicts to the diff that
followed, to see whether the flagged issue was actually fixed) was not completed within this
assessment's scope — the three named incidents above (`hk-2h2fa`, `hk-vh1jc`, the comment-loop
retirement) remain the confirmed, traced cases; the verdict-rate sample above is aggregate evidence
that the reviewer discriminates rather than proof that every individual `REQUEST_CHANGES` was acted
on correctly.

## 4. Is the JSON verdict schema useful or ceremony?

**Useful — confirmed by reading the consumers, not by taking the skill's claim.** Two independent
things depend on the exact shape:

1. **`internal/workspace/reviewverdict.go`** parses and validates the file mechanically (missing
   key, empty `notes`, or an unrecognized `verdict` string are all rejected as malformed), and
   `internal/daemon/dot_cascade_core.go` / `dot_cascade_helpers.go` branch on the parsed `Verdict`
   value to decide what the daemon does next. This is not a human-readable trailer that nothing
   reads back — it is an input to control flow.
2. **The commit-message gate** (`internal/commitmsg`) requires `Review-Verdict:` to be parseable
   JSON with the required fields, and requires `Reviewed-By:` to name a real, git-tracked reviewer
   skill directory — explicitly to prevent a free-text field (which used to allow `agent-reviewer
   (myself)` to read as a real approval) from smuggling in a self-approval. That is a stated,
   specific attack the structure closes.

The one place the schema *could* be seen as ceremony is the `flags` array: the schema requires the
key be present (`[]` is a valid value) but doesn't require the checks correspond mechanically to the
flags raised — a reviewer could in principle emit `"flags": []` for a real finding it only describes
in `notes`. That's a soft spot in enforcement, not in the schema's design; nothing found in this pass
suggests it has actually happened. Overall: real machine consumers, a real attack the shape closes,
and a genuine three-way branch in daemon logic. Not ceremony.

## Bottom line

The reviewer catches real things (two confirmed misses became two of its sharpest checks; the schema
gates real daemon control flow and closes a real self-approval exploit) and it also did real harm once
(the comment-loop), which was diagnosed and fixed rather than denied. It does not travel as a file
split — the general judgement and the harmonik specifics are interleaved within nearly every section,
not separated into two halves. A second project should treat this file as a worked example of the
*shape* of a good reviewer contract (ordered checks, config-beats-prose, name-what-an-abstraction-buys,
account for every deletion, verify citations before and after writing them) and author its own version
against its own stack, rather than trying to extract a reusable "general" file from this one.
