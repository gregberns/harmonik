# Salvaged from a deleted branch — the continuity checkpoint shape

**Status: research input, not a design. Nothing here is decided.**

Recorded 2026-08-06 by lane alpha, on the operator's instruction to keep any useful detail before
deleting the branch that carried it. The source was `internal/runloop/continuity` on a recovered
branch (`work/cq-mig-01`, tagged `preserve/cq-mig-01-20260806`), written 2026-07-24 against a tree
that is now 533 commits behind. **The code and the tag are deleted.** This note is all that is kept,
and it is kept because the problem is real and this work is chartered to solve it.

Read this as one person's earlier attempt at the same problem. It was never reviewed against the
current design, it never ran in production, and it is not a head start you should assume is correct.

## The problem it addressed

An agent session has an identity — a session ID the harness owns. To resume that session later, or
to hand it more work, the controller must know that ID and must have written it down somewhere
durable BEFORE the agent does work worth resuming. If the agent works first and the record is
written after, a crash in between leaves work that nothing can reattach to.

## The one distinction worth carrying forward

Harnesses differ in WHEN the authoritative identity becomes known, and the design treated this as the
central fork rather than an implementation detail:

- **Minted.** The caller decides the identity before any work starts, records it, and only then
  permits work. The identity is an input.
- **Captured.** The harness reveals its own native identity only after it launches. The caller cannot
  know it in advance, so the record can only be written after capture, and it must be written before
  any later resume or re-task.

The ordering obligation is the same in both cases and it is the whole point: **checkpoint the
identity before permitting the work that depends on it.** What differs is only whether the checkpoint
can happen before first launch or only after it.

## Other shapes it used, worth a look but not an endorsement

- The checkpoint was a port, so the ordering rule could be tested without git, ssh or a real harness.
- A typed identity-conflict error, distinguishing "this run already has a different identity" from a
  generic write failure.
- An optional protocol-version handshake, modelled as "does this protocol need a version-selected
  message, and if so exactly which version" rather than a free-form negotiation.
- A location kind, so a checkpoint could name where the work lives without assuming local disk.

## What it did NOT solve, and what this work must

It coordinated ordering and validation only. It supplied no answer for committed-state inspection,
git checkpoint creation, remote execution, or protocol writes — all of those were left to a
composition root that did not exist. It also predates this work's own rules: no parsing of model
prose, typed lifecycle events only, a controller-owned lease, and the canonical open-decision gate.
Where this note and `01-problem-space.md` disagree, the problem space wins.
