# Agent identity, roles & layered context injection

**Date:** 2026-07-03 · **Driver:** admiral + operator · **Status:** research + planning (not dispatched)

## The problem the operator raised

The fleet runs well unattended for hours, but **degrades after ~6h**: role boundaries blur
(admiral takes on captain's work), and roles seem to "forget their job" across many session
resumes. We keep sessions alive via keepers (restart at a context threshold) + session-handoff /
session-resume skills — that maintains *continuity* but not *identity*.

Prior art the operator points at: **OpenClaw → Hermes `SOUL.md`** — a file that describes *who
the agent is*. (See `../2026-06-30-distributed-fleet/05-hermes-harness-study/` — Hermes profiles
carry a `SOUL.md` persona + per-profile config/skills/keys.)

## What the operator wants (verbatim intent)

1. **A durable role identity** that doesn't degrade over sessions — the agent should always know
   *what its job is*, separate from *what happened last session*.
2. **Three distinct context layers** at session-resume, kept separate:
   - **Mission / job description** — the role's responsibilities (durable, per-role).
   - **Operating instructions** — the skills & knowledge this role needs (durable, per-role).
   - **Last-session handoff** — what happened, open items (ephemeral, per-session).
3. **Pluggable roles** — someone standing up harmonik should be able to rename/replace roles
   (mayor instead of captain, governor instead of admiral) and **add new roles** without the
   system stopping them. (captain may be a partial exception — it's a first-class `harmonik start
   captain` citizen from early on.)
4. **Per-role knowledge scoping** — AGENTS.md is getting too large; not every agent needs all of
   it. A queue worker doesn't need to know how to add to the queue; crew needs queue-add but not
   stack-startup; admiral/captain need stack management. Scope knowledge to the role.
5. **A configuration for all agents in the system** — so on launch, the right data is injected
   into each agent's context. Open question: *how* we inject it (startup message? declared skills
   to read? a config file the launcher reads?) and *how we layer it*.

## Decomposition — components to work with

Draft breakdown (to be refined against what investigation finds already exists):

- **C1 — Role model / "SOUL":** what defines a role. The durable identity: name, mission,
  responsibilities, boundaries (what it does NOT do). The unit that survives session resume.
- **C2 — Context layering contract:** the 3-layer split (mission / operating-instructions /
  handoff) and how session-resume assembles them. Which layer owns what today.
- **C3 — Agent configuration surface:** a declarative per-role config (what exists: `.harmonik/
  crew/*.json`, mission files, launch flags) → what a unified "agent registry" could look like.
- **C4 — Injection & layering mechanism:** how data reaches context at launch — the startup
  message the daemon seeds, the skills a role loads, the handoff read order. Where each piece is
  authored vs assembled.
- **C5 — Knowledge scoping / AGENTS.md decomposition:** the per-role load map (already partly in
  AGENTS.md "Per-role load map") → moving from one big file toward role-scoped knowledge.
- **C6 — Pluggability / renameability:** decoupling role *function* from role *name*; how a
  deployer adds/renames roles; the captain first-class-citizen exception.
- **C7 — Role-boundary degradation:** the actual failure — admiral↔captain overlap after ~6h.
  What causes drift, and whether a durable identity layer fixes it.

## Investigation (what exists today) — see `investigation/`

Five parallel probes launched 2026-07-03; findings land as `investigation/0N-*.md`:
- 01 — how roles are defined today (skills, missions, json, launch path)
- 02 — launch & context-injection mechanics (Go daemon startup message, seed, keeper re-hydration)
- 03 — AGENTS.md + knowledge-base scoping (per-role load map, what's over-shared)
- 04 — prior planning on this exact problem (admiral-framework, playbooks, role degradation)
- 05 — existing per-agent config (crew/*.json, mission schema, templates)
