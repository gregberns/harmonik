# Verdict

## There is no PASS/BLOCK here, and that is deliberate

This session was handed no `{branch, gate}` pair. The contract's on-wake step 1 says to stop rather
than guess a gate's scope, so no gate ran and **no verdict is issued**. What follows is the result
of the corpus work that was done instead.

Everything below names the commit it was measured on: **`84ad44e20`**, on
`work/bravo-reachability`, clean tree.

## What was asked, and what came back

`hk-scenario-tier-nondeterministic-xt1wa` had been open since 2026-08-07 waiting on one thing: a
scenario-tier run above the 10 GiB disk floor. That was done twice.

**The tier is red above the floor — `rc=2` on both runs — and disk is not why.** Zero of the 306
`dispatch paused` lines came from a real reading; all are fixture-injected. Free space never fell
below 22.1 GiB against a 10.24 GiB watermark. The bead's stated precondition is now discharged and
its premise has changed: this was never a disk story, and disk was one instance of a wider class.

**The blocker the bead was waiting on did not exist.** `make test-scenario` is its own target
gated only by `build-all`; `make full` merely runs `lint-allow` first. The tier was reachable the
whole time by a single command. Four days of waiting bought nothing.

## What the tier's redness actually is

Two runs, partially disjoint failure sets — 9 and 6, intersection 3 — which is the pattern
`hk-core-gate-nondeterministic-m9vlf` (P0) already describes for `make core`. This session extends
that P0 to a second tier and narrows it: **the scenario tier's instability is that bead's Cause B
alone** (load and parallelism), because Cause A's mechanism — the write lock on the operator's real
`~/.claude.json` — cannot reach these six packages.

The evidence that it is starvation and not broken code: every failure is a wall-clock deadline
expiring, none is a failed logical assertion, and the two load-varying survivors of the
intersection pass alone with large margins — 3.878s against a 20s deadline, 13.695s against 60s.

**The difference between 9 and 6 is NOT part of that evidence.** An earlier draft of this verdict
attributed it to run 2 having a quieter box; the full load sample refutes that (run 2 peaked at
34.24, higher than anything measured in run 1) and the correction is dated in `01-EVIDENCE.md`.
The count difference is unexplained. The isolation margins carry the conclusion on their own, and
they are the part that does not depend on attributing load on a shared box.

**This is not a caveat about the measurement. It is the finding.** These lanes run two or three
agents at once, so the contended box IS the condition under test, and a suite of wall-clock
deadlines cannot produce evidence on it.

## The one real defect

`hk-5ji8t` (new, P1). `TestScenario_Codex_EmptyModel_FullLifecycle` fails 4 of 4, including both
isolation runs — the only failure tonight that is not load. It sets a daemon-level
`WorkflowModeSingle` default that `resolveWorkflow` deliberately refuses, so the run falls through
to dot mode and walks a `commit_gate` node that runs `make full` inside a three-file temp dir that
is not a Go module.

**The product is right and the test is stale.** Filing this against the daemon would have been the
fourth instance of the exact failure the contract's §"did I measure the daemon, or my copy of it?"
exists to prevent. It carries two further defects: its diagnostic names a `--model` leak that does
not occur, and it prints `PASS` from an unconditional `t.Logf` after already failing.

## The through-line, which outlasts every bug above

Five surfaces were asked a direct question tonight and five gave a confident wrong answer: the task
runner said exit 0 on a tier that exited 2; the log said 306 disk pauses on a box with 23 GiB free;
three `[build failed]` lines came from passing tests; a test blamed a `--model` leak that never
happened; and another announced an "infinite-loop regression" that passed on a quieter box.

Every one of them fails toward *plausible*. That is what makes this tier expensive: not that it is
red, but that reading it carelessly produces a specific, confident, wrong story every time.

## Residual risk — what this session did NOT establish

- **No live dispatched run.** No scratch daemon, no real agent, no bead through the loop. The tier
  is an in-process suite, not the system doing work. The standing next step is still undone.
- **Run 2 is not fully independent.** Four of six packages reported `(cached)`. The failure-set
  comparison holds because every failure is in the two that re-ran, but the four green ones were
  measured once, not twice.
- **Two runs is a small sample** for a claim about nondeterminism, and both were on one box on one
  night.
- **The three run-2-only failures** (`BootSweep_DoesNotRemoveTheWorktree...`, `ParallelSmoke_Two
  BeadsConcurrent`, `Throughput_TenBeadsAtMaxFour`) were not isolated. They are presumed starvation
  by shape, and that is a presumption, not a measurement.
- **`hk-pw1wv` is untouched** and `make full` stays red, so the commit gate every dispatched run
  must pass still fails for a reason unrelated to any agent's work.
