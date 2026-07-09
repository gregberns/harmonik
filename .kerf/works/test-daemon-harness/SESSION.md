# SESSION — test-daemon-harness

## Planning walk (2026-07-07)
Walked the plan jig analyze -> ready in one pass. Key discovery: the CORE
capability was already LANDED and closed before this walk (beads hk-4tdlw,
hk-6vr02, hk-1gkc8, hk-6eqv9 — the full init/build/up/down/cycle/batch/feedback
loop + hermetic end-to-end smoke, plus runbook + methodology docs). The plan was
therefore authored RETROACTIVELY: it documents the as-built territory and scopes
only the genuine remaining gap (verification durability), not a greenfield build.

## Artifacts produced (on the bench)
- 02-analysis.md — as-built map + code-health gaps
- 03-components.md — 5 components (C1-C3 landed; C4/C5 = the gaps) + design decisions
- 04-research/{c4-verification,c5-durability}/findings.md
- 05-specs/{c4-verification,c5-durability}-spec.md
- 06-integration.md, SPEC.md (N1-N6 normative requirements)
- 07-tasks.md — 5 dispatchable tasks (T1-T5)

## Handoff to the captain
07-tasks.md defines T1-T5 (golden corpus, CI gate, `gc` subcommand, recorded live
run, make target + runbook section). All small, independent, fleet-safe. Label
`codename:test-daemon-harness`; do not pre-assign or pre-set in_progress.

## Open decisions surfaced to operator
1. Plan is retroactive — if "landed + documented + hermetic smoke" is deemed
   sufficient, T1-T5 are optional hardening and the work can close.
2. `gc --hard` default (soft vs always-reset beads DB).
3. T2 gate: CI job vs documented pre-merge rule (depends on PR CI runner existing).
