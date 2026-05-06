# S07 Scenario Harness v0.1 — Cross-Spec Architect R1 Review

**Date:** 2026-05-05
**Reviewer:** cross-spec-architect
**Scope:** specs/scenario-harness.md against 10-spec corpus.

## Summary

S07 fits the corpus well as a consumer-only spec. The author has correctly framed the harness as a layer that drives the production daemon stack via two surface mutations (handler-config override, fixture-path override) and explicitly forbids test-mode branches in production-imported packages — this aligns cleanly with HC-INV-002 and is reinforced by SH-INV-001. Twin substitution funnels through HC-003. Event-log capture cites EV-021 / §6.1 / §6.2 correctly. Failure-class taxonomy in §8 names EM analogs where they exist. The spec does not invent a parallel event taxonomy, a parallel workspace primitive, or a parallel handler interface; ownership boundaries are respected throughout.

The highest-priority cross-spec concerns are (1) one broken cite at the heart of the conformance suite (`[workspace-model.md §4.5.WM-007]` — WM-007 is in §4.2, not §4.5; the merge contract the cite *intends* lives at WM-018/WM-019); (2) a likely missed cite / latent ambiguity around HC-043's commit-hash check for twin binaries (SH-009 lets scenarios declare absolute paths to twins but does not say the daemon's normal HC-043/HC-045 hash check still applies — readers cannot tell whether scenarios bypass the check or merely source the path); and (3) a normative cite to operator-nfr §4.5 inside §6.3 (a forward MUST about N-1 readability) without operator-nfr being in `depends-on`.

The depends-on list is otherwise minimal-and-honest: every declared dep is used multiple times. One additional declaration (operator-nfr) is recommended; the BI scope-out cite is informative-only and need not pull BI into depends-on.

## Cite audit

Every cross-spec cite walked top-to-bottom. Citations resolved against current corpus.

| # | Cite (verbatim) | Where | Target resolves? | Anchor location | Status |
|---|---|---|---|---|---|
| 1 | `[docs/foundation/components.md §10]` | §1 | yes (BI section) | components.md §"Component 10" = BI | weak-cite — §10 of components.md is BI, not SH; §1's prose asserts "the harness is the regression net for self-build cycles" which is not §10 BI's content. The bootstrap parenthetical acknowledges this is provisional, but the §10 anchor carries no SH-related substance. |
| 2 | `[handler-contract.md §4.8]` | §2.2 | yes | HC §4.8 Twin parity | OK |
| 3 | `[execution-model.md §4.6, §4.10]` | §2.2 | yes | EM §4.6 Outcome spine, §4.10 Edge selection | OK (informative scope-out) |
| 4 | `[handler-contract.md §4.8.HC-038]` | §2.2 | yes | HC §4.8 line 399 | OK |
| 5 | `[beads-integration.md §4.4]` | §2.2 | (not verified — informative scope-out) | — | depends-on-violation (informative only; BI not in `depends-on`). Acceptable per existing corpus practice (HC has similar non-declared informative cites). |
| 6 | `[docs/concepts/digital-twins.md]` | §2.2, §4.8 | yes | docs file exists | OK (informative) |
| 7 | `[docs/goals/end-to-end-testability.md]` | §2.2, §9.3 | yes | docs file exists | OK (informative) |
| 8 | `[handler-contract.md §4.1.HC-003]` | §3, §4.3 SH-008 | yes | HC §4.1 line 89 | OK |
| 9 | `[workspace-model.md §4.1, §4.3]` | §2.1 | yes | WM §4.1, §4.3 | OK (section-anchor; consider sharpening to WM-NNN cites for the specific obligations) |
| 10 | `[handler-contract.md §4.4.HC-018]` | §3, §4.7 SH-026, §9.1 | yes | HC §4.4 line 235 | OK |
| 11 | `[handler-contract.md §4.6.HC-026a]` | §4.8 SH-027, §9.1 | yes | HC §4.6 line 308 | OK |
| 12 | `[handler-contract.md §4.8 HC-035..HC-040]` | §9.1 | yes | HC §4.8 lines 379–417 | OK |
| 13 | `[handler-contract.md §4.10.HC-042, HC-045]` | §9.1 | yes | HC §4.10 lines 427, 453 | OK |
| 14 | `[handler-contract.md §4.8.HC-035]` | §4.3 SH-011, §5 SH-INV-003 | yes | HC §4.8 line 379 | OK |
| 15 | `[handler-contract.md §5.HC-INV-002]` | §4.3 SH-008, §9.1 | yes | HC §5 line 535 | OK |
| 16 | `[execution-model.md §4.1 EM-005]` | §4.6 SH-021, §9.1, §6.1 | yes | EM §4.1 line 103 | weak-cite — EM-005 declares `Outcome.status` (enum); the field name `outcome_status` is the Transition record's projection per EM §6.1. Not broken, but the SH §6.1 OutcomeExpectation could read more cleanly as "Outcome.status enum" or cite EM §6.1. |
| 17 | `[execution-model.md §8]` | §2.1 §8, §9.1 | yes | EM §8 line 942 | OK |
| 18 | `[execution-model.md §8.2 \`structural\`]` | §8.4 | yes | EM §8 row 8.2 = structural | OK |
| 19 | `[execution-model.md §8.4 \`canceled\`]` | §8.6 | yes | EM §8 row 8.4 = canceled | OK |
| 20 | `[event-model.md §4.5.EV-021]` | §4.6 SH-020, §9.1 | yes | EV §4.5 line 409 | OK |
| 21 | `[event-model.md §6.1 Event]` | §9.1 | yes | EV §6.1 line 577 | OK |
| 22 | `[event-model.md §6.2]` | §4.6 SH-024, §9.1 | yes | EV §6.2 line 631 | OK (carries the post-fsync-tail rule SH-024 leans on) |
| 23 | `[event-model.md §8]` | §6.1 EventExpectation, §9.1 | yes | EV §8 line 70 | OK |
| 24 | `[workspace-model.md §4.1.WM-001]` | §4.4 SH-012, §9.1 | yes | WM §4.1 line 157 | OK |
| 25 | `[workspace-model.md §4.2 branching model]` | §4.4 SH-012, §9.1 | yes | WM §4.2 | OK |
| 26 | `[workspace-model.md §4.1.WM-003]` | §6.1 (in spec body — verify) | yes | WM §4.1 line 172 | OK |
| 27 | `[workspace-model.md §4.3.WM-010]` | §4.4 SH-013, §9.1 | yes | WM §4.3 line 243 | OK |
| 28 | `[workspace-model.md §4.3.WM-013b]` | §4.4 SH-015, §9.1 | yes | WM §4.3 line 283 | OK |
| 29 | `[workspace-model.md §4.5.WM-007]` | §10.1 conformance-set: `smoke/checkpoint-and-merge.yaml` | **broken** | WM-007 is at WM §4.2 line 223; §4.5 holds WM-018..WM-021 | **BROKEN CITE** — see Findings/MAJOR M-1 below. |
| 30 | `[execution-model.md §4.4.EM-016]` | §10.1 conformance-set | yes | EM §4.4 line 214 | OK |
| 31 | `[handler-contract.md §4.6.HC-024]` | §10.1 conformance-set | yes | HC §4.6 line 281 | OK |
| 32 | `[handler-contract.md §4.9.HC-039]` | §10.1 conformance-set, §9.1 | yes | HC §4.9 line 407 | OK |
| 33 | `[process-lifecycle.md §4.2.PL-005]` | §4.5 SH-017, §9.1 | yes | PL §4.2 line 215 | OK |
| 34 | `[process-lifecycle.md §4.1.PL-001]` | §11 OQ-SH-002 | yes | PL §4.1 line 148 | OK (informative; OQ block) |
| 35 | `[handler-contract.md §6.1 LaunchSpec]` | §6.1 FixtureSetup | yes | HC §6.1 line 600 | OK |
| 36 | `[architecture.md §4.1]` | §9.1 | yes | AR §4.1 four-axis classification | OK |
| 37 | `[architecture.md §4.6]` | §8 INFORMATIVE, §10.1 conformance | yes | AR §4.6 amendment protocol | OK |
| 38 | `[architecture.md §4.9]` | §9.1 | yes | AR §4.9 centralized-controller (now AR-INV-007) | OK; consider tightening to `AR-INV-007` for precision (minor). |
| 39 | `[operator-nfr.md §4.5]` | §6.3 schema-evolution + §11 OQ-SH-007 | yes | ON §4.5 line 282 (compat window) | depends-on-violation — cited in normative §6.3 prose with a forward-MUST; ON not in `depends-on`. **MAJOR.** |
| 40 | `[docs/bootstrap.md §5 step 8]` | §9.3 | yes | bootstrap.md §5 row 8 | OK (informative) |
| 41 | `[docs/goals/bootstrapping-self-building.md]` | §9.3 | yes | docs file exists | OK (informative) |
| 42 | `[docs/subsystems/scenario-harness.md]` | §9.3, §11 OQ-SH-004 | yes | seed doc exists | OK (informative; superseded normatively per §9.3 framing) |

**Total:** 42 distinct cross-spec cites; 1 broken, 1 depends-on-violation in normative prose, 1 weak (informative-anchor §10 of components.md), 2 minor field-name / section-precision suggestions, 37 OK.

## Findings

The four MAJOR findings below collectively require ~5 line-level edits to fix; none requires re-architecting a section or reopening a locked-in decision. Two MAJOR findings (M-1 broken cite, M-3 depends-on miss) are mechanical fixes; one (M-2 hash-check binding) requires a clarifying sentence and a cite; one (M-4 §10 components.md anchor) requires swapping in the correct seed-doc cites already declared at §9.3.

### BLOCKER

None at the inter-spec level. (The WM-007 broken cite below is severe enough to argue for BLOCKER, but is recoverable by a single edit and does not invent a contract or invert ownership; classified MAJOR. The reviewer would not block S07 at the cross-spec gate.)

### MAJOR

**M-1. Broken cite at heart of conformance suite.** Affected: §10.1 conformance-set bullet `smoke/checkpoint-and-merge.yaml` (line 593 of scenario-harness.md). The spec cites `[workspace-model.md §4.5.WM-007]` for "merges the task branch." WM-007 is at WM §4.2 line 223 — the three-level branching model declaration ("(a) node commits land on the task branch... (b) the task branch squash-merges onto the integration branch at run-terminal-success per §4.5; (c) the integration branch merges to main") — not at §4.5. WM §4.5 (Merge back to integration) declares WM-018 (merge-back is performed by a node in the same worktree), WM-018a (merge-back node dispatch contract), WM-019 (task branch squash-merges with one commit per task), WM-019a (merge executes outside main worktree), WM-020 (squash-merge is non-fast-forward), WM-021 (merge outcome emits `workspace_merge_status` with `status=merged`).

  The citation as written is broken: §4.5.WM-007 does not exist. Two fixes are admissible:
  - Replace with `[workspace-model.md §4.5.WM-019]` if the intent was the squash-merge requirement (most likely given the surrounding prose: "merges the task branch... emits `workspace_merge_status{status=merged}`").
  - Replace with `[workspace-model.md §4.2.WM-007]` if the intent was to assert that the squashed-merge-onto-integration shape conforms to the three-level branching model.

  Recommended: **WM-019** is the operative requirement. WM-007 is a model statement, not a merge contract. The conformance scenario's assertion `event_present(workspace_merge_status{status=merged})` corresponds to WM-021 (which declares the emission); the action being verified ("merges the task branch") is WM-019. A defensible expanded cite is `[workspace-model.md §4.5.WM-019, WM-021]`. Without correction, every reader of S07 §10.1 hits a dead anchor on the most reviewer-visible element of the conformance floor — and any tooling that resolves cite-by-section against the corpus will flag this as a hard miss.

**M-2. Likely missed cite — twin commit-hash check.** Affected: SH-009, SH-INV-003 (twin-binary discovery + harness-only-runs-twins invariant). HC-045 ("Twin binaries obey the same launch rules") and HC-043 (commit-hash check for in-repo binaries) together require every twin binary to be launched from a configured repo-relative path with an embedded-commit-hash check pinned at workflow/policy configuration time. SH-009 declares two resolution mechanisms — `(a) absolute path declared in the scenario, or (b) name resolved against a configured twin-binary search-path prefix delivered to the harness at startup` — and SH-INV-003's pre-launch check verifies "every resolved binary path is a twin" but does not say the HC-043 hash check still applies. Two reads are possible:
  - **Funnel-through read:** the scenario's `agent_overrides` only sources the binary path; the daemon's HC-003 → HC-045 → HC-043 chain still validates the hash. SH-008's "applied to the workflow's resolved handler configuration at workflow-load time per HC-003" supports this read. Under this read SH-009 is consistent but the dependency is implicit.
  - **Bypass read:** the scenario file declares an absolute path the harness uses directly, bypassing HC-045's "pinned at workflow/policy configuration time" rule.

  Why it matters: HC-043's hash check is the bootstrap-time integrity gate that prevents a stale or substituted twin binary from running undetected. HC-045 says twins follow it. If S07 v0.1 implementations land under the bypass read, the conformance scenarios `smoke/twin-launch-and-ready.yaml` etc. could pass against a hash-mismatched twin binary, defeating the integrity gate inside the harness regression net (the very surface where regression detection lives).

  Recommended fix: SH-009 should add an explicit cite to HC-043 / HC-045 stating that the hash check applies unchanged, OR (if the author intends a scenario-mode carve-out) declare and cite the carve-out as an HC amendment with the corresponding OQ. The HC-026a heartbeat carve-out is the precedent for "scenario-mode" being an explicit carve-out rather than an implicit bypass — apply the same pattern here. A single sentence in SH-009 ("Resolved twin paths remain subject to the HC-043 commit-hash check unchanged.") closes the ambiguity.

**M-3. depends-on miss for operator-nfr in normative §6.3.** Affected: §6.3 schema-evolution. The spec body says: "Once the harness is mature enough... a `schema_version` field MUST be added with the N-1 readable contract per [operator-nfr.md §4.5]." This MUST is contingent on a future revision but is normative-prose-anchored to ON-018's compat window. ON is not in S07's `depends-on`. Fix one of:
  - Add `operator-nfr` to `depends-on`.
  - Reframe the §6.3 statement as informative (e.g., demote to a `> INFORMATIVE:` block and let OQ-SH-007 carry the normative load).
  Note: existing corpus practice (HC v0.3.3) cites operator-nfr in normative-adjacent prose without declaring it in `depends-on`; the HC pilot review (`docs/reviews/2026-04-30-hc-pilot-r1/references-r1.md` Finding #9) flagged this as an open question. SH should not perpetuate the pattern when a clean fix exists.

**M-4. Weak / misleading anchor on the §1 components.md cite.** Affected: §1 purpose paragraph. The spec asserts "per [docs/foundation/components.md §10]... the scenario harness is the regression net for self-build cycles." But components.md §10 is the Beads-integration component; it carries no statement about S07 or self-build regression. The bootstrap parenthetical acknowledges this is provisional ("migrates to component subsystem refs once finalized"). However, even as a bootstrap cite the §10 anchor doesn't carry the asserted substance. The correct sources for the asserted claim are `[docs/bootstrap.md §5 step 8]` (already cited at §9.3) and `[docs/goals/bootstrapping-self-building.md]` (already cited at §9.3). Fix: change the §1 cite to one of those two and drop the §10 anchor; or add a `[docs/subsystems/scenario-harness.md]` cite (the seed doc that actually carries the regression-net framing).

### MINOR

**m-1. Section-anchor cite where a finer cite is available.** Affected: §9.1 cite to `[architecture.md §4.9]` for the centralized-controller principle. The principle was promoted from AR-037 to AR-INV-007 at AR v0.3.0 (see AR §4.9 line 327). A `AR-INV-007` cite is more durable across future AR revisions and matches existing corpus practice.

**m-2. EM-005 cite carries a field-name slip.** Affected: SH-021, §6.1 OutcomeExpectation. EM-005 declares `Outcome.status` (enum: SUCCESS/FAIL/RETRY/PARTIAL_SUCCESS); the wire-name `outcome_status` is the Transition record's projection per EM §6.1 + EM-005a's explicit alias note. Reading SH-021 ("`outcome_status` per [execution-model.md §4.1 EM-005]") leaves the reader guessing whether the assertion is against the Outcome.status field, the Transition.outcome_status field, or the `outcome_emitted` event payload's `outcome_status` (EV §8.1.8). Recommend: clarify in the prose ("the orchestration drive's terminal Outcome.status enum (carried as `outcome_status` on `outcome_emitted` per EV §8.1.8)") or cite EM §6.1 Transition + EV §8.1.8 directly.

**m-3. SH-026 leans on operator-stop semantics without citing.** Affected: SH-026 ("cancel via the daemon's normal cancellation surface (operator-stop equivalent)"). HC-018 (cited) carries the cancel-bound contract; ON-009 ("`stop --immediate` is the only control that aborts in-flight runs") carries the operator-stop semantics the parenthetical invokes. Either drop the "(operator-stop equivalent)" parenthetical (the HC-018 cite suffices for the cancel bound) or add an ON cite. Combined with M-3, declaring `operator-nfr` in `depends-on` would cleanly authorize this.

**m-4. SH-002 bans `.yml`; not a cross-spec issue but worth flagging the absence of a corpus pattern.** Affected: SH-002. No other spec in the corpus pins file extensions as on-disk-shape contracts. This is a harness-local invariant (fine), but the pattern has no precedent in the 10-spec corpus, so reviewers may flag it as over-strict at the internal-quality review. Not a cross-spec finding.

**m-5. §10.1 conformance-set scenario `regression/twin-failure-classification.yaml` cites HC-024 directly but not HC-INV-006.** HC-INV-006 is the invariant that says exactly-one terminal event per session — which is what the `event_absent(outcome_emitted)` + `event_present(agent_failed)` pair is verifying. Adding the invariant cite makes the conformance scenario's role visible: it's not just a behavior test, it's the harness-visible enforcement surface for HC-INV-006. Optional.

**m-6. SH-INV-002 sensor obligation aligns with AR-042 but does not cite it.** AR-042 ("Invariants MUST name their sensor") is a corpus-wide obligation. SH-INV-001 through SH-INV-005 each name a sensor (corpus-grep, post-suite assertion, pre-launch check, nightly-rerun diff, suite-load lint), satisfying AR-042. The §5 invariants section need not cite AR-042 inline (the corpus convention is implicit), but a `> INFORMATIVE:` note that "all invariants name their sensor per AR-042" would close the loop. Optional.

## depends-on audit

- **Declared:** architecture, handler-contract, event-model, workspace-model, execution-model, process-lifecycle.
- **Used (cite count per dep):**
  - handler-contract: 22 cites — heavy use; correctly declared.
  - execution-model: 13 cites — heavy use; correctly declared.
  - workspace-model: 10 cites — heavy use; correctly declared.
  - event-model: 9 cites — heavy use; correctly declared.
  - architecture: 5 cites — moderate use; correctly declared.
  - process-lifecycle: 3 cites — light use (PL-005 normative; PL-001 in OQ); correctly declared.
- **Missing (cited in normative prose but not declared):**
  - **operator-nfr** — 1 normative-prose cite at §6.3 (M-3 above), 1 informative cite at §2.2, 1 OQ-block cite at OQ-SH-007. The §6.3 cite is the load-bearing miss.
- **Cited but informative-only (acceptable per corpus practice):**
  - beads-integration — 1 informative scope-out cite at §2.2.
- **Unused (declared but not cited):** none.
- **Final recommendation:** **add `operator-nfr` to `depends-on`** (single edit) OR reframe the §6.3 future-MUST as informative. Trim nothing. Keep BI out of depends-on (informative scope-out cite is acceptable).

## Reverse-coverage notes

S07 is a consumer-only spec; in principle no existing reviewed spec needs to add `depends-on: scenario-harness`. Verified by walking each existing spec's S07 references:

- **HC** — four references found:
  - §2.2 line 54 (scope-out): "Scenario harness and twin-conformance drift detection — owned by the `scenario-harness` (S07) spec, post-MVH." Declarative scope-out. Not a consumption.
  - §4.6 line 312 (HC-026a scenario-mode scripted carve-out): "scenario-harness false-positive resilience tests (per §10.2 HC-026 obligations) MUST use the wall-clock timer mode, not the scripted mode." HC declares the carve-out and tells S07 how to use it; S07 cites HC-026a in its own SH-027 / OQ-SH-005. The MUST belongs to HC's test obligations, not to S07. Not a reverse consumption.
  - §4.8 line 383 (twin-parity carve-out for unit-test fakes): "The twin-parity requirement applies ONLY to the canonical twin binary used as the real-handler substitute in scenario-harness tests and CI." Definitional clarification. Not a consumption.
  - §4.8 line 399 HC-038 (twin drift detection scoped to S07): forward-handoff statement. HC declares the parity contract; S07 owns the drift workflow post-MVH. Not a consumption — the dependency direction is S07 → HC, not the inverse.
  **HC does NOT need `depends-on: scenario-harness`.**
- **EM, EV, WM, AR, PL, ON, CP, RC, BI** — no normative-prose references to S07 found in spot-checks. No depends-on additions needed.

**No reverse-coverage edits required across the existing 10 specs.**

Side-note for the integrator: the SH §2.2 deferral of OQ-SH-008 ("twin-conformance drift detection... declared in scope of S07 by HC-038, deferred from this v0.1 draft to a post-MVH revision") is consistent with HC-038's "post-MVH" framing. No realignment needed. When the post-MVH drift-detection revision lands in S07 v0.2+, that revision MAY warrant a reciprocal HC edit (HC-038 could acquire a pointer at that point); but at v0.1 the asymmetry is correct.

## Registry consistency

`specs/_registry.yaml` line 26: `SH: {spec-id: scenario-harness, reserved: 2026-05-05, status: draft}`. SH spec front matter declares `spec-id: scenario-harness`, `requirement-prefix: SH`, `status: draft`. Match. **OK.**

## Cross-checks specific to this spec

The review method called for nine targeted cross-checks against the SH-specific inter-spec contracts. Walk:

**C-1. Twin-substitution mechanism via handler config (not runtime branch).** SH-008 declares the substitution as a mutation of the workflow's *resolved handler configuration at workflow-load time*, citing HC-003. HC-003 itself specifies that handler selection "MUST be derived from DOT node attributes (`handler_ref`, `agent_type`) and YAML policy, resolved at workflow-load time" and forbids runtime-branching test-mode selectors. The cite chain S07 → HC-003 → HC-INV-002 is intact. **Pass.** Strengthening note (M-2 above): the binding to HC-043 / HC-045 is implicit; SH-009 should make it explicit.

**C-2. Event-log capture cites EV envelope + emission protocol.** SH §6.1 cites `[event-model.md §6.1 Event]` for the envelope; SH-020 cites EV-021 for observational-replay rules; SH-024 cites EV §6.2 for post-fsync-tail / torn-tail rules. SH does not redeclare the envelope, the JSONL line shape, or any §8 event type. SH §6.2 explicitly states "This spec emits no cross-bus events of its own. The harness is a consumer (observational replay reader) of every event type declared in [event-model.md §8]." **Pass.**

**C-3. Workspace fixture isolation expressed in WM primitives.** SH-012 cites WM-001 (workspace primitive) and WM §4.2 (branching model) for fixture creation. SH-013 cites WM-010 for lease-by-run continuation inside the harness. SH-015 cites WM-013b for lease release on terminal transitions. SH does not introduce a new isolation primitive; the per-suite ephemeral fixture root (SH-016) is just a directory under which standard WM worktrees live. **Pass.** Note: SH-016's "MUST NOT delete the fixture root automatically on suite completion" is a harness-local invariant that does not collide with WM-031 (failed-run worktree persistence) or WM-013d (released-workspace re-use forbidden) — both WM rules continue to apply inside the fixture root.

**C-4. Daemon entry point matches PL contract.** SH-017 cites PL-005 (deterministic startup order) and asserts "the harness MUST drive scenarios by invoking the same daemon entry-point and the same orchestrator startup sequence as production `daemon` mode... MUST NOT skip startup-sequence steps." PL-005 carries 10 ordered startup steps (0..9). SH-017 binds to the entry-point but does not enumerate which steps the harness skips/cannot-skip. The "MUST NOT skip startup-sequence steps" wording is unambiguous (zero skips), which is the safer-for-correctness reading but creates a tension with the Cat 0 pre-check (PL-005 step 4) and the reconciliation dispatch (step 8) — the harness probably wants reconciliation dispatched only against fixture state, which it would be by construction (each scenario fixture is fresh). **Pass with a runtime-resolvable note** (no spec change required; the binding is correct as written).

**C-5. Failure taxonomy coupling to EM §8.** SH §8 declares 8 failure classes; each names an "EM analog" where applicable:
  - §8.1 `scenario-load-failure` → EM analog: None (pre-orchestration). OK.
  - §8.2 `twin-binary-not-found` → EM §8.2 `structural` adjacency. OK.
  - §8.3 `fixture-setup-failed` → harness-local. OK; cites WM-003 for workspace-creation failure mode.
  - §8.4 `orchestration-internal-error` → EM §8.2 `structural` adjacency. OK.
  - §8.5 `assertion-failed` → None (observational). OK.
  - §8.6 `scenario-timeout` → EM §8.4 `canceled` adjacency, citing HC-018 for the cancel surface. OK.
  - §8.7 `harness-internal-error` → harness-local. OK.
  - §8.8 `cleanup-failed` → harness-local. OK.
  Every EM analog cites the row by §-number-and-class-name; cites resolve correctly. **Pass.**

**C-6. SH-021 assertion vocabulary cites the right type sources.** Four assertion kinds:
  - `event_present` / `event_absent` — types drawn from EV §8 (cited at §6.1 EventExpectation). OK.
  - `workspace_state` — files / git refs inspected from the post-orchestration tree. SH-022 forbids absolute paths (portability); references no other spec, which is correct (workspace tree shape is harness-local-evaluation territory).
  - `exit_code` — references EM §4.1 EM-005's `outcome_status`. See m-2 above re field-name precision.
  **Pass with minor clarification suggested.**

**C-7. SH-026 timeout cancel binds to HC-018 cancel-bound contract.** Cited; HC-018 declares 500ms ctx-cancel and 5s subprocess-cleanup bounds. SH-026 says cancellation "MUST honor the bounded-cancellation contract of HC-018." Aligned. The "(operator-stop equivalent)" parenthetical leans on ON-009 without citing — see m-3.

**C-8. SH-027 determinism vs HC-026a heartbeat carve-out.** SH-027 explicitly addresses this tension: "Twin binaries that violate determinism (e.g., emit non-deterministic timing under the wall-clock heartbeat mode of HC-026a) are a twin defect classified at OQ-SH-005." HC-026a's scenario-mode (scripted) carve-out lets twins emit heartbeats at scripted timestamps, preserving byte-reproducibility. SH-027's "Byte-identity of the captured JSONL is NOT required (UUIDv7 event IDs and wall-clock timestamps drift); semantic identity of asserted observables is" is the right reading of HC-026a's scope. **Pass.** OQ-SH-005 cleanly carries the residual question.

**C-9. SH-INV-003 vs HC's launch rules.** Per M-2 above, this is the one place where the implicit/explicit boundary needs sharpening. SH-INV-003 says "the harness MUST refuse to launch a real-model handler"; the sensor is "a pre-launch check at handler-config resolution (§4.3) verifies every resolved binary path is a twin." This is a harness-side gate that runs *before* the daemon's own HC-042 / HC-043 / HC-045 checks. Whether the daemon's checks then run unchanged on the twin path is the M-2 ambiguity.

## Non-findings (what I checked but did not flag)

To make the review's coverage transparent, the following candidate cross-spec issues were considered and ruled out after analysis:

- **SH §6.1 introduces new RECORDs (`ScenarioFile`, `ScenarioResult`, `SuiteResult`, etc.).** Considered as ownership encroachment. Ruled out: these are harness-local consumer records (verdict, suite-state, fixture-config). They do not redeclare any cross-bus event payload, any handler-contract type, any workspace-model record, or any execution-model type. SH §6.2 explicitly disclaims emission of cross-bus events. **No finding.**
- **SH-013 ("each scenario uses an isolated workspace") vs WM-002 (canonical worktree path).** Considered as conflict — does the harness create worktrees outside `<repo>/.harmonik/worktrees/<run_id>/`? Re-read: SH-013 says workspaces are "under a per-suite ephemeral root (see SH-016)" and references WM-010 lease rules continuing to apply. SH-016 places the fixture root "under an OS-temp directory or a configured override." This is consistent with WM-002's worktree-root-MAY-be-overridden-by-operator-configuration clause (line 165): "The worktree root (`<repo>/.harmonik/worktrees/`) MAY be overridden by operator configuration; the per-run subdirectory `<run_id>/` is fixed." The harness's per-suite ephemeral root is exactly this kind of override. **No finding.** (Minor latent question: does the override-precedence layer per CP-037 apply to the harness's override? Likely yes, but this is runtime-resolvable; not a spec-level concern.)
- **SH-017 says "MUST NOT skip startup-sequence steps" — does this conflict with PL's reconciliation step (8) when there is no prior state?** Considered: the conformance scenarios run against fresh fixture roots; there is no prior daemon-instance state for reconciliation to walk. PL-005 step 8 is "dispatch reconciliation per RC-008 action-mapping." With empty inputs, the step is a no-op (no detectors fire); the "no skipping" wording is satisfied trivially. **No finding.**
- **SH §6.1 ScenarioFile.matrix expansion (SH-030) — does this conflict with the workflow-as-DOT three-artifact separation principle (AR-INV-008 / AR §4.10)?** Considered: matrix expansion creates synthetic per-cell `name`s but does not synthesize new workflows; the underlying `workflow_ref` is unchanged. Each matrix cell binds different parameter values to a SH-side template, not to a DOT workflow file. **No finding.**
- **SH-014 ("each scenario uses an isolated event-log directory") vs EV §6.2 ("Primary log: `.harmonik/events/events.jsonl` — single-file-per-project").** Considered: does EV's single-file-per-project pin foreclose per-scenario log dirs? Re-read: EV §6.2's single-file-per-project rule is scoped to the *operator project*. The harness, by SH-014, configures the daemon's event-log path per scenario at startup (different "project" context per scenario fixture). Per-scenario fixtures are themselves separate project contexts in the daemon's view. The single-file invariant holds *within* each scenario's log directory; SH does not multi-write to the operator's primary log. **No finding.**
- **SH-INV-001 grep-based sensor — would it false-positive against the HC-035 in-process unit-test fakes carve-out?** HC-035 explicitly carves out "Hand-written in-process fakes" used in Go unit tests as not subject to the parity contract. SH-INV-001's grep-target is the production-imported package set: daemon, orchestrator, agent-runner, etc. — not the unit-test packages. The grep would scan only production code, leaving unit-test fakes untouched. **No finding** (assuming a careful grep configuration; an implementation-level concern, not a spec-level one).
- **Reconciliation interaction.** SH does not cite reconciliation/spec.md. Considered: should SH cite RC for any contract? Re-read: the harness runs scenarios fresh; reconciliation only fires on store-divergence detection (RC §8 categories). A scenario whose orchestration produces store divergence would route through reconciliation per the daemon's normal path, observed by SH-020's event-log capture. SH does not need to cite RC because RC is a downstream effect surface, not an interface SH binds to. **No finding.**
- **Beads-integration.** Considered: should SH cite BI? Re-read: BI writes are observable through event payloads (the `bead_id` field on run-lifecycle events per BI-017/BI-018), which SH captures via the JSONL surface. SH §2.2 explicitly scopes-out per-write contracts as BI's territory. **No finding.**

## Scope of this review

In scope (per remit): every cross-spec inline cite (cite-resolves, anchored-in-normative-prose, depends-on respected, level of abstraction); ownership claims that might encroach on another spec's contract (HC's Handler / Session interfaces, EM's Outcome / Transition / failure taxonomy, EV's envelope and event taxonomy, WM's workspace primitive and lease model, PL's daemon contract, AR's classifications and amendment protocol); duplication of normative content from another spec; subtle conflicts; missed cites where SH leans on another spec's contract without naming it; depends-on minimality (declared-but-unused, used-but-not-declared); reverse coverage (does any existing spec need to add `depends-on: scenario-harness`); registry consistency.

Out of scope (handled by separate reviewers): internal-quality (clarity, consistency-within-S07, completeness of §6.1 schemas, exhaustiveness of §8 enum vs. §7.1 lifecycle); the implementer review (already at `implementer-r1.md` in this directory); content of the SH-specific OQ list; recommendations for additional SH-NNN requirements not yet present; review of the conformance-test obligations in §10.2.

## Strengths

- **Twin substitution is a config knob, not a runtime branch.** SH-008 + SH-INV-001 + SH-018 collectively express the load-bearing inter-spec invariant: production-imported packages MUST NOT carry `if isTest` / `if isTwin` branches. This aligns one-for-one with HC-INV-002 ("twins indistinguishable from real handlers; daemon MUST carry zero conditional logic that varies on handler-is-twin") and reinforces it from the consumer side. The grep-based sensor named in SH-INV-001 makes the invariant verification mechanical.
- **Failure taxonomy explicitly cites EM §8 analogs.** Each §8 class names its EM analog where one applies (`scenario-timeout` → EM §8.4 canceled adjacency; `orchestration-internal-error` → EM §8.2 structural adjacency). The "EM analog. None — pre-orchestration / harness-local" framing for `scenario-load-failure`, `assertion-failed`, `harness-internal-error`, `cleanup-failed` correctly signals where the harness is the sole authority, avoiding the temptation to back-port harness-local errors into EM.
- **Event-log capture respects EV ownership.** SH-020 cites EV-021 and explicitly declares assertion evaluation as "observational replay"; §6.2 cite picks up the post-fsync-tail rule SH-024 leans on; SH does not redefine event identity, envelope shape, or taxonomy. §6.2 of SH ("This spec emits no cross-bus events of its own") is an exemplary ownership disclaimer.
- **Workspace fixture lifecycle uses WM primitives end-to-end.** SH-012 cites WM-001 + WM §4.2 branching for fresh-workspace creation; SH-013 cites WM-010 for lease-by-run continuation; SH-015 cites WM-013b for lease release on terminal transitions. The harness does not introduce a parallel isolation primitive; the fixture is just a per-suite ephemeral root containing standard WM workspaces. WM-INV-005 (canonical-path invariant) is preserved by construction since each scenario gets a fresh `run_id`.
- **Conformance scenario set is concretely named and tied to specific cross-spec obligations.** The three scenarios (`twin-launch-and-ready`, `checkpoint-and-merge`, `twin-failure-classification`) each cite the requirements they exercise (HC-039, EM-016, WM §4.5, HC-024). Apart from the M-1 broken cite, the set is a clean cross-spec-coverage triangle: handler lifecycle (HC), checkpoint + merge (EM + WM), failure classification (HC + EM). The "Adding to the set is a foundation amendment" framing makes the conformance floor durable.
