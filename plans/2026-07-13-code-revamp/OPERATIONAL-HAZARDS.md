# Operational hazards — daemon-off, sub-agent-heavy rebuild

> Durable notes on operational hazards specific to how the code-revamp runs: daemon OFF, work done
> by background Claude sub-agents, planner + implementer as two long-lived keeper-watched sessions
> that get `/clear`'d and re-hydrated. Not product bugs — no beads (per operator directive). These
> are workflow traps + their mitigations, so we stop re-learning them.

## H1 — Background sub-agents survive `/clear`; a mid-write bench looks like an orphan/fabrication

**What happened (2026-07-14, twice).** A dispatched background planning agent kept running across a
keeper `/clear` (~7 min task). The re-hydrated session read the agent's shared kerf bench *between*
its archive step and its file-rewrite step, saw the "rewritten" files still byte-identical to their
archived copies, and wrongly concluded the agent had fabricated its completion report / been
keeper-orphaned. The agent then finished the rewrites normally.

**Why it fools you.** Background agents re-notify on completion and keep working after a `/clear`.
A shared bench read mid-flight shows a partially-applied state that reads exactly like a
report-ahead-of-files orphan.

**Mitigation.**
- Before declaring an artifact orphaned or a report fabricated, **check for a live task** (the task
  notifications / running-task state) — a slow background agent may still be writing.
- Treat a report whose file-claims don't match disk as **"verify liveness first,"** not
  "fabricated." Only conclude orphan/fabrication once no live task owns that bench.

## H2 — Two agents on the same bench can clobber each other

**What happened.** Pre-restart, two design agents (M2 + M3) each survived a `/clear` and re-ran
~1h later, re-colliding on their benches. A third reconcile agent had to be killed to avoid a
3-way collision.

**Mitigation.**
- **Never run two agents against the same kerf bench concurrently.**
- If a `/clear` happens mid-dispatch, **reconcile survivors before re-dispatching** the same work —
  a fresh Agent call starts a *new* agent; it does not resume or replace the one still running.

## H3 — Correcting a shared file that a live agent also edits will race

The re-hydrated session added a correction banner to `RECONCILE.md`; the still-running agent then
overwrote it with its own (correct) banner. Harmless here, but: **if you must edit a file a live
agent owns, expect your edit to be lost.** Prefer recording the finding in a channel the agent
doesn't write (e.g. COORD) over editing its working file.
