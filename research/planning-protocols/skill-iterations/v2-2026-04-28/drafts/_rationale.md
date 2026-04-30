# v2 Rationale

> Why these drafts look the way they do, and what changed from v1.

## Source inputs synthesized

- [`research/ipass-deep-dive.md`](../research/ipass-deep-dive.md) — I-PASS protocol deep-dive + translation map.
- [`research/external-form-comparison.md`](../research/external-form-comparison.md) — Medical / military / incident-command form-and-economy extraction.
- [`research/anti-anchored-fresh-draft.md`](../research/anti-anchored-fresh-draft.md) — Fresh design from user's two-sentence brief, no anchoring on v1.
- [`../../trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](../../../trial-findings/2026-04-27-skills-too-verbose-and-procedural.md) — Trial finding 1 (the 8 named failure causes from v1).

## Convergent points across all three research inputs

These design choices appeared independently in all three pieces of research and are treated as load-bearing:

1. **Severity / status tag at the top.** I-PASS forces a one-word acuity tag (`stable / watcher / unstable`) because under cognitive load the receiver may skip the patient summary entirely. The external-form comparison: "severity tag survives even when summaries don't." v2 adopts `green / blocked / broken`. v1 had no analog.
2. **Compress the *why*.** Brief intent (1–2 sentences) — present in I-PASS Patient summary, military 2–3 sentence intent, ICS 1–3 sentence intent, anti-anchored draft. v2 keeps this.
3. **No verbatim token lists.** External-form comparison explicitly: *"None list 'load-bearing tokens' verbatim. The receiver re-encodes in their own words; verbatim repetition is identified as a failure mode."* I-PASS deep-dive: *"tokens earn their slot only if a substitute spelling would change behavior."* v2 removes the standalone Load-Bearing Tokens section. The translation discipline lives in the resume's paraphrase + the explicit "translate when asking the user" rule.
4. **No decide-vs-ask partition.** External-form comparison: *"None partition decisions into 'decide vs ask first' lists. Authority is granted by bound (intent / objectives / end state)."* I-PASS deep-dive: *"Autonomy Scope... belongs in the project's standing instructions, not in every handoff."* v2 removes the autonomy-scope section. The disposition (act on judgment; don't ask permission for routine choices) is named once in the resume skill instead of per-section in the handoff schema.
5. **No Out-of-Scope section.** External: *"None produce 'out of scope' sections. Scope is defined positively."* I-PASS deep-dive: *"implied by Intent."* v2 removes it.
6. **Closed-loop synthesis on receiver side.** Universal across the three domains. v1 already had this via `/session-resume` back-brief; v2 keeps it but slimmer (paraphrase + open question + staleness check, not a 4-step procedure with worked-example contrast).

## Distinctive contributions kept from each research input

- **From I-PASS:** the **if-then** structure for parked items. Pilot studies showed contingency thinking is rarely volunteered without a slot; same logic applies here. v2's If-then section forces a real trigger (`if X, then Y`). An item with no trigger is a TODO, not a handoff item — that demotion alone removes most of v1's "Decisions Parked" content.
- **From external-form comparison:** the *named* anti-verbosity rule. v2's prompt explicitly says "skip boilerplate; skip sections with nothing real to say; a 20-line handoff beats a 150-line one." This is doctrinal in all three external domains and was missing from v1.
- **From anti-anchored draft:** translation-of-jargon as a named behavior. v2 names it in both skills. v1 named verbatim-mirroring at session start but never named outward translation; that gap was Cause #7 in the trial finding.

## Tensions resolved

- **Decisions Made section.** I-PASS deep-dive: *"git log is authoritative; restating it is the procedural-permission-seeking generator."* Anti-anchored draft: allows mention of "a decision we just made... that isn't obvious from the repo." Resolution: no "Decisions Made" section as such, but the "Where it stands" slot can include a non-repo-visible decision when it's actually load-bearing for the next session.
- **Trial flag.** v1 mandated `<!-- PP-TRIAL:v1 -->` as the first line. The flag itself is one line, low cost, and useful for greppability across iterations. v2 keeps it, bumped to `PP-TRIAL:v2`.
- **Length.** Anti-anchored draft: 30 + 18 lines. v1 (current): 104 + 102 lines. v2: 28 + 16 lines (slightly shorter than the anti-anchored draft because removing the rationale-by-mention nudges it tighter).

## What v2 deliberately drops from v1

- **Mandatory 8-section schema.** Replaced with 6 conditional slots; agent told to skip ones that don't apply.
- **The 4-step resume procedure with worked example (❌/❌/✓ paraphrase contrast).** The example itself was advertising "this is delicate, be careful" — the wrong signal. Replaced with a one-paragraph instruction.
- **`If unsure, default to ask`.** Cause #1 in the trial finding. Replaced with explicit "use normal judgment, don't ask permission for routine choices."
- **7 behavior rules + 5 failure modes per skill.** ~25% of v1's bulk. Removed entirely; the implicit instructions in the prompt body cover what survived.
- **Severity tags on parked items (`routine / watch / escalate`).** Three-level scheme that the trial finding noted "becomes either always-empty or always-overused." Replaced by if-then triggering: an item with a real trigger goes in If-then; without one, it's a TODO that doesn't belong in the handoff.

## Open design questions left for review pass

- Is `green / blocked / broken` the right value set for Status? `blocked` covers two cases (waiting on user vs waiting on external); should it split? I-PASS's `watcher` is a third state (worth keeping an eye on) — does that translate?
- Does the resume's "wait for the user to confirm or correct" still install the back-brief-bleed-past-turn-1 problem (Cause #3)? Or is "Once they've responded, get on with the work using normal judgment" the disposition that breaks that bleed?
- Should the project's start-of-session files (CLAUDE.md, AGENT_INDEX.md, STATUS.md, TASKS.md) be conditional on existence (current draft) or named in the handoff itself? Current draft assumes the resume agent infers; alternative is to make the handoff name them under "First files to open."
- The trial flag `<!-- PP-TRIAL:v2 -->` — do we want it? It's one line of mandated boilerplate. Worth it for retrospective greppability of which iteration produced which handoff.

## What this draft does NOT yet have

- Not yet reviewed by the four reviewer angles. Round 3 will surface residual issues.
- Not yet tested in a real session. n=2 trial signal triggered the rewrite; the v2 rewrite needs n=1+ to be evaluated.
- Not yet replacing the actual skill files at `~/.claude/skills/`. Only on disk in this iteration's directory until the user signs off.
