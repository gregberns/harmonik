# Delete and rewrite

**Date:** 2026-07-27
**Supersedes:** `plans/2026-07-24-code-health-audit/` (see §4)
**Posture:** delete mechanically, rewrite from spec, harvest only at IO boundaries.

This plan is deliberately short. The failure mode of the last attempt was planning-doc churn
outweighing shipped production code by roughly an order of magnitude. The disease returns if this
acquires a task index or a state lattice — not if it gets longer. See
[`NEXT_STEPS.md`](NEXT_STEPS.md) for what happens after the deletions.

---

## 1. The decision

Do **not** rewrite the system. Do rewrite the two or three subsystems whose structure is
generating the bugs, and delete the test mass that currently makes any rewrite impossible.

Measured basis. The middle column is what this plan claimed on 2026-07-27. The right column is a
re-measurement made 2026-07-30, taken at `9c1061da5` (the commit just before this program's first
deletion) and at the current tip.

| Fact | Claimed 2026-07-27 | Re-measured 2026-07-30 |
|---|---|---|
| Production Go | 216,695 LOC / 902 files | 216,689 LOC / 900 files then. 214,117 LOC / 894 files now. |
| Test Go | 487,997 LOC (was 525,123 before the specaudit deletion) | 525,123 LOC then — that figure is exact. 307,741 LOC now. |
| Test files named after a bead ID | 885 files / 255,664 LOC (49% of all test code) | **Wrong when written: 720 files / 199,899 LOC, which is 38% of test code.** 30 files still match the selector now. |
| Production files nothing calls | 35 files / 4,276 LOC (a floor, not a ceiling) | **Wrong when written.** The deletion proved 8 of the 35 and removed 1,744 lines. 27 of the 35 are still in the tree. |
| Normative specs already written | 25,893 LOC across 19 files | 25,924 LOC across 34 files then. 26,068 LOC across 34 files now. The line count was right and the file count was wrong. |
| Last 50 commits: planning vs production code | 26,533 vs 1,808 lines (~15:1) | The ratio has inverted. Across the last 50 commits, `plans/` moved +1,389 / −62 lines and Go moved +10,911 / −14,153. |

The bead-ID row disagreed with its own evidence on the day it was written. The per-package table in
[`KEEP-DELETE.md`](KEEP-DELETE.md) sums to exactly 720 files, and 720 is what the selector returns
at `9c1061da5`. Re-run the selector before you trust any count on this page.

The specs are the rewrite oracle. They already exist and are not being used as one.

---

## 2. Do we need to inspect every file? No.

**Deletion is mechanically determined, not a judgment call.** Every deletion in §3 is
selected by a property a machine can check — zero production references, a build tag, a
filename pattern — and validated by a better oracle than any reviewer:

```
go build ./... && go test ./... && go test -tags=scenario ./test/scenario/... ./internal/daemon/...
```

If it compiles and the acceptance tier passes, the delete was safe. Sending a fleet of
agents to read 885 test files to reach the same conclusion the compiler reaches for free
is the same mistake as the last plan, at a different layer.

**Where agent effort IS justified:** harvesting irreducible external facts from IO-boundary
subsystems before their tests are deleted. A compiler cannot tell you that `tmux
paste-buffer` exits 0 before the TUI has rendered. That is real knowledge, it exists only
in commit messages and test headers, and it is about to be deleted. See §5.

Rule of thumb: **harvest at the boundary, delete in the interior.**

---

## 3. Keep / delete ledger

Full inventory in [`KEEP-DELETE.md`](KEEP-DELETE.md). Summary:

### Delete — ~323,000 lines planned, 225,080 lines removed

Outcome column re-derived from git on 2026-07-30.

| Order | Target | Planned lines | Why | What landed |
|---|---|---|---|---|
| 1 | `internal/specaudit` tagged sensors | 37,927 | 129 files behind `//go:build specaudit`, executing zero product code. Greps `specs/*.md` for headings. | **Landed 2026-07-27** at `e99a52fff`. 129 test files and 37,281 lines under `internal/specaudit`, 37,362 lines with the build wiring. The planned figure over-counted by about 650 lines. |
| 2 | bead-ID-named test files | 255,664 | Minus the carve-out below. | **Landed 2026-07-28** at `ec66da798`. 681 files and 185,114 lines. The planned figure was wrong when written: the selector matched 720 files and 199,899 lines, not 885 and 255,664. |
| 3 | 35 dead production files | 4,276 | Zero production callers. | **Landed 2026-07-28** at `afdfccbd0`. 1,744 lines. Only 8 of the 35 named files were provably dead. The other 27 are still in the tree. |
| 4 | `internal/scenario` harness engine | ~18,000 | Not in the scenario tier; `make test-scenario` never runs it. 35 of 44 tests test the harness itself (not re-verified). Engine is driven only by `harmonik harness`, which nothing invokes. | **Not done as planned.** The step that landed 2026-07-28 at `2179454a7` removed 860 lines of unwired crash-recovery types. `internal/scenario` still holds 16,269 lines, and `cmd/harmonik/harness.go` still registers the `harness` subcommand. |

**Order matters: 2 before 3.** The bead-ID tests are life support for dead production
code. Deleting them first makes more dead code provable, so re-run detection after step 2.
Step 3 shows what happens when you skip that: 27 of its 35 named files kept a caller.

### Retracted delete candidate: `internal/workflow/scenario`

Originally listed for deletion on the basis that the directory holds no non-test `.go`
files. **That reasoning was wrong** and the review caught it: "package contains no
production files" is not "tests do not exercise production code." All 20 files import
`internal/core`, `internal/workflow`, and `internal/workflow/dot`, and drive the real
traversal engine (`LoadDotWorkflow`, `DecideNextNode`) against the shipped
`specs/examples/*.dot` corpus. Deleting them would drop fixture coverage from **26 of 28
`.dot` examples to 6**, orphaning normative artifacts including
`plan-to-shipped-faithful.dot`, with no corpus-wide validator to backstop them. **Keep.**

Re-checked 2026-07-30: the directory still holds 20 files, they still import `internal/core`,
`internal/workflow` and `internal/workflow/dot`, and they still call `LoadDotWorkflow` and
`DecideNextNode`. `specs/examples/` still holds 28 `.dot` files. The 26-to-6 coverage figure
itself was not re-derived.

Generalize the lesson: a `prod=0 LOC` count for a directory is not evidence a test is
worthless. The only sound test is what the test file imports and calls.

### Keep

Sizes re-measured 2026-07-30.

| Target | Size | Why |
|---|---|---|
| `test/scenario/` | 6 Go files, 2,120 LOC, 12 test funcs | Black-box, cheapest real coverage in the repo. The original "11/11 green in 27s" was not re-run — see §7 for what the tier now reports. |
| `internal/daemon` `//go:build scenario` files (27 today) | 51 test funcs | The executable conformance suite for the specs the rewrite is driven from. Cites `EM-012a` ×26. The `EM-015d` and `PL-002` counts were not re-derived. |
| `internal/scenario` queue tests | 9 files, 4,253 LOC | Real queue tests that do not use the harness engine. Move them somewhere honest. |
| `specs/` | 26,068 LOC across 34 top-level files | The rewrite oracle. |

**Carve-out to the bead-ID rule:** the `//go:build scenario` files in `internal/daemon`
are bead-named but are the acceptance tier. They survive. Everything else bead-named goes.
The carve-out list was written as 28 paths. Two of them named review-loop scenarios and went
with the review-loop deletion on 2026-07-28, so 26 of the listed files remain. A 27th tagged
file, `rundriverfixture_test.go`, was never on the list and holds no test function.

---

## 4. Is the code-health audit still valid?

**Mostly no.** State as of today: **77 of 92 tasks still in triage, 13 complete, 5 deferred**
— roughly 14% executed. Re-checked 2026-07-30: `TASK-INDEX.yaml` still reads 77 triage, 13
complete, 1 design and 1 implement, and `tasks/` still holds 92 cards. The "5 deferred" is
not in the index and was not re-derived. And 47 of 92 task cards (94 files minus README and TEMPLATE) are written in extract / decompose /
characterize / thread language, which is work whose entire purpose is to make untestable
code testable. If the code is being replaced, those cards are moot.

What survives from it:

- **The specs it produced.** `specs/queue-model.md`, `execution-model.md`,
  `process-lifecycle.md`, `event-model.md` are normative and become the rewrite oracle.
- **The durable queue transaction substrate** (`internal/queue/transaction.go`) — real,
  landed, reviewed code.
- **The base-ref finding**: 44 commits stranded on
  `origin/integration/phase-reviewloop-20260725`, and at least one task marked integrated
  whose code was absent from the active branch. Reconcile before rewriting anything, or
  the rewrite lands on a branch that does not contain what you think it does.
  **Closed 2026-07-30.** `git ls-remote --heads origin` no longer lists that ref, and no
  local ref matches it. The audit's own `TASK-INDEX.yaml` already recorded the correction
  on 2026-07-27: the active line diverged at `498ad4450` and new work bases on
  `origin/phase1-session-restart-substrate`. Two salvage branches remain on origin,
  `salvage/reviewloop-decoupling-20260729` and `salvage/reviewloop-kernels-20260729`.
  Whether the 44 commits reached the active line was not re-derived, because the ref they
  sat on is gone.

What does not survive: the task cards, the coordinator protocol, the lane/staffing model.

**Do not formally close it.** Mark it superseded and move on; re-litigating it is more
planning.

---

## 5. Carry-forward: the one thing worth harvesting

Bug archaeology on `internal/daemon/pasteinject.go` (~53 bugs across 56 commits) split
**27 structure-caused : 26 world-caused** — roughly 1:1, not the 9:1 initially assumed.

- **Structure-caused** bugs vanish in a rewrite, and they are repetitive: 4× "used
  `os.Stat`/`exec.Command` instead of the run's `CommandRunner`", 4× "single-mode and
  review-loop paths drifted", 3× "gate applied to one of N call sites".
- **World-caused** bugs recur verbatim in any rewrite that does not carry the knowledge
  forward — and they are the chronic, user-visible half.

**Six subsystems are now harvested: 89 external facts recorded in**
[`CARRY-FORWARD.md`](CARRY-FORWARD.md)**, against 214 structure-caused bugs discarded — ~72%.**
The count read 83 when this section was first written. It is 89 today, counted 2026-07-30, and
one of the 89 is retracted. That repo-wide ratio is far more favourable to a rewrite than
pasteinject's 1:1, which is the worst case precisely because it sits on the TUI boundary.
**That file is the highest-value artifact of this effort.** It should graduate to `specs/`
once stable. Any rewrite of an agent-substrate subsystem must satisfy every fact in it.

Harvesting is **complete** for `pasteinject`, `tmuxsubstrate`, `codexwire`/`codexdriver`,
`harness/pi`, `brcli` and the srt sandbox gate. Do the same for any further IO-boundary subsystem
before deleting its tests. Not for interior logic.

---

## 6. Rewrite order

Driven by churn against a median of **3 commits per production file** in `internal/daemon`.
LOC and commit counts re-measured 2026-07-30.

| Target | LOC 2026-07-27 | LOC now | Commits now | Note |
|---|---|---|---|---|
| `workloop.go` | 6,656 | 3,389 | 382 | `beadRunOne` was 2,289 lines and is now 1,764. `runWorkLoop` was 1,657 (the plan said 1,670) and moved out — see below. |
| `scheduler.go` | — | 2,210 | 4 | New file. `runWorkLoop` moved here on 2026-07-28 at `756b6604c`. The function is now 1,281 lines. |
| `daemon.go` | — | 1,103 | 176 | |
| ~~`reviewloop.go`~~ | 2,194 | **deleted** | 116 | **Gone 2026-07-28** at `3cec5afd7`, together with `internal/daemon/launchspecbuild.go`, `internal/runloop/reviewcycle/` and `internal/runloop/continuity/`. Review-loop mode no longer exists. |
| `tmuxsubstrate.go` | 3,023 | 3,023 | 68 | |
| `pasteinject.go` | 2,691 | 2,691 | 56 | Three subsystems in one filename; paste injection is ~20% of it. |

**Start with `workloop.go`.** It has 6.8× the churn of pasteinject and the rewrite contract
is already known. It is now half the size the plan measured, and it no longer holds
`runWorkLoop`, so the target is `workloop.go` plus `scheduler.go`.

### The rewrite contract for workloop

No scenario test touches `beadRunOne` — re-checked 2026-07-30, the four scenario files that
name it name it only in comments. The acceptance tier enters one level up, through
`ExportedRunWorkLoop` (9 scenario-tagged files, unchanged) and `ExportedWorkLoopDeps`
(15 then, 13 now). So preserve exactly two things:

- `runWorkLoop(ctx, deps) error` — now defined in `internal/daemon/scheduler.go`, not `workloop.go`
- the `workLoopDeps` construction surface

and everything below can be burned while the entire acceptance suite validates the
replacement.

**The actual blockers were 4 unit test files** binding `beadRunOne`'s 7-parameter signature:
`pi_unknown_profile_refuse_test.go`, `pi_provider_selected_hk8ziid2_test.go`,
`workloop_gate_n5md3_test.go`, `hk3hozm_slot_leak_test.go`. The plan said all four were
bead-named and would die in step 3. **Three did. `pi_unknown_profile_refuse_test.go` is not
bead-named, the selector never matched it, and it is still in the tree** driving `beadRunOne`
through `runBeadOneTest` in `export_workloop_test.go`. That is the one remaining binder.

### pasteinject: three packages, not one

Real boundary, from the extraction analysis:

- `paneinject` — the actual injection (~528 LOC, 20% of the file)
- `agentwatchdog` — the two supervision watchdogs + budget policy (~880 LOC)
- `paneprobe` — process/worktree liveness probing (~282 LOC; its only consumer is
  `tmuxsubstrate.go`, not pasteinject)

The seam already exists and is narrow: **6 one-method interfaces + one `CommandRunner`**,
zero direct tmux calls. A rewrite is drivable entirely from fakes — no tmux, no pty, no
socket. ~80 package-level declarations collapse to 4 methods + 3 request structs.

Replace the ~23 mutable package globals with a config struct. That is what breaks the
existing tests, and it is the correct trade. Re-measured 2026-07-30: 23 top-level `var`
lines plus one `var (` block, and 74 top-level declarations in all. The three sub-package
line estimates above were not re-derived.

---

## 7. Known-live defects (do not lose these)

- ~~**Branch-protection deep guard fails open** (`hk-zobns`, P0)~~ — **closed as invalid
  2026-07-30. The guard never failed open.** The bead's own fixture asks to land on
  `integration` while only `main` was protected, so the merge correctly targeted an
  unprotected branch. The test was a false red. Coverage now exists at the guard's own
  seam in `internal/daemon/branchguard_test.go`, and that file still sits behind the
  scenario build tag. The rest of the bullet still holds: the `-tags=scenario` tier is
  **not run by `check-fast` or `check-short`** — re-checked in the `Makefile` 2026-07-30,
  only `test-scenario` and `check` run it.
- **4 raw `exec.Command` call sites still bypass `CommandRunner`** in `pasteinject.go` —
  `pgrep`, `ps`, `git status --porcelain=v1`, and `git diff --numstat`. The plan said 3.
  Three of the four sit beside a `…Via(runner)` twin (`hasAnyDirectChildVia`,
  `commandMatchesLiveAgentVia`, `worktreeActivityFingerprintVia`). `worktreeDiffLineCount`
  has no twin at all. The bug class that cost 5 commits is still open in the code today.
- **`bufferName` in `pasteinject.go` duplicates `tmux.BufferName`**, which already exists.
  Still true 2026-07-30: both functions are present.

**Process fix worth more than any single bug:** wire the scenario tier into a gate that can
block a merge. **Half of this landed 2026-07-29.** `continue-on-error` came out of every
GitHub workflow, so `.github/workflows/scenario.yml` now reports its own result instead of
reading green over 20 straight failures. It still blocks nothing on purpose: branch
protection requires only "check (Tier 2)". The tier cannot be made blocking yet, because
8 deterministic failures reproduce in isolation (`hk-97gcz`, both open P1, plus the
merge-path race `hk-co8g8`), and about half the tier silently skips for want of `br` and a
twin build (`hk-ynohn`). A green run there today would prove much less than it appears to.

---

## 8. Sequence

Status re-derived 2026-07-30.

1. ~~Reconcile the stranded integration branch (§4)~~ — **moot.** The ref is gone from origin.
2. ~~Harvest carry-forward facts for `tmuxsubstrate`, `codexwire`, `harness/pi`, `brcli` (§5).~~
   **Done, and the srt sandbox gate was harvested too. 89 facts.**
3. ~~Delete steps 1–5 (§3), one revertable commit each~~ — **four steps landed, 225,080 lines.
   See the outcome column in §3 for what each one actually removed.**
4. **Re-run dead-code detection.** Still outstanding, and now the priority: step 3 of the
   deletions proved only 8 of its 35 named files, so 27 candidates are unresolved.
5. Rewrite `workloop.go` and `scheduler.go` against the two-symbol contract (§6), validated by
   the acceptance tier.
6. Split `pasteinject.go` into three packages, rewritten from fakes against `CARRY-FORWARD.md`.
7. Wire the scenario tier into `check-short` (§7). **The stated reason is dead** — `hk-zobns`
   closed as invalid and the guard it named was never broken. The tier is red for other
   reasons (`hk-97gcz`, `hk-co8g8`, `hk-ynohn`), so making it blocking today would wedge
   every merge.

Nothing here should take days of planning. The deletions were mostly `git rm` validated by a
compiler.
