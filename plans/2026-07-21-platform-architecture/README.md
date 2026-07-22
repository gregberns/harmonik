# Platform Architecture — 2026-07-21

## Origin

This plan spun out of the Codex/remote realignment (`plans/2026-07-20-codex-strategy-
realignment/`). That effort concluded the ssh-per-node remote model is the wrong shape
(D4: scrap it). Pushing on "what replaces it" surfaced a much bigger operator concern:
**the underlying architecture itself needs to be gotten right** — not another partial,
hard-coded remote hack.

## The goal (operator)

Build a **really powerful underlying architecture** that other tools can be run on top of.
Today harmonik is a **large monolith** that does many things with lots of tight coupling.
The operator does NOT necessarily want multiple deploy artifacts — but DOES want:

- Internal components **split up and modular**.
- An underlying set of **layers that build on each other via consistent interfaces**,
  instead of every layer hard-coded to every other.
- Easy to **test**, easy to **extend** with new subsystems.
- **Build for the future — not a half-assed partial implementation.**

Explicit operator framing to honor: **this is NOT over-architecting.** A recurring pattern
has been to dismiss the need for a strong underlying system as too complicated / over-built.
The operator has raised distributed-architecture ideas multiple times and felt them
misunderstood or dismissed. Breaking that pattern is a first-class goal of this plan.

## Process (operator-set)

1. **Align on the PROBLEM first** — before digging into solutions. (We are here.)
2. Write everything down as we go.
3. This will likely become **two or more plans** (see "Likely split" below) once the
   problem is clear and agreed.

## Likely split (to confirm with operator)

Two intertwined but distinct threads:
- **Thread P — Platform / foundation layering.** The strong underlying architecture: a
  daemon-hosted **data transport layer** across machines, an **identity/address** system so
  components/plugins can discover each other, generic transport over that layer, and general
  decoupling (keeper, watchdog, harnesses ⟂ queue/comms/bead/DOT). Source of inspiration:
  `plans/2026-07-15-agent-substrate-v2` (its ARCHITECTURE, not its "data-sharing" premise).
- **Thread D — Distributed execution.** Running beads across machines / in containers,
  built ON Thread P. Replaces the scrapped ssh model. Incorporates
  `plans/2026-06-30-distributed-fleet`, `plans/2026-07-19-incus-container-remote-mode`,
  and scheduling (`plans/2026-07-09-scheduling-assessment`).

Thread P is the foundation; Thread D is a major thing built on it. Do NOT design D without P.

## Layout

- `INPUTS.md` — everything the operator has put on the table (concerns, architecture
  options, prior-art pointers). Written down verbatim-in-intent.
- `research/` — subagent absorption of the four prior plans + a current-coupling survey.
- `PROBLEM.md` — the problem statement for alignment (the immediate deliverable).
- `NOTES.md` / `DECISIONS.md` — as we go.

## Not in scope yet

No solutioning, no architecture decisions, until the problem statement is agreed.
