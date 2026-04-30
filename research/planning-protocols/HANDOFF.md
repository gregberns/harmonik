<!-- PP-TRIAL:v2 2026-04-28 main -->
# Session Handoff — Planning-Protocols Track

## Status

**green** — v2 deployed and validated in a real session (n=3 positive).

## What we're doing

Iterating on the `/session-handoff` and `/session-resume` skills under the planning-protocols trial. v2 replaced v1 today (2026-04-28) after [trial finding 1](trial-findings/2026-04-27-skills-too-verbose-and-procedural.md) diagnosed 8 named failure causes in v1.

## Where things stand

- **v2 live** at `~/.claude/skills/session-{handoff,resume}/SKILL.md` — 16 + 14 lines vs v1's 103 + 101.
- **v1 archived** at [`skill-iterations/v1-baseline/`](skill-iterations/v1-baseline/) for revert/comparison.
- **n=3 positive signal:** user ran v2 on the basata DocFlow kerf session today. Handoff went from 100 to 16 lines. Resume produced a clean plain-English paraphrase with naturally-translated jargon and a plain-English close (*"Ready to move on to Pass 7 (Tasks)?"* instead of v1's *"Decision — advance kerf?"*). Branch+date freshness check fired and was clean. The shape is holding up in real use.

## Two parked decisions (neither blocks further work)

1. **Phase 3 framing.** Keep `trial-findings/` and `skill-iterations/` as cross-phase active forward-work (current placement, recommended), or formalize as `phases/phase-3/`. See [`STATUS.md`](STATUS.md) 2026-04-28 entry for the three options.
2. **Deeper pattern.** harmonik a121e7f1 showed the same failure shape outside `/session-resume` (pilot-review protocol). Worth pulling into its own trial finding or a candidate evaluation-framework dimension (implementation-form risk)? Currently captured as one observation inside trial finding 1, Part 2.D.

## What fires on its own

The n=3+ trial signal accumulates as more real sessions use v2. If a new failure surfaces, that triggers trial finding 2 → v3 iteration per [`skill-iterations/CONVENTIONS.md`](skill-iterations/CONVENTIONS.md). Otherwise v2 keeps doing its job quietly.

## First files to open

1. [`STATUS.md`](STATUS.md) — 2026-04-28 entry has full chronology of trial finding + iteration + deployment.
2. [`trial-findings/2026-04-27-skills-too-verbose-and-procedural.md`](trial-findings/2026-04-27-skills-too-verbose-and-procedural.md) — the diagnosis. (This is also the doc the user passes to other agents who need to understand the issue.)
3. [`skill-iterations/v2-2026-04-28/process.md`](skill-iterations/v2-2026-04-28/process.md) — how the iteration was run, if you want to repeat the shape for v3.
4. [`protocol-trial-roadmap.md`](protocol-trial-roadmap.md) — other forward work parked behind trial signal.
