# Remote node telemetry + reporting — overview & plan of record

**Date:** 2026-06-20
**Origin:** operator question on top of the chani / remote-substrate lane — *"To run remote jobs we apparently need a harmonik instance on the remote box. If so, maybe we do more than use it as a channel: report resource state back, know when things max out, know when there are problems."*

**This folder is planning only.** It does not touch chani's unmerged remote-substrate fix or re-enable the worker. The reporting work lives in `internal/workers/` (the health-probe loop) and does **not** collide with chani's substrate commits (SSHRunner / tmuxsubstrate / reviewloop).

Backing docs: `01` current architecture · `02` telemetry+autoscale design · `03` expanded central-leverage · `04` prior-art & naming. **`05-phase1-spec.md` is the concrete first slice.**

---

## The premise, corrected

> "We need a harmonik daemon on the remote."

**Not true, and not a prerequisite.** The remote box runs **no persistent harmonik process**. The central daemon drives it through one-shot `ssh worker -- <cmd>` calls; the only long-lived process per run is a reverse SSH tunnel, and **it lives on the central box, not the remote.** The harmonik *binary* merely has to exist on the worker as a hook handler. (`01`, with file:line refs.)

So the control plane is already 100% central and the worker is dumb. Reporting resource state back needs exactly **one new thing: a worker→central signal** — and the existing health-probe loop can carry it with **no remote daemon at all.**

---

## DECISIONS (operator, 2026-06-20)

1. **Hardcoded max stays.** No dynamic concurrency controller in v1. `max_slots` remains the human-set ceiling. The dynamic autoscaler ("AIMD") is **deferred** — we build it *after* we've tracked real load numbers over time and can choose a sensible max from data, instead of guessing the control law up front.
2. **Reporting first.** The first build answers the two operator questions, nothing more:
   - **"Are there issues?"** — problems the box knows about and currently never says (orphaned claude, leaked worktrees, disk pressure, repeated no-commit).
   - **"At what point are we maxing out?"** — resource load (CPU/RAM/swap/disk), tracked over time so the eventual max is evidence-based.
3. **No metaphor name.** Central node stays plain in code (`hub` / `control node`); we'll name it if something obvious shows up. Not a blocker.

---

## Two questions, two transports (the key structural point)

These are different questions with different freshness needs, so they get different channels:

- **"Can I send this box more work / what's its baseline load?"** — slow and coarse. A periodic poll on the existing health-probe timer is plenty. Fine that it only runs on a timer; fine that it's central-pull.
- **"Is a job running *right now* pushing the box to its limit?"** — live and event-driven. A once-a-minute poll is too slow. You want the moment a threshold (e.g. **>80% CPU for N seconds**) is breached, pushed up *while the job runs* over the tunnel that's **already open** during a run. When the box is idle / daemon down, it stays silent — no phone-home, as you wanted.

---

## Phases

### Phase 1 — Reporting (slow poll). **← build now; spec in `05`.**
Extend the existing periodic health probe to collect and surface, per worker:
- a **resource snapshot** (load, mem, swap, disk, claude-process count) — logged over time so we can later pick a max from real data;
- **problem flags** answering "are there issues?" (orphaned claude, worktree leak, disk pressure).
`max_slots` stays hardcoded; dispatch behavior is unchanged. Pure observability. Off-by-default; no-op when no workers configured.

### Phase 2 — Live breach alerts (during a run).
A lightweight sampler that runs **only for the lifetime of a job**, watches CPU/RAM on the worker, and pushes a `resource_breach` event up the existing hook-relay tunnel when a threshold + duration is crossed (the "we're maxing out *now*" signal). No resident daemon — scoped to the run; silent when idle.

### Phase 3 — Resident node agent + data-driven autoscale.
The always-on process that phones home even between jobs, self-diagnoses, advertises capability — and, with Phase-1 history in hand, the dynamic controller that turns the hardcoded `max_slots` into a center-computed target. Earn this when there's a 2nd box and real load history. (Detail preserved in `02`/`03`/`04`.)

---

## What's deferred (and why it's fine to defer)
- **Dynamic autoscale / AIMD** → Phase 3. Needs Phase-1 data first; guessing the control law now is premature.
- **Capability-aware routing, per-box cost attribution, warm-worktree affinity** → want a 2nd worker + the resident agent (`03`).
- **Naming the central node** → cosmetic; decide later.

## Next action
Build Phase 1 per `05-phase1-spec.md`: the report struct, the worker collector, wiring into the health-probe loop, problem-detection flags, and surfacing to the operator. Beads tracked under `codename:worker-report`.
