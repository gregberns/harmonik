# Round 2 Skeptic Review — control-points.md v0.2

## Verdict

Round 1 did real work. CP-040a (envelope hash) closes the replay-safety hole
the round-1 critic hammered on — re-invocation is no longer gated on key
presence alone but on hash-match, with a typed `verdict_envelope_mismatch`
event and a Cat 6 escalation as the only legal re-invocation path. The
Registry INTERFACE in §6.1.7 turns a prose registry into a contract. CP-034b
closes the expression-cost DOS surface the round-1 implementer would have
flagged. GateVerdictRecord / HookVerdictRecord / SideEffect / CognitionMeta
are no longer prose. CP-049's split of syntactic-ingest-time vs
package-resolution-at-launch is a clean covering partition. CP-050 collapsing
to union-only (HC owns resolution) is the right scope retreat.

This is not a face-lift revision.

But the round-2 response pattern in two places is load-bearing-by-appendix:
§A.3's "registration-layer peerage" for Kind and §A.3's cel-go comparison
both answer round-1 challenges by adding rationale rather than by changing
the normative text. That is sometimes exactly right (rationale belongs in
A.3) and sometimes a verbal save. This review teases those apart. The 990-
line size is near the split threshold but — unlike execution-model at similar
length — the content is coherent: everything under §4 orbits a single
primitive (`ControlPoint`). The spec should not be split yet.

Recommendation: **proceed with one more pass focused on Challenges A and B
below**. Challenges C and D are scope-discipline notes that do not block
finalization but should be acknowledged in the revision history.

## Integration-fix audit

- **R1 "persisted verdict keyed only by control_point+run+transition" → CP-040a
  envelope hash.** Genuine, load-bearing fix. The hash is now the replay-safety
  pivot. But its *content* is under-specified — see Challenge A.
- **R1 "expr-lang grammar unbounded" → CP-034b cost ceiling.** Genuine fix in
  direction; under-specified in mechanism. See Challenge B.
- **R1 "cel-go has a protobuf-typed environment, why pick expr-lang?" → §A.3
  rationale.** Partial fix. The rationale acknowledges cel-go was considered
  but does not engage cel-go's actual advantage (protobuf-schema-typed
  environment catches type drift at policy-ingest). The decision stands on
  Go-native + published safety profile + workflow-ingest type-check — which
  is a real answer but a thin one. Acceptable because §A.3 is rationale, not
  normative.
- **R1 "Budget-as-Kind is disjoint from Gate/Hook/Guard at every column" →
  §A.3 registration-layer peerage.** Verbal save with normative substance
  under the hood — see Challenge C. Not a blocker but worth acknowledging.
- **R1 CP-050 over-reach on provisioning mechanics → collapsed to union-only.**
  Clean retreat. HC owns resolution. This is the best kind of r1 fix: less
  spec, not more.
- **R1 INTERFACE Registry missing → §6.1.7.** Genuine fix. The five methods
  cover every call-site the spec cites (LookupByName, LookupByTrigger,
  LookupByAttachPoint, All, Register).
- **R1 CP-INV-004/005 restatements → retired with non-reuse note.** Honest
  retirement; §5 now carries the three invariants that actually pass the
  selection test.

Net: round-1 got ~90% real resolution. The two under-finished pieces are
envelope-hash content (Challenge A) and cost-metric specification (Challenge
B). Both are in the normative body, not the rationale — and both are
mechanically testable, which means leaving them vague pushes cost to
implementers and to future reviewers.

## Challenges

### Challenge A — CP-040a envelope hash content is under-specified at the `context` subset

CP-040a names five inputs to the hash: (a) evaluator input snapshot, (b) the
`context` subset reachable from the evaluator's expression or prompt template,
(c) `prompt_template_ref` version, (d) `input_schema_ref` and
`response_schema_ref` versions, (e) skill-package versions.

(a), (c), (d), (e) are mechanical. (b) is not. "The `context` subset reachable
from the evaluator's expression or prompt template" requires two mechanisms
the spec does not provide:

1. **Static reachability analysis on an expr-lang expression.** For a
   mechanism-tagged evaluator the expression is parsed and type-checked at
   workflow-ingest (§6.4). The spec does not say whether the AST walker also
   produces a minimal `context`-path set as a by-product. If it does not, (b)
   is not computable. If it does, the AST-walker output is part of the trust
   base (a bug there silently changes the hash surface) and should be named.
2. **Reachability from a prompt template.** Prompt templates are provisioned
   via a skill (§4.11, §6.1.5 `prompt_template_ref`). The templating
   language is unspecified. A template that references `context.foo.bar`
   through a templating helper is NOT the same static surface as an expr-lang
   expression. Until the prompt templating language is pinned (which §4.11
   explicitly punts to handler-contract), CP-040a's (b) cannot be implemented
   for cognition-tagged evaluators.

Consequence: an implementer today could legally hash the *entire* `context`
map (conservative; any context change busts the hash and escalates to Cat 6),
or could legally hash only the expr-lang-referenced subset (tight; requires
the AST walker). Those two implementations produce different hashes on the
same inputs, which breaks replay across implementations and silently
nullifies CP-INV-003.

Proposed fix: pick one. Either (i) name conservative whole-context hashing
as the MVH default and a `reachability-mode` flag as an OQ, or (ii) require
the AST walker to emit the reachable-path set as a first-class output of
workflow-ingest and defer cognition-tagged envelope-hash for Hooks with
prompt templates to a follow-up OQ. Option (i) is cheaper and testable now;
option (ii) is tighter but requires §4.11 and handler-contract to co-move.

### Challenge B — CP-034b "cost" is underspecified; three distinct metrics are bundled

CP-034b says policy-expression evaluation MUST be bound by "a harmonik-level
cost ceiling (max AST nodes, max evaluation duration)" and names
`expr.MaxNodes(...)` and `expr.Timeout(...)` as the two knobs.

Three distinct metrics are in play and the spec conflates two:

1. **AST node count.** Static; computed at parse time; bounds the *compiled*
   expression shape. `expr.MaxNodes` is this knob.
2. **Evaluation wall-clock duration.** Dynamic; bounds how long a single
   evaluation can run. `expr.Timeout` is this knob.
3. **Evaluation *step* count** (number of AST visits during evaluation).
   Deterministic; bounds compute without wall-clock nondeterminism.
   `expr-lang/expr` does not ship a step-counter at the time of writing.

Only metrics 1 and 2 are named. Metric 3 is absent. This matters because (2)
is nondeterministic — a timeout fires based on wall-clock and will produce
different verdicts on a slow CI runner versus a fast dev box. For a spec
that names `io-determinism=deterministic` on CP-034b's axes line, admitting
a wall-clock dependence in the enforcement mechanism is inconsistent. The
axis tag claims deterministic; the mechanism is not.

`policy_expression_exceeded_cost` also does not carry a discriminator for
which bound fired. An event that means "(1) static-bound exceeded" vs
"(2) wall-clock-timeout fired" vs "(3) step-budget exceeded" is three
different failure modes. Operators investigating a flood of these events
will want the discriminator; re-adding it post-MVH is a breaking event-
payload change.

Proposed fix: (a) split the event payload (or emit three event variants) so
the bound-that-fired is visible; (b) either drop `io-determinism=deterministic`
from CP-034b's axes line (accepting that wall-clock timeouts are best-effort)
or specify that the MVH implementation MUST use a step-counter proxy and
only fall back to wall-clock as a safety net; (c) name AST-node count as
the primary mechanism and wall-clock as the backstop, not as peers.

### Challenge C — "Kind peerage at the registration layer" is a real distinction, not a verbal save

The round-1 critic argued that because the §4.1.CP-005 table shows disjoint
trigger / evaluator-input / outcome-action columns, the four Kinds are not
structural peers and calling them "Kinds" over-promises. §A.3's response
enumerates what the four DO share: struct, registry, lifecycle, axis-tagging,
name namespace, schema_version discipline, single dispatch shape routed by
`kind` + `trigger`.

That enumeration is in fact load-bearing and not a verbal save. The single
dispatch shape — consuming subsystems (S01, S02, S05) route any ControlPoint
by `kind` + `trigger` — is a concrete invariant that would be broken by
demoting Budget to a sibling primitive. You would get two registration
paths, two YAML loaders, two axis-tag audits, two event-emission audits.

The rationale does what rationale is for. The normative text (§4.1) does
not need to change.

The thing the §A.3 rationale does NOT do is name the test for "is X a new
Kind or a new primitive?" The rationale says "Future additions (e.g., a
Throttle primitive) land as a fifth Kind iff they satisfy the same
registration-layer peerage test." It then does not name that test. A reader
deciding whether, say, a `Probe` (periodic-sampling evaluator) is a Kind
or a sibling will re-derive it. Cheap fix: enumerate the four conditions
(single registry, single lifecycle, single axis-tagging surface, single
dispatch shape by `kind`+`trigger`) as the explicit test, not as a prose
list.

This does not block finalization.

### Challenge D — 990 lines is near the split threshold but the content is coherent

The spec has grown to 990 lines across 12 sections. Execution-model at
similar length has been flagged as a split candidate. control-points is
different: every §4 subsection orbits a single primitive (ControlPoint) and
its Kind parameterization, plus the policy surface that configures it.
There is no second subject lurking.

The three sections that ARE candidates for extraction are:

- **§4.11 skill declaration (CP-049..CP-052).** Could live in handler-
  contract with a reference here. Deliberately stays because the DECLARATION
  surface is policy-layer and HC owns only RESOLUTION. Keep.
- **§4.6 freedom profiles (CP-028..CP-033).** These are policy content but
  not control-point content per se. An argument exists for a sibling
  `policy-roles.md`. Not worth doing now; there is no third subsystem
  consuming them independent of the control-point surface.
- **§4.8 cognition-tagged replay-safety (CP-039..CP-042).** Could migrate to
  reconciliation or to a dedicated `replay-safety.md`. It stays because the
  persisted-verdict primitive is produced by ControlPoints; consumers
  elsewhere (reconciliation.md §4.5) are read-only consumers.

Verdict: do not split. Length alone is not a smell when the content is
coherent. Revisit at v0.3 if CP-039..CP-042 grows substantially (Cat 6
verdict vocabulary lives in reconciliation.md and may drag §4.8 along).

## Summary of recommendations

1. **Challenge A (envelope-hash content).** Pick conservative-whole-context
   as MVH default OR require AST-walker reachability output. Blocks
   interoperable replay across implementations.
2. **Challenge B (cost metric).** Split the three metrics; add event-payload
   discriminator; reconcile the `io-determinism=deterministic` axis with the
   wall-clock mechanism. Blocks clean failure diagnosis.
3. **Challenge C (Kind peerage test).** Enumerate the four-condition
   peerage test explicitly in §A.3. Non-blocking.
4. **Challenge D (length).** No action. Content is coherent.

Challenges A and B are load-bearing. The rest is polish.
