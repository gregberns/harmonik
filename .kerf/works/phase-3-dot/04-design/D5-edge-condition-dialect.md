# D5 Design — Edge-Condition Dialect (Pass-4, phase-3-dot)

## Decision

**Restricted equality mini-language.** Edge `condition` expressions are conjunctions of equality comparisons over the D4 LHS whitelist:

    condition := comparison ( " && " comparison )*
    comparison := lhs " == " literal  |  lhs " != " literal
    lhs := one of D4's whitelist (outcome.status | outcome.failure_class |
           outcome.preferred_label | outcome.kind | context.<key>)
    literal := double-quoted string

No disjunction (`||`), no negation operator beyond `!=`, no arithmetic, no function calls, no parenthesized grouping. Disjunction is expressed by authoring multiple edges from the same source node (the EM-041 cascade naturally handles "match the first true edge"). Comparison is case-sensitive string equality against the D4-declared value spaces (enum values for status/failure_class/kind; arbitrary string for preferred_label and context.*). An absent LHS (e.g., `outcome.failure_class` on a SUCCESS outcome) evaluates `==` to false and `!=` to true. The empty condition string is reserved as "always match" (per EM-002 prose for unconditional edges).

## Framings considered

- **A. Adopt `control-points.md §6.4` predicate language wholesale** — edge conditions and guard predicates are one dialect. Already includes `node_type:<type>`-style predicates. Single language to maintain; possibly over-powered (predicates designed for guards include things edges don't need).
- **B. Restricted equality mini-language (this decision)** — `lhs == literal` and `&&` only. Smallest grammar that closes G2; statically validatable against the D4 whitelist trivially.
- **C. Full CEL or similar expression language** — Google Common Expression Language or a JS-subset. Maximum power; introduces a third-party grammar dependency; vast overkill for v1.

## Why this framing

- **C is gold-plating.** No pass-1 G-gap requires arithmetic, function calls, or list operations on the routing surface. Adopting CEL (or any general expression language) buys flexibility we have zero evidence we need and saddles the parser, validator, and reviewer with a grammar they don't fully use.
- **A is plausible but premature unification.** `control-points.md §6.4`'s predicate language exists to express guard conditions, which historically have a richer surface (node_type matching, role checks, mechanism-tag presence). Edge-routing has narrower needs — every realistic edge condition in recon (Kilroy, Attractor) is `key=value` or a small conjunction. Unifying now locks edge-conditions to whatever §6.4 evolves into; keeping them separate at v1 lets each evolve. The boundary D5 draws ("edges = equality + &&; guards = §6.4") is the simplest one consistent with current usage.
- **B closes G2 with the smallest grammar.** Every recon-attested edge-condition pattern fits:
  - `outcome.status == "FAIL"` — failure routing
  - `outcome.failure_class == "transient"` — D1's archetypal case
  - `outcome.status == "SUCCESS" && context.last_verdict == "APPROVE"` — C5 review-loop close-terminal
  - `outcome.status == "SUCCESS" && context.last_verdict == "BLOCK"` — C5 review-loop close-needs-attention
  Each is a one-liner; the grammar is teachable in three sentences.
- **Disjunction-as-multiple-edges is idiomatic for routing graphs.** EM-041's 5-step cascade already evaluates edges in `ordering_key` order and picks the first matching one. "Match transient OR canceled" is naturally two edges (each routing identically); the cascade handles the OR via iteration. This eliminates the need for `||` syntax without losing expressivity.
- **No negation beyond `!=` keeps static validation trivial.** A validator checking "is the RHS a legal value for this LHS?" is a single closed-enum lookup per comparison. No precedence rules, no truth-table reasoning.
- **String-only literals match the D4 surface.** Every D4 LHS is string-typed (or absent). Reserving non-string literals (numbers, booleans) for a future amendment is cheap and keeps v1 unambiguous.
- **Empty condition = unconditional edge.** Aligns with EM-002 and recon convention. The cascade treats empty-condition edges as always-matching for step (a).

## What this unblocks / interacts with

- **D4.** D4 defined the LHS surface; D5 defines the operator + RHS surface. The pair is now end-to-end writable in C1's edge-condition section.
- **D1.** D1 admitted `failure_class` as LHS; D5 confirms the comparison form is `outcome.failure_class == "transient"` (quoted literal, case-sensitive).
- **C2 evaluator.** Gets a trivial implementation: tokenize on `&&`, split each comparison on ` == ` / ` != `, look up LHS in a small dispatch table, string-compare. No expression-tree, no precedence resolver.
- **C1 grammar section.** D5 is the EBNF; can be transcribed into `workflow-graph.md` verbatim.
- **G2 (edge-cascade scenarios).** Now writable. The C5 review-loop example's edge conditions are expressible.
- **D6 (verdict surfacing).** D6 must pick whether the review verdict lives as a `context.<key>` (route via `context.last_verdict == "APPROVE"`) or as `outcome.preferred_label` (route via `outcome.preferred_label == "approve"`). Both are expressible under D5; D6 picks which.
- **Guard-predicate boundary.** D5 explicitly does NOT subsume `control-points.md §6.4`. Guards remain on §6.4's richer predicate language. C4 owns the boundary.

## Open questions deferred

1. **Whitespace tolerance around `==` / `&&`.** Whether `outcome.status=="FAIL"` (no spaces) parses identically to `outcome.status == "FAIL"`. Lean: tolerant lexer, normalize on parse. Pass-5 spec prose owns.
2. **Escape sequences inside quoted literals.** §8 enum values and EM-005 status values contain no special characters; preferred_label and context.* values *could*. Lean: forbid embedded quotes at v1 (no escape syntax); if a use case appears, additive amendment. Pass-5 owns.
3. **Future operator additions.** Numeric comparison (`<`, `>`) becomes relevant if a future LHS is numeric (e.g., `context.iteration_count`). EM-015d hardcodes iteration_count cap = 3 currently; no graph-level comparison needed. Additive amendment when needed.
4. **Disjunction sugar.** If multi-edge expansion becomes unwieldy in practice (e.g., "match any of 5 failure classes"), `||` may be admitted as a future additive amendment. Not v1.
5. **Boolean/null literals.** Deferred until a non-string-typed LHS is admitted. D4's current whitelist is all-string.
