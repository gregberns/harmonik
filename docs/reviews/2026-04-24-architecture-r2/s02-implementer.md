# Architecture v0.2 — Round-2 Review: S02 Policy Engine Implementer

**Reviewer lens.** I am writing S02 Policy Engine in Go. S02 owns the ControlPoint registry per CP-043 / CP-047, constructs ControlPoint instances by reading policy YAML (CP-035 / §7.1 registration sequence), and exposes the Registry interface (§6.1.7). S01 (Gate/Guard invocation) and S05 (Hook dispatch) consult my registry but do not own it. Architecture's meta-rules (ZFC, four-axis, envelope, centralized-controller, three-artifact) constrain every Go type, registration path, and evaluator-tagging decision I make. This review asks: does architecture give me enough to author S02 spec and code without round-tripping through further foundation revisions?

## Verdict

**Conditionally sufficient.** Architecture v0.2 is load-bearing for S02 and the citations I need are present — AR-001/AR-002 for axis tags on ControlPoint and Evaluator records, AR-005/AR-006/AR-007 for the mechanism/cognition split on evaluators, AR-013/AR-052/AR-053 for my envelope shape, AR-037 for the centralized-controller posture S02 inherits, AR-046–AR-051 for keeping spec / workflow / bead separate at the primitive level. The Round-1 fixes (retiring AR-008, collapsing AR-INV duplicates, re-tagging AR-007 as mechanism, adding AR-052/053, adding §A.1 envelope exemplar) materially improve S02's authoring experience. BUT six gaps remain that force me to either invent local conventions or cite control-points.md back into architecture. The centralized-controller gap (Gap 4 below) is the most consequential: architecture does not say where the ControlPoint registry lives in the authority topology, and I inferred it from CP-043 rather than from AR. The four-axis-per-polymorphic-type gap (Gap 3) leaves me unable to mechanically author one ControlPoint record's Axes line without reading examples. Neither is a blocker, but both are latent coupling I would prefer foundation owned.

### Direct answers to the stress-test questions

**Q1 — Does AR specify enough to author S02's envelope?** Yes, with one meta-caveat (element (f) meaning; resolved by judgment). AR-052/053 + AR-013 + §A.1 give me a mechanical authoring path. I can produce S02 §4.0 in ~20 minutes.

**Q2 — Is the mechanism/cognition tagging decision rule clear for policy evaluation?** Mostly yes. AR-006 + AR-007 give me the mechanism-vs-cognition rule; CP-003 + CP-005 apply it per-Kind. The decision rule for *each evaluator instance* is unambiguous. The decision rule for *polymorphic types* like `Evaluator` is not — see Gap 3. For per-requirement tagging, the rule is clear and I can tag every S02 requirement mechanically.

**Q3 — Can S02 author requirement-by-requirement mechanically on the four axes?** Mostly yes. AR-002's baseline rule means most S02 requirements omit the Axes line; deviations declare the full tuple. I know the baseline is `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`. The only genuine tension is `Register` (technically non-idempotent on first registration — it mutates the map — but idempotent on re-registration with identical body per CP-044). I will tag it `idempotency=idempotent` with a footnote. No architecture change needed for this specific case; the AR-002 language covers it.

**Q4 — Does S02 consume ControlPoints from a central registry?** S02 *owns* the central registry; S01 and S05 consume from it. Architecture's centralized-controller principle (AR-037) supports this by spirit but does not enumerate "policy state" as a daemon-owned surface, so the load-bearing normative citation comes from CP-045 + CP-047, not AR. This is the inversion I flagged in the verdict: a control-points.md rule is doing architecture's work. Not a blocker — CP-047 is a well-formed spec-level rule — but the authority is downstream of where it should be. See Gap 4.

**Q5 — Does S02 reason about workflow / bead / spec distinctly?** Yes. S02 consumes policy YAML (a configuration artifact, not one of the three). S02 exports ControlPoint instances that are referenced by workflow-graph DOT attributes (CP-036). S02 never touches beads — beads are an execution-model / handler-contract surface. The three-artifact separation holds cleanly at S02's boundary. The one unresolved question is whether policy YAML is an implicit fourth artifact (Gap 5); architecture should say no explicitly.

## Implementation attempts (what I tried to author from architecture alone)

### Attempt 1: Author the S02 envelope from AR-013 + §A.1 exemplar

Per AR-052, S02's spec carries `spec-category: runtime-subsystem`. Per AR-053, §4.0 Subsystem envelope is the first subsection under §4. Per AR-013, I declare eight elements. The §A.1 exemplar gives me the exact markdown shape to fill in.

I can author eight elements:

- (a) events produced: `control_points_registered`, `gate_allowed`, `gate_denied`, `gate_escalated`, `guard_reordered`, `guard_failed`, `budget_*`, `hook_verdict_persisted`, `verdict_envelope_mismatch`, `policy_expression_exceeded_cost`. Payload schemas cited from event-model.
- (b) events consumed: on startup, the policy-YAML-load chain; at runtime, none directly — S02 is consulted, not event-driven.
- (c) types introduced (cross-subsystem): `ControlPoint`, `Evaluator`, `DelegationPath`, `GateVerdictRecord`, `HookVerdictRecord`, `CognitionMeta`, `SideEffect`, `FreedomProfile`, `PermissionSchema`. Each carries axes + mechanism/cognition tag per AR-004.
- (d) handlers implemented: none (S02 is not a handler-contract conformance class).
- (e) state owned: the Registry table (§6.1.7), the loaded policy-document set, the symbol table built during registration.
- (f) control points provided: S02 is the registry owner; it provides the primitive *type*, not instances. Worth calling out explicitly in the spec.
- (g) NFRs inherited/overridden: startup-time registration latency; deterministic lookups (CP-046); daemon-local scope (CP-045).
- (h) boundary classification per operation: `Register`, `LookupByName`, `LookupByTrigger`, `LookupByAttachPoint`, `All` — all mechanism, all `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` (Register is idempotent on identical body per CP-044; fails on divergent body, which is a detection not a mutation).

This worked. AR-052/053 + §A.1 gave me the slot and the shape. The envelope exemplar is exactly the authoring aid I needed — Round-1 left me guessing; Round-2 doesn't.

One subtle point: element (f) "control points provided." S02 is the registry owner. It does not *provide* ControlPoints in the sense that S01 consumes Gates from S02 — Gates are defined in policy YAML and registered through S02. So S02's (f) is "S02 provides the ControlPoint *type* and the registry; individual ControlPoints are policy-YAML-declared and are not S02-owned data." This meta-distinction is not addressed by AR-013 or §A.1. I will resolve it by writing "(f) Control points provided: the ControlPoint primitive type and registry surface; specific ControlPoint instances are policy-YAML-declared." An architecture informative note would help but is not required — the sensible reading is self-evident.

### Attempt 2: Tag every ControlPoint record field mechanically

CP-003 says every evaluator is boundary-classified; CP-005 names Kind-specific boundary rules (Gate mechanism-or-cognition, Hook mechanism-or-cognition, Guard mechanism-only, Budget mechanism-only). I tried to write the Axes line for each of the nine cross-subsystem types S02 exports without looking at a reference ControlPoint.

I failed on `Evaluator`. An Evaluator is a sum type — its *mode* field selects `mechanism` vs `cognition`. Under AR-005 every requirement has one tag, but `Evaluator` is a *type*, not a requirement, and the tag depends on which mode is instantiated. Architecture AR-004 says "every type a subsystem exports… MUST carry the four-axis tuple and the mechanism/cognition tag" — but Evaluator's tag is polymorphic. I defaulted to: tag the type itself `mechanism` (the envelope-declared structure is deterministic) and leave the `mode` field to drive per-instance classification. This is defensible but AR does not address polymorphic types explicitly. See Gap 3.

The same issue applies to `ControlPoint` itself: its `mode_tag` field is `mechanism` or `cognition` per instance, but the type declaration has to pick one. Same resolution as Evaluator.

For the other seven types (`DelegationPath`, `GateVerdictRecord`, `HookVerdictRecord`, `CognitionMeta`, `SideEffect`, `FreedomProfile`, `PermissionSchema`), all are straightforwardly mechanism-tagged. `DelegationPath` describes a cognition-tagged evaluator's *delegation surface*, but the description itself is structural metadata — mechanism. `CognitionMeta` is a mechanism-tagged record that *describes* a cognition invocation. This matches CP-042's pattern (the verdict persistence is mechanism-tagged; the verdict production is cognition-tagged) — the split between describing-cognition and doing-cognition is a clean principle architecture supports but does not spell out. I would prefer a one-line informative note under AR-004 naming this pattern.

### Attempt 3: Locate S02 in the daemon process geometry

AR-016 pins subsystem = Go package inside the daemon binary for MVH. AR-017 enumerates the only out-of-process actors (handlers, orchestrator-agents, `br` CLI). S02 is in-process; its registry is in-daemon memory. Good.

AR-019 explicitly allows post-MVH process-geometry changes without foundation revision, which means S02 could in principle be extracted into its own process later. My Go code should therefore not assume in-process-only — the Registry interface is a good abstraction boundary, and a future network-transported registry would keep the same interface (LookupByName, etc.). This is an architecture-induced design constraint I welcome.

AR-037 says the daemon owns workflow state, routing, dispatch. The ControlPoint registry is policy state, not workflow state. Is it covered? CP-045 says the registry is daemon-scoped, but AR-037 does not enumerate "policy" as a daemon-owned surface. I inferred from AR-037's spirit and from CP-047's three-owner split (S01/S02/S05, all in-daemon) that policy state is implicitly covered, but AR-037's enumeration is underspecified for cross-cutting registries that are not workflow state, routing, or dispatch. See Gap 4.

### Attempt 4: Reason about workflow / bead / spec distinctly within S02

AR-046–AR-051 pin the three artifacts. CP-036 enforces that DOT references YAML by `name`, never inline — architecturally clean. But S02 consumes policy YAML (a fourth kind of textual artifact that's not any of the three). Is policy YAML a "spec," a "workflow graph," or something else? AR-048 says workflow graph = DOT document, so policy YAML is not a workflow graph. AR-047 says spec = document landing in `specs/`, so policy YAML is not a spec. Policy YAML is a *configuration artifact* referenced by the workflow graph, and the three-artifact rule does not classify it. I can author S02 without architectural guidance on this, but see Gap 5.

### Attempt 5: Author the registration sequence without cross-subsystem method calls

CP-048 + CP-INV-002 require that every ControlPoint effect observable across subsystem boundaries crosses via a typed event. The registration pass (§7.1) is a startup phase; once complete, S02 emits `control_points_registered`. S01 and S05 consult the registry via a Go interface call (Registry.LookupByName etc.), which is a direct in-process method call, not an event. Is this a CP-INV-002 violation?

Reading CP-INV-002 carefully: it says "every ControlPoint *effect*… MUST be observable through one of the typed events." A *lookup* is not an effect — it is a read. The invariant is scoped to effects (allow/deny/escalate/reorder/side-effect/accrual). So direct in-process method calls for reads are fine. But this is not explicitly stated in architecture; AR-037 says agent-to-agent coordination routes through the daemon, but AR does not address *subsystem-to-subsystem* coordination within the daemon. I inferred the answer (direct Go calls fine; effect propagation must be via events), but architecture could be more explicit. Not a gap — more a doc-improvement opportunity — but worth flagging in case a future reviewer questions the pattern.

### Attempt 6: Reject cognition-tagged Guards at registration time

CP-020 (Guards MUST be mechanism-tagged) is a ZFC enforcement point. At registration, §7.1 includes an explicit `IF cp.kind == Guard AND cp.evaluator.mode == cognition: FAIL`. This is S02 code. Does architecture support the existence of this check, or does it require Guard-rejection to happen elsewhere (workflow-ingest, reviewer-scenario)?

AR-006 names the ZFC rule. AR-042 says every invariant MUST name its sensor. CP-020 names registration as the sensor (and §10.2 names cognition-Guard rejection at registration as a test obligation). Architecture admits S02 as the sensor locus via AR-042's "sensor MAY be a lint rule, a verification-node role-function, a review-agent scenario, or a conformance test" — a registration-time type check is effectively a lint. OK. But AR-042's enumeration does not explicitly include "registration-time type-check" as a sensor class; S02 authors should read this generously. A one-word addition to AR-042 (e.g., "registration-time type check") would remove ambiguity.

## Gaps

### Concrete Go sketch I can author from architecture v0.2

To ground the evaluation, here is the package skeleton I can commit from architecture + control-points alone:

- `package policy` (Go package name for S02).
- Types: `ControlPoint struct`, `Kind` int-backed enum, `Evaluator struct`, `DelegationPath struct`, four Kind-specific payload structs, verdict records, `Registry interface`, `registry struct` (implementation).
- `Registry.Register(cp) error` applies the body-equality check of CP-044; body = SHA-256 over canonicalized `(kind, trigger, evaluator, payload)` serialization.
- `Registry.LookupByName/ByTrigger/ByAttachPoint/All` return sorted lists per CP-046.
- Separate file `loader.go` parses policy YAML, builds symbol table in a two-pass sweep per §7.1, emits `control_points_registered` via the event bus (owned by event-model subsystem, S02 consumer).
- Cognition-Guard rejection is a direct guard clause in `construct_control_point`.

Architecture v0.2 supports every type-level decision above. The one judgment call is whether the `Evaluator` type is declared once with `Tags: mechanism` + mode-field-driven per-instance classification (my chosen approach), or split into `MechanismEvaluator` + `CognitionEvaluator` sibling types. See Gap 3.

### Gap 1 — AR-042 sensor-naming for S02-produced invariants

CP-INV-001 (registry is single source of truth), CP-INV-002 (control-point effects observable only via events), CP-INV-003 (cognition evaluators never silently re-invoke on replay) each name sensors (lint, scenario tests, envelope-hash check). AR-042 requires every invariant to name its sensor. S02 satisfies AR-042 for its own invariants, but AR-042 does not say whether the sensor must be implementable *inside the subsystem's envelope* or whether cross-subsystem sensors are admissible. CP-INV-002 (effects-via-events-only) is detectable only by the event bus + cross-subsystem reviewer scenarios; CP-INV-003 is detectable only by a replay test that spans S02 + reconciliation. AR-042 accepts this (the sensor "MAY be" any of four kinds), but it does not mark cross-subsystem sensors as preferred, discouraged, or neutral. This is a tone gap, not a blocker.

### Gap 2 — ZFC boundary for *evaluator registration* vs *evaluator invocation*

AR-006 / AR-007 describe ZFC for evaluation points at runtime. S02's registration code runs at daemon init: it *parses* evaluator declarations from YAML, *constructs* Evaluator records, *validates* that cognition-tagged Guards are rejected (CP-020). Registration is not an evaluation point in AR's sense — the evaluator is not being invoked; it's being catalogued. AR does not say whether registration-time classification-checking logic is itself mechanism- or cognition-tagged (trivially mechanism, since it's deterministic YAML parsing + type-check), but the meta-point is that S02's own code is uniformly mechanism, and there is no architecture language that explicitly confirms "a subsystem whose *only* job is to register and look up ControlPoints is trivially pure-mechanism." I would prefer AR-003 (skeleton profile) to include a phrase like "registration and lookup surfaces are skeleton." As written, AR-003 covers "daemon routing, dispatch, checkpoint emission, edge selection, validator execution" — it does not name "registry." Near-miss.

### Gap 3 — Polymorphic/sum-type tagging rule

AR-004 requires every exported type to carry `Axes:` + `Tags:`. The `Evaluator` record is polymorphic in its `mode` field (mechanism vs cognition). Architecture does not give a rule for sum-type classification:

- Option A: tag the type by its container shape (mechanism — it is a struct), instance tag follows `mode`.
- Option B: require separate declared types (MechanismEvaluator, CognitionEvaluator).
- Option C: allow `Tags: mechanism|cognition` (declared as polymorphic), with the per-instance tag carried on the Evaluator value at construction.

CP-003 reads like Option C in effect (every ControlPoint's evaluator is classified at construction), but AR-004 reads like it demands Option A or B at the *type* level. S02 spec author picks C and hopes architecture reviewers accept; the spec should pick explicitly.

### Gap 4 — Centralized-controller coverage for policy state

AR-037 enumerates "workflow state, routing, dispatch" as daemon-owned. Policy state (registry, loaded YAML, symbol table) is not in the enumeration but is daemon-owned in practice per CP-045. Either (a) AR-037 should read "all cross-cutting state" or (b) AR-037 should enumerate "policy state" as a fourth daemon-owned surface. Either fix is small. The current asymmetry is discoverable: a subsystem author reading AR-037 alone might think policy state could live elsewhere (e.g., in a separate process, in Beads, on disk) and only discover the in-daemon pin by reading CP-045. The three-owner split (CP-047) should be visible from architecture, not just from control-points.md. A one-sentence reference in AR-037 naming "policy registries owned by subsystems" or similar would close this.

### Gap 5 — Policy YAML classification in the three-artifact schema

AR-046–AR-051 name three artifacts (spec, workflow graph, bead) and forbid a fourth. Policy YAML is neither a spec nor a workflow graph nor a bead; it is a *configuration artifact* referenced by workflow graphs via `policy_ref` / `gate_ref` / `freedom_profile_ref` / `budget_ref`. AR-050 (many-to-many relationships) gestures at the relational shape but doesn't name policy YAML as an auxiliary artifact class. The right fix is an informative note under §4.10 stating that configuration artifacts (policy YAML, skill manifests) are not *work composition* primitives and therefore do not count as a fourth artifact under AR-051. Without that note, a future author might argue policy YAML is a fourth artifact and trigger a spurious AR-051 violation.

### Gap 6 — Registry ownership as a centralized-controller consequence is unstated

The centralized-controller principle (AR-037 / AR-038) makes strong claims about workflow routing and agent coordination, but does not derive the corollary that *shared mutable-at-startup / immutable-at-runtime registries MUST have a single owning subsystem*. CP-047 names the three-owner split (S01 owns Gate/Guard invocation, S02 owns registry, S05 owns Hook dispatch), and CP-INV-001 forbids shadow registries. These three rules are mutually reinforcing, but architecture does not name "single-owner registry" as a general pattern that subsystem specs inherit. If a future subsystem spec (e.g., a future S07 for skill packages) tries to introduce a "skill-package registry" owned by two subsystems in parallel, architecture today has no clause to reject it — the rejection would have to come from CP-INV-001-by-analogy, which is non-normative. A small architecture clause ("every daemon-scoped registry MUST have exactly one owning subsystem; consumers read-only via a declared interface") would close this. Not a blocker for S02 v0.3, but a latent hole.

## Recommendations

**R1 (must).** Add one clause to AR-037 stating that cross-subsystem registries (policy, skills, schemas) are daemon-owned state for the purpose of the centralized-controller invariant. The clause closes Gap 4 and lets S02 cite architecture for the in-daemon pin rather than relying on CP-045 alone.

**R2 (should).** Extend AR-003 (skeleton/organ profile) to name "registration and lookup surfaces" alongside "daemon routing, dispatch, checkpoint emission, edge selection, validator execution." One word each. Closes Gap 2.

**R3 (should).** Add an informative note under AR-004 covering polymorphic types: a sum-type struct whose mode-field selects mechanism vs cognition carries the tag at the instance level, not the type level, provided each instance is tagged at construction. This matches how CP-003 is already written and closes Gap 3. Alternatively, require every sum-type variant to be declared as a separate named type — but that would require S02 to split Evaluator into two types, which bloats the schema.

**R4 (should).** Add an informative note under §4.10 naming configuration artifacts (policy YAML, skill manifests, `.harmonik/*` sidecars) as non-composition artifacts exempt from AR-051. Closes Gap 5 and forestalls a future reviewer arguing policy YAML is a fourth artifact.

**R5 (nice-to-have).** In AR-042, mark cross-subsystem sensors (sensors whose detection logic spans >1 subsystem envelope) as an acceptable-but-callable-out category. S02's CP-INV-002 and CP-INV-003 both depend on such sensors. Not a blocker.

**R6 (should).** Add a clause under §4.5 or §4.9 stating "every daemon-scoped registry MUST have exactly one owning subsystem; consumers read via a declared interface." This generalizes CP-047 / CP-INV-001 and closes Gap 6. One sentence.

**R7 (should).** Tighten AR-042's sensor enumeration to explicitly include "registration-time type-check" as a sensor kind. This is how CP-020 (cognition-Guard rejection) is enforced; currently it falls under the "MAY be a lint rule" umbrella, which is defensible but not obvious. One phrase.

**Priority ordering.** R1 > R3 > R4 > R6 > R2 > R7 > R5. R1 and R3 are the two that force me to invent local conventions today. The rest are latent-hole fixes.

**Blocking-or-not analysis.** None of R1–R7 blocks S02 v0.3 drafting. R1 and R3 would change S02's final spec text by one to three sentences each (citation of an AR clause instead of inventing local language). R4 and R6 are pure-clarification gains. R2 and R7 are one-word edits. R5 is style. If architecture v0.3 lands with R1 + R3 + R4 + R6 before S02 v0.3 finalizes, S02 is clean; if architecture v0.3 is delayed, S02 v0.3 lands with six forward-reference footnotes pointing at a future architecture amendment. Either path works.

## Affirmations

**What is working.** The Round-1 → Round-2 diff is directly useful to S02:

- **AR-052 + AR-053 + §A.1 exemplar** let me author S02's §4.0 envelope mechanically. The envelope slot was ambiguous in v0.1; it is unambiguous in v0.2.
- **AR-007 re-tagged as mechanism** is correct for S02's use case: the cognition-tagged-evaluator delegation-path obligation is a spec-authoring obligation on the ControlPoint author, not a runtime cognition event in S02's code.
- **Retiring AR-008 + moving the anti-pattern list to the AR-007 EXAMPLE** cleans up what was formerly a duplicate of AR-006 + AR-INV-001. The retired ID (AR-008) is appropriately marked as never-reused.
- **AR-027's agent_type rename obligation** does not bind S02 directly (S02 does not reference agent_type in its own types), but it stabilizes cross-subsystem field names that S02 lookup paths touch via policy documents (freedom-profile's `role` field, which cites architecture.md §4.8 roles).
- **AR-029 fixed** to state verification as a role-function over `{agentic, non-agentic}` nodes aligns with S02's Gate subtype `quality-gate`, which references a prior verification-node's outcome via `verification_ref`. S02 now has a single source for the enum shape.
- **AR-013's explicit scoping to runtime-subsystem specs** via AR-052 means S02 authors know they must declare an envelope; reviewer cannot claim "S02 is cross-cutting so skip the envelope."

**The meta-rules hold together.** Architecture gives me enough to author S02 spec without inventing shared vocabulary. Every cross-cutting term S02 uses — `llm-freedom`, `mechanism`, `cognition`, `verification-node`, `role`, `agent_type`, `workflow graph`, `bead`, `spec` — traces back to an architecture requirement. I do not need to invent or redefine any of these.

**Centralized-controller posture is citable.** AR-037 + AR-038 + AR-041 + AR-044 together give me the architectural posture S02 inherits: S02 is in-daemon, registry is in-memory, no agent-to-agent coordination via policy registries (agents consult via handler-contract, not directly), and every policy artifact is repo-backed. I can write S02 spec §4.9 citing these and not having to re-argue the position.

**Three-artifact separation supports S02 cleanly.** CP-036 (DOT references YAML by name, never inline) is a direct application of AR-046–AR-051. The architectural pressure to keep DOT and YAML separate artifacts is what makes S02's registration path coherent: a workflow graph ingested by the execution-model does not embed policy bodies; it references S02 registry names. This is a genuine architectural win and S02 depends on it.

**Amendment protocol usable.** AR-020–AR-023 (amendment protocol with `touches:` front-matter list) gives me a clean path to propose the gaps above as amendments without blocking S02 v0.3 drafting. S02 can land with a compatibility note flagging the gaps; the amendments can process in parallel under AR-023's serialization rule. The `touches:` contract is non-optional for multi-amendment work, which is exactly what Gap 1 through Gap 6 would generate if all six were filed at once — AR-023 prevents accidental overlap-by-stepping-on-each-other.

**Retired-ID discipline is visible.** The §10.1 Core MVH language explicitly lists retired IDs (AR-008, AR-INV-002, AR-INV-004–006) with "never reused" — this matters for S02's citation discipline. S02 cannot accidentally cite a retired ID; reviewers can mechanically flag any AR-NNN citation that lands on a retired slot. The template-level discipline (template §5 selection test mentioned in §5 INFORMATIVE) is also visible and makes S02's own invariant-selection reasoning easier: when I draft S02 v0.3, I know CP-INV-001/002/003 were the three that survived the selection test and any new S02 invariant needs to pass the same test.

**Purpose and scope are tight.** §1 Purpose names the ten items architecture covers; §2.2 Out-of-scope explicitly disclaims internal subsystem behavior, event-model ownership, execution-model ownership, handler-contract ownership, control-points (sic — §2.2 names control-points.md as owner of permission schemas, which is correct). There is no scope creep. S02 spec author knows what to cite and what to own.

**Open-questions surface the right things.** OQ-AR-003 (amendment overlap detector tooling locus) is the one S02 cares about most; a scripted check is the default, and S02's registration path can be validated by that same tool if it ships. OQ-AR-001 (testing.md migration) is shared with every spec. OQ-AR-002 (agent-type namespace) does not touch S02 directly. OQ-AR-004 (decentralization-pivot triggers) is far-future. The open-questions surface is well-calibrated.

**Overall.** Architecture v0.2 is a strong foundation for S02. The six gaps above are all small-to-medium informative-note additions; none requires normative change. I can author S02 v0.3 spec and Go package immediately against v0.2 with a compatibility note flagging the six gaps as candidates for a foundation amendment (AR-020 path) if they prove to generate review friction downstream. The Round-2 diff is a net clarity improvement and I do not see any regression from Round-1. If all six R-items land in a future architecture v0.3, S02 authoring becomes mechanical; as of v0.2 it is strongly guided but requires a small number of judgment calls from the author.

## Summary matrix

| Stress question | Answer | Blocker? | Recommendation |
|---|---|---|---|
| Q1: envelope authorable from AR? | Yes (with judgment on element (f)) | No | None — §A.1 exemplar is strong |
| Q2: ZFC decision rule clear? | Yes for instances, no for polymorphic types | No | R3 (sum-type tagging note) |
| Q3: four-axis tagging mechanical? | Yes for baseline; judgment on non-idempotent-with-idempotent-reregistration | No | None required |
| Q4: central registry from AR? | No — inferred via CP-045/047 | No (but tightening desired) | R1 (AR-037 policy-state clause), R6 (single-owner rule) |
| Q5: three-artifact reasoning clean? | Yes; policy YAML unclassified | No | R4 (configuration-artifact note) |

All five answers are "yes / mostly yes with named gap." No answer is "no" or "blocked." This is the shape of a foundation spec that is doing its job.

## Closing note

Writing S02 Policy Engine against architecture v0.2 is a pleasant experience. The meta-rules carry enough weight that S02 spec text can cite them directly and enough flexibility that the six gaps can be resolved by local judgment without waiting for foundation revision. The round-1-to-round-2 improvement (particularly AR-052/053 and §A.1) is material. I am green-lighting S02 v0.3 drafting against architecture v0.2, with the six gap items captured as OQ-style forward references for an eventual architecture v0.3 pass.
