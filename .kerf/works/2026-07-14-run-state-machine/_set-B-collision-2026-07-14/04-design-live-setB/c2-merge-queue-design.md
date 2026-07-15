# 04-Design / C2 — Explicit merge queue (the mergeMu split)

> Pass 4 design. Elaborates D3 (00-decisions). Research: `03-research/c2-merge-queue/findings.md`.
> Target spec: `specs/run-state-machine.md` §Merge queue (RSM).

## Current state
A single global `mergeMu` (workloop.go:384) is held via `defer` across the ENTIRE
`mergeRunBranchToMain` (:6544) — `git rebase` → `go build`/`vet` → gofumpt/gci → `git push
origin` → `git reset --hard` → `br sync` — inside a 3-attempt retry loop. At most one bead
across ALL queues can be in any merge phase. The same lock also serialises the remote
base-sync + worktree-add (`:3636` region) and the escape-worktree check (`:5169`).
`worktreeCreateMu` (:407) is a partial split (nested under mergeMu today). No cross-queue
merge ordering guarantee. `update-ref` (:6821) is NOT a CAS — atomicity rests entirely on
the lock; every rollback is a blind restore to a remembered mainTip.

## Target state
- **RSM-MQ-1.** `mergeMu` is replaced by an explicit merge queue: a channel-fed single
  serialising goroutine per target branch in `internal/daemon/mergequeue`. The daemon holds
  no global merge mutex. Submit is synchronous to the caller, serialised at the goroutine,
  and accepts `context.Background()` jobs (shutdown-drain).
- **RSM-MQ-2 (the critical section).** Only this window runs serialised, per target branch:
  re-validate (FF-check w/ fresh mainTip) → `update-ref` land → [fmt ref-advance] →
  `git push origin` (+blind rollback) → `git restore --staged` → `git reset --hard HEAD`.
  The rebase, `go build`/`go vet`, and gofumpt/gci run OUTSIDE, speculatively, re-validated
  under the lock. Re-rebase ⇒ re-build/re-fmt (per-attempt re-run preserved).
- **RSM-MQ-3.** `git build`/`git push origin`/`br sync` NEVER run while the serialised
  section is held. A DoD check fails if build or network IO occurs inside the critical-section
  executor (success-criterion 4).
- **RSM-MQ-4 (preserved invariants).** The escape-worktree check routes through the same
  queue as a read-only tree-quiescent slot, fenced against update-ref→reset-hard (hk-zguy6).
  The remote base-sync + worktree-add keeps an equivalent exclusion (hk-lt091/hk-h8u7p) via
  the WorktreePort create serialisation; `worktreeCreateMu` survives, its nesting dissolves.
- **RSM-MQ-5.** `git push` stays inside the window (coupled to its blind rollback);
  push-async is deferred to M4.

## Rationale
The findings walkthrough (steps 0-7, LOCK/SPEC/FREE) settles the true critical section:
build+vet+fmt (multi-second-to-minute) currently under the global lock are the win to hoist;
the existing inner retry loop is the natural re-validation seam. The FIFO goroutine
strengthens ordering (loser re-rebase bounded to queue-depth) and breaks nothing. Hard
prereq for M4 (remote merges must thread the queue).

## Requirements traceability
02-components C2 → RSM-MQ-1..5. Goal "mergeMu → explicit merge queue" (01 §2) → RSM-MQ-1/2.
Success-criterion 4 → RSM-MQ-3. M4 dependency → RSM-MQ-1.

## PLANNER-RECONCILE
Push stays in the window; full push removal is M4-class (D3, item 2).
