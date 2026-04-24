# Harmonik Foundation — Problem Space

> Pass 1 output for the `harmonik-foundation` kerf work (spec jig).
>
> Created 2026-04-19. Revised 2026-04-19 after round-1 review (5 personas; 12 unique blockers consolidated).

## Summary

Harmonik is a composable agentic orchestration system being built **spec-first**: the spec describes how the system operates, and code is written to match. Nine subsystem docs, seven concept docs, seven problem docs, seven goal docs exist in the knowledge base. No code has been written.

The **foundation** spec set is the first normative spec layer. It defines the cross-cutting contracts every subsystem spec must honor: the architectural invariants (the deterministic/probabilistic boundary, the ZFC classification rule, the search+verifier+traces requirement), the shared data model (workflow, node, run, transition, event, workspace, outcome), the process-boundary contracts (handler wire protocol, concurrency, context, errors), the control-flow primitives (gates, hooks, transition guards as one unified concept), the operator-control semantics (the between-task invariant, pause/stop/upgrade state machine), and the subsystem envelope (what any subsystem — including a future S10 — must declare and inherit).

**Why foundation has to be done first.** A subsystem audit (`.kerf/recon/subsystem-audit.md`) found ~25 undefined data types, 9 concepts used but not defined, 7 naming conflicts across subsystems ("cycle", "gate", "policy", "hook", "workspace", "session", "verification" each mean different things in different docs), and 23 parked open architectural questions. If subsystem specs are written next without a foundation, each subsystem invents its own shared vocabulary and the interfaces drift. The spec jig's integration pass can detect drift but cannot arbitrate it. Foundation fixes the shared vocabulary once.

**Foundation's correctness is validated against MVH** (see Constraints §"MVH as forward-reference"). If the MVH described in `docs/bootstrap.md §2` is not buildable from foundation specs alone, foundation is incomplete — regardless of how many criteria it formally satisfies.

## Architectural frame (operational, not just content)

Four architectural commitments are load-bearing and apply *across* every spec area. Foundation treats them as operational tests, not doc sections.

### 1. The deterministic/probabilistic boundary

Harmonik is a "deterministic skeleton, probabilistic organs" system. Foundation operationalizes this by naming **four axes** along which any operation is classified, and by requiring every normative type/interface/evaluation point in foundation to be tagged on each axis.

The axes:
- **LLM-freedom.** Does this operation call an LLM? (Yes/no.)
- **I/O determinism.** Given identical inputs, does this operation always produce identical outputs? (Yes/no.)
- **Replay-safety.** Can this operation be re-executed from the event log and produce the same observable effect without external side effects re-firing? (Yes/no.)
- **Idempotency.** Does calling this operation N times have the same effect as calling it once? (Yes/no.)

Skeleton operations are expected to be (no LLM, yes deterministic, yes replay-safe, yes idempotent). Organ operations are typically (yes LLM, no deterministic, no replay-safe, idempotency-by-design). The boundary is drawn where these properties change.

### 2. ZFC classification test — mechanism vs. cognition

Every evaluation point in the system is either **mechanism** (deterministic structure, schema checks, type checks, state transitions, policy rules — allowed in framework code) or **cognition** (ranking, scoring, plan composition, semantic analysis, quality judgment — must delegate to a model). Foundation specs tag each evaluation point as mechanism or cognition. Cognition-tagged points must name the delegation path.

If a spec introduces a framework-level semantic judgment (keyword matching for completion, heuristic fallback tree, regex-based parsing of unstructured output, hardcoded scoring of output quality), it fails ZFC and requires explicit rationale.

### 3. Centralized-controller principle

Harmonik is a centralized-controller system, explicit inverse of the Gas Town polecats/mayors decentralized-orchestration pattern. The deterministic daemon (Go, no LLM logic) owns all workflow state, routing, and dispatch. Agents perform only cognitive work. Agent-to-agent coordination goes through the daemon — not through files or ad hoc IPC between agents. A merge agent (when a workflow uses a distinct merge node) operates in the SAME worktree the implementer used; the worktree is leased by the workflow RUN, not by any individual agent. This principle is load-bearing: it collapses an entire class of coordination problems (file reservations, agent-to-agent message routing, distributed consensus) that Gas Town's decentralized model would otherwise require. Foundation operationalizes this via the process-lifecycle component (daemon scope, deterministic dispatch) and the workspace-model component (worktree leased by run).

### 4. Search + verifier + traces as required mechanisms

Per the AlphaGo-modeled north star (`docs/concepts/alphago-system.md`), **all three are required**:
- **Search** — backtracking, candidate generation, controlled openings. Foundation must specify how backtracking is represented (at minimum: local patchback, architectural rollback, policy rollback, context rollback) even if the implementation is deferred.
- **Verifier** — verification-as-node-type per locked decision #9. Foundation specifies what a verifier node is and what it emits.
- **Traces** — comprehensive transition logging: prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence. Traces differ from events: events are signals; traces are decision records. Foundation defines both and their relationship.

## Goals

1. **Produce normative specs for cross-cutting contracts.** Every data type, event, and interface that crosses subsystem boundaries has exactly one authoritative definition, owned by one foundation spec. Subsystem specs cite without extending.

2. **Resolve the cross-cutting architectural questions that block subsystem specs from being written independently.** Concretely, answer:
   - Workflow definition schema + version-control contract
   - Deterministic/probabilistic boundary axes (see Architectural Frame §1) and which side of the boundary each type/event/interface is on
   - Event schema + versioning + compatibility window + clock source
   - Agent-handler Go interface + wire protocol + twin parity
   - Workspace and session-log aggregation strategy
   - Secrets delivery and handling across the agent-launch boundary
   - Fsync policy + event-loss window + restart RTO
   - Queue-format compatibility contract (even if binary signing defers to post-MVH)
   - Checkpoint-format stability contract
   - Concurrency model (goroutine ownership, channel closure, mutex discipline)
   - Context propagation (cancellation, deadlines, values across subsystem boundaries)
   - Error-type strategy (typed categories, sentinel values, wrap/unwrap policy)
   - Error propagation across async boundaries (timeouts, typed error events, dead-letter behavior)
   - Config loading precedence (runtime / workflow / operator-policy)
   - Graceful shutdown ordering / drain protocol
   - Health and liveness (external observability of the orchestrator itself)
   - Audit/compliance minimum (who approved what, when, why)
   - Resource-budget enforcement point (at dispatch? after accrual? real-time check?)
   - Convergence semantics (when parallel branches meet)
   - Operator-control semantics — between-task invariant, pause/stop/upgrade state machine

   This list replaces the "5 load-bearing decisions" framing from round-1 problem-space. The count is whatever emerges from decompose; the contents are the load-bearing ones, not the first five.

3. **Resolve the seven naming conflicts.** "Cycle", "gate", "policy", "hook", "workspace", "session", "verification" each receive a single definition. Alternative uses are renamed or explicitly distinguished as related-but-distinct concepts.

4. **Address every parked architectural question** from STATUS.md and the open-questions sections of subsystem docs — answer it in foundation, defer it to a named subsystem spec with reason, or explicitly out-of-scope it with reason. No parked question is left unaddressed.

5. **Normatively state the architectural principles.** Make declarative what is currently described exploratorily in concept docs: deterministic skeleton + probabilistic organs; ZFC mechanism/cognition test; search+verifier+traces; harness guides+sensors; filesystem-as-coordination-substrate; inspectability; testability-via-twins.

6. **Define the subsystem envelope** — what any subsystem (S01–S09 today, S10 tomorrow) must declare to participate in harmonik: events produced/consumed, handlers implemented, state owned, NFRs inherited, boundary classification for each operation it exposes.

7. **Define the foundation amendment protocol** — the concrete process for evolving foundation without reintroducing drift. The protocol specifies:
   - **Who may propose:** any downstream spec author (including subsystem spec works, implementation work, or operator feedback). Proposals identify the specific foundation section, the gap, the proposed change, and the downstream specs affected.
   - **Review gate:** every amendment is reviewed by at least two reviewer personas, one of which must be Architect. Architecture-critical amendments (changes to §1.1 axes, §1.4 subsystem envelope, SC-10 tagging rules, SC-12 convergence criterion) additionally require user sign-off — the same threshold used for problem-space itself.
   - **Acceptance criterion:** amendment proposals converge under the same SC-12 rule as primary passes (per-reviewer 0 Blocker + <3 Major). Non-converging amendments escalate to QUESTIONS.md.
   - **Propagation:** accepted amendments update the affected foundation spec; any subsystem spec that cited that section must re-review the citation in its next touch. Foundation spec versions bump; subsystem specs declare the foundation version they conform to.
   - **Timeline:** no fixed SLA. Amendments are prioritized by blockage impact (an amendment that blocks an active subsystem spec is prioritized over one that improves a completed spec).
   - **Rejection path:** an amendment may be rejected if the reviewers conclude the downstream spec should adapt within existing foundation constraints. Rejection is recorded; the rejected amendment is kept as documentation to prevent re-raising.

## Non-goals

Foundation does **not** cover:

- **Internals of any subsystem.** S01's state-machine implementation, S03's JSONL file layout, S08's CASS integration — each is its own subsystem spec work.
- **Implementation.** No Go code. No `go.mod`. No package structure in the sense of directory layout. However, foundation *does* specify the layering rules any Go implementation must respect (per `docs/01_architecture.md` "scalable to 500K LOC" requirement).
- **Operator CLI surface.** The *semantics* of operator controls (between-task invariant, pause/stop/upgrade state machine, queue-format compat contract) are in foundation. The *surface* (CLI flags, API shape, dashboard UI) is a separate spec work. This resolves Q-F1: semantics in, surface out.
- **Distributed tracing.** MVH is single-process per bootstrap.md §2. Tracing is a post-MVH concern; foundation instead specifies the structured-log format every subsystem emits.
- **Binary signing for pause-to-upgrade.** Post-MVH (Q-R4). For MVH, foundation specifies a commit-hash check as the integrity gate (cheaper and sufficient for the MVH threat model). Full binary signing is a post-MVH NFR.
- **Metrics exposition format** (Prometheus scrape endpoint / OpenTelemetry OTLP). Post-MVH for the *external wire format* (what a metrics scraper sees). Foundation defines the conceptual metric set and requires metrics to be emitted for MVH as (a) structured log entries with a `metric_name` field + numeric `value` field, AND (b) typed events of type `metric` in the event log. External scrapers reading Prom/OTel formats are post-MVH. This resolves the apparent contradiction between "structured-log format is in foundation" and "metrics exposition format defers": the internal emission is in foundation; only the external scrape protocol defers.
- **Human-facing documentation.** Knowledge-base docs (docs/problems/, docs/goals/, docs/concepts/) remain exploratory. Foundation specs are normative.
- **Per-subsystem NFRs.** NFRs specific to one subsystem (e.g., S03's fsync cadence, S06's workspace disk footprint) live in that subsystem's spec. Foundation covers only the *cross-cutting* NFRs.
- **Workflow library.** No example workflows, no "self-build workflow," no scenario examples. Produced after foundation.
- **Bootstrapping process detail.** Bootstrap gets its own kerf work once foundation lands. Foundation does not spec the bootstrap process. Foundation *is* validated against MVH (see Constraints §"MVH as forward-reference").
- **i18n / locale.** Explicitly out of scope. Reasoning: harmonik is an operator-facing tool with English-language LLM prompts; localization is a post-production concern.
- **Multi-tenancy / per-tenant cost attribution.** Out of scope for MVH. Harmonik MVH is single-user/single-project.
- **PII handling / data residency.** Out of scope until the system processes user-provided content beyond the operator's own repositories.
- **"Feature" as a product primitive.** Not introduced. Spec, workflow graph, and bead are three separate artifacts (design/thinking, normative execution graph, atomic queued work item). None projects from another. Work size varies by how many nodes / sub-graphs an agent composes, not by a distinct `feature` entity.

## In-scope silences to address

Round-1 critic review named 14 categories the original problem-space was silent on. Every one is now dispositioned:

| Silence | Disposition | Rationale |
|---|---|---|
| Clock / time-source model | **In foundation** (Event Model) | Affects every event consumer; replay semantics depend on it |
| Concurrency contract (goroutine ownership, channel closure, mutex discipline) | **In foundation** (Handler & Process Contract) | Cross-subsystem; Go-specific; drives every component's safety |
| Idempotency semantics | **In foundation** (Architectural Frame §1 + Event Model) | One of the four boundary axes |
| Error propagation across async boundaries | **In foundation** (Handler & Process Contract) | Affects S01↔S03↔S04↔S05 integration |
| `context.Context` propagation policy | **In foundation** (Handler & Process Contract) | Go-specific but cross-cutting; cancellation across subsystems |
| Error-type strategy (typed / sentinel / wrap) | **In foundation** (Handler & Process Contract) | Go-specific but cross-cutting |
| Handler wire format (stdin/stdout/files, JSON, versioning) | **In foundation** (Handler & Process Contract) | Binary-boundary contract; twins must match |
| Secrets / credentials | **In foundation** (Cross-cutting NFRs § security posture) | Cross-cutting; event-log-pollution risk |
| Operator CLI / API authentication | **Deferred to operator-surface spec** | Semantics in foundation (who can do what), surface later |
| Agent-to-orchestrator trust | **In foundation** (Handler & Process Contract) | Handler subprocess authenticity (is the `claude-twin` we launched the real `claude-twin`?) — resolved for MVH via launched-from-known-path-with-commit-hash-check; full process-identity attestation is post-MVH |
| Twin binary authenticity | **In foundation** (Handler & Process Contract) | Same mechanism as agent-to-orchestrator trust for MVH (launched from repo-relative path with commit-hash-check). Twin drift cadence (how often twins are re-verified against real agents) is deferred to S07 scenario-harness |
| Logging overhead budget | **Deferred post-MVH** | Performance optimization concern; MVH prioritizes correctness |
| Secrets in event payloads | **In foundation** (Event Model + Handler Contract) | Normative rule: event payloads MUST NOT contain secret fields. Redaction list in handler contract; schema review enforces. Any field whose name matches a secret-pattern regex is stripped before event emission |
| Config loading precedence (runtime/workflow/operator-policy) | **In foundation** (Architectural Frame + NFRs) | Q-P6 from STATUS.md |
| Graceful shutdown ordering / drain | **In foundation** (Operator Control Semantics) | Ties into pause/stop semantics |
| Health and liveness endpoints | **In foundation** (Cross-cutting NFRs § observability) | External observability of the orchestrator |
| Audit / compliance minimum | **In foundation** (Cross-cutting NFRs § observability) | Traces already required per Architectural Frame §4; audit is a subset of the trace contract |
| Resource-budget enforcement point | **In foundation** (Operator Control Semantics + Control Points) | Budget is a control point |
| Structured-log format | **In foundation** (Cross-cutting NFRs § observability) | Replaces the distributed-tracing concern for MVH |
| Queue-format compat window | **In foundation** (Operator Control Semantics) | Required even if binary signing defers |
| Checkpoint-format stability | **In foundation** (Core Execution Data Model) | Required for upgrade across MVH-v1→v2 |
| Fsync policy + event-loss window + RTO | **In foundation** (Event Model) | Hot-path decision affecting every event producer |
| Twin conformance | **Deferred to scenario-harness spec (S07)** | Subsystem-specific; foundation provides the *handler contract*, S07 specifies *how twins are kept honest against real* |
| Multi-repo workflows | **Deferred to post-MVH** | MVH operates against one repository at a time |
| Observability overhead budget | **Deferred to post-MVH** | Not blocking; MVH optimizes for correctness not throughput |
| Daemon / process lifecycle model | **In foundation** (Process Lifecycle) | Per-project daemon scope, startup/shutdown sequence, attach-UI separation, and daemon-vs-orchestrator-agent distinction are cross-cutting and must not be reinvented per-subsystem |
| Restart reconciliation model | **In foundation** (Reconciliation) | Category taxonomy, investigator-agent contract, and verdict vocabulary cross every subsystem that can be mid-run at crash time; replaces the DTW deferral with a concrete harmonik-native answer |

## Constraints

Fixed inputs that foundation must not contradict.

### Locked decisions (STATUS.md)

The ten architectural decisions from 2026-04-19 plus four additional decisions added 2026-04-20 and 2026-04-21. See STATUS.md §Decisions Locked In and §"Decisions Added 2026-04-20 and 2026-04-21". Summary:
1. Go. 2. Go-native orchestrator with Kilroy+Attractor as design references. 3. JSONL source of truth + in-process pub/sub. 4. NTM-wrapped Go; tmux inspectability required. 5. Claude Code + Pi handlers. 6. Separate twin binaries. 7. Worktree+merge workspace (Gas Town). 8. CASS-only memory MVH. 9. No verifier subsystem; verification as node type. 10. Operator controls between tasks.
11. **DOT as workflow representation.** Workflow graphs are expressed in DOT; policies remain YAML and are referenced by DOT node/edge attributes. Rationale: DOT is the smallest standard graph-serialization format with native node/edge attribute support, and it composes with existing graph-visualization tooling.
12. **No DTW (distributed durable workflow) adoption.** Q-R2 resolved 2026-04-20: harmonik does NOT adopt a DTW runtime. Restart recovery is agent-driven deterministic reconciliation against git + Beads; JSONL is observational, not replayed for state reconstruction.

    **Applicability conditions.** This decision is sound **on the following assumptions** — if any changes, the decision must be revisited:
    - **Single-machine execution.** Workflows run on one machine; no cross-host state replication is required.
    - **Cheap re-execution.** Task re-execution costs seconds of LLM time, not minutes of irreversible external action. Re-running an agent against a git-derived workspace is inexpensive.
    - **No irreversible external side effects.** Workflow nodes do not invoke operations that cannot be safely re-tried (deploy-to-prod, send-email, irreversible external API writes). If a workflow node carries irreversible effects, the current reconciliation model cannot guarantee exactly-once and DTW semantics must be revisited.
    - **Bounded wait-states.** Workflows do not include days-long external waits that DTW's wait-on-external-event primitive would natively handle.

    **If these conditions change** — specifically if harmonik grows to (a) span machines, (b) include irreversible external-action nodes, or (c) need multi-day external waits — reconciliation becomes insufficient and DTW (Temporal, Restate, or equivalent) must be re-evaluated. The reconciliation model is explicitly scoped to the MVH envelope; extending it to cover a DTW-shaped need is a foundation amendment, not an incremental change.

    Rationale for current scope: git + Beads already carry the authoritative state, and DTW's replay-on-restart model contradicts the store-authority discipline harmonik already requires. Within the MVH applicability conditions, reconciliation is a strictly simpler answer.
13. **Beads as task ledger.** Harmonik adopts `Dicklesworthstone/beads_rust` (SQLite-backed; NOT the Dolt variant) as the task queue / dependency graph. Rationale: typed dependency edges, atomic claim, stable bead IDs, and audit log are all pre-built in Beads; harmonik layers workflow orchestration on top rather than hand-rolling a queue.
14. **Handler skill-injection obligation.** The handler contract requires that the handler ensure the agent process has the skills/tools the workflow node requires. Skill declarations come from workflow node attributes (DOT or referenced YAML). Rationale: without this obligation, every agent-type would hand-roll skill provisioning; the obligation makes capability-supply a first-class harmonik concern (Beads CLI skill is the motivating instance).

### Architectural principles

Load-bearing concepts from `docs/concepts/`:
- Deterministic skeleton, probabilistic organs (alphago-system.md)
- Zero Framework Cognition: mechanism vs. cognition (zero-framework-cognition.md)
- Search + Verifier + Traces minimum pattern (alphago-system.md)
- Harness engineering: guides + sensors, constrain-to-empower, filesystem-backed coordination (harness-engineering.md)
- Role-based agents (seven roles per alphago-system.md §Role-Based Agent System): Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor

### Spec jig workflow

problem-space → decompose → research → change-design → spec-draft → integration → tasks → ready. Each pass produces its artifact before advancing status.

### MVH as forward-reference

Foundation is not complete until MVH per `docs/bootstrap.md §2` is buildable from foundation specs alone, without requiring a foundation revision. Concretely: for each MVH subsystem cut (S01–S08, S09 explicitly excluded per bootstrap), foundation answers every cross-cutting question that subsystem's MVH cut depends on. This is operationalized as Success Criterion SC-11.

### Knowledge base as source

Existing docs inform foundation; they do not bind it. Where exploratory docs are wrong or incomplete (see Recon-surfaced context below), foundation corrects, and the corrections are flagged in the affected-docs list for later cleanup.

## External inputs

Foundation inherits contractually from these external components. Their interfaces, capabilities, and constraints are external dependencies — version-pinned, not spec-revisable.

| Component | What foundation relies on | Versioning |
|---|---|---|
| Kilroy (github.com/danshapiro/kilroy) | Graph-as-workflow reference model, failure classification precedent, checkpoint-per-node precedent, goal-gate precedent. Harmonik may reference but does not adopt wholesale. | Concept-level reference; specific Kilroy version not pinned because harmonik is not using Kilroy's code. |
| NTM | Go wrapper for tmux-backed agent process management; inspectability mechanism. | Version-pinned when S04 implementation begins. |
| CASS | Session-indexing and retrieval; memory-layer MVH dependency. | Version-pinned when S08 implementation begins. |
| CASS Memory | Institutional-memory primitives; deferred until post-MVH per decision #8. | Not pinned yet. |
| adze | Environment setup within workspaces. | Version-pinned when S06 implementation begins. |
| git / git worktree | Workspace isolation primitive; checkpoint storage. | Minimum version TBD (git worktree v2+ sufficient). |
| Beads (`Dicklesworthstone/beads_rust`) | Task ledger: bead content, typed dependency edges (parent-child / blocks / conditional-blocks / waits-for), coarse bead status (open / in_progress / closed / deferred / tombstone), atomic claim, stable bead IDs, audit log. Accessed via the `br` CLI only (NOT Beads's MCP server). | Version-pinned per external-inputs protocol; pre-1.0 risk acknowledged — harmonik isolates the pin behind a thin `br`-CLI adapter and absorbs breakage in one place. |

## Recon-surfaced context

Findings from overnight recon (2026-04-19) that inform but do not bind foundation. These are *inputs to decompose*, not constraints. Foundation decompose and research passes will decide how much of each is adopted.

- **Attractor is a DOT-based pipeline runner, not a distributed workflow engine.** Same family as Kilroy; not a DTW reference. Harmonik docs that call it "distributed workflow coordination" are wrong (correction in TASKS.md backlog).
- **Kilroy's actual failure taxonomy is 6 classes** (transient_infra, budget_exhausted, compilation_loop, deterministic, canceled, structural), not 3 as the harmonik concept digest states. Foundation's failure-taxonomy decision is its own; Kilroy is a reference, not a default.
- **Kilroy's actual fidelity modes are 6** (full, truncate, compact, summary:low/medium/high), not 4.
- **Kilroy is fast-forward-only for fan-in.** Harmonik's Gas Town worktree+merge pattern is a **deliberate semantic divergence** — not a parameter tweak. Foundation's workspace model owns this divergence.
- **Kilroy `stack.manager_loop` is stubbed to FAIL in v1.** Recursive orchestration is aspirational in Kilroy; if harmonik needs it, it's harmonik's problem to solve.
- **Attractor Outcome shape** — `{status, preferred_label, suggested_next_ids, context_updates, notes}` — is a strong candidate for foundation's outcome spine, pending decompose-pass evaluation.
- **NFR inventory has 40+ missing NFRs** (`.kerf/recon/nfr-inventory.md`), not just the 10 in the top-10. The Success Criteria require every `missing` NFR to be dispositioned.
- **DTW (distributed durable workflow) — Q-R2 resolved 2026-04-20: harmonik does NOT adopt DTW.** Restart-reconciliation is agent-driven deterministic recovery against git + Beads; JSONL is observational, not replayed for state reconstruction. See locked decision #12. The in-flight-over-restart concern is answered by the reconciliation component (Component 9 in decompose) rather than by adopting a DTW runtime.

## Success criteria

Foundation is complete when all of the following hold. Each is decidable by automation or a named review.

**SC-1. Every undefined data type has a densely normative definition.** Each definition specifies (a) identity/key shape, (b) required fields with Go types, (c) lifecycle states with allowed transitions, (d) at least one cross-subsystem reference contract stating who-cites-whom. Decidable by reviewer grep for these four elements per type.

**SC-2. Every one of the 9 undefined concepts has either a densely normative definition or an explicit scope-out with reason.** Each normative concept definition specifies: (a) what the concept IS (one-sentence essence), (b) what it operates on or refers to (cites other foundation types), (c) its lifecycle if stateful (states and allowed transitions, or explicit "stateless"), (d) its cross-subsystem reference contract (which specs cite it for what purpose). Same density bar as SC-1 but applied to concepts rather than concrete data types. Concepts: workflow graph schema, agent type abstraction, convergence semantics, deterministic/probabilistic boundary, ZFC-compliant boundary, failure classification, ready-state detection, merge conflict resolution, process health.

**SC-3. Each of the 7 naming conflicts resolves to one canonical term.** Alternative uses are renamed or explicitly distinguished. Decidable by grep: the old ambiguous term appears ≤1 normative definition across foundation specs.

**SC-4. Every cross-cutting decision listed in Goal 2 is resolved.** Each is either answered normatively in a foundation spec, deferred to a named subsystem spec with written reason, or post-MVH-scoped with written reason. No item is left open.

**SC-5. Every parked question in STATUS.md and every open-questions section of the subsystem docs has a disposition** — answered / deferred-subsystem / deferred-post-MVH / explicitly-out-of-scope-with-reason.

**SC-6. Every `missing` NFR in `.kerf/recon/nfr-inventory.md` has a disposition** (not just the top-10). Dispositions: accepted / deferred-subsystem / deferred-post-MVH / rejected-with-rationale. Decidable by checklist.

**SC-7. Every normative claim has operationally adequate test specifications.** For each claim: happy-path test + defined failure-mode tests + at least one concurrency/partial-failure test, or an explicit statement that no such scenario applies with rationale. Decidable by reviewer audit.

**SC-8. No aspirational language in normative text.** Banned words: "appropriate", "adequate", "as needed", "handles gracefully", "is performant", "is reliable", "is clean", "is rigorous". Decidable by grep. (This list is normative; reviewers may add to it.)

**SC-9. Every inter-subsystem reference cites exactly one foundation spec as the normative definer.** Format: "Per [foundation-spec]§[section], {concept} is defined as…". Decidable by grep for the citation pattern.

**SC-10. Every normative type/interface/evaluation point is tagged on the four determinism axes** (LLM-freedom, I/O determinism, replay-safety, idempotency) **and tagged mechanism-or-cognition** per ZFC. Cognition-tagged points name the delegation path.

**SC-10a. The tagging itself is reviewed and auditable.** Tags are produced by the spec author (orchestrator agent) during spec-draft pass. Tags are reviewed by (at minimum) the architect persona in a dedicated pass-5 review pass whose sole task is tag audit. Disagreements between author-tagged and architect-reviewed classifications are recorded as findings and resolved before spec-draft advances. Additionally, tag correctness is revisited during integration review: if a cross-spec cite reveals a classification mismatch (one spec tags an operation mechanism while the consuming spec treats it as cognition), integration fails until the conflict is resolved.

**SC-10b. Tagging scope — cross-subsystem vs internal.** Cross-subsystem types, interfaces, and evaluation points (any type cited in a subsystem envelope declaration, any interface implemented across subsystems, any control-point used in a cross-subsystem policy) MUST carry tags. Subsystem-internal types may carry tags at the subsystem spec's option but are not foundation-audited. This keeps the tag burden finite while closing the drift-exposure surface.

**SC-11. MVH per bootstrap.md §2 is buildable from foundation specs alone.** Operationalized as a **buildability matrix** produced during the integration pass and reviewed in spec-draft and integration passes.

The matrix has:
- **Rows:** the eight MVH subsystems from bootstrap.md §2 (S01 orchestrator minimum, S02 YAML policies, S03 in-process + JSONL, S04 NTM + Claude Code + twin, S05 Claude Code hooks for completion, S06 worktree + adze + sequential-multi-agent, S07 one scenario + twin, S08 CASS pointed at session-log directory). S09 is NOT in MVH per bootstrap.md.
- **Columns:** the cross-cutting concerns from Goal 2 (workflow schema, determinism boundary, event schema, handler interface, workspace+session strategy, secrets, fsync/event-loss, queue compat, checkpoint stability, concurrency, context, error types, error propagation, config precedence, shutdown, health, audit, budget enforcement, convergence, operator semantics).
- **Cells:** one of `ANSWERED in spec X §Y` / `N/A for this subsystem (rationale)` / `DEFERRED-POST-MVH (rationale)` / `ESCALATED (in QUESTIONS.md)`.

An `UNANSWERED` cell → foundation incomplete → SC-11 fails. Matrix lives at `.kerf/projects/gregberns-harmonik/harmonik-foundation/06-buildability-matrix.md` and is a required artifact of integration pass. Any reviewer can verify SC-11 by spot-checking cells and cross-referencing the cited spec sections.

**SC-12. Every pass converges to the defined threshold.** Pass converges when each individual reviewer persona in the most recent round reports **0 Blocker-severity findings AND fewer than 3 Major findings** (per-reviewer, not summed across reviewers). Severity is determined by the reviewer; the synthesizer does NOT downgrade or merge-away findings during synthesis. If two reviewers produce the same finding at different severities, the higher severity is what the convergence criterion uses.

**SC-12a. Synthesizer conduct rules.** The orchestrator agent synthesizes review rounds. The synthesizer MUST: (i) preserve every finding from every reviewer verbatim or by direct reference in the synthesis document, (ii) record disagreements under an explicit `## Disagreements` heading, (iii) not re-classify severity. The synthesizer MAY: restate findings in its own words (with the original preserved in a linked or appendix file), group related findings by topic, and propose resolutions. Synthesizer outputs are themselves reviewed in the following round by at least one reviewer independent of the synthesizer.

**SC-13. `kerf square` passes** (all expected artifacts exist) and the spec-draft pass produces no unresolved Blocker-severity findings.

## Preliminary spec areas

Content-first description. File names chosen in decompose. These 7 areas are the preliminary cut; decompose may split or merge.

### 1. Architectural principles and classification tests

**What it contains.** The four determinism axes (LLM-freedom, I/O determinism, replay-safety, idempotency) and the tagging requirement. The ZFC mechanism/cognition classification test. Search + verifier + traces as a required triple. Harness engineering principles (guides + sensors, constrain-to-empower, filesystem-backed coordination). Role taxonomy (the seven alphago roles). The **subsystem envelope** — what any subsystem must declare. The **foundation amendment protocol** — how foundation evolves when downstream specs find gaps.

**What it answers.** What "deterministic" means concretely. How mechanism differs from cognition. What a subsystem is structurally. How foundation stays the source of truth.

### 2. Core execution data model and outcome spine

**What it contains.** The central types: `workflow`, `node`, `edge`, `run` (replaces the ambiguous "cycle"/"task"/"work item"), `state`, `transition`, `checkpoint`, `outcome`. Each with the SC-1 density (identity, fields, lifecycle, cross-subsystem contract). The **outcome/transition spine**: handler outcome → hook → gate → transition → event → trace, as a single integrated contract that crosses Areas 2, 3, 4, 5. The **failure taxonomy** (harmonik's own; Kilroy informs but does not bind). The **backtracking** model (four rollback types per AlphaGo north-star: local patchback, architectural, policy, context). **Checkpoint-format stability** contract.

**What it answers.** What a workflow is as a data structure. What a run is. How handlers produce outcomes that flow through hooks and gates. How rollback is represented. How checkpoints stay readable across versions.

### 3. Event model

**What it contains.** JSONL schema (id, type, timestamp, workflow_id, run_id, source_subsystem, payload, trace_context). Typed-payload taxonomy. Topic/routing semantics. Producer/consumer contract. Versioning and compatibility window. Replay semantics (what replay means when effects include external writes). Clock source (monotonic for ordering within a process; wall-clock for event timestamps). **Fsync policy + event-loss window + restart RTO.** Relationship between events and traces (events are signals; traces are decision records; both persist).

**What it answers.** What every event carries. How producers and consumers coordinate across versions. How events survive crashes. How event ordering is determined.

### 4. Handler & process contract

**What it contains.** The Go interface every agent handler implements (Claude Code, Pi, twin-Claude, twin-Pi, future handlers) — method signatures, lifecycle semantics. The **wire protocol** for the process-boundary contract: how the orchestrator invokes a handler process, what stdin/stdout/files are used, JSON message shapes, version negotiation. **Concurrency model**: goroutine ownership rules per subsystem, channel closure responsibilities, mutex discipline, work-queue patterns. **`context.Context` propagation**: cancellation, deadlines, values across subsystem boundaries. **Error-type strategy**: typed error categories, sentinel values, `errors.Is`/`errors.As` policy. **Error propagation across async boundaries**: how a handler crash mid-run becomes a typed event. **Secrets delivery**: how API keys reach the handler process (env var? file? socket?). **Handler session-log emission**: the signal that an agent has written a session log and where.

**What it answers.** How the orchestrator talks to handlers. How real handlers and twins interchange. How Go concurrency primitives are owned across subsystems. How errors and cancellation propagate.

### 5. Workspace and session model

**What it contains.** `workspace` as a data type (identity, state machine: create → lease → in-use → merge-pending → merged/discarded). Worktree creation, environment setup via adze, session-log placement inside the worktree, merge-back protocol. **Merge semantics that diverge from Kilroy's fast-forward-only fan-in.** The canonical session-log directory or per-worktree aggregation strategy (resolves Q-P7, the Pi log question). Workspace interaction with the between-task pause.

**What it answers.** What a workspace is as a data structure. How sessions log. How parallel branches merge when both changed code. How workspaces interact with operator pauses.

### 6. Control points, policy, and role

**What it contains.** **Control points as one primitive** (unifying gate, hook, transition-guard): a control point has a trigger, an evaluator (mechanism or cognition), and an outcome. Policy as data: YAML schema for policies, transition guards, freedom profiles, approval gates. **Role taxonomy** (concrete; builds on alphago's seven roles). **Config loading precedence**: runtime config vs workflow definition vs operator-policy file. **Resource-budget enforcement point**: budgets declared where, enforced where, with what pre-exhaustion warning threshold.

**What it answers.** What a gate/hook is (one thing, parameterized). How policies are structured. How roles are named. Where config comes from. How budgets actually stop things.

### 7. Cross-cutting NFRs and operator-control semantics

**What it contains.** **Observability protocol**: structured-log format every subsystem emits, conceptual metric set (without Prom/OTel wire format — that's post-MVH), health/liveness contract, audit-record requirements (subset of traces). **Security posture**: secrets lifecycle, command-execution sandbox, network egress policy, prompt-injection defense. **Operator control semantics**: the between-task invariant (what "task" means, where the boundary is), pause/stop/upgrade state machine (states, transitions, triggers), queue-format compatibility contract (N-1 supported; breaking change requires migration release). Graceful shutdown ordering. Commit-hash check as MVH integrity gate (Q-R4 resolution).

**What it answers.** What every subsystem must log, and how. What security guarantees the system makes. What the operator actually does, operationally (pause, stop, upgrade). How harmonik upgrades without breaking in-flight queues.

## Dependencies

### Foundation depends on

- Knowledge base documents as exploratory source material (not binding).
- External components (Kilroy, NTM, CASS, adze, git) as contractually-pinned dependencies.
- `docs/bootstrap.md §2` MVH cut as a forward-reference validation target.

### Foundation enables

- All subsystem spec works (harmonik-s01 through harmonik-s09). Subsystems cite foundation.
- The bootstrap spec work. Bootstrap references concrete foundation types and interfaces.
- All implementation work. Code builds against spec.

### Intra-foundation dependency graph

These foundation areas cross-depend; decompose must produce an explicit cross-reference map.

- Area 2 (Core Model) and Area 3 (Event Model) co-depend: events reference run/state/transition; runs/states/transitions emit events. **Resolution rule:** Area 2 owns the type definitions; Area 3 owns the wire formats. Each cites the other directionally.
- Area 2 (Outcome Spine) and Area 4 (Handler Contract) co-depend: outcomes are produced by handlers; handler contracts specify what outcomes contain. **Resolution rule:** Area 2 owns the outcome shape; Area 4 owns the handler's obligation to produce it.
- Area 6 (Control Points) and Area 7 (Operator Control Semantics) co-depend: both use "control" in different senses. **Resolution rule:** Area 6 is about control points *inside* workflows (gates/hooks/guards); Area 7 is about controls *over* the orchestrator itself (pause/stop/upgrade). Distinct, named differently in the specs.

## Affected existing docs

These docs will need updates after foundation lands. Tracked in TASKS.md backlog, NOT part of foundation itself.

- All nine subsystem docs under `docs/subsystems/` — each replaces locally-defined concepts with foundation citations.
- `docs/subsystems/orchestrator-core.md` — correct the Attractor mischaracterization (see Recon-surfaced context).
- `docs/concepts/kilroy.md` — correct the 3-vs-6 failure-class and 4-vs-6 fidelity-mode undercounts.
- `docs/concepts/alphago-system.md` — add annotation pointing to foundation specs where its exploratory architecture is now normative.
- `docs/concepts/zero-framework-cognition.md` — add annotation: the mechanism/cognition classification is now operational (Architectural Frame §2).
- `refs/AlphaGo-modeled-orch-system.md` — annotate as superseded by foundation where normative content overlaps.
- `STATUS.md` — move resolved parked questions from "Open" to "Resolved (in foundation)."

## Review discipline

### Personas

Foundation uses 5 reviewer personas per pass at minimum (7 at spec-draft where the normative text lands): Architect, Critic (adversarial), QA, Simplicity Advocate, Implementer (Go). Spec-draft adds Operator and Security. Persona prompts in `reviewers/PERSONAS.md`.

Downstream subsystem works may tier reviewer counts (2 for lightweight passes, 3 for middle, 5 for heavyweight). Foundation does not tier — every pass is heavyweight.

### Reviewer packet contract

Every reviewer packet includes:
- The artifact under review.
- The relevant persona brief from PERSONAS.md.
- Companion source documents (recon findings, prior-pass artifacts).
- **A "superseded by foundation" list** — existing knowledge-base docs the reviewer might read whose framing foundation has corrected or replaced. Reviewers should not cite these as authority.

### Synthesis

After each review round, the **orchestrator agent** (me) synthesizes across personas. Synthesis includes a `## Disagreements` heading where reviewer findings conflict, with the orchestrator's judgment recorded. Critic-reviewer independently reviews the synthesis in the next round — the synthesizer does not self-adjudicate.

### Convergence

Convergence is defined in SC-12 (each reviewer: 0 Blocker + <3 Major; synthesizer does not adjust severity). Up to 3 review rounds per pass. If a pass does not converge in 3 rounds, unresolved items escalate to `QUESTIONS.md` for the user to resolve. Subsequent passes block on those resolutions only if the unresolved items are in the subsequent pass's required-input surface.

## What this problem space explicitly acknowledges

- **Foundation is more work than "just start writing subsystem specs."** The bet is that up-front cost beats reconciling drift across nine subsystem specs. Recon evidence (~25 undefined types) supports the bet.

- **Foundation will conflict with some exploratory content in the knowledge base.** Conflicts resolve in foundation's favor. Affected docs are tracked for post-foundation cleanup.

- **Foundation is not the final spec.** Subsystem specs add detail. Foundation defines contracts and the shared model; subsystems define their slices within those contracts.

- **Foundation is validated against MVH, not against exhaustive theoretical completeness.** SC-11 forces the MVH-buildability check. If foundation can be "complete" but MVH can't be built, foundation is wrong.

- **Some decisions made in foundation are architecture-critical.** Those surface to `QUESTIONS.md` as they arise; the user reviews in batches.

- **Scope is defined negatively as well as positively.** The "In-scope silences to address" and "Non-goals" tables are as normative as the goal list. Reviewers should flag attempts to drift scope in either direction.
