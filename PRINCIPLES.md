# Engineering Principles

**A pure typed core, effects at the edge, boundaries the compiler enforces — so that if it compiles,
it works.** Every principle below moves an error from run time to compile time, or removes the error
from the program. They are principles, not rules: each gives a direction and leaves you the judgment.
Judge new and rewritten code against them; do not retrofit old code for its own sake. Mechanical
checks live in `.golangci.yml` and `docs/foundation/project-level/quality-checks.md`, not here.

## 1. A pure core, with the effects pushed to the edge

The inside of a subsystem takes values and returns values. Everything that touches the world — I/O,
processes, events, the clock, randomness, UUIDs, the environment, the filesystem — lives in a thin
shell around it. The clock is an input like any other; `substrate.ClockPort` is the one already in
the tree. The smell is a core you cannot test with plain values, or cannot replay offline.

## 2. Make illegal states unrepresentable — parse, don't validate

Prefer a type that cannot hold a wrong value over a check that a value is not wrong. A check happens
once, and every later reader has to trust that it happened.

- Convert unstructured input to a typed value at the boundary, once. Re-checking raw input at each
  stop inward is how one bug comes to live in several places.
- Two fields that must never both be set are one interface with two implementations. A shape you can
  construct wrongly will be, so put it behind a constructor that can refuse.
- Reach for a lever before a check: unexported fields, a named type instead of a bare `string`, an
  enum switch the `exhaustive` linter checks, an interface with an unexported method.

## 3. Total functions: answer for every input the type admits

A panic, a "this cannot happen", a `nil` in place of an answer, and an out-param that means something
only when the error is nil are the same defect — a signature that promised more than it delivers.

- Expected failure is a value in the return type, not a panic and not a magic zero.
- An answer that may be absent says so in its type. In Go that is a second return value, not a
  pointer the caller has to remember to check.
- If a case truly cannot happen, narrow the type until you cannot write it. Otherwise handle it.

## 4. Consumer-owned ports

A package declares the smallest interface it needs and lets callers satisfy it. Dependencies point
inward, and an inner package that imports an outer one has knowledge it can never be tested without.
`internal/queue` declares `QueueSetter`, `MutationLocker`, `EventEmitter` and `BeadLedger`; the
daemon wires in types that satisfy them, and the queue never learns that a daemon exists.

- A field whose validity depends on which copy you hold belongs in a declared port, not behind an
  `if deps.X != nil` guard repeated at each call site.
- `depguard` checks direct imports only, per package — a package with no rule is unfenced. Write it.

## 5. Compose small total functions rather than configure one large one

A function that grew a flag to serve a second caller now serves neither well, and the next caller
adds a third flag. Adding the flag for a caller that does not exist yet is the same mistake, early.

- Length is a signal. The cause underneath is a function that makes many decisions, so splitting a
  long one into two entangled halves buys nothing.
- Collapse two paths that agree in shape and differ in detail. "Duplication beats the wrong
  abstraction" is about code you do not yet understand, not permission to keep three copies.

## 6. One writer, and one explicit state machine

A lifecycle is one state machine you can point at, not the same transition open-coded in several
places and not a spread of ad-hoc booleans. Anything shared and mutable has exactly one writer; two
paths writing the same store is last-write-wins data loss that shows up later, so route mutation
through the owner. A lock held while you wait on the world is held for an unbounded time — release
it first.

## 7. A test defends a claim, and its name says which claim

Name a test after the promise it defends. A name pointing at a ticket, a person, or a date rots when
the tracker moves and teaches the next reader nothing.

**Assume the test does not work** — not running, or running and measuring nothing. A test that cannot
fail looks exactly like one that passes. Verify the harness before you add to it.

- **Break it on purpose and watch.** Make the claim false and confirm THAT test fails, then grep to
  confirm your edit applied — a no-op and a real edit look identical when the subject is an absence.
- **Pair every negative claim with positive evidence in the same test.** "The worktree survived"
  proves nothing alone. Count the calls that reached the boundary; prove the run parks before freeing it.
- **Ask what would have to be true for this test to be unable to fail.** Usually it is upstream: an
  ungated path that fires first, a fake whose default is the answer under test, an excluding filter.
- **Suspect a green gate as readily as a red one.** "Nothing to do" is a claim and it can be wrong.

**A test that cannot fail gets `t.Skip` — not a delete, and not a patched assertion.** Deleting loses
the record that its claim is unproven; leaving it running keeps it counting toward the green. Name the
owning issue in the skip, and above it write what the test claimed, why it cannot fail, and what must
be decided before it comes back. `tools/testreport` lists every skipped test in its **NOT RUN** section
on every run, green ones included. Skip the whole test when its entire subject is the unprovable claim;
one vacuous assertion inside a test that proves something real is a separate question.

## 8. Prefer behavior you can re-run

Where input is real and messy, capture the raw stream once and replay it offline through the §1 core.
The value is the rare transitions: a corpus replayed with faults — dropped, stalled, truncated,
duplicated — reaches edges live tests miss. Skip the recorder for few inputs and no clock.

## 9. Prove one vertical, then generalize

Take one path through the system, hold it to all of the above end to end, and prove it. Then extract
the parts it needed — ports, clock, replay — into something the next vertical reuses. Find the
subsystem that comes closest today and make the tree look like it; do not invent the exemplar.
