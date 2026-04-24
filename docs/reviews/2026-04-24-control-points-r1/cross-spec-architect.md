# Round 1 Cross-Spec Architect Review — control-points.md v0.1.0

## Verdict

The spec lands the right conceptual move (single ControlPoint primitive, four
Kinds, three-owner split) and §4.10 prose is clean in principle. The ownership
split itself survives scrutiny: S02 registry / S05 Hook dispatch / S01 Gate+
Guard invocation is well-factored and does not trespass on S01's cascade or
S05's dispatch loop.

But the spec breaks the cross-spec citation contract in a structural way.
Every other foundation spec (execution-model, handler-contract, event-model,
reconciliation, operator-nfr, beads-integration) cites control-points.md at
§6.1 / §6.2 / §6.3 / §6.4 / §6.5 / §6.7 / §6.8 / §6.9 / §6.11 — the section
numbers inherited from docs/foundation/components.md §6. This spec reorganized
those sections under §4.1–§4.11 (normative) and put Schemas under §6. Roughly
30 sibling-spec citations now point at sections that do not exist, and this
spec's own §2.2, §9, §10 self-cite bootstrap components.md numbers for
architecture / reconciliation / workspace-model / operator-nfr.

Three smaller issues: CP-007 leaks Gate invocation-ordering into the record
spec (S01's territory), CP-050 re-states handler-contract §4.11's internal
pipeline rather than citing by reference, and the Budget primitive does not
cleanly accommodate reconciliation's per-reconciliation-workflow-run scope.

Recommendation: **one integration pass required**. Section-numbering drift
must be resolved either by renumbering this spec to match the surface sibling
specs cite, or by a coordinated citation update across all siblings. The
three scope nits are cleanup on the same pass.

## Scope overlaps

### 1. Ownership split §4.10 — clean, with one leak

CP-047 (three-owner rule) and CP-010 / CP-016 / CP-021 each explicitly
disclaim invocation/dispatch mechanics and point at the owning subsystem.
The scope boundary is held in prose.

Leak: CP-007 says "Multiple Gates on the same attach point execute
sequentially in declaration order; the first non-`allow` verdict is
authoritative and subsequent Gates MUST NOT run." This is an **invocation-
sequencing rule**, which §4.10 disclaims as S01's responsibility. Either the
ordering belongs in S01's subsystem spec (and CP-007 only declares the
attach point is named), or control-points owns declared-order-of-evaluation
(and §2.2's "Gate invocation mechanics out of scope" is too broad). Pick one.

CP-014 (Hook ordering) has the same shape but is defensible because Hook
ordering is a declaration property (subsystem priority + declaration order),
not engine behavior. Text would be cleaner if CP-014 said "the dispatch
engine MUST honor the declared ordering" rather than "Hooks MUST execute in
an order determined by..."

### 2. Gate semantics (§4.2) — does not over-describe invocation

CP-006 through CP-011 stay on the record side. The invocation-mechanics
disclaimer in CP-010 is explicit. Clean except for CP-007 above.

Dangling reference: CP-009's "retry policy governed by the node's policy
per [execution-model.md §4.2]" cites execution-model's Node attributes, but
EM-008 only names `policy_ref` — the retry-policy surface itself is
unowned anywhere in the corpus. handler-contract §8.1 assumes it lives in
control-points; control-points does not expose it.

### 3. Hook semantics (§4.3) — does not over-describe dispatch

CP-012–CP-017 stay record-side. CP-016 disclaims dispatch-loop ownership.
Clean on the scope axis.

Vocabulary gap: CP-013's MVH-baseline Hook triggers (`on_agent_started`,
`on_agent_output`, etc.) do not match event-model §8's event type names
(`agent_started`, `agent_output_chunk`). Either these are aliases (then
normalize naming) or Hook-trigger vocabulary is a separate namespace (then
say so explicitly). Authoring-time-relevant.

### 4. Budget interaction with reconciliation wall-clock budget

CP-027 correctly co-references reconciliation and disclaims outer-bound
enforcement. Reconciliation RC-017 owns the outer bound. The co-reference
is directionally clean.

Primitive-layer gap: Budget §6.1.4 declares `scope ∈ {per_role, per_run,
per_state}`. Reconciliation's outer-bound budget is per-reconciliation-
workflow-run. Is that `per_run`? The spec doesn't say. If yes, CP-027
should state it explicitly so the wall-clock budget can register as a
Budget instance with resource=`wall_clock_seconds`, scope=`per_run`,
scope_target=reconciliation_run_id. If no, the scope enum needs a new
entry. Leaving it implicit invites drift.

### 5. Skill declaration surface (§4.11) — mostly clean, one leak

CP-049 (node `required_skills`), CP-051 (mechanism-tagged), CP-052
(Beads-CLI default) are declaration-side. Clean.

Leak: CP-050 says "The handler consumes this set per [handler-contract.md
§4.11]: it resolves declared skills against available skill packages,
provisions them into the agent process shape, emits the
`skills_provisioned` event per [event-model.md §3.2], and fail-launches if
resolution fails." The four-step body duplicates HC-046 through HC-050
inline rather than citing. Any future edit to handler-contract silently
diverges from CP-050's body.

Fix: CP-050 declares only the union computation; cite handler-contract
§4.11 by reference for consumption.

### 6. Policy expression grammar (`expr-lang/expr`) — correctly pinned here

CP-034 pins the grammar; §6.4 declares the evaluation environment. This is
the right home: every sibling spec consuming policy expressions cites
control-points for the grammar. Load-bearing single point of truth.

Small gap: §6.4's "evaluation : bounded -- per expr-lang default evaluation
limits" delegates boundedness to library defaults. operator-nfr assumes
policy evaluation cannot starve the daemon (ON-018 on policy-evaluation
cost). OQ-CP-003 defers version-pin but should also pin whether evaluation-
cost ceilings are declared at the spec level (numeric ceiling) or delegated
to library defaults.

## Ownership violations

### V1 — Section-numbering drift breaks ~30 cross-references from sibling specs

This is the headline issue. Sibling specs cite control-points.md at:

- `§6.1` — control-point primitive (execution-model, handler-contract
  LaunchSpec)
- `§6.2` — Gate semantics (event-model §6.5, execution-model §7.3,
  reconciliation RC-024)
- `§6.3` — Hook semantics (execution-model EM-027, event-model §6.5)
- `§6.4` — Guard semantics (execution-model §7.3, event-model §6.5)
- `§6.5` — Policy schema / PolicyExpression (execution-model multiple
  cites; reconciliation RC-017; operator-nfr ON-018)
- `§6.7` — Freedom profile (handler-contract LaunchSpec)
- `§6.8` — Config-loading precedence (operator-nfr ON-004)
- `§6.9` — Budget enforcement (handler-contract, execution-model EM-042
  and §8.5, event-model §6.5, operator-nfr ON-045)
- `§6.11` — Skill declaration (handler-contract §4.11, execution-model
  EM-008, beads-integration BI-027, reconciliation RC-004)

None of those sections exist at those numbers in this spec. Current
sections: §4.1 primitive, §4.2 Gate, §4.3 Hook, §4.4 Guard, §4.5 Budget,
§4.6 Roles/Freedom, §4.7 Policy grammar+documents, §4.8 Cognition-tagged
evaluator, §4.9 Registration, §4.10 Ownership, §4.11 Skill declaration.
§6 is Schemas.

This spec also self-cites old numbering in multiple places:

- §2.2 / §10.3: "reconciliation.md §9.4a" — reconciliation is now RC-017
  under §4.4.
- §4.8.CP-040, §9.3: "workspace-model.md §5.8 Branching model" —
  workspace-model's branching is §4.2 (WM-005–WM-011); §5 is Invariants.
- §9.1, §9.3, multiple CPs: "architecture.md §1.1, §1.2, §1.4, §1.5,
  §1.6, §1.8, §1.9" — architecture.md uses §4.1, §4.2, §4.4, §4.6, §4.8,
  §4.9, §4.10.
- §4.7.CP-037, §9.3: "operator-nfr.md §7.3, §7.5" — operator-nfr uses
  §4.3 and §4.5.

Underlying problem: control-points adopted the requirements-first template's
§4 numbering for normative requirements (template-compliant) but every
sibling encoded components.md §6.x numbering in citations. Either renumber
this spec back (template-noncompliant), or coordinated citation update
across siblings. The coordinated update is structurally cleaner.

### V2 — CP-007 ordering rule is S01 invocation territory

See Scope overlap 1. CP-007's "execute sequentially in declaration order;
first non-allow is authoritative" is a dispatcher rule. §2.2 lists "Gate
invocation mechanics" as out of scope. Contradiction.

### V3 — CP-050 describes handler-contract's internal pipeline

See Scope overlap 5. CP-050 inlines HC-046–HC-050's content rather than
citing.

## Cross-reference issues

- **event-model §3.7 and §3.2 do not exist.** CP-016 cites `[event-model.md
  §3.2 §3.7]` for Hook-as-consumer taxonomy. §3 is only the glossary;
  consumer taxonomy lives in §4.3 (EV-009–EV-014); event taxonomy is §8.
- **execution-model §4.10 citation is correct.** CP-018 and CP-021
  correctly cite execution-model §4.10 (EM-041–EM-046).
- **workspace-model §5.8 does not exist.** CP-040 and §9.3 need §4.2
  (WM-005–WM-011, branching) and §4.5 (merge semantics).
- **reconciliation citations use components.md numbering.** §9.1 → RC-001
  under §4.1; §9.2 → RC-006 + §6.1 schemas; §9.3 → RC-010–RC-014 under
  §4.3; §9.4a → RC-017–RC-018 under §4.4; §9.5 → RC-020–RC-025 under §4.5.
- **architecture §1.x citations use components.md numbering.** §1.1 →
  §4.1; §1.2 → §4.2; §1.4 → §4.4; §1.5 → §4.6; §1.6 → §4.8; §1.8 → §4.9;
  §1.9 → §4.10.
- **handler-contract §4.11 citations are correct.** HC-046–HC-050.
- **operator-nfr §7.x citations use components.md numbering.** §7.3 →
  §4.3 (ON-008); §7.5 → §4.5 (ON-019).

## Recommendations

**R1. Coordinated section-number migration.** Pick one of:
(a) renumber this spec's normative sections back to the §6.x shape sibling
specs cite — preserves citation integrity but violates template §4
convention;
(b) update every sibling spec's citations to point at the new §4.x
numbering — preserves template compliance, is mechanical, but touches
execution-model v0.2 as a revision event.
Option (b) is structurally cleaner. This spec's own self-citations to
bootstrap numbers (architecture, reconciliation, workspace-model,
operator-nfr) need migration regardless.

**R2. Three scope nits.**
- CP-007: hoist Gate ordering-and-halt-on-first-non-allow into S01's
  subsystem spec, or widen §2.2 to admit Gate invocation-ordering.
- CP-050: remove the four-step resolve/provision/emit/fail chain; cite
  handler-contract §4.11 by reference only.
- CP-027: state that reconciliation's wall-clock budget instantiates as a
  Budget with resource=`wall_clock_seconds` and scope=`per_run` (reconciliation-
  workflow-run), or add a new scope enum entry.

**R3. Tighten two primitive-layer gaps.**
- §6.4 policy-expression grammar: name whether evaluation-cost ceilings
  are spec-level numeric or delegated to library defaults; update OQ-CP-003.
- CP-013 Hook-trigger vocabulary: clarify whether `on_agent_started` etc.
  are aliases of event-model event types or a separate namespace.

## Notes on what is fine

The single-primitive-four-Kinds design is cleaner than a Gate/Hook/Guard/
Budget quartet; the §4.1.CP-005 per-Kind table is load-bearing and survives
scrutiny. The ZFC enforcement at CP-INV-004 (Guards are mechanism-only) is
the right place for the rule. The persisted-verdict contract (§4.8) is the
right split: mechanism persists, cognition produces. The registry-as-single-
source-of-truth invariant (CP-INV-001) and events-only-cross-boundary
invariant (CP-INV-002) together make the §4.10 ownership split enforceable.
These are not problems; they are load-bearing correct moves.
