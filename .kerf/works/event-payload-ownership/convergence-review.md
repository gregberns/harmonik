# Workflow Convergence Review — 2026-08-01

Three independent reviews agree:

1. Code topology: the single tail is a second executor. It can become a
   no-review DOT graph, but its remaining behavior needs a staged migration.
2. Plan and specification intent: DOT is the normal default, review-loop is
   retired, and Step 7 already directs the single-tail deletion.
3. Event analysis: neither former event option supplies a truthful workflow
   identity. The descriptor must exist before event emission and graph dispatch.

Result: accept the workflow-descriptor and one-DOT-executor direction. Do not
amend the event specification down to the current partial JSON. Do not generate
a workflow identifier only to satisfy the current core type.

## Independent integration review

2026-08-01, fresh-context review verdict:

```json
{"schema_version":1,"verdict":"APPROVE","notes":"The plan matches the adopted lane directive and current contracts. It resolves the descriptor before run_started, uses the descriptor for DOT execution and durable decoding, makes no-review an explicit audited graph selection, and removes the single tail only after behavior disposition. Alpha-only implementation ownership remains explicit."}
```

This approves the planning direction. It does not approve a final spec
amendment or authorize Alpha-owned production edits.

## 2026-08-02 published-spec recheck

The three independent rechecks converge on the published logical
`WorkflowID` contract. Accept it. A DOT-declared, validated logical ID is one
identity for the descriptor, durable start event, replay, and DOT executor.
It does not use a random, file-derived, graph-name-derived, or hash-derived
ID. It keeps legacy UUID text readable.

The reviewers compared this with a UUID-only identity. They found that UUID
would require a permanent graph-to-UUID mapping and would rewrite the declared
graph corpus. The bounded type migration is the lower-risk design. The
implementation must validate the named-ID grammar, remove UUID-only casts and
nil checks, keep N-1 event readers, and carry the selected value unchanged.

```json
{"schema_version":1,"verdict":"APPROVE","notes":"Accept the committed logical-ID contract. It has the lower complete migration cost. The DOT corpus already uses named workflow_id values, while dot.Graph does not yet parse that attribute. A UUID-only contract would rewrite shipped graphs and make source identity opaque. Widening core.WorkflowID changes a bounded set of type checks and fixtures, while JSON remains a string and the named-ID grammar preserves UUID legacy values without conversion."}
```

```json
{"schema_version":1,"verdict":"APPROVE","notes":"Accept the logical WorkflowID contract. It makes the DOT-declared named identity, Run record, run_started payload, replay value, and descriptor one value. It matches 36 of 37 committed DOT workflow_id values and avoids a permanent graph-to-UUID translation boundary."}
```

```json
{"schema_version":1,"verdict":"APPROVE","notes":"Accept the published contract. WG-055 makes workflow_id required, validates its grammar, and forbids filename or DOT-name fallback. UUIDs would retain a false run-like model and require mapping across the graph corpus. Keep legacy UUID text readable."}
```

`304cfe395` published the five reviewed drafts. A byte comparison confirmed
that every published file matches its Kerf draft. `kerf square` passes. The
next work is Alpha-owned implementation. This record grants no authority to
change production files.
