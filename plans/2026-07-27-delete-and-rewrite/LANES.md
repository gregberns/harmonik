# Lanes — two agents, and the line between them

**Date:** 2026-08-01. **Status:** alpha running, bravo staffed and idle, charlie retired.
**Read first:** [`CHARTER.md`](CHARTER.md) for what the program is.
[`DECOMPOSITION-MAP.md`](DECOMPOSITION-MAP.md) holds the ordered steps this file assigns.

This file says which work runs beside the core lane without breaking it, and in what order. It is not
a new plan. It is an ownership table over the plan that exists. The graph research in
[`../2026-08-01-graph-guided-decomposition/README.md`](../2026-08-01-graph-guided-decomposition/README.md)
adds the contract-first waves below.

**Two lanes are staffed, and no third will be staffed.** Alpha owns daemon contracts and final
composition. Bravo is the only parallel lane. It owns the package work that a stable alpha contract
makes independent. A lane named `charlie` existed for one evening and is retired. Its finished work
is merged. Its unfinished charter is bravo's.

**But three more branches sit in the lane namespace, and one of them is a live collision risk.**
Measured 2026-08-01: `work/cq-mig-01` (44 commits ahead, 256 behind, last commit 2026-07-26),
`work/arch-01-contract` and `work/cq-01` (1 commit each, about 210 behind, last commits 2026-07-27).
All three predate this program. All three have worktrees under `/Users/gb/github/harmonik-wt/` and
dirty trees. `work/cq-mig-01` touches 93 files, including 15 in `internal/daemon`, 8 in
`internal/core` — among them `transition.go`, the exact seam bravo's item 7 targets — and eleven
specs, including both `specs/run-state-machine.md` and `specs/execution-model.md`, which are alpha's.

Neither lane may adopt or rebase them. **Reconciling or deleting them is an operator decision**, and
it is listed in §8. Until then, treat `internal/core/transition.go` as contested and say so in any
commit that touches it.

The daemon is down, so this is a hand-run version of what harmonik does for itself. One agent per
lane, each with its own `HANDOFF-<lane>.md` and its own worktree.

---

## 1. The fact that decides the split

`internal/daemon` is ONE Go package. Measured 2026-08-01 at `462679b68`: **96 production files,
43,380 production lines, 204 test files.** Two agents in different files of that package still share
one compile unit, one `go test ./internal/daemon/` run of about 930 seconds, and the 14 freeze-gate
scripts that `check-fast` and `check-short` both run. A half-finished edit there makes EVERY lane red,
not only its own.

So the split is decided by the package boundary, not by the dependency arrows in the step list.
**A lane must own whole packages, and only one lane may hold `internal/daemon`.**

> ⚠ Two counts in the previous version of this file did not reproduce and are corrected above.
> "104 production files" counted the three subpackages (`bootconfig`, `router`, `scenariotest`),
> which are separate compile units and are not in the 930-second run. "193 test files" was stale by
> eleven. The direction of the error was to make the package look larger than the thing the rule
> protects.

**`internal/core` is a second package of that kind, and this file used to miss it.** 237 production
files, about 31,000 lines, one compile unit, and **58 of 103 packages depend on it**. Treat it the
same way. It is bravo's, and alpha does not edit it.

---

## 2. The lanes

### Lane `alpha` — the core

**Main checkout `/Users/gb/github/harmonik`, branch `phase1-session-restart-substrate`.** This is the
shared branch. Alpha commits here directly and merges the other lane into it.

**Owns** `internal/daemon/**`, `internal/runlease/**`, `internal/runloop/**`,
`internal/transport/tunnel/**`, `internal/harness/shared/**`, `specs/run-state-machine.md`,
`specs/execution-model.md`, and `DECOMPOSITION-MAP.md`. Alpha also owns each extraction contract,
the daemon construction change, and deletion of the old daemon adapter.

**Works** step 7 piece 1 and the capability ports, then step 8, then steps 10 and 12. See §4 for why
that order is not the order the map states.

### Lane `bravo` — the queue's writer, and the duplication outside the core

**Worktree `/Users/gb/github/harmonik-wt/bravo`, branch `work/bravo`.** The initial step 9 work landed
and merged. Its acceptance condition remains open. Item 1 below closes that condition.

**Charter:** everything the "one writer for the queue" idea can honestly reach from outside
`internal/daemon`, plus the consolidation the map's own measurements already placed outside alpha's
package.

**Owns** `internal/queue/**` (including `cli/`), `internal/queuewiring/**`, `internal/lifecycle/**`
(including `tmux/`), `internal/core/**`, `internal/replay/**`, `internal/projectconfig/**`,
`internal/runmerge/**`, `internal/eventbus/**`, `internal/hookrelay/**`, `cmd/harmonik/**`, and —
carried over from step 9 — `scripts/scratch-daemon.sh`, `scripts/core-loop-matrix.sh`,
`.github/workflows/scenario.yml`, `test/twins/**`, `test/scenario/**`,
`docs/scratch-daemon-runbook.md`, and the twin and `test-scenario` recipes in `Makefile`.

**Owns after an alpha contract lands** `internal/substrate/tmuxhost/**`,
`internal/transport/localsocket/**`, `internal/notify/**`, `internal/comms/cursor/**`, and the new
comms and decisions handler package. Bravo creates each new package and moves its focused tests.
Bravo keeps its public contract narrow. Alpha owns the final daemon cutover. These paths are reserved
now. They are not work that can start early.

**Owns the command construction changes** in `cmd/harmonik/main.go` and `cmd/harmonik/run.go` for the
tmux and socket waves. Alpha owns the daemon adapter cutover. Alpha alone creates each neutral
contract package before it hands the package to bravo.

Measured 2026-08-01, that is **404 production files and 90,457 production lines across 14 packages**,
against alpha's 96 files and 43,380 lines in one. **Bravo is the larger lane by volume.**

**Say plainly what that costs.** Bravo holds two large compile units, not none: `internal/core` is
237 production and 207 test files in a single package — **444 files, more concentrated than
`internal/daemon`'s 272** — and `cmd/harmonik` is another 81. §1's rule applies to `internal/core`
exactly as written, and nothing in this section softens it. The split is worth its cost because the
gate is cheap and the packages are independent of each other, not because bravo's units are small.

**The velocity argument for loading it heavily.** Measured 2026-08-01, bravo's whole package set runs
`go test -short -count=1` in **38 seconds**. Alpha's one package takes **145 seconds** in the same
short mode and about **930** in full. Bravo can run its entire gate about four times per alpha short
run and about twenty-four times per full one. Work that can go to bravo should go to bravo.

### The keeper park is LIFTED for one slice — operator directive, 2026-08-01

The "Deferred post-core keeper and harness-lifecycle lane" section later in this file parks
`internal/keeper` as research. **The operator wrote that park and has lifted it**, on the ground that
the research will not be finished for some time and should not block work that does not depend on it.
Their instruction: work what is ready, or **separate the logic so it can be worked on independently
and refined further.**

**The park still holds for anything the four open questions in `research/README.md` touch.** What
durable record owns a crew's work state, whether the Codex Stop-hook vertical fits the normal crew
launcher, **which responsibility leaves `Watcher.Run` first**, and the public event vocabulary — all
still the operator's, all still unanswered.

**Bravo takes the slice that needs no answer**, in this order:

1. **Restore the five lost corpus scenarios** (`hk-hs5a3`). Pure recovery of coverage the mass
   deletion took, recoverable verbatim from `ec66da798^`, and it is precisely the research's own
   "preserve one Claude vertical" step. Done condition: the conformance gate reaches 15 of 15 and the
   Makefile line stops reporting five gaps.
2. **Characterize `Watcher.Run` and move nothing.** The research says the outer watcher "still joins
   too many jobs". Pinning each job at its current seam is what makes a later extraction safe, and it
   is the evidence the operator needs to answer open question 3. **Extraction is not authorized. A
   clean-looking seam is a recommendation, not a licence.**

**Two prohibitions carried straight from the research, and they bind this slice too.** Do not build
the framework before both verticals exist. Do not answer an open question by adding harness branches
to the current watcher.

**Already done, and it was the first repair the research named:** `make test-keeper-conformance` was
false green — it reported success while running zero tests in `internal/keeper`, because `ec66da798`
deleted the two corpus registration files and `go test` exits 0 on an empty `-run` match. Repaired at
`a178b2e26`.

### `charlie` — retired 2026-08-01

Three lanes were staffed and the table recorded only two. Charlie's two commits were finished,
reviewed and gate-green, and sat unmerged in its worktree for a day. **Nothing in `git status` shows
an unmerged branch**, so the work was invisible from the main checkout. It is merged at `3982b9179`.

**The lesson is a rule, and it is in §5.** After this, delete the branch and the worktree.

---

## 3. Bravo's work, in order

> **STATUS 2026-08-01 — ITEMS 1 TO 7 ARE DONE AND MERGED. This list is history, not a queue.**
> Item 7, the queue status-transition API, merged at `8d275e9f4` in five commits: `6eddb4914`,
> `c91173fe4`, `ba56996d5`, `6fac57083`, `724c3ed5b`. `go build`, `go vet` and the full
> `internal/daemon` short suite are green at that tip, which is the §5 import-graph check and not
> optional after a bravo merge.
>
> **`724c3ed5b` also discharges decomposition step 19's last site.** `core.TerminalCoarseStatuses()`
> is now the single terminal-status source, `IsTerminal` derives from it, and
> `internal/lifecycle/activerun_em031a.go` queries it instead of repeating the string literals. That
> closes `hk-terminal-status-literals-aynz5`, the hand-off recorded below under "What alpha hands to
> bravo".
>
> **Two things came in WITH that merge and are not finished.** (1) `scripts/queue-status-writer-ratchet.sh`
> passes but **is referenced nowhere in the `Makefile`**, so it runs in no check tier — a ratchet
> nobody runs is indistinguishable from one that passes, which is item 8's own standing rule turned on
> itself. (2) Three of the five commits carry `"verdict":"PASS"` from a Codex reviewer, and schema v1
> defines only `APPROVE`, `REQUEST_CHANGES` and `BLOCK`. Filed as `hk-wq7hm`; the commits are genuinely
> reviewed and do not need rewriting.

Zero-design correctness first. Mechanical consolidation second. Design last. Every item below was
measured at `462679b68`, and **no item edits a file in `internal/daemon`.** Item 1 reads one test file
there and runs that package's scenario bundle. That is a read and a test run, not an edit, and §5's
rule on sharing the machine applies to it.

**Already landed by charlie and merged — do not redo.** The two hand-rolled event envelopes in
`internal/queue/cli/cancel.go` and `cmd/harmonik/handler.go` now write a real `core.Event`.
`IsTerminal()` exists on `core.CoarseStatus` and the four reachable sites use it.

1. **Close out step 9's own done condition.** It was never met. `make build-all` does not produce
   `twin-fail` or `twin-hang`, and `build-twin-claude` is still an alias to the generic recipe with a
   live TODO. Sources are `test/twins/fail-immediately` and `test/twins/hang`, and
   `internal/daemon/t2_scenarios_test.go` skips on their absence at **six** sites. Then record the
   full daemon scenario bundle's skip count, which step 9 measured only for the top-level package.
   **Announce that run to alpha first** — it is alpha's package and the box is shared.
2. **One `merge-base --is-ancestor` contract.** Four wrappers that genuinely disagree, in
   `internal/runmerge/merge.go`, `internal/lifecycle/branchtip_em024a.go`,
   `internal/lifecycle/orphansweepbeads.go` and `cmd/harmonik/version_verify.go`. Zero daemon files.
   It carries one real decision: what a git failure means. Today `orphansweepbeads.go` conflates a git
   failure with "not an ancestor".
3. **The `internal/runmerge` shadow payloads.** `internal/runmerge/events.go` declares
   `workingTreeRefreshFailedPayload` and `mergeBuildFailedPayload`, which are exact wire-shape
   duplicates of registered `core` types. Delete them and marshal the core types. The output is
   byte-identical, so no spec amendment is needed. About 20 lines.
4. **The uncancellable sleep in `internal/hookrelay`.** `sendToSocket` retries under a bare
   `time.Sleep`, and `wallMax` is checked only **before** each sleep and bounds neither the dial nor
   the read, so the worst case is about 35 seconds and not the 25 the constant suggests. **Scope it
   honestly:** `sendToSocket` is reached only from `hookrelay.Run`, which is the `harmonik hook-relay`
   CLI subcommand — a short-lived hook-client process, not the daemon's shutdown path. So this is an
   unbounded wait in a hook client, not a shutdown stall. About 10 lines, and it is the one piece of
   "time as a port" that is free today.
5. **Keep `eventbus.RunDrainer` for now.** The probe misses `busImpl.DrainRun` behavior and its three
   focused tests. Delete it only with an explicit behavior disposition and replacement or removed tests.
6. **Add `--model` to the two captain argv builders** in `cmd/harmonik/captain.go` and
   `cmd/harmonik/captain_respawn.go`. About 24 lines. **Do not attempt the four-way argv
   unification** — the freeze gates forbid sibling harness imports, so its only legal home is
   `internal/harness/shared/`, which alpha holds. `CHARTER.md` §4 says a preserved divergence needs a
   written reason, a test that fails when someone tidies it away, and a named trigger. The reason is
   above. **The test is part of this item**: assert that each builder emits `--model`, so a later
   unification cannot quietly drop it again. The trigger is alpha finishing with
   `internal/harness/shared/`.
7. **Then the design work: the queue's status-transition API.** This is the largest real unit and the
   reason the lane exists. Give `internal/queue` the transition surface and convert the 16 sites
   outside `internal/daemon`. `AdvanceGroup` in `internal/queue/state.go` and its terminal helpers are
   the seed. The kerf work `queue-status-writer` is open at problem-space and already scopes this
   slice.
8. **Standing, throughout: run the live pass on each tip alpha lands, and keep the artifact.** Step 9
   built this oracle. An oracle nobody runs is indistinguishable from one that passes.

### The honest done condition for item 7, and why it is not "one writer"

**There are 30 queue-status assignment sites, not the 27 the map states.** Fourteen are inside
`internal/daemon` — nine in `scheduler.go`, three in `scheduler_reservation.go`, two in
`perqueuespendmeter_tigaf11.go`. Sixteen are outside, in six files across three packages. The map's
"about 13 outside" is wrong and it omits `internal/queue/persistence.go` from its file list
altogether.

**And assignment is not the whole surface.** Composite-literal initialization of the same three fields
adds **18 more non-test sites**, including `cmd/harmonik/run.go` and `cmd/harmonik/run_via_daemon.go`
— a fourth package that the "six files across three packages" framing leaves out. **The true surface
is 48, not 30.** A transition API that admits only the 30 assignments does not describe how a queue
item gets its first status.

`Item.Status`, `Group.Status` and `Queue.Status` are exported and JSON-tagged. Unexporting them is the
only compiler-enforceable move and it breaks alpha's build on the spot. So bravo's API is **additive
only**.

**Most of the construction sites are reachable too, and the done condition must cover them.** They sit
mainly in `cmd/harmonik` and `internal/queue`, which are both bravo's, so the reachable share is much
larger than the 16-of-30 the assignment count alone suggests. **The exact split of the 18 construction
sites is the first thing item 7 measures**, before any API is designed — two passes over this program
have now produced different counts of this same surface, and neither was measured with construction in
scope. Do not carry a number from this file into the design. Re-derive it.

**Then state the denominator you actually measured, and state both halves.** Never say "one writer".
Claiming the whole thing on a fraction of the sites is exactly the scope-qualifier failure
`CHARTER.md` §5 names, and picking the flattering denominator is the same failure one step smaller.

The one enforcement lever that fits is a shrink-only grep ratchet listing alpha's 14 sites. Bravo
writes the script. **Bravo does not wire it into `check-fast`** — that recipe is alpha's. Hand over
the one line.

### What bravo hands to alpha, and does not reach into

1. **`maybeEmitEpicCompleted` in `internal/daemon/workloop.go`.** It tests `Closed` only, so one
   tombstoned child permanently suppresses `epic_completed` for its parent and silently stops a lane.
   **This is the entire live behaviour fix in step 19, and it is inside alpha's file.** The four sites
   bravo can reach already agreed with each other, so the change bravo landed is a pure refactor. If
   only one item on this list is ever done, make it this one.
2. The pre-claim terminal test in `internal/daemon/scheduler.go`, which asks the same
   closed-or-tombstone question before claiming a bead. Also inside the daemon package — **both the
   map and the old version of this file said it was reachable from a second lane, and it is not.**
3. The 14 remaining queue-status write sites, plus the one line that adds the ratchet to `check-fast`.
4. `workloopRunStartedPayload` and its three siblings in `workloop.go` — the shadow that makes replay
   decode `run_started` into the wrong shape.
5. `hk-l39bq`, the merge-race scenario failure. **Its own bead text forbids fixing it from the
   acceptance-check lane.** Bravo must not take it.

### What alpha hands to bravo

1. **`hk-terminal-status-literals-aynz5` — the last undischarged site of step 19.**
   `internal/lifecycle/activerun_em031a.go` builds the terminal set as string literals,
   `[]string{"closed", "tombstone"}`, and hands it to `ListBeadsByStatus`. **Step 19 is not fully
   closed without it**, and alpha cannot reach it: `internal/lifecycle` and `internal/core` are both
   bravo's. It is not a straight swap to `IsTerminal()` — that is a predicate over one status and this
   site needs the set enumerated for a query, so it wants a `core.TerminalCoarseStatuses()` accessor
   with both derived from one list.

**Bravo must not touch** `internal/daemon/**`, `internal/runlease/**`, `internal/runloop/**`,
`internal/transport/tunnel/**`, `internal/harness/shared/**`, `specs/run-state-machine.md`,
`specs/execution-model.md`, `DECOMPOSITION-MAP.md`, the `fmt` / `check-*` / `tools` recipes in
`Makefile`, and any `scripts/*gate*.sh` it did not itself author.

### Contract-first extraction waves

Package ownership stops a compile-unit collision. It must not trap every outer subsystem inside the
daemon package. The way out is a three-part handoff. Alpha defines and tests a narrow boundary. Bravo
builds the new package against that boundary. Alpha changes construction and removes the daemon code.

The comms handler, small leaves, and local socket are outside the charter core. They use spare bravo
capacity. They never delay the queue transition or the tmux host extraction.

| Wave | Alpha serial work | Bravo parallel work | Final handoff and evidence |
| --- | --- | --- | --- |
| Cursor, then comms handler | Create a daemon-free cursor and handler contract. Replace `SetRecvDeps`. Test it with a fake. | Move and test the cursor store first. Then move the comms and decisions handler. | Alpha wires the adapter. `go list -deps` must show no daemon dependency. Run focused tests and the composition test. |
| Tmux host | Add a narrow `.golangci.yml` depguard allow-list and a direct daemon deny. Replace concrete assertions in `workloop.go`, `dot_cascade_core.go`, `agentlaunch.go`, and `crewstart.go`. Define tested interfaces and a factory. | Create `internal/substrate/tmuxhost`. Move the tmux host, paste injection, and focused tests. Change constructors in `cmd/harmonik/main.go` and `cmd/harmonik/run.go`. | Alpha removes the daemon adapter. Prove the package has no daemon dependency. Run focused and scenario tests. |
| Notification stream | Keep the run path stable during the cutover. | Move the event consumer and its focused tests. | Alpha changes its construction site. Run the focused and composition tests. |
| Local socket | Add a narrow `.golangci.yml` depguard allow-list and a direct daemon deny. Create a daemon-free API package with every DTO and consumer-owned handler interface. Test a fake handler for all operations. | Create `internal/transport/localsocket`. Move the listener and router adapter. Update command state and dashboard models. | Alpha removes the daemon listener adapter. Prove no daemon dependency. Preserve wire-byte and real-socket scenarios. |

The tmux host includes `tmuxsubstrate.go` and `pasteinject.go`. They share per-run state, pane work,
command choice, and watchdog behavior. Do not split them from each other. The local socket wave keeps
byte-level wire tests and real socket scenarios. `cmd/harmonik` imports the state and dashboard models,
so that migration belongs to the same wave.

The comms handler has about 404 lines and no run-machine connection. It is the only current daemon
candidate that can run beside alpha's `beadRunOne` boundary work. It depends on the cursor store and
the shared live cursor. Bravo moves the cursor first. Alpha provides the daemon-free contract before
bravo moves either package.

The queue spend meter, stale watcher, quiesce arbiter, state projection, and dashboard projection stay
put. They still require daemon concrete state. Moving them now would export internals instead of making
a boundary. The run machine also stays whole until alpha shrinks `workLoopDeps` behind owned ports.

---

## 4. Ordering — what is real, and what is habit

The map states its ordering in prose, and a re-measurement on 2026-08-01 found much of it defensive
rather than logical. Both readings are recorded here because a lane needs to know which constraints it
may not break.

### Real. Break these and something goes wrong, sometimes silently

- **Step 7 piece 1 before piece 2.** Reversed, all eighteen capabilities get ported into two copies
  and are then deduplicated.
- **The operator answers the guard question before either guard moves.** The question, in full: *do
  the escape check and the no-commit guard belong to the Run machine, or to the caller?* Moving them
  into the machine **changes behaviour** — graph runs that pass today would start being guarded, and
  graph mode is the default. Deleting the escape check instead admits the machine's guarding phase is
  decorative. The run-state-machine spec currently forbids what the port would do, so a spec amendment
  travels with the answer. This is an authority gate, not an engineering one. The map files it as D2.
- **A new carrier for the `review_bypassed` audit before `core.WorkflowMode` collapses.** The failure
  is invisible — no compile error, no red test, and the audit obligation just stops firing.
- **The 16 gate scripts land in the same change as the package split.** A split that leaves 16 red
  gates is a split nobody can merge.
- **The live pass runs before steps 10 to 15, not after.** Run it before and it is a baseline. Run it
  after and it is an autopsy.

### Habit, defence, or lane ownership wearing a dependency costume

- **"Step 10 depends on steps 2 to 6."** The stated mechanism did not happen. `workLoopDeps` is still
  81 fields and its declaration **grew** from 743 to 770 lines across the completion of steps 2, 3a,
  4, 5 and 6. Five completed steps moved zero fields off the bundle. The gate was declared discharged
  by work that discharged nothing.
- **"Step 12 depends on step 10."** Refuted by the repo's own worked example. The `bandwidth_tuner`
  gate already solves the nil-field-consumed-later problem in `internal/daemon/bootstate.go` today,
  and **none of the nine ungated consumers is ever written onto `workLoopDeps`.** Step 12 can start
  whenever alpha has a free turn in the package.
- **"Step 14 depends on step 10."** The arrow probably points the other way. Declaring the contract
  removes the boot-time extraction that threads capabilities onto the bundle.
- **"The 14 daemon queue writers wait for step 7."** Step 7 touches `workloop.go`'s tail and
  `dot_cascade_core.go`. The writers are in `scheduler.go` and two other files. Zero overlap.
- **"Step 19 goes inside step 11's window."** Convenience, and events already broke it.
- **"Step 15 is last."** Risk framing — and **step 15 is not on the charter's path to done at all.**
  §6 names nothing about package structure and ends "nothing else is required". Doing it is an
  operator choice about §4's standing rule, not a gate.

### One contradiction inside the map, for the operator

Step 8's entry says the guard decision lands there. Step 7's map says it must be answered **before**
either guard moves in step 7. The decision is scheduled one step after the step that needs it. It
needs an operator answer either way. §8 carries it.

### Hidden collisions the map does not state

- **Step 12 and step 14 collide line for line.** Two substrate type assertions sit inside
  `wireWatchersAndObservers`, the function step 12 rewrites.
- **Step 11 and step 12 collide in two files** — `internal/daemon/perqueuespendmeter_tigaf11.go` and
  `internal/queuewiring/operatorevents.go` are each both a queue writer and a named ungated consumer.
- **Step 10 and step 14 collide in `bootworkloop.go`**, where a capability assertion sits inside
  `injectWorkLoopDeps`.
- **Two freeze gates fire when the program succeeds, with no file moved.**
  `workloop-scheduler-freeze-gate.sh` asserts `runWorkLoop` is at least 200 code lines. It is 796.
  Hollow it out enough and the gate goes red. `runloop-emitter-gate.sh` budgets `deps.bus` per file
  and budgets any unlisted file at zero, so **any new file in `internal/daemon` trips it**.
- **The charter's "time injected rather than called" has no step at all.** It is an unscheduled
  candidate, and it is a clause of §6's own definition of done.

---

## 5. How lanes share the repo

**Alpha works in the main checkout on the shared branch. Every other lane gets its own `git worktree`**,
on branch `work/<lane>`, at `/Users/gb/github/harmonik-wt/<lane>`. Alpha is the exception because it
is the lane that merges, and a merge needs the shared branch checked out somewhere.

Not a shared checkout: `check-fast` runs `go build ./...` and `go vet ./...`, so one lane's
half-finished edit fails the other lane's gate for a reason it cannot see or fix. And in a shared
checkout no lane can tell its own dirty files from another's, which is the exact shape of this repo's
`implementer_escaped_worktree` history.

### The real coupling is the import graph, and it runs both ways

**This is the hazard that matters, and the previous version of this file did not name it.**
`internal/daemon` directly imports **eight** of bravo's packages — `internal/core`, `internal/queue`,
`internal/queuewiring`, `internal/lifecycle`, `internal/lifecycle/tmux`, `internal/projectconfig`,
`internal/eventbus` and `internal/runmerge`. They are **upstream** of alpha's. And `cmd/harmonik`
directly imports `internal/daemon`, which reaches `internal/runloop`, `internal/runlease` and
`internal/transport/tunnel` transitively, so `cmd/harmonik` is **downstream** of every alpha package.

> ⚠ An earlier draft of this section said six packages and named only `internal/daemon` among the
> four alpha packages as a direct import. The two it missed, `internal/eventbus` and
> `internal/runmerge`, each carry a named bravo work item in §3. Both of those edits happen to be
> safe — the runmerge payloads are unexported and `internal/daemon` never names `RunDrainer` — but
> the rule below would not have caught it. That is the failure mode this section exists to prevent.

`check-fast` cannot see either direction. `scripts/changed-go-packages.sh` tests the changed directory
and never its dependents. **So bravo can land a fully green commit that breaks alpha's build.**

**The rule that follows:** a lane that edits a package the other lane imports announces it before
committing and names the exported symbols it changed. And **`make check-fast` is not sufficient proof
for a commit in a shared-dependency package** — before offering a merge, also run
`go build ./... && go vet ./...` (about 30 seconds warm), plus `go test -short ./internal/daemon` when
the change is behavioural rather than a rename.

`internal/replay` is the one genuinely free package. Its only reverse dependent is itself.

### The freeze gates are alpha's problem, not bravo's

**All 14 gates that run in `check-fast` and `check-short` scan `internal/daemon`.** Three of them also
scan `internal/runloop` — `runloop-freeze-gate.sh`, `readywait-freeze-gate.sh` and
`runloop-emitter-gate.sh`. **Not one scans `internal/queue`, `internal/core`, `internal/lifecycle`,
`internal/projectconfig`, `internal/replay` or `cmd/harmonik`.**

> ⚠ The previous version of this file called `queuewiring-freeze-gate.sh` the "sharpest hazard" for
> the queue lane, on the grounds that it names `internal/queue` four times. Verified 2026-08-01:
> both of its greps target `internal/daemon`, and its only non-comment mention of `internal/queue` is
> inside a failure **message**. It cannot fire on bravo's queue work. That paragraph was wrong and is
> deleted.

The one gate that does reach bravo is `scripts/harnesspi-freeze-gate.sh`, which greps the `Makefile`
for a `test-pi-live` recipe pointing at `./internal/daemon`. **Bravo must not point that recipe back
at the daemon package.**

The 14 gates cost about 40 seconds. A warm `go build ./...` costs about 2.5 seconds.

### Traps that belong to this repo

- **A worktree carries a decoy bead ledger.** Bravo's worktree holds a 270 KB `beads.db` beside the
  main checkout's 37.9 MB one. `br` from a worktree does **not** fail. It silently reads and writes an
  almost-empty ledger, so a close there reads as done while the real ledger still shows the issue
  open. **Run `br` only from `/Users/gb/github/harmonik`.** The same holds for `.harmonik/` and
  `events.jsonl`. Worktree agents report a defect as a title and a paragraph, and the main checkout
  files it.
- **This file must be tracked.** It was untracked until 2026-08-01, so it was absent from bravo's
  worktree entirely. The document that assigns the lanes could not be read by the lane it assigns.
  **Keep it in git.**
- **`HANDOFF-*.md` is gitignored**, so it does not travel and it never appears in `git status`. Each
  lane's handoff lives in its own worktree root. Do not read its absence from a diff as "not written".
- **Go code hard-codes script and test paths as string literals, in BOTH lanes.**
  `internal/runloop/scenariogate.go` embeds `test/scenario/`. Two daemon schedule files embed
  `scripts/ctx-watchdog-launch.sh` and `scripts/ops-monitor-check.sh` — and the same
  `ops-monitor-check.sh` literal also sits in `internal/projectconfig/projectconfig.go` and
  `cmd/harmonik/schedule.go`, which are **bravo's own files**. A rename under `scripts/` or `test/`
  does not fail the build. It fails at runtime. **Grep the whole tree for the old name, not just
  `internal/`** — an earlier draft of this rule said `internal/` and would have missed two of the four
  copies.
- **The shipped-skill pair is a single edit, and it belongs to bravo.** Bravo owns `cmd/harmonik`,
  which holds the embed source `cmd/harmonik/assets/skills/`, so **bravo also owns the
  `.claude/skills/` mirror at the repo root.** The two must stay byte-identical in the same commit.
  **If the lanes edit opposite ends of the pair, git merges each path independently and raises no
  conflict** — the invariant breaks with nothing red. Alpha does not edit either path.
- **Do not use `.claude/worktrees/`.** That belongs to the agent tool.
- **A `cd` into a worktree persists across shell calls here.** Use absolute paths and `git -C`.
- **Do not let the agent tool cut the worktree. Cut it yourself.** Four independent agents have now
  been handed a stale base. The fourth was measured on 2026-08-01: the tool placed it at
  `dc2217527`, dated 2026-07-21 — **571 commits behind the tip, and 27 commits off the mainline**, so
  it was not merely behind. The commit it was told to build on existed in the repo, so this is not a
  missing fetch. Four hits is not four coincidences; the creation step resolves some cached or
  default ref rather than the current branch tip.
  **What works:** `git worktree add -b work/<name> /Users/gb/github/harmonik-wt/<name> <sha>` from the
  main checkout, then hand the agent the PATH and tell it the worktree already exists. That also
  keeps it out of `.claude/worktrees/`, which is off-limits by the rule above.
  **Keep telling every worktree agent to verify its base commit before it reads anything**, and to
  stop rather than rebase if the base is wrong. That instruction is what caught this one, at the cost
  of eight tool calls instead of a wasted pass — and a rebase across 571 commits is exactly the
  hard-to-reverse move a subagent should never make unasked.

### Two lanes and one machine

- **`--allow-parallel-runners` has landed** on `check-fast` and `check-short` at `5d8b75d87`, so the
  machine-wide `golangci-lint` lock no longer stops two lanes from linting at once. It is **still
  missing** from `make check` and `make lint`. If that error returns, some other recipe lacks the flag.
- **The `GOCACHE` half is NARROWED as of 2026-08-01, and narrowed is the honest word.** Every Go step
  in `check-short` now runs under `scripts/with-lane-gocache.sh`, which keys a PERSISTENT cache on the
  checkout root. **Two lanes in DIFFERENT checkouts no longer share cache facts**, so neither can
  produce `could not import ...: no such file or directory` in the other. Two things survive and the
  word "fixed" would hide both: two runs in the SAME checkout — an agent and a human, or two agents —
  still share one cache and are exactly as exposed as before; and a persistent cache is reachable by
  Go's own daily trim, which a `mktemp -d` cache never lived long enough to meet, so that vector is
  given back. Only `GOCACHE` is isolated — golangci-lint's own cache stays shared, and
  `--allow-parallel-runners` is what covers that side.
  **Not `with-isolated-gocache.sh`, and the difference is the whole point.** That script hands the
  command a `mktemp -d` cache and deletes it on exit, which is right for the one-shot coverage gate
  and wrong for a recipe a lane runs repeatedly — every run would start cold. Measured back-to-back
  `go build ./...`: lane cache 9.15s then 1.33s, isolated 8.78s then 8.67s. Same isolation, about
  6.5x faster on the second run, and `go build` is the cheap case next to `-race` tests. This is the
  "one cache per session, not one per command" shape `docs/disk-reclaim.md` already prescribed.
  It costs one cache per lane on disk — 157 MiB for `go build` alone, more with test objects.
- **The `-p=1 -parallel=1` half still stands, and it is a REAL reason not to run two at once.** It
  serializes deliberately, to stop cross-package `-race` saturation on a 10-CPU box. Two concurrent
  runs in different checkouts no longer corrupt each other, but they still compete for CPU, and the
  rule below about a loaded machine applies in full. **The cache work made concurrent runs correct
  across checkouts, not free.**
- **Parallelism has a ceiling and it is the daemon test suite.** Five agents at once drove the load
  average past 70 and the suite went red with a different set of tests on each run, every one of which
  passed in isolation. **A gate result from a loaded machine is not evidence — neither a green nor a
  red.** Parallelize across packages, the way this table does.

### Shared files — declare before you touch

Two entries are shared-fate regardless of lane: **`Makefile`** (declare the target) and
**`scripts/*gate*.sh`**. Beyond those: `.golangci.yml` (both lanes' depguard rules land in the same
48-line tail block), `coverage.baseline`, `specs/_registry.yaml`, `specs/process-lifecycle.md`,
`specs/event-model.md`, `specs/queue-model.md`, this plan directory's `NEXT_STEPS.md` and
`OPEN-DEFECTS.md`, `STATUS.md`, and `go.mod` / `go.sum`. Resolve a `go.sum` conflict with
`go mod tidy`, never by hand.

**`specs/execution-model.md` and `specs/run-state-machine.md` are alpha's outright, not shared.** An
earlier draft listed `execution-model` in both places, which is two different protocols for one file.
Bravo cites those specs freely and reports a needed change rather than editing one.

`DECOMPOSITION-MAP.md` is alpha's and it is the most-changed file in the program. Bravo reports
corrections to it rather than editing it.

### Merge discipline

A lane never merges itself. It commits on `work/<lane>` and records "ready at `<sha>`" in its handoff.
Alpha merges, at a clean breakpoint.

Before offering a merge: rebase onto the shared tip, then run the gate. **Never hand over a red gate.**
Commit with `git commit -F <file>`. Non-trivial commits carry `Reviewed-By:` and `Review-Verdict:`
trailers from the lane's own reviewer subagent. Do not gate on `main` or CI — `origin/main` is
divergent and its required check has been red since 2026-07-17.

**Rebase `work/<lane>` onto the shared tip immediately after every merge, not only before the next
one.** The first bravo merge was a clean fast-forward, and then the branch sat five commits stale
because nobody reset it. A "ready at `<sha>`" signal from a stale base means nothing.

**When a lane ends, delete its branch and its worktree in the same turn.** Charlie's finished work was
invisible for a day because an unmerged branch shows up in no status command anyone runs.

---

## 6. Keeping a lane alive

Measured 2026-07-31. `internal/keeper` and `cmd/harmonik` both build clean, and the token gauge is
live. **The daemon is not needed** — `internal/keeper` is fenced from importing `internal/daemon`, it
writes events straight to disk, and the brief it injects has no socket code.

**The thresholds already match the target.** `internal/keeper/thresholds.go` and
`.harmonik/config.yaml` agree: warn 200,000, act 215,000, force 240,000 absolute tokens, with a
trip-wire at 280,000. The band is `min(absolute, percent × window)`, so on a 1M window the absolute
numbers win. No retune.

**The one real blocker is tmux.** `keeper.InjectText` is the only path text takes into a session. A
plain terminal window is unreachable — `ResolveTmuxTarget` returns empty and `RestartNow` aborts.
**Every lane must run inside tmux.** That is the whole price.

**Rebuild the binary before turning this on.** The installed `~/go/bin/harmonik` is from 2026-07-22,
and the fix that stops the restart effector from destroying the handoff it exists to preserve landed
on 2026-07-23. Turning the keeper on with the stale binary eats lane handoffs.

Per lane, four steps:

```
go build -o /Users/gb/go/bin/harmonik /Users/gb/github/harmonik/cmd/harmonik

tmux new-session -d -s <lane> -n agent -c <lane-worktree> 'HARMONIK_AGENT=<lane> claude'

touch /Users/gb/github/harmonik/.harmonik/keeper/<lane>.managed

/Users/gb/go/bin/harmonik keeper --agent <lane> --tmux <lane>:agent --project /Users/gb/github/harmonik
```

`HARMONIK_AGENT` is load-bearing. The statusline's tmux fallback uses the raw session name, so a
session named differently writes a gauge file the watcher does not read. Check with
`harmonik keeper doctor --agent <lane>`; all 12 should be green.

**A Codex lane is not available yet.** `harmonik start crew --harness codex` terminates in a
deliberate refusal in `internal/crewrun/launchspec.go`. Only per-bead worker runs launch Codex, as
one-shot `codex exec` subprocesses, so a long-running Codex session today is a hand-run terminal.

The design for waking a stalled Codex lane was measured on 2026-07-31 and is kept here rather than in
a commit nobody will find. **The signal:** every session appends to
`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, and a final record of `task_complete` means the agent
stopped and is waiting — 170 of 174 July sessions ended that way. File mtime age separates idle from
mid-turn. **The hard part:** a stall and a finished job have the same signature on disk. What works is
the opposite test. A keyword gate over the last message ("requires direction", "blocked on",
"permission to", "should I", "which approach") fired on 7 of 36 stops, and on all 7 the operator's
reply was a decision rather than "keep going". So **default to nudging and let an explicit ask veto
it**, cap consecutive nudges, and escalate at the cap. A nudge is not a kill, so the worst case is one
wasted turn — which is why this is worth doing where the movement governor was not.
**Do not build a second keeper:** `internal/keeper/dashboardnag.go` is already a cooldown-gated
detect-then-inject nudge riding the watcher tick, and `InjectText`, the pane probes, the tick, the
lock and the consent marker are all reusable. **One structural constraint:** the linter forbids the
codex vertical and the keeper from importing each other, so the signal must arrive over a file marker,
the way the gauge does. **The hazard:** the rollout schema is undocumented and unversioned, so assert
the record shape at startup rather than failing silently.

None of this is scheduled. It is here so that staffing a Codex lane does not start with the
measurement again.

---

## 7. Order

1. ~~**Alpha: the `epic_completed` fix.**~~ **DONE 2026-08-01.** `maybeEmitEpicCompleted` in
   `workloop.go` now tests `IsTerminal()`, with two short-mode tests that are red without the fix, and
   `specs/event-model.md` §8.13.1's emission rule amended to match. It was claimed as a deliberate
   exception to `CHARTER.md` §4's "we are not fixing bugs" rather than left to look like the program's
   priority, on the grounds that the step consisting of this fix was already scheduled, the rest of
   that step had landed, and the defect silently stopped a lane rather than producing a visible
   failure. **The exception is spent. A defect found from here on is recorded, not chased** — the one
   found while doing this is `hk-terminal-status-literals-aynz5`, handed to bravo above.
2. **Bravo: rebase onto the shared tip**, then take §3 in order. Items 1 to 6 need no design.
3. **Alpha: step 7 piece 1 and the fifteen ungated capability ports.** The step map says these are not
   blocked on the guard decision and to start them now, ordered by harm.
4. **Bravo: the queue transition API**, once items 1 to 6 are landed and the kerf work has an agreed
   scope. This remains bravo's core priority.
5. **Alpha: define the tmux-host capability contract.** This is a small serial seam. It must replace
   daemon concrete assertions and unexported capability methods before bravo starts the package move.
   Alpha also adds the new package's narrow depguard rule and direct daemon deny.
6. **Bravo: build the tmux host package.** It owns the implementation and focused tests. Alpha keeps
   the daemon package clean while this work runs.
7. **Alpha: cut the daemon over to the tmux host.** This is the only daemon edit in the wave. Re-run
   the graph after the cutover before dividing the per-run path.
8. **Alpha: step 12**, which is not blocked on step 10 and was believed to be.
9. **Parallel capacity only: alpha may define the cursor and comms contracts.** Replace `SetRecvDeps`
   with constructor configuration. Define the shared cursor without a daemon import. Do not delay item
   4 for this work.
10. **Parallel capacity only: bravo may extract the cursor store, then the comms handler, then the
    notification stream.** Each new package must pass its no-daemon dependency check. This wave must
    not delay a ready queue or tmux task.
11. **Alpha: freeze the local-socket contract when the core no longer needs the lane.** Bravo can then
   build the transport package while alpha works on a separate daemon seam. Alpha also adds the new
   package's narrow depguard rule and direct daemon deny.
12. **Bravo: build the local-socket package as parallel capacity.** Alpha then performs the one
    composition-root cutover.
13. **Alpha: step 10**, after steps 7 and 8 have settled `beadRunOne`. That is the real argument for
    deferring it. "Steps 2 to 6 will discharge it" was not.

---

## 8. What needs the operator

Neither lane may decide these. Each one is stated with what it blocks and what each answer costs.

1. **Do the escape check and the no-commit guard belong to the Run machine, or to the caller?**
   Blocks step 7's second half and all of step 8. Moving them into the machine changes behaviour —
   graph runs that pass today start being guarded, and graph mode is the default. Deleting the escape
   check instead admits the machine's guarding phase is decorative. A run-state-machine spec amendment
   travels with either answer. The map files this as D2 and schedules it one step late.

2. **Three abandoned branches sit in the lane namespace. Reconcile them or delete them.**
   `work/cq-mig-01` is 44 commits ahead and 256 behind, last touched 2026-07-26, with 93 files
   including 8 in `internal/core` — among them `transition.go`, the seam bravo's largest item
   rewrites. `work/arch-01-contract` and `work/cq-01` are one commit each and about 210 behind. All
   three have dirty worktrees. **An agent must not delete 44 commits of someone else's work**, and
   leaving them costs bravo a contested file. If they are dead, saying so takes one sentence.

3. **Reconcile `main`.** Still the one move that fixes the stale lint base, the poisoned worktree
   source and the required check together. `origin/main` is divergent and its required check has been
   red since 2026-07-17. It needs a push, and an agent must not rewrite the branch every merge is
   judged against.

4. **The guard choice in `specs/run-state-machine.md` §3.3**, and the four charter-amending candidates
   in `DECOMPOSITION-MAP.md` §3b. Both were waiting before this session and still are.
