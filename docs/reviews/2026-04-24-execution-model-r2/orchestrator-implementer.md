# Round 2 Orchestrator-Implementer Review — execution-model.md v0.2

Lens: I am writing S01 Orchestrator Core in Go against this spec as a contract. Can I build the main loop end-to-end without inventing load-bearing contracts? This review is distinct from round 1's per-requirement lens — I treat the spec as a whole and sketch five S01 responsibilities against it.

## Verdict

v0.2 is materially better than v0.1 on the atomic-commit plumbing (EM-016), durability decision table (EM-023a), active-run discovery (EM-031a), sub-workflow namespacing (EM-034a), and corrupted-checkpoint fallback (EM-017a). The five-hop protocol in §7.2 plus the cascade in §7.3 are close to drop-in pseudocode. But the **end-to-end orchestrator main loop is still not expressible as a closed function against only this spec**. Three gaps force invention at S01 build time: (1) the `pick_one → create_run → dispatch` prefix (bead-to-run wiring, handler.Launch plumbing into `checkpoint_and_emit`) has no single owning section; (2) the terminal-state decision procedure (run_completed vs run_failed vs stay-in-running) lacks a single "when does a run end" requirement; (3) node dispatch between `select_next_edge` returning an Edge and the next `checkpoint_and_emit` firing — the inner loop step 1–5 from core-scope §5 — has no protocol analog in §7. An implementer can bridge these with cross-reading, but the spec does not own the shape of its own main loop. Recommend one new §7 protocol block (run main loop) and tightening of three cross-refs.

## Main-loop sketch

```go
// S01 orchestrator main loop — what the spec demands I write.
func (o *Orchestrator) Run(ctx context.Context) error {
    // Covered by PL-005 (startup) + EM-031a (active-run discovery). OK.
    active := o.discoverActiveRuns()            // EM-031a: Beads non-terminal ∪ branch-trailer scan
    for _, run := range active {
        o.resumeRun(ctx, run)                   // <-- no EM requirement defines this entry
    }

    for {
        // core-scope §5 prescribes this loop; EM owns none of it.
        beads := o.beads.Query(ReadyWork{})     // BI-013; not cited from EM §4.3
        if len(beads) == 0 { o.idle(ctx); continue } // PL-013 (sleep on empty)
        bead := pickAny(beads)                  // "any, oldest-as-tiebreak" — not in EM
        wf   := o.resolveWorkflow(bead)         // bead.workflow_name → DOT — not in EM
        if err := o.validator.Validate(wf); err != nil { /* EM-038 */ continue }
        run := o.createRun(bead, wf)            // EM-012 declares Run shape, not creation
        o.emit(RunStarted, run)                 // §6.5 lists; §4 never mandates WHEN
        o.executeWorkflow(ctx, run)             // <-- see next section
        o.onTerminal(ctx, run)                  // <-- see gaps §1-2
        o.checkOperatorSignals(ctx)             // operator-nfr §7.3 (between-runs only)
    }
}
```

What the spec covers cleanly: EM-031a for startup active-run discovery; EM-038/EM-040 for validation obligations; EM-012 for Run record shape; §7.1 run state machine for the outer state progression; EM-INV-004 for the no-transactionality posture; EM-025's "reference `last_checkpoint` SHA on failure event" tells me what to stash before `handleFailure`. What the spec does NOT cover: `resumeRun` (re-entering an in-flight run post-restart after reconciliation has classified it — reconciliation.md handles classification, execution-model silent on re-entry); `pickAny` policy ("oldest is a tiebreaker, not a priority" lives only in core-scope §5 — but core-scope is not normative); `resolveWorkflow` (bead→DOT binding is asserted in core-scope §5 but no EM or BI requirement nails down the field name on the bead or the registry lookup); `createRun` (nothing says who allocates `run_id`, at what ordering point, whether the run_id is persisted before the first checkpoint, and what the daemon does if it crashes between allocation and first durable transition). These are all foundational to S01 and must be written by reading four specs in parallel.

### execute_workflow — inner loop

```go
func (o *Orchestrator) executeWorkflow(ctx context.Context, run *Run) {
    for {
        node := o.currentNode(run)
        if o.isTerminal(node) { break }              // §6.1 terminal_node_ids — but no EM req on detection
        outcome, tx := o.dispatchNode(ctx, run, node)// handler-contract §4.1; EM owns none of dispatch
        if isDurable(tx.Kind, outcome.Status) {      // EM-023a — GOOD
            cp := o.checkpointAndEmit(run, ..., tx, outcome) // §7.2 — GOOD
            run.State = cp.StateID
        } else if outcome.Status == FAIL {
            o.handleFailure(run, outcome, tx)        // §8 classes — tabular but no protocol
            return
        } else if outcome.Status == RETRY {
            continue                                  // EM-015 intra-run loop
        }
        edge := o.selectNextEdge(run, outcome)       // §7.3 — GOOD
        if edge == nil { o.failRun(run, NoMatchingEdge); return } // no class for this!
        run.CurrentNodeID = edge.ToNode
    }
}
```

§7.2 and §7.3 are strong. The gap: **between cascade-returns-edge and the next node's dispatch, there is no protocol segment** describing advance bookkeeping (when does `run.State` actually update? Before or after the edge traversal counter increments? When is the traversal cap checked — EM-043 says "at cap" but EM-043a locates the counter both in memory and in git). §7.3 returns `FAIL(class=compilation_loop)` inside the cascade, which conflates edge selection with transition failure emission — the caller now has two return shapes to handle.

## Sub-workflow sketch

```go
func (o *Orchestrator) expandSubWorkflow(run *Run, node *Node) *Outcome {
    child := o.resolveSubworkflow(node.SubWorkflowRef, run.WorkflowVersion) // EM-034 load-time pin
    for _, n := range child.Nodes {
        n.NodeID = node.NodeID + "/" + n.NodeID   // EM-034a namespacing
    }
    o.emit(SubWorkflowEntered, run, node.NodeID) // EM-036
    // Execute inline within run's task branch — EM-035 parent's checkpoint trail
    savedNodeID := run.CurrentNodeID
    run.CurrentNodeID = node.NodeID + "/" + child.StartNodeID
    var lastOutcome *Outcome
    for !o.isChildTerminal(run, child) {
        outcome, tx := o.dispatchNode(ctx, run, o.currentNode(run))
        if isDurable(tx.Kind, outcome.Status) {
            o.checkpointAndEmit(run, ..., tx, outcome)  // EM-035: on PARENT branch
        }
        run.CurrentNodeID = o.selectNextEdge(run, outcome).ToNode
        lastOutcome = outcome
    }
    o.emit(SubWorkflowExited, run, node.NodeID, lastOutcome) // OQ-EM-007 unresolved
    run.CurrentNodeID = savedNodeID
    return lastOutcome  // <-- spec never names this return shape
}
```

Parent → child data flow: spec says the sub-workflow expands in place and the parent `run_id` and task branch are the sole identifiers (EM-034, EM-035). Context is implicitly shared via `Run.context` — but that is not explicitly called out for sub-workflows (compare EM-041a context-update ordering for ordinary nodes). The sub-workflow has access to the parent's context; whether it can mutate it visibly to the parent is undeclared. Data-flow ownership across the expansion boundary is missing.

**Child → parent return value is unspecified.** The sub-workflow's terminal outcome is OQ-EM-007. The parent node's `Outcome` (used for the cascade leaving the sub-workflow node, via EM-041 condition expressions and `preferred_label`) is not declared — presumably it is the last expanded-node's Outcome, but §4.8 does not say so and `select_next_edge` at the parent's outgoing edges needs one. This blocks S01 from wiring the cascade across the sub-workflow boundary: without knowing the Outcome shape that escapes a `sub-workflow` node, edge conditions on outgoing edges cannot be evaluated.

Additionally: EM-034 pins sub-workflow version at workflow-load time. S01 needs a workflow-load function that resolves the full transitive sub-workflow graph and freezes versions before `executeWorkflow` begins. No EM requirement names that function, its return shape, or its idempotency properties. Is the expanded graph materialized once and cached, or re-resolved per expansion? EM-034a says "expansion is a runtime operation performed by the daemon after the pre-run validator completes" — but does not say whether the daemon can expand lazily (first time the sub-workflow node is visited) or MUST expand eagerly at run-start. For a sub-workflow behind a rarely-traversed edge, the difference is real.

## Gaps

1. **No "run terminates" requirement.** §7.1 has a `running → completed` row guarded by "node in `terminal_node_ids` reached with `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}`" but §4 has no EM-NNN encoding that guard. EM-033 forbids rollback on later failure but is silent on how the orchestrator decides the run is done. This is load-bearing for S01: it is the condition in the outer for-loop's break.

2. **No `run_started` / `run_failed` emission requirement.** §6.5 lists these events as co-owned, but no EM-NNN says "the orchestrator MUST emit `run_started` when…" — only the §7.1 state table implies it. Without a normative emission requirement, the test obligation in §10.2 EM-012–EM-015 cannot be proved.

3. **Failure handling has a taxonomy but no protocol.** §8 is a table; §7.1 shows transitions; but the orchestrator's `handleFailure` function — attempt caps for `transient`, escalation to `structural` after cap exhausted, where `last_checkpoint` reference comes from in-memory state — has no protocol analog. An implementer reading §8.1's "reclassify as `structural` and re-evaluate" must invent the retry-counter locus, the re-classification trigger, and the re-evaluation entry point.

4. **`select_next_edge` returning `None` is a hole.** Pseudocode at §7.3 line "IF not matched: RETURN None — no edge matches; run enters failure state per §8". §8 lists six failure classes; none of them is "no outgoing edge matches". This is a real failure mode (policy-expression edits that make all edges false against current context). Implementer has to pick a class (probably `structural`?) with no guidance.

5. **Edge from `selectNextEdge` to `dispatchNode` is not owned by any protocol.** §7.3 ends with a chosen edge; §7.2 begins with a transition already in hand. The advance step (move run to `edge.to_node`, load that node's handler_ref, prepare the handler's input) has no pseudocode and no EM requirement. Core-scope §5 "Node execution" 5-step list covers it informally, but core-scope is not normative.

6. **Reconciliation's re-entry into the loop.** EM-031a tells S01 how to discover in-flight runs at startup. It does NOT tell S01 how a reconciled run re-joins the main loop. A Cat 1 resumption and a Cat 5 rerun have different branch-and-run states (per reconciliation.md §9.3 per [reconciliation.md §9.2a]); S01 needs a protocol for "I received a reconciliation verdict, now continue the run". Cross-ref exists but the entry-point shape is invented.

7. **`createRun` ordering and `run_id` persistence.** EM-013 says `run_id` appears as trailer on every checkpoint and as event field. But the first event (`run_started`) fires BEFORE the first checkpoint, so `run_id` must exist before any durable transition. Where is it persisted between allocation and the first checkpoint? In Beads? In an intent log? Undeclared. If the daemon crashes between `run_started` emission and the first checkpoint, the run is unrecoverable without a persistence point.

8. **Traversal-counter reconstruction cost.** EM-043a says the authoritative count is "re-derived from git log" on restart. For a run with N prior transitions, this is O(N) git-show operations per edge. No performance floor is declared, and the counter feeds the cascade's cap check. For pathological loops this is a repeated cost. Acceptable at MVH but the implementer should be told (a non-binding NOTE would suffice).

9. **`workflow_class = reconciliation` exception leaks.** EM-026 exempts reconciliation workflows from EM-023's one-commit-per-durable-transition. S01 must branch on `workflow_class` at every `checkpoint_and_emit` call site. The condition is one-liner but the protocol in §7.2 does not show the branch. An implementer reading §7.2 literally would over-commit reconciliation workflows.

10. **Gate denial return shape.** §7.3 line: `IF gate_verdict == deny: RETURN STAY(current_state)`. What does "STAY" mean for the outer loop? No checkpoint, no state change, no next-node dispatch — but does the run poll? Enter an operator hold? Re-run the cascade? EM-042 says "no checkpoint is written" but not what the orchestrator does next. An infinite loop is possible if the gate is a function of context that cannot change without a handler firing.

## Cross-reference audit

Checked every `[spec.md §N]` cite in §9.1 and §9.3 against the corresponding spec:

- `[beads-integration.md §10.2]` — cited in EM-031; beads-integration uses §4.1–§4.12, no §10. Probably means §4.5 (read queries). **Broken cite.**
- `[beads-integration.md §10.3]` — cited in EM-014, glossary (bead), §6.1 (Run.bead_id); beads-integration bead-ID rule is at BI-008 (§4.3). **Broken cite.**
- `[beads-integration.md §10.4]` — §2.2; no §10.4. **Broken.**
- `[beads-integration.md §10.6]` — §4.3.EM-013 and §6.2; the bead-ID trailer rule is at BI-018 (§4.6). **Broken.**
- `[beads-integration.md §10.7]` — EM-INV-005; no §10.7. **Broken.**
- `[beads-integration.md §10.8]` — §2.2 + §9.3; actually §4.10 (intent log). **Broken.**
- `[beads-integration.md §10.8a]` — §2.2; no such section. **Broken.**
- `[beads-integration.md §10.9]` — not cited here but in process-lifecycle; the skill-injection is BI-028 (§4.11). **Broken in neighbor spec.**
- `[reconciliation.md §9.x]` — reconciliation is a split spec under `specs/reconciliation/`. I did not open each sibling; citations to §9.1a/§9.2/§9.2a/§9.3/§9.3a/§9.5/§9.5b should be verified. **At-risk.**
- `[workspace-model.md §5.x]` — not audited here but five cites to §5.1/§5.4/§5.8/§5.9 occur.
- `[control-points.md §6.x]` — eleven cites; not audited.
- `[handler-contract.md §4.x]` — seven cites; not audited.

**Net:** at least eight cites into beads-integration are wrong (use `§10.x` when the spec uses `§4.x` with `BI-NNN` IDs). These need a find-and-replace before `reviewed`.

## Types used before defined / unclear

- `ActionDescriptor` — §6.1 defers to handler-contract §4.1. Carried as OQ-EM-005. Fine as deferred.
- `PolicyExpression` — same shape, deferred to control-points. Fine.
- `OutcomeStatus` RETRY — EM-023a says RETRY transitions are not durable. But the cascade (§7.3) only runs after a durable transition. So a RETRY outcome → no checkpoint → is the cascade rerun against the same state? EM-015 implies yes (intra-run loops). But the *handler dispatch* then runs again on the same node with presumably new context. The retry-decision protocol is absent.
- `Outcome.context_updates` — EM-041a pre-cascade ordering is clean. But what if the outcome is FAIL? Are `context_updates` applied? §4.6.EM-027 says the flow is "integrated" and each segment produces the next's input, which could be read as "FAIL ends the flow before context_updates apply". Not stated.
- `Transition.from_state` / `to_state` — §6.1 declares them as full `State` records embedded in the transition. That duplicates state data across transitions. Implementer must decide whether to embed or to store state_id references; §6.1 literal says the full record.

## Recommendations

Priority ordered.

1. **Fix the beads-integration cites.** All `§10.x` → `§4.x` plus the BI-NNN IDs. This is mechanical.
2. **Add §7.4 "Run main loop" protocol pseudocode** covering `create_run`, the outer loop structure, the `dispatchNode` → `checkpoint_and_emit` → `select_next_edge` → advance cycle, and the terminal-state exit condition. Cross-reference EM-NNN at every branch. This closes gaps 1, 2, 5, 7 in one move.
3. **Add EM-NNN requirements for run lifecycle emissions.** At minimum: `run_started` emission point, `run_completed` emission point, `run_failed` emission point with failure-class reference. §6.5 lists them; §4 should own their WHEN.
4. **Add an EM requirement for "no matching edge" failure class**, or amend §8 to add a seventh class (or pick one of the six and say so). This is real and reachable via policy edits.
5. **Clarify gate-deny continuation.** If gate returns `deny`, does the orchestrator wait for an external signal, retry the cascade after a timeout, or fail the run? EM-042 needs one more sentence.
6. **Define the sub-workflow terminal-outcome composition** — resolve OQ-EM-007 before `reviewed`. Without it, cascade at the parent sub-workflow node's outgoing edges has no Outcome to condition on.
7. **Name `run_id` persistence locus.** Even a two-line requirement: "the `run_id` is persisted to the in-memory active-run table and to Beads (via the `claim` terminal transition) before `run_started` emits" closes gap 7.
8. **Add a NOTE on traversal-counter reconstruction cost** or move the counter to a non-authoritative cache with git as the reconstruction source only on restart. Current shape forces re-derivation per dispatch.
9. **Branch the §7.2 pseudocode on `workflow_class = reconciliation`**, or add a one-line comment in the protocol that calls out EM-026's exception. Currently §7.2 reads as if every run commits every durable transition.
10. **Clarify RETRY handling.** Add a short protocol block or a requirement extending EM-015: "A RETRY outcome causes the daemon to re-dispatch the same node against the run's current state; the `context_updates` map of the RETRY outcome MUST be applied pre-redispatch per §4.10.EM-041a." This makes the retry loop explicit.

## Affirmations

What v0.2 gets right from the S01 perspective:

- **EM-023a durability decision table** is a clean mechanical rule. S01's `isDurable(kind, status)` is one line.
- **EM-016 atomic-commit plumbing** (write-tree / commit-tree / update-ref) is exactly the Go-implementer's mental model. The atomicity boundary (`update-ref`) is explicit.
- **EM-017a corrupted-checkpoint fallback** closes the round-1 hole on trailer-vs-sibling disagreement.
- **EM-018a UUIDv7 globally-unique** is the right call; it makes the un-scoped sibling path safe under cross-run merges without cascading path changes.
- **EM-020a audit-tool detection rule** is concrete enough to write. Four integrity-violation categories, clear trailer-vs-file mapping.
- **§7.2 checkpoint-and-emit pseudocode** maps directly to Go. The in-pseudocode emit order (state_exited → transition → checkpoint_written → state_entered) is what the event-model and reconciliation detectors read.
- **§7.3 cascade pseudocode** is implementable. Guards-precede / gates-follow is unambiguous.
- **EM-031a active-run discovery** covers the startup case cleanly.
- **EM-043a traversal-counter storage** names both the memory and git side and picks git as authoritative — correct, if costly.
- **§6.1 typed ID aliases** make downstream specs citable without ambiguity.
- **EM-INV-004 "no workflow-level transactionality"** is a load-bearing architectural constraint and is crisply stated.

A spec of 909 lines that an S01 implementer can get ~80% of the way through without inventing contracts is a strong result. The remaining 20% is where the end-to-end loop lives, and is the one non-individual-requirement gap left for v0.3.

---

Line count: 181.
