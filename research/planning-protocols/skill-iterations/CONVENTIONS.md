# Skill Iterations — Directory Convention

> How to lay out a skill-iteration directory. Read this before starting a new iteration.

This directory holds successive iterations of the trial skills (`/session-handoff`, `/session-resume`, and any future trial skills). Each iteration is a directory; iterations are numbered + dated; the structure inside each iteration is standardized so the next iteration can copy the pattern.

## Naming

`v<N>-<YYYY-MM-DD>/` — version number + start date.

- `v1` is the original (pre-trial-finding) skill set. We do not snapshot it here; it lives at `~/.claude/skills/session-{handoff,resume}/SKILL.md` until replaced. If a v1 snapshot is needed for retrospective comparison, copy it into `v1-baseline/` at that point.
- `v2-2026-04-28/` is the first rewrite (this iteration).
- `v3-DATE/`, `v4-DATE/` etc. for further iterations.

## Iteration directory layout

```
v<N>-<DATE>/
  research/                  Inputs that informed this iteration.
                             Per-source files; no fixed list.
                             Examples: ipass-deep-dive.md, external-form-comparison.md,
                                       anti-anchored-fresh-draft.md.
  drafts/                    Skill drafts and their rationale.
                             session-handoff.md          (initial cut)
                             session-resume.md           (initial cut)
                             _rationale.md               (rationale for initial cut)
                             session-handoff-revised.md  (post-review cut, if any)
                             session-resume-revised.md   (post-review cut, if any)
                             _revisions.md               (what changed initial → revised)
  reviews/                   Sub-agent reviews of the initial draft.
                             r1-<frame>.md, r2-<frame>.md, ...
                             At least one self-application test
                             (sub-agent runs the draft on a fake mini-conversation,
                             output captured as r<N>-self-application-output.md).
  process.md                 How this iteration was conducted.
                             Sub-agent prompts used, rounds run, time taken,
                             which sub-agents had which context.
                             Reproducible if a future iteration wants to follow.
```

## What each file is for

- **`research/`** — exterior inputs (web research, external-source re-reads, fresh-draft from brief). One agent per file, parallel where independent.
- **`drafts/`** — the human-author (or top-level agent) synthesizes the research into a draft. Rationale doc explains design choices with attribution to research inputs.
- **`reviews/`** — orthogonal review angles, run in parallel as sub-agents. Mix of with-context and without-context reviewers (some have prior trial findings; some don't, to avoid anchoring).
- **`drafts/*-revised.md`** — post-review cut. Rationale for which review feedback was accepted / rejected lives in `_revisions.md`.
- **`process.md`** — meta-record of how the iteration was run. The skill drafts are the deliverable; `process.md` is how-it-was-built.

## Round structure (default; vary as needed)

1. **Round 1 — Research (parallel).** 3 agents typical: a deep-dive on a specific external protocol (e.g., I-PASS), a re-read of existing external-source files for shape lessons, an anti-anchored fresh draft from the user's brief alone.
2. **Round 2 — Synthesis & draft.** Top-level agent reads research outputs, writes draft + rationale. No sub-agents; concentration matters.
3. **Round 3 — Review (parallel).** 4 reviewers typical: skeptic-of-fix (with trial-finding context), adversarial completeness (without), self-application test (runs the draft on a fake mini-session), plain-language read.
4. **Round 4 — Revise.** Top-level agent synthesizes reviews into revised drafts + revisions doc.
5. **Round 5 — User signoff & deploy.** User reviews; if approved, replace `~/.claude/skills/session-{handoff,resume}/SKILL.md` with the revised drafts.
6. **Round 6 — Real-session test.** Use the new skills on the next real planning session. Adopt-and-notice. If the same failure mode (or a new one) shows up, that's a new trial finding triggering a v3 iteration.

## What to avoid

The same failure modes that motivated the rewrite apply to this directory's own work:

- **Don't write long reviewer prompts.** ~25-30 lines max. Verbose prompts produce verbose reviews.
- **Don't list every possible review angle.** Pick 3–4 orthogonal ones. A 7-reviewer panel produces redundant output.
- **Don't have committee synthesis.** A single agent (or human) writes the revision, citing which feedback came from which review. Synthesis-by-vote loses the reasoning.
- **Don't pad the rationale doc.** Attribute each design choice to a research input or review. If you can't attribute it, it might be unjustified.
- **Don't auto-deploy the revised skill files.** User signoff before replacing `~/.claude/skills/`. Each iteration is a proposal until accepted.

## Cross-references

- Trial findings that motivate iterations live at [`../trial-findings/`](../trial-findings/).
- The active trial roadmap is at [`../protocol-trial-roadmap.md`](../protocol-trial-roadmap.md).
- The methodology that governs the broader research track is at [`../METHODOLOGY.md`](../METHODOLOGY.md).
