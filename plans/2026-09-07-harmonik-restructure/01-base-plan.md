# Harmonik Restructure — Base Plan

The grounding document for the clean-room restructure. It states what "core" is, sets the bar a
package must clear to enter the clean module, records the measurements that prove the mess is real,
and gives a concrete port order that starts with the core spine and keeper.

Read `00-seed-playbook.md` first — that is the north star (strangler-fig, a separate `clean/` Go
module behind a compiler-enforced one-way wall). This plan adapts that playbook to the real repository
as it stands on 2026-09-07: a **single module** (`github.com/gregberns/harmonik`), 83 internal
packages, no `go.work`.

All numbers below are measured on this machine at the current `main`. Commands are given so anyone can
re-run them. Where a claim is a judgment and not a measurement, it says so.

---

## 0. The one finding that reframes the whole job

**The build is not slow. The tests are slow, and the slow tests are integration tests that no amount
of package-splitting speeds up.**

Measured, cold cache:

| What | Time |
| --- | --- |
| `go build ./...` (cold, after `go clean -cache`) | **7.4 s** |
| `go build ./...` (warm) | **0.9 s** |
| `go test -c` for every package, run none (compile the whole test tree) | **21 s** |
| `go test -count=1 ./internal/core` (449 files, 131k lines) | **2.0 s** |

The prior decomposition program measured the real cost precisely and it agrees:
`plans/2026-08-22-decomposition-program/` found that `internal/daemon` alone runs 1,091 tests in 294 s,
that **617 of 815 test CPU-seconds are whole-work-loop integration tests**, and that the suite is
**93% idle** — it waits on real processes, it does not compute. Splitting production code does not move
those seconds; the integration tests stay integration tests wherever their production code lives.

So the restructure has two honest wins, and neither is "compile faster":

1. **A small clean module you can test in seconds** because it holds none of the integration mass and
   none of the recompile-the-world coupling described in §3.
2. **A compiler-enforced wall** that keeps the clean zone clean by construction, so a change there has
   a small, known blast radius.

The verification-speed win is real but it comes from **quarantining the integration tests behind build
tags and running the fast per-package loop against the clean module** — not from the module boundary
making anything intrinsically faster. §7 states this plainly so no one promises the operator a speed-up
the mechanism cannot deliver.

---

## 1. The core subsystem set

### The definitive statement

The core is the minimum set that lets the system **accept a unit of work, run an agent on it, and land
the result.** The Charter (`plans/2026-07-27-delete-and-rewrite/CHARTER.md` §3) states it, the operator
signed off on it ("comprehensive but tight", 2026-07-28), and it is the definition this restructure
uses:

> **config → event bus → queue → bead-ledger adapter → worktrees → harness registry + one substrate →
> work loop → merge**

Everything not on that line is **deferred by default, not by argument**: comms, crew, captain, keeper,
dashboard, live-state, subscribe, the sentinel, and the socket listener itself. Adding to the core set
needs a reason; removing a deferral is a one-line config change later.

A rewrite that ends with only the queue and the bead path working is a **success**, not a partial one
(Charter §3, operator verbatim).

### Where the docs disagree, and the crisp resolution

Two other definitions of "core" exist, and they are wider. Name them so no one trips:

- **`specs/execution-model.md` EM-061** defines "Core conformance" as EM-001..EM-046, EM-049..EM-059,
  EM-066..EM-067 plus three invariants — the full `dot`-mode dispatch contract. That is a **spec
  conformance term**, not this program's build target. The Charter says so directly: "§6's 'nothing
  else is required' is this program's target, not a redefinition of that normative term."
- The operator's framing for *this* restructure adds one item the Charter defers: **keeper**, wanted
  early and wanted as a standalone binary.

**Resolution — two tracks, no conflict.** Track A is the Charter core spine above; it is the first
vertical through the clean room. Track B is **keeper as a standalone tool**, run in parallel by operator
direction. Keeper is *not* promoted into the minimal core set — the Charter still defers it at runtime —
but it is a first-class early **port** because a self-contained tool is exactly what the clean room is
built to produce (`00-seed-playbook.md` "Target architecture"). Keeping the two straight matters: work
on keeper must never be justified as "core work", and the core spine must never wait on keeper.

### What is explicitly out

The **socket listener stays out of the core** (Charter §3, operator decision). The daemon reaches the
core indirectly; the socket becomes one caller among possible others. Extract it only when it blocks the
decomposition, and amend `process-lifecycle.md` PL-003 rather than obey it.

The **reviewer is wrongly fused into the work loop** and unfusing it into a switchable stage is in
scope — it is a large part of why `beadRunOne` grew to thousands of lines.

---

## 2. The clean-room admission bar

A package earns a move into `clean/` only by evidence. The default answer is **NO**. "It looks fine" is
not evidence; "it compiles" is not evidence (the Charter's §5 records three deletions that compiled
clean and were wrong). The bar is a set of **measured, checkable signals**. A candidate must pass the
three hard gates and should be scored on the soft signals; a low score is a reason to fix the package
*before* it moves, not to wave it in.

### Hard gates (a failure blocks the move)

1. **The wall holds — the closure is legacy-free after the move.** This is the invariant of the whole
   program (`00-seed-playbook.md`). The check is mechanical and belongs in CI:

   ```bash
   cd clean && go list -deps ./... | grep 'harmonik/legacy' && exit 1 || exit 0
   ```

   A package cannot enter `clean/` while its dependency closure still reaches a legacy package. Either
   the thing it reaches for comes along too (if small and good) or it is inverted into a
   consumer-owned interface the clean package declares (PRINCIPLES §4). This gate is the teeth: it is
   the compiler, not anyone's discipline, that keeps the zone clean.

2. **No grandfathered lint.** `clean/` starts with its **own `.golangci.yml` and an empty allow list.**
   The legacy tree carries a 580-line, 1,041-finding grandfather list (measured in
   `plans/2026-08-22-decomposition-program/`, with **248 pairs under `internal/daemon` alone** that
   each fire the moment a file moves). A package may not drag those findings across. It lints clean, at
   full strength, on entry — or it does not enter. This single rule is what stops the restructure from
   becoming "same code, new folder": the in-place programs stalled on exactly this ratchet; the clean
   room refuses the debt instead of inheriting it.

3. **The pure core reaches no ambient effect.** Per PRINCIPLES §1, the inside of the package takes
   values and returns values. Grep the non-test files for the tells and require each to be injected,
   not called:

   ```bash
   d=$(go list -f '{{.Dir}}' ./internal/PKG)
   grep -l 'time.Now()\|exec.Command\|os.Exit\|rand\.\|os.Getenv' "$d"/*.go | grep -v _test
   grep -n '^var [a-z]' "$d"/*.go | grep -v _test   # package-level mutable state
   ```

   A hit is not an automatic reject — it is a required refactor before the move: thread the clock,
   the command runner, the environment through a port (`substrate.ClockPort` is the one already in the
   tree). Effects live in a thin shell wired in `main`, never in the core.

### Soft signals (score them; a bad score is pre-move work)

4. **Efferent fan-out — how many legacy tentacles.** `go list -f '{{range .Imports}}...` restricted to
   internal packages. A candidate whose only internal import is a small, portable set is cheap; one that
   drags `daemon`, `crew`, `dashboard`, or the run machine is expensive. This is the single best
   predictor of port cost, and it must be measured **by doing the move**, not by reading the AST —
   the decomposition program proved AST guesses wrong (a "zero-edge" move cost two exports; a
   "leaf" was a 24-file import cycle).

5. **Test isolation and speed.** Does the package's test suite run hermetically in seconds, or does it
   spin up daemons, tmux, or real processes? Count `*_integration_test.go` and `//go:build integration`
   files. A package whose claims are only proven by slow integration tests does not move *clean* until
   those tests are tagged and quarantined (§7) and a hermetic unit layer exists under them.

6. **Consumer-owned ports already present (PRINCIPLES §4).** Does the package declare the small
   interfaces it needs and let callers satisfy them, or does it reach into concretes? `internal/queue`
   is the exemplar the principles cite by name (it declares `QueueSetter`, `MutationLocker`,
   `EventEmitter`, `BeadLedger` and never learns a daemon exists). A package already shaped this way is
   nearly ready; one that reaches into concrete siblings needs the seams cut first.

7. **Honest test names (PRINCIPLES §7).** Count test files named after a ticket rather than a behavior.
   A ticket-named test pins a signature, not a promise, and its green means little. `internal/core` has
   **115 of 449 files** carrying ticket suffixes (`_hqwn59`, `_hka8bg`, `_rc018`, …). A package heavy
   with these must have its claims re-proven or the tests renamed before its tests can be trusted in the
   clean suite.

8. **Not on the do-not-cut list.** Two prior programs independently parked the same cuts. Respect them:
   **do not split `internal/core`** (13 mutually-cyclic type families — parked in `p2-extraction` E6 and
   again in the decomposition program) and **do not extract `workloop.go` / `scheduler.go` /
   `dot_cascade_core.go` as packages** (decompose in place behind a single writer). The clean-room
   approach does not violate these — see §3 on why it sidesteps the core problem entirely.

### Why the bar is hard to game

The wall check is the compiler. The lint gate has no allow list to pad. The purity gate is a grep
anyone can re-run. The fan-out and cycle facts must be produced by an actual `git mv` + build, so a
paper claim of "clean" cannot pass. And the standing rule from the Charter applies: **start from the
assumption that it does not work** — a passing suite is the beginning of the check, not the end.

---

## 3. Current-state evidence

### The linchpin: `internal/core`

`internal/core` is a **131,000-line package** (80,803 non-test + 50,602 test, 449 `.go` files). It
imports **almost nothing internal** — its efferent internal fan-out is essentially zero, it is a giant
leaf of domain types, IDs, and event payloads. But its **afferent fan-in is 60 of 83 internal
packages.**

```
afferent fan-in (packages whose transitive deps include X), of 83 internal packages:
  core: 60      handlercontract: 19   lifecycle: 12
  queue: 23     eventbus: 15          handler/dispatch: 11
  substrate: 14 presence: 8           run: 9 / workspace: 9
```

This is the mechanism behind "runs dog slow" on the *change*-loop, even though a cold full build is only
7.4 s. `core` holds every domain type, so almost every change touches it, and a change to any of its 449
files invalidates the test cache of **60 downstream packages**. Nothing is ever "unchanged", so Go's
incremental cache — which is excellent — rarely gets to fire. The seed playbook's diagnosis is exactly
right: "too much depends on one big thing, so nothing is ever unchanged."

**core cannot be cut in place** — two programs proved it (the 13 type families are a mutual cycle). The
clean-room approach is the one move that does not require cutting it: **leave the 131k-line core in
legacy, and build a fresh, minimal typed vocabulary in clean, porting only the types each moved package
actually needs.** This is the single most important structural decision in the plan, and it is also the
single biggest risk (§8): if "minimal types" balloons into re-deriving core, the clean room stalls at
its first real port.

### The other monster: `internal/daemon`

`internal/daemon` is **111,000 lines** and its dependency closure is **50 of 83 internal packages** — it
is the top of the graph and the sink of the tangle. It is *not* a clean-room candidate and never will be
as a unit; it is what shrinks as clean grows. It also **grew during the last decomposition** (44.8k →
49.4k production lines over the program) because nothing counted its size. Any plan without a visible
size scoreboard repeats that.

### The clean spine candidates — genuinely low-tentacle

Measured direct internal imports and transitive internal dep counts:

| Package | Direct internal imports | Transitive internal deps | Verdict |
| --- | --- | --- | --- |
| `substrate` | *(none)* | 1 (self) | **Clean leaf.** Holds the clock port. Move first. |
| `structuredlog` | core | 2 | **Clean, tiny.** Only tentacle is core. Warm-up port. |
| `eventbus` | core | 2 | **Clean-ish.** Only tentacle is core; one `time.Now` to inject. |
| `queue` | core | 2 | **The exemplar** (PRINCIPLES §4). Only tentacle is core. The centre. |
| `presence` | core, eventbus | 3 | Clean after eventbus lands. |
| `dispatch` | core, queue | 3 | Clean after queue lands. |
| `runexec` | core, substrate | 3 | Clean after substrate lands; must read no clock (linter rule). |
| `handlercontract` | core, .../lifecycle | 3 | Moderate; afferent 19 so high value. |

The recurring fact: **almost every good candidate's only real tentacle is `core`.** That is what makes
"port a minimal core vocabulary first" the enabling move for the entire spine.

### The tangle, named bluntly

- `runloop`: **17** transitive internal deps, 13 direct imports (`brcli`, `gitprobe`, `handler`,
  `harness/shared`, `lifecycle/tmux`, `runmerge`, `workers`, …). It is the run machine; it is *not* an
  early port. The `p2-extraction` program flagged its predecessor as the high-risk keystone.
- `daemon`: 50 deps. Out.
- `workspace` (27k lines), `lifecycle` (15k), `scenario` (15k): large, effect-heavy, late.

### Keeper — the operator's belief, tested against evidence

The operator believes keeper is "relatively clean" and should move early as a standalone binary.
**Half right, and the wrong half matters.**

- **Size:** 26,276 non-test + 19,856 test lines. Not small.
- **Tentacles:** direct imports are `core`, `dashboard`, `digest`, `presence`, `substrate`.
  Transitively that is **13 internal packages**, because `internal/digest` drags in `crew`, `eventbus`,
  `lifecycle/tmux`, `presence`, `queue`, `dispatch`, `run`, and `sentinel`. So keeper reaches the whole
  run machine through one status-summary import.
- **Purity:** keeper calls `time.Now()` directly (`gates.go`, `tmuxresolve.go`) and `exec.Command` in
  six files (`watcher.go`, `respawn.go`, `injector.go`, `tmuxresolve.go`, `awaitack.go`,
  `delivery_decision_0nlqs.go`). It is an **effects-heavy** package by nature — process, tmux, shell.
- **Tests:** heavy on `*_integration_test.go` (tmux, live-pane, twin-e2e, restart-now). Prior audits
  (`code-health-audit`) found `Watcher.Run` is a **549-line god-method, cognitive complexity 179**, and
  its inject/ack/respawn action path is **not covered** and **non-hermetic** (a short run failed its
  heartbeat budget despite high reported coverage).

**Verdict:** keeper is a genuine *tool* and a natural standalone binary, so the target shape is right.
But it is not clean today, and its afferent fan-in is low (**6**), which means moving it does little for
the recompile problem — its value is the standalone binary and the collision-avoidance, not build
speed. It is an early *Track B* port only **after** its `digest` and `dashboard` tentacles are inverted
and its god-method is split (§6). Every prior audit rates keeper **Simplify, never Rebuild** — the root
multi-writer-gauge cause is already fixed; the remaining work is flattening the god-method and covering
the action path.

---

## 4. The clean-room setup

The repo is one module. Here is the concrete path from here to the two-module split the playbook
assumes.

### Step A — introduce the split without moving code (an afternoon)

The playbook draws `legacy/` and `clean/` as siblings. Renaming the existing module root to `legacy/`
would rewrite thousands of import paths in one commit — high risk for zero benefit. **Keep the existing
module where it is and add `clean/` beside it.** The wall direction is identical; only the legacy
prefix differs.

```
harmonik/
  go.mod            # module github.com/gregberns/harmonik           (unchanged — "legacy")
  go.work           # go 1.26 \n use . \n use ./clean
  clean/
    go.mod          # module github.com/gregberns/harmonik/clean
    .golangci.yml   # full strength, EMPTY allow list
    doc.go          # package marker so the module builds empty
```

`go.work` ties them so the legacy module can import `.../clean/...` while both build together. The wall
invariant is unchanged: **clean may never import the legacy module; legacy may import clean.**

### Step B — the CI wall check (make it a gate, not a nicety)

Add a target that fails the build if clean ever reaches back:

```bash
# scripts/clean-wall.sh
cd clean && go list -deps ./... | grep -E 'gregberns/harmonik/internal|gregberns/harmonik/cmd' \
  && { echo 'WALL BREACH: clean imports legacy'; exit 1; } || echo 'wall intact'
```

Wire it into `make full`. The repo already has `tools/forbid-import` — reuse it if it is a better fit
than grep, but the closure check above is the authoritative one because it catches transitive breaches,
not just direct imports (PRINCIPLES §4 notes depguard checks direct imports only).

### Step C — the size scoreboard (do not skip this)

The last decomposition let `daemon` grow 44.8k → 49.4k because nothing counted. Add a trivial reported
counter to `make full`: lines in `clean/` and lines in `internal/daemon`, printed every run. The whole
program's progress is "clean up, daemon down." Make it visible or it will not happen.

### Step D — the port loop (per the playbook, unchanged)

`git mv` (preserve history) → rewrite the import prefix → `goimports -w` → fix what the wall rejects
(pull small good things along, invert messy things into consumer-owned ports) → leave a throwaway
adapter in legacy so old callers keep working → `cd clean && go test ./PKG/...` (seconds) → then
rewrite in place safely behind the wall.

---

## 5. Port order — the first ports, ranked by fewest legacy tentacles

The order is driven by the §3 measurements: start with the leaf that everything needs (the clock port),
then the minimal type vocabulary that unblocks the spine, then the spine itself in dependency order.

**Port 0 — `substrate` (the clock/effect ports).**
Blast radius: afferent 14, efferent 0 — a true leaf. It holds `ClockPort`, the injection point
PRINCIPLES §1 is built around. Wall/adapter needed: none; it is the adapter vocabulary. Why first:
nothing else can satisfy the purity gate without it. This is the cheapest possible first turn of the
loop — pick it to watch the machine work once.

**Port 1 — a minimal typed vocabulary (`clean/core` or split `clean/id`, `clean/event`).**
This is the hard one and the enabling one. Do **not** move the 131k-line `internal/core`. Build fresh in
clean only the IDs and event payloads the next two ports (`eventbus`, `queue`) actually reference, and
port more on demand. Wall/adapter needed: legacy `core` keeps its copies; a throwaway adapter converts
between legacy-core types and clean types at the seam until legacy callers are rewired. Why early: every
spine package's only tentacle is core, so this unblocks all of them. Why risky: see §8 — this is where
the program most plausibly stalls.

**Port 2 — `structuredlog` (warm-up).**
Blast radius: afferent 1, transitive deps 2, tiny. Only tentacle is core. A low-stakes second turn that
proves the type-porting seam from Port 1 before the centre depends on it.

**Port 3 — `eventbus`.**
Blast radius: afferent 15. Only tentacle is core; one `time.Now` to route through the Port-0 clock. It
is the Charter core-set's second element. Adapter: the legacy daemon subscribes through a shim until it
is rewired.

**Port 4 — `queue` (the centre, and the exemplar).**
Blast radius: afferent 23. Only tentacle is core. PRINCIPLES §4 cites it by name as the model of
consumer-owned ports — it already declares `QueueSetter`, `MutationLocker`, `EventEmitter`, `BeadLedger`
and never learns a daemon exists. It is the Charter's "centre" and the operator's success criterion. It
is the proving vertical for PRINCIPLES §9 ("prove one vertical, then generalize"). Adapter: `queuewiring`
stays in legacy and satisfies the ported queue's ports. Note the known two-writer hazard on `queue.json`
flagged by the census — fix it as part of the move (one writer, PRINCIPLES §6), do not carry it across.

**Port 5 — `presence`, then `dispatch`.**
`presence` (afferent 8) tentacles are core + eventbus — clean once Port 3 lands. `dispatch` (afferent
11) tentacles are core + queue — clean once Port 4 lands. Both are natural followers of the spine.

**Port 6 (Track B, parallel) — keeper, after its seams are cut.**
Not an early *clean* port in the naive sense — see §6 for the three-step preparation it needs first
(sever `digest`, sever `dashboard`, split `Watcher.Run`). Sequenced here because it can proceed in
parallel with the spine on a separate crew, and because the operator wants the standalone binary.

**Explicitly late:** `runexec`/`runloop`/`run` (the run machine — 17 deps, the high-risk keystone),
`workspace`, `lifecycle`, `scenario`, and everything the `daemon` closure holds. And **never as package
cuts:** `workloop.go`, `scheduler.go`, `dot_cascade_core.go` — decompose those in place behind a single
writer, per two prior programs.

---

## 6. Keeper as its own binary

**Target shape (from `00-seed-playbook.md`):** `tools/keeper/` = a library (`clean/keeper`, the pure
decision core) + a thin `main` + a bus/tmux adapter. Both `go build ./tools/keeper/cmd/keeper` (the
standalone binary) and the umbrella `harmonik keeper …` subcommand call the same `keeper.Run(ctx,
env)`. That shape is a good fit: keeper is already a self-contained watcher of one session.

**What blocks it today, in priority order:**

1. **The `digest` tentacle.** keeper imports `internal/digest`, which alone drags in `crew`, `queue`,
   `dispatch`, `run`, `sentinel`, `eventbus`, `lifecycle/tmux`, `presence`. This is the whole run
   machine behind one import, and it fails the wall gate. **Invert it:** keeper declares a small
   consumer-owned port for the exact status summary it needs, and legacy `digest` satisfies it via an
   adapter. This is the single highest-value cut for keeper.
2. **The `dashboard` tentacle.** keeper imports `internal/dashboard` for the nag. Invert to a tiny
   `Notifier` port keeper declares; wire the concrete dashboard notifier in `main`.
3. **`Watcher.Run` is a 549-line god-method** (cognitive 179) mixing pure decisions (should we warn,
   restart, defer?) with effects (`exec.Command`, tmux paste). It violates PRINCIPLES §1 and §5. **Split
   it:** a pure `decide(gaugeReading, config, clock) → Decision` function (values in, values out,
   trivially testable) and a thin `apply(Decision)` shell that runs the effects. This also fixes the
   untested, non-hermetic action path — the pure half is unit-testable without tmux.
4. **Direct `time.Now()`** in `gates.go` and `tmuxresolve.go` — route through the Port-0 clock.
5. **The 131k-line `core` import** — reduced to the handful of keeper-relevant types ported into clean
   (same mechanism as Port 1).

**Smallest first step:** it is not a move. keeper already has `ports.go` and `injector.go`, so the seam
vocabulary exists. The smallest real progress is **step 3 done in place, in legacy**: extract the pure
`decide` function out of `Watcher.Run` and give it a hermetic unit test that watches a real assertion
fail (PRINCIPLES §7). That one change de-risks the whole port, makes the untested action path testable,
and is reversible. Do it before any `git mv`.

**Honest bottom line:** keeper *should* become a standalone binary and the operator is right to want it.
But "keeper is relatively clean" is not what the code says — it drags the run machine through `digest`,
its central method is a god-method, and its action path is untested. Treat keeper as a Track-B port that
earns its move by the four cuts above, not as a clean package waiting to be relocated.

---

## 7. The fast-verification win, honestly

**What actually gets fast:**

- **The clean per-package loop.** `cd clean && go test ./queue/...` compiles and runs against a small
  dependency graph with no daemon, no integration mass, no 60-package fan-out. Seconds, not minutes.
- **Small packages = small blast radius.** In clean, a change to `queue` invalidates only `queue`'s few
  dependents, not 60 packages. This is the direct cure for the §3 recompile-the-world problem — but the
  cure is the *shape* (small packages, consumer-owned interfaces at seams), which the module wall
  protects. The wall does not create the speed; it protects it.
- **The module cache stays warm.** A legacy change cannot invalidate clean's test cache — different
  module, one-way wall — so clean's `(cached)` results actually stick.

**The honest caveat — say this to the operator plainly:**

The restructure does **not** make the slow integration tests fast. Measured fact: 617 of 815 daemon
test CPU-seconds are whole-work-loop integration tests that spin up real processes and are 93% idle
(waiting, not computing). Splitting production code moves their *location*, not their *duration*. The
restructure **quarantines** them behind `//go:build integration` so they leave the fast inner loop and
run only at the assembly stage — it does not speed them up. Decomposition makes the fast loop fast;
build tags keep the slow tests out of it. Anyone who promises the integration suite gets fast is
selling the wrong story.

**Two orthogonal quick wins worth taking now** (measured by the decomposition program, independent of
the module split): 28% of recent commits changed no `.go` file yet paid a full ~7-minute gate — a
change-scoped gate skips them; and the cold Go build cache costs ~25 s per attempt and is never reaped —
stop reaping it. These are cheap and do not wait on the restructure.

---

## 8. Risks, stated as risks

1. **Port 1 balloons (the program-killer).** "Port only the minimal core types" assumes the type
   families separate cleanly. Two programs found the 13 families are a **mutual cycle**. If the first
   real port drags half of core across, the clean room stalls at the starting line. **Mitigation:**
   treat Port 1 as a research spike first — attempt to carry only the IDs and one event family, measure
   what comes with them, and report before committing to the spine order. If the minimal set is not
   minimal, that is the finding, and the plan changes.
2. **The lint ratchet fights back.** Even with clean's empty allow list, the *legacy* side's 248 daemon
   allow-list pairs fire when a file moves out. **Mitigation:** the operator must settle how the legacy
   allow list survives a rename before the first `git mv` — the decomposition program named this the
   hard blocker and it is unresolved.
3. **Execution capacity, not plan quality.** The decomposition program's GAPS finding outranks
   everything: **7 beads closed in 5 weeks; codex 0 of 29, pi 1 of 41, claude 47%.** A restructure that
   assumes a cheap local lane does the bulk porting is betting on throughput the fleet has not shown.
   **Mitigation:** prove one port end-to-end with the intended harness before scheduling the rest.
4. **Adapter rot.** Every inverted seam leaves a throwaway shim in legacy. Shims that never get deleted
   become permanent. **Mitigation:** the shim's existence marks unfinished work; the size scoreboard
   (§4C) should also count shims so they cannot hide.

---

## 9. What to do next (concrete, small, reversible)

1. Stand up `clean/` + `go.work` + the wall check + the size scoreboard (§4). An afternoon, zero code
   moved.
2. Move `substrate` across (Port 0). Watch the loop turn once on the cheapest possible package.
3. Run the **Port 1 spike**: attempt the minimal core-type carry, measure the drag, report. Do not
   proceed to the spine until this is known.
4. In parallel, on a separate crew, do **keeper step 3** in legacy: extract the pure `decide` function
   from `Watcher.Run` with a hermetic test. Reversible, de-risks Track B.

Everything after that depends on what the Port 1 spike finds. Re-derive every number before acting on it
(the Charter's standing rule; estimates in this program have missed by 5× in both directions).
