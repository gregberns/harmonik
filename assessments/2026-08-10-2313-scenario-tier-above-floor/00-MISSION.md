# Mission

> Written BEFORE anything runs. This file is what you were asked to do, frozen at the start.
> If the scope changes mid-assessment, add a dated note at the bottom — do not edit what is above
> it, or the record stops showing what you actually set out to do.

**Started:** 2026-08-10 23:13 (local)
**Assessor:** lane bravo, hand-run session (no fleet, no `$HARMONIK_AGENT`)
**Gate:** neither — this is corpus work, not a merge or deploy gate
**Asked by:** operator, through `HANDOFF-bravo.md`

## What this session is, and what it is not

The handoff names no `{branch, gate}` pair, so **no gate runs here**. The contract's on-wake step 1
says to stop rather than guess a gate scope, and that is what happens: this session does the
§Grow-the-regression-corpus duty instead, which the operator weighted above test-reading on
2026-08-10.

| Lane / branch | Commit | What is in it |
|---|---|---|
| `work/bravo-reachability` | `84ad44e20` | the live case library, 17 cases, plus this session's additions |

**Merge base with `work/alpha-integration-merge`:** `14b632046`.
**Divergence:** alpha is 8 commits ahead of that base; bravo is 4 ahead. The handoff's claim that
bravo is "four commits ahead of alpha" was true when written and is now stale — the branches have
diverged. Measured 2026-08-10 23:13.

## Scope

**In bounds.**

1. Run the scenario tier above the disk floor. `hk-scenario-tier-nondeterministic-xt1wa` has been
   open since 2026-08-07 waiting for exactly one thing: a run above 10 GiB free. The box now has
   23 GiB. This is the whole of that bead's stated precondition.
2. Correct `LP-013` if the measurement contradicts it. The case states that `make test-scenario`
   "sits behind `lint-allow`". `make test-scenario` is a target of its own whose only prerequisite
   is `build-all`; only `make full` orders `lint-allow` ahead of it. The tier was never blocked —
   it was only ever unreached by the one command that was being run.
3. Add protocol cases. The corpus README and the handoff agree: a library of only `probe` cases
   decays into a regression suite that finds nothing new.

**Out of bounds, and will hold nothing.**

- `hk-pw1wv`, the receipt-GC lint allow-list failure. It is alpha's, it is confirmed still live
  (alpha has no commit touching `completion_gc.go`, `completion_gc_test.go` or
  `tools/lintreport/allow.txt`), and it is inherited debt from this lane's side.

## Independence

**Carve-out, and it is unavoidable on a hand-run lane.** This session did not build the code under
test, but it did write the corpus cases it is about to re-run and correct — `LP-013` among them.
Grading a suite one wrote oneself is exactly the conflict §Bounds names.

The position taken: this session **corrects** its own prior cases where a measurement refutes them
and records the refutation on the face of the record. It does not issue a PASS or BLOCK on
anything, because it is not running a gate. No verdict is being laundered here — there is no
verdict. Alpha has already refuted three of this lane's findings (the evaltasks fixture,
`set-concurrency`, and `hk-q21jt`), which is the independent check this lane actually has.

## Known-red going in

- `hk-pw1wv` — `lint-allow` fails on 6 file/linter pairs in the new receipt-GC code, so `make full`
  cannot pass on the integration branch. Confirmed still live at 2026-08-10 23:15. Not this lane's.
- `hk-cli-flag-first-starts-daemon-gjhiy` — a leading flag drops the verb and starts a daemon.
  Open, this lane's, unchanged.
- The scenario tier's own result is unknown by definition. That is the point of the run.

## Precondition checks done before the first command

    grep -c -- '--rev' scripts/scratch-daemon.sh    → 22   (safe to gate from this tree)
    df -h /System/Volumes/Data                      → 23 GiB free   (above the 10 GiB floor)
    git status --porcelain                          → empty
