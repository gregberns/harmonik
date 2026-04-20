# Questions for the User

> Accumulator for decisions that are critical to the architecture or UX and need the user's input. Reviewed together in a batch, not one at a time.
>
> Last updated: 2026-04-19

## How to read this file

Each question follows a consistent shape:

- **Question** — what needs to be decided
- **Why it's here** — why this isn't a straightforward call I should make alone
- **What I've thought through** — options considered, with the tradeoffs
- **My lean** — where I'd land if forced to decide, and the assumption behind that lean
- **Blocks** — what this decision unblocks

Questions are added as they surface during overnight work. Resolved questions get moved to the Resolved section at the bottom with the user's answer.

---

## Open — Foundation scope

### Q-F1. Should foundation include an operator-CLI surface spec?

- **Status: RESOLVED 2026-04-19** (by orchestrator agent during problem-space round-2 revision; user to confirm in morning).
- **Resolution:** Operator-control **semantics** (between-task invariant, pause/stop/upgrade state machine, queue-format compat contract, graceful shutdown ordering) are in foundation — they are architectural invariants every subsystem must honor. Operator-control **surface** (CLI flags, API shape, dashboard UI) defers to a separate spec work. The split matches Architect review F6 and Critic review F4 (both flagged the original conflation as a blocker).
- **Why this was split:** The between-task invariant is load-bearing — if it's not in foundation, subsystem specs invent their own interruption semantics. The CLI shape is delivery, not semantics, and a change to CLI flags should not require a foundation revision.
- **Flag for user review:** Confirm the split in morning; the semantics-in-foundation + surface-in-separate-work approach is the most defensible reading of the original question, but the user hasn't explicitly approved the split.

### Q-F2. NFRs as a foundation spec or as attributes on every subsystem spec?

- **Why it's here** — Non-functional requirements (performance, observability, security, reliability, cost) can either be one central spec every subsystem conforms to, or a set of attributes each subsystem spec carries. The choice shapes every spec we write downstream.
- **What I've thought through** — 
  - (a) One `nfr.md` spec with the cross-cutting requirements. Each subsystem spec cites it and notes deviations.
  - (b) Each subsystem spec has its own NFR section; no central doc.
  - (c) Hybrid: central doc for the cross-cutting NFRs (observability protocol, cost/budget model, security posture) + subsystem-specific NFRs inside each subsystem spec.
- **My lean** — (c). Rationale: some NFRs are genuinely cross-cutting (logging/tracing protocol, budget semantics), others are local (S03's disk durability guarantees). Assumption: the cross-cutting set is small enough to fit in one foundation doc.
- **Blocks** — foundation decompose pass; NFR component.

---

## Open — Surfaced during overnight recon (2026-04-19)

### Q-R1. Attractor mischaracterization in existing docs

- **Why it's here** — Discovery during overnight recon: `/Users/gb/github/harmonik/docs/subsystems/orchestrator-core.md` line 52 calls Attractor a "spec for distributed workflow coordination; likely covers patterns we need around durable execution and replay." The Attractor repo itself describes Attractor as a **DOT-based pipeline runner** with JSON-snapshot durability and single-threaded traversal — functionally the same family as Kilroy, not a DTW engine. The existing framing is wrong and will mislead subsystem specs that try to use Attractor's "DTW patterns."
- **My lean** — Straightforward fix: correct the orchestrator-core.md framing in a backlog task. No architectural decision needed.
- **Blocks** — Nothing directly, but subsystem specs that read the incorrect framing may adopt Attractor-shaped patterns thinking they're getting distributed-durability. Flagging for visibility.

### Q-R2. Do we want a real DTW (distributed-durable-workflow) reference at all?

- **Why it's here** — Follow-on from Q-R1. If Attractor is *not* a DTW reference, and the harmonik team's docs repeatedly gesture at "durable execution, replay, long-running orchestration," we either (a) don't actually need DTW semantics and the existing framing was overreach, or (b) we do need them and should study Temporal, Restate, or DBOS as real references. Architecture-critical because it shapes the orchestrator's persistence/replay model.
- **What I've thought through** — Harmonik's current model (JSONL source-of-truth + git checkpoints per node) is *similar* to event-sourcing but is NOT a DTW in the Temporal sense (no deterministic replay with automatic compensation). If harmonik needs hours-long workflows that survive process restarts and resume exactly where they left off — yes DTW. If workflows complete within a single orchestrator lifetime and restart = abort-and-retry, no DTW.
- **My lean** — (b) but lightweight: study Temporal's core model (event history, workflow as pure function of events, deterministic replay) and selectively adopt. Full DTW is over-engineering for MVH.
- **Blocks** — foundation's "core execution data model" and "deterministic vs probabilistic boundary" decisions.

### Q-R3. Kilroy concept doc undercounts taxonomies

- **Why it's here** — Overnight recon found Kilroy actually has 6 failure classes (transient_infra, budget_exhausted, compilation_loop, deterministic, canceled, structural) and 6 fidelity modes (full, truncate, compact, summary:low/medium/high), not the 3/4 in the concept digest. This is a straightforward doc update.
- **My lean** — Straightforward fix. Backlog.
- **Blocks** — Nothing, but foundation's failure-taxonomy spec may need to re-evaluate whether the harmonik set should be 3, 6, or something custom. Noting for decompose pass.

### Q-R4. Binary signing for pause-to-upgrade (security gap)

- **Status: RESOLVED 2026-04-19 for MVH** (by orchestrator agent during problem-space round-2 revision; user to confirm in morning).
- **Resolution:** Commit-hash check as MVH integrity gate (option b). Full binary signing (option a) is deferred to post-MVH. Rationale: option b is cheap and closes the biggest risk (rogue binary from wrong source) while foundation/MVH ship; option a is a better security posture but not load-bearing for MVH's threat model (single-operator, single-project, no multi-tenancy).
- **Flag for user review:** Confirm commit-hash-check + signing-later in morning.

### Q-R5. Durability semantics — fsync, event-loss window, RTO

- **Why it's here** — NFR recon flagged: JSONL is declared the source of truth, but there is no fsync policy, no event-loss window ("if the orchestrator crashes, how many events can be lost?"), no orchestrator-restart RTO. These are hot-path decisions. Architecture-critical because they shape every event producer's code.
- **What I've thought through** — Options ranging from "fsync every event (expensive, zero loss)" to "fsync on flush timer (cheap, seconds of loss)" to "fsync on cycle boundary only." Also: is an event lost if the process crashes *during* writing it? Is every event idempotent, or do consumers need dedup?
- **My lean** — Fsync on cycle boundary with a timer-based flush for in-flight cycles (best effort) gives MVH a simple, defensible answer. Zero-loss is a post-MVH upgrade. Event producers MUST produce idempotent events (so loss-and-replay is safe).
- **Blocks** — foundation's event-model spec.

### Q-R6. Observability format — OTel, Prometheus, or custom?

- **Why it's here** — NFR recon noted: we have tmux + event bus + session logs (good), but distributed tracing, metrics exposition format, and a standard structured-log format are all missing. UX-critical for operators; architecture-critical because it constrains every subsystem's logging code.
- **My lean** — OpenTelemetry (OTLP) for spans + metrics; keep the event log (JSONL) as-is but emit structured logs in a stable JSON schema that tools can ingest. Minimal custom work, max ecosystem compatibility.
- **Blocks** — foundation's NFR spec on observability.

### Q-R7. "Cycle" terminology — consolidate or rename

- **Why it's here** — Subsystem audit found "cycle" used three ways: (1) Kilroy cycle detection (caps on edge traversals to prevent loops), (2) self-build cycle in bootstrap.md (one workflow execution that produces a mergeable change), (3) improvement cycle (S09 pause-for-improvement between tasks). UX/architecture affecting because docs and code will be confusing unless these are distinct.
- **My lean** — Rename: (1) stays "cycle detection" or "loop detection"; (2) becomes "run" or "workflow run" (one execution of a workflow from initial state to terminal state against one input); (3) stays "improvement cycle" but explicitly named as an operator-control-scoped term. Three terms; three concepts.
- **Blocks** — Naming conflict resolution in foundation's core-execution-data-model spec.

---

## Open — Architectural contracts

### Q-A1. Task-as-data-type: what is the canonical definition?

- **Why it's here** — Every subsystem interacts with tasks. Getting this shape wrong ripples through S01/S02/S03/S05/S09. Architecture-critical.
- **What I've thought through** — A task needs an identity, a workflow reference, a state, input/context, and lifecycle events. Open questions: does a task carry its full input payload in-memory or via a workspace pointer; does a task own its workspace or is workspace a sibling concept; is "task" the same granularity as "workflow execution" or are they different levels (workflow composed of tasks vs workflow IS a task).
- **My lean** — Task = one execution of a workflow graph against one input. Input is by workspace reference (worktree path), not inline. Workspace is a sibling concept tasks acquire and release. Granularity: one task = one workflow graph traversal. This will be proposed in detail during research pass.
- **Blocks** — essentially all subsystem specs; event model; workspace model.

### Q-A2. Node contract scope: what does a node minimally provide?

- **Why it's here** — The node contract determines whether agents, twins, scripts, tests, and other primitives can coexist in the same graph. Consulting Kilroy and Attractor per user's instruction.
- **What I've thought through** — Deferred to research pass; sub-agent will inventory Kilroy and Attractor node definitions and propose a synthesized contract.
- **My lean** — Pending research.
- **Blocks** — foundation node-contract spec; S01 orchestrator spec.

### Q-A3. Event schema v1: minimum viable field set?

- **Why it's here** — JSONL is the source of truth (locked decision). The specific schema is architecture-critical: it defines what observability and the memory layer can actually see.
- **What I've thought through** — Deferred to research pass; baseline candidates from Kilroy and OpenTelemetry semantics will be reviewed.
- **My lean** — Pending research.
- **Blocks** — S03 event bus spec; S08 memory spec; improvement loop spec.

---

## Open — Parked details from STATUS.md (pre-existing)

These predate tonight's work and are listed in STATUS.md §Open Architectural Questions. Carried forward here for visibility in the morning review.

### Q-P1. JSONL rotation, retention, indexing policy.
Size will grow fast; need a policy before storage surprises us. Sidecar index: sqlite? duckdb? deferred? Blocks S03 spec.

### Q-P2. Scenario definition format.
YAML vs Go-as-code vs hybrid. Blocks S07 spec.

### Q-P3. Workspace conflict resolution role.
Dedicated merge-agent type, original implementer, or always escalate? Blocks S06 spec.

### Q-P4. Twin conformance plan.
How to keep twins honest against real-agent drift. Needs a "conformance suite" concept. Blocks digital-twins concept + S07.

### Q-P5. Queue state format compatibility window.
What's the contract harmonik versions honor across upgrades? Blocks upgrade control (bootstrap.md §4.3).

### Q-P6. Configuration scope.
Runtime config vs workflow definition vs operator-policy file. Where do cadence/upgrade rules live? Blocks every subsystem with configurable behavior.

### Q-P7. Pi session-log format and location.
Needs concrete investigation before the Pi handler can support memory ingestion. Blocks S04 Pi support.

---

## Open — User decisions on bootstrap.md (pre-existing, from TASKS.md Group A)

Listed here for completeness. Full framing is in TASKS.md and docs/bootstrap.md.

- **Q-B1. §2 MVH cut** — Pi handler in MVH or post-MVH? Scenario harness CI in MVH or later?
- **Q-B2. §3 Workflow lifecycle** — Where do human gates sit? revision_loop retry cap? Cycle = subsystem or spec slice?
- **Q-B3. §4 Operator controls** — Stop default (graceful vs immediate)? Queue compat contract? Single interface or risk-differentiated? Where does pause/upgrade config live?
- **Q-B4. §5 Build order** — Orchestrator before policy — stub policy at step 5 or wait until step 7? Scenario stub timing? Twin as binary on day 1 or stub-script?
- **Q-B5. §6 Risk specifics** — Regression baseline shape? Sample human review cadence in Phase 2?

---

## Resolved

_(Moved here as questions are answered. Empty at project start.)_
