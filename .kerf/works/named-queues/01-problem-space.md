# named-queues — Problem Space

**Codename:** named-queues  ·  **Type:** plan  ·  **Status:** problem-space → analyze
**Authored:** 2026-05-31 from the user's design brief (thread re-derived after a lost conversation).

## Summary

Today harmonik runs a **single daemon-singleton queue** (`QM-027` in `specs/queue-model.md` forbids a second active queue), with one **global** `--max-concurrent` worker cap and one **global** pause/resume. This work generalizes that into **multiple concurrently-active, independently-named queues** ("channels"). Each named queue is an ordinary FIFO work queue with its own worker-slot count, its own lifecycle (start / pause / resume / stop), and an informational focus. Beads are routed to a named queue at submit time.

The motivating use case: the **flywheel** (the long-running Pi cognition loop) finishes a harmonik job and notices something untested, a bug, or other follow-up work. Rather than chase it inline or spin off its own sub-agents, the flywheel **files an `investigate` bead and submits it to a dedicated queue** (e.g. `investigate`). A *separate*, daemon-managed Claude Code session — billed against the **subscription**, not the Pi API key — pulls from that queue and either fixes the issue or files follow-up beads. The flywheel is **not** responsible for following up; the handoff is fire-and-forget through the queue.

This is intentionally a **standard queue system**, not a novel abstraction: ~20 items can sit in a queue while N workers process them M at a time, per queue. "This really isn't that hard a concept" (user). The design bias is toward the simplest thing that matches existing queue-system conventions.

## Goals

- **G1 — Multiple named queues.** A single daemon hosts N concurrently-active queues, each identified by a human-supplied name (e.g. `main`, `investigate`).
- **G2 — Per-queue worker slots.** Each queue carries its own max-concurrent worker count; the **sum across queues is bounded by a global daemon ceiling** (machine capacity).
- **G3 — Per-queue lifecycle.** Each queue can be independently started, paused, resumed, and stopped, without affecting the others.
- **G4 — Routing.** A bead is submitted (or appended) to a specific named queue.
- **G5 — Flywheel investigate handoff.** The flywheel files an `investigate` bead and submits it to a dedicated queue, where a separate **subscription-billed, daemon-managed** Claude Code session processes it (fix or file follow-up beads). The flywheel does not track the outcome.

## Non-goals

- **N1 — No flywheel-spawned sub-agent framework.** The flywheel does NOT spin up its own investigator agents. Handoff goes *through a queue* so the investigator runs as daemon-managed (subscription-billed) claude. (User explicitly chose this over building spin-off machinery.)
- **N2 — No per-queue spend budgets in v0.1.** Keep credfence's single unified daemon spend meter shared across all queues. (See Open Decision 1 — this is the one fork worth the user's read.)
- **N3 — No cross-queue priorities / weighted fairness / conditional ordering.** Those are the extqueue v0.2 deferred surface (`specs/queue-model.md` §A.3); out of scope here.
- **N4 — No new "channel" type distinct from a queue.** A channel *is* a named queue. We do not introduce a second abstraction. "Channel" is treated as a synonym for "named queue."
- **N5 — Flywheel does not own follow-up.** No outcome-tracking, retry, or escalation loop back into the flywheel for investigate beads (fire-and-forget).

## Constraints

- **C1 — Layer on extqueue v0.1.** Generalize `specs/queue-model.md`, don't replace it. The core change is relaxing `QM-027` (single-active-queue) to allow N named queues, plus adding a name/identity dimension.
- **C2 — Preserve a global concurrency ceiling.** Lesson from prior runs: wide concurrency exhausts disk + CPU; ~4–5 wide is the knee on this 10-core box. Per-queue worker counts must sum under a global daemon cap.
- **C3 — Backwards-compatible default queue.** Existing `harmonik queue submit <beads>` with no queue name lands in a default queue named `main`. Existing single-queue scenario tests keep passing.
- **C4 — Reuse pilot's pause/resume.** The agent-callable operator-pause producer landed in `pilot` is reused, scoped per-queue.
- **C5 — Subscription billing for the investigator.** The investigate-queue agent runs as daemon-managed claude on the subscription (NOT Pi/API). Credential path already built in `credfence`.
- **C6 — Multi-queue persistence.** Migrate from the singleton `.harmonik/queue.json` to a multi-queue registry (per-queue files or a registry index), preserving atomic writes / single-writer guarantees.

## Success criteria (concrete, verifiable)

- **SC1** — Operator runs `harmonik queue submit --queue investigate <bead>` and `harmonik queue submit --queue main <beads>`; `harmonik queue list` shows BOTH queues `active` simultaneously.
- **SC2** — `main` with 3 workers and `investigate` with 1 worker process concurrently; at most 4 claude sessions ever run, and the total never exceeds the global cap.
- **SC3** — `harmonik queue pause investigate` halts new dispatch on `investigate` while `main` keeps dispatching; the in-flight investigate run reaches a terminal state; `harmonik queue resume investigate` restores dispatch.
- **SC4** — The flywheel files an investigate bead and submits it to the `investigate` queue; a daemon-managed (subscription-billed) claude session pulls it and either commits a fix (`Refs:` trailer) or files follow-up beads — and the flywheel neither blocks on nor tracks the outcome.
- **SC5** — A `harmonik queue submit <beads>` with no `--queue` flag lands in `main`; existing single-queue scenario tests still pass unchanged.

## Dependencies on this morning's work (ordering)

- **extqueue v0.1** (`hk-lj0pb`, ✅ landed) — the single-queue control surface + `specs/queue-model.md` contract this work generalizes. **Hard prerequisite.**
- **pilot** (✅ landed) — provides the `queue submit/append` curated-dispatch surface and the agent-callable pause/resume producer reused per-queue. **Hard prerequisite** for G3 and G5.
- **credfence** (✅ landed) — the unified spend meter that Open Decision 1 keeps global. **Touch-point**, not a blocker.
- **cognition-loop / flywheel bridge** — the investigate-handoff (G5/SC4) plugs into the flywheel's post-run reflection. The multi-queue *core* (G1–G4) has no flywheel dependency and can land first; the handoff piece sequences after the core + alongside the flywheel.

## Open decisions (for the user)

1. **Budget scope (the one I'd want your read on).** Keep ONE unified daemon spend budget shared across all queues (default — simple, matches credfence; risk: a busy `investigate` queue can starve `main`'s budget), **or** add per-queue budget caps (more control, more moving parts). *Recommendation: shared global for v0.1; per-queue caps as a follow-up bead.*
2. **Term.** "named queue" (standard; what the spec will use) vs. "channel" (your earlier word). Identical thing. *Decided: "named queue", with "channel" as a documented synonym.*
3. **Queue creation.** Implicit-on-first-submit (`--queue investigate` auto-creates with default worker count) vs. explicit `harmonik queue create <name> --workers N`. *Decided: implicit-on-submit, plus an optional explicit `create` for setting workers/focus up front.*
