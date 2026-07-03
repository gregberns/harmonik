# tooling-capture — OPEN-CONTRIBUTION work  ⚠️ NEW KERF CONVENTION (v0)

> **This is NOT a normal plan.** Do **not** drive it through the plan passes toward a spec.
> It is an **open-contribution aggregation bin** — it stays in collection mode indefinitely
> until we *explicitly* decide to formalize. New idea to kerf; see kerf-beta-feedback.

## What this is
A living collection point for the emergent tooling, watchdogs, and safety/operational
patterns that accrete while building harmonik (escape-detector, pasteinject watchdogs,
reconciler/orphan-sweep, daemon keeper, monitors, pre-screen scouts, the session-keeper
idea, …). **Capture-only.** Aggregate over days; decide later what — if anything — to
formalize.

## OPEN-CONTRIB convention  (the marker — for ANY agent, human or AI session)
Any agent may contribute ideas here, no gatekeeping, no formalization required to add.
The marker is the **`contrib-open` label**:

- A bead labelled **`contrib-open`** is explicitly open for anyone to add to.
- **To contribute:** either
  1. comment on the canonical bin bead **`hk-nlhys`** — `br comments add hk-nlhys "…"`, or
  2. create a new bead labelled both `contrib-open` and `codename:tooling-capture` (it
     auto-attaches to this work via the bead_filter), or
  3. append to a linked capture doc (e.g. the peer's `.harmonik/comms/capture-daemon-lane.md`).
- Lower the bar to capture. A half-formed observation is worth more here than silence.

## Canonical bin + sources
- Bead **`hk-nlhys`** — the main capture-only inventory (labels: `contrib-open`,
  `codename:tooling-capture`).
- Peer (`named-queues`) feeds via `.harmonik/comms/capture-daemon-lane.md`.
- Standing maintenance: memory `emergent-tooling-capture` reminds every session to fold in.

## Why it's a new kerf idea (flagged to kerf-beta-feedback)
kerf works today assume a single driver advancing passes toward a spec/finalize. This is the
inverse: a **multi-contributor, never-necessarily-completed collection**. Candidate kerf
feature — an `aggregation` / `open-contrib` work *mode* that (a) doesn't force pass
progression and (b) advertises itself as contributable to other agents.
