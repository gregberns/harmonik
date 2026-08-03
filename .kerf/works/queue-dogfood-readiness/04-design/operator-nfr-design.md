# Operator NFR change design

## Current state

Ordered drain stops new dispatch and releases resources after checkpointing.
It does not name the post-commit interval or its timeout result.

## Target state

Add committed-before-merge as a special drain outcome. Its per-step timeout
either completes the terminal ladder or leaves the durable recovery record.
Resource release follows the selected owner. The controlled proof records load,
timeouts, stop point, and retained artifacts. New storage remains N-1 readable.

## Rationale

A global short wait followed by cancellation cannot be an operator-safe drain.

## Requirements traceability

Addresses ordered-drain and controlled-load requirements.
