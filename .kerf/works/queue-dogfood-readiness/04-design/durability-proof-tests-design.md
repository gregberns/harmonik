# Change Design — Durability proof tests and ratchets

## Current state

The reservation write-failure test drives the work loop.
The completion test reads the persisted queue file.
The source ratchet checks transition names but cannot prove a durable write.

## Target state

Create a release-contract table before implementation.
For every reservation, release, recovery, and completion edge, name the owner.
Name the persisted state, production-path test, fault test, and optional ratchet.
Each production test fails when its claimed write is removed.
Each ratchet remains a source-surface guard only.

## Rationale

Source shape does not prove a durable transaction.
The table prevents one lane from relying on an unproved shared transition.

## Requirements traceability

- `02-components.md`: durability proof tests and ratchets.
- `03-research/durability-proof-tests/findings.md`.
