# Repeatable architectural review process

## Purpose

Use this process to repeat the delete-and-rewrite architectural review.
It starts with current repository facts.
It ends with an evidence-backed and dependency-ordered implementation backlog.

The review agent analyzes and writes review documents.
The implementation agent changes production code only after the review is complete.

## Required outputs

Create one dated directory under `plans/2026-07-27-delete-and-rewrite/reviews/`.
Use a name such as `YYYY-MM-DD-follow-up-review`.

Write these files as the review runs:

1. `README.md` — scope, pinned revisions, verdict, and document map.
2. `EVIDENCE.md` — exact commands, counts, measurements, and limits.
3. `FINDINGS.md` — ranked current-tree conclusions and rejected work.
4. `IMPLEMENTATION-BACKLOG.md` — bounded tasks with problem, scope, acceptance, and limits.
5. `EXECUTION-ORDER.md` — dependencies, parallel work, stop gates, and first assignment.
6. `RECONCILIATION.md` — status of every task from the preceding review.

Do not edit a prior dated review to make it current.
The reconciliation file explains what changed.

## 1. Establish authority and preserve state

Read these inputs in order:

1. `PRINCIPLES.md`.
2. `plans/2026-07-27-delete-and-rewrite/CHARTER.md`.
3. The newest prior review directory.
4. Active plans and Kerf work for the core path.
5. The current integration branch log and working-tree status.

Record the branch, exact revision, remote divergence, and local changes.
Do not modify unrelated local changes.
Do not treat a dirty worktree as review evidence.

The charter core path is:

> config → event bus → queue → bead-ledger adapter → worktrees → harness registry + one substrate → work loop → merge

Use this path to rank work.

## 2. Pin and verify the analysis tools

Record the `codebase-organism` revision and its working-tree state.
Build tools into a new temporary directory.
Never write generated output into tracked Harmonik paths.

Before using results, run the tool quality gate:

```sh
make lint
make test
make determinism
```

If the tool tree has uncommitted fixes, name that fact.
Do not describe the run with only the old commit hash.

## 3. Run the reproducible analysis suite

Run these tools against the pinned Harmonik revision:

```sh
typegraph -repo /path/to/harmonik -out /tmp/review/harmonik ./...
detect -repo /path/to/harmonik -pkg ./... -out /tmp/review/findings.json
mq -graph /tmp/review/harmonik_symbols.json
hotspots -repo /path/to/harmonik -graph /tmp/review/harmonik_symbols.json -top 40
coref -graph /tmp/review/harmonik_symbols.json -out /tmp/review/coref.json
funcseam -repo /path/to/harmonik -pkg ./internal/queue -list
funcseam -repo /path/to/harmonik -pkg ./internal/daemon -list
```

Run extra package scans when the prior backlog names another live core package.
Keep raw output in the temporary directory.
Put only interpreted evidence in the review.

Record at least:

- finding count by class;
- A1 same-package, cross-package, and ambiguous counts;
- A2 total sites, multi-file findings, and largest file set;
- graph declarations, ties, STATE edges, and mutable nodes;
- MQ gap, package LOC Gini, and top-five share;
- current hotspots and largest live functions;
- core-path packages whose boundaries disagree with coupling.

## 4. Reconcile the preceding backlog first

For every prior task, classify it as:

- complete;
- complete with changed design;
- active;
- blocked;
- superseded;
- rejected;
- not started.

Prove status from current code, tests, commits, plans, or Kerf state.
Do not infer completion from a task title or branch name.

Record newly discovered prerequisites.
Move a task later when its safe input contract does not exist.
Do not preserve an old priority only for continuity.

## 5. Interpret detector output as candidates

Read the detector rule before its findings.
Sample one high-density result and one core-path result for every class used in the backlog.

Use these limits:

| Class | Evidence | Missing decision |
| --- | --- | --- |
| A1 | Text equals a declared constant | Concept ownership and dependency direction |
| A2 | Text repeats and all sites are known | Whether all sites mean one concept |
| A4 | Control flow reads error text | Whether Harmonik owns a typed producer boundary |
| B1/B2 | Logic and effects coexist | The correct pure input and output contract |
| B4 | A record is wide | Whether consumers receive unrelated fields |

Reject generated work when the text is a struct tag, CLI token, test-twin protocol value, independent wire vocabulary, or distinct domain term.
Do not schedule a bulk class because file locks are now correct.

## 6. Trace the live core path

For selected findings, inspect producers, consumers, state owners, durable writes, locks, and recovery paths.
Search current tests and active work.

For every durable transition, ask:

1. Who owns the decision?
2. Who owns the write?
3. What happens if the process stops after this write?
4. Which record is authoritative after restart?
5. Which test proves that claim and can it fail?

Prefer root boundaries over repeated symptoms.
Prefer an existing pure typed exemplar over a new abstraction.

## 7. Rank findings by architectural leverage

Use these priority meanings:

- **P0:** Work that removes duplicate authority, prevents unsafe follow-on work, or supplies a missing durability contract.
- **P1:** Work that creates the pure state machine or transaction boundary needed by the core path.
- **P2:** Work that narrows dependencies and makes the charter boundary compiler-enforceable.
- **P3:** Bounded cleanup that strengthens compiler edges but does not unblock the core.
- **Tool task:** A defect in `codebase-organism`. Do not repair it in Harmonik.

Rank queue and bead durability before daemon support code.
Rank ownership before file movement.
Rank a working vertical before generalization.

## 8. Write implementation-ready tasks

Each task must contain:

- **Problem:** one current architectural defect;
- **Scope:** exact behavior and likely owners;
- **Acceptance:** focused commands and observable claims;
- **Limits:** tempting adjacent work that must stay out.

One task should defend one architectural claim.
Split value design, durable execution, recovery, shell integration, and garbage collection when they have different failure boundaries.

Name prerequisites as dependencies.
Use priority only for preference.
Use dependencies only when one task creates a required safe input for another.

## 9. Define proof before implementation

Require the proof that matches the change:

| Change | Required proof |
| --- | --- |
| Pure decision | Fixed values produce exact results with no effects |
| Event intent | Exact type, payload bytes, and order; bus owns envelope identity and time |
| Durable transaction | Fault injection after every filesystem boundary |
| Recovery | Every supported namespace state has one idempotent result |
| State machine | Every admitted event has a total typed transition |
| Dependency split | Each consumer receives only what it reads |
| Import boundary | A lint rule rejects a forbidden import |
| Literal cleanup | Wire or serialized bytes remain identical |

Every new claim test must be observed failing under a deliberate mutation.
Negative proof must include positive evidence that the path ran.

## 10. Build execution order and stop gates

Give the implementation agent a small first assignment.
Keep the later backlog ready so work can continue after review.

Place a stop gate after each contract boundary, such as:

- value type and caller migration;
- durable transaction and recovery;
- pure policy and effect shell;
- one vertical run;
- package and import boundary.

Do not let blocked downstream work begin early.
State the no-go conditions in `EXECUTION-ORDER.md`.

## 11. Report honest limits

State which claims came from execution, static analysis, source reading, and inference.
Call out historical files in hotspot output.
Call out baseline test failures and unverified harness claims.
Do not present coverage, green tests, or a metric as proof of architecture.

## Completion checklist

The review is complete when:

- revisions and dirty state are recorded;
- the tool gates and analysis commands are recorded;
- the previous backlog is fully reconciled;
- selected findings are checked against current source;
- current findings are ranked and rejected work is explicit;
- implementation tasks have acceptance checks and limits;
- dependency order and stop gates are explicit;
- the first implementation assignment is small and unblocked;
- no Harmonik production code changed during the review.
