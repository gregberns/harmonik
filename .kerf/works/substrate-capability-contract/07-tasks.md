# Tasks — Declared Substrate Capability Contract

## Ownership and guardrails

Alpha owns all daemon contract, composition, and cutover work. Bravo has no
daemon implementation task in this work. Do not widen `handler.Substrate`,
`Session`, or `InputPort`. Do not begin `daemon-config-construction` from this
plan.

Every task uses the decision table in `04-design/daemon-contract-design.md`.
It must keep construction requirements separate from operation requirements.

## Implementation tasks

### T1 — Resolve the daemon capability record

**Owner:** Alpha

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a.

**Change:** Build the daemon-owned typed capability record at composition.
Give later consumer tasks a narrow resolved value or an operation-boundary
check. Keep the base substrate as process hosting only. This task does not
move consumer assertions.

**Deliverables:** Daemon construction and consumer-port code in
`internal/daemon`; focused base-only and provider test doubles.

**Acceptance:** The record can represent every decision-table capability and
supports base-only and provider doubles. No public handler seam gains a pane,
adapter, tmux, or session-control method. Consumer assertion removal belongs to
T2, T3, and T4.

**Depends on:** none.

### T2 — Apply boot and lifecycle capability outcomes

**Owner:** Alpha

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a.

**Change:** Cut over adapter consumers, session identity, keepalive, spawn-cap
read and write, readiness, and diagnostics. Keep each optional degradation
separate. Check construction requirements before dispatch. Do not fabricate a
cap or start a goroutine when the capability is absent. Do not edit either
`bootState.wireWatchersAndObservers` capability call. T5 owns its quiesce
adapter and diagnostic-hook conversion.

**Deliverables:** The boot, reconcile, scheduler, work-loop, and watcher
consumer cutovers; focused tests for every absent and provider case.

**Acceptance:** Every T2-owned assertion is removed and has an absent/provider
proof. The owned-session exclusion is safe. Cap refusal, readiness, keepalive,
and each non-watcher adapter consumer preserve their declared absent results.

**Depends on:** T1.

### T3 — Apply run, paste, and remote operation boundaries

**Owner:** Alpha

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a;
`specs/handler-contract.md` HC-069 through HC-071;
`specs/agent-input.md` AIS-003 and AIS-011.

**Change:** Cut over pane target and capture operations at their post-spawn
boundaries. Keep capture observation-only. Apply the paste caller and harness
matrix. Keep tmux/Claude delivery on `InputPort` and its positive acceptance on
the AIS async event. Check remote runner swapping only before worker tmux
operations. Keep absent remote session ensure behavior internal to that
operation.

**Deliverables:** Per-run substrate, paste, gate, reviewer, and remote worker
cutovers; focused operation-boundary tests.

**Acceptance:** A missing pane target does not undo ordinary spawn. Later pane
operations return their declared structural result. No paste caller silently
becomes acceptance. A missing remote runner swap makes no worker tmux call.

**Depends on:** T1.

### T4 — Remove unreachable independent run sessions

**Owner:** Alpha

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a.

**Change:** Remove the full run-only residue selected by the design:
`runSessionSpawner`, `tmuxSubstrate.SpawnRunSession`, its session-name helper,
`perRunSubstrate.runSessionID`, its independent `SpawnWindow` branch, and the
run-only tests and documentation. Keep independent crew sessions intact.

**Deliverables:** The source deletion, crew-only session-creation coverage,
and the shared-session input-buffer regression test.

**Acceptance:** No removed symbol or run-only test remains. Two concurrent
shared-session runs with distinct captured pane targets use distinct valid
input buffer names. Tmux window hosting remains available.

**Depends on:** T1 and T3. T3 owns the active per-run operation boundary in
`tmuxsubstrate.go`; T4 removes its run-only branch in one atomic follow-up.

### T5 — Serialize the shared watcher wiring

**Owner:** Alpha

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a.

**Change:** After the Step 12 subsystem-switch change merges, make one
integrated edit to `bootState.wireWatchersAndObservers`. Replace the quiesce
adapter and diagnostic-hook discovery through the resolved record. Preserve
every existing switch guard and disabled-subsystem path.

**Deliverables:** The one guarded composition edit and its integration tests.

**Acceptance:** A base-only substrate leaves quiesce and diagnostic hooks
absent. Each disabled Step 12 switch still prevents its construction. A
provider preserves existing quiesce and diagnostic behavior.

**Depends on:** T1, T2, and the external Step 12 base change.

### T6 — Scenario validation

**Tracker task:** `hk-vuih9` — scenario: process-lifecycle.md — selected
substrate capability contract.

**Spec trace:** `specs/process-lifecycle.md` PL-021b item 6a.

**Change:** Use the daemon construction test seam to select base-only and
provider substrate doubles. Do not add a production CLI selector. The isolated
fixture invokes `harmonik run --beads <fixture-bead>` through that test seam.
Exercise a construction refusal and an optional degradation.

**Acceptance:** The construction case refuses before `run_started`. The
optional case reaches its declared degraded result without changing the sealed
workflow. The terminal JSONL record or run result is captured.

**Depends on:** T2, T3, T4, T5.

### T7 — Operator-facing exploration

**Tracker task:** `hk-sv1hy` — explore: process-lifecycle.md — capability-loss
startup report.

**Spec trace:** `specs/process-lifecycle.md` PL-028b and PL-021b item 6a.

**Change:** Start `harmonik daemon` in an isolated project with unavailable
tmux hosting and a selected non-tmux substrate. Inspect the startup report.

**Acceptance:** The report names the unavailable hosting capabilities, does
not invent a substitute, and reaches the expected startup status with its
JSONL lifecycle record.

**Depends on:** T2, T5.

## Dependency graph

```text
T1
├── T2 ─── T5 ──┬── T6
└── T3 ─── T4 ─┘

T2 ─── T7

Step 12 base change ─── T5
```

## Parallelization

After T1, T2 and T3 may run in parallel only after their owners verify that the
claimed symbols do not overlap. T3 owns the active per-run changes in
`tmuxsubstrate.go`. T4 follows T3 and removes the run-only branch atomically.
T5 is serialized behind the Step 12 base change and must not overlap its shared
function. T6 waits for all implementation tasks. T7 may run after T2 and T5.

Neither this work nor its implementation tasks close until T6 and T7 close.
