# Mission

> Written BEFORE anything runs. This file is what you were asked to do, frozen at the start.
> If the scope changes mid-assessment, add a dated note at the bottom — do not edit what is above
> it, or the record stops showing what you actually set out to do.

**Started:** 2026-08-11 14:34 (local)
**Assessor:** lane bravo, `work/bravo-reachability`
**Gate:** merge
**Asked by:** operator

## What is being gated

| Lane / branch | Commit | What is in it |
|---|---|---|
| `work/bravo-reachability` | `4ebd8a334` | 9 bravo commits (corpus + records only, no product code) rebased onto alpha's tip |
| `work/alpha-integration-merge` | `eac00a6c2` | alpha's 8 commits since bravo's previous base `6e80e44c6` |

**Merge base:** `eac00a6c2` — bravo is 9 ahead, **0 behind**, rebased clean at 14:34 with no
conflicts. Backup of the pre-rebase tip: `backup/bravo-pre-rebase3-20260811` (`0d44509ac`).
**Conflicts:** none. The rebase replayed all 9 commits without intervention.

## What was asked

The operator asked three questions:

1. What did alpha get done in the round that just ended?
2. Is that enough to justify re-running the gate?
3. How many of the release-critical issues this lane tracks are now fixed?

Question 2 was answered YES before the gate started, on three grounds, all measured and not
assumed:

- **The disk blocker cleared.** 17–18 GiB free against a 10 GiB floor. The previous handoff opened
  with the box below the floor and the fleet stalled.
- **Lint is green.** Both gate-blocking lint beads (`hk-pw1wv`, `hk-iy9tx`) were repaired by alpha
  and verified from this lane at `6e80e44c6`.
- **Alpha's last three commits attack this lane's own largest finding** — the merge decision
  contains wall-clock deadline assertions that fail as a function of box load
  (`hk-vp02y` / `hk-core-gate-nondeterministic-m9vlf` P0 / `hk-scenario-tier-nondeterministic-xt1wa`).
  22 shutdown stopwatches became one 60-second hang detector.

So the gate run is not only a merge decision. **It is the test of whether alpha's repair of the
load-sensitivity class actually holds**, and that is the question this assessment is really about.

## Scope

**In bounds:**

- One `make full` run on `4ebd8a334`, with box load and free disk sampled every 15s alongside it,
  per LP-018. The logged exit code is read from a file, never from a pipe and never from the task
  runner's own report.
- Re-verification of the two corpus cases alpha's commits claim to have moved:
  **LP-005** (`set-concurrency` accepts any value — `hk-set-concurrency-unbounded-ad79i`) and
  **`hk-5ji8t`** (the stale codex scenario test). Verified by running, never by reading a commit
  message.
- A count of the release-critical set, with each entry's status carrying the commit it was
  measured on.

**Out of bounds and will not hold this gate:**

- The corpus cases nothing in alpha's range touched — LP-006 (`promote --dry-run`), LP-008
  (`usage` misattribution), LP-015 (undeliverable comms message), LP-016 (`wake` accepts any
  string). Checked: no commit in `84ad44e20..eac00a6c2` touches those command surfaces, so they
  are presumed unmoved and are not re-run here.
- A live dispatched run on a scratch daemon. Still the honest gap, still undone, and it is
  deliberately not attempted while a `make full` is loading the box — starting one would corrupt
  the measurement this assessment exists to take.

## Independence

This lane wrote no product code in the range under test. All 9 bravo commits are corpus files,
assessment records and documentation. Every product change being gated came from alpha. So the
assessor is not grading its own work, which is the position the role requires.

The one exception to state plainly: this lane **filed** `hk-5ji8t` and `hk-iy9tx`, and it is now
checking whether alpha's repairs of them hold. Filing a defect is not building the fix, so the
independence holds — but the temptation to confirm one's own diagnosis is real, and it is named
here so the record shows it was known going in.
