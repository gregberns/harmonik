# 05 — Spec-draft changelog (`run-state-machine`)

> Pass 5 output summary. One NEW spec drafted; no existing spec amended.

## New spec
- **`specs/run-state-machine.md`** — spec-id `run-state-machine`, prefix **RX**
  (verified free in `specs/_registry.yaml` 2026-07-14). 20 requirements
  (RX-001..RX-020) + 5 invariants (RX-INV-001..005). Draft:
  `05-spec-drafts/run-state-machine.md`.

## Deliberately NOT amended
- **`specs/event-model.md`** — RX-018 pins zero new event types (M3-D10).
- **`specs/session-keeper.md` / `specs/replay-substrate.md`** — cited by
  reference only (RX-004, RX-INV-003); no text change. One OPTIONAL reciprocal
  reference-touch on each is carried to Integration (§2 there), mirroring P1's
  R5 pattern.

## Landing actions (at finalize)
1. Reserve `RX: {spec-id: run-state-machine, reserved: <land-date>, status: draft}`
   in `specs/_registry.yaml` — same commit as the spec.
2. Land AFTER `replay-substrate.md`/`session-keeper.md` are finalized (both
   already landed on this branch — satisfied).
3. Reciprocal reference-touches (optional, integration §2): SK §9 + RS §9 gain a
   one-line pointer to RX as "the daemon instantiation".

## Supersessions / corrections carried in the draft
- RX-015 supersedes the problem-space §2 literal wording "push … outside any
  global daemon lock" with the rollback-pairing-grounded formulation (M3-D5,
  00b R2) — recorded for the planner.
- The §1 motivation states the measured resume-path behavior (three
  uncoordinated bounds, replay-invisible gap), correcting the census
  "in_progress forever" folklore (00b R5).
