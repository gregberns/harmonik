# 01 — Problem Space: `run-state-machine` (code-revamp M3)

> **Pass 1 (Problem Space).** Autonomous authoring, signoffs waived (2026-07-13).
> Design pass is intentionally **deferred** — this work stops after Decompose
> (Pass 2). P1 (`session-restart-substrate`) must prove the reactor seam on the
> keeper first; M3 mirrors that proven shape on the daemon, so its *design* should
> follow the landed keeper template rather than invent a parallel one.
>
> Sources (all `file:line` verified against the working tree, 2026-07-13):
> `plans/2026-07-13-code-revamp/research/01-daemon-godfunction.md` (primary dossier),
> `plans/2026-07-13-code-revamp/PLAN.md` §2/§4, `plans/2026-07-13-code-revamp/ROADMAP.md`,
> `plans/2026-07-12-codebase-census/{REPORT,PLAN}.md`.

---

## 1. Summary — what is changing and why

The daemon's per-bead run lifecycle is a single ~2,366-line god-function,
`beadRunOne` (`internal/daemon/workloop.go:3072`–`:5438`), with **17 parameters**
including a `runSucceeded *bool` out-param (`:3072`, written by the `emitDone`
closure at `:3120`). It takes a **85-field** god-struct `workLoopDeps`
(`workloop.go:182`–`:942`) **by value** — copying the struct on every dispatch,
though its pointer/map/mutex/chan fields alias shared cross-goroutine state
(dossier §2.2). There is **no explicit state enum** for a run; the lifecycle is
spread across four separate representations (dossier §4): a formal
`hclifecycle.Machine` driven only through three terminal states
(`transitionToTerminated`, `workloop.go:7994`), the observable pre-exec event
chain, the Step-9 terminal `switch` (`:5230`), and the review-loop iteration
state (`reviewloop.go:135`). The launch→gate→merge→close spine is **open-coded 4×**
(dossier §6): the merge/close block is re-derived at the review-loop region
(`:3904`/`:4123`), the agent_completed branch (`:5252`), the exit-0 auto-close
branch (`:5302`), and the shutdown-drain branch (`:5374`) — each a near-duplicate
of scenario-gate → `preMergeSync` → `lockedMergeRunBranchToMain` → close-or-reopen.

Independently, a single global `mergeMu` (`workloop.go:384`) is held **across
build + network + git IO**: `lockedMergeRunBranchToMain` (`:6512`) takes the lock
and holds it (via `defer`) for the entire `mergeRunBranchToMain` (`:6544`) —
`git rebase` → `go build`/`go vet` → fmt-check → `git push origin` → `git reset
--hard` → `br sync`, inside a 3-attempt retry loop (dossier §3.1). The same lock
also serialises remote `fetchBaseOnWorker` + `git worktree add` (`:3623` region)
and the post-run escape-worktree check (`:5162`). Consequence: **at most one bead
across ALL queues** can be in any of those phases at once. (`cacheReapMu`,
`workloop.go:894`, is an analogous over-broad lock — a W-lock held across
`go clean -cache` for up to 5 min — but is out of M3's first-slice scope.)

Absorbed into this work is the **resume-hang** defect (census REPORT §4 Addendum,
"Second exemplar"): a run completes its first implement, then goes dead-silent at
`implementer_resumed` — no output, no heartbeat, no `run_stale` — so `in_progress`
never resolves. The hang lives on the review-loop relaunch branch
(`reviewloop.go:250`, iteration ≥ 2 at `:252`), today caulked only by a fixed
2-second grace fallback (`resumeReadyFallbackGrace`, `reviewloop.go:110`). Because
this bug lives *inside* the exact function M3 rewrites, ROADMAP folds the census
STEP-0a fix into M3 as a **required bounded-liveness invariant** of the new state
machine — the daemon analog of the keeper's SR9 (`specs/session-keeper.md`
SK-INV-005 / SK-015), which is already landed and proven-shaped on P1.

The change: extract `beadRunOne` into an explicit **`runexec` state machine** with
named states and a single factored terminal spine, and split the overloaded
`mergeMu` into an explicit **merge queue** that serialises only the true merge
critical section (never held across the build / push / SSH IO). This is the **first
slice** of the daemon breakup, done behind the same functional-core/reactor seam
that P1 has landed on the keeper (`internal/substrate`, `internal/codexreactor`).

## 2. Goals (what should be true of the system after M3)

- A run's lifecycle is expressed as an **explicit state machine with named states**
  (claim → worktree → launch → monitor → gate → merge → close), replacing the four
  scattered implicit representations. The terminal decision is one factored spine,
  not four open-coded copies.
- `beadRunOne` becomes a **thin driver** over that machine: no 17-param signature,
  no `*bool` out-param, no mutable closure flags threaded across ~2,366 lines.
- The **resume-hang cannot recur silently**: the resume/relaunch branch is a state
  whose every timer edge lands in a state with an outgoing action, so a resumed run
  reaches a terminal outcome or emits a failure signal within a bounded window —
  never dead silence (bounded liveness, the daemon SR9 analog).
- `mergeMu` is replaced by an **explicit merge queue** that holds a lock only over
  the ref-update critical section; `git rebase`, `go build`/`vet`, `git push`,
  `git reset`, and `br sync` no longer run under a global daemon-wide lock.
- The new code lives behind **depguard boundaries** (a `runexec` / `merge`
  sub-package edge) and reuses the **proven `internal/substrate` seam**
  (`ClockPort`, `EventSource`/`Effector`/`Run`) rather than a parallel invention.

## 3. Non-goals (explicit scope boundaries)

- **NOT the full daemon god-package breakup.** The ≥8-subsystem decomposition
  (orchestrator / policy / agentrunner / hook / memory / improvement / adapters)
  is **M5 — daemon-decompose** (ROADMAP "Orphan homing"). M3 is the *first slice*;
  it extracts the run-lifecycle + merge boundary only. It does **not** relocate the
  85-field `workLoopDeps` grab-bag wholesale, nor carve the other subsystems out of
  `internal/daemon`.
- **NOT the agent-input rebuild.** Replacing tmux paste-injection with a structured
  protocol driver is **M2 — agent-input-substrate**. M3 consumes whatever
  `handler.Substrate` the run launches through; it does not change the input channel.
- **NOT the remote rebuild.** Worker-resident agent + real protocol is **M4 —
  remote-substrate**. M3-phase-1's merge queue is a *hard prerequisite* of M4, but
  M3 does not extract the Local/Remote execution interface (M4 owns that; 13/15
  `runner`-nil sites live outside `beadRunOne` — census PLAN M4).
- **NOT the `cacheReapMu` rework** and NOT the test-theater deletion (M1). The
  `cacheReapMu` over-broad W-lock is noted but not carved here.
- **NOT a rewrite.** The ~466-file incident-pinned regression net is the thing that
  makes this a refactor; M3 preserves observable dispatch behavior (see §5).
- **NOT re-opening any of the ten locked decisions** (STATUS.md, 2026-04-19).

## 4. Constraints

- **Regression net stays green throughout.** The ~466 incident-pinned tests
  (census REPORT §1) are the correctness envelope; every M3 increment is green
  per-commit. Small state-transition-at-a-time PRs, not a big-bang cut-over.
- **Single-writer on daemon core.** `beadRunOne`/`workloop.go` is where ~80% of fix
  commits historically land (census REPORT §3); M3 is single-owner — no parallel
  agents inside the daemon core while it is being carved (census PLAN Global
  constraints + M3 Key risk).
- **Observable dispatch behavior must not change** — same events, same bead
  transitions, same terminal outcomes — **except** the resume-hang fix (a run that
  used to hang now terminates or emits failure). This is the daemon analog of P1's
  "reproduce-the-freeze first" parity constraint (D11): the extraction is
  behavior-preserving; the *only* permitted divergence is the SR9 liveness fix.
- **Out-of-pipeline.** The daemon is stopped for the rebuild (ROADMAP "Operating
  fact"); M3 is done via reviewed merges to a branch, not dispatched through the
  daemon. Daemon returns to service only after the acceptance oracle passes offline.
- **The daemon owns terminal bead transitions.** The state machine still routes
  close/reopen through the daemon-owned ledger writes (`ReopenBead` /
  `closeBeadWithHistoryTrim` — 50 + 27 sites in the current body, dossier §1.2);
  the extraction factors them, it does not move ownership.
- **Reuse the landed seam, don't fork it.** `internal/substrate` (`seam.go`,
  `clock.go`) is landed and green on codex + keeper; M3 instantiates it, and any
  genericization gap it hits is a substrate change, not a daemon-local copy.
- **Gated on the M1→M3 coverage audit** (census PLAN): the state-machine path must
  meet a measured coverage floor from tests that actually `exec` product code —
  "466 tests green" is not sufficient if the real coverage lived in pruned files.

## 5. Success criteria (concrete statements about the resulting system)

1. **An explicit `runexec` state machine with named states replaces the 4×
   open-coded lifecycle.** The launch→gate→merge→close terminal logic exists once
   (a factored transition spine), not as four near-duplicate blocks at
   `workloop.go:3904`/`4123`, `:5252`, `:5302`, `:5374`.
2. **`beadRunOne` is reduced to a thin driver** over the machine — a hard line
   ceiling asserted (e.g. ≤ N lines, dispatch delegated to transitions), the 17-param
   signature and the `runSucceeded *bool` out-param eliminated (census PLAN M3 DoD 3).
3. **The resume-hang is expressed as a bounded-liveness invariant with a property
   test.** Every resumed/relaunched run reaches a terminal outcome (or emits a
   `run_stale`/failure-class signal) within a bounded window; a fault-injection test
   (stalled agent on relaunch) asserts a terminal signal, never silence — mirroring
   `specs/session-keeper.md` SK-INV-005 (SR9).
4. **`mergeMu` is replaced by an explicit merge queue serializing only the merge
   critical section.** A static/lint or test **fails if a lock is held across an IO
   call** in the merge path (census PLAN M3 DoD 2); `git push`/`go build`/`br sync`
   run outside any global daemon lock.
5. **`runexec` (and `merge`) are sub-packages with depguard entries in
   `.golangci.yml`** so run-lifecycle logic cannot leak back into flat `daemon`
   (census PLAN M3 DoD 1).
6. **Every hk- regression test is green throughout**, and the state-machine path
   meets the measured coverage floor from the M1→M3 audit.

## 6. Preliminary affected spec / code areas

- **Code (primary):** `internal/daemon/workloop.go` — `beadRunOne` (`:3072`–`:5438`),
  `workLoopDeps` (`:182`–`:942`), `newWorkLoopDeps` (`:1043`), the merge wrappers
  `lockedMergeRunBranchToMain` (`:6512`) / `mergeRunBranchToMain` (`:6544`),
  `transitionToTerminated` (`:7994`); the goroutine wrapper that calls `beadRunOne`
  (`:3029`).
- **Code (adjacent lifecycle):** `internal/daemon/reviewloop.go` (the resume branch
  `:250`/`:252`, `reviewLoopState` `:135`, `resumeReadyFallbackGrace` `:110`) and
  `internal/daemon/dot_cascade.go` (the DOT mode-dispatch branch), both of which the
  Step-9 terminal spine feeds.
- **Seam reuse:** `internal/substrate` (`seam.go` `EventSource`/`Effector`/`Run`,
  `clock.go` `ClockPort`) — the daemon has **26 `time.Now()` sites in workloop.go**
  and no `ClockPort` today; a daemon `ClockPort` seam is a prerequisite for the
  liveness property test. `internal/replay` (the SR-invariant harness landed by P1
  T4) is the likely home for the bounded-liveness checker.
- **Specs:** a new normative home for the run-lifecycle state machine + merge-queue
  contract (working name `specs/run-state-machine.md`, prefix TBD at design) — the
  daemon has no such spec today. The bounded-liveness invariant is specced there as
  the daemon peer of `specs/session-keeper.md` SR9. Event-model touch is minimal
  (the terminal events already exist; the run may gain a durable liveness/failure
  event — a design-pass question).
- **Enforcement:** `.golangci.yml` (new depguard edges for `runexec`/`merge`; the
  complexity-linter ratchet from Track C, which caps the extracted functions).

## 7. Relationship to other works

- **Builds on P1 — `session-restart-substrate` (the proven seam).** P1 has **landed**
  (commits `a8035fac` T1 … `14bac10b` T6 on this branch): `internal/substrate` (the
  generic `EventSource[E]`/`Effector[A]`/`Run` + `ClockPort`), `internal/codexreactor`
  re-instantiated on it via type aliases (T3), the keeper's `Cycler` moved onto
  `ClockPort` + named ports (T5/T6), the `internal/replay` invariant harness (T4), and
  the **RS + SK specs** (`specs/replay-substrate.md`, `specs/session-keeper.md`) with
  SR3/SR4/SR6/SR7/SR9 as normative per-cycle invariants. M3 is the **second production
  instantiation** of that seam and the **daemon peer** of the keeper reactor: SK-INV-005
  (SR9, keeper bounded liveness) is the exact template for M3's resume-hang invariant.
  *This is why M3's design is deferred until P1 proves the shape — M3 should follow the
  landed keeper template, not a parallel design.*
- **M4 — remote-substrate depends on M3.** M4 hard-depends on M3-phase-1's merge queue
  (remote merges must thread the queue, so `mergeMu` must stop being held over network
  IO first — ROADMAP, census PLAN M4 dependency (a)). M4 owns its own Local/Remote
  execution-interface extraction; M3 does not hand it over.
- **M5 — daemon-decompose: M3 is its first slice.** The full god-package breakup is
  M5 and is explicitly held (ROADMAP "Hold"); speccing it now designs against a moving
  target. M3 carves run-lifecycle + merge only.
- **Track B / STEP-0 overlap.** STEP-0a (resume-hang) is **absorbed here** (not a
  standalone fix — ROADMAP resolution). STEP-0b (noChange false-close) and the
  queue.json two-writer fix are seam-independent data-integrity fixes done elsewhere;
  they touch the same file region (the noChange terminal branch, `workloop.go:5331`)
  so M3 must reconcile with whatever landed there. STEP-0c (honest-probe in
  `createworktree.go`) is carried by M4, not M3.
- **Parallelism note.** M3-phase-1 (the `mergeMu` → merge-queue split) can start as
  soon as P1 is proven + M1 lands; it does **not** need M2 (ROADMAP "the one
  legitimate parallelism"). The full `beadRunOne` extraction follows M2.
- **No overlap with keeper-redesign / stall-sentinel / reap.** Searched: the keeper
  redesign is P1 (different package, depguard-forbidden from importing daemon); the
  sentinel governor and `cacheReapMu` reaper are daemon subsystems M3 does not touch
  (M5 territory). No net-new codename collides with `run-state-machine`.
