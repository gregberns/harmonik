# D-verdict-surfacing Design — Reviewer Verdict Lives on `preferred_label` (Pass-4, phase-3-dot)

## Decision

**Option A — adopt Attractor's `preferred_label`.** A reviewer node returns `Outcome{status=SUCCESS, preferred_label="approved"}` on accept and `Outcome{status=FAIL, preferred_label="changes_requested"}` (or `Outcome{status=SUCCESS, preferred_label="changes_requested"}` for advisory review-loops) on reject. Edges discriminate the verdict via the standard 5-step cascade — step (b) `preferred_label` match — using D5's equality dialect: `condition="outcome.preferred_label == 'approved'"`. No new Outcome field is invented.

This closes the C5 review-loop example's open question and resolves what pass-3 SUMMARY listed as a separate decision (the verdict-surfacing decision, sometimes numbered D6 in earlier drafts).

## Framings considered

- **A. Use `preferred_label` (this decision)** — verdict is a string label the handler sets; cascade routes via step (b). Exactly Attractor's idiom. Zero new schema. Already covered by D4 row 3 (`outcome.preferred_label` is a whitelisted LHS).
- **B. Add a dedicated new field on Outcome (e.g., `verdict`)** — gives reviewers a named field that is "obviously theirs." Requires schema bump; adds a field used by exactly one node type; invents a new vocabulary where Attractor already has one.
- **C. Use a context-bag entry (`context.last_verdict`)** — verdict written via `Outcome.context_updates = {last_verdict: "approve"}`, edges route via D4 row 5 (`context.last_verdict == "approve"`). Already partially attested by EM-015d's reserved keys.
- **D. Encode in response shape — e.g., `kind=verdict` payload with structured rationale** — full first-class verdict object with reviewer identity, timestamp, rationale fields. Maximally rich; massive over-build for v1.

## Why A wins

- **A is exactly what Attractor specifies.** Attractor §3.3 introduces `preferred_label` precisely for the "node expresses a routing preference; engine still owns selection" case. A reviewer verdict is the archetypal case of this pattern: the reviewer knows what verdict it issued and wants the cascade to route accordingly, but routing remains the engine's responsibility. Per D-attractor-adoption, the burden of justification falls on divergence, not adoption — and there is no harmonik-specific reason to deviate here.

- **B (new field) is reinvention.** A dedicated `verdict` field on Outcome would do exactly what `preferred_label` already does. The only argument for B is "verdicts feel important enough to deserve their own field." That is aesthetic, not normative. EM-005's locked field set is already extended once (by `failure_class` in D2 with a clear necessity argument); extending it again without necessity violates the minimum-surface discipline.

- **C (context bag) works but is awkward for the primary signal.** Context entries are the right home for *workflow-wide* state (iteration count, last diff hash, accumulated decisions). The reviewer's verdict on *this* review is per-node terminal output, not workflow state — it belongs on the Outcome of the producing node, not in the shared bag. Additionally, EM-015d's `context.last_verdict` is already reserved, but it is reserved as a *mirror* of the per-review outcome, not as the *primary* surface. C5 review-loop can use `context.last_verdict` for the *trailing* "what was the last review's verdict?" question while the per-review edge routes on `outcome.preferred_label` for the *current* review's verdict.

- **D (kind=verdict payload) is overkill for v1.** A first-class verdict-payload type would mirror D7's `kind=gate_decision` shape and is structurally clean for capturing rationale-evidence-actor triples. But: at v1 the reviewer is a single Claude session emitting an APPROVE/BLOCK string; there is no rationale schema, no actor field beyond "the reviewer," and no consumer that reads structured rationale. Building the payload before the consumer is gold-plating. If post-MVH reviewer-evidence-aggregation lands, D-verdict-surfacing can be additively extended by introducing a `kind=verdict` payload alongside `preferred_label`. Until then, A.

## Implications

- **D4 row 3 is the LHS surface.** `outcome.preferred_label` is already whitelisted. No additional D4 changes.
- **D5 grammar covers this case.** `outcome.preferred_label == "approved"` is a valid D5 expression. No grammar extension needed.
- **EM-015d alignment.** The review-loop spec must clarify that `context.last_verdict` is a *mirror* of the reviewer node's `preferred_label`, updated by the reviewer's `context_updates`. The cascade routing for *this iteration's* verdict reads `outcome.preferred_label`; routing on *prior iteration's* verdict reads `context.last_verdict`. Pass-5 owns prose.
- **Canonical label values.** Lean: `{"approved", "changes_requested", "blocked"}` as the v1 reviewer label vocabulary. Not closed at D-verdict-surfacing — pass-5 picks the exact strings. The decision here is the *carrier*, not the *vocabulary*.
- **Reviewer handler contract.** HC prose for the reviewer node type should state: handler MUST set `preferred_label` to a recognized verdict string; absent label is a `structural` failure (no routing surface).

## Trade-offs accepted

- **`preferred_label` is overloaded across node types.** Reviewer nodes use it for verdicts; other agentic nodes use it for "the agent thinks edge X is the right one." This is fine — both usages have the same semantics ("node-expressed routing preference"); the *interpretation* is per-workflow, not per-field. The cascade does not care what the string means, only that it matches an edge label.
- **No structured rationale at v1.** A reviewer that wants to communicate *why* it blocked uses `notes` (freeform string, not routable). Consumers that want machine-readable rationale must wait for D7's `kind=gate_decision` pattern to be lifted to reviewers, or for a future `kind=verdict` extension.
- **Verdict strings are not type-checked statically.** D4's `preferred_label` LHS allows arbitrary string RHS; a workflow author can typo `"aproved"` and the cascade silently misses. C2 validator may emit a *warning* (not an error) for edges whose `preferred_label` RHS is not in a known vocabulary, but the v1 dialect does not enforce a closed vocabulary on `preferred_label`. Acceptable; matches Attractor.

## Open follow-ups

1. **Canonical verdict-string set.** Pass-5 picks `approved` vs `approve` vs `APPROVE` vs `pass`, etc. Lean: `approved`, `changes_requested`, `blocked` (past-tense, lowercase, snake_case for multi-word).
2. **Reviewer node type vs. plain agentic node.** Whether the spec distinguishes "reviewer" as a sub-type of `agentic` or just describes it as "an agentic node whose handler emits verdict labels." Lean: no sub-type; reviewer is a pattern, not a type.
3. **C5 review-loop example wording.** The example will use both `outcome.preferred_label` (current) and `context.last_verdict` (prior). Pass-5 prose must explain the relationship to avoid confusing readers.
