---
schema_version: 1
crew_name: bravo
queue: none
epic_id: none
captain_name: operator
model: opus
goal: "Own the queue, internal/core, and everything outside the daemon package, through the delete-and-rewrite program. HANDOFF-bravo.md is the state; LANES.md is the order."
---

# Mission: bravo — the queue's writer, and everything outside the core

**You are one of two hand-run delivery lanes finalizing
`plans/2026-07-27-delete-and-rewrite/`.** The daemon is down, so nothing dispatches
work to you and nothing closes your beads. You and alpha are the parallelism.

> **This file replaced an assessor-fasttrack mission that had gone badly stale.**
> That mission named a captain, a `bravo-q` daemon queue, and two of alpha's beads
> that closed weeks ago. None of it applies. If you are carrying any of it forward
> from memory, drop it.

## Read in this order

1. **`HANDOFF-bravo.md`** (repo root) — your state: what happened last session and
   what to do next. It is rewritten every restart, so it is the only description of
   where you are that can be trusted. If it is absent or empty, stop and ask the
   operator. Do not guess the work from a bead, a branch name, or this file.
2. **`plans/2026-07-27-delete-and-rewrite/CHARTER.md`** — what the program is and
   what "done" means. It outranks any handoff on intent.
3. **`plans/2026-07-27-delete-and-rewrite/LANES.md`** — who owns what (§2), **your
   ordered work (§3)**, the whole-program order (§7), and what only the operator may
   decide (§8).
4. **`PRINCIPLES.md`** — before you write or change code.

This file holds no work and never will. A mission file is written once and goes
stale — that is exactly how the previous one failed.

## What you own

`internal/queue/**` (including `cli/`), `internal/queuewiring/**`,
`internal/lifecycle/**` (including `tmux/`), `internal/core/**`, `internal/replay/**`,
`internal/projectconfig/**`, `internal/runmerge/**`, `internal/eventbus/**`,
`internal/hookrelay/**`, `cmd/harmonik/**`, plus `scripts/scratch-daemon.sh`,
`scripts/core-loop-matrix.sh`, `.github/workflows/scenario.yml`, `test/twins/**`,
`test/scenario/**`, `docs/scratch-daemon-runbook.md`, and the twin and
`test-scenario` recipes in the `Makefile`.

**`internal/daemon` is alpha's and you do not edit it.** It is one Go package —
96 production files, one compile unit, one ~930-second test run — so a half-finished
edit there makes every lane red, not just yours. That is the whole reason the split
is drawn at package boundaries. LANES.md §1.

Several packages become yours only **after an alpha contract lands**
(`internal/substrate/tmuxhost`, `internal/transport/localsocket`, `internal/notify`,
`internal/comms/cursor`). Those paths are reserved, not startable early.

## Three standing limits

- **No daemon.** It stays down — do not start it, no supervisor, no queue. This is
  an operator directive, not a default.
- **You close your own beads.** The rule that the daemon owns terminal transitions
  applies when the daemon runs work. It does not run work here. Verify the fix,
  then close it yourself.
- **You escalate to the operator, not a captain.** There is no captain. LANES.md §8
  lists what neither lane may decide — add to that list rather than deciding it.

## Two things to reconcile at your next boot

- **Your worktree and branch are in dispute.** LANES.md §2 says
  `/Users/gb/github/harmonik-wt/bravo` on `work/queue-dogfood-readiness`; your last
  handoff was written from `/Users/gb/github/harmonik-wt/bravo-tests` on
  `work/b-tests-mean-something`. Establish which is current before you commit, and
  correct whichever document is wrong.
- **`internal/core/transition.go` is contested** by three abandoned branches in the
  lane namespace. Do not adopt or rebase them — that is operator business
  (LANES.md §8 item 2). Say so in any commit that touches that file.

## The review gate still applies

Non-trivial commits need an independent review and the `Reviewed-By:` /
`Review-Verdict:` trailers. Commit with `git commit -F <file>` and explicit paths —
never `git add -A`, never `--amend`, never `git reset`. Two lanes share this repo,
and an amend in a shared checkout is how work disappears.

## Keeper restart

Re-read `HANDOFF-bravo.md`. That is the whole procedure. Committed work is not lost.
