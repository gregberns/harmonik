# D-attractor-adoption Design — Adopt Attractor's Outcome+Context Model Verbatim (Pass-4, phase-3-dot)

## Decision

**Adopt Attractor's outcome+context model verbatim; deviate only where harmonik has a specific reason.** This is the headline meta-decision underlying D1, D2, D4, D5, and the verdict-surfacing decision (D-verdict-surfacing). Where Attractor has a working answer, harmonik takes it as-is; the burden of justification falls on each *divergence*, not on each *adoption*.

## Items adopted verbatim

1. **Outcome envelope shape.** `{status, preferred_label, suggested_next_ids, context_updates, notes}` per Attractor `attractor-spec.md` §3. harmonik adds `failure_class` as a parallel typed field (see Divergences) but does not rename, drop, or restructure the five base fields. EM-005's locked field set already matches this shape.

2. **5-step edge-routing cascade.** condition match → preferred label → suggested-next-ids → weight → lexical tiebreak. Per Attractor §3.3 and harmonik EM-041. Each step is deterministic; the cascade picks the first matching edge. D5's restricted equality mini-language operates at step (a); D-verdict-surfacing routes via step (b).

3. **Context as a separate workflow-wide state bag.** A shared key-value store threaded through all nodes, mutated *only* via `Outcome.context_updates`. Nodes do not write context directly; the engine merges `context_updates` after each node completes. This is Attractor §3.4 verbatim, and matches harmonik's reserved-keys convention in EM-015d (`iteration_count`, `last_verdict`, etc.).

4. **Engine MUST fall back to unconditional edge — invariant.** Attractor audit V3.2 surfaced a real defect: if the cascade's first four steps find no match and there is an unconditional edge available, the engine must use it before declaring no-match. harmonik bakes this in from day one as a normative invariant on the cascade (D-edge-cascade-invariant). This is non-negotiable.

5. **Lowercase canonical status strings.** Status comparisons are case-sensitive against the lowercase enum `{success, fail, retry, partial_success}`. Attractor uses lowercase consistently; harmonik EM-005's current uppercase notation is a documentation drift, not a normative divergence. Pass-5 spec-draft normalizes to lowercase to align with Attractor and avoid case-folding edge cases in D5's evaluator.

## Genuine harmonik divergences

1. **`failure_class` as a parallel typed field on Outcome.** Attractor's status enum is 4-value (`success, fail, retry, partial_success`); Kilroy's failure taxonomy is 6-class (`transient, structural, deterministic, canceled, budget_exhausted, compilation_loop`). harmonik combines both — keeps Attractor's status at top level *plus* adds `failure_class` as a sibling field, present-when-FAIL. D2 owns this placement decision and explains why a sibling field beats stuffing the class into `status` or `notes`. The combination is clean, not a drift.

2. **JSONL event stream.** Attractor uses checkpoint-snapshot durability (`{logs_root}/checkpoint.json`); harmonik uses an append-only JSONL event stream (STATUS.md §3). The event surface is harmonik's, not Attractor's. Outcome contents are unchanged; the *transport* differs.

3. **Three-artifact spec/workflow/bead separation.** Attractor has one artifact (the DOT graph); harmonik has three (spec text, workflow `.dot`, bead instance). This shapes ingestion and reconciliation but does not alter Outcome semantics.

4. **Skill-injection via handler-contract.** harmonik's handlers inherit skills via HC contract injection; Attractor's handlers are statically configured. This is orthogonal to Outcome/context and does not require changes there.

## Cross-references

- **D2** holds: `status` at top level (Attractor) + `failure_class` at top level (Kilroy taxonomy) is a clean combination, not a drift. The two fields cooperate — `status=FAIL, failure_class=transient` is one coherent outcome, not two contradictory ones.

- **D4** narrows Attractor's open-context-key surface: Attractor allows `condition` expressions to reference any context key the workflow author chooses; D4's 5-LHS whitelist locks the surface to `outcome.status`, `outcome.failure_class`, `outcome.preferred_label`, `outcome.kind`, and `context.<registered-key>`. This is **tighter than Attractor** by design — narrow now, broaden later if a real workflow needs it. The justification is N-1 readability (§6.4): an open context surface couples workflow files to runtime field discovery, which breaks the schema-version contract.

- **D5** picks restricted equality + `&&` only. Attractor's spec is silent on the exact dialect (leaves it to the evaluator); harmonik picks the smallest grammar that covers every recon-attested pattern. This is **narrower than Attractor permits** but not divergent from any stated Attractor requirement.

- **D-verdict-surfacing** adopts Attractor's `preferred_label` as the verdict carrier. No new field invented.

- **D-edge-cascade-invariant** locks the audit-V3.2 fix as a normative day-one invariant.

## Rationale for the meta-decision

Attractor is a working spec with a public audit history (V1 → V3.2). Its outcome+context model has been exercised against `codergen`, `wait.human`, `conditional`, and `parallel` handler types and refined through audit. harmonik's recon (Kilroy, Attractor) found Attractor's model to be the better-defined of the two and one that harmonik can lift wholesale. Adopting verbatim minimizes invention surface, preserves the audit lineage, and lets harmonik focus design effort on the genuinely-novel parts (three-artifact separation, JSONL events, failure-class taxonomy, skill injection).

The alternative — re-derive Outcome shape from first principles — was rejected. There is no evidence the harmonik recon turned up a *requirement* Attractor's model fails to satisfy. Every gap the Phase-3 audit named is closed by either (a) adopting Attractor as-is or (b) one of the four named divergences above.

## Implications for pass-5 spec-draft

- EM-005 prose should cite Attractor §3 as the upstream source for the five Outcome fields. Schema-version provenance: "Outcome envelope adopted from Attractor v1.0; `failure_class` added by harmonik per D2."
- EM-041 cascade prose should cite Attractor §3.3 as upstream source and include the audit-V3.2 unconditional-edge invariant (per D-edge-cascade-invariant).
- Context-store prose (new section in execution-model.md) should cite Attractor §3.4 for the mutate-only-via-context_updates rule.
- The four divergences should be called out explicitly in a "Divergences from Attractor" subsection so future audits don't accidentally try to "fix" them back toward Attractor.

## Open follow-ups

1. **Status-string case normalization.** Pass-5 prose should pick lowercase canonical and either deprecate the uppercase notation in EM-005 or declare the two forms equivalent at the lexer. Lean: lowercase canonical, lexer is case-sensitive (no folding).
2. **`compilation_loop` as a harmonik-only class.** Attractor has no equivalent. Pass-5 should note this in the "Divergences" subsection and explain that the daemon-only-settable rule (D2 follow-up #2) is the consequence.
3. **Parallel handler.** Attractor's `parallel` node type with isolated context clones is *not* adopted at MVH (worktree model is one-per-run, sequential). Reserve language for post-MVH parallel fan-out per D20.
