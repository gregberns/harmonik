# D1 Design — Admit `failure_class` as Edge-Condition LHS (Pass-4, phase-3-dot)

## Decision

**Yes.** `failure_class` is a permitted left-hand-side reference in edge `condition` expressions. Edges MAY route on it via expressions of the form `outcome.failure_class == "<class>"` where `<class>` is one of the locked §8 6-class enum values. This admission is contingent on — and mechanically realized by — D2's resolution that `failure_class` is a top-level optional field on `Outcome` (present-when-FAIL).

## Framings considered

- **A. Admit `failure_class` as LHS** — workflow authors can write `condition="outcome.failure_class == 'transient'"`. Closes G5 cleanly; couples edge syntax to the §8 enum (which is locked, so coupling is fine).
- **B. Refuse — failure routing strictly via `status=FAIL` edges only** — the cascade evaluates `outcome.status` and nothing else for failure routing; the §8 class surfaces only on the `run_failed` event payload. Minimal vocabulary but cannot express "retry on transient, terminate on structural" within the graph.
- **C. Admit as LHS but via a separate dialect (predicate sub-language)** — e.g., guard-style `failure:transient` predicate distinct from `outcome.*` references. Two grammars for one routing surface; teaches twice.

## Why this framing

- **B forecloses G2 + G5 simultaneously.** Pass-1 G5 ("failed-edge cascade routing on failure class") is the canonical scenario for differentiated retry/terminate behavior. Recon (Attractor §3.7) treats this as a routine workflow capability. Refusing it forces every failure-routing workflow to either (a) push classification into `context_updates` (which D8 wants to keep narrow) or (b) live with status-only routing and add a `retry_target` node attribute (a parallel routing channel — research lean D16α explicitly rejects this).
- **C invents a second grammar.** D5 picks the operator/RHS surface; admitting `failure_class` under that same dialect (D5) keeps one grammar. A separate `failure:<class>` predicate would only make sense if guard-predicates and edge-conditions were already separate languages; D5 will collapse that distinction (or keep edge-conditions strictly smaller).
- **A is mechanically free after D2.** D2 placed `failure_class` as a top-level structured field. Once a structured field exists, admitting it as LHS is a vocabulary-only call; the evaluator already needs to read structured `Outcome` fields for `outcome.status`. No new evaluator capability.
- **Classification authority is unchanged.** D1 is a routing-vocabulary call, not an authority call. The daemon's §8/HC-020 sentinel path still classifies; the edge condition reads the classified value. Pass-3 SUMMARY item #13 is preserved.
- **`compilation_loop` is admissible as a value.** Even though `compilation_loop` is daemon-only-settable (D2 follow-up #2), it is still a legitimate routing target — a workflow author can write `condition="outcome.failure_class == 'compilation_loop'"` and the cascade matches when the daemon sets it. Authority-to-set is orthogonal to authority-to-route-on.

## What this unblocks / interacts with

- **D4 (LHS whitelist).** D1's `yes` is the `failure_class` row in D4's full whitelist. D4 enumerates the remaining fields.
- **D5 (condition dialect).** D5 must accept a comparison operator suitable for the §8 enum (string equality at minimum). D1 does not pre-constrain D5 beyond requiring `outcome.failure_class == "<class>"` be expressible.
- **D16 (retry_target attribute).** D1 = yes makes retry_target redundant for failure-class-differentiated retry. Reinforces research lean D16α (status-primary only).
- **G2 edge-cascade scenarios.** Now writable end-to-end — review-loop-style examples can express "retry on transient, terminate on structural" without parallel routing channels.
- **C5 examples.** A `failure_class`-routed example becomes a candidate canonical example post-`review-loop.dot`.

## Open questions deferred

1. **Enum value spelling at the syntax level.** Whether `condition` expressions write the value bare (`transient`) or quoted (`"transient"`) is D5's call. D1 only commits the LHS reference path.
2. **Absent-field semantics.** When `outcome.status != FAIL`, `failure_class` is absent (per D2). An edge condition `outcome.failure_class == "transient"` against a SUCCESS outcome must evaluate to false (not error). D5 prose owns this.
3. **Schema-version drift on enum extension.** If §8 ever grows a 7th class, edge conditions referencing the new class on N-1 daemons are unmatched (graceful) but conditions that *don't* reference it still work. EM-005 schema_version bump prose owns this.
