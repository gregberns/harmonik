# Operator NFR change design

## Current state

`specs/operator-nfr.md` defines pause, drain, and resume behavior. The current
operator consumer changes a loaded queue before persistence.

## Target state

No normative operator NFR text changes in this work.

The operator consumer will call the queue-owned live transaction form for each
named queue. It will emit `queue_paused` only after a committed pause. Resume
will wake only after a committed active state. Multi-queue pause remains a
sequence of independent per-queue operations.

## Rationale

The transaction store already prevents a failed durable write from changing the
installed queue. This removes the current memory-before-persist failure leak
without changing pause scope or event behavior.

## Requirements traceability

| Requirement | Design response |
| --- | --- |
| ON-027 pause and drain order | Keep active to paused-by-drain behavior. |
| QM-054 and QM-063 | Commit before event emission. |
| Operator resume behavior | Keep paused-by-drain to active and post-commit wake. |
