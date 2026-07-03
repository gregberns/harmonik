# D4 Design — Edge-Condition LHS Whitelist (Pass-4, phase-3-dot)

## Decision

**Closed whitelist.** Edge `condition` expressions may reference exactly the following left-hand-side identifiers:

1. `outcome.status` — string, one of `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` (EM-005 locked enum).
2. `outcome.failure_class` — string, one of the §8 6-class enum, absent when `status != FAIL` (D1 + D2).
3. `outcome.preferred_label` — string, absent when handler did not set one (EM-002/EM-005).
4. `outcome.kind` — string discriminator, defaults to `"default"` (EM-005a).
5. `context.<key>` — only keys registered for the workflow per D8's per-workflow registered-key list. Reading an unregistered `context.<key>` is a static validation error.

`outcome.suggested_next_ids`, `outcome.notes`, and `outcome.payload` are **not** routable LHS. `suggested_next_ids` is a cascade input at step (c) of EM-041, not a step-(a) routing input. `notes` is freeform unparseable text. `payload` is `kind`-discriminated and shape-variable — D7's `gate_decision` payload may eventually expose nested fields, but D4 does not admit them at v1.

## Framings considered

- **A. Closed whitelist (this decision)** — enumerate every legal LHS. Static-validatable; trivial to evolve via additive schema bumps. Forces D8 to land before `context.*` works.
- **B. Open `outcome.*` + open `context.*`** — author may reference any field. Maximum flexibility; cannot be statically validated; ties workflow files to internal Outcome structure (breaks N-1 readability).
- **C. Outcome-only, no context references** — pure `outcome.*` whitelist; `context.*` forbidden entirely. Maximally static; loses the verdict/iteration-count routing that the C5 review-loop example needs.

## Why this framing

- **B is incompatible with §6.4 schema discipline.** N-1 readability requires that the field surface a workflow references be stable. Open `outcome.*` means any handler emitting a new payload field implicitly extends the routing vocabulary, with no schema gate. Workflows authored against today's Outcome would silently start matching/missing under tomorrow's. Closed whitelist forces evolution through additive schema bumps (EM-005 schema_version), which is the locked discipline.
- **C breaks C5 review-loop.** EM-015d's review-loop reserves `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash` as context keys. The example needs `condition="context.last_verdict == 'BLOCK'"` to differentiate the close-needs-attention terminal. Banning `context.*` entirely re-opens D6 (verdict surfacing) as "must promote to Outcome field" — an unwanted ripple.
- **A is the smallest closed surface that closes G2.** Five LHS identifiers, every one already a normatively-defined structured field. `outcome.kind` admission costs nothing — it's already a top-level enum-typed field — and pre-positions D7's `gate_decision` discriminator for routing without committing to payload-field LHS.
- **D1 result lands as row 2.** Per D1, `outcome.failure_class` is admitted. This is the failure_class row of the whitelist.
- **`context.<key>` is admitted but gated on D8.** D8 picks the per-workflow registered-key discipline (research lean). Until D8 lands, `context.*` references are syntactically legal but fail static validation against an empty registered-key list. Workflows that don't use `context.*` are unaffected.
- **No `outcome.payload.*` at v1.** Nested-field references inside `payload` would require the validator to know which `kind`'s payload schema applies, which depends on the upstream node's type. The combinatorics aren't worth it for v1. Workflows that need to route on gate-decision rationale promote the decision through `preferred_label` or a registered `context.*` key.

## What this unblocks / interacts with

- **D1.** Row 2 of the whitelist is D1's `yes`.
- **D5 (condition dialect).** D4 defines the LHS surface; D5 defines the operator + RHS surface. D5 must accept string-equality against enum values (for status, failure_class, kind) and against arbitrary strings (for preferred_label, context.*).
- **D8 (`context_updates` typing).** D4 references D8's registered-key list. D8 must land before `context.*` LHS is usable end-to-end. D8 lean (per-workflow registered-key list) is compatible.
- **D7 (gate-node payload).** D4 admits `outcome.kind` as LHS, which lets workflows route on `kind == "gate_decision"` if D7 lands that shape. D7 payload internals remain unroutable.
- **C2 validator.** Gets a closed enum of legal LHS identifiers — straightforward static check.
- **G2 + G5 + Q6.** All closed by the D1+D4+D5 cluster once D5 lands.

## Open questions deferred

1. **`outcome.payload.<field>` routing post-D7.** If D7 lands `kind=gate_decision` with a stable payload schema, a future amendment may admit specific payload fields (e.g., `outcome.payload.policy_id`). Deferred to post-MVH; not v1.
2. **Nested context references.** Whether `context.review.verdict` (dotted) is legal vs. only flat `context.last_verdict`. D8 owns this; D4 only declares the `context.<key>` form.
3. **Case sensitivity of enum values.** Whether `outcome.status == "fail"` matches `FAIL`. D5 owns the comparison semantics; D4 only declares the LHS surface.
