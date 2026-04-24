# Architecture v0.2 — Round 2 Conformance Auditor Review

**Reviewer lens:** For every AR-NNN, does the spec tell us how to *prove* an implementation (or a downstream spec) conforms? Is the path lint-enforced, test-enforced, reviewer-enforced, or absent?

**Target:** `specs/architecture.md` v0.2.0 (spec-template-version 1.1).

## Verdict

**Conditional pass with structural gaps.** §10.2 names test obligations for AR-001..AR-053, which is a meaningful improvement over a naked "conformance = reviewers agree." The grouping is sound. However, the audit surfaces three classes of defect that MUST be fixed before this spec can backstop downstream conformance claims:

1. **Cognition-tagged requirements have no runtime verification path.** AR-021 is `cognition`-tagged but §10.2 groups it with AR-020..AR-023 under "procedure-doc test." A `cognition` requirement whose only sensor is "the procedure doc exists" is not verifiable — it only proves the doc was written, not that the judgment was correctly performed on any given amendment.
2. **AR-042 ("invariants MUST name their sensor") is normatively required, but the spec's own invariants AR-INV-001 and AR-INV-003 only partially satisfy it.** Their named sensors are group-level ("corpus-lint per §10.2 AR-005..AR-007 group") rather than an enforcement hook at the moment the invariant could be violated.
3. **Several requirements land in §10.2 with ambiguous enforcement.** AR-003's daemon-vs-handler boundary check is declared reviewer-enforced; AR-007's delegation-path completeness is also reviewer-enforced; AR-023's overlap detector is deferred to OQ-AR-003. These are defensible choices, but they concentrate semantic judgment in reviewers without naming which persona performs the judgment or what inputs they receive.

The spec is coherent and implementable; it is not yet *auditable* in the sense AR-042 demands of downstream specs.

## Conformance audit per-requirement

| Req ID | Tag | Verification path in §10.2 | Path type | Gap? |
|---|---|---|---|---|
| AR-001 | mechanism | "Corpus-level lint: every requirement either has no `Axes:` line or carries full tuple with valid tokens" | lint | none |
| AR-002 | mechanism | Implied in AR-001 group (baseline inference) | lint | none |
| AR-003 | mechanism | "Detection is reviewer-enforced per §10.2" (body) | reviewer | persona unnamed; no delegation path for the reviewer |
| AR-004 | mechanism | AR-001..AR-004 group — "every cross-subsystem type carries both tag lines" | lint | "transitively referenced" is not mechanically decidable without a type-dependency graph; lint underspecified |
| AR-005 | mechanism | "Every requirement carries exactly one `Tags:` value" | lint | none |
| AR-006 | mechanism | AR-005..AR-007 group | lint+reviewer | ZFC-violation detection ("keyword matching for completion") needs semantic read |
| AR-007 | mechanism | "Every `cognition`-tagged requirement names a delegation path" | reviewer | "names" is not mechanically decidable; what counts as complete path is not spelled out |
| AR-009 | mechanism | "Corpus presence test: search, verifier, trace representations appear in corpus" | lint-ish | presence test unspecified — which files, which tokens |
| AR-010 | mechanism | Same group as AR-009 | lint-ish | relies on execution-model landing; bootstrap citation to components.md §2 |
| AR-011 | mechanism | Same group as AR-009 | lint-ish | "No subsystem named verifier" is lintable; role-function realization is not |
| AR-012 | mechanism | Same group as AR-009 | lint-ish | "trace is distinct from events" requires cross-spec read |
| AR-013 | mechanism | "Every `runtime-subsystem` spec carries §4.0 Subsystem envelope with eight elements" | lint | none (strong — element presence is mechanical) |
| AR-014 | mechanism | AR-013..AR-019 group | reviewer | "invent shared vocabulary," "violate mechanism/cognition boundary" are semantic checks |
| AR-015 | mechanism | Same group | reviewer | "could the subsystem have been written without foundation revision" is retrospective judgment |
| AR-016 | mechanism | Same group — "every subsystem realized as a Go package" | lint | none (package layout is checkable) |
| AR-017 | mechanism | Same group — "enumerated out-of-process actors honored" | reviewer | detection of "fourth actor class introduced" is an architectural-review scan |
| AR-018 | mechanism | Same group (implied) | lint | lintable: no spec named `reconciliation` declares an envelope |
| AR-019 | mechanism | Same group (implied) | none-at-MVH | declarative future-scope, no MVH verification path |
| AR-020 | mechanism | "Procedure-doc test: amendment protocol is documented" | reviewer | proves doc existence, not adherence |
| AR-021 | cognition | Same group | **none** | cognition-tagged; no delegation-path audit of the amendment review itself |
| AR-022 | mechanism | Same group | lint | "each subsystem cites foundation version" is mechanical |
| AR-023 | mechanism | "Overlap detection is a scripted check" | deferred | OQ-AR-003 defers the tooling; until then reviewer-enforced |
| AR-024 | mechanism | AR-024..AR-031 group — "every occurrence of `agent_type` matches regex" | lint | none for identifier shape; conformance-class coverage is reviewer |
| AR-025 | mechanism | Same group | lint | regex is mechanically checkable |
| AR-026 | mechanism | Same group (orthogonality) | reviewer | "not conflated" is a read-the-spec check |
| AR-027 | mechanism | Same group — four-surface byte-equality | lint | STRONG — all four surfaces can be cross-diffed mechanically |
| AR-028 | mechanism | Same group (declarative) | none | no verification needed; it is an enabling rule |
| AR-029 | mechanism | "Every occurrence of `verification` is qualified as one of three hyphenated forms" | lint | strong; also protects node-type enum closure |
| AR-030 | mechanism | Same group | lint+reviewer | status-enum membership lintable; evidence/notes population is reviewer |
| AR-031 | mechanism | Same group | lint | "quality-gate reads a verification-result" is lintable in policy schema |
| AR-032 | mechanism | "No spec introduces a role name outside the seven" | lint | strong |
| AR-033 | mechanism | Same group | reviewer | "required at MVH" vs "deferred" is semantic |
| AR-034 | mechanism | Same group | reviewer | "WHAT the role IS" vs "ALLOWED TO DO" boundary is judgment |
| AR-035 | mechanism | Cross-reference to AR-026 | reviewer | inherits AR-026's verification path |
| AR-036 | mechanism | Same group — "merge responsibility does not appear as a top-level role" | lint | strong |
| AR-037 | mechanism | AR-037..AR-045 group — review-agent scenario | reviewer-scenario | scenario defined as "proposals introducing file-based handoff are rejected" — but the scenario corpus is not itself specified |
| AR-038 | mechanism | Same group | reviewer-scenario | same |
| AR-039 | mechanism | Same group | reviewer | worktree-usage audit |
| AR-040 | mechanism | Same group | none | declarative tradeoff acknowledgment |
| AR-041 | mechanism | Same group | lint | "every normative artifact is in the repo" — lintable if "normative artifact" is enumerable |
| AR-042 | mechanism | Same group — "guides+sensors pairing is verified per-invariant" | reviewer | SEE TRACE BELOW — this is the core auditor question |
| AR-043 | mechanism | Same group (declarative) | reviewer | "spec asking for exemption" is a review flag |
| AR-044 | mechanism | Same group | reviewer | filesystem-backed discipline check |
| AR-045 | mechanism | Same group | reviewer | quality-left ordering check on workflow DOTs |
| AR-046 | mechanism | AR-046..AR-051 group | lint | "no fourth artifact introduced" |
| AR-047..AR-050 | mechanism | Same group | reviewer | declarative artifact definitions |
| AR-051 | mechanism | Same group — "no spec treats `feature` as normative term" | lint | strong |
| AR-052 | mechanism | AR-013..AR-019 + AR-052..AR-053 group | lint | front-matter presence |
| AR-053 | mechanism | Same group | lint | §4.0 section-slot presence |
| AR-INV-001 | mechanism | Cross-refs AR-005..AR-007 + AR-037..AR-045 groups | lint+reviewer | sensor named; see gap below |
| AR-INV-003 | mechanism | Cross-refs AR-009..AR-012 group | lint | sensor named |

### Summary of the path-type distribution

Counting the 53 requirements + 2 invariants by primary verification path:

- **Pure lint-enforced:** AR-001, AR-002, AR-005, AR-013, AR-016, AR-018, AR-022, AR-025, AR-027, AR-029, AR-031, AR-032, AR-036, AR-041, AR-046, AR-051, AR-052, AR-053 — ~18 requirements (≈34%).
- **Pure reviewer-enforced:** AR-003, AR-007, AR-014, AR-015, AR-017, AR-020, AR-026, AR-033, AR-034, AR-035, AR-039, AR-043, AR-044, AR-045, AR-047..AR-050 — ~17 requirements (≈32%).
- **Mixed (lint grammar + reviewer semantics):** AR-004, AR-006, AR-024, AR-030, AR-042 — ~5 requirements.
- **Review-agent scenario:** AR-037, AR-038 — 2 requirements.
- **Deferred / declarative / future-scope:** AR-019, AR-028, AR-040 — 3 requirements.
- **No verification path at all:** AR-021 (cognition-tagged; §10.2 provides nothing beyond "doc exists").
- **Deferred to tooling OQ:** AR-023 (OQ-AR-003 defers the overlap detector).

The distribution is top-heavy on reviewer-enforced checks. That is defensible for a meta-spec, but only if the reviewer persona is named and the input shape is defined. Currently the personas are implicit.

## Unverifiable or under-verified requirements

### AR-021 — cognition-tagged material-change determination

AR-021 is `cognition`-tagged: "Material-change determination is a cognition-tagged reviewer step." Per AR-007, cognition-tagged requirements MUST name a delegation path (role, model class, input shape). AR-021's body says "the reviewer persona evaluates whether the proposed change alters cross-cutting invariants, renames canonical terms, or widens/narrows a scope." This names *criteria* but not a *delegation path*: which reviewer persona (architect? critic? conformance auditor?); which model class; what input shape the reviewer receives (full amendment doc + prior version diff? only the proposal?). §10.2 does not add a verification path for AR-021 beyond the AR-020..AR-023 "procedure-doc test," which proves the doc exists, not that any particular judgment was correctly made. **AR-021 fails its own AR-007 obligation and has no runtime verification path.**

### AR-042 sensor-tracing — pick AR-INV-001

Trace the claim "the invariant names its sensor." AR-INV-001: "Sensor: corpus-lint per §10.2 AR-005..AR-007 group plus reviewer-agent scenario per §10.2 AR-037..AR-045 group." Follow the pointer:

- §10.2 AR-005..AR-007 group: "Every requirement carries exactly one `Tags:` value. Every `cognition`-tagged requirement names a delegation path per AR-007. No daemon-side requirement is `cognition`-tagged."
- The lint for "no daemon-side requirement is `cognition`-tagged" requires knowing which requirements are "daemon-side." The lint has no way to know that — it would need a machine-readable daemon/agent-subprocess classification on every requirement, which the template does not require. The sensor for AR-INV-001 is therefore under-defined: it names a group but the group's own lint cannot actually decide the invariant.
- §10.2 AR-037..AR-045 group: "Review-agent scenario tests: proposals introducing file-based agent handoff or per-agent worktree ownership are rejected." This is a scenario for AR-037/AR-038, not for AR-INV-001.

**AR-INV-001's named sensor does not actually detect its own violation.** AR-042 passes by the letter (a sensor is named) but fails by the spirit (the sensor cannot fire on the invariant being violated).

### AR-003 daemon/handler boundary

AR-003's body: "Detection is reviewer-enforced per §10.2 (the lint only checks tag grammar, not tag semantics against the daemon/handler boundary)." The spec acknowledges the lint gap but does not name which review persona is obligated to perform the check or when (pre-finalize? re-reviewed on amendment?). This is a reviewer-enforced path without a reviewer-assignment hook.

### AR-007 delegation-path completeness

"A reviewer verifies the path is complete." No persona named, no input shape for the reviewer, no accept/reject criteria rubric. This is the conformance equivalent of a TODO: the obligation is declared, the verification is promised, the mechanism is unspecified.

### AR-019 post-MVH process geometry

Declarative future-scope; has no MVH verification path. Acceptable because it is an enabling rule (constrains future amendments, not current implementation), but §10.2 should say so explicitly rather than leaving the group silent.

### AR-014 and AR-015 "did we do the procedure right?"

AR-014 forbids subsystems from "inventing shared vocabulary not grounded in an existing foundation term" and "violating the mechanism/cognition boundary by performing cognition in framework code." Both checks require a reviewer to read the downstream spec and decide whether a term is "grounded" or whether a surface "performs cognition." Neither is mechanical. §10.2 gives no rubric and names no persona; the checks effectively collapse into "someone must have looked."

AR-015 is retrospective: "if a new subsystem cannot be written without foundation revision, the gap MUST be captured as an amendment." The sensor would have to fire at spec-draft-time by someone noticing the author wanted foundation revision and didn't file an amendment. There is no forcing function.

### AR-023 overlap detection deferred

The requirement names `touches:` front matter as the lintable surface, which is a solid mechanical hook. But the *overlap detector* that reads `touches:` lists and flags intersections is deferred to OQ-AR-003; until it lands, overlap detection is reviewer-enforced. This is honest, but the spec does not say what the MVH bridging obligation is — does the orchestrator-agent run the check on each amendment? Is it a pre-finalize gate? Unnamed.

## Missing test obligations

1. **Per-persona assignment for reviewer-enforced checks.** §10.2 says "review-agent scenario tests" and "reviewer-enforced" but never names which persona performs which check. The round-2 review jig has architect, critic, conformance-auditor, implementer, and scope-steward personas; §10.2 should say e.g., "AR-003 boundary detection is conformance-auditor-enforced at spec finalize; architect-enforced on amendment."
2. **Cognition-tagged auditability loop.** Add a §10.2 note: cognition-tagged requirements' verification is performed by a designated reviewer persona; the persona's own input shape and rubric are declared here or in a cited operator-nfr section.
3. **Invariant-sensor grounding.** AR-042 demands invariants name their sensor. Strengthen §10.2 to explicitly list, per invariant (not per group), the sensor that fires on violation and the classification of that sensor (lint / reviewer-scenario / test-ID). AR-INV-001 and AR-INV-003 should each have their own §10.2 line.
4. **Migration verification.** AR-027 mandates a `handler_type` → `agent_type` rename across event-model, handler-contract, workspace-model in their next revision cycle. §10.2 has no obligation to verify the migration landed. Add a corpus-lint entry: "no spec carries `handler_type` identifier after the owning spec's next revision cycle is closed."
5. **AR-004 transitive-type-tagging check.** The obligation "types transitively referenced by any event payload a different subsystem consumes MUST carry tags" needs either (a) a type-dependency-graph lint, or (b) a declaration that the check is reviewer-enforced on every envelope edit.
6. **AR-017 actor-enumeration closure.** The lint for "no fourth actor class is introduced without amendment" is not specified. Mechanically: enumerate every subprocess-spawn site in the corpus; reject any not matching (a)/(b)/(c). State this explicitly.
7. **AR-036 merge-node verification.** "Merge MUST operate in the run's leased worktree" needs a sensor on workflow-library DOT files. §10.2 does not mention DOT linting of merge nodes; add it or acknowledge reviewer-only.
8. **AR-045 quality-left auditing.** The rule admits explicit overrides in DOT with a cited policy reason. The sensor should either (a) require a structured `quality-left-override: <reason>` DOT attribute that lint can find, or (b) name the reviewer persona that audits policy reasons. Currently neither path is specified.
9. **§10.2 migration trigger to testing.md.** OQ-AR-001 tracks migrating the prose obligations to `[testing.md §<layer>]` citations. No deadline. A conformance auditor has no way to notice the migration fell off. Tie the migration to testing.md's `status: reviewed` transition as a mechanical trigger.

## Recommendations

1. **Split §10.2 from group-level to per-requirement.** Every AR-NNN gets a line naming: (sensor type ∈ {lint, reviewer-persona, test-ID, scenario, declarative}), (enforcement timing ∈ {draft-time, finalize, amendment, runtime}), (persona or test artifact). Group-level obligations hide failures.
2. **Fix AR-021's delegation path** or re-tag it as mechanism (the criteria it names — "alters cross-cutting invariants, renames canonical terms, widens/narrows scope" — are arguably mechanical schema checks, not semantic judgment). If it stays cognition-tagged, name the reviewer persona, the model class, and the input shape the reviewer receives.
3. **Strengthen AR-042's own sensor ground.** Add a §10.2.1 subsection dedicated to the two architecture invariants AR-INV-001 and AR-INV-003, each with its own enforcement hook rather than piggy-backing on §4 requirement groups.
4. **Name the reviewer persona for every reviewer-enforced check.** AR-003, AR-007, AR-014, AR-015, AR-017, AR-020, AR-021, AR-026, AR-033, AR-034, AR-037..AR-045 all say "reviewer-enforced" without naming the persona. The round-2 jig proves personas exist; use them.
5. **Add a conformance-auditor-specific lint corpus.** The spec is good at declaring lints; it is not yet packaged as a set of runnable rules under `tools/`. Track this as OQ-AR-005 (new): "ship the corpus-lint script that satisfies §10.2's lint entries; until landed, lint obligations are reviewer-enforced by hand."
6. **Require each invariant to carry its own `Sensor:` field in the block** rather than relying on a prose line. A structured `Sensor: lint=<id>; persona=<name>; timing=<stage>` field would render AR-042 mechanically auditable.
7. **Add a §10.3 exclusion for AR-019.** State that post-MVH process geometry is not verified at MVH; otherwise AR-019 looks like an obligation with no test.
8. **AR-027 migration deadline.** "Next revision cycle" is not a deadline the auditor can pin. Either name a date, or condition the migration on event-model.md / handler-contract.md / workspace-model.md status transitions that the lint can observe.

The spec is strong on *what* must be true and only partially strong on *how we know it is true*. Closing the recommendations above lifts architecture v0.2 from "coherent meta-spec" to "auditable meta-spec."

## Appendix: full sensor trace for a second invariant

To cross-check AR-042's workability, trace AR-INV-003 ("search + verifier + traces are required, not optional"). Named sensor: "corpus presence test per §10.2 AR-009..AR-012 group." Follow the pointer:

§10.2 AR-009..AR-012: "Corpus presence test: search representation (transition kinds in execution-model), verifier representation (verification-node type in execution-model), trace representation (Transition record in execution-model) all appear in the corpus."

This is more concrete than AR-INV-001's trace: the sensor reduces to three token-presence checks in execution-model.md. A lint can literally grep for (a) the four transition kinds named in AR-010, (b) the verification-node concept in execution-model, (c) the Transition record. If execution-model.md ever landed without one of these tokens, the lint fires. AR-INV-003 therefore *does* satisfy AR-042 — its sensor fires on its violation.

The asymmetry is instructive: AR-INV-003's sensor is grounded because the invariant is about *presence of tokens in another spec*, which is mechanically checkable. AR-INV-001's sensor is under-grounded because the invariant is about *semantic properties of the runtime process boundary*, which the template cannot tag for the lint. The recommended fix is to require, per §4.N+1, a `Process:` tag per requirement (`daemon`, `agent-subprocess`, `orchestrator-agent`, `br-subprocess`, `meta`) so the lint can enforce AR-INV-001 mechanically.

## Appendix: correspondence with round-1 findings

Round 1's conformance review (if it followed the same lens) would have flagged AR-008 as a duplicate verification path with AR-006 + AR-INV-001; v0.2 retires AR-008, which closes that loop. Round 1 would also have noted that AR-011 and AR-029 previously described verification as a node-type-enum member (now role-function; EM-006 alignment). Those fixes landed. The remaining gaps this round surfaces (AR-021 cognition path, AR-042 sensor grounding, persona naming for reviewer-enforced checks) are net-new or deepened from round 1.

## Appendix: priorities

Rank the recommendations by blocking/non-blocking for spec finalize:

1. **Blocking (fix before `reviewed`):** Fix AR-021 delegation path (it fails its own AR-007). Strengthen AR-INV-001 sensor (it fails AR-042 in spirit).
2. **Blocking if §10.2 is to be normatively enforceable:** Per-requirement §10.2 restructuring; persona naming for reviewer-enforced checks.
3. **Non-blocking but high-value:** Corpus-lint tooling OQ; AR-027 migration-deadline tightening; `Sensor:` and `Process:` requirement-block fields.
4. **Declarative/future:** AR-019 §10.3 exclusion note; post-MVH amendment trigger list in OQ-AR-004.

With the blocking items closed, architecture v0.2 becomes the bedrock the conformance auditor persona needs when reviewing downstream specs: every downstream AR's verification path can be traced back to a sensor that actually fires.

## Appendix: conformance-auditor workflow implied by the spec

To make the audit loop explicit, here is how a conformance-auditor persona would actually use architecture v0.2 to audit a downstream subsystem spec:

1. Read the downstream spec's front matter; check `spec-category` is declared (AR-052 lint).
2. If `runtime-subsystem`, confirm §4.0 Subsystem envelope exists and names all eight elements (AR-053, AR-013 lint).
3. For each type declared in the envelope, check `Tags:` + `Axes:` (AR-001, AR-004, AR-005 lint).
4. For each `cognition`-tagged requirement, check the body names role + model class + input shape (AR-007 reviewer).
5. For each cross-subsystem reference to `agent_type`, check regex match (AR-025 lint) and identical spelling across the four surfaces (AR-027 lint).
6. For each use of "verification" in prose, check it is one of the three hyphenated forms (AR-029, AR-030, AR-031 lint).
7. For each role name, check it is one of the seven (AR-032 lint).
8. For each invariant, check the `Sensor:` name is present and grounded (AR-042 reviewer).

Steps 1, 2, 3, 5, 6, 7 are mechanically runnable today if the lint script existed. Steps 4 and 8 need a reviewer persona with a rubric. Packaging these steps as a checklist under `tools/conformance-auditor-checklist.md` (or embedded in kerf's review jig) would operationalize the spec.
