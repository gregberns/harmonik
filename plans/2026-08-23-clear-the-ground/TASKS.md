# TASKS — clear the ground

**Charlie's processing list.** A row here has settled dependencies and a task file in `tasks/` that
is complete enough to become a bead without interpretation.

If a task file is ambiguous, that is a defect in the task file. Send it back rather than deciding.

**Ordering is a recommendation, not a schedule.** Run anything whose `depends_on` has landed. The
rows are grouped by what unblocks the most work, not by batch — batches are a writing unit only.

**The live queue is `charlie-batch`.** Measured 2026-08-25 06:32Z with
`harmonik queue status --queue charlie-batch`: it is active, it has one worker, and that worker holds
the spend-meter extraction (`hk-spendmeter-extract-pph48`, dispatched). One item in flight, not two.
The run-registry extraction it used to hold is completed. Take your own reading before you submit —
this queue moves by the minute.

**No queue named `charlie-q` exists now.** An earlier revision of this file warned that one was
stopped. `harmonik queue list` on 2026-08-25 shows no queue of that name, among the active queues or
the completed ones. What does exist is `charlie-work-q`, which is completed — that near-miss is the
likely source of the confusion. The queue that is stopped is **`main`** — `paused-by-failure` since
`hk-pipeline-commits-claim-unreviewed-wgk5x` failed in it. That bead has since landed (`584cfdee1`),
so the pause now holds on a fault that is fixed: it needs a resume, not a report. Do not submit work
to `main` in the meantime.

Last updated 2026-08-25 (fourth revision), after re-measuring the bead ledger and the live queue.
The 2026-08-24 revision measured every row against the bead ledger and against both branches
**by file presence, not by `git log --grep`** — a grep over commit messages
misses a cherry-pick and misses a squash. Three corrections came out of it. **Seven more rows were
carried as ready when the work had already landed** on `work/charlie-batch-1`; they are moved to
§Landed. **`lint-burn-gosec` was carried as dispatched and is not dispatched** — most of its work is done
and parked on a tag, and only one commit of that is safe to land; see §Done, not landed.
**`runregistry-extract` and `lint-burn-copyloopvar` have since closed and landed** on
`work/charlie-batch-1`; both are moved to §Landed. The two branches have diverged and neither is a
superset: `crew-cleanup-skill` is on the alpha branch only, `dead-shell-sweep`,
`run-goroutine-supervisor`, the run-registry move and the loop-variable sweep are on
`work/charlie-batch-1` only.

---

## Ready now — nothing blocks these

| # | Task | P | Workstream | Bead | Why it is first |
|---|---|---|---|---|---|
| 1 | [`structural-scoreboard`](tasks/structural-scoreboard.md) | P1 | W0 | `hk-structural-scoreboard-7o2u2` | Without it this program cannot tell real progress from a comment deletion. |
| 2 | [`test-mass-cost-measure`](tasks/test-mass-cost-measure.md) | P1 | W5 | `hk-test-mass-cost-measure-3gmk8` | Answers whether the whole program is possible. Read-only, so it is free to run alongside anything. |
| 3 | [`skill-copy-governance`](tasks/skill-copy-governance.md) | P1 | W7 | `hk-skill-copy-governance-8fz8h` | Its dependency landed. Small. |
| 4 | [`runbead-extract-decisions`](tasks/runbead-extract-decisions.md) | P1 | W2 | `hk-runbead-extract-decisions-4wj2g` | `cmd/harmonik/run.go` `runBeadSubcommandIO` is 540 lines. Outside the single-owner rule below. **Read this before you start — the first attempt failed and the trap is easy to walk into again.** That attempt is on the tag `rescue/runbead-extract-4wj2g`, was cherry-picked as `25d4720fc` and `18a75fda2`, and was reverted by `2083adcfa`. **It renamed the function instead of decomposing it:** a 14-line shim over a new `runBeadOptionsIO` carrying 338 lines at `gocognit` 86, plus a new `parseRunBeadOptions` at 27, against a ceiling of 20, with no allow-list entry for either. Renaming the body moved the allow-list key rather than resolving the finding, and `make lint-allow` judges the whole tree inside `make fast`, so the branch would have stayed red under every later bead. Real reduction was 540 lines to 421, not 540 to 14. Two of the three mutation checks the commit body claimed do not reproduce, and seven flag doc comments were deleted, which the task file Limits forbid. **Start from acceptance item 4, the mutation check, rather than ending with it.** The bead is open; it is marked `failed` in `charlie-batch` from that run. |

**Four rows, plus the eight feedable linter rows below (of thirteen written).** One structural
measurement, one read-only cost measurement, one governance row, and one command-line split that has
already failed once. The two
review-gate repairs and the two test repairs that used to head this list are landed on
`work/charlie-batch-1` — see §Landed.

## The lint exclusion-list burn-down — thirteen rows, eight feedable at 2026-08-25 06:32Z

`tools/lintreport/allow.txt` holds **956 tolerated entries**. Its re-keying dependency landed
(`fbe830453`), so no row here waits on it.

**"956 findings" is the wrong word, and the tool itself makes the distinction.** An entry on the
allow list is one content identity, and one identity can carry more than one finding. `make
lint-allow` reports **989 findings across 934 identities**, and it names **22 of the 956 entries** as
already stale — the finding they tolerate is gone and the line is waiting to be deleted. Re-measured
2026-08-25 06:32Z; the 2026-08-24 run gave the same three figures. Use "identity" for a row and
"finding" for a report, the way `judge` in `tools/lintreport/main.go` does.

**Five of the thirteen are not feedable, and one of the five is a trap.** Measured 2026-08-25 06:32Z
from `br ready --limit 0`, from `br show` on each of the thirteen, and from `br blocked`. Take your
own reading before you pull anything: this ledger moves by the minute, and a count on this page is
only worth what its stamp says.

| Row | State at 2026-08-25 06:32Z | Why you cannot take it |
|---|---|---|
| `lint-burn-copyloopvar` | **closed** | **DO NOT PULL — the work is done.** A worker held it `in_progress` earlier the same morning and it landed on `work/charlie-batch-1` (`9aaf0ea5b`). Starting it again redoes landed work. |
| `lint-burn-gosec` | `deferred` | Most of the work is done and parked on a tag. See §Done, not landed. |
| `lint-burn-errcheck` | `open`, blocked | `br blocked` holds it behind `lint-burn-gosec`. 17 shared declarations. |
| `lint-burn-gocritic` | `open`, blocked | `br blocked` holds it behind `lint-burn-gosec`. 8 shared declarations. |
| `lint-burn-last-three-linters` | `open`, blocked | `br blocked` holds it behind the first core split (`hk-core-split-by-cluster-ee833`), which is `deferred` and sits in §Held. So it waits on a row nobody is going to feed. |

**The other eight are in `br ready` now**, and they are the whole feedable set: `lint-burn-unused`,
`lint-burn-revive`, `lint-burn-forbidigo`, `lint-burn-context-plumbing`, `lint-burn-unparam`,
`lint-burn-prealloc-unconvert`, `lint-burn-exhaustive` and `lint-burn-error-returns`.

**These thirteen rows are not one batch, and nothing makes them one.** The coupling is the allow
list, not the linter configuration. `tools/lintreport` keys each tolerated finding by a hash of the
linter, the message and the **formatted source of the enclosing declaration** — see `findingDigest`
and `enclosing` in `tools/lintreport/main.go`. Edit a declaration and every tolerated finding inside
it re-keys, whatever linter reported it. **That is the hard coupling: two rows serialize when they
share a declaration.** Measured 2026-08-24 from the allow list: `gosec` and `errcheck` share **17**
declarations and `gosec` and `gocritic` share **8**, which is why the ledger holds those two behind
`gosec`. Four lesser overlaps are not serialized. Each of the six task files on either side of those
four pairs names its counterpart and tells the implementer to grep the allow list for a declaration's
file and name before editing it.

**There is a second, softer coupling, and 17 and 8 do not predict it.** `tools/lintreport/allow.txt`
is written sorted by digest, so the lines of all thirteen linters interleave through one file that
every lane deletes from. Measured 2026-08-24 and re-measured 2026-08-25, both times by real
three-way merges of the deletions: `gosec` against `errcheck` gives **44** conflict hunks, `gosec`
against `gocritic` gives **25**, and `gosec` against `depguard` gives **1**, although those two share
**zero** declarations. These are much weaker than the re-key hazard — each one resolves as the union
of the two deletion sets, which is mechanical — but a planner who uses 17 and 8 to decide what runs
in parallel will meet 44 and 25 instead. Plan for the re-key hazard, and expect text conflicts everywhere else.

**The lint cache is not the coupling, and `.golangci.yml` is not in play.** All thirteen task files
forbid editing that file. No row here can invalidate a cache the way a configuration change would, so
"they must land together or the cache goes stale" is not a reason to batch them.

| # | Task | P | Bead | Findings |
|---|---|---|---|---|
| — | [`lint-burn-gosec`](tasks/lint-burn-gosec.md) | P1 | `hk-lint-burn-gosec-ywauz` | 182 security findings. **DO NOT PULL — most of the work is done and one commit of it needs landing. See §Done, not landed.** The bead is `deferred`, so `br ready` no longer offers it and the ledger enforces this row. |
| 1 | [`lint-burn-unused`](tasks/lint-burn-unused.md) | P1 | `hk-lint-burn-unused-5ozcz` | 65 unused symbols — deletion, so no caller and no reference means no gate. |
| — | [`lint-burn-copyloopvar`](tasks/lint-burn-copyloopvar.md) | P1 | `hk-lint-burn-copyloopvar-bztqp` | 16 loop-variable copies Go 1.22 made unnecessary. **DO NOT PULL — done.** The bead closed 2026-08-25 and the work is on `work/charlie-batch-1` (`9aaf0ea5b`). A worker held this row earlier the same morning. See §Landed. |
| 2 | [`lint-burn-revive`](tasks/lint-burn-revive.md) | P2 | `hk-lint-burn-revive-3hn98` | 67 doc-comment and Go-naming findings. |
| 3 | [`lint-burn-forbidigo`](tasks/lint-burn-forbidigo.md) | P2 | `hk-lint-burn-forbidigo-it5ql` | 40 panics that should be returned errors. **This row owns all 40**, including the 8 the parked security series suppressed inline — see §Done, not landed. |
| 4 | [`lint-burn-context-plumbing`](tasks/lint-burn-context-plumbing.md) | P2 | `hk-lint-burn-context-plumbing-96zln` | 32 findings across three context linters. |
| 5 | [`lint-burn-unparam`](tasks/lint-burn-unparam.md) | P2 | `hk-lint-burn-unparam-wzbmo` | 28 parameters and results nothing varies or reads. |
| 6 | [`lint-burn-prealloc-unconvert`](tasks/lint-burn-prealloc-unconvert.md) | P2 | `hk-lint-burn-prealloc-unconvert-cj33e` | 17 pre-allocation and redundant-conversion findings. |
| 7 | [`lint-burn-exhaustive`](tasks/lint-burn-exhaustive.md) | P2 | `hk-lint-burn-exhaustive-29g0t` | 7 non-exhaustive switches. **The repair is a `default:` clause, never an allow-list entry** — adding an event type re-fingerprints every `exhaustive` finding. |
| 8 | [`lint-burn-error-returns`](tasks/lint-burn-error-returns.md) | P2 | `hk-lint-burn-error-returns-v3hm5` | 22 swallowed, unwrapped or unchecked errors. **Feedable — an earlier revision said this row was held behind `lint-burn-gosec` and it is not.** Re-checked 2026-08-25 06:32Z: the bead is `open`, `br blocked` does not list it, it has no dependency, and `br ready` offers it. |
| — | [`lint-burn-last-three-linters`](tasks/lint-burn-last-three-linters.md) | P3 | `hk-lint-burn-last-three-linters-lj4wu` | The last six rows. **Held in the ledger behind the first core split (`hk-core-split-by-cluster-ee833`)**, which is `deferred` and sits in §Held. |
| — | [`lint-burn-errcheck`](tasks/lint-burn-errcheck.md) | P1 | `hk-lint-burn-errcheck-udr56` | **Held in the ledger behind `lint-burn-gosec`.** 17 shared declarations. |
| — | [`lint-burn-gocritic`](tasks/lint-burn-gocritic.md) | P2 | `hk-lint-burn-gocritic-hsqwt` | **Held in the ledger behind `lint-burn-gosec`.** 8 shared declarations. |

## Done, not landed — do not implement these

| Task | Bead | Where the work is |
|---|---|---|
| [`lint-burn-gosec`](tasks/lint-burn-gosec.md) | `hk-lint-burn-gosec-ywauz` | Four commits, `296c759d3..b2136f097`, tagged `rescue/gosec-final-zero`. On neither branch. **Land `352e00e6b` only** — read the section below first. |

**Do not re-implement this row, and do not land the series wholesale.** An earlier revision of this
page said to land all four commits. An audit on 2026-08-24 overruled that half of the advice. The
work is real and must not be redone from zero, but three of the four commits carry a change nobody
asked for.

The series is `296c759d3..b2136f097` — `352e00e6b`, `3061a0897`, `fa206bdf7`, `b2136f097` — carried
on the tag `rescue/gosec-final-zero`. Measured 2026-08-24: none of those four commits is an ancestor
of `work/alpha-integration-merge` or of `work/charlie-batch-1`.

**Land `352e00e6b` only.** It is the clean, separable base of the range: 21 files, it removes 61 rows
from `tools/lintreport/allow.txt`, it adds zero suppressions, and it touches no argv. It stands on
its own. **The 61 rows are not all `gosec`** — they are 49 `gosec`, 4 `errcheck`, 3 `unused`, 3
`gocognit` and 1 `gocritic`, which is why the commit message says "49 gosec identities" and not 61.
The two figures do not disagree.

**Then one follow-up run, starting from the tag, to strip five things and keep a sixth.**

1. Delete both `safeexec` files and revert all 71 call sites.
2. Revert the `safeexec` depguard line in `.golangci.yml`. Keep the two `internal/secureio` entries.
3. Drop the 4 rows the series adds to `tools/lintreport/allow.txt`. Keep all 194 removals.
4. Drop the 2 `#nosec G304` comments on the `secureio.ReadFile` calls. They are redundant, because
   `secureio` is itself the fix.
5. **Put the 8 `//nolint:forbidigo` directives back to the `//nolint:gocritic` they replaced.**
   Reverting the safeexec call sites does **not** remove them, so do this as its own step. All 8 sit
   on intentional panics in recovery tests — 1 in `internal/core/crashrecovery_rc031_test.go`, 2 in
   `internal/core/detectorharness_rc010_test.go` and 5 in
   `internal/daemon/detectorbarrier_rc020b_test.go` — and not one of them is a command call site.
   Measured 2026-08-25: the base of the range carries 13 `//nolint:forbidigo` and 11
   `//nolint:gocritic`, and the tag carries 21 and 3. The last commit of the series, `b2136f097`,
   relabelled those 8 lines from one linter to the other and changed nothing else about them.
   `352e00e6b`, the commit to land, adds none of them. The allow list still tolerates all 8 as
   `forbidigo` entries — 40 at the base and the same 40 digests at the tag — so the inline directive
   quietens a finding the allow list already quietens, and it takes those 8 out of the reach of
   `lint-burn-forbidigo`, which owns all 40.
6. Keep `internal/secureio`, the ~105 real error checks and the permission tightenings.

**The good parts cannot be cherry-picked out, which is why the strip is a follow-up run and not a
shorter range.** The legitimate allow-list removals and the `safeexec` conversion are fused inside
one commit, `fa206bdf7` — 111 files, and 133 of the removals. No selection of commits separates them.

**What the audit found.** The series adds two copies of a `safeexec` helper —
`internal/safeexec/safeexec.go` and `internal/core/safeexec.go` — and converts 71 call sites across
21 files to them. The helper builds `exec.CommandContext(ctx, "/usr/bin/env")` and sets `cmd.Args[0]`
to `env`, so every argv shifts by one position. The tests that assert on `argv[0]` were never
updated: the tree at the tag holds 57 such assertions and the series edits none of them. The audit
counts 14 that break by inspection. The suppression counts also match the original rejection exactly
— 2 `#nosec`, 8 `//nolint:forbidigo` and 4 new rows in the burn-down file — so nothing was stripped
between iterations. All four commits carry `Reviewed-By: none` and `Review-Verdict: NOT_REVIEWED`.

**Charlie owns this row and lands the base commit through its own queue.** Leave the tag
`rescue/gosec-final-zero` in place — the follow-up run starts from it.

**Landing it releases two more rows.** The ledger holds `lint-burn-errcheck` (17 shared declarations)
and `lint-burn-gocritic` (8 shared declarations) behind this bead, and `br blocked` lists both of
them against it. Both become feedable the moment this one closes.

## In flight — do not pick up

| Task | Bead | Where |
|---|---|---|
| [`spendmeter-extract`](tasks/spendmeter-extract.md) | `hk-spendmeter-extract-pph48` | Dispatched on `charlie-batch`, bead `in_progress`, and the one item in that queue's in-flight set at 2026-08-25 06:32Z. It holds the W2 run-machine files, so the single-owner rule below holds `handlerpause-extract` until it lands. |

The keystone extraction that used to sit here, `runregistry-extract`, closed 2026-08-25 and landed on
`work/charlie-batch-1`. It is in §Landed. Do not carry it as in flight.

## Held — do not feed these yet

| Task | Bead | Why held |
|---|---|---|
| [`core-split-by-cluster`](tasks/core-split-by-cluster.md) | `hk-core-split-by-cluster-ee833` | **Failed twice, and each failure stopped the queue it ran on.** Two attempts are stranded on run branches, neither merged, each touching 330+ files against a task file that says one package per landing. A reviewer also found spec-bearing doc comment cut from `internal/queue/resume.go` during the move. The task file is being rewritten to name one starting package and to forbid comment edits inside a move. Do not re-feed until that lands. The bead is `deferred`, so the ledger enforces this row. |
| [`run-machine-second-wave`](tasks/run-machine-second-wave.md) | `hk-run-machine-second-wave-8hurm` | The bead is `deferred`, so `br ready` does not surface it and the ledger enforces this row, in the same way as the two rows around it. An earlier revision of this page carried the row as `open` and asked someone to defer it; that was done on 2026-08-24. **Why it is held:** the review gate blocked it, correctly, and the fault is the task file's. The implementer met "under complexity 30" by renaming four functions, moving each body verbatim behind an inline `//nolint:funlen,gocognit,cyclop`, and leaving a pass-through wrapper under the old name. A body moved verbatim cannot change complexity. **Do not quote that run's complexity figures** — they match no command in this repo; `gocognit` on the current tree reports `driveDotWorkflow` 205, `beadRunOne` 129, `runWorkLoop` 505. Rule 3 below now forbids the manoeuvre. Do not re-feed until the task file and the bead body agree and both carry rule 3. |
| [`runenv-narrow-inputs`](tasks/runenv-narrow-inputs.md) | `hk-runenv-narrow-inputs-1z88x` | Dependency landed, but the single-owner rule below covers the whole W2 run-machine chain and `run-machine-second-wave` holds those files right now. Free the moment it finishes. The bead is `deferred`, so the ledger enforces this row. |

## Blocked — waiting on something real

| Task | P | Bead | Waits on |
|---|---|---|---|
| [`handlerpause-extract`](tasks/handlerpause-extract.md) | P1 | `hk-handlerpause-extract-bkjoo` | **Nothing in the ledger holds it any more.** The run-registry extraction it waited on closed 2026-08-25, the bead is `open`, `br blocked` does not list it, and `br ready` offers it at 06:32Z. What holds it is the **single-owner rule** below: `spendmeter-extract` is in flight on the same run-machine files. Free the moment that lands. |
| [`harnesspick-extract`](tasks/harnesspick-extract.md) | P1 | `hk-harnesspick-extract-3290z` | `harness-composition-root-policy`, which is deferred pending an operator ruling. `br blocked` confirms the hold at 2026-08-25 06:32Z. |

## Landed — kept so the record is not re-derived

Verified 2026-08-24 by **file presence on each branch**, not by `git log <branch> --grep "<bead-id>"`.
A grep over commit messages misses a cherry-pick and misses a squash, and it is how the first twelve
of these rows stayed on the ready list after they had landed. All beads closed.

**A row marked "batch only" stays on this list until `work/charlie-batch-1` merges.** The work is not
on the integration branch yet, so the list still owes the record. Delete those rows at the merge, not
before.

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
| `review-trailer-never-stamped` | closed | `584cfdee1` | **batch only** |
| `subprocess-smoke-accepts-crash` | closed | `9b596838a` | **batch only** |
| `keeper-watcher-test-forks-tmux` | closed | `71bfb9edd`, `8f3374762`, `ab735241a` | **batch only** |
| `run-verb-table` | closed | `67f52c022` | **batch only** |
| `ops-monitor-never-flags-a-stopped-queue` | closed | `45d423898` | **batch only** |
| `red-gate-merges-without-reviewer` | closed | `4a6e9071a` | **batch only** |
| `ops-monitor-ignores-lane-status` | closed | `70daf3828` | **batch only** |
| `runregistry-extract` | closed | `1c7263b31`, `44c51f029` | **batch only** |
| `lint-burn-copyloopvar` | closed | `9aaf0ea5b` | **batch only** |

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
| 3 | The four W2.3 extractions — `run() int`, `runBeadSubcommandIO`, `driveDotWorkflow`, `beadRunOne` | **Written, reviewed, fixed and listed.** `run-verb-table` has landed on `work/charlie-batch-1` (`67f52c022`). `runbead-extract-decisions` is on the ready list above, with the record of its failed first attempt. The other two — [`dotworkflow-extract-decisions`](tasks/dotworkflow-extract-decisions.md) and [`beadrunone-extract-phases`](tasks/beadrunone-extract-phases.md) — are held: they touch `dot_cascade_core.go` and `workloop.go`, which `run-machine-second-wave` holds right now. No bead yet; create one for each the moment that lands. |
| 3 | W1.3 — burn down the exclusion list centre, split per linter | **Thirteen rows written, reviewed and listed above — but they cover 726 of the 956 entries, not all of them.** |
| 1 | The three complexity linters — `gocognit` 186, `cyclop` 38, `funlen` 6, so **230 entries, a quarter of the list, that no row owns** | Not written, and deliberately last. Every one of them is a symptom of a function that the extraction workstream is already splitting, so writing these files before those land would produce work that the extractions redo. Write them when the run-machine and command-line splits are in. |
| 6 | Shell to Go — six task files written 2026-08-24 | **WRITTEN, NOT LISTED. Needs an operator ruling first.** PLAN.md does not describe a shell-to-Go workstream at all; W6 there is the crew-cleanup skill. The only mandate anywhere was a single line in an earlier revision of THIS file, which is not an authority. Three of the six convert gates that `make fast` / `make core` / `make full` depend on, so this is not a free change. |
| — | Follow-on packages for `core-split-by-cluster`, one per package after the first | Not started; depends on the rewrite of that task file |
| 3 | **The one test that boots the real binary accepts a crash as a pass** (`hk-subprocess-smoke-accepts-crash-n9e3v`, P0) | **Landed on `work/charlie-batch-1` (`9b596838a`).** Writing it corrected the record: accepting a crashed run was a documented design choice from the originating commit, not an oversight, so the fix was a judge that decodes the events and correlates on run id — not an assertion that the run completed. |
| 3 | **A red commit gate merges when the workflow graph has no reviewer node** (`hk-red-gate-merges-without-reviewer-uhboc`, P1) | **Landed on `work/charlie-batch-1` (`4a6e9071a`).** Writing it sharpened the record: all five tracked graphs that declare a `commit_gate` also declare a reviewer, so the unsafe branch is unreachable from every graph in this repo. The topology guard was also not an original design choice — it was added with no reviewer check at all, then narrowed after three confirmed production merges of unreviewed work, which argued for removing it rather than narrowing it a third time. |

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
4. **A rename is not a decomposition, and it makes the branch red.** The same week, a second
   implementer moved a 540-line body into a new name behind a 14-line shim. The new name carried the
   complexity, no allow-list entry keyed to it, and `make lint-allow` judges the whole tree inside
   `make fast`, so every later bead on that branch would have inherited a red gate. It was reverted
   (`2083adcfa`). The allow list keys a tolerated finding to the source of its enclosing
   declaration, so renaming a function moves the key instead of resolving the finding.
5. **A move is a move.** Do not edit or delete doc comments inside a landing that relocates code.
   Comments are re-examined afterwards as a separate step (PLAN.md §W3.3). Spec-bearing comment has
   already been lost this way once, and seven flag doc comments were lost the second way in the
   reverted run above.
6. **Deletion of something with no caller and no reference needs no gate.** Moves need the re-keying
   task, which has landed.
7. `plans/2026-08-21-back-on-track/` and `plans/2026-08-22-decomposition-program/` are rejected by the
   operator and are not inputs. Do not revive their task IDs.
