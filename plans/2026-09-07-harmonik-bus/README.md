# Harmonik Bus — Track 2 of 2

Started 2026-09-07. Operator-initiated. Sibling track: [`../2026-09-07-harmonik-restructure/`](../2026-09-07-harmonik-restructure/).

> **READ THIS FIRST — `05-CORRECTION-the-real-design.md`.** The `01-base-plan.md` and
> `02-execution-plan.md` in this folder were built off the shallow seed and a research pass that
> missed the real work. The bus is **already designed to the proto level and has six locked
> operator decisions (C1–C6, 2026-07-21)**. It is the **kernel/fabric** (`internal/kernel/`), not
> a "transport"; hub/spoke competing-consumers is **core (`POINT_TO_POINT` + the `dispatch`
> primary/worker plugin)**, not deferred. Treat `01`/`02` as superseded by `05` and by the P1
> program. Authoritative sources are listed in `05`.

## Why this exists (operator intent)

Agents need to talk to each other. Tools need to communicate. Systems may run on
different machines: a **hub** exposes jobs to be completed, and a **spoke**
processor picks up a job and runs with it. All of this needs one **substrate** to
communicate over. Tools plug into a mechanism where they receive and publish
messages.

The seed doc here is just the idea. **The real planning went a lot deeper** and
already exists in the repo — the base plan's job is to find it, consolidate it, and
not re-invent it. Once the restructure basics are in place, the operator wants the
**fundamentals** of the bus built.

## The shape

A `Bus` interface (Publish / Subscribe / Request), a `Service` plugin contract
(Name / Subjects / Handle), and `Mount` to attach any Service to any Bus. An
in-memory bus for the embedded single-binary case and for tests; a networked bus
(NATS, per the seed) for the distributed hub/spoke case. **The tool code is
identical either way** — embedded vs networked is a wiring choice in `main`. Never
Go's `plugin` package.

## Prior planning to consolidate (do not re-invent)

- `../2026-06-30-distributed-fleet/` — hub/spoke + peer comms.
- `../2026-07-21-platform-architecture/`, `../2026-07-21-p1-kernel-fabric/`,
  `../2026-07-21-p3-distributed-execution/` — the P1/P2/P3 platform program.
- Existing code: `internal/eventbus`, `internal/presence`, `internal/dispatch`,
  the `harmonik comms` bus, the `agent-comms` skill.

## Files

- `00-seed-bus.md` — the operator's seed idea. The Bus/Service/Mount contract.
- `01-base-plan.md` — the grounded base plan: what exists, what was already
  planned, the reconciled design, the fundamentals to build first. *(In progress.)*

## Relationship to the restructure track

The bus lives in the clean-room module the restructure stands up. Interface design
and this consolidation proceed in parallel now; the first real bus code waits on
the clean-room scaffolding.
