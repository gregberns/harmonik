---
id: skill-copy-governance
title: Four physical copies of the shipped skills exist and only two are governed by any rule
type: bug
priority: 1
labels: [agent-config, clear-the-ground]
depends_on: [reviewer-subagent-drift]
blocks: []
workstream: W7
batch: 1
---

## Problem

The shipped skill set exists four times on disk:

1. `cmd/harmonik/assets/skills/` — the source of truth, embedded via `//go:embed`.
2. `.claude/skills/` — generated mirror. `harmonik sync-assets` overwrites it from the embed.
3. `.harmonik/agents/_skills/` — a third copy of `agent-comms`, `beads-cli`, `crew-launch`,
   `harmonik-dispatch`. Undocumented. `sync-assets` does not touch it. Byte-identical as of 23 Aug.
4. `.claude/agents/` — the Agent-tool definitions for the two reviewer skills. **Already drifted**
   (see `reviewer-subagent-drift`).

`AGENTS.md` §Key conventions governs the pair (1)↔(2) and nothing else. The two ungoverned copies are
where the drift happened, and there is no check that would have caught it.

## Scope

- `AGENTS.md` §Key conventions — the statement of which copies exist and which rule governs each.
- A gate that fails when any two copies of the same skill body diverge.

Prefer removing a copy over governing it. A copy that can be generated at build time or read from
the embed does not need a rule; it needs to stop being a checked-in file.

## Done when

1. Every physical copy is either (a) eliminated, or (b) covered by a gate that fails the build when
   it diverges from the source of truth.
2. The gate FAILS when one copy is edited and the others are not — proved by mutation, one case per
   copy that survives.
3. `AGENTS.md` names every surviving copy. A copy that exists and is not named in the convention is
   the exact state that produced this bug.

## Limits

- **Do not create a fifth copy** in the course of fixing this.
- Do not reverse the generation direction. `cmd/harmonik/assets/skills/` is the source; there is no
  reverse sync and adding one is a larger decision than this task.
- `scripts/agents-skills-sync.sh` (333 lines) claims to enforce a single skill tree and has no
  caller anywhere in the repo. Decide whether it is the answer or whether it is dead weight — do not
  silently leave it sitting there either way.
