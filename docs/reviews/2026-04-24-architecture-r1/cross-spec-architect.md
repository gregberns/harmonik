# Architecture R1 — Cross-Spec Architect Review

**Reviewer lens.** Architecture is the root spec every other depends on. The review asks whether architecture.md declares the right meta-rules the other nine specs will actually use, whether anything here belongs elsewhere, and whether downstream specs can cite this spec without ambiguity.

**Scope of cross-check.** Read: architecture.md (full). Browsed: execution-model.md, event-model.md, handler-contract.md, control-points.md, reconciliation/spec.md, beads-integration.md, workspace-model.md, process-lifecycle.md, operator-nfr.md.

## Verdict

**Changes required before foundation-version bump.** The spec is substantively correct: the meta-rules it declares (four-axis, ZFC, envelope, amendment protocol, agent-type identifier, three-artifact separation, centralized-controller) match what the nine downstream specs assume and cite. However, **citation anchors are broken across the entire corpus**, **one identifier contract is already violated by a downstream spec (AR-027)**, and **one meta-rule (verification-node as a first-class node type, AR-011/AR-029) does not match what execution-model declares**. These are correctness issues for the foundation layer, not just editorial polish. Until they are resolved, downstream specs cannot be soundly re-reviewed for conformance.

## Consistency checks (six)

### 1. Four-axis + ZFC tagging usage

The tagging convention is applied consistently across downstream specs: every requirement in event-model.md, execution-model.md, control-points.md, handler-contract.md, operator-nfr.md, process-lifecycle.md carries a `Tags:` line with `mechanism` or `cognition`, and the baseline-omit rule of AR-002 is honored (most requirements omit `Axes:` and deviators declare full tuples). Spot-checking event-model shows two dozen `Tags: mechanism` entries with `Axes:` lines appearing only where `idempotency=non-idempotent` or `io-determinism=best-effort` deviate from baseline — the pattern architecture wants. Execution-model EM-011 correctly requires every node to carry four-axis tags matched against its `idempotency_class`, pinning the axis → enum correspondence at the node layer.

**AR-003 (skeleton vs organ profile) is used correctly.** Process-lifecycle PL-020 pins the daemon as `llm-freedom=none`; handler-contract scopes cognition to subprocesses. No daemon-side `cognition`-tagged requirement appeared in any spec. AR-INV-001 is holding.

**One ambiguity downstream specs work around rather than resolve.** AR-002 says "reviewers infer baseline from absence," which creates a silent-default rule that is harder to lint than an explicit-always rule. Downstream tooling (the corpus lint named in §10.2 AR-001) will have to distinguish "author omitted because baseline" from "author forgot" — the spec offers no machine-readable signal. Consider adding either (a) a `Baseline` marker as an explicit opt-in for baseline, or (b) a rule that the lint accepts absence only when the requirement contains no I/O-, mutation-, or LLM-language — a keyword-check the ZFC principle would otherwise forbid. Neither is urgent; the rule works for careful authors.

**AR-007 (cognition delegation-path obligation) is cited and honored.** Control-points §4.8 requires cognition-tagged evaluators name role + model class + input shape + response schema; reconciliation §9.4b names the investigator-agent's delegation path; workspace-model §5.9 names the conflict-resolution delegation path (role=ORIGINAL IMPLEMENTER, model-class from handler class, input shape). AR-007 is doing real work in the corpus.

### 2. Subsystem envelope (§4.4, §4.5)

**Gap: no downstream spec actually declares its envelope in the eight-element shape of AR-013.** Searching for "envelope" across the nine sibling specs returns references to the event envelope (a different concept), to `internal/daemon` as composition root (process-lifecycle PL-020), and to unstructured prose — but no spec carries an "Envelope" section listing (a) events produced, (b) events consumed, (c) cross-payload types, (d) handlers implemented, (e) state owned, (f) control points provided, (g) NFRs inherited/overridden, (h) boundary classification.

Each spec publishes *some* of these elements scattered across its sections (execution-model §4.1 declares types, §4.3 declares emission obligations, handler-contract §4.2.HC-007 lists emitted event types, control-points §4.9 covers registration), but no single Envelope section collects them, and no two specs publish them in the same shape. The eight-tuple AR-013 requires exists only in architecture.md itself.

This is a review-finish obligation on every downstream spec, not a bug in architecture. But architecture should make that obligation unmissable: either (a) point at a canonical envelope template location (analogous to `docs/foundation/spec-template.md` for the spec as a whole), or (b) declare the envelope section number (e.g., "subsystem specs MUST carry a §X.Y Envelope section"). Without a canonical locus, authors will drop the envelope into various places or skip it. AR-INV-004 ("subsystem envelope is the only declaration surface") is unenforceable as long as no envelope section actually appears.

**AR-016 (Go-package MVH pin) holds.** Process-lifecycle PL-020 makes `internal/daemon` the composition root; PL-022 prohibits ntm from being adopted as a subsystem-shape. Architecture's pin is echoed downstream correctly.

### 3. Centralized-controller principle (§4.9)

Every downstream spec assumes and cites this principle, and the citations are substantively correct (handler-contract §4.3 watcher/adapter split, workspace-model WM-INV-001 lease-by-run, process-lifecycle daemon-binary shape, execution-model §4.10 daemon-owned cascade + EM-validation-is-a-daemon-submission-path, control-points the three owners S01/S02/S05, operator-nfr daemon-owned control state machine, reconciliation as a workflow-library entry per AR-018). AR-037 is strong enough for those citations.

AR-038 (explicit inverse of Gas Town) is reinforced by workspace-model's lease-by-run (vs. lease-by-agent) and by process-lifecycle PL-022 (rejection of ntm's Agent Mail file reservations). AR-039 (merge-in-run-worktree) is echoed by workspace-model's single-lease invariant.

**Minor scope question.** AR-040 (acknowledged tradeoff) names two trigger scenarios for decentralization pivot (remote-daemon, multi-operator-single-daemon). OQ-AR-004 captures the gap. Acceptable as-is; downstream specs don't need to re-state the tradeoff.

### 4. Three-artifact separation (§4.10)

Three-artifact usage across downstream specs is internally consistent: execution-model EM-001 calls the DOT representation the workflow artifact, beads-integration names beads as the claimable unit, specs are self-evidently the spec artifact. No downstream spec treats one as a projection of another, and no spec introduces a "feature" primitive. AR-051 holds across the corpus — grep for "feature" as a primitive returns only architecture's own exclusion prose.

Control-points CP-036 invokes the three-artifact separation when prohibiting inline policy bodies in DOT attributes ("DOT is the graph; YAML is the policy; each is a separate artifact"). This is a productive use of the rule: it rejects a real design temptation (inline policy) on architectural grounds. The rule is not merely declarative.

**Minor:** AR-050 (many-to-many, no projection) lists join-keys (`Harmonik-Run-ID` trailer, `run_id` event field, optional `Harmonik-Bead-ID`). These are execution-model's contract; architecture cites [docs/foundation/components.md §10] for the bead side. Downstream execution-model declares the trailers at §4.4.EM-017. Consistent.

**Minor:** AR-049's definition of bead is terse. Beads-integration §4.3 elaborates with the parent-child edge shape and the claim-state machine. Architecture correctly stops at the compositional claim and defers structure to beads-integration. Acceptable.

### 5. Agent-type identifier shape (§4.7.AR-025)

AR-025 (regex `^[a-z][a-z0-9-]{1,62}$`, reserved identifiers `claude-code` / `pi` / `claude-twin` / `pi-twin`, daemon-scoped) is the only data shape architecture owns, and it is correctly self-contained in §6.1. AR-028 (adding a new agent type is a subsystem-envelope exercise, not a foundation revision) is honored in handler-contract's "future-shape" requirements.

**Violation: `handler_type` vs. `agent_type` cross-surface drift.** AR-027 requires four surfaces use `agent_type` byte-for-byte identically: YAML policies, DOT attributes, `LaunchSpec.agent_type`, and event payloads naming an agent. Event-model.md §8.3.2 declares the `agent_started` payload carrying `handler_type` (not `agent_type`); §8.3.8 declares `session_log_location` carrying `handler_type`. Handler-contract §4.2.HC-008 also writes `handler_type` into the `session_log_location` event. Workspace-model §5.3a writes `handler_type` into the `harmonik.meta.json` sidecar. LaunchSpec (handler-contract §6.1) correctly uses `agent_type`. Control-points §4.6 uses role names for the YAML side, which is orthogonal (AR-026); `agent_type` appears in freedom-profile + role-assignment YAML via policy layer references, and there it uses the `agent_type` form.

Either architecture should declare `handler_type` and `agent_type` as synonyms with a single canonical form (with one deprecated), or downstream specs should rename `handler_type` → `agent_type` in those payloads. The current state is a direct AR-027 violation that a corpus lint would immediately flag.

### 6. Amendment protocol (§4.6)

AR-020 says "the revision MUST trigger re-review of every subsystem spec that cited the affected foundation spec." This is sufficient intent but lacks a mechanical trigger. OQ-AR-003 raises the overlap-detector tooling locus; there is no analogous open question for where the re-review triggering is tracked. Consider adding a requirement that every foundation-spec revision history row in §12 list the section numbers changed, so the re-review scope is machine-extractable. Without this, "every subsystem spec that cited" is unverifiable.

AR-022 requires each subsystem spec cite the foundation version it conforms to in front-matter or §9.1 Depends-on. Checked: every downstream spec's `depends-on:` names `architecture` but does not pin a version. Front-matter lists `version: 0.1.0` for the downstream spec itself, never the architecture-version it conforms against. Either this requirement should be softened (version is implicitly "current") or every downstream spec needs a `foundation-version: 0.1.0` line.

AR-023 (parallel-amendment serialization via mechanical overlap detection) is well-specified and is the only place in the foundation where a concrete tooling obligation lands. OQ-AR-003 defers the locus; that is acceptable.

**AR-020 vs. AR-015 interaction.** AR-015 (add-a-subsystem procedure) forbids foundation revision for adding a subsystem but requires amendment if a gap is discovered. The flowchart is: write subsystem spec → gap detected → amendment via AR-020 → revision → re-review all dependents. This is coherent but presumes gaps will be noticed. A downstream author working under time pressure can silently work around a missing foundation rule; the amendment is the right-thing path, not the enforced one. Consider whether review personas should flag "spec does not cite a foundation rule at point X where one might be expected" as a positive obligation.

## Required triple (§4.3) cross-check

AR-009 through AR-012 declare search + verifier + traces as load-bearing in every deployment. Corpus cross-check:

- **Search** (AR-010): execution-model EM-044 declares backtracking with four transition kinds (`local-patchback`, `architectural-rollback`, `policy-rollback`, `context-restore`). Byte-identical to architecture's enumeration. EM-006 candidate-generation node-type `agentic`. Control-points §4.6 freedom profiles per state. All three search surfaces satisfied.
- **Verifier** (AR-011): partially satisfied — see the §5 "types/rules" section above. Execution-model does not have a `verification-node` node type in its enum, though the concept is realized through `agentic` nodes with `actor_role=Reviewer`. Fix AR-011 prose.
- **Traces** (AR-012): execution-model EM-004 declares `Transition` with the full AlphaGo field set (`candidate_actions`, `chosen_action`, `policy_version`, `evidence`, `verifier_metrics`, `confidence`). EM-018 pins the sibling-file storage at `.harmonik/transitions/<transition_id>.json`. EM-005 + §4.4 make transitions distinct from events. AR-012 is fully satisfied.

AR-INV-003 ("search + verifier + traces are required, not optional") is verifiable by the above; the only gap is the AR-011 language, not the substance.

## Types/rules to add, move, or fix

### Citation-anchor drift (blocking)

**Every downstream spec cites architecture as `§1.1`, `§1.2`, `§1.3`, `§1.4`, `§1.4a`, `§1.5`, `§1.6`, `§1.6a`, `§1.8`, `§1.9`.** Architecture's actual section numbers are `§4.1` through `§4.10`. Architecture's own §1 (line 22) even refers to itself as "§1.1, §1.2, §1.4, §1.8, and §1.9" in the Purpose prose. Every citation is wrong.

Counts: event-model 3, handler-contract 9, execution-model 13, control-points 27, reconciliation 6, operator-nfr 8, process-lifecycle 6, beads-integration 1, workspace-model 0 direct (uses `docs/foundation/components.md §1.N` throughout). Total >70 broken citations.

**Resolution options** — pick one, apply corpus-wide:
- (a) Renumber architecture.md to use §1.1–§1.10 for the ten meta-rule groupings, matching how downstream specs already cite it. Lowest-friction.
- (b) Keep architecture §4.1–§4.10 and fix every downstream citation to use the §4.N form, plus drop `§1.4a` / `§1.6a` (which do not exist in architecture) and replace with explicit AR-NNN requirement IDs.

Option (a) preserves downstream citations; option (b) is more mechanically correct. Either is acceptable; both require coordinated corpus edits. This is the single largest blocker for foundation-version bump.

### `§1.4a` and `§1.6a` do not exist

Multiple specs cite `[architecture.md §1.4a]` (event-model EV-001, handler-contract) for the Go-package-identifier subsystem shape, and `[architecture.md §1.6a]` for the agent-type identifier shape. Neither sub-heading exists in architecture.md. Under option (a) above, add §1.4a for the subsystem-identifier shape (currently implicit in §4.5.AR-016) and §1.6a for the agent-type identifier shape (currently §4.7.AR-025). These are the most-cited anchors; they should be explicit sub-sections.

### Verification-node is not in the execution-model node type enum

AR-011 and AR-029 pin `verification-node` as "a node type in the workflow graph." Execution-model EM-006 declares the node-type enum as `{agentic, non-agentic, gate, control-point, sub-workflow}` — no `verification-node`. Either:
- Architecture relaxes AR-029 to say "a role-function of `agentic` or `non-agentic` nodes that delegate to a reviewer or run deterministic checks" (matches execution-model's current shape and the existing `reviewer` role default at EM-010).
- Or execution-model adds `verification-node` to the node-type enum.

The first option is more faithful to the system's actual shape (an agentic node with `actor_role=Reviewer` is already a verification node). AR-029 overreaches by pinning node-type membership. The distinction architecture wants to preserve (verification-not-a-subsystem, verification-as-a-graph-level concept) is fully satisfied by option 1.

AR-030 (`verification-result` shape: status ∈ `{SUCCESS, FAIL, PARTIAL_SUCCESS}`) correctly cites the execution-model outcome shape, which declares `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}` at EM-005. AR-030 excludes `RETRY` implicitly — this is correct (a verification node returning "retry" is a category error). Make the exclusion explicit in AR-030, otherwise downstream implementors will wonder whether `RETRY` is admissible for verifiers.

AR-031 (`quality-gate` reads a prior `verification-result`) matches control-points §4.2 CP-gate-subtypes, which includes `quality-gate` with a required `verification_ref` field. This three-way coupling (AR-031 ↔ CP gate-subtype ↔ verification-node output) is the canonical case of architecture's meta-rules doing productive work.

### Handler-emitted events are listed but not pinned to the agent-type surface

AR-027 cites "event payloads naming an agent (e.g., `agent_started`)." The event surface in event-model §8.3 uses `handler_type`. Consolidate the naming before asking downstream specs to enforce byte-identity. Suggested resolution: architecture adds a glossary note that `agent_type` is the canonical identifier and `handler_type` (legacy) MUST be renamed in event-model §8.3.2, event-model §8.3.8, handler-contract HC-008, workspace-model §5.3a, and workspace-model `harmonik.meta.json` sidecar in the next revision cycle.

### Role taxonomy citation consistency

AR-032 names seven roles (Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor). AR-033 splits these into MVH-required (Planner, Builder, Reviewer) and deferred (Researcher, Verifier, Scheduler, Governor). Control-points §4.6.CP-029 and CP-030 correctly cite this split. Execution-model's idempotency-class defaults (EM-010) name `reviewer, researcher, lint, test, typecheck, analysis` as idempotent-by-default and `builder, merge` as non-idempotent-by-default; these are role-like strings not tied to AR-032's canonical case. Lowercase `reviewer` in EM-010 is acceptable (it's the node-type idempotency-default key, not a role-name invocation), but a corpus lint could produce false positives against AR-032's "Planner, Builder, Reviewer" capitalization without a style guide note. Architecture should pin the capitalization convention (Title Case for role-as-noun; lowercase for node-type-idempotency-default-key).

### Baseline axis default opt-out needs a signal

See §1 above. Not blocking, but the lint described in §10.2 will have false negatives unless explicit-baseline is distinguishable from omitted-by-accident.

### Envelope section locus

See §2 above. Pick a canonical `§N` number for the Envelope section and require downstream specs to include it, or add an envelope-template doc alongside `spec-template.md`.

### AR-INV-002 needs a machine-readable locus

AR-INV-002 says "every cross-subsystem surface is fully tagged." "Cross-subsystem surface" is not defined in the glossary; it is used in prose across AR-001, AR-004, AR-013. A reviewer asked to enforce AR-INV-002 must decide what counts: does a type used only in one envelope's "state owned" slot count? Does a type used only as an intermediate in a multi-hop payload count? Consider adding an AR-013a sub-requirement that says: "a type is cross-subsystem iff it appears in any (a) event payload declared in any envelope, (b) handler interface declared in any envelope, (c) state type declared in any envelope and named in another envelope's reads-or-mutates list." That closes the definitional gap.

### Nothing belongs in another spec

Reviewing what architecture declares against what the other nine own: nothing in architecture reads as "should have been in X spec." The meta-rules are meta-rules. The agent-type identifier regex (§6.1) could conceivably live in handler-contract, but handler-contract already cites architecture for it, and since the same identifier is referenced by control-points (YAML), execution-model (DOT), and event-model (payloads), architecture is the correct home — no single downstream spec subsumes the contract.

The Rationale appendix (§A.3) could be trimmed if spec-corpus aesthetic prefers short appendices, but the content is load-bearing for "why this, not that" questions future readers will have.

## Recommendations (ranked)

1. **Resolve the citation-anchor drift.** Either renumber architecture to §1.N (matching existing downstream citations) or corpus-edit every downstream citation to §4.N and AR-NNN. This is blocking; nothing else can be soundly re-reviewed until anchors are correct. Architecture's own line 22 self-cites §1.1, §1.2, §1.4, §1.8, §1.9 — this is evidence the author intended §1.N numbering; renumbering to match is lower-friction than corpus-wide downstream edits.
2. **Add explicit §1.4a (or §4.5a) and §1.6a (or §4.7a) sub-sections** for the subsystem-identifier shape (Go-package-identifier string, per the informal §4.5 pin) and the agent-type identifier shape (regex, currently in §6.1). These are the two most-cited anchors; they should be addressable without scanning a full section body. Event-model EV-001, handler-contract, workspace-model all cite these sub-anchors today; currently they resolve to 404.
3. **Resolve `handler_type` vs. `agent_type`.** Either unify in architecture's glossary with an explicit "these are the same field" rule, or corpus-rename `handler_type` → `agent_type` across event-model, handler-contract, and workspace-model. AR-027 currently fails its own byte-identity requirement. Preferred resolution: rename `handler_type` → `agent_type` corpus-wide in the next revision cycle; architecture adds a transitional note.
4. **Relax AR-029 `verification-node` language.** Pinning it as a separate node-type contradicts execution-model EM-006's enum. Restate as a role-function over the existing `{agentic, non-agentic}` set. AR-011 ("verifier as a node type") remains fine once "node type" is understood as "node role/purpose" rather than "member of the node-type enum." Fix AR-011 prose in parallel.
5. **Add an Envelope section locus requirement.** Either name the section number every subsystem spec must use, or point at an envelope-template doc. Without this, AR-INV-004 is unenforceable.
6. **Clarify amendment re-review triggering (AR-020).** Require revision-history rows to enumerate the section numbers changed so the re-review scope is machine-extractable. Add a companion requirement: every subsystem spec front-matter carries a `foundation-version:` field so conformance-against-version is queryable.
7. **Define "cross-subsystem surface" precisely.** See the AR-INV-002 locus note above. A one-line definition in the glossary closes the enforcement gap.
8. **Capitalization style pin for role names.** Title Case for role-as-noun (AR-032 canonical), lowercase for node-type-idempotency-default keys (EM-010). Mention explicitly.
9. **Consider an explicit baseline marker for four-axis tagging.** Optional; improves lint correctness.
10. **Clarify AR-030 `RETRY` status exclusion for verification-result.** Add: "`verification-result.status` ∈ `{SUCCESS, FAIL, PARTIAL_SUCCESS}`; `RETRY` is NOT admissible — a verification node that would return `RETRY` MUST fail-hard as `ErrDeterministic` instead."

None of these are design reversals; all are editorial tightenings that make architecture's meta-rules operable for downstream specs. The substantive choices (centralized controller, ZFC, three artifacts, no "feature" primitive, agent-type-as-handler-conformance-class, verification-not-a-subsystem, subsystem-as-Go-package, acknowledged decentralization tradeoff) are sound and match how the corpus actually reads.

## Harness-engineering invariants (§4.9 subset) cross-check

- **AR-041 (repo as single source of truth):** Honored. Every spec cites on-disk artifacts only; no out-of-band documentation is load-bearing for conformance. Kerf process artifacts are under `.kerf/` gitignore but specs themselves are in `specs/`.
- **AR-042 (guides and sensors pair):** Architecture itself declares test-surface obligations in §10.2 (corpus-lint for tagging, presence tests for search/verifier/traces, review-agent scenario tests for centralized-controller). This is a guides+sensors stance for the meta-rules. Downstream specs mostly have tests declared, though operator-nfr §10 is the thinnest.
- **AR-043 (constrain to empower):** Declarative — no way to verify compliance except by rejecting proposals that ask for exemptions. A review-persona obligation, captured at AR-020.
- **AR-044 (filesystem-backed coordination):** Workspace-model §5.3a and handler-contract §4.9 pin session-log files as the coordination surface; execution-model pins sibling transition files; event-model pins JSONL. AR-044 is satisfied.
- **AR-045 (quality-left ordering):** Not cited by any downstream spec as an enforcement point. Either this should have a concrete hook (a gate policy that orders fast deterministic checks before cognitive ones) declared in control-points, or the rule is merely advisory.

AR-045 stands out as the one harness-engineering invariant with no downstream pin. It reads as advice more than contract. Either give it teeth (control-points requires an ordering attribute on policy bundles, or execution-model's edge-cascade consults a deterministic-first ordering), or reclassify it as informative.

## Out-of-scope observations

- Architecture.md is ~46KB / ~565 lines, at the upper end of reasonable for a root spec. No split recommendation; the rules belong together because they co-constrain each other.
- The Rationale appendix (§A.3) is well-targeted at the load-bearing choices (why Go-package pin, why centralized-controller tradeoff explicit, why "feature" excluded, why three hyphenated verification forms). Keep.
- The §2.2 Out-of-scope deferrals (event schemas, workflow semantics, handler interface, permission schemas, process geometry, reconciliation taxonomy, Beads pin, operator NFRs) correctly delegate to the nine sibling specs. Every deferral has a downstream owner and a citation. Clean.
- §9.3 Co-references are entirely to `docs/foundation/components.md` sub-sections. Most of those specs now exist as standalone files under `specs/`. The "INFORMATIVE: bootstrap-only" note promises migration within one revision cycle. This is effectively the same citation-drift problem as the main recommendation, at the co-reference layer. Fix alongside #1.
- Conformance profile §10.1 ("Core MVH covers AR-001 — AR-051 and AR-INV-001 — AR-INV-006; no requirement is deferred") is appropriately strict. Meta-rules cannot be meaningfully deferred.
- §10.2 test obligations are in prose; OQ-AR-001 tracks migration to `testing.md §<layer>` once that spec lands. Acceptable for MVH.
- §11 open questions are all scoped to post-MVH concerns (namespace discipline, overlap-detector locus, decentralization triggers) except OQ-AR-001 which is a bootstrap-migration. No open question should block foundation-version 0.1.0.
- Glossary (§3) uses bolded terms followed by em-dash definitions, matching the spec-template convention. Cross-check: every term bolded in §3 appears at least once in the normative body. No orphan glossary entries. Clean.
- The terms `verification-node`, `verification-result`, `quality-gate` are all hyphenated forms per the explicit disambiguation in AR-031. Downstream control-points consistently uses the hyphenated `quality-gate`. Downstream execution-model uses the unhyphenated `verification node` in prose at §4.9 (reachability validation) and spawns reviewer-tier nodes at EM-010. Both forms refer to the same concept, but architecture pins the hyphenated canonical. A style lint should flag "verification node" (unhyphenated, two words) in future corpus revisions.

## Summary table of findings

| # | Finding | Severity | Fix locus |
|---|---|---|---|
| 1 | Citation anchors (§1.N) don't match architecture's section numbers (§4.N) | Blocking | architecture renumber or corpus-wide citation edit |
| 2 | `§1.4a` and `§1.6a` sub-anchors don't exist | Blocking | architecture add sub-sections |
| 3 | `handler_type` vs `agent_type` cross-surface drift | AR-027 violation | corpus rename to `agent_type` |
| 4 | AR-029 pins `verification-node` as node-type; execution-model enum has no such type | Contradiction | architecture relax to role-function |
| 5 | No downstream spec declares eight-element envelope in AR-013 shape | AR-INV-004 gap | architecture pin envelope section locus |
| 6 | AR-020 re-review trigger has no machine-readable hook | Operability gap | revision-history section-number list |
| 7 | "Cross-subsystem surface" undefined | AR-INV-002 operability gap | glossary entry |
| 8 | AR-030 `RETRY` status admissibility ambiguous | Minor | explicit exclusion |
| 9 | AR-045 quality-left has no downstream enforcement | Advisory-vs-normative unclear | control-points pin or reclassify |
| 10 | Baseline-axis-omission lint signal absent | Tool correctness | explicit baseline marker |

The first four are blocking for any corpus-wide spec-version bump. The remaining six are editorial tightenings that can land in a follow-up revision without re-reviewing every dependent spec.
