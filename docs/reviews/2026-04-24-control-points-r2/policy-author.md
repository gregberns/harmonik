# Round 2 Policy-Author Review — control-points.md v0.2.0

**Lens.** I am an operator sitting down to write real policy YAML against this spec, with no source access. I tried five authoring tasks and recorded every place I got stuck, every bit of `expr-lang` environment I could not verify, and every YAML key that is missing or ambiguous in §6.3.

**Verdict.** 2 of 5 tasks are authorable unambiguously. 3 of 5 require guesses the spec does not resolve. The §6.3 YAML template is a sketch, not a schema: at least eight keys used in tasks 1–5 are either missing, implicit, or contradicted by other sections.

---

## Task 1 — Gate: deny agentic nodes whose `handler_type = claude-code` during pause state

**Goal.** A Gate that blocks transitions into any node handled by `claude-code` while the daemon is in an operator-pause.

**My attempt.**

```yaml
gates:
  - name: deny-claude-code-on-pause
    subtype: goal-gate
    attach_point: node-pre-entry
    evaluator:
      mode: mechanism
      expression: "run.paused && run.next_node.handler_type == 'claude-code'"
```

**Where I got stuck.**

1. **`run.paused` is unverifiable.** §6.4 names the environment as `run`, `outcome`, `event`, `context`, `policy_meta`. The spec defers the `Run` shape to `[execution-model.md §6.1 Run]`. From this spec alone I cannot confirm that a `paused` boolean, a `run.state == "paused"`, or an equivalent exists. CP-037 mentions an "operator pause" tied to `[operator-nfr.md §4.3]`, but that is daemon state, not necessarily reflected on `run`.
2. **`run.next_node` is a guess.** A Gate with `attach_point: node-pre-entry` must expose the candidate node to the evaluator. §4.2.CP-006 says "the evaluator receives the current state, the candidate transition ... and the handler outcome." There is no "candidate node" field named. Is it `transition.target`? `transition.to_state`? A separate `node` binding? The Gate evaluator-input column of the §4.1.CP-005 table says "current state, candidate transition, outcome" — no node handle at all. **Stuck.**
3. **`handler_type` is not mentioned anywhere in the grammar environment.** `handler-contract.md` owns `handler_type`, but this spec does not expose a node's handler metadata into the expression environment. The DOT attribute `handler_type` (per execution-model.md §4.2) is presumably reachable as `run.next_node.handler_type` or `transition.target.handler_type`, but §6.4 does not name either path. **Missing environment binding.**
4. **`subtype: goal-gate` chosen by elimination.** CP-008 describes goal-gate as "a policy-level goal assertion that cannot be bypassed by the run" — this fits, but the spec never gives a worked example distinguishing goal-gate from approval-gate for a deny-of-tool-class case. Reasonable guess; not grounded.

**Verdict.** Cannot write this policy without reading `execution-model.md §6.1` and `handler-contract.md` for the Run/Node shape. Spec is not self-contained for mechanism-Gate authoring.

---

## Task 2 — Hook: after every checkpoint, log `run_id` + `bead_id`

**Goal.** Fire a side-effect whenever a checkpoint event occurs, capturing `run_id` and a linked `bead_id`.

**My attempt.**

```yaml
hooks:
  - name: log-checkpoint-run-bead
    trigger_event: on_checkpoint_written      # guessed
    subscription_filter: null
    side_effect_kind: external-action
    halt_on_failure: false
    subsystem_priority: 100
    side_effect_target: checkpoint-audit-log  # GUESS — not in §6.1.2
    side_effect_payload:                      # GUESS — not in §6.1.2
      run_id: "{{ event.payload.run_id }}"
      bead_id: "{{ event.payload.bead_id }}"
```

**Where I got stuck.**

1. **`on_checkpoint_written` is not in the MVH Hook-trigger set.** CP-013 enumerates: `on_agent_started`, `on_agent_output`, `on_agent_completed`, `on_timeout`, `on_review_required`, `on_transition_attempted`. Checkpoint is a first-class concept elsewhere (operator-nfr.md §4.5, CP-038 mentions "Checkpoint-format stability") but there is no `on_checkpoint_*` trigger baselined here. The spec says subsystems MAY declare additional trigger types via subsystem envelope — so I would be authoring a policy that depends on a subsystem-registered trigger name that is nowhere defined. **Fatal gap for this task.**
2. **`bead_id` is not in the event environment.** §6.4's `event` binding comes from `[event-model.md §3.2]`. This spec does not specify that checkpoint-associated events carry `bead_id`. Beads-integration.md §4.9 is co-referenced but not summarized. **Cannot verify.**
3. **The YAML template has no `side_effect_target` or `side_effect_payload` keys.** §6.3's `hooks[]` shape ends at `subsystem_priority`. §6.1.2 Hook payload record does NOT include target/payload fields; those live on the `SideEffect` record (§6.1.6) produced by the *evaluator at runtime*. So where does the author DECLARE the side effect? Either (a) the evaluator expression synthesizes the SideEffect descriptor (but `mode: mechanism` + `expression: <expr-lang>` returns a value, not a struct — unclear), or (b) a declarative side-effect spec belongs in the YAML and is missing. **§6.3 is incomplete for Hook authoring.**
4. **Hooks have no `evaluator` block in §6.3.** Gates and Guards show an `evaluator: { mode, expression | delegation_path }` block. Hooks in §6.3 jump straight to `trigger_event` / `subscription_filter` / `side_effect_kind`. But §4.3.CP-012 says the Hook has an evaluator that "returns a side-effect descriptor." Where does the author specify THAT evaluator? Missing from §6.3.

**Verdict.** Not authorable. Three missing pieces: a `bead_id`-bearing checkpoint event, a declared side-effect shape in YAML, and an evaluator block for Hooks.

---

## Task 3 — Budget: cap wall-clock per node to 300s

**Goal.** Per-node wall-clock ceiling.

**My attempt.**

```yaml
budgets:
  - name: per-node-wallclock-300
    resource: wall_clock_seconds
    scope: per_state             # "per_state" appears to mean per-node; confirmed below
    limit: 300
    warning_threshold: 0.8
    scope_target: "*"            # GUESS — wildcard for "all nodes" not documented
```

**Where I got stuck.**

1. **`scope: per_state` vs. "per node" terminology.** CP-022 enumerates `{per_role, per_run, per_state}`. §6.1.4 and §6.3 both use `per_state`. Execution-model.md §4.1 uses State and Node somewhat interchangeably, but this spec does not confirm `per_state` = "per node instance" vs. "per state-name class" vs. "per state-entry." If I want ONE budget that caps EACH node to 300s, is that scope=`per_state` with a wildcard target, or must I declare one budget per node?
2. **`scope_target` shape is under-specified.** §6.1.4 says `scope_target : String` with comment "role name | run_id | state_id per scope." No syntax for "every state" (wildcard). No indication of whether `state_id` means a node_id from DOT or a run-state-instance id. **Cannot write a "cap every node at 300s" policy from this spec alone.**
3. **CP-027 references wall-clock composition; implied but not stated: reconciliation wall-clock is an outer bound — what about per-node inner bounds?** CP-027 says inner per-role/per-state budgets must not extend beyond the outer wall-clock bound. Good. But it does not answer: if a node takes 250s and the run already consumed 200s of a 300s per-node budget, does per-state scope RESET on state entry? **No reset semantics declared.**
4. **Interaction with `freedom_profiles[].wall_clock_budget_ref`.** §6.2's FreedomProfile has `wall_clock_budget_ref`. If a freedom profile and a policy-level Budget both apply, tightest-wins per CP-033. But FreedomProfile has `max_iterations` as a direct integer, not a ref — whereas wall-clock is a ref. Why the inconsistency? Likely because budgets carry warning thresholds that a raw integer cannot; but the spec does not explain. Minor stuck.

**Verdict.** Partially authorable. The shape `wall_clock_seconds / per_state / limit: 300` is unambiguous; `scope_target` semantics for "every node" are not, and per-state reset semantics are not declared.

---

## Task 4 — Guard: reorder/filter eligible beads by cost

**Goal.** When selecting the next bead to dispatch from a queue of ready beads, reorder them so cheapest (by token estimate) comes first.

**My attempt.**

```yaml
guards:
  - name: reorder-beads-by-cost
    evaluator:
      mode: mechanism
      expression: "sortBy(edges, 'target.estimated_tokens')"   # GUESS — expr-lang sortBy not verified
```

**Where I got stuck.**

1. **"filter" is explicitly forbidden.** CP-018: "A Guard MUST NOT add edges not present in the input, remove edges, or block a transition." So the "filter" part of the task is rejected by the spec. The task as stated is half-illegal: reorder yes, filter no. **Good — spec is clear here.** (This is the one clean win across all five tasks.)
2. **Guard applies to edges, not to beads.** A Guard fires during edge evaluation of the cascade. But "eligible beads" is a queue concept, not an edge-cascade concept. Beads-integration.md §4.9 is where bead-selection lives. Reordering EDGES at the DOT-graph level does not obviously translate to reordering BEADS in a work queue. **Mismatch between the task framing and the Guard primitive's scope.** Guards cannot be used for bead-queue reordering as posed.
3. **Edge shape in the expression environment is undeclared.** The evaluator input per CP-005 is "Edge set, current state, outcome." But §6.4 names only `run`, `outcome`, `event`, `context`, `policy_meta`. Is `edges` a binding in Guard context? Not named. `edge.target.estimated_tokens`? No schema. **Cannot author the expression body.**
4. **Return shape: "reordered edge list" — how is this expressed in expr-lang?** expr-lang returns scalars or collections; the evaluator must return a List<Edge>. The spec gives one example (`outcome.status == "SUCCESS" && ...` — scalar bool). No example of returning a reordered list. **No grounded syntax.**

**Verdict.** The task as posed violates the Guard primitive (filter forbidden, wrong scope — edges vs. beads). Even the legal reorder-edges part is unauthorable without edge-binding documentation.

---

## Task 5 — Skill set: declare `builder` role has `[beads-cli, go-test-run]`

**Goal.** Role-default skills, per §6.3 / §4.6.

**My attempt.**

```yaml
roles:
  - name: builder
    permission_schema:
      allowed_tools: [bash, read, edit, write]
      writable_paths: ["src/**", "tests/**"]
      readable_paths: ["**"]
      default_skills: [beads-cli, go-test-run]
      allowed_hooks: []
      invocable_by: [planner]
    status: mvh-required
```

**Where I got stuck.**

1. **`model_tier` omitted — is that legal?** §6.2's PermissionSchema has `model_tier: String | None`. §6.3's YAML template for roles does NOT show `model_tier`. Is it required? Optional? Missing from YAML even though in the record. **§6.3/§6.2 inconsistency.**
2. **Skill-name shape.** CP-049 cites `[handler-contract.md §4.11]` for the shape: "lowercase hyphenated identifier, optionally suffixed with `@<version>`." OK — `beads-cli` and `go-test-run` are valid. But this spec does not show a skill-name with version pin (`beads-cli@1.2.0`) in any example. **Minor gap; authorable.**
3. **CP-031 mandates `beads-cli` as a default.** Fine — but the role `builder` is not among Planner/Builder/Reviewer explicitly; `Builder` (capitalized?) is named in CP-030 and architecture.md §4.8 owns the canonical casing. I wrote `builder` lowercase; the spec is silent on casing. **Minor stuck.**
4. **`skill_sets[]` vs. `default_skills` duplication.** §6.3 introduces a `skill_sets[]` block "referenceable from DOT `policy_ref`." But roles already carry `default_skills` inline. When do I use which? CP-049 says nodes MAY declare `required_skills` as "a YAML policy reference via `policy_ref` naming a skill set declared in the §6.3 `skill_sets[]` block." So skill_sets are for NODE-level declarations, not role defaults. That is NOT obvious from §6.3's placement of the two keys side by side. **Doc clarity gap.**
5. **`status: mvh-required` field placement.** §6.3 shows `status` as a sibling of `permission_schema` under a role. §6.2's Role record has `status: RoleStatus`. Consistent. Authorable.

**Verdict.** Authorable with minor ambiguity. The role/skill-set duality in §6.3 invites misuse; `model_tier` absence in the YAML template is a real omission.

---

## Missing / ambiguous §6.3 YAML keys (consolidated)

Every key below is referenced by normative requirements or §6.1 records but is missing, implicit, or inconsistent in §6.3:

- **Role: `model_tier`.** In §6.2 record, absent from §6.3 YAML template.
- **Hook: evaluator block.** §4.3.CP-012 requires an evaluator; §6.3's hook template has none.
- **Hook: `side_effect_target`, `side_effect_payload` (or equivalent declarative side-effect spec).** §6.1.6's SideEffect record has target + payload, but the AUTHORING surface (§6.3) has no place to declare them.
- **Gate: `named_approver` and `verification_ref`.** §6.1.1 GatePayload has them (required for approval-gate / quality-gate subtypes). §6.3's gate template omits them.
- **Budget: wildcard or multi-target `scope_target`.** §6.1.4 says String; §6.3 shows a singular value. "Every node" not expressible.
- **Guard: `applies_to_node` scoping.** §6.1.3 has it; §6.3's guard template omits it.
- **FreedomProfile: `token_budget_ref`, `wall_clock_budget_ref`.** In §6.2 record; §6.3's freedom_profiles template omits them.
- **Guard expression return shape.** No YAML example of an expr-lang expression returning a list.

## Unverifiable expr-lang environment (consolidated)

Every binding path below was needed to author tasks 1–5 but is not documented in §6.4:

- `run.paused`, `run.state` (pause detection).
- `run.next_node.*`, `transition.target.*`, or any node-handle binding (needed for Gate at `node-pre-entry`).
- `run.next_node.handler_type` (needed for handler-class predicates).
- Guard input: is the edge set bound as `edges`? `candidate_edges`? Not named.
- Edge field shape: `edge.target`, `edge.weight`, `edge.preferred_label`? Referenced in execution-model but not restated here.
- `event.payload.bead_id` (needed for Task 2) — the spec does not document which events carry `bead_id`.
- expr-lang builtins available: `sortBy`, `filter`, `map` — §6.4 says "pipeline operators and map/filter builtins" exist (per §A.3), but the authoring surface gives no list. Task 4's `sortBy` was a guess.
- Return-type coercion for non-scalar evaluator outputs (Guard must return `List<Edge>`; Hook must return `SideEffect` struct). No grammar for structured returns.

## Cross-cutting authoring friction

Beyond the task-by-task gaps, three patterns made authoring hard:

1. **The spec is self-referentially complete but externally dependent.** §6.4 says the expression environment is `Run`, `Outcome`, `Event` — each defined in a neighboring spec. A policy-author sitting down without a pre-assembled view of all four specs cannot write a mechanism-tagged expression of any complexity. At minimum, §6.4 should inline the field paths that policy expressions are permitted to reach, or declare an explicit "exposed subset" schema. Delegating the full Run shape to `execution-model.md §6.1` is correct for normative ownership, but it leaves the authoring surface incomplete.

2. **Declarative vs. imperative expression outputs are conflated.** The grammar `expr-lang/expr` returns scalars or simple collections. Gates return `{allow, deny} + reason` — that's a scalar-plus-string pair, ill-typed for a Boolean expression. The example under §6.4 (`outcome.status == "SUCCESS" && ...`) is a bool, which SUGGESTS the convention is "expression returns bool, deny = !bool, reason = fixed message" — but the spec never states this convention. Guards return reordered edge lists (structured). Hooks return side-effect descriptors (structured). **Three different return conventions for one grammar are unlabeled.**

3. **The §6.3 YAML and the §6.1 record schemas drift.** A real schema document would either (a) derive the YAML from the records mechanically, or (b) declare the YAML as the authoritative surface and map records to it. §6.3 claims to be the authoring surface but is materially smaller than §6.1. An author reading §6.3 top-down will miss `named_approver`, `verification_ref`, `applies_to_node`, `token_budget_ref`, `wall_clock_budget_ref`, and evaluator blocks for Hooks — all of which are normative.

## Recommendations (policy-author priority order)

1. **Add an "expression environment reference" subsection** (§6.4a) with the exposed field paths on `run`, `outcome`, `event` for each evaluator context (Gate-pre-entry, Gate-post-exit, Guard, Hook, Budget). Inline enough of the Run/Outcome/Event schemas that an author can write expressions without cross-spec reading.

2. **Regenerate §6.3 YAML from §6.1 records.** Every record field should appear in the YAML, or be explicitly flagged as runtime-only. Mark required-when-X fields (e.g., `named_approver` required when `subtype: approval-gate`) explicitly.

3. **Add evaluator block to Hook YAML in §6.3.** Hook §4.3.CP-012 demands an evaluator that returns a side-effect descriptor; §6.3's template has no slot for it.

4. **Declare return-shape conventions per Kind.** A single table: Gate evaluator expression returns Bool (false = deny with declared reason) OR structured `{action, reason}`; Guard returns `List<Edge>`; Hook returns `SideEffect`; Budget returns `{verdict, cost}`. One paragraph fixes a class of author confusion.

5. **Document wildcard/multi-target `scope_target` syntax** for Budgets. A `scope_target: "*"` or `scope_targets: [...]` convention, or an explicit statement that one budget declaration is needed per target.

6. **Provide a worked example per Kind.** The §6.4 example is a single Gate expression. A worked end-to-end policy YAML (one role, one freedom profile, one Gate of each subtype, one Hook with each side_effect_kind, one Guard, one Budget of each resource type) would resolve half the ambiguities above at documentation cost only — no spec-level changes.

## Summary

Two tasks (3, 5) are authorable with minor guesses. Three tasks (1, 2, 4) are not authorable from this spec in isolation because the `expr-lang` environment schema is elsewhere, the Hook authoring surface is incomplete, and the Guard primitive does not cover the framing in Task 4. The §6.3 YAML is a sketch; a real policy-author schema needs all eight keys above and a grounded expr-lang environment reference.
