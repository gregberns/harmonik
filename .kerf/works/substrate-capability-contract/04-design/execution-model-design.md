# Design — Execution Model

## Current State

EM-012a seals one resolved DOT workflow before `run_started`. It does not make
the selected workflow depend on private substrate capabilities.

## Target State

Make no execution-model amendment for an internal capability move. This work
selects removal of the unreachable run-session path. It does not make
independent run-session isolation reachable.

## Rationale

A capability fallback must not change the sealed workflow or select another
execution shape.

## Requirements Traceability

This preserves DOT resolution while requiring an explicit disposition for the
run-session capability.
