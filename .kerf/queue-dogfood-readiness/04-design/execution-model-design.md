# Execution model change design

## Current state

The success ladder has merge, push, close, and terminal emission. It has no
durable representation for a committed branch that stopped before merge.

## Target state

Add a durable terminal-recovery record keyed by run, bead, queue item, branch
tip, and stage. The record is written before shutdown can release its recovery
owner. A record selects exactly one action: finish the existing terminal ladder
or retain a reviewable recovery state. Reconstruction reads the record with Git
and Beads and never treats it as fresh dispatch. Queue advance occurs once,
after the selected terminal action.

## Rationale

Git and Beads stay reconstruction authorities while the record removes the
post-commit ambiguity.

## Requirements traceability

Addresses execution-model requirements and findings in `03-research/`.
