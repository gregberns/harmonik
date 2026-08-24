# TASKS — clear the ground

**Charlie's processing list.** A row here has settled dependencies and a task file in `tasks/` that
is complete enough to become a bead without interpretation.

If a task file is ambiguous, that is a defect in the task file. Send it back rather than deciding.

**Ordering is a recommendation, not a schedule.** Run anything whose `depends_on` has landed. The
rows are grouped by what unblocks the most work, not by batch — batches are a writing unit only.

Last updated 2026-08-23 evening, after a review of the plan found three defects that would have sent
an implementer the wrong way. The task files for `lint-rekey-exclusion-list`, `workloop-name-the-state`
and `workloop-extract-pure-decisions` were corrected, and their issue bodies were re-synced from the
files — an issue body is a copy, so a fix to a task file does not reach a dispatched implementer on
its own. Batches 1, 2, 4 and 5 written. Batch 3 (the four daemon extractions from the
2026-08-22 review) is in progress and waits on a boundary survey. Batch 6 (shell → Go) not started.

---

## Ready now — nothing blocks these

| # | Task | P | Workstream | Bead | Why it is first |
|---|---|---|---|---|---|
| 1 | [`reviewer-subagent-drift`](tasks/reviewer-subagent-drift.md) | P0 | W7 | `hk-reviewer-subagent-drift-q0rvu` | Reviewer sub-agents are producing junk comment work *right now*. One file. |
| 2 | [`lint-rekey-exclusion-list`](tasks/lint-rekey-exclusion-list.md) | P0 | W1 | `hk-lint-rekey-exclusion-list-gfry7` | Blocks eight other tasks. Nothing structural starts until this lands. |
| 3 | [`dispatch-activation-guard`](tasks/dispatch-activation-guard.md) | P0 | W0 | `hk-dispatch-activation-guard-atfwr` | Cheap, and the thing it prevents can permanently wedge restart. |
| 4 | [`workloop-name-the-state`](tasks/workloop-name-the-state.md) | P0 | W2 | `hk-workloop-name-the-state-d25s3` | The function the operator named first. Nobody is on it. |
| 5 | [`structural-scoreboard`](tasks/structural-scoreboard.md) | P1 | W0 | `hk-structural-scoreboard-7o2u2` | Without it, this program cannot tell real progress from a comment deletion. |
| 6 | [`cli-structure-assessment`](tasks/cli-structure-assessment.md) | P1 | W4 | `hk-cli-structure-assessment-a0dzb` | 24,491 lines nobody has ever assessed. Read-only, so it is free to run alongside anything. |
| 7 | [`test-mass-cost-measure`](tasks/test-mass-cost-measure.md) | P1 | W5 | `hk-test-mass-cost-measure-3gmk8` | Answers whether this whole program is possible. Read-only. |
| 8 | [`crew-cleanup-skill`](tasks/crew-cleanup-skill.md) | P1 | W6 | `hk-crew-cleanup-skill-y1tz6` | Independent of everything else. |
| 9 | [`dead-shell-sweep`](tasks/dead-shell-sweep.md) | P2 | W6 | `hk-dead-shell-sweep-5zupe` | Pure deletion, needs no gate and no ruling. |

## Ready when their dependency lands

| # | Task | P | Workstream | Bead | Waits on |
|---|---|---|---|---|---|
| 10 | [`lint-ratchet-mutation-proof`](tasks/lint-ratchet-mutation-proof.md) | P0 | W1 | `hk-lint-ratchet-mutation-proof-hdju9` | `lint-rekey-exclusion-list` |
| 11 | [`workloop-extract-pure-decisions`](tasks/workloop-extract-pure-decisions.md) | P0 | W2 | `hk-workloop-extract-pure-decisions-bxhw7` | `workloop-name-the-state` |
| 12 | [`run-goroutine-supervisor`](tasks/run-goroutine-supervisor.md) | P1 | W2 | `hk-run-goroutine-supervisor-bwfze` | `workloop-name-the-state` |
| 13 | [`runenv-narrow-inputs`](tasks/runenv-narrow-inputs.md) | P1 | W2 | `hk-runenv-narrow-inputs-1z88x` | `run-goroutine-supervisor` |
| 14 | [`run-machine-second-wave`](tasks/run-machine-second-wave.md) | P1 | W2 | `hk-run-machine-second-wave-8hurm` | `workloop-extract-pure-decisions` |
| 15 | [`skill-copy-governance`](tasks/skill-copy-governance.md) | P1 | W7 | `hk-skill-copy-governance-8fz8h` | `reviewer-subagent-drift` |
| 16 | [`reviewer-portability-review`](tasks/reviewer-portability-review.md) | P1 | W7 | `hk-reviewer-portability-review-9fbjm` | `reviewer-subagent-drift` |
| 17 | [`core-cluster-map`](tasks/core-cluster-map.md) | P1 | W3 | `hk-core-cluster-map-l6rq9` | `lint-rekey-exclusion-list` |
| 18 | [`core-split-by-cluster`](tasks/core-split-by-cluster.md) | P1 | W3 | `hk-core-split-by-cluster-ee833` | `core-cluster-map` |
| 19 | [`cli-extract-logic`](tasks/cli-extract-logic.md) | P1 | W4 | `hk-cli-extract-logic-aie1g` | `cli-structure-assessment` + `lint-rekey-exclusion-list` |
| 20 | [`runregistry-extract`](tasks/runregistry-extract.md) | P1 | W2 | `hk-runregistry-extract-l8w1u` | `lint-rekey-exclusion-list` |
| 21 | [`harnesspick-extract`](tasks/harnesspick-extract.md) | P1 | W2 | `hk-harnesspick-extract-3290z` | `lint-rekey-exclusion-list` + `harness-composition-root-policy` |
| 22 | [`spendmeter-extract`](tasks/spendmeter-extract.md) | P1 | W2 | `hk-spendmeter-extract-pph48` | `runregistry-extract` |
| 23 | [`handlerpause-extract`](tasks/handlerpause-extract.md) | P1 | W2 | `hk-handlerpause-extract-bkjoo` | `runregistry-extract` |

## Single-owner constraint

**Tasks 4, 11, 12, 13 and 14 — the whole W2 run-machine chain — must have one owner.** They touch the same files
(`scheduler.go`, `workloop.go`, `dot_cascade_core.go`, `agentlaunch.go`) and two lanes working the
run machine at once will collide. Everything else on this list is free to run in parallel.

## Written, deliberately not listed

**These three are `deferred` in the ledger, not `open`**, so `br ready` does not surface them and
Charlie will not pick them up. When a decision is recorded in the task file, move the bead back to
`open` and it becomes dispatchable. Deferring is not a terminal transition, so this does not tread on
the daemon's ownership of those.

| Task | Bead | Why not |
|---|---|---|
| [`eager-refill-decision`](tasks/eager-refill-decision.md) | `hk-eager-refill-decision-ogq02` | Needs an operator decision on whether eager refill exists at all. Both branches are real work and doing either speculatively wastes it. |
| [`harness-composition-root-policy`](tasks/harness-composition-root-policy.md) | `hk-harness-composition-root-policy-95byq` | A freeze gate states in writing that two of these files stay in the daemon on purpose. Extracting them silently reverses a written policy. Blocks task 21. |
| [`signalresumewatcher-dead`](tasks/signalresumewatcher-dead.md) | `hk-signalresumewatcher-dead-r3kxe` | 74 lines with zero production callers, found while mapping the pause boundary. Delete or wire — not a call an agent should make. |

---

## Standing rules for anything on this list

1. **Never widen `tools/lintreport/allow.txt`.** Task 2 changes how entries are keyed, not which
   findings are tolerated.
2. **A structural acceptance test, or it is not done.** Complexity, parameter count, package
   boundary, caller count. Never a line count — the comment cut proved that metric is gameable.
3. **Deletion of something with no caller and no reference needs no gate.** Moves need task 2.
4. `plans/2026-08-21-back-on-track/` and `plans/2026-08-22-decomposition-program/` are rejected by the
   operator and are not inputs. Do not revive their task IDs.
