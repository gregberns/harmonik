# Round 1 Cross-Spec Architect Review — reconciliation/spec.md + schemas.md v0.2.0

## Verdict summary

RC occupies the right role in the foundation graph — it claims the ownership
a "reconciliation-as-workflow" design requires: the category taxonomy, the
verdict vocabulary, the action-mapping dispatch contract, and the
investigator-agent contract. The two-file split is coherent and template-
legal. Scope boundaries with EV, HC, and EM are mostly clean; the scope-leak
surface is narrow.

The dominant finding is **pervasive citation drift** — roughly 79% of
inter-spec citations are stale (52 of 66). RC cites the pre-cleanup anchor
scheme for all five declared `depends-on` specs (execution-model's
`Harmonik-Verdict-Executed` trailer now RC-owned, event-model's §3.N →
§8.N/§6.3 shift, beads-integration's §10.N → §4.N migration, control-points's
§6.N → §4.N for policy+skills). Process-lifecycle (§8.2 → §4.2) and
operator-nfr (§7.N → §4.N) citations are also stale. This is the "second
citation-migration pass deferred" flag; scale is large and paired with
extensive **reverse-drift** — every spec that back-cites `[reconciliation.md
§9.N]` now cites a section that is a cross-references section, not the
normative content. RC should publish a migration map.

Secondary findings: `depends-on` list is defensible as-is; RC's verdict-
execution table mostly cleanly dispatches mechanical actions to owning
subsystems, with two small WHO → WHAT gaps; `workflow_class = reconciliation`
metadata tag is introduced without an owner-of-field declaration.

## Dependency graph correctness

### Front-matter `depends-on`

Declared: `execution-model, event-model, handler-contract, control-points,
beads-integration` (spec.md lines 14–20).

Cross-check against body cites:

| Dep listed | Body cites | Verdict |
|---|---|---|
| execution-model | §4.1, §4.2 EM-009, §4.3 EM-014, §4.4 EM-016/017/018, §4.5 EM-023/026, §4.7, §5 EM-INV-005, §6.2, §4.10 EM-044/045 | Correct forward dependency. |
| event-model | §3.2 (×13 — see Citation correctness), §3.6, §4.1, §3.1 | Correct as a category, but the cited anchors do not exist in event-model; see Citation correctness. |
| handler-contract | §4.6, §4.11 | Correct as a category; §4.6 resolves, §4.11 is a named subsection. |
| control-points | §6.5, §6.11 | Problem: §6.5 does not hold policy YAML / budget_ref; §6.11 does not exist (skill declaration is §4.11 in control-points). See Citation correctness. |
| beads-integration | §10.3, §10.4, §10.7, §10.8, §10.8a | Problem: beads-integration migrated §10.N → §4.N and §6.N; every cite is stale. |

Process-lifecycle and workspace-model and operator-nfr and architecture and
handler-contract all appear in the body as normative-looking cites ("MUST
transition to `degraded` status per [process-lifecycle.md §8.2]") but only
appear in §9.3 Co-references (lines 681–690). The prose reads them as
dependencies; §9.3 classifies them as read-only. That boundary case warrants
review per the co-dep rules in components.md; see below.

### Cycle risk

- **RC ↔ EV.** EV back-cites RC at §9.1/§9.2/§9.3/§9.3a/§9.5 (all stale;
  actual anchors are §4.1, §8, §4.3, §4.5). The EV back-cites appear in
  EV's §9.3 Co-references, not §9.1 Depends on. Directional resolution:
  EV depends on RC for reconciliation event SEMANTICS, RC depends on EV
  for event SHAPES. RC placing EV in `depends-on` is correct: RC-INV-001,
  RC-011, RC-014 read EV envelope + replay semantics as load-bearing
  contracts, not merely payload names.

- **RC ↔ EM.** EM v0.2.0 dropped EV from its own `depends-on` to break the
  EM ↔ EV cycle (EM revision history line 1064). EM back-cites RC at
  `[reconciliation.md §4.3 RC-010]` (EM line 964) in §9.3. One-way, clean.

- **RC ↔ HC, RC ↔ CP, RC ↔ BI.** HC back-cites `§9.4b` (bootstrap form,
  stale); BI back-cites `§9.5` (stale); CP has no reverse dep. RC depends
  forward on all three. No cycles.

- **RC ↔ PL, ON, WM.** Each back-cites RC at §9.N anchors; each is cited
  by RC in §9.3 Co-references only. Directional resolution clean; anchor
  mismatch is the universal reverse-drift pattern.

### Conclusion — dependency graph shape is correct; list is defensible

No cycle introduced by RC. `depends-on` list accurately captures the forward
normative dependencies. Handler-contract, control-points, and beads-integration
could arguably be moved to §9.3 for the minority of cites that are "read-only
consumption" — but the body has normative forward references to all three, so
keeping them in `depends-on` is right.

## Two-file split audit

### Split is template-legal

Template v1.1 §Multi-file split (lines 510–547) permits splitting when a spec
exceeds ~1000 lines. `reconciliation/spec.md` is currently 752 lines and
`schemas.md` is 178 lines — together ~930. The split is under the template's
split threshold but permitted (the threshold is a SHOULD, not a MUST). The
content-type split (schemas into sibling) follows the template's prescribed
split structure (line 512–523: `spec.md` + `schemas.md`).

### Sibling-file rules compliance

Per template lines 524–541:

1. **Rule 1: `spec.md` keeps §§1–5, §9–12, stubs §6.** ✓ `spec.md` §6
   (lines 568–598) is a proper stub: it lists the schemas with pointers to
   `[schemas.md §6.1]`, `[schemas.md §6.2]`, `[schemas.md §6.3]`, and
   retains §6.4 (Schema evolution) and §6.5 (Co-owned event payloads) inline.
   The INFORMATIVE note at line 244 explicitly flags the §8.12 vs [schemas.md
   §6.3] duplication with a synchrony-is-a-lint-failure rule — this is the
   canonical way to handle the taxonomy-first shape's table-in-§8 vs table-
   in-§6.3 tension.

2. **Rule 2: Siblings are `status: supplement` with same `spec-id`.** ✓
   `schemas.md` front matter line 7–8 declares `spec-id: reconciliation,
   status: supplement`. Correct.

3. **Rule 3: Inter-sibling citations use the inter-spec form.** ✓
   `spec.md` cites `[schemas.md §6.1]`, `[schemas.md §6.2]`, `[schemas.md
   §6.3]` (lines 388, 432, 444, 480, 572–578, 643, 705). `schemas.md` cites
   `[spec.md §4]`, `[spec.md §6.4]`, `[spec.md §8.12]` (lines 71, 150, 168).
   Both directions follow the prescribed form.

4. **Rule 4: Requirement IDs are spec-wide.** ✓ All RC-NNN IDs appear in
   spec.md only; schemas.md references them (RC-019 at line 142, RC-018 at
   line 411, RC-014 at line 176) without introducing new IDs. Correct.

5. **Rule 5: Revision history lives in `spec.md`.** ✓ Only spec.md carries
   §12; schemas.md notes only `last-updated: 2026-04-23` in its front matter.
   Correct.

### One split inconsistency

schemas.md line 168 says "payload schemas for the following events are
declared in [event-model.md §3.2]; this spec is normative for the emission
timing per [spec.md §4] requirements." The "per [spec.md §4]" wording is
template-legal but not how the template's split-rule example phrases it —
the usual form is "per `[spec.md §<N>] <RC-NNN>`" citing a specific
requirement. This is minor; pass.

### Two-copy table divergence risk

§8.12 in spec.md and §6.3 in schemas.md both carry the category × action
table. Template does not forbid duplication, and both files explicitly flag
"divergence is a lint failure" (spec.md line 244, schemas.md line 150). But
**the two tables already disagree**:

- schemas.md §6.3 (line 163 Cat 6a) lists the Cat 6a detectors: "Workspace
  missing AND transition-record absent; OR trailer-vs-sibling-file mismatch;
  OR worktree has uncommitted git-in-progress op (rebase/merge/cherry-pick/
  bisect); OR bead `in_progress` with two+ task branches each advertising
  `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`."
- spec.md §8.11 Cat 6a (lines 193–201) lists the same detectors in prose but
  inverts the first two (workspace-path absence is the FIRST bullet) and
  uses slightly different wording for the git-in-progress-op row.

The wording differences are below the lint threshold, but the divergence
points directly at the risk the dual-table rule creates. Recommend one
of: (a) make §8.12 a pointer-only stub that says "full table in [schemas.md
§6.3]"; (b) make schemas.md §6.3 a pointer back. The taxonomy-first reading
order would preserve §8's role without needing the full table inline.

## Citation correctness

Walking every inter-spec citation.

### event-model citations (17 cites total)

EV §3 is Glossary only (EV lines 52–66); payload schemas live at §6.3;
taxonomy at §8.6/§8.7. Every `[event-model.md §3.N]` cite in RC is stale:

- `§3.2` (×15) for event taxonomy and payload schemas → split into
  `[event-model.md §8.6]`/`§8.7` for taxonomy rows (`infrastructure_unavailable`
  at §8.7.15, `operator_escalation_required` at §8.6.9), and
  `[event-model.md §6.3]` for per-type payload schemas.
- `§3.6` (×2) for observational-vs-state-reconstruction replay split
  (spec.md line 65, §9.1 line 666) → `[event-model.md §4.5]` (EV-021–024).
- `§3.1` (schemas.md line 131) for envelope → `[event-model.md §6.1]`.
- `§3.3` (schemas.md line 23) for RFC 3339 advisory → `[event-model.md §4.2]`
  (no §3.3 exists).
- `§4.1` for UUID v7 `event_id` (spec.md line 323) — resolves but should
  point at §4.2 Clock and ordering (EV-008) or §6.1 envelope `event_id`.

### beads-integration citations (16 cites total)

BI migrated §10.N → §4.N / §6.N; every RC cite is stale. Mapping:

- `§10.3` → `§4.3` Beads-managed data + `§6.1` Record schemas (bead type)
- `§10.4` → `§4.4` Harmonik write surface (terminal transitions)
- `§10.7` → `§4.7` Store-authority rules
- `§10.8` → `§4.8` Version-pin + adapter layer
- `§10.8a` → `§4.10` `br`-adapter idempotency

This is the most concentrated drift in the spec: 16 cites, all wrong.

### execution-model citations (11 cites)

Ten correct; one load-bearing miss:

- §4.1 Workflow/Node/Edge, §4.2 EM-009 idempotency_class, §4.3 EM-014
  one-bead-many-runs, §4.4 EM-016/017/018 checkpoint contract, §4.5
  EM-023/026 cadence, §4.7 state reconstruction, §5 EM-INV-005 git-wins,
  §4.10 EM-044 rollback, §3 Glossary terms, §8 failure taxonomy — all
  resolve. (EM-045 existence not verified in this review; schemas.md line
  141 cites `EM-044, EM-045` — flag for spot-check.)
- **§6.2 for `Harmonik-Verdict-Executed` trailer (load-bearing miss).** EM
  v0.2.0 removed this trailer from §6.2 (EM revision history line 1064:
  "Removed `Harmonik-Verdict-Executed` from §6.2 trailer table (deferred to
  reconciliation §9.5b)"). EM §6.2 INFORMATIVE at line 747: "Reconciliation-
  specific trailers ... are declared by [reconciliation.md §4.5] when it
  lands; they are not owned by this spec." RC cites EM §6.2 in four places
  (spec.md §4.5 RC-025 line 482, §9.1 line 664, §8.5 Cat 3b line 145;
  schemas.md §6.2 line 146) for a trailer EM explicitly disowns. RC must
  declare the trailer shape itself (new subsection in schemas.md).

### handler-contract citations

| RC cite | Verdict |
|---|---|
| `[handler-contract.md §4.6]` for error propagation + subprocess cleanup (§2.2 line 51, §4.4 RC-018 line 413, §9.1 line 667) | Correct (HC §4.6 is "Error propagation across async boundaries" at HC line 272). |
| `[handler-contract.md §4.11]` for skill injection (§4.1 RC-004 line 277, §9.1 line 668) | Correct (HC §4.11 is "Skill injection" at HC line 446). |

Clean.

### control-points citations

| RC cite | Verdict |
|---|---|
| `[control-points.md §6.5]` for YAML policy surface / budget_ref (§4.4 RC-017 line 399, §9.1 line 669) | **WRONG**. CP §6.5 is "Co-owned event payloads" (CP line 900). Policy YAML shape is at §6.3 (CP line 717); budget semantics are at §4.5 (CP line 223). Correct anchor: `[control-points.md §4.5]` for budget semantics + `[control-points.md §6.3]` for YAML policy shape. |
| `[control-points.md §6.11]` for skill declaration (§4.1 RC-004 line 277, §9.1 line 670) | **WRONG**. CP's skill-declaration subsection is §4.11 (CP line 447). §6.11 does not exist. Correct: `[control-points.md §4.11]`. |
| `[control-points.md §6.2]`, `[control-points.md §6.4]` for gates/guards (§4.6 RC-030 line 524) | **WRONG**. CP §6.2 is "Role and freedom-profile schemas" (CP line 686); §6.4 is "Policy expression grammar (adopted)" (CP line 808). Correct: `[control-points.md §4.2]` for Gate semantics + `[control-points.md §4.4]` for Guard semantics. |

Three misses; the §6.N / §4.N swap pattern suggests RC was written against
an older CP numbering.

### workspace-model citations (4 cites, all stale)

WM migrated §5.x → §4.x; WM §A.4 (WM line 982) publishes a migration map:
`§5.1 Lease` → `§4.3 Lease model`; `§5.3 Session-log` → `§4.7`; `§5.8
Branching` → `§4.2` (or `§4.5` for merge); `§5.9 Re-run rule` → `§4.9`.

### process-lifecycle citations (5 cites, all stale)

PL's startup sequence is §4.2 (PL-005), not §8.2. Step references like
"§8.2 step 1a" and "§8.2 step 5" map to PL-005 steps 2 (PL-006 orphan
sweep) and 7 (reconciliation dispatch) respectively; RC should cite by
PL-NNN ID, since PL-005 has 8 flat numbered steps (no "1a" sub-letters).

### operator-nfr citations (5 cites, all stale)

ON normative sections are §4.x (§7 is protocols): `§7.3` → `§4.3` operator-
control state machine; `§7.5` → `§4.5` Schema compatibility window; `§7.8`
→ `§4.8` Restart RTO; `§7.10` does not exist — ON out-of-scopes CLI flag
surface at §2.1 line 53 with a components.md bootstrap cite; RC should
follow the same pointer.

### architecture citations (2 cites)

| RC cite | Verdict |
|---|---|
| `[architecture.md §4.2]` ZFC test (§4.5 RC-023 RATIONALE line 461, §9.3 line 689) | Correct (AR §4.2 is ZFC mechanism/cognition classification, AR line 118). |
| `[architecture.md §4.6]` Amendment protocol (§4.1 RC-006 line 289, §4.2 RC-009 line 309, §9.3 line 690, §11 OQ-RC-004 line 745) | Correct (AR §4.6 is Foundation amendment protocol, AR line 214). |

Architecture cites are clean. Matches the prior cleanup note (AR §1.N →
§4.N migration is done across the corpus with RC's AR cites among the
adopters).

### Citation correctness summary

Count of stale citations, by target spec:

| Target | Stale | Total | Stale % |
|---|---|---|---|
| event-model | 15 | 17 | 88% |
| beads-integration | 16 | 16 | 100% |
| execution-model | 4 (Harmonik-Verdict-Executed owner) | 11 | 36% (of which 1 is load-bearing) |
| handler-contract | 0 | 2 | 0% |
| control-points | 3 | 4 | 75% |
| workspace-model | 4 | 4 | 100% |
| process-lifecycle | 5 | 5 | 100% |
| operator-nfr | 5 | 5 | 100% |
| architecture | 0 | 2 | 0% |
| **Total** | **52** | **66** | **79%** |

The 79% miss rate is consistent with the task's flag "Second citation-migration
pass deferred." Flagging but not expecting a full fix.

## Scope leaks

### No event payload re-declaration

RC §6.5 (lines 584–598) correctly declares itself normative for WHEN each
event fires and points at event-model for the payload shape:

> "This spec is normative for WHEN each event fires; [event-model.md §3.2] is
> normative for the payload shape." (line 598)

RC does not redeclare any event payload. schemas.md declares three RECORD
shapes whose payload fields overlap with event payloads (`BudgetExhaustedPayload`,
`StaleVerdictPayload`, `VerdictEvent`), but each is declared as the typed
value the reconciliation workflow emits; event-model §6.3 would still own
the envelope-level shape. **Verify**: if event-model §6.3 does NOT declare
the per-type `reconciliation_verdict_emitted` payload and instead
cross-references `[reconciliation.md §6 VerdictEvent]`, the ownership is
clean. If event-model re-declares it, the two specs need to resolve
canonical location. event-model line 147 registers
`reconciliation_verdict_emitted` in the §8.6 table with payload columns
naming `investigator_run_id`, `target_run_id`, `verdict (per [reconciliation.md
§9.5])`, `rationale?` — which DOES defer to RC's verdict definition, so
co-owned is the intent. Note the verdict cite in that event-model row is
stale (`§9.5` should be `§4.5` or `schemas.md §6.1 Verdict`).

### No handler interface re-declaration

RC invokes handler-contract for investigator subprocess cleanup (RC-018 line
413) and skill injection (RC-004 line 277) but does not redeclare any
Handler or Session method, LaunchSpec field, or wire-protocol message type.
Clean.

### One scope rubric miscue

RC §6.5 enumerates 9 events. Of these:

- `reconciliation_category_assigned`, `reconciliation_verdict_emitted`,
  `reconciliation_verdict_executed`, `reconciliation_verdict_stale`,
  `reconciliation_verdict_malformed`, `reconciliation_budget_exhausted`,
  `store_divergence_detected` — RC is the WHEN owner (correct).
- `infrastructure_unavailable` — event-model §8.7.15 registers this under
  "Operator-control and daemon lifecycle." RC emits it (RC-012) but the
  event's primary source is PL per components.md. Cross-ownership is
  process-lifecycle emits it on Cat 0 detect; RC's detector is the trigger.
  Claim of "this spec's requirements drive emission" is defensible for the
  Cat 0 detector path but the event's WHEN is co-owned with PL-010 (PL line
  361: "emitted on Cat 0 failure (§PL-010)").
- `operator_escalation_required` — event-model §8.6.9 registers this under
  Reconciliation lifecycle. RC's RC-025 emits it on `escalate-to-human`
  verdict execution. operator-nfr §4.3 (ON-008 operator control state
  machine) also consumes it. Co-owned WHEN: RC on verdict, ON on pause-stop
  boundaries.

Recommend splitting §6.5 into "exclusive-WHEN-owned" and "co-owned WHEN"
sub-lists so the scope boundary is legible to readers.

## Taxonomy-first shape conformance

### Section numbering

Template §Spec-shape selection (template lines 38–41): "taxonomy-first — 
sections appear in the reading order `0, 1, 2, 3, 8, 4, 5, 6, 7, 9, 10, 11,
12, A`. Section *numbers* remain stable in both shapes — only the on-page
reading order shifts."

RC's on-page order (verified by heading scan):
§1 Purpose (line 23) → §2 Scope (line 29) → §3 Glossary (line 53) → §8 Error
and failure taxonomy (line 71) → §4 Normative requirements (line 250) → §5
Invariants (line 536) → §6 Schemas (line 568) → §7 Protocols (line 600) → §9
Cross-references (line 653) → §10 Conformance (line 692) → §11 Open
questions (line 717) → §12 Revision history (line 747).

✓ Matches template order exactly. §8 precedes §4 on the page; §3 → §8 jumps
correctly; §6 and §7 follow §5. Stable anchors are preserved
(cross-references like `§4.3 RC-014` resolve against the normative §4, not
an in-page ordinal).

### Shape-selection justification

Spec §1 line 27 says:
> "This spec is a separate file from execution-model.md because the taxonomy
> is cross-cutting (consumed by process-lifecycle.md §8.2 startup,
> operator-nfr.md §7.8 restart RTO, and workspace-model.md §5.9 re-run
> rule) and because its shape is taxonomy-first rather than
> requirements-first."

Both cited section numbers in the justification prose are stale (PL
startup is §4.2; ON restart RTO is §4.8; WM re-run rule is §4.9). But the
justification ITSELF — "shape is taxonomy-first because consumers reference
categories by number" — is correct. Fix the numbers without disturbing the
shape claim.

### Reading-order note

Line 69:
> "INFORMATIVE: Reading order for this spec is `1, 2, 3, 8, 4, 5, 6, 7, 9,
> 10, 11, 12, A` per `spec-shape: taxonomy-first`. Section numbers are
> stable; only the on-page sequence shifts. §8 appears next, then §4
> references it."

This note restates the template rule and correctly flags that §8 references
§4 via requirement IDs ("see RC-016"). Good.

## Reconciliation-as-workflow assertion

RC-001 (line 256) establishes reconciliation-as-workflow. The assertion is
load-bearing for the whole design. Does RC cite EM for workflow primitives,
CP for gates, EV for events — rather than re-declaring?

| Primitive | RC reference | Re-declare? |
|---|---|---|
| Workflow / Node / Edge types | "DOT-defined per [execution-model.md §4.1]" (RC-001 line 258) | No ✓ |
| Dispatch deterministic | "dispatched deterministically by the daemon" (RC-001) | No ✓ (daemon is owned by PL) |
| Event log | "event-logged per [event-model.md §3.2]" (RC-001 stale cite) | No ✓ |
| Checkpoint cadence exception | "explicit exception to [execution-model.md §4.5 EM-023]" (RC-002 line 264) | No ✓; RC cites EM-026's exception-key-ing mechanism |
| `workflow_class = reconciliation` metadata tag | RC-002 declares it | Introduces a workflow-metadata field |
| Wall-clock budget | "declared as a YAML policy field `wall_clock_seconds` ... attached via `budget_ref` per [control-points.md §6.5]" (RC-017 line 399, §6.5 cite stale) | No ✓; cites CP for budget semantics |
| Investigator agent | handler-contract §4.6 cleanup + §4.11 skill injection | No ✓ |
| `workflow_class` as a workflow-metadata field | RC introduces it | **Check**: does EM's Workflow record schema at EM §6.1 carry a `workflow_class` field? If EM's Workflow does not have this field, RC is IMPLICITLY extending the Workflow schema without EM-owner sign-off. Recommend: (a) add an open question asking whether `workflow_class` is an EM Workflow field or a reconciliation-only policy tag, OR (b) declare in RC-001/RC-002 that `workflow_class` is a workflow metadata tag registered with EM's Workflow schema under a co-owned extension path. |

**Scope issue**: The `workflow_class = reconciliation` tag is a workflow-
metadata field. The question is who owns the field's declaration. Options:

- Owner = EM, and RC consumes it. Then EM's Workflow RECORD (EM §6.1) MUST
  list `workflow_class` as a field; EM revision history does not mention
  this addition.
- Owner = RC, and RC is declaring a new workflow-metadata extension. Then
  the extension mechanism (how a spec adds a metadata field to another
  spec's type) needs declaration somewhere (architecture §4.6 amendment
  protocol, likely).

At the current draft, RC-002 uses the tag without sourcing it. Flag as OQ.

## Action-mapping dispatch (§8.12) — WHO → WHAT split

### Per verdict, who executes the mechanical action

RC-025 (line 476) and schemas.md §6.2 (lines 137–144) list verdict →
mechanical action. Walking the dispatch targets:

| Verdict | Mechanical action | Executing subsystem | RC cites the dispatch target? |
|---|---|---|---|
| `resume-here` | Re-dispatch outer run's current node | **EM** (orchestrator-core run-state machine) | Implicit; RC says "dispatching the outer run's next node is idempotent at the dispatch layer" (line 487) without citing the orchestrator-core. Recommend adding `[execution-model.md §4.1]` or `[execution-model.md §7.1]` run state machine as the target. |
| `resume-with-context` | Re-dispatch with investigator `context` injected per `[execution-model.md §4.1 EM-005]` | **EM** + context-injection surface; schemas.md line 140 cites EM-005 | ✓ |
| `reset-to-checkpoint` | Revert via `transition_kind = architectural-rollback` per `[execution-model.md §4.10 EM-044, EM-045]` | **EM** (transition kinds) + **WM** (worktree revert); schemas.md cites EM-044/045 but not WM for worktree-reset mechanics | Partial; recommend co-cite WM's worktree-state surface |
| `reopen-bead` | `br reopen` via adapter per `[beads-integration.md §10.8a]` (STALE) + workspace-model §5.9 re-run rule (STALE) | **BI** (reopen write) + **WM** (fresh worktree on re-claim) | ✓ targets cited but both cites stale |
| `accept-close-with-note` | Append annotation + close write through adapter | **BI** (close write) | ✓ |
| `escalate-to-human` | Emit `operator_escalation_required`; mark outer run "quarantined state per [operator-nfr.md §7.3]" (STALE) | **EV** (event) + **ON** (quarantined state machine) | ✓ targets cited but ON cite stale |

The WHO → WHAT dispatch is fundamentally clean — every mechanical action
lands in the spec that owns the mechanism. Two gaps:

1. `resume-here` does not cite its execution target. Under the verdict-
   execution table in schemas.md §6.2, the row's "Mechanical action" column
   says "Re-dispatch the outer run's current node (no context change)" but
   does not say WHO does the re-dispatch. Implicitly: the orchestrator
   (S01 / execution-model's run main loop). Add an EM cite.

2. `reset-to-checkpoint` cites EM for the transition-kind representation but
   not for the actual worktree-state reset (which is WM's concern — the
   worktree is reset to the state the checkpoint commit's tree describes).
   Add `[workspace-model.md §4.7]` session-log metadata co-cite (or whichever
   subsection owns worktree-reset mechanics; potentially §4.1 or §4.5).

The `workflow_class = reconciliation` exception keys on RC-002 / EM-026;
this part is well-done.

## Cat 6 escalation to operator — ON coordination

RC-INV-001 (line 540) + RC-025's `escalate-to-human` action (schemas.md
line 144) both emit `operator_escalation_required` and mark the run
quarantined. Cat 6b auto-escalates without investigator per §8.11a (line
210) with the same event.

### Ownership check

- Event `operator_escalation_required` — registered in event-model §8.6.9
  under Reconciliation lifecycle, with producer "daemon-core" and consumers
  "operator-observability, audit". WHEN is co-owned between RC (on verdict
  emission) and ON (on `quarantined` state).
- "Quarantined state" — RC cites `[operator-nfr.md §7.3]` (stale; should be
  §4.3 ON-008 between-task operator state machine) as the holder. ON §4.3
  declares the operator-control state machine; whether it declares a per-run
  "quarantined" sub-state is worth checking. If ON does not declare that
  sub-state, RC is introducing a run-state attribute without the owning
  spec. OQ worth flagging.
- Operator CLI surface for acknowledging an escalation — ON §2.1 line 53
  out-of-scopes CLI flag surface to "a separate operator-CLI-surface spec
  work." RC OQ-RC-002 (line 727) tracks the `harmonik confirm-verdict` /
  `harmonik veto-verdict` CLI — but that's for verdict override at RC-027,
  not for the Cat 6b / `escalate-to-human` acknowledgment path. An open
  question about "how does the operator ACK an escalation and clear the
  quarantined state" is latent but not flagged.

Recommend:
- Add OQ asking whether ON declares a per-run `quarantined` sub-state; if
  not, decide owner.
- Strengthen §9.3 operator-nfr cite list: add ON §4.3 for operator control
  state machine (for Cat 6 quarantine coordination) AND explicit prose tying
  `operator_escalation_required` emission back to the ON state surface.

## Verdict-vocabulary authoritativeness

RC declares the six-value `Verdict` enum in schemas.md §6.1 (lines 74–82).
RC-020 line 430 claims it normatively: "A verdict event's `verdict` field
MUST be exactly one of the six enum values listed in [schemas.md §6.1
Verdict]."

Downstream consumption:
- event-model §8.6.3 `reconciliation_verdict_emitted` row cites `verdict
  (per [reconciliation.md §9.5])` — STALE anchor (should be `§4.5` or
  `schemas.md §6.1 Verdict`), but the owner-declaration is correct: EV
  names verdict but defers enum to RC.
- workspace-model WM-036 (per task prompt) consumes `reopen-bead` / `reset-
  to-checkpoint`. Verify the WM cite resolves to the RC enum.

Is RC explicit that it is the authoritative enum home? Line 430 says "MUST
be exactly one of the six enum values listed in [schemas.md §6.1 Verdict]"
— normatively yes. The §10.3 Excluded conformance (line 713) does not
name verdict enum — implying verdict enum IS within RC's conformance scope.
schemas.md §6.1 ENUM Verdict lists all six values (line 75). Good.

Recommend: add a one-line normative statement in RC-020 or the §6.5
preamble: "This spec is authoritative for the Verdict enum; downstream
specs consume the enum by cross-reference and MUST NOT redeclare."

## Bootstrap citations

No `[docs/foundation/components.md §N]` leftovers in spec.md or schemas.md.
All forward cites use the `[<spec-id>.md §N.N]` form. Clean.

Two cites that the spec text has but that don't resolve to real sections
are effectively forward references into sections that DO NOT EXIST in the
target spec (not bootstrap leftovers, just stale — e.g., `[operator-nfr.md
§7.10]`, `[control-points.md §6.11]`). These are cleaned up via the Citation
correctness table above, not via bootstrap migration.

## Reverse drift — RC should publish a migration map

The task highlight is accurate: **every foundation spec that back-cites
reconciliation does so at `[reconciliation.md §9.N]`** — see Dependency
graph section above. Counts:

- `process-lifecycle.md`: 13 back-cites at §9.1 / §9.1a / §9.2 / §9.2a /
  §9.3.
- `event-model.md`: 9 back-cites at §9.1 / §9.2 / §9.3 / §9.3a / §9.4 /
  §9.5.
- `operator-nfr.md`: 7 back-cites at §9.1 / §9.2 / §9.2a / §9.3 / §9.4 /
  §9.4a / §9.5b.
- `beads-integration.md`: `§9.5`.
- `workspace-model.md`: per task prompt, WM consumes RC verdict enum; if
  WM-036 cites `§9.5` then that back-cite is also stale.
- `handler-contract.md`: `§9.4b` (bootstrap form).

All consumed `§9.N` anchors map to NEW §4.N / §8.N anchors:

| Old `§9.N` (reverse drift) | New anchor |
|---|---|
| §9.1 reconciliation-as-workflow | §4.1 (RC-001..RC-006) |
| §9.1a workflow idempotence | §4.1 RC-003 (bounded recursion) + §5 RC-INV-002 |
| §9.2 category taxonomy | §8 (§8.1–§8.11a) |
| §9.2a action-mapping | §8.12 (and schemas.md §6.3 canonical copy) |
| §9.3 detectors | §4.3 (RC-010..RC-014) |
| §9.3a post-crash-window guardrail | §4.3 RC-014 (forbidden/permitted list) |
| §9.4 investigator-agent contract | §4.4 (RC-015..RC-019) |
| §9.4a wall-clock budget | §4.4 RC-017, RC-018 |
| §9.4b investigator LaunchSpec (HC cite) | §4.4 RC-015 (snapshot token) or schemas.md §6.1 InvestigatorInput |
| §9.5 verdict vocabulary | §4.5 + schemas.md §6.1 Verdict |
| §9.5b verdict execution | §4.5 RC-025, RC-026, RC-027 |

### Recommendation

Add a `§A.4 Reverse-drift migration map` appendix to spec.md exactly
modeled on workspace-model's §A.4 (WM line 982). Shape:

```
| Legacy §9.N back-cite | Current anchor | Subject |
|---|---|---|
| `§9.1` | `§4.1 (RC-001..RC-006)` | reconciliation-as-workflow |
| `§9.2` | `§8` | category taxonomy (see §8.1 through §8.11a) |
| `§9.2a` | `§8.12` (and [schemas.md §6.3] canonical) | action-mapping |
| `§9.3` | `§4.3 (RC-010..RC-014)` | detectors |
| `§9.4` | `§4.4 (RC-015..RC-019)` | investigator contract |
| `§9.5` | `§4.5 + [schemas.md §6.1 Verdict]` | verdict vocabulary |
| `§9.5b` | `§4.5 RC-025..RC-027` | verdict execution |
```

This lets each of the 30+ reverse-drift cites be resolved by a single read
of RC §A.4 without every upstream spec editing every back-cite immediately.
Matches the WM precedent.

## Recommended graph edits

In priority order:

### 1. Reclaim `Harmonik-Verdict-Executed` trailer ownership (load-bearing)

EM v0.2.0 deferred this trailer to RC (EM line 747 + revision history line
1064: "Removed `Harmonik-Verdict-Executed` from §6.2 trailer table (deferred
to reconciliation §9.5b)"). RC currently cites `[execution-model.md §6.2]`
for the trailer (spec.md line 482, 664, 145; schemas.md line 146) — a
dangling cite.

Actions:
- Add a new subsection `§6.6 Reconciliation-specific commit trailers` to
  `schemas.md` declaring the `Harmonik-Verdict-Executed: true` trailer
  format (type, presence-only semantics, where it is appended).
- Update every back-cite from `[execution-model.md §6.2]` → `[schemas.md
  §6.6]` (or equivalent).
- Notify EM authors: EM §6.2 INFORMATIVE note at line 747 can now say
  "declared by `[reconciliation/schemas.md §6.6]`" instead of
  "`[reconciliation.md §4.5] when it lands`".

### 2. Mass citation migration pass (deferred per task, but tracked here)

- event-model: 15 cites. `§3.2` → `§8.6`/`§8.7` for taxonomy rows, `§6.3`
  for per-type payloads, `§6.1` for envelope. `§3.6` → `§4.5`. `§3.3`
  → `§4.2`.
- beads-integration: 16 cites. `§10.N` → `§4.N` / `§6.1`. The mapping
  is straightforward (10.3→4.3/6.1, 10.4→4.4, 10.7→4.7, 10.8→4.8,
  10.8a→4.10).
- control-points: 3 cites. `§6.5` → `§4.5` + `§6.3`; `§6.11` → `§4.11`;
  `§6.2`/`§6.4` → `§4.2`/`§4.4`.
- workspace-model: 4 cites. `§5.1` → `§4.3`; `§5.3` → `§4.7`; `§5.8` →
  `§4.2` or `§4.5`; `§5.9` → `§4.9`.
- process-lifecycle: 5 cites. `§8.2` → `§4.2` everywhere; "step 1a/5"
  → cite by PL-NNN IDs (PL-006 for orphan sweep; PL-005 step 7 for
  reconciliation dispatch).
- operator-nfr: 5 cites. `§7.3` → `§4.3`; `§7.5` → `§4.5`; `§7.8` →
  `§4.8`; `§7.10` → out-of-scope pointer via `§2.2`.

### 3. Publish reverse-drift migration map

Add `§A.4` to `spec.md` exactly like WM's §A.4 (WM line 982). See Reverse-
drift section for suggested table.

### 4. Resolve `workflow_class` ownership

RC-002 introduces `workflow_class = reconciliation` as a workflow-metadata
field without citing where the field lives in the Workflow schema. Add
either:
- A cite to `[execution-model.md §4.1]` Workflow record (if EM declares
  the field), OR
- An OQ asking whether `workflow_class` is an EM schema extension (amend
  the Workflow record) or an RC-declared policy tag.

### 5. Declare Verdict enum authoritativeness

Add a one-line normative statement in §4.5 (near RC-020) or §6.5 preamble:
"This spec is authoritative for the Verdict enum ([schemas.md §6.1
Verdict]). Downstream specs consume by cross-reference and MUST NOT
redeclare."

### 6. Co-owned WHEN sub-list for §6.5

Split §6.5 into:
- exclusive-WHEN-owned (the seven `reconciliation_*` and `store_divergence_detected`
  events RC originates),
- co-owned WHEN (`infrastructure_unavailable` with PL-010;
  `operator_escalation_required` with ON-008 quarantined state).

### 7. Dispatch targets for `resume-here` and `reset-to-checkpoint`

- In schemas.md §6.2 Verdict-execution table, add the subsystem cite
  for `resume-here` (EM orchestrator run main loop).
- In the `reset-to-checkpoint` row, add a co-cite to WM for worktree reset.

### 8. Front-matter `depends-on` — keep as-is

All five entries in `depends-on` (execution-model, event-model,
handler-contract, control-points, beads-integration) are cited normatively
in the body. Template §0 allows co-dependency via one side owning the
forward dep and the other side citing co-ref; RC correctly places event-
model as a forward dep (EV's replay semantics are load-bearing for RC's
forbidden-uses rule).

Do NOT add process-lifecycle, operator-nfr, or workspace-model to
`depends-on`; those are properly one-way consumption relationships that
belong in §9.3 Co-references.

## Affirmations

1. **Taxonomy-first shape applied correctly.** Section numbering stable; on-page
   order matches template; §8 references §4's requirement IDs and §4 cites §8
   categories by number/sub-letter. INFORMATIVE reading-order note at line 69
   correctly flags the non-standard sequence.

2. **Reconciliation-as-workflow cleanly expressed.** RC-001–RC-006 use existing
   EM/EV/HC/CP primitives without redeclaring. The `workflow_class =
   reconciliation` metadata tag is the single load-bearing extension; bounded-
   recursion chain (RC-002 → RC-003 → RC-INV-002) is correctly chained.

3. **Two-file split is template-legal and mostly clean.** `spec.md` keeps
   §§1–5, §7, §9–12 + proper §6 stub; `schemas.md` carries `status: supplement`
   + same `spec-id`; inter-sibling citations use spec-relative form consistently.
   The §8.12 / §6.3 dual-table risk is self-flagged.

4. **Verdict-execution WHO→WHAT mostly clean.** Each verdict's mechanical action
   lands in the owning subsystem (BI for `br reopen`/`br close`, EM for
   `transition_kind`, WM for fresh worktree on re-claim, EV+ON for
   `escalate-to-human`). Per-verdict idempotency declared inline (RC-025).

5. **Scope leaks narrow.** No event payload re-declaration; no handler
   interface re-declaration; §6.5 co-owned-WHEN miscue is cosmetic.

6. **Malformed-verdict / staleness / budget-exhausted handling well-shaped.**
   RC-023 converts LLM schema violations to deterministic escalation; RC-024
   snapshot-bounded verdict execution prevents acting on stale analysis;
   RC-018 fallback `escalate-to-human` is the right posture. §8.11a Cat 6b
   rationale is sharply reasoned.

7. **Invariant set disciplined.** RC-INV-001–005 each constrain multiple
   subsystems, passing template §5 selection test. No invariant duplicates a
   §4 requirement.
