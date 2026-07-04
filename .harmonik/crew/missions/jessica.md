<!-- Mission handoff — locked 6-field schema -->
```yaml
schema_version: 1
crew_name: jessica
queue: jessica-q2
epic_id: ""
goal: "Drive the daemon-reliability lane: the highest-impact P1 daemon/worktree reliability findings from logmine iter-20. Dispatch to jessica-q2, sequence to avoid daemon-internals merge collisions, reproduce-before-fix on the reversions. These are the throughput critical-path — daemon crash-loops and the resurrected worktree-create race are what idle-hang the fleet (esp. gb-mbp)."
captain_name: captain
```

## Lane: daemon reliability (logmine iter-20 P1 register)

You own these four P1 beads on queue **jessica-q2**. They are ALREADY created and ranked — you do NOT decompose; you sequence and drive to merge. All touch core daemon/worktree internals, so they collide with each other at merge-rebase — **serialize, do not fan all four at once.**

Dispatch order (highest impact first):

1. **hk-lt091** — empty-HEAD worktree-create race **RESURRECTED** (was iter-19 N1, fixed at fb11aabd, regressed). This is the direct gb-mbp idle-hang culprit. **Reproduce-before-fix**: it's a reversion, so write a RED test that reproduces the empty-HEAD race first, confirm the prior fix fb11aabd was lost/regressed, then restore. Dispatch this ALONE first.
2. **hk-rnkuy** — daemon crash-loop 17:14 & 17:17 (07-04) abandoned 8 runs; + no daemon-death event type. Two parts: (a) root-cause the crash-loop, (b) add a daemon-death EventType so future crashes are observable (watch the EV-0xx wantCount landmine — a new EventType needs the wantCount bump).
3. **hk-qe736** — worktree leak: 58 worktrees ~3.1G, stale LOCKED agent-a974b0ef ~11 days never reaped. Disk-pressure risk (disk<10GiB → cache wipe → build failures). Fix the reaper's LOCKED-worktree blind spot.
4. **hk-gf59k** — ledger-dep false-defer (blocker closed ~50s BEFORE defer fired) + same-chain re-defer churn.

Run #1 alone. Run #2/#3/#4 serialized (or #3 in parallel if it proves file-disjoint from #2 — #3 is the reaper, #2 is crash-loop/event-emit; verify before parallelizing).

## Discipline
- Standard queue submit to **jessica-q2** (never `main`; never `jessica-q` — that queue is paused-by-failure on a dead run, leave it). Daemon owns terminal transitions — do NOT pre-set in_progress or close on merge.
- **Triage your own failures** (Opus lane): reproduce + diagnose before re-dispatch; escalate to captain only if a root cause is refuted ≥2× or a wedge survives ≥2 fix attempts (major-issue fan-out threshold).
- These are daemon-internals — a bad fix can crash-loop the live daemon (self-fix bootstrap trap). Keep fixes minimal, RED-test first, land to a branch; they do NOT take effect until the next daemon redeploy, so blast radius is controlled.
- Post progress to **both** `comms send --from jessica --to captain --topic status` AND `br` comments: on every bead close + a ≤10-min timer while dispatching (≤15-min idle/draining) + boot and drain bookends.
- Keep `--follow` armed on jessica-q2 for re-task when this lane drains.

## Source
Findings register: `.kerf/works/logmine/04-research/findings-iter20.md`. duncan owns the rest of the iter-20 findings (harness-eval, Pi-harness, comms-log) on duncan-q — do NOT touch those; this lane is the daemon/worktree-reliability subset only.
