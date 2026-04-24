# Round 2 Crash Adversary Review — control-points.md v0.2

## Scope and stance

This is a crash-adversary review. Assume the daemon dies at the worst possible moment — mid-loop, mid-write, mid-emit — and ask whether the spec tells a consistent story on restart. Targets are the six failure surfaces where v0.2 introduced new mechanism that did NOT exist in v0.1: the two-pass registration sequence (§7.1), the envelope-hash replay gate (§4.8.CP-040a/041), the harmonik-level evaluation-cost ceiling (§4.7.CP-034b), skill-set declaration (§6.3), Budget counter internality (§4.5.CP-026), and the Hook dispatch loop boundary to S05 (§4.3).

Four scenarios selected as load-bearing; two (skill-set load mismatch, Hook dispatch mid-chain) are lower severity and sketched together in §4.

## Scenarios reviewed

1. Registry populated from YAML crashes mid-registration (§1).
2. Hook dispatch loop fails mid-callback chain (§4, shared with §5 sketch).
3. Persisted Gate verdict survives crash, envelope changed on replay — Cat 6 triggers? Detection-before-downstream? (§2).
4. `policy_expression_exceeded_cost` on cognition evaluator — cost-tracking-in-progress crash (§3).
5. Skill-set YAML disagrees with registry at load — conflict resolution (§4, shared).
6. Budget counter crosses crash boundary — rehydrated from what? (§5).

---

## 1. Registry populated from YAML crashes mid-registration

### The scenario

§7.1 defines a two-pass registration: Pass 1 validates every document and builds a symbol table; Pass 2 resolves cross-refs and calls `s02.registry.Register(cp)` one ControlPoint at a time. Suppose the daemon dies during Pass 2 after registering 200 ControlPoints out of 350 — OOM, SIGKILL from supervisor, panic in `construct_control_point`. No `control_points_registered` event was emitted (the emit is after the loop).

### What the spec says about recovery

Nothing directly. CP-045 says the registry is daemon-local and "daemon restart rebuilds the registry from policy YAML as a startup step." That is the recovery story: a fresh process, a fresh `s02.registry`, another full two-pass walk. No residual half-registered state is expected because the registry is in-process-only.

### Where the crash story still holds

The registry itself is ephemeral (in-process Go map), so the classic "half-written index file" failure mode does not apply. Daemon restart is a total reset of registry state; CP-045 closes this cleanly.

### Where the crash story does NOT hold

Four gaps.

**Gap 1-A. Side-effects of partial registration are not bounded.** §7.1 calls `construct_control_point(cp_decl, symbol_table)` inside the Pass 2 loop. The spec does NOT say `construct_control_point` is pure. If a ControlPoint's construction emits an event (e.g., a diagnostic `control_point_registered` event, not currently in §6.5 but easy to add), fires a validation Hook via the event bus, or writes any audit record, then 200 ControlPoints' side-effects are durable while the other 150 are not. The post-crash restart will re-run all 350 constructions, producing duplicate side-effects for the first 200 unless each side-effect is made idempotent. The spec is silent.

**Gap 1-B. `control_points_registered` emission is at end of function, not inside the loop.** §7.1's final line is `emit_event("control_points_registered", count=...)`. A crash at ControlPoint 200-of-350 produces NO completion event. Downstream consumers (any subsystem that waits for "registration is done" before subscribing) have no signal that registration failed — they see only absence. The spec should either require a companion `control_points_registration_started` event with a correlation ID so absence of `...registered` can be diagnosed as a crash, OR document that subsystem startup is sequenced (S02 registration completes, then S05 subscribes; no subsystem is permitted to consume registry state mid-registration). Neither is stated.

**Gap 1-C. Pass 1 / Pass 2 boundary is a correctness hinge but not a durability hinge.** §7.1's two-pass structure was added in v0.2 to resolve cross-document references per OQ-CP-005. But the symbol table built in Pass 1 is in-memory only. A crash between Pass 1 and Pass 2 discards the symbol table — harmless, since Pass 1 is pure. A crash DURING Pass 2 discards both the partial registry AND the in-memory symbol table. Restart rebuilds both. This is fine IF AND ONLY IF the policy YAML on disk is unchanged between crash and restart. The spec does not pin this assumption. If an operator edits policy YAML between the crash and the restart, the restart registers a different registry than the one that was partially built — which is semantically correct (next-restart wins) but should be stated explicitly because it is a surprise waiting to happen for anyone reasoning about crash atomicity.

**Gap 1-D. Divergent-body detection (§4.9.CP-044) is cross-process, not intra-process.** Re-registration under an identical name with divergent body fails at startup. But the divergent-body check is against the CURRENT in-process registry. After a crash the in-process registry is empty, so a YAML edit between crash-and-restart that produces a "divergent body" registration will NOT fail as divergent — it will register as the new body. If the old body's verdicts are persisted on the run's task branch per §4.8.CP-040, the replayer on the next run will be running against a silently-replaced ControlPoint. The spec provides no persisted registry snapshot to cross-check post-crash state against pre-crash state; divergent-body protection is intra-process only. The envelope-hash mechanism (§4.8.CP-040a) catches this at the verdict boundary via §4.8.CP-041, but only for cognition-tagged evaluators; a mechanism-tagged Gate whose `expression` was silently changed between crash and restart produces no envelope-mismatch signal because its verdict is not persisted.

### Severity

Gap 1-A is implementer-blocking (construct-side-effect purity must be specified). Gap 1-B is operationally blocking (you cannot diagnose a registration crash without the start event). Gap 1-C and 1-D are clarifications rather than bugs; 1-D deserves an explicit note because it is the failure mode the rest of the spec's replay-safety story assumes away.

---

## 2. Persisted Gate verdict survives crash; envelope changed on replay

### The scenario

A cognition-tagged Gate fires, produces a verdict, persists it to the run's `Transition.evidence` via §4.8.CP-040. The daemon then crashes. On restart, the run is resumed. Between crash and restart, the run's `context` map changed (e.g., a Hook fired during the last moments before the crash and the state-mutation side-effect is now durable), or a skill-package version pinned in `input_envelope_hash` was upgraded by the operator, or the policy YAML's `prompt_template_ref` version was bumped. The replayer at §7.2 recomputes the envelope hash and it no longer matches.

### What the spec says

§4.8.CP-041 is explicit: the replayer MUST emit `verdict_envelope_mismatch` and escalate to a Cat 6 reconciliation verdict path per [reconciliation.md §4.2]. Cat 6 is the only authority that can authorize re-invocation. CP-INV-003 reinforces: any replay path that silently re-invokes a model violates the invariant.

§7.2's pseudocode matches: on hash mismatch, emit the event, call `escalate_to_cat_6`, return the Cat 6 verdict.

### Does the spec guarantee detection before any downstream action?

Partially. The guarantee is structural at the Gate-verdict site: `invoke_cognition_evaluator` returns ONLY after either (a) hash-match + read, (b) no existing verdict + dispatch + persist, or (c) hash-mismatch + Cat 6 escalation. Control does not flow past the function until one of these three branches returns. So the Gate invocation site is safe — the Gate cannot allow a transition to advance while an unresolved envelope mismatch is pending.

BUT the guarantee is LESS clear at three adjacent sites.

**Site A — Hook verdicts are persisted to a task-branch file path (§4.8.CP-040: `.harmonik/hooks/<run_id>/<hook_invocation_id>.json`), not into a Transition's evidence field.** On replay, a Hook's persisted verdict is read from git. The spec does not state that Hook replay follows the same pseudocode as §7.2 — §7.2 is labeled "Cognition-tagged evaluator invocation with persisted verdict" and is general, but §4.8.CP-040's path description and §4.8.CP-041's `{transition_id | event_id}` key both suggest a symmetric path. Neither §4.3 (Hook semantics) nor §4.8 explicitly wires Hook replay through `invoke_cognition_evaluator`. An implementer could read §4.3.CP-012 ("the evaluator receives the matched event and subscription context") literally and invoke a cognition-tagged Hook evaluator directly on replay without a hash check. CP-INV-003 catches this (it applies to "every cognition-tagged evaluator invocation") but the concrete wiring in §7.2 is presented only for Gates. The spec should state explicitly that §7.2 is the sole path for cognition-tagged evaluator invocation across all Kinds.

**Site B — The envelope-hash input list at §4.8.CP-040a includes "skill-package versions provisioned at the originating handler launch."** Skills are provisioned per [handler-contract.md §4.11] at agent launch, and `skills_provisioned` is emitted per §4.11.CP-050. If the handler that launched the agent is NO LONGER RUNNING at replay time (the agent completed, its process is gone), how does the replayer know which skill versions to include in the recomputed hash? The spec says the hash is "computed over... skill-package versions provisioned at the originating handler launch" — i.e., the ORIGINAL versions, not the current versions. This is correct (replay must use historical inputs), but it requires the `skills_provisioned` event's payload to be consulted as the source of truth for "what versions were provisioned." The spec does not state this. If the replayer reads the CURRENT skill package versions from disk, the hash will mismatch whenever any skill has been upgraded, triggering Cat 6 escalation on every replay after any skill upgrade — which is correct behavior but not what the spec implies (it implies "hash matches when the ORIGINAL envelope is unchanged"). The envelope-construction algorithm needs pseudocode; the current prose is ambiguous.

**Site C — The replayer reads persisted verdicts "from git" (§7.2). If the task branch has been rebased, merged, or had its history rewritten between crash and replay, the verdict file's path or content-hash may change without the verdict itself changing semantically.** This is a workspace-model concern, but the crash-adversary angle is: a crash during a branch operation (e.g., the handler was mid-commit when the daemon died) may leave the verdict file in an intermediate state. The spec does not specify whether `read_persisted_verdict_from_git` must succeed-or-fail atomically, or whether it may observe a partial-write.

### Cat 6 escalation triggers correctly

For the CORE case (Gate, envelope changed, persisted verdict intact), yes — the spec is clear and the pseudocode matches the invariant.

### Severity

Site A (Hook replay path under-specified) is implementer-blocking. Site B (envelope-construction ambiguity over "originating" vs. "current" skill versions) is implementer-blocking. Site C (git read atomicity) can be deferred to workspace-model but should be cross-referenced.

---

## 3. `policy_expression_exceeded_cost` with cost-tracking-in-progress crash

### The scenario

A cognition-tagged Gate's `subscription_filter` (or a mechanism-tagged expression at any site) is evaluated under the harmonik-level cost ceiling per §4.7.CP-034b (`expr.MaxNodes` + `expr.Timeout`). Suppose the timeout fires while the evaluator is mid-expression. The spec says the evaluator MUST abort with `ErrDeterministic` and emit a `policy_expression_exceeded_cost` event.

Sub-case: the daemon crashes BETWEEN the timeout trigger and the event emission — i.e., the expression aborted but the event was never written to the event bus.

### What the spec says

§4.7.CP-034b: "A pathological expression that would otherwise stall evaluation MUST abort with a typed `ErrDeterministic` per [handler-contract.md §4.5] and emit a `policy_expression_exceeded_cost` event."

§6.5 lists the event. [event-model.md §3.2] (referenced but not read here) owns the payload shape.

### Where the crash story does NOT hold

**Gap 3-A. The spec pairs "abort with ErrDeterministic" and "emit event" as conjoined obligations but does not specify atomicity or ordering.** If the abort happens first and then the event emit, a crash between them loses the event. If the emit happens first and the abort follows, a crash between them could leave the run in a "we told the world we aborted but we are still in the evaluator" state — impossible in a single-threaded evaluator, but possible if the event emit is routed through a goroutine (§4.10.CP-048 says effects cross boundaries ONLY via events; the event bus is plausibly async). The spec should state: the abort and the event emission are a durability pair; the event MUST be durable (written to JSONL per event-model's durability contract) BEFORE control returns from the evaluator wrapper. Otherwise crash recovery is ambiguous — does the replayer see an aborted-but-silent evaluator and re-invoke it? CP-INV-003 prohibits silent re-invocation for cognition-tagged evaluators, but a mechanism-tagged expression has no such invariant; a pathologically-expensive mechanism-tagged Gate expression whose abort-event was lost to the crash could be re-evaluated on replay with the same pathological input and crash the daemon again. This is a liveness concern, not a correctness concern, but it means the spec needs an abort-event durability obligation before the evaluator returns.

**Gap 3-B. Cost-tracking state is not durable.** The evaluator maintains an internal node-count or timer while evaluating. On a mid-evaluation crash, this state is lost. Restart has no memory that the last evaluation was expensive; if the policy YAML is unchanged and the same expression is evaluated on the same input, the pathological cost recurs. The spec does not require any short-circuit record of "this expression exceeded cost on run X, refuse to re-evaluate without operator action." For mechanism-tagged expressions this is probably acceptable (the cost ceiling catches it again), but for a replay scenario where the daemon keeps crashing on the same expression, the operator has no signal to intervene other than observing the crash loop. A persisted `expression_poison_pill` marker keyed on `(expression_hash, input_hash)` would close this, but is out of v0.2 scope.

**Gap 3-C. For cognition-tagged Gates, the cost ceiling bounds the EXPRESSION evaluator (a subscription filter or evaluator expression), not the model call.** §4.7.CP-034b is explicitly about `expr-lang/expr` evaluation. A cognition-tagged Gate's cost model is entirely separate — tokens (§4.5 Budget), wall-clock (§4.5 Budget), model timeout (not specified in this spec). A crash during a cognition Gate's model call is covered by §4.8's persisted-verdict story (no verdict persisted = re-invoke on replay), but a crash during the `expr` filter that SELECTS which Gate to run is not — the expression's abort + event pair has the Gap 3-A durability hole. This is a seam between the two cost-ceiling stories.

### Severity

Gap 3-A is spec-blocking (abort + event durability pair must be explicit). Gaps 3-B and 3-C are notes for the open-questions block, not v0.2 blockers.

---

## 4. Hook dispatch mid-chain crash + skill-set YAML disagrees with registry

### Scenario 2 — Hook dispatch loop fails mid-callback chain

Five Hooks are ordered for a single event per §4.3.CP-014 (subsystem priority + declaration order). Hook 3 is mid-execution when the daemon dies. Which Hooks ran? §4.3.CP-016 says Hook dispatch is owned by S05 and "this spec... does NOT define the dispatch loop." §8 says `halt_on_failure = true` maps Hook errors to `ErrTransient` or `ErrDeterministic`. Neither addresses the crash case.

The crash-recovery obligation lives in S05's subsystem spec (not yet written). This spec is thus correct to be silent IF the ownership boundary is stated clearly. It is — §4.3.CP-016 defers dispatch-loop mechanics. But the spec SHOULD name, even as a co-ownership boundary note, whether Hooks are expected to be idempotent (re-run from the start on replay) or whether partial-chain progress is persisted so replay resumes mid-chain. The §4.8.CP-040 Hook-verdict persistence path (cognition-tagged Hooks only) gives replay-safety to cognition Hooks but says nothing about mechanism-tagged Hook idempotency. Implementers will reasonably assume mechanism Hooks are idempotent (they are pure expressions producing side-effect descriptors), but the side-effect APPLICATION (state mutation, external action per §6.1.6) is a different story. A crash after Hook 3's evaluator produced a side-effect descriptor but before the S05 dispatcher applied it loses the side-effect. On replay, if the Hook's evaluator runs fresh and produces the same descriptor, the side-effect is applied once and all is well. If the side-effect application is persisted but not the evaluator's having-run flag, the side-effect is applied twice. This is an S05 concern; this spec should state "side-effect application semantics are owned by S05 and MUST bound at-least-once vs. at-most-once explicitly" to close the handoff.

### Scenario 5 — Skill-set YAML disagrees with registry at load

§6.3 adds a `skill_sets[]` block to policy YAML. §4.11.CP-049 references it ("a YAML policy reference via `policy_ref` naming a skill set declared in the §6.3 `skill_sets[]` block"). §6.3 says "unknown names fail validation." §7.1 registers Gates / Hooks / Guards / Budgets but its pseudocode does NOT register skill sets — the loop iterates only `doc.gates + doc.hooks + doc.guards + doc.budgets`. So where is the skill-set symbol table validated? Presumably in the `symbol_table = build_symbol_table(policy_docs)` call, but the pseudocode is opaque on skill sets specifically.

Crash angle: if a DOT workflow at ingest time (per [execution-model.md §4.9]) references a `policy_ref` naming a skill set, the resolution has to look up the skill set in the symbol table. After a crash-and-restart where policy YAML was edited to remove that skill set, the workflow's registration will now fail. This is correct behavior. What the spec does NOT address: workflows that are MID-RUN at crash time, whose agents were launched against a skill set that no longer exists in the reloaded policy. §4.7.CP-037 says "policy reloads are bound to the between-task invariant; there are NO mid-run policy reloads." Good — a crash-and-restart within a run sees the same policy YAML it had before (modulo operator edits between crash and restart), and edits mid-crash are an operator-error surface the operator-nfr protocol should own. But the spec should explicitly state: a crash-recovery startup is subject to the same between-task invariant; restarts that reload edited YAML mid-run are a pause-point obligation. This is the same point as Gap 1-C but re-surfaces at the skill-set layer.

### Severity

Scenario 2: ownership-boundary clarification (at-least-once vs. at-most-once for Hook side-effect application) is a one-line spec addition; S05 owns the rest. Scenario 5: clarification of the §7.1 Pass 1 symbol table to include skill sets explicitly, plus a note that §4.7.CP-037's between-task invariant applies to crash-recovery restarts.

---

## 5. Budget counter crosses crash boundary — rehydrated from what?

### The scenario

A Budget with `resource=tokens`, `scope=per_run`, `limit=100000` has accrued 73000 tokens via `budget_accrual` events. The daemon crashes. On restart, the run is resumed. What does the handler's in-memory counter say?

### What the spec says

§4.5.CP-026 is unambiguous: "The Budget counter state MUST be internal to the handler. It MUST NOT be written to any durable store other than through the typed `budget_accrual`, `budget_warning`, and `budget_exhausted` events. Cross-subsystem reads of the counter MUST go through the event bus per [event-model.md §3.2]; there is no `GetBudgetCounter()` surface."

§4.5.CP-025: "The threshold check uses live in-handler counters (the handler tracks accrual against remaining budget in real time per its own tick cadence)."

So the counter is in-handler, and observable externally only via events. On crash, the in-handler state is lost because the handler process died with the agent.

### The hole

The spec does NOT say how the counter is rehydrated on restart. Three candidate rehydration sources:

- **Git checkpoint** — the run has a durable trace on its task branch. Does the trace include a Budget counter snapshot? Not per this spec. Budget counter state is forbidden from durable stores by CP-026.
- **JSONL event stream** — `budget_accrual` events ARE durable (per event-model.md's JSONL contract, referenced). The rehydration algorithm is: read the run's accrual events from JSONL, sum them, start the handler with that as the starting counter. This is consistent with CP-026 (the event stream IS the durable store the counter reads from).
- **In-memory only** — re-run the agent from scratch, accumulate fresh.

CP-026 technically permits option 2 ("Cross-subsystem reads of the counter MUST go through the event bus") but does NOT mandate it. The spec should name option 2 explicitly. Without it, an implementer could reasonably choose option 3 ("in-memory only, restart = fresh counter") which would DOUBLE-SPEND tokens: the 73000 accrued before the crash are billed once in the pre-crash run-segment and again in the post-crash re-run if the agent is re-invoked.

### Additional holes

**Hole 5-A.** Budget accrual events are emitted per-chunk per §4.5.CP-024 "within the same handler tick that produces the chunk." A crash DURING a handler tick — chunk emitted, accrual event queued but not yet durable — means the chunk's token cost may be lost from the accrual stream. On replay via option 2, the rehydrated counter is under-counting. This is the converse of double-spending: the Budget now appears to have MORE remaining than it actually did. The spec should require that the `budget_accrual` event be durable-before-the-chunk-event, or at least name the ordering as a dependency on [event-model.md]'s durability contract.

**Hole 5-B.** `budget_warning` and `budget_exhausted` are DERIVED from counter state (warning at 0.8 of limit, exhausted at dispatch check). If rehydration sums accrual events and the sum crosses the warning threshold at rehydration time, does a `budget_warning` event re-emit? §4.5.CP-025 says the warning fires "when cumulative accrual crosses the warning_threshold fraction of limit." At rehydration, the cumulative accrual doesn't cross — it IS already past. The event would not fire under a strict reading of "crosses." But the handler's state machine would then be in "past-warning-threshold" mode without the downstream consumers of `budget_warning` ever having seen the signal for this run-after-crash. The spec should require rehydration to replay-emit any threshold-crossings that occurred before the crash but weren't acknowledged by downstream consumers — OR state that `budget_warning` is an at-most-once-per-run event observable via JSONL and downstream consumers are responsible for their own crash recovery by reading JSONL. Either is defensible; silence is not.

**Hole 5-C.** Per-chunk accrual events have a "typed `ErrTransient` or `ErrDeterministic`" wrapping obligation inherited from Hook failures (CP-015) but not explicitly from Budget. If the accrual event's emission fails (event bus backpressure, JSONL writer error), what does the handler do? The spec says accrual is within the handler's tick cadence; a failed accrual emission either blocks the tick (liveness concern) or is silently dropped (correctness concern). Neither is addressed.

### Severity

The primary hole — rehydration source not named — is implementer-blocking because it is the difference between correct budget enforcement across crashes and token double-spending. Option 2 (JSONL replay) is the only option consistent with CP-026 and should be named as the normative rehydration path. Holes 5-A and 5-B are consequent clarifications; 5-C is a cross-reference to event-model's durability contract.

---

## Summary of spec-blocking gaps for v0.3

1. **§7.1 `construct_control_point` purity** — MUST be pure; no side-effects (events, writes) during registration. One-line addition.
2. **§7.1 `control_points_registration_started` signal** — the absence-of-`control_points_registered` failure mode needs a paired start event for diagnosis. One-line addition or explicit "no start event; registration is bounded by daemon-startup sequencing" note.
3. **§4.8 Hook replay path** — explicitly state §7.2 is the sole path for cognition-tagged evaluator invocation across ALL Kinds (Gates and Hooks), not just Gates. One-line addition to §4.8.CP-041 or §7.2's header.
4. **§4.8.CP-040a envelope-construction algorithm** — pseudocode for "originating handler launch" version lookup (read from `skills_provisioned` event, not from current disk). Short pseudocode block.
5. **§4.7.CP-034b abort-and-emit durability pair** — the event MUST be durable before the evaluator wrapper returns control. One-line addition.
6. **§4.5 Budget rehydration** — name JSONL-event-stream replay as the normative rehydration path. One-line addition, possibly a new CP-027a.
7. **§4.3 Hook side-effect application semantics** — co-ownership note that at-least-once vs. at-most-once is S05's obligation to specify explicitly. One-line addition to §4.3.CP-016.

None of these require architectural rework; all are tightening-of-language additions to close crash-recovery ambiguity. The underlying v0.2 design (registry + persisted verdicts + envelope hash + Cat 6 escalation) is crash-coherent; the gaps are at the boundaries where v0.2 introduced new mechanism without extending the durability prose to match.
