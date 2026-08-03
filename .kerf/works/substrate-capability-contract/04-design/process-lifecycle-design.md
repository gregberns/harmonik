# Design — Process Lifecycle

## Current State

PL-021b and PL-028b define selected hosting, failure before dispatch for a
required tmux host, and boot-time announced degradation for a non-tmux host.
They do not name daemon-private capability interfaces.

## Target State

Add only a narrow cross-reference near PL-021b and PL-028b. It states that the
selected substrate declares the capabilities needed for its selected hosting
configuration. A missing construction requirement fails before dispatch. A
capability known only after a run-plan choice or pane spawn is checked
immediately before its operation and uses that operation's declared result. A
missing optional capability has an explicit, tested degradation.

The amendment must not name private Go interfaces, alter session resolution,
restore daemon-run paste, treat capture as acknowledgment, or promise remote
worker behavior.

## Rationale

This makes absence honest without turning tmux implementation details into a
public compatibility surface.

## Requirements Traceability

The amendment covers the planning goal that a missing capability is explicit,
observable, and testable at its construction or operation boundary. The
detailed behavior remains in the daemon contract and focused tests.
