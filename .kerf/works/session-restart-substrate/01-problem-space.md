# 01 — Problem Space

> **Pass 1 of the spec jig.** Authored 2026-07-13 by admiral with the operator's full context
> (PLAN.md + six research dossiers, all cross-referenced below). **The operator has explicitly
> signed off on this work proceeding through ALL passes solo — signoffs are WAIVED.** Do not
> pause to ask the operator for pass-gate confirmation; proceed pass-to-pass, using judgment,
> and surface only a genuine reversal-of-locked-decision or destructive op. This waiver is
> operator-directed (2026-07-13) and applies to this work only.

## Source material (read these first — this doc distills them, it does not replace them)
- `plans/2026-07-13-code-revamp/PLAN.md` — the structured revamp plan; this work IS its Phase 1.
- `plans/2026-07-13-code-revamp/research/0{1..6}-*.md` — the six dossiers (real `file:line` facts).
- `plans/2026-07-12-codebase-census/REPORT.md` + `generative-system-exploration/4_synthesis.md`.

## What is changing, and why

Harmonik's core "kind of holds together but isn't rock-solid." The census diagnosis: the domain
logic is recoverable, but two architectural decisions were never made (no internal daemon
boundaries; ack-free IO channels), and "green means nothing" because much of the test mass never
executes the product and the close/reconcile path can fabricate done-status. The operator's cure
is a **quality mechanism, not a rewrite**: put a subsystem behind a *stable event + swappable-port
interface*, record real event streams, **replay them offline**, and assert invariants with
**property tests** — so any layer can be swapped for a testable fake and correctness is proven
against recorded reality, not hoped for.

That mechanism is **already built and green** for one subsystem: the codex app-server stack
(`internal/apptap`, `internal/codexwire`, `internal/codexreactor`+`fake.go`,
`internal/codexdigitaltwin`, `internal/codextest` L0–L3). This work **generalizes that proven
pattern into a reusable `internal/substrate` seam and proves the generalization by rebuilding a
second, non-codex vertical behind it: the session-restart (keeper) process.**

Session-restart is the chosen first vertical because it is the only candidate that is (a)
self-contained — `internal/keeper`, which depguard already forbids from importing the daemon;
(b) already partly event-sourced (cycle-boundary events exist); and (c) home to a **real,
currently-unenforced correctness gap**: the load-bearing ordering invariant "never send `/clear`
before the model is done" (SR4) has **no interior implementation today** (dossier 02 §1). So the
proof is simultaneously a real fix, not a throwaway exercise.

## Goals — what is true about the system after this work

1. **A generic `internal/substrate` package exists** owning the reusable record→replay spine
   (dossier 03 §8): the stdio-capture tee (`Tap`), the `EventSource`/`Effector` interfaces + the
   `Run` driver loop, the `FakeEffector`/`SyntheticSource` test doubles, the `Twin` replay engine +
   fault-injection model, and the L0–L3 test-taxonomy + Makefile policy.
2. **Codex is re-instantiated on `internal/substrate`** (proving the extraction is truly generic,
   not a codex-shaped copy) and stays green.
3. **The session-restart vertical is rebuilt behind the seam**: a `ClockPort` replacing the 32
   direct clock sites in `cycle.go` and the wall-clock sleeps in `injector.go`; the tmux/gauge/
   handoff/emitter dependencies promoted from `CyclerConfig` function-fields to named ports; and a
   pure `Step(event)→[]action` reactor for the cycle state machine.
4. **The restart interior is durable on the event bus**: 3–4 new durable events
   (`keeper_handoff_written`, `keeper_model_done`, `keeper_clear_sent`, `keeper_new_session_up`)
   carrying a real cycle id — today these are journal-only + overwritten each cycle (dossier 02 §2,
   dossier 04 §7), which is why the recorded corpus can only be replayed at boundary granularity.
5. **The load-bearing invariants are encoded as property tests** run over the recorded corpus:
   **SR4** (`/clear` never before model-done — the currently-missing signal), **SR9** (bounded
   liveness: every cycle terminates or emits a `restart_failed`; never silent — the resume-hang
   class), and SR3 (handoff-write-done before `/clear`), SR6 (brief only after new-session
   confirmed), SR7 (no overlapping restarts).
6. **Measurement is replay-vs-baseline, not live A/B**: the reactor is driven over the ~476
   recorded restart cycles + the four twin fault modes, asserted against the frozen baseline
   (`.harmonik/events/baseline-2026-07-13/`: restart-completion 84% = 427/507, `clear_unconfirmed`
   347). These numbers are what the rebuild must characterize and not regress.

## Non-goals (explicitly out of scope for this work / this phase)

- **The daemon god-function** (`beadRunOne`, `workLoopDeps`, `mergeMu`) — later phase (PLAN §4
  M-runexec). Not touched here.
- **The remote/SSH rebuild** and **agent-input (tmux) rebuild** — later phases (M-remote, M-input;
  existing `remote-substrate` kerf work is the remote home). Not touched here.
- **Test-theater deletion** (`specaudit`/`operatornfr`) — parallel direct work (PLAN §4 Track C /
  census M1). Not part of this spec.
- **queue.json two-writer fix and false-close fix** — parallel *direct* fixes, out-of-pipeline
  (PLAN §4 Track B). They are data-integrity fixes, not new contracts, so they do NOT run through
  this spec jig.
- **NOT a rewrite of keeper decision logic.** The gate ladder, thresholds, and cycle semantics are
  kept; they are re-expressed behind ports + a state machine. Behavior parity is a constraint.
- **No new abstraction the operator hasn't asked for** — in particular, no monadic FP library
  (§Constraints).

## Constraints (things that must not change / must hold)

1. **No daemon execution.** This work is implemented with Claude Code sub-agents + kerf,
   **out-of-pipeline**, single-writer on any shared file, human-reviewed merge to a branch. The
   daemon pipeline is the thing under repair and cannot be its own oracle (census Acceptance
   Oracle). Rationale in PLAN §"Execution model" and the 2026-07-13 operator exchange.
2. **Keep the regression net.** The ~466 incident-pinned tests + the keeper's ~55 test files stay
   green throughout; this is a refactor-behind-seams, not a rewrite.
3. **Go-native typed discipline, no monads.** The target FP style is the `internal/queue` idiom —
   pure handlers returning `(newState, events, error)`, consumer-owned narrow ports — NOT
   `Result`/`Option`/`Either` containers (none exist in the tree; do not introduce them).
4. **Behavior parity for the keeper** under replay of the recorded corpus, except where a property
   test deliberately tightens a currently-missing invariant (SR4).
5. **The event log is append-only, UUIDv7-ordered** (dossier 04 §3); any new events must fit that
   substrate and be joinable by a real id (fixing the keeper's current zero-`run_id` problem).
6. **The codex stack stays green** after re-instantiation on the extracted seam.

## Preliminary success criteria (concrete, spec-level)

- The spec defines `internal/substrate`'s seam contract: `EventSource[E]`/`Effector[A]`, the `Run`
  loop, the `Twin`+`FaultConfig` model, and the L0–L3 taxonomy policy — as normative text in
  `specs/`.
- The spec defines a `ClockPort` contract and names it as the required time seam for the keeper
  (and, by precedent, for future verticals).
- The spec defines the four new durable keeper interior events + their ordering invariants
  (SR3/SR4/SR6/SR7/SR9) as normative, testable statements.
- The spec states the measurement standard: replay-regression over the recorded corpus + fault
  injection vs the frozen baseline.
- `07-tasks.md` breaks the above into consumable implementation tasks (the "bits").

## Preliminary affected spec areas (for the Decompose pass to refine)

- **NEW**: `specs/substrate.md` (or similarly named) — the generic record→replay seam contract.
- **UPDATE**: the session-keeper spec (existing keeper spec/`session-keeper` kerf lineage) — ports,
  ClockPort, interior events, SR-invariants.
- **UPDATE/REFERENCE**: the event spec / `internal/core` event catalog — the four new event types,
  the zero-`run_id` fix, and whether to adopt the currently-dead typed-decode path
  (`DecodePayload`/schema-validate) for replay invariant-checking (PLAN §8 decision #3 —
  recommend adopt).
- **REFERENCE**: `process-lifecycle.md` / handler-contract (the substrate/PanePort touches the
  same tmux boundary the daemon uses; keep them consistent but do not rebuild the daemon side).

## Open decisions to settle in later passes (from PLAN §8; operator recommendations noted)
- Substrate genericization: **Go generics** (`EventSource[E]`/`Effector[A]`/`Run[E,A]`) vs an
  `any`-typed boundary. Operator lean: generics (one-method interfaces, low cost). Settle in
  Change-Design.
- Adopt vs delete the dead typed-decode registry path for replay invariant checks. Operator lean:
  adopt. Settle in Change-Design.

## Status
Problem space authored from operator-provided material; operator pre-signed-off (signoffs waived).
Advancing to `decompose`. Next passes run solo, using Fable for the research fan-out and any other
read-heavy pass.
