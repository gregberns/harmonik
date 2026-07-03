# D3 Design — Control-Point Framing (Pass-4, phase-3-dot)

## Decision

**Framing A.** `control-point` is NOT a node-type in the workflow-graph taxonomy; it is a policy-layer primitive bound to nodes (and edges) via the existing typed `*_ref` attributes (`gate_ref`, `policy_ref`, `freedom_profile_ref`, `budget_ref`, plus a new `skills_ref` per D14). The 5-type taxonomy collapses to 4: `agentic | non-agentic | gate | sub-workflow`. A `gate` node-type is retained as "a node whose handler evaluates a Gate-kind ControlPoint" — Gate is the one Kind whose trigger ("transition attempt") fits a node-shaped execution slot.

## Rationale

- **CP-005 per-Kind trigger table is structurally disjoint.** Per `specs/control-points.md` §4.1.CP-005 and the §A.3 rationale ("Why `Kind` is peerage at the registration layer, not structural peerage"), the four Kinds have non-interchangeable triggers: Gate fires on transition attempt, Hook fires on lifecycle event, Guard fires on a registry lookup-by-trigger, Budget fires on dispatch + per-chunk accrual. Only Gate's trigger maps to "evaluate when the walker arrives at this node." Forcing all four Kinds into a single `control-point` node-type with a `kind` discriminant (Framing C) would either (a) misrepresent Hook/Guard/Budget as node-arrival evaluators, or (b) require the dispatcher to special-case `kind` so heavily that the node-type is fictional — gaining the word "control-point" in the catalog while losing dispatch coherence.

- **The CP contract already binds by attribute, not by node-type.** CP-002 / CP-036 (locked) state that DOT attributes `gate_ref` / `policy_ref` / `freedom_profile_ref` / `budget_ref` carry the YAML `name` of a registered ControlPoint. The binding rail is *attribute-based*. Adding a `control-point` node-type would create a second, parallel binding rail (node-as-CP), redundant with the attribute rail. C4 findings §2 explicitly: "B fragments the catalog without buying anything"; the same argument applies to C at higher cost.

- **Kilroy precedent and pass-3 lean.** C4 findings §2 record both Kilroy's `goal_gate=true` attribute-form precedent and an explicit lean toward A. Pass-3 SUMMARY rows D3 + items #1 and #15 in the "already-resolved" list cite EM-006's 5-type taxonomy as locked *categories* with Q1 (this decision) as the framing question within it — meaning the resolved item #1 is consistent with collapsing the catalog to 4 when Q1 lands.

- **Guard and Hook already have non-node trigger surfaces.** Per CP-019 the driver MUST call Guard `LookupByTrigger` BEFORE the EM-042 cascade (i.e., guards run as a *pre-cascade reorder*, not as a node arrival). Hooks fire at lifecycle events (node start/finish, run start/finish per CP-013). Budgets accrue per-chunk. None of these are dispatchable as "the walker arrived at node X" — they are subsystem-S05 dispatch concerns, NOT graph-walk events. Putting them in the node-type catalog at all is a category error.

- **Smaller taxonomy reduces the C2 dispatch table.** With 4 node-types the EM-006 dispatch table has rows for `agentic` (handler-ref), `non-agentic` (handler-ref), `gate` (handler-ref evaluates Gate-kind CP), `sub-workflow` (workflow expansion). All four have a single dispatch shape: "walker arrives, executes a unit of work, returns an Outcome." A 5th `control-point` row would need to special-case Hook (no walker involvement), Guard (pre-cascade only), Budget (chunk accrual) — three exceptions to a four-row table.

## Implications for C1 (`specs/workflow-graph.md`)

- **Node-type catalog table has 4 rows, not 5:** `agentic`, `non-agentic`, `gate`, `sub-workflow`.
- **EM-006 amendment is required.** EM-006 currently lists `{agentic, non-agentic, gate, control-point, sub-workflow}`. Pass-5 spec-draft for execution-model.md §dot binding must remove `control-point` from this enum. This is a *renaming/removing* operation per §6.4 → breaking schema bump from v1 to v2. Acceptable because phase-3-dot is the first wire-up of DOT mode; no v1 corpus exists in production.
- **Per-node-type Outcome-status legality table** (G1 point 5): the 4-type catalog simplifies the matrix. `gate` is the only node-type that may return a Gate-kind Outcome payload (per D7 if landed).
- **`gate` node attribute set:** `gate_ref` (REQUIRED), `handler_ref` (REQUIRED — points to a Gate-evaluator handler), plus optional `freedom_profile_ref`, `budget_ref`. The `gate_ref` resolves to a `gates[]` entry in policy YAML.
- **Non-gate nodes may still carry `*_ref` attributes** — any node may carry `freedom_profile_ref`, `budget_ref`, and (per D14) `skills_ref`, because these bind subsystem-S05 policy that runs alongside the node's execution. `gate_ref` is the only ref that pins the *evaluator semantics* of the node itself.

## Implications for C4 (`specs/control-points.md` binding §)

- **CP-036 stays unchanged.** The by-name attribute binding (`gate_ref`, `policy_ref`, `freedom_profile_ref`, `budget_ref`) IS the binding rail; this decision affirms it.
- **`policy_ref` overload (D14 territory) becomes more urgent under A.** Because Framing A leans hard on attribute-based binding, the overloaded `policy_ref` (gates + freedom_profiles + skill_sets) needs the typed `skills_ref` disambiguation. D3 does not decide D14, but D3 raises D14's priority.
- **No `gate-node` carve-out needed in CP contract.** The CP spec is about Kinds and policy; the fact that `gate` is a node-type is a workflow-graph concern. CP-036's wording ("DOT attributes ... resolve to registered names") is sufficient; CP does not need to know that one of those attribute carriers happens to also be a typed node.
- **CP-049 unchanged.** Skill declaration at node level remains attribute-based (`required_skills` inline OR `policy_ref` to `skill_sets[]`, to become `skills_ref` per D14).
- **Hook / Guard / Budget bindings.** Hooks bind via `hooks[]` YAML registration — they are NOT node-bound. Guards bind via `guards[]` YAML registration — pre-cascade lookup-by-trigger. Budgets bind via `budget_ref` on any node. None of these need a node-type representation.

## Implications for C5 (`specs/examples/review-loop.dot`)

- **The review node is type `gate` ONLY if the verdict gate is being modeled as a Gate-kind ControlPoint.** Per EM-015d, review-loop has reviewer-and-implementer nodes plus terminal nodes (`close`, `close-needs-attention`). The review *evaluation* is performed by the reviewer node (type: `agentic`, `agent_type: reviewer`); the routing decision is then expressed via edge `condition` on the reviewer's Outcome (e.g., `outcome.preferred_label == "APPROVE"`). NO `gate`-type node is required for review-loop v1.
- **If a separate Gate evaluator is wanted** (e.g., a mechanism-tagged "verdict == APPROVE AND iteration < 3" check), it lands as a `gate`-type node downstream of `reviewer`, carrying `gate_ref="review_verdict_gate"` pointing into `gates[]`. Optional; pass-5 picks.
- **review-loop.dot does NOT need a `control-point` node anywhere.** Hooks/Guards/Budgets attached to the workflow live in the policy YAML the workflow imports; they do not appear as DOT nodes.

## Trade-offs accepted

- **We give up the "uniform CP catalog presence in the graph."** Framing C would have made every ControlPoint visible at the graph layer (one node-type, four `kind` rows). Under A, Hooks/Guards/Budgets are invisible at the DOT layer — visible only in the imported policy YAML and in event-stream observability. The operator viewing only the `.dot` cannot enumerate the project's hooks/budgets without also opening the policy YAML. Mitigation: the AGENT_INDEX / project map already cross-references the YAML.

- **We give up the option to inline a non-Gate ControlPoint as a "graph node."** Any future ControlPoint Kind that *would* be node-shaped (e.g., a hypothetical `Throttle` Kind) cannot be reused as a graph node-type under A — it would either join `gate` (if its trigger is transition-attempt-like) or stay attribute-bound. This is consistent with CP §A.3 (new Kinds added at the registration layer, not the graph layer).

- **`gate` as a node-type is slightly idiosyncratic** — it's the only node-type whose semantics are tied to a specific CP Kind. The alternative would be to rename `gate` to something like `policy-evaluator` to weaken the coupling, but pass-3 SUMMARY item #1 records EM-006's category names as locked. Idiosyncrasy is accepted; renaming is out of scope.

## Open follow-up questions

These are triggered by D3 but NOT closed by it. They become tractable in subsequent pass-4 decisions or pass-5 spec-draft:

1. **D14 (`policy_ref` overload disambiguation) priority elevation.** With Framing A, `*_ref` attributes are the only binding rail; the overloaded `policy_ref` needs typed disambiguation before C5 can ship a coherent example. Recommend lifting D14 into the next pass-4 batch.

2. **Does the `gate` node-type require `handler_ref`?** Locked EM-007 says agentic carries `handler_ref` and the other four node-types MUST NOT. Under A, `gate` is now in the "MUST NOT" set per current EM-007, but Gate evaluation requires *some* dispatch — does it route through a Gate-specific evaluator path (per CP §7.2 cognition pseudocode) bypassing the handler registry? Pass-5 must reconcile EM-007 with the Gate-evaluator dispatch path.

3. **D7 (gate-node Outcome payload — `kind=gate_decision`).** Now scoped to *gate* node-type only, simplifying the C3 Outcome-discriminator surface. D7 still needs to land.

4. **D17 (tool-node handler contract).** Independent of D3; whether tool-nodes are `non-agentic` (likely under A) and what their handler contract looks like is still open.

5. **Schema-version bump documentation.** EM-006 enum collapse from 5→4 is a breaking change per §6.4; pass-5 must write the bump rationale and migration note (trivial since no v1 corpus exists, but the bump must be recorded for the §6.4 N-1 readability discipline).

6. **`bead-process.dot` (C5 second example) scope.** Pass-3 SUMMARY D13 leaned defer. D3 does not change that lean; if it ships, it has to demonstrate attribute-bound budgets/guards/hooks rather than node-shaped ones.

---

## Footer — Reviewer note

Reviewer sub-agent was not dispatched in this pass; the Agent tool is not available in this thread. Per D3 brief, a **fresh-context re-read pass** was performed against (a) the decision sentence, (b) the rationale bullets, and (c) the four implication sections. Re-read confirmed: no rationale bullet contradicts a locked item in pass-3 SUMMARY items #1–#20; the EM-006 schema bump is correctly flagged as breaking; the `handler_ref` tension for the `gate` node-type (open follow-up #2) is correctly surfaced rather than papered over. No BLOCK-grade issues identified in re-read; one REQUEST_CHANGES-grade item (open follow-up #2) is already surfaced for pass-5. Recommend a fresh-context reviewer agent run before pass-5 spec-draft begins.
