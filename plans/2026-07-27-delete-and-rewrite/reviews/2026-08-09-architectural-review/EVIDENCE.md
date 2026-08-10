# Evidence

## Scope source

`CHARTER.md` section 3 defines the core pipeline.
Section 6 defines the target architecture and the meaning of done.
The `CORE_PKGS` variable in `Makefile` resolves that pipeline to 29 packages.

## Size of the current core test set

The 29 package entries contain 165,418 production lines and 237,484 test lines.
Tests are 1.44 times the production size in this set.

Three entries dominate the set:

| Package | Production lines | Test lines |
| --- | ---: | ---: |
| `internal/daemon` | 44,774 | 78,219 |
| `internal/core` | 32,096 | 53,941 |
| `cmd/harmonik` | 29,832 | 19,767 |

This count uses tracked checkout files under each package directory.
It counts Go source lines with `wc -l`.
It separates files by the `_test.go` suffix.

## Largest functions on the live path

The line spans below use function boundaries in the current files.
They include comments inside each function span.

| Function | File | Approximate span |
| --- | --- | ---: |
| `runWorkLoop` | `internal/daemon/scheduler.go` | 1,352 lines |
| `driveDotWorkflow` | `internal/daemon/dot_cascade_core.go` | 1,019 lines |
| `beadRunOne` | `internal/daemon/workloop.go` | 949 lines |
| `runAgentLaunch` | `internal/daemon/agentlaunch.go` | 857 lines |
| `dispatchDotAgenticNode` | `internal/daemon/dot_cascade_core.go` | 681 lines |

The first four functions all suppress length and complexity checks.
The scheduler suppression says that the code moved without a design change.
The work-loop suppression calls the function the run-path giant.

## The scheduler still owns run policy

`runWorkLoop` does more than select work.
It owns these actions in one loop:

- maintenance and disk gates
- queue selection and fairness
- several admission stages
- queue reservation
- bead lookup and claim
- stale-state repair
- worker choice and capacity
- run identity generation
- run registry writes
- goroutine lifecycle
- completion and group advance
- shutdown drain and adoption

The function takes 22 parameters.
Several parameters are bundles that contain more dependencies.

## The run state machine is partial

`internal/runexec.Run` is a pure state machine.
It owns the terminal path from mode outcome through gate, merge, and close.
This is a good direction.

The production path creates it through `runloop.NewRunBridge`.
However, `beadRunOne` still owns plan resolution, remote selection, worktree creation, session setup, workflow execution, and failure salvage.
`driveDotWorkflow` owns graph traversal and node outcomes.
`runAgentLaunch` owns launch and post-launch policy.

The system therefore has a pure terminal spine inside a larger implicit run machine.
The larger machine still uses control flow, deferred functions, mutable captures, and early returns as state.

## Queue state and bead state use compensation

The queue path first writes a durable queue reservation.
It then calls the bead ledger to claim the bead.
If the claim fails, the scheduler calls `releaseReservation` to compensate.

This order avoids an empty run identifier in the queue record.
It still leaves two durable owners for one dispatch transition.
The scheduler must repair every failure between the writes.

The code comments name this risk directly.
They say a missed drain can strand a dispatched item forever.
They also say a failed release can leave disk and memory with different states.

## The core boundary includes optional subsystems

The core package set includes the full `internal/daemon` package.
That package imports optional packages that the charter excludes.
Examples include `crew`, `crewrun`, `dashboard`, `digest`, `keeper`, `presence`, `schedule`, and `sentinel`.
It also imports all three harness packages.

`runWorkLoop` always constructs a maintenance object.
The object receives schedule, coordinator, disk, refill, governor, and gate ports.
Disabled features can use nil or inactive ports, but the scheduler still knows their concepts.

The package graph therefore cannot express the charter boundary.
`make core` proves a selected test list, not a separable architecture.

## The nominal inner packages still perform effects

`internal/runexec` is close to a pure core.
The surrounding packages are not.

Examples from production code:

- `internal/runloop` starts `go` and `git` commands and reads files.
- `internal/runloop` writes to standard error and creates UUID values.
- `internal/queue` reads clocks, creates UUID values, and writes files.
- `internal/queuewiring` reads the system clock.
- `internal/core` reads and writes files and reads the system clock.

The name `internal/core` therefore does not mean pure core.
It is a shared domain and utility package with effectful code.

## Port boundaries remain wide

`runloop.RunEnv` carries daemon config, queue fields, handler config, sandbox config, bead data, and routing overrides.
`runloop.SharedHandles` carries registries, counters, semaphores, substrates, stores, workers, a runner, and a mutex.

`internal/runloop` imports concrete packages such as `brcli`, `workers`, `projectconfig`, and `lifecycle/tmux`.
It also imports harness and handler types.

The ports reduce direct access to the old daemon dependency bundle.
They do not yet make `runloop` an inner policy package.

## Clock and identity injection is incomplete

The run path has `substrate.ClockPort` and a transition identifier source.
Several live scheduler and queue paths still call `time.Now` or `uuid.NewV7` directly.

Examples include queue submit, append, event creation, scheduler refusal windows, and run identity creation.
Tests must therefore control some time and identity through process timing or package globals.

## Test-shape warning

The current core set still has more test code than product code.
Many tests use issue names, characterization names, or exported test seams.
That shape does not prove that a test is bad.
It does show that the old architecture still controls much of the proof surface.

This pass did not mutate production code to test failure efficacy.
It does not call any named test vacuous without that proof.

## Commands used

The review used these command classes:

```text
rg --files
rg -n
find ... -name '*.go'
wc -l
go list -f ...
git status --short --branch
git log -1 --oneline
```

The first `go list` attempt could not write the default Go cache.
The second used a scratch build cache and returned package imports.
It also warned about a read-only module cache stat file.
The import result was still complete for the inspected packages.
