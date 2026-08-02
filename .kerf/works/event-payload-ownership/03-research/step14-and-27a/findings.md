# Later Work Inventory — 2026-08-01

## Step 14

This is source inventory only until Step 10 is complete. It names 32 production
comma-ok assertions for the 16 Step 14 substrate capabilities. It does not add
related optional interfaces to the scope.

`internal/daemon/tmuxsubstrate.go` has 11 self-probes. They cover session
ensure, session creation, spawn readiness, pane target capture, remote runner
swap, remote session ensure, and pane capture. Their fallbacks range from a
no-op keepalive or skipped probe to a structural error or unsupported capture.

The 21 probes outside that file are in `bootreconcile.go`, `bootsocket.go`,
`bootstate.go`, `bootworkloop.go`, `crewstart.go`, `daemon.go`,
`dot_cascade_core.go`, `dot_gate.go`, `pasteinject.go`, `scheduler.go`, and
`workloop.go`. Their fallbacks disable a sweep, cap control, diagnostic hook,
readiness wait, reaper, keepalive, paste injection, or session isolation. The
only structural fallbacks are missing session creation and remote runner swap.

Step 10 collisions require serialization:

- `bootworkloop.go` `injectWorkLoopDeps` owns the readiness probe block and
  `spawnSubstrateReadyCh` construction.
- `bootworkloop.go` `wireStaleWatcherReapSeams` owns the stale reaper seam.
- `workloop.go` `newWorkLoopDeps` populates `coordinatorReapAdapter`.

`bootstate.go` `wireWatchersAndObservers` also collides with Step 12 through
the quiesce adapter and diagnostic hook setters.

## Step 27a

Step 27a is deferred until Step 10 is complete. It must use the final
`daemon.Config` construction shape. No constructor design starts before then.
The current inventory has 40 Config fields. `cmd/harmonik/main.go` sets 21 and
`cmd/harmonik/run.go` sets 17. The run path lacks `WorkflowModeDefault` and
fails boot validation.
