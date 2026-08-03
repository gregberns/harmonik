# Design — Handler Contract

## Current State

The handler contract keeps `Substrate` to process hosting and keeps input and
observation on `InputPort` and `Session.Attach`. The 16 Step 14 capabilities
are daemon-private operations.

## Target State

Make no handler-contract amendment. The Step 14 contract stays at the daemon
composition boundary. It must not add pane, tmux, session-sweep, or launch-cap
methods to `handler.Substrate`, `Session`, or `InputPort`.

## Rationale

Adding those methods would force other substrates to fake tmux behavior. The
existing narrow ports already express the public input and observation needs.

## Requirements Traceability

This preserves the narrow base port while allowing Alpha to declare daemon
capabilities separately.
