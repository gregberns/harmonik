---
schema_version: 1
crew_name: alpha
queue: none
epic_id: none
captain_name: operator
goal: "Own the core — internal/daemon and the run machine — through the delete-and-rewrite program. HANDOFF-alpha.md is the state; LANES.md is the order."
---

# Mission: alpha — the core lane

**You are one of two hand-run delivery lanes finalizing
`plans/2026-07-27-delete-and-rewrite/`.** The daemon is down, so nothing dispatches
work to you and nothing closes your beads. You and bravo are the parallelism.


## THE OBJECTIVE — read this before anything else in this file

**Get the daemon running work reliably, so load moves onto the DGX Spark and the
operator stops spending their own Claude and Codex tokens driving it by hand.**
That has been the objective for weeks. It outranks every other framing in this
file, in the handoffs, and in the plan documents. If something you are about to
do does not move that forward, it is not the job.

**The only question the per-bead gate answers is: CAN A BEAD RUN THROUGH THE
QUEUE?** The gate is `make core` — the 29 CORE_PKGS, run without `-short`. It is
`make core` in every TRACKED workflow graph as of 2026-08-13 (D3=v3). The
palette under `.harmonik/workflows/` is gitignored, so a sweep over tracked files
does not see it — check a graph you copy from rather than assuming it carries the
current gate. Do NOT read "gitignored" as "inert": `resolveWorkflowRef` in
`internal/daemon/moderesolve.go` returns a queue item's own workflow ref verbatim
at Tier 0, so a bead that names a path under that directory is read and run.

- **`make full` is NOT the per-bead gate and never was supposed to be.** It
  measured 20 minutes on 2026-08-11 and every bead was paying for it. It is the
  integration-branch-into-main decision, and it runs there and in CI only.
  **Do not report `make full` failures as release blockers.** We know it is
  broken. It is not the current job.
- **Anything outside the core set is DEFERRED BY DEFAULT.** File it and move on.
  Do not put it on a blocker list, do not rank it, do not ask about it.
- **A defect that only appears because the gate is broad is not a product defect.**
  Scenario-tier load flakiness, wall-clock budgets and whole-tree lint are test
  debt, not release blockers.

**Do not ask permission for anything inside this scope.** Reversing a locked
decision that blocks the objective, deleting a rule that is doing more harm than
good, narrowing a gate — these are expected, not escalations. There are too many
rules in this repo and they are costing more than they protect. If a policy
contradicts the objective above, say so plainly and change it.

**Read the paragraph above as a grant FROM the operator, not as a lane deciding
its own authority.** The operator gave it directly and it is written here for
that reason. `AGENTS.md` says reopening a locked decision is the operator's call,
and that finding good evidence is not the same as holding the authority to act on
it. That still governs everything the operator has not named. A mission file does
not silently outrank the router.

## Read in this order

1. **`HANDOFF-alpha.md`** (root of the checkout you work in) — your state: what happened last session and
   what to do next. It is rewritten every restart, so it is the only description of
   where you are that can be trusted. If it is absent or empty, say so plainly.
   That is a real signal and not routine — the keeper does not empty this file.
   Rebuild what you can from `git log`, this file for scope, and your open beads, and get the
   operator's read before you change code.
2. **`plans/2026-07-27-delete-and-rewrite/CHARTER.md`** — what the program is and
   what "done" means. It outranks any handoff on intent.
3. **`plans/2026-07-27-delete-and-rewrite/LANES.md`** — who owns what (§2), the
   ordered work (§7), and what only the operator may decide (§8).
4. **`PRINCIPLES.md`** — before you write or change code.

This file holds no work and never will. A mission file is written once and goes
stale; the handoff and LANES.md are maintained.

## What you own

`internal/daemon/**`, `internal/runlease/**`, `internal/runloop/**`,
`internal/runexec/**`, `internal/workflow/dot/**`, `internal/brcli/**`,
`internal/transport/tunnel/**`, `internal/harness/shared/**`,
`specs/run-state-machine.md`, `specs/execution-model.md`, and
`DECOMPOSITION-MAP.md`. You work the main checkout `/Users/gb/github/harmonik` on
`phase1-session-restart-substrate`, commit there directly, and merge bravo in.

**`internal/core` is bravo's and you do not edit it.** It is one compile unit that
58 of 103 packages depend on. The same rule that keeps bravo out of
`internal/daemon` keeps you out of `internal/core`. LANES.md §1 has the reasoning,
§5 has how to announce a change that crosses the line.

## Three standing limits

- **No daemon.** It stays down — do not start it, no supervisor, no queue. This is
  an operator directive, not a default. (A mistyped verb used to boot one; fixed at
  `0a8ad3674`, so the old warning about `harmonik status` is historical.)
- **You close your own beads.** The rule that the daemon owns terminal transitions
  applies when the daemon runs work. It does not run work here. Verify the fix,
  then close it yourself.
- **You escalate to the operator, not a captain.** There is no captain. LANES.md §8
  lists what neither lane may decide — add to that list rather than deciding it.

## The review gate still applies

Non-trivial commits need an independent review and the `Reviewed-By:` /
`Review-Verdict:` trailers. Commit with `git commit -F <file>` and explicit paths —
never `git add -A`. Never run a command that can discard work you did not write:
no `--amend` on a commit another lane can already see, no `git reset --hard`, no
`git checkout -- .`. Several lanes work this repo at the same time, and an amend
on a shared branch is how work disappears. Unstaging a file you staged by mistake
is safe and is often the repair.

## Keeper restart

Re-read `HANDOFF-alpha.md`. That is the whole procedure. Committed work is not lost.
