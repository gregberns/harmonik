# Round 1 Implementer Review — execution-model.md v0.1

## Verdict summary

The spec is implementable in broad strokes: record shapes, trailer grammar, and the checkpoint sequence are concrete enough to get Go types and a `checkpoint_and_emit` prototype flowing. It falls short on five fairly load-bearing surfaces — atomic-commit mechanics, sub-workflow expansion bookkeeping, validator entry points, traversal-cap counter location, and context/outcome plumbing around the cascade — where the implementer is forced to invent contracts. Roughly a quarter of the requirements I attempted are genuinely stuck without further detail; the rest are implementable with minor clarifications.

## Requirements I attempted to implement

### EM-012 — Run model — IMPLEMENTABLE

```go
type Run struct {
    RunID            uuid.UUID
    WorkflowID       uuid.UUID
    WorkflowVersion  string
    Input            workspace.Ref   // workspace-model §5.1
    BeadID           *string
    State            State
    StartTime        time.Time
    EndTime          *time.Time
}
```

§6.1 schema enumerates every field; EM-012 normative body matches. One ambiguity: §6.1 does not declare a `transitions` field on `Run`, but EM-012 prose says `transitions` (commit-range on the task branch). Verdict: implementable with the schema as canonical; EM-012 prose is redundant-but-harmless.

### EM-017 / EM-018 / EM-019 — Checkpoint trailers + sibling file — PARTIALLY

```go
func writeCheckpoint(run *Run, from, to State, tx *Transition) (*Checkpoint, error) {
    blob, _ := json.Marshal(tx)
    // stage .harmonik/transitions/<tx.ID>.json + workspace tree
    msg := buildMessage(tx) + "\n\n" +
        "Harmonik-Run-ID: " + run.RunID.String() + "\n" +
        "Harmonik-State-ID: " + to.StateID.String() + "\n" +
        "Harmonik-Transition-ID: " + tx.ID.String() + "\n" +
        "Harmonik-Schema-Version: " + strconv.Itoa(tx.SchemaVersion) + "\n"
    if run.BeadID != nil { msg += "Harmonik-Bead-ID: " + *run.BeadID + "\n" }
    sha, err := git.CommitAtomic(run.TaskBranch, tree, msg) // ??? see below
    ...
}
```

Stuck points:

1. EM-016 demands "atomic: tree and message land together in one `git commit` operation." Git does not give a single syscall that atomically (a) prepares the tree, (b) writes message with trailers, (c) advances ref. Standard implementations use `git write-tree` + `git commit-tree` + `git update-ref` which is atomic at `update-ref`, but the SPEC does not say this, so implementer must invent the contract.
2. EM-017 declares trailers "authoritative index is cheap; authoritative fields live in the sibling file." Fallback behavior when the sibling file is missing despite trailers being present is unspecified — implementer must choose between crash, error-return, or reconciliation dispatch.

### EM-020 — Transition record immutability — PARTIALLY

```go
// nothing to write; enforced by "never re-stage an existing transition_id.json"
```

The write side is trivially implementable by path check. The post-hoc audit tool is named but not specified: what detection rule? Walk all commits, parse all trailers, confirm every `transition_id` has exactly one sibling file at exactly one commit? Implementer is left to invent.

### EM-023 / EM-024 — One commit per successful durable transition — IMPLEMENTABLE

Straightforward: the orchestrator loop calls `checkpoint_and_emit` before advancing. EM-024 "tip of task branch = last-durable-state" falls out for free once the commit lands. The protocol pseudocode in §7.2 suffices.

### EM-026 — Reconciliation exception — PARTIALLY

```go
if run.Workflow.Class == "reconciliation" {
    // emit ONE verdict commit at the end, skip intermediate commits
}
```

Implementable mechanically, but "verdict commit" shape is declared nowhere in this spec (schema-wise the `workflow_class` string is in §6.1 Workflow record, but the verdict-commit payload is only referenced by trailer `Harmonik-Verdict-Executed` in §6.2 with no corresponding §4 requirement describing what else differs from a normal checkpoint). Implementer needs reconciliation.md §9.5b to be landable to actually ship.

### EM-031 — State reconstruction uses git plus Beads only — PARTIALLY

```go
func reconstructRun(runID uuid.UUID) (*Run, error) {
    commits := git.Log("--all", "--grep=Harmonik-Run-ID: "+runID.String())
    // walk commits, materialize last state from the tip's sibling file
    beadID := trailer(tipCommit, "Harmonik-Bead-ID")
    if beadID != "" { bead := beads.Get(beadID) }
    ...
}
```

Two gaps: (a) "walk the git checkpoint trail" does not say where to start scanning — every branch in the repo? a registry of active task branches? workspace-model §5.8 may answer, but EM-031 should say; (b) there is no requirement naming the index/discovery mechanism for ACTIVE runs. If the daemon crashed, how does it even know which task branches to scan? Implementer must invent a discovery rule.

### EM-034 / EM-035 — Sub-workflow expansion — STUCK

```go
func expandSubworkflow(parentRun *Run, subNode *Node) {
    sub := registry.Resolve(subNode.SubWorkflowRef)
    // splice sub.nodes/edges into parent run's live graph... how?
}
```

Stuck points:

1. EM-034 says "sub-workflow's nodes and edges become part of the parent run's execution graph." There is no specification of how sub-workflow node IDs are namespaced to avoid collision with parent `node_id` (parent has node `validate`, sub-workflow also has node `validate` — same ID or prefixed?). §6.1 Node record requires `node_id : String` "unique within the workflow" which breaks on expansion.
2. EM-035 requires every nested durable transition to emit a checkpoint on the parent task branch. Works mechanically, but the `state.node_id` in the transition record is ambiguous between parent and sub namespaces. Required: namespacing rule (e.g., `sub.node_id` becomes `<parent_node_id>/<sub.node_id>`).
3. EM-034's "expand in place at runtime" — before or after `pre-run validator` (§4.9)? §4.9 says validator "runs to completion before any node executes" and resolves sub-workflows "transitively." If validator statically expands, runtime re-expansion is redundant; if validator only checks resolution, runtime has to expand. The spec needs to pick.

### EM-038 / EM-039 / EM-040 — Pre-run validator — PARTIALLY

```go
type Validator interface {
    Validate(ctx, wf *Workflow) ([]ValidationError, error) // mechanism-tagged
}

func (v *validator) Validate(ctx, wf *Workflow) ([]ValidationError, error) {
    // DOT parseability — done by parser
    // sub-workflow resolution — walk wf.nodes for type=sub-workflow, recurse
    // reference resolution — for each handler_ref/policy_ref/... look up in registry
    // attribute type checks — enum match, positive ints
    // reachability — BFS from start node; BFS to any terminal from every node
    // cycle-bound check — find cycles; each must have one edge w/ traversal_cap
    ...
}
```

Implementable, but:

1. "Designated start node" (EM-038) is never declared as a field on `Workflow`. §6.1 has no `start_node_id`. Implementer has to invent the convention (first node in list? special `type=start`? attribute on node?).
2. "Terminal node" likewise undefined. Is a terminal a node with no outgoing edges? Is it a declared attribute? Edge count and declared attribute give different results for cyclic graphs.
3. EM-040 says "an agent that produces a DOT workflow MUST run the validator before submitting." What counts as "submission" to the daemon? Is there a named submit RPC? Otherwise this is unenforceable.

### EM-041 — Deterministic edge-selection cascade — IMPLEMENTABLE

§7.3 pseudocode is complete enough to compile against. Minor nit: `evaluate_condition(e.condition, run.context, outcome)` references `run.context` which appears nowhere in the §6.1 Run record. Implementer infers it's a `Map<String, Any>` but the spec is silent.

### EM-043 — Per-edge traversal cap — PARTIALLY

```go
// where do we store traversal_count per edge per run?
```

`Edge` record carries `traversal_cap` but there is no declared counter-storage. Is it a field on `Run`? A sibling file in the task branch? Re-counted from git log each time? Spec is silent; implementer must invent (and the choice affects replay semantics).

OQ-EM-004 acknowledges the multi-cycle case but does not close the counter-storage question.

## Under-specified requirements

1. **EM-016 atomic-commit mechanics.** Spec says "atomic: tree and message land together in one `git commit` operation." Needs: name the concrete git plumbing sequence (`write-tree` → `commit-tree` → `update-ref`) or state that `update-ref` is the atomicity boundary. Implementers will otherwise diverge.

2. **EM-017 trailer-vs-sibling fallback.** Spec declares trailers a "cheap index" and sibling file "authoritative." What happens when the sibling file is missing at the expected path but trailers are present? Add a normative: "MUST treat as corrupted checkpoint and dispatch reconciliation Cat-<N>."

3. **EM-020 audit tooling detection rule.** The post-hoc audit tool is referenced but its detection rule is not specified. Add: "audit tool MUST walk every commit on every task branch, parse `Harmonik-Transition-ID` trailers, verify exactly one sibling file exists per trailer value; divergence is a reconciliation flag."

4. **EM-031 active-run discovery.** State reconstruction requires walking the git checkpoint trail "for every in-flight run" — but no requirement declares how the daemon knows which runs are in-flight on startup. Add a reference to a run-registry surface (likely beads, since active beads correspond to runs) or an explicit "scan all branches matching `task/*` and read trailers."

5. **EM-034 sub-workflow node-ID namespacing.** Parent and sub-workflow may share node IDs. Add a normative rule: either "IDs are rewritten to `<parent_node_id>/<sub_node_id>` on expansion" or "collision is a validator error per §4.9."

6. **EM-034 expansion locus (validate-time vs runtime).** §4.9 says validator resolves sub-workflows transitively. Does it expand them into the parent graph, or only verify resolution? Decide and state.

7. **EM-038 start-node declaration.** `Workflow` record has no `start_node_id` field and no rule to identify the start node. Add to §6.1 Workflow record OR declare a convention.

8. **EM-038 terminal-node declaration.** Same problem: no declared rule for what makes a node terminal. Add to §6.1 Node record (`is_terminal : Bool`) or declare "node with zero outgoing edges" as the definition.

9. **EM-041 run context surface.** `run.context` is used by the cascade and by `Outcome.context_updates` but never declared in §6.1 Run. Add `context : Map<String, Any>` to the Run record.

10. **EM-043 traversal-counter storage locus.** `traversal_cap` exists; counter storage does not. Add a rule: counter lives in a sibling file on the task branch, or is re-derived from git log on each cascade evaluation, or is held in daemon memory only (with implications for restart).

11. **EM-025 failure-event emission locus without a failure commit.** If failed transitions emit no commit but MUST emit events, where do they attach to the git trail? The implementer needs a rule — likely "the last successful checkpoint's SHA is the reference" — stated explicitly.

## Type-vs-reference friction

- `WorkspaceRef` (§6.1 Run) — referenced as a type, declared to belong to `[workspace-model.md §5.1]`. Fine, but workspace-model.md is listed in the foundation set, not in this spec's `depends-on`. If workspace-model is a dep, add it; if not, declare `WorkspaceRef` inline.
- `PolicyExpression` (§6.1 Edge) — referenced as a type owned by `[control-points.md §6.5]`. Control-points is likewise not in `depends-on`. Same fix.
- `PolicyRef` (§6.1 Workflow.policies) — same issue.
- `ActionDescriptor` (§6.1 Transition.candidate_actions / chosen_action) — referenced but never declared in this spec and never cross-referenced. Where does `ActionDescriptor` live? Must name it.
- `AxisTags` and `ModeTag` (§6.1 Node) — referenced as types; architecture.md presumably owns them but the §6.1 text does not say. Annotate with cross-ref.
- `CommitRange` (§6.1 State.transition_history) — defined inline as a 2-tuple but not as a `RECORD`. Either formalize or inline-expand fields.

## Protocol / state machine gaps

§7.1 run state machine is small but serviceable. Gaps:

- No `pending → canceled` transition. An operator can stop a run before it dispatches; the table only allows cancellation from `running`.
- No `canceled` in terminal-state set for §10.1 conformance — §7.1 has it as a state but §10 does not enumerate how `canceled` differs from `failed` at test-obligation level.
- `terminal success` guard says "terminal node reached" but §4.9 validator reachability clause ("every node can reach a terminal node") does not define "terminal." Same gap as EM-038 above.
- §7.2 `checkpoint_and_emit` uses `write_transition_record` and `workspace_tree_at(run)` as undefined helpers. Either inline their contracts or declare them in §6 as `INTERFACE` methods.
- §7.3 `apply_guards` and `evaluate_gate` are called but not declared; they live in control-points.md. Add §7.3 cross-ref.

## §4–§6 consistency

- EM-003 State prose names `transition_history` as a "commit-range on the task branch." §6.1 State record has `transition_history : CommitRange`. Matches.
- EM-012 Run prose enumerates `transitions` but §6.1 Run has no `transitions` field. The State record's `transition_history` commit-range is how the info is stored. Reconcile: either drop `transitions` from EM-012 prose or rename to match the State field.
- EM-041 cascade uses `preferred_label` on the Edge (§6.1 has both `label` and `preferred_label`) — but §7.3 pseudocode only checks `e.label == outcome.preferred_label`. The edge's `preferred_label` is never consulted. Either the Edge record field is vestigial, or the cascade should check it.
- EM-044 says `rollback_to_state_id` populated when kind ∈ {architectural-rollback, policy-rollback}. §6.1 Transition comment says "set iff transition_kind ∈ {architectural-rollback, policy-rollback}" — consistent. But EM-046 adds `context-restore` as a kind that does NOT alter graph state yet still is a checkpoint — does it populate `rollback_to_state_id`? Spec is silent; the "iff" language in §6.1 says no. State explicitly.
- §6.5 Co-owned events names `transition_event`, `checkpoint_written`, etc., but §4.4/§4.6 never normatively enumerate all emission points. EM-036 covers sub_workflow events; EM-027 covers transition_event; but `state_entered` / `state_exited` / `checkpoint_written` / `run_started` / `run_completed` have no dedicated §4 requirement.

## Default-baseline Axes application

Mostly consistent. Findings:

- EM-020 declares Axes (`idempotency=idempotent`) but the content is a declaration that transition records are never rewritten — this is a declaration-only requirement per template §4.N+1 exemption. Axes line is redundant but not wrong.
- EM-031 carries Axes — correct, state reconstruction reads git and Beads (external I/O) so axes are load-bearing.
- EM-016, EM-023, EM-036, EM-038 — all load-bearing mutations/IO; Axes present and correct.
- EM-013, EM-014, EM-022, EM-025, EM-028, EM-029, EM-030, EM-033, EM-037, EM-041, EM-042, EM-043, EM-044, EM-045, EM-046 — declarations or routing rules; Axes omitted correctly per the default-baseline rule.
- Missing: EM-017 (trailer-writing) and EM-018 (sibling-file writing) describe persistent-state mutation; under the template's strict reading, Axes SHOULD be present. They are subsumed by EM-016's atomic commit, but a reviewer looking at EM-017/018 in isolation would flag missing Axes. Recommend either adding Axes lines or adding an INFORMATIVE note that EM-017/018 are sub-obligations of EM-016 and inherit its axes.
- EM-024 (invariant-shaped requirement about task branch tip) does not mutate state itself; correctly baseline.

## Recommendations

1. **Add `start_node_id` and a terminal-node rule to §6.1 Workflow/Node.** Blocks EM-038 validator implementation; cheap fix.
2. **Declare `run.context` on the Run record in §6.1.** The cascade and outcome both reference it; adding the field unblocks EM-041 and tightens EM-005.
3. **Name the sub-workflow node-ID namespacing rule explicitly in §4.8.** Pick `<parent>/<sub>` or validator-rejects-collisions; either works but the spec must pick.
4. **Add a normative requirement for the traversal-counter storage locus under §4.10.** This materially affects restart and replay semantics; it is not an editorial detail.
5. **Resolve the trailer-vs-sibling fallback in EM-017 or add a new requirement.** A corrupted checkpoint today is "undefined behavior" in the spec.
6. **Either add `WorkspaceRef` / `PolicyExpression` / `PolicyRef` / `ActionDescriptor` to `depends-on`, or cite them as co-references with explicit `[other.md §N <TypeName>]` annotations.** Currently ambiguous. `ActionDescriptor` in particular has no declared home.

## Affirmations

1. The **trailer schema table in §6.2** is exemplary — each trailer has name, type, required-flag, notes. An implementer can generate parsers from this table directly.
2. The **checkpoint-and-emit pseudocode in §7.2** correctly flags its own non-idempotency and names the guard against re-entry (`look up transition_id under .harmonik/transitions/`). Good honesty about a subtle hazard.
3. The **§8 failure taxonomy** is tightly scoped: six classes, each with detection rule, default response, escalation path, and emitted event. The ErrX sentinel-error mapping ties it to handler-contract without ambiguity. Good taxonomy-first discipline.
4. The **sibling-file-over-commit-message-only choice** is carried through consistently: EM-018 declares the path, EM-019 the retrieval contract, EM-020 the immutability, EM-021 the large-payload externalization, EM-028 names the record as canonical and the event as projection. One decision applied at five sites with no drift.
