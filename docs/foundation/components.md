# Harmonik Foundation — Components

> Pass 2 output for the `harmonik-foundation` kerf work (spec jig).
>
> Created 2026-04-19. Decomposes the foundation spec set into target spec files, each with concrete requirements and inter-component dependencies.

## Component overview

Ten foundation specs. Each is a new spec file; no existing spec files are being modified — foundation is the first normative spec layer. Filenames shown are *target filenames* that will appear in `specs/` after `kerf finalize`.

| # | Target spec | One-line purpose |
|---|---|---|
| 1 | `specs/architecture.md` | Architectural principles, classification tests (ZFC + determinism axes), subsystem envelope, foundation amendment protocol, centralized-controller principle, three-artifact separation |
| 2 | `specs/execution-model.md` | Core execution data model — workflow (DOT), node, edge, run, state, transition, checkpoint, outcome — the outcome-spine contract, and the three-store cross-reference |
| 3 | `specs/event-model.md` | JSONL event schema, versioning, clock/time semantics, fsync/durability contract, observational-vs-state-reconstruction replay semantics |
| 4 | `specs/handler-contract.md` | Handler Go interface, process-boundary wire protocol, concurrency model, context propagation, error types, secrets handling, skill/tool injection obligation |
| 5 | `specs/workspace-model.md` | Worktree lifecycle (leased by the RUN, not per-agent), branching model, session-log aggregation, merge semantics, workspace/operator-control interaction |
| 6 | `specs/control-points.md` | Unified gate/hook/transition-guard primitive, policy and role taxonomy (YAML policies referenced by DOT attributes), skill-declaration surface, config precedence, budget enforcement |
| 7 | `specs/operator-nfr.md` | Observability protocol, security posture, operator-control semantics (between-task invariant, pause/stop/upgrade), graceful shutdown, queue-format compat (Beads as queue) |
| 8 | `specs/process-lifecycle.md` | Daemon scope (per-project), startup/shutdown sequence, command surface, agent-subprocess contract, daemon-vs-orchestrator-agent distinction, ntm's role |
| 9 | `specs/reconciliation.md` | Restart reconciliation: category taxonomy, detection rules, investigator-agent contract, verdict vocabulary, re-run semantics (reconciliation IS a workflow, not a subsystem) |
| 10 | `specs/beads-integration.md` | Beads as task ledger: access model (`br` CLI), write/read surfaces, bead-ID propagation, store-authority rules, version-pin + adapter |

This decomposition grew from the original seven foundation specs to ten after decisions locked in on 2026-04-20 and 2026-04-21 (DOT workflow format, no-DTW, Beads-as-ledger, handler skill-injection obligation — see problem-space §Locked decisions 11–14). The three new components (process-lifecycle, reconciliation, beads-integration) name cross-cutting content that was previously implicit or scattered across other specs; naming them explicitly prevents drift and keeps each spec single-topic. Consolidation was considered (e.g., merging process-lifecycle into operator-nfr) but rejected because process lifecycle is operationally dense enough to warrant its own normative surface.

---

## Component 1: `specs/architecture.md`

### Purpose
Normative statement of harmonik's architectural invariants, operational classification tests, and the meta-contracts for subsystems and foundation evolution.

### Requirements (what the spec must describe)

**1.1 The deterministic/probabilistic boundary — operationalized.**
- Names the four axes (LLM-freedom, I/O determinism, replay-safety, idempotency).
- Provides a decision procedure for classifying any operation on each axis.
- States the expected profile for skeleton vs. organ operations and the acceptance conditions for operations on the boundary.
- Specifies that every normative type, interface, and evaluation point in foundation *and* in every subsystem spec is tagged with its classification on each axis.

**1.2 The ZFC (Zero Framework Cognition) classification test.**
- States the rule: every evaluation point is tagged `mechanism` or `cognition`.
- Defines mechanism (allowed): I/O operations, schema/type checks, policy enforcement via deterministic rules, state transitions, typed error handling.
- Defines cognition (must delegate): ranking, scoring, plan composition, semantic analysis, quality judgment.
- Cognition-tagged points must name the delegation path (which model, under what prompt, with what input shape).
- Provides an anti-pattern checklist: keyword matching for completion, heuristic fallback trees, regex parsing of unstructured output, hardcoded quality scoring.

**1.3 Search + verifier + traces — the required triple.**
- States that all three mechanisms must exist in any harmonik deployment; removing one is not permitted.
- Defines **search** at the foundation level: backtracking must be representable (four rollback types: local-patchback, architectural, policy, context), candidate generation is a first-class node type, controlled openings (freedom profiles per state, per Architectural Frame §2 of problem-space).
- Defines **verifier** at the foundation level: verification is a node type in the graph (per locked decision #9), with specified inputs (what is being verified), outputs (pass/fail/partial + evidence), and emission (event on completion).
- Defines **traces** at the foundation level: transition records carrying prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence. Traces are durable and distinct from events.

**1.4 The subsystem envelope.**
- Specifies what every subsystem (S01–S09 today, S10 tomorrow) must declare. **Runtime realization of a subsystem** is defined in §1.4a; subsystems declare their envelope in the shape below regardless of runtime shape.
  - **Events it produces** (type names, schemas cited from event-model).
  - **Events it consumes.**
  - **Types it introduces that appear in cross-subsystem event payloads or shared state** (types transitively referenced by any event payload a *different* subsystem consumes). Each such type carries the four-axis + mechanism/cognition tags per SC-10/SC-10b. This declaration surface closes the loophole where a subsystem could mark its types "internal" to avoid tagging while still exporting them via event payloads.
  - **Handlers it implements** (if any — cited from handler-contract).
  - **State it owns** (types cited from execution-model).
  - **Control points it provides** (cited from control-points).
  - **NFRs it inherits and/or overrides** (cited from operator-nfr).
  - **Boundary classification for each operation it exposes** (the four-axis + mechanism/cognition tags).
- Specifies what every subsystem must NOT do: invent shared vocabulary, violate the boundary, skip NFR compliance.
- Provides an "add a new subsystem" procedure that references only foundation — a new subsystem spec is written, foundation is not revised.

**1.4a Runtime realization of a subsystem — pinned for MVH.**
- For MVH, a **subsystem is a Go package inside the daemon process** (see process-lifecycle.md §8.6 for the daemon's single-binary shape). The envelope's "events it produces / consumes" discipline is therefore **inter-package discipline within a single process**, not inter-process messaging. The event bus (§3.7) is in-process; the consumer taxonomy's "in-process synchronous / in-process asynchronous / fan-out observer" classes all live inside the same daemon binary.
- **The only out-of-process actors in MVH are:**
  - (a) **Agent handler subprocesses** (Claude Code, Pi, twin binaries; per handler-contract.md §4.10, §4.12 and process-lifecycle.md §8.5). Handlers are NOT subsystems; they implement the handler contract but do not declare a subsystem envelope.
  - (b) **Orchestrator-agent sessions** (per process-lifecycle.md §8.6 — separate Claude Code sessions that drive the daemon via CLI). Orchestrator-agents are NOT subsystems; they are external callers of the command surface.
  - (c) **The `br` CLI invocations** the daemon spawns for Beads reads/writes (per beads-integration.md §10.2). `br` is not a subsystem; it is an external dependency invoked as a subprocess.
- **Reconciliation is NOT a subsystem** (per §9.1). Reconciliation is a **workflow-library entry** — a named set of DOT workflows + YAML policies packaged with harmonik. It runs on the same primitives as any other workflow; it has no envelope declaration and introduces no shared types. Cross-reference: reconciliation.md §9.1.
- **Post-MVH shape is out of scope for foundation.** If harmonik later evolves to a multi-process shape (e.g., splitting the event bus into its own process), the subsystem envelope's semantics remain: each Go package that declares an envelope is a subsystem regardless of which process binary hosts it. Post-MVH process geometry is a subsystem spec concern, not a foundation concern.
- **Mechanism-tagged.** Subsystem definition is structural; no cognition participates.

**1.5 Foundation amendment protocol.**
- Specifies the process when downstream spec work discovers a foundation gap:
  1. Downstream agent writes an amendment proposal (≥1 paragraph describing the gap + proposed change).
  2. Amendment reviewed by ≥2 foundation-review personas (architect + critic at minimum).
  3. If accepted, foundation spec revised; revision triggers re-review of any subsystem spec that cited the affected foundation spec.
  4. If rejected, downstream spec adapts within existing foundation constraints.
- Specifies the authority: foundation amendments are a product-level decision; the orchestrator agent proposes, reviewer personas critique, the user approves material changes.
- Specifies that foundation is versioned; each subsystem spec cites the foundation version it conforms to.
- **Specifies parallel-amendment serialization and overlap detection.** When two or more amendment proposals are open simultaneously: (i) every proposal declares the foundation spec sections it touches; (ii) a mechanical overlap detector scans declared sections across open proposals; (iii) overlapping proposals are serialized — later proposals are rebased onto earlier merged proposals or rejected-with-reason if the earlier amendment removed the premise; (iv) non-overlapping proposals may merge independently in any order. This is mechanism-tagged: no cognition is required to detect overlap, only section-path comparison.

**1.6 Role taxonomy.**
- The AlphaGo north-star names seven roles: Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor.
- Foundation specifies which roles are **MVH-required** and which are **declared-but-deferred**:
  - MVH-required: Planner, Builder, Reviewer (sufficient to run the minimum self-build cycle described in bootstrap.md §3).
  - Declared-but-deferred: Researcher, Verifier, Scheduler, Governor. Named in foundation so subsystem specs don't invent alternatives, but not required at MVH time — each is activated when its triggering pattern appears in a workflow.
- For each role, this spec defines: purpose, typical actions, what it does not do. **Concrete permission schemas per role are owned by control-points.md §6.6, not this spec.** architecture.md describes WHAT the role is; control-points.md describes WHAT A ROLE IS ALLOWED TO DO.
- States that role is orthogonal to agent type (§1.6a): role is a *function* assignment; handler is a *process* assignment. The same agent process can fill different roles across runs.
- **Merge responsibility is NOT a role.** A workflow may contain a distinct merge node assigned to whatever agent type (Builder, Reviewer, or a dedicated merger) the policy names. The merge node operates in the RUN's leased worktree (per workspace-model.md §5.1 and §5.4), not in a new worktree. Merge is a node function, not a top-level role in the taxonomy.
- Scope authority: this section is the normative definition of the role set. control-points.md §6.6 cites this section by §1.6 for names and semantics; this section cites control-points.md §6.6 for concrete permissions. Neither spec duplicates the other's content.

**1.6a Agent type abstraction — concept definition.**
- Defines `agent type` as a handler-contract conformance class: Claude Code, Pi, `claude-twin`, `pi-twin`, and any future handler are instances of the same abstraction. The abstraction consists of: (a) the Go `Handler` interface defined in handler-contract.md §4.1, (b) the wire protocol defined in §4.2, (c) the event emission contract defined in event-model.md §3.2, (d) the secrets-delivery contract defined in §4.7.
- Agent type is ORTHOGONAL to role (see §1.6): role is a function assignment (planner, builder, reviewer); agent type is a process assignment (Claude Code process, Pi process, twin process). The same agent type can fill different roles; the same role can be filled by different agent types across runs.
- Adding a new agent type is an exercise of the subsystem envelope (§1.4) — the new type's handler package writes a subsystem spec that cites handler-contract.md and declares its envelope. No foundation revision required.
- **Stable identifier shape.** An `agent_type` is a **lowercase-hyphenated ASCII string** matching the regex `^[a-z][a-z0-9-]{1,62}$`. Reserved identifiers for MVH: `claude-code`, `pi`, `claude-twin`, `pi-twin`. A new agent type is registered by (a) the subsystem spec introducing it declaring the identifier in its envelope, and (b) the handler-contract.md §4.1 conformance class being claimed. Identifiers are process-scoped (daemon-wide); no namespace prefix is required for MVH. Post-MVH, if cross-daemon identifier conflicts become possible, a reverse-DNS prefix discipline may be introduced via foundation amendment.
- **Cross-subsystem reference.** Specifically: YAML policies (§6.5, §6.6, §6.7) reference `agent_type` in freedom-profile and role-assignment fields; DOT node attributes may carry `agent_type` as a routing hint; `LaunchSpec.agent_type` (handler-contract.md §4.2) uses this identifier; event payloads in event-model.md §3.2 that name an agent (e.g., `agent_started`) carry the identifier. All four usages MUST match byte-for-byte; mismatches are spec-draft-time errors.
- **Mechanism-tagged.**

**1.6b Verification — naming-conflict resolution.**
- "Verification" has three distinct meanings across the knowledge base. Foundation resolves them:
  - `verification-node` — a node type in the workflow graph whose purpose is to evaluate prior work. Non-agentic verification nodes are deterministic (test runner, linter, type-check, policy-check — mechanism-tagged); agentic verification nodes delegate to a reviewer agent (cognition-tagged). Per locked decision #9 there is no "verifier" subsystem; verification is realized as graph nodes.
  - `verification-result` — the outcome of running a verification node. Shape per execution-model.md §2.1 `outcome` with `status ∈ {SUCCESS, FAIL, PARTIAL_SUCCESS}`, and an `evidence` field (mechanism-tagged: structured output from the underlying tool) or `notes` field (cognition-tagged: reviewer's written critique).
  - `quality-gate` — a gate (control-points.md §6.2) whose evaluator references a prior `verification-result`. Quality-gates are gates that happen to read verification outcomes; they are not themselves verification.
- All three are distinct; specs use the hyphenated forms above.

**1.7 Harness-engineering invariants.**
- Repository as single source of truth (everything in repo, no external wikis).
- Guides + sensors: declarative constraints (specs, policies, conventions) + runtime checks (tests, verification nodes, review agents).
- Constrain to empower: strict architectural boundaries as a productivity multiplier.
- Filesystem-backed coordination: agents read/write files; conversations are ephemeral.
- Quality-left: fast deterministic checks early (lint, type, policy), expensive inferential checks late (review agent, scenario run).

**1.8 Centralized-controller principle.**
- States the load-bearing invariant: harmonik is a centralized-controller system. The deterministic daemon (Go binary, no LLM logic; see process-lifecycle.md §8.6 for the daemon-vs-orchestrator-agent distinction) owns ALL workflow state, routing, and dispatch.
- Agents perform ONLY cognitive work. Agent-to-agent coordination MUST route through the daemon — NOT through files, NOT through ad hoc IPC between agent processes.
- Explicit inverse of the Gas Town polecats/mayors decentralized-orchestration pattern. The user rejected Gas Town's decentralized-agent-coordination model in favor of centralized deterministic dispatch.
- Merge responsibility: a merge agent (when the workflow uses a distinct merge node) operates in the SAME worktree the implementer used; the worktree is leased by the workflow RUN, not by any individual agent. See workspace-model.md §5.1 (lease-by-run) and §5.4 (merge semantics).
- Rationale: collapses an entire class of coordination problems (file reservations, agent-to-agent message routing, distributed consensus) that Gas Town's model would otherwise require. Core operational test: if a design proposal introduces file-based agent-to-agent handoff or per-agent worktree ownership for the same run, it fails this principle.
- **Acknowledged tradeoff — no graceful degradation under daemon failure.** The centralized-controller principle carries a real cost: if the daemon dies mid-run, every agent goes silent and reconciliation is required on restart (per §9.1). A decentralized alternative (Gas Town polecats/mayors pattern) offers graceful degradation: if the central mayor dies, polecats still coordinate directly. Harmonik's centralized-controller choice foregoes that property in favor of routing simplicity. Within the MVH envelope — single-user, single-project, developer-machine — the daemon is colocated with the work, so "daemon dies" and "machine dies" have the same recovery path (reconcile from git + Beads). The tradeoff is therefore acceptable for MVH. But it is a tradeoff: in scenarios where the daemon is the only failure domain (remote-daemon, multi-operator-single-daemon), centralized-controller's brittleness is a real cost that the decentralized alternative does not pay. This is acknowledged here; a pivot to decentralized would require re-evaluating the principle under those scenarios.
- Mechanism-tagged: daemon routing is deterministic. Mechanism/cognition split is strict — daemon is all mechanism; agents carry the cognition.

**1.9 Three-artifact separation.**
- States the glossary rule: harmonik uses THREE distinct artifacts for work composition, and NONE is a projection of any other.
  - **`spec`** — a kerf output landing in `specs/` at the repo root. The design/thinking artifact: what, why, acceptance criteria. Normative for system behavior.
  - **`workflow graph`** — a DOT document (per execution-model.md §2.1). The normative description of HOW execution happens: nodes, edges, policies by reference.
  - **`bead`** — an atomic queued work item in the Beads SQLite store (per beads-integration.md §10.1–10.3). The claimable unit the daemon dispatches a run against.
- Relationships are many-to-many without projection: a spec may describe zero, one, or many workflows and beads. A workflow may be invoked by many beads. A bead references the workflow + node it is executing, and carries a bead ID that appears in the run's checkpoint-commit trailers and event payloads (per beads-integration.md §10.6).
- **"Feature" is NOT a product primitive.** Work size varies by how many nodes or sub-graphs an agent composes, not by a distinct `feature` entity. Specs, workflows, and beads are the normative vocabulary; "feature" is at most a casual-speech term.
- Mechanism-tagged glossary. Any subsystem spec that invents a fourth compositional artifact or treats one as projected from another fails ZFC at the structural layer.

### Dependencies
- **Depends on:** none (foundation's root spec).
- **Depended on by:** all other foundation specs (they cite §1.1 classification tests, §1.4 subsystem envelope, §1.8 centralized-controller, and §1.9 three-artifact separation at minimum); all subsystem specs.

---

## Component 2: `specs/execution-model.md`

### Purpose
Normative definition of the core execution data model and the outcome-spine contract that threads through handler → hook → gate → transition → event.

### Requirements

**2.1 Core types — SC-1 density for each (identity, fields with Go types, lifecycle states with allowed transitions, cross-subsystem reference contract).**

- `workflow` — a named, versioned DAG. Fields: `workflow_id`, `name`, `version`, `nodes[]`, `edges[]`, `policies[]` (cited from control-points), `metadata`. **Represented on disk as a DOT document** (locked decision #11 per problem-space §Locked decisions). DOT node and edge attributes encode: node type (agentic / non-agentic / gate / control-point / sub-workflow), handler reference (if agentic — cited from handler-contract.md §4.1), edge conditions and labels, policy references (`policy_ref`, `gate_ref`, `freedom_profile_ref`, `budget_ref` — per control-points.md §6.5), and required skills for the node (per control-points.md §6.11 and handler-contract.md §4.11). **Policies themselves remain YAML documents** in a sibling directory; DOT workflow attributes reference the YAML policy documents by name. The foundation corpus therefore uses **three serialization formats** with non-overlapping responsibilities: DOT (workflow graphs), YAML (policies including freedom profiles, role permissions, gate definitions, budgets), JSONL (the observational event stream — NOT state reconstruction; see §2.2 and event-model.md §3.6). Replaces ambiguous "workflow definition" usage. **Acknowledged — DOT untyped-attribute validator obligation.** DOT attributes are untyped strings. A DOT workflow with `policy_ref = "nonexistent-policy"` or `idempotency_class = "idempotant"` [sic] parses cleanly but fails at runtime. Spec-draft produces a normative **external workflow-attribute validator** that runs at workflow-ingest time and enforces: (a) typed attribute shapes (enum values match, references resolve), (b) cross-attribute consistency (e.g., `agent_type` per §1.6a matches a registered handler, `policy_ref` resolves to a YAML policy on disk). The validator is itself mechanism-tagged. DOT's native lack of typing is a known limitation absorbed by this validator; a future pivot to a typed workflow DSL (Starlark, custom typed language) is a foundation amendment, not an incremental change.
- `node` — a graph vertex with a `node_id`, a `type` (agentic, non-agentic, gate, control-point, sub-workflow), handler reference (if agentic — cited from handler-contract), inputs, expected outputs, timeout, boundary classification (axis tags; idempotency-axis tag derives from `idempotency_class` per §2.1c), and a **reconciliation-behavior tag** (`idempotency_class ∈ {idempotent, non-idempotent, recoverable-non-idempotent}` per §2.1c).
- `edge` — a directed transition with a `from_node`, `to_node`, optional `condition` expression, optional `preferred_label`, `weight`, ordering key. Deterministic edge selection cascade specified (condition match → label match → preferred_label → weight → lexical order).
- `run` — one execution of one workflow against one input, with a stable `run_id`, `workflow_id`, `workflow_version`, `input` (workspace reference, not inline), `state` (current), `transitions[]` history, `start_time`, optional `end_time`. Replaces ambiguous "task" / "cycle" / "work item" / "workflow execution" usage (see §Naming-Conflict Resolution below).
- `state` — a position in a run's progression. Fields: `state_id`, `run_id`, `node_id`, `entered_at`, `transition_history`, boundary-classification. States are durable checkpoints.
- `transition` — a move from one state to another. Fields: `transition_id`, `run_id`, `from_state`, `to_state`, `actor_role`, `candidate_actions` (the full set considered), `chosen_action`, `policy_version`, `evidence` (structured), `verifier_metrics`, `confidence`. Matches the AlphaGo trace contract. Durable storage per §2.1b.
- `checkpoint` — durable state record. Represented as a git commit per locked decision. The commit **TREE** carries (a) the work product (files at that state transition) AND (b) a **transition-record sibling file** at the canonical path `.harmonik/transitions/<transition_id>.json` within the tree. The sibling file contains the full `transition` record (see §2.1b) in typed JSON. The commit **MESSAGE** carries structured trailers as a cheap index: `Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, `Harmonik-Schema-Version` (all required); `Harmonik-Bead-ID` optional (present when the run is tied to a bead; per beads-integration.md §10.6). Schema version is integer-incrementing; upgrade readability contract specified (N-1 always readable — see operator-nfr.md §7.5).
- `outcome` — handler-produced result. Fields: `status` (SUCCESS / FAIL / RETRY / PARTIAL_SUCCESS), `preferred_label` (routing hint, optional), `suggested_next_ids[]` (routing hint, optional), `context_updates` (key/value), `notes` (freeform). Shape derived from Attractor recon and adapted to harmonik's semantics.

**2.1a Checkpoint cadence and last-durable-state invariant.**
- Normative cadence: checkpoints fire at EVERY durable state transition. No in-flight run may advance past a durable state transition without emitting a checkpoint commit before the transition event is considered final.
- Invariant: git always knows the last durable state of every in-flight run. Restart reconciliation (reconciliation.md §9.2) relies on this; if the invariant is violated, reconciliation cannot classify the run's state without JSONL replay (which harmonik explicitly does NOT use for state reconstruction — see event-model.md §3.4 and §3.6).
- **Failure commits explicitly NOT required for MVH.** A failed transition emits a failure event (event-model.md §3.2) but does not create a checkpoint commit. Design slot: revisit if the improvement loop later needs `git bisect` over failures. See reconciliation.md §9.7.
- Cadence is mechanism-tagged: commit emission is deterministic given the transition; no cognition participates in deciding when to checkpoint. Reconciliation workflows are an explicit exception per reconciliation.md §9.1a (verdict-only commit).

**2.1b Transition-record storage — the sibling-file contract.**
- Every checkpoint commit that records a durable state transition MUST include, in the commit tree, a **transition-record file** at the path `.harmonik/transitions/<transition_id>.json`. The file is typed JSON containing the full `transition` record per §2.1: `transition_id`, `run_id`, `from_state`, `to_state`, `actor_role`, `candidate_actions[]` (full set considered), `chosen_action`, `policy_version`, `evidence` (structured), `verifier_metrics`, `confidence`.
- **Discovery path.** Given a `transition_id` and a branch tip, the record is retrievable via `git show <commit>:.harmonik/transitions/<transition_id>.json` where `<commit>` is the checkpoint commit whose `Harmonik-Transition-ID` trailer matches. This makes records deterministically addressable without a cross-commit index.
- **Immutability.** Once committed, the transition-record file is never rewritten. A new transition in the same run adds a new file under a new `transition_id`; it does not modify the old file.
- **Size contract.** The file is expected to be small (single-digit KB typical). Large `evidence` or `verifier_metrics` payloads MUST be externalized (sibling files under `.harmonik/transitions/<transition_id>/evidence/*`) and referenced from the primary record by relative path, to keep the primary record cheaply parseable.
- **Schema versioning.** The JSON document carries a `schema_version` field (integer) that matches the commit's `Harmonik-Schema-Version` trailer. Readers enforce N-1 readability per operator-nfr.md §7.5.
- **Mechanism-tagged.** The sibling-file write and read paths are deterministic; no cognition participates.

**2.1c Node idempotency tag — reconciliation input.**
- Every `node` carries an **`idempotency_class`** field declared on the DOT node attribute (`idempotency_class = "idempotent"` etc.) or inherited from the node's type default. Values:
  - **`idempotent`** — the node is safe to re-spawn after interruption against the same state; re-spawning produces an equivalent outcome. Examples: reviewer-agent nodes, researcher-agent nodes, non-agentic nodes that read-only-inspect (lint, typecheck, test-runner). Reconciliation treats this class as Cat 1 per §9.2 (auto-resume by re-spawning). Default for node types: reviewer, researcher, lint, test, typecheck, analysis.
  - **`non-idempotent`** — the node may have produced partial filesystem or external effects mid-execution; re-spawning without intervention risks corruption or duplicate effects. Examples: builder-agent nodes (mid-implementation file writes), merge-agent nodes (mid-merge state). Reconciliation treats this class as Cat 2 per §9.2 (quarantine + investigator). Default for node types: builder, merge.
  - **`recoverable-non-idempotent`** — the node makes durable changes but has a declared resume protocol (the node's handler knows how to detect prior in-progress state and resume). Examples: an `rsync --partial` wrapper, a node with explicit checkpoint-sub-protocol. Reconciliation treats this class as Cat 2 by default (so investigator decides whether to invoke the resume protocol) unless the node's policy declares an auto-resume contract; see §9.3 for the detector's precedence. Post-MVH node types may register here; MVH defaults to none.
- **Declaration locus.** The `idempotency_class` is a DOT node attribute; YAML policies may set per-node-type defaults that the workflow author overrides on specific nodes. Attribute absence is an authoring error detected by the DOT validator (per §2.1 untyped-validator obligation per F-C6-4).
- **Reconciliation consumption.** Cat 1 detector (§9.3) classifies a crashed node by reading this tag from the node's DOT attributes (retrieved via the checkpoint's `node_id`). Detector is mechanism-tagged.
- **Mechanism-tagged.** Tag declaration and detector consumption are both deterministic; cognition does not participate.

**2.2 Outcome spine — the integrated contract.**
- Specifies that handler outcome → hook dispatch → gate evaluation → transition selection → event emission is ONE integrated flow.
- For each segment: which spec owns it (handler-contract owns outcome emission; control-points owns hook dispatch and gate evaluation; this spec owns transition; event-model owns event shape). Cross-reference map explicit.
- Specifies the relationship between `transition` (trace record) and `transition_event` (bus event). They are **two views of one underlying record**, not two independent writes:
  - The canonical durable record is the `transition` JSON file at `.harmonik/transitions/<transition_id>.json` in the checkpoint commit's tree (per §2.1b). The file carries the full AlphaGo fields (prior state, actor role, candidate actions, chosen action, policy version, evidence, verifier metrics, next state, confidence).
  - A `transition_event` is emitted to the event bus **as a projection** of the transition record. The event payload cites the transition by `transition_id` plus the containing `run_id` and checkpoint commit hash; the full record is recoverable via `git show <commit>:.harmonik/transitions/<transition_id>.json` (per §2.1b). This prevents storage duplication and prevents trace/event schema drift.
  - Consumers that need cheap/streamable access read `transition_event` from the bus; consumers that need complete audit fidelity read the `transition` record from git. One record, two retrieval paths.
- Specifies the "deterministic replay" contract: given the run's **git checkpoint trail** and Beads bead record, the run's state can be reconstructed to any point (for debugging, for audit, for scenario harness assertions, and — most importantly — for restart reconciliation per reconciliation.md §9.1). **"Transition history" in this spec means the git checkpoint trail, NOT the JSONL event tail.** JSONL is observational only (see event-model.md §3.6 for the observational-replay vs state-reconstruction split).
- **Authoritative statement on trace vs. event duplication:** the transition-record file under `.harmonik/transitions/` in the checkpoint commit's tree is the canonical transition record. The `transition_event` in the JSONL event stream is a projection for streaming consumers; the full record is recoverable by resolving the checkpoint reference. On restart, state reconstruction walks git + queries Beads; JSONL is NOT replayed to rebuild state.

**2.3 Failure taxonomy — harmonik's own.**
- Names harmonik's failure classes. Candidates for the research pass to decide **using these explicit criteria**:
  - **Criterion 1 — coverage:** each class corresponds to a distinct retry/escalation response. Two classes with the same response collapse to one.
  - **Criterion 2 — classifier determinism:** the class must be determinable from structured fields of the outcome (exit code, error type, timeout flag). Any class requiring semantic judgment belongs in a cognition-tagged verification node, not the taxonomy.
  - **Criterion 3 — subsystem consumability:** each class must be consumable by S02 retry-policy evaluation, S09 pattern analysis, and S04 agent-failed event routing without further disambiguation.
  - **Candidate classes (all six must be evaluated against the criteria):**
    - `transient` — safe to retry unchanged (network failure, rate limit, timeout without progress)
    - `structural` — retry only after approach change (wrong tool, missing precondition recoverable by different plan)
    - `deterministic` — must not retry (confirmed bug, impossible condition)
    - `canceled` — operator or policy interrupted (not a failure of the approach)
    - `budget_exhausted` — resource limit hit (retry contingent on budget increase)
    - `compilation_loop` — revision-loop specific: fixes introduce new regressions (cap on retry attempts)
- For each class: detection signal (who classifies, using what evidence), implied retry policy (concrete: N attempts with M-second backoff), escalation path.
- Classifier is mechanism-tagged: the classification decision is deterministic given the outcome's fields. Any "is this failure semantically X or Y?" judgment that requires a model is a cognition-tagged node, not a classifier.

**2.4 Backtracking model — the four rollback types.**
- `local-patchback` — fix the current output and retry. Preserves transition history; adds a new transition on top.
- `architectural` — revert to a prior design decision. Preserves history; marks the branch as abandoned; spawns a new state from an earlier checkpoint.
- `policy` — revert a policy change. Specific to the improvement loop; re-runs recent transitions under the prior policy version.
- `context` — restore a previous context window. Agent-level; does not alter graph state.
- Specifies how each is represented in the state/transition model.

**2.5 Naming-conflict resolution.**
- "Cycle" — `cycle` now means ONLY cycle-detection in the graph (Kilroy-style: cap on edge traversals to prevent infinite loops). The "self-build cycle" in bootstrap.md is renamed `run` (per 2.1). The "improvement cycle" in S09 is renamed `improvement-pause` (term scoped to operator-control semantics, specified in operator-nfr.md).
- "Gate" — specified in control-points.md as one of the control-point kinds; this spec references it.
- "Checkpoint" — unified meaning in this spec (a git commit with structured message).
- "State" — unified meaning in this spec (position in a run's progression).
- **"Bead" vs. "run".** A `bead` is a queued work item in the Beads SQLite store (beads-integration.md §10.3). A `run` is one execution of a workflow against a bead as input. Multiple runs MAY reference the same bead: the first run failed fundamentally (crash, unrecoverable error, investigator `reopen-bead` verdict per reconciliation.md §9.5) and a subsequent claim spawned a second run. A new run gets a fresh worktree + fresh branch (per workspace-model.md §5.9 re-run rule). **Intra-run loops** (a workflow edge routing back to an earlier node) are NOT new runs; they are edge traversals within a single run. Re-runs happen ONLY on fundamental failure, not on intra-run recovery.

**2.6 Three-store cross-reference.**
- Harmonik persists workflow-related state in three stores. Each is authoritative in its own domain; none is a cache of the others.
  - **git** — authoritative for **completion** and the **work product** (files at each state transition). The checkpoint commit trail (§2.1, §2.1a) is the canonical state-reconstruction source.
  - **Beads (SQLite, via `br` CLI)** — authoritative for **bead content** (title, description, type), **dependency edges** (parent-child, blocks, conditional-blocks, waits-for), and **coarse bead status** (open / in_progress / closed / deferred / tombstone). See beads-integration.md §10.3.
  - **JSONL event log** — authoritative for the **observational sequence** of events the daemon emitted. NOT used for state reconstruction. Consumers: CASS memory indexing, improvement loop, observability dashboards, post-mortem debugging. See event-model.md §3.6.
- **Store-authority rule on completion disagreement: git wins.** If Beads reports a bead as `closed` but no merge commit exists in git, or a transition event appears in JSONL but no corresponding checkpoint commit exists, the divergence is a flag, not a completion. An investigator agent (per reconciliation.md §9.2 Cat 3) resolves; the daemon does NOT silently auto-reconcile.
- Cross-reference: reconciliation.md §9.2 defines the category taxonomy for store divergence; beads-integration.md §10.7 names the Beads-side authority rules.

### Dependencies
- **Depends on:** architecture.md (§1.1 boundary axes, §1.3 traces, §1.5 amendment, §1.8 centralized controller, §1.9 three-artifact separation).
- **Depended on by:** event-model.md (events reference run/state/transition), handler-contract.md (handlers produce outcomes), control-points.md (control points gate transitions), workspace-model.md (workspaces bind to runs), operator-nfr.md (operator controls interrupt runs), reconciliation.md (checkpoints are the state-reconstruction source), beads-integration.md (bead-ID trailer on checkpoints).

---

## Component 3: `specs/event-model.md`

### Purpose
Normative event schema, versioning, clock semantics, durability contract, and replay model.

### Requirements

**3.1 Event schema — mandatory fields.**
- `event_id` (UUID v7 for time-ordering without coordinated clocks), `schema_version` (integer), `type` (from typed taxonomy — see 3.2), `timestamp_wall` (RFC 3339 wall clock at emitter), `timestamp_mono_nsec` (optional, monotonic nanoseconds from emitter's process for intra-process ordering; **process-scoped — NOT comparable across daemon restarts**), `run_id` (if scoped to a run), `state_id` (if scoped to a state), `source_subsystem` (an opaque subsystem identifier string — the schema does not enumerate "S01–S09"; any subsystem ID declared via the subsystem envelope is acceptable, keeping the schema layout-open for S10+; the subsystem ID is a Go package identifier per §1.4a; all subsystems share process space in the MVH daemon), `trace_context` (for cross-subsystem correlation), `payload` (typed per 3.2).
- Emission contract: every normatively required emission point is specified in the spec that owns the emitter (subsystem specs).
- **Every event type declared in §3.2 carries the four-axis tags (LLM-freedom, I/O determinism, replay-safety, idempotency) plus the mechanism/cognition tag per SC-10.** Tagging is mandatory for event types; this spec's requirement echoes SC-10b.
- **Events are lifecycle-boundary signals, NOT agent internals.** JSONL holds lifecycle-boundary events (agent started, agent completed, transition occurred, budget warning fired). Agent internals (tool calls, thinking, full token-by-token output) live in the agent's own session log (Claude Code's log, Pi's log) and are CASS-indexed separately (per workspace-model.md §5.3). Agent-chunk events like `agent_output_chunk` and `budget_accrual` are explicitly retained for MVH (see §3.2a); they remain lifecycle-boundary signals routed to the bus, not the mechanism by which the orchestrator reconstructs agent-internal state.

**3.1a Go representation — decision criterion and commitment.**
- Event types in Go are represented as a **tagged-union pattern**: a top-level `Event` envelope struct carries the common fields (`event_id`, `schema_version`, `type`, `timestamp_wall`, `timestamp_mono_nsec`, `run_id`, `state_id`, `source_subsystem`, `trace_context`) and a `Payload json.RawMessage`. The `Payload` is decoded against a typed struct chosen by `Event.type`, using a registry of `map[EventType]func() EventPayload` constructors.
- Decision criterion: the registry approach allows the event-type list to grow per the amendment protocol without a single breaking change to the Event struct. Alternatives considered and rejected: (a) a single `Event` struct with all possible fields (breaks on every new type), (b) a tagged-union via generics (Go generics don't support discriminated unions cleanly), (c) one Go type per event with no common envelope (consumers can't process heterogeneous streams). The registry + RawMessage pattern is standard in Go for this class of problem (see `encoding/json` stdlib patterns).
- Envelope + registry is mechanism-tagged; type dispatch is deterministic on the `type` field.

**3.2 Typed event taxonomy.**
- Enumerates the complete set of event types for foundation MVH. Each type has: required/optional payload fields, emitter subsystem (opaque ID, per §3.1), typical consumers, the four-axis + mechanism/cognition tags (per SC-10).
- **Complete MVH starting list** (research pass prunes/validates but does not extend without criteria): `run_started`, `run_completed`, `run_failed`, `state_entered`, `state_exited`, `transition_event` (projection of the `transition` trace per execution-model.md §2.2; NOT authoritative — git checkpoint is), `outcome_emitted`, `hook_fired`, `gate_evaluated`, `guard_denied` (Guard control-point rejected an edge — per control-points.md §6.4), `checkpoint_written` (payload includes `run_id`, `state_id`, `transition_id`, optional `bead_id` for audit queries joining event stream to Beads), `agent_ready` (ready-state signal per handler-contract §4.9), `agent_started`, `agent_output_chunk`, `agent_completed`, `agent_failed`, `agent_rate_limited`, `session_log_location`, `skills_provisioned` (emitted by handler per handler-contract.md §4.11 — names the skill set installed for the agent), `budget_warning`, `budget_accrual` (per-chunk cost event from handler subprocess — retained for MVH per §3.2a), `budget_exhausted`, `workspace_created`, `workspace_leased`, `workspace_merge_pending`, `workspace_merged`, `workspace_discarded`, `workspace_interrupted` (workspace lost or became inaccessible mid-run — detected by reconciliation per reconciliation.md §9.2 Cat 6), `merge_conflict_escalation` (emitted by S06 when a merge requires human resolution), `policy_violation`, `consumer_failed` (synchronous consumer failed to process an event — per §3.7), `dead_letter_enqueued` (async consumer retries exhausted; event moved to dead-letter queue — per §3.7), `operator_pausing`, `operator_paused`, `operator_resuming`, `operator_stopped`, `operator_upgrading`, `health_check`, `metric` (a measurement emitted for observability, with `metric_name` + `value` fields — replaces the distributed-tracing-format deferral per problem-space §7.10), `reconciliation_started` (reconciliation workflow began for an in-flight run — per reconciliation.md §9.1), `reconciliation_category_assigned` (detector classified an in-flight run into one of Cat 1–6 — per reconciliation.md §9.2–9.3), `reconciliation_verdict_emitted` (investigator agent emitted verdict from vocabulary in reconciliation.md §9.5), `store_divergence_detected` (git / Beads / JSONL disagreement found — Cat 3 trigger; payload schema per reconciliation.md §9.3a).
- **Reconciliation + crash-safety event-type additions** (per round-2 foundation amendments):
  - `reconciliation_verdict_executed` (payload: `investigator_run_id`, `target_run_id`, `verdict` [enum per §9.5], `executed_at_timestamp`, `action_summary`); emitter: daemon-core; consumers: reconciliation-monitoring, audit, improvement-loop.
  - `reconciliation_verdict_malformed` (payload per §9.5a: `investigator_run_id`, `target_run_id`, `malformation_reason`, `raw_verdict_excerpt`); emitter: daemon-core; consumers: reconciliation-monitoring, audit.
  - `reconciliation_budget_exhausted` (payload per §9.4a: `run_id`, `workflow_id`, `budget_seconds`, `elapsed_seconds`); emitter: daemon-core; consumers: reconciliation-monitoring, audit, improvement-loop.
  - `reconciliation_verdict_stale` (payload per §9.4b: `investigator_run_id`, `target_run_id`, `snapshot_token`, `current_state`, `divergence_reason`); emitter: daemon-core; consumers: reconciliation-monitoring, audit.
  - `infrastructure_unavailable` (payload per §9.2 Cat 0: `failed_prerequisite` [enum: `br_missing`, `br_timeout`, `br_version_incompatible`, `beads_sqlite_locked`, `git_index_locked`, `harmonik_dir_unwritable`, `filesystem_full`], `detail_string`, `retry_count`); emitter: daemon-core; consumers: operator-observability, audit.
  - `daemon_orphan_sweep_completed` (payload per process-lifecycle.md §8.2 step 1a: counts of tmux sessions killed, locks cleared, subprocesses killed); emitter: daemon-core; consumers: observability, audit.
  - `operator_escalation_required` (payload: `target_run_id`, `reason` [enum including `cat_6a_investigator_escalated`, `cat_6b_auto_escalated`, plus verdict-driven variants], `reference_commits[]`); emitter: daemon-core; consumers: operator-observability, audit.
- The listed types are the **complete cross-subsystem emission surface** for MVH. Any subsystem that emits or consumes an event type not on this list must amend the spec (per §1.5 amendment protocol). Subsystems may emit internal events not listed here; those do not cross the bus.
- Adding a new event type: a subsystem spec declares it; event-model spec is revised per the foundation amendment protocol.

**3.2a Event taxonomy acceptance criteria and granularity stance** (for research pass and future amendments):
- A candidate event type is accepted iff: (a) at least one cross-subsystem consumer exists (granularity criterion 1: cross-subsystem boundary), (b) it is a lifecycle-boundary signal rather than an intra-lifecycle detail (granularity criterion 2), (c) at least one cross-subsystem consumer requires per-chunk or per-boundary access rather than a single summary event (granularity criterion 3), (d) its payload schema is defined with typed Go fields, (e) it carries the four-axis tags, (f) it specifies its replay side-effect classification per §3.6.
- **Granularity stance: keep fine-grained types for MVH.** Per-chunk events such as `agent_output_chunk` and `budget_accrual` remain in the taxonomy. Rationale: the improvement-loop subsystem (future) wants per-chunk cost attribution and mid-run signals; collapsing to a single summary event loses information the loop needs. A log-level / filter mechanism for suppressing chunk events at consumer boundaries is a future refinement slot. This position is recorded explicitly so later amendments can re-audit with evidence.

**3.3 Clock / time-source model.**
- Wall-clock (`timestamp_wall`) is the public-facing time; used for audit, logs, external correlation.
- Monotonic time (`timestamp_mono_nsec`) is for intra-process event ordering where wall-clock skew would break invariants.
- Cross-process ordering relies on `event_id` (UUID v7) for total ordering guarantees.
- NTP skew tolerance: wall-clock is advisory for ordering across processes; do not use wall-clock for ordering decisions.
- Reconciliation detectors are bound by this rule per reconciliation.md §9.3 ordering invariant.

**3.4 Fsync policy + event-loss window + restart semantics.**
- Specifies the fsync cadence: fsync is called at every run-boundary (run_started, run_completed, run_failed) and at every checkpoint_written event. Between these points, an optional timer-based flush MAY reduce the event-loss window; timer-flush cadence is operator-configurable.
- Names the event-loss window: in the worst case (hard crash between fsyncs), events emitted since the last fsync-point are lost. Producers MUST emit idempotent events so that loss-and-replay is safe.
- **Restart semantics — state reconstruction uses git + Beads ONLY.** The daemon walks git log (the checkpoint commit trail) and queries Beads (via `br` CLI) to build an in-memory model of completions + in-flight beads. Ambiguous in-flight state triggers reconciliation workflows (per reconciliation.md §9.1–9.2). The JSONL event log is **NOT replayed for state reconstruction**; it is observational only (§3.6). See operator-nfr.md §7.8 for the restart RTO target and process-lifecycle.md §8.2 for the startup sequence.
- The restart sequence is deterministic; it calls no LLMs directly (investigator agents, when spawned, run as regular workflows — cognition lives in the investigator node, not in the restart logic itself) and makes no external side effects in its deterministic portion (replay-safe tag per SC-10).

**3.5 Versioning and compatibility window.**
- Schema version is per-event-type AND per-envelope (the schema_version field).
- Compatibility contract: N-1 always readable. Breaking changes require a migration release; migration happens at an operator pause.
- Adding optional fields: non-breaking; bump schema version but N-1 readers accept.
- Removing/renaming fields: breaking; requires migration release.

**3.6 Replay semantics — observational vs. state-reconstruction (two distinct purposes).**
- The word "replay" is overloaded; this spec names each purpose explicitly.
  - **(a) Observational replay** — tooling walks the JSONL tail to answer questions like "what did we see between T1 and T2?" Consumers: debugging tools, improvement-loop pattern analysis, dashboards, audit. **Observational replay does NOT reconstruct system state.** Events must still be idempotent in their observational effect (so observational-replay tooling doesn't duplicate side effects it logs or indexes downstream).
  - **(b) State reconstruction** — the daemon (or a reconciliation tool) walks the **git checkpoint trail** and queries Beads to rebuild the in-memory state model on restart. This path is **deterministic, calls no LLMs, and is the only path used for correctness-critical state recovery** (see §3.4 and reconciliation.md §9.1).
- The JSONL event log is NEVER walked to reconstruct in-memory workflow state. Any tool that walks JSONL for state is at best a debugging aid and its output is advisory.
- Every event must be idempotent in its effect on observable state (producers enforce this; consumers assume this).
- What neither replay purpose can do: re-establish agent process state (agent sessions are not replayable), re-invoke LLMs (model output is not deterministic).
- **(c) Divergence-evidence read** — reconciliation detectors (reconciliation.md §9.3) and investigator agents (reconciliation.md §9.4) MAY read the JSONL tail to detect **inconsistency** between the three stores (git / Beads / JSONL). This is the third permitted use, distinct from (a) and (b): the JSONL read's purpose is to identify that stores disagree, NOT to supply missing state. The authoritative post-divergence decision is always driven by git + Beads per §3.4 and §9.2 Cat 3. A divergence-evidence read may ingest any portion of JSONL needed to surface the disagreement (checkpoint-missing-from-git, parse-failure, etc.), but its output is a typed divergence event (`store_divergence_detected` per §3.2), never a reconstructed state. Consumers of the divergence event cannot use it as state; they use it to trigger reconciliation workflows that re-authenticate against git + Beads.

**3.7 Producer/consumer contract.**
- Producers name their event types in their subsystem spec (the subsystem envelope declaration).
- Consumers subscribe by type; subscription is declared, not dynamic.
- **Consumer taxonomy (three classes, each with its own failure-handling rule):**
  - **Synchronous consumer (in-process, critical path)** (all consumers in MVH; see §1.4a) — e.g., the orchestrator's transition-advance logic. A consumer failure here halts the producer's progress on the specific run. The producer receives a typed error; it does NOT retry synchronously. A `consumer_failed` event is emitted. The run enters a quarantine state; operator escalation is required. There is no deadlock because no producer can have more than one synchronous consumer per event type (declaration-time check).
  - **Asynchronous consumer (in-process, side-channel)** — e.g., the memory layer's indexing consumer. A consumer failure does not block the producer. Failed deliveries go to a dead-letter queue (persisted; structure per operator-nfr.md §7.1 observability). Async consumers have bounded retry policy (3 attempts, exponential backoff); exhausted retries leave the event in the dead-letter queue.
  - **Fan-out observer (zero or more passive subscribers)** — e.g., dashboards, audit tools. Failures do not produce events or side effects beyond logging at the observer's side.
- A consumer's class is declared in its subscription; an in-process subscriber may not be synchronous by default — synchronous consumption is opt-in and requires explicit registration.
- **Dead-letter destination:** a persistent JSONL file (`${event_log_dir}/dead-letters.jsonl`) per the same durability contract as the main event log (§3.4). Operator can replay dead-letters after resolving the consumer issue; replayability honors the idempotency contract of the original event type.

**3.8 Structured-log format (replaces distributed tracing for MVH).**
- Every subsystem emits structured JSON logs in addition to events.
- Log schema: `timestamp_wall`, `level`, `source_subsystem`, `run_id` (if applicable), `state_id` (if applicable), `event_id` (if the log is associated with a specific event), `message`, optional key-value fields.
- Logs are NOT the event log; logs are human-readable debugging, events are machine-readable observational record. (Per §3.6 clarification: neither logs nor events are the state-reconstruction source; git + Beads are.)

### Dependencies
- **Depends on:** architecture.md (§1.1 classification — event emission is mechanism-tagged; §1.3 traces relationship), execution-model.md (run/state/transition types referenced in event scoping; §2.1a checkpoint cadence).
- **Depended on by:** all subsystem specs that emit or consume events; operator-nfr.md (operator events specified); handler-contract.md (handler-lifecycle events specified, `skills_provisioned` event); reconciliation.md (reconciliation-lifecycle event types; divergence-evidence read per §9.3a); beads-integration.md (optional `bead_id` payload field).

---

## Component 4: `specs/handler-contract.md`

### Purpose
Normative Go interface, process-boundary wire protocol, and Go-specific cross-cutting contracts (concurrency, context, errors, secrets) for agent handlers and their twins.

### Requirements

**4.1 Go interface — `Handler`.**
- Method signatures with Go types for:
  - `Launch(ctx context.Context, spec LaunchSpec) (Session, error)` — start an agent process.
  - `Session` is itself an interface with: `ID() SessionID`, `SendInput(ctx, input) error`, `Attach(ctx) (io.Reader, error)` for tmux/log attachment, `Kill(ctx) error`, `Wait(ctx) (Outcome, error)`, `LogLocation() string`.
- The interface is the SAME for real handlers and twin handlers. The selection of which handler binary to launch is config-level.

**4.2 Wire protocol — process-boundary contract.**
- Specifies stdin/stdout/files/sockets used by the handler subprocess.
- LaunchSpec delivery: research pass decides between two options with **explicit criteria**:
  - Option A — JSON on stdin (simple, composable, no file cleanup): preferred for short-lived handlers.
  - Option B — file-path argument (larger specs, inspectable post-launch): preferred for long-lived handlers or specs exceeding 1 MiB.
  - Criterion: spec size. If MVH LaunchSpec size stays under 1 MiB (expected), Option A. Otherwise Option B. Research pass measures.
- **LaunchSpec fields (authoritative).** Required fields include: `run_id`, `workflow_id`, `node_id`, `agent_type`, `workspace_path`, `required_skills[]` (per §4.11 — the set of skills the handler must ensure are available in the agent process), `skill_search_paths[]` (locations the handler searches to resolve declared skills), `bead_id` (optional — present when the run is tied to a bead per beads-integration.md §10.6), plus execution parameters (timeout, budget, freedom-profile ref). For investigator-agent handlers, LaunchSpec includes a `snapshot_token` per reconciliation.md §9.4b.
- Handler subprocess emits progress events on a named pipe or file; events conform to event-model schema.
- Handler subprocess writes its session log to a path the orchestrator knows (via `session_log_location` event emitted early in lifecycle; S04 is the emitter per workspace-model.md §5.3a).
- **Outcome delivery: event-based.** Outcome is emitted as a final `outcome_emitted` event on the progress stream. The subprocess exit status is used only as a liveness signal (exit 0 = clean shutdown, non-zero = crash before outcome emission). The outcome data model is authoritative; exit code is a secondary liveness indicator. Criterion for this choice: unifies with the real-time progress stream (no separate exit-value retrieval path), handles crash-before-outcome cleanly (no exit event is observed, which is itself a signal), and supports streaming outcomes for long-running handlers.
- Specifies version negotiation: the handler subprocess announces its supported protocol versions on a `handler_capabilities` event early in the session; the orchestrator selects a common version or aborts with a typed `ErrProtocolMismatch` error.

**4.3 Concurrency model.**
- **Goroutine ownership — daemon-owned watcher, S04-owned adapter.**
  - **Daemon (S01 Orchestrator Core)** spawns ONE watcher goroutine per active handler session. The watcher owns the session's read-loop (reading from the handler's progress stream), its lifecycle events (`agent_ready`, `agent_started`, `agent_output_chunk`, `agent_completed`, `agent_failed`), and its cleanup at session end. The watcher reads from the handler subprocess's named pipe or socket (per §4.2) and publishes events to the in-process event bus (per event-model.md §3.7).
  - **Agent Runner (S04)** owns the per-agent-type **adapter** — a non-goroutine-per-session callback object that the watcher invokes synchronously on specific lifecycle events. The adapter provides: ready-state detection (per §4.9), rate-limit recognition, clean-exit sequencing, account rotation (for handler types that support it). The adapter is stateless per-call or carries only agent-type-level state (not session-level state).
  - **Net.** The daemon has N watcher goroutines for N active sessions, each calling into S04's adapter (M adapters for M registered agent types). S04 itself has zero per-session goroutines; all per-session state lives in the watcher.
  - **Rationale.** Centralizing per-session concurrency in the daemon watcher means S04 can be re-implemented for new agent types by providing an adapter alone; the concurrency boundary never moves. This pins the centralized-controller principle (§1.8) at the concurrency layer.
  - **Mechanism-tagged.** Goroutine allocation is structural; cognition does not participate.
- **Channel closure:** "the emitter closes the channel on EOF; consumers treat closed channels as end-of-stream, not error."
- **Mutex discipline:** "state transitions acquire a per-run write lock; events are published without blocking the state lock; event consumers read from a per-subscriber channel."
- **Work queue patterns:** "the orchestrator maintains one work queue per agent role; workers drain their own queue; cross-queue handoffs go through explicit transitions, never shared memory."

**4.4 context.Context propagation.**
- Every public method takes `ctx context.Context` as its first parameter.
- Cancellation: ctx cancellation terminates in-flight operations; the operation's cleanup is declared.
- Deadlines: per-operation deadlines are declared; handlers propagate deadlines to subprocesses (via wire protocol).
- Values: context values are NOT used for business data (scoping rule — harmonik treats ctx values as deprecated pattern). Context values are for observability metadata only (trace IDs, user identity).

**4.5 Error-type strategy.**
- Typed error categories (concrete Go sentinel errors or types):
  - `ErrTransient` (retry)
  - `ErrStructural` (retry with different approach)
  - `ErrDeterministic` (do not retry)
  - `ErrCanceled` (operator/policy interrupted)
  - `ErrBudget` (resource limit)
- Wrapping: every error returned across a subsystem boundary MUST wrap with one of the sentinel categories (or satisfy an `errors.As` check for one).
- Matching: consumers use `errors.As` for category detection, not string comparison.

**4.6 Error propagation across async boundaries.**
- When a handler subprocess crashes: the agent runner detects via Wait; emits an `agent_failed` event with typed error category; the orchestrator surfaces this to gate evaluation; transition selection uses the failure-category-to-routing rules (from control-points).
- When a ctx is canceled mid-operation: in-flight goroutines return within a bounded interval (specified per subsystem; target ≤500ms for runtime goroutines, ≤5s for subprocess cleanup) with `context.Canceled`; partial state is preserved via checkpoint (last durable state).
- Specifies the "dead-letter" behavior for events that cannot be delivered.
- **Spec-draft obligation — silent-hang detection.** Spec-draft produces a normative silent-hang detector specification for handler subprocesses that are alive (socket connected, Wait not returning) but emit no progress events beyond a threshold T. The spec must define: T per agent type, the sequence (warning event → soft termination → hard termination), the resulting typed error category (maps to `ErrStructural` per current inclination).

**4.7 Secrets handling.**
- Secrets (API keys, tokens) are passed to the handler subprocess via environment variable named with a stable prefix (e.g., `HARMONIK_SECRET_*`).
- The `agent_started` event **never** includes environment variables. The handler subprocess receives secrets but the event stream does not.
- **Redaction mechanism (normative).** The foundation defines a **redaction registry**: a mechanism-tagged component that scans all event payloads and log lines before emission. The registry consists of:
  - A **common prefix rule**: any field whose name matches `(?i)(secret|token|password|api[_-]?key|auth)` is stripped to `"<redacted>"` before emission. Enforced by a middleware function in the event-bus producer path.
  - A **per-handler redaction list**: each handler spec may contribute additional regex patterns (for handler-specific secret formats, e.g., Anthropic API keys match `sk-ant-*`). Patterns are declared in the handler's subsystem envelope as redaction entries.
  - A **compile-time check**: the event-schema registry verifies at startup that no event type's payload schema names a field matching the prefix rule; any such field is a build-time error.
- No secret appears in the event log, session log (unredacted), or any audit record. Operator debugging of secret-related failures uses redacted forms; full-secret access requires operator privileges at the filesystem level outside harmonik.
- Secret rotation: out of scope for MVH (secrets are injected at handler launch; rotation requires a new launch).

**4.8 Twin parity contract.**
- Twins implement the same Handler interface, the same wire protocol, the same event schema.
- Twins differ from real handlers only in: the LLM call is replaced by scripted output; the model budget is not charged; the binary name is `*-twin`.
- Twins must satisfy the boundary classification: same mechanism/cognition tags as real handlers. A twin does not make decisions the real handler would delegate; it scripts them.
- Twin conformance (how twins are kept honest against real-agent drift) is scoped to S07 scenario-harness, NOT foundation.

**4.9 Ready-state detection.**
- Specifies the signal every handler uses to emit "ready to receive input": a specific event type `agent_ready` with required fields (`session_id`, `capabilities[]`).
- Real handlers must emit this on process startup before accepting work. Twins must emit the same.

**4.10 Agent-to-orchestrator trust (MVH).**
- Handler subprocesses are launched from a repo-relative path (not `$PATH` lookup).
- Before launch, the orchestrator verifies the handler binary exists at the expected path; a configurable commit-hash check (the build artifact's embedded commit hash matches the orchestrator's expected hash) is performed for binaries shipped in-repo. For system handlers (e.g., Claude Code CLI installed on the operator's machine), version reporting via `--version` is logged at startup, but no signature check is performed for MVH.
- **Launch model.** The daemon spawns the handler subprocess as a child process (per process-lifecycle.md §8.5). The handler subprocess communicates back to the daemon via a local Unix socket at `.harmonik/daemon.sock` (per process-lifecycle.md §8.1). Socket authenticity is filesystem-permission-based for MVH (the socket is owned by the operator's user; harmonik does not perform per-connection identity challenges). Post-MVH: per-connection authentication if the trust model evolves.
- Post-MVH: full binary signing / cosign verification (Q-R4 deferred).
- Twin binaries follow the same launched-from-known-path rule; the twin's expected commit hash is pinned at workflow/policy configuration time.

**4.11 Skill/tool injection obligation.**
- Normative: the handler MUST ensure the agent process has the skills/tools the workflow node requires before the agent begins work.
- **Skill declarations come from workflow node attributes** — DOT node attributes (`required_skills`) or referenced YAML policy documents (per control-points.md §6.5 and §6.11). The handler resolves the declared skill set against available skill packages (`skill_search_paths[]` in LaunchSpec per §4.2).
- **Provisioning per agent-type shape.** Skill installation is agent-type-specific: file drops into a known directory for Claude Code skills, CLI binaries added to the agent's PATH, MCP registrations, reference-doc bundles, etc. The handler adapter for each agent type knows its shape.
- **Obligations:**
  - (a) The handler MUST fail-launch if a required skill cannot be provisioned. It emits a typed `ErrSkillProvisioningFailed` error and a corresponding `agent_failed` event; the run does not proceed.
  - (b) The handler MUST emit a `skills_provisioned` event (event-model.md §3.2) naming what was installed, the source path for each skill, and the skill package versions where available.
  - (c) The skill set is part of LaunchSpec (§4.2) so the request is auditable and reproducible.
- **Mechanism-tagged.** Resolution of declared skills to available skill packages is deterministic; no cognition participates.
- Motivating instance: the Beads-CLI skill (beads-integration.md §10.9) must be available to every agent that touches Beads.
- The skill-declaration surface (per control-points.md §6.11) is read by handlers from the same registry via S02 lookups (see control-points.md §6.1b).

**4.12 Handler as modularity boundary.**
- States the architectural load-bearing role of the handler contract: it IS the boundary between the deterministic daemon (workflow state, routing, dispatch — per process-lifecycle.md §8.6) and the execution shape (currently ntm + tmux + subprocess; potentially future alternatives).
- Normative: the handler contract remains stable across execution-shape evolution. If harmonik later replaces ntm with a custom tmux + agent-profile library, or adds a cloud-execution shape, the new execution shape MUST re-implement the same handler contract without altering its cross-subsystem surface.
- Any proposal that would couple the daemon to a specific execution shape (e.g., importing ntm-specific types into the daemon's routing logic) fails this boundary. Cross-reference: process-lifecycle.md §8.5 for the agent-subprocess contract; process-lifecycle.md §8.7 for ntm's specifically-bounded role.
- The concurrency split (daemon-owned watcher, S04-owned adapter, per §4.3) is the load-bearing shape: a new execution shape re-implements the adapter, not the watcher.
- Mechanism-tagged.

### Dependencies
- **Depends on:** architecture.md (§1.1 classification, §1.4 subsystem envelope, §1.8 centralized controller), execution-model.md (outcome type), event-model.md (event schema for handler lifecycle, `skills_provisioned` event), process-lifecycle.md (daemon-vs-subprocess relationship per §8.5, socket path per §8.1).
- **Co-references:** control-points.md §6.11 — handler-contract.md §4.11 *consumes* the skill-declaration surface defined in control-points.md, but does not depend on control-points' internal types. Treated as a co-dependency resolved directionally: control-points defines WHERE declarations live on a node; handler-contract consumes the declared set at launch. See the Intra-foundation dependency graph §"Co-dependency resolution rules."
- **Depended on by:** subsystem spec for S04 (Agent Runner; cites §4.3 goroutine pin); subsystem spec for S01 (Orchestrator Core; owns watcher per §4.3); subsystem spec for S05 (Hook System); control-points.md (control points consume handler outcomes and agent-lifecycle events); beads-integration.md (skill injection delivers the Beads-CLI skill); reconciliation.md (investigator agent is launched via handler); workspace-model.md (handler launches bind to workspace leases).

---

## Component 5: `specs/workspace-model.md`

### Purpose
Normative worktree-as-data-type, lifecycle, session-log aggregation, merge semantics (the deliberate divergence from Kilroy), and workspace/operator-control interaction.

### Requirements

**5.1 `workspace` type — SC-1 density.**
- Fields: `workspace_id` (stable across merges and restarts), `repository` (path or URL), `parent_commit` (the commit this workspace was branched from), `branch_name` (canonical naming convention per §5.8), `path` (absolute), `run_id` (the run this workspace is leased to — see lease rule below), `state` (see 5.2), `metadata` (adze environment fingerprint, creation timestamp, etc.).
- **Lease rule (normative).** The worktree is leased by the workflow **RUN**, NOT by individual agents. Multiple agents may operate sequentially in the same worktree across the run's lifetime — the implementer agent, a reviewer agent, and a merge agent can all occupy the same worktree as the run traverses its graph. This is the concrete realization of the centralized-controller principle (architecture.md §1.8) at the workspace layer. Any design proposal that would give an agent exclusive ownership of a worktree for the duration of an agent-level session (as opposed to the run-level lease) fails this rule.
- **Tagging per SC-10:** the `workspace` type and all workspace operations (create, lease, merge, discard) carry the four-axis + mechanism/cognition tags. Workspace operations are mechanism-tagged by default; merge-conflict-resolution nodes are cognition-tagged when resolved by a reviewer agent and mechanism-tagged when resolved by three-way merge.

**5.2 Workspace state machine.**
- States: `created` → `setup` (adze in progress) → `ready` → `leased` (an agent is using it) → (return to `ready` if the lease releases) → `merge-pending` → `merged` or `conflict-resolving` → `merged` or `discarded`.
- Transition events emitted to event bus per event-model.
- Each state: required preconditions, allowed actions by external agents, allowed transitions.

**5.3 Session-log aggregation strategy.**
- The **canonical session-log directory** is per-workspace, not global. Each workspace has `${workspace_path}/.harmonik/sessions/` where session logs for agents running in that workspace land.
- Memory-layer indexing (CASS) follows these directories via a configured set of roots; on workspace `merged`, the directory is copied to a post-merge archive path (or left in the merged branch — spec decides).
- The `session_log_location` event emitted by the handler on startup points to `${workspace_path}/.harmonik/sessions/${session_id}/`.
- **Bead-ID metadata.** When a session is launched for a bead-bound run, the session-log metadata carries the `bead_id` (per beads-integration.md §10.6; stamped by S06 per workspace-model.md §5.3a). CASS indexing uses this metadata to join session logs to the Beads task ledger for post-hoc analysis.
- Pi-specific log paths (Q-P7) resolve by specifying Pi's log location relative to the workspace.
- Pipeline ownership specified in §5.3a.

**5.3a Session-log pipeline — end-to-end ownership.**
- The session-log pipeline crosses three subsystems; this subsection names each owner.
- **S04 (Agent Runner) — emission.** Each handler writes its session log to `${workspace_path}/.harmonik/sessions/${session_id}/session.log` (format per-handler — Claude Code's log format, Pi's log format). S04 emits the `session_log_location` event (per event-model.md §3.2) on handler startup with payload `{session_id, run_id, node_id, handler_type, log_path, log_format, bead_id?}`. S04 is the authoritative writer.
- **S06 (Workspace Manager) — path + metadata stamping.** S06 owns the session-log directory structure under the workspace. At handler launch, S06 creates `${workspace_path}/.harmonik/sessions/${session_id}/` and writes a metadata sidecar file `${workspace_path}/.harmonik/sessions/${session_id}/harmonik.meta.json` carrying `{run_id, node_id, handler_type, workflow_id, bead_id?, launched_at}`. The metadata file is the authoritative join key for CASS indexing.
- **S08 (Memory Layer / CASS) — ingestion.** S08 follows the configured session-log roots (per §5.3: `${workspace_path}/.harmonik/sessions/`) using filesystem watchers OR a pull-based re-scan cadence. On detecting a new session directory, S08 reads the metadata sidecar AND the session log, indexing the conversation + metadata into CASS. S08 is the authoritative reader.
- **Payload schema for `session_log_location`.** The event payload fields are named above; S04 is the owner of the emission contract and reference schema lives in event-model.md §3.2.
- **Metadata stamping timing.** S06 writes the metadata sidecar BEFORE emitting `workspace_leased` (workspace is ready for S04's handler). S04's subsequent `agent_started` event confirms the handler is attached; no further stamping is required from S04.
- **Post-merge archival.** On `workspace_merged`, S06 either (a) preserves the sessions directory in the merged branch (audit-retention default), OR (b) moves the directory to a post-merge archive path (operator-configured). Spec-draft pins the default and the configuration knob.
- **Mechanism-tagged.** Path construction, metadata writes, and ingestion are all deterministic.

**5.4 Merge semantics — the Gas Town divergence from Kilroy.**
- Parallel branches (e.g., two builder agents working on different areas) produce independent workspaces, each with its own branch.
- Convergence nodes in the workflow graph trigger a merge operation: one branch is merged into the other, then both are marked `merged` or the losing branch is `discarded`.
- **The merge agent (when the workflow uses a distinct merge node) runs in the SAME worktree the implementer used** — NOT a new worktree. The lease-by-run rule (§5.1) means the merge is another node executed inside the run's already-leased worktree. Any design suggestion that the merge step creates a new workspace is incorrect and contradicts architecture.md §1.8.
- Merge conflicts: the agent that performs the merge is responsible for resolving. Resolution role (Q-P3): for MVH, the original implementer handles conflicts in their own branch; if unresolvable, the workflow emits a `merge_conflict_escalation` event for human review.
- The merge is NOT fast-forward-only. Real merges are supported; conflict markers are real.
- Merge outcome: emits `workspace_merged` event with the merged commit hash and a reference to the surviving branch.

**5.5 Workspace/operator-control interaction.**
- On operator `pause`: workspaces in state `leased` complete their lease; workspaces in state `created` or `setup` may be deferred to resumption.
- On operator `stop`: in-flight leases complete (graceful mode) or are forcibly killed (immediate mode). The workspace state reflects the interruption.
- On operator `upgrade`: between-task invariant — no workspace is mid-lease at upgrade time (operator-control ensures this).

**5.6 Cleanup policy.**
- `merged` workspaces are retained for audit for N runs (N defined in operator-nfr.md). After N, they are archived (branch deleted, worktree directory compressed) or discarded per retention policy.
- `discarded` workspaces are retained for M runs for post-mortem review, then deleted.
- Cleanup is its own workflow (runs against harmonik's own repo).

**5.7 Non-git artifacts (Q-P3 extension).**
- Out of scope for MVH: workspaces can only manage git-tracked content. If a workflow requires databases, cloud resources, or non-git state, that's handled by workflow-specific logic outside the workspace model. Foundation does not specify non-git workspace support.

**5.8 Branching model.**
- **Three-level branching.**
  - **Node commits** land on a **task branch**. Every durable state transition in the run emits one checkpoint commit to the task branch (per execution-model.md §2.1a checkpoint cadence).
  - **Task branch** — one per task/bead. Branch name convention: `harmonik/{run_id}/{node_id}` or a configurable equivalent (the convention is declared in policy and must be stable across a harmonik version per operator-nfr.md §7.4 compat contract). Multiple node commits accumulate on the task branch across the lifetime of the run.
  - **Integration branch** — when multiple tasks compose into a larger unit of work (a Beads parent-bead with children), each task branch squash-merges onto the parent's integration branch as one commit per task. The integration branch name is derived from the parent bead's ID.
  - **Main** — harmonik does NOT dictate merge style from integration to main. The developer (or policy layered on top) decides: squash, rebase, or keep-separate. Harmonik's contract ends at "integration branch holds one commit per task."
- **Small-scope collapse.** A single-task change with no parent bead (or where the parent-child relationship is not used) skips the integration branch: the task branch squash-merges directly to main as one commit.
- **Parent-child relationship.** If a bead has a parent bead (Beads typed parent-child edge per beads-integration.md §10.3), the child's task branch lands on the parent's integration branch. Otherwise the task branch lands direct-to-main. Mechanism-tagged: the decision is deterministic given the Beads parent-child edge.
- Rationale: matches standard feature-branch developer intuition; keeps main clean (no per-node churn on main); preserves per-node history on the task branch for debugging and audit; gives a natural "unit of value" commit on main per task.

**5.9 Re-run rule.**
- When a bead is re-claimed after a fundamental failure (run crash, unrecoverable error, or investigator `reopen-bead` verdict per reconciliation.md §9.5), the new run gets a **fresh worktree and a fresh branch**. The prior run's branch is referenced in the audit trail (through the bead's history in Beads) but is orphaned for purposes of the new attempt.
- **Intra-run loops are different.** Intra-run loops (workflow edges routing back to an earlier node) happen within a single run and keep the same worktree. Investigator verdicts that keep the run alive (`resume-here`, `resume-with-context`, `reset-to-checkpoint` per reconciliation.md §9.5) are intra-run rollbacks — they keep the worktree and revert to a checkpoint; they do NOT spawn a new run.
- Mechanism-tagged: the decision between intra-run continuation and fresh-worktree re-run is deterministic based on the investigator's verdict enum.
- Cross-reference: execution-model.md §2.5 distinguishes "bead" vs. "run"; reconciliation.md §9.6 references this rule for re-run semantics.

### Dependencies
- **Depends on:** architecture.md (§1.1 classification — workspace operations are mechanism-tagged, §1.8 centralized controller), execution-model.md (run/state, §2.1a checkpoint cadence), event-model.md (workspace lifecycle events), handler-contract.md (handler launch binds to workspace), beads-integration.md (parent-child edges drive branching), reconciliation.md (re-run vs. intra-run distinction).
- **Depended on by:** subsystem spec for S06 (Workspace Manager); S01 (Orchestrator uses workspace transitions for run state); operator-nfr.md (operator controls interrupt workspace lifecycle); reconciliation.md (workspace-missing is a Cat 3 / Cat 6 detector signal).

---

## Component 6: `specs/control-points.md`

### Purpose
Normative unified primitive for gates, hooks, and transition guards — the realization that these are one thing parameterized differently. Plus policy, role taxonomy, config precedence, and budget enforcement.

### Requirements

**6.1 The `ControlPoint` primitive.**
- One type, three kinds: `ControlPoint.Kind ∈ {Gate, Hook, Guard}`. The three kinds share common fields but have distinct trigger types, evaluator return types, outcome-action enums, and boundary-classification constraints. The unification is in the primitive's shape (one Go struct, one lifecycle, one registration path); the semantics per Kind are explicit and are NOT interchangeable.
- Common fields: `name` (unique), `kind`, `trigger`, `evaluator`, `outcome_action`, plus Kind-specific typed payload (see per-Kind table below).
- The evaluator is boundary-classified: mechanism-tagged evaluators are pure deterministic expressions (condition, schema match, threshold); cognition-tagged evaluators delegate to a model (with specified prompt, input, response schema).

**6.1a Per-Kind semantics table.**

| Kind | Trigger | Evaluator input | Evaluator returns | Outcome-action enum | Boundary-classification rule |
|---|---|---|---|---|---|
| **Gate** | Transition attempt (pre-selection) | Current state, candidate transition, outcome | `{allow, deny}` + optional `reason` | `{allow, deny, escalate-to-human}` | Mechanism OR cognition (allowed to delegate) |
| **Hook** | Event match | Matching event + subscription context | Side-effect descriptor (event emission, state mutation, external action) | `{fire-side-effect, no-op}` (a Hook never halts a run) | Mechanism OR cognition |
| **Guard** | Edge evaluation (during deterministic cascade) | Edge set, current state, outcome | **Reordered edge list** (subset or permutation of input edge set) | `{reorder-edges}` (Guards cannot emit any other action) | Mechanism only (cognition-tagged Guards are forbidden — they would violate ZFC by putting cognition in the selection-logic layer) |

- A Gate is sequential: at most one Gate decides per transition attempt. An `allow` advances to edge cascade; a `deny` fails the transition; an `escalate-to-human` enters a quarantine state awaiting external resolution.
- A Hook is parallel: many Hooks may fire on one event; their side-effects compose. Hooks never block the producer (per consumer taxonomy in event-model.md §3.7).
- A Guard is deterministic: reorders edges but cannot add, remove, or block. Guards run before Gates in the transition cascade (Guards shape the choice set; Gates permit/deny the chosen transition).

**6.1b ControlPoint registry — ownership pin.**
- The **ControlPoint registry** is a single in-process Go map (by `name`) keyed to ControlPoint instances. Its owner is **S02 (Policy Engine)**. Rationale: S02 reads policy YAML (§6.5) and constructs ControlPoint instances from Gate, Hook, and Guard YAML definitions; registration is the direct follow-on.
- **S05 (Hook System) responsibility — Hook dispatch only.** S05 owns the **Hook dispatch loop** (subscribing to events, invoking Hook evaluators in the order specified by §6.3). S05 does NOT own the registry; it consults the registry by looking up Hooks by event-match criteria. Gate and Guard invocations (pre-transition and edge-evaluation paths) are owned by S01 (Orchestrator Core) consulting the same registry.
- **Registration path.** A subsystem that introduces a Gate, Hook, or Guard via its subsystem envelope (§1.4) calls `s02.RegisterControlPoint(cp)` during daemon init. Registration is idempotent-by-name; double-registration with different bodies is a startup-time error.
- **Scope.** The registry is daemon-scoped (process-scoped per §1.4a). No cross-daemon sharing; per-project daemon keeps its own registry.
- **Mechanism-tagged.** Registration and lookup are deterministic.

**6.2 Gate semantics.**
- A gate fires on transition attempt and allows or denies.
- Gate kinds: `goal-gate` (cannot be bypassed — replaces Kilroy's concept at foundation level), `approval-gate` (requires named approver — human or role), `quality-gate` (requires verification node to pass).
- Gates may be attached to nodes (pre-entry, post-exit) or edges (before-selection, after-selection).
- Gate failure: the transition does not proceed; the run remains in the source state; a `gate_denied` event is emitted; retry policy per the failure taxonomy.
- Gate invocation is owned by S01 consulting the §6.1b registry at transition attempts.

**6.3 Hook semantics.**
- A hook fires on event match and executes side effects (state mutation, external action, new event emission).
- Hook lifecycle: `on_agent_started`, `on_agent_output`, `on_agent_completed`, `on_timeout`, `on_review_required`, `on_transition_attempted` (per AlphaGo north-star). Subsystems may declare additional hook types via the subsystem envelope.
- Hooks execute in a defined order when multiple match; ordering is by subscription-declaration order within a subsystem, then by subsystem priority (explicit).
- Hook failures are typed; per-hook failure does not halt hook chain unless declared.
- Dispatch invocation is owned by S05 consulting the §6.1b registry.

**6.4 Transition-guard semantics.**
- A transition guard fires before a transition is selected (during edge evaluation in the deterministic cascade).
- Unlike gates (which allow/deny), transition guards can *reorder* edge candidates. Used for dynamic routing.
- Guards are mechanism-tagged; they cannot be cognition-tagged (that would violate ZFC at the selection-logic layer).
- Guard invocation is owned by S01 consulting the §6.1b registry during edge evaluation.

**6.5 Policy schema — DOT / YAML split.**
- Policies are declarative **YAML** documents. Workflow graphs are **DOT** documents (per execution-model.md §2.1). DOT node/edge attributes REFERENCE YAML policy documents by name; they do NOT embed policy bodies inline.
- **Concrete DOT attributes** that reference YAML policy documents: `policy_ref` (generic policy attachment), `gate_ref` (a specific gate definition), `freedom_profile_ref` (a freedom profile to apply to a state), `budget_ref` (a budget to enforce). Additional attributes (per-node `required_skills` for §6.11, handler references for execution-model.md §2.1) also live on DOT nodes.
- Required sections in a policy YAML document: `metadata` (name, version, author), `roles[]` (role permission schemas — see §6.6), `freedom_profiles[]` (per-state constraint bundles — see §6.7), `gates[]` (gate definitions referenceable by DOT `gate_ref`), `budgets[]` (budget declarations per role or per workflow — see §6.9).
- Policy schema version is per-policy and per-document; compatibility is N-1 per the same rules as event-model versioning (per operator-nfr.md §7.6).

**6.6 Role permissions — concrete.**
- **Scope authority:** role names, semantics, and MVH-required vs declared-but-deferred distinctions are defined in architecture.md §1.6 (not here). This spec defines permission SCHEMAS per role; all citations of role names cite architecture.md §1.6.
- For each MVH-required role (Planner, Builder, Reviewer per architecture §1.6): the normative default permission set (read-only vs. write to specific directories, tools allowed, **skills included in the default set**, agents that may invoke this role, hooks that may modify a role's behavior).
- For each declared-but-deferred role (Researcher, Verifier, Scheduler, Governor): a permission schema shell is declared with `allowed=[]` defaults. Shell declarations are activation-time-filled; deferred roles may not be activated in MVH without foundation amendment.
- **Default skills.** Every MVH-required role's default permission set includes the **Beads-CLI skill** as a first-class default (per beads-integration.md §10.9). This is one concrete instance of the general skill-injection pattern in §6.11 and handler-contract.md §4.11: roles may declare default skill sets, nodes may declare additional `required_skills`, and the handler ensures the union is provisioned at agent launch.

**6.7 Freedom profile.**
- A freedom profile is a per-state constraint bundle specifying: tool whitelist, directory write access, LLM model tier (if any), token budget, wall-clock budget, max iterations.
- Freedom profiles are applied additively as agents traverse states; the *tightest* applicable profile wins.

**6.8 Config loading precedence — resolves Q-P6.**
- Order (highest precedence first): (1) runtime override (set by operator before launch), (2) operator-policy file (persistent operator preferences), (3) workflow definition (per-workflow overrides), (4) default configuration (shipped with harmonik).
- Resolution: deep-merge with higher-precedence values replacing lower-precedence.
- A change to a higher-precedence layer takes effect on the next operator pause (no mid-run config reloads).
- **Spec-draft obligation — config inventory.** Spec-draft produces a normative config inventory: every "operator-configurable" knob referenced across components (§3.4 timer-flush cadence, §6.9 budget warning threshold, §7.7 drain timeout, §7.8 RTO thresholds, §8.4 queue-empty re-query cadence, §9.3 Cat 0 pre-check retry cadence, reconciliation per-Cat budgets per §9.4a) is enumerated with its precedence layer (per §6.8), default value, allowed range, and change-takes-effect semantics.

**6.9 Budget enforcement point.**
- Budget declared in policy (per role, per run, per state).
- Budget enforced by the agent runner AT DISPATCH (pre-exhaustion): if the pending dispatch would exceed remaining budget, emit `budget_exhausted` event and deny the dispatch.
- Warning threshold: at 80% of budget (configurable), emit `budget_warning` event; continue. The threshold check uses **live in-handler counters** (the handler tracks accrual against remaining budget in real time, per its own tick cadence).
- Budget accrual: every agent output chunk emits a `budget_accrual` event within the same handler tick that produces the chunk (bounded by the chunk-emission cadence of the underlying handler). This per-chunk granularity is explicitly retained for MVH per event-model.md §3.2a; a future log-level filter may suppress chunk events at consumer boundaries without changing the emission contract.
- Boundary-hit (`budget_exhausted`, `budget_warning`) emission is a typed event; the counter state is internal to the handler and exposed only through events.
- Reconciliation workflows additionally carry a mandatory wall-clock budget per reconciliation.md §9.4a; the daemon enforces this as an outer-most bound.

**6.10 Naming-conflict resolution.**
- "Gate" = ControlPoint.Kind=Gate (this spec).
- "Hook" = ControlPoint.Kind=Hook (this spec).
- "Policy" = the declarative YAML document (6.5); not access control per se (that's "role permissions"); not configuration (that's "runtime config or operator-policy file" in 6.8); not tunable-parameters (that's "freedom profile" in 6.7). Policy is the term for the document; role permissions, freedom profiles, and config are its sub-concepts.

**6.11 Skill-declaration surface.**
- A **node** in a DOT workflow graph MAY declare `required_skills` as an attribute. Forms accepted: (a) a comma-separated list of skill names in the DOT attribute, (b) a YAML policy reference via `policy_ref` that names a skill set.
- **Policies** MAY declare default skill sets per role (per §6.6); a node's effective skill set is the union of its node-level `required_skills` and the default skill set of its assigned role.
- **The handler consumes** the declared skill set per handler-contract.md §4.11: it resolves declared skills against available skill packages, provisions them into the agent process shape, emits the `skills_provisioned` event (per event-model.md §3.2), and fail-launches if resolution fails.
- **Mechanism-tagged.** Declaration + resolution is deterministic; no cognition participates.
- Motivating instance: the **Beads-CLI skill** (per beads-integration.md §10.9) is a common declaration. Any agent that needs to query Beads or update bead status needs the skill; declaring it on the node (or in the role's default set per §6.6) triggers the handler's provisioning obligation.

### Dependencies
- **Depends on:** architecture.md (§1.1 classification, §1.6 role taxonomy, §1.9 three-artifact separation), execution-model.md (runs, states, transitions, the outcome spine, §2.1 DOT workflow representation), event-model.md (events emitted by control points and events that Hooks subscribe to; `guard_denied` event, `skills_provisioned` event), handler-contract.md (Hooks fire on agent-lifecycle events defined by handler-contract §4.1 and emitted per §4.2; skill-injection obligation per §4.11).
- **Depended on by:** subsystem spec for S02 (Policy Engine; owns the §6.1b registry); subsystem spec for S05 (Hook System; owns Hook dispatch); subsystem spec for S01 (Orchestrator Core; invokes Gates and Guards via the §6.1b registry); every subsystem that declares control points via the subsystem envelope; operator-nfr.md (budgets are control-point instances); beads-integration.md (Beads-CLI skill is a default-role skill); reconciliation.md (gates determine escalation paths for Cat 6 verdicts).

---

## Component 7: `specs/operator-nfr.md`

### Purpose
Normative cross-cutting non-functional requirements and operator-control semantics. This spec's contents are the invariants every subsystem must honor regardless of its internal design.

### Requirements

**7.1 Observability protocol.**
- Every subsystem emits events per event-model.md §3.2 (typed events).
- Every subsystem emits structured logs per event-model.md §3.8.
- Every subsystem exposes a **health check** interface: a function (or endpoint in implementation) returning `health_status ∈ {OK, degraded, failed}` plus an optional reason string. The orchestrator aggregates subsystem health into a harmonik-wide health status.
- Every subsystem emits **liveness** signals: a heartbeat event on a defined cadence. Missing heartbeat beyond tolerance triggers a degraded classification.
- Every subsystem records **audit** records as a subset of traces: traces where `actor_role` is in a privileged role and the `chosen_action` affected policy, role permissions, or budget.
- Operator-observable exit codes: defined per operator command; non-zero exit codes are structured (category → code mapping specified).
- **Tagging per SC-10:** every observability operation (health-check, heartbeat, metric emission, log emission, audit record creation) carries the four-axis + mechanism/cognition tags. All observability operations are mechanism-tagged by definition; any operation requiring cognition to produce the observability signal belongs in a separate verification node, not in the observability protocol.
- **Spec-draft obligation — exit-code and failure-mode catalogs.** Spec-draft produces (a) a normative exit-code taxonomy mapping every non-zero exit code to a failure category, and (b) a cross-reference to the startup failure-mode catalog in process-lifecycle.md §8.2 + the reconciliation Cat 0 detector in reconciliation.md §9.3.

**7.2 Security posture.**
- Secrets lifecycle: injected at handler launch (per handler-contract.md §4.7), never in event log, never in session log without redaction.
- Command-execution sandbox: agents execute within workspace directory; escape attempts (symlinks outside workspace, path traversal, git hooks from untrusted sources) are prevented. Specific enforcement per S04 and S06; foundation states the invariant.
- Network egress: declared per policy; a policy may whitelist domains for agent access.
- Prompt-injection defense: input sanitization responsibility is on handlers; the foundation contract states that handlers must NOT let user-provided content in the input workspace alter the agent's system-prompt instructions.
- **Skill-injection policy enforcement.** Skills provisioned per handler-contract.md §4.11 MUST honor the network egress policy (a provisioned skill that would require egress to a non-whitelisted domain fails provisioning) and the command-execution sandbox (a skill that would require filesystem access outside the workspace fails provisioning). The `skills_provisioned` event (event-model.md §3.2) records the skill set actually installed; audit reviews this against the policy's allowed set.
- Trust model for pause-to-upgrade: commit-hash check (the to-be-installed binary's source-commit hash must match the operator-supplied expected hash). Full binary signing is deferred to post-MVH (Q-R4).

**7.3 Operator-control semantics — the between-task invariant.**
- Defines "task" in the operator sense: one complete run of a workflow, from `run_started` to `run_completed` or `run_failed`. Terminology note: the operator-facing word "task" = the execution-model's `run`. Foundation resolves this naming: operator surfaces use "task" for user-friendliness; specs use `run` for precision.
- The between-task invariant: pause and upgrade complete in-flight runs before taking effect. Only `stop --immediate` aborts in-flight runs.
- **Reconciliation carve-out.** Pause MUST NOT interrupt reconciliation workflows (per reconciliation.md §9.1). Reconciliation workflows are idempotent and short; interrupting them risks worse state than letting them complete. The daemon's status progression is `starting` → `reconciling` → `ready`; the between-task invariant applies only once the daemon reaches `ready`. An operator pause issued during `reconciling` is queued and applied when the reconciliation batch completes (boundary event: all reconciliation runs either resume into normal flow or produce a verdict).
- State machine for operator control:
  - States: `running` → `pausing` (draining in-flight) → `paused` → `resuming` → `running`.
  - `stop` transitions to `stopped` (terminal); recoverable via `start`.
  - `upgrade` transitions `running` → `pausing` → `paused` → `upgrading` → `running` (new binary).
  - `improvement-pause` (the renamed S09 cycle) is a subtype of pause with a scheduled or triggered onset; resumes automatically when improvement loop completes.
- Events per state transition: `operator_pausing`, `operator_paused`, `operator_resuming`, `operator_stopped`, `operator_upgrading`.

**7.4 Queue-format compatibility contract.**
- **Beads (SQLite via `br` CLI) IS the queue.** The operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads. "Queue" and "task ledger" in harmonik vocabulary refer to the same thing (beads-integration.md §10.1–10.3).
- **Compatibility** = (a) **Beads schema compat** (the `Dicklesworthstone/beads_rust` SQLite schema — managed upstream) AND (b) **harmonik's overlay schema compat** (the bead-ID trailers in checkpoint commits per execution-model.md §2.1, the bead-ID references in events per event-model.md §3.2, the session-log bead-ID metadata per workspace-model.md §5.3). Both must be N-1 readable.
- **Pre-1.0 Beads risk.** Beads is pre-1.0 and may make breaking changes. Harmonik mitigates by (i) version-pinning Beads per the external-inputs protocol (problem-space §External inputs), (ii) routing all Beads interactions through a thin `br`-CLI adapter layer (beads-integration.md §10.8), so a Beads breaking change produces one localized adapter update rather than scattered code changes. Harmonik **absorbs breakage** rather than forking Beads.
- Queue format (i.e., Beads SQLite schema) version is checked on daemon startup; unsupported versions cause startup failure with a specific error code directing the operator to the migration release.

**7.5 Checkpoint-format stability.**
- (Referenced from execution-model.md §2.1.) Same N-1 compatibility contract as queue format.
- **Spec-draft obligation — `harmonik upgrade` contract.** Spec-draft produces a normative `harmonik upgrade` contract specifying: binary-source mechanism (repo path / hash-supply flag), operator-supplied expected commit hash check procedure, drain-vs-reconciliation interaction (what `upgrade` does if reconciliation workflows are in-flight per §7.3), cross-version state contract (what upgrade does if the new binary's schema-version is N-1, N, or N+1 vs the on-disk state), and socket/client-CLI retry behavior during exec-replacement.

**7.6 Event schema compatibility.**
- (Referenced from event-model.md §3.5.) Same N-1 compatibility contract.

**7.7 Graceful shutdown ordering.**
- On `stop --graceful` or SIGTERM:
  1. Orchestrator stops pulling new tasks from queue.
  2. In-flight runs proceed to next checkpoint, then suspend.
  3. Agent runners wait for handler subprocesses to complete or timeout.
  4. Event bus flushes pending events (fsync).
  5. Memory layer flushes indexing.
  6. Workspace manager unlocks leased workspaces (cleans up incomplete adze setups).
  7. Orchestrator exits with code 0 if clean, code 1 if timeout-escalated.
- On `stop --immediate` or SIGKILL: skip steps 2–3; in-flight state is recoverable via checkpoint but in-flight agent subprocesses are killed.
- Drain timeout: specified; operator-configurable.

**7.8 Restart RTO.**
- **Restart reconstruction path.** Daemon restart walks the git checkpoint trail and queries Beads (via `br`) to build the in-memory model; the JSONL event log is NOT replayed for state reconstruction (per event-model.md §3.4 and §3.6, and per locked decision #12 — no DTW). Reconciliation workflows spawn for in-flight runs per reconciliation.md §9.2.
- Reaches the pre-restart state within **X seconds**, measured from SIGTERM to the daemon transitioning `reconciling` → `ready`.
- X is set by the research pass using these **explicit criteria**:
  - **Criterion 1 — operator expectation.** MVH assumes single-operator, single-instance deployment. Target: X ≤ 30 seconds for 95th percentile under nominal conditions (≤ a few hundred open beads, ≤ a few dozen in-flight runs).
  - **Criterion 2 — reconstruction complexity.** Restart time is proportional to (a) git-log walk depth since the oldest open-bead's first checkpoint, and (b) Beads query latency for ready + in-flight bead sets. Research pass measures git-walk and Beads-query rates on representative hardware at MVH scale. JSONL event count is NOT a restart-time factor (it is not read on restart).
  - **Criterion 3 — hard ceiling.** 300 seconds. Beyond this, operator is notified and restart is escalated (the daemon enters a degraded state that reports `reconciling` with progress markers; operator may intervene). Criterion 3 is NON-NEGOTIABLE; criterion 1 may be relaxed with reason if measurements show 30 seconds is unachievable at MVH scale.
  - **Reconciliation-workflow dispatch time is separate from the RTO target above.** The RTO measures time to `ready` (the deterministic reconstruction + reconciliation-workflow dispatch). Each reconciliation workflow's own execution (investigator-agent LLM calls per reconciliation.md §9.4) is bounded by that workflow's own policy, not by this RTO.
- Measured from SIGTERM to the daemon's `ready` status event emission.

**7.9 Resource budgets cross-subsystem.**
- Declared in policy (per control-points.md §6.9).
- Enforced at dispatch (agent runner).
- Attributed in observability (per run, per role, aggregated to per-workflow and per-harmonik-instance).
- Cost attribution per subsystem is OUT of scope for MVH (no multi-tenancy).

**7.10 Aspects explicitly deferred post-MVH.**
- Distributed tracing protocol (tracing across multiple harmonik instances). The per-project daemon model (process-lifecycle.md §8.1) makes multi-instance tracing an OS-process-isolation concern, not a harmonik-code concern — each daemon is a separate process with its own event log and its own state. Cross-daemon correlation (if ever needed) is an external-tooling layer, not a foundation spec.
- Metrics exposition format (Prometheus/OpenTelemetry wire format).
- Binary signing (commit-hash check is MVH).
- **Multi-tenancy / per-tenant cost attribution.** Per-project daemon isolation means multi-tenancy at the OS-process layer reduces to "run more daemons, one per project." No per-tenant cost attribution in the code; operator aggregates across daemon instances externally.

    **Acknowledged concerns for post-MVH.** Per-project daemon isolation does NOT address (and is explicitly deferred, not solved): (a) **shared operator LLM budgets** — the Anthropic quota is a per-account limit; running N daemons does not create N quotas, so a machine-level budget coordinator will be needed post-MVH; (b) **shared operator identity and auth** — `harmonik attach` across N daemons is the same human with the same skills and the same `br` binary; global install conflicts and access controls are shared concerns; (c) **shared skill registries** — skills are typically installed machine-wide (Claude Code skills under `~/.claude/skills`), so a provisioning failure in one project is a global failure surface. These are real multi-tenancy concerns that per-daemon isolation does not resolve; they are deferred to post-MVH under the caveat that "deferred ≠ dismissed."

    **Spec-draft obligation — multi-daemon commands.** Per-project daemon isolation does not eliminate the operator need for machine-level coordination. Spec-draft produces normative definitions for: `harmonik list` (list running daemons machine-wide), stop/pause/attach flag-based daemon identification (`--socket`, `--cwd`, or `--daemon-id`), and a machine-level resource budget mechanism (cross-daemon agent-subprocess count ceiling enforced by a shared lock or a machine-level coordinator process). These are operator-observable commands; their obligation is named here to ensure spec-draft does not silently defer them under the "OS-process-isolation concern" framing.

    The multi-daemon command obligation is the minimum operator-visible concession foundation makes in MVH; deeper multi-tenancy (shared budgets, shared skill resolution) is post-MVH.
- Observability overhead budget.
- Multi-repo workflow support.

### Dependencies
- **Depends on:** architecture.md (§1.1 classification, §1.7 harness invariants, §1.8 centralized controller), event-model.md (event and log schema), execution-model.md (run/state for operator semantics, §2.6 three-store cross-reference), control-points.md (budget declared in policy, enforced via the control-point primitive), handler-contract.md (secrets delivery, handler exit codes, skill-injection policy enforcement), process-lifecycle.md (daemon startup and command surface, per-project scope), reconciliation.md (restart RTO includes reconciliation dispatch), beads-integration.md (Beads is the queue per §7.4).
- **Depended on by:** all subsystem specs inherit these requirements; the operator-CLI-surface spec (separate work) implements §7.3–§7.4 as CLI commands.

---

## Component 8: `specs/process-lifecycle.md`

### Purpose
Normative contract for harmonik's process lifecycle. Specifies daemon scope, startup and shutdown sequences, socket and pidfile locations, the agent-subprocess model, the daemon-vs-orchestrator-agent distinction, and the separation between the headless daemon and any attach UI. This component names cross-cutting content that was previously implicit — making it normative prevents drift between subsystems about who owns the process story.

### Requirements

**8.1 Daemon scope — per-project.**
- Each project (a git repo containing a `.harmonik/` directory) runs its own daemon. Multiple projects on the same machine run multiple independent daemons.
- Per-project files:
  - Socket: `.harmonik/daemon.sock` — the local Unix socket the daemon listens on. Subprocesses (handlers, CLI commands) connect here.
  - Pidfile: `.harmonik/daemon.pid` — the running daemon's PID; used by CLI commands to detect whether a daemon is already running and to signal it.
  - Event log: `${event_log_dir}/events.jsonl` and `${event_log_dir}/dead-letters.jsonl` per event-model.md §3.4 and §3.7.
- Mechanism-tagged: daemon-per-project isolation is a structural rule, not a cognitive one.

**8.2 Startup sequence.**
- On `harmonik daemon` (or its implicit launch via the `harmonik runner` wrapper per §8.3):
  1. Daemon process starts; daemon takes the pidfile lock. If another daemon is already running for this project, the new process exits with a specific error code.
  1a. **Orphan sweep.** Before reconciliation dispatches, the daemon enumerates residual resources from any prior daemon instance and cleans them up:
    - **Tmux sessions.** List tmux sessions matching the project's harmonik naming convention (e.g., prefix `harmonik-<project-hash>-`). Any session the new daemon does not have in in-memory tracking (the daemon just started; it has no tracking yet — so all matching sessions are orphans by definition) is killed via `tmux kill-session`. The kill is followed by a short wait (bounded; e.g., ≤2 seconds) for the underlying processes to exit.
    - **Worktree locks.** Check each worktree under the project's configured worktree root for lock files (`.harmonik/lease.lock` or equivalent per workspace-model.md §5.1 implementation detail). Locks older than the current daemon's start time are stale; the daemon removes them before any reconciliation reads workspace state.
    - **Subprocess cleanup.** Any process whose parent pid is 1 (re-parented to init after the old daemon died) and whose binary path matches a handler binary under the project's daemon-expected launch path is killed via SIGTERM then SIGKILL (bounded timeout between them).
    - The sweep also enumerates `.harmonik/beads-intents/` for stale intent files; entries older than the current daemon's start time trigger a Cat 3a detector invocation per §9.3.
    - The sweep emits a `daemon_orphan_sweep_completed` event (declared via event-model.md §3.2 amendment) naming the counts of tmux sessions killed, locks cleared, subprocesses killed.
    - **Mechanism-tagged.** Sweep is deterministic given the filesystem + process state; no cognition participates.
    - **Invariant.** After step 1a completes, no harmonik-owned process from a prior daemon instance is alive and no harmonik-owned worktree is locked by a prior-instance lease. Step 2 (git walk) proceeds against a quiescent filesystem.
  2. Daemon walks the git log for the project's repo, collecting all checkpoint commits (identified by the `Harmonik-Run-ID` trailer per execution-model.md §2.1).
  3. Daemon queries Beads (via `br`) for all `open` and `in_progress` beads.
  4. Daemon builds an in-memory model of: completions (beads with corresponding merge-to-main commits), open beads (pending), in-flight beads (in Beads `in_progress` status with the most-recent checkpoint commit telling last durable state).
  5. Before classification, §9.3 Cat 0 pre-check runs; if any prerequisite fails, the daemon enters `degraded` status and defers classification. Otherwise, the daemon follows the §9.2a action-mapping: auto-resolvable categories (Cat 0, 1, 3a, 3b, 3c, 4, 5, 6b) resolve inline; investigator-required categories (Cat 2, 3 generic, 6a) dispatch reconciliation workflows. Classification of a reconciliation workflow with a verdict commit but no verdict-executed commit is Cat 3b per §9.2a (F-C3-3).
  6. Daemon status transitions `starting` → `reconciling` → `ready`; a Cat 0 condition inserts a `degraded` state between `starting` and `reconciling` that persists until prerequisites clear. The `ready` transition emits a status event.
- The entire startup sequence (steps 1–5) is deterministic. Investigator-workflow execution (triggered by step 5) has its own per-workflow duration and runs in parallel with the daemon's `ready` state; see operator-nfr.md §7.8 for the RTO definition.
- **Spec-draft obligation.** Spec-draft produces a normative **startup failure-mode catalog** enumerating every prerequisite failure (git bad state categories, Beads SQLite states, schema-version-mismatch cases, stale-pidfile race resolution, filesystem-unwritable cases, disk-full during checkpoint commit) with: failure-detection rule, exit code (per operator-nfr.md §7.1), operator remediation procedure, and per-failure event emission. The catalog is consumed by §9.3 Cat 0 pre-check and by the operator surface commands.

**8.3 Command surface.**
- **`harmonik daemon`** — start the daemon headless. Blocks until signaled to stop; suitable for invocation under a process supervisor.
- **`harmonik attach`** — open an observability TUI showing enqueued / in-progress / done / reconciliation state. Multiple simultaneous attaches are supported; detaching does NOT kill the daemon.
- **`harmonik runner`** — convenience wrapper for solo-dev ergonomics: starts the daemon (if not running), opens a tmux session showing all agent processes (per the ntm inspectability requirement — problem-space locked decision #4), and optionally spawns an orchestrator-agent session. A `runner` session is sugar on top of the headless daemon + attach, not a distinct execution mode.
- **`harmonik enqueue`**, **`harmonik status`**, **`harmonik pause`**, **`harmonik stop`**, **`harmonik upgrade`** — operator commands that communicate with the running daemon via the local socket (§8.1). `harmonik status` must report the current infrastructure-prereq status (Cat 0 per reconciliation.md §9.3) per the F-C5-6 obligation-naming.
- Note: the `harmonik upgrade` contract is specified in operator-nfr.md §7.5 (spec-draft obligation).
- Mechanism-tagged: command dispatch is deterministic CLI.

**8.4 Queue-empty behavior.**
- When all beads are closed or deferred and nothing is in-flight, the daemon sleeps (does not consume CPU) and waits for a subsequent `harmonik enqueue` or for external changes to the Beads store (periodically re-queried per a configurable cadence).
- The daemon does NOT exit on queue-empty. Exit occurs only on explicit `harmonik stop`, on an operator upgrade transition (`running` → `upgrading`), or on crash.

**8.5 Agent-subprocess contract.**
- The daemon spawns agent processes as **children** of the daemon (per the current execution shape: via ntm or a custom tmux + agent-profile library — the implementation choice is a detail; handler-contract.md §4.12 is the stable boundary).
- Agent communicates back to the daemon through the local socket at `.harmonik/daemon.sock` (§8.1). Operator-exposed agent commands (`harmonik claim-next`, `harmonik emit-outcome`, Beads-CLI invocations delivered via the skill — per beads-integration.md §10.9) route daemon-ward.
- Agent-subprocess failure (crash, hang, policy violation) is observed by the daemon per handler-contract.md §4.6 (error propagation) and produces typed events (`agent_failed`, etc.).
- The daemon-side watcher goroutine per handler-contract.md §4.3 is the S01-owned component; S04's adapter is the per-agent-type logic.
- Note: silent-hang detection is obligated by handler-contract.md §4.6 (spec-draft obligation).
- Mechanism-tagged.

**8.6 Daemon-vs-orchestrator-agent distinction — load-bearing.**
- **The daemon is a deterministic Go binary.** It contains NO LLM logic; it NEVER calls a model. All cognition lives in agent subprocesses or in orchestrator-agent sessions that interact via the CLI.
- **An "orchestrator-agent" (or "coordinator-agent") is a separate Claude Code session** sitting on top of the daemon. It interacts with the daemon through the CLI (`harmonik enqueue`, `harmonik status`, priority triage, backlog grooming). It is NOT a component of the daemon and MUST NOT share process space with the daemon.
- **Do not conflate the two.** Proposals that would embed cognition in the daemon (e.g., "let the daemon decide which bead to claim next using an LLM") violate this distinction and must be rejected.
- **Terminology note.** Several foundation specs (event-model, handler-contract, operator-nfr) pre-date this distinction and use the bare word "orchestrator" to mean the deterministic runtime. In the spec-draft pass, all such usages are to be normalized to "daemon." An "orchestrator-agent" is always hyphenated or prefixed to distinguish it from the daemon.
- Cross-reference: architecture.md §1.8 (centralized-controller principle — deterministic daemon owns all workflow state, agents do only cognitive work); §1.4a for subsystem-as-Go-package definition.
- Mechanism-tagged (for the daemon's portion of the split). The orchestrator-agent is cognition-tagged and lives outside the daemon's mechanism boundary.

**8.7 ntm's role.**
- ntm is the current process/tmux layer. Harmonik consumes from ntm:
  - Agent process spawning (launching a subprocess in a tmux pane).
  - Agent profile knowledge: ready-state detection per agent type, rate-limit signals, clean exit sequences.
  - Lifecycle events: process start, ready, rate-limited, stopped.
  - Account rotation (for agent types that support it).
- Harmonik **ignores** from ntm:
  - ntm's Pipeline System (harmonik's workflow semantics live in DOT graphs, not ntm pipelines).
  - ntm's SwarmPlan format (harmonik uses DOT, not SwarmPlan).
  - ntm's checkpoint/recovery (tmux-session-resume is NOT equivalent to harmonik's workflow-state git checkpoint; the two solve different problems).
  - ntm's file-reservation / Agent Mail features (harmonik uses Gas Town worktree+merge per problem-space locked decision #7; file reservations are explicitly rejected).
- **Boundary statement.** ntm's own docs state "no workflow semantics — that lives elsewhere." Harmonik IS that elsewhere. The handler contract (handler-contract.md §4.12) is where the boundary lives; proposals that cross it (importing ntm pipeline types into the daemon) fail.

**8.8 Crash semantics.**
- Daemon crash (unexpected termination) leaves the pidfile stale; the next `harmonik daemon` invocation detects a stale pidfile (by checking the PID is no longer a live process) and proceeds with startup per §8.2. On restart, §8.2 step 1a sweeps orphans before reconciliation classifies in-flight runs.
- Crash during startup reconciliation: the next restart re-runs §8.2 from step 1. Reconciliation is idempotent; re-running detection rules against the same git + Beads state produces the same classifications.
- Agent-subprocess crash while the daemon is alive is handled per handler-contract.md §4.6 (error propagation across async boundaries) and routes into reconciliation only if the resulting run state is ambiguous.

**8.9 Operator-control integration.**
- The daemon state machine (`starting` → `reconciling` → `ready` → `running` → `pausing` → `paused` → `resuming` → `stopped` / `upgrading`) is defined in operator-nfr.md §7.3. This spec defines §8.2's `starting` → `reconciling` → `ready` prefix; operator-nfr.md owns the `ready` → everything-after states.
- The between-task invariant (operator-nfr.md §7.3) applies only once the daemon reaches `ready`. During `reconciling`, an operator pause is queued and takes effect at the boundary.

### Dependencies
- **Depends on:** architecture.md (§1.4 subsystem envelope, §1.8 centralized controller, §1.9 three-artifact separation), execution-model.md (§2.1 checkpoint trailers carry run IDs used in git walk, §2.6 three-store rule).
- **Depended on by:** handler-contract.md (§4.10 launch + socket model, §4.12 handler as modularity boundary), workspace-model.md (lease-by-run — workspaces live inside per-project daemons), operator-nfr.md (§7.3 operator-control state machine, §7.8 restart RTO), reconciliation.md (§8.2 startup triggers reconciliation; §9.2 pre-classification invariant per §8.2 step 1a), beads-integration.md (`br` CLI invocations route through daemon context).

---

## Component 9: `specs/reconciliation.md`

### Purpose
Normative model for restart reconciliation and store-divergence recovery. This is harmonik's concrete answer to "why we don't need DTW" (locked decision #12 per problem-space §Locked decisions): deterministic reconstruction from git + Beads plus agent-driven investigation for ambiguous cases. Reconciliation itself runs as a harmonik workflow — not a separate subsystem.

### Requirements

**9.1 Reconciliation-as-workflow principle.**
- Reconciliation runs as a normal harmonik workflow: DOT-defined, routed deterministically by the daemon, checkpointed per execution-model.md §2.1a, and event-logged per event-model.md §3.2.
- **No separate reconciliation subsystem.** Each quarantine type (Cat 2, Cat 3, Cat 6 per §9.2) has its own reconciliation workflow in the workflow library per §1.4a (reconciliation is a workflow-library entry, not a subsystem). The same primitives that execute normal work execute reconciliation work.
- Mechanism-tagged at the dispatch layer (the daemon's category detector is deterministic). The investigator-agent node within a reconciliation workflow is cognition-tagged (an LLM reads state and emits a verdict).
- Applicability conditions for reconciliation-vs-DTW are specified in problem-space locked decision #12 (expanded per F-C6-1).

**9.1a Reconciliation checkpoint cadence — bounded to verdict only.**
- Reconciliation workflows are an **explicit exception to the checkpoint-at-every-durable-transition invariant** of execution-model.md §2.1a. A reconciliation workflow emits **exactly one checkpoint commit**: the **verdict commit**, which lands on the investigator-run's task branch and carries the `reconciliation_verdict_emitted` event's payload plus any evidence the investigator produced (per §9.4). Intermediate state transitions (e.g., the investigator-agent reading inputs, calling the model, producing reasoning) are NOT checkpointed.
- **Rationale.** This makes recursion bounded. A daemon crash during reconciliation leaves no mid-investigation durable state; on restart, the outer run whose reconciliation was interrupted is re-classified from its original category (unchanged — git + Beads state has not been mutated by the investigator), and a fresh reconciliation workflow is dispatched. There is no "in-flight investigator" for a detector to classify.
- **Budget interaction.** The investigator still runs under the wall-clock budget of §9.4a. A crash that interrupts the investigator mid-call means the LLM tokens up to that point are lost; the re-dispatched investigator starts fresh.
- **Verdict idempotency.** If the investigator had already emitted its verdict commit at crash time, the restart detector sees the verdict commit and follows the §9.5b verdict-execution discovery rule (F-C2-3) rather than re-spawning the investigator. A verdict commit, once durable, is never re-computed.
- **Scope of the exception.** This exception applies ONLY to reconciliation workflows (DOT workflows that dispatch as a result of a reconciliation-category classification per §9.2). Ordinary workflows continue to obey §2.1a's cadence. A reconciliation workflow is identified by a workflow-metadata tag `workflow_class = reconciliation` set at workflow-library registration.
- **Mechanism-tagged.** The cadence exception is a deterministic rule keyed on workflow class.

**9.1b Reconciliation workflow library — authoring owner.**
- The **reconciliation workflow library** — the concrete DOT workflows and YAML policies that implement Cat 2, Cat 3, Cat 3a, Cat 3b, Cat 3c, and Cat 6a reconciliation per §9.2a — is **owned by S01 (Orchestrator Core)** and ships as part of the S01 package. Rationale: S01 is the subsystem that already executes workflows; shipping the reconciliation workflow set alongside the executor keeps the library and its runtime colocated.
- **What S01 ships.** (a) A DOT workflow per investigator-required category (Cat 2 investigator, Cat 3 generic investigator, Cat 6a investigator); (b) YAML policies for each, including the mandatory wall-clock budget (§9.4a) and the skill-injection requirements for the investigator agents (per control-points.md §6.11, §4.11 — investigators need Beads-CLI and git-inspection skills at minimum); (c) the investigator-agent prompt templates (cognition-tagged — per §4.11 the handler resolves these from the `skill_search_paths[]` at launch).
- **What S01 does NOT ship.** (a) The detectors themselves (those live in the daemon's Go code; per §9.3 detectors are mechanism-tagged functions, not workflow content). (b) The verdict-execution mechanics (those live in the daemon's Go code per §9.5b).
- **Upgrade discipline.** A new reconciliation category (per foundation amendment §1.5) requires both a daemon-code change (detector + action-map entry in §9.2a) AND a workflow-library addition in S01 (for investigator-required categories). These ship together in a harmonik release; split releases are forbidden.
- **Mechanism-tagged.** Library packaging is structural; the workflow contents are themselves a mix of mechanism-tagged gates and cognition-tagged investigator nodes.

**9.2 Category taxonomy — six categories (granularity bias: prefer more detection categories to keep per-category investigator playbooks distinct; default actions per category are specified in §9.2a).**
- Detectors below assume the orphan sweep of process-lifecycle.md §8.2 step 1a has completed; no harmonik-owned orphan process or stale worktree lock remains at classification time.
- **Cat 0 — Infrastructure unavailable.** A prerequisite for classification itself is unreachable: `br` CLI missing, wrong version, or timing out; Beads SQLite locked by a non-harmonik process; git index locked; `.harmonik/` directory unwritable; filesystem full. Rule: **halt reconciliation at the detector layer**; the daemon transitions to `degraded` status (a pre-`ready` state) and waits for infrastructure resolution. No in-flight run is classified until Cat 0 is cleared. Emits `infrastructure_unavailable` event (declared via event-model.md §3.2 amendment) naming the specific prerequisite that failed.
- **Cat 1 — Idempotent rerun.** Interrupted node is safe to re-spawn against the same state. Examples: reviewer agent mid-run, researcher agent, mechanism-tagged non-agentic nodes (lint, test, typecheck). Rule: **auto-resume** by re-spawning. Reconciliation-workflow nodes are never classified here because reconciliation workflows are not checkpointed mid-investigation (per §9.1a); the recursion question does not arise.
- **Cat 2 — Non-idempotent in-flight.** Interrupted node may have been mid-write to the workspace. Examples: builder mid-implementation, merge agent mid-merge. Rule: **quarantine + investigator workflow** with the per-category playbook.
- **Cat 3 — Store disagreement.** git, Beads, and JSONL tell inconsistent stories about the same bead. Examples: Beads reports `closed` but no merge commit exists; bead in `in_progress` but worktree missing; duplicate `transition_id` across commits. Rule: **git wins on completion facts** (per execution-model.md §2.6); investigator workflow explains the divergence and corrects the cache (Beads status update, typically). Sub-detectors (see §9.2a for the default-action map): Cat 3a (torn Beads write), Cat 3b (verdict-unexecuted), Cat 3c (inverse premature-close — merge exists but Beads still `in_progress`). Each has a named detector in §9.3 and a default auto-resolver in §9.2a.
- **Cat 4 — Recoverable known state.** Agent was in a well-defined retry/backoff. Examples: rate-limited pending retry, waiting for a human gate. Rule: **auto-resume with the pending action** (respawn with the prior retry/backoff timer, or re-present the gate).
- **Cat 5 — Clean restart.** Nothing in-flight. Rule: **normal startup** (proceed to `ready` per process-lifecycle.md §8.2). Includes orphaned branches from prior runs of beads that have been re-claimed.
- **Cat 6a — Integrity violation, LLM-triageable.** Structurally wrong data whose cause an investigator agent can reason about and whose resolution might be recommending operator action (e.g., unexpected workspace loss, inconsistent trailer values on a checkpoint, a transition-record sibling file missing from a commit whose trailers are intact). Rule: **investigator workflow + `escalate-to-human` verdict** if the investigator cannot resolve.
- **Cat 6b — Integrity violation, mechanically unrecoverable.** Structurally wrong data with no recovery path an LLM could meaningfully affect (e.g., JSONL corrupted past a byte offset the daemon cannot parse, a checkpoint commit referenced in JSONL that is missing from git's object database, git object database corruption). Rule: **auto-escalate to operator without investigator spawn** — emits `operator_escalation_required` with Cat 6b reason and target-run context; no reconciliation workflow is dispatched. Rationale: spawning an investigator to "explain why git is corrupt" burns LLM tokens on a problem only the operator can fix (restore from backup, rebuild worktree, etc.).

**9.2a Action-mapping layer — default resolution per category.**

The 6-category taxonomy (plus sub-categories from F-C3-1/-2/-3/-4/-5/-6) classifies *what went wrong*. This subsection specifies *what the daemon does by default* for each class. The action-mapping layer is the dispatch contract; the category taxonomy is the detection contract.

| Category | Default action | Investigator spawned? | Typical verdict (if investigator) | Auto-resolver present? |
|---|---|---|---|---|
| Cat 0 (infra unavailable) | halt classification + `degraded` status | No | — | Yes (wait-and-retry) |
| Cat 1 (idempotent rerun) | auto-resume by re-spawning | No | — | Yes (spawn the node) |
| Cat 2 (non-idempotent in-flight) | investigator workflow | Yes | `resume-with-context` / `reset-to-checkpoint` / `reopen-bead` | No (investigator required) |
| Cat 3 (generic store disagreement) | investigator workflow | Yes | `accept-close-with-note` / `reopen-bead` | No (escalates through investigator) |
| Cat 3a (torn Beads write) | auto-resolve via adapter re-issue | No | — | Yes (§10.8a) |
| Cat 3b (verdict-unexecuted) | auto-resolve via §9.5b re-execution | No | — | Yes (re-run verdict action) |
| Cat 3c (inverse premature-close) | auto-verdict `accept-close-with-note` + mechanical close | No | — | Yes (direct close-write) |
| Cat 4 (recoverable known state) | auto-resume with pending action | No | — | Yes (re-arm retry/gate) |
| Cat 5 (clean restart) | normal startup; proceed to `ready` | No | — | Yes (no-op) |
| Cat 6a (integrity, LLM-triageable) | investigator workflow | Yes | `escalate-to-human` (usually) | No (investigator required) |
| Cat 6b (integrity, mechanically unrecoverable) | auto-escalate without investigator | No | — | N/A (operator intervention) |

**Invariant.** The default action is normative; any deviation (e.g., a policy override that routes Cat 1 through an investigator instead of auto-resume) is a policy decision logged in a reconciliation-workflow YAML and accompanied by rationale. Auto-resolver-present categories MUST have a deterministic resolver implementation; investigator-required categories MUST have a playbook per §9.4.

**Rationale for keeping 6 detection categories.** The investigator's playbook-per-category benefits from keeping distinct detection buckets: a Cat 2 investigator reads a different state surface than a Cat 3 investigator than a Cat 6a investigator. Collapsing to 3 action buckets (auto-resume / investigator / clean) would flatten those playbook distinctions. The action-mapping table above captures the action-level simplicity without losing the detection-level granularity.

**Taxonomy shape — settled, not critical.** Resolved 2026-04-24 by user decision: the 6-category detection layer + §9.2a action-mapping is the shape. The "3-action-vs-6-category" framing previously flagged by Skeptic review is not a blocker — it was a style debate, and runtime behavior of reconciliation is unchanged between framings. **This does not need to be re-audited before spec-draft or implementation.**

**Mechanism-tagged.** The action-mapping table is deterministic dispatch given a category.

**9.3 Detection rules — concrete and mechanism-tagged.**

**Scoping invariant (applies to all detectors below).** Detectors operate on **runs**, not on beads. A single bead may have zero, one, or many runs over its lifetime (per execution-model.md §2.5); detectors evaluate each run's state independently and classify each run into a category. In particular:
- An orphaned task branch from a prior run of a bead (where the bead has since been claimed by a subsequent run via `reopen-bead`) is Cat 5 for the old run — no reconciliation dispatched — and the new run is classified against its own branch alone.
- A detector MUST filter checkpoints by the `Harmonik-Run-ID` trailer, NOT by matching on `Harmonik-Bead-ID`.
- A single bead MAY have multiple in-flight runs at restart time if a `reopen-bead` write landed but the Beads audit entry is missing (a Cat 3a-adjacent pathology). Detector: "bead in `in_progress` with two or more task branches each advertising an `Harmonik-Run-ID` without a `reconciliation_verdict_executed` marker" → classify as Cat 6a (LLM-triageable integrity violation) for operator review.
- **Ordering invariant.** Detectors MUST use **git DAG parentage** (parent-pointer chain) for ordering checkpoints within a run's task branch, and **UUID v7 `event_id`** (per event-model.md §3.1) for ordering events. Wall-clock timestamps (`timestamp_wall`, git commit `author_date` / `committer_date`) are for display only and MUST NOT drive classification decisions. The most-recent checkpoint for a run is the tip of the run's task branch (the commit with no child in the run's branch-subgraph), not the commit with the latest wall-clock timestamp.

**Cat 0 pre-check** (runs before any other detector): the daemon verifies (a) `br --version` returns successfully within timeout T (suggested 5s) AND reports a version compatible with the pin (per beads-integration.md §10.8), (b) a trial `br list --limit 1` or equivalent returns without error, (c) git `rev-parse HEAD` succeeds (exposing any index-lock state as a failure), (d) `.harmonik/` is writable. Any prerequisite failing halts classification; the daemon emits `infrastructure_unavailable` and enters the `degraded` status loop (retry every N seconds; operator can query current Cat 0 state via `harmonik status`).

- Each category has a **detector function** that classifies an in-flight run by inspecting git state + Beads state + (optionally) JSONL tail signals. Detectors are deterministic: given the same inputs, they produce the same classification.
- Example detectors (non-exhaustive; the full table is the spec's §9.3):
  - Cat 1 detector: last checkpoint's `node_id` refers to a DOT node whose `idempotency_class` attribute is `idempotent` (per execution-model.md §2.1c).
  - Cat 2 detector: DOT node whose `idempotency_class` is `non-idempotent` or `recoverable-non-idempotent` per execution-model.md §2.1c; AND the bead is in `in_progress` AND there is no `run_completed` or `run_failed` event for the run since that checkpoint.
  - Cat 3 detectors: multiple pattern-specific sub-detectors (premature-close, manual-close, missing-worktree, duplicate transition_id). Detection reads the sibling-file paths (the filename is `<transition_id>.json`, so duplication is a path collision — detector can list `.harmonik/transitions/` across checkpoint commits in a run's branch to find it).
  - Cat 3a detector ("torn Beads write"): triggered when the daemon's in-memory intent log (persisted per §10.8) records an outstanding `br` write for `(target_run_id, transition_id, op)` AND the Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key. Auto-resolver: read the audit-log entry to determine whether the write landed; if so, mark the in-memory operation complete without re-writing; if not, re-issue the `br` call with the same idempotency key (per §10.8). No investigator is spawned for the common case; a Cat 3a where the audit log is itself ambiguous or missing the idempotency field escalates to Cat 3 generic (investigator-dispatched).
  - Cat 3b detector ("verdict-unexecuted"): triggered when an investigator-run's task branch contains a `reconciliation_verdict_emitted` commit AND there is no subsequent `Harmonik-Verdict-Executed: true` commit on the same branch (per §9.5b). Auto-resolver: the daemon performs the verdict's staleness check (§9.4b) — if stale, re-dispatches fresh reconciliation; if not stale, executes the verdict per §9.5b (reissuing the mechanical action idempotently) and writes the executed-commit. No new investigator is spawned (the original verdict is still durable).
  - Cat 3c detector ("inverse premature-close" / terminal-transition-without-Beads-write): triggered when a merge commit exists on the target branch (main or integration per workspace-model.md §5.8) tagged with `Harmonik-Run-ID R` (for run R whose workflow reached a success terminal state), the bead for R is still in `in_progress` in Beads, and no subsequent in-flight checkpoints for R exist. Auto-verdict `accept-close-with-note` with mechanical close-write (routed through the idempotency-keyed adapter per §10.8a). No investigator spawned. Rationale: git is authoritative for completion (§2.6 / §10.7); the divergence is deterministically resolvable in Beads's direction.
  - Cat 6a detectors: (a) workspace path referenced by in-flight bead does not exist on disk (and the sibling's transition-record file is absent); (b) trailer-vs-sibling-file mismatch on a checkpoint commit (e.g., `Harmonik-Transition-ID` trailer present but sibling file missing per §2.1b); (c) worktree has in-progress git operation (rebase, merge, cherry-pick, bisect) that is not the work product being tracked by harmonik — detected by checking for `.git/rebase-merge`, `.git/rebase-apply`, `.git/MERGE_HEAD`, `.git/CHERRY_PICK_HEAD`, `.git/BISECT_LOG` in the run's worktree. Auto-verdict default: `escalate-to-human` (an investigator may downgrade to a repair path, but the default recommendation is operator-intervention).
  - Cat 6b detectors: (a) JSONL is corrupt / unparseable past a byte offset such that the divergence-evidence reader (§9.3a) cannot make a determination; (b) a checkpoint commit hash referenced in JSONL is missing from git's object database; (c) git object database itself is corrupted (e.g., `git fsck` fails).
- Emits `reconciliation_category_assigned` event per event-model.md §3.2.

**9.3a JSONL divergence-evidence reads — scope and invariants.**
- Detectors in §9.3 and investigators per §9.4 MAY read JSONL for the purpose of **identifying divergence** between stores (per event-model.md §3.6 (c)). Permitted uses:
  - Detecting that a checkpoint commit referenced in a JSONL `checkpoint_written` event is missing from git (Cat 6 trigger).
  - Detecting that JSONL is corrupted past a byte offset (Cat 6 trigger).
  - Detecting that a `transition_event` exists in JSONL but no corresponding transition-record file exists in the checkpoint tree referenced by the event (Cat 3 trigger; per §2.1b storage contract).
  - Supplying observational context to an investigator agent so it can reason about the sequence of events leading to the divergence.
- **Forbidden uses.** A detector MUST NOT:
  - Use JSONL as the source of last-known `run_id`, `state_id`, or `transition_id` for any in-flight run. Those are derived from git (per §2.6, §3.6).
  - Use JSONL to decide which bead is in-flight. Beads (queried via `br`) is authoritative per §10.7.
  - Reconstruct the `state` or `transition` object from JSONL payloads. The authoritative record lives in git per §2.1b.
- **Output.** When divergence is detected, the detector emits a `store_divergence_detected` event (per event-model.md §3.2) with: the divergence class (checkpoint-missing, jsonl-corrupt, transition-file-missing, etc.), the triggering JSONL entry reference, the conflicting git/Beads facts, and the Cat classification this implies. The investigator workflow for the resulting Cat consumes the event, not the JSONL tail directly.
- **Mechanism-tagged.** Divergence detection is a deterministic comparison of three stores; no cognition participates.

**9.4 Investigator-agent contract.**
- **Inputs** to an investigator-agent node (bound to a **snapshot token** per §9.4b):
  - Snapshot token: `{git_head_hash, beads_audit_entry_id, captured_at_timestamp}` — captured by the daemon at investigator-dispatch time.
  - Run metadata (run_id, workflow_id, workflow_version) of the outer run being reconciled.
  - Bead ID and the Beads record for that bead as-of `beads_audit_entry_id`.
  - Git state at the last checkpoint as-of `git_head_hash` (commit hash, tree, trailers, and transition-record sibling file per §2.1b).
  - JSONL tail since the last checkpoint as-of snapshot (observational only — per event-model.md §3.6 and §9.3a divergence-evidence scope rules).
  - Workspace state (path exists? branch state? WIP files present?) as-of snapshot.
  - Agent session log if one exists (CASS-indexed per workspace-model.md §5.3).
- The investigator receives commits in git-DAG-parentage order per the §9.3 ordering invariant.
- Not all categories dispatch an investigator; see §9.2a for the action-mapping.
- **Playbook.** For each category with an investigator (Cat 2, Cat 3, Cat 6), the spec defines a playbook — specific checks in a specific order, producing evidence for the verdict.
- **Outputs.** The investigator emits (a) a `reconciliation_verdict_emitted` event with one of the §9.5 verdict enum values, and (b) a reconciliation commit on the relevant branch documenting the finding (so the audit trail includes the investigator's analysis in git per execution-model.md §2.6 store-authority rules). The verdict event MUST conform to §9.5a; malformed verdicts produce a fallback escalation. The investigator emits the verdict commit per §9.5; no intermediate checkpoints are written per §9.1a.
- **Acknowledged — WIP-loss mitigation on `reopen-bead`.** Before emitting a `reopen-bead` verdict (per §9.5), the investigator MUST capture any recoverable WIP from the outer run's worktree into the reconciliation commit it writes. Concretely: the investigator inspects the worktree for uncommitted changes (`git status --porcelain`, untracked files), captures a diff + file listing, and includes the capture in the reconciliation commit's body and/or as an annotated file under `.harmonik/reconciliation/<investigator_run_id>/wip-capture/`. Rationale: `reopen-bead` triggers a fresh worktree + fresh branch per §5.9; any WIP in the old worktree is otherwise lost. The WIP capture makes post-hoc review possible (operator or next-run agent can inspect what was in progress when reconciliation fired). This is mandatory for `reopen-bead` verdicts; it is OPTIONAL for other verdicts (which keep the worktree and thus retain WIP by default).

**9.4a Wall-clock budget — mandatory per reconciliation workflow.**
- Every reconciliation workflow (per §9.1 — any DOT workflow tagged `workflow_class = reconciliation` per §9.1a) MUST declare a **wall-clock budget**: a hard ceiling, measured from the workflow's `run_started` event to its terminal event, beyond which the daemon forcibly terminates the workflow.
- **Declaration locus.** The budget is declared as a YAML policy attached to the reconciliation workflow's DOT via the `budget_ref` attribute (per control-points.md §6.5, §6.9). The declaration includes a `wall_clock_seconds` field (required, positive integer). Declared in the S01-shipped reconciliation-workflow YAML per §9.1b.
- **Default.** In the absence of an explicit budget, the workflow-library packaging (F-C4-5) MUST supply a default budget per reconciliation category. Suggested defaults for MVH: Cat 2 → 600s, Cat 3 → 300s, Cat 6 → 900s. (Actual values set in spec-draft.)
- **Enforcement.** The daemon tracks wall-clock elapsed per reconciliation run against the declared budget. On exhaustion:
  - The daemon emits a `reconciliation_budget_exhausted` event (declared via event-model.md §3.2 amendment; payload: `run_id`, `workflow_id`, `budget_seconds`, `elapsed_seconds`).
  - The daemon issues a **default verdict of `escalate-to-human`** on the original (outer) run (per §9.5). This verdict is indistinguishable from an investigator-emitted `escalate-to-human` in the operator-facing surface.
  - The investigator subprocess is killed per handler-contract.md §4.6 cleanup rules.
  - No reconciliation commit is written by the daemon on budget exhaustion; the budget-exhausted event + the daemon's default verdict are the durable trace. (Rationale: the investigator did not produce evidence worth preserving; the audit trail is the budget-exhausted event.)
- **Interaction with F-C1-5.** Because reconciliation workflows are not checkpointed mid-investigation (§9.1a), budget exhaustion does not leave an in-flight reconciliation state to re-classify; the default verdict is the terminal state.
- **Mechanism-tagged.** Budget tracking and the default-verdict rule are both deterministic.

**9.4b Snapshot token — inputs bound to a consistent view.**
- **Snapshot capture.** At investigator dispatch time (the moment the daemon enters reconciliation-workflow dispatch per §8.2 step 5), the daemon captures a **snapshot token** with two fields:
  - `git_head_hash` — the SHA of the project's current `HEAD` (or the reference the investigator is reading from; may be a branch tip).
  - `beads_audit_entry_id` — the ID of the most recent Beads audit-log entry at capture time.
  - `captured_at_timestamp` — RFC 3339 wall-clock (advisory; for operator display only per §3.3).
- **Binding.** The snapshot token is passed to the investigator as part of its LaunchSpec (per handler-contract.md §4.2) and appears in the verdict-event payload (per §9.5a). The investigator's inputs in §9.4 are computed relative to the snapshot (e.g., git state is read at `git_head_hash`; Beads state is read with an audit-entry filter `<= beads_audit_entry_id`).
- **Verdict staleness check.** Before executing a verdict (§9.5b), the daemon re-captures the current `(git_head_hash', beads_audit_entry_id')` and compares against the snapshot. If either value has advanced in a way that invalidates the verdict (concretely: the target run's checkpoint trail has gained a new commit, OR the target run's bead has changed status in the Beads audit log since the snapshot), the daemon:
  - emits a `reconciliation_verdict_stale` event (declared via event-model.md §3.2 amendment; payload: snapshot token, current values, divergence reason),
  - does NOT execute the verdict,
  - re-dispatches a fresh reconciliation workflow against the new state.
- **Staleness-divergence scope.** Changes to sibling beads or to the daemon's JSONL event log do NOT trigger staleness. Only changes to the target run's git branch or the target bead's Beads audit entries count.
- **Rationale.** The investigator reasons over a consistent snapshot; the daemon refuses to act on a verdict that is no longer current. This closes the "investigator reads stale state" failure mode without adding coordination between the investigator and the daemon's runtime.
- **Mechanism-tagged.** Snapshot capture + staleness check are both deterministic comparisons.

**9.5 Verdict vocabulary (enum) — the daemon executes these mechanically.**
- **`resume-here`** — continue from the current state with the same or a fresh agent. Daemon re-dispatches the current node.
- **`resume-with-context`** — continue with additional context supplied by the investigator (e.g., a WIP summary). Daemon re-dispatches the current node with the context injected.
- **`reset-to-checkpoint`** — revert to a named earlier checkpoint and re-run from there. Intra-run rollback; keeps the worktree.
- **`reopen-bead`** — the work is not actually done; mark the bead `open` in Beads. Daemon clears the in-flight tracking for this bead; a subsequent claim produces a new run with a fresh worktree + fresh branch (per §9.6 and workspace-model.md §5.9).
- **`accept-close-with-note`** — the close is legitimate; annotate the audit gap. Daemon leaves Beads status as-is and records the annotation in the reconciliation commit.
- **`escalate-to-human`** — the investigator cannot resolve. Daemon pauses the affected run and emits an operator-observable event. Reserved for rare Cat 6 cases.
- **Verdict structural contract:** a reconciliation workflow emits **exactly one** verdict event, and that event is its terminal-state event. Multiple verdicts, a verdict event after the workflow's `run_completed`, or a verdict whose shape deviates from §9.5a is a structural violation with a defined handling per §9.5a.
- **Spec-draft obligation — operator override of verdicts.** Spec-draft produces a normative pre-execution-pause-on-verdict operator flag. Concretely: a per-reconciliation-workflow policy option to pause the daemon's verdict-execution step (per §9.5b) until an operator confirms or vetoes the verdict via `harmonik confirm-verdict <run_id>` / `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: execution proceeds without operator confirmation; operators opt in by policy. This obligation applies to all investigator-dispatched categories (Cat 2, 3, 6a per §9.2a).

**9.5a Verdict schema and malformed-verdict handling.**
- **Schema.** A verdict event (`reconciliation_verdict_emitted` per event-model.md §3.2) carries a payload conforming to:
  ```json
  {
    "verdict": "<one of: resume-here | resume-with-context | reset-to-checkpoint | reopen-bead | accept-close-with-note | escalate-to-human>",
    "investigator_run_id": "<run_id of the reconciliation workflow>",
    "target_run_id": "<run_id of the outer run being reconciled>",
    "evidence_ref": "<optional git commit hash of the reconciliation commit carrying evidence, or null>",
    "context": "<optional string; required when verdict = resume-with-context, MUST be empty otherwise>",
    "checkpoint_ref": "<optional transition_id; required when verdict = reset-to-checkpoint, MUST be null otherwise>"
  }
  ```
  The `verdict` field value MUST be exactly one of the six enum values. Extra top-level fields MUST be rejected.
- **One-verdict rule.** A reconciliation workflow MUST emit exactly one `reconciliation_verdict_emitted` event over its lifetime. The emission of the first verdict event marks the workflow as terminal; any subsequent verdict event from the same workflow is a structural violation.
- **Malformed-verdict handling.** On any of the following, the daemon:
  - emits a `reconciliation_verdict_malformed` event (declared via event-model.md §3.2 amendment; payload: `investigator_run_id`, `target_run_id`, `malformation_reason` [enum: unknown-verdict-value, missing-required-field, extra-fields, wrong-type, multiple-verdicts, verdict-after-terminal], `raw_verdict_excerpt`),
  - issues a **fallback verdict of `escalate-to-human`** on the target (outer) run,
  - terminates the reconciliation workflow (kills the investigator subprocess),
  - does NOT attempt to interpret the malformed payload.
- **Rationale.** Trusting LLM-generated text to conform to a schema is a ZFC violation at the classification layer (cognition-tagged content cannot drive mechanism-tagged routing without validation). The malformed-verdict path converts cognitive failure into a deterministic escalation.
- **Mechanism-tagged.** Schema validation + malformed-verdict handling are deterministic.

**9.5b Verdict execution — durable and idempotent.**
- The daemon first performs the staleness check of §9.4b; only non-stale verdicts proceed to the execution step below.
- **Execution step.** When a reconciliation workflow emits a verdict event (per §9.5a), the daemon performs the verdict's mechanical action (see §9.5 for per-verdict semantics). After the action succeeds, the daemon emits a `reconciliation_verdict_executed` event (declared via event-model.md §3.2 amendment; payload: `investigator_run_id`, `target_run_id`, `verdict`, `executed_at_timestamp`, `action_summary`) AND appends a second commit to the investigator's task branch tagged with the trailer `Harmonik-Verdict-Executed: true` (payload-free; presence-only marker).
- **Durability invariant.** The pair `(reconciliation_verdict_emitted, reconciliation_verdict_executed)` is the complete durable record. A verdict event without a matching execution event is a discoverable condition (see next bullet).
- **Discovery on restart.** The restart detector (per §8.2 step 5) treats a reconciliation workflow as resolved ONLY if both the verdict commit AND the verdict-executed commit are present on the investigator's branch. A reconciliation workflow with a verdict commit but no verdict-executed commit is classified as **Cat 3b** (verdict-unexecuted; see §9.2a and F-C3-3 in this delta plan) with a dedicated auto-resolver that re-attempts the verdict's mechanical action.
- **Idempotency.** Each verdict's mechanical action is designed to be idempotent:
  - `reopen-bead` → `br reopen <bead_id>` with an idempotency key `<target_run_id>:reopen` (per F-C3-2's `br`-adapter idempotency rule). If the bead is already `open`, no-op with success.
  - `resume-here`, `resume-with-context`, `reset-to-checkpoint` → dispatching the outer run's next node is idempotent at the dispatch layer (the next dispatch check sees the outer run already running and does not re-dispatch).
  - `accept-close-with-note` → appends an annotation to the reconciliation commit and writes the close to Beads if not already closed; idempotent on re-run.
  - `escalate-to-human` → emits `operator_escalation_required` event and marks the outer run in a quarantined state; idempotent (subsequent emissions are deduplicated by `target_run_id`).
- **Mechanism-tagged.** Verdict execution + its durable record are deterministic.

**9.6 Re-run vs. intra-run distinction.**
- A **new run** (cross-reference execution-model.md §2.5) is spawned only when the verdict is `reopen-bead` followed by a subsequent claim. The new run receives a fresh worktree + fresh branch per workspace-model.md §5.9.
- **Intra-run rollbacks** (`reset-to-checkpoint`) keep the worktree and the run ID; they revert to a named checkpoint and re-run from there within the same run.
- **Intra-run loops** (workflow edges routing back to an earlier node) are NOT produced by reconciliation — they are ordinary workflow-graph traversal handled by edge conditions and Guard/Gate control-points (control-points.md §6.2–§6.4). Reconciliation handles only restart and store-divergence cases.

**9.7 Failure commits — explicitly deferred.**
- Reconciliation does NOT require a git commit for every failed transition. Failure events (event-model.md §3.2) record the failure; checkpoints record successful durable states (execution-model.md §2.1a).
- **Design slot:** revisit if the improvement loop later needs `git bisect` over failures. If that need materializes, failure-commits become an additive change (a new optional kind of checkpoint commit) without breaking the current contract.

**9.8 Crash-recovery-tested.**
- Reconciliation workflows are covered by the crash-recovery test layer described in `docs/methodology/TESTING.md`. Every category taxonomy update, every new detector, every verdict-execution path MUST be exercised by at least one crash-recovery scenario test before landing.

### Dependencies
- **Depends on:** execution-model.md (§2.1 checkpoints, §2.1a cadence, §2.5 run-vs-bead, §2.6 three-store cross-reference), event-model.md (reconciliation event types per §3.2), handler-contract.md (investigator-agent launched via handler; §4.11 skill injection), control-points.md (gates determine escalation paths for Cat 6 verdicts; §6.11 skill declarations), beads-integration.md (Beads is queried as the pending-work authority; §10.7 store-authority rules).
- **Co-reference:** workspace-model.md §5.1 lease-by-run, §5.4 merge semantics, §5.9 re-run rule — reconciliation emits verdicts that trigger workspace-model behaviors; worktree-state detection drives Cat 3 / Cat 6 detectors. Treated directionally per Intra-foundation §"Co-dependency resolution rules."
- **Depended on by:** process-lifecycle.md (§8.2 startup triggers reconciliation, §8.8 crash semantics), operator-nfr.md (§7.8 restart RTO accounts for reconciliation dispatch; §7.3 pause carve-out for reconciliation workflows), workspace-model.md (§5.9 re-run rule references reconciliation verdicts), subsystem spec for S01 (Orchestrator Core; ships the reconciliation workflow library per §9.1b).

---

## Component 10: `specs/beads-integration.md`

### Purpose
Normative description of harmonik's integration with Beads as task ledger. Binds together the Beads references scattered across execution-model, event-model, handler-contract, workspace-model, operator-nfr, process-lifecycle, and reconciliation. Exists as a standalone component because the integration shape is cross-cutting and load-bearing (locked decision #13 per problem-space §Locked decisions).

### Requirements

**10.1 Beads selection.**
- Harmonik adopts `github.com/Dicklesworthstone/beads_rust` (SQLite-backed).
- Harmonik does NOT adopt the Dolt-backed Beads variant (`gastownhall/beads`). Rationale: the user has observed persistent operational issues with Dolt in practice; the SQLite fork is local-first and fits harmonik's single-machine per-project daemon model (process-lifecycle.md §8.1).
- Harmonik is the workflow engine layered ON TOP of Beads; Beads is not modified or forked.
- Mechanism-tagged (pinning a specific dependency is a structural choice).

**10.2 Access model — `br` CLI only.**
- All Beads interactions go through the `br` CLI.
- **Harmonik does NOT use Beads's MCP server (`br serve`).** Rationale: the CLI is the authoritative surface (30 commands); MCP exposes a subset; the CLI composes with shell + `jq`; running `br serve` adds another process harmonik must manage. Any future proposal to use `br serve` requires fresh justification.
- **Agents** invoke `br` via the **Beads-CLI skill** delivered through the handler-contract skill-injection mechanism (handler-contract.md §4.11 and control-points.md §6.11). The skill documents the `br` command surface and output formats.
- **The daemon** invokes `br` directly (as a subprocess) for its read queries during startup (process-lifecycle.md §8.2) and for its terminal-transition writes (§10.4 below).

**10.3 Beads-managed data.**
- **Bead content:** title, description, type.
- **Typed dependency edges:** parent-child, blocks, conditional-blocks, waits-for.
- **Coarse status:** `open`, `in_progress`, `closed`, `deferred`, `tombstone`.
- **Audit log:** Beads's own audit log records bead lifecycle changes.
- **Stable bead IDs:** a bead ID is stable for the lifetime of the bead.
- **Atomic claim:** Beads provides atomic-claim semantics so two agents cannot simultaneously claim the same bead.

**10.4 Harmonik write surface — terminal transitions only.**
- Harmonik writes to Beads ONLY at terminal workflow transitions:
  - **Claim**: `open` → `in_progress`. Occurs when the daemon dispatches a run against a ready bead.
  - **Close**: `in_progress` → `closed`. Occurs when the run's workflow reaches a success terminal state AND the merge to the target branch has completed (per workspace-model.md §5.8 branching model).
  - **Reopen**: `closed` → `open`. Occurs when a failure classification (or an investigator `reopen-bead` verdict per reconciliation.md §9.5) determines the work is not actually done.
- Harmonik does **NOT** write intra-run workflow state to Beads. Per-node workflow transitions, outcome details, and fine-grained failure types live in harmonik's JSONL event log + git checkpoint trail, NOT in Beads. Rationale (per feedback_beads_integration memory): Beads's `status` enum is deliberately coarse; writing every intra-run micro-transition would thrash the `blocked_issues_cache` and flood other Beads consumers.
- Writes route through the §10.8a adapter for idempotency.

**10.5 Harmonik read surface.**
- **Ready-work queries** — `br ready` (or equivalent) produces the set of beads whose dependencies are satisfied and whose status is `open`.
- **Dependency graph queries** — harmonik reads the typed-edge set for a bead to determine parents/children/blockers (informs branching per workspace-model.md §5.8).
- **Bead-detail queries** — title, description, status, edges, audit trail.
- **Reconciliation queries** — read-only queries during the daemon's startup sequence (process-lifecycle.md §8.2 steps 3–4).

**10.6 Bead-ID propagation.**
- The bead ID propagates through harmonik's artifacts:
  - (a) **Run metadata.** A run records its `bead_id` when dispatched against a bead.
  - (b) **Checkpoint commit trailers.** The optional trailer `Harmonik-Bead-ID: <bead_id>` (per execution-model.md §2.1) is present on every checkpoint commit whose run is tied to a bead.
  - (c) **Event payloads.** The optional `bead_id` field (per event-model.md §3.2 `checkpoint_written` payload and taxonomy notes) appears on lifecycle events scoped to a bead-bound run.
  - (d) **Session-log metadata.** Per workspace-model.md §5.3, session logs for bead-bound runs carry `bead_id` metadata (consumed by CASS for join-to-Beads indexing).
- Propagation is mechanism-tagged; it is deterministic given the bead-bound run.

**10.7 Store-authority rules.**
- **Beads is authoritative for bead content and coarse status within its domain.** If harmonik's in-memory model and Beads disagree about bead title or description, Beads wins.
- **Git is authoritative for completion.** If Beads reports a bead as `closed` but no corresponding merge commit exists in git, the divergence is a flag (a Cat 3 case per reconciliation.md §9.2). Beads status is corrected after the investigator's verdict, NOT silently.
- **JSONL is observational only.** JSONL is never used to override Beads or git.
- Cross-reference: execution-model.md §2.6 (three-store authority); reconciliation.md §9.2 Cat 3 (store-disagreement resolution).

**10.8 Version-pin + adapter layer.**
- Beads is pre-1.0. Harmonik mitigates breaking-change risk by:
  - (a) **Version-pinning** Beads per the external-inputs protocol (problem-space §External inputs). A harmonik release names the Beads version it tested against; upgrade requires a harmonik release that knows how to upgrade.
  - (b) **`br`-CLI adapter layer.** All Beads interactions route through a thin adapter module that translates harmonik's typed queries + writes into `br` invocations and parses `br` output. A breaking change in Beads produces exactly one adapter change; no scattered code updates required.
- Harmonik **absorbs breakage** rather than forking Beads. If Beads makes a backwards-incompatible change harmonik disagrees with, harmonik either stays on the prior version (delaying upgrade) or updates the adapter.

**10.8a Adapter idempotency — terminal-transition writes.**
- The `br`-CLI adapter (§10.8) MUST wrap every terminal-transition write (§10.4: claim, close, reopen) with an **idempotency key** derived deterministically from the context: `<run_id>:<transition_id>:<op>` where `op ∈ {claim, close, reopen}`.
- **Pre-write intent log.** Before issuing the `br` call, the adapter writes the intended operation to an on-disk intent log at `.harmonik/beads-intents/<idempotency_key>.json` and fsyncs it (per event-model.md §3.4 durability contract). After the `br` call succeeds, the adapter deletes the intent file.
- **Beads audit log check.** When the adapter is invoked with an idempotency key whose intent file already exists (indicating a prior crash or restart), the adapter first queries Beads's audit log for an entry matching the key. If found, the prior write landed; adapter deletes the intent file and returns success without re-writing. If not found, the adapter re-issues the `br` call (Beads's idempotency + the adapter's key ensures at-most-once effect).
- **Restart integration.** Cat 3a detector (§9.3) consumes the intent log + Beads audit log as its divergence-evidence source.
- **Mechanism-tagged.** Key derivation and log check are deterministic.

**10.9 The Beads-CLI skill.**
- The Beads-CLI skill is cited here, not defined. Its authoritative location:
  - Skill package: (path TBD; determined at bootstrap time).
  - Documentation: `docs/components/external/beads.md` (and/or the skill package's own docs).
- The skill explains `br` commands, output formats, idiomatic jq pipelines, and the harmonik-specific write rules from §10.4 (agents do NOT close beads outside the harmonik workflow path; harmonik's daemon owns the terminal-transition writes).
- Handler-contract.md §4.11 mandates that every agent operating in a harmonik run has this skill available (unless the role-specific permission set excludes it, which is unusual).

### Dependencies
- **Depends on:** handler-contract.md (§4.11 skill injection for Beads-CLI skill), execution-model.md (§2.1 bead-ID trailer, §2.5 bead-vs-run distinction, §2.6 three-store cross-reference), event-model.md (§3.2 `bead_id` event payload field), control-points.md (§6.6 Beads-CLI in default role permission sets, §6.11 skill declaration).
- **Co-reference:** workspace-model.md §5.3 (session-log bead-ID metadata consumer) and §5.8 (parent-child branching consumer). beads-integration defines the bead ID and typed edges; workspace-model consumes them. Treated directionally per Intra-foundation §"Co-dependency resolution rules."
- **Depended on by:** process-lifecycle.md (§8.2 Beads queried during startup), reconciliation.md (§9.2 Cat 3 store-divergence rules, §9.5 `reopen-bead` writes through §10.4), operator-nfr.md (§7.4 queue-format contract is Beads schema + harmonik overlay), workspace-model.md (§5.3 session-log metadata, §5.8 parent-child branching).

---

## Intra-foundation dependency graph

Per the Dependencies section in each component, the edges are:

| Spec | Direct depends-on |
|---|---|
| architecture.md | (none — root) |
| execution-model.md | architecture.md |
| event-model.md | architecture.md, execution-model.md |
| handler-contract.md | architecture.md, execution-model.md, event-model.md, process-lifecycle.md (daemon-subprocess relationship §8.5). *Co-reference (not depends-on):* control-points.md §6.11 for skill-declaration surface — see co-dep rules below |
| workspace-model.md | architecture.md, execution-model.md, event-model.md, handler-contract.md, beads-integration.md (parent-child branching). *Co-reference (not depends-on):* reconciliation.md — workspace-model §5.9 re-run rule is triggered by reconciliation verdicts; treated directionally per co-dep rules |
| control-points.md | architecture.md, execution-model.md, event-model.md, handler-contract.md (hooks consume handler outcomes and agent-lifecycle events; skill-injection obligation §4.11) |
| operator-nfr.md | architecture.md, execution-model.md, event-model.md, handler-contract.md, workspace-model.md, control-points.md, process-lifecycle.md, reconciliation.md, beads-integration.md |
| process-lifecycle.md | architecture.md, execution-model.md |
| reconciliation.md | execution-model.md, event-model.md, handler-contract.md, control-points.md, beads-integration.md. *Co-reference:* workspace-model.md §5.9 — reconciliation verdicts trigger workspace-model's re-run rule; treated directionally |
| beads-integration.md | handler-contract.md, execution-model.md, event-model.md, control-points.md. *Co-reference:* workspace-model.md §5.3 (session-log bead-ID metadata) and §5.8 (parent-child branching) — beads-integration defines the IDs and edges; workspace-model consumes them |

Rendered as a DAG (simplified; all co-dependencies resolved directionally per the rules below):

```
  architecture.md
        |
        v
  execution-model.md
        |
        +---> event-model.md
        |
        +---> process-lifecycle.md
        |
        v
  handler-contract.md  <--- control-points.md
        |                        ^
        v                        |
  beads-integration.md ----------+
        |
        v
  workspace-model.md  <--- reconciliation.md
        |
        v
  operator-nfr.md (cites all above)
```

This is a valid DAG (no cycles). The diagram is structural, not strictly linear — several specs share layers (e.g., handler-contract and process-lifecycle at the same layer of directness to architecture).

**Co-dependency resolution rules** (these are authoring-order constraints, not runtime cycles):
- `execution-model ↔ event-model`: execution-model owns type definitions (`run`, `state`, `transition`); event-model owns wire formats (event schema that references those types). Each cites the other directionally.
- `execution-model ↔ handler-contract`: execution-model owns outcome shape (`outcome` type); handler-contract owns the handler's obligation to produce outcomes conforming to that shape.
- `control-points ↔ handler-contract`: control-points owns the hook/gate/guard primitive and the skill-declaration surface (§6.11); handler-contract cites control-points for hooks that fire on handler-lifecycle events AND for the skill-declaration consumed by §4.11. Handler-contract emits events and consumes skill declarations; control-points defines the surface.
- `process-lifecycle ↔ handler-contract`: process-lifecycle owns the daemon-subprocess relationship and the socket path; handler-contract cites process-lifecycle for the launch model (§4.10).
- `reconciliation ↔ workspace-model`: reconciliation owns the category taxonomy and verdict vocabulary; workspace-model owns the re-run rule that reconciliation verdicts trigger.
- `beads-integration ↔ execution-model`: beads-integration owns the Beads access model and store-authority rules; execution-model owns the bead-ID trailer schema and the bead-vs-run distinction. Each cites the other directionally.
- `beads-integration ↔ workspace-model`: beads-integration owns the bead-ID and the typed parent-child edge; workspace-model consumes those to (a) stamp session-log metadata (§5.3) and (b) decide where task branches land (§5.8). beads-integration does NOT depend on workspace-model's internals.

The prior revision of this diagram had inconsistencies with the per-component Dependencies prose (Architect B-1); this version reconciles both, and extends to the ten-component set added after 2026-04-20 / 2026-04-21 decisions.

## Outcome spine cross-reference map (architect F7 response)

The handler → hook → gate → transition → event chain crosses five of the ten specs:

| Segment | Owned by spec |
|---|---|
| Handler produces outcome | handler-contract.md §4.1, §4.8 |
| Outcome shape (type) | execution-model.md §2.1 (`outcome`) |
| Hook dispatch on outcome | control-points.md §6.3 |
| Gate evaluation (allow/deny) | control-points.md §6.2 |
| Transition selection | execution-model.md §2.1 (edge cascade), §2.2 (outcome spine integration) |
| Transition record (trace) | execution-model.md §2.1 (`transition` type with full AlphaGo fields) |
| Transition event emission | event-model.md §3.2 (`transition_event` event type; projection of the trace record) |

This is the integrated contract the research pass investigates in depth per-segment.

## Success criteria for this pass (applied in decompose's own review)

- Every goal in problem-space maps to at least one component requirement here.
- Every component requirement is concrete (specifies what the spec must describe, not what the spec text should read).
- Dependencies form a DAG (with acknowledged co-dependencies resolved by directional rules).
- The 10+ naming conflicts are distributed across components for resolution (cycle/gate/hook/policy/workspace/session/verification).
- Every undefined data type from the subsystem audit (`.kerf/recon/subsystem-audit.md`) maps to a component requirement.
- Every "missing" NFR from the NFR inventory maps to a disposition (a requirement here, a deferral in §7.10, or an explicit subsystem-local claim).
