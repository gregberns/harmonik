# MVH Roadmap

The Minimum Viable Harmonik (MVH) is the first runnable state of the system: a user adds tasks to the bead ledger, starts `harmonik`, the daemon bootstraps its composition root, and the process consumes each task by dispatching a sub-agent that executes to completion. The composition root (event bus + handler registry + skill registry + control-point registry + redaction registry) is already scaffolded and the EventBus concrete implementation (`hk-8mup.62`) is closed. The one remaining critical chain is the redaction wiring that gates `Emit` correctness, which is the last normative precondition for running live dispatch.

Generated from the bead corpus snapshot as-of 2026-05-12. Open-bead counts: 142 total; 127 post-mvh; 1 phase:0 meta; 10 hollow spec-epic rollups; **4 on the MVH critical path**.

---

## Critical path (must close to reach MVH)

### Tier 0 — dispatchable now (1 bead)

| ID | Title | Spec ref | Blocked by | Notes |
|---|---|---|---|---|
| hk-8i31.83 | RedactionRegistry + RegisterPattern + RedactionMiddleware in `internal/handlercontract/` wired at daemon.Start | handler-contract.md §4.7 (HC-030, HC-032) | hk-8mup.61 (closed) | Defines `RedactionRegistry` type + `RegisterPattern` + applies HC-031 `RedactByFieldName` + HC-032 per-handler patterns; wires `registry.RegisterPattern(…)` calls at `daemon.Start` before `bus.Seal()`. `redaction.go` already has `RedactByFieldName`; this adds the per-subsystem overlay. |

### Tier 1 — unblocks after Tier 0 (1 bead)

| ID | Title | Spec ref | Blocked by | Notes |
|---|---|---|---|---|
| hk-8i31.37 | Redaction registry middleware | handler-contract.md §4.7 (HC-030) | hk-8i31.83 | Installs the composed middleware (HC-031 + HC-032 patterns) in the event-bus producer path so every `Emit` applies both redaction layers to every payload and every structured log line before emission. |

### Tier 2 — unblocks after Tier 1 (1 bead)

| ID | Title | Spec ref | Blocked by | Notes |
|---|---|---|---|---|
| hk-hqwn.45 | Redaction registry applied before emission | event-model.md §4.4 (EV-035) | hk-8i31.37 | The `busimpl.Emit` path MUST apply the redaction registry before JSONL append AND before consumer dispatch; fields matching `(?i)(secret|token|password|api[_-]?key|auth)` → `"<redacted>"`. Per-handler patterns from HC-032 applied by same path. Structurally required before any live run. |

### Tier 3 — unblocks after Tier 2 (1 bead)

| ID | Title | Spec ref | Blocked by | Notes |
|---|---|---|---|---|
| hk-hqwn.19 | Dispatch semantics | event-model.md §4.4 (EV-014a) | hk-hqwn.45 | `Emit` must return after: (a) redaction (EV-035), (b) JSONL append + fsync per durability class (EV-016), (c) synchronous-consumer dispatch (blocking). Async + observer dispatches off critical path via bus worker pool (default 4 workers). This closes the full `Emit` contract and makes the bus live-dispatch-correct. |

---

## Parallel-safe groupings

### Tier 0

Only one bead (hk-8i31.83). No parallelism available. Target: `internal/handlercontract/redaction.go` (add `RedactionRegistry` type, `RegisterPattern`) + `internal/daemon/daemon.go` (wire at `Start`).

### Tier 1

Only one bead (hk-8i31.37). Target: `internal/handlercontract/` — adds middleware installer. Touches same package as Tier 0; must serialize.

### Tier 2

Only one bead (hk-hqwn.45). Target: `internal/eventbus/` — adds redaction registry invocation inside `busimpl.Emit`. Different package from Tier 0/1; could hypothetically parallel-schedule with unrelated work but is itself serial on the chain.

### Tier 3

Only one bead (hk-hqwn.19). Target: `internal/eventbus/` — finalizes `Emit` dispatch sequencing. Same file as Tier 2; must serialize.

**Net shape:** a single 4-step linear chain. No intra-tier parallelism. Expected cycle count: 4 implementer dispatches, sequential.

---

## Out of MVH scope

| Category | Count | Filter rationale |
|---|---|---|
| `post-mvh`-labeled open beads | 127 | Explicit label designating post-MVH work; confirmed per-bead via `br show` (not trusted from `br ready --json` which omits labels) |
| `phase:0` / `tag:meta` open beads | 1 (hk-ahvq) | Spec-decomposition meta-work; Phase-0 epic tracked separately, not on critical path |
| Hollow spec-epic rollups | 10 | Non-dispatchable parent-tracking beads; auto-close at MVH cut |

Total excluded from roadmap: 138 open beads.

---

## End-of-MVH cleanup

These 11 beads are not dispatchable work. They close manually at MVH cut after all task-beads under them are resolved.

| ID | Spec | One-line |
|---|---|---|
| hk-872 | beads-integration | Beads Integration spec — implementation epic rollup |
| hk-b3f | execution-model | Execution Model spec — implementation epic rollup |
| hk-hqwn | event-model | Event Model spec — implementation epic rollup |
| hk-8i31 | handler-contract | Handler Contract spec — implementation epic rollup |
| hk-a8bg | control-points | Control Points spec — implementation epic rollup |
| hk-8mwo | workspace-model | Workspace Model spec — implementation epic rollup |
| hk-8mup | process-lifecycle | Process Lifecycle spec — implementation epic rollup |
| hk-sx9r | operator-nfr | Operator NFR spec — implementation epic rollup |
| hk-63oh | reconciliation | Reconciliation spec — implementation epic rollup |
| hk-i0tw | scenario-harness | Scenario Harness spec — implementation epic rollup |
| hk-ahvq | (meta) | Phase 0 completion — load remaining pilots and exit to code phase; substantive meta-task, not a rollup; reviewed separately at MVH cut |

---

## Operator's worked example (the doc's reason for being)

This is what "add a task → run → it processes" looks like once MVH lands.

1. **Add a task.** Operator runs `br create "Implement feature X"`. The bead is created in `parked` state. A readiness workflow (task ingestion, separate from MVH scope) moves it to `ready`. The corpus already supports this: `br list --status ready` shows the bead. *(No MVH bead enables this step — it is already operational.)*

2. **Start the daemon.** Operator runs `harmonik start` (or the equivalent foreground binary per the MVH daemonization deferral decision from 2026-05-08). `daemon.Start(cfg)` executes PL-005 step 0: instantiates the EventBus concrete impl (closed: hk-8mup.62), registers the redaction patterns via `RedactionRegistry.RegisterPattern(…)` **(hk-8i31.83)**, installs the redaction middleware in the bus producer path **(hk-8i31.37)**, and seals the bus.

3. **Daemon emits its first events.** `daemon_started` (event-model §8.7.1) flows through `Emit`. Because the redaction middleware is installed **(hk-8i31.37 → hk-hqwn.45)** and `Emit` honors the full dispatch sequence **(hk-hqwn.19)** — redaction then JSONL fsync then synchronous-consumer dispatch — the event is durably written to `.harmonik/events/events.jsonl` and delivered to any synchronous consumers without secrets leaking into the log.

4. **Daemon picks up the ready task.** After the Cat 0 pre-check and reconciliation pass (PL-005 steps 3–8; reconciliation spec is post-MVH for full investigator workflows but the startup sweep is already scaffolded), the daemon finds the `ready` bead via its Beads-CLI skill integration and emits `node_dispatch_requested` (event-model §8.1.11). `Emit` again uses the fully-wired dispatch path **(hk-hqwn.19)**.

5. **Sub-agent dispatched and completes.** The daemon launches a Claude Code sub-agent (handler-contract.md §4.1 HC-001, `internal/handlercontract/handler_hc001.go`) against a task branch (workspace-model WM-009, already in `internal/workspace/`). The agent works to completion and calls `emit-outcome`. The daemon receives the outcome via the socket (PL-003), updates bead state via the Beads-CLI skill, and emits `run_completed` durably through the now-correct `Emit` path.

**The 4 MVH critical-path beads are the last normative gap between the current scaffolded state and a bus that can durably record a completed run without leaking secrets.**

---

## Gaps — beads that should exist but don't

| Proposed title | Spec ref | Why it's missing |
|---|---|---|
| `daemon.Start` integration test: wires bus + redaction + dispatch end-to-end against a no-op task | process-lifecycle.md §4.2 PL-005 step 0, event-model.md §4.4 EV-014a | The composition root is scaffolded (hk-8mup.61 closed) and the EventBus impl is closed (hk-8mup.62), but no bead captures a live-dispatch round-trip test that exercises the full `Start → Emit → JSONL → consumer` chain. The specaudit sensor for PL-020 (composition root structure) is present; an integration-level smoke test confirming the wired system runs is absent. |
| `busimpl` JSONL writer wiring: `internal/eventbus/` `busimpl` calls `internal/eventbus/jsonlwriter.go` correctly (EV-015 / EV-016 fsync) | event-model.md §4.2 EV-015, §4.3 EV-016 | `jsonlwriter.go` and `jsonlwriter_test.go` exist. The busimpl description (hk-8mup.62, now closed) references wiring it in; no standalone bead confirms the `busimpl → jsonlwriter` seam is enforced by a sensor or test. Gap may be covered by existing tests — worth confirming before filing. |
