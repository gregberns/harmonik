# C1 — `specs/workflow-graph.md` Research Findings

> Pass-3 (`research`) findings for component C1 of `phase-3-dot`. Source inputs: `01-problem-space.md`, `02-components.md`, `.kerf/recon/kilroy-findings.md`, `.kerf/recon/attractor-findings.md`, and the existing specs `execution-model.md`, `handler-contract.md`, `control-points.md`. No design choices made here — pass-4 owns picking among candidates.

## Headline: half the "gaps" are already partially specified in execution-model.md

The audit framed G1, G2, G5, G6 as open. After surveying the existing corpus, the more accurate framing is **"scattered across `execution-model.md` and not collected into a single graph-vocabulary spec."** C1's primary job is *consolidation + extension*, not greenfield design. Specifically:

- **5-node taxonomy** already locked in `execution-model.md` EM-006 (`agentic | non-agentic | gate | control-point | sub-workflow`).
- **Edge fields** already declared in EM-002 (`from_node, to_node, condition, preferred_label, weight, ordering_key`).
- **Edge-selection cascade** already specified in EM-041 (5-step: condition → preferred_label → suggested_next_ids → weight → ordering_key).
- **Outcome shape** already specified in EM-005 (`status, preferred_label, suggested_next_ids, context_updates, notes, kind, payload`) with the closed status enum `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`.
- **Failure taxonomy** already specified in §8 (6 classes: `transient, structural, deterministic, canceled, budget_exhausted, compilation_loop`).
- **Schema-version contract** already specified in §6.4 (N-1 readable, per-schema integer field).

C1 must NOT re-litigate these; it must reference them, then add the genuinely-missing pieces. Recon's "Kilroy ships 8 node types" framing is misleading: harmonik has already abstracted away from the shape-named taxonomy. The relevant question is which concrete **agent-types** populate the `agentic` category (per `handler-contract.md §4.1`), not what Kilroy's `box`/`hexagon`/`parallelogram` map to.

---

## G1 — Node-type catalog

**Already established (cite):**
- The five categories are locked (`execution-model.md` EM-006).
- Agentic nodes carry `handler_ref` resolving to a registered handler (EM-007); non-agentic / gate / control-point / sub-workflow MUST NOT (EM-007).
- Per-node-type idempotency-class defaults (EM-010): reviewer/researcher/lint/test/typecheck/analysis → `idempotent`; builder/merge → `non-idempotent`.
- Four-axis tags REQUIRED on every node (EM-011 → architecture.md §4.1, §4.2).
- Control-point node-type binding exists in `control-points.md` (CP-036, the `gate_ref`/`policy_ref`/`freedom_profile_ref`/`budget_ref` attribute set).

**Genuinely open:**
1. **Within `agentic`, the concrete `agent_type` catalog** is not enumerated in one place. Hits scattered — `claude-code`, `pi`, `reviewer`, `implementer-initial`, `implementer-resume` appear in `handler-contract.md` and `execution-model.md`. C1 needs to either (a) declare a normative enum, (b) point at a registry in `handler-contract.md`, or (c) accept the open-set posture explicitly.
2. **Within `non-agentic`, what subtypes exist?** Lint, test, typecheck, build, merge are mentioned but no enum. Same posture decision as #1.
3. **`gate` vs. `control-point` boundary.** EM-006 lists them as separate node types. `control-points.md` CP-036 treats control-point as a node-attribute-flavor (`*_ref` set). Q1 from pass-1 — is `gate` a degenerate `control-point`, or genuinely distinct? Recon: Kilroy's `goal_gate=true` is a flag, not a node-type — harmonik already diverges here.
4. **`sub-workflow` node attributes.** EM-034/034a-c reference sub-workflow expansion but the *node-side* attributes (workflow_ref, version, input_mapping) are not consolidated.
5. **Per-node-type outcome surface.** Recon flags this (Attractor: handlers can return PARTIAL_SUCCESS for partial agent completion; gate nodes shouldn't). C1 should table which `status ∈ {SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` values are legal per node type.

**Candidate approaches (pass-4 picks):**
- **A.** Registry-as-spec: C1 declares the agent-type / non-agentic-subtype catalog inline, pinned at MVH; additions via amendment protocol. *Trade-off:* normative but couples C1 to every new handler.
- **B.** Registry-in-handler-contract: C1 cites `handler-contract.md §4.1` for agent-types and is silent on non-agentic subtypes. *Trade-off:* cleaner separation but leaves the validator without a canonical list.
- **C.** Open-set with required `idempotency_class` declaration: any node may declare any `agent_type` string; the catalog grows by convention; validators key off the axis-tags. *Trade-off:* maximally forward-compatible but loses the "Kilroy-style canonical catalog" virtue.

**Cross-component dependencies:**
- C1 ↔ C4 (control-points): Q1's resolution determines whether C4 is "control-point is its own node type" alignment or "control-point is a node-attribute-flavor" alignment.
- C1 ↔ C3 (handler-contract): the per-node-type outcome surface (point 5 above) is normatively pinned by C3's Outcome record but its *taxonomy* (which surface for which type) lives in C1.

---

## G2 — Edge-condition syntax (SHARED with C2)

**Already established (cite):**
- EM-002: edges carry `condition` (a "policy expression — see `control-points.md §6.4`").
- EM-041: cascade step (a) is "edges whose `condition` expressions evaluate true against the current run context and outcome."
- `control-points.md §6.4` is the cited authority on policy-expression syntax (referenced by EM-002).
- `control-points.md` defines a predicate language for guards (`node_type:<type>`-style predicates appear at line 600).

**Genuinely open:**
1. **What is the LHS whitelist for `condition` expressions?** Recon shows Kilroy uses `outcome=fail`, `outcome=needs_dod`. Harmonik has `outcome.status`, `outcome.preferred_label`, `outcome.failure_class` (informally), plus arbitrary `context_updates` keys. C1 must enumerate.
2. **Expression dialect.** Recon documents Kilroy's `key=value` literal form. Harmonik's `control-points.md §6.4` uses a more expressive predicate language (per the `node_type:<type>` syntax). The cut between "edge condition" and "guard predicate" is unclear — they may be the same language, or two related ones.
3. **Failed-edge-fallback.** EM-046a already specifies: empty match set → `structural` failure with reason `no_outgoing_edge_matches`. Closed; not actually open. Audit-item is **resolved**.
4. **Can `condition` read context keys outside a whitelist?** Open. Recon flags this as a real choice (free-form read = flexibility but breaks static-validation).

**Candidate approaches:**
- **A.** Adopt `control-points.md §6.4` predicate language wholesale; edge conditions and guard predicates are one dialect. *Trade-off:* single-language elegance but possibly over-powered for edge routing.
- **B.** Define a restricted edge-condition mini-language (Kilroy-style `key=value`) layered atop `control-points.md §6.4`'s predicates for guards. *Trade-off:* simpler edges but two grammars to teach.
- **C.** Reuse Outcome-shape field references only (`outcome.status`, `outcome.preferred_label`, etc.) — no context-key references at all in conditions. *Trade-off:* maximally static-checkable but limits routing power.

**Cross-component dependencies:**
- **G2 is shared with C2** by design — C2 owns *evaluation* of conditions at dispatch; C1 owns *syntax*.
- C1 ↔ C3: the LHS whitelist on the Outcome side depends on which Outcome fields C3 exposes (currently `status, preferred_label, suggested_next_ids, context_updates, notes, kind`).
- C1 ↔ control-points.md §6.4: must explicitly declare whether edge conditions = guard predicates or are a subset/superset.

---

## G5 — Failure-routing cascade (SHARED with C2, C3)

**Already established (cite):**
- `execution-model.md §8` defines 6 failure classes (`transient, structural, deterministic, canceled, budget_exhausted, compilation_loop`). The audit claim "harmonik has not picked which set is canonical" is **stale** — harmonik's classes diverge from Attractor's (renames + addition of `compilation_loop`) but the set is locked.
- Classification is mechanism-tagged: derived from handler-returned `ErrX` sentinels per `handler-contract.md §4.5`, not from semantic judgment. Authority is **status + sentinel**, not a free-form `failure_class` field on Outcome.
- EM-046a: empty edge-match → `structural` failure.
- EM-046b: RETRY status → re-dispatch protocol with attempt-cap, reclassifies to `transient` on cap.
- §8.1: `transient` cap-exhaustion reclassifies to `structural`.
- §8 explicit disjointness: `structural` and `compilation_loop` do not overlap at emission.

**Genuinely open:**
1. **Is `failure_class` a routable LHS in edge `condition` expressions?** The §8 classes exist, but no §4 requirement says `condition="failure_class=transient"` is legal. C1 should specify yes-or-no (and if yes, the LHS whitelist must include it — links to G2).
2. **`retry_target` / `fallback_retry_target` analogues.** Recon (Attractor §3.7) defines a retry cascade `fail-edge → retry_target → fallback_retry_target → terminate`. Harmonik's EM-046b handles RETRY differently (same-node re-dispatch). The graph-level *retry-target* attribute is NOT in execution-model — it may be a missing primitive or a deliberate omission (handle by `condition="status=FAIL"` edges instead).
3. **Per-node `max_retries` vs. graph-level `default_max_retry`.** Recon flags Kilroy's defaults (3, contested 50). Harmonik's retry policy is referenced (`node's retry policy`) but neither attribute nor default is declared in `execution-model.md` that I found. C1 may need to add this OR cite it elsewhere.
4. **Failure-class authority tension (Kilroy spec-vs-code).** Already resolved in harmonik's favor: §8 says classification is from `ErrX` sentinel, period. `failure_class` is not on Outcome (it's a payload field on `run_failed` event). This is a *better* position than Kilroy's. C1 must keep it.

**Candidate approaches:**
- **A.** No new attributes; failure routing entirely via `condition="status=FAIL"` edges plus the EM-046b RETRY protocol. *Trade-off:* minimalist; matches existing posture; but no first-class retry-target.
- **B.** Add `retry_target` / `fallback_retry_target` as optional node attributes (Attractor parity). *Trade-off:* richer; matches Attractor recon; but introduces a second routing channel alongside the edge cascade.
- **C.** Add `failure_class` to the edge-condition LHS whitelist (so authors can route on class without exposing the field on Outcome). *Trade-off:* granular routing; preserves classification authority; but couples edge syntax to the §8 enum.

**Cross-component dependencies:**
- **G5 is shared with C2 and C3.** C2 owns dispatch-time routing decisions; C3 owns whether `failure_class` becomes an Outcome field; C1 owns the *vocabulary* (which classes exist and whether edge conditions can name them).
- Resolution of A vs. B affects sub-workflow expansion (EM-034) — does a sub-workflow's terminal FAIL cascade out via the parent's retry_target, or only via the parent's outgoing edges?

---

## G6 — Schema versioning + repo convention

**Already established (cite):**
- `execution-model.md §6.4`: all schemas carry `schema_version` integer; N-1 readable per `operator-nfr.md §4.5`; additive changes bump; renaming/removing is breaking.
- `architecture.md §4.10`: three-artifact separation — DOT is the workflow artifact.
- `control-points.md` CP-036: DOT references YAML policy by `name`; no inline policy bodies.
- Workflows have a `workflow_id, name, version` per EM-001 — version is graph-level.

**Genuinely open:**
1. **Where do `.dot` files live in-repo?** No spec mandates a path. Problem-space proposed `specs/examples/*.dot` for canonical examples and `.harmonik/workflows/` for project-local workflows. C1 must declare both (or just the spec-examples path and leave project-local out of scope).
2. **Unknown-attribute policy** (Q3 from pass-1). Three postures: (a) refuse-to-run strict; (b) warn-and-continue forward-compat; (c) refuse-to-run for unknown node attributes but warn for unknown edge attributes. Harmonik's §6.4 N-1-readable contract leans toward (b) for *known fields with newer values* but is silent on *unknown fields*.
3. **Graph-level `schema_version` attribute on the DOT** is not currently mandated. EM-001 has a graph-level `version` but that's the workflow's own version, not the schema-format version. Two distinct concepts; C1 must declare both required.
4. **Per-node `schema_version`** (Q4 from pass-1). Open. Lean: no — graph-level is sufficient because additive changes per §6.4 are by-field, not by-node-type.
5. **Forward-compat for unknown node-type values.** EM-006 says `type ∈ {agentic, non-agentic, gate, control-point, sub-workflow}` — closed enum. A v2-introduced sixth type would fail v1 validators. This is consistent with §6.4 ("renaming or removing fields is breaking") but the *additive-type* case isn't explicit.

**Candidate approaches:**
- **A.** Strict: graph-level `schema_version` REQUIRED; unknown attributes → ingest error. *Trade-off:* clean static guarantee; painful for forward-compat.
- **B.** Permissive: graph-level `schema_version` REQUIRED; unknown attributes → warning event, attribute dropped silently from ingested AST. *Trade-off:* easier rollouts; harder to debug "why didn't my new attribute do anything."
- **C.** Mixed: REQUIRED + strict for node-type (EM-006 enum) and reserved attributes (`type`, `handler_ref`, `idempotency_class`); permissive for everything else. *Trade-off:* aligns with §6.4's "additive change" posture; two-tier mental model.

**Cross-component dependencies:**
- C1 ↔ C5 (examples): the in-repo path declared by C1 is *exercised* by C5's `specs/examples/*.dot` files. C5 cannot proceed without C1's decision on path convention.
- C1 ↔ C2 (validator): the unknown-attribute policy is *implemented* by C2's parser/validator. Same decision; different specs own different facets.

---

## Most-confident-yet-unresolved question for pass-4

**Is `failure_class` a permitted LHS on edge `condition` expressions, or is failure routing strictly via `status=FAIL` edges plus the §8 classification feeding `run_failed` event payloads only?**

This is the load-bearing call for pass-4 because:

1. It resolves G5 (failure-routing vocabulary) AND constrains G2 (LHS whitelist) AND determines whether `retry_target` / `fallback_retry_target` are needed at all (B becomes redundant if `condition="failure_class=transient"` is legal).
2. It pins the relationship between harmonik's `ErrX`-sentinel-driven classification (mechanism-tagged, locked) and the cognition-adjacent question of "does the workflow author *route on* the class, or only *observe* it after termination?"
3. The Kilroy-vs-Attractor tension recon flags is reframed cleanly: harmonik already picked status-as-primary (via ErrX sentinels), so the question is no longer "which authority" but "how much expressivity do edge authors get."

Pass-4 should pick this first; G1/G2/G5/G6 all narrow once it's settled.
