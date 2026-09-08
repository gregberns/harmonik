# Restructure Execution Plan — Adversarial Review

**VERDICT: APPROVE-WITH-CHANGES.** The approach is sound and the value story is honest (the
integration-test caveat is stated four times and it is correct). But the admission bar — the one thing
the whole program rests on — has two mechanical holes that let a port slip the wall on day one, the
enabling move (Port 1 vocabulary) has no task that actually lands it, and the "unresolved hard blocker"
D2 is restated as a policy rather than resolved by an experiment. Close the four must-fix items below
before task 1; the rest can be fixed during. This is not a rubber stamp: I attacked the bar and it
leaks.

All findings are checked against the repo at current `main` (go.mod is `go 1.25`, confirmed; the
execution plan's correction of the base plan's `go 1.26` diagram is right — the base plan artifact is
wrong).

---

## MUST-FIX before starting

### 1. The wall check is blind to four legacy prefixes — the "teeth" leak
Execution plan §Phase 0 step 3 (and base plan §4B) writes the wall grep as
`grep -E 'gregberns/harmonik/(internal|cmd)'`. The legacy module has Go packages under **four more
top-level prefixes**, verified with `go list ./...`:

```
   8 cmd
  13 evaltasks   <- missed
  83 internal
   1 scripts     <- missed
   2 test        <- missed
   4 tools        <- missed
```

A clean package that imports `harmonik/tools/forbid-import`, `harmonik/evaltasks/...`,
`harmonik/test/...`, or `harmonik/scripts/...` sails straight through the gate — simulated and
confirmed MISSED. The seed playbook's original `grep 'harmonik/legacy'` was airtight *because of the
rename*; the plan deliberately declines the rename (§Phase 0 step 1, correctly, for import-churn
reasons) but then keeps a wall check that only works under the rename it rejected. **This is the
central invariant of the entire program and it is porous.**

Worse: the Phase-0 proof-of-teeth (task 8) plants an `import ".../internal/core"` — an import the
buggy grep *does* catch. The proof is calibrated to the gate's blind spot, so it "proves" a wall that
has holes.

**Fix:** make the check an allow-list, not a deny-list. Any dependency in `cd clean && go list -deps
./...` whose path starts `github.com/gregberns/harmonik/` but **not** `.../clean/` is a breach. That is
robust to new legacy top-level dirs; the current enumerate-the-bad-prefixes form is not. Change task 8
to plant a `harmonik/tools/...` import too, so the proof exercises the general case.

### 2. Purity hard-gate 3 falsely blocks Port 0 — the first port trips the bar
Hard gate 3 (base plan §2, execution plan §Phase 2 admission checklist) greps moved non-test files for
`time.Now()` and blocks on any hit. **Port 0 is `substrate`, and `internal/substrate/clock.go` holds
`func (SystemClock) Now() time.Time { return time.Now() }`** — the one real clock the whole design is
built around (PRINCIPLES §1 names `substrate.ClockPort` as *the* injection point). So the very first
port trips the mechanical gate. Either a reviewer waves gate 3 through on judgment — which dents the
plan's load-bearing claim that the bar is "mechanical, a grep anyone can re-run, hard to game" — or Port
0 cannot land.

**Fix:** gate 3 must grep the pure-core files and **exempt the designated adapter/port shell** (the
`substrate` clock adapter, and each package's named `main`-wired effect shell). State the exemption
explicitly so it is a rule, not a per-review wave-through. Without it, gate 3 is inconsistent the first
time it runs.

### 3. D2 is restated, not resolved — the base plan's one unresolved hard blocker survives
Base plan §8 risk 2 calls the legacy lint allow-list surviving a `git mv` "the hard blocker and it is
unresolved." The execution plan's DECISION D2 (§Kill/stop criteria) offers a *policy* — "freeze the
legacy allow list, remove entries as files leave, never add" — but **no experiment that proves the
allow-list actually keeps suppressing after a file moves out of its package.** The root `.golangci.yml`
is 1,265 lines with ~87 exclude/allow constructs and a documented depguard component matrix keyed to
package paths; whether a `git mv` leaves a neighbor's suppression intact or makes new findings fire is a
mechanical fact nobody has measured. A policy is not a proof, and the plan bets the whole spine on it.

**Fix:** add a Phase-0 (or pre-task-16) spike: move one daemon-adjacent file out, run `make full`,
confirm (a) the moved file lints clean under `clean/.golangci.yml` and (b) **no new finding fires in the
files left behind**. Only then is D2 resolved. Until that spike is green, the first daemon-adjacent port
is blocked, exactly as the base plan warned.

### 4. No task lands the Port-1 vocabulary as production code
Phase 1 is explicitly a **throwaway spike** ("in a throwaway branch", §Phase 1 What to try). Yet the
GREEN branch says "commit the spike result as the real `clean/` vocabulary and proceed to Phase 2." You
cannot ship a throwaway, and the spike measured *drag* — it did not necessarily pass the empty-lint or
purity gates as production. The task table jumps from task 12 (spike report + D1) straight to task 14
(port `structuredlog`), which **imports a vocabulary that no task creates**. The single most important
structural artifact in the program (base plan §3: "the single most important structural decision") has
no task, no size, no review gate, and no admission-bar pass of its own.

**Fix:** insert a task 12.5 — "build the clean vocabulary + the legacy↔clean adapter as reviewed
production code; pass all three hard gates." Size it honestly (likely M–L, not folded into the spike),
and make task 14 depend on it.

---

## FIX DURING

### 5. Keeper Track B silently omits the `presence` tentacle
Verified: `go list -f Imports ./internal/keeper` returns **core, dashboard, digest, presence,
substrate**. Track B steps 1–5 handle digest, dashboard, `time.Now`, and core — but **never
`presence`**, and `presence` pulls `eventbus` pulls `core`. So after every listed Track-B cut, moving
keeper into `clean/keeper` (step 5, task 23) still breaches the wall through `presence` until spine
Ports 3 (`eventbus`) and 5a (`presence`) land. The Track B gate ("after steps 1–2, the digest and
dashboard closures are gone") is not sufficient for step 5, and task 23's deps (21, 22) never mention
presence. The plan's promise that keeper couples to Track A *only* through the vocabulary (step 4) is
wrong: step 5 also waits on the spine reaching presence. **Fix:** add an explicit
invert-or-port-presence step for keeper and correct task 23's dependencies.

### 6. The task DAG self-serializes independent work — costly given the #1 risk is throughput
Two artificial edges, both against a fleet that closed 7 beads in 5 weeks (base plan §8 risk 3, the
plan's own top execution risk):
- **Task 13** (extract pure `decide` from `Watcher.Run`, in-place in legacy) depends on task 9 (the
  Phase-0 gate) but needs **nothing** from the clean module — it is pure legacy refactoring. The plan
  itself calls this "the highest-value first move" and "reversible", then gates it behind an afternoon
  of scaffolding it never touches.
- **Tasks 14→15→16** serialize `structuredlog`→`eventbus`→`queue`, but each package's only internal
  tentacle is `core` (verified) — there is no import edge between them. The serialization is purely
  conservative and forgoes parallelism the base plan's own measurements support.

**Fix:** unblock task 13 to start at once (depend on nothing, or on the keeper mission brief only); mark
14/15/16 as parallelizable once the vocabulary (12.5) exists.

### 7. "Green in seconds" is a gameable exit criterion
The three HARD gates are wall + empty-lint + purity. Hermetic test coverage is only a **soft** signal
(base plan §2 signal 5). So a port can pass all three hard gates while its real behavior is proven only
by quarantined `//go:build integration` tests and a thin-or-absent hermetic layer — and the exit
criterion "`cd clean && go test ./PKG/...` green in seconds" is then satisfied *vacuously* (PRINCIPLES
§7: a test that cannot fail looks exactly like one that passes). `internal/queue` has 45 test files, of
which only 1 carries a build tag today — evidence the fast loop's "seconds" may come from excluding the
tests that actually prove queue, not from queue being hermetically covered. **Fix:** promote "a
hermetic unit layer that covers the ported behavior, watched-to-fail" from soft signal to a hard exit
gate for each port.

### 8. Task 16 bundles a relocation with a concurrency-semantics change
Task 16 ("port `queue`") is sized L and folds in "fix the two-writer `queue.json` hazard on the way."
That violates the plan's own small-reversible-commit discipline (Phase 0 is proud of moving "zero
production code"; here one L task both relocates a package and changes its concurrency behavior) and
muddies review attribution — a failure could be the move or the fix. **Fix:** port `queue` as-is behind
the shim (carry the hazard), then fix the two-writer hazard in place in clean as a separate reviewed
bead.

---

## Minor / note
- The `//go:build integration` tag is **already used in 20 files** (cmd/harmonik, tools/commentcut,
  test/integration). Phase-0 step 7 treats the convention as greenfield; it must instead reconcile with
  existing usage and any Makefile wiring already keyed to it.
- Base plan §4 diagram states `go.work # go 1.26`; the module is `go 1.25`. The execution plan corrects
  this — leave a one-line note that the base plan artifact carries the stale number.

---

## The 3 things most likely to make this plan fail in practice
1. **Port 1 balloons AND has no landing task.** The plan's own #1 risk (a minimal vocabulary that is
   not minimal) meets a task table with no production-vocab task even on GREEN — so a good spike result
   still has nowhere to go, and a RED result has a real off-ramp (D1) but a GREEN one does not. Fix #4.
2. **The wall and purity gates leak on day one.** The grep is blind to four legacy prefixes (finding 1)
   and the purity grep blocks the first port (finding 2). The bar is the entire value of the program;
   two holes at the start teach the crew the bar is negotiable.
3. **Throughput the fleet has never shown, spent on a self-serialized DAG.** 7 beads in 5 weeks is the
   measured reality (risk 3). The plan gates its cheapest de-risk (task 13) behind unrelated
   scaffolding and serializes three independent ports (findings 6), spending the scarcest resource on
   sequencing that the import graph does not require.
