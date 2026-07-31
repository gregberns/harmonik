# Engineering Principles

**The target, in one sentence: a pure typed core, effects at the edge, boundaries the compiler
enforces — so that if it compiles, it works.** Go cannot reach that the way Haskell can. Aim at it
anyway. Every choice below moves an error from runtime to compile time, or removes the error from
the program.

> **These are principles, not rules.** A rule tells you what to type. An agent obeys the letter and
> misses the point. A principle gives a direction to travel and leaves you the judgment.
>
> A principle nothing checks stops being true, so the checks belong in the toolchain. The settings
> live in `.golangci.yml` and `docs/foundation/project-level/quality-checks.md`. If you find
> yourself writing a filename pattern, a threshold, or a required flag into this file, it belongs
> there or in a skill instead. Mechanism put here gets obeyed and not understood.
>
> Judge new and rewritten code against these. Do not retrofit old code for its own sake.

## 1. A pure core, with the effects pushed to the edge

The inside of a subsystem takes values and returns values. It does no I/O, spawns no process,
emits no event, reads no clock, and generates no random number. Everything that touches the world
lives in a thin shell around it.

- The clock is an input like any other. So are randomness, UUIDs, the environment, and the
  filesystem. A core that reads them directly cannot be tested and cannot be replayed. Thread them
  in — `substrate.ClockPort` is the one already in the tree.
- The payoff is not purity for its own sake. A pure core is testable with plain values, and the
  shell is the only place a fake is needed.

## 2. Make illegal states unrepresentable — parse, don't validate

Prefer a type that cannot hold a wrong value over a check that a value is not wrong. A check
happens once. Every later reader has to trust that it happened. A type carries the guarantee
everywhere the value goes.

- Convert unstructured input into a typed value at the boundary, once. Let the inside work only
  with the typed form. Passing raw input inward and checking it again at each stop is how one bug
  comes to live in several places.
- If two fields must never both be set, that is one interface with two implementations, not two
  fields and a comment.
- A shape you can construct wrongly will be constructed wrongly. Where that matters, put the value
  behind a constructor that can refuse, rather than trusting each caller to fill it in correctly.
- Go gives you four levers toward this: unexported fields, named types instead of a bare `string`,
  enum switches the `exhaustive` linter checks, and interfaces with an unexported method. Reach for
  those before you reach for a check.

## 3. Total functions: answer for every input the type admits

A function should have an answer for every value its signature accepts. A panic, a "this cannot
happen", a `nil` returned in place of an answer, and an out-param that means something only when an
error is nil are all the same defect — a signature that promised more than the function delivers.

- Expected failure is a value in the return type, not a panic and not a magic zero.
- An answer that may be absent says so in its type. In Go that is a second return value, not a
  pointer the caller has to remember to check.
- If a case truly cannot happen, narrow the type until you cannot write the case. If you cannot
  narrow it, handle it.

## 4. Consumer-owned ports

A package declares the smallest interface it needs and lets callers satisfy it. Dependencies point
inward and inner packages never import outward.

- The queue package declares `QueueSetter`, `EventEmitter` and `BeadLedger`. The daemon's types
  happen to satisfy them. The queue never learns that a daemon exists.
- An inner module that imports an outer one has been handed knowledge it can never be tested
  without.
- A boundary a linter can deny is a boundary that holds. `depguard` checks direct imports only, and
  most of its rules are per-package, so a package nobody wrote a rule for is nearly unfenced.

## 5. Compose small total functions rather than configure one large one

A function that grew a flag to serve a second caller now serves neither well, and the next caller
adds a third flag.

- Length is a signal. The cause underneath it is a function that makes many decisions. Splitting a
  long function into two entangled halves buys nothing.
- Two paths that agree in shape and differ in detail are what this rewrite exists to undo. Collapse
  them. The old advice that duplication beats the wrong abstraction is about code you do not yet
  understand. It is not permission to keep three copies.

## 6. One writer, and one explicit state machine

- A lifecycle is one state machine you can point at, not the same transition open-coded in several
  places and not a spread of ad-hoc booleans.
- Anything shared and mutable has exactly one writer. Two paths writing the same store is
  last-write-wins data loss that shows up later. Route mutation through the owner.
- A lock held while you wait on the world is held for an unbounded time. Release it before anything
  slow begins.

## 7. A test defends a claim, and its name says which claim

A test exists so that one specific claim breaks the build when it stops being true. Someone reading
the test list should learn what the system promises.

- Name a test after the promise it defends. A name that points at a ticket, a person, or a date
  stops being true when the tracker moves, and it teaches the next reader nothing. This project
  reached 199,899 lines of test code named that way before anyone counted.
- Watch for a test that passes without reaching the code it claims to cover. A suite that mostly
  asserts constants is not coverage. A test whose pass condition is "X did not happen" is satisfied
  for free by any environment where nothing happens at all.
- If you did not watch the test fail, you do not know what it tests.

## 8. Prefer behavior you can re-run

Where a subsystem's input is real and messy, capture the raw stream once and replay it offline
through the pure core of §1. The value is in the rare transitions: a replayed corpus with faults
injected — dropped, stalled, truncated, duplicated — reaches edges no live test hits reliably.

- This is a technique and not a law. Do not build a recorder for a subsystem with a few inputs and
  no clock.

## 9. Prove one vertical, then generalize

Take one path through the system, hold it to all of the above end to end, and prove it. Then
extract the parts it needed — ports, clock, replay — into something the next vertical reuses. Find
the subsystem that already comes closest and make the rest of the tree look like it. Do not invent
the exemplar, and do not generalize from zero working examples.
