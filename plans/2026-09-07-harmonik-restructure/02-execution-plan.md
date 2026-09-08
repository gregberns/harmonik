# Harmonik Restructure — Execution Plan

The runnable, phased, tasked plan. It turns `01-base-plan.md` into work a captain or crew can pick up.
Read `01-base-plan.md` first: it is authoritative on the core definition, the admission bar, the
measurements, and the port order. This file does not re-argue those. It sequences them, sets a gate at
each phase boundary, ties the work to how harmonik actually dispatches (beads, the daemon queue, the
review gate, `make full`), and names the decisions the operator must sign before a phase can close.

**One honest caveat up front, repeated so no one is surprised.** This restructure does NOT make the
slow integration tests fast. Measured fact from `01-base-plan.md`: 617 of 815 daemon test CPU-seconds
are whole-work-loop integration tests that spin up real processes and sit 93% idle. Splitting
production code moves their location, not their duration. The wins are a small clean module you can
test in seconds and a compiler-enforced wall that keeps a change's blast radius small. Anyone who
promises a faster integration suite is selling the wrong story.

---

## How this work runs in the fleet

- **The work is beads.** Each task below becomes one or more `br` beads. The phase is the label lane;
  the dependency column is the `br dep` edge. Dispatch is the normal daemon queue loop
  (`harmonik queue submit --beads …`), not hand-run sub-agents (orchestrator-rules §Dispatch).
- **Plan the subsystems with kerf.** The clean-module scaffold, the minimal-vocabulary spike, and each
  spine port cross subsystem boundaries and add new packages, so each gets a kerf work with the
  `codename:` bead-label convention (AGENTS.md §Key conventions). Suggested codenames are given per
  phase. Trivial one-file changes skip kerf.
- **Every non-trivial commit hits the review gate.** A separate reviewer node sits on the only inbound
  edge to `close`; the commit carries `Reviewed-By:` and `Review-Verdict:` trailers. See §5 for what
  the reviewer checks on restructure work specifically.
- **`make full` is the merge decision.** It runs in CI on every push and PR. Phase 0 wires the wall
  check and the size scoreboard into it, so from then on every restructure commit is measured by them.
- **Harness.** Codex or Pi implement; Claude is reserved for oversight (AGENTS.md). Staff the porting
  crew on Codex or Pi. Keep Claude for the reviewer node and for the Port-1 spike judgment.

---

## The sibling bus track lands into this module (reconciliation — 2026-09-07)

This restructure builds ONE `clean/` module. Promoting its standalone pieces into separate `libs/` +
`tools/` modules is END-STATE, deferred until a piece is proven (`00-seed-playbook.md` §Finishing and
§"Target architecture"; the same deferral this plan already applies to every package). The sibling bus
track (`../2026-09-07-harmonik-bus/02-execution-plan.md`) is a same-day sibling, and it lands INSIDE
this single module — NOT in a `libs/` module that does not exist yet.

The join point is two named packages this plan reserves and the bus track builds:

- **`clean/transport`** — the bus surface (`Bus` / `Service` / `Message` / `Mount` + the in-memory
  subject router). The bus track's Phase-0 scratch package git-mv's here once this module exists (into
  `clean/transport`, NOT `libs/transport`).
- **`clean/cli`** — the shared env seam, carrying `Env.Bus transport.Bus`. This is the composition
  point the umbrella injects (the `libs/cli.Env.Bus` of the end-state, born in `clean/` first). The bus
  track lands the minimal `clean/cli` it needs; this plan only reserves the path.

What the bus track needs from THIS plan is exactly the Phase-0 scaffold: the `clean/` module, `go.work`,
and the one-way wall (task 9 done). It does not need `libs/`, `libs/cli`, or `tools/keeper` — those are
END-STATE promotions, deferred. Keeper as a bus `Service` waits on Track B landing `clean/keeper`
first (§Track B); the bus track does not ride keeper as its first service. Both plans now reference the
SAME paths (`clean/transport`, `clean/cli`) and the SAME keeper sequencing (keeper is the late Track-B
tail), and neither depends on anything the other does not produce.

---

## Phase 0 — Scaffolding (the enabling step; everything waits on it)

Goal: stand up the two-module split, the wall, the lint boundary, the size scoreboard, and the
integration-test build tag — **with zero production code moved.** This is the "get things in place"
the operator wants first (`README.md` §What we do first).

Kerf codename: `codename:clean-scaffold`.

### Steps

1. **Create the clean module.** Add `clean/go.mod` = `module github.com/gregberns/harmonik/clean`,
   `go 1.25` (match the root `go.mod`; it is `go 1.25`, not 1.26). Add `clean/doc.go` with a package
   marker so the empty module builds. Do NOT rename the root module to `legacy/` — that rewrites
   thousands of import paths for zero benefit (`01-base-plan.md` §4A). The root module keeps its path
   and plays the "legacy" role; only the clean prefix differs.

2. **Add `go.work`** at the repo root: `go 1.25`, `use .`, `use ./clean`. This lets the root module
   import `.../clean/...` while both build together. The wall invariant is unchanged: **clean may
   never import the root module; the root module may import clean.**

3. **Write the wall check** as `scripts/clean-wall.sh` — an **ALLOW-LIST, not a deny-list.** Any
   dependency in `cd clean && go list -deps ./...` whose path starts `github.com/gregberns/harmonik/`
   but is NOT under `.../clean/` is a breach (third-party and stdlib deps carry no such prefix, so they
   pass):

   ```bash
   cd clean && go list -deps ./... \
     | grep '^github.com/gregberns/harmonik/' \
     | grep -v '^github.com/gregberns/harmonik/clean/' \
     && { echo 'WALL BREACH: clean imports legacy'; exit 1; } \
     || echo 'wall intact'
   ```

   The deny-list form the base plan and the seed used (`grep -E '(internal|cmd)'`) was airtight ONLY
   under the `legacy/` rename this plan declines (Phase 0 step 1). Without the rename, the legacy
   module has Go packages under FOUR more top-level prefixes — `evaltasks`, `scripts`, `test`,
   `tools` — that a `(internal|cmd)` grep sails straight past. A clean package that imports
   `harmonik/tools/forbid-import` or `harmonik/evaltasks/...` would breach the wall and the deny-list
   would not see it. The allow-list is robust to any new legacy top-level dir; the enumerate-the-bad
   form is not. The dependency-closure form is authoritative because it catches transitive breaches,
   which depguard (direct imports only) does not. The repo already has `tools/forbid-import` — reuse it
   as a second, faster direct-import guard if wanted, but the closure check is the teeth.

4. **Wire the wall check into `make full`** as its own target (for example `clean-wall:`), added to the
   `full` target's prerequisites. From this commit on, a clean→legacy import fails CI. This is a gate,
   not a nicety.

5. **Give clean its own `.golangci.yml` with an EMPTY allow list.** Full strength, no grandfathered
   findings. The root tree's ~1,041-finding, 580-line grandfather list (248 pairs under
   `internal/daemon` alone) may not cross the wall. A package lints clean on entry or it does not
   enter. This single rule is what stops the restructure from becoming "same code, new folder".

6. **Add the size scoreboard to `make full`.** Print, every run: total lines under `clean/`, total
   lines under `internal/daemon`, and a count of legacy shim files (grep a `// RESTRUCTURE-SHIM`
   marker convention, defined here so shims cannot hide). The program's whole progress is "clean up,
   daemon down, shims → zero." The last decomposition let daemon grow 44.8k → 49.4k because nothing
   counted. Make it visible or it will not happen.

7. **Define the integration-test build-tag convention.** Adopt `//go:build integration` as the tag
   for whole-work-loop tests (the repo already uses `//go:build scenario`, `subprocess`, and others,
   so the pattern is established — see the Makefile freeze-gate and `test-scenario` targets). Add a
   fast per-package target for the clean module (`cd clean && go test ./...`) that excludes the
   integration tag, and a documented note that clean-module tests must run hermetically in seconds.
   No production integration test is retagged in Phase 0 — this step only fixes the convention and the
   fast-loop target so Phase 2 ports land already-quarantined.

### Phase 0 gate (explicit — the plan does not advance until all hold)

- `go build ./...` and `cd clean && go build ./...` both green under `go.work`.
- `make full` runs `scripts/clean-wall.sh` and it prints `wall intact`.
- `make full` prints the size scoreboard (clean lines, daemon lines, shim count) on a normal run.
- `clean/.golangci.yml` exists with an empty allow list and lints the empty module clean.
- The `integration` build tag and the clean fast-loop target exist and are documented.
- A deliberately-planted `import "…/tools/forbid-import"` (a NON-core, NON-`internal`/`cmd` legacy
  prefix) in a throwaway `clean/` file makes `make full` fail the wall check — proving the gate catches
  the prefix the old deny-list grep was blind to — then is removed. Planting an `internal/core` import
  would only exercise the prefix even the buggy grep caught; the proof must hit the real blind spot.
  (Break it on purpose and watch, PRINCIPLES §7.)
- **The legacy-lint-ratchet experiment (resolves D2 by measurement, not policy).** `git mv` ONE
  daemon-adjacent file out of its package to a throwaway location, run `make full`, and confirm two
  facts: (a) the moved file lints clean under `clean/.golangci.yml`, and (b) **no new finding fires in
  the files left behind** in the source package. Then revert the move. The root `.golangci.yml` carries
  ~248 daemon allow-list pairs keyed to package paths; whether a rename leaves a neighbour's
  suppression intact is a mechanical fact nobody has measured, and the base plan calls it "the hard
  blocker and it is unresolved." A policy ("freeze the allow list") is not a proof. Until this
  experiment is green, the first daemon-adjacent port stays blocked. This replaces DECISION D2 with a
  result.

Nothing in Phase 0 is code-behavior change, so each step is a small reversible commit through the
normal review gate. Estimated size: an afternoon to a day.

---

## Phase 1 — The Port-1 research spike (de-risk before committing to the spine)

Goal: find out, by measurement, whether "port only a minimal core vocabulary" is real, BEFORE any
spine package depends on it. This is the single biggest risk in the program (`01-base-plan.md` §8,
risk 1): if the first real port drags half of `internal/core` across, the clean room stalls at the
starting line. Phase 1 exists so that discovery is cheap, contained, and reported — not discovered
halfway through the spine.

**This phase is a SPIKE, not a delivery. Its deliverable is a decision, not merged production code.**
Do not let it silently become "port all of core." The STOP condition below is load-bearing.

Kerf codename: `codename:core-vocab-spike`. Run the judgment on Claude (this is the one place judgment
is the product, not throughput).

### What to try

Port 0 first, because it is free insurance and the spike needs it: **move `substrate` across** (a true
leaf, afferent 14, efferent 0; it holds `ClockPort`). This is Port 0 in the base plan and it is the
cheapest possible turn of the loop. It also proves the loop mechanics (git mv, prefix rewrite, wall,
fast test) on a package that cannot balloon. Do this as the spike's warm-up, not as a separate phase.

Then the spike proper: in a throwaway branch, attempt to build in `clean/` ONLY the IDs and ONE event
family that the next two ports (`eventbus`, `queue`) actually reference. Do not move
`internal/core`. Carry types on demand. Every time a carried type forces another type across, record
it. The measurement IS doing the move, not reading the AST — the decomposition program proved AST
guesses wrong by large factors (`01-base-plan.md` §2, soft signal 4).

### What to measure

- **Closure size of the minimal carry.** How many distinct `core` types must come across to make
  `eventbus` and `queue` compile against clean? Count them.
- **Did a cycle drag?** The 13 mutually-cyclic type families are the known hazard. Record whether the
  IDs + one event family separate cleanly, or whether pulling one pulls a family.
- **Adapter surface.** How large is the throwaway legacy↔clean type-conversion shim at the seam? A big
  shim is a signal the vocabulary is not minimal.
- **Line count carried** vs. the 80.8k non-test lines in `internal/core`. The number that matters is
  the fraction of core the "minimal" set actually is.

### The decision it feeds (report to the operator)

- **GREEN — minimal is minimal.** The carry is small (rough guide: low tens of types, no family
  cycle dragged, a small conversion shim). Commit the spike result as the real `clean/` vocabulary and
  proceed to Phase 2 in the base plan's order.
- **AMBER — minimal is medium.** The carry is larger than hoped but the cycle did not drag. Report the
  size, propose a revised vocabulary boundary, and get an operator DECISION on whether to proceed or
  reshape the split (for example `clean/id` + `clean/event` as two packages instead of one).
- **RED — STOP condition — minimal is not minimal.** Pulling the IDs and one event family drags a type
  family or a large fraction of core. **That is the finding.** Do not push through it. Stop, report,
  and the spine plan changes: the base plan's port order assumes the vocabulary separates, and if it
  does not, the enabling move is different (candidates to put to the operator: cut a genuine seam in
  legacy core first, or redraw the minimal set around what actually separates). Reversing course here
  is a success of the spike, not a failure of the program.

### Phase 1 gate

- `substrate` is moved into `clean/`, the wall holds, and `cd clean && go test ./substrate/...` is
  green in seconds. (Port 0 landed for real.)
- The spike measurement exists as a written report with the four numbers above.
- A GREEN / AMBER / RED verdict is recorded and, for AMBER or RED, an operator DECISION is captured
  before Phase 2 opens.

**DECISION D1 (operator sign-off required to leave Phase 1): does the minimal-vocabulary carry clear
the GREEN bar, and if not, which reshaped boundary do we adopt?** Recommended default: proceed only on
GREEN; on AMBER adopt a two-package `clean/id` + `clean/event` split and re-measure; on RED pause the
spine and bring the reshape options to the operator.

---

## Phase 2 — The spine port loop

Goal: port the Charter core spine into `clean/`, one package per turn of the loop, in dependency order.
Only opens after the Phase 1 gate. Order (from `01-base-plan.md` §5, Port 0 already done in Phase 1):

**land the PRODUCTION vocabulary (task 14, the reviewed artifact — NOT the throwaway Phase-1 spike) →
then `structuredlog`, `eventbus`, `queue`-relocation IN PARALLEL (each imports only `core`, no edge
between them) → `presence` follows `eventbus`, `dispatch` follows `queue`, and the `queue.json`
two-writer fix is its own bead after the relocation.**

The Phase-1 spike measured whether a minimal vocabulary is real; it did not ship one. Task 14 builds
the vocabulary + the legacy↔clean adapter as reviewed production code that passes all three hard gates,
because a throwaway branch cannot be the thing every spine port imports.

Each port is ONE turn of the port loop and ONE (or a small few) beads. Kerf codename per port, for
example `codename:port-eventbus`, `codename:port-queue`.

### The port loop, per package (the seed playbook, unchanged)

1. `git mv` the package into `clean/` (preserve history).
2. Rewrite the import prefix (`…/internal/PKG` → `…/clean/PKG`), run `goimports -w`.
3. Fix what the wall rejects: pull a small good dependency along, or invert a messy one into a
   **consumer-owned interface** the clean package declares (PRINCIPLES §4).
4. Leave a throwaway adapter in legacy (marked `// RESTRUCTURE-SHIM`) so old callers keep working.
5. Verify the SEAM, not the world: `cd clean && go test ./PKG/...` — seconds.
6. Rewrite in place behind the wall only after it is green and isolated.

### Entry criteria (per port)

- The port's only unported tentacle is already in clean, or is invertible into a port the moved
  package declares. (`structuredlog`, `eventbus`, `queue` each have `core` as their only real
  tentacle — the production vocabulary of task 14 is what unblocks all three, and it unblocks them
  together, not in a chain.)
- `presence` waits for `eventbus`; `dispatch` waits for `queue`.

### Admission-bar checklist (per port — from `01-base-plan.md` §2; a hard-gate failure blocks the move)

- **Hard gate 1 — the wall holds.** `cd clean && go list -deps ./... | grep '^github.com/gregberns/harmonik/'
  | grep -v '^github.com/gregberns/harmonik/clean/'` is empty after the move (the allow-list form of
  Phase 0 step 3, not the blind `internal|cmd` deny-list).
- **Hard gate 2 — no grandfathered lint.** The package lints clean under `clean/.golangci.yml` (empty
  allow list) on entry.
- **Hard gate 3 — pure core, with the adapter-shell exemption.** Grep the moved non-test files for
  `time.Now()`, `exec.Command`, `os.Exit`, `rand.`, `os.Getenv`, and package-level mutable `var`. In
  domain code each hit is threaded through a port (the Port-0 `ClockPort` for the clock), not called
  directly. **The exemption, stated precisely so it is a rule and not a per-review wave-through:** a
  single adapter file whose ENTIRE job is to wrap ONE effect behind the injected port MAY call that one
  effect directly. `substrate`'s `SystemClock.Now() { return time.Now() }` is the canonical case — it
  is the concrete the `ClockPort` injects, and without the exemption Port 0 (the first port) trips the
  gate the design is built around. The exemption covers only the named adapter/effect shell (the clock
  adapter, and each package's `main`-wired effect shell); it does NOT cover domain code, which stays
  effect-free and takes the port. `eventbus` has one `time.Now` to route through the port; the
  `queue.json` two-writer concurrency fix is its own separate bead, not folded into the relocation (see
  the task table).
- **Soft signals scored, not gating** but a bad score is pre-move work: efferent fan-out measured by
  doing the move; integration tests tagged `//go:build integration` and a hermetic unit layer present;
  consumer-owned ports already declared (`queue` is the exemplar — it already declares `QueueSetter`,
  `MutationLocker`, `EventEmitter`, `BeadLedger`); ticket-named tests renamed to name the behavior
  (PRINCIPLES §7).

### Exit criteria (per port)

- The package lives in `clean/`, all three hard gates pass, `cd clean && go test ./PKG/...` is green in
  seconds, and the legacy shim is in place and counted by the size scoreboard.
- `make full` stays green (the wall check and the whole legacy suite still pass through the shim).
- The size scoreboard shows clean lines up and, once callers are rewired, daemon lines starting down.

### Notes on specific ports

- **`queue` is the proving vertical** (PRINCIPLES §9 — prove one vertical, then generalize) and the
  operator's success criterion. Land the relocation FIRST, carrying the known two-writer `queue.json`
  hazard behind the shim unchanged; then fix the hazard in place in clean (one writer, PRINCIPLES §6)
  as its own reviewed bead. Splitting the relocation from the concurrency-semantics change keeps each a
  small reversible commit and keeps review attribution clean — a failure is either the move or the fix,
  never both. Queue clean AND the hazard fixed is the milestone that proves the whole approach.
- **Explicitly late, not in Phase 2:** `runexec` / `runloop` / `run` (the run machine, 17 deps, the
  high-risk keystone), `workspace`, `lifecycle`, `scenario`, and everything in the `daemon` closure.
- **Never as package cuts:** `workloop.go`, `scheduler.go`, `dot_cascade_core.go` — decompose in place
  behind a single writer (two prior programs parked these; the Makefile already carries freeze-gates
  for them).

---

## Track B (parallel) — keeper as its own binary

Goal: move keeper toward a standalone binary (`tools/keeper/` = library + thin `main` + bus/tmux
adapter), on a SEPARATE crew, sequenced so it never blocks the spine and the spine never waits on it.
By operator direction keeper moves early; by evidence keeper is NOT clean today, so it earns its move
by cuts, not by relocation (`01-base-plan.md` §6).

**Honest framing to keep repeating: Track B buys the standalone binary and collision-avoidance, NOT
build speed.** Keeper's afferent fan-in is only 6, so moving it does little for the recompile problem.
Work on keeper must never be justified as "core work"; the core spine must never wait on keeper.

Kerf codename: `codename:keeper-standalone`.

### The smallest first step (do this before any `git mv`)

**Extract the pure `decide` function out of `Watcher.Run`, in place, in legacy.** `Watcher.Run` is a
549-line god-method (cognitive complexity 179) that mixes pure decisions (warn? restart? defer?) with
effects (`exec.Command`, tmux paste), and its action path is untested and non-hermetic. Split it into:

- a pure `decide(gaugeReading, config, clock) → Decision` — values in, values out, unit-testable
  without tmux; and
- a thin `apply(Decision)` shell that runs the effects.

Give `decide` a hermetic unit test that watches a real assertion fail (PRINCIPLES §7 — if you did not
watch it fail, you do not know what it tests). This one change is reversible, de-risks the whole port,
and makes the untested action path testable. It is the highest-value first move and it needs no module
boundary yet.

### Then, in order (each a bead, none blocking the spine)

1. **Sever the `digest` tentacle.** keeper imports `internal/digest`, which alone drags in `crew`,
   `queue`, `dispatch`, `run`, `sentinel`, `eventbus`, `lifecycle/tmux`, `presence` — the whole run
   machine behind one status-summary import, and it fails the wall gate. Invert it: keeper declares a
   small consumer-owned port for the exact status summary it needs; legacy `digest` satisfies it via an
   adapter. Highest-value cut for keeper.
2. **Sever the `dashboard` tentacle.** Invert to a tiny `Notifier` port keeper declares; wire the
   concrete dashboard notifier in `main`.
3. **Account for the `presence` tentacle.** keeper's direct imports are `core`, `dashboard`, `digest`,
   `presence`, `substrate` (verified: `go list -f Imports ./internal/keeper`), and `presence` pulls
   `eventbus` pulls `core`. Severing `digest` and `dashboard` does NOT remove this edge, so keeper's
   clean move still breaches the wall through `presence` until it is handled. Two routes, decide by the
   shape of what keeper actually needs: (a) invert to a small consumer-owned port keeper declares (a
   liveness/presence read) satisfied by legacy `presence` via an adapter — the same move as `digest`;
   or (b) let keeper's move wait on the spine landing `eventbus` (Port 3) then `presence` (Port 5a) and
   import `clean/presence` directly. Route (a) keeps Track B independent of the spine; route (b) trades
   independence for less shim. Prefer (a) unless the presence surface keeper needs is large.
4. **Route direct `time.Now()`** in `gates.go` and `tmuxresolve.go` through the Port-0 clock.
5. **Reduce the `core` import** to the handful of keeper-relevant types, ported into clean via the same
   production vocabulary Track A lands (the vocabulary task, not the throwaway spike). This is the step
   that couples Track B to Track A: keeper's clean move waits on the vocabulary existing, but steps 1–4
   above do not.
6. **Move keeper into `clean/keeper`** and add `tools/keeper/cmd/keeper/main.go` (standalone binary) +
   the `harmonik keeper …` subcommand, both calling the same `keeper.Run(ctx, env)`.

### Track B gate

- Step 0 (`decide` extracted, hermetic test green, watched-to-fail) landed and reviewed. This alone is
  real progress and de-risks everything after it.
- After steps 1–2, `cd clean && go list -deps` on a keeper spike shows the `digest` and `dashboard`
  closures gone.
- After step 3, the `presence` edge is either inverted behind a keeper-declared port (route a) or
  explicitly gated on Ports 3 + 5a (route b) — the keeper spike's dep closure names no unhandled legacy
  edge except the ones the chosen route still expects.
- After step 6, `go build ./tools/keeper/cmd/keeper` produces a standalone binary AND `harmonik keeper`
  runs the same code, both green, wall intact.

---

## Review gates (at each phase boundary and on every commit)

This repo runs a review gate on **every non-trivial commit** (a reviewer node on the only inbound edge
to `close`; `Reviewed-By:` + `Review-Verdict:` trailers; a `BLOCK` verdict never lands). Restructure
work fits that unchanged. On top of the per-commit gate, each phase boundary has a reviewer check tied
to the admission bar.

**Per-commit reviewer, restructure-specific checks (add to the standard review):**

- The wall holds: `cd clean && go list -deps ./...` reaches no legacy package.
- No grandfathered lint crossed: the moved package lints clean under `clean/.golangci.yml`.
- Purity: no direct `time.Now` / `exec.Command` / `os.Exit` / `rand.` / `os.Getenv` / package-global
  mutable state in a moved clean package's DOMAIN code; effects are injected via consumer-owned ports.
  The one allowed exception is the adapter-shell exemption (Phase 2 hard gate 3): a named adapter file
  that wraps ONE effect behind the injected port may call that one effect — the reviewer confirms the
  file does nothing else.
- The seam is an inversion, not a re-coupling: any new interface is consumer-owned (declared by the
  clean package), and the abstraction names what it buys in the commit body (AGENTS.md §Judgment
  calls). A shim exists only where a caller is not yet ported, is marked `// RESTRUCTURE-SHIM`, and is
  counted by the scoreboard.
- Tests moved with the code, run hermetically in seconds in the clean module, and are named after the
  behavior they defend, not a ticket (PRINCIPLES §7).
- The reviewer verifies the working tree it read equals the index being shipped (`git diff` empty for
  the reviewed files) — a review reads the tree, a commit ships the index, nothing makes them agree.

**Phase-boundary review — pass/fail bar:**

- **Phase 0 exit:** all Phase 0 gate bullets hold, including the planted-import wall-breach proof (a
  NON-core prefix) and the legacy-lint-ratchet experiment. FAIL if the wall check is not wired into
  `make full`, or the wall check still enumerates bad prefixes instead of allow-listing `clean/`, or
  the size scoreboard does not print, or clean's `.golangci.yml` has any allow-list entry, or the
  lint-ratchet experiment has not been run and its result recorded (D2 is not resolved by a policy
  sentence).
- **Phase 1 exit:** substrate landed clean; the spike report exists with its four numbers; a
  GREEN/AMBER/RED verdict and (for AMBER/RED) an operator decision are recorded. FAIL if the spine is
  opened before D1 is signed.
- **Phase 2 per-port exit:** all three hard gates pass, the clean fast test is green in seconds,
  `make full` stays green, and the scoreboard moved. FAIL if any hard gate is waved through on "it
  compiles" — the Charter records three deletions that compiled clean and were wrong.
- **Track B step 0 exit:** the `decide` unit test was watched to fail before it passed. FAIL if the
  test's fail was not observed (an unwatched test that cannot fail looks exactly like one that passes).

Reviewer nodes stay on Claude (`cmd/harmonik/substrate_select.go` `reviewerSubstrate`); judgment is the
product there.

---

## Task breakdown (ordered; each becomes a bead)

Size key: S = under a day, M = a day or two, L = several days. Phase in brackets. Dependency by task
number.

| # | Task (one-line outcome) | Size | Phase | Depends on |
|---|---|---|---|---|
| 1 | `clean/go.mod` + `clean/doc.go`: empty clean module builds | S | 0 | — |
| 2 | `go.work` ties root + clean; both build together | S | 0 | 1 |
| 3 | `scripts/clean-wall.sh` allow-list closure check written and passes on empty clean | S | 0 | 2 |
| 4 | Wall check wired into `make full` as a gate | S | 0 | 3 |
| 5 | `clean/.golangci.yml` at full strength, empty allow list | S | 0 | 1 |
| 6 | Size scoreboard (clean lines, daemon lines, shim count) in `make full` | S | 0 | 4 |
| 7 | `//go:build integration` convention + clean fast-loop target documented | S | 0 | 2 |
| 8 | Planted-breach test: plant a NON-core `tools/forbid-import` import, prove the wall gate fails, then remove it | S | 0 | 4 |
| 9 | Legacy-lint-ratchet experiment: `git mv` one daemon-adjacent file out, confirm no new legacy finding fires, revert — **resolves D2** | M | 0 | 5 |
| 10 | **Phase 0 gate review + sign-off** | S | 0 | 4,5,6,7,8,9 |
| 11 | Port 0: move `substrate` into clean; wall holds; fast test green | M | 1 | 10 |
| 12 | Port-1 spike: attempt minimal core-vocab carry, measure the four numbers | M | 1 | 11 |
| 13 | Spike report + GREEN/AMBER/RED verdict; **operator DECISION D1** | S | 1 | 12 |
| 14 | **Land the PRODUCTION clean vocabulary + legacy↔clean adapter as reviewed code; pass all three hard gates** (not the throwaway spike) | M–L | 2 | 13 |
| 15 | Port `structuredlog` into clean (warm-up; only tentacle is core) | M | 2 | 14 |
| 16 | Port `eventbus` into clean; route its one `time.Now` through ClockPort | M | 2 | 14 |
| 17 | Port `queue` into clean — RELOCATION ONLY; carry the two-writer hazard behind the shim | M | 2 | 14 |
| 18 | Fix the two-writer `queue.json` hazard in place in clean (single writer, PRINCIPLES §6) | M | 2 | 17 |
| 19 | Port `presence` into clean | M | 2 | 16 |
| 20 | Port `dispatch` into clean | M | 2 | 17 |
| 21 | Track B step 0: extract pure `decide` from `Watcher.Run` + hermetic watched-to-fail test (legacy, in place) | M | B | — |
| 22 | Track B step 1: sever keeper→`digest` via consumer-owned status port | L | B | 21 |
| 23 | Track B step 2: sever keeper→`dashboard` via `Notifier` port | M | B | 21 |
| 24 | Track B step 3: account for keeper→`presence` — invert to a keeper port (route a) or gate on Ports `eventbus`+`presence` (route b) | M | B | 21 (route b also 16,19) |
| 25 | Track B step 4: route keeper `time.Now` through ClockPort | S | B | 11,21 |
| 26 | Track B step 5: reduce keeper `core` import via the PRODUCTION vocabulary (task 14) | M | B | 14,22,23 |
| 27 | Track B step 6: `clean/keeper` + `tools/keeper/cmd/keeper` + `harmonik keeper` subcommand | L | B | 24,25,26 |

**On the DAG shape (un-serialized on purpose).** Tasks 15, 16, 17 each have `core` as their only
internal tentacle (verified) — there is NO import edge between them, so they are NOT a chain; all three
depend only on the production vocabulary (task 14) and run in parallel. This matters because throughput
is the program's #1 risk (§Kill/stop): a fleet that closed 7 beads in 5 weeks must not spend its
scarcest resource on sequencing the import graph does not require. `presence` (19) follows `eventbus`
(16); `dispatch` (20) follows `queue` relocation (17); the `queue.json` concurrency fix (18) is its own
bead after the relocation, never folded into it.

Tasks 21–27 are Track B and run on a SEPARATE crew, in parallel with 14–20. **Task 21 depends on
nothing in the clean module** — it is pure in-place legacy refactoring the plan calls the
"highest-value first move", so it starts at once and is not gated behind the Phase-0 scaffold it never
touches. Tasks 22, 23, 24 (route a), 25 also do not wait on the spine and can start right after task
21. Track B couples to Track A only at task 26, which needs the production vocabulary (task 14).

---

## Kill / stop criteria and honest risks

**When to pause and re-check with the operator:**

- **RED on the Port-1 spike (task 13 verdict).** If the minimal vocabulary drags a type family or a
  large fraction of `internal/core`, STOP. Do not push the spine forward on a vocabulary that is not
  minimal — and note the production-vocabulary task (task 14) has nowhere to land until this clears.
  The finding reshapes the plan; bring the reshape options to the operator (DECISION D1).
- **The legacy lint ratchet — RESOLVED BY EXPERIMENT, not by policy (risk 2).** The legacy side's ~248
  daemon allow-list pairs may fire when a file moves out. This was the base plan's one unresolved hard
  blocker. It is now settled the only way a mechanical fact can be: **task 9 (a Phase-0 experiment)
  moves one daemon-adjacent file, runs `make full`, and confirms no new legacy finding fires in the
  files left behind.** If task 9 is green, the ratchet does not block; if it is red, the first
  daemon-adjacent port is blocked and the operator decides the remedy on real evidence (freeze the
  allow list, refactor the suppression keys, or reshape the port) — a policy sentence is not a
  substitute for the measurement. Pair whichever remedy with the scoreboard so legacy debt stays
  visibly monotonic-down.
- **Throughput does not appear (risk 3).** The decomposition program closed 7 beads in 5 weeks (codex 0
  of 29, pi 1 of 41). This plan assumes a Codex/Pi crew does the porting. **Prove one port end-to-end
  with the intended harness (task 11 or 15) before scheduling the rest.** If the first real port does
  not land through the intended crew, pause and re-plan capacity with the operator rather than queueing
  the whole port list against throughput the fleet has not shown.
- **Adapter rot (risk 4).** Every inverted seam leaves a `// RESTRUCTURE-SHIM` in legacy. A shim that
  never gets deleted becomes permanent. The scoreboard counts shims; a rising shim count with flat
  daemon lines is the signal to stop porting and start deleting.

**The caveat, restated so no one is surprised:** this restructure does not speed up the slow
integration tests — 617 of 815 daemon test CPU-seconds are 93%-idle whole-work-loop tests, and
splitting code moves their location, not their duration. The win is the fast per-package clean loop and
a small, wall-protected blast radius. Two orthogonal quick wins (a change-scoped gate for the 28% of
commits that touch no `.go` file, and stopping the reap of the cold Go build cache) are cheap and do
not wait on this program — take them separately.

**Re-derive every number before acting on it** (the Charter's standing rule; estimates in this program
have missed by 5× in both directions).
