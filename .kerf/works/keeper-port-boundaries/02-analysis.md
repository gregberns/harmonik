# Current State

## Cycle construction

`internal/keeper/cycle.go` defines `CyclerConfig`. It contains policy, paths, named ports, and function seams. `applyDefaults` fills production functions. `NewCycler` builds `fnPane`, `fnGauge`, `fnHandoff`, and `fnRespawn` adapters when named ports are absent.

## Ports

`internal/keeper/ports.go` defines `PanePort`, `GaugePort`, `HandoffPort`, and `RespawnPort`. `Emitter` and `substrate.ClockPort` complete the effect set.

`PanePort` combines pane writes, pane capture, and operator attachment reads. The automatic cycle does not use capture. Restart and acknowledgement code use capture.

`GaugePort` combines context files, managed-session writes, precompact markers, queue state, sleep state, hold state, pane attachment, idle markers, and transcript activity. Its `Snapshot` method reads facts from several external systems.

`HandoffPort` combines the handoff document and the crash-recovery journal. Both use the file system, but they have different lifecycles.

## Reactor and shell

`internal/keeper/step.go` is a pure state transition function. It accepts observations in events and returns actions.

`internal/keeper/shell.go` reads ports, creates events, and executes actions. It treats most effects as best effort. An opened journal write is the main fatal effect.

## Watcher

`internal/keeper/watcher.go` is 2,110 lines. It owns warning delivery, hard-ceiling checks, recovery, reaping, heartbeat work, and cycle entry. Much of its state lives in local variables inside `Run`.

## Tests

The keeper has 381 test functions and 81.1 percent statement coverage. Tests create at least 73 `CyclerConfig` values. Several helpers repeat production wiring.

`internal/keepertest` provides reactor and integration harnesses. It does not yet provide one production-parity cycler builder.

The tmux twin tests cover real pane effects. They do not always use production gate values or write all transcript effects.

## Constraints

- `CyclerConfig` is exported and used by tests outside the package.
- The cycle journal and event vocabulary support crash recovery.
- The pure reactor is useful and must remain pure.
- Tests need fake time and deterministic effect control.
- Production behavior must stay stable during migration.

## Recent work

The latest keeper change added origin-aware transcript classification. It also removed the transcript-driven persistent hold. That incident exposed production-default drift in an integration test.
