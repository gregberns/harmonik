# Engineering Principles

> Portable, language-neutral. Distilled from the harmonik code-revamp plan (2026-07-13).
> The target is one sentence: **typed-functional core + hexagonal boundaries + record→replay
> testing, enforced by CI rather than by discipline.** Don't invent the target — find the one
> subsystem that already embodies these and make the rest of the tree look like it.

## 1. Functional core / imperative shell
The core of every subsystem is **pure**: it takes parsed input plus in-memory state and returns
a typed result. No I/O, no clock, no event emission, no logging with side effects inside the
core. All effects — disk, network, process spawn, emitting events — live in a thin **shell** that
wraps the core.

- A handler is `f(request, state) -> response`, not a method that also writes to disk.
- The core is trivially testable with plain values; the shell is where mocks/fakes plug in.

## 2. Consumer-owned ports (dependency inversion)
A package declares the **minimal interfaces it needs** and lets the caller's types satisfy them
structurally. Inner packages never import outward.

- The queue package declares `QueueSetter`, `EventEmitter`, `BeadLedger` — the outer daemon's
  types happen to satisfy them, so queue never imports daemon.
- Rule of thumb: dependencies point inward. If an inner module imports an outer one, invert it
  behind an interface the inner module owns.
- Enforce the layering with a boundary linter (see §7), not code review alone.

## 3. Record → replay → fault injection
Build every I/O-bearing subsystem so its behavior can be **captured and replayed offline**.

- **Capture** the raw input stream (a stdio tee / event tap) to an append-only log.
- **Decode** it with a pure function; feed a pure **state machine**; apply effects through a
  swappable **effector**.
- **Replay** the captured corpus as a synthetic source, and **inject faults** (drop / stall /
  truncate / duplicate) to prove the state machine's edges.
- The effector is swappable: real, fake-recorder, or bridge — the core never knows which.

## 4. Time is a port
No direct wall-clock calls (`now()`, `sleep`) inside the core. Thread a `Clock` interface
(`Now`, `Since`, `NewTicker`, `Sleep`) through it.

- This is the #1 unlock for deterministic tests of timeouts, poll loops, and races.
- Same rule for randomness, UUIDs, and any other ambient nondeterminism — inject it.

## 5. Explicit state machines, single writer
- Represent lifecycle as **one explicit state machine**, not the same transition open-coded in
  several places or spread across ad-hoc booleans and an out-param.
- Any shared mutable state has **exactly one writer**. Two code paths writing the same store =
  last-write-wins data loss. Route all mutation through the owner.
- Keep locks off the hot path: never hold a global mutex across build + network + disk I/O.

## 6. Spec-pinned, named regression tests; a real coverage taxonomy
- Tests cite the **spec rule or bug id** they pin, in the test name
  (e.g. `validation_<rule>_<bug-id>_test.go`). A test's name says what it defends.
- Tier tests **L0–L3**: L0/L1 pure + replay (fast, ~zero external cost, run always); higher
  tiers gated behind an env flag for the one live end-to-end pass + a drift canary.
- Beware **test theater**: a suite that mostly asserts constants is not coverage. "Green" must
  mean the product code actually ran.
- A bug fix ships with a scenario test that **reproduces** it first.

## 7. Enforce the principles with CI levers, not vibes
Principles that aren't enforced rot. Turn on:

- **Complexity ceiling** — cyclomatic / cognitive / function-length linters, so god-functions
  fail the build.
- **Coverage floor** — a gate that fails if coverage on new code drops below a threshold.
- **Boundary/layering linter** — deny-by-default import rules encoding the dependency direction
  from §2.
- **Typed containers** — prefer `Result` / `Option` / `Either`-style types over sentinel
  errors + null for expected failure paths.
- **Ratchet, don't boil the ocean** — gate on *new* code (`--new-from-rev=main`) so legacy is
  auto-grandfathered; every commit ratchets quality up without a big-bang rewrite.

## 8. Method: prove one vertical, then generalize
Don't rewrite everything. Take **one part of the code**, embed the discipline above end-to-end,
prove it (green, replayable, enforced), then generalize the shared seams
(record→replay substrate, ports, clock) into a reusable core the next vertical instantiates.

---

### Source (harmonik, for reference)
- `plans/2026-07-13-code-revamp/PLAN.md` — §1 "what good looks like", §3 abstractions, §7 enforcement
- `plans/2026-07-13-code-revamp/track-c-enforcement.md` — concrete linter/coverage/boundary config
- `plans/2026-07-13-code-revamp/research/06-architecture-and-fp.md` §5 — existing exemplars
- `docs/foundation/project-level/build-practices.md` — commit / review / test-plan discipline
