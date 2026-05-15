# Architecture

```yaml
---
title: Architecture
spec-id: architecture
requirement-prefix: AR
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.3.1
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-24
depends-on: []
---
```

## 1. Purpose

This spec defines harmonik's architectural meta-contracts — the invariants, classification tests, and declaration rules that every other foundation and subsystem spec is required to conform to. It names the four-axis determinism classification, the ZFC mechanism/cognition test, the search+verifier+traces triple, the subsystem envelope, the MVH runtime realization of a subsystem as a Go package, the foundation amendment protocol, the agent-type abstraction, the verification-term disambiguation, the centralized-controller invariant, and the three-artifact separation of `spec` / `workflow graph` / `bead`.

It is a separate spec from everything else because these rules are meta-rules: they constrain how downstream specs are shaped, not the runtime shape of any single subsystem. Downstream specs cite §4.1 (four-axis classification), §4.2 (ZFC test), §4.4 (subsystem envelope), §4.9 (centralized-controller), and §4.10 (three-artifact separation) at minimum. Citations to sub-requirements MUST use the `AR-NNN` form; section numbers alone are insufficient to target the re-review obligation in §4.6.AR-020.

## 2. Scope

### 2.1 In scope

- Four-axis determinism classification test (LLM-freedom, I/O determinism, replay-safety, idempotency) and default-baseline rule.
- ZFC (Zero Framework Cognition) mechanism/cognition classification test and the delegation-path obligation for cognition-tagged points.
- The required triple — search, verifier, traces — declared at foundation level.
- The subsystem envelope: what every subsystem declares, what it is forbidden from doing, the "add-a-subsystem" procedure.
- MVH runtime realization pin: a subsystem is a Go package inside the daemon binary; out-of-process actors are enumerated and excluded from subsystem status.
- Foundation amendment protocol, including parallel-amendment serialization and overlap detection.
- Agent-type abstraction: concept, identifier shape, reserved identifiers, cross-subsystem reference points, orthogonality to role.
- Verification naming: the three distinct meanings (`verification-node`, `verification-result`, `quality-gate`) and the hyphenated canonical forms.
- Role taxonomy glossary entries for deferred AlphaGo abstractions (Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor); MVH-required vs declared-but-deferred split; orthogonality to agent type; merge-responsibility clarification.
- Harness-engineering invariants (single-source-of-truth repo, guides+sensors, constrain-to-empower, filesystem-backed coordination, quality-left).
- Centralized-controller principle and its acknowledged tradeoff (no graceful degradation under daemon failure).
- Three-artifact separation (`spec`, `workflow graph`, `bead`) and the explicit exclusion of "feature" as a product primitive.

### 2.2 Out of scope

- Internal behavior of any specific subsystem — each subsystem's own spec owns its requirements.
- Event types, payload schemas, envelope shape, fsync policy — owned by [docs/foundation/components.md §3] (event-model) until event-model.md lands.
- Workflow execution semantics (state machine, edge cascade, checkpoint cadence, outcome shape, failure taxonomy) — owned by [docs/foundation/components.md §2] (execution-model) until execution-model.md lands.
- Handler Go interface, wire protocol, secrets delivery, skill-injection mechanics — owned by [docs/foundation/components.md §4] (handler-contract) until handler-contract.md lands.
- Concrete permission schemas per role (what a Builder is allowed to do, what a Reviewer is allowed to do) — owned by [docs/foundation/components.md §6] (control-points) until control-points.md lands.
- Daemon process geometry, socket location, startup sequence — owned by [docs/foundation/components.md §8] (process-lifecycle) until process-lifecycle.md lands.
- Reconciliation category taxonomy, detection rules, investigator-agent contract — owned by [docs/foundation/components.md §9] (reconciliation) until reconciliation.md lands.
- Beads access model, version pin, terminal-transition writes — owned by [docs/foundation/components.md §10] (beads-integration) until beads-integration.md lands.
- Operator controls, NFRs, observability format — owned by [docs/foundation/components.md §7] (operator-nfr) until operator-nfr.md lands.

## 3. Glossary

- **mechanism** — an evaluation point whose behavior is fully specified by deterministic rules (I/O, schema/type checks, policy enforcement by deterministic evaluator, state transitions, typed error handling). See §4.2.
- **cognition** — an evaluation point that requires semantic judgment and delegates to an LLM under a named prompt and input shape (ranking, scoring, plan composition, semantic analysis, quality judgment). See §4.2.
- **mechanism/cognition tag** — one of `mechanism` or `cognition`, carried on every normative requirement per the template §4.N+1. See §4.2.
- **four-axis classification** — the four-tuple `(llm-freedom, io-determinism, replay-safety, idempotency)` applied to every type, interface, and evaluation point that crosses a subsystem boundary. See §4.1.
- **search** — the backtracking-and-candidate-generation mechanism; represented by transition kinds, candidate generation as a node type, and freedom profiles per state. See §4.3.
- **verifier** — a role-function of a workflow node whose purpose is to evaluate prior work; NOT a distinct node-type enum member and NOT a subsystem. See §4.3, §4.7.
- **traces** — durable transition records carrying the full AlphaGo field set (prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence). Distinct from events. See §4.3.
- **subsystem** — a unit declaring an envelope per §4.4 and realized at MVH as a Go package inside the daemon binary per §4.5.
- **subsystem envelope** — the declaration surface a subsystem publishes (events produced, events consumed, types exported via event payloads or shared state, handlers implemented, state owned, control points provided, NFRs inherited/overridden, boundary classification per operation). See §4.4.
- **agent type** — a handler-contract conformance class (`claude-code`, `pi`, `claude-twin`, `pi-twin`, future handlers) identified by a lowercase-hyphenated ASCII string. Orthogonal to role. See §4.7.
- **role** — a function assignment on a workflow node (Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor). Orthogonal to agent type. See §4.8.
- **verification-node** — any workflow node configured with a verification role-function: an `agentic` node with `actor_role=Reviewer`, or a `non-agentic` node running a deterministic checker. NOT a distinct member of the node-type enum. See §4.7.
- **verification-result** — the outcome record produced by a verification-node. See §4.7.
- **quality-gate** — a gate whose evaluator reads a prior verification-result. See §4.7.
- **spec** — a normative document landed under `specs/` at the repo root; describes what and why. See §4.10.
- **workflow graph** — a DOT document describing how execution happens: nodes, edges, policies by reference. See §4.10.
- **bead** — an atomic queued work item in the Beads SQLite store; the claimable unit the daemon dispatches a run against. See §4.10.

## 4. Normative requirements

### 4.0 Spec categories and envelope locus

#### AR-052 — Spec category distinguishes runtime-subsystem from foundation-cross-cutting

Every spec under `specs/` MUST declare, in its front matter, a `spec-category` of either `runtime-subsystem` or `foundation-cross-cutting`. A `runtime-subsystem` spec is realized at MVH as a Go package inside the daemon binary per §4.5.AR-016 and MUST declare an envelope per AR-053. A `foundation-cross-cutting` spec owns invariants or cross-cutting obligations imposed on runtime subsystems (e.g., operator-nfr, beads-integration, reconciliation, testing, architecture itself) and is exempt from envelope declaration. The category assignment is reviewer-enforced; the front-matter presence is lint-enforced.

Tags: mechanism

#### AR-053 — Envelope declaration section slot

Every `runtime-subsystem` spec MUST carry its Subsystem envelope as the FIRST subsection under §4, titled `§4.a Subsystem envelope`. The `a` suffix is a letter suffix (not a new top-level subsection number) chosen to avoid colliding with front-matter §0 conventions and with existing numeric subsection IDs `§4.1 … §4.N` that hold stable requirement-ID ranges across the subsystem corpus. A spec-local requirement prefix for the envelope block MUST use the reserved `<PREFIX>-ENV-NNN` range (e.g., `EM-ENV-001`, `HC-ENV-001`) so that envelope-section requirement IDs do not consume topical `<PREFIX>-NNN` ID space. The subsection MUST declare the eight envelope elements of §4.4.AR-013: (a) events produced, (b) events consumed, (c) types introduced in cross-subsystem event payloads or shared state (each with four-axis + mechanism/cognition tags per §4.1 and §4.2), (d) handlers implemented, (e) state owned, (f) control points provided, (g) NFRs inherited/overridden, (h) boundary classification per operation. An element with no content MUST be declared as "none" rather than omitted. A reference template of the envelope block is provided in §A.1.

Tags: mechanism

### 4.1 Four-axis classification

#### AR-001 — Four-axis classification applies to every cross-subsystem surface

Every type, interface, and evaluation point that appears in a cross-subsystem contract (event payload, shared state, handler interface, policy reference, control-point primitive) MUST be classifiable on four axes: `llm-freedom ∈ {none, bounded, unbounded}`, `io-determinism ∈ {deterministic, best-effort, nondeterministic}`, `replay-safety ∈ {safe, unsafe, n/a}`, `idempotency ∈ {idempotent, non-idempotent, recoverable-non-idempotent, n/a}`. Foundation and subsystem specs MUST carry an `Axes:` line on any requirement that deviates from the baseline or involves LLM invocation, external I/O, state mutation, or non-idempotent effects, per the template §4.N+1 default-baseline rule.

Tags: mechanism

#### AR-002 — Baseline axis values are the default

The baseline axis tuple is `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`. A requirement that matches baseline on every axis MAY omit the `Axes:` line; reviewers infer baseline from absence. A requirement that deviates on any axis MUST declare the full four-axis tuple.

Tags: mechanism

#### AR-003 — Profile for skeleton vs. organ operations

Skeleton operations (daemon routing, dispatch, checkpoint emission, edge selection, validator execution) MUST profile as `llm-freedom=none` and `io-determinism ∈ {deterministic, best-effort}`. Organ operations (agent cognition within a handler subprocess) MUST profile as `llm-freedom ∈ {bounded, unbounded}` and are not required to be deterministic. An operation profiling as `llm-freedom ∈ {bounded, unbounded}` inside the daemon process is a design error; such logic MUST be relocated to an agent handler per §4.9. Detection is reviewer-enforced per §10.2 (the lint only checks tag grammar, not tag semantics against the daemon/handler boundary).

Tags: mechanism

#### AR-004 — Classification applies to every subsystem-exported type

Every type a subsystem exports across its envelope (§4.4) — including types transitively referenced by any event payload a different subsystem consumes — MUST carry the four-axis tuple and the mechanism/cognition tag per §4.2. A subsystem MUST NOT mark a type "internal" to avoid tagging if that type is transitively referenced by a cross-subsystem event payload.

Tags: mechanism

> INFORMATIVE (polymorphic / sum-type tagging): A type with a mode field that selects the mechanism/cognition classification per instance (e.g., `Evaluator` whose `mode` field selects `mechanism` vs `cognition`) MUST tag each variant separately — either as two sibling named types (`MechanismEvaluator`, `CognitionEvaluator`), or as a single type carrying a per-instance `Tags:` value populated at construction (the type declaration in the envelope table then declares `Tags: mechanism|cognition` with a note that per-instance resolution is required). The type container itself MUST NOT be tagged unilaterally as mechanism when the runtime classification depends on the instance mode. The describing-cognition vs doing-cognition distinction is admissible: a `mechanism`-tagged record (e.g., `CognitionMeta`) that *describes* a cognition invocation is not itself cognition; only the evaluation point that performs the LLM call is cognition-tagged.

### 4.2 ZFC mechanism/cognition classification

#### AR-005 — Every evaluation point is tagged mechanism or cognition

Every normative requirement in every foundation and subsystem spec MUST carry a `Tags:` line with exactly one of `mechanism` or `cognition`. The two tags are mutually exclusive per template §4.N+1. A requirement describing both surfaces MUST split into two requirements.

Tags: mechanism

#### AR-006 — Mechanism-tagged surface definition

A `mechanism`-tagged evaluation point MUST NOT invoke an LLM. It is permitted to perform: I/O operations, schema and type checks, policy enforcement via deterministic evaluators, state transitions defined by typed rules, and typed error handling. A mechanism-tagged point whose behavior depends on semantic judgment (keyword matching for completion, heuristic fallback trees, regex parsing of unstructured output, hardcoded quality scoring) is a ZFC violation and MUST be refactored into a deterministic evaluator or a cognition-tagged delegation.

Tags: mechanism

#### AR-007 — Cognition-tagged delegation path obligation

A `cognition`-tagged evaluation point MUST delegate to an LLM under a named delegation path: the role performing the evaluation (per §4.8), the model class or handler, and the input shape the agent receives. The delegation path MUST appear in the requirement body or cite a schema where it is declared. Gestures at a path (e.g., "a reviewer evaluates this") without naming the role, model class, and input shape are insufficient. This is a spec-authoring obligation: the spec author writes the delegation path; the reviewer (per §10.2) verifies the path is complete. Detection is reviewer-enforced, not lint-enforced.

Tags: mechanism

> INFORMATIVE: AR-007 describes what a SPEC AUTHOR must write for cognition-tagged points. The runtime cognition happens inside the agent subprocess; the spec-authoring obligation is a mechanism-tagged meta-rule about spec shape.

> EXAMPLE (anti-pattern list, drawn from the retired AR-008): daemon-process anti-patterns forbidden as semantic-judgment violations — keyword-matching for completion status, heuristic fallback trees over unstructured output, regex parsing of free-text to derive state, hardcoded quality scoring formulas. Each is a ZFC violation under AR-006; the process-boundary case is covered by AR-INV-001.

### 4.3 The required triple — search, verifier, traces

#### AR-009 — All three mechanisms MUST exist in every deployment

Every harmonik deployment MUST include a representation of search (§4.3.AR-010), a representation of verification (§4.3.AR-011), and a representation of traces (§4.3.AR-012). Removing any one of the three from a deployment is not a valid configuration. A subsystem spec that obscures or elides any of the three fails review.

Tags: mechanism

#### AR-010 — Search mechanism at the foundation level

Backtracking MUST be representable in the execution model via four transition kinds: `local-patchback`, `architectural-rollback`, `policy-rollback`, `context-restore` (see [docs/foundation/components.md §2] transition-kind enum). Candidate generation MUST be expressible as a first-class node type (see [docs/foundation/components.md §2] node type `agentic` with policy-driven freedom profile). Freedom profiles per state — a bounded-exploration mechanism — MUST be declarable per state via YAML policy reference (see [docs/foundation/components.md §6] freedom-profile). These three surfaces MUST be declared by execution-model and control-points respectively; this spec pins them as load-bearing.

Tags: mechanism

#### AR-011 — Verifier is a role-function on a workflow node, not a subsystem

Verification MUST be realized as a role-function of a workflow node (assigned to an `agentic` node with `actor_role=Reviewer` or a `non-agentic` node running a deterministic checker), NOT as a distinct member of the node-type enum and NOT as a subsystem. No subsystem in harmonik MAY be named "verifier" at the subsystem level. A verification-capable node's inputs, outputs, and emission obligations are: inputs cite what is being verified (a prior state, a work product reference, evidence paths); outputs conform to the verification-result shape (see §4.7.AR-030); completion MUST emit an event naming the verification outcome (event type declared in [docs/foundation/components.md §3]).

Tags: mechanism

#### AR-012 — Traces are durable AlphaGo decision records distinct from events

Every durable state transition MUST produce a trace record containing prior state, actor role, candidate actions considered, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, and confidence. The trace record is durable (the execution-model spec names the storage path); it is distinct from the event stream. Events MAY project traces for streaming consumers, but events MUST NOT replace traces as the audit-fidelity source of truth.

Tags: mechanism

### 4.4 Subsystem envelope

#### AR-013 — Subsystem envelope declaration

Every `runtime-subsystem` spec (per §4.0.AR-052) MUST declare its envelope in the §4.a slot reserved by §4.0.AR-053. The envelope consists of: (a) events produced (type names, schemas cited from event-model), (b) events consumed, (c) types introduced that appear in cross-subsystem event payloads or shared state — each carrying the four-axis and mechanism/cognition tags per §4.1 and §4.2, (d) handlers implemented (if any, cited from handler-contract), (e) state owned (types cited from execution-model), (f) control points provided (cited from control-points), (g) NFRs inherited and/or overridden (cited from operator-nfr), (h) boundary classification for each operation exposed (the four-axis + mechanism/cognition tags). `foundation-cross-cutting` specs are exempt per §4.0.AR-052. A reference template of the envelope block is provided in §A.1.

Tags: mechanism

#### AR-014 — Subsystem obligations

A subsystem MUST NOT: invent shared vocabulary not grounded in an existing foundation term; violate the mechanism/cognition boundary by performing cognition in framework code; skip NFR compliance by omitting the NFR declaration in its envelope; export a type across an event payload while marking it internal to escape §4.1.AR-004.

Tags: mechanism

#### AR-015 — Add-a-subsystem procedure

Adding a new subsystem MUST be accomplished by writing a new subsystem spec that references only foundation specs and existing sibling subsystem specs. Foundation revision MUST NOT be required to add a subsystem. If a new subsystem cannot be written without foundation revision, the gap MUST be captured as a foundation amendment per §4.6 before the subsystem spec is drafted.

Tags: mechanism

### 4.5 Subsystem runtime realization — MVH pin

#### AR-016 — Subsystem is a Go package inside the daemon for MVH

For MVH, a subsystem MUST be realized as a Go package inside the daemon binary (see [docs/foundation/components.md §8] for the daemon's single-binary shape). The envelope's events-produced and events-consumed discipline is therefore inter-package discipline within a single process; the event bus is in-process. The consumer taxonomy's in-process-synchronous, in-process-asynchronous, and fan-out-observer classes all live inside the same daemon binary.

Tags: mechanism

#### AR-017 — Enumerated out-of-process actors

The only out-of-process actors admitted in MVH are: (a) agent handler subprocesses (Claude Code, Pi, twin binaries; see [docs/foundation/components.md §4]), (b) orchestrator-agent sessions (separate Claude Code sessions driving the daemon via CLI; see [docs/foundation/components.md §8]), (c) `br` CLI invocations the daemon spawns for Beads reads and writes (see [docs/foundation/components.md §10]). None of (a), (b), (c) is a subsystem; handlers implement the handler contract but declare no envelope; orchestrator-agents are external callers of the command surface; `br` is an external dependency invoked as a subprocess. Introducing a fourth out-of-process actor class MUST proceed via the amendment protocol of §4.6; ad-hoc addition is forbidden.

Tags: mechanism

#### AR-018 — Reconciliation is not a subsystem

Reconciliation MUST NOT be a subsystem. Reconciliation is a workflow-library entry — a named set of DOT workflows plus YAML policies packaged with harmonik — that runs on the same primitives as any other workflow. It declares no envelope and introduces no shared types. See [docs/foundation/components.md §9].

Tags: mechanism

#### AR-019 — Post-MVH process geometry is out of foundation scope

If harmonik later evolves to a multi-process shape (e.g., splitting the event bus into its own process), the subsystem envelope's semantics MUST remain unchanged: each Go package that declares an envelope is a subsystem regardless of which process binary hosts it. Post-MVH process geometry is a subsystem-spec concern, not a foundation concern; a change in process geometry MUST NOT require a foundation revision.

Tags: mechanism

### 4.6 Foundation amendment protocol

#### AR-020 — Amendment-proposal procedure

When a downstream spec (foundation sibling or subsystem) discovers a gap in a foundation spec, the downstream agent MUST write an amendment proposal of at least one paragraph describing the gap and the proposed change. The amendment MUST be reviewed by at least two foundation-review personas (architect and critic at minimum). If accepted, the foundation spec is revised; the revision MUST trigger re-review of every subsystem spec that cited the affected foundation spec. If rejected, the downstream spec MUST adapt within existing foundation constraints.

**Reviewer persona definitions (minimum two required).**

- **Architect persona.** Evaluates structural soundness: whether the proposed change preserves or violates cross-cutting invariants (four-axis classification, ZFC test, mechanism/cognition boundary, subsystem envelope, centralized-controller principle, three-artifact separation). Determines material-change status per AR-021. Produces a verdict of `accept | reject | revise` with a written rationale that names every cross-cutting invariant affected. The architect persona is the authority for material-change determination when those two roles coincide on a single reviewer invocation.

- **Critic persona.** Evaluates fitness-for-purpose: whether the proposed change solves the stated gap without over-reaching, whether the gap could instead be resolved within existing foundation constraints (making an amendment unnecessary), and whether the proposal introduces hidden downstream obligations (subsystem specs that must change, new cross-subsystem contracts, new out-of-process actors). Produces a verdict of `accept | reject | revise` with a written rationale that names every downstream spec anticipated to require re-review if the amendment is accepted.

Both personas are cognition-tagged reviewer steps; their invocation MUST follow the delegation path declared in AR-021. A single reviewer subagent MAY satisfy both personas in a single invocation if the invocation prompt addresses both evaluation lenses and the verdict document is structured to distinguish the structural and fitness-for-purpose findings.

Tags: mechanism

#### AR-021 — Amendment authority

Foundation amendments are product-level decisions. The orchestrator agent proposes, reviewer personas critique, the user approves material changes. Material-change determination is a cognition-tagged reviewer step with the following delegation path: **role** = `Reviewer` per §4.8 (specifically the architect persona per build-practices.md §Agent review); **model class** = agentic (LLM-backed reviewer subagent invoked through the handler-contract); **input shape** = the amendment-proposal document plus a diff of the proposed foundation-spec change against the prior foundation baseline (the full prior spec text is available on demand). The reviewer persona evaluates whether the proposed change alters cross-cutting invariants, renames canonical terms, or widens/narrows a scope declared in §2.1 or §2.2 of any foundation spec; the output is a structured verdict (`material | non-material`) with a written rationale that the user reads before approving.

Tags: cognition
Axes: llm-freedom=bounded; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### AR-022 — Foundation versioning and conformance citation

Foundation MUST be versioned. Each subsystem spec MUST cite the foundation version it conforms to in its front matter or in §9.1 Depends-on. A foundation-version bump MUST appear in the foundation spec's revision history row that introduced the change.

Tags: mechanism

#### AR-023 — Parallel-amendment serialization and overlap detection

When two or more amendment proposals are open simultaneously: (i) every proposal MUST declare, in a front-matter `touches:` list, the foundation-spec sections and requirement IDs it modifies (shape: `[architecture.md §4.1]`, `[architecture.md AR-016]`); (ii) overlapping proposals MUST be serialized — later proposals MUST be rebased onto earlier merged proposals, OR rejected-with-reason if the earlier amendment removed the premise; (iii) non-overlapping proposals MAY merge independently in any order. Overlap is defined as non-empty lexical intersection of `touches:` lists. Whether overlap detection runs as a mechanical script, a kerf check, or a reviewer-persona scan is deferred to OQ-AR-003; until that tool lands, overlap detection is reviewer-enforced.

Tags: mechanism

### 4.7 Agent-type abstraction and verification naming

#### AR-024 — Agent-type is a handler-contract conformance class

Foundation defines `agent type` as a handler-contract conformance class. Claude Code, Pi, `claude-twin`, `pi-twin`, and any future handler MUST be instances of the same abstraction. The abstraction consists of: (a) the Go `Handler` interface declared in [docs/foundation/components.md §4], (b) the wire protocol declared in [docs/foundation/components.md §4], (c) the event emission contract declared in [docs/foundation/components.md §3], (d) the secrets-delivery contract declared in [docs/foundation/components.md §4].

Tags: mechanism

#### AR-025 — Agent-type identifier shape

An `agent_type` identifier MUST be a lowercase-hyphenated ASCII string matching the regex `^[a-z][a-z0-9-]{1,62}$`. Reserved identifiers for MVH: `claude-code`, `pi`, `claude-twin`, `pi-twin`. A new agent type is registered by (a) the subsystem spec introducing it declaring the identifier in its envelope, and (b) the handler-contract conformance class being claimed. Identifiers are daemon-scoped (process-scoped); no namespace prefix is required for MVH. A post-MVH reverse-DNS prefix discipline MAY be introduced via foundation amendment per §4.6.

Tags: mechanism

#### AR-026 — Agent-type is orthogonal to role

Agent type MUST NOT be conflated with role (§4.8). Role is a function assignment (Planner, Builder, Reviewer, etc.); agent type is a process assignment (Claude Code process, Pi process, twin process). The same agent type MAY fill different roles across runs; the same role MAY be filled by different agent types across runs.

Tags: mechanism

#### AR-027 — Cross-subsystem agent-type reference points

The following four surfaces MUST reference `agent_type` with the identifier shape of §4.7.AR-025, byte-for-byte identical: (i) YAML policies (freedom-profile and role-assignment fields in [docs/foundation/components.md §6]); (ii) DOT node attributes (as a routing hint, per [docs/foundation/components.md §2]); (iii) `LaunchSpec.agent_type` (per [docs/foundation/components.md §4]); (iv) event payloads naming an agent (e.g., `agent_started`, per [docs/foundation/components.md §3]). The canonical field name across all four surfaces is `agent_type`. Mismatch across these four surfaces is a spec-draft-time error detectable by corpus lint (see §10.2 AR-024..AR-031 group).

Tags: mechanism

> INFORMATIVE: A coordinated corpus rename of the legacy identifier `handler_type` to `agent_type` is tracked in §12 revision history under foundation-amendment AR-MIG-001 (see revision row for v0.3.0). The migration sites as of architecture v0.3.0 are event-model.md §8.3.2 and §8.3.8, handler-contract.md HC-008, and workspace-model.md §5.3a / `harmonik.meta.json` sidecar. AR-MIG-001 is a foundation-amendment SHOULD carried in the revision-history log; it is not a normative clause of AR-027 itself.

#### AR-028 — Adding a new agent type does not require foundation revision

Adding a new agent type MUST be an exercise of the subsystem envelope procedure (§4.4, §4.5). The new type's handler package writes a subsystem spec that cites handler-contract and declares its envelope; no foundation revision is required.

Tags: mechanism

#### AR-029 — Verification is a role-function, not a distinct node-type enum member

Verification MUST be a role-function of a workflow node, NOT a distinct member of the node-type enum declared in [docs/foundation/components.md §2]. The canonical enum is `{agentic, non-agentic, gate, control-point, sub-workflow}`; no `verification-node` value is admitted. An `agentic` node with `actor_role=Reviewer` is verification-capable (cognition-tagged delegation to a reviewer-agent); a `non-agentic` node running a deterministic checker (test runner, linter, type-check, policy-check) is verification-capable (mechanism-tagged). The term `verification-node` in this spec and elsewhere refers to any node so configured, not to an enum value. No "verifier" subsystem exists per §4.5.AR-018 and §4.3.AR-011.

Tags: mechanism

#### AR-030 — Verification-result shape

A `verification-result` MUST be the outcome of running a `verification-node` (per AR-029) and MUST conform to the outcome shape declared in [docs/foundation/components.md §2] with `status ∈ {SUCCESS, FAIL, PARTIAL_SUCCESS}`. The `RETRY` outcome status admitted by the broader outcome shape is NOT admissible for a verification-result: a verification node that would return `RETRY` MUST instead fail-hard (classification is a category error). Mechanism-tagged verification nodes MUST populate a structured `evidence` field (output from the underlying tool); cognition-tagged verification nodes MUST populate a `notes` field (reviewer's written critique). The hyphenated form `verification-result` is canonical across all specs.

Tags: mechanism

#### AR-031 — Quality-gate is a gate that reads a verification-result

A `quality-gate` MUST be a gate (per [docs/foundation/components.md §6]) whose evaluator references a prior `verification-result`. Quality-gates are gates that happen to read verification outcomes; they are NOT themselves verification. The three hyphenated forms `verification-node`, `verification-result`, `quality-gate` are distinct and MUST NOT be used interchangeably in any spec.

Tags: mechanism

### 4.8 Role taxonomy

#### AR-032 — Seven roles named

Foundation names seven roles drawn from the AlphaGo north-star: Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor. These role names MUST be the canonical vocabulary across all specs. A subsystem spec that invents an alternative role name for a function already covered by one of the seven fails review.

Tags: mechanism

#### AR-033 — MVH-required vs declared-but-deferred split

Planner, Builder, and Reviewer MUST be implemented at MVH (sufficient to run the minimum self-build cycle described in the bootstrap doc). Researcher, Verifier, Scheduler, and Governor are declared-but-deferred: named in foundation so subsystem specs do not invent alternatives, but not required at MVH. Each deferred role MUST be activated when its triggering pattern appears in a workflow; activation MUST NOT require a foundation revision.

Tags: mechanism

#### AR-034 — Per-role definition scope

For each role, this spec MUST define: purpose, typical actions, what it does not do. Concrete permission schemas per role are OWNED by [docs/foundation/components.md §6] control-points, not this spec. This spec describes WHAT the role IS; control-points describes WHAT A ROLE IS ALLOWED TO DO. Neither spec duplicates the other's content; control-points cites this section by §4.8 for names and semantics, this section cites control-points for permissions.

Tags: mechanism

#### AR-035 — Role is orthogonal to agent type (cross-reference)

Role is orthogonal to agent type. See §4.7.AR-026 for the normative rule; this subsection cross-references it for role-taxonomy locality.

Tags: mechanism

#### AR-036 — Merge responsibility is not a role

Merge responsibility MUST NOT be a top-level role. A workflow MAY contain a distinct merge node assigned to whatever agent type (Builder, Reviewer, or a dedicated merger) the policy names. The merge node MUST operate in the run's leased worktree (per [docs/foundation/components.md §5]), not in a new worktree. Merge is a node function, not a role.

Tags: mechanism

### 4.9 Centralized-controller principle and harness-engineering invariants

#### AR-037 — Retired (promoted to AR-INV-007)

Retired at v0.3.0. The centralized-controller principle was promoted to an invariant per the §5 template selection test (cross-corpus quantifier over every subsystem spec). See AR-INV-007. ID never reused.

Tags: mechanism

#### AR-038 — Explicit inverse of Gas Town decentralization

The centralized-controller invariant is the explicit inverse of the Gas Town polecats/mayors decentralized-orchestration pattern. A design proposal that introduces file-based agent-to-agent handoff, or per-agent worktree ownership for the same run, fails this principle and MUST be rejected or refactored.

Tags: mechanism

#### AR-039 — Merge agent operates in the run's worktree

A merge agent (when the workflow uses a distinct merge node) MUST operate in the same worktree the implementer used; the worktree is leased by the workflow run, not by any individual agent. See [docs/foundation/components.md §5].

Tags: mechanism

#### AR-040 — Acknowledged centralized-controller tradeoff

The centralized-controller principle carries a real cost: if the daemon dies mid-run, every agent goes silent and reconciliation is required on restart (per [docs/foundation/components.md §9]). A decentralized alternative (Gas Town polecats/mayors) would offer graceful degradation; harmonik's choice foregoes that property in favor of routing simplicity. This tradeoff is acceptable within the MVH envelope (single-user, single-project, developer-machine — daemon colocated with work). A pivot to decentralized would require re-evaluating this principle under scenarios where the daemon is the only failure domain (remote-daemon, multi-operator-single-daemon); such a pivot is a foundation amendment per §4.6.

Tags: mechanism

#### AR-041 — Repository as single source of truth

Every normative artifact — specs, policies, workflow DOT files, skill registries, conventions — MUST live in the repository. External wikis, out-of-band knowledge bases, and tribal-knowledge channels MUST NOT be load-bearing for any spec's conformance.

Tags: mechanism

#### AR-042 — Invariants MUST name their sensor

Every invariant (any `<prefix>-INV-NNN` block in any foundation or subsystem spec) MUST name, inline within the invariant block or in the §10.2 test-surface obligations cross-reference, the sensor that enforces it. The sensor MAY be a lint rule, a verification-node role-function (per AR-029), a review-agent scenario, or a conformance test cited by ID. A sensor that only fires at spec-draft-time (reviewer reads the spec) counts as a sensor for this requirement; a runtime-only sensor is not required. An invariant whose named sensor is "TBD" or "reviewer judgment" fails AR-042.

Tags: mechanism

#### AR-043 — Constrain to empower

Strict architectural boundaries (the four-axis tags, the mechanism/cognition split, the subsystem envelope, the three-artifact separation) are productivity multipliers and MUST be enforced. A spec that asks for an exemption "to move faster" MUST be rejected; the remedy is a foundation amendment per §4.6, not a local escape hatch.

Tags: mechanism

#### AR-044 — Filesystem-backed coordination

Agent coordination artifacts (session logs, transition-record sibling files, skill manifests, policy YAML) MUST be filesystem-backed; conversations between agents are ephemeral and MUST NOT be load-bearing for any state the daemon consumes. The daemon reads state from the filesystem; it does not read state from agent conversation transcripts.

Tags: mechanism

#### AR-045 — Quality-left ordering

Fast deterministic checks (lint, type-check, policy-check) MUST run before expensive inferential checks (review agent, scenario run) wherever a workflow can admit both. The quality-left ordering is a default; workflow authors MAY override where a node's inputs require the opposite order, but the override MUST be explicit in the workflow DOT and MUST cite a policy reason.

Tags: mechanism

### 4.10 Three-artifact separation

#### AR-046 — Retired (promoted to AR-INV-008)

Retired at v0.3.0. The three-artifact separation was promoted to an invariant per the §5 template selection test (cross-corpus quantifier forbidding any spec from introducing a fourth compositional artifact). See AR-INV-008. ID never reused.

Tags: mechanism

#### AR-047 — Spec artifact definition

A `spec` is a kerf output landing in `specs/` at the repo root. It is the design-and-thinking artifact: what, why, acceptance criteria. Specs are normative for system behavior. See the template in `docs/foundation/spec-template.md`.

Tags: mechanism

#### AR-048 — Workflow-graph artifact definition

A `workflow graph` is a DOT document (per [docs/foundation/components.md §2]). It is the normative description of HOW execution happens: nodes, edges, policies by reference. Workflow graphs are authored in the workflow library, reviewed as code, and versioned alongside the spec corpus.

Tags: mechanism

#### AR-049 — Bead artifact definition

A `bead` is an atomic queued work item in the Beads SQLite store (per [docs/foundation/components.md §10]). It is the claimable unit the daemon dispatches a run against.

Tags: mechanism

#### AR-050 — Many-to-many relationships without projection

Relationships among the three artifacts MUST be many-to-many without projection: a spec MAY describe zero, one, or many workflows and beads; a workflow MAY be invoked by many beads; a bead references the workflow and node it is executing, and carries a bead ID that appears in the run's checkpoint-commit trailers and event payloads (per [docs/foundation/components.md §10]).

Tags: mechanism

#### AR-051 — "Feature" is not a product primitive

"Feature" MUST NOT appear as a product primitive in any spec. Work size varies by how many nodes or sub-graphs an agent composes, not by a distinct `feature` entity. Specs, workflows, and beads are the normative vocabulary; "feature" is at most a casual-speech term and MUST NOT carry normative weight.

Tags: mechanism

## 5. Invariants

> INFORMATIVE: Per template §5 selection test, an invariant is retained when it constrains multiple subsystems' requirements with a cross-corpus quantifier and does not duplicate a §4 requirement. v0.2 retired AR-INV-002 (duplicate of AR-001/AR-005), AR-INV-004 (duplicate of AR-013), AR-INV-005 (covered by AR-037 at the time), and AR-INV-006 (covered by AR-046 at the time). v0.3 promoted AR-037 → AR-INV-007 and AR-046 → AR-INV-008 (both cross-corpus in nature; the §4 requirement form underweighted the universal-over-corpus quantifier that cross-cutting lint requires). AR-037 and AR-046 are retired at v0.3. Retired IDs are never reused. See §12 revision history.

#### AR-INV-001 — Mechanism/cognition split is strict at the process boundary

The daemon process MUST carry only mechanism-tagged logic. All cognition-tagged evaluation MUST occur in agent handler subprocesses. A cognition-tagged requirement whose delegation path (per §4.2.AR-007) resolves inside the daemon binary (rather than to an agent subprocess) is a violation of this invariant. Sensor: **corpus-search heuristic** — reviewer persona (conformance-auditor per build-practices.md §Agent review) scans every `cognition`-tagged requirement in the corpus and checks the declared delegation path targets an agent handler subprocess per §4.5.AR-017(a), not an in-daemon code path. The heuristic is reviewer-enforced (mechanism-tagged lint grammar cannot resolve "in-daemon" vs "in-subprocess" without a `Process:` tag the template does not currently require). Known false-negatives are tracked in OQ-AR-005; a post-MVH `Process:` tag to make the sensor mechanical is tracked there as a candidate amendment.

Tags: mechanism

#### AR-INV-003 — Search + verifier + traces are required, not optional

Any harmonik deployment claiming foundation conformance MUST exhibit search (§4.3.AR-010), verification (§4.3.AR-011), and traces (§4.3.AR-012). A deployment that elides any of the three is non-conforming. Sensor: corpus presence test per §10.2 AR-009..AR-012 group.

Tags: mechanism

#### AR-INV-007 — Centralized-controller invariant

Harmonik MUST be a centralized-controller system. The deterministic daemon (Go binary, no LLM logic — see [docs/foundation/components.md §8] for the daemon-vs-orchestrator-agent distinction) MUST own all workflow state, routing, and dispatch. Agents MUST perform only cognitive work. Agent-to-agent coordination MUST route through the daemon. Agent-to-agent coordination via files, or via ad hoc IPC between agent processes, is forbidden. **All cross-subsystem registries (policy, control-point, handler, skill) are daemon-owned and in-process for MVH**; a subsystem spec MAY NOT locate its cross-cutting registry outside the daemon without invoking the amendment protocol of §4.6. Sensor: reviewer-agent scenario per §10.2 AR-038..AR-045 group — proposals introducing file-based agent handoff, per-agent worktree ownership for the same run, or an out-of-daemon cross-subsystem registry are rejected.

Tags: mechanism

#### AR-INV-008 — Three-artifact separation

Harmonik uses exactly three artifacts for work composition: `spec`, `workflow graph`, and `bead`. No artifact MAY be treated as a projection of another. No spec MAY introduce a fourth compositional artifact (shapes such as `feature`, `story`, `epic`, `capability-plan`, `initiative`). "Compositional" means: a durable, authored artifact that carries work-shape. Configuration artifacts (policy YAML, skill manifests, `.harmonik/*` sidecars) are not compositional and are exempt. Sensor: corpus-lint per §10.2 AR-047..AR-051 group (`feature` non-normative; no fourth compositional term introduced).

Tags: mechanism

## 6. Schemas and data shapes

This spec declares meta-rules, not data types. The schemas that downstream specs produce under these meta-rules live in their owning specs:

- `Handler` interface — [docs/foundation/components.md §4] until handler-contract.md lands.
- Event envelope and event payloads — [docs/foundation/components.md §3] until event-model.md lands.
- `Workflow`, `Node`, `Edge`, `Run`, `State`, `Transition`, `Checkpoint`, `Outcome` — [docs/foundation/components.md §2] until execution-model.md lands.
- Policy, freedom-profile, gate, skill-declaration surfaces — [docs/foundation/components.md §6] until control-points.md lands.

### 6.1 Agent-type identifier regex

The only data shape this spec owns is the agent-type identifier regex declared in §4.7.AR-025:

```
agent_type := ^[a-z][a-z0-9-]{1,62}$
```

Reserved identifiers for MVH: `claude-code`, `pi`, `claude-twin`, `pi-twin`.

### 6.2 Schema evolution

This spec has no runtime schema to version. Revisions to meta-rules are governed by the amendment protocol (§4.6) and recorded in §12.

## 9. Cross-references

### 9.1 Depends on

None. This spec is the root of the foundation corpus.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Every other foundation spec and every subsystem spec is expected to depend on this spec; the reverse index is populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[docs/foundation/components.md §2]** — this spec reads transition-kind and node-type enumerations declared there; does not depend on execution-model's internals.
- **[docs/foundation/components.md §3]** — this spec names event emission obligations for verification-node completion; event shape is declared there.
- **[docs/foundation/components.md §4]** — this spec names the handler conformance class and wire protocol; the shape is declared there.
- **[docs/foundation/components.md §6]** — this spec names control-point, policy, freedom-profile, gate, and skill-declaration surfaces; their grammar is declared there.
- **[docs/foundation/components.md §8]** — this spec names the daemon single-binary shape and the daemon-vs-orchestrator-agent distinction; process-lifecycle owns the details.
- **[docs/foundation/components.md §9]** — this spec names reconciliation-as-workflow; reconciliation owns the category taxonomy.
- **[docs/foundation/components.md §10]** — this spec names `br` CLI as an external-dependency invocation; beads-integration owns the adapter.

> INFORMATIVE: All `docs/foundation/components.md` citations are bootstrap-only per the template §Cross-reference convention. When the target spec is finalized, the citation is to be migrated to the spec reference within one revision cycle.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST satisfy every requirement AR-001 through AR-053 (IDs AR-008 retired in v0.2, AR-037 and AR-046 retired in v0.3 after promotion to AR-INV-007 and AR-INV-008; never reused) and every invariant AR-INV-001, AR-INV-003, AR-INV-007, AR-INV-008 (IDs AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 retired in v0.2; never reused). No requirement is deferred at MVH; the meta-rules are all load-bearing for spec-corpus coherence.

**Post-MVH extensions.** The reserved post-MVH extension is agent-type namespace discipline (reverse-DNS prefix per §4.7.AR-025), triggered only if cross-daemon identifier conflicts become possible. Its introduction is a foundation amendment per §4.6, not a Core MVH obligation.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each group's obligation:

> INFORMATIVE (reviewer-persona assignment): Requirements whose verification path is "reviewer-enforced" (rather than "lint" or "scenario test") are checked by reviewer subagents per the persona catalogue in `docs/components/internal/build-practices.md §Agent review on every commit`. Default persona assignments: the **architect** persona checks cross-cutting-invariant violations and AR-020/AR-021 amendment material-change determinations; the **conformance-auditor** persona checks AR-003 daemon/handler boundary, AR-007 delegation-path completeness, AR-042 invariant-sensor grounding, and AR-INV-001 corpus-search heuristics; the **critic** persona checks AR-014/AR-015 subsystem-obligation violations and AR-017 out-of-process-actor enumeration closure; the **scope-steward** persona checks AR-026, AR-033, AR-034 role/agent-type orthogonality. A single reviewer persona MAY satisfy multiple checks per invocation; cross-referencing to the persona catalogue is sufficient — this spec does not duplicate the catalogue inline.

- **AR-001 — AR-004 (four-axis classification).** Corpus-level lint: every requirement in every spec either has no `Axes:` line (baseline) or carries the full four-axis tuple with valid tokens per template §4.N+1. Every cross-subsystem type declared in any subsystem envelope carries both tag lines.
- **AR-005 — AR-007 (ZFC; AR-008 retired in v0.2).** Corpus-level lint: every requirement carries exactly one `Tags:` value (`mechanism` or `cognition`). Every `cognition`-tagged requirement names a delegation path per AR-007. No daemon-side requirement is `cognition`-tagged (per AR-INV-001).
- **AR-009 — AR-012 (required triple).** Corpus presence test: search representation (transition kinds in execution-model), verifier representation (verification-node type in execution-model), trace representation (Transition record in execution-model) all appear in the corpus.
- **AR-013 — AR-019, AR-052 — AR-053 (subsystem envelope and realization).** Corpus-level lint: every spec's front matter declares `spec-category` per AR-052; every `runtime-subsystem` spec carries `§4.a Subsystem envelope` per AR-053 with the eight elements of AR-013 (envelope requirement IDs use the reserved `<PREFIX>-ENV-NNN` range); every subsystem is realized as a Go package per AR-016; the enumerated out-of-process actors of AR-017 are honored (no other process class is introduced without amendment). `foundation-cross-cutting` specs are exempt from envelope declaration.
- **AR-020 — AR-023 (amendment protocol).** Procedure-doc test: the amendment protocol is documented in a location accessible to any downstream agent; overlap detection is a scripted check.
- **AR-024 — AR-031 (agent-type and verification).** Corpus-level lint: every occurrence of `agent_type` matches the regex of §6.1; every occurrence of `verification` is qualified as one of the three hyphenated forms.
- **AR-032 — AR-036 (role taxonomy).** Corpus-level lint: no spec introduces a role name outside the seven named here; control-points.md §6.6 cites this spec's §4.8 for names; merge responsibility does not appear as a top-level role.
- **AR-038 — AR-045 (harness-engineering; AR-037 retired at v0.3 → AR-INV-007).** Review-agent scenario tests: proposals introducing file-based agent handoff or per-agent worktree ownership are rejected (enforced by AR-INV-007); every normative artifact is in the repo (AR-041); guides+sensors pairing is verified per-invariant (AR-042).
- **AR-047 — AR-051 (three-artifact separation; AR-046 retired at v0.3 → AR-INV-008).** Corpus-level lint: no spec treats `feature` as a normative term; no fourth compositional artifact is introduced (enforced by AR-INV-008).

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle after testing.md is finalized; this migration obligation is tracked in OQ-AR-001.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over the internal behavior of any specific subsystem; each subsystem's conformance is owned by its own spec.
- This spec does NOT specify the operator CLI surface, the daemon socket location, or the concrete event payload schemas; those are owned by operator-nfr, process-lifecycle, and event-model respectively.
- This spec does NOT specify how a review agent is implemented; the delegation path obligation (AR-007) names the role and input shape, but the reviewer agent's prompt engineering is owned by the workflow library.

## 11. Open questions

#### OQ-AR-001 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-AR-002 — Agent-type namespace discipline post-MVH

Question: Should agent-type identifiers adopt a reverse-DNS prefix discipline (e.g., `com.anthropic.claude-code`) once cross-daemon identifier conflicts become possible, or is a flat namespace sufficient indefinitely?
Owner: foundation-author
Blocks: none (MVH decision: flat namespace per §4.7.AR-025)
Default-if-unresolved: Flat namespace. Revisit via amendment only if cross-daemon conflicts are observed or multi-tenant deployments appear.

#### OQ-AR-003 — Amendment overlap detector tooling locus

Question: Where does the amendment overlap detector (§4.6.AR-023) live — in the kerf tooling, in a spec-corpus lint script, or as an orchestrator-agent skill?
Owner: foundation-author
Blocks: none (overlap detection is mechanism-tagged and can be implemented in any of the three locations)
Default-if-unresolved: Spec-corpus lint script under `tools/`. Revisit if kerf gains a corpus-wide validator.

#### OQ-AR-004 — Post-MVH decentralization-pivot trigger conditions

Question: The centralized-controller tradeoff (§4.9.AR-040) names remote-daemon and multi-operator-single-daemon as trigger scenarios for re-evaluating the principle. Are there additional triggers (e.g., daemon-as-SaaS, multi-machine execution under a single run)?
Owner: foundation-author
Blocks: none
Default-if-unresolved: The two scenarios named are sufficient for MVH. Additional triggers are captured as amendments when concrete cases arise.

#### OQ-AR-005 — AR-INV-001 sensor false-negative rate and candidate `Process:` tag

Question: AR-INV-001's sensor is a reviewer-enforced corpus-search heuristic because the template carries no machine-readable daemon-vs-subprocess classification per requirement. A post-MVH amendment could introduce a `Process:` tag per requirement (`daemon`, `agent-subprocess`, `orchestrator-agent`, `br-subprocess`, `meta`) that lets the lint mechanically flag any `cognition`-tagged requirement whose `Process:` resolves to `daemon`. What is the false-negative rate of the reviewer-enforced heuristic in practice, and does it warrant the template amendment?
Owner: foundation-author
Blocks: none (MVH sensor is the reviewer-enforced heuristic)
Default-if-unresolved: Keep the reviewer-enforced heuristic. Revisit via amendment after the first two subsystem specs reach `reviewed` and the false-negative rate is observable.

#### OQ-AR-006 — Trace-shape uniformity under deterministic dispatch

Question: AR-012 mandates the 11-field AlphaGo trace shape (prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence) on every durable state transition. For deterministic-dispatch transitions (e.g., a non-agentic lint node succeeding), five to six of the eleven fields are vacuous (no candidate actions, no parameter vector, no confidence score). Is the uniform shape the right rule, or should trace records split into `agentic-trace` and `mechanism-trace` subtypes with the mechanism shape a strict subset?
Owner: foundation-author (coordinate with execution-model owner)
Blocks: none (current AR-012 admits vacuous-field population)
Default-if-unresolved: Keep uniform shape; vacuous fields populate as `null`/`0` with a lint that flags non-null populations inconsistent with node-type.

#### OQ-AR-007 — Mechanical test for spec-category assignment

Question: AR-052 lists examples (operator-nfr, beads-integration, reconciliation, testing, architecture itself) of `foundation-cross-cutting` specs, but the assignment is reviewer-enforced rather than mechanical. Edge cases exist — e.g., beads-integration declares a subprocess (`br` CLI), owns a state store (SQLite), and produces/consumes events, which suggests `runtime-subsystem` under a concrete test; AR-052 lists it as `foundation-cross-cutting` by reviewer judgment. Should AR-052 adopt a concrete mechanical test ("a spec is `runtime-subsystem` iff it declares a Go package that registers with the in-process event bus OR owns a state type cited from execution-model") and reclassify edge cases accordingly?
Owner: foundation-author
Blocks: none (reviewer-enforced category is operational)
Default-if-unresolved: Keep reviewer-enforced. Revisit after the first three subsystem specs reach `reviewed` and inconsistencies become visible.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-24 | 0.1.0 | foundation-author | Initial draft. Encodes four-axis and ZFC classification tests, required triple, subsystem envelope and Go-package MVH pin, amendment protocol with parallel-amendment serialization, agent-type abstraction and verification naming disambiguation, role taxonomy glossary, centralized-controller principle with acknowledged tradeoff, and three-artifact separation. |
| 2026-04-24 | 0.2.0 | foundation-author | Round-1 review integration. Fixed §1 Purpose self-citations (§1.x → §4.x). Added AR-052 (spec-category: `runtime-subsystem` vs `foundation-cross-cutting`) and AR-053 (pin §4.0 envelope section slot); scoped AR-013 to runtime-subsystem specs. Fixed AR-011 and AR-029 to state verification as a role-function over `{agentic, non-agentic}` nodes (not a distinct node-type enum member), aligning with execution-model EM-006. Added `handler_type` → `agent_type` migration note to AR-027 (downstream specs event-model, handler-contract, workspace-model MUST rename in their next cycle). Retired AR-008 (duplicated AR-006 + AR-INV-001; anti-pattern list moved to AR-007 EXAMPLE). Retired AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 as duplicates of §4 requirements per template selection test; retained AR-INV-001 and AR-INV-003 as genuinely cross-subsystem. Re-tagged AR-007 as `mechanism` (it is a spec-authoring obligation, not a runtime cognition event). Tightened AR-042 to "invariants MUST name their sensor." Demoted AR-023's "mechanical overlap detector" to a declared `touches:` front-matter contract with reviewer-enforced overlap detection until tooling lands. Added explicit RETRY-exclusion to AR-030. Dropped "expected" from AR-003 title. Merged AR-035 into AR-026 as a cross-reference. Tightened AR-017 with explicit amendment-required clause. Added envelope-declaration exemplar in §A.1. NOTE: downstream specs carry wrong citation anchors (`[architecture.md §1.x]`, `§1.4a`, `§1.6a`) pointing at sections that never existed in architecture.md; the coordinated corpus-fix maps `§1.1→§4.1`, `§1.2→§4.2`, `§1.3→§4.3`, `§1.4→§4.4`, `§1.4a→§4.5` (subsystem runtime realization), `§1.5→§4.6`, `§1.6→§4.8`, `§1.6a→§4.7` or `§6.1` (agent-type identifier), `§1.8→§4.9`, `§1.9→§4.10`. Downstream specs fix these in their own integration cycles. |
| 2026-04-24 | 0.3.0 | foundation-author | Round-2 review integration; status `draft` → `reviewed`. Fixed AR-053 §4.0 collision by renaming envelope slot to §4.a (letter suffix) and reserving a `<PREFIX>-ENV-NNN` ID range so envelope blocks do not consume topical ID space in existing subsystem specs; §A.1 exemplar updated in lockstep. Added front-matter `spec-category: foundation-cross-cutting`. Promoted AR-037 → AR-INV-007 (centralized-controller invariant, including new daemon-owned cross-subsystem registry clause per S02-implementer R1) and AR-046 → AR-INV-008 (three-artifact separation with explicit fourth-artifact forbid and configuration-artifact exemption); AR-037 and AR-046 retired (IDs never reused) per skeptic S5. Reworked AR-027: removed the self-violating "downstream specs MUST rename handler_type → agent_type in next revision cycle" migration clause from the normative body; the rename is now tracked as foundation-amendment `AR-MIG-001` in this revision-history row. **AR-MIG-001 (amendment SHOULD):** coordinated rename of `handler_type` → `agent_type` across event-model.md §8.3.2 and §8.3.8, handler-contract.md HC-008, workspace-model.md §5.3a and `harmonik.meta.json` sidecar; processed under AR-020/AR-023 on each owning spec's next revision; overlap-free since sites are non-overlapping per `touches:` accounting. AR-021 cognition-tagged delegation path completed per conformance-auditor: role=Reviewer(architect persona), model-class=agentic, input-shape=amendment doc + spec-diff against prior foundation baseline, output=`material | non-material` verdict with rationale. AR-INV-001 sensor weakened to "reviewer-enforced corpus-search heuristic" per conformance-auditor; known false-negative rate tracked in new OQ-AR-005 (candidate `Process:` tag amendment). Added polymorphic/sum-type tagging INFORMATIVE note under AR-004 (S02-implementer R3). Added reviewer-persona informative block at §10.2 top (architect / conformance-auditor / critic / scope-steward defaults). Added new OQs: OQ-AR-005 (AR-INV-001 false-negative tracking + `Process:` tag), OQ-AR-006 (AR-012 trace-shape uniformity under deterministic dispatch — skeptic hidden assumption 7), OQ-AR-007 (mechanical test for spec-category assignment — skeptic S4 / beads-integration edge case). §10.1 and §10.2 updated for retirements (AR-037, AR-046) and new invariants (AR-INV-007, AR-INV-008). Not addressed in this revision and deferred: corpus-wide §1.N → §4.N anchor migration (tracked per prior v0.2 NOTE); AR-022 `foundation-version:` front-matter enforcement across downstream specs (carried forward as a coordinated amendment). |
| 2026-04-24 | 0.3.1 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; 2 citations fixed (both in §A.1 envelope exemplar: `[event-model.md §3.2]` → `[event-model.md §6.3]` for payload schema references). |

## A. Appendices

### A.1 Envelope-declaration exemplar

> INFORMATIVE: This is a copy-pasteable template for the §4.a Subsystem envelope section required of every `runtime-subsystem` spec per §4.0.AR-053. Fill each element; write "none" explicitly when a category is empty. Envelope requirement IDs use the reserved `<PREFIX>-ENV-NNN` range so they do not consume topical ID space.

```markdown
### 4.a Subsystem envelope

#### <PREFIX>-ENV-001 — Envelope declaration

(a) Events produced:
  - `<event_type>` — emission rule; payload schema in [event-model.md §6.3].
  - (or "none")

(b) Events consumed:
  - `<event_type>` — consumption rule; payload schema in [event-model.md §6.3].
  - (or "none")

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `<TypeName>` | mechanism | llm-freedom=none; ... |
  (or "none")

(d) Handlers implemented:
  - `<handler class>` — cited from [handler-contract.md §N].
  - (or "none")

(e) State owned:
  - `<StateType>` — cited from [execution-model.md §6.1].
  - (or "none")

(f) Control points provided:
  - `<control-point>` — cited from [control-points.md §N].
  - (or "none")

(g) NFRs inherited / overridden:
  - Inherited: `<nfr-id>` from [operator-nfr.md §N].
  - Overridden: `<nfr-id>` with rationale.
  - (or "none")

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `<op>` | mechanism | llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent |

Tags: mechanism
```

### A.3 Rationale

**Why meta-rules live in a separate spec.** The four-axis classification, the ZFC test, the subsystem envelope, and the three-artifact separation are all rules about how OTHER specs are shaped. Folding them into a "general rules" section of any single subsystem spec would bury them; hoisting them into their own spec gives every downstream author a single citation point and a single review surface. Every other foundation spec is expected to depend on this spec.

**Why subsystem-as-Go-package is pinned rather than abstract.** The alternative — leaving subsystem realization open — would invite subsystem authors to propose out-of-process shapes for individual subsystems (separate event-bus process, separate workspace-manager process, etc.) during MVH. That proliferation would fragment the envelope discipline across process boundaries before the envelope discipline itself is proven. Pinning the Go-package shape for MVH forces the envelope discipline to prove itself in-process first; the post-MVH slot (§4.5.AR-019) preserves the option to split process boundaries later without revising the envelope semantics.

**Why the centralized-controller tradeoff is acknowledged explicitly.** The decision to reject Gas Town's decentralized polecats/mayors pattern is load-bearing; subsystem authors downstream will look for the principle and assume it is costless. It is not costless: if the daemon dies mid-run, everything stops. Within the MVH envelope (daemon colocated with the developer's machine), "daemon dies" and "machine dies" have the same recovery path, so the tradeoff is acceptable. Outside that envelope, the tradeoff is a real cost the decentralized alternative does not pay. Naming this explicitly in §4.9.AR-040 prevents surprise and sets a clear trigger condition for re-evaluation (OQ-AR-004).

**Why "feature" is explicitly excluded as a product primitive.** The temptation to introduce "feature" as a first-class compositional artifact is strong — it matches vocabulary from other systems and reads naturally in product discussions. But "feature" has no durable representation in harmonik: it is not a spec, not a workflow, not a bead. A spec describes what; a workflow graph describes how; a bead is the claimable unit. Any aggregation larger than a bead is achieved by composing nodes or sub-graphs. Admitting "feature" as a fourth artifact would require inventing a durable representation for it, which would fragment the three-store discipline (git, Beads, JSONL) and invent a fourth store. The exclusion in §4.10.AR-051 is load-bearing: it prevents that invention.

**Why three hyphenated forms for verification.** "Verification" appears across the knowledge base meaning three distinct things (a node, an outcome, a gate reading that outcome). Without disambiguation, downstream specs would use "verifier" and "verification" interchangeably and trigger the temptation to reintroduce a "verifier subsystem" (rejected by locked decision #9). Pinning `verification-node` / `verification-result` / `quality-gate` as the canonical forms (§4.7.AR-029, AR-030, AR-031) forces the three meanings to stay separate in prose and prevents the subsystem-style drift.
