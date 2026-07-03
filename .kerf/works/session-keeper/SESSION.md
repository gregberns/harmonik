# session-keeper — Session log

**Work:** session-keeper (plan jig) · **Bead:** hk-ekap1 (P2) · **Author:** flywheel thread · **Date:** 2026-06-02

## What this work is
Designs the per-orchestrator context-window watcher that lets a long-running Claude Code orchestrator (flywheel/named-queues/controlpoints) run indefinitely without lossy auto-compaction — by injecting a wrap-up warning at ~80% and driving an intent-preserving `/session-handoff → /clear → /session-resume` cycle at ~90%. Realizes the "managed context, no compaction" capability scoped by the parent `flywheel` kerf work.

## Passes (all complete → ready)
1. Problem space — `01-problem-space.md` (goals/non-goals/success criteria from hk-ekap1).
2. Analyze — `02-analysis.md` (3 investigations: Claude Code surfaces; pasteinject+supervise; handoff/resume skills+registry).
3. Decompose — `03-components.md` (C1 gauge, C2 watcher, C3 injector, C4 cycle, C5 backstop, C6 events).
4. Research — `04-research/findings.md` (8 decisions incl. hosting=A standalone keeper).
5. Spec — `05-specs/session-keeper-spec.md` / `SPEC.md`.
6. Integration — `06-integration.md`.
7. Tasks — `07-tasks.md` (Phase 1 SK-1..6 / Spike SK-7 / Phase 2 SK-8..11 blocked).

## Key decisions
- **Hosting = (A)** standalone `harmonik keeper --agent <name>`, one per orchestrator — keeps named-queues' `supervise` contract untouched. *(Pending operator confirm vs (B) generalize supervise.)*
- **Phase 1 (warn-only) ships + dogfoods first**; non-destructive.
- **Phase 2 gated on a 4-experiment spike (SK-7)** — the empirical unknowns the review surfaced.

## Review outcome (independent adversarial reviewer)
Verdict **NEEDS-REWORK → addressed.** Blockers folded into the spec: (1) keeper-nonce handoff-confirmation replacing the non-unique date+branch stamp; (2) post-`/clear` readiness probe before resume; (3) anti-loop + crash-recovery journal; plus risk fixes (single-keeper lock, opt-in `.managed` marker, no-gauge self-check, dispatch-gating attribution). Phase-2 build gated on §12 experiments.

## Open before finalize
- Operator A/B confirm (§2).
- Spec-text check-in before copying `SPEC.md` → `specs/`.
- Whether to create Phase-1 + spike beads now and dispatch Phase 1 via the daemon.
