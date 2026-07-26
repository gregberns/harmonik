# Ralph Autonomous Loop — daemon-hosted churn loop with periodic supervision

**Date:** 2026-07-19
**Status:** investigation / scaffold (not a spec). "Don't dive too deep" per operator.
**Owner:** captain scopes + staffs; admiral oversight.

## Frame

Operator wants a **long-running autonomous work loop** in the daemon: something that
churns through work with little supervision on **cheap/local compute**, while a managing
Claude instance **checks in occasionally** to see where it's at. Reference experience:
a "Ralph Loop" run in **Pi** using a local **Ornith** model on a DGX — it ran a long time,
ground through work unattended, and a managing Claude checked in from time to time. It
worked well; there was one **fundamental issue in the Pi plugin** used (specifics TBD —
capture from operator) that was problematic but the overall shape was compelling.

## ⚠️ Naming collision — resolve first

harmonik **already has a "ralph mode"**, but it is **NOT** the thing the operator is
describing. Do not conflate them:

- **Existing `workflow_mode: ralph`** (`plans/005_workflow_modes/`): a per-task
  **implement → review → {approve: close, request_changes: implement}** loop, capped at
  3 iterations. It is the hardcoded 2-node **proto-DOT** case that the DOT graph-walker
  generalizes. This is a *useful building block* — **do NOT scrap it**; it earns its keep
  as the review-loop workflow and the DOT precursor.
- **Operator's "Ralph Loop"** (this plan): an **autonomous, self-repeating churn engine**
  — same objective fed back to a cheap model over and over, self-correcting, running
  largely unattended, with a supervisor that samples progress on a cadence. This is a
  **new capability**, not a mode of the existing workflow.

**Decision to make:** name the new thing something other than "ralph" to kill the
collision (e.g. `autoloop` / `churn` / `grind`), OR explicitly re-own "ralph" for the
autonomous loop and rename the workflow_mode. Recommend a distinct name.

## What we already have (don't rebuild)

- **Daemon queue + dispatch loop** — already a churn engine: pulls ready work, spawns a
  worker, merges the result, repeats. The gap vs a Ralph loop is *granularity*: the queue
  churns **discrete beads**, whereas a Ralph loop hammers **one objective repeatedly** until
  it's satisfied (or a cap/goal check fires).
- **Captain / keeper cadence** — already a periodic supervisor. The "managing Claude checks
  in" role maps onto a crew subscribed to the loop's progress feed.
- **comms bus** — the natural transport for "check in on the loop" messages (progress out,
  intervention in). No new IPC needed.
- **Cheap-model routing** — the harness-selection chain (bead > queue > node > global,
  `--default-harness`) + `harness:codex` already lets a loop run on codex instead of Claude.
  A truly local model (Ornith-equivalent) would be a **new harness/substrate** (pi harness
  scaffolding exists — see `init_keeper_template` pi-dispatchable checks).

## The new loop — shape

1. **Objective, not a bead.** The loop takes a standing goal/prompt + a satisfaction check
   (tests pass? oracle? iteration cap?). Each iteration: run the model on the objective →
   apply/commit → evaluate → loop or stop.
2. **Cheap compute.** Runs on codex or a local model (pi harness), NOT Claude — Claude is
   only the periodic supervisor. Directly serves the operator's token-conservation goal.
3. **Progress feed.** Every iteration emits a structured progress event to comms (iteration
   N, what changed, eval result, cost).
4. **Supervisor check-in.** A managing crew subscribes and checks in on a **cadence** —
   options: (a) every iteration, (b) every K iterations, (c) on a timer, (d) on a
   stall/regression signal. It can **nudge, re-scope, or halt** the loop. Cheap sampling,
   not per-token babysitting.
5. **Guardrails.** Hard iteration cap, wall-clock cap, cost cap, and a kill on repeated
   no-progress (Ralph loops can spin). Fail-closed to `needs-attention`, like workflow_mode
   ralph's cap behavior.

## Key open questions

- **What was the "fundamental issue in the Pi plugin"?** Get specifics from operator — it's
  the highest-value input; the plan should design *around* that failure mode explicitly.
- **Termination:** goal-satisfaction oracle vs pure iteration cap vs supervisor-halt. Likely
  all three, layered.
- **Check-in cadence:** every-iteration is safest but chattiest; timer/stall-triggered is
  cheaper. Probably configurable per loop.
- **State between iterations:** does the loop keep a resident session (ties to the codex
  **resident sidecar**, hk-160yb — a persistent multi-turn client is exactly what a churn
  loop wants) or re-spawn per iteration (simpler, stateless, colder)?
- **Local model path:** is Ornith-equivalent local inference in scope now, or start on codex
  and add local later? (Start on codex; local harness is a follow-on.)
- **Scope of one loop:** one objective/repo, or can it fan out? Start single-objective.

## Rough phases

1. **Untangle naming** + get the Pi-plugin failure specifics from operator.
2. **Design the loop contract**: objective + eval + guardrails + progress-event schema +
   supervisor check-in protocol (reuse comms). Kerf-worthy.
3. **Prototype on codex** (cheap, already wired): single-objective loop, iteration cap,
   comms progress feed, one supervisor crew checking in on a timer.
4. **Guardrails + kill conditions** (no-progress detection, cost/wall caps).
5. **Resident-session option** (fold in hk-160yb sidecar if the loop wants warm state).
6. **Local-model harness** (Ornith-equivalent via pi harness) — follow-on.

## Relationship to existing work

- **Reuses** the daemon dispatch engine, comms bus, harness-selection chain, keeper cadence.
- **Ties to** codex resident sidecar (hk-160yb) for warm inter-iteration state.
- **Serves** the operator's Claude-conservation goal (loop runs on cheap compute; Claude
  only supervises).
- **Distinct from** workflow_mode `ralph` (keep that; rename one of the two).
