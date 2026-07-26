# Core queue recovery task packets

This directory turns [`TASK-INDEX.yaml`](../TASK-INDEX.yaml) into executable
handoffs. The index is the coordinator-owned state machine; each file here is
the immutable brief for one bounded worker outcome.

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

The first recovery wave is:

| Slot | Task | Profile | Why |
|---|---|---|---|
| frontier builder | `CQ-DEF-01` | GPT-5.6 Sol, `high` | shared dispatch spine with a Sol-reviewed card |
| bounded worker | `CQ-00A` | Pi/Nemotron Ralph | deterministic ingress/caller inventory |
| bounded worker | `CQ-00B` | Pi/Nemotron Ralph | deterministic test/crash-cut inventory |
| coordinator/overseer | review and integration | GPT-5.6 Sol, `xhigh` | contracts, security, recovery, and next-card review |

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

Sol is the default frontier choice for restoring the queue because the current
failures cross persistence, selection, Run ownership, process lifecycle, and
terminalization. Terra is preferred once a Sol-reviewed card has reduced the
task to a bounded implementation; it is faster and sufficient for that shape.
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
