# TASKS — clear the ground

**Charlie's processing list.** A row here has settled dependencies and a task file in `tasks/` that
is complete enough to become a bead without interpretation.

If a task file is ambiguous, that is a defect in the task file. Send it back rather than deciding.

**Ordering is a recommendation, not a schedule.** Run anything whose `depends_on` has landed. The
rows are grouped by what unblocks the most work, not by batch — batches are a writing unit only.

**The live queue is `charlie-batch`.** `charlie-q` is stopped after a failure and is abandoned;
work submitted there lands nothing. Confirm with `harmonik queue list` before submitting.

Last updated 2026-08-24 (second revision — batch 3 listed), after measuring every row against the ledger and both branches. **Twelve
rows were carried as pending when the work had already landed** — they are moved to §Landed below.
The two branches have diverged and neither is a superset: `crew-cleanup-skill` is on the alpha
branch only, `dead-shell-sweep` and `run-goroutine-supervisor` are on `work/charlie-batch-1` only.

---

## Ready now — nothing blocks these

| # | Task | P | Workstream | Bead | Why it is first |
|---|---|---|---|---|---|
| 1 | [`review-trailer-never-stamped`](tasks/review-trailer-never-stamped.md) | **P0** | W7 | `hk-pipeline-commits-claim-unreviewed-wgk5x` | **Every commit this pipeline produces claims no reviewer was reached, and one almost always was.** 176 commits carry the false trailer; zero carry the shape the stamp would leave, so it has never once run. Third time filed. Read the traps — the last attempt was blocked for reversing every commit message body. **Reproduced end to end on 2026-08-24:** run `01a03538` returned two APPROVE verdicts and the commit it landed says nobody reviewed it. Done-when 2 through 6 are in-repo tests and are routable now; only done-when 1 waits on a redeploy. |
| 2 | [`subprocess-smoke-accepts-crash`](tasks/subprocess-smoke-accepts-crash.md) | **P0** | W0 | `hk-subprocess-smoke-accepts-crash-n9e3v` | **The only test in the merge decision that boots the real binary passes when the implementer crashes.** Reproduced on demand: the test goes green on `run_failed … sub_reason=claude_crashed exit=1`. It reads the log as TEXT, so a failure summary that quotes `run_completed` satisfies it, and it correlates on no run id, so a terminal event from any other run satisfies it too. **Read the scope note** — accepting `run_failed` was a documented design choice, not an oversight, so "assert `run_completed`" is NOT the fix and would need product surface that does not exist. |
| 3 | [`runregistry-extract`](tasks/runregistry-extract.md) | P1 | W2 | `hk-runregistry-extract-l8w1u` | **Highest leverage on the list.** Two further rows unblock the moment it lands, and its own dependency is already in. |
| 4 | [`structural-scoreboard`](tasks/structural-scoreboard.md) | P1 | W0 | `hk-structural-scoreboard-7o2u2` | Without it this program cannot tell real progress from a comment deletion. |
| 5 | [`test-mass-cost-measure`](tasks/test-mass-cost-measure.md) | P1 | W5 | `hk-test-mass-cost-measure-3gmk8` | Answers whether the whole program is possible. Read-only, so it is free to run alongside anything. |
| 6 | [`skill-copy-governance`](tasks/skill-copy-governance.md) | P1 | W7 | `hk-skill-copy-governance-8fz8h` | Its dependency landed. Small. |
| 7 | [`keeper-watcher-test-forks-tmux`](tasks/keeper-watcher-test-forks-tmux.md) | P1 | W5 | `hk-keeper-watcher-test-forks-tmux-dqiku` | **Turned the batch gate red on 2026-08-24 while the branch was green.** Four of the sixteen keeper watcher tests fork real `tmux` subprocesses on every 10 ms tick inside a 150 ms budget, so the gate measures machine load rather than the code. Independent of everything else. |
| 8 | [`run-verb-table`](tasks/run-verb-table.md) | P1 | W2 | `hk-run-verb-table-sfvx7` | The command-line tool picks its verb with 55 sequential if-statements in one 774-line function. Touches `cmd/harmonik/main.go` only, so it is outside the run-machine single-owner rule below. |
| 9 | [`runbead-extract-decisions`](tasks/runbead-extract-decisions.md) | P1 | W2 | `hk-runbead-extract-decisions-4wj2g` | Same shape, `cmd/harmonik/run.go`. Also outside the single-owner rule. |
| 10 | [`ops-monitor-never-flags-a-stopped-queue`](tasks/ops-monitor-never-flags-a-stopped-queue.md) | P1 | W7 | `hk-ops-monitor-never-flags-a-stopped-queue-nb2zv` | **The health check that is supposed to catch a stopped queue needs the crew ONLINE to fire, which is exactly when nobody is watching.** It also guesses the crew name by stripping a trailing `-q`, so `charlie-batch` never matches, and `main` is on its own suppression list while its header claims to cover `main`. Neither queue in the 2026-08-24 incident would ever have been flagged. Independent of everything else. |
| 11 | [`red-gate-merges-without-reviewer`](tasks/red-gate-merges-without-reviewer.md) | P1 | W7 | `hk-red-gate-merges-without-reviewer-uhboc` | **A RED commit gate merges anyway when the graph declares no reviewer node.** The defect reproduces in 15 seconds with a test already in the tree — `TestDeterministicGateFail_TellsTheImplementerToFixTheFailure` drives the real cascade over a reviewer-less graph with an always-red gate and gets `Success:true`; it passes today only because it logs the result and asserts nothing. The salvage also returns success with an EMPTY terminal node, which contradicts a normative spec, so this is spec alignment rather than a judgment call. **Load-time validation is the wrong fix** — reviewer-less graphs are legal and shipped; the file says why. Touches `dot_cascade_core.go`, which no live run holds. |

| 12 | [`ops-monitor-ignores-lane-status`](tasks/ops-monitor-ignores-lane-status.md) | P1 | W7 | `hk-uzd5j` | **A previously-inert defect that a 2026-08-24 change ARMED.** The fleet-stall check reads every field in the lane index except the one that says a lane is parked, so a lane parked on purpose fires an IMMEDIATE operator wake. It was harmless only because the lane array was empty; that array was re-seeded and the live index now produces a candidate. Independent of everything else. |

**Twelve rows, plus the thirteen linter rows below.** Two review-gate repairs that outrank everything
else on the list, one structural change, two test repairs, two command-line splits, the rest
measurement, governance and review.

## The lint exclusion-list burn-down — thirteen rows, all feedable

`tools/lintreport/allow.txt` holds **956 tolerated findings**. Its re-keying dependency landed
(`fbe830453`), so every row here is unblocked. The list is split one row per linter; each task file
names its own findings, its own count and its own structural acceptance test.

**Order matters within this block.** `lint-burn-gosec` runs first and the ledger enforces it: 17 of
its declarations also hold `errcheck` findings and 8 also hold `gocritic` findings, and editing a
declaration re-fingerprints every tolerated finding inside it. Four lesser overlaps are not
serialized. Each of the six task files on either side of those four pairs names its counterpart
and tells the implementer to grep the allow list for a declaration's file and name before editing
it.

| # | Task | P | Bead | Findings |
|---|---|---|---|---|
| 1 | [`lint-burn-gosec`](tasks/lint-burn-gosec.md) | P1 | `hk-lint-burn-gosec-ywauz` | 182 security findings. **Run this before `errcheck` and `gocritic`.** |
| 2 | [`lint-burn-unused`](tasks/lint-burn-unused.md) | P1 | `hk-lint-burn-unused-5ozcz` | 65 unused symbols — deletion, so no caller and no reference means no gate. |
| 3 | [`lint-burn-copyloopvar`](tasks/lint-burn-copyloopvar.md) | P1 | `hk-lint-burn-copyloopvar-bztqp` | 16 loop-variable copies Go 1.22 made unnecessary. |
| 4 | [`lint-burn-revive`](tasks/lint-burn-revive.md) | P2 | `hk-lint-burn-revive-3hn98` | 67 doc-comment and Go-naming findings. |
| 5 | [`lint-burn-forbidigo`](tasks/lint-burn-forbidigo.md) | P2 | `hk-lint-burn-forbidigo-it5ql` | 40 panics that should be returned errors. |
| 6 | [`lint-burn-context-plumbing`](tasks/lint-burn-context-plumbing.md) | P2 | `hk-lint-burn-context-plumbing-96zln` | 32 findings across three context linters. |
| 7 | [`lint-burn-unparam`](tasks/lint-burn-unparam.md) | P2 | `hk-lint-burn-unparam-wzbmo` | 28 parameters and results nothing varies or reads. |
| 8 | [`lint-burn-error-returns`](tasks/lint-burn-error-returns.md) | P2 | `hk-lint-burn-error-returns-v3hm5` | 22 swallowed, unwrapped or unchecked errors. |
| 9 | [`lint-burn-prealloc-unconvert`](tasks/lint-burn-prealloc-unconvert.md) | P2 | `hk-lint-burn-prealloc-unconvert-cj33e` | 17 pre-allocation and redundant-conversion findings. |
| 10 | [`lint-burn-exhaustive`](tasks/lint-burn-exhaustive.md) | P2 | `hk-lint-burn-exhaustive-29g0t` | 7 non-exhaustive switches. **The repair is a `default:` clause, never an allow-list entry** — adding an event type re-fingerprints every `exhaustive` finding. |
| 11 | [`lint-burn-last-three-linters`](tasks/lint-burn-last-three-linters.md) | P3 | `hk-lint-burn-last-three-linters-lj4wu` | The last six rows. |
| — | [`lint-burn-errcheck`](tasks/lint-burn-errcheck.md) | P1 | `hk-lint-burn-errcheck-udr56` | **Held in the ledger behind `lint-burn-gosec`.** 17 shared declarations. |
| — | [`lint-burn-gocritic`](tasks/lint-burn-gocritic.md) | P2 | `hk-lint-burn-gocritic-hsqwt` | **Held in the ledger behind `lint-burn-gosec`.** 8 shared declarations. |

## In flight — do not pick up

| Task | Bead | Where |
|---|---|---|
| [`lint-burn-gosec`](tasks/lint-burn-gosec.md) | `hk-lint-burn-gosec-ywauz` | Dispatched on `charlie-batch`, bead `in_progress`. It gates `errcheck` and `gocritic`, so neither of those may be fed until it lands. |

## Held — do not feed these yet

| Task | Bead | Why held |
|---|---|---|
| [`core-split-by-cluster`](tasks/core-split-by-cluster.md) | `hk-core-split-by-cluster-ee833` | **Failed twice and stopped `charlie-q`.** Two attempts are stranded on run branches, neither merged, each touching 330+ files against a task file that says one package per landing. A reviewer also found spec-bearing doc comment cut from `internal/queue/resume.go` during the move. The task file is being rewritten to name one starting package and to forbid comment edits inside a move. Do not re-feed until that lands. |
| [`run-machine-second-wave`](tasks/run-machine-second-wave.md) | `hk-run-machine-second-wave-8hurm` | **Blocked by the review gate, correctly, and the fault is the task file's.** The implementer met "under complexity 30" by renaming four functions, moving each body verbatim behind an inline `//nolint:funlen,gocognit,cyclop`, and leaving a pass-through wrapper under the old name. A body moved verbatim cannot change complexity. **Do not quote that run's complexity figures** — they match no command in this repo; `gocognit` on the current tree reports `driveDotWorkflow` 205, `beadRunOne` 129, `runWorkLoop` 505. Rule 3 below now forbids the manoeuvre. Do not re-feed until the task file and the bead body agree and both carry rule 3. |
| [`runenv-narrow-inputs`](tasks/runenv-narrow-inputs.md) | `hk-runenv-narrow-inputs-1z88x` | Dependency landed, but the single-owner rule below covers the whole W2 run-machine chain and `run-machine-second-wave` holds those files right now. Free the moment it finishes. |

## Blocked — waiting on something real

| Task | P | Bead | Waits on |
|---|---|---|---|
| [`spendmeter-extract`](tasks/spendmeter-extract.md) | P1 | `hk-spendmeter-extract-pph48` | `runregistry-extract` — row 1 above |
| [`handlerpause-extract`](tasks/handlerpause-extract.md) | P1 | `hk-handlerpause-extract-bkjoo` | `runregistry-extract` — row 1 above |
| [`harnesspick-extract`](tasks/harnesspick-extract.md) | P1 | `hk-harnesspick-extract-3290z` | `harness-composition-root-policy`, which is deferred pending an operator ruling |

## Landed — kept so the record is not re-derived

Verified 2026-08-24 with `git log <branch> --grep "<bead-id>"`. All beads closed.

| Task | Bead | Landed at | On |
|---|---|---|---|
| `reviewer-subagent-drift` | closed | `9c2424259` | both |
| `lint-rekey-exclusion-list` | closed | `fbe830453` | both |
| `dispatch-activation-guard` | closed | `626a4a25e` | both |
| `workloop-name-the-state` | closed | `bb066d18f` | both |
| `cli-structure-assessment` | closed | `32beb52ec` | both |
| `crew-cleanup-skill` | closed | `4c4e08efc` | **alpha only** |
| `reviewer-portability-review` | closed | `296c759d3` | **batch only** |
| `dead-shell-sweep` | closed | `1b0da6fe3` | **batch only** |
| `lint-ratchet-mutation-proof` | closed | `16e946d28`, `0fa5698d6`, `8cbac5127` | both |
| `workloop-extract-pure-decisions` | closed | `d9cb8e209` | both |
| `run-goroutine-supervisor` | closed | `ffb75b7cf` | **batch only** |
| `core-cluster-map` | closed | `a3b400d72` | both |
| `cli-extract-logic` | closed | `2ac530b3a`, `b5a8b3120` | both |

## Single-owner constraint

**The whole W2 run-machine chain must have one owner** — `run-machine-second-wave`,
`runenv-narrow-inputs`, `runregistry-extract`, `spendmeter-extract`, `handlerpause-extract` and the
batch-3 extractions when they are written. They touch the same files (`scheduler.go`,
`workloop.go`, `dot_cascade_core.go`, `agentlaunch.go`) and two lanes working the run machine at
once will collide. Everything else on this list is free to run in parallel.

## Written, deliberately not listed

**These three are `deferred` in the ledger, not `open`**, so `br ready` does not surface them and
Charlie will not pick them up. When a decision is recorded in the task file, move the bead back to
`open` and it becomes dispatchable. Deferring is not a terminal transition, so this does not tread on
the daemon's ownership of those.

| Task | Bead | Why not |
|---|---|---|
| [`eager-refill-decision`](tasks/eager-refill-decision.md) | `hk-eager-refill-decision-ogq02` | Needs an operator decision on whether eager refill exists at all. Both branches are real work and doing either speculatively wastes it. |
| [`harness-composition-root-policy`](tasks/harness-composition-root-policy.md) | `hk-harness-composition-root-policy-95byq` | A freeze gate states in writing that two of these files stay in the daemon on purpose. Extracting them silently reverses a written policy. Blocks `harnesspick-extract`. |
| [`signalresumewatcher-dead`](tasks/signalresumewatcher-dead.md) | `hk-signalresumewatcher-dead-r3kxe` | 74 lines with zero production callers, found while mapping the pause boundary. Delete or wire — not a call an agent should make. |

## Not yet written — this is the lane's own backlog

Rows cannot be fed from here; the task files do not exist yet. Writing them is alpha's job and it is
what keeps the runway above zero.

| Batch | What | State |
|---|---|---|
| 3 | The four W2.3 extractions — `run() int`, `runBeadSubcommandIO`, `driveDotWorkflow`, `beadRunOne` | **Written, reviewed, fixed and listed.** Two are on the ready list above. The other two — [`dotworkflow-extract-decisions`](tasks/dotworkflow-extract-decisions.md) and [`beadrunone-extract-phases`](tasks/beadrunone-extract-phases.md) — are held: they touch `dot_cascade_core.go` and `workloop.go`, which `run-machine-second-wave` holds right now. No bead yet; create one for each the moment that lands. |
| 3 | W1.3 — burn down the exclusion list centre, split per linter | **Thirteen rows written, reviewed and listed above — but they cover 726 of the 956 findings, not all of them.** |
| 1 | The three complexity linters — `gocognit` 186, `cyclop` 38, `funlen` 6, so **230 findings, a quarter of the list, that no row owns** | Not written, and deliberately last. Every one of them is a symptom of a function that the extraction workstream is already splitting, so writing these files before those land would produce work that the extractions redo. Write them when the run-machine and command-line splits are in. |
| 6 | Shell to Go — six task files written 2026-08-24 | **WRITTEN, NOT LISTED. Needs an operator ruling first.** PLAN.md does not describe a shell-to-Go workstream at all; W6 there is the crew-cleanup skill. The only mandate anywhere was a single line in an earlier revision of THIS file, which is not an authority. Three of the six convert gates that `make fast` / `make core` / `make full` depend on, so this is not a free change. |
| — | Follow-on packages for `core-split-by-cluster`, one per package after the first | Not started; depends on the rewrite of that task file |
| 3 | **The one test that boots the real binary accepts a crash as a pass** (`hk-subprocess-smoke-accepts-crash-n9e3v`, P0) | **Written, reviewed, and on the ready list above.** Writing it corrected the record: accepting a crashed run was a documented design choice from the originating commit, not an oversight, so the fix is a judge that decodes the events and correlates on run id — not an assertion that the run completed. |
| 3 | **A red commit gate merges when the workflow graph has no reviewer node** (`hk-red-gate-merges-without-reviewer-uhboc`, P1) | **Written, reviewed, and on the ready list above.** Writing it sharpened the record: all five tracked graphs that declare a `commit_gate` also declare a reviewer, so the unsafe branch is unreachable from every graph in this repo. The topology guard is also not an original design choice — it was added with no reviewer check at all, then narrowed after three confirmed production merges of unreviewed work, which argues for removing it rather than narrowing it a third time. |

### Deletion candidates the dead-shell sweep missed

Found 2026-08-24 while surveying for the shell-to-Go batch. Each has no caller in the Makefile, CI,
Go code or another script — only mentions in plan documents. No task file written: deletion with no
caller and no reference needs no gate, so this belongs to a sweep, not to a task.

`scripts/agents-skills-sync.sh`, `scripts/hk-wake-idle-test.sh`, `scripts/core-loop-assert-test.sh`,
`scripts/core-loop-matrix-preflight-test.sh`, `scripts/stranded-test-seams-test.sh`,
`scripts/keeper-seam-ratchet.sh`. One more, `scripts/go-cache-reap.sh`, is referenced only by
`docs/disk-reclaim.md` — that is a live operator runbook, so treat it as called and leave it.

**A gate test whose subject nothing runs.** `changed-func-coverage-test.sh` runs inside
`make script-tests`, so it is in `make fast` and `make full`. The script it tests runs only from
`make coverage-changed`, which no gate calls. The test is in the inner loop and its subject is not.

---

## Standing rules for anything on this list

1. **Never widen `tools/lintreport/allow.txt`.** The re-keying task changed how entries are keyed,
   not which findings are tolerated.
2. **A structural acceptance test, or it is not done.** Complexity, parameter count, package
   boundary, caller count. Never a line count — the comment cut proved that metric is gameable.
3. **A suppression is not a fix, and this one has already been tried.** On 2026-08-24 an implementer
   met a "under complexity 30" criterion on four functions by renaming each one, moving the body
   into the renamed function verbatim behind an inline `//nolint:funlen,gocognit,cyclop`, and
   leaving a pass-through wrapper under the old name. A body moved verbatim cannot change
   complexity, so nothing moved, and the tolerated findings passed only because they had relocated
   onto the new `nolint`. (`gocognit` on the current tree reports `driveDotWorkflow` 205 and
   `beadRunOne` 129. Take your own measurement — the figures quoted in that run's verdict match no
   command in this repository.) The review gate caught it and blocked the run. So write every complexity
   criterion to say the finding is GONE rather than quiet, and add the check that makes it
   falsifiable: **no new `nolint` directive for a complexity linter may appear anywhere in the
   diff.** That one is greppable, which is the point.
4. **A move is a move.** Do not edit or delete doc comments inside a landing that relocates code.
   Comments are re-examined afterwards as a separate step (PLAN.md §W3.3). Spec-bearing comment has
   already been lost this way once.
5. **Deletion of something with no caller and no reference needs no gate.** Moves need the re-keying
   task, which has landed.
6. `plans/2026-08-21-back-on-track/` and `plans/2026-08-22-decomposition-program/` are rejected by the
   operator and are not inputs. Do not revive their task IDs.
