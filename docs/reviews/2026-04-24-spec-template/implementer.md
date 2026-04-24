# Implementer Review — Spec Template v1.0

## Verdict summary

The template is usable end-to-end for a small-to-medium spec, and I got a full skeleton of `handler-contract.md` sketched without being blocked. But several rules that the template calls "mechanical" or "linter-enforced" require real judgment in practice, and a handful of shape questions (how to write a Go interface inline, how to scope an invariant, when an event belongs in §6 vs §7, how to tag a requirement that splits its work between mechanism and cognition) will generate inconsistency across specs unless tightened. About half my friction came from ambiguity in the axis-tagging rules, not from missing structure.

## Stress test: drafting handler-contract stub

I walked top-to-bottom, reserving prefix `HC`, and produced:

- **Front matter.** Fine. `depends-on` needed architecture, execution-model, event-model, process-lifecycle, control-points (co-dep); I had to decide whether control-points is depends-on or something else (template doesn't name a co-dep field even though `components.md` introduces the concept).
- **§1 Purpose.** Fine, one paragraph fitting the ≤200 word cap.
- **§2 Scope.** In-scope: Go interface, wire protocol, concurrency shape, context rules, error strategy, secrets/redaction, twin parity, skill injection. Out-of-scope: subsystem-internal adapter implementations (S04), LLM-output schema (handler-specific), twin-drift conformance (S07), per-agent-type skill shapes.
- **§3 Glossary.** Wrote entries for: handler, session, adapter, watcher, twin, LaunchSpec, skill-provisioning. Friction: the template says "do not duplicate `docs/foundation/` glossaries" but `docs/foundation/` has no single glossary file — I would be guessing at where the canonical definition lives. See Stuck #4.
- **§4 Normative requirements.** Drafted stubs HC-001..HC-030, grouped into:
  - §4.1 interface, §4.2 wire protocol, §4.3 concurrency, §4.4 context, §4.5 errors;
  - §4.6 error-propagation, §4.7 secrets, §4.8 twin parity, §4.9 ready-state;
  - §4.10 trust, §4.11 skill injection, §4.12 modularity boundary.
  
  Shape worked. Axis-tagging each requirement surfaced most of my rule-ambiguity findings:
  - HC-003 (wire protocol — LaunchSpec on stdin): `Tags: mechanism`, `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` — straightforward.
  - HC-012 (skill resolution): same axes — also straightforward.
  - HC-015 (handler MUST fail-launch when a required skill can't be provisioned): I wrote `idempotency=recoverable-non-idempotent`, but I'm not fully confident. "Recoverable-non-idempotent" is defined for *node-level* behavior in execution-model, not for launch-time failure. That choice alone took longer than drafting the normative sentence.
- **§5 Invariants.** Drafted HC-INV-001 "Handler contract is execution-shape-invariant" (from components §4.12) and HC-INV-002 "Twins and real handlers share interface, wire protocol, and event schema." Drawing the line between "invariant" and "ordinary requirement" was judgment (Stuck #1).
- **§6 Schemas.** Inline shapes for `LaunchSpec`, `Handler`, `Session`, `Outcome` (or a cross-ref), and the `agent_ready` event payload. Had to choose §6.1 pseudocode vs §6.2 JSON for a Go interface — the template gives a `RECORD` shape, nothing for method-bearing interfaces. See Stuck #2.
- **§7 Protocols.** Drafted the launch handshake (spawn → `handler_capabilities` → version negotiation → `agent_ready` → `skills_provisioned` → work) as pseudocode per §7.2, and the silent-hang state machine from §4.6 as a transition table per §7.1. Both forms fit; the §7.1 choice between pseudocode, table, ASCII was judgment with no selection rule (see Missing Guidance #1).
- **§8 Error taxonomy.** Drafted subsections for each sentinel:
  - `ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`, `ErrBudget` (from components §4.5);
  - `ErrProtocolMismatch` (from components §4.2 — version negotiation);
  - `ErrSkillProvisioningFailed` (from components §4.11).
  
  Template's "detection rule, default response, escalation path, emitted event type" fields were useful. I did not know whether the "detection rule" should be mechanism-tagged itself or whether the tag applies to the requirement that cites the class (I picked the former; not sure the linter agrees).
- **§9 Cross-references.** Fine for depends-on; §9.2 is "mechanical" but the template does not say what produces it (Stuck #5).
- **§10 Conformance.** Drafted "Core MVH" profile listing HC-001..HC-030 minus post-MVH items (binary signing HC-tbd, secret rotation). §10.2 required me to cite test layers from `testing.md` which I do not have — no blocker, but guesswork.
- **§11 Open questions.** Drafted OQ-HC-001 (stdin-vs-file LaunchSpec delivery, blocks HC-003) and OQ-HC-002 (silent-hang threshold T per agent type, blocks HC-021).
- **§12 Revision history.** Fine.

Every required section produced at least a skeleton. The §7 and §8 optional sections were both exercised, as was Appendix A.3 (Rationale, for the daemon-owned-watcher goroutine-ownership decision from components §4.3).

## Places I got stuck (numbered)

1. **Requirement vs invariant boundary.** Both carry the same tag lines; both use RFC 2119 keywords; both live in numbered blocks. The template says invariants "span multiple requirements or subsystems" but HC-INV-002 (twin parity) could equally well have been a normal §4.8 requirement, and the parity rule already has a normal requirement in that subsection. I cannot resolve which one is canonical. **Template should clarify:** is an invariant *a different kind of rule* or just *a presentation choice for a cross-cutting rule*? If the former, give a crisp selection test; if the latter, allow an "also-an-invariant" tag on a regular requirement instead of a duplicated block.

2. **Go interfaces have no schema shape.** §6.1 is pseudocode RECORD; §6.2 is wire format. A Go `Handler` / `Session` interface is neither — it is a method set with semantic contracts per method. I ended up forcing it into §6.1 with a non-standard `INTERFACE <Name>:` header, which the linter (if it parses §6.1) will reject. **Template should clarify:** add an `INTERFACE` shape sibling to `RECORD` in §6.1, or explicitly sanction §6.2 pseudocode-Go. This blocks every spec that declares a Go interface across a boundary (execution-model, handler-contract, control-points).

3. **Where do event payload schemas live when the event is OWNED by event-model?** `agent_ready`, `skills_provisioned`, `session_log_location`, `handler_capabilities` — handler-contract *emits* these, event-model *registers* them. Template says §6 must be present "even if 'none — see [other-spec.md §N]'" but doesn't resolve whether a partial delegation (emit here, schema there) fits the rule. I wrote §6 with both inline schemas and `[event-model.md §3.2]` pointers mixed, and I am not sure that's conformant.

4. **"Do not duplicate `docs/foundation/` glossaries" but which file?** There is no single-source glossary in `docs/foundation/`; terms are spread across `OVERVIEW.md`, `core-scope.md`, `components.md`, and `problem-space.md`. For `session` specifically, I could not find a canonical prior definition, so I defined it fresh. If another spec has already defined it, my spec drifts. **Template should clarify:** either name the canonical glossary file or acknowledge terms are defined in whichever spec first introduces them and cross-referenced thereafter.

5. **§9.2 "maintained mechanically by the spec-linter on `finalize`" — no such linter exists yet.** As a drafting agent I can't tell if I'm supposed to fill this in by hand for now, leave it `[]`, or gate finalization on the linter. Template should say explicitly "leave as `depended-on-by: []` in draft; finalization requires linter to populate."

6. **Conformance profiles without `testing.md`.** §10.2 cites `[testing.md §<layer>]` by convention, but `testing.md` may not exist when the first several specs are drafted. Template should state what to do in the bootstrap case (cite a test-surface obligation by prose + add an Open Question).

7. **Appendix numbering under `A.`.** The outline calls appendices "A"; the template lists A.1 Examples, A.2 Counter-examples, A.3 Rationale, A.4 Migration — but these are listed as "common appendices," not required numbering. If a spec has only a rationale appendix, does it become "A.1" or "A.3"? I picked `A.3` on the assumption the numbers are reserved slots. The template should state that explicitly: "When present, each appendix keeps its reserved number (A.1 = Examples, A.2 = Counter-examples, etc.); gaps are fine."

8. **No template guidance for citing `components.md` during bootstrap.** My actual source for handler-contract content is `docs/foundation/components.md §Component 4`, not another spec. The cross-ref convention is spec-to-spec; foundation docs are not specs. I ended up with ad-hoc `(source: docs/foundation/components.md §Component 4)` comments in the draft, which fails the convention. Template should either grant a transition-period exemption or state the canonical citation form for foundation docs (which do get superseded by specs once drafted).

## Ambiguous rules (numbered)

1. **Tag single vs plural.** §4.N+1 says "**Tags** — `mechanism` or `cognition`" — singular. HC-004 (skill resolution) is mechanism, but HC-011 (skill declaration consumed from a cognition-tagged workflow node) has both a mechanism surface and a cognition delegation. Three plausible interpretations:
   - (a) pick the dominant tag and name the other in prose;
   - (b) allow `Tags: mechanism, cognition`;
   - (c) split into two requirements.
   
   **Recommend:** pick (c) and state it explicitly — "If a requirement has both, split it." Otherwise every author will choose differently.

2. **`replay-safety=n/a` vs `safe` for pure-compute operations.** The `Handler` interface itself performs no I/O — but the operations it defines do. Does the *interface-definition* requirement (HC-001) carry axes at all? Template says "every requirement MUST carry" but interfaces are declarations, not runtime behaviors. Three plausible interpretations:
   - (a) interface-declaring requirements get `n/a` on all axes;
   - (b) they inherit the axes of the methods they declare;
   - (c) they're exempt.
   
   **Recommend:** (a) with an explicit sentence in §4.N+1 that declaration-only requirements use `n/a` across the board.

3. **"Every `MUST/SHOULD/MAY` appears within a numbered-requirement block."** The conformance checklist has a `MUST` in prose ("The drafting agent MUST check each box"). Does that clause apply to the template itself, or only to specs that use the template? Template should exempt its own meta-text, or rewrite the meta-text with lowercase must.

4. **"Heading numbering matches the outline."** Does §4 need subsections to match across specs (every spec has §4.1 = interface)? Or can each spec freely organize §4.1..§4.N? §4.N+2 says "grouping is informative" but the tooling invariants call it a numbering match. **Recommend:** clarify that §1..§12 are fixed; sub-section numbering under §4 is per-spec.

5. **Requirement-ID gap rule.** "Gaps allowed after deletions — never renumber." But does this apply to *draft* specs too? When I add HC-015 then realize I want it earlier, may I renumber (draft is pre-public) or am I already frozen? **Recommend:** IDs freeze at `status: reviewed`, not at `draft`.

6. **Cross-reference to a sub-sub-section.** §4.2 of handler-contract has bullet-labelled sub-points (a, b, c in components.md). The cross-ref convention allows `§N.N` but what about `§4.2(b)` or `§4.2.1`? Not specified. **Recommend:** allow `§N.N.N` for nested numbering and forbid `§N.N(letter)`.

7. **Outcome delivery — is it a requirement or a schema?** Components §4.2 says "Outcome delivery: event-based. Outcome is emitted as a final `outcome_emitted` event." I wrote a §4.2 requirement `HC-008 Outcome delivery is event-based` with axes, AND a §6.2 payload schema for `outcome_emitted`. That seems right, but the template gives no example of a requirement whose normative content is "a thing happens via a specific data shape." Three plausible interpretations:
   - (a) prose is normative, schema is illustrative;
   - (b) both are normative;
   - (c) schema is canonical and prose references it.
   
   Different specs will pick differently. **Recommend:** (a), with schemas marked `> INFORMATIVE:` unless the spec explicitly elevates them.

## Rules that look mechanical but require judgment

1. **"Prefixes are globally unique."** There is no registry file. The template lists 10 reservations in §Requirement-numbering, but that list is in a narrative paragraph, not an extracted table that a linter can parse. An agent drafting spec #11 has to scan the current list and pick. This is human judgment, not mechanical.

2. **"Every cognition-tagged requirement names the delegation path."** Mechanical-sounding, but "names the delegation path (which role, which model-class, what input shape)" requires the author to *know* those three values. For HC-011, the delegation path is the workflow author choosing the skills in DOT attributes — is "workflow author" a role? A model-class? I wrote "workflow author (human), no model; input shape = DOT node attrs" but I was guessing at the required grammar.

3. **"Mark non-normative content with `> INFORMATIVE:` / `> RATIONALE:` / `> EXAMPLE:`."** The three are not mutually exclusive — a paragraph can be all three. Author must pick one. Judgment.

4. **"Total line count is under ~1000."** Mechanical to check, but the split-threshold is a SHOULD not a MUST, and the split structure (§Multi-file split) fragments the spec by content type. Deciding whether to split at 900 lines or 1100 is judgment, and the template offers no heuristic beyond the number.

5. **"`depends-on` list in front matter."** This is human-curated against the actual cross-references used in the body. A linter could verify consistency, but the author has to enumerate correctly in the first pass. Missing a dependency here doesn't fail any automated check named in the conformance checklist.

6. **"Non-normative callouts MUST NOT contain MUST/SHOULD/MAY keywords."** Easy for a linter to find lowercase/uppercase matches, but the author has to re-phrase a natural-sounding sentence to avoid the keyword. Mid-draft I wrote "> EXAMPLE: the orchestrator SHOULD await `agent_ready`" and had to rewrite to "the orchestrator waits for `agent_ready`." Judgment call on whether the rewrite preserves meaning.

## Missing guidance

1. **How to express a state machine inline vs in §7.1 vs as a tabular schema in §6.3.** Workspace has a 9-state lifecycle; handler-contract has a small 4-state one. Template gives three forms (pseudocode, table, ASCII) but no selection rule.

2. **Event payload schemas that are co-owned with another spec.** (Related to Stuck #3.) Need a pattern for "this spec defines the emission rule; the schema is registered in event-model.md."

3. **How to cite a type defined in another spec.** Cross-ref convention covers specs and sections, but `Outcome` (a type) — do I write `[execution-model.md §6.1 Outcome]`? `Outcome (defined in [execution-model.md §6.1])`? No canonical form.

4. **Go-specific content.** The template is language-neutral but handler-contract §4.1 and §4.4 are Go-specific (`context.Context`, `errors.As`, sentinel errors, goroutines). Should Go types be §6.1 RECORDs with Go-ish syntax, or is there a designated "Go interface declaration" shape? A single worked example would be enough.

5. **Cross-cutting `Tags:` for a requirement that spans subsystems.** HC-INV-001 is about a cross-subsystem invariant; is its mechanism/cognition tag the handler's viewpoint or the system's? Not guided.

6. **Skeleton vs fully-filled axis values.** §4.N+1 requires all four axes populated. But some requirements (e.g., "Handler MUST implement the Handler interface") have no meaningful axes. The template should show one or two canonical examples of `n/a`-heavy axis tags for declaration-only requirements.

7. **How to write "contract with another subsystem" content.** Components §4.11 notes handler-contract *consumes* the skill-declaration surface from control-points.md, with an explicit "co-dependency resolved directionally" note. The template has no shape for expressing this: depends-on treats it symmetrically, but the content is one-directional and the co-dep resolution is meta. I needed something like a §9.3 "co-references" subsection, or at least guidance on how to express "I read X from spec Y but don't normatively depend on Y's internals."

8. **No worked example of an Open Question that blocks nothing but matters later.** OQ-HC-002 (silent-hang threshold T) has `Blocks: HC-021` — fine. But the deferred Post-MVH item for binary signing (components §4.10) blocks nothing yet and isn't a decision waiting to be made; it's a commitment to a later phase. Is that an Open Question, a §10.3 excluded conformance claim, or both? I wrote it in both places, probably redundantly.

## Recommendations

1. **Add an `INTERFACE` shape to §6.1** next to `RECORD`, with a one-line-per-method contract form. Example:

   ```
   INTERFACE Handler:
       Launch(ctx, spec) -> (Session, error)  -- starts an agent process; idempotent on (run_id, node_id)
       ...
   ```

2. **Add a "single-tag rule" clarification to §4.N+1:** "If a requirement describes both a mechanism surface and a cognition surface, split it into two requirements."

3. **Add an "declaration-only requirement" exemption to §4.N+1:** declaration-only requirements use `axes: llm-freedom=n/a; io-determinism=n/a; replay-safety=n/a; idempotency=n/a`. Alternatively, make all four axes optional for invariants and declaration-only requirements and give one worked example.

4. **Extract the prefix-reservation list to a machine-readable block** (YAML or table at the bottom of the template). New specs append a row; linter reads the block. Removes the "global uniqueness is judgment" problem.

5. **Name the canonical glossary file** — or state that no central glossary exists and terms belong to the first spec that introduces them, cross-referenced from all downstream specs.

6. **Add a short "requirements vs invariants" selection test** to §5: "An invariant is a system-wide property that constrains multiple subsystems' requirements. If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant."

7. **Add a §9.3 "Co-references"** subsection for one-way consumption relationships (spec A reads from spec B's declared surface but does not depend on B's internals). List each co-reference with the direction and the consumed surface. Addresses Missing Guidance #7.

## Affirmations

1. **The requirement-block shape (ID + title + body + Tags + Axes) is crisp.** Once I understood it, every requirement took under a minute to draft. The uniform shape is the single biggest usability win.

2. **The "in scope / out of scope WITH reason" rule** caught me trying to include twin-drift conformance; the rule redirected me to S07 cleanly. Load-bearing.

3. **Cross-reference convention is narrow and complete.** Exactly four forms, forbidden forms listed. No ambiguity for inter-spec or intra-spec refs.

4. **The conformance checklist at the end is a real checklist**, not aspirational — I used it as a closing pass against my stub and it caught two missing axis tags and one stray `TODO`.
