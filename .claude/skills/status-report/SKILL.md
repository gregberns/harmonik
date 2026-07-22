---
name: status-report
description: >
  On-demand, operator-invoked status command. Run it when the operator wants a
  big-picture, program-wide progress report — "where are we", "status report",
  "what's the state of the program". It discovers the ACTIVE program/plan from
  this repo's conventions (a dated `plans/<dir>/` with a ROADMAP/PLAN + an
  append-only COORD log, plus kerf works, beads, and git log), reconciles the
  sources (git log is ground truth — docs lag reality), and prints a plain
  terminal-markdown report: the frame, a phase scoreboard, live per-phase
  detail, what needs the operator, and a one-line bottom line. Optional arg: a
  specific plan-dir name to report on instead of the most-recent one. No scripts,
  no side effects — read-only, emits text only.
---

# Status report — program-wide progress, on demand

The operator invokes this to get a **big-picture snapshot of the active program**: what's
landed, what's in flight, what's blocked on a human. It is **read-only** — gather state,
reconcile it, print the report. Do NOT edit files, close beads, or change any state.

Optional argument: a plan-dir name (e.g. `2026-07-13-code-revamp`). If given, report on that
program. If absent, discover the most recently active one.

## Step 1 — Discover the active program

1. **Pick the plan dir.** If an argument was passed, use `plans/<arg>/`. Otherwise list
   `plans/*/` newest-first (by mtime) and take the most recently modified one that has a
   `ROADMAP.md` or `PLAN.md`. Announce which one you chose.
2. **Read the phase map.** `ROADMAP.md` (preferred) or `PLAN.md` in that dir defines the phases /
   milestones (often M1–M5 or Phase 1..N) and their intended sequence. This is the *plan* — it
   may lag reality.
3. **Read the coordination log if present.** A `COORD.md` in the same dir is an append-only
   planner⇄implementer channel: newest entries sit at the TOP, right under a `--- LOG ---` line,
   with `DONE` / `BLOCKED` / `HANDOFF` / `STATUS` entry types. Read from the top down until the
   picture is clear — this is the **freshest** account of what just landed and what's blocked.
   Not every program has a COORD; degrade gracefully if it's missing.
4. **If no plan dir exists at all:** say so plainly, then fall back to a lighter status built from
   `kerf map` + `br ready` (skip the phase scoreboard, keep the frame + what-needs-operator +
   bottom line).

## Step 2 — Gather corroborating state

Run these read-only commands (skip any that error, note it):

- `git log --oneline -30` (or scoped to the plan's branch) — **ground truth for what actually
  landed.** A phase is only ✅ if its commits are really in the log, not because a doc claims it.
- `kerf map` — kerf works grouped by area (which subsystems have active works).
- `kerf next --format=json` — ranked backlog; what's next below the named initiatives.
- `kerf show <codename>` — status of a specific work when a phase maps to one.
- `br ready` and `br list --status=open` — open/actionable beads.

## Step 3 — Reconcile (the important part)

Sources disagree; trust them in this order for "what's true right now":

1. **git log** (commits landed) — beats everything for "done".
2. **COORD top entries** (freshest human account of in-flight / blocked).
3. **kerf / beads** state (works and beads).
4. **ROADMAP / PLAN prose** (the intended shape) — LAST; treat as the plan, not the status. If a
   roadmap says a phase is done but no commit backs it, it is NOT done — mark it in-flight and say
   why.

Cross-reference: map each phase to its landing evidence (commit SHA / closed beads / a COORD
`DONE`). A phase claimed complete with no evidence is a reconciliation flag worth surfacing.

## Step 4 — Emit the report (plain terminal markdown)

Match this shape. Generalize labels to whatever the active program uses (phases may be M1–M5,
Phase 1..N, tracks, workstreams — use their real names).

### 1. The frame — one paragraph
The operating context in prose: is the daemon on or off, what's the sequencing rule, what
constraint drives the order (e.g. "daemon stopped for the rebuild; everything runs
single-writer, human-reviewed to a branch"). One tight paragraph.

### 2. Phase scoreboard — a table
Columns: **Phase | What it is | Status**. Status uses these markers:

- ✅ **LANDED** — commits are in git log.
- 🔵 **IN FLIGHT (~fraction)** — actively being built; give a rough fraction (e.g. ~3/5).
- 🟡 **DESIGNED-GATED** — designed/spec'd but gated on something.
- ⚪ **HELD / GATE** — parked or blocked behind a gate or decision.

### 3. Live detail — one sub-table per in-flight phase
For each 🔵 phase, a sub-table of its components/tasks: **Component | Status**, where status is a
landed commit SHA, in-progress, next-up, or blocked (with the blocker). Pull these from COORD
entries, `kerf show`, and open beads.

### 4. What needs the operator — a section
Only the items genuinely blocked on a **human decision** (design forks, locked-decision reversals,
destructive-op approvals, product direction). Phrase each as the **actual decision to make**, not
"waiting on operator". If nothing is blocked on a human, say so.

### 5. Bottom line — one line
The critical path in a single sentence: what unblocks the most, or what's next.

## Style

- Plain terminal markdown. Tables render as pipes. Emoji markers only where specified.
- Say the thing, not the pointer: name the phase and what it means, not just a bead ID or SHA
  (a SHA is fine as *evidence appended to* a named item, never as the handle).
- Be tight — the operator wants the scoreboard at a glance, not an essay. No preamble, no
  "here is your report"; lead with the frame.
