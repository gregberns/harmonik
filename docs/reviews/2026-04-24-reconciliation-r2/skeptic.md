# Round 2 Skeptic Review — reconciliation/spec.md + schemas.md v0.3.0

Target: `specs/reconciliation/spec.md` (880 lines) + `specs/reconciliation/schemas.md` (221 lines), both v0.3.0, `status: draft`, `spec-shape: taxonomy-first`. R1 integration dated 2026-04-24.

## 1. Verdict summary

**Do NOT transition to `reviewed`. Block on a cluster of integration-accuracy defects, cross-spec drift introduced by R1's own additions, and a missing-envelope template violation.**

R1 re-shaped the spec correctly in the large (priority-ordered first-match, concurrent-reconciliation lock named, trailer reclaimed, three invariants retired, evidence-corroboration contract linked). But the R1 integration pass self-reports a set of fixes that it did not actually execute, introduces a seventh verdict (`no-op-accept`) without propagating it to the dual-table twin or to the downstream consumer (workspace-model), and attaches to event-model fields (`evidence.sources`) and operator-nfr requirements (ON-008 escalation, `quarantined` state) that do not exist in those specs. The effect is a spec that *claims* to be post-review-ready but carries seven concrete cross-spec contradictions that would blow up on a corpus lint pass.

The blocking list (§11) is substantial but every item is a targeted fix, not a redesign. Fix-size is probably ≤150 lines across both files plus one PL/ON/EV coordinated sibling nudge. The shape is right; the citations, the dual tables, and two fabricated schemas are wrong.

**Headline findings:**

- **Template violation:** `spec-category: runtime-subsystem` is declared (spec.md:10) but the AR-053-mandated `§4.a Subsystem envelope` section is absent. Architecture.md AR-053 non-rationale lists reconciliation as an example of `foundation-cross-cutting` (exempt from envelope); front-matter category is mis-declared OR the envelope is missing. Either fix is one edit; not doing either is lint-blocking.
- **Dual-table divergence already present:** R1 item I6 added `no-op-accept` to RC-020 and §8.12 Cat 3 typical-verdicts, but did NOT update schemas.md §6.3 Cat 3 row. §8.12 Cat 3 row now lists three verdicts; schemas.md §6.3 Cat 3 row still lists two. §8.12-vs-§6.3 divergence is declared a lint failure by RC itself (spec.md:251) and the R1 integration seeded it.
- **`no-op-accept` seventh verdict is not reciprocated in workspace-model.** workspace-model.md §4.9 (WM-034/WM-035/WM-036) has a table mapping six verdict values to workspace dispositions; values not in the table are classified "malformed" per RC-023 (workspace-model.md:580). RC v0.3.0 now emits a verdict WM would classify as malformed. Coordinated WM change not filed; no OQ tracks it.
- **Fabricated schema field:** RC-019a and RC-INV-004 cite `evidence.sources` with shape "declared in [event-model.md §8.6.10]" and require ≥2-store corroboration. Event-model §8.6.8 `store_divergence_detected` carries a single-value `corroboration` enum (`git-corroborated | beads-corroborated`), no `evidence.sources` array; event-model §8.6.10 is the `divergence_inconclusive` event (unrelated payload). EV-023a itself requires corroboration by EITHER git OR beads — not both; RC-019a's "≥2 stores" is a bigger claim than EV licenses.
- **Fabricated cross-spec requirement:** `operator_escalation_required` is cited as co-owned with "[operator-nfr.md §4.8] (ON-008: escalation-to-operator contract)" (spec.md:652, schemas.md:219). ON-008 is the between-task pause-invariant (operator-nfr.md:183-188), not an escalation contract; `operator_escalation_required` does not appear in operator-nfr.md at all; operator-nfr has no "quarantined" state, which schemas.md §6.2 cites as owned by ON §4.3.
- **Stale citations that R1 claims to have fixed:** ten `[beads-integration.md §4.8]` cites for adapter idempotency/intent-log — but BI v0.3.0 put idempotency + intent-log at §4.10, not §4.8. R1's revision-history row claims "beads-integration.md §10.N → §4.N" migration; the migration landed but targeted the wrong anchor in the common case.
- **Cross-spec numbering drift:** `[process-lifecycle.md §4.3]` cited by RC-002a for "workflow registry lock semantics" — PL §4.3 is Ready-state transition (PL-009/009a/009b/010); no workflow registry or registry lock is declared in PL. The RC-002a primitive ("registry lock keyed on target_run_id") is mechanically named but attached to a non-existent PL contract.
- **PL step-number drift:** RC references assume reconciliation dispatch at PL-005 step 7; PL actually runs it at step 8 (PL-005 lines 214–224). Low-severity mismatch because RC doesn't cite the step number, but the assumption surfaces in the orphan-sweep paragraph (spec.md:80) and §7.1 pseudocode (spec.md:660-682).
- **Glossary says six verdict values, RC-020 says seven.** Four internal inconsistencies in RC on the enum cardinality (spec.md:62 "six"; spec.md:481 "seven-value"; spec.md:483 "seven enum values"; schemas.md:65 "one of six enum values (see below)" with seven below). RC-009 still calls the taxonomy "6-detection-category" though §8 and §1 both say 11.
- **Payload-field leak persists in RC-023 and RC-025.** R1 item I12 claims payload-field refactor into schemas.md; RC-018 is clean but RC-023 (spec.md:506) and RC-025 (spec.md:534) still carry inline field lists.

Severity distribution: **four blocking** (§4.a envelope; dual-table divergence; fabricated evidence.sources / ≥2-store; fabricated ON-008 escalation); **five important** (stale §4.8 cluster; PL §4.3 workflow-registry-lock; WM reciprocity for seventh verdict; enum-cardinality inconsistency; payload-field leak); **several recommended** (Cat 6a workspace-missing bullet ordering vs §8.12 divergence; Cat 3a detection rule still describes pre-BI-031 audit-log idempotency-key model; missing reconciliation_started event; §A.2/§A.3 absent).

R1 identified 79% citation-miss on the v0.2.0 surface; v0.3.0 fixed most but regenerated new drift via I-items that declare fields and requirements that don't exist in the cited spec.

## 2. Integration-fix audit

Walking every fix RC's revision-history (spec.md:880) claims to have landed.

### B1 — Citation-drift migration: PARTIAL

Claim: "citation-drift migration across 52 of 66 outbound cites — migrated event-model.md §3.N anchors to §4.N / §6.3 / §8 per-event; beads-integration.md §10.N → §4.N; ... control-points.md §6.N → §4.N".

Reality:
- event-model migrations look correct. §8.6.8 / §8.6.10 / §6.3 / §4.5 resolve.
- control-points migrations look correct. §4.2 (Guard), §4.4 (Gate), §4.5 (Budget), §4.11 (Skill) resolve against current CP headings.
- workspace-model §5.N → §4.N migrated but with **wrong target**: RC-028 cites §4.7 for fresh-worktree-on-reopen-bead; real cite is §4.9 WM-034. schemas.md §6.2 reopen-bead row cites §4.7 with the same misdirection. §4.7 is "Session-log directory and metadata sidecar" — about CASS, not re-run.
- beads-integration §10.N → §4.N migrated but with **wrong target** for the adapter/intent-log cluster: ~10 cites to §4.8 should be §4.10. §4.8 is "Version-pin + adapter layer"; §4.10 is "`br`-adapter idempotency — terminal-transition writes" (BI-029/030/031/032). Examples: spec.md:52, 138, 140, 164, 243, 539, 736; schemas.md:144, 161.
- process-lifecycle migration landed for PL-005 but RC-002a cites PL §4.3 for "workflow registry lock semantics"; no such lock exists in PL. PL §4.3 covers ready-transition and degraded state. The phrase "workflow registry" does not appear anywhere in PL. Fabrication.
- operator-nfr §4.8 migration landed for restart RTO (correct), but §4.8 is cited for ON-008's "escalation-to-operator contract" (spec.md:652; schemas.md:219). ON-008 is about pause/drain; `operator_escalation_required` is not owned by ON at all.

Status: **partial — most migrations correct on numbering, but the BI/PL/ON cluster targets the wrong subsection or a non-existent contract.**

### B2 — Reclaim Harmonik-Verdict-Executed trailer in schemas.md §6.4: ACCURATE

Claim: "declared shape in [schemas.md §6.4]; all RC cites to [execution-model.md §6.2] for this trailer rewired to [schemas.md §6.4]."

Reality: schemas.md §6.4 (lines 169-184) declares the trailer with key/value/placement/emission-contract/cross-spec-coordination blocks. Grep for `[execution-model.md §6.2]` in RC yields zero hits — rewire landed. RC-025 cites `[schemas.md §6.4]` directly. Clean.

One small nit: schemas.md §6.4 line 171 says "reclaimed from execution-model.md after EM v0.2.0 removed the trailer from its §6 trailer registry" — but I didn't verify EM §6 has a corresponding "trailer registry" section. Not a blocker; cross-spec housekeeping in EM's own revision.

### B3 — RC-003a priority-ordering rule: ACCURATE BUT UNDERSPECIFIED

Claim: "Added RC-003a category-orthogonality priority-ordering rule (Cat 0 → 6b → 6a → 5 → 3c → 3b → 3a → 3 → 2 → 4 → 1 first-match)."

Reality: RC-003a (spec.md:289-298) is present. Priority order published. The rule is mechanically deterministic at the pair-match level. Tie-break rationale is published per-pair. Passes the orthogonality test R1 challenged.

Concern: the first-match evaluation itself is a mechanical property of the detector code, not a structural property of the categories. RC-003a makes it normative but does not say what the detector does when a detector itself *errors* during evaluation (e.g., Cat 3a's intent-log read fails midway). Does the daemon skip that detector and fall through, or route to Cat 6b? The failure mode is open. (This is the concern R1 critic Challenge 2 (b) flagged as "per-detector precondition"; I believe it is still live and RC-003a does not close it.)

Status: **accurate on what it claims; leaves RC-012's per-detector-precondition hole from R1 Challenge 2 untouched.**

### B4 — RC-002a concurrent-reconciliation lock: ACCURATE ON PRIMITIVE, BROKEN ON CITATION

Claim: "at most one reconciliation workflow per target_run_id, 5s bounded wait via process-lifecycle.md §4.3 registry lock."

Reality: RC-002a (spec.md:282-287) declares the lock, the key, the 5s bounded-wait timeout, and the reclassification path. The primitive IS named ("daemon's workflow registry... hold a lock keyed on target_run_id"). Good — R2 brief asked for this.

But the cite "[process-lifecycle.md §4.3]" is wrong: PL §4.3 is Ready-state transition; no workflow registry, no registry lock. Either (a) PL needs a new requirement for workflow-registry-lock semantics and RC should cite that, or (b) RC owns the lock itself and should not cite PL. The R1 integration took option (a) on paper and did not file a PL-coordination OQ. No OQ-RC-00N tracks "PL accepts workflow-registry-lock contract."

A second concern surfaced only on close reading: RC-002a says a second dispatch "MUST fail-classify the attempt as Cat 4 (recoverable known state, awaiting lock) if the first reconciliation workflow is still progressing." Cat 4's §8.7 detection rule is "Agent was in a well-defined retry/backoff state at crash time (e.g., rate-limited pending retry timer, waiting for a human gate)." Re-using Cat 4 as "awaiting-lock" is a semantic hijack. Cat 4 auto-resumes; the "wait for lock" case has no autonomous resume — it needs to re-enter the dispatcher when the first reconciliation completes. Proposing: new sub-category or a purpose-built "lock-contended" disposition, not a Cat 4 misuse.

Status: **primitive correctly named; citation is fabricated; Cat 4 overload is a semantic bug.**

### B5 — RC-019a evidence-corroboration rule: FABRICATED AGAINST EV-023a

Claim: "every `store_divergence_detected` requires ≥2-store corroboration; Cat 6b exempt."

Reality: RC-019a (spec.md:394-405) declares this and cites `[event-model.md §8.6.8] (EV-023a)` and `[event-model.md §8.6.10]` for the `evidence.sources` payload shape.

Cross-checking EV:
- EV §8.6.8 (event-model.md:152) `store_divergence_detected` payload: `run_id?, bead_id?, divergence_kind, evidence_ref, post_crash_window, corroboration (git-corroborated / beads-corroborated)`. Single-valued enum. **No `evidence.sources` field.**
- EV-023a (event-model.md:425-427): "A divergence detector MUST classify the evidence supporting every candidate divergence event into one of: `git-corroborated`, `beads-corroborated`, or `inconclusive`." The detector corroborates EITHER against git OR against beads. **"≥2 of the three stores" is not what EV-023a requires.**
- EV §8.6.10 (event-model.md:154) is the `divergence_inconclusive` payload: `run_id?, bead_id?, evidence_ref, post_crash_window, reason (no_authority_reference | authority_unavailable)`. **No `evidence.sources` field.**

RC-019a consequently requires a payload field that the registered payload does not carry and imposes a stricter corroboration rule than the upstream EV contract. RC-INV-004 (spec.md:612-618) inherits the same fabrication. Also: RC-014 (spec.md:390) embeds the "≥2 of three stores" rule inline.

This is worse than a citation drift — it invents an upstream contract. An implementer reading EV-023a and RC-019a will not know which rule to enforce; the detector's emission validator will reject emissions whose `evidence.sources` field is absent (RC-019a line 399 "daemon MUST reject the emission on absence") but the field is not in the event-model payload.

Also note: EV §8.6.8's enum has only TWO corroboration values. RC-019a's Cat 6b-exempt path says "where only one store is readable" — but EV's per-evidence-item classification is already per-authority-reference, not per-pair. The ontology doesn't match.

Fix options:
1. Rewrite RC-019a to EV-023a's actual semantics: a single-authority corroboration, per-event-item, with Cat 6b as the "authority-unavailable" path (which IS what EV-023a models via the `divergence_inconclusive` emission).
2. File an EV amendment adding the `evidence.sources` multi-authority array if reconciliation genuinely needs ≥2-store witness. This is an amendment-protocol item per AR-020.

Status: **blocking fabrication. Fix before `reviewed`.**

### B6 — AR-042 sensors on invariants: PARTIAL

Claim: "Added AR-042 sensors to invariants RC-INV-001 and RC-INV-004."

Reality:
- RC-INV-001 (spec.md:596-602) carries a "Sensor." block describing three detection paths (tag audit at registry, JSONL query, per-daemon-lifecycle sample).
- RC-INV-004 (spec.md:612-618) carries a "Sensor." block describing emission-layer validation with `evidence.sources.length >= 2` — but as noted in B5, the field does not exist in the event-model payload. The sensor references a field that cannot be queried. Broken sensor.
- Retired invariants RC-INV-002/003/005 have no sensor (appropriate for retired stubs).

Status: **RC-INV-001 sensor: clean. RC-INV-004 sensor: inherits B5's fabrication.**

### B7 — Invariants retired: ACCURATE

Claim: "Retired RC-INV-002, RC-INV-003, RC-INV-005 as selection-test failures; preserved IDs as retirement stubs; rewrote RC-INV-001 ... and RC-INV-004 ..."

Reality:
- RC-INV-002 (spec.md:604-606) is retired with a stub note: "restates RC-002 + RC-003 ... No semantic loss." Correct — the rule IS at RC-002 and RC-003.
- RC-INV-003 (spec.md:608-610) is retired with a stub note: "restates RC-002 + RC-025 + RC-026 ... Requirements remain normative at their original requirement IDs." Correct.
- RC-INV-005 (spec.md:620-622) is retired with a stub note: "restates RC-010 + RC-015." Correct.
- RC-INV-001 rewritten to reconciliation-as-workflow uniqueness (spec.md:596-602). Cross-subsystem cuts: RC-001 ↔ workflow registry ↔ event-model. Selection-test passes.
- RC-INV-004 rewritten to evidence-corroboration guarantee across detector dispatch points (spec.md:612-618). Cross-subsystem cuts: detector dispatch ↔ EV-023a ↔ JSONL writer. Selection-test passes except for the B5 fabrication on the field.

Status: **retirements and rewrites are genuine; B5 fabrication contaminates RC-INV-004.**

### I1 — §A.4 reverse-drift migration map: LANDED

Table at spec.md:847-872. Follows WM v0.3.0 shape. Useful for downstream specs that still cite legacy anchors.

Gap: the migration map covers §N.N section anchors but does NOT cover retired invariants. If a downstream spec cites `[reconciliation.md §5 RC-INV-002]` (retired), that reader lands at the retirement stub — which is acceptable but the migration map could call this out. Low severity.

### I2 — `workflow_class` extension: LANDED

schemas.md §6.5 (lines 186-205) declares the extension clearly, with semantics, ownership, and future-growth reservation. OQ-RC-002 tracks EM acceptance. Correct shape.

Concern: `EXTENSION OF ExecutionModel.Workflow` is a schema-extension primitive that is not itself defined in any spec. The template does not describe extension semantics. schemas.md line 191 says "optional tag, absent means 'ordinary'" — plausible but the "ordinary" sentinel is undeclared elsewhere. For a runtime-subsystem to extend a foundation record, the amendment protocol of architecture.md §4.6 applies; OQ-RC-002 is the right vehicle but should cite AR-020 explicitly.

Status: **landed; plumbing is informal but tracked via OQ-RC-002.**

### I3 — Cat 2 JSONL controlled-indexed-query: LANDED

RC-014 (spec.md:373-390) and §8.3 Cat 2 detection rule (spec.md:112) both specify "controlled, indexed query against the bounded JSONL divergence-evidence reader ... NOT a live-follow tail." R1 implementer Challenge flagged this as a Cat-2-vs-RC-014 tension; R1 integration partly resolved it by clarifying the query discipline.

Remaining: Cat 2 detection still consumes `run_completed`/`run_failed` events via JSONL. RC-014 forbidden-uses list (spec.md:384-388) forbids JSONL as a source of "last-known `run_id`, `state_id`, or `transition_id`" — but allows a permitted Cat 2 liveness probe. The carve-out is declared; implementer can interpret it. Still a grey zone: the detector reads run-terminal event-TYPE presence, but NOT state/transition IDs from JSONL. Fine.

Status: **landed; permitted carve-out is explicit.**

### I4 — Cat 3 vs Cat 6a workspace-missing precedence: LANDED VIA RC-003a

§8.11 Cat 6a (spec.md:198-215) has a precedence bullet at line 204-205 citing RC-003a priority order. Ordering is declared.

The §8.11 text is slightly awkward — it reads like prose retrofitted onto the priority-order framework, with the numbered bullet becoming a sub-sub-description. Not a blocker, but a reader unfamiliar with RC-003a could miss the mechanical dispatch rule in favor of the prose interpretation. Fix: move the precedence paragraph to RC-003a or §8.4a and leave §8.11 as a pure detector rule list.

Status: **landed; presentation is awkward but mechanical rule holds.**

### I5 — InvestigatorInput as LaunchSpec-extended: PARTIAL + FABRICATED FIELDS

Claim: "Rewrote RC-015 so InvestigatorInput is an adapted/extended LaunchSpec per [handler-contract.md §6.1]; schema validation failure routes through RC-023."

Reality: RC-015 (spec.md:422-437) says InvestigatorInput "carries every LaunchSpec field (agent role, model class, skill injection set, prompt template reference, cwd, env, policy refs) AND the reconciliation-specific fields below."

Cross-check against HC §6.1 / HC-006 (handler-contract.md:111-113): LaunchSpec required fields are `run_id, workflow_id, node_id, agent_type, workspace_path, required_skills[], skill_search_paths[], timeout, provisioning_timeout, budget, freedom_profile_ref`; optional `bead_id?, snapshot_token?`.

RC-015's enumeration of LaunchSpec fields ("agent role, model class, skill injection set, prompt template reference, cwd, env, policy refs") does not match HC-006:
- "agent role" — HC has `agent_type`, not `agent role`. Plausible rename but not declared.
- "model class" — HC has NO `model_class` field. Fabrication.
- "skill injection set" — HC has `required_skills[]` and `skill_search_paths[]`. Two fields combined under one informal label.
- "prompt template reference" — HC has NO `prompt_template_ref` field. Fabrication.
- "cwd, env" — HC has `workspace_path` (proxy for cwd); no `env` field declared.
- "policy refs" — HC has `freedom_profile_ref`; plural "refs" doesn't match.

The downstream schemas.md §6.1 `InvestigatorInput` RECORD (lines 27-43) carries a different field list than RC-015's prose. Neither matches HC-006. An implementer constructing InvestigatorInput by "extending LaunchSpec" has three competing field lists.

Fix: either (a) reference HC-006's actual LaunchSpec field list verbatim with a carve-out for reconciliation-specific fields; or (b) declare InvestigatorInput as a *view* the investigator assembles from its snapshot-token-bounded reads (not a LaunchSpec extension at all — which is what R1 critic Challenge 5 alternative (a) recommended and which is cleaner).

Status: **partial — claim landed in shape; field-list enumeration is loose prose that doesn't match HC.**

### I6 — `no-op-accept` seventh verdict: LANDED WITH DUAL-TABLE DIVERGENCE + WM COORDINATION GAP

Claim: "Added `no-op-accept` verdict to RC-020 (now seven-value enum) with mechanical action in RC-025 and [schemas.md §6.2]; annotated §8.12 Cat 3 typical-verdicts."

Reality:
- RC-020 (spec.md:481-485) declares seven values. ✓
- schemas.md §6.1 Verdict ENUM (schemas.md:76-84) lists seven. ✓
- schemas.md §6.2 has a row for `no-op-accept` (schemas.md:146). ✓
- RC-025 idempotency table (spec.md:542) has a `no-op-accept` bullet. ✓
- §8.12 Cat 3 typical-verdicts column (spec.md:242) lists `no-op-accept`. ✓
- **schemas.md §6.3 Cat 3 typical-verdicts column (schemas.md:160) does NOT list `no-op-accept`.** Lint-failure divergence.
- Glossary (spec.md:62) still says "one of six enum values"; schemas.md VerdictEvent (schemas.md:65) comment still says "one of six enum values (see below)". Four call-sites out of sync with the seven-value rule.

**Cross-spec reciprocity gap:** workspace-model.md v0.4.1 §4.9 WM-036 (workspace-model.md:569-580) tables six verdicts and explicitly says "Any verdict value not in the table above is a malformed verdict per RC-023 and MUST NOT be routed by this spec." A `no-op-accept` verdict, per WM-036 as currently written, would be classified malformed at the WM side. No OQ tracks WM's acceptance of the seventh verdict. No §9.1 cross-reference by RC to WM cites the verdict-mapping table. This is the exact "verdict-enum extensions must be declared in the reconciliation spec" sentence WM has at line 580 — but WM hasn't been updated, and RC added the value unilaterally.

Fix: (a) file an OQ tracking WM acceptance of the seventh-value enum; (b) update schemas.md §6.3 Cat 3 row to include `no-op-accept`; (c) fix all four internal inconsistencies on the six-vs-seven count.

Status: **blocking — dual-table divergence + missing WM reciprocity.**

### I7 — RC-020a detector cadence: LANDED

RC-020a (spec.md:407-418) declares three dispatch points (daemon startup; on-demand operator command; scheduled hourly background scan) plus an idempotency-across-dispatch-points rule. Post-MVH cadence tuning queued as OQ-RC-004. Clean.

Concern: RC-020a cadence (c) says "MVH default is hourly, configurable via operator YAML per [operator-nfr.md §4.3]." ON §4.3 is the operator-control state machine. The cadence knob belongs in ON's operator-config surface per ON §4.3.ON-006 (config inventory). ON-006 (operator-nfr.md:156) says "At minimum the inventory covers ... per-Cat reconciliation budgets ([reconciliation/spec.md §4.4])." But it doesn't call out the hourly cadence knob. Coordinated OQ missing.

Status: **landed; minor ON-coordination gap (cadence knob in ON-006's inventory).**

### I8 — Dispatch points declared: LANDED

Already covered under I7.

### I9 — §8.12 vs schemas.md §6.3 dual-table ownership: LANDED (WITH IMMEDIATE DIVERGENCE)

Claim: "Clarified §8.12 vs schemas.md §6.3 dual-table ownership — semantics vs mechanical dispatch."

Reality: §8.12 has an INFORMATIVE note (spec.md:251) declaring the ownership split. But the ownership split does not *prevent* drift — and in I6 above the integration promptly seeded a divergence. The mechanism is declarative, not mechanical. A lint-enforced pre-commit check comparing the two tables cell-by-cell is the only mechanism that would actually enforce the rule, and it is not named in the spec.

R1 critic Challenge 4 recommended either collapsing one table to a pointer OR making the two tables byte-identical. The R1 integration picked neither; it declared a soft split and promptly landed a divergence in the same pass.

Status: **landed as declarative; mechanical drift already present; the rule is self-violated.**

### I10 — EM orchestrator dispatch + WM §4.7 worktree: PARTIAL

Claim: "Cited EM orchestrator dispatch + WM §4.7 worktree interaction in RC-022 for `resume-here` / `reset-to-checkpoint`."

Reality: RC-022 (spec.md:495-500) cites `[execution-model.md §4.1]` for current-node resolution and `[workspace-model.md §4.7]` for worktree interactions on `reset-to-checkpoint`.

- EM §4.1 is Workflow/Node/Edge definitions (not the orchestrator dispatcher). The "current-node resolution contract" is actually at EM §7.1 (state machine). Mis-targeted.
- WM §4.7 is Session-log directory and metadata sidecar. Worktree-reset mechanics live in WM §4.4 (Lifecycle) and §4.6 (Conflict resolution), not §4.7.

Status: **citations mis-targeted. Landed in shape, wrong in anchor.**

### I11 — §6.5 co-owned notes: PARTIAL + FABRICATED ON-008

Claim: "§6.5 notes `infrastructure_unavailable` co-owned with PL-010 and `operator_escalation_required` co-owned with ON-008."

Reality:
- `infrastructure_unavailable` co-owned with PL-010: accurate. PL-010 (process-lifecycle.md:336-340) is the `degraded` state on Cat 0 failure and declares "The daemon MUST emit `infrastructure_unavailable`". Clean.
- `operator_escalation_required` co-owned with ON-008: **fabrication**. ON-008 (operator-nfr.md:183-188) is "Pause and upgrade respect the between-task invariant." It has no relationship to `operator_escalation_required`. Grep for `operator_escalation_required` in operator-nfr.md yields zero hits. Grep for `quarantined` in operator-nfr.md yields zero hits. The event is owned, per event-model.md §8.6.9, by daemon-core emitter; operator-nfr's role is unspecified. The co-ownership claim is invented.

Status: **first item accurate; second item fabricated.**

### I12 — Payload-field leak refactor into schemas.md: PARTIAL

Claim: "Refactored RC-018, RC-023, RC-024, RC-025 payload-field leak into schemas.md references; RC declares WHEN/WHY only."

Reality:
- RC-018 (spec.md:458-471): clean. References `[schemas.md §6.1] BudgetExhaustedPayload`. ✓
- RC-023 (spec.md:502-509): **NOT refactored**. Still carries `(investigator_run_id, target_run_id, malformation_reason, raw_verdict_excerpt)` inline (spec.md:506). The accompanying MalformationReason enum is correctly delegated to schemas.md but the payload tuple is still inline.
- RC-024 (spec.md:516-525): clean. References `StaleVerdictPayload` in schemas.md §6.1. ✓
- RC-025 (spec.md:529-546): **NOT refactored**. Still carries `(investigator_run_id, target_run_id, verdict, executed_at_timestamp, action_summary)` inline (spec.md:534). No `VerdictExecutedPayload` declared in schemas.md §6.1 either — the schema home named in I12 does not exist for this event.

Fix: (a) add `MalformedVerdictPayload` and `VerdictExecutedPayload` records to schemas.md §6.1; (b) replace the inline tuples in RC-023 and RC-025 with schema citations.

Status: **2 of 4 landed; RC-023 and RC-025 un-refactored.**

### I13 — First-plausible-answer resolutions via OQs: PARTIAL

R1 critic flagged seven first-plausible-answer findings; of those:

- RC-017 budget defaults (600/300/900s) — still declared in RC-017 (spec.md:452-454). Not moved to OQ; not deleted. Claim "resolved via OQ" is inaccurate; the numbers are still normative.
- RC-025 resume-here/resume-with-context idempotency — still in spec.md:540 with the same "additive context" claim R1 flagged as non-idempotent under competing investigator runs. No OQ tracks this.
- Cat 0 Beads SQLite-locked-by-non-harmonik probe — still declared in §8.1 (spec.md:84). No OQ tracks the `lsof` + argv mechanical probe.
- RC-019 WIP capture as museum piece — still at spec.md:472-477. No "captured for operator inspection, not automatically re-applied" carve-out.
- RC-016 playbook-as-named-artifact-without-schema — still at spec.md:441-447. The playbook schema is deferred to S01 (per scope) but there's no OQ tracking the inline-vs-prompt-fragment question R1 critic posed.
- RC-020 missing "no-op" verdict — PARTIALLY addressed by `no-op-accept` (I6). But see B5 on dual-table + WM reciprocity drift.
- §8.12 "typical verdict" column conflation — spec.md:237 column is still titled "Typical verdict (if investigator)" without clarifying whether "typical" is legality or likelihood.

Status: **1 of 7 semi-addressed (no-op-accept); 6 of 7 unaddressed. Claim "concrete resolutions for all 7" is inaccurate.**

### I14 — spec-id + status: CLEAN

Both files declare `spec-id: reconciliation`; schemas.md declares `status: supplement`; spec.md declares `status: draft`; version: 0.3.0 on both. Inter-sibling references resolve. Clean.

### Integration-fix audit summary

| Item | Claim | Reality |
|---|---|---|
| B1 citation migration | 52 of 66 | Numbering migrated; wrong anchor in BI §4.8 cluster (~10 cites); fabricated PL §4.3; fabricated ON-008 |
| B2 trailer reclaim | Shape in §6.4 | Clean |
| B3 RC-003a priority order | First-match published | Clean on orthogonality; doesn't close per-detector preconditions |
| B4 RC-002a lock | Primitive named | Primitive named; citation fabricated; Cat 4 overload |
| B5 RC-019a corroboration | ≥2-store rule | **Fabrication against EV-023a and EV §8.6.8** |
| B6 AR-042 sensors | Added to INV-001/004 | INV-001 clean; INV-004 sensor references fabricated field |
| B7 invariants retired | 3 retired, 2 rewritten | Clean |
| I1 §A.4 migration map | Added | Clean |
| I2 workflow_class extension | Declared | Landed with OQ-RC-002 |
| I3 Cat 2 JSONL clarification | Landed | Clean |
| I4 Cat 3 vs 6a precedence | Via RC-003a | Landed; presentation awkward |
| I5 InvestigatorInput as LaunchSpec | Rewrote | Field-list enumeration fabricated |
| I6 seven-value enum | Added no-op-accept | **Landed with dual-table divergence + missing WM reciprocity** |
| I7 detector cadence | RC-020a | Landed; minor ON coordination gap |
| I9 dual-table ownership | Declared | Declared; immediate drift self-seeded |
| I10 RC-022 citations | EM + WM cited | Mis-targeted anchors |
| I11 co-owned notes | PL + ON | PL accurate; **ON-008 fabricated** |
| I12 payload-field refactor | 4 requirements | 2 of 4 landed |
| I13 OQ resolutions | 7 items | 1 of 7 |

Seven genuine fixes; four partial/misleading; three fabrications. Revision-history claim accuracy: poor.

## 3. Hidden assumptions

The spec relies on unspecified-elsewhere mechanisms:

1. **`.harmonik/beads-intents/` survives across the orphan sweep.** R1 hidden-assumption 1 flagged this; PL v0.4 actually addresses it (process-lifecycle.md:238: "Stale entries MUST be LEFT on disk for classification by the reconciliation Cat 3a detector"). RC should cite this explicit PL provision instead of silently depending on it; currently the coordination is one-sided (PL cites RC; RC does not cite back).

2. **Investigator subprocess can emit outcomes before daemon reaches `ready`.** PL-003b (process-lifecycle.md:193-195) rejects `emit-outcome` / `claim-next` for unknown run_id during the pre-ready window. Reconciliation workflows are dispatched at PL-005 step 8, before PL-009 `ready`. Are reconciliation workflow run_ids registered in the daemon's in-memory model at the time the investigator subprocess starts emitting? PL-005 step 7 "Build the in-memory model" — the reconciliation workflow's own run_id is minted AT step 8 during dispatch, not at step 7. The investigator subprocess's early emissions (e.g., progress-stream heartbeats) will be rejected by PL-003b unless the dispatch layer registers the run_id synchronously before spawning the subprocess. Spec silent on this. Implementer might assume it; a twin-handler scenario test would catch it; the rule is unstated.

3. **`br show --format json` is available at reconciliation startup.** BI-031 (beads-integration.md:371-376) requires `br show <bead_id> --format json` for the status-check-before-reissue path. Cat 3a's auto-resolver (RC spec.md:140) doesn't cite BI-031; it cites §4.8 (wrong section) and describes the pre-BI-031 audit-log-idempotency-key model. If Cat 3a is implemented per RC's Cat 3a prose, the adapter re-issues writes using an audit-log idempotency-key read that Beads itself is not guaranteed to expose. The BI-031 reframe (v0.3.0) aligned to exactly the kind of status-check path that works without Beads idempotency-key exposure — but RC didn't update its Cat 3a detection rule or auto-resolver description. Cross-spec drift.

4. **Snapshot-token advancement is observable via git branch head + Beads audit entries.** RC-024 (spec.md:516-525) says staleness fires if "the target run's checkpoint trail has gained a new commit since the snapshot, OR the target run's bead has changed status in the Beads audit log since the snapshot." The git-branch-tip comparison is well-defined; the Beads audit log comparison requires a total ordering on audit entries. schemas.md §6.1 types `beads_audit_entry_id : String` — an opaque string. Two strings cannot in general be range-tested without a secondary ordering. BI spec does not say audit entry IDs are monotonic UUIDv7. Hidden assumption: entry ID comparison is meaningful. Either type it (`UUIDv7` or `Integer`) or cite BI's ordering rule.

5. **Reconciliation workflow wall-clock is measurable from `run_started` to terminal event.** RC-017 (spec.md:448-454) bounds the wall-clock budget from `run_started` to terminal. Handler-contract's silent-hang detection (HC-019) already imposes a 600s default silent-hang ceiling on the subprocess. Two timers apply to the same subprocess; spec does not declare their interaction. Budget ceiling > silent-hang ceiling → silent-hang fires first; budget ceiling < silent-hang ceiling → budget fires first. Cat 2 default (600s) equals HC silent-hang default — race condition on which fires. Unresolved.

6. **"Quarantined" state has a definition.** RC-025 bullet 5 (spec.md:543) and schemas.md §6.2 row for `escalate-to-human` (schemas.md:147) refer to a "quarantined state" per operator-nfr.md §4.3. ON §4.3 has no `quarantined` state. No `interrupt_state` value in workspace-model §4.10 WM-037 (workspace-model.md:586-590) matches "quarantined" either. The word is used twice, defined nowhere. Either name the enum value (e.g., `run_state = quarantined` on the Run record) or rewrite as "paused pending operator intervention" with the existing ON vocabulary.

7. **`infrastructure_unavailable` emission timing race between RC-012 and PL-010.** RC-012 (spec.md:352-363) says the daemon emits `infrastructure_unavailable` on prerequisite failure. PL-010 (process-lifecycle.md:336-340) says the daemon emits `infrastructure_unavailable` on Cat 0 prerequisite failure. Co-owned. Who emits it first? Is there a possibility of double-emission? RC §6.5 says "RC owns the Cat 0 emission trigger, PL owns the daemon-lifecycle response." But both specs say "the daemon MUST emit" — that's a single event, emitted once, by daemon code that consumes both specs. Deduplication mechanism unstated.

8. **Multiple investigator-dispatched categories concurrently dispatching.** RC-002a covers "one reconciliation workflow per target_run_id". But the daemon at startup might have twenty in-flight runs, each classified into different investigator-required categories. Twenty concurrent investigator workflows running under HC's concurrency ceiling (PL-014a: `min(RLIMIT_NOFILE/8, 1024)` defaults). No carve-out: investigator workflows consume the same per-daemon ceiling as production workflows. On a heavily-loaded restart, new work gets queued behind reconciliation. The budget exhaustion path (RC-018) kicks in at 600s per Cat 2 — a restart with 20 Cat 2s would exhaust all of them sequentially in a worst case. Maybe this is fine; the spec doesn't analyze it.

9. **Cat 3b auto-resolver re-dispatches with a fresh staleness check — but against what snapshot?** RC-026 says the auto-resolver "re-attempts the verdict's mechanical action under a fresh staleness check (RC-024)." The staleness check of RC-024 compares current state against a snapshot token captured at *investigator-dispatch time*. For a Cat 3b auto-resolver re-attempting a prior verdict's action, the snapshot was captured by a *prior* daemon instance (the one that crashed). Is that prior snapshot still consulted, or does the auto-resolver capture a fresh snapshot? The spec doesn't say; §7.2 pseudocode (spec.md:685-704) implies the verdict event carries its snapshot token through, but RC-026 doesn't state this explicitly.

## 4. R1 regressions

Places where R1's integration introduced a new defect not present in v0.2.0:

1. **Dual-table divergence (I6 + I9).** v0.2.0 already had the dual-table pattern; v0.3.0 added `no-op-accept` to §8.12 only. Net: a previously-consistent (if-fragile) pattern is now inconsistent.

2. **RC-020a hourly cadence claim.** v0.2.0 was silent on detector cadence (R1 Challenge 8 flagged). v0.3.0 declares "hourly default." R1 integration picked the first plausible number. No telemetry backs the choice; the critique applies equally to the new value. OQ-RC-004 defers tuning. Fine for MVH but "hourly" is pulled from thin air.

3. **RC-002a 5-second bounded wait timeout.** v0.2.0 had no concurrency primitive. v0.3.0 adds a 5-second timeout. No rationale for 5s specifically; reads as "matches Cat 0's 5s br-version ceiling." Coincidence, not analysis. If the first reconciliation workflow has a 300s budget (Cat 3 default), the second dispatch falls through to Cat 4 after 5 seconds — a 59× mismatch.

4. **evidence.sources fabrication (I5 + RC-INV-004).** New in v0.3.0. Not a regression against v0.2.0 (the field didn't exist either way) but a new contract declared against a non-existent upstream field.

5. **ON-008 co-ownership fabrication (I11).** New in v0.3.0. operator-nfr.md R1 integration (dated 2026-04-24) does not introduce `operator_escalation_required` ownership at ON-008. If someone patches this by moving the requirement to a real ON section, they'd have to land a coordinated ON change — filing an OQ is the minimum.

6. **RC-002a's Cat 4 overload.** New in v0.3.0. Cat 4 is "Agent was in a well-defined retry/backoff state at crash time." The new "awaiting-lock" case has no agent in a retry state at all — it's a pure dispatcher condition. Reusing Cat 4 muddles the taxonomy for the rest of time.

7. **RC-015's LaunchSpec-field fabrications (I5).** v0.2.0 did not claim to adapt LaunchSpec; it had its own InvestigatorInput shape. v0.3.0 declares adaptation but enumerates fields that don't exist in LaunchSpec. New cross-spec drift introduced by the "we align with HC" pivot.

8. **Cat 3a detection-rule stale against BI-031 reframe.** BI v0.3.0 replaced Beads-idempotency-reliant path with status-check-before-reissue. RC's Cat 3a detection rule (spec.md:138) still describes the old contract. R1 integration did not sync this. Not strictly a regression in RC (same prose as v0.2.0) but a cross-spec regression: BI moved, RC didn't follow.

## 5. Over- and under-specification

**Over-specified:**

- **RC-017 per-category budget defaults.** 600s / 300s / 900s per Cat 2 / Cat 3 / Cat 6a. R1 flagged this; R1 integration defended by pointing at S01. Per RC's own scope §2.2 (spec.md:55), "DOT authoring details for the reconciliation workflow library ... owned by the S01 Orchestrator Core subsystem spec, post-MVH." Yet the numbers remain inline here. Scope leak.

- **RC-002a 5s timeout.** As discussed in §4.3 above; prescriptive without analysis.

- **§8.12 "Typical verdict" column.** Column-wise informational content in a normative table. R1 flagged this ("conflates legality with likelihood"); R1 integration didn't touch it. A reader infers the column is contract; the author intends it as informative. Fix: rename to "Likely verdicts (informative)" or drop.

- **RC-025 idempotency bullets prescribing the shape of the `br` CLI call.** `br reopen <bead_id>` with specific idempotency key format `<target_run_id>:reopen`. The adapter layer (BI §4.10) owns this; RC is duplicating the contract here. Fix: delegate to BI-029 / BI-030 and don't name the key format.

- **RC-019 `.harmonik/reconciliation/<investigator_run_id>/wip-capture/`.** Prescriptive directory path. R1 critic first-plausible-answer note (capture as museum piece); R1 integration didn't touch. Path semantics should live in workspace-model or be abstracted to "evidence store" with the path owned elsewhere.

**Under-specified:**

- **RC-003a failure mode when a detector errors mid-evaluation.** Priority-ordered first-match is clean when detectors all terminate cleanly. No rule for "detector raised an exception" — fall through? Treat as Cat 6b? Per-detector-precondition failure per R1 Challenge 2 (b)? Silent in v0.3.0.

- **Concurrent-reconciliation lock fairness and reclaim semantics.** RC-002a says "the lock MUST be released atomically with the verdict-executed commit per PL §4.3." PL §4.3 has no such lock. Lock is named but not implemented against. Fairness (FIFO? LIFO?), reclaim on crash (holder died with lock held?), lock-file-vs-in-memory-mutex — none of these are named. R2 brief asked for the primitive; RC-002a answers "registry lock" without declaring it in PL.

- **`no-op-accept` dispatch semantics.** RC-025 bullet for `no-op-accept` says "no mechanical action beyond emission ... and the verdict-executed commit. The outer run is left untouched." Good. But `no-op-accept` is an investigator-emitted verdict for a *Cat 2 / Cat 3 generic* case where the investigator concludes no action is needed. The outer run is non-idempotent or in store disagreement — does the next dispatch cycle re-investigate it? If the initial classification that dispatched the investigator now reads the same evidence, a loop is plausible. The verdict needs a "suppress future reclassification" side effect (marker on the outer run's branch? idempotency-class override?) to avoid live-lock. Not declared.

- **`VerdictExecutedPayload` schema.** RC-025 (spec.md:534) names payload fields; schemas.md §6.1 declares no such record. Either named in RC without declaration anywhere, or invisibly delegated to event-model §8.6.4 (event-model.md:148 has the exact same field list). The schema home is ambiguous. Fix: cite event-model §8.6.4 explicitly or define the record in schemas.md.

- **`MalformedVerdictPayload` schema.** Same pattern as VerdictExecutedPayload. RC-023 names fields; event-model §8.6.5 has them; schemas.md doesn't declare the record. Unclear home.

- **"Quarantined state" enum value.** Used in RC-025 and schemas.md §6.2 but defined nowhere. See §3 item 6.

- **Per-detector preconditions.** R1 Challenge 2 stronger-alternative recommended per-detector precondition declarations. Not in v0.3.0. RC-012 remains a flat Cat 0 gate only.

## 6. Cross-spec promises — OQ realism

RC v0.3.0 has 7 OQs (OQ-RC-001 through OQ-RC-007). Walking each:

- **OQ-RC-001 (failure-commit additive extension).** Unchanged from v0.2.0. Defer is reasonable. Blocks nothing at MVH.
- **OQ-RC-002 (EM acceptance of `workflow_class`).** New in v0.3.0. OQ-target is EM's "next minor version." No commitment filed at EM side; EM's revision history doesn't mention the extension. One-way filing. The default-if-unresolved (retain in schemas.md §6.5) is operable, so this is a safe OQ.
- **OQ-RC-003 (fail-open reconciliation escalation).** Specifies behavior when reconciliation detectors themselves panic. Defers the choice. Plausible at MVH. No PL coordination filed; this is squarely a PL concern (whether to reach `ready` or stay `degraded`). Default-if-unresolved is conservative (refuse to reach `ready`). Tractable.
- **OQ-RC-004 (detector cadence tuning).** Defers post-MVH tuning. Fine.
- **OQ-RC-005 (operator verdict-override CLI grammar).** Owner: operator-CLI spec. ON §2.1 out-of-scopes this surface. Default-if-unresolved: keep current naming. Fine but the downstream owner (operator-CLI spec) does not yet exist. Latent.
- **OQ-RC-006 (testing.md migration).** Procedural housekeeping. Fine.
- **OQ-RC-007 (recoverable-non-idempotent resume protocol).** Defers post-MVH EM change. EM-010 is the upstream; once the class ships with a resume protocol, Cat 2 MAY split into Cat 2 and Cat 2a. Amendment protocol (AR §4.6) applies. Fine.

OQs NOT filed but should be:
- **OQ-RC-008 (WM acceptance of seventh-value verdict).** WM v0.4.1 tables six values; RC added a seventh; no OQ tracks reciprocity.
- **OQ-RC-009 (EV acceptance of `evidence.sources` field OR RC-019a rewrite to EV-023a).** See B5. The fabricated field / stricter rule needs resolution.
- **OQ-RC-010 (PL workflow-registry-lock primitive).** RC-002a cites PL §4.3 for a contract that doesn't exist; either PL adopts or RC owns.
- **OQ-RC-011 (ON escalation-to-operator contract real home).** RC-025 cites ON for quarantined state and ON-008 for operator_escalation_required; ON has neither. Either file a coordinated ON amendment OR own the surface here and rewrite the citations.

Fix: add these four OQs at minimum OR resolve them in-pass.

## 7. Definitional drift

- **"Verdict" — glossary vs requirement cardinality.** Glossary (spec.md:62) says six values. RC-020 (spec.md:481) says seven. schemas.md VerdictEvent record comment (schemas.md:65) says six; ENUM below lists seven. RC-009 (spec.md:334) talks about "the 6-detection-category taxonomy" while §8 lists 11. At least four internal contradictions.

- **"Detection category" count.** §1 Purpose (spec.md:41) says "11 detection categories"; §8 lists 11 sub-sections. RC-009 (spec.md:334) locks in the "6-detection-category taxonomy." Residue from an earlier count. Should be "11 categories across 6 top-level groupings" or just "11 categories."

- **"Category" spelling.** spec.md uses "Cat N" (e.g., Cat 3a) as shorthand. schemas.md §6.1 ENUM uses lowercase hyphenated `cat-3a`. Both appear; no canonical form declared. Fix: name the canonical form in the glossary (Cat N in text; cat-N as enum value).

- **"Investigator run" vs "reconciliation run".** RC uses `investigator_run_id` throughout (schemas.md InvestigatorInput, VerdictEvent, MalformedVerdictPayload, StaleVerdictPayload). EV §8.6.2 uses `reconciliation_run_id`. Same thing, different name.

- **"target run" vs "outer run".** Both appear in prose (spec.md:285 "target run"; spec.md:429 "outer run being reconciled"). Schema field is `target_run_id`. R1 critic flagged this; R1 integration didn't reconcile.

- **"divergence" without a mechanical definition.** RC-014 (spec.md:390): "On detecting divergence, the detector MUST emit `store_divergence_detected`." No §3 glossary entry. R1 critic flagged this; R1 integration didn't add one.

- **"in-flight run" without a mechanical definition.** Used in §8 intro, RC-010, §7.1 pseudocode, RC-INV-001. R1 critic flagged; not added. Operator-nfr.md §3 (operator-nfr.md:73) defines `in_flight(run)` mechanically; RC should cite or mirror.

- **"snapshot" — scope.** RC-024 defines staleness in terms of target run's git branch tip + target bead's Beads audit. But schemas.md §6.1 `SnapshotToken.git_head_hash` is typed as "SHA of project HEAD" (schemas.md:23). Project HEAD is not the same as the target run's task-branch tip. The ontology mismatch R1 implementer flagged is unresolved.

- **"store" count.** RC-019a says "≥2 of the three stores" (spec.md:390). What is the third store? git, Beads, JSONL. JSONL is *observational* per RC-014; it is NOT a state authority (RC-INV-001 locks git-wins). Corroborating against JSONL doesn't establish real divergence; a JSONL-claim-of-divergence needs one of git or Beads to back it up. Which is exactly EV-023a's rule. RC-019a's "≥2 stores" framing is a cleaner phrase for "authority + observation" — but the phrase itself suggests three co-equal stores, which RC-014 denies.

## 8. Template conformance

Walking the template:

- **§0 Front matter.** Present. All required fields. `spec-category: runtime-subsystem`; `spec-template-version: 1.1`; `version: 0.3.0`. OK EXCEPT the category choice: runtime-subsystem triggers AR-053 envelope obligation.

- **§1 Purpose.** Present (spec.md:28). ≤200 words OK. Names normative scope. ✓

- **§2 Scope.** Present. In-scope (spec.md:36) and out-of-scope (spec.md:50) enumerated. ✓

- **§3 Glossary.** Present (spec.md:58). Issues: glossary says "six enum values" (contradicts RC-020 seven); no `divergence` entry; no `in-flight run` entry.

- **§4 Normative requirements.** Present. Under taxonomy-first shape, §8 precedes §4 on-page. ✓
  - **§4.a Subsystem envelope — ABSENT.** Required by AR-053 for runtime-subsystem specs. Blocking unless category switches to foundation-cross-cutting.

- **§5 Invariants.** Present. Five IDs (three retired as stubs, two active with Sensor blocks). RC-INV-004's sensor references a fabricated field (see B5).

- **§6 Schemas and data shapes.** Present. `spec.md` §6 is a stub pointing at schemas.md; schemas.md carries §6.1–§6.6. Multi-file split is template-legal. Sibling-rule compliance verified (status: supplement, same spec-id, inter-sibling citations use the prescribed form, revision history only in spec.md).

- **§7 Protocols and state machines.** Present (spec.md:656). Two pseudocode blocks. §7.2's `execute_verdict` branch on `stale` doesn't capture the Cat 3b re-execution path cleanly, but that's interpretation-level.

- **§8 Error and failure taxonomy.** Present (spec.md:76). 11 categories + §8.12 action-map + §8.13 failure-commit deferral. Correct position (before §4 in the taxonomy-first reading order).

- **§9 Cross-references.** Present. §9.1 depends-on, §9.2 reverse-dependencies stub, §9.3 co-references. Depends-on list is expanded in v0.3.0; adequate.

- **§10 Conformance.** Present. Core MVH profile declared. §10.2 test-surface obligations are prose (OQ-RC-006 tracks testing.md migration).

- **§11 Open questions.** Present. Seven OQs. Missing OQs for WM reciprocity, EV field reality, PL lock primitive, ON escalation home (see §6 above).

- **§12 Revision history.** Present. v0.3.0 row is very long and contains inaccurate claims (see §2).

- **§A Appendices.** §A.4 reverse-drift migration map present. No §A.1, §A.2, §A.3. Not mandatory per template (§A is O) but R1 critic recommended §A.2 counter-examples for orthogonality and §A.3 rationale for taxonomy count — both non-trivially load-bearing for the taxonomy-first shape.

**Per-requirement discipline:**

- **Tags:** lines. Spot-checked 15 requirements (RC-001, RC-002, RC-002a, RC-003, RC-003a, RC-004, RC-005, RC-010, RC-012, RC-014, RC-015, RC-019a, RC-020, RC-020a, RC-023). All present. ✓
- **Axes:** lines. Required when LLM/IO/state-mutation. RC-002a has Axes (IO ✓). RC-003a has Axes (✓). RC-012 has Axes (✓). RC-013 has Axes (non-idempotent ✓). RC-014 is MECHANISM but mutates no state — OK to omit Axes. RC-015 has Axes (LLM ✓). RC-016 has Axes (cognition ✓). RC-019a has Axes (✓). RC-020a has Axes (✓). Spot-check passes.

**MUST/SHOULD discipline.** Scanned for "typically" / "usually" / "typically" inside normative MUST blocks:
- RC-018 (spec.md:463): "MUST be indistinguishable from an investigator-emitted `escalate-to-human` in the operator-facing surface" — wording is normative but "indistinguishable" is reader-interpretive.
- RC-020 (spec.md:483): clean.
- §8 per-category "Escalation path" prose uses "typically" but these are INFORMATIVE descriptions, not MUST blocks — acceptable.
- RC-025 bullets use "idempotent at the dispatch layer (the next dispatch check sees the outer run already running)" — the "sees" is descriptive, OK.

Overall MUST/SHOULD discipline reasonable.

## 9. New failure modes surfaced by v0.3.0

1. **Seven-value verdict emitted to WM classifier: malformed-verdict cascade.** An investigator emits `no-op-accept`. RC's verdict-execution path handles it per schemas.md §6.2. But any WM sensor enforcing WM-036's "verdict not in six-value table → classification-error" sees the mismatch. If WM's classifier runs as a sensor at the verdict-routing boundary, every `no-op-accept` becomes a spurious classification-error. The cascade is silent at the spec level; the first time it surfaces is when S06 or an equivalent tries to act on the verdict.

2. **Priority-ordered Cat 0 → 6b → 6a → ... → 1 evaluation with a detector that panics.** RC-003a says first-match wins; doesn't say what "match" means if a detector throws. Implementer has three plausible choices (skip/fall through, route to Cat 6b, halt all classification). Priority order is undefined under panic.

3. **Concurrent reconciliation lock fail-classify as Cat 4.** A second dispatch within 5s gets classified Cat 4 (spec.md:285). Cat 4's auto-resolver is "re-arm the retry/backoff timer, or re-present the gate to the relevant actor." Neither path applies to "wait for lock." The auto-resolver will be handed a run with no retry state → undefined behavior.

4. **Cat 3a prescribes the pre-BI-031 audit-log idempotency-key read.** Under BI v0.3.0, Cat 3a's auto-resolver (per RC) asks Beads for an audit-log query that BI-031 explicitly moved away from. If implemented per RC's §8.4a prose, the adapter has to implement a non-BI-031-conformant path.

5. **Investigator subprocess emits before run_id is registered.** PL-003b rejects unknown run_ids pre-ready. If an investigator subprocess emits a heartbeat before the daemon's dispatcher has registered the reconciliation run_id in the in-memory model, the heartbeat is rejected and the subprocess's watcher loop may interpret it as protocol-mismatch. Ordering undeclared.

6. **RC-INV-004's sensor (`evidence.sources.length >= 2`) returns a type error at runtime.** The event-model payload has no such field. The sensor's validation check will always fail (or be skipped by a well-meaning implementer who notices the missing field). Either way, the invariant is unsensed in practice.

7. **RC-INV-001 sensor requires `workflow_class` registry tag** — but RC-002 declares `workflow_class` as a schemas.md §6.5 RC-owned extension to EM's Workflow record. If EM rejects the extension (per OQ-RC-002), the registry tag lives in RC-controlled metadata only. The sensor's ability to "filter `reconciliation_verdict_*` events and join against Workflow registry" then requires an RC-owned Workflow registry view. The sensor is declared at RC but the tag home is split. The AR-042 sensor obligation is nominally satisfied, but the Workflow registry is owned by EM/S01. Operational responsibility is unclear.

8. **`no-op-accept` verdict on a Cat 2 non-idempotent run creates a reclassification loop.** As discussed in §5 under-spec: the outer run retains whatever state made it Cat 2 in the first place; next cadence tick reclassifies the same way. Without a suppression marker, the investigator runs twice. Each run has a Cat 2 default budget (600s). Four hours of wall-clock on a single run before some other feedback loop cuts in. Live-lock failure mode.

## 10. Affirmations

Four decisions hold up.

1. **Harmonik-Verdict-Executed trailer reclaimed via schemas.md §6.4.** Clean declaration, placement rule (descendant of verdict commit, not on the verdict commit itself), emission contract (exactly once per execution, Cat 3b re-execution appends a new one). The ownership migration from EM is properly noted in cross-spec-coordination. This is the one R1 fix that landed completely and well.

2. **RC-003a priority-ordered first-match.** The priority order is published. Rationale per-pair is in the RC-003a text. The rule is mechanically deterministic and the overlap-pair walk R1 critic did becomes tractable against this ordering. If a reader follows the priority, they get one category per run every time.

3. **Three retired invariants (INV-002/003/005).** Selection-test compliance achieved. Retirement stubs are the right shape; no semantic loss. The remaining two (INV-001, INV-004) are genuinely cross-subsystem — INV-001 spans daemon Go code + event-model audit, INV-004 spans detector dispatch + EV-023a (modulo the fabrication on the field). Invariant surface is now lean.

4. **RC-001/002/003 reconciliation-as-workflow + bounded recursion.** The architectural move is clean. Verdict-only commit rule (RC-002) + no-mid-investigation-durable-state (RC-003) mutually imply bounded recursion. One of the more elegant spec shapes in the corpus.

## 11. Recommendation

**Do NOT transition to `reviewed`.** Seven blocking findings + eight important findings + downstream coordination with EV, WM, PL, and ON is required before a `reviewed` transition is credible.

**Blocking (must land before `draft` → `reviewed`):**

1. **Add §4.a Subsystem envelope OR change spec-category to `foundation-cross-cutting`.** Template-conformance per AR-053. Pick one. If AR-053 rationale lists reconciliation under `foundation-cross-cutting` (it does — architecture.md:80), the front-matter category is the more-obviously-wrong end.

2. **Re-sync §8.12 and schemas.md §6.3 on Cat 3 typical verdicts.** Add `no-op-accept` to schemas.md §6.3 Cat 3 row OR remove from §8.12. R1 item I6 self-seeded the drift.

3. **Fix RC-019a / RC-014 / RC-INV-004 / §3 glossary against EV-023a actual semantics.** Drop the `evidence.sources` field reference; rewrite "≥2 of three stores" to EV-023a's actual "corroborated by git OR beads" rule; fix RC-INV-004's sensor to query an existing event-model field. File OQ-RC-009 if the spec genuinely wants stricter corroboration (OQ queues the EV amendment).

4. **Fix ON-008 escalation fabrication.** Either (a) file a coordinated ON amendment naming the real owner of `operator_escalation_required` and `quarantined` state, with an OQ-RC-011 tracking ON acceptance; or (b) rewrite RC-025's action + schemas.md §6.2 row to own the escalation surface from the RC side without claiming ON ownership.

5. **Fix internal enum-cardinality inconsistencies.** §3 glossary, schemas.md §6.1 VerdictEvent comment, RC-009 "6-detection-category" — four call-sites to align to seven-value / 11-category.

6. **Fix BI §4.8 → §4.10 citation cluster.** ~10 cites for adapter idempotency and intent-log should be §4.10. §4.8 is version-pin only. If the author means both (§4.8 for CLI surface + §4.10 for idempotency), cite both or split per context.

7. **File OQ-RC-010 for PL workflow-registry-lock primitive OR own it in RC.** RC-002a cites PL §4.3 for a contract that doesn't exist.

**Important (should land but can be OQ-deferred):**

8. **Refactor RC-023 and RC-025 payload-field tuples into schemas.md.** Declare `MalformedVerdictPayload` and `VerdictExecutedPayload` in schemas.md §6.1 or cite event-model §8.6.4/.5 directly.

9. **File OQ-RC-008 for WM acceptance of seventh-value verdict.** Or coordinate a WM v0.5 row addition.

10. **Update Cat 3a detection rule and auto-resolver against BI-031 reframe.** §8.4a and RC-025 reopen-bead bullet should follow the status-check-before-reissue protocol of BI-031, not the pre-BI-031 audit-log-idempotency-key model.

11. **Fix RC-015 LaunchSpec field enumeration.** Either cite HC-006's actual field list or switch to the "investigator assembles InvestigatorInput from snapshot-bounded reads" framing (R1 critic Challenge 5 alternative (a)).

12. **Fix RC-022 citations** (EM §4.1 → §7.1 for orchestrator; WM §4.7 → §4.4/§4.6 for worktree-reset).

13. **Declare "quarantined" state in a real enum.** See §3 item 6.

14. **Fix Cat 4 overload in RC-002a.** Either a new minor category for "awaiting-lock" or a non-Cat-4 disposition.

15. **Define "divergence" and "in-flight run" in §3 glossary.** Cite operator-nfr.md for in-flight.

**Recommended (post-first-blocker fix):**

16. Add §A.2 counter-examples for the top-5 overlap pairs walking RC-003a's priority order. This is what R1 critic line 237 recommended; cheap vs. payoff.

17. Add §A.3 rationale for the 11-category count (RC-009's settled-shape position). Currently readers see an integer count and have to derive from §8 enumeration.

18. Reconsider RC-017's per-category budgets (600/300/900). Either move to S01 (per §2.2 scope) or calibrate against a published assumption about investigator turns.

19. Add a detector-error-mode rule (RC-003a's undefined panic behavior).

20. Define the `no-op-accept` suppression marker to prevent reclassification loops (§9 item 8).

**Fix-size estimate:** spec.md 880 lines; schemas.md 221 lines. Blocking + Important set: ≈150 lines changed across the two files; 4 coordinated sibling changes (WM accepts seventh verdict; ON or EV adapts for escalation/corroboration; PL for workflow-registry-lock; BI reference re-anchoring). None of the blockers requires rethinking the taxonomy or the central architectural moves.

None of the findings reopens the locked 11-category decision or the reconciliation-as-workflow shape. The shape is right. The integration was rushed; the fix is concrete.
