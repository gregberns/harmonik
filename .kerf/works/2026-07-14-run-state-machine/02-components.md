# 02 — Decompose: `run-state-machine` (code-revamp M3)

> **Pass 2 (Decompose).** Autonomous authoring, signoffs waived (2026-07-13).
> **The design pass (Pass 3+) is DEFERRED** — P1 must prove the reactor seam
> generalizes before M3's design is settled. This doc breaks M3 into components,
> gives each a separability rationale + rough dependency order + the open questions
> the deferred design pass must resolve. It does not settle those questions.
>
> Facts are `file:line`-verified against the working tree (2026-07-13). Reads
> `01-problem-space.md`, dossier `research/01-daemon-godfunction.md`, and the landed
> P1 design (`.kerf/works/session-restart-substrate/04-design/00-decisions.md`) as
> the template M3 mirrors.

---

## Component map (overview)

M3 breaks into **six components** in two phases. Phase 1 (merge boundary) is the
`mergeMu` split and can begin as soon as P1 is proven + M1 lands — it does not need
M2 (ROADMAP parallelism note). Phase 2 (the run reactor) is the `beadRunOne`
extraction proper and follows M2 + the M1→M3 coverage audit.

| # | Component | Phase | Depends on | Mirrors (P1 template) |
|---|-----------|-------|-----------|------------------------|
| C1 | `ClockPort` in the daemon | 1 | substrate seam (landed) | D4 (keeper ClockPort) |
| C2 | Explicit **merge queue** (mergeMu split) | 1 | C1 | — (daemon-specific) |
| C3 | **`workLoopDeps` decomposition** into ports | 2 | — (independent start) | D10 (keeper 5 ports) |
| C4 | The **`runexec` reactor** (`Step` state machine) | 2 | C3, C1 | D11 (keeper Step reactor) |
| C5 | **Resume-hang bounded-liveness** invariant + property test | 2 | C4, C1 | D12/SR9 (SK-INV-005) |
| C6 | **Terminal-spine factoring** (the 4× merge/close block) | 2 | C4, C2 | — (daemon-specific) |

---

## C1 — `ClockPort` in the daemon

**What it is.** Thread `substrate.ClockPort` (`internal/substrate/clock.go:10` —
`Now`/`Since`/`NewTicker`/`Sleep`) through the run-lifecycle core, replacing the
**26 direct `time.Now()` sites in `workloop.go`** and the wall-clock timeouts
(`agentReadyTimeout`, `postAgentReadyHangTimeout`, the `noChangeTimeoutCh` at
`workloop.go:4932`, the resume grace at `reviewloop.go:110`). The daemon uses
`SystemClock` in production and a `FakeClock` (`substrate/fakeclock.go`) in tests.

**Why separable.** It is a mechanical, behavior-preserving substitution with its
own regression surface, and it is the **prerequisite for C5** (a bounded-liveness
property test is impossible to write deterministically while timeouts read the wall
clock). It reuses a landed, green port — zero new abstraction. It can land ahead of
the reactor extraction and immediately de-risks the timeout tests.

**Dependency order.** Phase 1, alongside/just before C2. No dependency on C3/C4.

**Open questions for design.** (a) Does the daemon need `ClockPort` as a
`workLoopDeps` field now, or only where the reactor consumes it after C3? (b) The
existing `daemonTestHooks` clock overrides (if any) vs the fake — reconcile to one
seam. (c) Which timeouts become reactor *timer events* (C4's `ArmTimer`/`TimerFired`
shape, mirroring D11) vs plain `Clock.Since` interval gates.

## C2 — Explicit merge queue (the `mergeMu` split)

**What it is.** Replace the single global `mergeMu` (`workloop.go:384`) with an
explicit merge queue that serialises **only the ref-update critical section**, so
`git rebase` → `go build`/`vet` → fmt-check → `git push origin` → `git reset` →
`br sync` (all inside `mergeRunBranchToMain`, `workloop.go:6544`–, dossier §3.1) no
longer run while a daemon-wide lock is held. The five current merge call-sites
(`lockedMergeRunBranchToMain` at `workloop.go:3904`, `:4123`, `:5252`, `:5302`,
`:5374`) submit to the queue instead of taking the mutex directly. The queue also
absorbs the two non-merge uses of `mergeMu` — the remote `fetchBaseOnWorker` +
`git worktree add` serialisation (`:3623` region) and the escape-worktree check
(`:5162`) — deciding for each whether it needs the merge lock at all or a narrower one.

**Why separable.** This is the census-named **M3-phase-1**: it is the one part of
M3 that can start before M2 and is a **hard prerequisite for M4** (remote merges
must thread the queue — census PLAN M4 dependency (a)). It has a crisp, mechanically
checkable DoD (a lint/test that fails if a lock is held across an IO call — census
PLAN M3 DoD 2) independent of the larger reactor extraction. Note `worktreeCreateMu`
(`workloop.go:398`, hk-5qp7z) is *already* a separate lock for the remote-create
retry loop — so the create/merge split is partially begun; C2 finishes it for the
merge path.

**Dependency order.** Phase 1, after C1 (so timeouts inside the queue are fakeable).

**Open questions for design.** (a) Queue shape — a serialising goroutine + channel
(the queue idiom, `internal/queue`), or a bounded worker with an ordered submission
log? (b) What is the *true* critical section — is it only the `git update-ref`
(`:6821`) + rollbacks, with rebase/build/push done speculatively outside the lock and
re-validated under it, or the whole FF-check→update-ref window? (c) Fairness/ordering
guarantees across queues (does queue A's merge starve queue B?). (d) How the escape
check (`:5162`, hk-zguy6 — "sibling's update-ref→reset-hard cannot race") is preserved
once the lock is narrowed. (e) Interaction with `br sync --import-only` (`:7056`) and
the `git reset --hard` working-tree refresh (`:7022`) — do these belong in or out of
the critical section?

## C3 — `workLoopDeps` decomposition into ports

**What it is.** Break the **85-field** `workLoopDeps` (`workloop.go:182`–`:942`,
passed by value into `beadRunOne`) into the *minimal* consumer-owned ports the run
reactor actually needs — the `internal/queue` / keeper-D10 idiom (promote fields to
named narrow interfaces the daemon shell satisfies structurally). Candidate port
groupings drawn from dossier §2.1: a **LedgerPort** (`brAdapter`, close/reopen,
`staleBlockerCloser`), an **EmitterPort** (`bus`), a **WorktreePort**
(`worktreeFactory`, `fetchBaseOnWorker`), a **MergePort** (C2's queue), a
**LaunchPort** (`launchSpecBuilder`, `substrate`, `harnessRegistry`), a **ClockPort**
(C1), plus the concurrency-gating handles (`runRegistry`, `localInFlight`,
`agentSpawnSem`) that must stay shared-by-reference.

**Why separable.** It is the boundary-drawing that makes C4 possible (a pure `Step`
cannot take an 85-field by-value bundle) and it is independently reviewable: each
port extraction is a small, green, mechanical promotion. Critically it is scoped —
M3 promotes only the fields the *run lifecycle* touches; the rest of `workLoopDeps`
stays as-is for M5 (this is the seam between M3-the-slice and M5-the-full-breakup).

**Dependency order.** Phase 2. Can start independently of C1/C2 (parallel port
survey), but must land before C4 consumes the ports.

**Open questions for design.** (a) Which fields are genuinely run-lifecycle vs which
belong to sibling subsystems (M5) — the exact cut line. (b) The four periodic-
maintenance value fields (`lastCoordinatorReap`/`lastDiskCheck`/`lastGoCacheClean`/
`diskLow`, `:826`+) are work-loop-goroutine-owned and *not* seen by dispatch
goroutines (dossier §2.2) — they stay out of the run ports entirely; confirm. (c)
Do the shared mutables (`runRegistry`, `localInFlight`) become explicit port methods
or stay raw shared handles? (d) Whether the nil-means-disabled fallbacks
(`handlerPauseController`, `operatorPauseCtrl`, `decisionBlocker` — dossier §2.2)
become explicit optional ports or default no-op implementations.

## C4 — The `runexec` reactor (the `Step` state machine)

**What it is.** Recast the run as a pure `Step(state, event) → (state, []action)`
machine in a new `internal/daemon/runexec` sub-package, mirroring
`internal/codexreactor` (`reactor.go:193` `Step`, driven by `substrate.Run`). The
implicit lifecycle spread across four representations (dossier §4) becomes **explicit
named states**: `Claiming → ResolvingRun → WorktreeReady → Launching → Monitoring →
Gating → Merging → {Closed | Reopened}`, with the workflow-mode fork (ReviewLoop
`:3825`, Dot `:4014`, single `:4221`) as an early branch. Following D11 (the keeper
template), **timers become events**: the reactor emits `ArmTimer`/`CancelTimer`
actions and consumes `TimerFired`, and the shell owns `ClockPort` — dissolving the
blocking waits (`waitAgentReady` `:4798`, the ProcessExit fallbacks `:5066`,
`noChangeTimeoutCh` `:5331`) into replayable interleavings. `beadRunOne` shrinks to
a thin shell that runs the reactor and executes its actions via an `Effector`.

**Why separable.** This is the heart of the extraction and the largest blast radius
— it must be its own component so it can be built state-by-state, each increment
green (census PLAN M3 risk mitigation: "small state-transition-at-a-time PRs"). It
depends on C3 (ports) and C1 (clock as events) being in place, so isolating it keeps
those prerequisites clean.

**Dependency order.** Phase 2, after C3 + C1. Built incrementally; C6 (terminal
spine) and C5 (liveness) attach to it.

**Open questions for design.** (a) The exact state set and transition table —
especially how the review-loop iteration state (`reviewLoopState`, `reviewloop.go:135`,
`iterationCount` 108×) and the DOT cascade fold into (or nest under) the run states.
(b) "Reproduce-the-behavior-first" parity (the daemon analog of D11's InCycle
freeze): the extraction must not change observable event streams except the SR9 fix —
how is that parity asserted (an old-vs-new differential over recorded runs, à la
D13)? (c) Whether the `hclifecycle.Machine` (`:7994`, the formal 3-state terminal
machine) is subsumed by the reactor states or kept as a downstream projection. (d)
The `agentSpawnSem` acquire (`:4612`) and other concurrency gates — do they become
reactor actions or stay shell concerns? (e) Genericization gaps in
`substrate.EventSource`/`Effector` that the daemon (unlike codex/keeper) exposes —
report upstream to the substrate seam, don't fork.

## C5 — Resume-hang bounded-liveness invariant + property test

**What it is.** Express the census resume-hang (REPORT §4 Addendum) as a normative
**bounded-liveness invariant** on the reactor — the daemon peer of
`specs/session-keeper.md` SK-INV-005 / SK-015 (SR9): every run that reaches the
resume/relaunch state (today `reviewloop.go:250`, iteration ≥ 2 at `:252`) MUST reach
exactly one terminal outcome, or emit a `run_stale`/failure-class signal, within a
bounded window — silence is forbidden. Structurally (per SK-015) every `TimerFired`
edge must land in a state with an outgoing action, so the machine cannot wedge. The
fixed 2-second `resumeReadyFallbackGrace` caulk (`reviewloop.go:110`) is replaced by
this invariant. A **property/fault-injection test** (stalled agent on relaunch,
driven over `FakeClock`) asserts the terminal-or-stale signal — the census PLAN M3
DoD's "single deterministic fault-injection test + N=10 clean relaunch cycles."

**Why separable.** It is the *correctness deliverable* of M3 (the fix the census
called STEP-0a) as opposed to the *structural* extraction — it has its own spec
statement, its own test, and its own acceptance bar. It also validates C4 (a machine
that can't express bounded liveness is the wrong machine). Likely home: the invariant
checker rides the `internal/replay` harness P1 landed (T4).

**Dependency order.** Phase 2, after C4 + C1. The invariant is the last thing that
proves the extraction was worth it.

**Open questions for design.** (a) The exact bounded window (the keeper uses
`HandoffTimeout + model_done_timeout + ClearConfirmBackstop` ≈ 520s; the daemon's is
`agentReadyTimeout + postAgentReadyHangTimeout + ...` — derive it). (b) Whether a new
durable event is needed to make the resume boundary replayable (the keeper added four
interior events; the daemon may need a `run_resumed`/`run_liveness_timeout` durable
event — check what already exists on the bus). (c) Whether the fault-injection test
uses the substrate `Twin` fault modes (`FaultStall`) over a recorded run corpus, and
whether such a corpus must be built (the keeper had to build one — D13). (d) Fail-open
vs fail-closed on the timeout (the keeper's model-done bound is fail-open; the
daemon's resume bound should probably reopen/stale, not silently proceed).

## C6 — Terminal-spine factoring (the 4× merge/close block)

**What it is.** Collapse the **four near-identical** launch→gate→merge→close blocks
(the review-loop region `workloop.go:3904`/`:4123`, agent_completed `:5252`, exit-0
auto-close `:5302`, shutdown-drain `:5374` — dossier §6) into **one factored terminal
spine**: scenario-gate → `preMergeSync` → merge-queue submit (C2) → close-or-reopen.
This also unifies the 50 `ReopenBead` / 27 `CloseBead` / 45 `emitDone` open-coded
call-sites (dossier §1.2) behind the reactor's terminal transitions, eliminating the
`runSucceeded *bool` out-param (`workloop.go:3120`) — success becomes a terminal
state, read by the shell after the reactor returns.

**Why separable.** It is the visible payoff (four copies → one) and the DoD's "thin
driver" criterion, but it depends on both C4 (the states to attach to) and C2 (the
merge queue the spine submits to), so it is naturally the last structural component.
Isolating it keeps the earlier components from having to also de-duplicate.

**Dependency order.** Phase 2, after C4 + C2.

**Open questions for design.** (a) Is the exit-0 auto-close branch (`:5277`, a
"byte-for-byte duplicate" per dossier §1.4) genuinely redundant or does it carry a
distinct pre-bridge semantic that must survive? (b) How the shutdown-drain merge
(`:5374`, run under `bgCtx` not the per-run ctx) differs and whether it stays a
distinct terminal transition. (c) The `emitDone` closure's captured state
(`runTipSHA`, `owningEpicID`, session-data goroutine — dossier §5) — which becomes
reactor state vs shell effect.

---

## Rough build order (summary)

```
Phase 1 (can start after P1 proven + M1 lands; no M2 needed):
  C1  ClockPort in daemon
  C2  merge queue (mergeMu split)          ← hard prereq for M4

Phase 2 (after M2 + M1→M3 coverage audit):
  C3  workLoopDeps → ports  ──┐
  C4  runexec Step reactor  ──┴─ then ─→  C6  terminal-spine factoring
                                    └────→  C5  bounded-liveness invariant + test
```

## Goal → component traceability (from `01-problem-space.md` §2)

| Goal | Components |
|---|---|
| Explicit named-state machine | C4 (+ C6 terminal states) |
| `beadRunOne` a thin driver | C3, C4, C6 |
| Resume-hang cannot recur silently | C5 (+ C1, C4) |
| `mergeMu` → explicit merge queue | C2 |
| Behind depguard, reuse landed seam | C1 (seam), C3/C4 (sub-package + depguard) |

## Deferred to the (held) design pass

Every "open question" above is a **design-pass** input, not resolved here. The design
pass is intentionally held until P1's keeper reactor is proven end-to-end (ROADMAP
"Hold their design passes until P1 proves the reactor method generalizes"), because
M3 mirrors D10/D11/D12 and should follow the landed keeper template
(`specs/session-keeper.md`, `.kerf/works/session-restart-substrate/04-design/`) rather
than invent a parallel design. **This work stops at the Decompose boundary.**
