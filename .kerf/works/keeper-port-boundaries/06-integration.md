# Integration Plan

## Construction order

1. Resolve project configuration into `CyclePolicy` and `CycleEnv`.
2. Build production source adapters with one shared clock.
3. Assemble `CycleDeps`.
4. Validate all required dependencies without calling them.
5. Create the shell and pure reactor.
6. Recover the journal before the watcher starts normal polling.

## Data flow

The shell asks source probes for raw facts. A shell-owned sampler creates one `GateSnapshot`. The reactor receives an event with that value. It returns ordered actions. The shell sends each action to one effect port.

The handoff document and journal store use separate contracts. They retain their current paths and bytes. The shell retains the phase-specific journal error policy.

## Shared state

One clock drives timers, cycle IDs, injection timing, hold age, and test scenarios. `CycleEnv` holds resource identity. `CyclePolicy` holds behavior values. Ports do not retain policy.

## Error flow

Construction errors stop startup before any effect. Runtime port failures keep current semantics. An opened journal write remains fatal. Other effect failures remain best effort unless a later behavior specification changes them.

## Migration order

Add policy first. Add narrow ports and compatibility adapters second. Add the validated constructor third. Migrate command wiring before test call sites. Add the scenario builder before removing old seams. Remove broad adapters last.

## Integration tests

Differential tests run old and new construction with identical observations. A command-package scenario tests the actual production composition root. The tmux twin uses production policy or named gate opt-outs. Mutation tests prove origin and dependency wiring tests fail when their production checks are removed.
