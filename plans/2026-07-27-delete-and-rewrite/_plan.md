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

Measured basis:

| Fact | Value |
|---|---|
| Production Go | 216,695 LOC / 902 files |
| Test Go | 487,997 LOC (was 525,123 before the specaudit deletion) |
| Test files named after a bead ID | 885 files / 255,664 LOC (49% of all test code) |
| Production files nothing calls | 35 files / 4,276 LOC (a floor, not a ceiling) |
| Normative specs already written | 25,893 LOC across 19 files |
| Last 50 commits: planning vs production code | 26,533 vs 1,808 lines (~15:1) |

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

### Delete — ~323,000 lines

| Order | Target | Lines | Why |
|---|---|---|---|
| 1 | `internal/specaudit` tagged sensors | 37,927 | 129 files behind `//go:build specaudit`, executing zero product code. Greps `specs/*.md` for headings. **Landed 2026-07-27.** |
| 2 | 885 bead-ID-named test files | 255,664 | Minus the carve-out below. |
| 3 | 35 dead production files | 4,276 | Zero production callers. |
| 4 | `internal/scenario` harness engine | ~18,000 | Not in the scenario tier; `make test-scenario` never runs it. 35 of 44 tests test the harness itself. Engine is driven only by `harmonik harness`, which nothing invokes. |

**Order matters: 2 before 3.** The bead-ID tests are life support for dead production
code. Deleting them first makes more dead code provable, so re-run detection after step 2.

### Retracted delete candidate: `internal/workflow/scenario`

Originally listed for deletion on the basis that the directory holds no non-test `.go`
files. **That reasoning was wrong** and the review caught it: "package contains no
production files" is not "tests do not exercise production code." All 20 files import
`internal/core`, `internal/workflow`, and `internal/workflow/dot`, and drive the real
traversal engine (`LoadDotWorkflow`, `DecideNextNode`) against the shipped
`specs/examples/*.dot` corpus. Deleting them would drop fixture coverage from **26 of 28
`.dot` examples to 6**, orphaning normative artifacts including
`plan-to-shipped-faithful.dot`, with no corpus-wide validator to backstop them. **Keep.**

Generalize the lesson: a `prod=0 LOC` count for a directory is not evidence a test is
worthless. The only sound test is what the test file imports and calls.

### Keep

| Target | Size | Why |
|---|---|---|
| `test/scenario/` | 6 files, 2,093 LOC | 11/11 green in 27s, black-box, cheapest real coverage in the repo. |
| `internal/daemon/scenario_*` (26 `//go:build scenario` files) | 54 test funcs | The executable conformance suite for the specs the rewrite is driven from. Cites `EM-012a` ×26, `EM-015d` ×24, `PL-002` ×13. |
| `internal/scenario` queue tests | 9 files, ~4.8k LOC | Real queue tests that do not use the harness engine. Move them somewhere honest. |
| `specs/` | 25,893 LOC | The rewrite oracle. |

**Carve-out to the bead-ID rule:** the 26 `//go:build scenario` files in `internal/daemon`
are bead-named but are the acceptance tier. They survive. Everything else bead-named goes.

---

## 4. Is the code-health audit still valid?

**Mostly no.** State as of today: **77 of 92 tasks still in triage, 13 complete, 5 deferred**
— roughly 14% executed. And 47 of 92 task cards (94 files minus README and TEMPLATE) are written in extract / decompose /
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

**All five subsystems are now harvested: 83 external facts recorded in**
[`CARRY-FORWARD.md`](CARRY-FORWARD.md)**, against 214 structure-caused bugs discarded — ~72%.**
That repo-wide ratio is far more favourable to a rewrite than pasteinject's 1:1, which is the worst
case precisely because it sits on the TUI boundary.
**That file is the highest-value artifact of this effort.** It should graduate to `specs/`
once stable. Any rewrite of an agent-substrate subsystem must satisfy every fact in it.

Harvesting is **complete** for `pasteinject`, `tmuxsubstrate`, `codexwire`/`codexdriver`,
`harness/pi` and `brcli`. Do the same for any further IO-boundary subsystem before deleting its
tests. Not for interior logic.

---

## 6. Rewrite order

Driven by churn against a median of **3 commits per production file** in `internal/daemon`:

| Target | LOC | Commits | Note |
|---|---|---|---|
| `workloop.go` | 6,656 | **370** | `beadRunOne` is 2,289 lines (peak 2,394, born at 119); `runWorkLoop` 1,670. 60% of the file in two functions. |
| `daemon.go` | — | 175 | |
| `reviewloop.go` | 2,194 | 116 | |
| `tmuxsubstrate.go` | 3,023 | 67 | |
| `pasteinject.go` | 2,691 | 56 | Three subsystems in one filename; paste injection is ~20% of it. |

**Start with `workloop.go`.** It has 6.6× the churn of pasteinject and the rewrite contract
is already known.

### The rewrite contract for workloop

No scenario test touches `beadRunOne`. The acceptance tier enters one level up, through
`ExportedRunWorkLoop` (9 files) and `ExportedWorkLoopDeps` (15 files). So preserve exactly
two things:

- `runWorkLoop(ctx, deps) error`
- the `workLoopDeps` construction surface

and everything below can be burned while the entire acceptance suite validates the
replacement.

**The actual blockers are 4 unit test files** binding `beadRunOne`'s 7-parameter signature:
`pi_unknown_profile_refuse_test.go`, `pi_provider_selected_hk8ziid2_test.go`,
`workloop_gate_n5md3_test.go`, `hk3hozm_slot_leak_test.go`. All four are bead-named and
die in step 3 anyway.

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
existing tests, and it is the correct trade.

---

## 7. Known-live defects (do not lose these)

- **Branch-protection deep guard fails open** (`hk-zobns`, P0). A bead merges to a protected
  target and closes `approved`; the ref actually moves. Repro:
  `go test -tags=scenario -run TestBranchGuard_FailClosed_MergeGuardBackstop ./internal/daemon/`.
  Caught only by the `-tags=scenario` tier, which **`check-fast` and `check-short` do not run**.
- **3 raw `exec.Command` calls still bypass `CommandRunner`** in `pasteinject.go` (`pgrep`,
  `ps`, `git`), each sitting beside its own `…Via(runner)` twin. The bug class that cost 5
  commits is still open in the code today.
- **`bufferName` in `pasteinject.go` duplicates `tmux.BufferName`**, which already exists.

**Process fix worth more than any single bug:** wire the scenario tier into a gate that can
block a merge. Today a working acceptance suite blocks nothing.

---

## 8. Sequence

1. Reconcile the stranded integration branch (§4) — otherwise everything below lands on a
   branch that does not contain what you think it does.
2. Harvest carry-forward facts for `tmuxsubstrate`, `codexwire`, `harness/pi`, `brcli` (§5).
3. Delete steps 1–5 (§3), one revertable commit each, `go build` + acceptance tier between each.
4. Re-run dead-code detection after step 3 of the deletions; delete the newly-provable set.
5. Rewrite `workloop.go` against the two-symbol contract (§6), validated by the acceptance tier.
6. Split `pasteinject.go` into three packages, rewritten from fakes against `CARRY-FORWARD.md`.
7. Wire the scenario tier into `check-short` (§7). It goes red until `hk-zobns` is fixed —
   that is the point.

Steps 2 and 3 are independent and can run in parallel. Nothing here should take days of
planning; it is mostly `git rm` validated by a compiler.
