# ARCH-01 — Settle the immutable run-architecture contract

## Dispatch metadata

- Group / priority: run architecture / P0
- Execution profile: `sol_xhigh`
- Reviewer profile: two independent `sol_xhigh` reviewers
- Depends on: `ARCH-00`, `RL-01`, `CQ-00`, `JR-00`, `CQ-02`,
  `INPUT-ACK-CONTRACT-01`
- Work type: kerf/spec architecture contract; no production implementation

## Objective

Define the target ownership and construction graph that replaces temporal
`workLoopDeps` assembly and partially valid `RunEnv`, `RunPorts`, and
`SharedHandles`. Separate outer-loop services, immutable per-run facts,
process/session lifecycle, mode execution, and durable terminal effects. This
task settles cross-contract ownership, dependency direction, accepted migration
states, and structural targets. It does not refactor Go or redefine queue, Run,
Beads, event, or process-lifecycle semantics.

## Authoritative inputs and preflight

The coordinator must record all six dependencies as integrated, independently
approved, and proof-green before claim. Consume these exact artifacts:

- `tasks/evidence/ARCH-00.yaml`
- `tasks/evidence/RL-01.yaml`
- `tasks/evidence/CQ-00.yaml`
- `tasks/evidence/JR-00.yaml`
- the finalized CQ-02 contract and review artifact recorded in
  `TASK-INDEX.yaml`
- `tasks/evidence/INPUT-ACK-CONTRACT-01.yaml` and the reviewed
  run-state-machine amendment it pins

Paths above are relative to `plans/2026-07-24-code-health-audit/`. At claim
time, record each input's Git blob ID in `tasks/evidence/ARCH-01.yaml`. Stop on
an input mismatch, dirty input, incomplete dependency, or non-ancestor
integration.

Normative boundaries:

- `specs/architecture.md`: daemon/process and dependency invariants
- `specs/execution-model.md`: Run identity, sealed claim-time facts, modes,
  terminal events, and terminal ledger ordering
- `specs/run-state-machine.md`: pure reactors, daemon shell, consumer-owned
  ports, shared-reference exceptions, and terminal spine
- `specs/queue-model.md`: queue transaction, single writer,
  persist/emit/unlink ordering, and queue schema
- `specs/process-lifecycle.md`: process Wait/signal/reap and composition
- `specs/beads-integration.md`: terminal Bead-transition ownership
- the CQ-02 contract: queue mutation, persistence, and recovery decisions

ARCH-01 may define cross-contract ownership, construction, and dependency
direction. It must not redefine queue transaction semantics, Run terminal
semantics, event payloads, Bead transitions, or process lifecycle behavior. A
needed normative change outside the new ARCH-01 spec is a stop condition.

## Kerf work and exclusive lease

Use exactly:

```bash
test ! -e .kerf/works/run-architecture-contract
kerf new run-architecture-contract --jig spec \
  --title "Immutable run architecture contract" --no-auto-filter
```

The canonical work path is `.kerf/works/run-architecture-contract/`; never run
`kerf localize`. Follow each instruction printed by
`kerf show run-architecture-contract` and run
`kerf square run-architecture-contract` before packaging.

Allowed writes are exactly:

- `.kerf/works/run-architecture-contract/**`
- `specs/run-architecture-contract.md`
- `specs/_registry.yaml` solely to add exactly:
  `RAC: {spec-id: run-architecture-contract, reserved: 2026-07-27, status: reviewed}`
- `plans/2026-07-24-code-health-audit/tasks/evidence/ARCH-01.yaml`

The finalized spec must declare `spec-id: run-architecture-contract` and
`requirement-prefix: RAC`. No other registry row may change.

Do not modify existing specs, ADRs, production Go, tests, task cards,
`TASK-INDEX.yaml`, other kerf works, or other evidence artifacts. Design
rationale belongs in the kerf work; only reviewed normative requirements belong
in the final spec.

Kerf local storage is tracked in this repository and `kerf finalize` creates an
unreviewed branch and commit with a commit message that violates repository
policy. Therefore the worker must not invoke `kerf finalize`. After
`kerf square` and both reviews approve, copy the reviewed draft
`.kerf/works/run-architecture-contract/05-spec-drafts/run-architecture-contract.md`
byte-for-byte to `specs/run-architecture-contract.md`, update only the one
registry row, and commit the complete work as one reviewed commit on the
coordinator-created task branch. Record this beta-tool exception in the
evidence.

Stop if kerf selects a different work/spec path, the codename or output already
exists incompatibly, or satisfying the spec jig requires another write.

## Required decisions

1. Define one owner and allowed dependency direction for:
   - outer queue-loop maintenance, gates, source selection, and admission;
   - immutable queued and direct run plans;
   - per-run registry, counter, semaphore, and identity handles;
   - process/session launch, Wait, signal/kill, reap, and close;
   - single, review, and DOT mode execution;
   - queue terminal projection, Bead terminal transition, event emission,
     run-record cleanup, and restart recovery.
2. Define distinct queued and direct constructors so forbidden identity and
   queue-field combinations cannot be represented. Required dependencies must
   be constructor-enforced; late nil population and generic service-locator bags
   are forbidden.
3. Produce an explicit decision matrix for `runexec`, `runlaunch`, `runloop`,
   `reviewcycle`, `continuity`, queue transaction owners, and every LIFT
   artifact: `adopt`, `adapt`, `retire`, or `defer`, with evidence. In
   particular, settle every adopt/retire decision left by RL-01.
4. Preserve the CQ-02 transaction and recovery decisions. For every durable
   transition identify its decision owner, effect owner, serialization owner,
   recovery owner, and normative clause; do not invent a competing ordering.
5. Produce a migration DAG using existing task IDs where possible. Every node
   must name prerequisites, exact writable paths, lease family, rollback
   boundary, and an accepted intermediate state that compiles and is testable.
   If a missing slice is discovered, propose a new task record in evidence; do
   not create the task or edit the index.
6. Set literal numeric targets for every task consumed by `ARCH-GATE`.
   Baselines must come from ARCH-00 or a reproducible command and cover symbol
   span, cognitive complexity, dependency/field reach, and forbidden imports.
   A percentage or words such as “reduce” and “shrink” without an integer
   target are invalid.
7. State exact completion criteria for eliminating temporal `workLoopDeps`
   assembly and shrinking `runWorkLoop`, `beadRunOne`, `runReviewLoop`,
   `driveDotWorkflow`, and `dispatchDotAgenticNode`. Moving a giant function
   unchanged does not satisfy a target.

## Required evidence schema

`tasks/evidence/ARCH-01.yaml` must contain exactly these top-level keys:

```yaml
schema_version: 1
task_id: ARCH-01
base_sha: ""
inputs: []
locked_decisions: []
owners: []
run_variants: {}
durable_transitions: []
existing_contract_disposition: []
migration: []
targets: []
collisions: []
proof: []
review: {}
```

Required row shapes:

```yaml
inputs:
  - {path: "", blob_sha: "", review_state: approved}
locked_decisions:
  - {id: 1, decision: "", impact: preserved}
owners:
  - name: ""
    package: ""
    owns: []
    constructed_by: ""
    dependencies: [{name: "", direction: "consumer -> provider"}]
    forbidden_dependencies: []
run_variants:
  queued: {required: [], forbidden: [], constructor: ""}
  direct: {required: [], forbidden: [], constructor: ""}
durable_transitions:
  - transition: ""
    decision_owner: ""
    effect_owner: ""
    serialization_owner: ""
    recovery_owner: ""
    normative_source: ""
existing_contract_disposition:
  - {artifact: "", disposition: "<adopt|adapt|retire|defer>", reason: ""}
migration:
  - task: ""
    prerequisites: []
    lease_family: ""
    writes: []
    accepted_intermediate_state: ""
    rollback_boundary: ""
targets:
  - task: ""
    symbol: ""
    baseline_span: 0
    target_span: 0
    baseline_complexity: 0
    target_complexity: 0
    baseline_reach: 0
    target_reach: 0
    forbidden_imports: []
collisions:
  - {spine: "", tasks: [], serialization_rule: ""}
proof:
  - {command: "", exit_code: 0, result: ""}
review:
  queue: {reviewer: null, artifact: null, verdict: pending}
  run: {reviewer: null, artifact: null, verdict: pending}
```

Every owner, durable transition, migration node, collision rule, and numeric
target in evidence must have a literal counterpart in the final spec.

## Non-negotiable boundaries

Preserve these ten locked decisions verbatim in the evidence and show how the
contract preserves each:

1. Go implementation.
2. Go-native orchestrator.
3. In-process pub/sub plus JSONL source of truth.
4. tmux-inspectable NTM runner.
5. Claude Code and Pi handlers.
6. Separate twin binaries.
7. Workflow worktrees and merges without agent-mail reservations.
8. CASS-only initial memory.
9. No verifier subsystem.
10. Operator controls between tasks.

Also preserve the later locked rule that Beads owns terminal Bead transitions.
Stop rather than proposing an owner or dependency that violates one of these.
Record that rule in the `durable_transitions` row for the terminal Bead
transition, with `specs/beads-integration.md` as its normative source.

Serialize every migration touching `dispatch_spine`,
`workloop_recovery_spine`, `reviewloop_spine`, `dot_spine`,
`process_phase_scope`, `composition_spine`, or coordinator-owned ARCH-GATE
target data. A task may occupy only one active semantic-writer slot. ARCH-01
writes none of the production spines.

## Acceptance

- Every mutable resource and durable state has exactly one owner.
- Required dependencies and queued/direct variants are constructor-enforced.
- Single, review, and DOT modes share lifecycle contracts without a generic god
  port.
- Existing normative behavior is consumed by citation, not silently amended.
- Every downstream step has a testable intermediate state, rollback boundary,
  exclusive lease, and fixed numeric structural target.
- Both independent cross-group reviewers approve.

## Verification and review gate

After acquiring a coordinator builder token for build-class commands, record
exact output and exit status for:

```bash
kerf show run-architecture-contract
kerf square run-architecture-contract
make specaudit-lint
ruby -ryaml -e '
  y=YAML.load_file(ARGV.fetch(0))
  top=%w[schema_version task_id base_sha inputs locked_decisions owners
    run_variants durable_transitions existing_contract_disposition migration
    targets collisions proof review]
  abort("top-level schema") unless y.keys.sort == top.sort
  abort("identity") unless y["schema_version"] == 1 && y["task_id"] == "ARCH-01"
  abort("locked decisions") unless y["locked_decisions"].length == 10
  abort("variants") unless y["run_variants"].keys.sort == %w[direct queued]
  abort("targets") unless y["targets"].all? { |r|
    %w[baseline_span target_span baseline_complexity target_complexity
       baseline_reach target_reach].all? { |k| r[k].is_a?(Integer) } }
  %w[inputs owners durable_transitions existing_contract_disposition migration
     targets collisions proof].each { |k|
    abort("#{k} empty") unless y[k].is_a?(Array) && !y[k].empty?
  }
  ids=y["locked_decisions"].map { |r| r["id"] }
  abort("locked decision ids") unless ids.sort == (1..10).to_a
  q=y.dig("review","queue")
  r=y.dig("review","run")
  [q,r].each { |v|
    abort("review incomplete") unless v.is_a?(Hash) &&
      v["verdict"] == "APPROVE" &&
      v["reviewer"].is_a?(String) && !v["reviewer"].empty? &&
      v["artifact"].is_a?(String) && !v["artifact"].empty?
  }
  abort("reviewers not independent") if q["reviewer"] == r["reviewer"]
' plans/2026-07-24-code-health-audit/tasks/evidence/ARCH-01.yaml
cmp \
  .kerf/works/run-architecture-contract/05-spec-drafts/run-architecture-contract.md \
  specs/run-architecture-contract.md
git diff --check
# Before commit: includes staged and unstaged candidate changes.
git diff --name-only <claim.base_sha> --
# After the reviewed commit:
git diff --name-only <claim.base_sha>...HEAD
```

Both diff outputs must contain only the four allowed lease paths/prefixes and
must be recorded in `proof`. Run the YAML validator only after both review
artifacts exist and the final approved review fields have been recorded.

The worker leaves both review entries pending and returns the final candidate
to the coordinator. The coordinator routes it to two different `sol_xhigh`
reviewers: one queue-contract reviewer and one Run/lifecycle reviewer. The
worker resolves returned findings, but must not dispatch or select its own
reviewers. Review artifacts are written under
`.kerf/works/run-architecture-contract/reviews/`. Both verdicts must be
`APPROVE`; neither the worker nor coordinator may self-approve the architecture
contract.

## Stop conditions

Stop without packaging or committing if:

- an input pin or prerequisite state differs;
- CQ-02 is not finalized, integrated, approved, and proof-green;
- the kerf path, task branch, or spec output exists incompatibly;
- any required write falls outside the lease;
- the design changes queue schema or transaction semantics;
- it contradicts an existing normative requirement or finalized kerf work;
- it reopens a locked decision;
- Wait, close, terminal ledger, persistence, emission, unlink, or recovery has
  multiple owners;
- a migration node lacks an accepted intermediate state, rollback boundary, or
  literal numeric target;
- either required reviewer cannot be independent.

Report the exact conflict and smallest decision needed from the coordinator or
operator. Do not broaden scope to resolve it.

## Return

Use the directory return contract. **COMMIT EXPLICITLY.**
