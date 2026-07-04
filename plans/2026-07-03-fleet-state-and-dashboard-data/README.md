# Fleet state & dashboard data — PLACEHOLDER (deferred)

**Date opened:** 2026-07-03 · **Status:** parked — offloaded from the agent-identity/manifest work
(`../2026-07-03-agent-identity-and-context/`) to keep that stream focused. This is likely a LARGER
project than the manifest work and is a separate initiative / later phase. Ties into
`../2026-07-03-operator-dashboard/`.

## Why this exists
While designing the agent manifest we kept hitting "how do agents publish changing state?" — a real,
felt pain (the admiral's priorities go stale; `admiral-initiatives.md` rots). The operator wants to
solve the manifest FIRST, then return here. Everything below is captured so it's not lost.

## The core idea — "publish, don't narrate"
Stop telling agents to hand-edit prose files (which rot, sometimes aren't git-tracked, aren't
queryable). Instead agents call **explicit commands with structured updates** → a structured store →
flushed to a git-tracked artifact (the beads `br sync --flush-only` pattern: SQLite/JSON store +
JSONL export tracked in git = queryable AND diffable-over-time). The human-readable file becomes a
*generated view*, not a hand-edited source.

## Two distinct things to design here (operator's framing)
1. **A semi-generic / flexible data structure** defining what gets exposed — to the admiral AND
   through the operator dashboard. harmonik should NOT hardcode a `priorities` type; it's a general
   data-management surface that gets *configured*. (Priorities is one instance of it.)
2. **An orchestration-management data structure** an agent uses to track where things are + bead
   status — the "manage the overall project" layer, distinct from beads' "manage the tasks" layer.

## Lightweight capture idea (parked from manifest discussion)
- **Self-directed tagged bus messages as notes.** An agent messages ITSELF on the comms bus with
  **tags** → a searchable/filterable note trail, no new store needed. Question to resolve here: can
  comms messages carry tags today, and are they queryable? Could be the cheapest first version of
  "agent publishes state" before any structured store exists.

## Adjacent levers already in the repo (explore before building new)
- **`bv`** (beads_viewer) — under-used; graph metrics / relationships.
- **Better bead priority modeling** — manage relationships + priorities IN beads so `br` reflects
  priority order; could task an agent with maintaining it. (Task-management side is largely solved;
  the gap is project-level orchestration state.)
- Do NOT overload beads with orchestration intent — different data than work-items. Separate store.

## Related pain to fix when we get here
- `admiral-initiatives.md` is stale (operator has re-asked priorities repeatedly).
- `admiral-playbook.md` has degenerated into "a bunch of incident reports smashed together" — the
  LEDGER-as-hand-edited-prose antipattern. A publish seam + generated views would prevent this.

## Do NOT start this until the manifest work is landed / operator re-opens it.
