# The case for isolated crew / distributed compute (operator, 2026-07-02)

This is the *why* behind the whole cluster — the operator's argument for pushing a crew (or an
agent) into its **own full isolated environment** with its **own queue to dispatch into**. It ties
ideas 1 (multi-node), 2 (sandbox), and 5 (harness seam) together and gives them a shared objective.

---

## OBJECTIVE

> **Run a crew / an agent in its own sandbox, with its own set of work to do — a full environment,
> where the agent has a queue to dispatch into.**

"Isolated" = **own filesystem, own branch/worktree** — not just a subfolder inside the main repo.
The agent must not be able to accidentally write to the primary repo folder / main.

## What this gets us — the triggering scenarios

The signal for "push this crew into its own isolated environment" is when work is **more than a
single queue item**:

1. **Medium ad-hoc work like planning.** Planning takes a while, needs research, and may need
   several **agent↔operator rounds**. It's not a one-shot bead — it's a sustained session with its
   own sub-work.
2. **Complicated epics with heavy testing.** Often better run by an agent that **delegates to
   subagents** than by dropping one item on the queue. The epic-owner needs to *fan out* its own
   work.

The common shape in both: **an agent that has a queue OR can delegate to subagents, AND must run
isolated** (so its subagent/worktree churn can't touch main). That combination *is* the trigger to
give it its own environment.

## Two capabilities this unlocks

- **Different harnesses per crew.** Once a crew runs in a properly isolated env with all the right
  mechanisms wired, the harness driving it becomes swappable — Claude, **Codex**, Pi. (Operator is
  using Codex heavily elsewhere and it "has pretty much all the same conventions as Claude Code."
  The concrete pain that motivates isolation: **Codex started working in the main repo when it
  needed to start in its own worktree/env.** Isolation is the fix.) → this is exactly **idea 5's
  pluggable-harness seam**.
- **Operator can drop in directly.** We already start crew in their own tmux — valuable when they
  work only with the captain, *and* because when the operator needs to work directly with a crew
  member, they **just get in via tmux**. The isolated env must preserve this: **tmux-attachable,
  even when sandboxed/remote** (→ idea 1's `harmonik attach <node>/<crew>`; the sandbox must not
  break inspectability, which is a locked design preference).

---

## How this reframes the cluster

This objective is the connective tissue:

- **Idea 2 (sandbox)** was framed "tasks first, then crew." This case argues the **crew-level**
  isolation is itself a primary goal, not a later phase — because planning/epic-ownership is where
  the accidental-write-to-main risk is highest and the subagent fan-out is heaviest.
- **Idea 1 (multi-node)** provides *where* an isolated crew can live (its own box) and the
  attach-ergonomics.
- **Idea 5 (harness seam)** provides *what* runs inside — and the isolation is precisely what makes
  a non-Claude harness (Codex/Pi) safe to run, since the env boundary is what stops it clobbering
  main.

**The unifying requirement:** an "isolated crew env" = own filesystem + own branch + own queue/
delegate capability + still tmux-attachable. Every idea in this cluster is a facet of delivering
that. See the **new dedicated plan** for the highest-priority slice: running **Pi in a sandbox for
daemon jobs** (`plans/2026-07-02-pi-sandbox/`).
