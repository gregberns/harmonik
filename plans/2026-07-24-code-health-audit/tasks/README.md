# Core queue recovery task packets

This directory turns [`TASK-INDEX.yaml`](../TASK-INDEX.yaml) into executable
handoffs. The index is the coordinator-owned state machine; each file here is
the immutable brief for one bounded worker outcome.

## Program correction: architecture is the bootstrap

The queue is not restored by accumulating fixes in the existing giant state
machines. The P0 program is now:

```text
measure and freeze the real production graph
  → settle immutable ownership and construction contracts
  → decompose runWorkLoop, beadRunOne, reviewloop, and DOT
  → replace temporal workLoopDeps assembly
  → prove the real production composition
  → finish queue/Run/Pi correctness and end-to-end recovery
```

Queue durability and pure state-machine contracts may proceed off-spine because
they make the decomposition safe. Semantic work in `workloop.go`,
`reviewloop.go`, or the DOT core must consume an extracted owner or create the
fidelity proof needed for that extraction. A package move, interface wrapper,
or new suppression does not count as structural decomposition.

The architecture cards deliberately reconcile the existing P2/RT/LIFT and
`reviewloop-decoupling` work. They do not authorize a competing extraction
program or a move of the remaining giant functions intact.

## Operating without agent-to-agent communications

One coordinator is the hub. Workers do not need to communicate with each other.

1. The coordinator selects only a task whose index record is `phase: ready`,
   `disposition: open`, and whose `hard_requires` are all `complete`.
2. The coordinator pins the base commit, worktree, branch, owner, lease, and
   reviewer in the index before launch. Workers never edit the index.
3. Each worker reads only its task file and the sources/specs named there. It
   writes only inside its lease. Out-of-lease findings go in the return report;
   they are not fixed opportunistically.
4. Completion is durable even if chat/comms fail: the worker leaves a clean
   worktree and one commit (or the evidence artifact required by a read-only
   task). The coordinator can recover progress from `git worktree list`,
   `git status`, and `git log`.
5. A different agent reviews the result. The coordinator records review,
   test, commit, and integration evidence in the index and alone changes task
   status or unblocks dependents.
6. When a task discovers an unresolved contract, unsafe scope expansion, or
   lease collision, it stops at the stated escalation boundary. The coordinator
   splits or amends the task before redispatch.
7. Before any test, build, vet, lint, UBS, or check command, a worker acquires
   one of the coordinator's two builder tokens and releases it when the command
   finishes. Source searches and YAML parsing do not consume a token.

This is intentionally a star topology:

```text
worker A ─┐
worker B ─┼─ coordinator/index ─ reviewer/integration
worker C ─┘
```

No shared chat bus is required. Git worktrees provide write isolation, the task
cards provide context, and the coordinator-owned index provides serialization.

## Four-agent cadence

With four total agents, use one coordinator/overseer plus three workers:

- one frontier builder on the highest-risk ready implementation;
- up to two Pi/Nemotron workers on bounded evidence, fixture, or pure-kernel
  tasks;
- the coordinator reviews checkpoints, prepares the next cards, and performs
  integration. If both Pi slots are not safely feedable, use one Terra worker
  for characterization instead of inventing low-value Pi work.

The completed historical recovery wave was:

| Slot | Task | Profile | Why |
|---|---|---|---|
| frontier builder | `CQ-DEF-01` | GPT-5.6 Sol, `high` | shared dispatch spine with a Sol-reviewed card |
| bounded worker | `CQ-00A` | Pi/Nemotron Ralph | deterministic ingress/caller inventory |
| bounded worker | `CQ-00B` | Pi/Nemotron Ralph | deterministic test/crash-cut inventory |
| coordinator/overseer | review and integration | GPT-5.6 Sol, `xhigh` | contracts, security, recovery, and next-card review |

The architecture-foundation wave is:

| Slot | Task | Profile | Why |
|---|---|---|---|
| architecture evidence | `ARCH-00` | GPT-5.6 Terra, `high` | reconcile the live graph and two weeks of refactors |
| queue contract | `CQ-02` | GPT-5.6 Sol, `xhigh` | establish safe durable-write ownership off-spine |
| Run evidence | `JR-00` | GPT-5.6 Terra, `high` | map claim-to-terminal ownership before phase extraction |
| coordinator/overseer | review, leases, integration | GPT-5.6 Sol, `xhigh` | approve architecture and keep one shared-spine writer |

After the factual foundation, use Sol `xhigh` for `ARCH-01`, then a bounded
Terra worker for `ARCH-GATE`. Once `ARCH-01`, `ARCH-GATE`, and `PS-01` are
approved, the steady four-slot shape is:

- one `dispatch_spine` writer;
- one reviewloop **or** DOT writer with frozen lifecycle/interface contracts;
- one file-disjoint queue, process-adapter, pure-kernel, or proof worker;
- the Sol `xhigh` coordinator/reviewer.

Reviewloop and DOT may run beside a dispatch-spine task only when their leases
exclude shared lifecycle/interface files and the coordinator-owned architecture
baseline. If a mode task needs to change `PS-01` or `ARCH-01`, it stops for a
contract amendment.

If Pi cannot complete an inventory card after one corrected retry, reassign it
to GPT-5.6 Terra at `high`; do not let Pi broaden the lease.

## Model routing

- `sol_xhigh`: GPT-5.6 Sol with `reasoning_effort=xhigh`. Use for security,
  concurrency, durable recovery, shared-spine changes, cross-group review, and
  ambiguous production wiring.
- `sol_high`: GPT-5.6 Sol with `reasoning_effort=high`. Use for reviewed
  shared-spine wiring and bounded security implementation.
- `terra_high`: GPT-5.6 Terra with `reasoning_effort=high`. Use for bounded
  multi-file implementation where the contract and tests are already explicit,
  and for focused independent review.
- `pi_ralph`: local Nemotron through the Pi Ralph loop. Use only for
  deterministic inventories, fixtures, table/property tests, narrow pure
  functions, and mechanical changes with an exclusive lease and exact checks.
- `sol_xhigh`: a fresh GPT-5.6 Sol context at `xhigh` for security,
  recovery, concurrency, shared-spine, and cross-group verdicts.

Sol is the default frontier choice for the architectural spine because the
current failures cross persistence, selection, Run ownership, process
lifecycle, terminalization, and construction. Terra is preferred once a
Sol-reviewed card has reduced the task to a bounded extraction, caller
migration, or proof; it is faster and sufficient for that shape.
For `CQ-DEF-01`, `high` is the builder setting once the card is approved;
escalate to `xhigh` if production wiring diverges from the recorded path.

## Task lifecycle

```text
triage → design → ready → implement → review → integrate → verify → complete
```

`ready` is a strong claim. It means:

- every dependency is complete;
- the task card has been independently reviewed;
- the base strategy and exclusive lease are viable;
- acceptance tests and the escalation boundary are explicit;
- each structural card has exact coordinator-approved
  complexity/span/reach/import targets from `ARCH-01`/`ARCH-GATE`;
- the assigned model profile is allowed for the task.

Read-only characterization tasks finish by adding a tracked YAML evidence file
under `tasks/evidence/`. Implementation tasks finish with one reviewed commit.
Workers do not create or close Beads; the coordinator may mirror a ready task
into the machine-local ledger after checking that no equivalent bead exists.

## Return contract

Every worker returns:

- task ID, branch, worktree, pinned base, and final commit (or evidence path);
- exact changed files and a statement that the lease was obeyed;
- production call site(s) checked;
- commands and results for every required verification class;
- unresolved findings and every deferred proof;
- reviewer identity and verdict, if review was performed in the same handoff.

Implementation briefs end with: **COMMIT EXPLICITLY and push the task branch
only when the coordinator requested a push.**
