---
id: test-mass-cost-measure
title: Measure whether the test mass still stops us changing code
type: task
priority: 1
labels: [test-quality, clear-the-ground]
depends_on: []
blocks: []
workstream: W5
batch: 5
---

## Problem

The original diagnosis was that the suite pinned internal function signatures rather than
behaviour, so any restructuring broke hundreds of tests that were never protecting anything. The
operator's stated concern, 2026-08-23: if that is still true, this program fails, because every
workstream in it is a restructuring.

Nobody has re-measured since the deletion. **The line count cannot answer the question** — a large
suite that tests behaviour is fine, and a small one that pins signatures is not. An earlier revision
of this file led with a ratio of test lines to production lines. That number is the one that has
already failed to answer this question once. Do not open the report with it and do not treat it as
a result.

## What the question actually is

A restructuring edit is behaviour-preserving by construction. So "which broken tests were protecting
behaviour?" has a trivial answer — none — and an earlier revision of this file asked for exactly
that number. That is the defect being repaired here.

The answerable question is **what a behaviour-preserving restructure costs**:

1. How many test files must be touched at all.
2. Of those, how many can be repaired **mechanically** — a name, a type, an argument list, an
   import path — versus how many need **re-authoring**: a changed expectation, a new fake, a
   rewritten setup, or deletion.

(2) is the deliverable. Mechanical breakage is a tax and it is payable. Re-authoring is what makes
a restructuring program impossible, and it is the number the operator asked for.

## Scope

Read-only with respect to this checkout. Every trial edit happens in a throwaway worktree.

### Set up

Run from the repo root. Do not `cd` into the worktree; reach it with `git -C`.

    BASE=$(git rev-parse HEAD)
    WT=$(mktemp -d)/test-mass
    git worktree add --detach "$WT" "$BASE"

Record `$BASE` in the report. Every number below is measured against that one commit, so a
re-run can reproduce them. Remove the worktree at the end:

    git worktree remove --force "$WT"

The worktree is what makes this safe to run beside anything else. `internal/daemon` and
`internal/core` are under active extraction by other agents; because no trial edit touches the
shared checkout, that does not constrain this task and no coordination is needed.

### The three trial edits

Perform each edit **alone**, measure it, then restore the worktree with
`git -C "$WT" checkout -- .` and `git -C "$WT" clean -fd` before the next one. Do not combine them.
Each is behaviour-preserving: it changes a name or a shape and no logic.

**Edit A — rename an exported type in `internal/core`.**
In non-test `.go` files only, rename the exported type `CoarseStatus` to `CoarseStatusRenamed`.
Whole-word occurrences only (`\bCoarseStatus\b`). This deliberately does **not** touch the constants
`CoarseStatusOpen`, `CoarseStatusClosed` and their siblings, nor `terminalCoarseStatuses` — none is
a whole-word match. Defined in `internal/core/coarsestatus.go`.

**Edit B — add an unused parameter in `cmd/harmonik`.**
Add a trailing `_ bool` parameter to the exported function `ResolveKeeperConfig` in
`cmd/harmonik/resolve_keeper_config.go`, and pass `false` at its single non-test call site in
`cmd/harmonik/keeper_cmd.go`. The parameter is blank-named, so nothing can read it. Leave the
existing `//nolint:` directive on the declaration alone.

**Edit C — move a file into a new package.**
Move `internal/lifecycle/daemonpaths.go` to a new package `internal/daemonpaths`, changing only the
package clause. In non-test files only, qualify its 18 exported functions at their call sites
(`lifecycle.HarmonikDir(` becomes `daemonpaths.HarmonikDir(`, and so on for `PidfilePath`,
`SocketPath`, `InstanceIDPath`, `UpgradingMarkerPath`, `StateMarkerPath`, `EventIDHWMPath`,
`EventsDir`, `BeadsIntentsDir`, `BeadsOwnedDir`, `ReconciliationLocksDir`, `ReconciliationLockPath`,
`SpillFilePath`, `ReconciliationDir`, `InvestigatorEvidenceDir`, `WIPCaptureDir`,
`ReconciliationAttemptsDir`, `ReconciliationAttemptPath`), and qualify the in-package callers in
`internal/lifecycle` that become cross-package. The file imports only `fmt` and `path/filepath` and
its unexported constants are referenced nowhere else, so it moves without renames and creates no
import cycle.

`.golangci.yml` carries a per-package `depguard` matrix and the new package has no entry, so
`make lint` will complain about Edit C. **That is expected and is not part of the measurement.** Do
not add the entry and do not "fix" it — `go build` and `go test` are unaffected.

### Measure, per edit

**Step 1 — which test files break.** Compile the test binaries without running them:

    git -C "$WT" stash list >/dev/null
    go vet ./... 2>&1 | tee /tmp/vet-<edit>.txt
    go test -count=1 -run XXX_NONE ./... 2>&1 | tee /tmp/compile-<edit>.txt

`-run XXX_NONE` matches no test, so a package that reports anything other than `ok` or
`no test files` failed to **compile**. Record the list of failing packages and the count of distinct
`_test.go` files named in the output.

**Step 2 — run what still compiles.** For packages that compiled, run them and record failures and
wall-clock:

    go test -count=1 ./... 2>&1 | tee /tmp/run-<edit>.txt

Read the exit code from the file, not from the screen, and read it as `$pipestatus[1]` — in zsh
`$PIPESTATUS` is not a thing and the bash spelling evaluates to empty, which reads as success.

**Step 3 — classify every broken test file** by this procedure, in order. The first rule that
applies decides it. Apply it per **file**, not per assertion.

1. **The file failed to compile** → **mechanical**. It named a symbol that changed shape. No
   judgment is involved; the compiler decided.
2. **The file compiled and a test in it failed.** Repair it by editing **only that `_test.go` file**,
   changing only names, types, argument lists and import paths. Then:
   - the file passes, and the repair diff changed no expected value, no comparison and no assertion
     count → **mechanical**;
   - the file passes, but the repair required changing an expected value, adding or rewriting a
     fake, or rewriting setup → **re-authoring**;
   - no test-only repair makes it pass → **behaviour catch**.

**Rule 3's third branch must come out zero.** All three edits are behaviour-preserving, so a
genuine behaviour catch means the trial edit was performed wrongly. If any file lands there, the
run is invalid: say so in the report, fix the edit, and measure again. This is the check that makes
the experiment falsifiable rather than merely producible.

Do not repair a file you have already classified under rule 1 — the compiler already answered.

## Done when

A document exists at
**`plans/2026-08-23-clear-the-ground/test-mass-cost-measure-findings.md`**, in the same shape as the
existing `cli-structure-assessment-findings.md`, and it carries:

1. The `$BASE` commit every number was measured against, and the date.
2. A table with one row per trial edit and these columns: test files that failed to compile; test
   files that compiled but failed; **mechanical**; **re-authoring**; **behaviour catch**; wall-clock
   seconds for step 2.
3. **The headline number: across all three edits, of the test files that broke, what fraction needed
   re-authoring rather than a mechanical repair.** One fraction, stated as a fraction, in the first
   paragraph.
4. Where the re-authoring cases are concentrated — by package, named.
5. An explicit line reporting the behaviour-catch total. If it is not zero, the report says the run
   is invalid and why.
6. The exact commands run, so the measurement can be reproduced against `$BASE`.

## Limits

- **Report before proposing.** No deletion campaign, no target, and no plan for the suite comes out
  of this task. The measurement first.
- **Do not delete a test in this task**, in the worktree or anywhere else. The last sweep took ten
  behavioural tests with it and they had to be restored (`11c058b95`). That is the exact failure
  this task exists to avoid repeating.
- **The only file this task writes in the real checkout is the findings document.** Every trial edit
  lives and dies in the throwaway worktree. Remove the worktree before you finish.
- Do not use whole-suite line count as an answer, and do not lead the report with it.
- Do not add the `depguard` entry for the new package in Edit C, and do not chase `make lint`.
- **Take your own measurement of any count quoted here.** The three edits' fan-out was measured on
  2026-08-25 and this tree moves daily.
