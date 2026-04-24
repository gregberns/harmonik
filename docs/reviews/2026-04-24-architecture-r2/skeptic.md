# Skeptic Review — architecture.md (Round 2)

- Target: `/Users/gb/github/harmonik/specs/architecture.md` v0.2.0 (604 lines, 53 requirements: AR-001 — AR-053 with AR-008 retired; 2 invariants: AR-INV-001, AR-INV-003)
- Round-1 reviews: `/Users/gb/github/harmonik/docs/reviews/2026-04-24-architecture-r1/{critic,cross-spec-architect,implementer}.md`
- Reviewer lens: adversarial. Round-1 accepted a lot of "we'll fix the corpus later" promises. Round-2's job is to check whether the promises were kept, and whether the newly-introduced machinery (AR-052, AR-053, the `handler_type` migration clause, the 4→2 invariant pruning) is load-bearing or ceremony.
- Date: 2026-04-24

## Verdict

**Conditional — tighter than r1, but three of the key r1 fixes were performed only on paper.** The author did the easy work (pruning duplicate invariants, retiring AR-008, re-tagging AR-007, adding an envelope exemplar in §A.1) cleanly and defensibly. The author did **not** do the corpus work r1 demanded: every downstream spec still cites `§1.1 … §1.9` / `§1.4a` / `§1.6a` anchors that do not resolve in architecture.md, and `handler_type` is still present byte-for-byte in event-model, handler-contract, and workspace-model. The v0.2 spec acknowledges both defects inside its own normative text (AR-027 migration clause; §12 revision-history NOTE about the `§1.x → §4.x` map) and defers fixing them to "downstream specs' own integration cycles." That is an honest deferral — but it ships a foundation spec whose own conformance surface (AR-022 version pinning, AR-027 byte-identity, AR-053 envelope-section locus) is **self-violated at the corpus level on the day it lands**. Round-1 was not wrong about severity; r2 chose to accept the violation rather than resolve it.

Separately: AR-053's "§4.0 envelope section slot" has a quiet shape defect (the section number "§4.0" is structurally odd given architecture.md uses §4.0 itself for a different purpose), and the 4→2 invariant pruning was defensible but discards two constraints (AR-INV-004, AR-INV-006) whose content is now only locatable via §4 MUST clauses — which is strictly weaker than invariant status for cross-corpus lint.

Three to five challenges below. Integration-fix audit first.

## Integration-fix audit (what round-1 asked for vs. what v0.2 delivered)

Round-1 (critic + cross-spec-architect + implementer) produced a ranked list of ten fixes. Tracking:

| r1 ask | v0.2 delivered? | Evidence |
|---|---|---|
| 1. Split `runtime-subsystem` vs `foundation-cross-cutting` | **Yes, cleanly.** | AR-052 introduces `spec-category`; AR-013 now scoped to `runtime-subsystem` via cross-reference to AR-052. Glossary and §10.2 updated. |
| 2. Require AR-NNN citations in downstream `depends-on` | **No.** | No requirement added; §10.2 still says "cite the foundation version" (AR-022) without mandating AR-NNN granularity. |
| 3. Purge `§1.1 … §1.9` from §1 Purpose | **Partial.** | The self-citation in §1 was rewritten to `§4.1, §4.2, §4.4, §4.9, §4.10`. But the downstream corpus was NOT swept; every other spec still cites `§1.N`. The §12 revision-history NOTE codifies this as a "downstream specs fix these in their own integration cycles" deferral. |
| 4. Delete AR-008 | **Yes.** | Retired; anti-pattern list moved to an EXAMPLE under AR-007. ID reserved. |
| 5. Re-tag AR-007 as mechanism | **Yes.** | AR-007 is now `Tags: mechanism` with an INFORMATIVE clarifying that spec-authoring obligation ≠ runtime cognition. Clean. |
| 6. Clean up §5 invariants | **Yes, possibly over-aggressively.** | Retired AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006. Kept AR-INV-001 and AR-INV-003. See challenge S2. |
| 7. Merge AR-026 and AR-035 | **Partial.** | AR-035 now reads "Role is orthogonal to agent type. See §4.7.AR-026 for the normative rule; this subsection cross-references it for role-taxonomy locality." The merge is honest but AR-035 still occupies a numbered requirement slot, which invites the same confusion it was meant to resolve. |
| 8. Weaken AR-042 or make it concrete | **Weakened.** | Now "Every invariant MUST name its sensor inline or in §10.2." Keeps the MUST; the sensor surface is one of lint / verification-node / review-agent / conformance test. "Reviewer judgment" or "TBD" is explicitly disqualified. Defensible. |
| 9. Downgrade AR-023 or make declaration concrete | **Made concrete.** | AR-023 now requires a `touches:` front-matter field with shape `[architecture.md §4.1]` / `[architecture.md AR-016]`. Overlap is "non-empty lexical intersection." Tooling locus remains OQ-AR-003. Reasonable. |
| 10. Add trace-shape uniformity open question | **Not addressed.** | The AlphaGo 11-field trace shape is still universal per AR-012. No OQ tracks the deterministic-dispatch case where 5–6 fields are vacuous. This was a real r1 concern; it's gone silent rather than resolved. |

**Cross-spec-architect's top asks:**

| r1 ask | v0.2 delivered? |
|---|---|
| Resolve citation-anchor drift corpus-wide | **No** — architecture renumbered its own Purpose; corpus deferred. |
| Add explicit `§1.4a` / `§1.6a` sub-anchors | **No** — the §12 NOTE remaps them, but there are no sub-sections at those numbers. Under a lint, `[architecture.md §1.4a]` still resolves to nothing. |
| Resolve `handler_type` vs `agent_type` | **Partial.** AR-027 now includes a normative migration clause ("any downstream occurrence of `handler_type` for the same concept is a legacy artifact and MUST be migrated to `agent_type` in the next revision cycle"). The INFORMATIVE callout enumerates the five migration sites. The rename itself is not performed in this revision; the violation of AR-027's byte-identity rule is enshrined as "deferred work." |
| Relax AR-029 `verification-node` to role-function | **Yes.** AR-011 and AR-029 now state verification is a role-function of `{agentic, non-agentic}` nodes, not a node-type enum member. Canonical enum is pinned as `{agentic, non-agentic, gate, control-point, sub-workflow}`. Clean. |
| Envelope section locus | **Yes.** AR-053 pins `§4.0 Subsystem envelope` as required. See challenge S1 below. |
| AR-030 RETRY exclusion | **Yes.** Explicit now. |

**Implementer's blockers (citation fix, envelope slot + exemplar):**
- Citation fix: **still blocking, shipped unfixed.**
- Envelope slot + exemplar: **exemplar added in §A.1, slot pinned via AR-053.** Clean.

Net: r1 was heard. About 60% of the fixes were performed; 30% were performed by writing normative clauses that codify the unfixed state (migration-in-next-cycle, §12 remap NOTE, downstream-integration-cycles deferral); 10% went silent. The deferred work is at least now declared, which is strictly better than r1's state — but a lint run today would flag the foundation corpus as non-conformant to its own rules.

## Challenges

### S1 — AR-053's "§4.0 Subsystem envelope" slot is structurally incoherent with this spec's own use of §4.0 and with existing subsystem specs' stable IDs

AR-053 says: "Every `runtime-subsystem` spec MUST carry `§4.0 Subsystem envelope` as the FIRST subsection under §4." Architecture.md itself has a §4.0 — which is reserved for the `spec-category` rules (AR-052, AR-053 themselves). Architecture.md declares itself a `foundation-cross-cutting` spec (exempt from AR-053 per AR-052), so no direct conflict — but notice the abstraction: architecture uses §4.0 to declare rules ABOUT envelopes, while demanding downstream specs use §4.0 to declare the envelopes themselves. Two different things, same section number, which invites a downstream author to mimic architecture's §4.0 and get the wrong content in the right place.

Beyond the naming collision: §4 is "Normative requirements," whose subsections are §4.1 through §4.10 topically. The template (`docs/foundation/spec-template.md`) treats §4 as the requirements section with numbered subsections per topic.

Three questions a downstream author will ask immediately:

1. "Is §4.0 a subsection of §4 or a sibling of §4?" AR-053 says "under §4." Markdown-wise, `#### 4.0` is rendered as a depth-4 heading; if §4 is `##`, then §4.0 as `###` is a subsection of §4, and §4.1 is also a `###`. The envelope section then sits lexically before §4.1, which is coherent. But architecture.md's own §4.0 is rendered as `### 4.0 Spec categories and envelope locus` — a `###`, same level as §4.1. That works.

2. "What numbering do I use for requirements inside §4.0?" AR-053 doesn't say. The envelope has eight elements (a)–(h); each presumably carries requirement IDs. If the subsystem is execution-model, does it use `EM-001` for envelope-declaration, then `EM-002+` for the topical requirements? Or does the envelope get its own `EM-ENV-NNN` range? The A.1 exemplar writes `<prefix>-NNN — Envelope declaration` as a single requirement. So the eight elements collapse into **one** requirement block? That seems to contradict AR-005 (one tag per requirement; "A requirement describing both surfaces MUST split into two requirements") — because the envelope block mixes `mechanism`-tagged types with mechanism-tagged events and potentially cognition-tagged boundary operations.

3. "When I add an envelope to execution-model.md, my existing §4.1 `EM-001 Workflow declaration` requirement moves to §4.1 but gets renumbered because §4.0 now owns `EM-001`?" AR-053's front-of-section rule forces renumbering across every extant subsystem spec that already has stable requirement IDs. The A.1 exemplar uses `<prefix>-NNN` as if NNN were a free choice, but in the corpus these IDs are stable identifiers cited by other specs. Moving `EM-001` to the envelope and shifting the rest is a corpus-wide renumber.

**Alternative shape.** Either (a) name the envelope section something other than §4.0 — e.g., §2.3 Envelope declaration, as a scope-adjacent subsection that doesn't share numbering with §4's topical requirements — or (b) explicitly permit the envelope requirement to use a reserved prefix (`<PREFIX>-ENV-NNN`) so it doesn't consume topical ID space, and make the template accommodate it. The current AR-053 creates a renumbering obligation the author hasn't costed: execution-model already has EM-001 through EM-063 as stable IDs cited from four other specs, and forcing a §4.0 insertion at ID=001 breaks those back-references.

Evidence the cost is real: grep for `EM-001` in the corpus returns citations from handler-contract and control-points. If `EM-001` shifts from "Workflow declaration" to "Envelope declaration" under AR-053, every one of those citations silently retargets to a semantically-unrelated requirement. The v0.2 spec does not acknowledge this cost anywhere; AR-053 was added without a corpus-wide renumbering proposal.

### S2 — The 4→2 invariant prune discards cross-corpus constraint for requirement-locality

v0.2 retired AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 as "duplicates of §4." The retirements are defensible under the template §5 selection test (don't write the same rule twice), but the test's deletion direction (keep the §4, delete the invariant) is not always right, and the author applied it mechanically in all four cases.

Two cases deserve to be kept as invariants, not requirements:

- **AR-INV-004 (subsystem envelope as only declaration surface).** As a §4 requirement (AR-013), this constrains what a subsystem spec MUST do. As an invariant, it constrained what the whole corpus MUST NOT do (introduce a second declaration surface). The invariant form has a universal-over-corpus quantifier that the requirement form lacks. A lint against AR-013 asks: "Does this spec declare an envelope?" A lint against AR-INV-004 would ask: "Does this spec declare ANYTHING that looks like an envelope under a different name (e.g., 'capabilities', 'interface', 'boundary')?" The second is the cross-corpus check that catches drift; the first is not. Retiring AR-INV-004 trades a cross-corpus constraint for a per-spec one.

- **AR-INV-006 (three-artifact vocabulary exhaustive).** Same argument. AR-046 says "the three artifacts are spec, workflow graph, bead." AR-INV-006 said "no fourth compositional artifact may be introduced." These are not the same: AR-046 defines the set, AR-INV-006 forbids extension. A subsystem spec could satisfy AR-046 by correctly using the three, while simultaneously introducing a fourth ("feature plan," "capability record") — and AR-046 would not catch it. AR-INV-006 would. AR-051 covers the specific "feature" case but not the general "fourth artifact" case.

**Fix.** Re-promote AR-INV-004 and AR-INV-006 with explicit cross-corpus quantifiers. The other two retirements (AR-INV-002, AR-INV-005) are genuine duplicates and should stay retired. Suggested shapes:

- AR-INV-004 (revived): "No spec in the foundation or subsystem corpus MAY introduce a declaration surface equivalent to the subsystem envelope under a different name. 'Equivalent' means: declares some subset of the eight elements of AR-013 as a consolidated block. Detection: reviewer scans for section titles matching `{capabilities, interface surface, boundary, module declaration, integration contract}` applied to a subsystem; such section titles MUST either conform to AR-053 or be renamed."
- AR-INV-006 (revived): "No spec MAY introduce a compositional artifact beyond the three of AR-046. 'Compositional' means: a durable, authored artifact that carries work-shape. Detection: corpus lint for normative use of terms `{feature, story, epic, capability-plan, initiative}`."

### S3 — AR-027's migration clause is a normative self-violation, not a migration plan

AR-027 now reads (emphasis mine): "The canonical field name across all four surfaces is `agent_type`; **any downstream occurrence of `handler_type` for the same concept is a legacy artifact and MUST be migrated to `agent_type` in the next revision cycle of the owning spec.**" The INFORMATIVE callout below enumerates the five migration sites (event-model §8.3.2, event-model §8.3.8, handler-contract HC-008, workspace-model §5.3a, workspace-model `harmonik.meta.json` sidecar).

This is an honest improvement over r1's silent drift — the obligation is named. But consider what the rule actually says on the day architecture v0.2.0 lands:

- AR-027 asserts byte-for-byte identity across four surfaces.
- The five enumerated sites currently violate byte-for-byte identity.
- The violation is labeled "legacy" and deferred "to the next revision cycle."
- "Next revision cycle" is undefined in the foundation corpus; no spec has a revision schedule.
- §10.2 describes the lint as "corpus-level lint: every occurrence of `agent_type` matches the regex of §6.1." The lint does not check byte-identity across the four surfaces; it checks that each `agent_type` occurrence is a valid identifier. A `handler_type` occurrence is simply not scanned.

So: AR-027's byte-identity rule is normatively violated today, the violation is acknowledged, the enforcement mechanism is "wait for the next owning-spec revision," and there is no lint that would detect continued drift. This is strictly better than r1 (which did not acknowledge the drift at all) but it is also **worse than a cleanly-shipped rename would have been**. Architecture v0.2.0 is a foundation spec whose own normative clause is self-violated.

**Stronger alternatives:**
1. Ship the rename in the same corpus revision. The five migration sites are mechanical edits, ≤1 hour of work, no semantic changes. Round-1 did not do this because v0.2 was scoped to architecture-only; nothing forces that scope.
2. If the rename must defer, add a concrete trigger: "this revision cycle MUST complete before foundation version 0.2.1." That gives the obligation teeth. Currently it can slip indefinitely.
3. Add the byte-identity check to §10.2's lint list explicitly: "Every occurrence of the four reference points (YAML policies, DOT attributes, `LaunchSpec.agent_type`, event payloads) MUST use the `agent_type` identifier; occurrences of `handler_type` in these surfaces are a lint failure." Without this, AR-027's conformance test is "agent_type matches regex," which a `handler_type` occurrence trivially passes (because `handler_type` is not a `agent_type` occurrence).

Deeper concern: the migration-clause pattern is a precedent. Once foundation v0.2.0 ships with one self-violating normative clause, the bar drops. v0.3 can ship with two; v0.4 can ship with three. Each is honestly declared, each is deferred "to the next revision cycle," and the foundation's self-conformance rate drifts from 100% to some asymptote below 100%. The discipline that keeps a foundation spec credible is that its normative clauses are true on the day it lands. AR-027 broke that discipline for an arguable convenience (not shipping a cross-spec rename in the same PR). The precedent is worth questioning explicitly.

### S4 — AR-052/AR-053 are load-bearing but create a new failure mode: category-mismatch silence

AR-052 is the right fix for r1-C1. Splitting `runtime-subsystem` from `foundation-cross-cutting` resolves the aspirational-universality of AR-013 cleanly. But the new abstraction introduces a question r1 didn't anticipate: **what happens when a spec is mis-categorized?**

Three cases:

- **Under-declaration.** A runtime-subsystem spec is mis-categorized as `foundation-cross-cutting`, escaping AR-053's envelope obligation. AR-052 says "category assignment is reviewer-enforced; front-matter presence is lint-enforced." So the lint checks `spec-category: <one of two values>` but not whether the value is correct. A subsystem author who wants to skip envelope work writes `spec-category: foundation-cross-cutting` and the lint passes.

- **Over-declaration.** A foundation-cross-cutting spec (e.g., operator-nfr) is mis-categorized as `runtime-subsystem`, forcing a §4.0 envelope declaration where none meaningfully exists. The eight envelope elements would be populated with "none" across the board — which AR-053 permits — making the declaration vacuous.

- **Corpus drift.** A spec starts as foundation-cross-cutting (e.g., reconciliation at some stage of its life) and later becomes runtime-subsystem (or vice versa). Nothing in AR-052 triggers a re-categorization review on boundary change.

The reviewer-enforced check is the right mechanism, but the spec doesn't say what the reviewer is checking for. AR-052 lists examples ("operator-nfr, beads-integration, reconciliation, testing, architecture itself") but doesn't give the test. r1-C1's proposal had a concrete test: "a subsystem is a Go package that implements an event producer or consumer registered with the in-process event bus." That test is mechanical and lint-checkable (at build time, grep for bus registration). AR-052 substituted reviewer judgment for that test.

**Stronger alternative.** Add a concrete test in AR-052: "A spec is `runtime-subsystem` iff it declares a Go package that registers with the event bus per §4.4.AR-013(a)(b) OR owns state per §4.4.AR-013(e). A spec is `foundation-cross-cutting` otherwise." This is mechanically checkable against the corpus and removes the review-judgment escape hatch.

Secondary concern: AR-052's enumeration of examples ("operator-nfr, beads-integration, reconciliation, testing, architecture itself") includes beads-integration — but beads-integration declares a subprocess (`br` CLI), owns a state store (SQLite), and produces/consumes events. Under the concrete test above, it would classify as `runtime-subsystem`. Under AR-052's reviewer-judgment, it's listed as `foundation-cross-cutting`. The example set is already drifting from a principled rule; this is evidence that "reviewer-enforced category" is an operational cost, not a simplification.

### S5 — Two invariants retired; two invariants remain; the foundation's universal-quantifier surface is now thin

Pre-r1, architecture.md had 6 invariants. v0.2 has 2 (AR-INV-001: mechanism/cognition split at process boundary; AR-INV-003: search+verifier+traces required). The informative note at the top of §5 correctly cites the template selection test for retirement.

But consider what invariants are *for*: they name system-wide properties that a reviewer applies to the whole corpus, independent of any single spec's §4. Two invariants is a thin surface for a 10-spec corpus. Checking §4 closely:

- AR-037 (centralized-controller) is clearly system-wide ("agents MUST perform only cognitive work; agent-to-agent coordination MUST route through the daemon") — this is a property of the whole corpus, not a local requirement of architecture.md. r1-critic flagged this for invariant promotion; v0.2 did not promote it. It remains a §4.9 requirement, which means a subsystem review can satisfy "the spec does not cite AR-037" without the reviewer being forced to check whether the subsystem quietly violates centralized-controller.

- AR-046 (three artifacts, none projected) is similarly universal — it forbids ANY spec from treating one of the three as derived from another.

- AR-017 (enumerated out-of-process actors) is universal across the corpus.

The r1 observation that "AR-037 should be promoted, AR-INV-005 deleted" was correct on both halves; v0.2 deleted AR-INV-005 but did not promote AR-037. Result: the centralized-controller principle has no invariant form and lives as a §4 requirement cross-referenced by other specs. When the "INV" surface is what distinguishes cross-corpus rules from per-spec rules, this matters.

**Alternative.** Promote AR-037 and AR-046 to invariants (AR-INV-007 and AR-INV-008 to avoid collision with retired IDs, since AR-INV-002/004/005/006 are reserved). The two retained invariants plus these two promotions gives four system-wide constraints — close to the minimum a 10-spec corpus needs for cross-cutting lint. Leaving these as §4 requirements is not *wrong*, but the discipline in v0.2 — "delete duplicate invariants" — was applied without the companion discipline — "promote cross-corpus requirements." The asymmetry suggests the prune was template-compliance-driven rather than substance-driven.

A related observation: the §5 informative preamble says "an invariant is retained only when it constrains multiple subsystems' requirements and does not duplicate a §4 requirement." That test admits §4 requirements that constrain multiple subsystems — exactly the case for AR-037 and AR-046. The test as written under-prescribes: if a §4 requirement and an invariant would say the same thing, the invariant form is strictly more lint-visible (it appears in §5, which conformance tests scan explicitly). The decision to delete the invariant and keep the §4 form is a choice the template doesn't mandate; other defensible reading would keep both and rely on cross-reference.

## Hidden assumptions

1. **That "next revision cycle" is a meaningful trigger.** AR-027's migration clause, the §12 NOTE's "downstream specs fix these in their own integration cycles," and the §9.3 bootstrap-only migration all appeal to revision cadence. No spec declares one. The assumption is that kerf's review flow produces revisions on a regular beat; the corpus has no such beat.

2. **That AR-053's "§4.0 first under §4" survives template revision.** The spec-template is v1.1. If v1.2 revises §4's structure (e.g., introduces a mandatory §4.1 "Invariants" slot), AR-053's pin is silently wrong. The AR-053 rule is brittle to template evolution and doesn't say so.

3. **That foundation-cross-cutting specs don't need the eight envelope elements.** operator-nfr has NFR obligations that resemble envelope element (g) "NFRs inherited/overridden." reconciliation has state and events even though it's declared not-a-subsystem. Exempting these specs from envelope declaration saves authoring cost but loses a consistent cross-corpus declaration surface. The assumption is that cross-cutting specs are sui generis enough to not benefit from a standard declaration — plausible, but untested.

4. **That an INFORMATIVE callout is normative enough to enumerate migration sites.** The v0.2 AR-027 callout lists five sites "as of architecture v0.2.0." If the list drifts (new `handler_type` appears elsewhere, one of the listed sites is renamed before migration), the callout is stale. The spec has no lint that regenerates the list; it's reviewer memory.

5. **That deferring corpus-wide citation fixes to "downstream specs' own integration cycles" converges.** Ten specs, each with independent revision schedules, each carrying `§1.N` citations. The §12 NOTE assumes they'll all migrate before anyone actually tries to resolve a citation. No coordination mechanism exists; no single agent or author is accountable for "all `§1.N` citations resolved" as a milestone.

6. **That AR-022 (subsystem specs cite foundation version) works without AR-NNN granularity.** r1-cross-spec-architect noted that no downstream spec pins a `foundation-version:` — and v0.2 did not add a requirement mandating it. AR-022 says "cite the foundation version it conforms to in its front matter or in §9.1 Depends-on." Corpus check: zero specs do this. The rule is normatively violated on day one.

7. **That the `§1 Purpose` rewrite catches all self-citation drift.** §1 Purpose now uses `§4.1, §4.2, §4.4, §4.9, §4.10`, matching the actual spec. But the §22 line "Citations to sub-requirements MUST use the `AR-NNN` form; section numbers alone are insufficient to target the re-review obligation in §4.6.AR-020" is a new normative clause — and it's violated everywhere in the corpus, including by architecture.md's own §9.3 co-references (which cite `[docs/foundation/components.md §2]`, not `§2.EM-001` or similar). The author fixed the self-citation but introduced a new requirement that creates a new corpus-wide violation. AR-020's re-review targeting needs AR-NNN citations; the corpus has none; v0.2 codified the requirement without the corresponding migration.

8. **That kerf's review flow produces the "at least two foundation-review personas" AR-020 requires.** Round-1 flagged this as an availability assumption. v0.2 did not add an OQ tracking it. The kerf docs describe reviewer sub-agents, but whether "architect and critic at minimum" is a consistent pair or a menu varies by invocation. If kerf produces only one reviewer persona for a given invocation, AR-020 blocks the amendment until a second is spawned — the spec assumes seamless two-persona availability that operational flow may not deliver.

## Affirmations

- **AR-052/AR-053's split is the right abstraction** — it resolves r1-C1 (envelope aspirational-universality) cleanly and enables meaningful lint. The reservation about reviewer-enforced category assignment (S4) doesn't undermine the abstraction itself.

- **AR-029/AR-011's shift from node-type-enum to role-function** is exactly right and aligns with execution-model EM-006. r1 got this fix; v0.2 executed it crisply.

- **AR-042's tightening to "invariants MUST name their sensor, and 'reviewer judgment' / 'TBD' fail"** is a genuine constraint improvement. Before it was ceremony; now it does work.

- **AR-023's `touches:` front-matter concretization** removes the r1 objection that "mechanical overlap detector" was vaporware. Reviewer-enforced until tooling lands is honest.

- **The A.1 envelope exemplar** is load-bearing: it resolves the implementer review's "no extant spec has this shape, so authors can't copy-paste." Even if AR-053's section-number choice is shaky (S1), the exemplar makes the obligation concrete.

- **Retiring AR-008 into an EXAMPLE under AR-007** is clean deletion of a redundancy. The anti-pattern list survives with less normative weight, which is what it deserved.

- **Explicit RETRY exclusion in AR-030** closes a small but real ambiguity that r1-cross-spec-architect flagged. Cheap fix, correctly executed.

- **The §12 revision-history transparency** — explicitly listing every retired ID, every re-tagging, every merge, every deferral — is better craftsmanship than most specs manage. It's the reason this review can be specific about what was and wasn't fixed. The `§1.x → §4.x` remap table in the NOTE is the kind of artifact that future corpus-edit passes can automate against directly; it is operational debt declared cleanly rather than hidden.

- **AR-INV-001's sensor declaration** — "corpus-lint per §10.2 AR-005..AR-007 group plus reviewer-agent scenario per §10.2 AR-037..AR-045 group" — is a concrete answer to AR-042's "MUST name a sensor" rule. The two retained invariants both name sensors; the test-surface in §10.2 groups requirements by lint obligation. This is the pattern the whole foundation corpus should follow, and v0.2 set it correctly for the two surviving invariants.

- **Reserved-ID discipline is honored.** AR-008, AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 are all retired with "never reused" language in §10.1 and §5. This prevents the silent-renumbering failure mode where a new requirement could steal a retired ID and a reviewer reads a stale cross-reference to the wrong rule.
