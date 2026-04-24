# Critic Review — architecture.md (Round 1)

- Target: `/Users/gb/github/harmonik/specs/architecture.md` (564 lines, 51 requirements, 6 invariants)
- Template: v1.1
- Reviewer lens: challenge weakly-justified meta-rules; architecture is a force multiplier for over-engineering because every rule ripples into 9 downstream specs.
- Date: 2026-04-24

## Verdict

**Conditional.** The spec is structurally solid and the core positions (four-axis, ZFC, subsystem-envelope, three-artifact, centralized-controller) are load-bearing and well-argued. But roughly a third of the requirements are ceremony: they produce no observable implementation difference, overlap with one another, or claim universal applicability in ways the corpus already violates. Six of the nine downstream specs ignore the "MUST cite" and "MUST declare envelope" obligations outright — not because the specs are defective, but because the obligations are mis-shaped for specs that aren't runtime subsystems. Tighten these before review freeze or the foundation ships with unenforceable rules.

## Corpus evidence (cross-spec audit)

Two mechanical checks were run against the current `specs/` tree; both inform the challenges below.

| Spec | `architecture.md` citations | `AR-NNN` ID citations | Declares §4.4 envelope? |
|---|---|---|---|
| workspace-model | 0 | 0 | No |
| beads-integration | 1 | 0 | No (word "envelope" absent) |
| event-model | 3 | 0 | Partial |
| process-lifecycle | 6 | 0 | Partial |
| operator-nfr | 8 | 0 | No (has "observability envelope", different concept) |
| handler-contract | 9 | 0 | Yes-ish |
| execution-model | 14 | 0 | Partial |
| control-points | 27 | 0 | Yes |

Two takeaways: (a) citation density varies by 27x across downstream specs — control-points treats architecture as a living dependency, workspace-model treats it as invisible; (b) **no downstream spec cites any AR-NNN requirement by ID.** That second fact undermines AR-020 (amendment re-review), AR-022 (foundation versioning conformance citation), and OQ-AR-001 (test-obligation migration). The spec's enforcement surface is currently filename-level, not requirement-level.

## Challenges

### C1 — AR-013's envelope obligation is universal in prose, but the corpus already contains non-subsystem specs

AR-013 says "Every subsystem spec MUST declare its envelope" with eight required elements (events produced, events consumed, types, handlers implemented, state owned, control points, NFRs, boundary classification). The term "subsystem" is defined in §3 as "a unit declaring an envelope per §4.4 and realized at MVH as a Go package inside the daemon binary per §4.5."

Check the corpus:

- `operator-nfr.md` is a cross-cutting NFR spec. Its §2.1 names an "observability envelope" but that envelope is not the §4.4 envelope and it does not produce/consume events, export types, implement handlers, or own state. It imposes obligations on every runtime subsystem. The eight envelope elements are a category error here.
- `beads-integration.md` has **zero** occurrences of the word "envelope" and does not declare the eight elements.
- `reconciliation` is explicitly declared *not* a subsystem by AR-018 but is a foundation spec. Its "envelope" is undefined: AR-013 would reject it, AR-018 exempts it.

**Stronger alternative.** Split the abstraction. Introduce **runtime-subsystem** (must declare envelope, is a Go package, AR-013 applies) and **cross-cutting spec** (owns invariants, not runtime behavior; imposes obligations on runtime subsystems; no envelope). Mark operator-nfr and beads-integration as cross-cutting. Redraft AR-013 as "Every runtime-subsystem spec MUST declare its envelope." The current wording is aspirational; the corpus has already voted against it.

### C2 — "Downstream specs cite §1.1, §1.2, §1.4, §1.8, and §1.9" is false at the requirement-ID level

Purpose §1 states: "Downstream specs cite §1.1, §1.2, §1.4, §1.8, and §1.9 at minimum." Grep shows:

- `workspace-model.md`: **0** citations of `architecture.md`.
- `beads-integration.md`: **1** citation.
- `event-model.md`: 3.
- `process-lifecycle.md`: 6.
- `operator-nfr.md`: 8.
- `handler-contract.md`: 9.
- `execution-model.md`: 14.
- `control-points.md`: 27.

More damning: **zero** downstream specs cite any `AR-NNN` or `AR-INV-NNN` requirement ID anywhere. They cite section numbers (`[architecture.md §4.1]`), not requirement IDs. The amendment protocol (AR-020) promises "revision MUST trigger re-review of every subsystem spec that cited the affected foundation spec." Without requirement-ID-level citations, "cited the affected foundation spec" degrades to "mentioned architecture.md anywhere," which is over-broad and under-tracked.

Also, the §1 numbering itself (`§1.1 … §1.9`) does **not** correspond to this spec's own headings — the spec's §1 is "Purpose," a single-paragraph section. Those numbers trace back to an older outline (visible in OVERVIEW.md which uses "(architecture §1.1)" callouts). A reader who actually opens architecture.md to §1.1 finds prose, not a requirement.

**Stronger alternative.** Two fixes, one cheap, one not:

1. (Cheap) Purge the `§1.1 … §1.9` numbering from the Purpose statement. Replace with "Downstream specs conform to the four-axis test (§4.1), ZFC test (§4.2), required triple (§4.3), centralized-controller invariant (§4.9), and three-artifact separation (§4.10)." Those numbers match the actual spec.
2. (Not cheap but correct) Require downstream specs to cite AR-NNN IDs, not sections, when they claim conformance. Add a lint item: "Every spec's §9.1 Depends-on MUST cite the AR-IDs it is covered by, not just the foundation spec file." Without this, AR-022 (foundation versioning and conformance citation) is conformant-to-the-letter and hollow-in-practice.

### C3 — AR-042 "guides and sensors pair" is unobservable ceremony as stated

AR-042: "Every cross-cutting invariant MUST be covered by BOTH a declarative constraint (spec, policy, convention — a guide) AND a runtime check (test, verification node, review agent — a sensor). A guide without a sensor is unenforceable; a sensor without a guide is undocumented."

This reads rigorously. But what does it *do*? There is no enumerated list of "cross-cutting invariants" covered by AR-042. §5 names six `AR-INV-NNN` invariants. Does AR-042 apply only to those six, or to every invariant across all 10 foundation specs, or to any requirement marked MUST? Ambiguous. The test-surface in §10.2 says this is a "review-agent scenario test," which means a reviewer reads the spec and decides whether each invariant has a sensor. That's cognition-tagged in practice — but the requirement is mechanism-tagged.

More importantly: the spec itself violates AR-042. AR-INV-004 ("Subsystem envelope is the only declaration surface") has no named sensor. AR-INV-006 ("Three-artifact vocabulary is exhaustive") has a sensor in §10.2 ("no spec treats `feature` as a normative term") but that sensor fires only at spec-draft time, not at runtime, which AR-042 explicitly demands ("runtime check").

**Stronger alternative.** Either (a) weaken AR-042 to "every `AR-INV-NNN` invariant in this spec MUST name its sensor inline in the invariant block" — that is mechanically checkable and forces the work — or (b) delete AR-042 entirely and fold its intent into §10.2 conformance-test obligations. The current form is rigorous-sounding prose that does no work.

### C4 — AR-005 (single-tag rule) and AR-006/AR-008 overlap each other enough that one is redundant

AR-005: every requirement carries exactly one of `mechanism` or `cognition`.
AR-006: a mechanism-tagged point MUST NOT invoke an LLM; lists permitted and forbidden behaviors.
AR-008: the daemon process MUST NOT perform semantic judgment; names framework-layer anti-patterns.

AR-008's anti-pattern list (keyword-matching for completion, heuristic fallback trees, regex parsing of free-text, hardcoded quality scoring) is a **subset** of AR-006's anti-pattern list (same four items, same wording). AR-008 adds "the daemon process MUST NOT" while AR-006 speaks of "a mechanism-tagged evaluation point." Since AR-INV-001 already establishes "the daemon carries only mechanism-tagged logic," AR-008 is a logical consequence of AR-INV-001 + AR-006. It adds no new obligation.

**Stronger alternative.** Delete AR-008. Its anti-pattern list is useful as an example, not as a requirement. Move the four anti-patterns into a `> EXAMPLE:` callout under AR-006. The spec is already 564 lines; redundant requirements are a cost downstream authors pay on every review pass.

### C5 — AR-023's "mechanical overlap detector" is prescribed without the tool existing, and the tool's locus is an open question

AR-023 requires that parallel amendments "MUST declare the foundation-spec sections it touches" and "a mechanical overlap detector MUST scan declared sections across open proposals." OQ-AR-003 is titled "Amendment overlap detector tooling locus" and admits the detector doesn't exist yet — default "spec-corpus lint script under `tools/`."

Two problems:

1. A MUST requirement whose enforcing tool is an open question is deferring the real work. What happens to a parallel amendment during MVH drafting? The requirement is active (AR-023 is not marked deferred in §10.1 Core MVH — it lists "AR-001 through AR-051" as all required), but the mechanism to satisfy it doesn't exist.
2. "Declared sections" is not defined. A section is a numbered heading in this spec; but an amendment can touch multiple subsections of §4.7 without "declaring" them in a manifest. The declaration surface is implicit.

**Stronger alternative.** Either (a) downgrade AR-023 to SHOULD until the tool lands, and track the upgrade in OQ-AR-003, or (b) make the declaration concrete: "The amendment proposal MUST include a front-matter field `touches: [§4.1, §4.5.AR-016, ...]` enumerating affected sections and requirement IDs; overlap detection is lexical intersection of these lists." The second is mechanically trivial to build (30 lines of Python) and removes the open question. The first defers honestly. The current middle ground is a MUST with no teeth.

### C6 — AR-007 delegation-path obligation is cognition-tagged, but the check itself is enforced at spec-draft time

AR-007 demands cognition-tagged points "name the role, model class, and input shape." The `Tags:` line on AR-007 is `cognition` (with an `Axes:` line). That's odd: the requirement is about what a **spec author** must write, not about a runtime LLM call. The check fires during review — a reviewer persona reads the spec and decides whether the delegation path is named — which is cognition about the spec, not cognition performed by the system under spec.

This blurs the mechanism/cognition line at exactly the place the spec is trying to pin it. A spec-level obligation to name delegation paths is a **mechanism-tagged** meta-rule. The cognition happens when a human or reviewer-agent reads the spec and judges "is this enough detail?" — but that reviewer is applying the meta-rule, not violating ZFC.

**Stronger alternative.** Re-tag AR-007 as `mechanism` (it's a spec-shape obligation, not a runtime evaluation point). Keep AR-021 as `cognition`-tagged since it describes an actual reviewer-persona judgment the amendment flow depends on. The distinction: AR-007 is "the author MUST write X"; AR-021 is "the reviewer MUST judge Y." Only the latter is cognition.

## MUST/SHOULD discipline issues

- **AR-003 ("Expected profile for skeleton vs. organ") uses MUST for an expected profile.** "Skeleton operations MUST profile as `llm-freedom=none`." But "expected" in the title is in tension with MUST in the body. If a skeleton operation deviates, is that a spec violation or a signal that the operation was misclassified? Prose says the latter ("such logic MUST be relocated"), body says the former. Prefer: "An operation inside the daemon process MUST profile as `llm-freedom=none`. An operation profiling as `bounded` or `unbounded` inside the daemon is a design error and MUST be relocated." Drop "expected" from the title.
- **AR-015 ("Add-a-subsystem procedure") is aspirational.** "Foundation revision MUST NOT be required to add a subsystem." This is a goal, not a check. It only proves false in retrospect when a new subsystem turns out to need foundation revision. No test can fail this at spec-draft time. Either mark it SHOULD or reframe as an invariant the amendment-protocol maintains.
- **AR-026, AR-035 (orthogonality of agent-type and role) are the same invariant stated twice.** One is under §4.7, one under §4.8. Both say "role MUST NOT be conflated with agent type." Pick one; make the other a cross-reference. Duplication invites drift at edit time.
- **AR-017 ("Enumerated out-of-process actors") uses MUST implicitly** by naming "the only out-of-process actors in MVH are (a), (b), (c)." The enforcement discipline is "no fourth class without amendment" — but the requirement doesn't say that explicitly. Add: "Introducing a fourth out-of-process actor class MUST proceed via the amendment protocol of §4.6."
- **AR-041 ("Repository as single source of truth") uses MUST where SHOULD would be more honest.** "External wikis, out-of-band knowledge bases, and tribal-knowledge channels MUST NOT be load-bearing for any spec's conformance." This is only enforceable via reviewer judgment ("is this wiki page load-bearing?"). The wording is stronger than the enforcement mechanism supports. Either move to a SHOULD + reviewer-guidance, or tie it to a concrete test (e.g., "every `see` cross-reference from a spec MUST resolve within the `specs/` tree or `docs/` tree").
- **AR-044 ("Filesystem-backed coordination") conflates two rules.** (a) coordination artifacts must be on disk, and (b) agent conversation transcripts are not state the daemon consumes. These are distinct obligations with distinct enforcement surfaces; splitting them per the template's single-tag rule (AR-005) would clarify which rule the daemon implementers must satisfy versus which rule workflow authors must satisfy. Currently both constrain the daemon, but only the first constrains workflow authors.
- **AR-050 ("Many-to-many without projection") is a shape claim, not a behavior claim.** It asserts a relationship topology but prescribes no check. What test fails this? The template §5 selection test would suggest: if a rule constrains multiple subsystems' requirements about relationships, it's an invariant; promote to §5.

## Invariants challenged

Per template §5 selection test ("an invariant is a system-wide property that constrains multiple subsystems' requirements"):

- **AR-INV-003 (search + verifier + traces required)** is load-bearing and correctly invariantized. Keep.
- **AR-INV-004 (subsystem envelope is the only declaration surface)** is a requirement about subsystems, not a system-wide property. It restates AR-013 in invariant form. The template's rule: "If you write the same rule as both, delete the §4 copy." Keep the invariant; delete AR-013's obligation text and have AR-013 only declare the eight envelope elements as a schema.
- **AR-INV-006 (three-artifact vocabulary is exhaustive)** duplicates AR-046 word-for-word. Same deletion rule applies. Keep the invariant, reduce AR-046 to a definition-list entry.
- **AR-INV-002 (every cross-subsystem surface is fully tagged)** restates AR-001 + AR-005. Same issue.

Pattern: four of six invariants are restatements of §4 requirements. The template explicitly warns against this. Either the §4 requirement is the wrong level (promote to invariant, delete from §4) or the invariant is decorative (delete the invariant). Walking the list and applying the selection test would shrink §5 by half.

Conversely, one requirement **should** be an invariant and isn't: AR-037 (centralized-controller). It spans every agent, every coordination path, every subsystem. It constrains multiple subsystems' requirements. It belongs in §5 as `AR-INV-007`. Currently it's a requirement under §4.9, and AR-INV-005 is a restatement of it. Promote AR-037 and delete AR-INV-005.

A second candidate for promotion: AR-008 (framework-level semantic judgment is banned) constrains every daemon-side requirement in every spec. If not deleted per C4, it should be promoted to invariant, not retained as a §4.2 requirement.

A third observation: AR-009 ("all three mechanisms MUST exist in every deployment") is already an invariant in §5 (AR-INV-003). The §4 copy restates it and should be deleted per the template selection test. The current spec keeps both.

Summary of §5 cleanup: delete AR-INV-002 (duplicate of AR-001/AR-005), AR-INV-004 (duplicate of AR-013), AR-INV-005 (duplicate of AR-037), AR-INV-006 (duplicate of AR-046). Promote AR-037 as new AR-INV-003 (renumber), and leave AR-INV-001 (mechanism/cognition split) and AR-INV-003 (required triple) as the two genuine system-wide invariants. Result: six invariants become two, with no loss of normative content. Every deleted invariant's content already appears in §4.

## Affirmations

- The three-artifact separation (§4.10) is clean, well-motivated in A.3, and the "feature is not a primitive" exclusion (AR-051) prevents a known failure mode in similar systems. The rationale appendix is load-bearing here.
- The Go-package-as-subsystem MVH pin (AR-016) with explicit post-MVH preservation (AR-019) is the right shape: it forces envelope discipline to prove itself in-process before allowing process fragmentation. The A.3 rationale names the alternative (open realization) and the concrete failure mode (envelope discipline fragmenting across process boundaries before proving itself).
- The verification-term disambiguation (AR-029 / AR-030 / AR-031) is genuinely useful: the three hyphenated forms are easy to keep straight, and the rationale in A.3 names the failure mode clearly. This is the kind of terminological ceremony that pays for itself.
- The centralized-controller tradeoff acknowledgment (AR-040) is exactly the kind of explicit cost-naming that prevents surprise amendments later. Explicitly stating "we forego graceful degradation" and naming the trigger scenarios for re-evaluation (remote-daemon, multi-operator) is rare and valuable.
- The ZFC framing (mechanism/cognition) does real work — it forces the author to name the delegation path for every cognition-tagged point (AR-007 intent, if not its tag). This is high-value ceremony even if the specific AR-007 tagging needs the fix in C6.
- The enumeration of out-of-process actors (AR-017) is concretely closed: only three classes (handlers, orchestrator-agents, `br` subprocesses) are permitted, and any fourth must go through amendment. This is a clean negative space that prevents subsystem-author scope creep.
- The explicit anti-subsystem declarations (AR-011: no verifier subsystem; AR-018: no reconciliation subsystem) are doing exactly the work named: preventing drift toward vocabularies the locked decisions already rejected.

## Definitions that are circular or too narrow

- **"subsystem" (§3)** — defined as "a unit declaring an envelope per §4.4 and realized at MVH as a Go package inside the daemon binary per §4.5." This is circular with AR-013 ("every subsystem spec MUST declare its envelope"). What makes a spec a subsystem spec? That it declares an envelope. When must a spec declare an envelope? When it is a subsystem spec. The definition gives no independent test. A concrete test would be: "A subsystem is a Go package that implements an event producer or consumer registered with the in-process event bus." That is testable at build time (grep for bus registration calls).
- **"traces" (§3, AR-012)** — defined as "durable transition records carrying the full AlphaGo field set … distinct from events." The AlphaGo field set is enumerated (prior state, actor role, candidate actions, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence). But "candidate actions considered" for a deterministic dispatch step is vacuous — there is one candidate, always chosen. The trace record will be 11 fields where 6 are trivially-populated. Either the definition should distinguish "transition traces" (full field set, for agentic nodes) from "dispatch traces" (narrow, for deterministic routing), or the field set should be declared optional for deterministic transitions. As written, deterministic subsystems will produce 11-field traces where 6 fields are meaningless, and the invariant will technically pass.
- **"workflow graph" (§3, AR-048)** — "a DOT document describing how execution happens: nodes, edges, policies by reference." But DOT documents don't natively support "policies by reference." DOT has node attributes; policy references are carried as attribute values by convention. The definition glosses the encoding convention, and the convention lives in execution-model, not here. The definition is correct but uninformative without the cross-reference; add `(see [execution-model.md §2.1])` to the glossary entry.

## Hidden assumptions

1. **That subsystem = runtime Go package is the only meaningful subsystem shape for foundation specs.** The corpus contradicts: operator-nfr and reconciliation are both foundation specs but neither is a runtime subsystem. AR-013 and AR-016 implicitly assume all foundation specs are subsystems; they aren't.
2. **That downstream specs will cite requirement IDs, not just file names.** The amendment protocol (AR-020) depends on this for re-review targeting. The corpus shows zero AR-ID citations. Either the assumption is wrong or the protocol is aspirational.
3. **That "feature" temptation is stronger than "story," "epic," "task," "milestone" temptations.** §4.10 and A.3 single out "feature" but those other terms are equally tempting and equally absent. Why the special treatment? Either the exclusion generalizes (any aggregation artifact beyond the three) or the specific callout is folklore.
4. **That the amendment protocol's "at least two foundation-review personas" (AR-020) is available.** This depends on kerf's reviewer-spawning mechanics. If the spec is adopted before kerf reliably produces two personas, AR-020 blocks every amendment. The spec assumes the tooling is present.
5. **That post-MVH process geometry can keep envelope semantics unchanged (AR-019).** This is plausible but untested. In-process event bus has no network failure modes; out-of-process has many. Preserving "envelope semantics" across that boundary may require adding axes (e.g., delivery-guarantee) that the four-axis classification doesn't cover. The assumption is load-bearing for the MVH pin's defensibility and deserves an open question.
6. **That the daemon is a single process for MVH (AR-016 + AR-017).** AR-017 enumerates three out-of-process classes. But the daemon's "subprocess invocations of `br`" could be reframed as IPC; the handler-contract subprocesses are IPC by another name. The "single process" story is simpler than the topology the subsystems actually draw. Whether that simplification holds under stress is an open question.
7. **That a trace record's eleven AlphaGo fields are meaningful for every durable transition.** AR-012 enumerates prior state, actor role, candidate actions considered, chosen action, policy version, parameter vector, evidence, outcome, verifier metrics, next state, confidence. For a deterministic dispatch step, "candidate actions considered," "parameter vector," "confidence," and "verifier metrics" are vacuous or trivially populated. The invariant is shaped for agentic nodes and applied uniformly. Two likely outcomes: subsystems either emit 11-field records with 5–6 meaningless fields (ceremony), or the invariant is tacitly weakened in practice (undocumented drift). Either is a problem. Better: split trace-shape into "agentic-transition-trace" (full AlphaGo set) and "deterministic-transition-trace" (narrower) in execution-model, and have AR-012 reference both.
8. **That "semantic judgment" (AR-008) is a term a reviewer can apply deterministically.** The AR-008 anti-pattern list (keyword-matching, heuristic fallback trees, regex parsing, hardcoded scoring) is concrete. But the general phrase "semantic judgment" is not. A deterministic evaluator over structured fields that embeds a truth table written by a human who thought semantically about the fields — is that semantic judgment? The ZFC rule has a sharp edge against the four listed patterns and a fuzzy edge against anything else. That is fine for MVH but worth naming as a limitation of the test.

## What this review did not challenge (and why)

- **The four-axis tuple composition.** Whether `(llm-freedom, io-determinism, replay-safety, idempotency)` is the right four axes is outside the scope of a round-1 critic pass; challenging it would require a different review (research-backed, not corpus-audit-backed). The affirmations above assume the axes are load-bearing as chosen.
- **The seven role names.** AR-032 lists Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor. Whether this taxonomy is complete or carved at the right joints is an AlphaGo-design question, not a spec-shape question. The critic-pass accepts the taxonomy and challenges only its encoding.
- **The Beads-integration-specific rules.** Those are owned by beads-integration.md; this pass notes only that beads-integration does not declare an architecture-mandated envelope, which is the cross-spec issue.
- **The A.3 rationale quality.** Rationale appendices are non-normative by template convention; challenges would not change a single requirement. Noted positively in Affirmations where the rationale carries load.

## Consolidated edit list (what this review asks the author to change)

Ranked by impact on downstream specs. Each item is small-to-medium; together they reduce requirement count from 51 to roughly 40 and invariant count from 6 to 2 without removing normative content.

1. **Split "subsystem spec" from "foundation cross-cutting spec."** Introduce a category distinction, scope AR-013 and AR-016 to runtime subsystems only, exempt operator-nfr and reconciliation. (C1; affects AR-013, AR-016, AR-INV-004.)
2. **Require AR-NNN citations in downstream `depends-on`.** Not just `architecture.md`. Enables AR-020's re-review targeting. (C2; affects AR-020, AR-022, and every downstream spec's §9.1.)
3. **Purge the "§1.1 … §1.9" numbering from §1 Purpose.** Replace with citations to the actual spec's §4.N subsections. (C2; purely editorial, high confusion-reduction.)
4. **Delete AR-008.** Its anti-pattern list moves to a `> EXAMPLE:` under AR-006. (C4.)
5. **Re-tag AR-007 as mechanism.** Delegation-path-naming is a spec-draft obligation, not a runtime cognition event. (C6.)
6. **Clean up §5.** Delete AR-INV-002, AR-INV-004, AR-INV-005, AR-INV-006 as duplicates. Promote AR-037. (Invariants challenged.)
7. **Merge AR-026 and AR-035.** Two copies of the orthogonality rule. (MUST/SHOULD discipline.)
8. **Either weaken AR-042 or make it concrete.** Currently rigorous-sounding, does no work. (C3.)
9. **Either downgrade AR-023 or define the declaration surface.** Current MUST has no enforcement path. (C5.)
10. **Add an open question on trace-shape uniformity.** The 11-field AlphaGo set is shaped for agentic nodes; document the deterministic-node case. (Hidden assumption 7.)

Items 1, 2, 4, and 6 are the load-bearing ones; items 3, 5, 7 are cleanup; items 8, 9, 10 defer honest work that currently hides behind strong wording.
