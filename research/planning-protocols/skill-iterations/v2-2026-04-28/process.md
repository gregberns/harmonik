# v2 Iteration Process

> How this iteration was conducted. Reproducible if a future iteration wants to follow the same shape.

## Trigger

Trial finding 1 ([`trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](../../trial-findings/2026-04-27-skills-too-verbose-and-procedural.md)) — pushback in basata `426257cc` (under `/session-resume`) and harmonik `a121e7f1` (independent, no `/session-resume`). 8 named failure causes in the v1 skills. n=2 with strong signal; user authorized acting on it rather than waiting for n=3-5.

## Time and shape

Single overnight session (user asleep). One top-level agent (this conversation) coordinating sub-agents. Total wall-clock approximately 30-45 minutes for the agent rounds; doc-writing in the gaps.

## Round 1 — Research (3 parallel sub-agents)

Run in a single message with three Agent tool calls.

### Agent 1: I-PASS deep-dive + translation

- **Subagent type:** general-purpose (web access).
- **Context provided:** the trial-finding situation (skills are too verbose, induce procedural output) + 6 specific research questions about I-PASS (sections, time budget, evidence base, translation map, synthesis form, proposed structure).
- **Output:** 766 words, written to `research/ipass-deep-dive.md`.
- **Key finding:** I-PASS would collapse our 8 sections to 5, drop verbatim token lists entirely, and add a one-word severity tag.

### Agent 2: External-form comparison

- **Subagent type:** general-purpose (file-read access only).
- **Context provided:** paths to existing `phases/phase-2/analysis/external-sources/{medical-handoffs,military-briefings,incident-command}.md` + one focused question (FORM and ECONOMY across domains).
- **Output:** ~600 words, written to `research/external-form-comparison.md`.
- **Key finding:** Brevity is doctrinal in all three domains — each names over-specification as a failure mode. None list verbatim tokens. None partition decisions into decide-vs-ask. None produce out-of-scope sections.

### Agent 3: Anti-anchored fresh draft

- **Subagent type:** general-purpose.
- **Context provided:** user's two-sentence brief + design constraints (plain English, no jargon, no contract framing, judgment-preserving, translate jargon outward, short prompt). **Explicitly told NOT to read existing skill files or trial finding** — the point was design from goal not from problem.
- **Output:** drafts of both skills (~25-30 lines each) + 150-word rationale, written to `research/anti-anchored-fresh-draft.md`.
- **Key finding:** A clean-room design from the brief alone arrives at the same shape as I-PASS-translation arrives at, plus the named outward-translation rule.

## Round 2 — Synthesis and v2 draft 1

Top-level agent (this conversation) read all three research outputs, identified convergences (severity tag, brief intent, no token list, no decide-vs-ask, no out-of-scope, closed-loop synthesis), distinctive contributions from each (I-PASS if-then; external-form anti-verbosity rule; anti-anchored translation behavior), and resolved tensions (decisions-made section: don't restate; trial flag: keep with version bump). Drafted both skills + rationale doc. Saved to `drafts/`.

Length: 24 + 18 lines (vs v1's 104 + 102).

## Round 3 — Reviews (4 parallel sub-agents)

Single message with four Agent calls.

### R1: Skeptic-of-fix (with trial-finding doc)

- **Context provided:** trial-finding doc + v2 draft files.
- **Question:** does v2 still embed any of the 8 named failure causes? Bias toward CUTS only.
- **Output:** Causes 1, 5, 6, 8 cleanly addressed. Causes 2, 3, 4, 7 have residue concentrated in the bulleted-bold-prefix list. Highest-leverage cut: collapse bullets into prose.

### R2: Adversarial completeness (no trial-finding doc)

- **Context provided:** v2 draft files only.
- **Question:** what concrete failure scenarios will arise from gaps in the draft? Bias toward "live with the gap" unless concrete failure case can be named.
- **Output:** 4 concrete failure scenarios (no branch/worktree marker; nested-doc projects; mid-fan-out state; no freshness signal). 3 explicit "live-with" calls (decisions log, autonomy guidance, out-of-scope).

### R3: Self-application test (no doc; given draft + fake mini-conversation)

- **Context provided:** v2 handoff skill prompt + a fake 4-turn mini-conversation about adding intermediate-status notifications to a beads task ledger.
- **Task:** actually produce the HANDOFF.md per the v2 skill.
- **Output:** 17-line handoff. Translated jargon in open question. Three named files. No padding. Used bold-label-period bullet format (which R1 and R4 then independently flagged).

### R4: Plain-language read (no doc)

- **Context provided:** v2 draft files only.
- **Question:** any phrases formal, contract-like, jargon-y? Suggest plainer rewrites or cuts only.
- **Output:** Mostly already casual. Two recurring tics: compressed noun phrases starting to coin defined terms ("handoff item", "blocking question", "real trigger"); formal-register words ("authoritative", "greppable").

## Round 4 — Revision

Top-level agent read all four reviews. Synthesized into draft 2:

- **Structural change** (from R1): collapsed 6 bold-prefixed bullets into one prose sentence. Single highest-leverage edit.
- **Date + branch on first line** (from R2): extended the trial-flag marker.
- **Dropped explicit root-file list in resume** (from R2): replaced with "follow project's CLAUDE.md reading order if there is one."
- **Wording polish** (from R4): "authoritative" → "already has", bold prefixes dropped, etc.

Saved as `drafts/session-handoff-revised.md` and `drafts/session-resume-revised.md` plus `drafts/_revisions.md` documenting changes with attribution.

Final lengths: 17 + 15 lines.

## Rounds NOT run (deferred)

- **Round 5 (user signoff & deploy)** — pending user review tomorrow.
- **Round 6 (real-session test)** — fires whenever the next real planning session occurs after deploy.

## Meta-observations from running the iteration

Three things worth recording for future iterations:

1. **The discipline propagated to sub-agent prompts.** Each prompt was kept ~25-30 lines. The meta-failure (writing 200-line review prompts to fix verbose skills) was a real risk, consciously avoided. Future iterations should apply the same discipline.
2. **Without-context reviewers (R2, R4) caught things with-context reviewers missed.** R1 was anchored on the 8 failure causes; R2 surfaced freshness/branch/nesting issues that aren't in the trial finding. Mix of context vs no-context reviewers is structurally important.
3. **The self-application test (R3) was the most efficient signal.** A 17-line handoff produced from a fake mini-session is harder to argue with than a written review. Future iterations should always include at least one self-application sub-agent.

## Cross-references

- Trial finding: [`trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](../../trial-findings/2026-04-27-skills-too-verbose-and-procedural.md)
- Iteration conventions: [`skill-iterations/CONVENTIONS.md`](../CONVENTIONS.md)
- Active trial roadmap: [`protocol-trial-roadmap.md`](../../protocol-trial-roadmap.md)
