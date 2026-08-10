# Findings

## Summary verdict

The current core is not yet a safe base for broad feature work.

The rewrite has added several sound parts.
The pure `runexec` machines, the resource lease model, the merge queue, and typed queue transactions all move in the right direction.

The main bead path still has the old architectural shape around those parts.
Large procedural functions own policy, effects, recovery, and lifecycle at the same time.
The package graph cannot express the core boundary in the charter.

The next work should finish the run-path decomposition before it adds more daemon behavior.

## Critical findings

### C1. The core boundary is a test list, not a software boundary

`make core` includes the complete daemon, shared core, and command packages.
Those packages contain most optional product subsystems.
The daemon package imports many subsystems that the charter excludes from the core.

This means the compiler cannot prove that the bead path is separable.
A developer can add a new dependency from the bead path to keeper or dashboard code without crossing a package boundary.

**Consequence:** More feature work can deepen optional coupling while `make core` stays green.

**Required direction:** Create a small core composition package with imports limited to the charter pipeline.
Keep daemon services in an outer package.
Add an import fence that checks this boundary.

### C2. The critical loops remain procedural state machines

The live path still centers on `runWorkLoop`, `beadRunOne`, `driveDotWorkflow`, and `runAgentLaunch`.
Each function owns hundreds of lines of branching policy and effects.

The new `runexec.Run` machine owns only the terminal spine.
It does not own the full bead lifecycle.
Earlier lifecycle phases still exist as control flow and mutable captures in `beadRunOne`.

**Consequence:** A change to launch, shutdown, retry, or recovery must preserve hidden ordering across several functions.
Tests must pin implementation details because no smaller policy unit exists.

**Required direction:** Make the per-bead run machine own plan, provision, dispatch, workflow, merge, and final state.
Let a shell execute its actions.
Keep the DOT walker as a child machine that returns a typed outcome.

### C3. One dispatch transition has several durable owners

The queue store reserves an item before the bead ledger claims the bead.
The run registry and run-session record add two more state stores later.
Failure uses compensation and boot repair to restore agreement.

This design has improved local durability.
It still relies on every exit path to apply the correct repair.
The scheduler comments document several past strands from missed repairs.

**Consequence:** Process death between writes can leave an item dispatched without a claimed bead or live run.
Recovery behavior depends on which store survived.

**Required direction:** Define one dispatch transaction owner.
Give it a typed state transition and an intent record before external writes.
Make startup replay that record to one defined terminal result.
Do not spread repair policy across scheduler branches.

## High findings

### H1. The scheduler selects, repairs, launches, and shuts down

`runWorkLoop` takes 22 direct parameters and several dependency bundles.
It owns queue choice, admission, claim, repair, goroutine creation, completion, adoption, and shutdown.

This is not a scheduler with a few large helpers.
It is the fleet state machine in one function.

**Consequence:** Scheduler changes can alter bead durability or run cleanup.
Review cannot isolate fairness policy from lifecycle policy.

**Required direction:** Split selection into a pure decision.
Return one typed dispatch command.
Give a transaction service ownership of reserve and claim.
Give a run supervisor ownership of goroutines and recovery.

### H2. The run boundary uses dependency bags

`RunEnv` and `SharedHandles` replace one old dependency object with two broad objects.
They expose concrete worker, tmux, project config, handler, and harness types.

The fields reduce some call signatures.
They also let any run-path function reach unrelated shared state.

**Consequence:** The compiler does not show the true dependency set of a run phase.
Tests need large fixtures or nil conventions.

**Required direction:** Give each machine phase a small input value and a small effector interface.
Keep concrete registries and mutexes in adapters at the composition root.

### H3. Optional services still shape the core loop

The scheduler knows about schedule ticks, coordinator reap, disk reclaim, dashboard gates, sentinel policy, and eager refill.
It carries ports for them even when configuration turns them off.

This conflicts with the charter rule that off means not constructed.
It also makes core behavior depend on the order of optional maintenance passes.

**Consequence:** Optional work can delay, stop, or change core dispatch.
Removing an optional service still requires edits in the core scheduler.

**Required direction:** Move optional services to observers or outer admission providers.
Compose only the selected providers.
Let the core consume a single typed admission result.

### H4. The `internal/core` package is neither small nor pure

The package has 32,096 production lines and 450 Go files in this checkout.
It imports file, clock, YAML, expression, and UUID libraries.

The package holds domain values, event schemas, policy evaluators, persistence helpers, and utilities.
Its name gives no useful dependency guarantee.

**Consequence:** Almost any package can import a broad surface under the label `core`.
Effects can move inward without an obvious boundary breach.

**Required direction:** Keep stable shared value types in small domain packages.
Move persistence and policy evaluation to named subsystems.
Reserve a pure package name only for code that a dependency rule can protect.

### H5. Time and identity are only partly injected

The design has a clock port and typed identifier sources.
Live queue and scheduler code still calls system time and UUID functions directly.

**Consequence:** Replay is incomplete.
Timing tests can depend on the host clock.
Failure injection for identity creation varies by package.

**Required direction:** Put time and identity in boundary inputs.
Use one clock and one identifier source across the full bead path.

## Medium findings

### M1. Error policy still uses logging and best effort in the core path

Several core-path event writes, cleanup calls, and transition identifier calls discard errors.
Other sites write to standard error and continue.

Some best-effort choices are valid.
The decision often lives at the effect site instead of in typed policy.

**Consequence:** Callers cannot tell a degraded success from a full success.
Tests must inspect logs or later state.

**Required direction:** Return a typed effect result to the owning state machine.
Let policy decide whether to continue, retry, degrade, or stop.

### M2. The test mass still mirrors the old architecture

The core test set has 237,484 test lines for 165,418 production lines.
The daemon alone has 78,219 test lines.

Many tests target exported seams and issue-specific cases.
Some can still defend important failure history.

**Consequence:** The cost of structural change remains high.
Test volume can hide the absence of one clear vertical proof.

**Required direction:** Build a claim map for the one vertical bead path.
Probe those tests for failure efficacy.
Keep valuable history tests, but move them behind stable behavior surfaces.

## Daemon survival preview

The daemon survival review should follow the core work.
This pass found three items that deserve the next review slice:

1. `runWorkLoop` stops its drain wait after ten seconds while run goroutines can remain active.
2. The merge queue uses a background context so a shutdown merge can outlive cancellation.
3. Run survival depends on a tmux session, a disk record, a registry handle, and startup adoption.

These can be correct together.
They need one explicit ownership model and a fault matrix.
The next review must test SIGTERM, SIGKILL, and restart at each durable write boundary.

## Positive foundations to keep

- `internal/runexec` shows the right pure transition style.
- `internal/runlease` gives resources one release decision.
- `internal/mergeq` gives merge work one serialized owner.
- Queue replacement uses an intent and durable commit result.
- The charter gives a clear and narrow product target.

The review recommends extending these patterns.
It does not recommend another broad rewrite from zero.
